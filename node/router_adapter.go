// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"time"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	metric "github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/router"
	"github.com/luxfi/node/version"
	"github.com/luxfi/timer"
)

// routerAdapter adapts the node Router interface to router.Router interface
type routerAdapter struct {
	router Router
}

// chainHandlerAdapter adapts router.ChainHandler to handler.Handler
type chainHandlerAdapter struct {
	chainHandler router.ChainHandler
}

func (c *chainHandlerAdapter) HandleInbound(ctx context.Context, msg handler.Message) error {
	// This would need proper message conversion
	// For now, we just return nil as this is a stub
	return nil
}

func (c *chainHandlerAdapter) HandleOutbound(ctx context.Context, msg handler.Message) error {
	// This would need proper message conversion
	return nil
}

func (c *chainHandlerAdapter) Connected(ctx context.Context, nodeID ids.NodeID) error {
	// ChainHandler doesn't have a Connected method
	return nil
}

func (c *chainHandlerAdapter) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// ChainHandler doesn't have a Disconnected method
	return nil
}

// NewRouterAdapter creates a new router adapter
func NewRouterAdapter(r Router) router.Router {
	return &routerAdapter{router: r}
}

// Initialize the router - delegates to the underlying router
func (r *routerAdapter) Initialize(
	nodeID ids.NodeID,
	logger log.Logger,
	timeoutManager timer.AdaptiveTimeoutManager,
	closeTimeout time.Duration,
	criticalChains set.Set[ids.ID],
	sybilProtectionEnabled bool,
	onFatal func(int),
	healthConfig router.HealthConfig,
	reg metric.Registerer,
	namespace string,
) error {
	// The node Router has a different Initialize signature, so we can't directly delegate
	// This adapter assumes the router is already initialized
	return nil
}

// RegisterChain registers a handler for a specific chain
func (r *routerAdapter) RegisterChain(chainID ids.ID, chainHandler router.ChainHandler) error {
	// Wrap the router.ChainHandler to adapt it to consensus handler.Handler
	adapter := &chainHandlerAdapter{chainHandler: chainHandler}
	r.router.AddChain(context.Background(), chainID, adapter)
	return nil
}

// UnregisterChain removes a handler for a specific chain
func (r *routerAdapter) UnregisterChain(chainID ids.ID) error {
	// The node Router doesn't have a way to unregister chains
	return nil
}

// HandleInbound handles an inbound message
func (r *routerAdapter) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	r.router.HandleInbound(ctx, msg)
}

// Connected is called when a peer connects
func (r *routerAdapter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	r.router.Connected(nodeID, nodeVersion, netID)
}

// Disconnected is called when a peer disconnects
func (r *routerAdapter) Disconnected(nodeID ids.NodeID) {
	r.router.Disconnected(nodeID)
}

// RegisterRequest registers an outbound request
func (r *routerAdapter) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	r.router.RegisterRequest(ctx, nodeID, chainID, requestID, op, timeoutMsg, engineType)
}

// RegisterRequests registers outbound requests to multiple nodes
func (r *routerAdapter) RegisterRequests(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg message.InboundMessage,
	engineType p2p.EngineType,
) {
	// Delegate to RegisterRequest for each node
	for nodeID := range nodeIDs {
		r.RegisterRequest(ctx, nodeID, chainID, requestID, op, timeoutMsg, engineType)
	}
}

// HealthCheck returns the router's health status
func (r *routerAdapter) HealthCheck(ctx context.Context) (interface{}, error) {
	return r.router.HealthCheck(ctx)
}

// Shutdown gracefully shuts down the router
func (r *routerAdapter) Shutdown(ctx context.Context) {
	r.router.Shutdown(ctx)
}
