// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/version"
)

var _ ExternalHandler = (*testHandler)(nil)

// networkInboundHandler is the interface that testHandler needs to implement
type networkInboundHandler interface {
	HandleInbound(ctx context.Context, msg message.InboundMessage)
}

type testHandler struct {
	networkInboundHandler
	ConnectedF    func(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID)
	DisconnectedF func(nodeID ids.NodeID)
	HandleGossipF func(ctx context.Context, nodeID ids.NodeID, msg []byte)
}

// HandleInbound handles network message.InboundMessage
func (h *testHandler) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	// Forward to embedded handler if present
	if h.networkInboundHandler != nil {
		h.networkInboundHandler.HandleInbound(ctx, msg)
	}
}

func (h *testHandler) Connected(id ids.NodeID, nodeVersion *version.Application, netID ids.ID) {
	if h.ConnectedF != nil {
		h.ConnectedF(id, nodeVersion, netID)
	}
}

func (h *testHandler) Disconnected(id ids.NodeID) {
	if h.DisconnectedF != nil {
		h.DisconnectedF(id)
	}
}

func (h *testHandler) HandleGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) {
	if h.HandleGossipF != nil {
		h.HandleGossipF(ctx, nodeID, msg)
	}
}

func (h *testHandler) HandleTimeout(ctx context.Context) {
	// No-op for tests
}
