// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	metric "github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/timer"
	"github.com/luxfi/node/version"
	"github.com/luxfi/trace"
)

// SimpleRouter implements Router interface
type SimpleRouter struct {
	log            log.Logger
	lock           sync.RWMutex
	chains         map[ids.ID]handler.Handler
	timeoutManager timer.AdaptiveTimeoutManager
	nodeID         ids.NodeID
	healthConfig   HealthConfig
	reg            metric.Registerer
	namespace      string
	criticalChains set.Set[ids.ID]
	lastMsgTime    time.Time
	connectedPeers set.Set[ids.NodeID]
	requests       map[uint32]*requestInfo
	requestID      uint32
	onFatal        func(int)
}

type requestInfo struct {
	nodeID    ids.NodeID
	chainID   ids.ID
	op        message.Op
	startTime time.Time
}

// NewSimpleRouter creates a new router
func NewSimpleRouter(logger log.Logger, timeoutManager timer.AdaptiveTimeoutManager) Router {
	return &SimpleRouter{
		log:            logger,
		chains:         make(map[ids.ID]handler.Handler),
		timeoutManager: timeoutManager,
		connectedPeers: set.NewSet[ids.NodeID](10),
		requests:       make(map[uint32]*requestInfo),
	}
}

func (r *SimpleRouter) Initialize(
	nodeID ids.NodeID,
	logger log.Logger,
	timeoutManager timer.AdaptiveTimeoutManager,
	gossipFrequency uint64,
	harshQuittersTime uint64,
	harshQuittersSlashingFraction uint64,
	appGossipValidatorSize uint64,
	appGossipNonValidatorSize uint64,
	gossipAcceptedFrontierSize uint64,
	appSendQueueSize uint64,
	peerNotConnectedF uint64,
	connectedPeers ...ids.NodeID,
) error {
	r.nodeID = nodeID
	r.log = logger
	r.timeoutManager = timeoutManager
	r.lastMsgTime = time.Now()

	// Add initial connected peers if provided
	for _, peerID := range connectedPeers {
		r.connectedPeers.Add(peerID)
	}

	r.log.Info("initialized simple router",
		log.Stringer("nodeID", nodeID),
		log.Int("connectedPeers", len(connectedPeers)),
	)

	return nil
}

func (r *SimpleRouter) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.requests[requestID] = &requestInfo{
		nodeID:    nodeID,
		chainID:   chainID,
		op:        op,
		startTime: time.Now(),
	}
}

func (r *SimpleRouter) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	r.lock.Lock()
	r.lastMsgTime = time.Now()

	// Extract chain ID from the message
	chainID, err := message.GetChainID(msg.Message())
	if err != nil {
		r.lock.Unlock()
		r.log.Debug("dropping message without chain ID",
			log.Stringer("message", msg.Op()),
			log.Stringer("nodeID", msg.NodeID()),
		)
		return
	}

	h, ok := r.chains[ids.ID(chainID)]
	r.lock.Unlock()

	if !ok {
		r.log.Debug("dropping message for unknown chain",
			log.Stringer("chainID", chainID),
			log.Stringer("nodeID", msg.NodeID()),
		)
		return
	}

	// Convert InboundMessage to handler.Message and pass to handler
	requestID, _ := message.GetRequestID(msg.Message())
	handlerMsg := handler.Message{
		NodeID:    ids.NodeID(msg.NodeID()),
		RequestID: requestID,
		Op:        handler.Op(msg.Op()),
		Message:   []byte{}, // TODO: extract actual message bytes
	}
	go h.HandleInbound(ctx, handlerMsg)
}

func (r *SimpleRouter) Shutdown(ctx context.Context) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.log.Info("shutting down router")

	// Clear all chains
	for chainID := range r.chains {
		r.log.Debug("removing chain",
			log.Stringer("chainID", chainID),
		)
	}

	r.chains = make(map[ids.ID]handler.Handler)
	r.requests = make(map[uint32]*requestInfo)
}

func (r *SimpleRouter) AddChain(ctx context.Context, h handler.Handler) {
	r.lock.Lock()
	defer r.lock.Unlock()

	// For now, we'll use a placeholder chain ID
	// In practice, the handler should have a way to identify its chain
	chainID := ids.GenerateTestID()
	r.chains[chainID] = h

	r.log.Info("added chain to router",
		log.Stringer("chainID", chainID),
	)
}

func (r *SimpleRouter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.connectedPeers.Add(nodeID)
	r.log.Debug("peer connected",
		log.Stringer("nodeID", nodeID),
		log.Any("version", nodeVersion),
		log.Stringer("netID", netID),
	)
}

func (r *SimpleRouter) Disconnected(nodeID ids.NodeID) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.connectedPeers.Remove(nodeID)
	r.log.Debug("peer disconnected",
		log.Stringer("nodeID", nodeID),
	)
}

func (r *SimpleRouter) Benched(chainID ids.ID, nodeID ids.NodeID) {
	r.log.Debug("peer benched",
		log.Stringer("chainID", chainID),
		log.Stringer("nodeID", nodeID),
	)
}

func (r *SimpleRouter) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	r.log.Debug("peer unbenched",
		log.Stringer("chainID", chainID),
		log.Stringer("nodeID", nodeID),
	)
}

func (r *SimpleRouter) HealthCheck(ctx context.Context) (interface{}, error) {
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

	// Check chain count
	chainCount := len(r.chains)
	details["chains"] = chainCount

	// Check pending requests
	requestCount := len(r.requests)
	details["pendingRequests"] = requestCount

	details["healthy"] = healthy
	return details, nil
}

// Deprecated implements the Router interface
func (r *SimpleRouter) Deprecated() {
	// This method exists for compatibility
}

// Trace wraps a router with tracing capabilities
func Trace(router Router, name string, tracer trace.Tracer) Router {
	// For now, just return the router unchanged
	// TODO: Implement actual tracing
	return router
}
