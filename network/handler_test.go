// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	
	"github.com/luxfi/consensus/router"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/version"
)

var _ router.ExternalHandler = (*testHandler)(nil)

type testHandler struct {
	router.InboundHandler
	ConnectedF    func(nodeID ids.NodeID, nodeVersion *version.Application, netID ids.ID)
	DisconnectedF func(nodeID ids.NodeID)
	HandleGossipF func(ctx context.Context, nodeID ids.NodeID, msg []byte)
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
