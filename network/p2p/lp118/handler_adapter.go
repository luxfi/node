// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lp118

import (
	"context"
	"time"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/common"
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

// AppGossip is not supported by lp118 handlers
func (h *HandlerAdapter) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	// No-op - lp118 handlers don't support gossip
}

// AppRequest forwards to the lp118 handler
func (h *HandlerAdapter) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *core.AppError) {
	resp, err := h.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
	if err != nil {
		// Check if error is already an AppError from our own package
		if appErr, ok := err.(*common.AppError); ok {
			return nil, &core.AppError{
				Code:    int32(appErr.Code),
				Message: appErr.Message,
			}
		}
		return nil, &core.AppError{
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