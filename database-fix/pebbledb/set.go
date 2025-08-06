// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

// set is a simple generic set implementation
type set[T comparable] map[T]struct{}

// Add adds an element to the set
func (s set[T]) Add(t T) {
	s[t] = struct{}{}
}

// Remove removes an element from the set
func (s set[T]) Remove(t T) {
	delete(s, t)
}

// Clear removes all elements from the set
func (s set[T]) Clear() {
	for k := range s {
		delete(s, k)
	}
}
