// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// build_inner_parent_race_test.go — the CONCURRENT half of the self-rejection loop.
//
// build_inner_parent_test.go proves buildChild anchors the inner VM on the outer parent's
// inner block. That anchor is a PRECONDITION established at T and consumed by the miner at
// T+Δ, where Δ is a full ZAP round-trip. These tests cover what keeps it true across Δ.
//
// It was not kept true. The consensus engine drives the proposervm from several goroutines
// (chains/manager.go PullQuery, chains/nova_poll.go applyPollResponse, and the engine's own
// gossip path all call Verify, while ForwardVMNotifications drives BuildBlock), the ZAP
// server dispatches every request on its own goroutine, and a first-time inner Verify of a
// block extending the head OPTIMISTICALLY takes the head. Nothing serialized the two, so a
// gossiped block landing inside the window made the miner extend the sibling and the node
// then rejected the block it had just built:
//
//	error built block failed verification — dropping
//	      error="inner parentID didn't match expected parent" height=2766
//
// measured live on lux-testnet, lux-devnet and hanzo-mainnet on 2026-08-05, on the exact
// build (v1.36.56) that already carried the anchor.
package proposervm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/tree"
)

func testGenesis(height uint64) *blocktest.Block {
	id := ids.GenerateTestID()
	return &blocktest.Block{
		IDV:        id,
		HeightV:    height,
		BytesV:     id[:],
		TimestampV: time.Unix(1_785_216_000, 0),
	}
}

// TestBuildChild_HoldsInnerHeadAcrossAnchorAndBuild asserts the property the fix exists to
// provide: the inner head is held EXCLUSIVELY from the anchor until the miner has read it.
//
// The assertion is made from inside the window itself — beforeBuild runs at the top of the
// inner VM's BuildBlock, which is precisely where a competing Verify would land — and it is
// deterministic: TryLock either succeeds or it does not, no timing involved. Before the fix
// the lock did not exist and TryLock would succeed, meaning any goroutine was free to reorg
// the head out from under the build.
func TestBuildChild_HoldsInnerHeadAcrossAnchorAndBuild(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := testGenesis(1044)
	inner := newEVMLikeInner(genesis)
	acceptedTip := inner.child(genesis)
	inner.head = acceptedTip.IDV

	vm, outerParent := newBuildParentTestVM(t, acceptedTip, inner)

	var observed bool
	inner.beforeBuild = func() {
		observed = true
		// If this succeeds, the build window is unguarded and a concurrent inner Verify
		// could move the head between the anchor and the miner's read of it.
		if vm.innerHead.TryLock() {
			vm.innerHead.Unlock()
			t.Error("innerHead was NOT held across anchor+build: a concurrent inner Verify " +
				"can reorg the head inside the window, and the node then rejects the block it built")
		}
	}

	built, err := outerParent.buildChild(ctx)
	require.NoError(err)
	require.True(observed, "beforeBuild must have run — otherwise this test asserts nothing")
	require.Equal(acceptedTip.ID(), built.(*postForkBlock).innerBlk.Parent())
}

// TestVerifyAndRecordInnerBlk_TakesInnerHead pins the OTHER side of the exclusion. A lock
// held only by the builder excludes nothing; the head-mover must take it too. Asserting it
// from inside the inner VM's Verify is what makes the pair actually mutually exclusive.
func TestVerifyAndRecordInnerBlk_TakesInnerHead(t *testing.T) {
	require := require.New(t)

	genesis := testGenesis(10)
	inner := newEVMLikeInner(genesis)
	vm, _ := newBuildParentTestVM(t, genesis, inner)

	vm.Tree = tree.New()

	gossiped := inner.child(genesis)
	var ran bool
	probe := &verifyProbeBlock{
		Block: gossiped,
		onVerify: func() {
			ran = true
			if vm.innerHead.TryLock() {
				vm.innerHead.Unlock()
				t.Error("verifyAndRecordInnerBlk did not hold innerHead across the inner Verify, " +
					"so it can move the head while a build is between its anchor and the miner's read")
			}
		},
	}

	statelessBlk, err := block.BuildUnsigned(
		ids.GenerateTestID(), // outer parent
		gossiped.TimestampV,
		0,
		block.Epoch{},
		gossiped.BytesV,
	)
	require.NoError(err)

	postFork := &postForkBlock{
		SignedBlock:              statelessBlk,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: probe},
	}

	require.NoError(vm.verifyAndRecordInnerBlk(context.Background(), nil, postFork))
	require.True(ran, "the probe's Verify must have run")
}

// verifyProbeBlock lets a test observe the lock state at the instant the inner VM's Verify
// runs — the moment the real EVM would call writeBlockAndSetHead and move the head.
type verifyProbeBlock struct {
	*blocktest.Block
	onVerify func()
}

func (b *verifyProbeBlock) Verify(context.Context) error {
	b.onVerify()
	return nil
}

// TestBuildChild_HeadDriftsInsideTheWindow_FailsClosed covers everything the lock does not:
// any residual path that moves the head between the anchor and the miner's read. buildChild
// must refuse rather than emit a block this node's own Verify is guaranteed to reject —
// emitting it is what produced "built block failed verification — dropping" forever, with
// the tip frozen below the height the builder kept proposing.
//
// The drift is injected on the BUILDING goroutine, so it bypasses the lock by construction:
// this asserts the fail-closed guarantee in isolation, not the lock.
func TestBuildChild_HeadDriftsInsideTheWindow_FailsClosed(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := testGenesis(2764)
	inner := newEVMLikeInner(genesis)
	acceptedTip := inner.child(genesis) // 2765 — what the outer parent wraps
	inner.head = acceptedTip.IDV

	_, outerParent := newBuildParentTestVM(t, acceptedTip, inner)

	inner.beforeBuild = func() {
		// A sibling at 2766 takes the head after the anchor ran. The miner will now
		// build 2767-off-2766, while Verify expects 2766-off-2765.
		inner.verifyGossiped(inner.child(acceptedTip))
	}

	built, err := outerParent.buildChild(ctx)
	require.ErrorIs(err, errInnerParentMismatch,
		"a build off a drifted head must fail at the build site, not be emitted and dropped")
	require.Nil(built)
}

// TestBuildChild_ConcurrentGossipVerify_NeverEmitsADoomedBlock is the stress form, and the
// one that reproduces the live failure mode: builds and gossip verifies racing on the same
// inner head, exactly as the engine drives them. Run under -race it also covers the data
// race on the head itself.
//
// The assertion is the invariant, not a count: every block buildChild RETURNS must satisfy
// the check its own Verify applies. A build that fails is fine — the engine re-triggers and
// the next one re-anchors. A build that SUCCEEDS with a wrong inner parent is the defect.
func TestBuildChild_ConcurrentGossipVerify_NeverEmitsADoomedBlock(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := testGenesis(500)
	inner := newEVMLikeInner(genesis)
	acceptedTip := inner.child(genesis)
	inner.head = acceptedTip.IDV

	vm, outerParent := newBuildParentTestVM(t, acceptedTip, inner)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		built     int
		refused   int
		violation error
	)

	// Gossip: goroutines that verify siblings of the accepted tip, each of which would
	// take the head if it landed while the head is the tip. They take innerHead because
	// that is what verifyAndRecordInnerBlk does.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				sibling := &blocktest.Block{
					IDV:        ids.GenerateTestID(),
					ParentV:    acceptedTip.IDV,
					HeightV:    acceptedTip.HeightV + 1,
					TimestampV: acceptedTip.TimestampV.Add(time.Second),
				}
				sibling.BytesV = sibling.IDV[:]
				vm.innerHead.Lock()
				inner.verifyGossiped(sibling)
				vm.innerHead.Unlock()
			}
		}()
	}

	// Builders, driven concurrently exactly as ForwardVMNotifications drives BuildBlock.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				blk, err := outerParent.buildChild(ctx)
				mu.Lock()
				switch {
				case err != nil:
					refused++
					if !errors.Is(err, errInnerParentMismatch) {
						violation = err
					}
				case blk.(*postForkBlock).innerBlk.Parent() != acceptedTip.ID():
					violation = errors.New("buildChild returned a block whose inner parent is not " +
						"the outer parent's inner block — this node's own Verify will reject it")
				default:
					built++
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	require.NoError(violation)
	require.Positive(built, "no build ever succeeded — the test would pass vacuously")
	t.Logf("built=%d refused=%d (refusals are safe: the engine re-triggers and re-anchors)", built, refused)
}
