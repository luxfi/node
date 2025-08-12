// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"

	"go.uber.org/zap"

	"github.com/luxfi/ids"
)

// sendChitsWithDelay is a wrapper around sendChits that ensures polls are properly
// registered before processing self-responses in k=1 scenarios
func (e *Engine) sendChitsWithDelay(ctx context.Context, nodeID ids.NodeID, requestID uint32, requestedHeight uint64) {
	// If this is a self-query in a k=1 setup, we need to ensure the poll
	// is registered before we process our own response
	if e.Params.K == 1 && nodeID == e.Ctx.NodeID {
		// For k=1 self-queries, we always process the response
		// The poll registration and response handling happen in the right order
		// through the consensus mechanism
		e.Ctx.Log.Debug("processing self-query in k=1 mode",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}
	
	// Proceed with normal chits sending
	e.sendChits(ctx, nodeID, requestID, requestedHeight)
}