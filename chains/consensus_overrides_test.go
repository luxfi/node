// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"
	"time"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
)

const hanzoL1NetworkID = 36963

func intp(i int) *int { return &i }

// A five-validator sovereign L1 must be able to reach its own quorum.
//
// selectConsensusParams keys only on networkID, so an L1 whose networkID is its
// chainID is a "value network" and falls to DefaultParams: K=20, alpha=14,
// BetaVirtuous=14. An uncontested block then needs FOURTEEN consecutive clean
// polls, any miss resetting the run — which froze Hanzo 36963 at height 3248
// while --consensus-sample-size=5 --consensus-quorum-size=4
// --consensus-commit-threshold=2 were being passed and ignored.
func TestOperatorOverridesRescueSmallValueNetwork(t *testing.T) {
	base := selectConsensusParams(true, hanzoL1NetworkID)
	if base.K != 20 || base.BetaVirtuous != 14 {
		t.Fatalf("precondition changed: base K=%d BetaVirtuous=%d, want 20/14", base.K, base.BetaVirtuous)
	}

	o := &ConsensusOverrides{K: intp(5), AlphaPreference: intp(4), AlphaConfidence: intp(4), Beta: intp(2)}
	got := o.ApplyTo(base)

	if got.K != 5 {
		t.Errorf("K = %d, want 5", got.K)
	}
	if got.AlphaConfidence != 4 {
		t.Errorf("AlphaConfidence = %d, want 4", got.AlphaConfidence)
	}
	if got.BetaVirtuous != 2 {
		t.Errorf("BetaVirtuous = %d, want 2 — an uncontested block would still need %d consecutive polls", got.BetaVirtuous, got.BetaVirtuous)
	}
	if got.AlphaConfidence > got.K {
		t.Errorf("unreachable quorum: AlphaConfidence=%d > K=%d", got.AlphaConfidence, got.K)
	}
}

// Beta must carry BetaVirtuous. The virtuous path is what an uncontested block
// takes, so setting Beta alone leaves acceptance gated at the default.
func TestOverrideBetaSetsVirtuousAndNeverLowersRogue(t *testing.T) {
	base := selectConsensusParams(true, hanzoL1NetworkID)
	got := (&ConsensusOverrides{Beta: intp(2)}).ApplyTo(base)
	if got.BetaVirtuous != 2 {
		t.Errorf("BetaVirtuous = %d, want 2", got.BetaVirtuous)
	}
	if got.BetaRogue < 2 {
		t.Errorf("BetaRogue = %d, want >= 2", got.BetaRogue)
	}

	// Raising Beta above the default BetaRogue must raise Rogue, not lower it.
	high := (&ConsensusOverrides{Beta: intp(base.BetaRogue + 5)}).ApplyTo(base)
	if high.BetaRogue != base.BetaRogue+5 {
		t.Errorf("BetaRogue = %d, want %d", high.BetaRogue, base.BetaRogue+5)
	}
}

// No overrides must be byte-for-byte the old behaviour, on every branch. This is
// what makes the change safe for existing deployments.
func TestNoOverridesLeavesEveryNetworkUntouched(t *testing.T) {
	for _, netID := range []uint32{
		constants.MainnetID, constants.TestnetID,
		constants.DevnetID, constants.LocalID,
		hanzoL1NetworkID,
	} {
		want := selectConsensusParams(true, netID)
		if got := (*ConsensusOverrides)(nil).ApplyTo(want); got != want {
			t.Errorf("networkID %d: nil override changed params", netID)
		}
		if got := (&ConsensusOverrides{}).ApplyTo(want); got != want {
			t.Errorf("networkID %d: empty override changed params", netID)
		}
	}
}

// Mainnet and testnet keep their tuned sets unless the operator says otherwise —
// the networkID switch stays the DEFAULT, it just stops being the only word.
func TestOverridesDoNotDisturbMainnetDefaults(t *testing.T) {
	mainnet := selectConsensusParams(true, constants.MainnetID)
	if mainnet.K != consensusconfig.MainnetParams().K {
		t.Fatalf("precondition: mainnet K=%d", mainnet.K)
	}
	// An operator who sets only the settle window must not disturb K/alpha/beta.
	w := 2 * time.Second
	got := (&ConsensusOverrides{ConvergenceSettleWindow: &w}).ApplyTo(mainnet)
	if got.K != mainnet.K || got.AlphaConfidence != mainnet.AlphaConfidence || got.BetaVirtuous != mainnet.BetaVirtuous {
		t.Errorf("unrelated params moved: %+v vs %+v", got, mainnet)
	}
	if got.ConvergenceSettleWindow != w {
		t.Errorf("ConvergenceSettleWindow = %v, want %v", got.ConvergenceSettleWindow, w)
	}
}

// The overridden params must survive the SAME gate the manager applies right
// after selecting them (manager.go: ValidateForValueNetwork). If they do not,
// every validator crash-loops at boot simultaneously, and nothing catches it first
// because a quorum parameter is only ever rolled fleet-wide.
//
// For K=5: f = (K-1)/3 = 1, so f>=1 holds, and the BFT quorum floor
// ceil((K+f+1)/2) = 4 is exactly the alpha 36963 runs. This asserts it rather
// than trusting that arithmetic.
func TestOverriddenParamsPassTheValueNetworkGate(t *testing.T) {
	o := &ConsensusOverrides{K: intp(5), AlphaPreference: intp(4), AlphaConfidence: intp(4), Beta: intp(2)}
	got := o.ApplyTo(selectConsensusParams(true, hanzoL1NetworkID))

	if err := got.ValidateForValueNetwork(hanzoL1NetworkID); err != nil {
		t.Fatalf("overridden params rejected by the manager's own gate: %v", err)
	}
	if f := got.ByzantineFaultTolerance(); f < 1 {
		t.Errorf("ByzantineFaultTolerance = %d, want >= 1", f)
	}
}

// A hostile override must still be refused. The override path must not become a
// way to configure an unsafe chain: K=3 gives f=0, which one Byzantine validator
// forks, and the gate has to reject it exactly as it would without overrides.
func TestOverridesCannotWeakenPastTheGate(t *testing.T) {
	o := &ConsensusOverrides{K: intp(3), AlphaPreference: intp(2), AlphaConfidence: intp(2)}
	got := o.ApplyTo(selectConsensusParams(true, hanzoL1NetworkID))

	if err := got.ValidateForValueNetwork(hanzoL1NetworkID); err == nil {
		t.Error("K=3 (f=0) was accepted for a value network; the gate must reject it")
	}
}
