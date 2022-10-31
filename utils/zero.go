// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// Returns a new instance of a T.
func Zero[T any]() T {
	return *new(T) //nolint:gocritic
}
