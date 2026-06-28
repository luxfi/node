// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// transition_proposer_gate_test.go — focused unit tests for the CRITICAL-1 single-proposer GATE
// on the pre-fork → post-fork TRANSITION block (preForkBlock.buildChild) and the post-fork SIGNED
// build path (postForkCommonComponents.buildChild), which the giant ava VM harness — stripped in
// this fork — no longer covers.
//
// These do NOT rebuild that harness. They construct a minimal *VM directly (the same technique as
// vm_setpreference_context_test.go), inject a STUB Windower + the mockable Clock, and drive the
// real build functions. The gate is an OPTIMIZATION (fork-rate reduction), not a safety invariant —
// the consensus layer is sibling-tolerant — so these assert that the gate ELECTS one proposer per
// slot and that non-leaders DROP, and that the elected node produces a well-formed (unsigned)
// transition block. They deliberately do NOT assert "only one block can ever exist".
package proposervm

import (
	"context"
	"crypto"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/runtime"
	"github.com/luxfi/version"
	chain "github.com/luxfi/vm/chain"
)

// pfFork is a fixed, second-boundary parent timestamp so proposer.TimeToSlot is deterministic:
// slot S is reached by setting the clock to pfFork + S*proposer.WindowDuration.
var pfFork = time.Unix(1_700_000_000, 0)

// ----- minimal test doubles -------------------------------------------------

// pfTestBlock implements the inner chain.Block (= consensus engine/chain/block.Block).
type pfTestBlock struct {
	id, parent ids.ID
	height     uint64
	ts         time.Time
	bytes      []byte
}

func (b *pfTestBlock) ID() ids.ID                   { return b.id }
func (b *pfTestBlock) Parent() ids.ID               { return b.parent }
func (b *pfTestBlock) ParentID() ids.ID             { return b.parent }
func (b *pfTestBlock) Height() uint64               { return b.height }
func (b *pfTestBlock) Timestamp() time.Time         { return b.ts }
func (b *pfTestBlock) Status() uint8                { return 0 }
func (b *pfTestBlock) Verify(context.Context) error { return nil }
func (b *pfTestBlock) Accept(context.Context) error { return nil }
func (b *pfTestBlock) Reject(context.Context) error { return nil }
func (b *pfTestBlock) Bytes() []byte                { return b.bytes }

// pfInnerVM is a minimal block.ChainVM whose ONLY meaningful behavior is BuildBlock (the inner
// block the transition wraps). Every other method is inert — the build path never calls them.
type pfInnerVM struct {
	buildBlock func(context.Context) (cblock.Block, error)
}

func (vm *pfInnerVM) BuildBlock(ctx context.Context) (cblock.Block, error) {
	if vm.buildBlock != nil {
		return vm.buildBlock(ctx)
	}
	return nil, errors.New("pfInnerVM: BuildBlock not configured")
}
func (vm *pfInnerVM) Initialize(context.Context, cblock.Init) error { return nil }
func (vm *pfInnerVM) ParseBlock(context.Context, []byte) (cblock.Block, error) {
	return nil, errors.New("not implemented")
}
func (vm *pfInnerVM) GetBlock(context.Context, ids.ID) (cblock.Block, error) {
	return nil, errors.New("not implemented")
}
func (vm *pfInnerVM) Shutdown(context.Context) error                                    { return nil }
func (vm *pfInnerVM) NewHTTPHandler(context.Context) (http.Handler, error)              { return nil, nil }
func (vm *pfInnerVM) SetState(context.Context, uint32) error                            { return nil }
func (vm *pfInnerVM) Version(context.Context) (string, error)                           { return "", nil }
func (vm *pfInnerVM) Connected(context.Context, ids.NodeID, *version.Application) error { return nil }
func (vm *pfInnerVM) Disconnected(context.Context, ids.NodeID) error                    { return nil }
func (vm *pfInnerVM) HealthCheck(context.Context) (cblock.HealthCheckResult, error) {
	return cblock.HealthCheckResult{}, nil
}
func (vm *pfInnerVM) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	return ids.Empty, nil
}
func (vm *pfInnerVM) SetPreference(context.Context, ids.ID) error  { return nil }
func (vm *pfInnerVM) LastAccepted(context.Context) (ids.ID, error) { return ids.Empty, nil }
func (vm *pfInnerVM) WaitForEvent(context.Context) (cblock.Message, error) {
	return cblock.Message{}, nil
}

// pfStubWindower is a proposer.Windower whose ExpectedProposer is the gate's sole input. Only
// ExpectedProposer is exercised by the build path; the embedded interface (nil) covers the rest.
type pfStubWindower struct {
	proposer.Windower
	expected func(slot uint64) (ids.NodeID, error)
}

func (w pfStubWindower) ExpectedProposer(_ context.Context, _, _, slot uint64) (ids.NodeID, error) {
	return w.expected(slot)
}

// newTransitionVM builds a minimal proposervm.VM positioned at the pre-fork → post-fork boundary,
// with `self` as this node's identity, `win` driving the election, and a parent preForkBlock at
// `parentHeight`. The inner VM builds a child block at parentHeight+1.
func newTransitionVM(t *testing.T, self ids.NodeID, win proposer.Windower, parentHeight uint64) (*VM, *preForkBlock) {
	t.Helper()
	childInner := &pfTestBlock{
		id:     ids.GenerateTestID(),
		height: parentHeight + 1,
		ts:     pfFork,
		bytes:  []byte("inner-child"),
	}
	vm := &VM{
		ChainVM:        &pfInnerVM{buildBlock: func(context.Context) (cblock.Block, error) { return childInner, nil }},
		Windower:       win,
		validatorState: &mockValidatorState{},
		rt:             &runtime.Runtime{NodeID: self, ChainID: ids.GenerateTestID()},
		logger:         log.NewNoOpLogger(),
	}
	parentInner := &pfTestBlock{id: ids.GenerateTestID(), height: parentHeight, ts: pfFork, bytes: []byte("inner-parent")}
	return vm, &preForkBlock{Block: parentInner, vm: vm}
}

// ----- the gate: pre-fork → post-fork transition ----------------------------

// TestTransitionGate_SingleProposerElectedPerSlot is the core CRITICAL-1 assertion: over a
// 5-validator set, for EVERY slot exactly ONE node's buildChild produces the transition block and
// the other four DROP with errUnexpectedProposer. Iterating the slot also shows the elected builder
// ROTATES (the windower schedule advances), so the test simultaneously proves single-election and
// that the right to build is slot-driven.
func TestTransitionGate_SingleProposerElectedPerSlot(t *testing.T) {
	const n = 5
	vals := make([]ids.NodeID, n)
	for i := range vals {
		vals[i] = ids.GenerateTestNodeID()
	}
	win := pfStubWindower{expected: func(slot uint64) (ids.NodeID, error) {
		return vals[slot%uint64(n)], nil
	}}

	for slot := uint64(0); slot < n; slot++ {
		elected := vals[slot%uint64(n)]
		built, dropped := 0, 0
		for i, self := range vals {
			vm, parent := newTransitionVM(t, self, win, 7)
			vm.Clock.Set(pfFork.Add(time.Duration(slot) * proposer.WindowDuration))

			child, err := parent.buildChild(context.Background())
			if self == elected {
				require.NoError(t, err, "slot %d: the elected proposer (val %d) must build the transition", slot, i)
				pf, ok := child.(*postForkBlock)
				require.True(t, ok, "transition block must be a *postForkBlock")
				require.Equal(t, ids.EmptyNodeID, pf.SignedBlock.Proposer(),
					"the transition block MUST be UNSIGNED (verifyPostForkChild rejects a signed transition)")
				require.Equal(t, parent.ID(), pf.ParentID(), "transition's parent is the last pre-fork block")
				require.Equal(t, parent.Height()+1, pf.Height(), "transition is at parentHeight+1")
				built++
			} else {
				require.ErrorIs(t, err, errUnexpectedProposer, "slot %d: non-leader (val %d) must DROP", slot, i)
				require.Nil(t, child, "a dropped non-leader returns no block")
				dropped++
			}
		}
		require.Equal(t, 1, built, "slot %d: exactly ONE proposer builds the transition", slot)
		require.Equal(t, n-1, dropped, "slot %d: the other %d nodes drop", slot, n-1)
	}
}

// TestTransitionGate_SlotAdvanceFailsOver proves LIVENESS/FAILOVER: a single node's eligibility is
// driven by the wall-clock slot. The windower elects this node ONLY at slot 2; as the clock walks
// the slots the node DROPS until its slot arrives, then builds, then drops again — so a down leader
// does not stall the transition (the eligible set widens as the slot advances) and a node reliably
// gets its turn.
func TestTransitionGate_SlotAdvanceFailsOver(t *testing.T) {
	self := ids.GenerateTestNodeID()
	other := ids.GenerateTestNodeID()
	const mySlot = 2
	win := pfStubWindower{expected: func(slot uint64) (ids.NodeID, error) {
		if slot == mySlot {
			return self, nil
		}
		return other, nil
	}}

	for slot := uint64(0); slot <= 4; slot++ {
		vm, parent := newTransitionVM(t, self, win, 3)
		vm.Clock.Set(pfFork.Add(time.Duration(slot) * proposer.WindowDuration))

		child, err := parent.buildChild(context.Background())
		if slot == mySlot {
			require.NoError(t, err, "slot %d is OUR slot — we must build", slot)
			require.NotNil(t, child)
		} else {
			require.ErrorIs(t, err, errUnexpectedProposer, "slot %d is not our slot — we must drop and adopt the leader's block", slot)
			require.Nil(t, child)
		}
	}
}

// TestTransitionGate_AnyoneCanProposeFallsThrough covers the degenerate set (K==1 / windower not yet
// populated): ExpectedProposer returns proposer.ErrAnyoneCanPropose and the gate FALLS THROUGH to
// the legacy unsigned build — single-proposer cannot hold without a schedule, and the consensus
// per-height finality guard makes the residual equivocation survivable. ANY node builds.
func TestTransitionGate_AnyoneCanProposeFallsThrough(t *testing.T) {
	win := pfStubWindower{expected: func(uint64) (ids.NodeID, error) {
		return ids.EmptyNodeID, proposer.ErrAnyoneCanPropose
	}}
	// Two unrelated nodes: with no schedule, BOTH build (no election gates them).
	for _, self := range []ids.NodeID{ids.GenerateTestNodeID(), ids.GenerateTestNodeID()} {
		vm, parent := newTransitionVM(t, self, win, 0)
		vm.Clock.Set(pfFork)

		child, err := parent.buildChild(context.Background())
		require.NoError(t, err, "with ErrAnyoneCanPropose the gate falls through to the unsigned build")
		pf, ok := child.(*postForkBlock)
		require.True(t, ok)
		require.Equal(t, ids.EmptyNodeID, pf.SignedBlock.Proposer(), "still UNSIGNED on the fall-through path")
		require.Equal(t, parent.Height()+1, pf.Height())
	}
}

// TestTransitionGate_WindowerErrorPropagates covers the third gate branch: a windower error that is
// NOT ErrAnyoneCanPropose must propagate verbatim (the node cannot resolve the schedule, so it
// neither builds nor mislabels the failure as an election drop).
func TestTransitionGate_WindowerErrorPropagates(t *testing.T) {
	boom := errors.New("windower boom")
	win := pfStubWindower{expected: func(uint64) (ids.NodeID, error) {
		return ids.EmptyNodeID, boom
	}}
	vm, parent := newTransitionVM(t, ids.GenerateTestNodeID(), win, 0)
	vm.Clock.Set(pfFork)

	child, err := parent.buildChild(context.Background())
	require.ErrorIs(t, err, boom, "a non-ErrAnyoneCanPropose windower error must propagate")
	require.NotErrorIs(t, err, errUnexpectedProposer, "a resolution failure is NOT an election drop")
	require.Nil(t, child)
}

// ----- the post-fork SIGNED build path (staking signer wiring) --------------

// TestPostForkSignedBuild_UsesStakingSignerNoNilPanic proves the post-fork SIGNED build path works
// when the staking signer is present: the elected proposer produces a SIGNED block stamped with its
// own NodeID (derived from the staking cert), with NO nil-panic.
//
// This is the regression guard for the signer wiring. block.Build dereferences BOTH the leaf cert
// (cert.Raw) and the leaf signer (key.Sign) with no nil guard, so a nil StakingLeafSigner panics on
// exactly this path. The test pins the contract the node MUST satisfy: provide a non-nil
// StakingLeafSigner (and StakingCertLeaf) so an elected validator can sign post-fork blocks.
func TestPostForkSignedBuild_UsesStakingSignerNoNilPanic(t *testing.T) {
	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	leaf, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	signer, ok := tlsCert.PrivateKey.(crypto.Signer)
	require.True(t, ok, "staking TLS private key must be a crypto.Signer")
	// The windower elects the node whose cert signs (production identity coupling).
	nodeID := ids.NodeIDFromCert(leaf)

	const parentHeight = 12
	childInner := &pfTestBlock{id: ids.GenerateTestID(), height: parentHeight + 1, ts: pfFork, bytes: []byte("inner-child")}
	win := pfStubWindower{expected: func(uint64) (ids.NodeID, error) { return nodeID, nil }}

	vm := &VM{
		ChainVM:        &pfInnerVM{buildBlock: func(context.Context) (cblock.Block, error) { return childInner, nil }},
		Windower:       win,
		validatorState: &mockValidatorState{},
		rt:             &runtime.Runtime{NodeID: nodeID, ChainID: ids.GenerateTestID()},
		logger:         log.NewNoOpLogger(),
		Config: Config{
			StakingCertLeaf:   leaf,
			StakingLeafSigner: signer,
		},
	}
	vm.Clock.Set(pfFork)

	parentInner := &pfTestBlock{id: ids.GenerateTestID(), height: parentHeight, ts: pfFork, bytes: []byte("inner-parent")}
	p := &postForkCommonComponents{vm: vm, innerBlk: parentInner}
	parentID := ids.GenerateTestID()

	child, err := p.buildChild(context.Background(), parentID, pfFork, 0, chain.Epoch{})
	require.NoError(t, err, "elected post-fork build with a present signer must succeed without panicking")

	pf, ok := child.(*postForkBlock)
	require.True(t, ok)
	require.Equal(t, nodeID, pf.SignedBlock.Proposer(), "the signed block must carry this node's proposer identity")
	require.Equal(t, parentID, pf.ParentID())
	require.Equal(t, uint64(parentHeight+1), pf.Height())
}
