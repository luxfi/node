// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakingparams

import (
	"testing"

	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/node/utils/units"
)

// The genesis entry is what the chain was born with, and the current entry is
// what genesis now says for a NEW network. They are allowed to differ — that is
// the whole point of a history — but the current one must not fall behind
// genesis, or a network built today would admit at a floor the running chain
// no longer offers.
func TestMainnetHistoryEndsWhereGenesisStands(t *testing.T) {
	if got, want := MainnetHistory.Current().MinValidatorStake, genesis.MainnetParams.MinValidatorStake; got != want {
		t.Fatalf("current floor = %d, genesis for a new network says %d", got, want)
	}
	if got, want := MainnetHistory.Current().MaxValidatorStake, genesis.MainnetParams.MaxValidatorStake; got != want {
		t.Fatalf("current ceiling = %d, genesis says %d", got, want)
	}
}

// One unit for the node. The thresholds are stated in LUX and must resolve
// through the same constant everything else uses.
func TestUnitsAreTheNodesUnits(t *testing.T) {
	if Lux != units.Lux {
		t.Fatalf("Lux = %d, node says %d", Lux, units.Lux)
	}
	if MainnetGenesis.MinValidatorStake != 2_000*units.Lux {
		t.Fatalf("mainnet min stake = %d, want 2,000 LUX at 6 decimals", MainnetGenesis.MinValidatorStake)
	}
}

// The floor moves at the activation and nowhere else. A validator bonding one
// second before it is held to 2,000; one bonding at it is admitted at 1,000.
func TestMainnetFloorMovesAtTheActivation(t *testing.T) {
	if err := MainnetHistory.Valid(); err != nil {
		t.Fatalf("history: %v", err)
	}
	activation := MainnetHistory[1].Activation
	if got := MainnetHistory.At(activation - 1).MinValidatorStake; got != 2_000*Lux {
		t.Fatalf("one second before activation: floor = %d, want 2,000 LUX", got)
	}
	if got := MainnetHistory.At(activation).MinValidatorStake; got != 1_000*Lux {
		t.Fatalf("at activation: floor = %d, want 1,000 LUX", got)
	}
	// Nothing else moved: the change is the floor and only the floor.
	before, after := MainnetHistory.At(activation-1), MainnetHistory.At(activation)
	before.MinValidatorStake, after.MinValidatorStake = 0, 0
	if before != after {
		t.Fatalf("a field other than the floor changed at activation: %+v -> %+v", before, after)
	}
}
