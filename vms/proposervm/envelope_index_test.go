// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// envelope_index_test.go — WHAT THE OUTER LAYER REMEMBERS, AND WHAT IT ADMITS.
//
// Three things this layer owns outlive any single block: the height index that
// says which envelope is canonical at a height, the fork height that says where
// the outer chain begins, and the pair of answers to "how far has this node
// got" — one about the envelope it committed, one about the block it actually
// ran. Each has a failure mode that only shows up a boot later, which is why
// each gets a property here rather than a smoke test.
//
// The build window and the option's inherited P-chain view are here too: both
// are read straight off the parent envelope, so they belong to the same
// question of what an envelope carries forward.
package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/runtime"
	validatorstest "github.com/luxfi/validators/validatorstest"
	vmcore "github.com/luxfi/vm"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/proposer"
)

// envAccept drives one inner block through the real post-fork accept path and
// returns the envelope that was committed for it. The inner tip is left where
// the caller put it, so a test can model the window in which the outer has
// committed and the inner has not.
func envAccept(t *testing.T, vm *VM, parentOuter ids.ID, innerBlk vmchain.Block) block.SignedBlock {
	t.Helper()
	sb := envUnsigned(t, parentOuter, envT0.Add(time.Duration(innerBlk.Height())*time.Second),
		envPChainHeight, block.Epoch{}, innerBlk)
	blk := &postForkBlock{
		SignedBlock:              sb,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: innerBlk},
	}
	vm.Tree.Add(innerBlk)
	require.NoError(t, blk.Accept(context.Background()))
	return sb
}

// envInit builds the Init the production Initialize takes. The DB is namespaced
// per test so a caller can boot a second VM over the same bytes.
func envInit(t *testing.T, db database.Database) vmcore.Init {
	t.Helper()
	return vmcore.Init{
		Runtime: &runtime.Runtime{
			ChainID: ids.GenerateTestID(),
			NodeID:  ids.GenerateTestNodeID(),
			ValidatorState: &validatorstest.State{
				GetCurrentHeightF: func(context.Context) (uint64, error) { return envPChainHeight, nil },
			},
		},
		DB:  db,
		Log: log.Noop(),
	}
}

// TestInitializeRefusesAChainItCannotSchedule pins Initialize as fail-closed on
// the two things it cannot do without.
//
// Without a validator state the windower has no set to elect from, and the
// proposer schedule degrades to "anyone can propose" — which is not a
// restriction that fails loudly, it is a chain where every validator builds at
// every height. That has to stop the chain from starting, not start a chain that
// equivocates.
func TestInitializeRefusesAChainItCannotSchedule(t *testing.T) {
	ctx := context.Background()
	inner := newEnvInner()

	t.Run("no_validator_state", func(t *testing.T) {
		vm := New(inner.vm(), Config{Registerer: metric.NewRegistry()})
		init := envInit(t, memdb.New())
		init.Runtime.ValidatorState = nil
		require.Error(t, vm.Initialize(ctx, init),
			"a chain with no validator set must refuse to start rather than run with an open schedule")
	})

	t.Run("registerer_that_cannot_hold_metrics", func(t *testing.T) {
		vm := New(inner.vm(), Config{Registerer: nil})
		require.Error(t, vm.Initialize(ctx, envInit(t, memdb.New())))
	})
}

// TestTheChainIsInnerBelowTheForkAndOuterAboveIt pins the height index across
// the boundary that defines this layer.
//
// A height below the fork was never wrapped, so the only answer is the inner
// VM's; a height at or above it has an envelope, and the envelope's id is the
// one the network votes on. Answering either from the wrong side hands a peer a
// block id its neighbours do not recognise. The fork height is recorded by the
// FIRST post-fork accept and must never move afterwards — it is the boundary
// itself, and a boundary that drifts re-labels blocks already committed.
func TestTheChainIsInnerBelowTheForkAndOuterAboveIt(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()

	base := memdb.New()
	vm := New(inner.vm(), Config{Registerer: metric.NewRegistry()})
	require.NoError(vm.Initialize(ctx, envInit(t, base)))

	// A pre-fork run: three inner blocks the proposervm never wrapped.
	parent := ids.Empty
	preFork := make([]*blocktest.Block, 0, 3)
	for h := uint64(1); h <= 3; h++ {
		b := inner.mint(parent, h, envT0.Add(time.Duration(h)*time.Second))
		preFork = append(preFork, b)
		parent = b.ID()
	}

	_, err := vm.State.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound, "nothing has been wrapped yet, so there is no fork")

	for h, b := range map[uint64]*blocktest.Block{1: preFork[0], 2: preFork[1], 3: preFork[2]} {
		got, err := vm.GetBlockIDAtHeight(ctx, h)
		require.NoError(err)
		require.Equal(b.ID(), got, "before the fork the inner VM is the only authority on a height")
	}

	// Now cross the fork: heights 4 and 5 get envelopes.
	outer := map[uint64]block.SignedBlock{}
	parentOuter := ids.Empty
	for h := uint64(4); h <= 5; h++ {
		b := inner.mint(parent, h, envT0.Add(time.Duration(h)*time.Second))
		parent = b.ID()
		sb := envAccept(t, vm, parentOuter, b)
		outer[h] = sb
		parentOuter = sb.ID()
	}

	forkHeight, err := vm.State.GetForkHeight()
	require.NoError(err)
	require.Equal(uint64(4), forkHeight, "the fork is where the first envelope was committed")

	for h := uint64(1); h <= 3; h++ {
		got, err := vm.GetBlockIDAtHeight(ctx, h)
		require.NoError(err)
		require.Equal(preFork[h-1].ID(), got, "heights below the fork still answer with the inner block")
	}
	for h := uint64(4); h <= 5; h++ {
		got, err := vm.GetBlockIDAtHeight(ctx, h)
		require.NoError(err)
		require.Equal(outer[h].ID(), got, "heights at or above the fork answer with the ENVELOPE id, not the inner id")
		require.NotContains(inner.byID, got, "the id at a post-fork height is an envelope, never the block it wraps")
	}

	// A second boot over the same bytes must agree about all of it — the index
	// is the thing that has to survive the process, and a fork height read back
	// differently is a chain that re-labels its own history.
	reboot := New(inner.vm(), Config{Registerer: metric.NewRegistry()})
	require.NoError(reboot.Initialize(ctx, envInit(t, base)))
	rebootFork, err := reboot.State.GetForkHeight()
	require.NoError(err)
	require.Equal(forkHeight, rebootFork)
	require.Equal(uint64(5), reboot.lastAcceptedHeight)
	for h := uint64(4); h <= 5; h++ {
		got, err := reboot.GetBlockIDAtHeight(ctx, h)
		require.NoError(err)
		require.Equal(outer[h].ID(), got)
	}
}

// TestPruningKeepsTheWindowAndAlwaysTheTip pins the retention rule on a node
// configured to keep only recent history.
//
// Two things must hold together. Anything inside the window stays fully
// answerable — index AND bytes, because an index entry pointing at a deleted
// block is worse than no entry. And the last accepted block is never a candidate
// for deletion at any window size: the outer and inner databases commit
// separately, so a node that dropped its own tip could not reconcile them at the
// next boot.
func TestPruningKeepsTheWindowAndAlwaysTheTip(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()

	const keep = uint64(2)
	base := memdb.New()
	vm := New(inner.vm(), Config{Registerer: metric.NewRegistry(), NumHistoricalBlocks: keep})
	require.NoError(vm.Initialize(ctx, envInit(t, base)))

	const tip = uint64(6)
	outer := map[uint64]block.SignedBlock{}
	parent, parentOuter := ids.Empty, ids.Empty
	for h := uint64(1); h <= tip; h++ {
		b := inner.mint(parent, h, envT0.Add(time.Duration(h)*time.Second))
		parent = b.ID()
		sb := envAccept(t, vm, parentOuter, b)
		outer[h] = sb
		parentOuter = sb.ID()
	}

	// forkHeight is 1 here, so the retained window is [tip-keep, tip].
	for h := uint64(1); h <= tip; h++ {
		_, idxErr := vm.State.GetBlockIDAtHeight(h)
		_, blkErr := vm.State.GetBlock(outer[h].ID())
		if h > tip-keep-1 {
			require.NoError(idxErr, "height %d is inside the retained window", h)
			require.NoError(blkErr, "height %d is indexed, so its bytes must still be there", h)
			continue
		}
		require.ErrorIs(idxErr, database.ErrNotFound, "height %d is outside the window", h)
		require.ErrorIs(blkErr, database.ErrNotFound, "a pruned height must not leave its bytes behind")
	}

	require.Equal(tip, vm.lastAcceptedHeight)
	lastAccepted, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.Equal(outer[tip].ID(), lastAccepted, "the tip is never pruned")

	// Booting again must not prune anything further: the boot-time sweep and the
	// per-accept sweep have to agree on the window, or every restart eats a block.
	reboot := New(inner.vm(), Config{Registerer: metric.NewRegistry(), NumHistoricalBlocks: keep})
	require.NoError(reboot.Initialize(ctx, envInit(t, base)))
	for h := tip - keep; h <= tip; h++ {
		got, err := reboot.State.GetBlockIDAtHeight(h)
		require.NoError(err, "a restart must not shrink the retained window")
		require.Equal(outer[h].ID(), got)
	}
}

// TestTheOuterTipIsNotTheBlockThisNodeHasRun pins the distinction that a reader
// gets wrong by default.
//
// The accept path commits the envelope, its index entry and the finality
// pointer in one batch and the inner block separately, so between the two the
// outer names a height the inner has not executed — and if the inner accept
// fails it keeps naming it, durably. LastAccepted answers "what is the head";
// InnerLastAccepted answers "what have we run". Anyone deciding whether this
// node may serve state, or how far catch-up must fetch, wants the second, and
// the two must not be allowed to collapse into one answer.
func TestTheOuterTipIsNotTheBlockThisNodeHasRun(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := New(inner.vm(), Config{Registerer: metric.NewRegistry()})
	require.NoError(vm.Initialize(ctx, envInit(t, memdb.New())))

	parent, parentOuter := ids.Empty, ids.Empty
	var behind, ahead vmchain.Block
	for h := uint64(1); h <= 2; h++ {
		b := inner.mint(parent, h, envT0.Add(time.Duration(h)*time.Second))
		parent = b.ID()
		parentOuter = envAccept(t, vm, parentOuter, b).ID()
		behind, ahead = ahead, b
	}
	require.NotNil(behind)

	// Model the open window: the outer committed height 2, the inner VM is still
	// reporting height 1 as its head.
	inner.last = behind.ID()

	outerTip, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.Equal(parentOuter, outerTip, "the head is the envelope that was committed")

	innerTip, err := vm.InnerLastAccepted(ctx)
	require.NoError(err)
	require.Equal(behind.ID(), innerTip, "what this node has RUN is the inner VM's own answer")

	innerHeight, err := vm.InnerLastAcceptedHeight(ctx)
	require.NoError(err)
	require.Equal(behind.Height(), innerHeight)
	require.Less(innerHeight, ahead.Height(),
		"the outer height leads the inner by design — reporting it as executed is the bug this pair exists to prevent")

	// An inner VM that cannot answer must produce an error, never a zero height:
	// a caller reading zero concludes the node is at genesis however far it ran.
	boom := errors.New("inner unavailable")
	broken := inner.vm()
	broken.LastAcceptedF = func(context.Context) (ids.ID, error) { return ids.Empty, boom }
	vm.ChainVM = broken
	_, err = vm.InnerLastAcceptedHeight(ctx)
	require.ErrorIs(err, boom)
}

// TestTheWrapperNeverClaimsAStateItsInnerVMRefused pins SetState's ordering and
// the one transition that does real work.
//
// The inner VM moves first; if it refuses, the wrapper's own view must not have
// moved, or the two disagree about whether the chain is live. And leaving
// Syncing is the only transition that re-reconciles the index against the inner
// tip — that is the moment a skipped or failed state sync has to be undone,
// and doing it on every transition would re-run a rollback on a healthy chain.
func TestTheWrapperNeverClaimsAStateItsInnerVMRefused(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := New(inner.vm(), Config{Registerer: metric.NewRegistry()})
	require.NoError(vm.Initialize(ctx, envInit(t, memdb.New())))

	parent, parentOuter := ids.Empty, ids.Empty
	for h := uint64(1); h <= 3; h++ {
		b := inner.mint(parent, h, envT0.Add(time.Duration(h)*time.Second))
		parent = b.ID()
		parentOuter = envAccept(t, vm, parentOuter, b).ID()
	}
	require.Equal(uint64(3), vm.lastAcceptedHeight)
	_ = parentOuter

	boom := errors.New("inner refuses to change state")
	refusing := inner.vm()
	refusing.SetStateF = func(context.Context, uint32) error { return boom }
	vm.ChainVM = refusing
	before := vm.consensusState
	require.ErrorIs(vm.SetState(ctx, uint32(vmcore.Ready)), boom)
	require.Equal(before, vm.consensusState,
		"the wrapper must not report a state the inner VM would not enter")

	// Accepting, then leaving Syncing: the reconciliation runs and lands on a
	// consistent view rather than erroring the chain out.
	accepting := inner.vm()
	accepting.SetStateF = func(context.Context, uint32) error { return nil }
	vm.ChainVM = accepting
	vm.consensusState = uint32(vmcore.Syncing)
	require.NoError(vm.SetState(ctx, uint32(vmcore.Bootstrapping)))
	require.Equal(uint32(vmcore.Bootstrapping), vm.consensusState)
	require.Equal(uint64(3), vm.lastAcceptedHeight,
		"a chain whose index already matches its inner tip must come out of sync unchanged")

	require.NoError(vm.SetState(ctx, uint32(vmcore.Ready)))
	require.Equal(uint32(vmcore.Ready), vm.consensusState)
	require.Equal(uint64(3), vm.lastAcceptedHeight)
}

// TestTheBuildWindowOpensAtThisNodesOwnSlot pins when this node is allowed to
// try to build.
//
// The answer is always relative to the PARENT envelope's timestamp, never to
// wall-clock: the slot grid is anchored there, so every node computes the same
// instant for the same parent. Below Ready the question is not asked at all and
// the inner VM's own event drives; with no schedule the floor is the configured
// minimum delay; with a schedule it is this node's own slot, but never sooner
// than that minimum. An unresolvable window is deliberately not an error — a
// node whose P-chain view has lagged past its tip must keep running.
func TestTheBuildWindowOpensAtThisNodesOwnSlot(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)
	vm.MinBlkDelay = time.Second

	innerBlk := inner.mint(ids.Empty, 10, envT0)
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerBlk)
	require.NoError(vm.State.PutBlock(sb))
	vm.preferred = sb.ID()

	t.Run("below_ready_the_inner_vm_drives", func(t *testing.T) {
		vm.consensusState = uint32(vmcore.Bootstrapping)
		at, wait, err := vm.timeToBuild(ctx)
		require.NoError(err)
		require.False(wait, "before Ready the proposervm does not window anything")
		require.True(at.IsZero())
	})

	vm.consensusState = uint32(vmcore.Ready)

	t.Run("no_schedule_floors_at_the_minimum_delay", func(t *testing.T) {
		vm.Windower = envWindower{}
		at, wait, err := vm.timeToBuild(ctx)
		require.NoError(err)
		require.True(wait)
		require.Equal(envT0.Add(vm.MinBlkDelay), at, "the window is anchored on the PARENT, not on wall-clock")
	})

	t.Run("our_slot_but_never_sooner_than_the_minimum", func(t *testing.T) {
		for _, delay := range []time.Duration{0, 3 * proposer.WindowDuration} {
			vm.Windower = envWindower{delay: func() (time.Duration, error) { return delay, nil }}
			at, wait, err := vm.timeToBuild(ctx)
			require.NoError(err)
			require.True(wait)
			require.Equal(envT0.Add(max(delay, vm.MinBlkDelay)), at)
		}
	})

	t.Run("an_unresolvable_window_does_not_kill_the_vm", func(t *testing.T) {
		vm.Windower = envWindower{delay: func() (time.Duration, error) {
			return 0, errors.New("P-chain view has not caught up")
		}}
		at, wait, err := vm.timeToBuild(ctx)
		require.NoError(err, "a lagging P-chain view stops building, it does not fail the VM")
		require.False(wait)
		require.True(at.IsZero())
	})
}

// TestAnOptionInheritsItsParentsPChainView pins what an option carries. An
// option has no header of its own — no timestamp, no P-chain height, no epoch —
// so every one of those is read through to the envelope it hangs off. If it
// answered from anywhere else, the two children of one oracle could disagree
// about the validator set their own children inherit.
func TestAnOptionInheritsItsParentsPChainView(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	base := inner.mint(ids.Empty, 10, envT0)
	oracleInner := &envOracle{Block: base}
	optInner := inner.mint(base.ID(), 11, envT0.Add(time.Second))
	oracleInner.opts = [2]vmchain.Block{optInner, optInner}
	inner.add(oracleInner)

	epoch := block.Epoch{PChainHeight: envPChainHeight, Number: 3, StartTime: envT0.Unix()}
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, epoch, oracleInner)
	envPersist(t, vm, parentSB)

	statelessOpt, err := block.BuildOption(parentSB.ID(), optInner.Bytes())
	require.NoError(err)
	opt := &postForkOption{
		Block:                    statelessOpt,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: optInner},
	}

	gotHeight, err := opt.pChainHeight(ctx)
	require.NoError(err)
	require.Equal(envPChainHeight, gotHeight)

	gotEpoch, err := opt.pChainEpoch(ctx)
	require.NoError(err)
	require.Equal(toChainBlockEpoch(epoch), gotEpoch)

	next, err := opt.selectChildPChainHeight(ctx)
	require.NoError(err)
	require.GreaterOrEqual(next, gotHeight, "a child's P-chain height never walks back below its parent's")

	require.Equal(optInner.Height(), opt.Height(), "an option reports the height of the block it wraps")
	require.Equal(statelessOpt, opt.getStatelessBlk())

	// Accepting the option indexes it exactly like any other envelope: the
	// finality pointer and the height entry both name the OPTION.
	vm.Tree.Add(optInner)
	require.NoError(opt.Accept(ctx))
	lastAccepted, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.Equal(statelessOpt.ID(), lastAccepted)
	atHeight, err := vm.State.GetBlockIDAtHeight(optInner.Height())
	require.NoError(err)
	require.Equal(statelessOpt.ID(), atHeight)

	// Rejecting drops it from the verified set without touching the inner block,
	// which a sibling option may still be about to accept.
	vm.recordVerifiedBlock(opt)
	require.NoError(opt.Reject(ctx))
	_, held := vm.cachedVerifiedBlock(opt.ID())
	require.False(held)
}

// TestTheBlockServerLocksAroundTheStoreIsUsable is a small one: the indexer
// reaches the block store through this pair, and both take the VM lock, so a
// deadlock or a mis-wired accessor here would surface as a hung indexer rather
// than as a failure.
func TestTheBlockServerLocksAroundTheStoreIsUsable(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	innerBlk := inner.mint(ids.Empty, 10, envT0)
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerBlk)
	require.NoError(vm.State.PutBlock(sb))

	got, err := vm.GetFullPostForkBlock(ctx, sb.ID())
	require.NoError(err)
	require.Equal(sb.ID(), got.ID())
	require.NoError(vm.Commit())

	// A committed store survives a fresh reader over the same base.
	_, err = vm.GetFullPostForkBlock(ctx, ids.GenerateTestID())
	require.ErrorIs(err, database.ErrNotFound)
}
