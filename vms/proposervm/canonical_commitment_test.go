// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// canonical_commitment_test.go — proves the proposervm block exposes the INNER
// execution commitment to the consensus engine (Part A of the mainnet 1082879
// duplicate-wrapper-storm fix).
//
// THE BUG THIS GUARDS AGAINST. The consensus engine's finality object is the
// CanonicalID (VotePosition.CanonicalID; the signed accept vote binds it and
// EXCLUDES the outer envelope id). The engine reads it by asserting a VM block to a
// structural canonicalCommitter interface (engine/chain/engine.go canonicalIDOf);
// when the assertion FAILS it falls back to the block's OUTER id. Before Part A NO
// block type anywhere implemented that interface, so every one of the 758 outer
// wrappers of the single inner EVM block 25Q837Lw got a DISTINCT canonical (its own
// outer id), votes scattered across them, no α-of-K cert formed, and the C-Chain
// froze at 1082879. These tests fail-closed if the proposervm block ever stops
// exposing the inner id.
package proposervm

import (
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/vm/chain/blocktest"
)

// canonicalCommitter mirrors the exact (unexported) structural interface the
// consensus engine's canonicalIDOf asserts. If *postForkBlock / *postForkOption
// stop satisfying this shape, the engine silently falls back to the outer id and
// the duplicate-wrapper storm can no longer collapse — so pin it at compile time.
type canonicalCommitter interface {
	CanonicalID() ids.ID
	ParentCanonicalID() ids.ID
	ExecutionStateRoot() ids.ID
	PayloadRoot() ids.ID
}

var (
	_ canonicalCommitter = (*postForkBlock)(nil)
	_ canonicalCommitter = (*postForkOption)(nil)
)

// TestCanonicalCommitter_ExposesInnerID proves the wrapper's CanonicalID is the
// INNER block's id (not the outer envelope id) and ParentCanonicalID is the inner
// PARENT's id — the values that let the engine collapse aliases and a forked parent.
func TestCanonicalCommitter_ExposesInnerID(t *testing.T) {
	innerID := ids.GenerateTestID()
	innerParent := ids.GenerateTestID()
	inner := &blocktest.Block{IDV: innerID, ParentV: innerParent}
	pfb := &postForkBlock{postForkCommonComponents: postForkCommonComponents{innerBlk: inner}}

	if got := pfb.CanonicalID(); got != innerID {
		t.Fatalf("CanonicalID = %s, want inner id %s (engine would fall back to the outer id → storm)", got, innerID)
	}
	if got := pfb.ParentCanonicalID(); got != innerParent {
		t.Fatalf("ParentCanonicalID = %s, want inner parent id %s (forked parent would not collapse)", got, innerParent)
	}
	if pfb.ExecutionStateRoot() != ids.Empty || pfb.PayloadRoot() != ids.Empty {
		t.Fatal("exec/payload roots must be Empty (not exposed) to keep the signed message byte-compatible")
	}
}

// TestCanonicalCommitter_CollapsesAliasesNotForks proves the core invariant: two
// DISTINCT outer wrappers of the SAME inner block share a CanonicalID (they collapse
// to one consensus object), while wrappers of DIFFERENT inner blocks keep DISTINCT
// CanonicalIDs (a genuine execution fork is never merged — safety).
func TestCanonicalCommitter_CollapsesAliasesNotForks(t *testing.T) {
	innerID := ids.GenerateTestID()

	// Two wrappers of the SAME inner block (the re-wrapped-each-slot storm shape).
	aliasA := &postForkBlock{postForkCommonComponents: postForkCommonComponents{innerBlk: &blocktest.Block{IDV: innerID}}}
	aliasB := &postForkOption{postForkCommonComponents: postForkCommonComponents{innerBlk: &blocktest.Block{IDV: innerID}}}
	if aliasA.CanonicalID() != aliasB.CanonicalID() {
		t.Fatal("two wrappers of the SAME inner block must share a CanonicalID (alias collapse — the storm fix)")
	}

	// A wrapper of a DIFFERENT inner block (a real execution fork).
	fork := &postForkBlock{postForkCommonComponents: postForkCommonComponents{innerBlk: &blocktest.Block{IDV: ids.GenerateTestID()}}}
	if aliasA.CanonicalID() == fork.CanonicalID() {
		t.Fatal("wrappers of DIFFERENT inner blocks must NOT collapse (genuine-fork safety)")
	}
}
