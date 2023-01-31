// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnboundedBlockingDequePush(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)
	require.Empty(deque.List())
<<<<<<< HEAD
<<<<<<< HEAD
	_, ok := deque.Index(0)
	require.False(ok)
=======
>>>>>>> 6945e5d93 (add `List() []T` method to deque (#2403))
=======
	_, ok := deque.Index(0)
	require.False(ok)
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))

	ok = deque.PushRight(1)
	require.True(ok)
	require.Equal([]int{1}, deque.List())
<<<<<<< HEAD
<<<<<<< HEAD
	got, ok := deque.Index(0)
	require.True(ok)
	require.Equal(1, got)
=======
>>>>>>> 6945e5d93 (add `List() []T` method to deque (#2403))
=======
	got, ok := deque.Index(0)
	require.True(ok)
	require.Equal(1, got)
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))

	ok = deque.PushRight(2)
	require.True(ok)
	require.Equal([]int{1, 2}, deque.List())
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))
	got, ok = deque.Index(0)
	require.True(ok)
	require.Equal(1, got)
	got, ok = deque.Index(1)
	require.True(ok)
	require.Equal(2, got)
	_, ok = deque.Index(2)
	require.False(ok)
<<<<<<< HEAD
=======
>>>>>>> 6945e5d93 (add `List() []T` method to deque (#2403))
=======
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))

	ch, ok := deque.PopLeft()
	require.True(ok)
	require.Equal(1, ch)
	require.Equal([]int{2}, deque.List())
<<<<<<< HEAD
<<<<<<< HEAD
	got, ok = deque.Index(0)
	require.True(ok)
	require.Equal(2, got)
=======
>>>>>>> 6945e5d93 (add `List() []T` method to deque (#2403))
=======
	got, ok = deque.Index(0)
	require.True(ok)
	require.Equal(2, got)
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))
}

func TestUnboundedBlockingDequePop(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)
	require.Empty(deque.List())

	ok := deque.PushRight(1)
	require.True(ok)
	require.Equal([]int{1}, deque.List())
<<<<<<< HEAD
<<<<<<< HEAD
	got, ok := deque.Index(0)
	require.True(ok)
	require.Equal(1, got)
=======
>>>>>>> 6945e5d93 (add `List() []T` method to deque (#2403))
=======
	got, ok := deque.Index(0)
	require.True(ok)
	require.Equal(1, got)
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))

	ch, ok := deque.PopLeft()
	require.True(ok)
	require.Equal(1, ch)
	require.Empty(deque.List())

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		ch, ok := deque.PopLeft()
		require.True(ok)
		require.Equal(2, ch)
		wg.Done()
	}()

	ok = deque.PushRight(2)
	require.True(ok)
	wg.Wait()
	require.Empty(deque.List())
<<<<<<< HEAD
<<<<<<< HEAD
	_, ok = deque.Index(0)
	require.False(ok)
=======
>>>>>>> 6945e5d93 (add `List() []T` method to deque (#2403))
=======
	_, ok = deque.Index(0)
	require.False(ok)
>>>>>>> 6a86eca6b (Add Index method to deque (#2407))
}

func TestUnboundedBlockingDequeClose(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)

	ok := deque.PushLeft(1)
	require.True(ok)

	deque.Close()

	_, ok = deque.PopRight()
	require.False(ok)

	ok = deque.PushLeft(1)
	require.False(ok)
}
