// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/chains/atomic/gsharedmemory"
	"github.com/luxfi/node/db/rpcdb"
	"github.com/luxfi/node/ids/galiasreader"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/components/chain"
	"github.com/luxfi/node/vms/platformvm/warp/gwarp"
	"github.com/luxfi/node/vms/rpcchainvm/appsender"
	"github.com/luxfi/node/vms/rpcchainvm/ghttp"
	"github.com/luxfi/node/vms/rpcchainvm/grpcutils"
	"github.com/luxfi/node/vms/rpcchainvm/gvalidators"
	"github.com/luxfi/node/vms/rpcchainvm/messenger"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	aliasreaderpb "github.com/luxfi/node/proto/pb/aliasreader"
	appsenderpb "github.com/luxfi/node/proto/pb/appsender"
	httppb "github.com/luxfi/node/proto/pb/http"
	messengerpb "github.com/luxfi/node/proto/pb/messenger"
	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
	sharedmemorypb "github.com/luxfi/node/proto/pb/sharedmemory"
	validatorstatepb "github.com/luxfi/node/proto/pb/validatorstate"
	vmpb "github.com/luxfi/node/proto/pb/vm"
	warppb "github.com/luxfi/node/proto/pb/warp"
)

var (
	_ vmpb.VMServer = (*VMServer)(nil)

	originalStderr = os.Stderr

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
)

// VMServer is a VM that is managed over RPC.
type VMServer struct {
	vmpb.UnsafeVMServer

	vm block.ChainVM
	// If nil, the underlying VM doesn't implement the interface.
	bVM block.BuildBlockWithContextChainVM
	// If nil, the underlying VM doesn't implement the interface.
	ssVM block.StateSyncableVM

	allowShutdown *utils.Atomic[bool]

	metrics prometheus.Gatherer
	db      database.Database
	log     log.Logger

	serverCloser grpcutils.ServerCloser
	connCloser   wrappers.Closer

	ctx    context.Context
	closed chan struct{}
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(vm block.ChainVM, allowShutdown *utils.Atomic[bool]) *VMServer {
	bVM, _ := vm.(block.BuildBlockWithContextChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &VMServer{
		vm:            vm,
		bVM:           bVM,
		ssVM:          ssVM,
		allowShutdown: allowShutdown,
	}
}

func (vm *VMServer) Initialize(ctx context.Context, req *vmpb.InitializeRequest) (*vmpb.InitializeResponse, error) {
	subnetID, err := ids.ToID(req.SubnetId)
	if err != nil {
		return nil, err
	}
	chainID, err := ids.ToID(req.ChainId)
	if err != nil {
		return nil, err
	}
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	publicKey, err := bls.PublicKeyFromCompressedBytes(req.PublicKey)
	if err != nil {
		return nil, err
	}
	_, err = ids.ToID(req.XChainId) // xChainID not used in context.Context
	if err != nil {
		return nil, err
	}
	cChainID, err := ids.ToID(req.CChainId)
	if err != nil {
		return nil, err
	}
	luxAssetID, err := ids.ToID(req.LuxAssetId)
	if err != nil {
		return nil, err
	}

	pluginMetrics := metric.NewPrefixGatherer()
	vm.metrics = pluginMetrics

	processMetrics, err := metric.MakeAndRegister(
		pluginMetrics,
		"process",
	)
	if err != nil {
		return nil, err
	}

	// Current state of process metrics
	processCollector := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
	if err := processMetrics.Register(processCollector); err != nil {
		return nil, err
	}

	// Go process metrics using debug.GCStats
	goCollector := collectors.NewGoCollector()
	if err := processMetrics.Register(goCollector); err != nil {
		return nil, err
	}

	grpcMetrics, err := metric.MakeAndRegister(
		pluginMetrics,
		"grpc",
	)
	if err != nil {
		return nil, err
	}

	// gRPC client metrics
	grpcClientMetrics := grpc_prometheus.NewClientMetrics()
	if err := grpcMetrics.Register(grpcClientMetrics); err != nil {
		return nil, err
	}

	vmMetrics := metric.NewPrefixGatherer()
	if err := pluginMetrics.Register("vm", vmMetrics); err != nil {
		return nil, err
	}

	// Dial the database
	dbClientConn, err := grpcutils.Dial(
		req.DbServerAddr,
		grpcutils.WithChainUnaryInterceptor(grpcClientMetrics.UnaryClientInterceptor()),
		grpcutils.WithChainStreamInterceptor(grpcClientMetrics.StreamClientInterceptor()),
	)
	if err != nil {
		return nil, err
	}
	vm.connCloser.Add(dbClientConn)
	vm.db = rpcdb.NewClient(rpcdbpb.NewDatabaseClient(dbClientConn))
	// TODO: Add corruptabledb wrapper once logger is available

	// TODO: Allow the logger to be configured by the client
	// TODO: Properly configure logger
	vm.log = nil // No logger needed

	clientConn, err := grpcutils.Dial(
		req.ServerAddr,
		grpcutils.WithChainUnaryInterceptor(grpcClientMetrics.UnaryClientInterceptor()),
		grpcutils.WithChainStreamInterceptor(grpcClientMetrics.StreamClientInterceptor()),
	)
	if err != nil {
		// Ignore closing errors to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	vm.connCloser.Add(clientConn)

	msgClient := messenger.NewClient(messengerpb.NewMessengerClient(clientConn))
	// keystoreClient := gkeystore.NewClient(keystorepb.NewKeystoreClient(clientConn)) // Keystore removed
	sharedMemoryClient := gsharedmemory.NewClient(sharedmemorypb.NewSharedMemoryClient(clientConn))
	bcLookupClient := galiasreader.NewClient(aliasreaderpb.NewAliasReaderClient(clientConn))
	appSenderClient := appsender.NewClient(appsenderpb.NewAppSenderClient(clientConn))
	validatorStateClient := gvalidators.NewClient(validatorstatepb.NewValidatorStateClient(clientConn))
	_ = gwarp.NewClient(warppb.NewSignerClient(clientConn)) // warpSignerClient not used

	toEngine := make(chan block.Message, 1)
	vm.closed = make(chan struct{})
	go func() {
		for {
			select {
			case msg, ok := <-toEngine:
				if !ok {
					return
				}
				// Convert block.Message to core.Message
				if coreMsg, ok := msg.(core.Message); ok {
					_ = msgClient.Notify(coreMsg)
				}
			case <-vm.closed:
				return
			}
		}
	}()

	// Create wrappers for SharedMemory to match interfaces.SharedMemory
	smWrapper := &serverSharedMemoryWrapper{sm: sharedMemoryClient}

	// Create wrapper for BCLookup
	bcWrapper := &serverBCLookupWrapper{client: bcLookupClient}

	// Create wrapper for ValidatorState
	vsWrapper := &serverValidatorStateWrapper{client: validatorStateClient}

	// Set IDs in context
	vm.ctx = consensus.WithIDs(ctx, consensus.IDs{
		NetworkID: req.NetworkId,
		SubnetID:  subnetID,
		ChainID:   chainID,
		NodeID:    nodeID,
		PublicKey: publicKey,
	})

	// The VM already has a log field
	vm.metrics = vmMetrics

	// Create a simple DBManager implementation
	dbMgr := &dbManagerImpl{db: vm.db}

	// Initialize the VM - convert back to block.ChainContext for the interface
	blockChainCtx := &block.ChainContext{
		NetworkID:      req.NetworkId,
		SubnetID:       subnetID,
		ChainID:        chainID,
		NodeID:         nodeID,
		PublicKey:      publicKey,
		CChainID:       cChainID,
		LUXAssetID:     luxAssetID,
		ChainDataDir:   req.ChainDataDir,
		Log:            vm.log,
		Metrics:        vmMetrics,
		SharedMemory:   smWrapper,
		BCLookup:       bcWrapper,
		ValidatorState: vsWrapper,
	}
	if err := vm.vm.Initialize(ctx, blockChainCtx, dbMgr, req.GenesisBytes, req.UpgradeBytes, req.ConfigBytes, toEngine, nil, appSenderClient); err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.connCloser.Close()
		close(vm.closed)
		return nil, err
	}

	lastAccepted, err := vm.vm.LastAccepted(ctx)
	if err != nil {
		// Ignore errors closing resources to return the original error
		// VM.Shutdown not available in ChainVM interface
		// _ = vm.vm.Shutdown(ctx)
		_ = vm.connCloser.Close()
		close(vm.closed)
		return nil, err
	}

	blk, err := vm.vm.GetBlock(ctx, lastAccepted)
	if err != nil {
		// Ignore errors closing resources to return the original error
		// VM.Shutdown not available in ChainVM interface
		// _ = vm.vm.Shutdown(ctx)
		_ = vm.connCloser.Close()
		close(vm.closed)
		return nil, err
	}
	parentID := blk.Parent()
	return &vmpb.InitializeResponse{
		LastAcceptedId:       lastAccepted[:],
		LastAcceptedParentId: parentID[:],
		Height:               blk.Height(),
		Bytes:                blk.Bytes(),
		Timestamp:            grpcutils.TimestampFromTime(blk.Timestamp()),
	}, nil
}

func (vm *VMServer) SetState(ctx context.Context, stateReq *vmpb.SetStateRequest) (*vmpb.SetStateResponse, error) {
	// SetState not available in ChainVM interface, check if VM implements it
	type stateSetter interface {
		SetState(context.Context, consensus.State) error
	}

	if ss, ok := vm.vm.(stateSetter); ok {
		err := ss.SetState(ctx, consensus.State(stateReq.State))
		if err != nil {
			return nil, err
		}
	}

	lastAccepted, err := vm.vm.LastAccepted(ctx)
	if err != nil {
		return nil, err
	}

	blk, err := vm.vm.GetBlock(ctx, lastAccepted)
	if err != nil {
		return nil, err
	}

	parentID := blk.Parent()
	return &vmpb.SetStateResponse{
		LastAcceptedId:       lastAccepted[:],
		LastAcceptedParentId: parentID[:],
		Height:               blk.Height(),
		Bytes:                blk.Bytes(),
		Timestamp:            grpcutils.TimestampFromTime(blk.Timestamp()),
	}, nil
}

func (vm *VMServer) Shutdown(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	vm.allowShutdown.Set(true)
	if vm.closed == nil {
		return &emptypb.Empty{}, nil
	}
	errs := wrappers.Errs{}
	// VM.Shutdown not available in ChainVM interface
	// errs.Add(vm.vm.Shutdown(ctx))
	close(vm.closed)
	vm.serverCloser.Stop()
	errs.Add(vm.connCloser.Close())
	return &emptypb.Empty{}, errs.Err
}

func (vm *VMServer) CreateHandlers(ctx context.Context, _ *emptypb.Empty) (*vmpb.CreateHandlersResponse, error) {
	// CreateHandlers not available in ChainVM interface, check if VM implements it
	type handlerCreator interface {
		CreateHandlers(context.Context) (map[string]http.Handler, error)
	}

	var handlers map[string]http.Handler
	if hc, ok := vm.vm.(handlerCreator); ok {
		var err error
		handlers, err = hc.CreateHandlers(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		// Return empty handlers if not implemented
		handlers = make(map[string]http.Handler)
	}
	resp := &vmpb.CreateHandlersResponse{}
	for prefix, handler := range handlers {
		serverListener, err := grpcutils.NewListener()
		if err != nil {
			return nil, err
		}
		server := grpcutils.NewServer()
		vm.serverCloser.Add(server)
		httppb.RegisterHTTPServer(server, ghttp.NewServer(handler))

		// Start HTTP service
		go grpcutils.Serve(serverListener, server)

		resp.Handlers = append(resp.Handlers, &vmpb.Handler{
			Prefix:     prefix,
			ServerAddr: serverListener.Addr().String(),
		})
	}
	return resp, nil
}

func (vm *VMServer) Connected(ctx context.Context, req *vmpb.ConnectedRequest) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	peerVersion := &version.Application{
		Name:  req.Name,
		Major: int(req.Major),
		Minor: int(req.Minor),
		Patch: int(req.Patch),
	}
	// Check if VM implements network.Handler interface
	type connectedHandler interface {
		Connected(context.Context, ids.NodeID, *version.Application) error
	}
	if handler, ok := vm.vm.(connectedHandler); ok {
		return &emptypb.Empty{}, handler.Connected(ctx, nodeID, peerVersion)
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) Disconnected(ctx context.Context, req *vmpb.DisconnectedRequest) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	// Check if VM implements network.Handler interface
	type disconnectedHandler interface {
		Disconnected(context.Context, ids.NodeID) error
	}
	if handler, ok := vm.vm.(disconnectedHandler); ok {
		return &emptypb.Empty{}, handler.Disconnected(ctx, nodeID)
	}
	return &emptypb.Empty{}, nil
}

// If the underlying VM doesn't actually implement this method, its [BuildBlock]
// method will be called instead.
func (vm *VMServer) BuildBlock(ctx context.Context, req *vmpb.BuildBlockRequest) (*vmpb.BuildBlockResponse, error) {
	var (
		blk chain.Block
		err error
	)
	if vm.bVM == nil || req.PChainHeight == nil {
		blk, err = vm.vm.BuildBlock(ctx)
	} else {
		blk, err = vm.bVM.BuildBlockWithContext(ctx, &block.Context{
			PChainHeight: *req.PChainHeight,
		})
	}
	if err != nil {
		return nil, err
	}

	blkWithCtx, verifyWithCtx := blk.(block.WithVerifyContext)
	if verifyWithCtx {
		verifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	var (
		blkID    = blk.ID()
		parentID = blk.Parent()
	)
	return &vmpb.BuildBlockResponse{
		Id:                blkID[:],
		ParentId:          parentID[:],
		Bytes:             blk.Bytes(),
		Height:            blk.Height(),
		Timestamp:         grpcutils.TimestampFromTime(blk.Timestamp()),
		VerifyWithContext: verifyWithCtx,
	}, nil
}

func (vm *VMServer) ParseBlock(ctx context.Context, req *vmpb.ParseBlockRequest) (*vmpb.ParseBlockResponse, error) {
	blk, err := vm.vm.ParseBlock(ctx, req.Bytes)
	if err != nil {
		return nil, err
	}

	blkWithCtx, verifyWithCtx := blk.(block.WithVerifyContext)
	if verifyWithCtx {
		verifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	var (
		blkID    = blk.ID()
		parentID = blk.Parent()
	)
	return &vmpb.ParseBlockResponse{
		Id:       blkID[:],
		ParentId: parentID[:],
		// Status:            vmpb.Status(blk.Status()), // Status method no longer exists on chain.Block
		Height:            blk.Height(),
		Timestamp:         grpcutils.TimestampFromTime(blk.Timestamp()),
		VerifyWithContext: verifyWithCtx,
	}, nil
}

func (vm *VMServer) GetBlock(ctx context.Context, req *vmpb.GetBlockRequest) (*vmpb.GetBlockResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(ctx, id)
	if err != nil {
		return &vmpb.GetBlockResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	blkWithCtx, verifyWithCtx := blk.(block.WithVerifyContext)
	if verifyWithCtx {
		verifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	parentID := blk.Parent()
	return &vmpb.GetBlockResponse{
		ParentId: parentID[:],
		Bytes:    blk.Bytes(),
		// Status:            vmpb.Status(blk.Status()), // Status method no longer exists on chain.Block
		Height:            blk.Height(),
		Timestamp:         grpcutils.TimestampFromTime(blk.Timestamp()),
		VerifyWithContext: verifyWithCtx,
	}, nil
}

func (vm *VMServer) SetPreference(ctx context.Context, req *vmpb.SetPreferenceRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.SetPreference(ctx, id)
}

func (vm *VMServer) Health(ctx context.Context, _ *emptypb.Empty) (*vmpb.HealthResponse, error) {
	// HealthCheck not available in ChainVM interface, check if VM implements it
	type healthChecker interface {
		HealthCheck(context.Context) (interface{}, error)
	}

	var vmHealth interface{}
	if hc, ok := vm.vm.(healthChecker); ok {
		var err error
		vmHealth, err = hc.HealthCheck(ctx)
		if err != nil {
			return &vmpb.HealthResponse{}, err
		}
	} else {
		vmHealth = map[string]interface{}{"status": "healthy"}
	}
	dbHealth, err := vm.db.HealthCheck(ctx)
	if err != nil {
		return &vmpb.HealthResponse{}, err
	}
	report := map[string]interface{}{
		"database": dbHealth,
		"health":   vmHealth,
	}

	details, err := json.Marshal(report)
	return &vmpb.HealthResponse{
		Details: details,
	}, err
}

func (vm *VMServer) Version(ctx context.Context, _ *emptypb.Empty) (*vmpb.VersionResponse, error) {
	// Version not available in ChainVM interface, check if VM implements it
	type versionGetter interface {
		Version(context.Context) (string, error)
	}

	var version string
	var err error
	if vg, ok := vm.vm.(versionGetter); ok {
		version, err = vg.Version(ctx)
	} else {
		version = "1.0.0" // Default version
	}

	return &vmpb.VersionResponse{
		Version: version,
	}, err
}

func (vm *VMServer) CrossChainAppRequest(ctx context.Context, msg *vmpb.CrossChainAppRequestMsg) (*emptypb.Empty, error) {
	_, err := ids.ToID(msg.ChainId)
	if err != nil {
		return nil, err
	}
	_, err = grpcutils.TimestampAsTime(msg.Deadline)
	if err != nil {
		return nil, err
	}
	// CrossChainAppRequest method no longer exists on ChainVM interface
	// return &emptypb.Empty{}, vm.vm.CrossChainAppRequest(ctx, chainID, msg.RequestId, deadline, msg.Request)
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) CrossChainAppRequestFailed(ctx context.Context, msg *vmpb.CrossChainAppRequestFailedMsg) (*emptypb.Empty, error) {
	_, err := ids.ToID(msg.ChainId)
	if err != nil {
		return nil, err
	}

	_ = &core.AppError{
		Code:    msg.ErrorCode,
		Message: msg.ErrorMessage,
	}
	// CrossChainAppRequestFailed method no longer exists on ChainVM interface
	// return &emptypb.Empty{}, vm.vm.CrossChainAppRequestFailed(ctx, chainID, msg.RequestId, appErr)
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) CrossChainAppResponse(ctx context.Context, msg *vmpb.CrossChainAppResponseMsg) (*emptypb.Empty, error) {
	_, err := ids.ToID(msg.ChainId)
	if err != nil {
		return nil, err
	}
	// CrossChainAppResponse method no longer exists on ChainVM interface
	// return &emptypb.Empty{}, vm.vm.CrossChainAppResponse(ctx, chainID, msg.RequestId, msg.Response)
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) AppRequest(ctx context.Context, req *vmpb.AppRequestMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	deadline, err := grpcutils.TimestampAsTime(req.Deadline)
	if err != nil {
		return nil, err
	}
	// Check if VM implements AppHandler interface
	type appHandler interface {
		AppRequest(context.Context, ids.NodeID, uint32, time.Time, []byte) error
	}
	if handler, ok := vm.vm.(appHandler); ok {
		return &emptypb.Empty{}, handler.AppRequest(ctx, nodeID, req.RequestId, deadline, req.Request)
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) AppRequestFailed(ctx context.Context, req *vmpb.AppRequestFailedMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	appErr := &core.AppError{
		Code:    req.ErrorCode,
		Message: req.ErrorMessage,
	}
	// Check if VM implements AppHandler interface
	type appFailHandler interface {
		AppRequestFailed(context.Context, ids.NodeID, uint32, *core.AppError) error
	}
	if handler, ok := vm.vm.(appFailHandler); ok {
		return &emptypb.Empty{}, handler.AppRequestFailed(ctx, nodeID, req.RequestId, appErr)
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) AppResponse(ctx context.Context, req *vmpb.AppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	// Check if VM implements AppHandler interface
	type appRespHandler interface {
		AppResponse(context.Context, ids.NodeID, uint32, []byte) error
	}
	if handler, ok := vm.vm.(appRespHandler); ok {
		return &emptypb.Empty{}, handler.AppResponse(ctx, nodeID, req.RequestId, req.Response)
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) AppGossip(ctx context.Context, req *vmpb.AppGossipMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	// Check if VM implements AppHandler interface
	type appGossipHandler interface {
		AppGossip(context.Context, ids.NodeID, []byte) error
	}
	if handler, ok := vm.vm.(appGossipHandler); ok {
		return &emptypb.Empty{}, handler.AppGossip(ctx, nodeID, req.Msg)
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) Gather(context.Context, *emptypb.Empty) (*vmpb.GatherResponse, error) {
	metrics, err := vm.metric.Gather()
	return &vmpb.GatherResponse{MetricFamilies: metrics}, err
}

func (vm *VMServer) GetAncestors(ctx context.Context, req *vmpb.GetAncestorsRequest) (*vmpb.GetAncestorsResponse, error) {
	blkID, err := ids.ToID(req.BlkId)
	if err != nil {
		return nil, err
	}
	maxBlksNum := int(req.MaxBlocksNum)
	maxBlksSize := int(req.MaxBlocksSize)
	_ = time.Duration(req.MaxBlocksRetrivalTime) // Not used in simple implementation

	// GetAncestors implementation - get blocks iteratively
	var blocks [][]byte
	currentID := blkID
	for i := 0; i < maxBlksNum; i++ {
		blk, err := vm.vm.GetBlock(ctx, currentID)
		if err != nil {
			break // Stop when we can't get more blocks
		}
		blkBytes := blk.Bytes()
		if len(blkBytes) > maxBlksSize {
			break // Stop if block is too large
		}
		blocks = append(blocks, blkBytes)
		currentID = blk.Parent()
		if currentID == ids.Empty {
			break // Reached genesis
		}
	}
	return &vmpb.GetAncestorsResponse{
		BlksBytes: blocks,
	}, nil
}

func (vm *VMServer) BatchedParseBlock(
	ctx context.Context,
	req *vmpb.BatchedParseBlockRequest,
) (*vmpb.BatchedParseBlockResponse, error) {
	blocks := make([]*vmpb.ParseBlockResponse, len(req.Request))
	for i, blockBytes := range req.Request {
		block, err := vm.ParseBlock(ctx, &vmpb.ParseBlockRequest{
			Bytes: blockBytes,
		})
		if err != nil {
			return nil, err
		}
		blocks[i] = block
	}
	return &vmpb.BatchedParseBlockResponse{
		Response: blocks,
	}, nil
}

func (vm *VMServer) GetBlockIDAtHeight(
	ctx context.Context,
	req *vmpb.GetBlockIDAtHeightRequest,
) (*vmpb.GetBlockIDAtHeightResponse, error) {
	blkID, err := vm.vm.GetBlockIDAtHeight(ctx, req.Height)
	return &vmpb.GetBlockIDAtHeightResponse{
		BlkId: blkID[:],
		Err:   errorToErrEnum[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncEnabled(ctx context.Context, _ *emptypb.Empty) (*vmpb.StateSyncEnabledResponse, error) {
	var (
		enabled bool
		err     error
	)
	if vm.ssVM != nil {
		enabled, err = vm.ssVM.StateSyncEnabled(ctx)
	}

	return &vmpb.StateSyncEnabledResponse{
		Enabled: enabled,
		Err:     errorToErrEnum[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetOngoingSyncStateSummary(
	ctx context.Context,
	_ *emptypb.Empty,
) (*vmpb.GetOngoingSyncStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetOngoingSyncStateSummary(ctx)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetOngoingSyncStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetOngoingSyncStateSummaryResponse{
		Id:     summaryID[:],
		Height: summary.Height(),
		Bytes:  summary.Bytes(),
	}, nil
}

func (vm *VMServer) GetLastStateSummary(ctx context.Context, _ *emptypb.Empty) (*vmpb.GetLastStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetLastStateSummary(ctx)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetLastStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetLastStateSummaryResponse{
		Id:     summaryID[:],
		Height: summary.Height(),
		Bytes:  summary.Bytes(),
	}, nil
}

func (vm *VMServer) ParseStateSummary(
	ctx context.Context,
	req *vmpb.ParseStateSummaryRequest,
) (*vmpb.ParseStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.ParseStateSummary(ctx, req.Bytes)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.ParseStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.ParseStateSummaryResponse{
		Id:     summaryID[:],
		Height: summary.Height(),
	}, nil
}

func (vm *VMServer) GetStateSummary(
	ctx context.Context,
	req *vmpb.GetStateSummaryRequest,
) (*vmpb.GetStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetStateSummary(ctx, req.Height)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetStateSummaryResponse{
		Id:    summaryID[:],
		Bytes: summary.Bytes(),
	}, nil
}

func (vm *VMServer) BlockVerify(ctx context.Context, req *vmpb.BlockVerifyRequest) (*vmpb.BlockVerifyResponse, error) {
	blk, err := vm.vm.ParseBlock(ctx, req.Bytes)
	if err != nil {
		return nil, err
	}

	if req.PChainHeight == nil {
		err = blk.Verify(ctx)
	} else {
		blkWithCtx, ok := blk.(block.WithVerifyContext)
		if !ok {
			return nil, fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, blk)
		}
		blockCtx := &block.Context{
			PChainHeight: *req.PChainHeight,
		}
		err = blkWithCtx.VerifyWithContext(ctx, blockCtx)
	}
	if err != nil {
		return nil, err
	}

	return &vmpb.BlockVerifyResponse{
		Timestamp: grpcutils.TimestampFromTime(blk.Timestamp()),
	}, nil
}

func (vm *VMServer) BlockAccept(ctx context.Context, req *vmpb.BlockAcceptRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := blk.Accept(ctx); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) BlockReject(ctx context.Context, req *vmpb.BlockRejectRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := blk.Reject(ctx); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) StateSummaryAccept(
	ctx context.Context,
	req *vmpb.StateSummaryAcceptRequest,
) (*vmpb.StateSummaryAcceptResponse, error) {
	var (
		mode = block.StateSyncSkipped
		err  error
	)
	if vm.ssVM != nil {
		var summary block.StateSummary
		summary, err = vm.ssVM.ParseStateSummary(ctx, req.Bytes)
		if err == nil {
			mode, err = summary.Accept(ctx)
		}
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	return &vmpb.StateSummaryAcceptResponse{
		Mode: vmpb.StateSummaryAcceptResponse_Mode(mode),
		Err:  errorToErrEnum[err],
	}, errorToRPCError(err)
}

// Server-specific wrapper types

type serverSharedMemoryWrapper struct {
	sm *gsharedmemory.Client
}

func (s *serverSharedMemoryWrapper) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	return s.sm.Get(peerChainID, keys)
}

func (s *serverSharedMemoryWrapper) Apply(requests map[ids.ID]interface{}, batches ...interface{}) error {
	// Convert interface{} back to proper types
	reqMap := make(map[ids.ID]*atomic.Requests, len(requests))
	for k, v := range requests {
		if r, ok := v.(*atomic.Requests); ok {
			reqMap[k] = r
		}
	}
	batchSlice := make([]database.Batch, 0, len(batches))
	for _, b := range batches {
		if batch, ok := b.(database.Batch); ok {
			batchSlice = append(batchSlice, batch)
		}
	}
	return s.sm.Apply(reqMap, batchSlice...)
}

type serverBCLookupWrapper struct {
	client *galiasreader.Client
}

func (b *serverBCLookupWrapper) PrimaryAlias(chainID ids.ID) (string, error) {
	return b.client.PrimaryAlias(chainID)
}

func (b *serverBCLookupWrapper) Lookup(alias string) (ids.ID, error) {
	return b.client.Lookup(alias)
}

type serverValidatorStateWrapper struct {
	client validators.State
}

func (v *serverValidatorStateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return v.client.GetCurrentHeight(ctx)
}

func (v *serverValidatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// Minimum height not available, return 0
	return 0, nil
}

func (v *serverValidatorStateWrapper) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// Not available, return empty ID
	return ids.Empty, nil
}

func (v *serverValidatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	return v.client.GetValidatorSet(ctx, height, subnetID)
}

// dbManagerImpl is a simple DBManager implementation
type dbManagerImpl struct {
	db database.Database
}

func (d *dbManagerImpl) Current() database.Database {
	return d.db
}

func (d *dbManagerImpl) Get(version uint64) (database.Database, error) {
	return d.db, nil
}

func (d *dbManagerImpl) Close() error {
	return nil
}
