// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import "github.com/luxfi/node/v2/quasar/engine/core"

var (
	// ErrUnexpected should be used to indicate that a request failed due to a
	// generic error
	ErrUnexpected = &core.AppError{
		Code:    -1,
		Message: "unexpected error",
	}
	// ErrUnregisteredHandler should be used to indicate that a request failed
	// due to it not matching a registered handler
	ErrUnregisteredHandler = &core.AppError{
		Code:    -2,
		Message: "unregistered handler",
	}
	// ErrNotValidator should be used to indicate that a request failed due to
	// the requesting peer not being a validator
	ErrNotValidator = &core.AppError{
		Code:    -3,
		Message: "not a validator",
	}
	// ErrThrottled should be used to indicate that a request failed due to the
	// requesting peer exceeding a rate limit
	ErrThrottled = &core.AppError{
		Code:    -4,
		Message: "throttled",
	}
)
