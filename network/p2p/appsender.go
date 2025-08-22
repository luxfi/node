// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/luxfi/consensus/core/appsender"
	"github.com/luxfi/ids"
)

// ExtendedAppSender extends the consensus AppSender with cross-chain methods
type ExtendedAppSender interface {
	appsender.AppSender

	// SendCrossChainAppRequest sends a cross-chain app request
	SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error
	
	// SendCrossChainAppResponse sends a cross-chain app response
	SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error
	
	// SendCrossChainAppError sends a cross-chain app error
	SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error
}