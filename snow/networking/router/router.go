// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/api/health"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/snow/networking/benchlist"
	"github.com/luxfi/node/snow/networking/handler"
	"github.com/luxfi/node/snow/networking/timeout"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
)

// Router routes consensus messages to the Handler of the consensus
// engine that the messages are intended for
type Router interface {
	ExternalHandler
	InternalHandler

	Initialize(
		nodeID ids.NodeID,
		log logging.Logger,
		timeouts timeout.Manager,
		shutdownTimeout time.Duration,
		criticalChains set.Set[ids.ID],
		sybilProtectionEnabled bool,
		trackedSubnets set.Set[ids.ID],
		onFatal func(exitCode int),
		healthConfig HealthConfig,
		reg prometheus.Registerer,
	) error
	Shutdown(context.Context)
	AddChain(ctx context.Context, chain handler.Handler)
	health.Checker
}

// InternalHandler deals with messages internal to this node
type InternalHandler interface {
	benchlist.Benchable

	RegisterRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
		op message.Op,
		failedMsg message.InboundMessage,
		engineType p2p.EngineType,
	)

	// HandleInternal enqueues a message to be handled by the consensus engine
	// in the future. This function should only be called with messages
	// generated internally, and not from messages received from a peer.
	HandleInternal(context.Context, message.InboundMessage)
}
