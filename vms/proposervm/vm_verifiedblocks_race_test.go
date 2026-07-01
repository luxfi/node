// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/luxfi/ids"
)

// idBlock is a minimal PostForkBlock whose only live method is ID(). The
// embedded nil PostForkBlock satisfies the rest of the (large) interface so the
// value is storable in the verified set; the race test never calls any other
// method on it.
type idBlock struct {
	PostForkBlock
	id ids.ID
}

func (b idBlock) ID() ids.ID { return b.id }

// TestVerifiedBlocksConcurrentAccess hammers the verified-block set from many
// goroutines: readers via getPostForkBlock + cachedVerifiedBlock run
// concurrently with writers via recordVerifiedBlock + forgetVerifiedBlock. This
// is the unit-level reproduction of the production crash, where a PullQuery/Put
// handler verifying a block (which writes verifiedBlocks) raced a Qbit handler
// reading the same map via GetBlock -> getPostForkBlock. On the pre-fix code
// the map was accessed without a lock, which the Go runtime aborts with
// "fatal error: concurrent map read and map write" (and which -race flags as a
// data race). With verifiedBlocksLock in place this is clean.
//
// Run with -race to guard the fix:
//
//	go test -race -run TestVerifiedBlocksConcurrentAccess ./vms/proposervm/
func TestVerifiedBlocksConcurrentAccess(t *testing.T) {
	vm := &VM{
		verifiedBlocks: make(map[ids.ID]PostForkBlock),
	}

	// A permanently-present key so getPostForkBlock always takes the fast map
	// path (a miss would dereference the nil vm.State). The read still races
	// against concurrent writes to other keys on the unlocked map.
	permanentID := ids.GenerateTestID()
	vm.verifiedBlocks[permanentID] = idBlock{id: permanentID}

	// Churn keys mutated by writers (disjoint from the permanent key).
	churn := make([]ids.ID, 64)
	for i := range churn {
		churn[i] = ids.GenerateTestID()
	}

	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	const readers, writers = 8, 8

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Real entry point, fast-path hit on the permanent key.
				if _, err := vm.getPostForkBlock(ctx, permanentID); err != nil {
					t.Errorf("getPostForkBlock(permanent): %v", err)
					return
				}
				for _, id := range churn {
					vm.cachedVerifiedBlock(id)
				}
			}
		}()
	}

	for i := 0; i < writers; i++ {
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
				id := churn[j%len(churn)]
				vm.recordVerifiedBlock(idBlock{id: id})
				vm.forgetVerifiedBlock(id)
				j++
			}
		}(i)
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
