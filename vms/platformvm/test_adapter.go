// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"

	"github.com/luxfi/ids"
)

// TestAppSender is a test implementation of AppSender for platformvm tests
type TestAppSender struct{}

// SendAppGossip is a no-op for tests
func (t *TestAppSender) SendAppGossip(ctx context.Context, appGossipBytes []byte) error {
	return nil
}

// SendAppRequest is a no-op for tests
func (t *TestAppSender) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, appRequestBytes []byte) error {
	return nil
}

// SendAppResponse is a no-op for tests  
func (t *TestAppSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	return nil
}

// SendCrossChainAppRequest is a no-op for tests
func (t *TestAppSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	return nil
}

// SendCrossChainAppResponse is a no-op for tests
func (t *TestAppSender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	return nil
}

// SendCrossChainAppError is a no-op for tests
func (t *TestAppSender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	return nil
}