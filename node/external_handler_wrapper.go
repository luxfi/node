// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"

	"github.com/luxfi/consensus/networking/router"
	"github.com/luxfi/node/message"
	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/utils/set"
)

// externalHandlerWrapper wraps a router to implement network.ExternalHandler
type externalHandlerWrapper struct {
	router router.Router
}

func (e *externalHandlerWrapper) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	e.router.HandleInbound(ctx, msg)
}

func (e *externalHandlerWrapper) Stop(ctx context.Context) {
	e.router.Shutdown(ctx)
}

func (e *externalHandlerWrapper) StopWithError(ctx context.Context, err error) {
	// Log the error and stop
	e.router.Shutdown(ctx)
}

func (e *externalHandlerWrapper) Connected(nodeID ids.NodeID) {
	e.router.Connected(nodeID, nil, ids.Empty)
}

func (e *externalHandlerWrapper) Disconnected(nodeID ids.NodeID) {
	e.router.Disconnected(nodeID)
}

func (e *externalHandlerWrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	return e.router.HealthCheck(ctx)
}

func (e *externalHandlerWrapper) Dispatch() error {
	// This is called to start processing messages
	return nil
}

func (e *externalHandlerWrapper) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg message.InboundMessage,
	engineType int,
) {
	e.router.RegisterRequest(ctx, nodeID, chainID, requestID, op, timeoutMsg, engineType)
}

func (e *externalHandlerWrapper) RegisterRequests(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg message.InboundMessage,
	engineType int,
) {
	// Register for each node
	for nodeID := range nodeIDs {
		e.router.RegisterRequest(ctx, nodeID, chainID, requestID, op, timeoutMsg, engineType)
	}
}