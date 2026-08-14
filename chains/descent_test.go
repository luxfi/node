// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// descent_test.go — a walk toward our own tip must not be draggable backwards.
//
// A node whose finality has fallen behind asks its peers for context, and every peer
// answers. Each reply names the window that peer served, which is wherever that peer
// happened to look — not where the walk had got to. Adopting whichever reply arrived
// last means the highest reply wins most rounds, and a walk that had descended to
// within one window of the gap is thrown back above it before its request goes out.
//
// These tests pin the walk as strictly decreasing, which is what makes it terminate.

package chains

import (
	"sync"
	"testing"

	"github.com/luxfi/ids"
)

const window = 256 // what a responder serves per reply

func walker() *blockHandler { return &blockHandler{} }

// TestDescentOnlyGoesDown is the property itself.
func TestDescentOnlyGoesDown(t *testing.T) {
	b := walker()

	if !b.descend(ids.GenerateTestID(), 1_160_000) {
		t.Fatal("the first candidate must start the walk")
	}
	if !b.descend(ids.GenerateTestID(), 1_159_744) {
		t.Fatal("a lower candidate must move the walk")
	}
	if b.descend(ids.GenerateTestID(), 1_160_786) {
		t.Fatal("a reply from ABOVE the walk moved it back up — this is the mainnet stall")
	}
	if b.descend(ids.GenerateTestID(), 1_159_744) {
		t.Fatal("a candidate level with the walk is not progress and must be refused")
	}
	if b.descentHeight != 1_159_744 {
		t.Fatalf("walk stands at %d, want 1159744", b.descentHeight)
	}
}

// TestWalkOutlivesOneFoldedBlock is the cost of ending a walk too early. A node
// thousands of blocks behind folds ONE block out of the window the walk found; if
// that retires the walk, the next height pays another full descent, and finality
// creeps up one block per walk instead of one window per walk.
func TestWalkOutlivesOneFoldedBlock(t *testing.T) {
	b := walker()
	b.descend(ids.GenerateTestID(), 1_159_400) // the window holding our next height

	b.retire(1_159_170) // one block folded; still ~2000 short of the fleet

	if b.descentAnchor == ids.Empty {
		t.Fatal("one folded block ended the walk — the next height re-descends from the tip")
	}
	if b.descentHeight != 1_159_400 {
		t.Fatalf("walk moved to %d on a fold; it should be where it was", b.descentHeight)
	}
}

// TestWalkEndsWhenFinalityPassesIt: the other side. Once our finality is at or above
// the window, the walk is asking about blocks that are already ours, so it must let
// callers name their own blocks again.
func TestWalkEndsWhenFinalityPassesIt(t *testing.T) {
	b := walker()
	b.descend(ids.GenerateTestID(), 1_159_400)

	b.retire(1_159_399)
	if b.descentAnchor == ids.Empty {
		t.Fatal("walk retired one block early")
	}

	b.retire(1_159_400)
	if b.descentAnchor != ids.Empty {
		t.Fatal("finality reached the walk and it is still running — callers stay pinned to a stale anchor")
	}
	if !b.descend(ids.GenerateTestID(), 2_000_000) {
		t.Fatal("a retired walk must accept any candidate, including a higher one")
	}
}

// TestWalkReachesTheGap guards the FIX, not the bug: refusing replies is how the walk
// could be made to stall in the other direction, so something has to assert it still
// arrives. It passes with and without monotonicity — do not read it as evidence that
// last-writer-wins is what stalled mainnet. The two tests that fail without the fix
// are TestDescentOnlyGoesDown and TestConcurrentRepliesLeaveTheLowest.
//
// The load is the awkward one: four peers answering at staggered lags, so a reply that
// was in flight when the walk moved names a window from rounds ago, and the stalest
// lands last each round on top of the freshest.
//
// The gap is 8 windows; the cap is 200 rounds.
func TestWalkReachesTheGap(t *testing.T) {
	const (
		finalized = 1_159_169 // where mainnet sat
		tip       = 1_161_140 // where the certs were
	)

	b := walker()
	// Peer i answers the ask it received lags[i] rounds ago; peer 0 is prompt. The
	// stalest peer is delivered LAST each round, which is the interleaving that
	// hurts: the freshest reply is already recorded when the oldest one lands on
	// top of it. Ordering it explicitly makes the test decide the rule rather than
	// the scheduler; the concurrent case is TestConcurrentRepliesLeaveTheLowest.
	lags := []int{0, 1, 2, 3}
	starts := []uint64{tip, tip - 353, tip - 730, tip - 964}

	inflight := make([][]uint64, len(lags))
	for i := range inflight {
		inflight[i] = []uint64{starts[i]} // where each peer is looking to begin with
	}

	for step := 0; step < 200; step++ {
		for i := range inflight {
			if len(inflight[i]) <= lags[i] {
				continue // this peer's reply is still on the wire
			}
			served := inflight[i][0]
			inflight[i] = inflight[i][1:]
			// A responder serves the window ENDING at what it was asked for, so the
			// next block down is served-window.
			b.descend(ids.GenerateTestID(), served-window)
		}

		if b.descentHeight != 0 && b.descentHeight <= finalized+1 {
			return // the walk covers our next height; the fold can start
		}

		ask := b.descentHeight
		if ask == 0 {
			ask = tip
		}
		for i := range inflight {
			inflight[i] = append(inflight[i], ask)
		}
	}
	t.Fatalf("walk stalled at %d after 200 rounds, %d above our tip of %d — a stale reply is re-anchoring it every round",
		b.descentHeight, b.descentHeight-finalized, finalized)
}

// TestConcurrentRepliesLeaveTheLowest: the race itself. Whichever goroutine wins, the
// walk must hold the lowest candidate offered, never the last one to arrive.
func TestConcurrentRepliesLeaveTheLowest(t *testing.T) {
	b := walker()
	const lowest = 1_000_000

	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.descend(ids.GenerateTestID(), lowest+uint64(i))
		}(i)
	}
	wg.Wait()

	if b.descentHeight != lowest {
		t.Fatalf("walk stands at %d after 500 concurrent replies; the lowest offered was %d",
			b.descentHeight, lowest)
	}
}
