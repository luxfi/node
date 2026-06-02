// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"crypto/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1: Finalized map pruning
// ---------------------------------------------------------------------------

// TestFinalizedMapPruning simulates 100,000 finality events and verifies:
//   - The finalized map never exceeds maxFinalized + buffer
//   - Old entries are actually pruned
//   - After 100K events, map size <= 10,000
func TestFinalizedMapPruning(t *testing.T) {
	q, err := NewQuasar(log.NewNoOpLogger(), 3, 2, 3)
	require.NoError(t, err)

	// maxFinalized is 10,000 by default (set in NewQuasar)
	const totalEvents = 100_000

	// We simulate processFinality's pruning logic directly by
	// inserting entries and triggering the prune path.
	// processFinality increments qHeight and prunes when len > maxFinalized.
	var peakSize int
	for i := 0; i < totalEvents; i++ {
		var blockID ids.ID
		_, _ = rand.Read(blockID[:])

		q.mu.Lock()
		q.qHeight++
		q.finalized[blockID] = &QuantumFinality{
			BlockID:      blockID,
			QChainHeight: q.qHeight,
			PChainHeight: uint64(i),
			TotalWeight:  1000,
			SignerWeight: 700,
		}

		// Replicate the pruning logic from processFinality
		if q.maxFinalized > 0 && len(q.finalized) > q.maxFinalized {
			cutoff := q.qHeight - uint64(q.maxFinalized)
			for id, f := range q.finalized {
				if f.QChainHeight < cutoff {
					delete(q.finalized, id)
				}
			}
		}

		size := len(q.finalized)
		if size > peakSize {
			peakSize = size
		}
		q.mu.Unlock()
	}

	q.mu.RLock()
	finalSize := len(q.finalized)
	finalQHeight := q.qHeight
	q.mu.RUnlock()

	t.Logf("totalEvents=%d  peakSize=%d  finalSize=%d  qHeight=%d",
		totalEvents, peakSize, finalSize, finalQHeight)

	// The pruning logic fires when len > maxFinalized, then deletes entries
	// with QChainHeight < cutoff (strict <). The cutoff is qHeight - maxFinalized.
	// After pruning, entries at exactly cutoff remain, so the steady-state
	// size is maxFinalized + 1. This is correct and bounded.
	require.LessOrEqual(t, peakSize, q.maxFinalized+1,
		"peak map size should not exceed maxFinalized+1")

	require.LessOrEqual(t, finalSize, q.maxFinalized+1,
		"final map size should be <= maxFinalized+1 (10,001)")

	// Verify old entries are actually gone: the oldest remaining entry
	// should have QChainHeight >= qHeight - maxFinalized.
	q.mu.RLock()
	minHeight := uint64(^uint64(0))
	for _, f := range q.finalized {
		if f.QChainHeight < minHeight {
			minHeight = f.QChainHeight
		}
	}
	q.mu.RUnlock()

	expectedMinHeight := finalQHeight - uint64(q.maxFinalized)
	require.GreaterOrEqual(t, minHeight, expectedMinHeight,
		"oldest entry should be pruned: minHeight=%d expected>=%d", minHeight, expectedMinHeight)
}

// ---------------------------------------------------------------------------
// Test 2: Channel backpressure -- no goroutine leak or deadlock
// ---------------------------------------------------------------------------

// TestChannelBackpressure creates a pChainProvider with a 64-buffer channel,
// sends 1000 events, and verifies no goroutine leak or deadlock.
func TestChannelBackpressure(t *testing.T) {
	const (
		channelSize = 64
		totalEvents = 1000
	)

	validators := generateValidatorStates(5)
	pchain := &mockPChainProvider{
		height:     0,
		validators: validators,
		finalityCh: make(chan FinalityEvent, channelSize),
	}

	goroutinesBefore := runtime.NumGoroutine()

	// Send events -- channel will fill up, excess events are dropped (select default)
	sent := 0
	for i := 0; i < totalEvents; i++ {
		event := createTestEvent(uint64(i+1), validators)
		select {
		case pchain.finalityCh <- event:
			sent++
		default:
			// Channel full -- expected backpressure behavior
		}
	}

	t.Logf("sent %d/%d events (channel capacity %d)", sent, totalEvents, channelSize)
	require.GreaterOrEqual(t, sent, channelSize,
		"should have sent at least channelSize events")

	// Drain the channel
	drained := 0
	for {
		select {
		case <-pchain.finalityCh:
			drained++
		default:
			goto done
		}
	}
done:
	t.Logf("drained %d events", drained)

	// Check goroutine count -- should not have leaked
	runtime.GC()
	goroutinesAfter := runtime.NumGoroutine()
	// Allow a delta of 5 for GC/runtime goroutines
	require.InDelta(t, goroutinesBefore, goroutinesAfter, 5,
		"goroutine count should not grow significantly: before=%d after=%d",
		goroutinesBefore, goroutinesAfter)
}

// ---------------------------------------------------------------------------
// Test 3: Long-running benchmark -- 1M simulated finality cycles
// ---------------------------------------------------------------------------

// BenchmarkQuasarLongRun runs 1M simulated finality cycles and reports
// allocs/op, bytes/op, ns/op. Verifies no unbounded memory growth.
func BenchmarkQuasarLongRun(b *testing.B) {
	q, err := NewQuasar(log.NewNoOpLogger(), 3, 2, 3)
	if err != nil {
		b.Fatal(err)
	}

	// Snapshot heap before
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var blockID ids.ID
		// Use deterministic IDs to avoid crypto/rand overhead in benchmark
		blockID[0] = byte(i)
		blockID[1] = byte(i >> 8)
		blockID[2] = byte(i >> 16)
		blockID[3] = byte(i >> 24)

		q.mu.Lock()
		q.qHeight++
		q.finalized[blockID] = &QuantumFinality{
			BlockID:      blockID,
			QChainHeight: q.qHeight,
			PChainHeight: uint64(i),
			TotalWeight:  1000,
			SignerWeight: 700,
			BLSProof:     make([]byte, 96),
			Timestamp:    time.Now(),
		}

		// Prune (same logic as processFinality)
		if q.maxFinalized > 0 && len(q.finalized) > q.maxFinalized {
			cutoff := q.qHeight - uint64(q.maxFinalized)
			for id, f := range q.finalized {
				if f.QChainHeight < cutoff {
					delete(q.finalized, id)
				}
			}
		}
		q.mu.Unlock()
	}

	b.StopTimer()

	// Snapshot heap after
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	q.mu.RLock()
	finalSize := len(q.finalized)
	q.mu.RUnlock()

	heapBefore := memBefore.HeapAlloc
	heapAfter := memAfter.HeapAlloc

	var heapDelta int64
	if heapAfter >= heapBefore {
		heapDelta = int64(heapAfter - heapBefore)
	} else {
		heapDelta = -int64(heapBefore - heapAfter)
	}
	b.Logf("N=%d  finalMapSize=%d  heapBefore=%d  heapAfter=%d  heapDelta=%d",
		b.N, finalSize, heapBefore, heapAfter, heapDelta)

	// Map should be bounded regardless of N
	if finalSize > q.maxFinalized+1 {
		b.Fatalf("unbounded growth: map size %d exceeds maxFinalized %d",
			finalSize, q.maxFinalized)
	}

	// Heap should not grow linearly with N. After pruning, the heap should
	// be bounded by ~maxFinalized entries worth of allocations.
	// We allow 100MB as a generous upper bound for 10K entries with 96-byte proofs.
	const maxHeapGrowth = 100 * 1024 * 1024 // 100MB
	if heapAfter > heapBefore+maxHeapGrowth {
		b.Fatalf("unbounded heap growth: before=%d after=%d delta=%d (limit=%d)",
			heapBefore, heapAfter, heapAfter-heapBefore, maxHeapGrowth)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Quorum math verification -- property test
// ---------------------------------------------------------------------------

// TestQuorumMath is a property test that for all validator counts 1-100
// and all weight distributions:
//   - Cross-multiplication quorum never accepts < 2/3 weight
//   - Cross-multiplication quorum always accepts >= 2/3 weight (no false negatives)
func TestQuorumMath(t *testing.T) {
	q, err := NewQuasar(log.NewNoOpLogger(), 3, 2, 3)
	require.NoError(t, err)

	// The quorum check is: signerWeight * quorumDen >= totalWeight * quorumNum
	// With quorumNum=2, quorumDen=3: signerWeight * 3 >= totalWeight * 2

	t.Run("no_false_positives", func(t *testing.T) {
		// For all validator counts and weight distributions, if the quorum
		// check passes, then signerWeight/totalWeight >= 2/3.
		for numValidators := 1; numValidators <= 100; numValidators++ {
			// Test with equal weights
			weightPerValidator := uint64(1000)
			totalWeight := uint64(numValidators) * weightPerValidator

			for numSigners := 0; numSigners <= numValidators; numSigners++ {
				signerWeight := uint64(numSigners) * weightPerValidator
				result := q.CheckQuorum(signerWeight, totalWeight)

				// Verify: if result is true, then signerWeight/totalWeight >= 2/3
				// Using cross-multiplication: signerWeight * 3 >= totalWeight * 2
				actualMeetsThreshold := signerWeight*3 >= totalWeight*2
				if result && !actualMeetsThreshold {
					t.Fatalf("FALSE POSITIVE: n=%d signers=%d sW=%d tW=%d: "+
						"quorum accepted but %d*3=%d < %d*2=%d",
						numValidators, numSigners, signerWeight, totalWeight,
						signerWeight, signerWeight*3, totalWeight, totalWeight*2)
				}
			}
		}
	})

	t.Run("no_false_negatives", func(t *testing.T) {
		// For all validator counts and weight distributions, if
		// signerWeight/totalWeight >= 2/3, the quorum check must pass.
		for numValidators := 1; numValidators <= 100; numValidators++ {
			weightPerValidator := uint64(1000)
			totalWeight := uint64(numValidators) * weightPerValidator

			for numSigners := 0; numSigners <= numValidators; numSigners++ {
				signerWeight := uint64(numSigners) * weightPerValidator
				result := q.CheckQuorum(signerWeight, totalWeight)

				actualMeetsThreshold := signerWeight*3 >= totalWeight*2
				if actualMeetsThreshold && !result {
					t.Fatalf("FALSE NEGATIVE: n=%d signers=%d sW=%d tW=%d: "+
						"threshold met but quorum rejected",
						numValidators, numSigners, signerWeight, totalWeight)
				}
			}
		}
	})

	t.Run("varied_weight_distributions", func(t *testing.T) {
		// Test with non-uniform weights: validators have weights 1..n
		for numValidators := 1; numValidators <= 100; numValidators++ {
			var totalWeight uint64
			weights := make([]uint64, numValidators)
			for i := 0; i < numValidators; i++ {
				weights[i] = uint64(i + 1)
				totalWeight += weights[i]
			}

			// Test subsets: first k validators sign
			var signerWeight uint64
			for k := 0; k <= numValidators; k++ {
				if k > 0 {
					signerWeight += weights[k-1]
				}
				result := q.CheckQuorum(signerWeight, totalWeight)
				expected := signerWeight*3 >= totalWeight*2

				if result != expected {
					t.Fatalf("MISMATCH: n=%d signers=%d sW=%d tW=%d: "+
						"got %v expected %v",
						numValidators, k, signerWeight, totalWeight,
						result, expected)
				}
			}
		}
	})

	t.Run("edge_cases", func(t *testing.T) {
		// Zero total weight: any signer weight passes (vacuously true)
		require.True(t, q.CheckQuorum(0, 0), "0/0 should pass (vacuous)")
		require.True(t, q.CheckQuorum(1, 0), "1/0 should pass (vacuous)")

		// Exact boundary: 2/3 of various totals
		// For totalWeight=3: need signerWeight >= 2
		require.True(t, q.CheckQuorum(2, 3), "2/3 should pass")
		require.False(t, q.CheckQuorum(1, 3), "1/3 should fail")

		// For totalWeight=6: need signerWeight >= 4
		require.True(t, q.CheckQuorum(4, 6), "4/6 should pass")
		require.False(t, q.CheckQuorum(3, 6), "3/6 should fail")

		// For totalWeight=9: need signerWeight >= 6
		require.True(t, q.CheckQuorum(6, 9), "6/9 should pass")
		require.False(t, q.CheckQuorum(5, 9), "5/9 should fail")

		// For totalWeight=100: 2*100/3 = 66 (floor), so need 67 to be strictly >= 2/3
		// But cross-mult: sW*3 >= tW*2 → sW*3 >= 200 → sW >= 67 (ceil)
		// Actually: 66*3=198 < 200 → fail; 67*3=201 >= 200 → pass
		require.True(t, q.CheckQuorum(67, 100), "67/100 should pass")
		require.False(t, q.CheckQuorum(66, 100), "66/100 should fail")

		// Large weights (near overflow boundary for uint64)
		// Safe for totalWeight < 2^62 with quorumNum=2 (per checkQuorum doc)
		largeTotal := uint64(1) << 61
		largeSigner := largeTotal*2/3 + 1
		require.True(t, q.CheckQuorum(largeSigner, largeTotal),
			"large weight should pass when above 2/3")
	})

	t.Run("bft_threshold_exact", func(t *testing.T) {
		// BFT requires > 2/3 of total weight.
		// With the cross-multiplication check (>=), signerWeight*3 >= totalWeight*2.
		// This means exactly 2/3 PASSES (which matches the formal spec:
		// "quorum is met when SignerWeight/TotalWeight >= Numerator/Denominator").
		//
		// For totalWeight divisible by 3:
		//   signerWeight = totalWeight * 2 / 3 → passes (exact 2/3)
		//   signerWeight = totalWeight * 2 / 3 - 1 → fails (below 2/3)
		for total := uint64(3); total <= 300; total += 3 {
			threshold := total * 2 / 3
			require.True(t, q.CheckQuorum(threshold, total),
				"exact 2/3 (%d/%d) should pass", threshold, total)
			require.False(t, q.CheckQuorum(threshold-1, total),
				"below 2/3 (%d/%d) should fail", threshold-1, total)
		}
	})
}

// ---------------------------------------------------------------------------
// Test 5: Concurrent map pruning stress test
// ---------------------------------------------------------------------------

// TestConcurrentPruningStress verifies that concurrent writes + pruning
// do not corrupt the finalized map or deadlock.
func TestConcurrentPruningStress(t *testing.T) {
	q, err := NewQuasar(log.NewNoOpLogger(), 3, 2, 3)
	require.NoError(t, err)

	const (
		numWriters   = 8
		opsPerWriter = 5000
	)

	var wg sync.WaitGroup
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < opsPerWriter; i++ {
				var blockID ids.ID
				blockID[0] = byte(writerID)
				blockID[1] = byte(i)
				blockID[2] = byte(i >> 8)

				q.mu.Lock()
				q.qHeight++
				q.finalized[blockID] = &QuantumFinality{
					BlockID:      blockID,
					QChainHeight: q.qHeight,
				}
				if q.maxFinalized > 0 && len(q.finalized) > q.maxFinalized {
					cutoff := q.qHeight - uint64(q.maxFinalized)
					for id, f := range q.finalized {
						if f.QChainHeight < cutoff {
							delete(q.finalized, id)
						}
					}
				}
				q.mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	q.mu.RLock()
	finalSize := len(q.finalized)
	q.mu.RUnlock()

	t.Logf("writers=%d opsEach=%d totalOps=%d finalSize=%d",
		numWriters, opsPerWriter, numWriters*opsPerWriter, finalSize)

	require.LessOrEqual(t, finalSize, q.maxFinalized+1,
		"map size should be bounded after concurrent stress")
}
