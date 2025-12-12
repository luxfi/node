// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import "github.com/luxfi/warp"

var (
	// ErrUnexpected should be used to indicate that a request failed due to a
	// generic error
	ErrUnexpected = &warp.Error{
		Code:    -1,
		Message: "unexpected error",
	}
	// ErrUnregisteredHandler should be used to indicate that a request failed
	// due to it not matching a registered handler
	ErrUnregisteredHandler = &warp.Error{
		Code:    -2,
		Message: "unregistered handler",
	}
	// ErrNotValidator should be used to indicate that a request failed due to
	// the requesting peer not being a validator
	ErrNotValidator = &warp.Error{
		Code:    -3,
		Message: "not a validator",
	}
	// ErrThrottled should be used to indicate that a request failed due to the
	// requesting peer exceeding a rate limit
	ErrThrottled = &warp.Error{
		Code:    -4,
		Message: "throttled",
	}
)
