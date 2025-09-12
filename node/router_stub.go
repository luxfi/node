// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"time"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/consensus/networking/sender"
	"github.com/luxfi/consensus/networking/timeout"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/version"
	"github.com/prometheus/client_golang/prometheus"
)

// ChainRouter is a router that routes messages to different chains
type ChainRouter interface {
	router.Router
	AddChain(ctx context.Context, chain handler.Handler) error
	Shutdown(ctx context.Context) error
}

// Trace is a noop tracer for router
func Trace(router router.Router, name string, tracer any) router.Router {
	return router
}

// NewChainRouter creates a new chain router
func NewChainRouter(
	log log.Logger,
	sender sender.Sender,
	timeoutManager timeout.Manager,
	closeTimeout time.Duration,
	criticalChains map[ids.ID]struct{},
	whitelistedNetIDs map[ids.ID]struct{},
	onFatal func(exitCode int),
	config HealthConfig,
	registerer prometheus.Registerer,
	p2pTracker interface{},
) ChainRouter {
	return &chainRouter{
		log: log,
	}
}

type chainRouter struct {
	log log.Logger
}

// Implement router.Router interface
func (c *chainRouter) Deprecated() {
	// Required by router.Router interface
}

// Implement ChainRouter methods
func (c *chainRouter) AddChain(ctx context.Context, chain handler.Handler) error {
	// Stub implementation
	return nil
}

func (c *chainRouter) Shutdown(ctx context.Context) error {
	// Stub implementation
	return nil
}

// stubBenchlistManager implements consensus benchlist.Manager
type stubBenchlistManager struct{}

func (s *stubBenchlistManager) Deprecated() {
	// Required by benchlist.Manager interface
}

// externalHandlerWrapper wraps a router.Router to implement network.ExternalHandler
type externalHandlerWrapper struct {
	router router.Router
}

func (e *externalHandlerWrapper) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	// Handle connection - router doesn't have this method
}

func (e *externalHandlerWrapper) Disconnected(nodeID ids.NodeID) {
	// Handle disconnection - router doesn't have this method
}

func (e *externalHandlerWrapper) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	// Forward to router if it had this method
}

type noBenchable struct{}

func (n *noBenchable) Benched(chainID ids.ID, nodeID ids.NodeID)   {}
func (n *noBenchable) Unbenched(chainID ids.ID, nodeID ids.NodeID) {}

// NewLockedCalculator creates a new locked uptime calculator
func NewLockedCalculator() uptime.LockedCalculator {
	return &lockedCalculator{
		calculator: &uptime.NoOpCalculator{},
	}
}

type lockedCalculator struct {
	calculator uptime.Calculator
}

func (l *lockedCalculator) CalculateUptime(nodeID ids.NodeID, subnetID ids.ID) (time.Duration, time.Duration, error) {
	// Return stub values for uptime calculation
	return 0, 0, nil
}

func (l *lockedCalculator) CalculateUptimePercent(nodeID ids.NodeID, subnetID ids.ID) (float64, error) {
	return l.calculator.CalculateUptimePercent(nodeID, subnetID)
}

func (l *lockedCalculator) CalculateUptimePercentFrom(nodeID ids.NodeID, subnetID ids.ID, startTime time.Time) (float64, error) {
	return l.calculator.CalculateUptimePercentFrom(nodeID, subnetID, startTime)
}

func (l *lockedCalculator) SetCalculator(subnetID ids.ID, calc uptime.Calculator) error {
	l.calculator = calc
	return nil
}