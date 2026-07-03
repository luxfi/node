// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"sync"
	"testing"

	validators "github.com/luxfi/validators"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/timer/mockable"
)

// emptyCache reports whether c is the no-op cache returned for chains the
// manager does not cache (untracked at startup).
func emptyCache(c validatorSetCacher) bool {
	_, ok := c.(*cache.Empty[uint64, map[ids.NodeID]*validators.GetValidatorOutput])
	return ok
}

// validatorSetCacher is the value type stored in manager.caches.
type validatorSetCacher = cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]

// TestGetValidatorSetCacheConcurrent hammers getValidatorSetCache from many
// goroutines across many chainIDs simultaneously. It is the regression guard
// for the unsynchronized map write on manager.caches that crashed nodes with
// "fatal error: concurrent map writes" during multi-validator restart storms
// (proposervm windower.ExpectedProposer -> GetValidatorSet -> getValidatorSetCache
// across multiple chains warming their caches at once).
//
// Run with -race. The test asserts two properties:
//  1. Race-freedom: no concurrent map read/write on manager.caches.
//  2. Correctness: a given chainID resolves to exactly one cache instance for
//     every caller (no lost insert — the check-then-create-then-store is
//     atomic), and that instance is the one persisted in manager.caches.
func TestGetValidatorSetCacheConcurrent(t *testing.T) {
	const (
		numChains     = 16
		numGoroutines = 64
		numIters      = 256
	)

	// All generated chains are tracked so they exercise the cached (racy)
	// branch. PrimaryNetworkID is always cached and is included to cover that
	// branch too.
	generated := make([]ids.ID, numChains)
	tracked := set.NewSet[ids.ID](numChains)
	for i := range generated {
		generated[i] = ids.GenerateTestID()
		tracked.Add(generated[i])
	}
	testChains := append([]ids.ID{constants.PrimaryNetworkID}, generated...)

	m := &manager{
		trackedChains: tracked,
		caches:        make(map[ids.ID]validatorSetCacher),
	}

	// results[g][c] holds the cache instance goroutine g last observed for
	// testChains[c]. Each goroutine writes only its own row (a distinct backing
	// array), so the grid is race-free without a lock; it is read only after all
	// goroutines join.
	results := make([][]validatorSetCacher, numGoroutines)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := 0; g < numGoroutines; g++ {
		results[g] = make([]validatorSetCacher, len(testChains))
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			// Release all goroutines simultaneously to maximize the first-access
			// collision window on each chain's cache creation.
			<-start
			for iter := 0; iter < numIters; iter++ {
				for c, chainID := range testChains {
					results[g][c] = m.getValidatorSetCache(chainID)
				}
			}
		}(g)
	}
	close(start)
	wg.Wait()

	// Correctness: for each chain, every goroutine must have observed the exact
	// same (non-nil) cache instance, and that instance must be the one stored in
	// manager.caches. A lost insert would surface here as a mismatch.
	for c, chainID := range testChains {
		want := results[0][c]
		if want == nil {
			t.Fatalf("chain %s (idx %d): got nil cache", chainID, c)
		}
		for g := 1; g < numGoroutines; g++ {
			if results[g][c] != want {
				t.Fatalf("chain %s (idx %d): goroutine %d observed a different cache instance (lost insert)", chainID, c, g)
			}
		}
		if stored := m.caches[chainID]; stored != want {
			t.Fatalf("chain %s (idx %d): manager.caches holds a different instance than callers observed", chainID, c)
		}
	}

	if len(m.caches) != len(testChains) {
		t.Fatalf("expected %d cache entries, got %d", len(testChains), len(m.caches))
	}
}

// TestGetValidatorSetCacheUntrackedReturnsEmpty locks in the behavior that
// untracked chains are never cached (they get a fresh cache.Empty), so the
// locking change does not alter which cache is returned.
func TestGetValidatorSetCacheUntrackedReturnsEmpty(t *testing.T) {
	m := &manager{
		trackedChains: set.NewSet[ids.ID](0),
		caches:        make(map[ids.ID]validatorSetCacher),
	}

	untracked := ids.GenerateTestID()
	got := m.getValidatorSetCache(untracked)
	if !emptyCache(got) {
		t.Fatalf("untracked chain: expected *cache.Empty, got %T", got)
	}
	if len(m.caches) != 0 {
		t.Fatalf("untracked chain must not be cached; caches has %d entries", len(m.caches))
	}
}

// TestGetValidatorSetCacheTrackedChainsSnapshot is the regression guard for
// Vector-4: getValidatorSetCache must not read the network's runtime-mutable
// TrackedChains set. In production cfg.TrackedChains aliases the same set.Set
// that network.TrackChain mutates (under peersLock) via the admin
// setTrackedChains RPC, so a lock-free Contains on it from the consensus hot
// path races that writer -> "concurrent map read and map write" -> exit(2).
//
// NewManager snapshots TrackedChains into a manager-owned immutable copy, so
// this test races getValidatorSetCache against Add on the ORIGINAL set (RED's
// repro shape) and requires it to be race-clean. It also pins the snapshot
// semantic: chains tracked at construction are cached; chains added to the
// original set afterwards are not reflected (they read as untracked/uncached).
//
// Run with -race.
func TestGetValidatorSetCacheTrackedChainsSnapshot(t *testing.T) {
	const (
		numReaders = 32
		numIters   = 4096
	)

	// live simulates the network's runtime-mutable TrackedChains instance that
	// cfg.TrackedChains aliases in production.
	live := set.NewSet[ids.ID](1)
	seed := ids.GenerateTestID() // tracked at construction time
	live.Add(seed)

	m := NewManager(
		config.Internal{TrackedChains: live},
		nil, // State: unused by getValidatorSetCache
		nil, // Metrics: unused by getValidatorSetCache
		&mockable.Clock{},
	).(*manager)

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Writer: continuously mutate the ORIGINAL live set, exactly as
	// network.TrackChain does. After the fix the manager never reads this set,
	// so this must not race the readers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < numIters; i++ {
			live.Add(ids.GenerateTestID())
		}
	}()

	// Readers: hammer the consensus hot path.
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < numIters; i++ {
				_ = m.getValidatorSetCache(seed)
				_ = m.getValidatorSetCache(constants.PrimaryNetworkID)
				_ = m.getValidatorSetCache(ids.GenerateTestID())
			}
		}()
	}

	close(start)
	wg.Wait()

	// Snapshot semantic: the chain tracked at construction is cached.
	if emptyCache(m.getValidatorSetCache(seed)) {
		t.Fatal("seed chain tracked at construction must be cached, got cache.Empty")
	}
	// A chain added to the original set AFTER construction is not reflected by
	// the immutable snapshot: it reads as untracked (uncached). This documents
	// the deliberate trade-off of decoupling from the runtime set.
	later := ids.GenerateTestID()
	live.Add(later)
	if !emptyCache(m.getValidatorSetCache(later)) {
		t.Fatal("chain added to the original set post-construction must not be tracked by the snapshot")
	}
}
