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

// signatureRequestVerifier is the warp signature verifier wired into
// the platformvm network. Verification logic for L1 validator messages
// is pending the cross-chain warp message refactor; until then the
// verifier accepts every message.
type signatureRequestVerifier struct {
	stateLock sync.Locker
	state     state.Chain
}

func (s signatureRequestVerifier) Verify(
	_ context.Context,
	_ *warp.Core,
	_ []byte,
) error {
	return nil
}
