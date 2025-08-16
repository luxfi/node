// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

var _ Handler = (*ThrottlerHandler)(nil)

func NewThrottlerHandler(handler Handler, throttler Throttler, log log.Logger) *ThrottlerHandler {
	return &ThrottlerHandler{
		handler:   handler,
		throttler: throttler,
		log:       log,
	}
}

type ThrottlerHandler struct {
	handler   Handler
	throttler Throttler
	log       log.Logger
}

func (t ThrottlerHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !t.throttler.Handle(nodeID) {
		t.log.Debug("dropping message",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "throttled"),
		)
		return
	}

	t.handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t ThrottlerHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *core.AppError) {
	if !t.throttler.Handle(nodeID) {
		return nil, ErrThrottled
	}

	return t.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

func (t ThrottlerHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return t.handler.CrossChainAppRequest(ctx, chainID, deadline, requestBytes)
}
