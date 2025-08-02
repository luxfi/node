// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core"
	"github.com/luxfi/node/quasar/engine/core/appsender"
	"github.com/luxfi/node/utils/set"
)

// appSenderWrapper wraps a core.AppSender to implement appsender.AppSender
type appSenderWrapper struct {
	sender core.AppSender
}

// newAppSenderWrapper creates a new wrapper
func newAppSenderWrapper(sender core.AppSender) appsender.AppSender {
	return &appSenderWrapper{sender: sender}
}

// SendAppRequest implements appsender.AppSender
func (w *appSenderWrapper) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error {
	// Convert set to slice
	nodeIDSlice := nodeIDs.List()
	return w.sender.SendAppRequest(ctx, nodeIDSlice, requestID, message)
}

// SendAppResponse implements appsender.AppSender
func (w *appSenderWrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error {
	return w.sender.SendAppResponse(ctx, nodeID, requestID, message)
}

// SendAppGossip implements appsender.AppSender
func (w *appSenderWrapper) SendAppGossip(ctx context.Context, config appsender.SendConfig, message []byte) error {
	// Simple implementation - just send the gossip message
	return w.sender.SendAppGossip(ctx, message)
}

// SendAppGossipSpecific implements appsender.AppSender
func (w *appSenderWrapper) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], message []byte) error {
	// core.AppSender doesn't have this method, so we'll use SendAppGossip as a fallback
	return w.sender.SendAppGossip(ctx, message)
}

// SendCrossChainAppRequest implements appsender.AppSender
func (w *appSenderWrapper) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error {
	return w.sender.SendCrossChainAppRequest(ctx, chainID, requestID, message)
}

// SendCrossChainAppResponse implements appsender.AppSender
func (w *appSenderWrapper) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error {
	return w.sender.SendCrossChainAppResponse(ctx, chainID, requestID, message)
}