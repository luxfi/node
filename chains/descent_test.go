// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/luxfi/ids"
)

// TestShouldDescend_StopsAtGenesis pins the end of the walk.
//
// The descent asks a peer for the oldest entry's parent to move the served window
// one step closer to our tip. Genesis is where that walk ends: its height is 0 and
// its parent is the empty id, so there is no window below it to ask for. Asking
// anyway requests a block that cannot exist — the peer answers "not found", or the
// plugin answers with a zero-length parent id — and the empty response brings us
// straight back here to ask the same question of the next peer.
//
// Nothing stops that on its own. The height comparison cannot: a node whose
// finalized ledger is unset takes the `!finalizedSet` branch, and one whose ledger
// reads 0 finds every window "above" it. So the guard has to be the parent itself.
func TestShouldDescend_StopsAtGenesis(t *testing.T) {
	parentOfGenesis := ids.Empty

	for _, tc := range []struct {
		name          string
		finalized     uint64
		finalizedSet  bool
	}{
		{"no finalized ledger", 0, false},
		{"finalized reads zero", 0, true},
		{"finalized far ahead", 1162106, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if shouldDescend(0, true, 0, parentOfGenesis, tc.finalized, tc.finalizedSet) {
				t.Fatal("descended below genesis — this requests the empty block id, " +
					"which no peer can answer and which returns us here to ask again, " +
					"once per response, from every peer, without end")
			}
		})
	}
}

// TestShouldDescend_WalksDownFromARealBlock is the anti-vacuity control: the guard
// above must not have switched the descent off. A batch whose oldest entry is a real
// block, sitting above our tip, still walks down.
func TestShouldDescend_WalksDownFromARealBlock(t *testing.T) {
	realParent := ids.GenerateTestID()

	if !shouldDescend(0, true, 1159050, realParent, 1162106, false) {
		t.Fatal("a node with no finalized ledger must still descend from a real block")
	}
	if !shouldDescend(0, true, 1159050, realParent, 1000000, true) {
		t.Fatal("a batch starting well above our finalized tip must descend")
	}
}

// TestShouldDescend_HoldsWhenTheBatchLanded guards the other direction: a batch that
// accepted something is making progress on the window it already has, and asking for
// the window below it would walk away from the tip.
func TestShouldDescend_HoldsWhenTheBatchLanded(t *testing.T) {
	realParent := ids.GenerateTestID()

	if shouldDescend(168, true, 1159050, realParent, 1000000, true) {
		t.Fatal("descended from a batch that accepted 168 blocks")
	}
	if shouldDescend(0, false, 1159050, realParent, 1000000, true) {
		t.Fatal("descended without an oldest entry to descend from")
	}
	if shouldDescend(0, true, 1162107, realParent, 1162106, true) {
		t.Fatal("descended from a batch that already connects at finalized+1")
	}
}
