// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	metric "github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/p2p"
	"github.com/luxfi/node/version"
	"github.com/luxfi/timer"
	"github.com/luxfi/node/trace"
)

// router implements Router interface for routing messages to chain handlers
type chainRouter struct {
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

// NewRouter creates a new chain router
func NewRouter(logger log.Logger, timeoutManager timer.AdaptiveTimeoutManager) Router {
	return &chainRouter{
		log:            logger,
		chains:         make(map[ids.ID]handler.Handler),
		timeoutManager: timeoutManager,
		connectedPeers: set.NewSet[ids.NodeID](10),
		requests:       make(map[uint32]*requestInfo),
		lastMsgTime:    time.Now(),
	}
}

// NewSimpleRouter is deprecated - use NewRouter instead
func NewSimpleRouter(logger log.Logger, timeoutManager timer.AdaptiveTimeoutManager) Router {
	return NewRouter(logger, timeoutManager)
}

func (r *chainRouter) Initialize(
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

	for _, peerID := range connectedPeers {
		r.connectedPeers.Add(peerID)
	}

	r.log.Info("initialized chain router",
		log.Stringer("nodeID", nodeID),
		log.Int("connectedPeers", len(connectedPeers)),
	)

	return nil
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
	r.lock.Lock()
	defer r.lock.Unlock()

	r.requests[requestID] = &requestInfo{
		nodeID:    nodeID,
		chainID:   chainID,
		op:        op,
		startTime: time.Now(),
	}
}

func (r *chainRouter) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	r.lock.Lock()
	r.lastMsgTime = time.Now()

	// Extract chain ID from the message
	chainID, err := message.GetChainID(msg.Message())
	if err != nil {
		r.lock.Unlock()
		r.log.Debug("dropping message without chain ID",
			log.Stringer("op", msg.Op()),
			log.Stringer("nodeID", msg.NodeID()),
		)
		return
	}

	h, ok := r.chains[chainID]
	r.lock.Unlock()

	if !ok {
		r.log.Debug("dropping message for unknown chain",
			log.Stringer("chainID", chainID),
			log.Binary("chainIDBytes", chainID[:]),
			log.Stringer("nodeID", msg.NodeID()),
			log.Stringer("op", msg.Op()),
		)
		return
	}

	// Convert message Op to handler Op
	consensusOp, ok := message.ToConsensusOp(msg.Op())
	if !ok {
		r.log.Debug("unhandled message op",
			log.Stringer("nodeID", msg.NodeID()),
			log.Stringer("op", msg.Op()),
		)
		return
	}

	// Extract request ID and container bytes
	requestID, _ := message.GetRequestID(msg.Message())
	containerBytes := message.GetContainerBytes(msg.Message())

	// Create handler message
	handlerMsg := handler.Message{
		NodeID:    ids.NodeID(msg.NodeID()),
		RequestID: requestID,
		Op:        handler.Op(consensusOp),
		Message:   containerBytes,
	}

	// Dispatch to handler asynchronously
	go func() {
		if err := h.HandleInbound(ctx, handlerMsg); err != nil {
			r.log.Debug("handler returned error",
				log.Stringer("chainID", chainID),
				log.Stringer("nodeID", msg.NodeID()),
				log.Stringer("op", msg.Op()),
				log.Err(err),
			)
		}
	}()
}

func (r *chainRouter) Shutdown(ctx context.Context) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.log.Info("shutting down router")

	r.chains = make(map[ids.ID]handler.Handler)
	r.requests = make(map[uint32]*requestInfo)
}

func (r *chainRouter) AddChain(ctx context.Context, chainID ids.ID, h handler.Handler) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.chains[chainID] = h

	r.log.Info("added chain to router",
		log.Stringer("chainID", chainID),
		log.Binary("chainIDBytes", chainID[:]),
		log.Int("totalChains", len(r.chains)),
	)
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

	connectedCount := r.connectedPeers.Len()
	details["connectedPeers"] = connectedCount
	if connectedCount < r.healthConfig.MinConnectedPeers {
		healthy = false
		details["error"] = "insufficient connected peers"
	}

	timeSinceMsg := time.Since(r.lastMsgTime)
	details["timeSinceLastMessage"] = timeSinceMsg.String()
	if timeSinceMsg > r.healthConfig.MaxTimeSinceMsgReceived {
		healthy = false
		details["error"] = "no recent messages"
	}

	details["chains"] = len(r.chains)
	details["pendingRequests"] = len(r.requests)
	details["healthy"] = healthy

	return details, nil
}

func (r *chainRouter) Deprecated() {}

// Trace wraps a router with tracing capabilities
func Trace(router Router, name string, tracer trace.Tracer) Router {
	return router
}
