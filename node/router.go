// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
	"github.com/prometheus/client_golang/prometheus"
)

// Router is a complete router interface that works with Lux packages
type Router interface {
	Initialize(
		nodeID ids.NodeID,
		log logging.Logger,
		timeouts handler.TimeoutManager,
		shutdownTimeout time.Duration,
		criticalChains set.Set[ids.ID],
		whitelistedSubnets set.Set[ids.ID],
		onFatal func(exitCode int),
		healthConfig HealthConfig,
		reg prometheus.Registerer,
		metricsNamespace string,
	) error
	RegisterRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
		op message.Op,
		failedMsg message.InboundMessage,
		engineType p2p.EngineType,
	)
	HandleInbound(context.Context, message.InboundMessage)
	Shutdown(context.Context)
	AddChain(context.Context, handler.Handler)
	Connected(ids.NodeID, *version.Application, ids.ID)
	Disconnected(ids.NodeID)
	Benched(chainID ids.ID, nodeID ids.NodeID)
	Unbenched(chainID ids.ID, nodeID ids.NodeID)
	HealthCheck(context.Context) (interface{}, error)
}

// chainRouter wraps the consensus router and adds missing functionality
type chainRouter struct {
	consensusRouter router.Router
	log             log.Logger
	lock            sync.RWMutex
	chains          map[ids.ID]handler.Handler
	timeoutManager  handler.TimeoutManager
	nodeID          ids.NodeID
	healthConfig    HealthConfig
	reg             prometheus.Registerer
	namespace       string
	criticalChains  set.Set[ids.ID]
	lastMsgTime     time.Time
	connectedPeers  set.Set[ids.NodeID]
}

// NewChainRouter creates a new router with full functionality
func NewChainRouter(logger log.Logger, timeoutManager handler.TimeoutManager) Router {
	// Create a wrapper for the consensus router that expects logging.Logger
	wrappedLogger := NewLoggerWrapper(logger)
	return &chainRouter{
		consensusRouter: router.NewRouter(wrappedLogger, timeoutManager),
		log:             logger,
		chains:          make(map[ids.ID]handler.Handler),
		timeoutManager:  timeoutManager,
		connectedPeers:  set.NewSet[ids.NodeID](10),
	}
}

func (r *chainRouter) Initialize(
	nodeID ids.NodeID,
	logger log.Logger,
	timeouts handler.TimeoutManager,
	shutdownTimeout time.Duration,
	criticalChains set.Set[ids.ID],
	whitelistedSubnets set.Set[ids.ID],
	onFatal func(exitCode int),
	healthConfig HealthConfig,
	reg prometheus.Registerer,
	metricsNamespace string,
) error {
	r.nodeID = nodeID
	r.log = logger
	r.timeoutManager = timeouts
	r.healthConfig = healthConfig
	r.reg = reg
	r.namespace = metricsNamespace
	r.criticalChains = criticalChains
	r.lastMsgTime = time.Now()
	
	// Create a wrapper for the consensus router that expects logging.Logger
	wrappedLogger := NewLoggerWrapper(logger)
	
	// Initialize the consensus router with the parameters it expects
	return r.consensusRouter.Initialize(
		nodeID,
		wrappedLogger,
		timeouts,
		shutdownTimeout,
		criticalChains,
		whitelistedSubnets,
		onFatal,
	)
}

func (r *chainRouter) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	// Convert p2p.EngineType to handler.EngineType
	var handlerEngineType handler.EngineType
	switch engineType {
	case p2p.EngineType_ENGINE_TYPE_DAG:
		handlerEngineType = handler.EngineTypeDAG
	case p2p.EngineType_ENGINE_TYPE_CHAIN:
		handlerEngineType = handler.EngineTypeChain
	default:
		handlerEngineType = handler.EngineTypeUnspecified
	}
	
	r.consensusRouter.RegisterRequest(ctx, nodeID, chainID, requestID, op, failedMsg, handlerEngineType)
}

func (r *chainRouter) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	r.lock.Lock()
	r.lastMsgTime = time.Now()
	r.lock.Unlock()
	
	r.consensusRouter.HandleInbound(ctx, msg)
}

func (r *chainRouter) Shutdown(ctx context.Context) {
	r.consensusRouter.Shutdown(ctx)
}

func (r *chainRouter) AddChain(ctx context.Context, handler handler.Handler) {
	r.lock.Lock()
	defer r.lock.Unlock()
	
	// Add chain to our local map
	chainID := handler.Context().ChainID
	r.chains[chainID] = handler
	
	// Also register with consensus router
	r.consensusRouter.AddChain(ctx, handler)
}

func (r *chainRouter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	r.lock.Lock()
	defer r.lock.Unlock()
	
	r.connectedPeers.Add(nodeID)
	r.log.Debug("peer connected",
		log.Stringer("nodeID", nodeID),
		log.Any("version", nodeVersion),
		log.Stringer("netID", netID),
	)
}

func (r *chainRouter) Disconnected(nodeID ids.NodeID) {
	r.lock.Lock()
	defer r.lock.Unlock()
	
	r.connectedPeers.Remove(nodeID)
	r.log.Debug("peer disconnected",
		log.Stringer("nodeID", nodeID),
	)
}

func (r *chainRouter) Benched(chainID ids.ID, nodeID ids.NodeID) {
	r.log.Debug("peer benched",
		log.Stringer("chainID", chainID),
		log.Stringer("nodeID", nodeID),
	)
}

func (r *chainRouter) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	r.log.Debug("peer unbenched",
		log.Stringer("chainID", chainID),
		log.Stringer("nodeID", nodeID),
	)
}

func (r *chainRouter) HealthCheck(ctx context.Context) (interface{}, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	
	healthy := true
	details := make(map[string]interface{})
	
	// Check connected peers
	connectedCount := r.connectedPeers.Len()
	details["connectedPeers"] = connectedCount
	if connectedCount < r.healthConfig.MinConnectedPeers {
		healthy = false
		details["error"] = "insufficient connected peers"
	}
	
	// Check time since last message
	timeSinceMsg := time.Since(r.lastMsgTime)
	details["timeSinceLastMessage"] = timeSinceMsg.String()
	if timeSinceMsg > r.healthConfig.MaxTimeSinceMsgReceived {
		healthy = false
		details["error"] = "no recent messages"
	}
	
	details["healthy"] = healthy
	return details, nil
}

// Helper types and functions

type Registerer = prometheus.Registerer

type stubBenchlistManager struct{}

func (s *stubBenchlistManager) Deprecated() {}
func (s *stubBenchlistManager) IsBenched(nodeID ids.NodeID, chainID ids.ID) bool { return false }
func (s *stubBenchlistManager) GetBenched(chainID ids.ID) []ids.NodeID { return nil }

func NewLockedCalculator(log logging.Logger, reg prometheus.Registerer, namespace string, calc tracker.Targeter) tracker.Targeter {
	return calc
}

type externalHandlerWrapper struct {
	handler handler.Handler
}

func Trace(log logging.Logger, name string, getTime func() time.Time) func() {
	return func() {}
}

// Removed LoggerWrapper - now in logging_adapter.go