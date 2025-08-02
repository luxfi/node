// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"

	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/network/p2p/lp118"
	"github.com/luxfi/node/v2/vms/platformvm/warp"
)

var _ lp118.Verifier = (*lp118Verifier)(nil)

// lp118Verifier allows signing all warp messages
type lp118Verifier struct{}

func (lp118Verifier) Verify(context.Context, *warp.UnsignedMessage, []byte) *core.AppError {
	return nil
}
