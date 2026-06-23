// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_params_test.go — CRITICAL-2 node-layer regression: the node must NEVER
// wire a non-BFT consensus param set for a multi-validator (sybil-protected)
// chain. The round-1 hole was manager.go selecting LocalParams() (K=3/α=2 → f=0,
// CFT) for ALL multi-node nets — a single Byzantine validator forks K=3/α=2.
// These tests pin selectConsensusParams to a BFT-safe set for every multi-node
// network and prove the value-network backstop (ValidateForValueNetwork) accepts
// the selected params.
//
// LIVENESS regression (this revision): local-dev params are sized to the LIVE
// validator set (K=numValidators, α=bftSafeAlpha(K)). A hardcoded K=4 on a
// 5-validator set wedged finality at height 0 — the gossiper proposer
// self-filters its K-sample, so K=4 polled only 3 peers while α=3 demanded all 3,
// and one lagging validator made the quorum unreachable. K=N polls the whole set
// so α has BFT slack.
package chains

import (
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
)

// the live primary-network validator count the node passes for these nets in
// production (devnet runs 5). Using a realistic value exercises the K=N path.
const testNumValidators = 5

// TestSelectConsensusParams_MultiNodeIsBFT proves that for EVERY multi-validator
// (sybilProtection==true) network the node selects a Byzantine-fault-tolerant
// param set (f≥1, i.e. K≥4) that also clears the value-network validator — and
// that it is NEVER LocalParams (K=3) or any K<4 set. This is the node half of
// CRITICAL-2 (the consensus half is config.ValidateForValueNetwork, tested in
// the consensus module).
func TestSelectConsensusParams_MultiNodeIsBFT(t *testing.T) {
	local := consensusconfig.LocalParams()

	cases := []struct {
		name      string
		networkID uint32
	}{
		{"mainnet", constants.MainnetID},
		{"testnet", constants.TestnetID},
		{"localnet-multinode", constants.LocalID},
		{"unittest-multinode", constants.UnitTestID},
		{"arbitrary-multinode", 424242},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := selectConsensusParams(true /* sybilProtection */, tc.networkID, testNumValidators)

			// MUST be Byzantine-fault-tolerant: f≥1 ⟹ K≥4.
			if p.ByzantineFaultTolerance() < 1 {
				t.Fatalf("multi-node net %q got non-BFT params K=%d f=%d (CRITICAL-2: a single faulty validator forks)",
					tc.name, p.K, p.ByzantineFaultTolerance())
			}
			// MUST NOT be the CFT LocalParams (K=3/α=2) that was the round-1 hole.
			if p.K == local.K && p.AlphaPreference == local.AlphaPreference && p.K == 3 {
				t.Fatalf("multi-node net %q selected LocalParams (K=3/α=2) — the CRITICAL-2 fork config", tc.name)
			}
			// The selected params MUST themselves pass Valid() (the BFT α-floor:
			// 2·AlphaPreference − K ≥ f+1).
			if err := p.Valid(); err != nil {
				t.Fatalf("multi-node net %q selected params fail Valid(): %v (K=%d α=%d)",
					tc.name, err, p.K, p.AlphaPreference)
			}
		})
	}
}

// TestSelectConsensusParams_LocalDevSizedToSet proves that an explicitly-local
// dev network (devnet 3 / localnet 1337) selects a committee SIZED TO THE LIVE
// VALIDATOR SET (K=numValidators, α=bftSafeAlpha(K)) — satisfiable by that many
// localhost validators — and NOT the large Default (K=20/α=14), whose α=14 quorum
// is unreachable with a handful of validators (the freeze-at-height-0 bug). The
// 5-validator case is the EXACT liveness bug this fixes: K must be 5 (not a
// hardcoded 4) so the self-filtered proposer polls all 4 peers and α=4 has slack.
func TestSelectConsensusParams_LocalDevSizedToSet(t *testing.T) {
	for _, networkID := range []uint32{constants.DevnetID, constants.LocalID} {
		p := selectConsensusParams(true /* sybilProtection */, networkID, 5)

		// THE FIX: a 5-validator local net gets K=5/α=4, NOT the hardcoded K=4/α=3
		// that wedged finality (proposer polled 3, α=3 needed all 3).
		if p.K != 5 || p.AlphaPreference != 4 {
			t.Fatalf("local dev net %d with 5 validators: want K=5/α=4 (sized to set), got K=%d/α=%d",
				networkID, p.K, p.AlphaPreference)
		}
		// Satisfiable on a small committee: α must be small (≤5), NOT 14.
		if p.AlphaPreference > p.K || p.K > 5 {
			t.Fatalf("local dev net %d: committee not sized to set (got K=%d/α=%d)", networkID, p.K, p.AlphaPreference)
		}
		// Still genuinely BFT (f≥1) — does NOT regress CRITICAL-2.
		if p.ByzantineFaultTolerance() < 1 {
			t.Fatalf("local dev net %d: K=%d is not BFT (f=%d)", networkID, p.K, p.ByzantineFaultTolerance())
		}
		// The BFT α-floor (2·α − K ≥ f+1) must hold.
		if err := p.Valid(); err != nil {
			t.Fatalf("local dev net %d: sized params fail Valid(): %v (K=%d α=%d)", networkID, err, p.K, p.AlphaPreference)
		}
		// Must clear the value backstop the manager asserts before engine start.
		if err := p.ValidateForValueNetwork(networkID); err != nil {
			t.Fatalf("local dev net %d: sized params must pass the value backstop, got %v", networkID, err)
		}
	}

	// A custom value L1 with a high networkID is NOT a local dev network: it must
	// keep the large Default set (the isLocalDevNetwork predicate is EXACT, not a
	// >=1337 range that would wrongly catch value L1s).
	const customValueL1 = uint32(909090)
	if p := selectConsensusParams(true, customValueL1, 5); p.K != consensusconfig.DefaultParams().K {
		t.Fatalf("custom value L1 %d must get Default K=%d, got K=%d (must NOT match the local-dev path)",
			customValueL1, consensusconfig.DefaultParams().K, p.K)
	}
}

// TestSelectConsensusParams_LocalDevSmallSetClampsToBFTFloor proves that a tiny
// local set (1-3 validators, or "set not yet known"=0) clamps to K=4 so it still
// clears the value-network BFT floor (K≥4, f≥1). Such a set degrades to
// near-unanimous fault tolerance but remains safe and Valid.
func TestSelectConsensusParams_LocalDevSmallSetClampsToBFTFloor(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3} {
		p := selectConsensusParams(true, constants.DevnetID, n)
		if p.K != 4 {
			t.Fatalf("local dev with %d validators must clamp to K=4 (value BFT floor), got K=%d", n, p.K)
		}
		if p.ByzantineFaultTolerance() < 1 {
			t.Fatalf("local dev with %d validators: clamped K=%d not BFT (f=%d)", n, p.K, p.ByzantineFaultTolerance())
		}
		if err := p.ValidateForValueNetwork(constants.DevnetID); err != nil {
			t.Fatalf("local dev with %d validators: clamped params must pass value backstop, got %v", n, err)
		}
	}
}

// TestBFTSafeAlpha pins the safe-quorum formula to the SAME values the consensus
// engine's α-floor admits and the operator's BFTSafeAlpha emits — one definition
// of "safe quorum" so node and operator never disagree. For each K the returned α
// must satisfy 2·α − K ≥ f+1 (the engine bound) and be the SMALLEST such α.
func TestBFTSafeAlpha(t *testing.T) {
	// α = ceil((K + f + 1)/2), f = ⌊(K-1)/3⌋ — the minimal α satisfying 2α−K ≥ f+1.
	cases := []struct {
		k, wantAlpha int
	}{
		{1, 1}, {3, 2}, {4, 3}, {5, 4}, {7, 5}, {11, 8}, {20, 14}, {21, 14},
	}
	for _, tc := range cases {
		got := bftSafeAlpha(tc.k)
		if got != tc.wantAlpha {
			t.Fatalf("bftSafeAlpha(%d) = %d, want %d", tc.k, got, tc.wantAlpha)
		}
		f := (tc.k - 1) / 3
		if 2*got-tc.k < f+1 {
			t.Fatalf("bftSafeAlpha(%d)=%d violates BFT α-floor 2α−K≥f+1 (2*%d-%d=%d < %d)",
				tc.k, got, got, tc.k, 2*got-tc.k, f+1)
		}
		// smallest: α−1 must FAIL the bound (for K≥3 where a smaller α exists)
		if got > 1 && 2*(got-1)-tc.k >= f+1 {
			t.Fatalf("bftSafeAlpha(%d)=%d is not minimal: α=%d also satisfies the floor", tc.k, got, got-1)
		}
	}
}

// TestSelectConsensusParams_SingleNodeIsK1 proves --dev / sybil-disabled selects
// the K=1 single-validator regime (the sole validator's accept is the 1-of-1
// quorum) — and NOT a multi-node BFT set.
func TestSelectConsensusParams_SingleNodeIsK1(t *testing.T) {
	p := selectConsensusParams(false /* sybilProtection */, constants.LocalID, 5)
	if p.K != 1 {
		t.Fatalf("sybil-disabled (single-node) must select K=1, got K=%d", p.K)
	}
}

// TestSelectConsensusParams_ValueBackstop proves the params selected for a
// multi-node net pass the STRICTER value-network validator for that net — the
// fail-closed backstop asserted at the manager call site before starting the
// engine. (Mainnet enforces K≥11, so MainnetParams K=21 passes; Default K=20
// passes for an arbitrary value net.)
func TestSelectConsensusParams_ValueBackstop(t *testing.T) {
	cases := []struct {
		name      string
		networkID uint32
	}{
		{"mainnet", constants.MainnetID},
		{"arbitrary-value-net", 909090},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := selectConsensusParams(true, tc.networkID, testNumValidators)
			if err := p.ValidateForValueNetwork(tc.networkID); err != nil {
				t.Fatalf("selected params for value net %q must pass the value backstop, got %v (K=%d)",
					tc.name, err, p.K)
			}
		})
	}
}
