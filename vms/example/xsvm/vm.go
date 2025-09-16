// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/luxfi/log"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
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
	chainCtxIntf interface{},
	dbManagerIntf interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngineIntf interface{},
	fxsIntf []interface{},
	appSenderIntf interface{},
) error {
	// Type assertions to convert interfaces to concrete types
	chainCtx := chainCtxIntf.(*block.ChainContext)
	toEngine := toEngineIntf.(chan<- block.Message)
	// Create a logger since ChainContext doesn't have one
	logger := log.NewNoOpLogger()

	logger.Info("initializing xsvm",
		log.Stringer("version", Version),
	)

	// Store the ChainContext
	vm.chainCtx = chainCtx
	// DBManager doesn't have Current() method, use a versiondb directly
	baseDB := memdb.New()
	vm.db = versiondb.New(baseDB)
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
		log.Stringer("lastAcceptedID", vm.chain.LastAccepted()),
	)
	return nil
}

func (vm *VM) SetState(_ context.Context, state interfaces.State) error {
	// Pass the state directly since it's already the right type
	vm.chain.SetChainState(state)
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

// Status returns the uint8 status directly from the underlying block
func (b *blockWrapper) Status() uint8 {
	return b.Block.Status()
}
