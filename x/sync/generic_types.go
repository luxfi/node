//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sync provides generic types for synchronization.
// This file contains the generic implementations that can be used
// with any comparable type, while maintaining backward compatibility
// with the existing byte-based implementation.

package sync

import (
	"bytes"
	"time"

	"github.com/google/btree"

	"github.com/luxfi/ids"
	"github.com/luxfi/container/heap"
	"github.com/luxfi/container/maybe"
)

// GenericWorkItem represents a work item that can work with any comparable type T
type GenericWorkItem[T any] struct {
	Start       maybe.Maybe[T]
	End         maybe.Maybe[T]
	Priority    priority
	LocalRootID ids.ID
	Attempt     int
	QueueTime   time.Time
}

// RequestFailed increments the attempt counter
func (w *GenericWorkItem[T]) RequestFailed() {
	attempt := w.Attempt + 1
	// Overflow check
	if attempt > w.Attempt {
		w.Attempt = attempt
	}
}

// NewGenericWorkItem creates a new generic work item
func NewGenericWorkItem[T any](
	localRootID ids.ID,
	start maybe.Maybe[T],
	end maybe.Maybe[T],
	priority priority,
	queueTime time.Time,
) *GenericWorkItem[T] {
	return &GenericWorkItem[T]{
		LocalRootID: localRootID,
		Start:       start,
		End:         end,
		Priority:    priority,
		QueueTime:   queueTime,
	}
}

// GenericWorkHeap is a priority queue that can work with any type T
type GenericWorkHeap[T any] struct {
	// Max heap of items by priority
	innerHeap heap.Set[*GenericWorkItem[T]]
	// Items sorted by range start
	sortedItems *btree.BTreeG[*GenericWorkItem[T]]
	closed      bool
	compareFn   func(a, b T) int
	equalFn     func(a, b T) bool
}

// NewGenericWorkHeap creates a new generic work heap
func NewGenericWorkHeap[T any](compareFn func(a, b T) int, equalFn func(a, b T) bool) *GenericWorkHeap[T] {
	wh := &GenericWorkHeap[T]{
		compareFn: compareFn,
		equalFn:   equalFn,
	}

	wh.innerHeap = heap.NewSet[*GenericWorkItem[T]](func(a, b *GenericWorkItem[T]) bool {
		return a.Priority > b.Priority
	})

	wh.sortedItems = btree.NewG(
		2,
		func(a, b *GenericWorkItem[T]) bool {
			aNothing := a.Start.IsNothing()
			bNothing := b.Start.IsNothing()
			if aNothing {
				return !bNothing
			}
			if bNothing {
				return false
			}
			return compareFn(a.Start.Value(), b.Start.Value()) < 0
		},
	)

	return wh
}

// Close marks the heap as closed
func (wh *GenericWorkHeap[T]) Close() {
	wh.closed = true
}

// Insert adds a new item into the heap
func (wh *GenericWorkHeap[T]) Insert(item *GenericWorkItem[T]) {
	if wh.closed {
		return
	}
	wh.innerHeap.Push(item)
	wh.sortedItems.ReplaceOrInsert(item)
}

// GetWork pops and returns a work item from the heap
func (wh *GenericWorkHeap[T]) GetWork() *GenericWorkItem[T] {
	if wh.closed || wh.Len() == 0 {
		return nil
	}
	item, _ := wh.innerHeap.Pop()
	wh.sortedItems.Delete(item)
	return item
}

// MergeInsert inserts the item into the heap, merging with adjacent items if possible
func (wh *GenericWorkHeap[T]) MergeInsert(item *GenericWorkItem[T]) {
	if wh.closed {
		return
	}

	var mergedBefore, mergedAfter *GenericWorkItem[T]
	searchItem := &GenericWorkItem[T]{
		Start: item.Start,
	}

	wh.sortedItems.DescendLessOrEqual(
		searchItem,
		func(beforeItem *GenericWorkItem[T]) bool {
			if item.LocalRootID == beforeItem.LocalRootID &&
				maybe.Equal(item.Start, beforeItem.End, wh.equalFn) {
				beforeItem.End = item.End
				beforeItem.Priority = max(item.Priority, beforeItem.Priority)
				wh.innerHeap.Fix(beforeItem)
				mergedBefore = beforeItem
			}
			return false
		})

	wh.sortedItems.AscendGreaterOrEqual(
		searchItem,
		func(afterItem *GenericWorkItem[T]) bool {
			if item.LocalRootID == afterItem.LocalRootID &&
				maybe.Equal(item.End, afterItem.Start, wh.equalFn) {
				afterItem.Start = item.Start
				afterItem.Priority = max(item.Priority, afterItem.Priority)
				wh.innerHeap.Fix(afterItem)
				mergedAfter = afterItem
			}
			return false
		})

	if mergedBefore != nil && mergedAfter != nil {
		mergedBefore.End = mergedAfter.End
		wh.remove(mergedAfter)
		mergedBefore.Priority = max(mergedBefore.Priority, mergedAfter.Priority)
		wh.innerHeap.Fix(mergedBefore)
	}

	if mergedBefore == nil && mergedAfter == nil {
		wh.Insert(item)
	}
}

// remove deletes an item from the heap
func (wh *GenericWorkHeap[T]) remove(item *GenericWorkItem[T]) {
	wh.innerHeap.Remove(item)
	wh.sortedItems.Delete(item)
}

// Len returns the number of items in the heap
func (wh *GenericWorkHeap[T]) Len() int {
	return wh.innerHeap.Len()
}

// ByteWorkHeap is a specialized work heap for byte slices
// This provides backward compatibility with the existing implementation
type ByteWorkHeap struct {
	*GenericWorkHeap[[]byte]
}

// NewByteWorkHeap creates a new byte-based work heap
func NewByteWorkHeap() *ByteWorkHeap {
	return &ByteWorkHeap{
		GenericWorkHeap: NewGenericWorkHeap[[]byte](bytes.Compare, bytes.Equal),
	}
}
