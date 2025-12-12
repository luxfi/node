// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package lp118

import (
	"context"
	"github.com/luxfi/warp"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p"
)

// HandlerAdapter adapts an lp118.Handler to p2p.Handler
type HandlerAdapter struct {
	handler Handler
}

// NewHandlerAdapter creates a new adapter
func NewHandlerAdapter(handler Handler) p2p.Handler {
	return &HandlerAdapter{handler: handler}
}

// Gossip is not supported by lp118 handlers
func (h *HandlerAdapter) Gossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	// No-op - lp118 handlers don't support gossip
}

// Request forwards to the lp118 handler
func (h *HandlerAdapter) Request(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *warp.Error) {
	resp, err := h.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
	if err != nil {
		// Check if error is already an Error from warp package
		if appErr, ok := err.(*warp.Error); ok {
			return nil, &warp.Error{
				Code:    appErr.Code,
				Message: appErr.Message,
			}
		}
		return nil, &warp.Error{
			Code:    -1,
			Message: err.Error(),
		}
	}
	return resp, nil
}

// CrossChainAppRequest is not supported by lp118 handlers
func (h *HandlerAdapter) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return nil, nil
}
