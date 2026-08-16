// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_trust_test.go — the A–G proof matrix for the BootstrapTrust policy: the SEPARATE
// trust object (distinct from consensus finality) that lets a node recover when validators are
// down (mass recovery) while refusing partition-capture and never weakening finality.
//
// Most cases test the POLICY decision (AcceptsFrontier) directly — deterministic, no network
// timing — since that IS the acceptance gate the owner specified. The mass-recovery success (A)
// and the global-tally height-floor guard also run the FULL fetch+execute loop over the real
// transport to prove the node converges (or fails safe) end to end. Each matters: revert
// the response-floor policy to the prior ⅔-of-current-total-stake gate and A deadlocks; drop the
// configured-beacon filter and D/E capture; drop the MinFrontierHeight floor and the shared-
// genesis fork false-completes.
package chains

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/require"

	consensusconfig "github.com/luxfi/consensus/config"
	chainbootstrap "github.com/luxfi/consensus/engine/chain/bootstrap"
	"github.com/luxfi/ids"
)

// ----- policy test helpers --------------------------------------------------

// stubAncestry is an in-memory AncestrySource: it walks a parent-linked BlockRef map down from a
// tip, exactly as the real wire transport would serve content-addressed ancestry. Modeling the
// transport this way keeps the policy unit tests deterministic while exercising the real ancestor-
// tolerant tally. `withhold` models a beacon that names a tip but does NOT serve its ancestry.
type stubAncestry struct {
	byID     map[ids.ID]BlockRef
	withhold map[ids.ID]bool
}

// Ancestry serves the PRODUCTION wire order: oldest-first, ending at the requested
// block. chains/manager.go builds the real response by walking from the requested block
// down to genesis and PREPENDING each entry ("Prepend to keep oldest-first"), so the
// requested block is LAST. This double appended in the opposite order, which no peer
// ever sends; it went unnoticed because the walk it fed was order-agnostic, and it is
// the reason the ancestry verifier was first written against the wrong contract.
func (s *stubAncestry) Ancestry(_ context.Context, tip ids.ID, max int) ([]BlockRef, error) {
	if s.withhold[tip] {
		return nil, nil
	}
	var out []BlockRef
	cur := tip
	for i := 0; i < max; i++ {
		ref, ok := s.byID[cur]
		if !ok {
			break
		}
		out = append([]BlockRef{ref}, out...) // oldest-first, as production sends
		if ref.Parent == ids.Empty {
			break
		}
		cur = ref.Parent
	}
	return out, nil
}

// refChain builds genesis..n as content-addressed BlockRefs (parent-linked), returning the slice
// and an id→ref index for the stub AncestrySource.
func refChain(n int) ([]BlockRef, map[ids.ID]BlockRef) {
	refs := make([]BlockRef, 0, n+1)
	byID := map[ids.ID]BlockRef{}
	var parent ids.ID
	for h := 0; h <= n; h++ {
		r := BlockRef{ID: ids.GenerateTestID(), Height: uint64(h), Parent: parent}
		refs = append(refs, r)
		byID[r.ID] = r
		parent = r.ID
	}
	return refs, byID
}

// childRef makes a block extending `parent` at height parentHeight+1 — used to forge a "higher"
// sibling tip built on a real block.
func childRef(parent BlockRef) BlockRef {
	return BlockRef{ID: ids.GenerateTestID(), Height: parent.Height + 1, Parent: parent.ID}
}

func nodeIDs(n int) []ids.NodeID {
	out := make([]ids.NodeID, n)
	for i := range out {
		out[i] = ids.GenerateTestNodeID()
	}
	return out
}

// equalBeacons builds a TrustedBeacons map of equal-weight validators.
func equalBeacons(beacons []ids.NodeID, w uint64) map[ids.NodeID]StakeWeight {
	m := make(map[ids.NodeID]StakeWeight, len(beacons))
	for _, id := range beacons {
		m[id] = w
	}
	return m
}

func reply(id ids.NodeID, tip ids.ID, w uint64) BeaconReply {
	return BeaconReply{NodeID: id, Tip: tip, Weight: w}
}

// equalStake is the shape the policy is sized against: 5 validators each 0.5e18, total 2.5e18.
const equalStake uint64 = 500_000_000_000_000_000

// ----- A: MASS RECOVERY SUCCESS ---------------------------------------------

// TestBootstrapTrust_A_MassRecoverySucceeds is THE deadlock fix. 5 EQUAL-weight validators; the 2
// stranded recovery targets are down, so only 3 are reachable; the 3 reachable agree on the
// frontier. With MinResponses=3 the policy ACCEPTS — even though 3 of 5 stake (1.5e18) is BELOW
// the ⅔-of-current-total floor (1.667e18) that the prior code required to be CONNECTED. That old
// floor was mathematically unsatisfiable here (the down nodes ARE validators), which is exactly
// why no node could recover. This test pins both: the policy accepts, AND the old gate would have
// rejected (the deadlock), AND the full loop converges over the real transport.
func TestBootstrapTrust_A_MassRecoverySucceeds(t *testing.T) {
	// The deadlock the fix escapes: 3-of-5 connected stake does NOT clear ⅔ of the total set.
	require.LessOrEqual(t, 3*equalStake, consensusconfig.TwoThirdsStakeFloor(5*equalStake),
		"precondition: 3 of 5 equal validators is BELOW ⅔ of total — the prior connect gate's deadlock")

	// Policy decision: 5 configured, 3 reachable agree on the frontier.
	beacons := nodeIDs(5)
	frontier := ids.GenerateTestID()
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, equalStake),
		MinResponses:   3,
	}
	replies := []BeaconReply{
		reply(beacons[0], frontier, equalStake),
		reply(beacons[1], frontier, equalStake),
		reply(beacons[2], frontier, equalStake),
		// beacons[3], beacons[4] are down/stranded — no reply.
	}
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err, "3 of 5 reachable beacons agreeing MUST be accepted — the mass-recovery case")
	require.Equal(t, frontier, f.ID)
	require.Equal(t, 3, f.Responders)
	require.False(t, f.FromCheckpoint)

	// End to end over the real GetAcceptedFrontier/GetAncestors transport: a STALE node with only
	// 3 of its 5 equal-weight validators reachable converges to the frontier N (not stuck at M).
	const N = 40
	const M = 23
	chain, byID := buildBSChain(N, -1)
	vm := newBSVMAt(chain, M)
	v := nodeIDs(5)
	weights := equalBeacons(v, equalStake)
	bh, chainID := newBSHandlerWeighted(t, vm, weights)
	bh.bootstrapMinResponses = 3 // the owner's MinBootstrapResponses=3
	bh.net = &bsBeaconNet{
		bh: bh, chainID: chainID, connected: []ids.NodeID{v[0], v[1], v[2]}, // 2 stranded down
		byID: byID, tip: chain[N], serveAncestors: true,
	}
	bh.msgCreator = bsMsgBuilder{}
	ctx := context.Background()

	bh.bsActive.Store(true)
	tip, status := bh.FrontierTip(ctx)
	bh.bsActive.Store(false)
	require.Equal(t, chainbootstrap.FrontierNamed, status,
		"MASS RECOVERY: 3 of 5 equal validators reachable + agreeing must NAME the frontier (no deadlock)")
	require.Equal(t, chain[N].id, tip)

	require.NoError(t, runBS(t, bh), "mass-recovery node must converge")
	last, _ := vm.LastAccepted(ctx)
	require.Equal(t, chain[N].id, last, "RECOVERED: converged to the frontier N=%d despite 2 of 5 validators down", N)
	require.True(t, bh.Accepted(ctx, chain[N].id))
}

// ----- B: ONE-BEACON CAPTURE REJECTED ---------------------------------------

// TestBootstrapTrust_B_OneBeaconCaptureRejected: 5 configured, only 1 reachable. A single beacon —
// even an authentic configured one — cannot name the frontier (it could be the attacker's lone
// peer in an eclipse). The response FLOOR rejects it.
func TestBootstrapTrust_B_OneBeaconCaptureRejected(t *testing.T) {
	beacons := nodeIDs(5)
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, equalStake), MinResponses: 3}
	replies := []BeaconReply{reply(beacons[0], ids.GenerateTestID(), equalStake)}

	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.Nil(t, f)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses,
		"1 of 5 reachable must be REJECTED (capture) — below the MinResponses floor")
}

// TestBootstrapTrust_ShortfallNamesTheBindingCondition: the floor has two conditions, and the
// refusal has to name the one that actually bound. Reporting the count when it is the weight that
// binds prints a contradiction — five of six responded, need four — and sends the reader after
// beacons that are already answering. The numbers ride out on the error, which is where everything
// downstream reads them from.
func TestBootstrapTrust_ShortfallNamesTheBindingCondition(t *testing.T) {
	beacons := nodeIDs(6)
	frontier := ids.GenerateTestID()

	// COUNT binds: 3 of 6 responded against a majority floor of 4.
	byCount := &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, equalStake)}
	_, err := byCount.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], frontier, equalStake),
		reply(beacons[1], frontier, equalStake),
		reply(beacons[2], frontier, equalStake),
	})
	require.ErrorContains(t, err, "3 of 6 beacons responded, need 4",
		"the refusal must carry the responder count, the configured set size, and the floor")

	// WEIGHT binds: 5 of 6 responded (clearing the count floor of 4) but carry less stake than
	// MinResponseWeight demands. The count is satisfied, so naming it would state a falsehood.
	byWeight := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(beacons, equalStake),
		MinResponseWeight: 6*equalStake + 1, // unreachable by any subset — weight is what binds
	}
	_, err = byWeight.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], frontier, equalStake),
		reply(beacons[1], frontier, equalStake),
		reply(beacons[2], frontier, equalStake),
		reply(beacons[3], frontier, equalStake),
		reply(beacons[4], frontier, equalStake),
	})
	require.ErrorContains(t, err, "stake",
		"when weight is what binds, the refusal must say so rather than name a count that was met")
	require.NotContains(t, err.Error(), "5 of 6 beacons responded",
		"naming a count that IS satisfied reads as a contradiction and misdirects the reader")
}

// ----- C: TWO-BEACON PARTITION REJECTED -------------------------------------

// TestBootstrapTrust_C_TwoBeaconPartitionRejected: 5 configured, 2 reachable AGREEING. Two beacons
// is still below MinResponses=3, so the policy rejects by default — an attacker who partitions the
// node down to 2 beacons cannot capture the frontier even if both agree.
func TestBootstrapTrust_C_TwoBeaconPartitionRejected(t *testing.T) {
	beacons := nodeIDs(5)
	frontier := ids.GenerateTestID()
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, equalStake), MinResponses: 3}
	replies := []BeaconReply{
		reply(beacons[0], frontier, equalStake),
		reply(beacons[1], frontier, equalStake),
	}
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.Nil(t, f)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses,
		"2 of 5 reachable + agreeing must be REJECTED by default — the partition-capture floor is MinResponses=3")
}

// ----- D: NON-CONFIGURED PEER IGNORED ---------------------------------------

// TestBootstrapTrust_D_NonConfiguredPeerIgnored: an attacker peer that is NOT in the configured
// beacon set reports a higher forged tip. INVARIANT 1 (non-circular eligibility): peers never
// define who is a beacon, so the forged reply is dropped entirely and the configured beacons name
// the real frontier.
func TestBootstrapTrust_D_NonConfiguredPeerIgnored(t *testing.T) {
	beacons := nodeIDs(5)
	real := ids.GenerateTestID()
	forgedHigher := ids.GenerateTestID()
	attacker := ids.GenerateTestNodeID() // NOT in TrustedBeacons

	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, equalStake), MinResponses: 3}
	replies := []BeaconReply{
		reply(beacons[0], real, equalStake),
		reply(beacons[1], real, equalStake),
		reply(beacons[2], real, equalStake),
		reply(attacker, forgedHigher, 9_000_000_000_000_000_000), // huge self-reported weight, ignored
	}
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.Equal(t, real, f.ID, "the non-configured attacker's forged tip must be IGNORED")
	require.NotEqual(t, forgedHigher, f.ID)
	require.Equal(t, 3, f.Responders, "only the 3 configured beacons count toward the quorum")
}

// ----- E: MINORITY CONFIGURED FORGERY REJECTED ------------------------------

// TestBootstrapTrust_E_MinorityConfiguredForgeryRejected: 3 honest configured beacons report
// frontier A; 2 configured beacons report a FORGED tip B built directly on A (a forged higher
// sibling). C1: the forgers can only RATIFY A (the real block they built on); B itself holds only
// the Byzantine minority's stake and is NEVER named. The policy selects A.
func TestBootstrapTrust_E_MinorityConfiguredForgeryRejected(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(30) // genesis..30; A := refs[30]
	A := refs[30]
	forgedB := childRef(A) // forged sibling at height 31, parent = real A
	byID[forgedB.ID] = forgedB

	beacons := nodeIDs(5)
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		Source:         &stubAncestry{byID: byID},
	}
	replies := []BeaconReply{
		reply(beacons[0], A.ID, w),
		reply(beacons[1], A.ID, w),
		reply(beacons[2], A.ID, w),       // 3 honest on A (300)
		reply(beacons[3], forgedB.ID, w), // 2 Byzantine on the forged child (200)
		reply(beacons[4], forgedB.ID, w),
	}
	// floor = ⅔ of 500 = 333. Neither A (300) nor forgedB (200) clears it directly, so the
	// ancestor-tolerant tally runs: the forgers' stake flows DOWN through A (its real parent),
	// crediting A with 500 while forgedB keeps only 200 → A named, forgedB never.
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.Equal(t, A.ID, f.ID, "C1: the forged child only RATIFIES A — A is named")
	require.NotEqual(t, forgedB.ID, f.ID, "C1: the Byzantine-minority forged tip is NEVER named")
	require.Equal(t, A.Height, f.Height)
}

// ----- F: SPLIT REACHABLE ANCESTRY ------------------------------------------

// TestBootstrapTrust_F_SplitReachableAncestrySelectsCommonAncestor: 3 reachable configured beacons
// each report a DIFFERENT sibling tip (three pending blocks built on the same committed block H —
// the healthy bleeding edge). No single tip holds a supermajority, but H is in all three accepted
// chains, so the policy names H (the highest ⅔-of-responders common committed block), NOT any
// isolated tip.
func TestBootstrapTrust_F_SplitReachableAncestrySelectsCommonAncestor(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(39) // genesis..39; H := refs[39] (the common committed block)
	H := refs[39]
	a1, a2, a3 := childRef(H), childRef(H), childRef(H) // three sibling pending blocks at height 40
	for _, c := range []BlockRef{a1, a2, a3} {
		byID[c.ID] = c
	}

	beacons := nodeIDs(3)
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		Source:         &stubAncestry{byID: byID},
	}
	replies := []BeaconReply{
		reply(beacons[0], a1.ID, w),
		reply(beacons[1], a2.ID, w),
		reply(beacons[2], a3.ID, w),
	}
	// floor = ⅔ of 300 = 200. Each sibling holds only 100, but H is shared by all three → 300 > 200.
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.Equal(t, H.ID, f.ID, "must select the common committed ancestor H")
	require.Equal(t, H.Height, f.Height)
	require.NotEqual(t, a1.ID, f.ID)
	require.NotEqual(t, a2.ID, f.ID)
	require.NotEqual(t, a3.ID, f.ID)
}

// ----- G: FINALITY UNCHANGED ------------------------------------------------

// TestBootstrapTrust_G_FinalityUnchanged proves INVARIANT 3: a bootstrap-accepted frontier is NOT
// finality. The SAME 3-of-5 support that AcceptsFrontier admits as a sync anchor does NOT satisfy
// FinalityQuorum.HasFinality — live block acceptance still requires > ⅔ of CURRENT validator
// stake (4 of 5 here). The bootstrap quorum cannot finalize a block.
func TestBootstrapTrust_G_FinalityUnchanged(t *testing.T) {
	const w uint64 = 100
	const total = 5 * w
	beacons := nodeIDs(5)
	frontier := ids.GenerateTestID()
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, w), MinResponses: 3}

	// BootstrapTrust ACCEPTS 3 of 5 (a sync anchor).
	f, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], frontier, w),
		reply(beacons[1], frontier, w),
		reply(beacons[2], frontier, w),
	})
	require.NoError(t, err)
	require.Equal(t, frontier, f.ID)
	require.Equal(t, StakeWeight(3*w), f.Weight, "the frontier is backed by exactly the 3 responders")

	// FinalityQuorum says that SAME 3-of-5 weight is NOT finality — the decisions are different
	// objects with different thresholds. Finality is unchanged: it still needs > ⅔ (4 of 5).
	cq := DefaultFinalityQuorum()
	require.False(t, cq.HasFinality(3*w, total),
		"INVARIANT 3: a bootstrap-accepted frontier (3 of 5) is NOT a finalizing supermajority")
	require.True(t, cq.HasFinality(4*w, total),
		"finality UNCHANGED: > ⅔ of current stake (4 of 5) still finalizes")
	require.False(t, cq.HasFinality(f.Weight, total),
		"the bootstrap quorum's own backing weight cannot finalize a block")

	// AFTER-SYNC (the owner's "bootstrap is not a finality bypass"): the node has now SYNCED to the
	// frontier via BootstrapTrust and re-entered live consensus. A NEW block that collects the SAME
	// 3-of-5 stake STILL does not finalize — bootstrap admitted a sync ANCHOR, it did not lower the
	// finality bar. Live acceptance returns to strict > ⅔ of CURRENT stake, exactly as before any
	// bootstrap. HasFinality is stateless in the bootstrap outcome, which is the whole point: there
	// is no code path by which "we bootstrapped from 3/5" leaks into the finality decision.
	require.False(t, cq.HasFinality(3*w, total),
		"AFTER syncing from a 3-of-5 bootstrap frontier, live finality STILL needs > ⅔ (4 of 5) — no bypass")
}

// ----- checkpoint override (complements B) ----------------------------------

// edCheckpointAuthority is a test checkpoint authority backed by Ed25519 — a PROVEN primitive, no
// custom crypto. It signs a checkpoint's canonical (id,height) message and verifies against its own
// public key, rejecting an empty signature and any key that is not the configured authority.
type edCheckpointAuthority struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newEdCheckpointAuthority(t *testing.T) *edCheckpointAuthority {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return &edCheckpointAuthority{priv: priv, pub: pub}
}

func (a *edCheckpointAuthority) sign(id ids.ID, height uint64) []byte {
	return ed25519.Sign(a.priv, CanonicalCheckpointMessage(id, height))
}

// VerifyCheckpoint implements CheckpointVerifier: authenticate against the authority's public key.
func (a *edCheckpointAuthority) VerifyCheckpoint(id ids.ID, height uint64, sig []byte) bool {
	return len(sig) != 0 && ed25519.Verify(a.pub, CanonicalCheckpointMessage(id, height), sig)
}

// TestBootstrapTrust_CheckpointOverride: below the response floor (1 of 5), the DEFAULT is reject
// (test B), but an operator who pins a SIGNED checkpoint gets the explicit override — the node
// anchors to the authenticated (id,height) instead of trusting the lone beacon. This is the
// sanctioned escape hatch for a deeply-partitioned node, NEVER an open-ended ≥1-beacon acceptance,
// and (INVARIANT 4) NEVER a bare unsigned config value.
func TestBootstrapTrust_CheckpointOverride(t *testing.T) {
	beacons := nodeIDs(5)
	authority := newEdCheckpointAuthority(t)
	ckptID := ids.GenerateTestID()
	const ckptHeight = uint64(1_082_796)
	policy := &BootstrapPolicy{
		TrustedBeacons:     equalBeacons(beacons, equalStake),
		MinResponses:       3,
		Checkpoint:         &Checkpoint{ID: ckptID, Height: ckptHeight, Signature: authority.sign(ckptID, ckptHeight)},
		CheckpointVerifier: authority,
	}
	// 1 reachable beacon — below the floor — but a SIGNED checkpoint is pinned.
	f, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], ids.GenerateTestID(), equalStake),
	})
	require.NoError(t, err)
	require.True(t, f.FromCheckpoint, "below the floor with a SIGNED checkpoint → anchor to the checkpoint")
	require.Equal(t, ckptID, f.ID)
	require.Equal(t, ckptHeight, f.Height)

	// Without the checkpoint the same 1-of-5 is rejected (the default — never trust the lone beacon).
	policy.Checkpoint = nil
	_, err = policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], ids.GenerateTestID(), equalStake),
	})
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses)
}

// TestBootstrapTrust_CheckpointMustBeSigned is INVARIANT 4: a checkpoint that is present but not
// AUTHENTICATED is REJECTED (fail closed). A compromised flag/config that pins a false (id,height)
// cannot inject a sync anchor without the authority's signature. Four rejection modes, one accept.
func TestBootstrapTrust_CheckpointMustBeSigned(t *testing.T) {
	beacons := nodeIDs(5)
	authority := newEdCheckpointAuthority(t)
	attacker := newEdCheckpointAuthority(t) // a DIFFERENT key — not the configured authority
	ckptID := ids.GenerateTestID()
	const h = uint64(500_000)
	lone := []BeaconReply{reply(beacons[0], ids.GenerateTestID(), equalStake)} // 1-of-5, below floor

	base := func() *BootstrapPolicy {
		return &BootstrapPolicy{TrustedBeacons: equalBeacons(beacons, equalStake), MinResponses: 3, CheckpointVerifier: authority}
	}

	// (1) UNSIGNED checkpoint (empty signature) → rejected even with a verifier wired.
	p := base()
	p.Checkpoint = &Checkpoint{ID: ckptID, Height: h}
	_, err := p.AcceptsFrontier(context.Background(), lone)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses, "an UNSIGNED checkpoint must be rejected")

	// (2) signed by a NON-AUTHORITY (attacker) key → rejected.
	p = base()
	p.Checkpoint = &Checkpoint{ID: ckptID, Height: h, Signature: attacker.sign(ckptID, h)}
	_, err = p.AcceptsFrontier(context.Background(), lone)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses, "a checkpoint signed by a non-authority key must be rejected")

	// (3) authority signature over a DIFFERENT (id,height) — replay onto a forged anchor → rejected.
	p = base()
	forgedID := ids.GenerateTestID()
	p.Checkpoint = &Checkpoint{ID: forgedID, Height: h, Signature: authority.sign(ckptID, h)} // sig binds ckptID, not forgedID
	_, err = p.AcceptsFrontier(context.Background(), lone)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses, "a signature transplanted to a different (id,height) must be rejected")

	// (4) NO verifier configured → any checkpoint is untrusted (fail closed).
	p = base()
	p.CheckpointVerifier = nil
	p.Checkpoint = &Checkpoint{ID: ckptID, Height: h, Signature: authority.sign(ckptID, h)}
	_, err = p.AcceptsFrontier(context.Background(), lone)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses, "no verifier ⇒ even a validly-signed checkpoint is untrusted")

	// (accept) authority signs the exact pinned (id,height) → trusted.
	p = base()
	p.Checkpoint = &Checkpoint{ID: ckptID, Height: h, Signature: authority.sign(ckptID, h)}
	f, err := p.AcceptsFrontier(context.Background(), lone)
	require.NoError(t, err)
	require.True(t, f.FromCheckpoint)
	require.Equal(t, ckptID, f.ID)
}

// ----- safety guard for the global ancestor-tolerant tally ------------------

// TestBootstrapTrust_ForkAtSharedGenesisFailsSafe guards the
// MinFrontierHeight floor — the safety property the global cross-anchor tally (which makes case F
// work) would otherwise break. Two branches fork at a DEEP shared ancestor H (height 5), and the
// node is stale ABOVE the fork (height 23). The tally credits H with the union of BOTH halves'
// stake (all responders share H), so without the floor it would name H — and since the node
// already HOLDS H, the loop would FALSE-COMPLETE at the stale height instead of recognizing it has
// no ⅔-agreed frontier ahead. The MinFrontierHeight floor refuses to name any block beneath the
// node's last-accepted height, turning the partition into a safe ErrNoBootstrapQuorum.
//
// Asserted deterministically at the POLICY level (a stub AncestrySource serves BOTH branches'
// shared ancestry — the real wire transport's rotated sampling may only serve one, masking the
// vulnerability, so the integration path is NOT a faithful test of this guard). Revert the floor
// (set MinFrontierHeight: 0) and this names H instead of failing safe.
func TestBootstrapTrust_ForkAtSharedGenesisFailsSafe(t *testing.T) {
	const w uint64 = 100
	const nodeHeight = 23

	// Shared prefix genesis..H (H at height 5), then two divergent branches to height 40.
	shared, byID := refChain(5)
	H := shared[5]
	branchA := []BlockRef{H}
	branchB := []BlockRef{H}
	for h := 6; h <= 40; h++ {
		a := childRef(branchA[len(branchA)-1])
		b := childRef(branchB[len(branchB)-1])
		byID[a.ID], byID[b.ID] = a, b
		branchA = append(branchA, a)
		branchB = append(branchB, b)
	}
	tipA, tipB := branchA[len(branchA)-1], branchB[len(branchB)-1]

	beacons := nodeIDs(6)
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(beacons, w),
		MinResponses:      4,
		MinFrontierHeight: nodeHeight, // the node is stale at height 23, ABOVE the fork at 5
		Source:            &stubAncestry{byID: byID},
	}
	replies := []BeaconReply{
		reply(beacons[0], tipA.ID, w), reply(beacons[1], tipA.ID, w), reply(beacons[2], tipA.ID, w),
		reply(beacons[3], tipB.ID, w), reply(beacons[4], tipB.ID, w), reply(beacons[5], tipB.ID, w),
	}
	// H (height 5) is shared by all 6 → 600 > floor(400). But it is BELOW the node's height, so the
	// floor refuses it; no block at/above height 23 has ⅔ → fail safe.
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.Nil(t, f, "must not name the deep shared ancestor — that would false-complete at the stale height")
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"a fork sharing only blocks BELOW the node's height must fail safe, never name the deep common ancestor")

	// The same split with the node BELOW the fork (a fresh node) legitimately names H — the floor
	// only blocks naming history the node already has, never a real frontier ahead.
	policy.MinFrontierHeight = 0
	f, err = policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.Equal(t, H.ID, f.ID, "with the node below the fork, H IS the ⅔-common frontier to sync to")
}

// ----- H: SKEWED-WEIGHT PARTITION-CAPTURE (the MinResponseWeight floor) ---------------------

// weightedBeacons builds a TrustedBeacons map from an explicit per-node weight list — for
// modeling a SKEWED (non-uniform) validator stake distribution.
func weightedBeacons(beacons []ids.NodeID, w []uint64) map[ids.NodeID]StakeWeight {
	m := make(map[ids.NodeID]StakeWeight, len(beacons))
	for i, id := range beacons {
		m[id] = StakeWeight(w[i])
	}
	return m
}

// TestBootstrapTrust_H_SkewedWeightPartitionRejected pins the stake-majority floor.
// Under SKEWED validator weights the MinResponses COUNT floor and the ⅔-of-responders
// WEIGHT agreement diverge: an attacker who eclipses the HEAVY honest beacon but lets enough LIGHT
// honest beacons through to satisfy the count can shrink the responder-WEIGHT denominator until his
// < ⅓-of-total Byzantine stake clears ⅔-of-responders and NAMES A FORGED FRONTIER. The MinResponseWeight
// stake-majority floor (> ½ of TOTAL configured beacon stake) closes this — a < ⅓-stake adversary can
// never make the responders carry a ⅔ weight majority once they must also carry > ½ of the total.
//
// The capture, concretely: 6 beacons w={3,3,13,1,1,1}, total 22, Byzantine {B0,B1}=6 (27% < ⅓). The attacker
// partitions to {B0,B1 on forgedF} + {H2,H3 on realR} = 4 responders (= the majority count floor),
// responderWeight=8, ⅔-floor=5, backing[forgedF]=6 > 5 → forgedF would be named. The heavy honest H1
// (weight 13, on the real tip) is eclipsed. With MinResponseWeight=⌈22/2⌉=12, responderWeight=8 < 12
// → the partition is rejected (the node waits for / re-samples a stake-majority of beacons).
func TestBootstrapTrust_H_SkewedWeightPartitionRejected(t *testing.T) {
	refs, byID := refChain(30)
	realR := refs[30]
	forgedF := childRef(realR) // forged sibling at height 31 (its only honest ancestor is realR)
	byID[forgedF.ID] = forgedF

	b := nodeIDs(6)
	weights := []uint64{3, 3, 13, 1, 1, 1} // total 22; Byzantine b[0],b[1]=6 (<⅓)
	var total uint64
	for _, w := range weights {
		total += w
	}
	tb := weightedBeacons(b, weights)

	// The eclipse: only the 2 Byzantine + 2 LIGHT honest answer; the HEAVY honest b[2] (the real
	// tip's weight-13 voter) and b[5] are partitioned away.
	replies := []BeaconReply{
		reply(b[0], forgedF.ID, weights[0]), // Byzantine, light
		reply(b[1], forgedF.ID, weights[1]), // Byzantine, light
		reply(b[3], realR.ID, weights[3]),   // honest, light
		reply(b[4], realR.ID, weights[4]),   // honest, light
	}

	// WITHOUT the stake-majority floor (the bug): the forged tip is named.
	vuln := &BootstrapPolicy{TrustedBeacons: tb, MinResponses: 4, Source: &stubAncestry{byID: byID}}
	if f, err := vuln.AcceptsFrontier(context.Background(), replies); err == nil && f != nil {
		require.Equal(t, forgedF.ID, f.ID,
			"VULN PRECONDITION: without MinResponseWeight the eclipsed skewed partition names the forged tip (proves the floor is required)")
	}

	// WITH the stake-majority floor (the fix, exactly as bootstrapPolicy() now wires it): rejected.
	fixed := &BootstrapPolicy{
		TrustedBeacons:    tb,
		MinResponses:      4,
		MinResponseWeight: StakeWeight(total/2 + 1), // ⌈total/2⌉ = 12
		Source:            &stubAncestry{byID: byID},
	}
	_, err := fixed.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses,
		"FIX: responderWeight 8 < ½-stake floor 12 → the skewed partition cannot name a frontier (forged or otherwise)")

	// And the fix still admits an HONEST stake-majority: add the heavy honest H1 (weight 13) on realR.
	full := append(replies, reply(b[2], realR.ID, weights[2])) // responderWeight 8+13 = 21 ≥ 12
	f, err := fixed.AcceptsFrontier(context.Background(), full)
	require.NoError(t, err, "an honest stake-majority of responders still names the real frontier")
	require.Equal(t, realR.ID, f.ID, "the real tip is named once a stake-majority is reachable; forged never")
}

// TestBootstrapTrust_D2_NonConfiguredSwarmNamesNothing is the cleaner isolation of the
// configured-beacon filter (INVARIANT 1): a SWARM of non-configured peers
// (enough to clear any count floor on their own) all shouting a forged frontier names NOTHING,
// because none is in TrustedBeacons. This proves the filter, not merely the MinResponders floor.
func TestBootstrapTrust_D2_NonConfiguredSwarmNamesNothing(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(30)
	real := refs[30]
	forged, fbyID := refChain(40) // a wholly forged chain from a fresh genesis
	for id, r := range fbyID {
		byID[id] = r
	}

	configured := nodeIDs(3) // the real beacon set
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(configured, w),
		MinResponses:      2,
		MinResponseWeight: StakeWeight(w*3/2 + 1),
		Source:            &stubAncestry{byID: byID},
	}

	// 50 non-configured peers, each heavy, all on the forged tip — NOT in TrustedBeacons.
	swarm := nodeIDs(50)
	var replies []BeaconReply
	for _, p := range swarm {
		replies = append(replies, reply(p, forged[40].ID, 9_000_000))
	}
	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses,
		"INVARIANT 1: non-configured peers carry ZERO weight — a forged swarm names nothing")

	// Add the 3 real configured beacons on the real tip → the real tip is named, swarm invisible.
	for _, c := range configured {
		replies = append(replies, reply(c, real.ID, w))
	}
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err)
	require.Equal(t, real.ID, f.ID, "only configured beacons name the frontier; the 50-peer forged swarm is ignored")
}

// TestBootstrapPolicy_WiresStakeMajorityFloor pins the constructor:
// bootstrapPolicy() MUST emit MinResponseWeight = ⌈total/2⌉. The H/D2
// tests construct policies directly, so a mutation that stops the constructor from setting the
// floor would not be caught — this asserts the WIRING on the real path. Mutation-proof:
// neuter `if total > 0` in bootstrapPolicy() and this test fails (MinResponseWeight==0).
func TestBootstrapPolicy_WiresStakeMajorityFloor(t *testing.T) {
	refs, _ := refChain(5)
	vm := newBSVMAt(refs5BSBlocks(refs), 0)
	bh, _ := newBSHandlerWeighted(t, vm, map[ids.NodeID]uint64{}) // handler shell; we call bootstrapPolicy directly

	// SKEWED set: total = 22, ⌈total/2⌉ = 12.
	b := nodeIDs(6)
	weights := map[ids.NodeID]uint64{b[0]: 3, b[1]: 3, b[2]: 13, b[3]: 1, b[4]: 1, b[5]: 1}
	var total uint64
	for _, w := range weights {
		total += w
	}

	pol := bh.bootstrapPolicy(weights)
	require.Equal(t, StakeWeight(total/2+1), pol.MinResponseWeight,
		"REGRESSION: bootstrapPolicy() must wire MinResponseWeight = ⌈total/2⌉ (skewed-weight floor)")
	require.Equal(t, len(weights)/2+1, pol.MinResponses,
		"bootstrapPolicy() must wire the count-majority floor too")
	require.NotNil(t, pol.Source, "the policy must carry an AncestrySource")

	// EQUAL-weight: 5 × 0.5e18 — the floor must not re-deadlock 3-of-5 (= 0.6 ≥ 0.5).
	eq := equalBeacons(nodeIDs(5), 500_000_000_000_000_000)
	var eqTotal uint64
	for _, w := range eq {
		eqTotal += uint64(w)
	}
	eqPol := bh.bootstrapPolicy(eq)
	require.Equal(t, StakeWeight(eqTotal/2+1), eqPol.MinResponseWeight)
	require.Less(t, eqPol.MinResponseWeight, StakeWeight(3*500_000_000_000_000_000),
		"3-of-5 equal stake (0.6·total) must clear the ½ floor — no re-deadlock")

	// DEGENERATE: empty weights → floor disabled (0), no panic.
	require.Equal(t, StakeWeight(0), bh.bootstrapPolicy(map[ids.NodeID]uint64{}).MinResponseWeight,
		"empty weights → MinResponseWeight disabled (pre-P-chain / single-node fallback)")
}

// refs5BSBlocks adapts a BlockRef chain to the []*bsTestBlock the bsTestVM needs (genesis only
// accepted), so newBSHandlerWeighted has a VM. The handler is used only to call bootstrapPolicy().
func refs5BSBlocks(refs []BlockRef) []*bsTestBlock {
	out := make([]*bsTestBlock, len(refs))
	var parent ids.ID
	for i, r := range refs {
		out[i] = &bsTestBlock{id: r.ID, parent: parent, height: r.Height, bytes: []byte(r.ID.String()), valid: true}
		parent = r.ID
	}
	return out
}

// ----- CaughtUp: the tip-holder go-live determination -----------------------
//
// CaughtUp is the DUAL of AcceptsFrontier — "nobody is ahead" vs "here is the block ahead to sync
// to". It is the go-live path for a TIP-HOLDER on a mixed-height co-restart, where the responders
// SPLIT below the ⅔ naming threshold so AcceptsFrontier names NOTHING yet the node is plainly not
// behind. Getting its SAFETY exactly right is the hinge between "a tip-holder proceeds" and "a
// stale node declares itself ready": these pin all three conditions (floor met, none-ahead,
// holds-every-tip) and prove the two adversarial fake-caught-up attempts FAIL.

// heldOracle builds the height ORACLE CaughtUp injects: a block's height, ok=false when not held.
func heldOracle(held map[ids.ID]uint64) func(ids.ID) (uint64, bool) {
	return func(id ids.ID) (uint64, bool) { h, ok := held[id]; return h, ok }
}

// TestBootstrapTrust_CaughtUp_TipHolderSplitGoesReady is the CRITICAL regression at the policy layer:
// the shape a whole fleet comes back in. A producer at N sees 4 responders split {N, N, N-16, genesis};
// the tip-holders are only ½ (< ⅔), so AcceptsFrontier names NOTHING (ErrNoBootstrapQuorum) — yet the
// node holds every reported tip and none is above N, so CaughtUp is TRUE. It pins BOTH halves: the
// SAME replies yield no NAMED frontier (the case the tip-holder fails safe DOWN without this fix) but
// ARE caught-up.
func TestBootstrapTrust_CaughtUp_TipHolderSplitGoesReady(t *testing.T) {
	const N = 40
	refs, byID := refChain(N) // genesis..N
	b := nodeIDs(5)           // 5 equal-weight beacons (the node is the 5th, not a responder)
	const w = uint64(100)     // total 500 → MinResponseWeight ⌈500/2⌉=251, MinResponses majority=3
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(b, w),
		MinResponses:      3,
		MinResponseWeight: 251,
		MinFrontierHeight: N,
		Source:            &stubAncestry{byID: byID},
	}

	// 4 connected responders: 2 at the tip N, one stale at N-16, one at genesis — the production shape.
	replies := []BeaconReply{
		reply(b[0], refs[N].ID, w),
		reply(b[1], refs[N].ID, w),
		reply(b[2], refs[N-16].ID, w),
		reply(b[3], refs[0].ID, w),
	}

	// HALF 1: AcceptsFrontier names NOTHING — the tip-holders (200) do not clear ⅔ (266), and the
	// ⅔-backed common ancestor N-16 is below MinFrontierHeight=N (history the node already has).
	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"the mixed-height split names no frontier — exactly the case the tip-holder froze on without CaughtUp")

	// HALF 2: the node HOLDS its accepted chain 0..N, so it holds every reported tip and none is above
	// N → CaughtUp is TRUE. This is the go-live path the regression was missing.
	held := map[ids.ID]uint64{refs[N].ID: N, refs[N-16].ID: N - 16, refs[0].ID: 0}
	require.True(t, policy.CaughtUp(replies, N, heldOracle(held)),
		"a tip-holder that holds every reported tip and is at the top of all of them IS caught up")
}

// TestBootstrapTrust_CaughtUp_StaleNodeNotCaughtUp is the FORWARD safety guard (the stale-go-live bug
// staying FIXED): a STALE node at N-16 with honest producers at N PRESENT must NOT be caught-up — an
// honest responder is ahead, so it still SYNCS. CaughtUp must not fire merely because SOME responders
// are at/below the node.
func TestBootstrapTrust_CaughtUp_StaleNodeNotCaughtUp(t *testing.T) {
	const N = 40
	refs, _ := refChain(N)
	b := nodeIDs(5)
	const w = uint64(100)
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, MinResponseWeight: 251, MinFrontierHeight: N - 16}

	replies := []BeaconReply{
		reply(b[0], refs[N].ID, w),    // honest, AHEAD
		reply(b[1], refs[N].ID, w),    // honest, AHEAD
		reply(b[2], refs[N-16].ID, w), // at the node's height
		reply(b[3], refs[N-20].ID, w), // below
	}
	// The node holds only 0..N-16 — it does NOT hold the producers' tip N.
	held := map[ids.ID]uint64{refs[N-16].ID: N - 16, refs[N-20].ID: N - 20}
	require.False(t, policy.CaughtUp(replies, N-16, heldOracle(held)),
		"a stale node with an honest responder ahead must NOT be caught up — it syncs (stale-go-live stays fixed)")
}

// TestBootstrapTrust_CaughtUp_StaleNodeMinorityFakeRejected is adversarial fake-caught-up #1 (honest
// present): a node at N-16 where a <⅓-stake set of beacons reports ≤ N-16 to fake caught-up WHILE the
// honest producers at N are also present. The honest max is ahead (and the node lacks tip N) → NOT
// caught up. The minority cannot fake it past the honest responders.
func TestBootstrapTrust_CaughtUp_StaleNodeMinorityFakeRejected(t *testing.T) {
	const N = 40
	refs, _ := refChain(N)
	b := nodeIDs(5)
	const w = uint64(100)
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, MinResponseWeight: 251, MinFrontierHeight: N - 16}

	// 3 honest producers at N (ahead) + 1 Byzantine at N-16 trying to fake "everyone is at my height".
	replies := []BeaconReply{
		reply(b[0], refs[N].ID, w),
		reply(b[1], refs[N].ID, w),
		reply(b[2], refs[N].ID, w),
		reply(b[3], refs[N-16].ID, w), // the < ⅓ liar
	}
	held := map[ids.ID]uint64{refs[N-16].ID: N - 16} // node holds only up to N-16
	require.False(t, policy.CaughtUp(replies, N-16, heldOracle(held)),
		"a <⅓ minority reporting ≤N-16 cannot fake caught-up while honest producers at N are present")
}

// TestBootstrapTrust_CaughtUp_EclipsedMinorityFailsSafe is adversarial fake-caught-up #2 (honest
// eclipsed): the honest producers (at N) are SUPPRESSED and only a <½-stake set of beacons reports
// ≤ N-16. The response FLOOR (the SAME one AcceptsFrontier uses) is not met → CaughtUp is FALSE →
// fail safe. Faking caught-up costs the same stake-majority of honest beacons that faking a NAMED
// frontier does — no partition-capture.
func TestBootstrapTrust_CaughtUp_EclipsedMinorityFailsSafe(t *testing.T) {
	const N = 40
	refs, _ := refChain(N)
	b := nodeIDs(5)
	const w = uint64(100) // total 500 → MinResponseWeight 251
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, MinResponseWeight: 251, MinFrontierHeight: N - 16}

	// Only 2 of 5 beacons answer (the honest producers at N are eclipsed). Their weight 200 < 251.
	replies := []BeaconReply{
		reply(b[2], refs[N-16].ID, w),
		reply(b[3], refs[N-20].ID, w),
	}
	held := map[ids.ID]uint64{refs[N-16].ID: N - 16, refs[N-20].ID: N - 20}
	require.False(t, policy.CaughtUp(replies, N-16, heldOracle(held)),
		"an eclipsed <½-stake responder set cannot fake caught-up — the floor is not met (fail safe)")

	// Sanity: AcceptsFrontier ALSO rejects this set below the floor (the SAME floor gates both paths).
	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses, "the same floor gates naming and caught-up")
}

// TestBootstrapTrust_CaughtUp_OneAcceptedBlockBehindSyncs proves condition (b) uses the ACCEPTED
// height: a node at accepted N that has merely PROCESSED N+1 (holds it) is NOT caught up when a
// producer has ACCEPTED N+1 — it must sync that block. heightOf reads the block's canonical height,
// so a held-but-above-lastAccepted tip correctly defeats caught-up (the ±1 pending skew cannot fake it).
func TestBootstrapTrust_CaughtUp_OneAcceptedBlockBehindSyncs(t *testing.T) {
	const N = 40
	refs, _ := refChain(N + 1) // includes N+1
	b := nodeIDs(5)
	const w = uint64(100)
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, MinResponseWeight: 251, MinFrontierHeight: N}
	replies := []BeaconReply{
		reply(b[0], refs[N+1].ID, w), // a producer ACCEPTED N+1
		reply(b[1], refs[N].ID, w),
		reply(b[2], refs[N].ID, w),
	}
	// The node holds 0..N AND has processed N+1 (held), but its ACCEPTED height is N.
	held := map[ids.ID]uint64{refs[N+1].ID: N + 1, refs[N].ID: N}
	require.False(t, policy.CaughtUp(replies, N, heldOracle(held)),
		"a node one ACCEPTED block behind (even if it processed N+1) must NOT be caught up — it syncs")
}

// TestBootstrapTrust_CaughtUp_SameHeightForkNotHeld proves condition (c): a responder reporting a
// DIFFERENT block at the node's height (a fork the node never finalized) defeats caught-up — the node
// must HOLD every reported tip, not merely match heights numerically.
func TestBootstrapTrust_CaughtUp_SameHeightForkNotHeld(t *testing.T) {
	const N = 40
	refs, _ := refChain(N)
	fork := BlockRef{ID: ids.GenerateTestID(), Height: N} // a sibling at height N the node does NOT hold
	b := nodeIDs(5)
	const w = uint64(100)
	policy := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, MinResponseWeight: 251, MinFrontierHeight: N}
	replies := []BeaconReply{
		reply(b[0], refs[N].ID, w),
		reply(b[1], refs[N].ID, w),
		reply(b[2], fork.ID, w), // a fork at the same height N
	}
	held := map[ids.ID]uint64{refs[N].ID: N} // the node holds its tip N but NOT the fork
	require.False(t, policy.CaughtUp(replies, N, heldOracle(held)),
		"a same-height fork the node does not hold defeats caught-up (condition c: holds every reported tip)")
}

// ----- M1: the pre-existing eclipse-stale own-height path (red fast-follow) ------------------
//
// M1 is the pre-existing path the own-height filter tightening closes. BEFORE: nameFrontier filtered
// the ancestor-tolerant tally with `ref.Height < MinFrontierHeight` (== the node's own last-accepted),
// so a block AT the node's own height PASSED the filter and could be NAMED. An eclipse that throttles
// the genuinely-ahead responders below the ⅔ naming threshold — while letting the at-height responders
// through — makes the node's OWN height accrue ⅔ purely as the shared ANCESTOR of those ahead tips, so
// nameFrontier names it → FrontierNamed at own height → the node goes Ready STALE (here, 5 blocks
// behind a finalized N+5). AFTER: the filter is `ref.Height <= MinFrontierHeight`, so own height is
// EXCLUDED from naming; the at-own-height decision routes to CaughtUp, which SEES the N+5 ahead tips
// (un-held, above) and REFUSES → the node syncs/fails safe instead of going Ready stale.
//
// Deterministic, no network timing. Revert the filter to `<` and the first assertion (own height
// NOT named → ErrNoBootstrapQuorum) FAILS — that revert IS the M1 bug, so this is the RED-before /
// GREEN-after pin. The boundary sub-assertion (one notch lower DOES name N) proves it is precisely
// the OWN-HEIGHT exclusion doing the work, not some unrelated filter.
func TestBootstrapTrust_EclipseOwnHeightNotNamedRoutesToCaughtUp(t *testing.T) {
	const N = 40       // the node's own last-accepted height
	const ahead = N + 5 // a GENUINELY FINALIZED block 5 ahead — the eclipse throttles its visibility
	refs, byID := refChain(ahead) // genesis..N+5, parent-linked; the ahead set's tip descends through N
	const w = uint64(100)

	b := nodeIDs(6) // 6 configured beacons @100 → total 600; MinResponseWeight ⌈600/2⌉=301, MinResponses majority=4
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(b, w),
		MinResponses:      4,
		MinResponseWeight: 301,
		MinFrontierHeight: N, // the node's own last-accepted height — exactly the M1 boundary
		Source:            &stubAncestry{byID: byID},
	}

	// THE ECLIPSE CONSTRUCTION (red's, verbatim numbers): the ahead responders are throttled to
	// R_a = 300 (3 beacons at N+5, BELOW the ⅔-of-responders naming threshold), the behind/at-height
	// responders R_b = 200 (2 beacons at N) all get through; the 6th beacon is eclipsed (no reply).
	// R = R_a + R_b = 500 > ½·600 (floor met). R_a = 300 < ⅔R = 333 (so N+5 is NOT named). YET block N
	// accrues R_a + R_b = 500 > ⅔R because the ahead nodes credit N as an ANCESTOR of N+5.
	replies := []BeaconReply{
		reply(b[0], refs[N].ID, w),     // at the node's own height N
		reply(b[1], refs[N].ID, w),     // at the node's own height N  (R_b = 200)
		reply(b[2], refs[ahead].ID, w), // genuinely ahead at N+5
		reply(b[3], refs[ahead].ID, w), // genuinely ahead at N+5
		reply(b[4], refs[ahead].ID, w), // genuinely ahead at N+5      (R_a = 300, < ⅔·500 = 333)
		// b[5] eclipsed — no reply.
	}

	// Sanity pins on the construction (so a future edit that breaks the eclipse shape is caught).
	require.Equal(t, uint64(333), Ratio{2, 3}.floorOf(500), "⅔-of-responders floor over R=500 is 333")
	require.Less(t, uint64(300), uint64(333), "R_a=300 is BELOW the ⅔ naming threshold — N+5 is not nameable")

	// AFTER (the fix): own height N is EXCLUDED from naming → no ⅔-backed block ABOVE N exists
	// (N+5 is sub-⅔) → ErrNoBootstrapQuorum. (Revert `<=`→`<` and this names refs[N] — the M1 bug.)
	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"M1 FIX: the node's OWN height must NOT be named even when ahead tips credit it as a ⅔-backed ancestor")

	// …and the decision routes to CaughtUp, which SEES the genuinely-ahead N+5 tips (un-held, above
	// the node's height) and REFUSES — so the node syncs toward N+5, never goes Ready at stale N.
	held := map[ids.ID]uint64{} // the node holds 0..N, NOT N+1..N+5
	for h := 0; h <= N; h++ {
		held[refs[h].ID] = uint64(h)
	}
	require.False(t, policy.CaughtUp(replies, N, heldOracle(held)),
		"M1 FIX: routed to CaughtUp, the eclipse's ahead tips (un-held, above N) correctly defeat caught-up → sync")

	// BOUNDARY: the SAME replies with MinFrontierHeight one notch lower (N-1) DO name N (height N is
	// now STRICTLY ABOVE the floor). This proves the refusal above is precisely the OWN-HEIGHT
	// exclusion — not the ⅔ tally, the responder floor, or the voter count — doing the work.
	policy.MinFrontierHeight = N - 1
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err, "one notch below own height, N is strictly above the floor and IS the ⅔-common frontier")
	require.Equal(t, refs[N].ID, f.ID, "boundary: N is named iff its height is STRICTLY ABOVE MinFrontierHeight")
	require.Equal(t, uint64(N), f.Height)
}

// ----- HALTED FLEET: the own tip IS the frontier under a COMPLETE view -------
//
// The LIVENESS HOLE between the own-height exclusion above and CaughtUp. When the
// highest ⅔-backed block equals the node's OWN accepted tip AND a SUB-⅔ minority reports tips
// above it, nameFrontier names nothing (own height excluded) and CaughtUp refuses (the minority
// tips are un-held): the node neither completes nor syncs. On a LIVE network that is transient —
// the minority tip gains backing as finalization propagates. On a HALTED network it is a
// PERMANENT deadlock.
//
// THE SHAPE: 5 equal-stake validators on ONE chain — a minority two blocks ahead, a peer at the
// node's own tip, one far behind. The ahead blocks are a pure LAG, not a fork, so the node's own
// tip is an ancestor of every tip reported to it. The ahead pair's stake therefore credits the
// node's OWN height as their shared ancestor: own height clears the ⅔-of-responders floor and the
// height above it does not. Naming excludes the one block that clears (it is the node's own),
// CaughtUp refuses the tips the node does not hold, and every retry reproduces both jaws.
//
// The safety property that separates this from M1 is COVERAGE: under FULL configured-beacon
// coverage — every beacon connected AND replied — no eclipse can hide an ahead node, so the view
// is COMPLETE and the highest ⅔-backed block IS the network frontier. In M1 the 6th beacon is
// eclipsed, coverage is NOT full, and the exclusion must (and does) still apply.

// TestBootstrapTrust_HaltedFleet_OwnTipNamedUnderFullCoverage reproduces that shape exactly and
// pins BOTH halves: un-covered it deadlocks (ErrNoBootstrapQuorum), covered it names the node's
// own tip. It also pins that CaughtUp is FALSE for these replies, so the fix
// provably comes from the naming exemption and NOT from loosening CaughtUp.
func TestBootstrapTrust_HaltedFleet_OwnTipNamedUnderFullCoverage(t *testing.T) {
	const (
		own  = 5090 // the node's own last-accepted height, shared with one peer
		high = 5092 // the minority sitting two blocks ahead
		lag  = 4243 // the far-behind peer
	)
	refs, byID := refChain(high) // genesis..5092, parent-linked; 5092 descends through 5090
	b := nodeIDs(5)              // 5 equal-stake validators; b[0] is the node itself
	const w = uint64(100)        // total 500 → MinResponseWeight ⌈500/2⌉=251, MinResponses majority=3
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(b, w),
		MinResponses:      3,
		MinResponseWeight: 251,
		MinFrontierHeight: own,
		Tip:               refs[own].ID, // the node's own accepted tip, named by VALUE not just height
		Source:            &stubAncestry{byID: byID},
	}

	// All FOUR peers reply — with the node itself that accounts for the whole configured set.
	replies := []BeaconReply{
		reply(b[1], refs[high].ID, w), // two ahead
		reply(b[2], refs[high].ID, w), // two ahead
		reply(b[3], refs[own].ID, w),  // at the node's own tip
		reply(b[4], refs[lag].ID, w),  // far behind
	}

	// Sanity pins on the arithmetic (R = 400): the ahead pair carries 200, BELOW the ⅔
	// threshold, so 5092 is not nameable; 5090 carries 300 (the two 5092 tips credit it as their
	// shared ANCESTOR, plus the direct reply at that height) and DOES clear — at exactly own height.
	require.Equal(t, uint64(266), Ratio{2, 3}.floorOf(400), "⅔-of-responders floor over R=400 is 266")
	require.Less(t, uint64(200), uint64(266), "the ahead minority is BELOW the naming threshold — 5092 is not nameable")

	// THE DEADLOCK, reproduced. Without the coverage proof the own-height exclusion stands,
	// nothing above own height is ⅔-backed, and the node names nothing — every round, forever.
	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"un-covered, the own-height exclusion stands and the halted fleet names NOTHING — the live devnet bug")

	// …and CaughtUp REFUSES these same replies, because the node does not hold 5092. That is the
	// other jaw of the deadlock, and it pins that the fix below cannot be coming from CaughtUp.
	held := map[ids.ID]uint64{} // the node holds its accepted chain 0..5090, NOT 5091/5092
	for h := 0; h <= own; h++ {
		held[refs[h].ID] = uint64(h)
	}
	require.False(t, policy.CaughtUp(replies, own, heldOracle(held)),
		"the node does not hold 5092, so CaughtUp refuses — the naming exemption, not CaughtUp, is the fix")

	// THE FIX. Under FULL coverage the view is COMPLETE: every configured beacon is accounted for,
	// so no eclipse can be hiding an ahead node, and blocks above own height are held by < ⅔ of the
	// full set — never finalized. The node IS at the network's ⅔-backed frontier: name it, go Ready.
	policy.Covered = true
	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err, "under a COMPLETE view the node's own tip IS the ⅔-backed frontier")
	require.Equal(t, refs[own].ID, f.ID, "the named frontier is the node's own tip, by value")
	require.Equal(t, uint64(own), f.Height)
}

// TestBootstrapTrust_HaltedFleet_OwnTipNotNamedWhenBeaconMissing pins that COVERAGE is
// the same fleet with one beacon SILENT is not a complete view, so the own-height
// exclusion stands and the node fails safe. 5090 still clears the ⅔ floor here (300 > 200), so
// `Covered` is the only thing keeping it un-named — exactly the M1 eclipse discriminator.
func TestBootstrapTrust_HaltedFleet_OwnTipNotNamedWhenBeaconMissing(t *testing.T) {
	const (
		own  = 5090
		high = 5092
	)
	refs, byID := refChain(high)
	b := nodeIDs(5)
	const w = uint64(100)
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(b, w),
		MinResponses:      3,
		MinResponseWeight: 251,
		MinFrontierHeight: own,
		Tip:               refs[own].ID,
		// b[4] is SILENT, so repliedCovers is false and the caller computes Covered=false —
		// a beacon is missing and an ahead tip may be hidden behind it.
		Covered: false,
		Source:  &stubAncestry{byID: byID},
	}

	// Only 3 of the 4 peers answer. R = 300 (above the 251 stake floor and the 3-beacon count
	// floor, so the round is JUDGED, not skipped), ⅔ floor = 200, backing[5090] = 300 > 200.
	replies := []BeaconReply{
		reply(b[1], refs[high].ID, w),
		reply(b[2], refs[high].ID, w),
		reply(b[3], refs[own].ID, w),
		// b[4] silent — no reply.
	}
	require.Equal(t, uint64(200), Ratio{2, 3}.floorOf(300), "⅔-of-responders floor over R=300 is 200")

	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"a MISSING beacon means an incomplete view — own height stays excluded and the node fails safe")
}

// TestBootstrapTrust_HaltedFleet_SameHeightSiblingNotOwnTip pins that the exemption is by VALUE
// (id AND height), not by height alone: a ⅔-backed SIBLING at the node's own height is a FORK the
// node is not on, so the exemption's own argument ("this node sits exactly at the network's
// frontier") does not hold for it and it must NOT be named. It fails safe to CaughtUp, which
// refuses because the node does not hold the sibling — so the node syncs off its minority fork.
//
// The sibling is ⅔-backed as a shared ANCESTOR, never as a directly-reported tip: an actively
// reported ⅔ tip takes the EXACT FAST PATH, which is exempt from MinFrontierHeight by design and
// is not what this pins.
func TestBootstrapTrust_HaltedFleet_SameHeightSiblingNotOwnTip(t *testing.T) {
	const own = 5090
	refs, byID := refChain(own) // the node's own accepted chain, genesis..5090

	// A fork branching one block BELOW own height: sibling S at 5090, extended to 5092. The fleet
	// is on it; the node is not. S is at the node's height but is NOT the node's block.
	sibling := BlockRef{ID: ids.GenerateTestID(), Height: own, Parent: refs[own-1].ID}
	byID[sibling.ID] = sibling
	s1 := childRef(sibling)
	byID[s1.ID] = s1
	s2 := childRef(s1)
	byID[s2.ID] = s2

	bs := nodeIDs(5)
	const w = uint64(100)
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(bs, w),
		MinResponses:      3,
		MinResponseWeight: 251,
		MinFrontierHeight: own,
		Tip:               refs[own].ID, // the node's OWN block at 5090 — not the sibling
		Covered:           true,         // full coverage: the exemption is armed, only the VALUE check stands
		Source:            &stubAncestry{byID: byID},
	}

	// All four peers reply. R = 400, ⅔ floor 266. No single tip clears it (s2 carries 200, S 100,
	// 4243 100), so the fast path does not fire; S accrues 300 as the shared ANCESTOR of the s2
	// tips plus one direct reply — ⅔-backed, at exactly own height, and NOT the node's tip.
	replies := []BeaconReply{
		reply(bs[1], s2.ID, w),
		reply(bs[2], s2.ID, w),
		reply(bs[3], sibling.ID, w),
		reply(bs[4], refs[4243].ID, w),
	}

	_, err := policy.AcceptsFrontier(context.Background(), replies)
	require.ErrorIs(t, err, ErrNoBootstrapQuorum,
		"a same-height SIBLING is never the node's own tip — the exemption is by value, so it fails safe")

	// …and CaughtUp refuses too (the node holds neither the sibling nor its descendants), so the
	// node syncs off its minority fork instead of declaring itself at the frontier.
	held := map[ids.ID]uint64{}
	for h := 0; h <= own; h++ {
		held[refs[h].ID] = uint64(h)
	}
	require.False(t, policy.CaughtUp(replies, own, heldOracle(held)),
		"the node holds no block of the fork — it is behind the ⅔-backed chain and must sync")
}

// ----- H: HALT-SKEW RECOVERY -------------------------------------------------

// TestBootstrapTrust_H_HaltSkewDeeperThanOneWindow is the wedge the chunked descent exists for.
//
// SHAPE: a fleet halts below its α threshold. One node runs on alone while the rest stay at a
// lower height that IS an ancestor of the high tip (no fork — the low nodes' `latest` is that
// block and they hold nothing competing), so the ⅔-of-RESPONDER floor is satisfied at the common
// block by all three responders and it MUST be named.
//
// A single-fetch nameFrontier does not name it. It fetches one bootstrapNamingWindow (256) of
// ancestry per anchor, so once the skew exceeds that window the high tip's ancestry never reaches
// down far enough to vouch for the common block. The common block then holds only its 2 direct
// responders (2 of 3 = below the ⅔ floor of 2, which the strict `>` rejects), the tally names
// NOTHING, and every retry does the same — the node sits at height 0 and the fleet, already below
// α, cannot regain quorum.
//
// The gap here (535) is deliberately > one window (256) and < maxNamingDepth. With the
// single-fetch window this FAILS (frontier is nil → ErrNoBootstrapQuorum); with the chunked
// descent the walk continues past the first chunk and names the common ancestor.
func TestBootstrapTrust_H_HaltSkewDeeperThanOneWindow(t *testing.T) {
	const w uint64 = 100
	const gap = 535 // a halt skew wider than one 256-block fetch window

	// common is the last block the whole fleet accepted; `ahead` extends it by `gap`.
	refs, byID := refChain(1000)
	common := refs[1000]
	prev := common
	for i := 0; i < gap; i++ {
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
	}
	ahead := prev
	require.Equal(t, common.Height+gap, ahead.Height)
	require.Greater(t, gap, bootstrapNamingWindow, "gap MUST exceed one fetch window or this proves nothing")
	require.Less(t, gap, bootstrapMaxBlocksPerAttempt, "gap must fit one attempt's block budget")

	beacons := nodeIDs(3) // the three responders still answering
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		Source:         &stubAncestry{byID: byID},
	}
	replies := []BeaconReply{
		reply(beacons[0], ahead.ID, w),  // the node that ran on alone
		reply(beacons[1], common.ID, w), // held at the common block
		reply(beacons[2], common.ID, w), // held at the common block
	}

	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err, "a fleet split across ONE chain must name a frontier, not wedge")
	require.NotNil(t, f)
	require.Equal(t, common.ID, f.ID,
		"must name the ⅔-backed COMMON ancestor: the high tip vouches for it via ancestry")
	require.Equal(t, common.Height, f.Height)
}

// TestBootstrapTrust_H_HaltSkewBeyondDepthStillFailsSafe pins the budget's edge: a skew deeper
// than ONE ATTEMPT's block budget names nothing rather than guessing, and says so with
// ErrNamingIncomplete rather than ErrNoBootstrapQuorum. Both fail safe; only one of them is
// recoverable, because only one tells the caller that looking further would help.
func TestBootstrapTrust_H_HaltSkewBeyondDepthStillFailsSafe(t *testing.T) {
	const w uint64 = 100
	refs, byID := refChain(10)
	common := refs[10]
	prev := common
	for i := 0; i < 64; i++ {
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
	}
	ahead := prev

	beacons := nodeIDs(3)
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, w),
		MinResponses:   3,
		NamingWindow:   4, // tiny chunk...
		Source:         &stubAncestry{byID: byID},
		// ...and a per-attempt block budget far shallower than the 64-block skew.
		Budget: NamingBudget{MaxBlocks: 8},
	}
	_, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], ahead.ID, w),
		reply(beacons[1], common.ID, w),
		reply(beacons[2], common.ID, w),
	})
	require.Error(t, err, "a skew beyond the attempt budget must fail SAFE, never name a guess")
	require.ErrorIs(t, err, ErrNamingIncomplete,
		"out of budget is not the same fact as out of agreement: reported as no-quorum, a wide "+
			"gap looks permanent and the descent restarts at the tip every round")
	require.NotErrorIs(t, err, ErrNoBootstrapQuorum)
}

// TestBootstrapTrust_H_AcceptanceMatrix pins the owner-specified recovery matrix at the policy
// layer. Each case is the SAME five-validator shape with a different responder pattern.
//
// The rule being pinned is "highest verifiably-vouched descendant wins", NOT "numerically highest
// tip advertised". A tip whose ancestry does not link into the fleet's chain earns NO credit no
// matter how high it claims to be, so a Byzantine node cannot pull the fleet onto a fabricated
// chain by advertising a tall one.
func TestBootstrapTrust_H_AcceptanceMatrix(t *testing.T) {
	const w uint64 = 100
	mk := func() (BlockRef, BlockRef, map[ids.ID]BlockRef) {
		refs, byID := refChain(600)
		common := refs[600]
		prev := common
		for i := 0; i < 535; i++ { // the same halt skew, > one 256 window
			c := childRef(prev)
			byID[c.ID] = c
			prev = c
		}
		return common, prev, byID
	}

	t.Run("1_high_2_low_2_unavailable__names_common_ancestor", func(t *testing.T) {
		common, ahead, byID := mk()
		b := nodeIDs(3)
		p := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, Source: &stubAncestry{byID: byID}}
		f, err := p.AcceptsFrontier(context.Background(), []BeaconReply{
			reply(b[0], ahead.ID, w), reply(b[1], common.ID, w), reply(b[2], common.ID, w)})
		require.NoError(t, err)
		require.Equal(t, common.ID, f.ID)
	})

	t.Run("3_high_2_unavailable__names_high_tip", func(t *testing.T) {
		_, ahead, byID := mk()
		b := nodeIDs(3)
		p := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, Source: &stubAncestry{byID: byID}}
		f, err := p.AcceptsFrontier(context.Background(), []BeaconReply{
			reply(b[0], ahead.ID, w), reply(b[1], ahead.ID, w), reply(b[2], ahead.ID, w)})
		require.NoError(t, err)
		require.Equal(t, ahead.ID, f.ID, "unanimous high tip must be named, not an ancestor")
	})

	t.Run("fabricated_tall_tip_2_low__rejects_fake_names_common", func(t *testing.T) {
		common, _, byID := mk()
		// A Byzantine node advertises a tip on a chain of its own that never links into the
		// fleet's history. Its ancestry earns credit only for ITS OWN blocks, never for the
		// fleet's — so it cannot outvote the two honest low responders.
		fakeRefs, fakeByID := refChain(9_000_000)
		fake := fakeRefs[9_000_000]
		for id, r := range fakeByID {
			byID[id] = r
		}
		b := nodeIDs(3)
		p := &BootstrapPolicy{TrustedBeacons: equalBeacons(b, w), MinResponses: 3, Source: &stubAncestry{byID: byID}}
		f, err := p.AcceptsFrontier(context.Background(), []BeaconReply{
			reply(b[0], fake.ID, w), reply(b[1], common.ID, w), reply(b[2], common.ID, w)})
		// The fake tip earns credit ONLY on its own disjoint chain (100), far below the ⅔ floor
		// of 200, so it can never be named however tall it claims to be. `common` earns exactly
		// 200 from its two honest backers — and the floor is STRICT (`> floor`), so 200 is not
		// enough either: the Byzantine node withheld the third vouch by sitting on a chain that
		// does not link. Correct outcome is a SAFE HALT, not "fall back to the low tip".
		// This is the honest BFT trade: 1 of 3 responders can cost LIVENESS, never SAFETY.
		require.Error(t, err, "must halt safely; must NOT follow a taller unvouched chain")
		require.Nil(t, f)
	})

	t.Run("two_conflicting_branches_same_height__halts_safely", func(t *testing.T) {
		refs, byID := refChain(600)
		common := refs[600]
		// Two disjoint branches of equal height off the common block, each backed by one node,
		// and NO responder majority on either. Nothing may be named by height tiebreak.
		l, r := childRef(common), childRef(common)
		byID[l.ID], byID[r.ID] = l, r
		b := nodeIDs(3)
		p := &BootstrapPolicy{
			TrustedBeacons: equalBeacons(b, w), MinResponses: 3,
			MinFrontierHeight: common.Height, // node already holds `common`; only a tip AHEAD may be named
			Source:            &stubAncestry{byID: byID},
		}
		_, err := p.AcceptsFrontier(context.Background(), []BeaconReply{
			reply(b[0], l.ID, w), reply(b[1], r.ID, w), reply(b[2], common.ID, w)})
		require.Error(t, err, "conflicting equal-height branches must halt safely, never pick by height")
	})
}
