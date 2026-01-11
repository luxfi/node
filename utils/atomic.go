// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"github.com/luxfi/atomic"
)

// Atomic is a re-export from the standalone atomic module for backward compatibility.
// Use github.com/luxfi/atomic directly for new code.
type Atomic[T any] = atomic.Atomic[T]

// NewAtomic creates a new Atomic with the given initial value.
// Use github.com/luxfi/atomic.NewAtomic directly for new code.
func NewAtomic[T any](value T) *Atomic[T] {
	return atomic.NewAtomic(value)
}
