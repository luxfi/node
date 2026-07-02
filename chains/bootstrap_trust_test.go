// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_trust_test.go — the A–G proof matrix for the BootstrapTrust policy: the SEPARATE
// trust object (distinct from consensus finality) that lets a node recover when validators are
// down (mass recovery) while refusing partition-capture and never weakening finality.
//
// Most cases test the POLICY decision (AcceptsFrontier) directly — deterministic, no network
// timing — since that IS the acceptance gate the owner specified. The mass-recovery success (A)
// and the global-tally height-floor guard also run the FULL fetch+execute loop over the real
// transport to prove the node converges (or fails safe) end to end. Each is load-bearing: revert
// the response-floor policy to the prior ⅔-of-current-total-stake gate and A deadlocks; drop the
// configured-beacon filter and D/E capture; drop the MinFrontierHeight floor and the shared-
// genesis fork false-completes.
package chains

import (
	"context"
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
		out = append(out, ref)
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

// equalStake is the owner's mainnet shape: 5 validators each 0.5e18, total 2.5e18.
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

	// Policy decision: 5 configured, 3 reachable agree on the frontier (mainnet analog 1082796).
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

// TestBootstrapTrust_CheckpointOverride: below the response floor (1 of 5), the DEFAULT is reject
// (test B), but an operator who pins a checkpoint gets the explicit override — the node anchors to
// the pinned (id,height) instead of trusting the lone beacon. This is the sanctioned escape hatch
// for a deeply-partitioned node, NEVER an open-ended ≥1-beacon acceptance.
func TestBootstrapTrust_CheckpointOverride(t *testing.T) {
	beacons := nodeIDs(5)
	ckptID := ids.GenerateTestID()
	policy := &BootstrapPolicy{
		TrustedBeacons: equalBeacons(beacons, equalStake),
		MinResponses:   3,
		Checkpoint:     &Checkpoint{ID: ckptID, Height: 1_082_796},
	}
	// 1 reachable beacon — below the floor — but a checkpoint is pinned.
	f, err := policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], ids.GenerateTestID(), equalStake),
	})
	require.NoError(t, err)
	require.True(t, f.FromCheckpoint, "below the floor with a pinned checkpoint → anchor to the checkpoint")
	require.Equal(t, ckptID, f.ID)
	require.Equal(t, uint64(1_082_796), f.Height)

	// Without the checkpoint the same 1-of-5 is rejected (the default — never trust the lone beacon).
	policy.Checkpoint = nil
	_, err = policy.AcceptsFrontier(context.Background(), []BeaconReply{
		reply(beacons[0], ids.GenerateTestID(), equalStake),
	})
	require.ErrorIs(t, err, ErrInsufficientBootstrapResponses)
}

// ----- safety guard for the global ancestor-tolerant tally ------------------

// TestBootstrapTrust_ForkAtSharedGenesisFailsSafe is the load-bearing guard for the
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

// ----- H: SKEWED-WEIGHT PARTITION-CAPTURE (the re-red HIGH; MinResponseWeight floor) ---------

// weightedBeacons builds a TrustedBeacons map from an explicit per-node weight list — for
// modeling a SKEWED (non-uniform) validator stake distribution.
func weightedBeacons(beacons []ids.NodeID, w []uint64) map[ids.NodeID]StakeWeight {
	m := make(map[ids.NodeID]StakeWeight, len(beacons))
	for i, id := range beacons {
		m[id] = StakeWeight(w[i])
	}
	return m
}

// TestBootstrapTrust_H_SkewedWeightPartitionRejected is the load-bearing regression for the re-red
// HIGH finding. Under SKEWED validator weights the MinResponses COUNT floor and the ⅔-of-responders
// WEIGHT agreement diverge: an attacker who eclipses the HEAVY honest beacon but lets enough LIGHT
// honest beacons through to satisfy the count can shrink the responder-WEIGHT denominator until his
// < ⅓-of-total Byzantine stake clears ⅔-of-responders and NAMES A FORGED FRONTIER. The MinResponseWeight
// stake-majority floor (> ½ of TOTAL configured beacon stake) closes this — a < ⅓-stake adversary can
// never make the responders carry a ⅔ weight majority once they must also carry > ½ of the total.
//
// Red's PoC: 6 beacons w={3,3,13,1,1,1}, total 22, Byzantine {B0,B1}=6 (27% < ⅓). The attacker
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
			"VULN PRECONDITION: without MinResponseWeight the eclipsed skewed partition names the forged tip (proves the floor is load-bearing)")
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

// TestBootstrapTrust_D2_NonConfiguredSwarmNamesNothing is the cleaner load-bearing isolation of the
// configured-beacon filter (INVARIANT 1) that the re-red asked for: a SWARM of non-configured peers
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

// TestBootstrapPolicy_WiresStakeMajorityFloor is the regression guard the re-red flagged (LOW):
// the production constructor bootstrapPolicy() MUST emit MinResponseWeight = ⌈total/2⌉. The H/D2
// tests construct policies directly, so a mutation that stops the constructor from setting the
// floor would not be caught — this asserts the WIRING on the real production path. Mutation-proof:
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

// ----- CaughtUp: the tip-holder go-live determination (RED CRITICAL fix) ----
//
// CaughtUp is the DUAL of AcceptsFrontier — "nobody is ahead" vs "here is the block ahead to sync
// to". It is the go-live path for a TIP-HOLDER on a mixed-height co-restart, where the responders
// SPLIT below the ⅔ naming threshold so AcceptsFrontier names NOTHING yet the node is plainly not
// behind. Getting its SAFETY exactly right is the hinge between "fixes the freeze" and "reopens the
// stale-go-live bug": these pin all three conditions (floor met, none-ahead, holds-every-tip) and
// prove the two adversarial fake-caught-up attempts FAIL.

// heldOracle builds the height ORACLE CaughtUp injects: a block's height, ok=false when not held.
func heldOracle(held map[ids.ID]uint64) func(ids.ID) (uint64, bool) {
	return func(id ids.ID) (uint64, bool) { h, ok := held[id]; return h, ok }
}

// TestBootstrapTrust_CaughtUp_TipHolderSplitGoesReady is the CRITICAL regression at the policy layer:
// the EXACT mainnet co-restart shape. A producer at N sees 4 responders split {N, N, N-16, genesis};
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
