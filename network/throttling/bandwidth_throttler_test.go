// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/metric"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

func TestBandwidthThrottler(t *testing.T) {
	require := require.New(t)
	// Assert initial state
	config := BandwidthThrottlerConfig{
		RefillRate:   8,
		MaxBurstSize: 10,
	}
	logger := log.NewNoOpLogger()
	throttlerIntf, err := newBandwidthThrottler(logger, metric.NewNoOpMetrics("test").Registry(), config)
	require.NoError(err)
	require.IsType(&bandwidthThrottlerImpl{}, throttlerIntf)
	throttler := throttlerIntf.(*bandwidthThrottlerImpl)
	require.NotNil(throttler.log)
	require.NotNil(throttler.limiters)
	require.Equal(config.RefillRate, throttler.RefillRate)
	require.Equal(config.MaxBurstSize, throttler.MaxBurstSize)
	require.Empty(throttler.limiters)

	// Add a node
	nodeID1 := ids.GenerateTestNodeID()
	throttler.AddNode(nodeID1)
	require.Len(throttler.limiters, 1)

	// Remove the node
	throttler.RemoveNode(nodeID1)
	require.Empty(throttler.limiters)

	// Add the node back
	throttler.AddNode(nodeID1)
	require.Len(throttler.limiters, 1)

	// Should be able to acquire 8
	throttler.Acquire(context.Background(), 8, nodeID1)

	// Make several goroutines that acquire bytes.
	wg := sync.WaitGroup{}
	wg.Add(int(config.MaxBurstSize) + 5)
	for i := uint64(0); i < config.MaxBurstSize+5; i++ {
		go func() {
			throttler.Acquire(context.Background(), 1, nodeID1)
			wg.Done()
		}()
	}
	wg.Wait()
}
