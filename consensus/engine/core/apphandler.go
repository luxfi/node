// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/version"
)

// AppHandler handles application-level network events
type AppHandler interface {
	// AppRequest is called when an application request is received.
	AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error

	// AppResponse is called when an application response is received.
	AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error

	// AppRequestFailed is called when an application request fails.
	AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *AppError) error

	// AppGossip is called when an application gossip message is received.
	AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error

	// Connected is called when a node is connected.
	Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error

	// Disconnected is called when a node is disconnected.
	Disconnected(ctx context.Context, nodeID ids.NodeID) error
}