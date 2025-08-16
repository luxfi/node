// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"

	"github.com/luxfi/node/consensus/engine/common"
	"github.com/luxfi/node/network/p2p/lp118"
	"github.com/luxfi/node/vms/platformvm/warp"
)

var _ lp118.Verifier = (*lp118Verifier)(nil)

// lp118Verifier allows signing all warp messages
type lp118Verifier struct{}

func (lp118Verifier) Verify(context.Context, *warp.UnsignedMessage, []byte) *common.AppError {
	return nil
}
