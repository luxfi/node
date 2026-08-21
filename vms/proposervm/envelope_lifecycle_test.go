// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// envelope_lifecycle_test.go — WHAT AN ENVELOPE OWNS AND WHAT IT ONLY CARRIES.
//
// An envelope has a header of its own — parent, timestamp, P-chain height,
// epoch, proposer — and it borrows everything else from the block inside it:
// height, status, and every application-level signal. Getting that division
// wrong in the direction of "the envelope owns it" is what made 758 wrappers of
// one execution block look like 758 different chains. So the tests here pin the
// division itself, at the two moments it is load-bearing: when a wrapper is
// rejected, and when the node is asked what it would propose next.
//
// The pre-fork block gets the same treatment. It is the same object with no
// header at all, so every one of those questions has to fall through to the
// inner block, and its P-chain view has to read as absent rather than as zero
// that someone can build on.
package proposervm

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	validatorstest "github.com/luxfi/validators/validatorstest"
	vmcore "github.com/luxfi/vm"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/proposer"
)

// envSignals is an inner block that answers the application-level questions an
// envelope has no opinion of its own about.
type envSignals struct {
	*blocktest.Block
	bit   bool
	votes [][]byte
}

func (s *envSignals) EpochBit() bool     { return s.bit }
func (s *envSignals) FPCVotes() [][]byte { return s.votes }

// envEventful counts how often the inner VM is asked for work, which is the
// only way to see whether the build window actually held an event back.
type envEventful struct {
	*blocktest.VM
	calls int
	msg   vmcore.Message
}

func (e *envEventful) WaitForEvent(context.Context) (vmcore.Message, error) {
	e.calls++
	return e.msg, nil
}

// TestRejectingAWrapperLeavesTheBlockInsideItAlone is the accept-path half of
// the aliasing story.
//
// Post-fork, one execution block can be wrapped many times — the elected
// proposer re-wraps it each slot the chain is stuck. Consensus discards the
// wrappers it did not pick, and if discarding one rejected the block inside it,
// the winning wrapper would be accepting a block that has already been rejected.
// So a wrapper's Reject touches only the wrapper: it leaves the verified set and
// nothing else moves. The sibling then accepts normally, which is what this
// test measures.
func TestRejectingAWrapperLeavesTheBlockInsideItAlone(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	shared := inner.mint(ids.Empty, 10, envT0)

	// Two envelopes over the SAME inner block, differing only in their headers.
	loser := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, shared)
	winner := envUnsigned(t, ids.GenerateTestID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{}, shared)
	require.NotEqual(loser.ID(), winner.ID())

	wrap := func(sb block.SignedBlock) *postForkBlock {
		return &postForkBlock{
			SignedBlock:              sb,
			postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: shared},
		}
	}
	losing, winning := wrap(loser), wrap(winner)
	vm.recordVerifiedBlock(losing)
	vm.recordVerifiedBlock(winning)
	vm.Tree.Add(shared)

	require.NoError(losing.Reject(ctx))
	_, held := vm.cachedVerifiedBlock(losing.ID())
	require.False(held, "a rejected wrapper leaves the verified set")
	require.NotEqual(blocktest.Rejected, shared.Status(),
		"the block inside a rejected wrapper must survive — a sibling wrapper may still win")

	require.NoError(winning.Accept(ctx), "the surviving wrapper accepts the same inner block normally")
	require.Equal(blocktest.Accepted, shared.Status())
	_, held = vm.cachedVerifiedBlock(winning.ID())
	require.False(held, "an accepted wrapper leaves the verified set too")

	lastAccepted, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.Equal(winner.ID(), lastAccepted, "the finality pointer names the WRAPPER that won")
	require.Equal(shared.Height(), vm.lastAcceptedHeight)
}

// TestAnEnvelopeReportsTheBlockInsideIt pins the borrowed half of the header:
// height, status and the application-level signals are the inner block's
// answers, verbatim, through both wrapper types. An envelope that answered any
// of these itself would let two wrappers of one block disagree about the block.
func TestAnEnvelopeReportsTheBlockInsideIt(t *testing.T) {
	require := require.New(t)
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	base := inner.mint(ids.Empty, 41, envT0)
	base.StatusV = blocktest.Processing
	signals := &envSignals{Block: base, bit: true, votes: [][]byte{{1, 2, 3}}}
	inner.add(signals)

	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, signals)
	pfb := &postForkBlock{
		SignedBlock:              sb,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: signals},
	}
	statelessOpt, err := block.BuildOption(sb.ID(), signals.Bytes())
	require.NoError(err)
	pfo := &postForkOption{
		Block:                    statelessOpt,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: signals},
	}
	pre := &preForkBlock{Block: signals, vm: vm}

	for name, blk := range map[string]interface {
		Height() uint64
		Status() uint8
	}{"post_fork_block": pfb, "post_fork_option": pfo, "pre_fork_block": pre} {
		require.Equal(uint64(41), blk.Height(), "%s must report the inner height", name)
		require.Equal(blocktest.Processing, blk.Status(), "%s must report the inner status", name)
	}

	// The application-level signals forward through the block wrappers. The
	// OPTION does not carry them at all — it implements neither method — so a
	// caller probing for them gets "not supported" from an option and the inner
	// block's answer from an envelope over that same inner block. Nothing reads
	// these yet, so this is recorded rather than asserted against.
	for name, blk := range map[string]interface {
		EpochBit() bool
		FPCVotes() [][]byte
	}{"post_fork_block": pfb, "pre_fork_block": pre} {
		require.True(blk.EpochBit(), "%s must forward the inner epoch bit", name)
		require.Equal([][]byte{{1, 2, 3}}, blk.FPCVotes(), "%s must forward the inner votes", name)
	}
	_, optHasSignals := interface{}(pfo).(interface{ EpochBit() bool })
	require.False(optHasSignals,
		"an option carries no application signals; a change here would make the two wrappers of one block disagree")

	// An inner block with nothing to say produces the zero answers rather than a
	// panic on the missing methods.
	plain := inner.mint(ids.Empty, 1, envT0)
	quiet := &postForkBlock{postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: plain}}
	require.False(quiet.EpochBit())
	require.Nil(quiet.FPCVotes())
	quietPre := &preForkBlock{Block: plain, vm: vm}
	require.False(quietPre.EpochBit())
	require.Nil(quietPre.FPCVotes())
}

// TestAPreForkBlockCarriesNoPChainViewOfItsOwn pins the other end of the fork.
//
// A pre-fork block has no envelope, so it has no P-chain height and no epoch,
// and both have to read as absent — zero and the empty epoch — because that is
// what the transition block's epoch is derived from. Its timestamp is the inner
// block's, and its only legal child is a pre-fork one, and only when it is an
// oracle: after the fork, an ordinary pre-fork parent has no way to father
// anything, which is what forces the chain through the transition block.
func TestAPreForkBlockCarriesNoPChainViewOfItsOwn(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	base := inner.mint(ids.Empty, 10, envT0)
	pre := &preForkBlock{Block: base, vm: vm}

	height, err := pre.pChainHeight(ctx)
	require.NoError(err)
	require.Zero(height)
	epoch, err := pre.pChainEpoch(ctx)
	require.NoError(err)
	require.Equal(vmchain.Epoch{}, epoch, "an absent epoch, not an epoch numbered zero")
	require.True(base.Timestamp().Equal(pre.Timestamp()))

	// The proposed height for a pre-fork child is whatever the P-chain reports;
	// there is no parent floor to respect yet.
	next, err := pre.selectChildPChainHeight(ctx)
	require.NoError(err)
	require.Equal(envPChainHeight, next)

	// Verify resolves the parent through the inner VM and lets it judge.
	child := inner.mint(base.ID(), 11, envT0.Add(time.Second))
	require.ErrorIs((&preForkBlock{Block: child, vm: vm}).Verify(ctx), errUnexpectedBlockType,
		"a non-oracle pre-fork parent cannot father a pre-fork child")

	// An oracle pre-fork parent can, and its options are pre-fork blocks too.
	oracleBase := inner.mint(ids.Empty, 10, envT0)
	optA := inner.mint(oracleBase.ID(), 11, envT0.Add(time.Second))
	optB := inner.mint(oracleBase.ID(), 11, envT0.Add(time.Second))
	oracle := &envOracle{Block: oracleBase, opts: [2]vmchain.Block{optA, optB}}
	inner.add(oracle)

	oraclePre := &preForkBlock{Block: oracle, vm: vm}
	opts, err := oraclePre.Options(ctx)
	require.NoError(err)
	for i, want := range []vmchain.Block{optA, optB} {
		require.IsType(&preForkBlock{}, opts[i], "a pre-fork block's options are pre-fork blocks")
		require.Equal(want.ID(), opts[i].ID())
	}
	require.NoError(oraclePre.verifyPreForkChild(ctx, &preForkBlock{Block: optA, vm: vm}))

	// A plain block is not an oracle, and says so rather than pretending to be one.
	_, err = pre.Options(ctx)
	require.ErrorIs(err, errNotOracle)

	// A pre-fork block can never father an option — options belong to envelopes.
	require.ErrorIs(pre.verifyPostForkOption(ctx, nil), errUnexpectedBlockType)

	// Accepting a pre-fork block before the fork is a no-op on the outer index;
	// the inner block is what moves.
	require.NoError(pre.Accept(ctx))
	require.Equal(blocktest.Accepted, base.Status())
	require.Zero(vm.lastAcceptedHeight, "a pre-fork accept writes nothing to the outer index")
}

// TestTheProposedHeightNeverWalksBackFromThePreferredBlock pins the one number
// the RPC surface hands out.
//
// A child's P-chain height must be at least its parent's — Verify refuses
// anything lower — so "what would you propose next" has a floor at the preferred
// envelope's own height, whatever the P-chain currently reports. A node whose
// P-chain view has slipped behind its own tip must answer with the tip's height,
// not the stale one, or the block it then builds is one its neighbours refuse.
func TestTheProposedHeightNeverWalksBackFromThePreferredBlock(t *testing.T) {
	require := require.New(t)
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	innerBlk := inner.mint(ids.Empty, 10, envT0)
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerBlk)
	require.NoError(vm.State.PutBlock(sb))
	vm.preferred = sb.ID()

	handler, err := NewHTTPHandler(vm)
	require.NoError(err)
	require.NotNil(handler)

	svc := &Service{vm: vm}
	ask := func() uint64 {
		reply := &GetProposedHeightReply{}
		require.NoError(svc.GetProposedHeight(httptest.NewRequest("POST", "/", nil), &GetProposedHeightArgs{}, reply))
		return reply.ProposedHeight
	}

	require.Equal(envPChainHeight, ask(), "with the P-chain at the tip's height, that is the answer")

	atHeight := func(h uint64) {
		vm.validatorState = &validatorstest.State{
			GetCurrentHeightF: func(context.Context) (uint64, error) { return h, nil },
		}
	}

	// The P-chain has moved on: propose the newer height.
	atHeight(envPChainHeight + 5)
	require.Equal(envPChainHeight+5, ask())

	// The P-chain view has slipped BEHIND the preferred envelope: the floor holds.
	atHeight(envPChainHeight - 5)
	require.Equal(envPChainHeight, ask(),
		"a stale P-chain view must not produce a child that walks back below its parent")

	// An unreadable P-chain is an error, not a zero height that would build a
	// block every peer rejects.
	boom := errors.New("P-chain unreachable")
	vm.validatorState = &validatorstest.State{
		GetCurrentHeightF: func(context.Context) (uint64, error) { return 0, boom },
	}
	reply := &GetProposedHeightReply{}
	require.ErrorIs(svc.GetProposedHeight(httptest.NewRequest("POST", "/", nil), &GetProposedHeightArgs{}, reply), boom)

	// And an unheld preference is an error rather than an answer about a block
	// this node does not have.
	atHeight(envPChainHeight)
	vm.preferred = ids.GenerateTestID()
	require.Error(svc.GetProposedHeight(httptest.NewRequest("POST", "/", nil), &GetProposedHeightArgs{}, reply))
}

// TestWaitForEventDoesNotFireBeforeThisNodesSlot pins the gate between the
// inner VM's "I have work" and this node actually being allowed to build.
//
// The inner VM signals as soon as it has transactions, which on a busy chain is
// continuously. Forwarding that straight through would have every validator
// attempt a build every slot and drop all but the elected one's. So below Ready,
// and once the window is already open, the inner event passes; while the window
// is still ahead, nothing passes, and a cancelled context ends the wait rather
// than turning into an event.
func TestWaitForEventDoesNotFireBeforeThisNodesSlot(t *testing.T) {
	require := require.New(t)
	inner := newEnvInner()
	vm := newEnvVM(t, inner)
	vm.MinBlkDelay = time.Second

	pending := vmcore.Message{Type: vmcore.PendingTxs}
	eventful := &envEventful{VM: inner.vm(), msg: pending}
	vm.ChainVM = eventful

	innerBlk := inner.mint(ids.Empty, 10, envT0)
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerBlk)
	require.NoError(vm.State.PutBlock(sb))
	vm.preferred = sb.ID()

	t.Run("below_ready_the_inner_event_passes_through", func(t *testing.T) {
		vm.consensusState = uint32(vmcore.Bootstrapping)
		msg, err := vm.WaitForEvent(context.Background())
		require.NoError(err)
		require.Equal(pending, msg)
	})

	t.Run("an_open_window_passes_the_event", func(t *testing.T) {
		// The parent envelope sits far in the past, so this node's slot opened
		// long ago and there is nothing to wait for.
		vm.consensusState = uint32(vmcore.Ready)
		vm.Windower = envWindower{}
		msg, err := vm.WaitForEvent(context.Background())
		require.NoError(err)
		require.Equal(pending, msg)
	})

	t.Run("a_future_window_holds_the_event_back", func(t *testing.T) {
		// A parent stamped in the future puts this node's slot ahead of now.
		future := envUnsigned(t, ids.GenerateTestID(), time.Now().Add(time.Hour).Truncate(time.Second),
			envPChainHeight, block.Epoch{}, innerBlk)
		require.NoError(vm.State.PutBlock(future))
		vm.preferred = future.ID()
		vm.Clock.Set(time.Now())

		before := eventful.calls
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := vm.WaitForEvent(ctx)
		require.ErrorIs(err, context.DeadlineExceeded)
		require.Equal(before, eventful.calls, "the inner VM must not be asked for work before this node's slot")
	})

	t.Run("a_dead_context_never_reaches_the_inner_vm", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		before := eventful.calls
		_, err := vm.WaitForEvent(ctx)
		require.ErrorIs(err, context.Canceled)
		require.Equal(before, eventful.calls)
	})
}

// TestTheTransitionBuildIsWindowedToo pins the pre-fork half of the same gate.
//
// The transition block cannot be signed, so nothing in it identifies who built
// it — which means WHO builds it has to be settled by the schedule instead. A
// node whose slot has not arrived waits and adopts the leader's block; a chain
// with no schedule at all falls back to building immediately, because
// single-proposer cannot hold without an election and the per-height finality
// guard is what keeps that survivable.
func TestTheTransitionBuildIsWindowedToo(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)
	vm.MinBlkDelay = time.Second
	vm.consensusState = uint32(vmcore.Ready)

	// The preferred block is a bare inner block: the chain has not forked.
	last := inner.mint(ids.Empty, 10, envT0)
	vm.preferred = last.ID()

	t.Run("our_slot_sets_the_window", func(t *testing.T) {
		vm.Windower = envWindower{delay: func() (time.Duration, error) {
			return 4 * proposer.WindowDuration, nil
		}}
		at, wait, err := vm.timeToBuild(ctx)
		require.NoError(err)
		require.True(wait, "a non-leader waits its slot rather than forking the chain at its start")
		require.Equal(envT0.Add(4*proposer.WindowDuration), at)
	})

	t.Run("no_schedule_forwards_immediately", func(t *testing.T) {
		vm.Windower = envWindower{}
		_, wait, err := vm.timeToBuild(ctx)
		require.NoError(err)
		require.False(wait, "with no election to wait for, the legacy immediate build stands")
	})

	t.Run("an_unresolvable_window_forwards_rather_than_stalling", func(t *testing.T) {
		vm.Windower = envWindower{delay: func() (time.Duration, error) {
			return 0, errors.New("validator set unavailable")
		}}
		_, wait, err := vm.timeToBuild(ctx)
		require.NoError(err)
		require.False(wait)
	})

	t.Run("an_unheld_preference_forwards_rather_than_erroring", func(t *testing.T) {
		vm.preferred = ids.GenerateTestID()
		_, wait, err := vm.timeToBuild(ctx)
		require.NoError(err, "a preference neither layer holds must not fail the VM")
		require.False(wait)
	})
}

// TestGetBlockAnswersFromWhicheverLayerHoldsIt pins the resolution order the
// engine depends on: an envelope id resolves to the envelope, and only an id
// that is not an envelope falls through to the inner VM. Answering an envelope
// id from the inner VM is how a wrapped block ends up being accepted as an
// unwrapped one, which leaves the outer index behind the chain it indexes.
func TestGetBlockAnswersFromWhicheverLayerHoldsIt(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	innerBlk := inner.mint(ids.Empty, 10, envT0)
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerBlk)
	require.NoError(vm.State.PutBlock(sb))

	got, err := vm.GetBlock(ctx, sb.ID())
	require.NoError(err)
	require.IsType(&postForkBlock{}, got)
	require.Equal(sb.ID(), got.ID())

	// Before the fork, an inner id resolves to a pre-fork block.
	got, err = vm.GetBlock(ctx, innerBlk.ID())
	require.NoError(err)
	require.IsType(&preForkBlock{}, got)

	// After the fork, the same inner id must NOT come back as a pre-fork block:
	// its accept would move the inner VM and leave the index where it was.
	require.NoError(vm.State.SetForkHeight(innerBlk.Height()))
	_, err = vm.GetBlock(ctx, innerBlk.ID())
	require.ErrorIs(err, errPreForkAfterFork)

	// Below the recorded fork height the pre-fork construction is still correct.
	older := inner.mint(ids.Empty, innerBlk.Height()-1, envT0)
	got, err = vm.GetBlock(ctx, older.ID())
	require.NoError(err)
	require.IsType(&preForkBlock{}, got)

	_, err = vm.GetBlock(ctx, ids.GenerateTestID())
	require.Error(err, "an id no layer holds is an error, never an empty block")

	require.NoError(vm.Shutdown(ctx))
}
