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
	coreinterfaces "github.com/luxfi/consensus/core/interfaces"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/consensus/utils/set"
	consensuschain "github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	metric "github.com/luxfi/metric"
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
	chainCtx interface{},
	dbManager interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine interface{},
	fxs []interface{},
	appSender interface{},
) error {
	// Type assert to get concrete types
	var snowCtx *consensus.Context
	if cc, ok := chainCtx.(*block.ChainContext); ok && cc != nil {
		snowCtx = cc.Context
		if snowCtx != nil {
			ctx = consensus.WithIDs(ctx, consensus.IDs{
				NetworkID: snowCtx.QuantumID,
				ChainID:   snowCtx.ChainID,
				NodeID:    snowCtx.NodeID,
				PublicKey: snowCtx.PublicKey,
			})
		}
	}

	// Get the current database from the manager
	var db database.Database
	if currentDB, ok := dbManager.(interface{ Current() database.Database }); ok {
		db = currentDB.Current()
	}
	if len(fxs) != 0 {
		return errUnsupportedFXs
	}
	
	// Get chain ID for primary alias
	var primaryAlias string
	if snowCtx != nil {
		primaryAlias = snowCtx.ChainID.String()
		// Try to get the primary alias if BCLookup is available
		if snowCtx.BCLookup != nil {
			if bcLookup, ok := snowCtx.BCLookup.(interface{ PrimaryAlias(ids.ID) (string, error) }); ok {
				if alias, err := bcLookup.PrimaryAlias(snowCtx.ChainID); err == nil {
					primaryAlias = alias
				}
			}
		}
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

	// Skip metrics registration if Metrics is not available in snow context
	if snowCtx != nil && snowCtx.Metrics != nil {
		if metrics, ok := snowCtx.Metrics.(interface{ Register(string, interface{}) error }); ok {
			if err := metrics.Register("", vm); err != nil {
				return err
			}
		}
	}

	// Initialize the database
	dbServerListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	dbServerAddr := dbServerListener.Addr().String()

	// Create a database wrapper that provides the Database interface
	// dbMgr is block.DBManager which has methods, not a database itself
	// We need to create a wrapper or use it as-is
	var dbWrapper database.Database = db
	go grpcutils.Serve(dbServerListener, vm.newDBServer(dbWrapper))
	// Create a logger for RPC VM
	logger := log.New("rpcchainvm")
	if snowCtx != nil {
		logger.Info("grpc: serving database",
			zap.String("address", dbServerAddr),
		)
	}

	// Create a channel for message passing
	msgChannel := make(chan core.MessageType, 1)
	vm.messenger = messenger.NewServer(msgChannel)
	// vm.keystore = gkeystore.NewServer(chainContext.Keystore) // Keystore removed from context.Context

	// Create SharedMemory wrapper if available
	// SharedMemory is not part of the snow.Context, skip it
	// vm.sharedMemory = gsharedmemory.NewServer(nil, dbMgr)

	// Create BCLookup wrapper - handle interface{} type
	var bcLookup *bcLookupWrapper
	if snowCtx != nil && snowCtx.BCLookup != nil {
		if bc, ok := snowCtx.BCLookup.(BCLookup); ok {
			bcLookup = &bcLookupWrapper{bc: bc}
		} else {
			// Create a wrapper that converts the interface
			bcLookup = &bcLookupWrapper{bc: &bcLookupAdapter{lookup: snowCtx.BCLookup}}
		}
	} else {
		// Create a no-op BCLookup
		bcLookup = &bcLookupWrapper{bc: &noopBCLookup{}}
	}
	vm.bcLookup = galiasreader.NewServer(bcLookup)

	// Convert appSender
	var coreAppSender block.AppSender
	if as, ok := appSender.(block.AppSender); ok {
		coreAppSender = as
	}
	if coreAppSender != nil {
		vm.appSender = appsender.NewServer(&appSenderWrapper{appSender: coreAppSender})
	}

	// Create ValidatorState wrapper - not available in current context
	// Skip for now as ValidatorState is not part of ChainContext
	vm.validatorStateServer = gvalidators.NewServer(nil)
	// WarpSigner doesn't exist in context.Context - skip it
	// vm.warpSignerServer = gwarp.NewServer(chainContext.WarpSigner)

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	serverAddr := serverListener.Addr().String()

	go grpcutils.Serve(serverListener, vm.newInitServer())
	if snowCtx != nil {
		logger.Info("grpc: serving vm services",
			zap.String("address", serverAddr),
		)
	}

	resp, err := vm.client.Initialize(ctx, &vmpb.InitializeRequest{
		NetworkId:    uint32(snowCtx.QuantumID),
		SubnetId:     snowCtx.NetID[:],
		ChainId:      snowCtx.ChainID[:],
		NodeId:       snowCtx.NodeID.Bytes(),
		PublicKey:    snowCtx.PublicKey,
		XChainId:     snowCtx.XChainID[:],
		CChainId:     snowCtx.CChainID[:],
		LuxAssetId:   snowCtx.AVAXAssetID[:],
		ChainDataDir: "",
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
			LastAcceptedBlock:     &protocolBlockWrapper{blockClient: lastAcceptedBlk},
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

func (vm *VMClient) SetState(ctx context.Context, state coreinterfaces.State) error {
	// For now, assume state is a simple interface that can be type asserted
	// to a numeric value. This is a temporary fix.
	var stateValue uint32
	
	// Try to get a numeric representation
	// State is an interface, so we'll use a default mapping
	stateValue = 0 // Default to Bootstrapping
	
	resp, err := vm.client.SetState(ctx, &vmpb.SetStateRequest{
		State: vmpb.State(stateValue),
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
	return vm.State.SetLastAcceptedBlock(&protocolBlockWrapper{blockClient: &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Accepted,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	}})
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
	blk, err := vm.newBlockFromBuildBlock(resp)
	if err != nil {
		return nil, err
	}
	return &componentsBlockWrapper{blockClient: blk}, nil
}

func (vm *VMClient) buildBlock(ctx context.Context) (chain.Block, error) {
	resp, err := vm.client.BuildBlock(ctx, &vmpb.BuildBlockRequest{})
	if err != nil {
		return nil, err
	}
	blk, err := vm.newBlockFromBuildBlock(resp)
	if err != nil {
		return nil, err
	}
	return &componentsBlockWrapper{blockClient: blk}, nil
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

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return nil, err
	}
	return &componentsBlockWrapper{blockClient: &blockClient{
		vm:                  vm,
		id:                  id,
		parentID:            parentID,
		status:              status,
		bytes:               bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}}, nil
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

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return nil, err
	}
	return &componentsBlockWrapper{blockClient: &blockClient{
		vm:                  vm,
		id:                  blkID,
		parentID:            parentID,
		status:              status,
		bytes:               resp.Bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}}, nil
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

		time, err := grpcutils.TimestampAsTime(blkResp.Timestamp)
		if err != nil {
			return nil, err
		}

		res = append(res, &componentsBlockWrapper{blockClient: &blockClient{
			vm:                  vm,
			id:                  id,
			parentID:            parentID,
			status:              status,
			bytes:               blksBytes[idx],
			height:              blkResp.Height,
			time:                time,
			shouldVerifyWithCtx: blkResp.VerifyWithContext,
		}})
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

// GetChainID implements block.ChainVM.
func (vm *VMClient) GetChainID(ctx context.Context) (ids.ID, error) {
	// For now return empty ID - will be implemented later
	return ids.Empty, nil
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

func (b *blockClient) Status() uint8 {
	return uint8(b.status)
}

func (b *blockClient) Parent() ids.ID {
	return b.parentID
}

// ParentID implements block.Block
func (b *blockClient) ParentID() ids.ID {
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
func (vm *VMClient) WaitForEvent(ctx context.Context) (core.MessageType, error) {
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

// Status implements block.Block - returns uint8
func (b *chainBlockWrapper) Status() uint8 {
	// chain.Block already has Status() that returns uint8
	return b.Block.Status()
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

// protocolBlockWrapper wraps blockClient to implement protocol/chain.Block
type protocolBlockWrapper struct {
	*blockClient
}

// Status converts choices.Status to uint8 for protocol/chain.Block
func (b *protocolBlockWrapper) Status() uint8 {
	return uint8(b.blockClient.Status())
}

// componentsBlockWrapper wraps blockClient to implement components/chain.Block
type componentsBlockWrapper struct {
	*blockClient
}

// Status converts choices.Status to uint8 for components/chain.Block
func (b *componentsBlockWrapper) Status() uint8 {
	return uint8(b.blockClient.Status())
}

// Define missing interfaces locally
type SharedMemory interface {
	Apply(map[ids.ID]interface{}, ...interface{}) error
}

type BCLookup interface {
	Lookup(string) (ids.ID, error)
	PrimaryAlias(ids.ID) (string, error)
}

type ValidatorState interface {
	GetCurrentHeight() (uint64, error)
	GetNetID(context.Context, ids.ID) (ids.ID, error)
	GetValidatorSet(uint64, ids.ID) (map[ids.NodeID]uint64, error)
}

// sharedMemoryWrapper wraps SharedMemory to match atomic.SharedMemory
type sharedMemoryWrapper struct {
	sm SharedMemory
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

// noopDatabase is a database that does nothing
type noopDatabase struct{}

func (n *noopDatabase) Has([]byte) (bool, error) { return false, nil }
func (n *noopDatabase) Get([]byte) ([]byte, error) { return nil, database.ErrNotFound }
func (n *noopDatabase) Put([]byte, []byte) error { return nil }
func (n *noopDatabase) Delete([]byte) error { return nil }
func (n *noopDatabase) NewBatch() database.Batch { return &noopBatch{} }
func (n *noopDatabase) NewIterator() database.Iterator { return &emptyIterator{} }
func (n *noopDatabase) NewIteratorWithStart([]byte) database.Iterator { return &emptyIterator{} }
func (n *noopDatabase) NewIteratorWithPrefix([]byte) database.Iterator { return &emptyIterator{} }
func (n *noopDatabase) NewIteratorWithStartAndPrefix([]byte, []byte) database.Iterator { return &emptyIterator{} }
func (n *noopDatabase) Compact([]byte, []byte) error { return nil }
func (n *noopDatabase) Close() error { return nil }
func (n *noopDatabase) HealthCheck(context.Context) (interface{}, error) { return nil, nil }

type noopBatch struct{}
func (n *noopBatch) Put([]byte, []byte) error { return nil }
func (n *noopBatch) Delete([]byte) error { return nil }
func (n *noopBatch) Size() int { return 0 }
func (n *noopBatch) Write() error { return nil }
func (n *noopBatch) Reset() {}
func (n *noopBatch) Replay(database.KeyValueWriterDeleter) error { return nil }
func (n *noopBatch) Inner() database.Batch { return n }

// emptyIterator is a database iterator that returns nothing
type emptyIterator struct{}
func (e *emptyIterator) Next() bool { return false }
func (e *emptyIterator) Error() error { return nil }
func (e *emptyIterator) Key() []byte { return nil }
func (e *emptyIterator) Value() []byte { return nil }
func (e *emptyIterator) Release() {}

// bcLookupWrapper wraps BCLookup to match ids.AliaserReader
type bcLookupWrapper struct {
	bc BCLookup
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

// validatorStateWrapper wraps ValidatorState to match validators.State
type validatorStateWrapper struct {
	vs ValidatorState
}

func (v *validatorStateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return v.vs.GetCurrentHeight()
}

func (v *validatorStateWrapper) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return v.vs.GetNetID(ctx, chainID)
}

func (v *validatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Get the raw validator set
	valSet, err := v.vs.GetValidatorSet(height, netID)
	if err != nil {
		return nil, err
	}
	
	// Convert map[ids.NodeID]uint64 to map[ids.NodeID]*validators.GetValidatorOutput
	result := make(map[ids.NodeID]*validators.GetValidatorOutput, len(valSet))
	for nodeID, weight := range valSet {
		result[nodeID] = &validators.GetValidatorOutput{
			NodeID: nodeID,
			Weight: weight,
		}
	}
	return result, nil
}

// GetCurrentValidatorOutput represents a current validator
type GetCurrentValidatorOutput struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	Weight    uint64
}

func (v *validatorStateWrapper) GetCurrentValidatorSet(ctx context.Context, netID ids.ID) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error) {
	// Get current height first
	height, err := v.vs.GetCurrentHeight()
	if err != nil {
		return nil, 0, err
	}
	
	// Get validators at current height
	valSet, err := v.vs.GetValidatorSet(height, netID)
	if err != nil {
		return nil, 0, err
	}
	
	// Convert to GetCurrentValidatorOutput format
	result := make(map[ids.ID]*GetCurrentValidatorOutput, len(valSet))
	for nodeID, weight := range valSet {
		// Convert NodeID to ID by copying the bytes
		var id ids.ID
		copy(id[:], nodeID[:])
		result[id] = &GetCurrentValidatorOutput{
			NodeID: nodeID,
			Weight: weight,
		}
	}
	
	return result, height, nil
}

func (v *validatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// GetMinimumHeight is optional - return 0 if not available
	if vs, ok := v.vs.(interface{ GetMinimumHeight(context.Context) (uint64, error) }); ok {
		return vs.GetMinimumHeight(ctx)
	}
	return 0, nil
}

// GetCurrentValidators implements validators.State
func (v *validatorStateWrapper) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Get validators at specified height
	return v.GetValidatorSet(ctx, height, netID)
}

// appSenderWrapper wraps block.AppSender to match core.AppSender
type appSenderWrapper struct {
	appSender block.AppSender
}

func (a *appSenderWrapper) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	// block.AppSender expects a slice of nodeIDs
	nodeIDSlice := nodeIDs.List()
	if len(nodeIDSlice) > 0 {
		return a.appSender.SendAppRequest(ctx, nodeIDSlice, requestID, request)
	}
	return nil
}

func (a *appSenderWrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return a.appSender.SendAppResponse(ctx, nodeID, requestID, response)
}

func (a *appSenderWrapper) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// AppSender in block package doesn't have SendAppError, just return nil
	return nil
}

func (a *appSenderWrapper) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// block.AppSender expects a slice of nodeIDs  
	nodeIDSlice := nodeIDs.List()
	return a.appSender.SendAppGossip(ctx, nodeIDSlice, appGossipBytes)
}

func (a *appSenderWrapper) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Same as SendAppGossip for this wrapper
	nodeIDSlice := nodeIDs.List()
	return a.appSender.SendAppGossip(ctx, nodeIDSlice, appGossipBytes)
}

func (a *appSenderWrapper) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	// Not implemented - return nil
	return nil
}

func (a *appSenderWrapper) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	// Not implemented - return nil
	return nil
}

// bcLookupAdapter adapts interface{} to BCLookup
type bcLookupAdapter struct {
	lookup interface{}
}

func (b *bcLookupAdapter) Lookup(alias string) (ids.ID, error) {
	if l, ok := b.lookup.(interface{ Lookup(string) (ids.ID, error) }); ok {
		return l.Lookup(alias)
	}
	return ids.Empty, fmt.Errorf("BCLookup.Lookup not supported")
}

func (b *bcLookupAdapter) PrimaryAlias(id ids.ID) (string, error) {
	if l, ok := b.lookup.(interface{ PrimaryAlias(ids.ID) (string, error) }); ok {
		return l.PrimaryAlias(id)
	}
	return "", fmt.Errorf("BCLookup.PrimaryAlias not supported")
}

// noopBCLookup is a no-op implementation of BCLookup
type noopBCLookup struct{}

func (n *noopBCLookup) Lookup(string) (ids.ID, error) {
	return ids.Empty, fmt.Errorf("BCLookup not available")
}

func (n *noopBCLookup) PrimaryAlias(ids.ID) (string, error) {
	return "", fmt.Errorf("BCLookup not available")
}

// validatorStateAdapter adapts consensus.context.ValidatorState to our ValidatorState interface
type validatorStateAdapter struct {
	vs interface{}
}

func (v *validatorStateAdapter) GetCurrentHeight() (uint64, error) {
	if vs, ok := v.vs.(interface{ GetCurrentHeight() (uint64, error) }); ok {
		return vs.GetCurrentHeight()
	}
	return 0, fmt.Errorf("GetCurrentHeight not supported")
}

func (v *validatorStateAdapter) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// Try with context first
	if vs, ok := v.vs.(interface{ GetNetID(context.Context, ids.ID) (ids.ID, error) }); ok {
		return vs.GetNetID(ctx, chainID)
	}
	// Try without context
	if vs, ok := v.vs.(interface{ GetNetID(ids.ID) (ids.ID, error) }); ok {
		return vs.GetNetID(chainID)
	}
	return ids.Empty, fmt.Errorf("GetNetID not supported")
}

func (v *validatorStateAdapter) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	if vs, ok := v.vs.(interface{ GetValidatorSet(uint64, ids.ID) (map[ids.NodeID]uint64, error) }); ok {
		return vs.GetValidatorSet(height, netID)
	}
	return nil, fmt.Errorf("GetValidatorSet not supported")
}
