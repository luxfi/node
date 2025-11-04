// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling
import (
	"github.com/luxfi/metric"
	"context"
	"testing"
	"time"
	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
)
// Test inboundMsgBufferThrottler
func TestMsgBufferThrottler(t *testing.T) {
	require := require.New(t)
	throttler, err := newInboundMsgBufferThrottler(metric.NewNoOp().Registry(), 3)
	require.NoError(err)
	nodeID1, nodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	// Acquire shouldn't block for first 3
	throttler.Acquire(context.Background(), nodeID1)
	require.Len(throttler.nodeToNumProcessingMsgs, 1)
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID1])
	// Acquire shouldn't block for other node
	throttler.Acquire(context.Background(), nodeID2)
	require.Len(throttler.nodeToNumProcessingMsgs, 2)
	require.Equal(uint64(3), throttler.nodeToNumProcessingMsgs[nodeID2])
	// Acquire should block for 4th acquire
	done := make(chan struct{})
	go func() {
		throttler.Acquire(context.Background(), nodeID1)
		done <- struct{}{}
	}()
	select {
	case <-done:
		require.FailNow("should block on acquiring")
	case <-time.After(50 * time.Millisecond):
	}
	throttler.release(nodeID1)
	// fourth acquire should be unblocked
	<-done
	// Releasing from other node should have no effect
	throttler.release(nodeID2)
	// Release remaining 3 acquires
	require.Empty(throttler.nodeToNumProcessingMsgs)
}
// Test inboundMsgBufferThrottler when an acquire is cancelled
func TestMsgBufferThrottlerContextCancelled(t *testing.T) {
	vdr1Context, vdr1ContextCancelFunc := context.WithCancel(context.Background())
	nodeID1 := ids.GenerateTestNodeID()
	throttler.Acquire(vdr1Context, nodeID1)
		throttler.Acquire(vdr1Context, nodeID1)
	// Acquire should block for 5th acquire
	done2 := make(chan struct{})
		done2 <- struct{}{}
	case <-done2:
	// Unblock fifth acquire
	vdr1ContextCancelFunc()
		require.FailNow("cancelling context should unblock Acquire")
		require.FailNow("should be blocked")
