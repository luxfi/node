// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"sync"
	"testing"

	"github.com/luxfi/math/set"
)

// verifiedHeights is read and written from different goroutines.
//
// chainRouter.HandleInbound starts a goroutine per inbound message, so two
// blocks can be in VerifyWithRuntime at once holding the SAME *blockState —
// backend.blkIDToStateLock guards the map, not the struct it hands out. One
// goroutine is at block.go:47 doing Contains while another is at block.go:67
// doing Add, and set.Set is a bare map, so the Go runtime aborts the process:
//
//	fatal error: concurrent map read and map write
//	github.com/luxfi/math/set.Set[...].Contains(...) set/set.go:33
//	.../block/executor.(*Block).VerifyWithRuntime block.go:47
//
// That is a hard abort, not a panic — recover cannot see it. pars-mainnet
// parsd-mv-2 died this way 52 times in 59h.
func TestVerifiedHeightsConcurrentAccessIsSafe(t *testing.T) {
	bs := &blockState{verifiedHeights: set.Of[uint64](0)}

	var wg sync.WaitGroup
	for i := uint64(1); i <= 64; i++ {
		wg.Add(2)
		go func(h uint64) {
			defer wg.Done()
			bs.markVerifiedHeight(h)
		}(i)
		go func(h uint64) {
			defer wg.Done()
			_ = bs.hasVerifiedHeight(h)
		}(i)
	}
	wg.Wait()

	if !bs.hasVerifiedHeight(64) {
		t.Fatal("markVerifiedHeight did not record height 64")
	}
}
