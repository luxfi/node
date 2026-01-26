// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"

	"github.com/luxfi/consensus/core/interfaces"
	enginechain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/vms/example/xsvm/api"
	"github.com/luxfi/node/vms/example/xsvm/builder"
	"github.com/luxfi/node/vms/example/xsvm/execute"
	"github.com/luxfi/node/vms/example/xsvm/genesis"
	"github.com/luxfi/node/vms/example/xsvm/state"
	"github.com/luxfi/p2p"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
	"github.com/luxfi/warp"

	xsblock "github.com/luxfi/node/vms/example/xsvm/block"
	xschain "github.com/luxfi/node/vms/example/xsvm/chain"
	smblock "github.com/luxfi/vm/chain"
)

// TODO: Update xsvm to match current consensus ChainVM interface
// The consensus interface has evolved to use interface{} parameters
// var (
// 	_ smblock.ChainVM                      = (*VM)(nil)
// 	_ smblock.BuildBlockWithRuntimeChainVM = (*VM)(nil)
// )

type VM struct {
	*p2p.Network

	rt      *runtime.Runtime
	db      database.Database
	genesis *genesis.Genesis

	chain   xschain.Chain
	builder builder.Builder
}

func (vm *VM) Initialize(
	_ context.Context,
	init vmcore.Init,
) error {
	rt := init.Runtime
	db := init.DB
	genesisBytes := init.Genesis
	appSender := init.Sender
	logger := init.Log
	if logger == nil {
		logger = rt.Log.(log.Logger)
	}
	logger.Info("initializing xsvm",
		log.Stringer("version", Version),
	)

	metrics := metric.NewRegistry()
	if metricsReg, ok := rt.Metrics.(interface {
		Register(name string, gatherer metric.Gatherer) error
	}); ok {
		if err := metricsReg.Register("p2p", metrics); err != nil {
			return err
		}
	}

	var err error
	vm.Network, err = p2p.NewNetwork(
		logger,
		appSender,
		metrics,
		"",
	)
	if err != nil {
		return err
	}

	// Allow signing of all warp messages. This is not typically safe, but is
	// allowed for this example.
	signatureCache := &cache.LRU[ids.ID, []byte]{Size: 100}
	// Cast WarpSigner directly to warp.Signer since both use external warp
	warpSigner := rt.WarpSigner.(warp.Signer)
	cachedHandler := warp.NewCachedSignatureHandler(
		signatureCache,
		xsvmVerifier{},
		warpSigner,
	)
	signatureHandler := warp.NewSignatureHandlerAdapter(cachedHandler)
	if err := vm.Network.AddHandler(warp.SignatureHandlerID, signatureHandler); err != nil {
		return err
	}

	vm.rt = rt
	vm.db = db
	g, err := genesis.Parse(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis bytes: %w", err)
	}

	vdb := versiondb.New(vm.db)
	chainID := rt.ChainID
	if err := execute.Genesis(vdb, chainID, g); err != nil {
		return fmt.Errorf("failed to initialize genesis state: %w", err)
	}
	if err := vdb.Commit(); err != nil {
		return err
	}

	vm.genesis = g

	vm.chain, err = xschain.New(rt, vm.db)
	if err != nil {
		return fmt.Errorf("failed to initialize chain manager: %w", err)
	}

	vm.builder = builder.New(rt, vm.chain)

	logger.Info("initialized xsvm",
		log.Stringer("lastAcceptedID", vm.chain.LastAccepted()),
	)
	return nil
}

func (vm *VM) SetState(ctx context.Context, newState interfaces.State) error {
	// SetState receives the consensus engine, which we pass to the chain
	// The state parameter is actually the consensus engine
	if engine, ok := ctx.Value("engine").(enginechain.Engine); ok {
		vm.chain.SetChainState(engine)
	}
	return nil
}

// Connected overrides p2p.Network.Connected to match consensus interface
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *smblock.VersionInfo) error {
	// Convert interface{} back to the specific type p2p.Network expects
	return vm.Network.Connected(ctx, nodeID, nil)
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.rt == nil {
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
	jsonRPCAPI := api.NewServer(
		vm.rt,
		vm.genesis,
		vm.db,
		vm.chain,
		vm.builder,
	)
	return map[string]http.Handler{
		"": server,
	}, server.RegisterService(jsonRPCAPI, constants.XSVMName)
}

// NewHTTPHandler is defined in vm_http_grpc.go (with grpc build tag)
// and vm_http_zap.go (default, without grpc reflection)

func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return http.StatusOK, nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (smblock.Block, error) {
	blk, err := vm.chain.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return &blockWrapper{Block: blk}, nil
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (xschain.Block, error) {
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

func (vm *VM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	return vm.builder.WaitForEvent(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (smblock.Block, error) {
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

func (vm *VM) BuildBlockWithRuntime(ctx context.Context, blockContext *runtime.Runtime) (smblock.Block, error) {
	blk, err := vm.builder.BuildBlock(ctx, blockContext)
	if err != nil {
		return nil, err
	}
	return &blockWrapper{Block: blk}, nil
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return state.GetBlockIDByHeight(vm.db, height)
}

// blockWrapper wraps an xsvm chain.Block to implement consensus block.Block
type blockWrapper struct {
	xschain.Block
}

// Status returns the uint8 status directly from the underlying block
func (b *blockWrapper) Status() uint8 {
	return b.Block.Status()
}
