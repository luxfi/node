// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum_params_test.go — CRITICAL-2 node-layer regression: the node must NEVER
// wire a non-BFT consensus param set for a multi-validator (sybil-protected)
// chain. The round-1 hole was manager.go selecting LocalParams() (K=3/α=2 → f=0,
// CFT) for ALL multi-node nets — a single Byzantine validator forks K=3/α=2.
// These tests pin selectConsensusParams to a BFT-safe set for every multi-node
// network and prove the value-network backstop (ValidateForValueNetwork) accepts
// the selected params.
package chains

import (
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
)

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
			p := selectConsensusParams(true /* sybilProtection */, tc.networkID)

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

// TestSelectConsensusParams_SingleNodeIsK1 proves --dev / sybil-disabled selects
// the K=1 single-validator regime (the sole validator's accept is the 1-of-1
// quorum) — and NOT a multi-node BFT set.
func TestSelectConsensusParams_SingleNodeIsK1(t *testing.T) {
	p := selectConsensusParams(false /* sybilProtection */, constants.LocalID)
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
			p := selectConsensusParams(true, tc.networkID)
			if err := p.ValidateForValueNetwork(tc.networkID); err != nil {
				t.Fatalf("selected params for value net %q must pass the value backstop, got %v (K=%d)",
					tc.name, err, p.K)
			}
		})
	}
}
