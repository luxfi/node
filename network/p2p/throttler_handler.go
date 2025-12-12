// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/p2p"
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

func (t ThrottlerHandler) Gossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !t.throttler.Handle(nodeID) {
		t.log.Debug("dropping message",
			log.Stringer("nodeID", nodeID),
			log.UserString("reason", "throttled"),
		)
		return
	}

	t.handler.Gossip(ctx, nodeID, gossipBytes)
}

func (t ThrottlerHandler) Request(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *p2p.Error) {
	if !t.throttler.Handle(nodeID) {
		return nil, ErrThrottled
	}

	return t.handler.Request(ctx, nodeID, deadline, requestBytes)
}
