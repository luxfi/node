//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"

	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/warp"
)

var _ warp.Verifier = (*signatureRequestVerifier)(nil)

type signatureRequestVerifier struct {
	stateLock sync.Locker
	state     state.Chain
}

func (s signatureRequestVerifier) Verify(
	_ context.Context,
	_ *warp.UnsignedMessage,
	_ []byte,
) error {
	// ZAP mode: warp verification not implemented
	return nil
}
