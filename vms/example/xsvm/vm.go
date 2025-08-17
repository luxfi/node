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
	"github.com/luxfi/consensus/core"
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
	chainContext context.Context
	db           database.Database
	genesis      *genesis.Genesis
	toEngine     chan<- block.Message

	chain   chain.Chain
	builder builder.Builder
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
	// Convert ChainContext to context.Context for compatibility
	chainContext := context.WithValue(ctx, "chainContext", chainCtx)
	logger := consensus.GetLogger(chainContext)

	logger.Info("initializing xsvm",
		zap.Stringer("version", Version),
	)

	vm.chainContext = chainContext
	vm.db = dbManager.Current()
	vm.toEngine = toEngine
	g, err := genesis.Parse(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis bytes: %w", err)
	}

	vdb := versiondb.New(vm.db)
	chainID := consensus.GetChainID(chainContext)
	if err := execute.Genesis(vdb, chainID, g); err != nil {
		return fmt.Errorf("failed to initialize genesis state: %w", err)
	}
	if err := vdb.Commit(); err != nil {
		return err
	}

	vm.genesis = g

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

func (vm *VM) SetState(_ context.Context, state consensus.State) error {
	vm.chain.SetChainState(state)
	return nil
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.chainContext == nil {
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
		vm.chainContext,
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
	return vm.chain.GetBlock(blkID)
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (block.Block, error) {
	blk, err := xsblock.Parse(blkBytes)
	if err != nil {
		return nil, err
	}
	return vm.chain.NewBlock(blk)
}

func (vm *VM) BuildBlock(ctx context.Context) (block.Block, error) {
	return vm.builder.BuildBlock(ctx, nil)
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
	return vm.builder.BuildBlock(ctx, smContext)
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
