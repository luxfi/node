// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxdefi/luxd/api/health"
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/message"
	"github.com/luxdefi/luxd/snow/networking/benchlist"
	"github.com/luxdefi/luxd/snow/networking/handler"
	"github.com/luxdefi/luxd/snow/networking/timeout"
	"github.com/luxdefi/luxd/utils/logging"
)

// Router routes consensus messages to the Handler of the consensus
// engine that the messages are intended for
type Router interface {
	ExternalHandler
	InternalHandler

	Initialize(
		nodeID ids.NodeID,
		log logging.Logger,
		msgCreator message.InternalMsgBuilder,
		timeouts timeout.Manager,
		shutdownTimeout time.Duration,
		criticalChains ids.Set,
		whiteListedSubnets ids.Set,
		onFatal func(exitCode int),
		healthConfig HealthConfig,
		metricsNamespace string,
		metricsRegisterer prometheus.Registerer,
	) error
	Shutdown()
	AddChain(chain handler.Handler)
	health.Checker
}

// InternalHandler deals with messages internal to this node
type InternalHandler interface {
	benchlist.Benchable

	RegisterRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		sourceChainID ids.ID,
		destinationChainID ids.ID,
		requestID uint32,
		op message.Op,
	)
}
