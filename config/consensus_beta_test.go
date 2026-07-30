// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

// --consensus-commit-threshold must set the VIRTUOUS threshold too, not just Beta.
//
// A block with no conflict is accepted on the virtuous path, so BetaVirtuous is
// what gates it. Nothing else in getConsensusConfig writes BetaVirtuous or
// BetaRogue, so before this fix they kept DefaultParams' 14/20 whatever the
// operator passed — and Hanzo L1 36963 halted on exactly that: effective params
// K=5 AlphaPreference=4 AlphaConfidence=4 Beta=2 BetaVirtuous=14, meaning an
// uncontested block all five validators had verified needed FOURTEEN consecutive
// successful polls, any single miss resetting the run.
func TestCommitThresholdSetsVirtuousAndRogue(t *testing.T) {
	v := viper.New()
	v.Set(ConsensusCommitThresholdKey, 2)

	p := getConsensusConfig(v)

	if p.Beta != 2 {
		t.Errorf("Beta = %d, want 2", p.Beta)
	}
	if p.BetaVirtuous != 2 {
		t.Errorf("BetaVirtuous = %d, want 2 — an uncontested block would need %d consecutive polls", p.BetaVirtuous, p.BetaVirtuous)
	}
	if p.BetaRogue < 2 {
		t.Errorf("BetaRogue = %d, want >= Beta(2)", p.BetaRogue)
	}
}

// Unset must not disturb the defaults.
func TestCommitThresholdUnsetKeepsDefaults(t *testing.T) {
	def := getConsensusConfig(viper.New())
	if def.BetaVirtuous <= 0 || def.BetaRogue <= 0 {
		t.Fatalf("defaults look wrong: BetaVirtuous=%d BetaRogue=%d", def.BetaVirtuous, def.BetaRogue)
	}
}

// The convergence settle window must be reachable from configuration.
//
// Same shape as BetaVirtuous above: the field exists and the engine honours it,
// but nothing wrote it, and for the PRIMARY network there is no other path — the
// net-config file is only read for TrackedChains, and naming the primary network
// there is rejected ("cannot track primary network"). The auto value is derived
// from a round budget rather than measured latency, max(RoundTO/2, 150ms), which
// on Hanzo L1 36963 is 150ms against ~560ms real inter-validator latency: every
// validator signs its own block and the height never reaches the quorum.
func TestConvergenceSettleWindowIsSettable(t *testing.T) {
	v := viper.New()
	v.Set(ConsensusConvergenceSettleWindowKey, 2*time.Second)

	p := getConsensusConfig(v)

	if p.ConvergenceSettleWindow != 2*time.Second {
		t.Errorf("ConvergenceSettleWindow = %v, want 2s — the operator value never reached the engine", p.ConvergenceSettleWindow)
	}
}

// Unset must leave the engine's auto derivation in charge (zero value).
func TestConvergenceSettleWindowUnsetStaysAuto(t *testing.T) {
	if w := getConsensusConfig(viper.New()).ConvergenceSettleWindow; w != 0 {
		t.Errorf("ConvergenceSettleWindow = %v, want 0 so the engine keeps its auto derivation", w)
	}
}

// A commit threshold above the default BetaRogue must raise it, never lower it.
func TestCommitThresholdNeverLowersBetaRogue(t *testing.T) {
	def := getConsensusConfig(viper.New())

	v := viper.New()
	v.Set(ConsensusCommitThresholdKey, def.BetaRogue+5)
	p := getConsensusConfig(v)

	if p.BetaRogue != def.BetaRogue+5 {
		t.Errorf("BetaRogue = %d, want %d", p.BetaRogue, def.BetaRogue+5)
	}
}
