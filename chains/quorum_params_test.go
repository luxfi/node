// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_params_test.go — node-layer regression for DYNAMIC live-set consensus
// sizing. selectConsensusParams sizes the committee to the LIVE validator set
// (K=N) for EVERY sybil-protected network — mainnet, testnet, devnet, localnet
// and sovereign L1s alike — with α = the strict-⅔ stake threshold derived from
// the cert verifier's rule. This replaces the retired per-tier split (mainnet
// K=21 / testnet K=11 / Default K=20) that wedged finality on a 5-validator set:
// the oversized K demanded an α the few reachable peers could never supply.
//
// It also pins: the operator's explicit committee/quorum override is honored but
// clamped UP to the dynamic floor (never below the strict-⅔ stake-cert
// threshold); single-node (--dev) selects K=1; and the live-aware value backstop
// (ValidateForLiveValueNetwork) admits the sized params while still rejecting
// genuinely non-BFT sets.
package chains

import (
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
)

// the live primary-network validator count the node passes for these nets in
// production (all three nets run 5 today). Using a realistic value exercises K=N.
const testNumValidators = 5

// TestSelectConsensusParams_SizedToLiveSet proves that for EVERY multi-validator
// (sybilProtection==true) network the node sizes K to the live validator count
// and α to the strict-⅔ threshold — one dynamic path, no per-tier schedule. The
// 5-validator case is the fleet outage fix: K=5/α=4 (NOT mainnet K=21/α=15 or
// testnet K=11/α=8), the fastest viable BFT setting that a 5-peer set can satisfy.
func TestSelectConsensusParams_SizedToLiveSet(t *testing.T) {
	cases := []struct {
		name      string
		networkID uint32
	}{
		{"mainnet", constants.MainnetID},
		{"testnet", constants.TestnetID},
		{"devnet", constants.DevnetID},
		{"localnet", constants.LocalID},
		{"sovereign-L1", 8675309},
		{"arbitrary-value-net", 424242},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := selectConsensusParams(true /* sybilProtection */, tc.networkID, testNumValidators, ConsensusOverride{})

			// Sized to the live set: K=5, α=4 (strict-⅔ of 5).
			if p.K != testNumValidators {
				t.Fatalf("net %q: K=%d, want K=N=%d (sized to live set)", tc.name, p.K, testNumValidators)
			}
			if p.AlphaPreference != 4 || p.AlphaConfidence != 4 {
				t.Fatalf("net %q: α=%d, want 4 (4-of-5 finalizes, 1 laggard tolerated)", tc.name, p.AlphaPreference)
			}
			// Byzantine-fault-tolerant (f≥1 ⟹ K≥4) — never the CRITICAL-2 K=3 fork set.
			if p.ByzantineFaultTolerance() < 1 {
				t.Fatalf("net %q got non-BFT params K=%d f=%d", tc.name, p.K, p.ByzantineFaultTolerance())
			}
			// Selected params pass Valid() (the BFT α-floor 2α−K ≥ f+1).
			if err := p.Valid(); err != nil {
				t.Fatalf("net %q params fail Valid(): %v (K=%d α=%d)", tc.name, err, p.K, p.AlphaPreference)
			}
			// And clear the LIVE-aware value backstop the manager asserts.
			if err := p.ValidateForLiveValueNetwork(tc.networkID, testNumValidators); err != nil {
				t.Fatalf("net %q params rejected by live backstop (liveN=%d): %v", tc.name, testNumValidators, err)
			}
		})
	}
}

// TestSelectConsensusParams_RecomputesForLargerSet proves the committee
// recomputes from the live count: a 21-validator set yields K=21/α=15 — 15, not
// 14, because 14/21 does not strictly exceed ⅔. No hardcoded schedule.
func TestSelectConsensusParams_RecomputesForLargerSet(t *testing.T) {
	p := selectConsensusParams(true, constants.MainnetID, 21, ConsensusOverride{})
	if p.K != 21 || p.AlphaPreference != 15 {
		t.Fatalf("21-validator mainnet: want K=21/α=15 (strict >⅔), got K=%d/α=%d", p.K, p.AlphaPreference)
	}
	if err := p.ValidateForLiveValueNetwork(constants.MainnetID, 21); err != nil {
		t.Fatalf("K=21/α=15 must clear the live backstop on a 21-validator mainnet: %v", err)
	}
}

// TestSelectConsensusParams_SmallSetClampsToBFTFloor proves a tiny set (1-3
// validators, or "set not yet known"=0) clamps to K=4 so it still clears the
// value-network BFT floor (K≥4, f≥1). Such a set degrades to near-unanimous fault
// tolerance but remains safe and Valid.
func TestSelectConsensusParams_SmallSetClampsToBFTFloor(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3} {
		p := selectConsensusParams(true, constants.DevnetID, n, ConsensusOverride{})
		if p.K != 4 {
			t.Fatalf("%d validators must clamp to K=4 (value BFT floor), got K=%d", n, p.K)
		}
		if p.ByzantineFaultTolerance() < 1 {
			t.Fatalf("%d validators: clamped K=%d not BFT (f=%d)", n, p.K, p.ByzantineFaultTolerance())
		}
		if err := p.ValidateForLiveValueNetwork(constants.DevnetID, n); err != nil {
			t.Fatalf("%d validators: clamped params must pass live backstop, got %v", n, err)
		}
	}
}

// TestSelectConsensusParams_HonorsOverrideClampedUp proves the operator's
// explicit committee/quorum is honored but never lowers safety:
//   - a quorum override BELOW the strict-⅔ floor is clamped UP (e.g. operator
//     pins α=14 for a 21-set → node raises to 15, the cert threshold);
//   - a quorum override ABOVE the floor is honored as-is;
//   - a sample-size override is honored but never exceeds the live set.
func TestSelectConsensusParams_HonorsOverrideClampedUp(t *testing.T) {
	// Operator under-sets α to the BFT-overlap value 14 on a 21-validator mainnet.
	// The node MUST clamp up to the strict-⅔ value 15 (never below the cert floor).
	p := selectConsensusParams(true, constants.MainnetID, 21, ConsensusOverride{QuorumSize: 14})
	if p.AlphaPreference != 15 {
		t.Fatalf("operator α=14 on a 21-set must clamp UP to the strict-⅔ floor 15, got %d", p.AlphaPreference)
	}

	// Operator pins a HIGHER α (16) — honored (operator may be more conservative).
	p = selectConsensusParams(true, constants.MainnetID, 21, ConsensusOverride{QuorumSize: 16})
	if p.AlphaPreference != 16 {
		t.Fatalf("operator α=16 (above floor) must be honored, got %d", p.AlphaPreference)
	}

	// Sample-size override is clamped to the live set (cannot sample 9 of 5).
	p = selectConsensusParams(true, constants.DevnetID, 5, ConsensusOverride{SampleSize: 9})
	if p.K != 5 {
		t.Fatalf("sample-size override must not exceed the live set of 5, got K=%d", p.K)
	}

	// An α override below the dynamic floor for the 5-set is clamped to 4.
	p = selectConsensusParams(true, constants.DevnetID, 5, ConsensusOverride{QuorumSize: 2})
	if p.AlphaPreference != 4 {
		t.Fatalf("operator α=2 on a 5-set must clamp UP to the strict-⅔ floor 4, got %d", p.AlphaPreference)
	}
	if err := p.Valid(); err != nil {
		t.Fatalf("override-clamped params must pass Valid(): %v", err)
	}
}

// TestSelectConsensusParams_SingleNodeIsK1 proves --dev / sybil-disabled selects
// the K=1 single-validator regime (the sole validator's accept is the 1-of-1
// quorum) — and NOT a multi-node BFT set.
func TestSelectConsensusParams_SingleNodeIsK1(t *testing.T) {
	p := selectConsensusParams(false /* sybilProtection */, constants.LocalID, 5, ConsensusOverride{})
	if p.K != 1 {
		t.Fatalf("sybil-disabled (single-node) must select K=1, got K=%d", p.K)
	}
	_ = consensusconfig.SingleValidatorParams // referenced regime
}
