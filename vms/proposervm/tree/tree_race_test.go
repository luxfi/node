// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tree

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/vm/chain/blocktest"
)

// TestTreeConcurrentAccess hammers the Tree's node map from many goroutines:
// Add (writes the map) and Get (reads the map) run concurrently with Accept
// (deletes from the map). The proposervm drives Add/Get during Verify and
// Accept during commit from the consensus engine's handler goroutines, so the
// node map is genuinely accessed concurrently. On the pre-fix code the map was
// unlocked, which the Go runtime aborts with "fatal error: concurrent map read
// and map write" (and which -race flags as a data race). With the tree lock in
// place this is clean.
//
// Run with -race to guard the fix:
//
//	go test -race -run TestTreeConcurrentAccess ./vms/proposervm/tree/
func TestTreeConcurrentAccess(t *testing.T) {
	tr := New()
	ctx := context.Background()

	// Sibling blocks off the genesis: Add/Get all touch the same parent bucket.
	const n = 64
	blocks := make([]*blocktest.Block, n)
	for i := range blocks {
		blocks[i] = blocktest.BuildChild(blocktest.Genesis)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	const writers, readers = 8, 8

	// Writers: Add blocks (map writes).
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			j := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				tr.Add(blocks[j%n])
				j++
			}
		}(w)
	}

	// Readers: Get blocks (map reads).
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, b := range blocks {
					tr.Get(b)
				}
			}
		}()
	}

	// Accepter: repeatedly builds an isolated parent/child off the genesis, adds
	// the child, and accepts it — exercising Accept's top-level map insert and
	// delete against the concurrent Add/Get above. The subtree is unique each
	// iteration, so it never rejects the readers' blocks.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			parent := blocktest.BuildChild(blocktest.Genesis)
			child := blocktest.BuildChild(parent)
			tr.Add(child)
			if err := tr.Accept(ctx, child); err != nil {
				t.Errorf("Accept(child): %v", err)
				return
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
