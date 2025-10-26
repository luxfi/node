// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	metric "github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/timer"
	"github.com/luxfi/node/version"
)

// ChainHandler handles messages for a specific chain
type ChainHandler interface {
	HandleInbound(ctx context.Context, msg message.InboundMessage)
	HealthCheck(ctx context.Context) (interface{}, error)
}

// Router routes messages between chains and manages network connections
type Router interface {
	// Initialize the router
	Initialize(
		nodeID ids.NodeID,
		logger log.Logger,
		timeoutManager timer.AdaptiveTimeoutManager,
		closeTimeout time.Duration,
		criticalChains set.Set[ids.ID],
		sybilProtectionEnabled bool,
		onFatal func(int),
		healthConfig HealthConfig,
		reg metric.Registerer,
		namespace string,
	) error

	// RegisterChain registers a handler for a specific chain
	RegisterChain(chainID ids.ID, handler ChainHandler) error

	// UnregisterChain removes a handler for a specific chain
	UnregisterChain(chainID ids.ID) error

	// HandleInbound routes an inbound message to the appropriate chain
	HandleInbound(ctx context.Context, msg message.InboundMessage)

	// Connected is called when a peer connects
	Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID)

	// Disconnected is called when a peer disconnects
	Disconnected(nodeID ids.NodeID)

	// RegisterRequest registers an outbound request
	RegisterRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
		op message.Op,
		timeoutMsg message.InboundMessage,
		engineType p2p.EngineType,
	)

	// RegisterRequests registers outbound requests to multiple nodes
	RegisterRequests(
		ctx context.Context,
		nodeIDs set.Set[ids.NodeID],
		chainID ids.ID,
		requestID uint32,
		op message.Op,
		timeoutMsg message.InboundMessage,
		engineType p2p.EngineType,
	)

	// HealthCheck returns the router's health status
	HealthCheck(ctx context.Context) (interface{}, error)

	// Shutdown gracefully shuts down the router
	Shutdown(ctx context.Context)
}

// HealthConfig contains health check configuration
type HealthConfig struct {
	MaxTimeSinceMsgReceived time.Duration
	MaxTimeSinceMsgSent     time.Duration
	MaxPortionSendQueueFull float64
	MaxSendFailRate         float64
	SendFailRateHalflife    time.Duration
}

// routerImpl implements the Router interface
type routerImpl struct {
	mu             sync.RWMutex
	nodeID         ids.NodeID
	log            log.Logger
	chains         map[ids.ID]ChainHandler
	connectedPeers set.Set[ids.NodeID]
	requests       map[uint32]*requestInfo
	timeoutManager timer.AdaptiveTimeoutManager
	closeTimeout   time.Duration
	criticalChains set.Set[ids.ID]
	onFatal        func(int)
	healthConfig   HealthConfig
	metrics        *routerMetrics
}

type requestInfo struct {
	chainID    ids.ID
	op         message.Op
	timeoutMsg message.InboundMessage
	engineType p2p.EngineType
}

type routerMetrics struct {
	droppedMsgs metric.Counter
	routedMsgs  metric.Counter
	// Add more metrics as needed
}

// NewRouter creates a new router
func NewRouter() Router {
	return &routerImpl{
		chains:         make(map[ids.ID]ChainHandler),
		connectedPeers: set.NewSet[ids.NodeID](10),
		requests:       make(map[uint32]*requestInfo),
	}
}

func (r *routerImpl) Initialize(
	nodeID ids.NodeID,
	logger log.Logger,
	timeoutManager timer.AdaptiveTimeoutManager,
	closeTimeout time.Duration,
	criticalChains set.Set[ids.ID],
	sybilProtectionEnabled bool,
	onFatal func(int),
	healthConfig HealthConfig,
	reg metric.Registerer,
	namespace string,
) error {
	r.nodeID = nodeID
	r.log = logger
	r.timeoutManager = timeoutManager
	r.closeTimeout = closeTimeout
	r.criticalChains = criticalChains
	r.onFatal = onFatal
	r.healthConfig = healthConfig

	// Initialize metrics
	r.metrics = &routerMetrics{
		droppedMsgs: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "dropped_msgs",
			Help:      "Number of dropped messages",
		}),
		routedMsgs: metric.NewCounter(metric.CounterOpts{
			Namespace: namespace,
			Name:      "routed_msgs",
			Help:      "Number of successfully routed messages",
		}),
	}

	// Metrics are self-registering via metric.NewCounter

	r.log.Info("router initialized",
		log.Stringer("nodeID", nodeID),
		log.Int("criticalChains", criticalChains.Len()),
	)

	return nil
}

func (r *routerImpl) RegisterChain(chainID ids.ID, handler ChainHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.chains[chainID]; exists {
		return ErrChainAlreadyRegistered
	}

	r.chains[chainID] = handler
	r.log.Info("registered chain",
		log.Stringer("chainID", chainID),
	)
	return nil
}

func (r *routerImpl) UnregisterChain(chainID ids.ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.chains[chainID]; !exists {
		return ErrChainNotRegistered
	}

	delete(r.chains, chainID)
	r.log.Info("unregistered chain",
		log.Stringer("chainID", chainID),
	)
	return nil
}

func (r *routerImpl) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Extract chain ID from message
	chainID, err := getChainID(msg)
	if err != nil {
		r.log.Debug("dropping message without chain ID",
			log.Reflect("error", err),
		)
		r.metrics.droppedMsgs.Inc()
		return
	}

	// Find handler for chain
	handler, exists := r.chains[chainID]
	if !exists {
		r.log.Debug("dropping message for unregistered chain",
			log.Stringer("chainID", chainID),
		)
		r.metrics.droppedMsgs.Inc()
		return
	}

	// Route to handler
	handler.HandleInbound(ctx, msg)
	r.metrics.routedMsgs.Inc()
}

func (r *routerImpl) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.connectedPeers.Add(nodeID)
	r.log.Debug("peer connected",
		log.Stringer("nodeID", nodeID),
		log.Stringer("netID", netID),
	)

	// Notify all chains of the connection
	for chainID, handler := range r.chains {
		// If handler implements a Connected method, call it
		type connectable interface {
			Connected(ids.NodeID, *version.Application, ids.ID)
		}
		if ch, ok := handler.(connectable); ok {
			ch.Connected(nodeID, nodeVersion, netID)
		}
		// Use chainID to avoid unused variable error
		_ = chainID
	}
}

func (r *routerImpl) Disconnected(nodeID ids.NodeID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.connectedPeers.Remove(nodeID)
	r.log.Debug("peer disconnected",
		log.Stringer("nodeID", nodeID),
	)

	// Notify all chains of the disconnection
	for _, handler := range r.chains {
		// If handler implements a Disconnected method, call it
		if ch, ok := handler.(interface {
			Disconnected(ids.NodeID)
		}); ok {
			ch.Disconnected(nodeID)
		}
	}
}

func (r *routerImpl) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests[requestID] = &requestInfo{
		chainID:    chainID,
		op:         op,
		timeoutMsg: timeoutMsg,
		engineType: engineType,
	}
}

func (r *routerImpl) RegisterRequests(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	// Register for each node
	for nodeID := range nodeIDs {
		r.RegisterRequest(ctx, nodeID, chainID, requestID, op, timeoutMsg, engineType)
	}
}

func (r *routerImpl) HealthCheck(ctx context.Context) (interface{}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := map[string]interface{}{
		"connectedPeers":   r.connectedPeers.Len(),
		"registeredChains": len(r.chains),
		"pendingRequests":  len(r.requests),
	}

	// Check each chain's health
	chainHealth := make(map[string]interface{})
	for chainID, handler := range r.chains {
		if result, err := handler.HealthCheck(ctx); err == nil {
			chainHealth[chainID.String()] = result
		}
	}
	health["chains"] = chainHealth

	return health, nil
}

func (r *routerImpl) Shutdown(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Info("shutting down router")

	// Clear all state
	r.chains = make(map[ids.ID]ChainHandler)
	r.connectedPeers.Clear()
	r.requests = make(map[uint32]*requestInfo)
}

// getChainID extracts the chain ID from a message
func getChainID(msg message.InboundMessage) (ids.ID, error) {
	// Try to get chain ID from message fields
	// This is a simplified version - you may need to adjust based on your message structure
	msgFields := msg.Message()
	if msgFields == nil {
		return ids.Empty, ErrNoChainID
	}

	// Different message types may have chain ID in different fields
	// You'll need to implement the actual extraction logic based on your message format
	// For now, returning empty ID
	return ids.Empty, ErrNoChainID
}
