// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// build_inner_parent_test.go — the deterministic repro of the SELF-REJECTION LOOP
// measured on devnet 96367 and testnet 96368 on 2026-07-28:
//
//	info  built block   blkID=2srokbs… height=1047 innerBlkID=1DMGHAoo… pChainHeight=0
//	error built block failed verification — dropping
//	      error="inner parentID didn't match expected parent" height=1047 parentID=2V6mi4tX…
//
// 83–456 drops/min per node, forever, with the externally visible tip frozen two heights
// BELOW what the builder kept proposing. Every node rejected the block it had just built.
//
// The invariant being violated is postForkCommonComponents.Verify's third check
// (block.go, errInnerParentMismatch): a child's inner parent MUST be the inner block
// wrapped by the outer parent it extends. buildChild did not establish it — it asked the
// inner VM to build, and the inner VM builds on ITS OWN head. These tests model the inner
// VM with the luxfi/evm head semantics that make the two pointers diverge, and assert the
// invariant on the block buildChild actually returns.
package proposervm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	validatorstest "github.com/luxfi/validators/validatorstest"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/node/vms/proposervm/state"
)

// evmLikeInner models the inner execution VM's HEAD, which is the only part of the inner
// VM this defect involves. Three behaviours are copied from luxfi/evm:
//
//   - BuildBlock builds on the head            (miner/worker.go: parent = chain.CurrentBlock())
//   - verifyGossiped advances the head          (core/blockchain.go writeBlockAndSetHead →
//     newTip(block) → writeCanonicalBlockWithLogs → writeHeadBlock)
//   - SetPreference reorgs the head, and is a no-op when it already matches
//     (core/blockchain.go setPreference: early return on current.Hash() == block.Hash(),
//     otherwise writeKnownBlock)
//
// Nothing else is stubbed: the test drives the REAL postForkBlock.buildChild.
type evmLikeInner struct {
	vmchain.ChainVM
	mu           sync.Mutex                  // the inner VM's own head lock (luxfi/evm bc.chainmu)
	blocks       map[ids.ID]*blocktest.Block // every block inserted into the inner chain
	head         ids.ID
	setPrefCalls int
	reorgs       int

	// beforeBuild, when set, runs at the top of BuildBlock — i.e. INSIDE the window
	// between the anchor and the miner reading the head. It is the seam the race tests
	// use to observe (or attempt) head movement at exactly the moment that matters.
	beforeBuild func()
}

func newEVMLikeInner(genesis *blocktest.Block) *evmLikeInner {
	return &evmLikeInner{
		blocks: map[ids.ID]*blocktest.Block{genesis.IDV: genesis},
		head:   genesis.IDV,
	}
}

func (c *evmLikeInner) insert(b *blocktest.Block) { c.blocks[b.IDV] = b }

// child mints a block extending [parent] without touching the head.
func (c *evmLikeInner) child(parent *blocktest.Block) *blocktest.Block {
	id := ids.GenerateTestID()
	b := &blocktest.Block{
		IDV:        id,
		ParentV:    parent.IDV,
		HeightV:    parent.HeightV + 1,
		BytesV:     id[:],
		TimestampV: parent.TimestampV.Add(time.Second),
	}
	c.insert(b)
	return b
}

// verifyGossiped is the drift mechanism: verifying a peer's block whose parent is the
// current head OPTIMISTICALLY makes it the head — no accept, no proposervm involvement.
func (c *evmLikeInner) verifyGossiped(b *blocktest.Block) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.insert(b)
	if b.ParentV == c.head { // core/blockchain.go newTip
		c.head = b.IDV
	}
}

func (c *evmLikeInner) currentHead() ids.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.head
}

func (c *evmLikeInner) BuildBlock(context.Context) (vmchain.Block, error) {
	if c.beforeBuild != nil {
		c.beforeBuild()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.child(c.blocks[c.head]), nil
}

func (c *evmLikeInner) SetPreference(_ context.Context, id ids.ID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setPrefCalls++
	blk, ok := c.blocks[id]
	if !ok {
		return fmt.Errorf("evm-like inner VM: block %s not in chain", id)
	}
	if c.head == blk.IDV { // setPreference early return — already the head
		return nil
	}
	c.head = blk.IDV
	c.reorgs++
	return nil
}

func (c *evmLikeInner) GetBlock(_ context.Context, id ids.ID) (vmchain.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	blk, ok := c.blocks[id]
	if !ok {
		return nil, fmt.Errorf("evm-like inner VM: block %s not found", id)
	}
	return blk, nil
}

// anyoneCanProposeWindower makes every slot open, so buildChild takes the UNSIGNED path
// and needs no staking key. The proposer election is orthogonal to the parent invariant.
type anyoneCanProposeWindower struct{}

func (anyoneCanProposeWindower) Proposers(context.Context, uint64, uint64, int) ([]ids.NodeID, error) {
	return nil, proposer.ErrAnyoneCanPropose
}

func (anyoneCanProposeWindower) Delay(context.Context, uint64, uint64, ids.NodeID, int) (time.Duration, error) {
	return 0, proposer.ErrAnyoneCanPropose
}

func (anyoneCanProposeWindower) ExpectedProposer(context.Context, uint64, uint64, uint64) (ids.NodeID, error) {
	return ids.EmptyNodeID, proposer.ErrAnyoneCanPropose
}

func (anyoneCanProposeWindower) MinDelayForProposer(context.Context, uint64, uint64, ids.NodeID, uint64) (time.Duration, error) {
	return 0, proposer.ErrAnyoneCanPropose
}

// newBuildParentTestVM returns a proposervm whose outer PREFERRED envelope wraps
// innerParent, plus the inner chain, at the moment before a build attempt.
func newBuildParentTestVM(t *testing.T, innerParent *blocktest.Block, inner *evmLikeInner) (*VM, *postForkBlock) {
	t.Helper()

	vm := &VM{
		verifiedBlocks: map[ids.ID]PostForkBlock{},
		logger:         log.Noop(),
	}
	vm.State = state.New(versiondb.New(memdb.New()))
	vm.ChainVM = inner
	vm.Windower = anyoneCanProposeWindower{}
	vm.validatorState = &validatorstest.State{
		GetCurrentHeightF: func(context.Context) (uint64, error) { return 0, nil },
	}
	// Deterministic clock, one second after the parent envelope: slot 0, so the child
	// timestamp is not window-snapped and the parent check is the only thing under test.
	parentTimestamp := innerParent.TimestampV
	vm.Clock.Set(parentTimestamp.Add(time.Second))

	statelessParent, err := block.BuildUnsigned(
		ids.GenerateTestID(), // outer grandparent
		parentTimestamp,
		0, // pChainHeight — 0 fleet-wide, mainnet included; orthogonal to this defect
		block.Epoch{},
		innerParent.BytesV,
	)
	require.NoError(t, err)

	return vm, &postForkBlock{
		SignedBlock:              statelessParent,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: innerParent},
	}
}

// TestBuildChild_InnerHeadDriftedByGossipVerify_ChildExtendsOuterParentsInner is the
// repro. The node's outer preferred wraps inner height 1045 (the accepted tip); it then
// VERIFIES a gossiped inner 1046, which optimistically becomes the EVM head. The next
// build must still produce 1046-off-1045 — the block its own Verify will accept.
//
// BEFORE THE FIX buildChild delegated straight to the inner VM, which built off the
// drifted head: a child at height 1047 whose inner parent is 1046, while the outer parent
// wraps 1045 ⇒ errInnerParentMismatch on the node's own block, dropped, repeat forever.
// That is the live devnet signature exactly: built 1047 against an accepted tip of 1045.
func TestBuildChild_InnerHeadDriftedByGossipVerify_ChildExtendsOuterParentsInner(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := &blocktest.Block{
		IDV:        ids.GenerateTestID(),
		HeightV:    1044,
		BytesV:     []byte("1044"),
		TimestampV: time.Unix(1_785_216_000, 0),
	}
	inner := newEVMLikeInner(genesis)

	// The accepted inner tip, wrapped by this node's outer preferred envelope.
	acceptedTip := inner.child(genesis) // height 1045
	inner.head = acceptedTip.IDV

	_, outerParent := newBuildParentTestVM(t, acceptedTip, inner)

	// DRIFT: a gossiped inner block at 1046 verifies and optimistically takes the head.
	gossiped := inner.child(acceptedTip) // height 1046
	inner.verifyGossiped(gossiped)
	require.Equal(gossiped.IDV, inner.head, "precondition: the EVM head drifted off the accepted tip")

	built, err := outerParent.buildChild(ctx)
	require.NoError(err)

	child, ok := built.(*postForkBlock)
	require.True(ok)

	// THE INVARIANT — verbatim the condition postForkCommonComponents.Verify enforces
	// (expectedInnerParentID := p.innerBlk.ID(); innerParentID := child.innerBlk.Parent()).
	require.Equal(acceptedTip.ID(), child.innerBlk.Parent(),
		"the built child's inner parent must be the outer parent's inner block, or the node "+
			"rejects its own block with errInnerParentMismatch and drops it forever")
	require.Equal(acceptedTip.HeightV+1, child.Height(),
		"the child must be built one above the outer parent's inner block, not above the drifted head")

	// And the head was reorged back — the drift is repaired, not merely tolerated.
	require.Equal(acceptedTip.IDV, inner.head)
	require.Equal(1, inner.reorgs)
}

// TestBuildChild_HealthyHead_NoReorg pins the no-behaviour-change half: when the inner
// head already IS the outer parent's inner block (every healthy node, every block), the
// re-anchor is a no-op inside the inner VM — no reorg, no head movement.
func TestBuildChild_HealthyHead_NoReorg(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := &blocktest.Block{
		IDV:        ids.GenerateTestID(),
		HeightV:    100,
		BytesV:     []byte("100"),
		TimestampV: time.Unix(1_785_216_000, 0),
	}
	inner := newEVMLikeInner(genesis)
	acceptedTip := inner.child(genesis)
	inner.head = acceptedTip.IDV

	_, outerParent := newBuildParentTestVM(t, acceptedTip, inner)

	built, err := outerParent.buildChild(ctx)
	require.NoError(err)

	require.Equal(acceptedTip.ID(), built.(*postForkBlock).innerBlk.Parent())
	require.Equal(acceptedTip.IDV, inner.head, "head must not move on a healthy build")
	require.Zero(inner.reorgs, "no reorg on a healthy node — the inner SetPreference early-returns")
}

// TestBuildChild_PreForkTransition_ChildExtendsThisBlock covers the OTHER build
// delegation, preForkBlock.buildChild, which produces the pre-fork→post-fork transition
// block and is verified by preForkBlock.verifyPostForkChild's identical inner-parent check
// ("Make sure [b] is the parent of [child]'s inner block", pre_fork_block.go).
//
// At the fork height every validator may emit its own unsigned transition candidate, so a
// gossiped sibling verifying here is the norm, not an edge case — and it drifts the inner
// head exactly as on a live chain. Without the anchor the transition wedges at chain
// start, which is every fresh net's first two blocks.
func TestBuildChild_PreForkTransition_ChildExtendsThisBlock(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := &blocktest.Block{
		IDV:        ids.GenerateTestID(),
		HeightV:    0,
		BytesV:     []byte("genesis"),
		TimestampV: time.Unix(1_785_216_000, 0),
	}
	inner := newEVMLikeInner(genesis)

	vm, _ := newBuildParentTestVM(t, genesis, inner)
	preFork := &preForkBlock{Block: genesis, vm: vm}

	// A peer's competing transition candidate verifies and takes the inner head.
	sibling := inner.child(genesis)
	inner.verifyGossiped(sibling)
	require.Equal(sibling.IDV, inner.head)

	built, err := preFork.buildChild(ctx)
	require.NoError(err)

	require.Equal(genesis.ID(), built.(*postForkBlock).innerBlk.Parent(),
		"the transition block's inner parent must be the pre-fork block itself")
	require.Equal(genesis.HeightV+1, built.Height())
	require.Equal(genesis.IDV, inner.head)
}

// TestBuildChild_InnerParentNotInInnerChain_FailsInsteadOfBuildingADoomedBlock covers the
// error case: if the inner VM cannot anchor on the outer parent's inner block, then the
// head is provably NOT that block, so any child built would fail this node's own Verify.
// Returning the error is strictly better than emitting a block we will drop.
func TestBuildChild_InnerParentNotInInnerChain_FailsInsteadOfBuildingADoomedBlock(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	genesis := &blocktest.Block{
		IDV:        ids.GenerateTestID(),
		HeightV:    100,
		BytesV:     []byte("100"),
		TimestampV: time.Unix(1_785_216_000, 0),
	}
	inner := newEVMLikeInner(genesis)

	// An inner parent the inner VM does not hold (never inserted).
	orphanID := ids.GenerateTestID()
	orphan := &blocktest.Block{
		IDV:        orphanID,
		ParentV:    genesis.IDV,
		HeightV:    101,
		BytesV:     orphanID[:],
		TimestampV: genesis.TimestampV.Add(time.Second),
	}

	_, outerParent := newBuildParentTestVM(t, orphan, inner)

	_, err := outerParent.buildChild(ctx)
	require.ErrorContains(err, "failed to anchor inner build parent")
}
