// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// state_syncable_vm_test.go — the proposervm's state-summary envelope.
//
// A summary names a height the receiver will adopt WITHOUT executing anything
// below it, so the proposervm cannot simply forward the inner VM's summary: the
// receiver would land with an inner head and no outer envelope at that height,
// and every subsequent build would fail the parent check. The wrapper therefore
// carries three things — the fork height, the outer envelope, and the inner
// summary's own bytes — and the receiving node must be able to reconstruct all
// three from the bytes ALONE, holding no index and no blocks of its own. That
// round trip is what these tests pin.
//
// The shared harness (innerChain, testVM, acceptThroughProposervm) lives in
// height_lag_repro_test.go.
package proposervm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"
)

// syncable is an inner VM presented with both halves of its contract at once —
// blocks and summaries. The test doubles split them across two types; the
// proposervm asserts for the join, so the tests must supply it.
type syncable struct {
	*blocktest.VM
	*blocktest.StateSyncableVM
}

func syncableOver(ic *innerChain, ss *blocktest.StateSyncableVM) *syncable {
	return &syncable{VM: ic.vm(), StateSyncableVM: ss}
}

// TestSummaryRoundTripsThroughTheEnvelope is the whole point of the wrapper: a
// summary built by a node that HAS the index must reconstruct identically on a
// node that has nothing. The receiving node is a second, cold VM over its own
// empty database — no fork height, no envelopes, no height index — because that
// is the only state a state-syncing node is in when the bytes arrive.
func TestSummaryRoundTripsThroughTheEnvelope(t *testing.T) {
	require := require.New(t)

	const tip = 4
	ic := newInnerChain(t, tip)
	source := testVM(t, ic)
	acceptThroughProposervm(t, source, ic, tip)

	innerID := ids.GenerateTestID()
	innerBytes := []byte("inner summary at the tip")
	source.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
		GetLastStateSummaryF: func(context.Context) (vmchain.StateSummary, error) {
			return &blocktest.StateSummary{IDV: innerID, HeightV: tip, BytesV: innerBytes}, nil
		},
	})

	built, err := source.GetLastStateSummary(context.Background())
	require.NoError(err)
	// The height a summary reports is the INNER height — what the receiver will
	// have executed to — not the fork height the envelope also carries.
	require.Equal(uint64(tip), built.Height())

	var handedToInner []byte
	cold := testVM(t, ic)
	cold.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
		ParseStateSummaryF: func(_ context.Context, b []byte) (vmchain.StateSummary, error) {
			handedToInner = b
			return &blocktest.StateSummary{IDV: innerID, HeightV: tip, BytesV: b}, nil
		},
	})

	parsed, err := cold.ParseStateSummary(context.Background(), built.Bytes())
	require.NoError(err)

	// Identity is what the ratification vote is taken over: two nodes naming the
	// same summary must produce the same ID, or no summary can ever clear a
	// stake threshold.
	require.Equal(built.ID(), parsed.ID())
	require.Equal(built.Height(), parsed.Height())
	require.Equal(built.Bytes(), parsed.Bytes())

	// The inner VM's summary must survive the envelope byte-for-byte — the
	// wrapper is a carrier, and an inner VM that re-parses its own bytes cannot
	// tolerate a re-encode.
	require.Equal(innerBytes, handedToInner)

	// And the envelope the cold node recovered is the real one, at the real
	// height — this is what it will build on once the trie lands.
	recovered, err := cold.parsePostForkBlock(context.Background(), built.(*stateSummary).BlockBytes(), true)
	require.NoError(err)
	require.Equal(uint64(tip), recovered.Height())
	require.Equal(ic.byHeight[tip].ID(), recovered.getInnerBlk().ID())
}

// TestPreForkSummaryPassesThroughUnwrapped covers both ways a summary can sit
// below the proposervm: the fork was never reached, and the fork was reached
// above the summary. In neither case is there an envelope to carry, so wrapping
// would fabricate one.
func TestPreForkSummaryPassesThroughUnwrapped(t *testing.T) {
	t.Run("fork never reached", func(t *testing.T) {
		require := require.New(t)

		ic := newInnerChain(t, 3)
		vm := testVM(t, ic)

		inner := &blocktest.StateSummary{IDV: ids.GenerateTestID(), HeightV: 2, BytesV: []byte("inner")}
		vm.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
			GetLastStateSummaryF: func(context.Context) (vmchain.StateSummary, error) {
				return inner, nil
			},
		})

		got, err := vm.GetLastStateSummary(context.Background())
		require.NoError(err)
		require.Same(inner, got)
	})

	t.Run("summary below the fork height", func(t *testing.T) {
		require := require.New(t)

		ic := newInnerChain(t, 5)
		vm := testVM(t, ic)
		// The fork height is recorded at the FIRST post-fork accept, so starting
		// at 3 leaves heights 1 and 2 genuinely pre-fork.
		acceptRangeThroughProposervm(t, vm, ic, 3, 5, ids.Empty)
		require.Equal(uint64(3), mustForkHeight(t, vm))

		inner := &blocktest.StateSummary{IDV: ids.GenerateTestID(), HeightV: 2, BytesV: []byte("inner")}
		vm.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
			GetStateSummaryF: func(context.Context, uint64) (vmchain.StateSummary, error) {
				return inner, nil
			},
		})

		got, err := vm.GetStateSummary(context.Background(), 2)
		require.NoError(err)
		require.Same(inner, got)
	})
}

// TestSummaryAcceptMovesTheIndexForwardOnly pins the invariant the accept path
// exists to hold: the proposervm's index is never behind the inner VM's. A
// summary ahead of us drags the index up to it; a summary behind us must leave
// the index alone, because lowering it would strand every envelope above.
// Either way the inner VM decides — its Accept always runs and its mode is what
// the caller sees.
func TestSummaryAcceptMovesTheIndexForwardOnly(t *testing.T) {
	const tip = 5

	// summaryAt returns the bytes a node holding the full index would publish for
	// [height], as they would arrive over the wire.
	summaryAt := func(t *testing.T, ic *innerChain, height uint64) []byte {
		t.Helper()
		source := testVM(t, ic)
		acceptThroughProposervm(t, source, ic, tip)
		source.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
			GetStateSummaryF: func(_ context.Context, h uint64) (vmchain.StateSummary, error) {
				return &blocktest.StateSummary{IDV: ids.GenerateTestID(), HeightV: h, BytesV: []byte("inner")}, nil
			},
		})
		s, err := source.GetStateSummary(context.Background(), height)
		require.NoError(t, err)
		return s.Bytes()
	}

	t.Run("ahead of us", func(t *testing.T) {
		require := require.New(t)

		ic := newInnerChain(t, tip)
		bytes := summaryAt(t, ic, tip)

		// A node that has only reached height 2 — the ordinary state-sync case.
		target := testVM(t, ic)
		outers := acceptThroughProposervm(t, target, ic, 2)
		require.Equal(outers[2].ID(), mustLastAccepted(t, target))

		accepted := false
		target.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
			ParseStateSummaryF: func(_ context.Context, b []byte) (vmchain.StateSummary, error) {
				return &blocktest.StateSummary{
					HeightV: tip,
					BytesV:  b,
					AcceptF: func(context.Context) (vmchain.StateSyncMode, error) {
						accepted = true
						return vmchain.StateSyncStatic, nil
					},
				}, nil
			},
		})

		s, err := target.ParseStateSummary(context.Background(), bytes)
		require.NoError(err)

		mode, err := s.Accept(context.Background())
		require.NoError(err)
		require.Equal(vmchain.StateSyncStatic, mode)
		require.True(accepted)

		// The index jumped to the summary's height and now names the envelope
		// the receiver must build on.
		require.Equal(uint64(tip), target.lastAcceptedHeight)
		require.Equal(s.(*stateSummary).block.ID(), mustLastAccepted(t, target))
	})

	t.Run("behind us", func(t *testing.T) {
		require := require.New(t)

		ic := newInnerChain(t, tip)
		bytes := summaryAt(t, ic, 3)

		target := testVM(t, ic)
		outers := acceptThroughProposervm(t, target, ic, tip)

		accepted := false
		target.ssVM = syncableOver(ic, &blocktest.StateSyncableVM{
			ParseStateSummaryF: func(_ context.Context, b []byte) (vmchain.StateSummary, error) {
				return &blocktest.StateSummary{
					HeightV: 3,
					BytesV:  b,
					AcceptF: func(context.Context) (vmchain.StateSyncMode, error) {
						accepted = true
						return vmchain.StateSyncSkipped, nil
					},
				}, nil
			},
		})

		s, err := target.ParseStateSummary(context.Background(), bytes)
		require.NoError(err)

		mode, err := s.Accept(context.Background())
		require.NoError(err)
		require.Equal(vmchain.StateSyncSkipped, mode)
		require.True(accepted)

		// Untouched: still at the tip, still naming the tip's envelope.
		require.Equal(uint64(tip), target.lastAcceptedHeight)
		require.Equal(outers[tip].ID(), mustLastAccepted(t, target))
	})
}

// TestInnerWithoutStateSyncReportsNotImplemented — an inner VM that does not
// serve summaries must not be made to look like one that serves none. The first
// is a capability answer the caller can route around; the second is a claim
// about the chain.
func TestInnerWithoutStateSyncReportsNotImplemented(t *testing.T) {
	require := require.New(t)

	ic := newInnerChain(t, 2)
	vm := testVM(t, ic)
	require.Nil(vm.ssVM)

	ctx := context.Background()

	enabled, err := vm.StateSyncEnabled(ctx)
	require.NoError(err)
	require.False(enabled)

	_, err = vm.GetOngoingSyncStateSummary(ctx)
	require.ErrorIs(err, vmchain.ErrStateSyncableVMNotImplemented)

	_, err = vm.GetLastStateSummary(ctx)
	require.ErrorIs(err, vmchain.ErrStateSyncableVMNotImplemented)

	_, err = vm.GetStateSummary(ctx, 1)
	require.ErrorIs(err, vmchain.ErrStateSyncableVMNotImplemented)

	_, err = vm.ParseStateSummary(ctx, []byte("anything"))
	require.ErrorIs(err, vmchain.ErrStateSyncableVMNotImplemented)
}

// TestConstructorBindsTheInnerSummaryHalf — the summary half is reached through
// a type assertion made once, at construction. If that assertion is dropped the
// wrapper answers "no summaries" for every chain, including the ones that serve
// them, and nothing else in the tree notices.
func TestConstructorBindsTheInnerSummaryHalf(t *testing.T) {
	require := require.New(t)

	ic := newInnerChain(t, 2)

	plain := New(ic.vm(), Config{})
	require.Nil(plain.ssVM)

	enabled, err := plain.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	bound := New(syncableOver(ic, &blocktest.StateSyncableVM{
		StateSyncEnabledF: func(context.Context) (bool, error) {
			return true, nil
		},
	}), Config{})
	require.NotNil(bound.ssVM)

	enabled, err = bound.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)
}

func mustLastAccepted(t *testing.T, vm *VM) ids.ID {
	t.Helper()
	id, err := vm.State.GetLastAccepted()
	require.NoError(t, err)
	return id
}
