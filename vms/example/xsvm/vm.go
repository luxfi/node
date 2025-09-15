// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"
	"go.uber.org/zap"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/database"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/example/xsvm/api"
	"github.com/luxfi/node/vms/example/xsvm/builder"
	"github.com/luxfi/node/vms/example/xsvm/chain"
	"github.com/luxfi/node/vms/example/xsvm/execute"
	"github.com/luxfi/node/vms/example/xsvm/genesis"
	"github.com/luxfi/node/vms/example/xsvm/state"

	smblock "github.com/luxfi/consensus/engine/chain/block"
	xsblock "github.com/luxfi/node/vms/example/xsvm/block"
)

var (
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
)

type VM struct {
	chainCtx     *block.ChainContext
	db           database.Database
	genesis      *genesis.Genesis
	toEngine     chan<- block.Message

	chain   chain.Chain
	builder builder.Builder
}

// GetChainID returns the chain ID
func (vm *VM) GetChainID(ctx context.Context) (ids.ID, error) {
	if vm.chainCtx != nil {
		return vm.chainCtx.ChainID, nil
	}
	return ids.Empty, nil
}

func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *block.ChainContext,
	dbManager block.DBManager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- block.Message,
	fxs []*block.Fx,
	appSender block.AppSender,
) error {
	// Use the logger from ChainContext
	logger := chainCtx.Log

	logger.Info("initializing xsvm",
		zap.Stringer("version", Version),
	)

	// Store the ChainContext
	vm.chainCtx = chainCtx
	vm.db = dbManager.Current()
	vm.toEngine = toEngine
	g, err := genesis.Parse(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis bytes: %w", err)
	}

	vdb := versiondb.New(vm.db)
	chainID := chainCtx.ChainID
	if err := execute.Genesis(vdb, chainID, g); err != nil {
		return fmt.Errorf("failed to initialize genesis state: %w", err)
	}
	if err := vdb.Commit(); err != nil {
		return err
	}

	vm.genesis = g

	// Create a context.Context with chain information
	chainContext := context.WithValue(context.Background(), "chainCtx", chainCtx)
	vm.chain, err = chain.New(chainContext, vm.db)
	if err != nil {
		return fmt.Errorf("failed to initialize chain manager: %w", err)
	}

	vm.builder = builder.New(chainContext, vm.chain)

	logger.Info("initialized xsvm",
		zap.Stringer("lastAcceptedID", vm.chain.LastAccepted()),
	)
	return nil
}

func (vm *VM) SetState(_ context.Context, state interfaces.State) error {
	// Import consensus to use consensus.State type
	var consensusState consensus.State = consensus.State(state)
	vm.chain.SetChainState(consensusState)
	return nil
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.chainCtx == nil {
		return nil
	}
	return vm.db.Close()
}

func (*VM) Version(context.Context) (string, error) {
	return Version.String(), nil
}

func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	api := api.NewServer(
		context.WithValue(context.Background(), "chainCtx", vm.chainCtx),
		vm.genesis,
		vm.db,
		vm.chain,
		vm.builder,
	)
	return map[string]http.Handler{
		"": server,
	}, server.RegisterService(api, constants.XSVMName)
}

func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return http.StatusOK, nil
}

func (*VM) Connected(context.Context, ids.NodeID, *version.Application) error {
	return nil
}

func (*VM) Disconnected(context.Context, ids.NodeID) error {
	return nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (block.Block, error) {
	blk, err := vm.chain.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return &blockWrapper{Block: blk}, nil
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (block.Block, error) {
	blk, err := xsblock.Parse(blkBytes)
	if err != nil {
		return nil, err
	}
	chainBlk, err := vm.chain.NewBlock(blk)
	if err != nil {
		return nil, err
	}
	return &blockWrapper{Block: chainBlk}, nil
}

func (vm *VM) BuildBlock(ctx context.Context) (block.Block, error) {
	blk, err := vm.builder.BuildBlock(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &blockWrapper{Block: blk}, nil
}

func (vm *VM) SetPreference(_ context.Context, preferred ids.ID) error {
	vm.builder.SetPreference(preferred)
	return nil
}

func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.chain.LastAccepted(), nil
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, blockContext *block.Context) (block.Block, error) {
	// Convert to smblock.Context for compatibility with builder
	smContext := &smblock.Context{
		PChainHeight: blockContext.PChainHeight,
	}
	blk, err := vm.builder.BuildBlock(ctx, smContext)
	if err != nil {
		return nil, err
	}
	return &blockWrapper{Block: blk}, nil
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return state.GetBlockIDByHeight(vm.db, height)
}

func (vm *VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	// xsvm doesn't need a custom HTTP handler
	return nil, nil
}

func (vm *VM) WaitForEvent(ctx context.Context) (core.Message, error) {
	return vm.builder.WaitForEvent(ctx)
}

// blockWrapper wraps an xsvm chain.Block to implement consensus block.Block
type blockWrapper struct {
	chain.Block
}

// Status converts the uint8 status to choices.Status
func (b *blockWrapper) Status() choices.Status {
	status := b.Block.Status()
	switch status {
	case 0: // Unknown
		return choices.Unknown
	case 1: // Processing
		return choices.Processing
	case 2: // Accepted
		return choices.Accepted
	case 3: // Rejected
		return choices.Rejected
	default:
		return choices.Unknown
	}
}
