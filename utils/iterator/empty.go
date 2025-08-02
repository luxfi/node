// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/luxfi/node/v2/utils"

var _ Iterator[any] = Empty[any]{}

// Empty is an iterator with no elements.
type Empty[T any] struct{}

func (Empty[_]) Next() bool {
	return false
}

func (Empty[T]) Value() T {
	return utils.Zero[T]()
}

func (Empty[_]) Release() {}
