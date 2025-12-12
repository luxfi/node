// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lp118

import (
	"context"

	"github.com/luxfi/warp"
)

// Verifier verifies warp messages according to LP-118
type Verifier interface {
	// Verify verifies an unsigned warp message with justification
	// Returns an error if verification fails, nil otherwise
	Verify(ctx context.Context, unsignedMessage *warp.UnsignedMessage, justification []byte) error
}
