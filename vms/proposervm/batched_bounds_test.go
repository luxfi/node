// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// batched_bounds_test.go — COUNTS THAT ARE NOT OURS.
//
// The batched surface takes two counts from outside the proposervm and uses
// both without checking them: the block budget a caller of GetAncestors names,
// and the length of the slice the inner VM returns from BatchedParseBlock. Each
// one sizes an allocation or an index directly. The tests here state what has to
// hold for a count you did not produce, and they FAIL — see the comments on each
// for what goes wrong and how far it reaches.
//
// The same file also pins the parts of the walk that are correct, so a later fix
// to the bounds is held to the behaviour that already works.
package proposervm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"

	"github.com/luxfi/node/vms/proposervm/block"
)

// TestGetAncestorsHonoursTheCountItWasGiven, FAILING.
//
// maxBlocksNum is the caller's ceiling on how many blocks come back. The walk
// appends first and tests the ceiling afterwards, so the ceiling is enforced one
// block late:
//
//   - a bound of 0 yields ONE block, over the caller's budget by a whole block;
//   - a negative bound reaches `make([][]byte, 0, maxBlocksNum)` and PANICS
//     ("makeslice: cap out of range") before the walk starts.
//
// Nothing in the node hands this method a count off the wire today — the ZAP
// server answers GetAncestors from vm.GetBlock and explicitly refuses a negative
// MaxBlocksNum before its own make, which is the same hazard already recognised
// one layer up. But this is the BatchedChainVM surface, the count arrives from
// whoever holds the interface, and a bound that is only checked after the work
// is done is not a bound. Testing the ceiling before the append fixes both rows.
func TestGetAncestorsHonoursTheCountItWasGiven(t *testing.T) {
	ctx := context.Background()
	inner := newEnvInner()
	vm, _ := newEnvBatchedVM(t, inner)

	a := inner.mint(ids.Empty, 10, envT0)
	b := inner.mint(a.ID(), 11, envT0.Add(time.Second))
	envA := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, a)
	envB := envUnsigned(t, envA.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{}, b)
	require.NoError(t, vm.State.PutBlock(envA))
	require.NoError(t, vm.State.PutBlock(envB))

	for _, count := range []int{-1, 0, 1, 2} {
		t.Run("bound_"+itoa(count), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("a bound of %d panicked before the walk began: %v", count, r)
				}
			}()
			got, err := vm.GetAncestors(ctx, envB.ID(), count, 1<<30, time.Minute)
			require.NoError(t, err)
			require.LessOrEqual(t, len(got), max(count, 0),
				"a bound of %d must not yield %d blocks", count, len(got))
		})
	}
}

// TestBatchedParseSurvivesAShortInnerResponse, FAILING.
//
// BatchedParseBlock splits its input at the first entry that is not an envelope,
// asks the inner VM to parse the inner halves in one call, and then rejoins the
// two by index: `innerBlks[innerBlocksIndex]` is read once per input entry with
// no check that the inner VM returned as many blocks as it was asked about. An
// inner VM that answers short — the shape of a truncated or partially decoded
// response from the out-of-process VM this talks to over ZAP — indexes past the
// end and takes the node down with "index out of range".
//
// The inner VM is our own plugin rather than a peer, so this is a robustness
// hole and not an attack, but a length that arrives from another process and
// lands straight in an index is the same shape either way: a slice returned by
// somebody else is a claim, and a panic is the most expensive way to find out it
// was wrong. `len(innerBlks) != len(innerBlockBytes)` is an error, not a crash.
func TestBatchedParseSurvivesAShortInnerResponse(t *testing.T) {
	ctx := context.Background()
	inner := newEnvInner()
	vm, batched := newEnvBatchedVM(t, inner)
	batched.short = true

	a := inner.mint(ids.Empty, 10, envT0)
	b := inner.mint(a.ID(), 11, envT0.Add(time.Second))
	envA := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, a)
	envB := envUnsigned(t, envA.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{}, b)

	for _, tt := range []struct {
		name  string
		batch [][]byte
	}{
		{name: "all_envelopes", batch: [][]byte{envA.Bytes(), envB.Bytes()}},
		{name: "envelope_then_pre_fork", batch: [][]byte{envA.Bytes(), b.Bytes()}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("an inner VM returning fewer blocks than it was asked about crashed the node: %v", r)
				}
			}()
			got, err := vm.BatchedParseBlock(ctx, tt.batch)
			if err != nil {
				return // an error is the right answer
			}
			require.Len(t, got, len(tt.batch),
				"a batch that cannot be answered in full must error, never return a partial batch")
		})
	}
}

// itoa keeps the subtest names readable without pulling strconv in for one call.
func itoa(i int) string {
	if i < 0 {
		return "neg" + itoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + itoa(i%10)
}
