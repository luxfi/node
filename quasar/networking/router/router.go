// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/message"
	"github.com/luxfi/node/v2/utils/set"
	"github.com/luxfi/log"
	"github.com/luxfi/node/v2/quasar/networking/timeout"
	"github.com/luxfi/node/v2/network/p2p"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/version"
)

// Router routes consensus messages
type Router interface {
	// Initialize initializes the router
	Initialize(
		nodeID ids.NodeID,
		log log.Logger,
		msgCreator message.Creator,
		timeouts *timeout.Manager,
		gossipFrequency time.Duration,
		shutdownTimeout time.Duration,
		criticalChains set.Set[ids.ID],
		sybilProtectionEnabled bool,
		trackedSubnets set.Set[ids.ID],
		onFatal func(exitCode int),
		healthConfig HealthConfig,
		peerTracker *p2p.PeerTracker,
	) error

	// RegisterRequest registers an outstanding request
	RegisterRequest(
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
		msgType message.Op,
		responseOp message.Op,
		timeoutMsg message.InboundMessage,
		engineType p2p.EngineType,
	)

	// HandleInbound handles an inbound message
	HandleInbound(context.Context, message.InboundMessage) error

	// Shutdown shuts down the router
	Shutdown(context.Context) error

	// AddChain adds a chain to the router
	AddChain(context.Context, *Chain) error

	// RemoveChain removes a chain from the router
	RemoveChain(context.Context, ids.ID) error

	// Benched routes messages to a benchlist
	Benched(chainID ids.ID, nodeID ids.NodeID) error

	// Unbenched routes messages from a benchlist
	Unbenched(chainID ids.ID, nodeID ids.NodeID) error

	// HealthCheck returns the router's health
	HealthCheck(context.Context) (interface{}, error)
}

// Chain represents a chain connected to the router
type Chain struct {
	ChainID     ids.ID
	SubnetID    ids.ID
	Context     *core.Context
	Handler     core.Handler
	VM          core.ChainVM
}

// HealthConfig configures router health checks
type HealthConfig struct {
	MaxOutstandingRequests  int
	MaxOutstandingDuration  time.Duration
	MaxRunTimeRequests      time.Duration
	MaxDropRate             float64
	MaxDropRateHalflife     time.Duration
}

// InboundHandler handles inbound messages
type InboundHandler interface {
	// HandleInbound handles an inbound message
	HandleInbound(context.Context, message.InboundMessage) error
}

// ExternalHandler handles messages from peers, including connection lifecycle events
type ExternalHandler interface {
	InboundHandler
	
	// Connected is called when a peer connects
	Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID)
	
	// Disconnected is called when a peer disconnects
	Disconnected(nodeID ids.NodeID)
}