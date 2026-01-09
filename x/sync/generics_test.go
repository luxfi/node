// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/vm/utils/maybe"
)

// TestGenericWorkItem tests the generic work item implementation
func TestGenericWorkItem(t *testing.T) {
	require := require.New(t)

	// Test with byte slices
	rootID := ids.GenerateTestID()
	start := maybe.Some([]byte{1, 2, 3})
	end := maybe.Some([]byte{4, 5, 6})

	item := &GenericWorkItem[[]byte]{
		LocalRootID: rootID,
		Start:       start,
		End:         end,
		Priority:    highPriority,
		QueueTime:   time.Now(),
	}
	require.NotNil(item)
	require.Equal(rootID, item.LocalRootID)
	require.True(item.Start.HasValue())
	require.True(item.End.HasValue())
	require.Equal(highPriority, item.Priority)

	// Test RequestFailed
	originalAttempt := item.Attempt
	item.RequestFailed()
	require.Equal(originalAttempt+1, item.Attempt)
}

// TestGenericWorkHeap tests the generic work heap implementation
func TestGenericWorkHeap(t *testing.T) {
	require := require.New(t)

	// Create a byte-based work heap using the constructor
	wh := NewGenericWorkHeap[[]byte](bytes.Compare, bytes.Equal)
	require.NotNil(wh)
	require.Equal(0, wh.Len())

	// Test Insert
	rootID := ids.GenerateTestID()
	item1 := &GenericWorkItem[[]byte]{
		LocalRootID: rootID,
		Start:       maybe.Some([]byte{1}),
		End:         maybe.Some([]byte{2}),
		Priority:    lowPriority,
		QueueTime:   time.Now(),
	}
	wh.Insert(item1)
	require.Equal(1, wh.Len())

	// Test GetWork - should return highest priority item
	item2 := &GenericWorkItem[[]byte]{
		LocalRootID: rootID,
		Start:       maybe.Some([]byte{3}),
		End:         maybe.Some([]byte{4}),
		Priority:    highPriority,
		QueueTime:   time.Now(),
	}
	wh.Insert(item2)
	require.Equal(2, wh.Len())

	// High priority item should be returned first
	work := wh.GetWork()
	require.NotNil(work)
	require.Equal(highPriority, work.Priority)
	require.Equal(1, wh.Len())

	// Low priority item should be returned next
	work = wh.GetWork()
	require.NotNil(work)
	require.Equal(lowPriority, work.Priority)
	require.Equal(0, wh.Len())

	// Empty heap should return nil
	work = wh.GetWork()
	require.Nil(work)
}

// TestGenericWorkHeapMerge tests merging functionality
func TestGenericWorkHeapMerge(t *testing.T) {
	require := require.New(t)

	wh := NewGenericWorkHeap[[]byte](bytes.Compare, bytes.Equal)
	rootID := ids.GenerateTestID()

	// Insert first item [1, 10]
	item1 := &GenericWorkItem[[]byte]{
		LocalRootID: rootID,
		Start:       maybe.Some([]byte{1}),
		End:         maybe.Some([]byte{10}),
		Priority:    lowPriority,
		QueueTime:   time.Now(),
	}
	wh.MergeInsert(item1)
	require.Equal(1, wh.Len())

	// Insert adjacent item [10, 20] - should merge
	item2 := &GenericWorkItem[[]byte]{
		LocalRootID: rootID,
		Start:       maybe.Some([]byte{10}),
		End:         maybe.Some([]byte{20}),
		Priority:    medPriority,
		QueueTime:   time.Now(),
	}
	wh.MergeInsert(item2)
	require.Equal(1, wh.Len()) // Should still be 1 after merge

	// Get the merged item
	merged := wh.GetWork()
	require.NotNil(merged)
	require.Equal([]byte{1}, merged.Start.Value())
	require.Equal([]byte{20}, merged.End.Value())
	require.Equal(medPriority, merged.Priority) // Should have highest priority
}

// TestCompatibilityLayer tests the byte-based heap wrapper
func TestCompatibilityLayer(t *testing.T) {
	require := require.New(t)

	// Test using NewByteWorkHeap
	heap := NewByteWorkHeap()
	require.NotNil(heap)
	require.Equal(0, heap.Len())

	rootID := ids.GenerateTestID()
	item := &GenericWorkItem[[]byte]{
		LocalRootID: rootID,
		Start:       maybe.Some([]byte{1}),
		End:         maybe.Some([]byte{2}),
		Priority:    highPriority,
		QueueTime:   time.Now(),
	}

	heap.Insert(item)
	require.Equal(1, heap.Len())

	work := heap.GetWork()
	require.NotNil(work)
	require.Equal(rootID, work.LocalRootID)
	require.Equal([]byte{1}, work.Start.Value())
	require.Equal([]byte{2}, work.End.Value())
}
