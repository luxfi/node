// Copyright (C) 2025-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"

	luxWarp "github.com/luxfi/warp"
)

var _ luxWarp.Verifier = (*xsvmVerifier)(nil)

// xsvmVerifier allows signing all warp messages
type xsvmVerifier struct{}

func (xsvmVerifier) Verify(context.Context, *luxWarp.Message, []byte) error {
	return nil
}

