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
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/trace"
	"github.com/luxfi/node/version"
	"github.com/luxfi/timer"
	luxversion "github.com/luxfi/version"
)

// router implements Router interface for routing messages to chain handlers
type chainRouter struct {
	log            log.Logger
	lock           sync.RWMutex
	chains         map[ids.ID]handler.Handler
	timeoutManager timer.AdaptiveTimeoutManager
	nodeID         ids.NodeID
	healthConfig   HealthConfig
	lastMsgTime    time.Time
	connectedPeers set.Set[ids.NodeID]
}

// NewRouter creates a new chain router
func NewRouter(logger log.Logger, timeoutManager timer.AdaptiveTimeoutManager) Router {
	return &chainRouter{
		log:            logger,
		chains:         make(map[ids.ID]handler.Handler),
		timeoutManager: timeoutManager,
		connectedPeers: set.NewSet[ids.NodeID](10),
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

// versionedConnector is the capability a chain handler advertises when it can
// forward a peer's REAL application version to its VM. The consensus
// handler.Handler.Connected signature carries only the nodeID (the version was
// dropped at that boundary); a handler that implements this receives the real
// version instead. blockHandler implements it. The version must survive to the
// inner VM: the C-Chain EVM's state-sync peer tracker compares peer versions
// and dereferences a nil version, panicking a state-syncing node.
type versionedConnector interface {
	ConnectedWithVersion(ctx context.Context, nodeID ids.NodeID, nodeVersion *luxversion.Application) error
}

// toAppVersion converts the node's peer version (github.com/luxfi/node/version)
// to the github.com/luxfi/version.Application the VM Connected boundary
// (chain.VersionInfo) expects. nil-safe: a nil peer version maps to nil.
func toAppVersion(v *version.Application) *luxversion.Application {
	if v == nil {
		return nil
	}
	return &luxversion.Application{
		Name:  v.Name,
		Major: v.Major,
		Minor: v.Minor,
		Patch: v.Patch,
	}
}

func (r *chainRouter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	r.lock.Lock()
	r.connectedPeers.Add(nodeID)
	handlers := make([]handler.Handler, 0, len(r.chains))
	for _, h := range r.chains {
		handlers = append(handlers, h)
	}
	r.lock.Unlock()

	r.log.Debug("peer connected",
		log.Stringer("nodeID", nodeID),
		log.Any("version", nodeVersion),
		log.Stringer("netID", netID),
	)

	// Deliver the connection to every chain handler so VMs observe peer
	// connectivity — the P-chain uptime tracker in particular. Handlers dedup, so
	// repeated dispatch of the same connection (once per tracked network) is
	// safe. Dispatch OUTSIDE the router lock: a handler must never re-enter the
	// router while we hold it.
	//
	// Deliver the REAL peer version through the versionedConnector capability so
	// it reaches the inner VM (proposervm → EVM state-sync). Only handlers
	// that cannot carry a version fall back to the plain nodeID-only Connected.
	appVersion := toAppVersion(nodeVersion)
	for _, h := range handlers {
		var err error
		if vc, ok := h.(versionedConnector); ok {
			err = vc.ConnectedWithVersion(context.Background(), nodeID, appVersion)
		} else {
			err = h.Connected(context.Background(), nodeID)
		}
		if err != nil {
			r.log.Debug("chain handler Connected failed",
				log.Stringer("nodeID", nodeID),
				log.Err(err),
			)
		}
	}
}

func (r *chainRouter) Disconnected(nodeID ids.NodeID) {
	r.lock.Lock()
	r.connectedPeers.Remove(nodeID)
	handlers := make([]handler.Handler, 0, len(r.chains))
	for _, h := range r.chains {
		handlers = append(handlers, h)
	}
	r.lock.Unlock()

	r.log.Debug("peer disconnected",
		log.Stringer("nodeID", nodeID),
	)

	for _, h := range handlers {
		if err := h.Disconnected(context.Background(), nodeID); err != nil {
			r.log.Debug("chain handler Disconnected failed",
				log.Stringer("nodeID", nodeID),
				log.Err(err),
			)
		}
	}
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
	details["healthy"] = healthy

	return details, nil
}

func (r *chainRouter) Deprecated() {}

// Trace wraps a router with tracing capabilities
func Trace(router Router, name string, tracer trace.Tracer) Router {
	return router
}
