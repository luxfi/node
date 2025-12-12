// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import "github.com/luxfi/p2p"

// Re-export error types from p2p package
var (
	ErrUnexpected          = p2p.ErrUnexpected
	ErrUnregisteredHandler = p2p.ErrUnregisteredHandler
	ErrNotValidator        = p2p.ErrNotValidator
	ErrThrottled           = p2p.ErrThrottled
)

// Error is an alias for p2p.Error
type Error = p2p.Error
