// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/utils/set"
)

// Adapter adapts an AppSender to core.AppSender interface
type Adapter struct {
	sender AppSender
}

// NewAdapter creates a new adapter that converts AppSender to core.AppSender
func NewAdapter(sender AppSender) core.AppSender {
	return &Adapter{sender: sender}
}

// SendAppRequest implements core.AppSender
func (a *Adapter) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, msg []byte) error {
	nodeSet := set.Of(nodeIDs...)
	return a.sender.SendAppRequest(ctx, nodeSet, requestID, msg)
}

// SendAppResponse implements core.AppSender
func (a *Adapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return a.sender.SendAppResponse(ctx, nodeID, requestID, msg)
}

// SendAppError implements core.AppSender
func (a *Adapter) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// Since the underlying AppSender doesn't have SendAppError, we send it as a special response
	// This is a workaround - in a real implementation, you'd want to extend the AppSender interface
	errorData := []byte{0x00} // Error marker
	errorData = append(errorData, byte(errorCode>>24), byte(errorCode>>16), byte(errorCode>>8), byte(errorCode))
	errorData = append(errorData, []byte(errorMessage)...)
	return a.sender.SendAppResponse(ctx, nodeID, requestID, errorData)
}

// SendAppGossip implements core.AppSender
func (a *Adapter) SendAppGossip(ctx context.Context, msg []byte) error {
	// Send to all nodes (empty config means all)
	return a.sender.SendAppGossip(ctx, SendConfig{}, msg)
}

// SendCrossChainAppRequest implements core.AppSender
func (a *Adapter) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return a.sender.SendCrossChainAppRequest(ctx, chainID, requestID, msg)
}

// SendCrossChainAppResponse implements core.AppSender
func (a *Adapter) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return a.sender.SendCrossChainAppResponse(ctx, chainID, requestID, msg)
}