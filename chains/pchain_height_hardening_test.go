// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// pchain_height_hardening_test.go — the receive-side hardening proofs for the b2
// transport wrapper (Red round): the three node-layer fixes that close the
// receive-side gaps the build-side stamping left open.
//
//	(HIGH-1, predicate b — RECENCY) ParseBlock rejects a wrapped block whose
//	stamped P-chain epoch exceeds this node's live P-chain height by more than the
//	recency slack (an absurd-future epoch). Honest forward skew within the slack —
//	including a legitimate P-chain advance during a staking change — still parses.
//
//	(MEDIUM-3 — MAP DoS) the heights map is bounded: it plateaus at the finalized
//	watermark under mass parse+accept, and a still-pending block's epoch is never
//	evicted by the watermark prune. A sustained unverified-parse flood is capped.
//
//	(LOW-2 — FRAME DISCRIMINATOR) a raw inner block whose first bytes coincidentally
//	equal the 4-byte frame magic is NOT mis-parsed as framed: the inner re-parse of
//	the framed payload fails, so ParseBlock falls back to a raw whole-block parse.
package chains

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
)

// --- (HIGH-1 b) RECENCY: reject absurd-future epoch ---------------------------

// TestParseBlock_RejectsFarFutureEpoch is the receive-side upper-bound proof. A
// node whose live P-chain height is 100 parses a wrapped block stamped at epoch
// 10_000 (far beyond 100 + slack). ParseBlock REJECTS it fail-closed — the block
// is dropped, never returned to be tracked. This stops a Byzantine proposer from
// pinning a block to an epoch the network has not reached.
func TestParseBlock_RejectsFarFutureEpoch(t *testing.T) {
	const localH = uint64(100)
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	state := stateByHeight(netID, localH, map[uint64][]blsValidator{localH: {v}})
	wrapper := newPChainHeightVM(newFakeInnerVM(), state, netID)

	// Craft a frame stamped at an absurd-future epoch, with a registered inner block
	// so the ONLY reason to reject is the recency bound (not an inner parse failure).
	inner := newFakeInner(5_000_000, ids.Empty, "far-future-epoch")
	wrapper.inner.(*fakeInnerVM).register(inner)
	framed := wrapPChainHeight(inner.Bytes(), localH+pChainHeightRecencySlack+1) // just past the bound

	if _, err := wrapper.ParseBlock(context.Background(), framed); err != errPChainHeightNotRecent {
		t.Fatalf("ParseBlock(far-future epoch) err = %v, want errPChainHeightNotRecent — an absurd-future "+
			"P-chain epoch must be rejected fail-closed so a follower never tracks a block at an unreachable epoch", err)
	}
}

// TestParseBlock_AcceptsEpochWithinSlack proves the recency gate admits HONEST
// forward skew: a follower at local P-chain height 100 parses a block stamped at
// 100 + slack (the boundary — the largest legitimate skew, e.g. a proposer that
// saw P-chain blocks this follower has not yet synced during a staking change).
// The block parses and carries its real epoch.
func TestParseBlock_AcceptsEpochWithinSlack(t *testing.T) {
	const localH = uint64(100)
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	state := stateByHeight(netID, localH, map[uint64][]blsValidator{localH: {v}})
	wrapper := newPChainHeightVM(newFakeInnerVM(), state, netID)

	inner := newFakeInner(5_000_000, ids.Empty, "within-slack-epoch")
	wrapper.inner.(*fakeInnerVM).register(inner)
	atBound := localH + pChainHeightRecencySlack // exactly at the bound — must be admitted
	framed := wrapPChainHeight(inner.Bytes(), atBound)

	parsed, err := wrapper.ParseBlock(context.Background(), framed)
	if err != nil {
		t.Fatalf("ParseBlock(epoch within slack) err = %v, want nil — honest forward skew up to the slack "+
			"(a real P-chain advance during a staking change) must still parse", err)
	}
	if got := parsed.(*pChainHeightBlock).PChainHeight(); got != atBound {
		t.Fatalf("parsed epoch = %d, want %d — the wrapper must carry the real (recent) epoch unchanged", got, atBound)
	}
}

// TestParseBlock_RecencyDisabledWithoutState proves the fail-soft: a wrapper with
// no P-chain view (nil state, current height 0) cannot bound recency, so it admits
// the block — the verifier still fails closed on an unresolvable future epoch at
// set-resolution time. (Production installs the wrapper only on K>1 chains the
// manager guards to have a live state, so this is a defensive path, not a mode.)
func TestParseBlock_RecencyDisabledWithoutState(t *testing.T) {
	wrapper := newPChainHeightVM(newFakeInnerVM(), nil, ids.GenerateTestID())
	inner := newFakeInner(7, ids.Empty, "no-state-epoch")
	wrapper.inner.(*fakeInnerVM).register(inner)
	framed := wrapPChainHeight(inner.Bytes(), 9_999_999) // would be far-future if state existed

	if _, err := wrapper.ParseBlock(context.Background(), framed); err != nil {
		t.Fatalf("ParseBlock with nil state must admit (recency unenforceable, verifier fails closed downstream): %v", err)
	}
}

// --- (LOW-2) FRAME DISCRIMINATOR: a magic-colliding raw block is NOT mis-framed -

// TestParseBlock_MagicCollisionParsesRaw is the discriminator proof. A raw inner
// block whose Bytes() coincidentally BEGIN with the 4-byte frame magic (the
// 2^-32 collision the raw-hash-prefix VMs hit) must parse as a RAW block, not be
// mis-classified as framed (which used to fail the inner decode → bootstrap stall).
// The inner re-parse of the framed payload fails (the payload is the block minus
// its first 12 bytes — not a valid inner block), so ParseBlock falls back to a raw
// whole-block parse.
func TestParseBlock_MagicCollisionParsesRaw(t *testing.T) {
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	state := stateByHeight(netID, 50, map[uint64][]blsValidator{50: {v}})
	inner := newFakeInnerVM()
	wrapper := newPChainHeightVM(inner, state, netID)

	// A raw block whose bytes START with the exact magic prefix, then arbitrary
	// payload. Length is >= the header width so the prefix check would treat it as a
	// candidate frame. Crucially, bytes[12:] is NOT a registered inner block, so the
	// framed-payload re-parse fails and the whole-bytes parse must be used.
	raw := &fakeInnerBlock{
		id:       ids.GenerateTestID(),
		parentID: ids.Empty,
		height:   1234,
		bytes: append(append([]byte{}, pChainHeightMagic[:]...),
			[]byte("RAW-BLOCK-COLLIDES-WITH-MAGIC-PREFIX-payload")...),
	}
	inner.register(raw) // registers byBytes[whole] = raw; bytes[12:] stays UNregistered

	parsed, err := wrapper.ParseBlock(context.Background(), raw.bytes)
	if err != nil {
		t.Fatalf("ParseBlock(magic-colliding raw block) err = %v, want nil — a raw block whose prefix collides "+
			"with the frame magic must parse as RAW (the old behavior mis-framed it → inner decode fail → bootstrap stall)", err)
	}
	if parsed.ID() != raw.id {
		t.Fatalf("parsed block ID %s != raw block ID %s — the colliding block must decode to the SAME raw inner block", parsed.ID(), raw.id)
	}
	// A raw block falls back to the genesis-set epoch (0): it was NOT framed, so no
	// proposer epoch was carried.
	if got := parsed.(*pChainHeightBlock).PChainHeight(); got != 0 {
		t.Fatalf("magic-colliding raw block epoch = %d, want 0 — it must NOT be read as a framed epoch", got)
	}
	// Its re-gossip bytes must be the raw passthrough (the inner bytes verbatim), not
	// re-framed — so a follower of THIS node also sees it raw.
	if string(parsed.Bytes()) != string(raw.bytes) {
		t.Fatal("a raw colliding block must re-gossip its inner bytes verbatim (passthrough), not a new frame")
	}
}

// TestParseBlock_GenuineFrameStillParses guards the discriminator's other side: a
// GENUINE frame (whose payload IS a valid inner block) still parses as framed and
// recovers the stamped epoch. This pins that the collision fix did not break the
// normal framed path.
func TestParseBlock_GenuineFrameStillParses(t *testing.T) {
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	const epoch = uint64(33)
	state := stateByHeight(netID, epoch, map[uint64][]blsValidator{epoch: {v}})
	inner := newFakeInnerVM()
	wrapper := newPChainHeightVM(inner, state, netID)

	innerBlk := newFakeInner(9000, ids.Empty, "genuine-frame")
	inner.register(innerBlk)
	framed := wrapPChainHeight(innerBlk.Bytes(), epoch)

	parsed, err := wrapper.ParseBlock(context.Background(), framed)
	if err != nil {
		t.Fatalf("ParseBlock(genuine frame) err = %v, want nil", err)
	}
	if parsed.ID() != innerBlk.ID() {
		t.Fatalf("genuine frame parsed ID %s != inner ID %s", parsed.ID(), innerBlk.ID())
	}
	if got := parsed.(*pChainHeightBlock).PChainHeight(); got != epoch {
		t.Fatalf("genuine frame epoch = %d, want %d", got, epoch)
	}
}

// --- (MEDIUM-3) MAP BOUND: plateau at watermark, pending never evicted --------

// TestHeightsMap_PlateausAtWatermark is the DoS bound proof. We parse a long run
// of distinct framed blocks (each adds a heights entry) and ACCEPT them in order;
// each Accept advances the finalized-height watermark and prunes entries at/below
// it. The map plateaus — it does NOT grow without bound as the chain advances.
func TestHeightsMap_PlateausAtWatermark(t *testing.T) {
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	const epoch = uint64(10)
	state := stateByHeight(netID, epoch, map[uint64][]blsValidator{epoch: {v}})
	inner := newFakeInnerVM()
	wrapper := newPChainHeightVM(inner, state, netID)

	const n = 500
	var lastParsed *pChainHeightBlock
	for h := uint64(1); h <= n; h++ {
		blk := newFakeInner(h, ids.Empty, "plateau-"+itoa(h))
		inner.register(blk)
		framed := wrapPChainHeight(blk.Bytes(), epoch)
		parsed, err := wrapper.ParseBlock(context.Background(), framed)
		if err != nil {
			t.Fatalf("ParseBlock #%d: %v", h, err)
		}
		// Accept the PREVIOUS block (so the most-recent one is still "pending").
		if lastParsed != nil {
			if err := lastParsed.Accept(context.Background()); err != nil {
				t.Fatalf("Accept #%d: %v", h-1, err)
			}
		}
		lastParsed = parsed.(*pChainHeightBlock)

		// After each accept the map must be bounded: only entries strictly above the
		// watermark survive. We accept (h-1) blocks, so at most a tiny constant remain
		// (the just-parsed, not-yet-accepted block, plus any equal-height entries).
		wrapper.mu.Lock()
		size := len(wrapper.heights)
		wm := wrapper.finalizedHeight
		wrapper.mu.Unlock()
		if size > 4 { // generous constant: the pending working set, not O(n)
			t.Fatalf("heights map grew to %d entries at height %d (watermark %d) — it must plateau at the "+
				"finalized watermark, not accrete one permanent entry per parsed block (MEDIUM-3 DoS)", size, h, wm)
		}
	}
}

// TestHeightsMap_PendingNeverEvictedByWatermark proves the watermark prune NEVER
// drops a still-pending block's epoch. We track a pending block at height 1000
// (epoch recalled), accept a LOWER-height block (watermark advances only to that
// lower height), and assert the pending block's epoch is still recallable. A blind
// LRU would have dropped it; the watermark prune must not.
func TestHeightsMap_PendingNeverEvictedByWatermark(t *testing.T) {
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	const epoch = uint64(12)
	state := stateByHeight(netID, epoch, map[uint64][]blsValidator{epoch: {v}})
	inner := newFakeInnerVM()
	wrapper := newPChainHeightVM(inner, state, netID)

	// Pending block at a HIGH value height (1000).
	pending := newFakeInner(1000, ids.Empty, "pending-high")
	inner.register(pending)
	pendingFrame := wrapPChainHeight(pending.Bytes(), epoch)
	if _, err := wrapper.ParseBlock(context.Background(), pendingFrame); err != nil {
		t.Fatalf("ParseBlock(pending): %v", err)
	}

	// A different block at a LOW value height (5) finalizes → watermark = 5.
	low := newFakeInner(5, ids.Empty, "finalized-low")
	inner.register(low)
	lowFrame := wrapPChainHeight(low.Bytes(), epoch)
	lowParsed, err := wrapper.ParseBlock(context.Background(), lowFrame)
	if err != nil {
		t.Fatalf("ParseBlock(low): %v", err)
	}
	if err := lowParsed.Accept(context.Background()); err != nil {
		t.Fatalf("Accept(low): %v", err)
	}

	// The watermark advanced to 5 (the low block). The PENDING block at height 1000
	// is strictly above the watermark, so its epoch MUST survive the prune.
	if got, ok := wrapper.recall(pending.ID()); !ok || got != epoch {
		t.Fatalf("pending block's epoch recall = (%d,%v), want (%d,true) — the watermark prune (wm=5) must NEVER "+
			"evict a still-pending block at height 1000 (that would reset its epoch to 0 = re-freeze)", got, ok, epoch)
	}
	// And the finalized low block's entry WAS pruned (it is at/below the watermark).
	if _, ok := wrapper.recall(low.ID()); ok {
		t.Fatal("the finalized low block's entry should have been pruned at/below the watermark (loss-free: epoch already captured)")
	}
}

// TestHeightsMap_FloodCappedPreservingNearTip proves the hard-cap backstop: a
// sustained UNVERIFIED-parse flood (blocks that never finalize, so the watermark
// never advances) cannot grow the map past the cap, AND the cap evicts
// highest-value-height-first so the near-tip pending band survives. We seed a
// near-tip pending entry (low height), then flood with far-future-height frames;
// the map stays at the cap and the near-tip entry is retained.
func TestHeightsMap_FloodCappedPreservingNearTip(t *testing.T) {
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	const epoch = uint64(8)
	// Slack must admit the flood frames' epoch; we stamp them at the current epoch so
	// the recency gate is not what bounds them — the CAP is what we are testing.
	state := stateByHeight(netID, epoch, map[uint64][]blsValidator{epoch: {v}})
	inner := newFakeInnerVM()
	wrapper := newPChainHeightVM(inner, state, netID)

	// Near-tip pending block at LOW value height 1.
	nearTip := newFakeInner(1, ids.Empty, "near-tip")
	inner.register(nearTip)
	if _, err := wrapper.ParseBlock(context.Background(), wrapPChainHeight(nearTip.Bytes(), epoch)); err != nil {
		t.Fatalf("ParseBlock(near-tip): %v", err)
	}

	// Flood: cap + 200 distinct frames at FAR-future value heights (never accepted).
	for i := 0; i < pChainHeightMapCap+200; i++ {
		h := uint64(1_000_000 + i) // far above the near-tip height
		blk := newFakeInner(h, ids.Empty, "flood-"+itoa(h))
		inner.register(blk)
		if _, err := wrapper.ParseBlock(context.Background(), wrapPChainHeight(blk.Bytes(), epoch)); err != nil {
			t.Fatalf("ParseBlock(flood %d): %v", i, err)
		}
	}

	wrapper.mu.Lock()
	size := len(wrapper.heights)
	_, nearTipKept := wrapper.heights[nearTip.ID()]
	wrapper.mu.Unlock()

	if size > pChainHeightMapCap {
		t.Fatalf("heights map = %d entries, must be capped at %d under flood (MEDIUM-3 backstop)", size, pChainHeightMapCap)
	}
	if !nearTipKept {
		t.Fatal("the near-tip pending entry (height 1) must SURVIVE the cap — eviction is highest-height-first, " +
			"so a flood of far-future-height spam is shed before any near-tip pending block (NOT a blind LRU)")
	}
}

// itoa is a tiny, allocation-light uint64→string for unique test tags (avoids
// importing strconv into a hot test loop and keeps byBytes keys distinct).
func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
