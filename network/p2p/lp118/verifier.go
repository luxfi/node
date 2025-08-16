// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lp118

import (
	"context"
	
	"github.com/luxfi/node/consensus/engine/common"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// Verifier verifies warp messages according to LP-118
type Verifier interface {
	// Verify verifies an unsigned warp message with justification
	Verify(ctx context.Context, unsignedMessage *warp.UnsignedMessage, justification []byte) *common.AppError
}