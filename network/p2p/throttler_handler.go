// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/utils/logging"
)

var _ Handler = (*ThrottlerHandler)(nil)

func NewThrottlerHandler(handler Handler, throttler Throttler, log logging.Logger) *ThrottlerHandler {
	return &ThrottlerHandler{
		handler:   handler,
		throttler: throttler,
		log:       log,
	}
}

type ThrottlerHandler struct {
	handler   Handler
	throttler Throttler
	log       logging.Logger
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

func (t ThrottlerHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	if !t.throttler.Handle(nodeID) {
		return nil, ErrThrottled
	}

	return t.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

func (t ThrottlerHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return t.handler.CrossChainAppRequest(ctx, chainID, deadline, requestBytes)
}
