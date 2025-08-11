// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"sync"
	"testing"
	"time"
	
	"github.com/stretchr/testify/require"


	"github.com/luxfi/node/consensus/networking/benchlist"

	"github.com/luxfi/ids"

	"github.com/luxfi/node/utils/timer"

	"github.com/luxfi/node/utils/metrics"
)

func TestManagerFire(t *testing.T) {
	benchlist := benchlist.NewNoBenchlist()
	manager, err := NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutCoefficient: 1.25,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist,
		metrics.NewTestRegistry(),
		metrics.NewTestRegistry(),
	)
	require.NoError(t, err)
	go manager.Dispatch()
	defer manager.Stop()

	wg := sync.WaitGroup{}
	wg.Add(1)

	manager.RegisterRequest(
		ids.EmptyNodeID,
		ids.Empty,
		true,
		ids.RequestID{},
		wg.Done,
	)

	wg.Wait()
}
