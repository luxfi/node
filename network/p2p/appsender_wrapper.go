// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/luxfi/consensus/core/appsender"
	"github.com/luxfi/ids"
)

// AppSenderWrapper wraps a consensus AppSender to provide CrossChain methods
type AppSenderWrapper struct {
	appsender.AppSender
}

// NewAppSenderWrapper creates a new wrapper that adds CrossChain methods
func NewAppSenderWrapper(sender appsender.AppSender) ExtendedAppSender {
	return &AppSenderWrapper{AppSender: sender}
}

// SendCrossChainAppRequest is not implemented for wrapped senders
func (w *AppSenderWrapper) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	// Not implemented for regular app senders
	return nil
}

// SendCrossChainAppResponse is not implemented for wrapped senders
func (w *AppSenderWrapper) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	// Not implemented for regular app senders
	return nil
}

// SendCrossChainAppError is not implemented for wrapped senders
func (w *AppSenderWrapper) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	// Not implemented for regular app senders
	return nil
}