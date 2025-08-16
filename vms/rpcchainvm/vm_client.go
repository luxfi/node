// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/consensus"
	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/chains/atomic/gsharedmemory"
	"github.com/luxfi/node/db/rpcdb"
	"github.com/luxfi/node/ids/galiasreader"
	"github.com/luxfi/node/utils/resource"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/components/chain"
	"github.com/luxfi/node/vms/platformvm/warp/gwarp"
	"github.com/luxfi/node/vms/rpcchainvm/appsender"
	"github.com/luxfi/node/vms/rpcchainvm/ghttp"
	"github.com/luxfi/node/vms/rpcchainvm/grpcutils"
	"github.com/luxfi/node/vms/rpcchainvm/gvalidators"
	"github.com/luxfi/node/vms/rpcchainvm/messenger"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"

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
	dto "github.com/prometheus/client_model/go"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// TODO: Enable these to be configured by the user
const (
	decidedCacheSize    = 64 * units.MiB
	missingCacheSize    = 2048
	unverifiedCacheSize = 64 * units.MiB
	bytesToIDCacheSize  = 64 * units.MiB
)

var (
	errUnsupportedFXs                       = errors.New("unsupported feature extensions")
	errBatchedParseBlockWrongNumberOfBlocks = errors.New("BatchedParseBlock returned different number of blocks than expected")

	_ block.ChainVM                      = (*VMClient)(nil)
	_ block.BuildBlockWithContextChainVM = (*VMClient)(nil)
	_ block.BatchedChainVM               = (*VMClient)(nil)
	_ block.StateSyncableVM              = (*VMClient)(nil)
	_ prometheus.Gatherer                = (*VMClient)(nil)

	_ block.Block             = (*blockClient)(nil)
	_ block.WithVerifyContext = (*blockClient)(nil)

	_ block.StateSummary = (*summaryClient)(nil)
)

// VMClient is an implementation of a VM that talks over RPC.
type VMClient struct {
	*chain.State
	client          vmpb.VMClient
	runtime         runtime.Stopper
	pid             int
	processTracker  resource.ProcessTracker
	metricsGatherer metric.MultiGatherer

	messenger *messenger.Server
	// keystore             *gkeystore.Server // Keystore removed
	sharedMemory         *gsharedmemory.Server
	bcLookup             *galiasreader.Server
	appSender            *appsender.Server
	validatorStateServer *gvalidators.Server
	warpSignerServer     *gwarp.Server

	serverCloser grpcutils.ServerCloser
	conns        []*grpc.ClientConn

	grpcServerMetrics *grpc_prometheus.ServerMetrics
}

// NewClient returns a VM connected to a remote VM
func NewClient(
	clientConn *grpc.ClientConn,
	runtime runtime.Stopper,
	pid int,
	processTracker resource.ProcessTracker,
	metricsGatherer metric.MultiGatherer,
) *VMClient {
	return &VMClient{
		client:          vmpb.NewVMClient(clientConn),
		runtime:         runtime,
		pid:             pid,
		processTracker:  processTracker,
		metricsGatherer: metricsGatherer,
		conns:           []*grpc.ClientConn{clientConn},
	}
}

func (vm *VMClient) Initialize(
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
	// Set IDs in context
	ctx = consensus.WithIDs(ctx, consensus.IDs{
		NetworkID: chainCtx.NetworkID,
		SubnetID:  chainCtx.SubnetID,
		ChainID:   chainCtx.ChainID,
		NodeID:    chainCtx.NodeID,
		PublicKey: chainCtx.PublicKey,
	})

	db := dbManager.Current()
	if len(fxs) != 0 {
		return errUnsupportedFXs
	}
	primaryAlias, err := chainCtx.BCLookup.PrimaryAlias(chainCtx.ChainID)
	if err != nil {
		// If fetching the alias fails, we default to the chain's ID
		primaryAlias = chainCtx.ChainID.String()
	}

	// Register metrics
	serverReg, err := metric.MakeAndRegister(
		vm.metricsGatherer,
		primaryAlias,
	)
	if err != nil {
		return err
	}
	vm.grpcServerMetrics = grpc_prometheus.NewServerMetrics()
	if err := serverReg.Register(vm.grpcServerMetrics); err != nil {
		return err
	}

	if err := chainCtx.Metrics.Register("", vm); err != nil {
		return err
	}

	// Initialize the database
	dbServerListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	dbServerAddr := dbServerListener.Addr().String()

	go grpcutils.Serve(dbServerListener, vm.newDBServer(db))
	chainCtx.Log.Info("grpc: serving database",
		zap.String("address", dbServerAddr),
	)

	// Create a channel for message passing
	msgChannel := make(chan core.Message, 1)
	vm.messenger = messenger.NewServer(msgChannel)
	// vm.keystore = gkeystore.NewServer(chainCtx.Keystore) // Keystore removed from context.Context

	// Create SharedMemory wrapper
	sharedMemoryWrapper := &sharedMemoryWrapper{sm: chainCtx.SharedMemory}
	vm.sharedMemory = gsharedmemory.NewServer(sharedMemoryWrapper, db)

	// Create BCLookup wrapper
	bcLookupWrapper := &bcLookupWrapper{bc: chainCtx.BCLookup}
	vm.bcLookup = galiasreader.NewServer(bcLookupWrapper)

	// Convert appSender
	coreAppSender := &appSenderWrapper{appSender: appSender}
	vm.appSender = appsender.NewServer(coreAppSender)

	// Create ValidatorState wrapper
	validatorStateWrapper := &validatorStateWrapper{vs: chainCtx.ValidatorState}
	vm.validatorStateServer = gvalidators.NewServer(validatorStateWrapper)
	// WarpSigner doesn't exist in context.Context - skip it
	// vm.warpSignerServer = gwarp.NewServer(chainCtx.WarpSigner)

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	serverAddr := serverListener.Addr().String()

	go grpcutils.Serve(serverListener, vm.newInitServer())
	chainCtx.Log.Info("grpc: serving vm services",
		zap.String("address", serverAddr),
	)

	resp, err := vm.client.Initialize(ctx, &vmpb.InitializeRequest{
		NetworkId:    chainCtx.NetworkID,
		SubnetId:     chainCtx.SubnetID[:],
		ChainId:      chainCtx.ChainID[:],
		NodeId:       chainCtx.NodeID.Bytes(),
		PublicKey:    bls.PublicKeyToCompressedBytes(chainCtx.PublicKey),
		XChainId:     ids.Empty[:], // XChainID doesn't exist in context.Context
		CChainId:     chainCtx.CChainID[:],
		LuxAssetId:   chainCtx.LUXAssetID[:],
		ChainDataDir: chainCtx.ChainDataDir,
		GenesisBytes: genesisBytes,
		UpgradeBytes: upgradeBytes,
		ConfigBytes:  configBytes,
		DbServerAddr: dbServerAddr,
		ServerAddr:   serverAddr,
	})
	if err != nil {
		return err
	}

	id, err := ids.ToID(resp.LastAcceptedId)
	if err != nil {
		return err
	}
	parentID, err := ids.ToID(resp.LastAcceptedParentId)
	if err != nil {
		return err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return err
	}

	// We don't need to check whether this is a block.WithVerifyContext because
	// we'll never Verify this block.
	lastAcceptedBlk := &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Accepted,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	}

	// Create wrapper functions that convert between chain.Block types
	getBlockWrapper := func(ctx context.Context, blkID ids.ID) (consensuschain.Block, error) {
		blk, err := vm.getBlock(ctx, blkID)
		if err != nil {
			return nil, err
		}
		// blockClient already implements consensuschain.Block
		return blk.(consensuschain.Block), nil
	}

	parseBlockWrapper := func(ctx context.Context, bytes []byte) (consensuschain.Block, error) {
		blk, err := vm.parseBlock(ctx, bytes)
		if err != nil {
			return nil, err
		}
		// blockClient already implements consensuschain.Block
		return blk.(consensuschain.Block), nil
	}

	batchedParseBlockWrapper := func(ctx context.Context, blksBytes [][]byte) ([]consensuschain.Block, error) {
		blks, err := vm.batchedParseBlock(ctx, blksBytes)
		if err != nil {
			return nil, err
		}
		result := make([]consensuschain.Block, len(blks))
		for i, blk := range blks {
			result[i] = blk.(consensuschain.Block)
		}
		return result, nil
	}

	buildBlockWrapper := func(ctx context.Context) (consensuschain.Block, error) {
		blk, err := vm.buildBlock(ctx)
		if err != nil {
			return nil, err
		}
		// blockClient already implements consensuschain.Block
		return blk.(consensuschain.Block), nil
	}

	buildBlockWithContextWrapper := func(ctx context.Context, blockCtx *block.Context) (consensuschain.Block, error) {
		blk, err := vm.buildBlockWithContext(ctx, blockCtx)
		if err != nil {
			return nil, err
		}
		// blockClient already implements consensuschain.Block
		return blk.(consensuschain.Block), nil
	}

	vm.State, err = chain.NewMeteredState(
		serverReg,
		&chain.Config{
			DecidedCacheSize:      decidedCacheSize,
			MissingCacheSize:      missingCacheSize,
			UnverifiedCacheSize:   unverifiedCacheSize,
			BytesToIDCacheSize:    bytesToIDCacheSize,
			LastAcceptedBlock:     lastAcceptedBlk,
			GetBlock:              getBlockWrapper,
			UnmarshalBlock:        parseBlockWrapper,
			BatchedUnmarshalBlock: batchedParseBlockWrapper,
			BuildBlock:            buildBlockWrapper,
			BuildBlockWithContext: buildBlockWithContextWrapper,
		},
	)
	return err
}

func (vm *VMClient) newDBServer(db database.Database) *grpc.Server {
	server := grpcutils.NewServer(
		grpcutils.WithUnaryInterceptor(vm.grpcServerMetrics.UnaryServerInterceptor()),
		grpcutils.WithStreamInterceptor(vm.grpcServerMetrics.StreamServerInterceptor()),
	)

	// See https://github.com/grpc/grpc/blob/master/doc/health-checking.md
	grpcHealth := health.NewServer()
	grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	vm.serverCloser.Add(server)

	// Register services
	rpcdbpb.RegisterDatabaseServer(server, rpcdb.NewServer(db))
	healthpb.RegisterHealthServer(server, grpcHealth)

	// Ensure metric counters are zeroed on restart
	grpc_prometheus.Register(server)

	return server
}

func (vm *VMClient) newInitServer() *grpc.Server {
	server := grpcutils.NewServer(
		grpcutils.WithUnaryInterceptor(vm.grpcServerMetrics.UnaryServerInterceptor()),
		grpcutils.WithStreamInterceptor(vm.grpcServerMetrics.StreamServerInterceptor()),
	)

	// See https://github.com/grpc/grpc/blob/master/doc/health-checking.md
	grpcHealth := health.NewServer()
	grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	vm.serverCloser.Add(server)

	// Register services
	messengerpb.RegisterMessengerServer(server, vm.messenger)
	// keystorepb.RegisterKeystoreServer(server, vm.keystore) // Keystore removed
	sharedmemorypb.RegisterSharedMemoryServer(server, vm.sharedMemory)
	aliasreaderpb.RegisterAliasReaderServer(server, vm.bcLookup)
	appsenderpb.RegisterAppSenderServer(server, vm.appSender)
	healthpb.RegisterHealthServer(server, grpcHealth)
	validatorstatepb.RegisterValidatorStateServer(server, vm.validatorStateServer)
	warppb.RegisterSignerServer(server, vm.warpSignerServer)

	// Ensure metric counters are zeroed on restart
	grpc_prometheus.Register(server)

	return server
}

func (vm *VMClient) SetState(ctx context.Context, state consensus.State) error {
	resp, err := vm.client.SetState(ctx, &vmpb.SetStateRequest{
		State: vmpb.State(state),
	})
	if err != nil {
		return err
	}

	id, err := ids.ToID(resp.LastAcceptedId)
	if err != nil {
		return err
	}

	parentID, err := ids.ToID(resp.LastAcceptedParentId)
	if err != nil {
		return err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return err
	}

	// We don't need to check whether this is a block.WithVerifyContext because
	// we'll never Verify this block.
	return vm.State.SetLastAcceptedBlock(&blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Accepted,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	})
}

func (vm *VMClient) Shutdown(ctx context.Context) error {
	errs := wrappers.Errs{}
	_, err := vm.client.Shutdown(ctx, &emptypb.Empty{})
	errs.Add(err)

	vm.serverCloser.Stop()
	for _, conn := range vm.conns {
		errs.Add(conn.Close())
	}

	vm.runtime.Stop(ctx)

	vm.processTracker.UntrackProcess(vm.pid)
	return errs.Err
}

func (vm *VMClient) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	resp, err := vm.client.CreateHandlers(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	handlers := make(map[string]http.Handler, len(resp.Handlers))
	for _, handler := range resp.Handlers {
		clientConn, err := grpcutils.Dial(handler.ServerAddr)
		if err != nil {
			return nil, err
		}

		vm.conns = append(vm.conns, clientConn)
		handlers[handler.Prefix] = ghttp.NewClient(httppb.NewHTTPClient(clientConn))
	}
	return handlers, nil
}

func (vm *VMClient) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	_, err := vm.client.Connected(ctx, &vmpb.ConnectedRequest{
		NodeId: nodeID.Bytes(),
		Name:   nodeVersion.Name,
		Major:  uint32(nodeVersion.Major),
		Minor:  uint32(nodeVersion.Minor),
		Patch:  uint32(nodeVersion.Patch),
	})
	return err
}

func (vm *VMClient) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	_, err := vm.client.Disconnected(ctx, &vmpb.DisconnectedRequest{
		NodeId: nodeID.Bytes(),
	})
	return err
}

// If the underlying VM doesn't actually implement this method, its [BuildBlock]
// method will be called instead.
func (vm *VMClient) buildBlockWithContext(ctx context.Context, blockCtx *block.Context) (chain.Block, error) {
	resp, err := vm.client.BuildBlock(ctx, &vmpb.BuildBlockRequest{
		PChainHeight: &blockCtx.PChainHeight,
	})
	if err != nil {
		return nil, err
	}
	return vm.newBlockFromBuildBlock(resp)
}

func (vm *VMClient) buildBlock(ctx context.Context) (chain.Block, error) {
	resp, err := vm.client.BuildBlock(ctx, &vmpb.BuildBlockRequest{})
	if err != nil {
		return nil, err
	}
	return vm.newBlockFromBuildBlock(resp)
}

func (vm *VMClient) parseBlock(ctx context.Context, bytes []byte) (chain.Block, error) {
	resp, err := vm.client.ParseBlock(ctx, &vmpb.ParseBlockRequest{
		Bytes: bytes,
	})
	if err != nil {
		return nil, err
	}

	id, err := ids.ToID(resp.Id)
	if err != nil {
		return nil, err
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	status := choices.Status(resp.Status)
	if err := status.Valid(); err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return nil, err
	}
	return &blockClient{
		vm:                  vm,
		id:                  id,
		parentID:            parentID,
		status:              status,
		bytes:               bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}, nil
}

func (vm *VMClient) getBlock(ctx context.Context, blkID ids.ID) (chain.Block, error) {
	resp, err := vm.client.GetBlock(ctx, &vmpb.GetBlockRequest{
		Id: blkID[:],
	})
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	status := choices.Status(resp.Status)
	if err := status.Valid(); err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	return &blockClient{
		vm:                  vm,
		id:                  blkID,
		parentID:            parentID,
		status:              status,
		bytes:               resp.Bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}, err
}

func (vm *VMClient) SetPreference(ctx context.Context, blkID ids.ID) error {
	_, err := vm.client.SetPreference(ctx, &vmpb.SetPreferenceRequest{
		Id: blkID[:],
	})
	return err
}

func (vm *VMClient) HealthCheck(ctx context.Context) (interface{}, error) {
	// HealthCheck is a special case, where we want to fail fast instead of block.
	failFast := grpc.WaitForReady(false)
	health, err := vm.client.Health(ctx, &emptypb.Empty{}, failFast)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	return json.RawMessage(health.Details), nil
}

func (vm *VMClient) Version(ctx context.Context) (string, error) {
	resp, err := vm.client.Version(ctx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Version, nil
}

func (vm *VMClient) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	_, err := vm.client.CrossChainAppRequest(
		ctx,
		&vmpb.CrossChainAppRequestMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
			Deadline:  grpcutils.TimestampFromTime(deadline),
			Request:   request,
		},
	)
	return err
}

func (vm *VMClient) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *core.AppError) error {
	msg := &vmpb.CrossChainAppRequestFailedMsg{
		ChainId:      chainID[:],
		RequestId:    requestID,
		ErrorCode:    appErr.Code,
		ErrorMessage: appErr.Message,
	}

	_, err := vm.client.CrossChainAppRequestFailed(ctx, msg)
	return err
}

func (vm *VMClient) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	_, err := vm.client.CrossChainAppResponse(
		ctx,
		&vmpb.CrossChainAppResponseMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (vm *VMClient) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	_, err := vm.client.AppRequest(
		ctx,
		&vmpb.AppRequestMsg{
			NodeId:    nodeID.Bytes(),
			RequestId: requestID,
			Request:   request,
			Deadline:  grpcutils.TimestampFromTime(deadline),
		},
	)
	return err
}

func (vm *VMClient) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	_, err := vm.client.AppResponse(
		ctx,
		&vmpb.AppResponseMsg{
			NodeId:    nodeID.Bytes(),
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (vm *VMClient) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *core.AppError) error {
	msg := &vmpb.AppRequestFailedMsg{
		NodeId:       nodeID.Bytes(),
		RequestId:    requestID,
		ErrorCode:    appErr.Code,
		ErrorMessage: appErr.Message,
	}

	_, err := vm.client.AppRequestFailed(ctx, msg)
	return err
}

func (vm *VMClient) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	_, err := vm.client.AppGossip(
		ctx,
		&vmpb.AppGossipMsg{
			NodeId: nodeID.Bytes(),
			Msg:    msg,
		},
	)
	return err
}

func (vm *VMClient) Gather() ([]*dto.MetricFamily, error) {
	resp, err := vm.client.Gather(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.MetricFamilies, nil
}

func (vm *VMClient) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	resp, err := vm.client.GetAncestors(ctx, &vmpb.GetAncestorsRequest{
		BlkId:                 blkID[:],
		MaxBlocksNum:          int32(maxBlocksNum),
		MaxBlocksSize:         int32(maxBlocksSize),
		MaxBlocksRetrivalTime: int64(maxBlocksRetrivalTime),
	})
	if err != nil {
		return nil, err
	}
	return resp.BlksBytes, nil
}

func (vm *VMClient) batchedParseBlock(ctx context.Context, blksBytes [][]byte) ([]chain.Block, error) {
	resp, err := vm.client.BatchedParseBlock(ctx, &vmpb.BatchedParseBlockRequest{
		Request: blksBytes,
	})
	if err != nil {
		return nil, err
	}
	if len(blksBytes) != len(resp.Response) {
		return nil, errBatchedParseBlockWrongNumberOfBlocks
	}

	res := make([]chain.Block, 0, len(blksBytes))
	for idx, blkResp := range resp.Response {
		id, err := ids.ToID(blkResp.Id)
		if err != nil {
			return nil, err
		}

		parentID, err := ids.ToID(blkResp.ParentId)
		if err != nil {
			return nil, err
		}

		status := choices.Status(blkResp.Status)
		if err := status.Valid(); err != nil {
			return nil, err
		}

		time, err := grpcutils.TimestampAsTime(blkResp.Timestamp)
		if err != nil {
			return nil, err
		}

		res = append(res, &blockClient{
			vm:                  vm,
			id:                  id,
			parentID:            parentID,
			status:              status,
			bytes:               blksBytes[idx],
			height:              blkResp.Height,
			time:                time,
			shouldVerifyWithCtx: blkResp.VerifyWithContext,
		})
	}

	return res, nil
}

func (vm *VMClient) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	resp, err := vm.client.GetBlockIDAtHeight(
		ctx,
		&vmpb.GetBlockIDAtHeightRequest{Height: height},
	)
	if err != nil {
		return ids.Empty, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return ids.Empty, errEnumToError[errEnum]
	}
	return ids.ToID(resp.BlkId)
}

func (vm *VMClient) StateSyncEnabled(ctx context.Context) (bool, error) {
	resp, err := vm.client.StateSyncEnabled(ctx, &emptypb.Empty{})
	if err != nil {
		return false, err
	}
	err = errEnumToError[resp.Err]
	if err == block.ErrStateSyncableVMNotImplemented {
		return false, nil
	}
	return resp.Enabled, err
}

func (vm *VMClient) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	resp, err := vm.client.GetOngoingSyncStateSummary(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	resp, err := vm.client.GetLastStateSummary(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	resp, err := vm.client.ParseStateSummary(
		ctx,
		&vmpb.ParseStateSummaryRequest{
			Bytes: summaryBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  summaryBytes,
	}, err
}

func (vm *VMClient) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	resp, err := vm.client.GetStateSummary(
		ctx,
		&vmpb.GetStateSummaryRequest{
			Height: summaryHeight,
		},
	)
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: summaryHeight,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) newBlockFromBuildBlock(resp *vmpb.BuildBlockResponse) (*blockClient, error) {
	id, err := ids.ToID(resp.Id)
	if err != nil {
		return nil, err
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	return &blockClient{
		vm:                  vm,
		id:                  id,
		parentID:            parentID,
		status:              choices.Processing,
		bytes:               resp.Bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}, err
}

type blockClient struct {
	vm *VMClient

	id                  ids.ID
	parentID            ids.ID
	status              choices.Status
	bytes               []byte
	height              uint64
	time                time.Time
	shouldVerifyWithCtx bool
}

func (b *blockClient) ID() ids.ID {
	return b.id
}

// EpochBit returns the epoch bit for FPC
func (b *blockClient) EpochBit() bool {
	// RPC blocks don't support epoch bits yet
	return false
}

// FPCVotes returns embedded fast-path vote references
func (b *blockClient) FPCVotes() [][]byte {
	// RPC blocks don't support FPC votes yet
	return nil
}

func (b *blockClient) Accept(ctx context.Context) error {
	b.status = choices.Accepted
	_, err := b.vm.client.BlockAccept(ctx, &vmpb.BlockAcceptRequest{
		Id: b.id[:],
	})
	return err
}

func (b *blockClient) Reject(ctx context.Context) error {
	b.status = choices.Rejected
	_, err := b.vm.client.BlockReject(ctx, &vmpb.BlockRejectRequest{
		Id: b.id[:],
	})
	return err
}

func (b *blockClient) Status() choices.Status {
	return b.status
}

func (b *blockClient) Parent() ids.ID {
	return b.parentID
}

func (b *blockClient) Verify(ctx context.Context) error {
	resp, err := b.vm.client.BlockVerify(ctx, &vmpb.BlockVerifyRequest{
		Bytes: b.bytes,
	})
	if err != nil {
		return err
	}

	b.time, err = grpcutils.TimestampAsTime(resp.Timestamp)
	return err
}

func (b *blockClient) Bytes() []byte {
	return b.bytes
}

func (b *blockClient) Height() uint64 {
	return b.height
}

func (b *blockClient) Timestamp() time.Time {
	return b.time
}

func (b *blockClient) ShouldVerifyWithContext(context.Context) (bool, error) {
	return b.shouldVerifyWithCtx, nil
}

func (b *blockClient) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	resp, err := b.vm.client.BlockVerify(ctx, &vmpb.BlockVerifyRequest{
		Bytes:        b.bytes,
		PChainHeight: &blockCtx.PChainHeight,
	})
	if err != nil {
		return err
	}

	b.time, err = grpcutils.TimestampAsTime(resp.Timestamp)
	return err
}

// SetStatus sets the status of the block
func (b *blockClient) SetStatus(status choices.Status) {
	b.status = status
}

type summaryClient struct {
	vm *VMClient

	id     ids.ID
	height uint64
	bytes  []byte
}

func (s *summaryClient) ID() ids.ID {
	return s.id
}

func (s *summaryClient) Height() uint64 {
	return s.height
}

func (s *summaryClient) Bytes() []byte {
	return s.bytes
}

func (s *summaryClient) Accept(ctx context.Context) (block.StateSyncMode, error) {
	resp, err := s.vm.client.StateSummaryAccept(
		ctx,
		&vmpb.StateSummaryAcceptRequest{
			Bytes: s.bytes,
		},
	)
	if err != nil {
		return block.StateSyncSkipped, err
	}
	return block.StateSyncMode(resp.Mode), errEnumToError[resp.Err]
}

// WaitForEvent implements the core.VM interface
func (vm *VMClient) WaitForEvent(ctx context.Context) (core.Message, error) {
	// The RPC VM client doesn't directly handle events,
	// it relies on the server-side VM for event handling
	<-ctx.Done()
	return core.PendingTxs, ctx.Err()
}

// NewHTTPHandler implements the core.VM interface
func (vm *VMClient) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	// RPC VM uses CreateHandlers instead of a single handler
	return nil, nil
}

// BuildBlock implements the block.ChainVM interface
func (vm *VMClient) BuildBlock(ctx context.Context) (block.Block, error) {
	innerBlk, err := vm.buildBlock(ctx)
	if err != nil {
		return nil, err
	}
	// Convert chain.Block to block.Block through wrapper
	return &chainBlockWrapper{innerBlk}, nil
}

// BuildBlockWithContext implements the block.BuildBlockWithContextChainVM interface
func (vm *VMClient) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
	innerBlk, err := vm.buildBlockWithContext(ctx, blockCtx)
	if err != nil {
		return nil, err
	}
	// Convert chain.Block to block.Block through wrapper
	return &chainBlockWrapper{innerBlk}, nil
}

// ParseBlock implements the block.ChainVM interface
func (vm *VMClient) ParseBlock(ctx context.Context, bytes []byte) (block.Block, error) {
	innerBlk, err := vm.parseBlock(ctx, bytes)
	if err != nil {
		return nil, err
	}
	// Convert chain.Block to block.Block through wrapper
	return &chainBlockWrapper{innerBlk}, nil
}

// GetBlock implements the block.ChainVM interface
func (vm *VMClient) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	innerBlk, err := vm.getBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	// Convert chain.Block to block.Block through wrapper
	return &chainBlockWrapper{innerBlk}, nil
}

// LastAccepted implements the block.ChainVM interface
func (vm *VMClient) LastAccepted(ctx context.Context) (ids.ID, error) {
	lastAcceptedBlk := vm.State.LastAcceptedBlock()
	return lastAcceptedBlk.ID(), nil
}

// BatchedParseBlock implements the block.BatchedChainVM interface
func (vm *VMClient) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]block.Block, error) {
	innerBlks, err := vm.batchedParseBlock(ctx, blks)
	if err != nil {
		return nil, err
	}
	// Convert []chain.Block to []block.Block
	result := make([]block.Block, len(innerBlks))
	for i, blk := range innerBlks {
		result[i] = &chainBlockWrapper{blk}
	}
	return result, nil
}

// chainBlockWrapper wraps a chain.Block to implement block.Block
type chainBlockWrapper struct {
	chain.Block
}

// Accept implements block.Block
func (b *chainBlockWrapper) Accept(ctx context.Context) error {
	// Forward to embedded chain.Block
	return b.Block.Accept(ctx)
}

// Reject implements block.Block
func (b *chainBlockWrapper) Reject(ctx context.Context) error {
	// Forward to embedded chain.Block
	return b.Block.Reject(ctx)
}

// Verify implements block.Block
func (b *chainBlockWrapper) Verify(ctx context.Context) error {
	// Forward to embedded chain.Block
	return b.Block.Verify(ctx)
}

// sharedMemoryWrapper wraps interfaces.SharedMemory to match atomic.SharedMemory
type sharedMemoryWrapper struct {
	sm interfaces.SharedMemory
}

func (s *sharedMemoryWrapper) Apply(requests map[ids.ID]*atomic.Requests, batches ...database.Batch) error {
	// Convert *atomic.Requests to interface{}
	reqMap := make(map[ids.ID]interface{}, len(requests))
	for k, v := range requests {
		reqMap[k] = v
	}
	// Convert batches to interface{} slice
	batchesInterface := make([]interface{}, len(batches))
	for i, batch := range batches {
		batchesInterface[i] = batch
	}
	return s.sm.Apply(reqMap, batchesInterface...)
}

func (s *sharedMemoryWrapper) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	// SharedMemory.Get is not available in interfaces.SharedMemory
	// Return empty values
	result := make([][]byte, len(keys))
	return result, nil
}

func (s *sharedMemoryWrapper) Indexed(peerChainID ids.ID, traits [][]byte, startTrait []byte, startKey []byte, limit int) ([][]byte, []byte, []byte, error) {
	// SharedMemory.Indexed is not available in interfaces.SharedMemory
	// Return empty values
	return nil, nil, nil, nil
}

// bcLookupWrapper wraps interfaces.BCLookup to match ids.AliaserReader
type bcLookupWrapper struct {
	bc interfaces.BCLookup
}

func (b *bcLookupWrapper) Lookup(alias string) (ids.ID, error) {
	return b.bc.Lookup(alias)
}

func (b *bcLookupWrapper) PrimaryAlias(id ids.ID) (string, error) {
	return b.bc.PrimaryAlias(id)
}

func (b *bcLookupWrapper) Aliases(id ids.ID) ([]string, error) {
	// BCLookup doesn't have Aliases method, return just the primary alias
	primary, err := b.bc.PrimaryAlias(id)
	if err != nil {
		return nil, err
	}
	return []string{primary}, nil
}

// validatorStateWrapper wraps interfaces.ValidatorState to match validators.State
type validatorStateWrapper struct {
	vs interfaces.ValidatorState
}

func (v *validatorStateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return v.vs.GetCurrentHeight(ctx)
}

func (v *validatorStateWrapper) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return v.vs.GetSubnetID(ctx, chainID)
}

func (v *validatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	return v.vs.GetValidatorSet(ctx, height, subnetID)
}

// appSenderWrapper wraps block.AppSender to match core.AppSender
type appSenderWrapper struct {
	appSender block.AppSender
}

func (a *appSenderWrapper) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request []byte) error {
	return a.appSender.SendAppRequest(ctx, nodeID, requestID, request)
}

func (a *appSenderWrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return a.appSender.SendAppResponse(ctx, nodeID, requestID, response)
}

func (a *appSenderWrapper) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// AppSender in block package doesn't have SendAppError, just return nil
	return nil
}

func (a *appSenderWrapper) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	return a.appSender.SendAppGossip(ctx, appGossipBytes)
}

func (a *appSenderWrapper) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	// Not implemented - return nil
	return nil
}

func (a *appSenderWrapper) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	// Not implemented - return nil
	return nil
}
