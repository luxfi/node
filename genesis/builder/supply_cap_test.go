// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"strings"
	"testing"

	"github.com/luxfi/constants"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/ids"
)

// mkUniformAllocs builds n allocations each funded with amt base units at a
// distinct UTXO/EVM address. No unlockSchedule — so the supply accounting
// counts InitialAmount directly (the devnet-style spendable-UTXO shape).
func mkUniformAllocs(amt uint64, n int) []genesiscfg.Allocation {
	out := make([]genesiscfg.Allocation, n)
	for i := range out {
		var addr ids.ShortID
		addr[0] = byte(i)
		addr[1] = byte(i >> 8)
		out[i] = genesiscfg.Allocation{
			EVMAddr:       addr,
			UTXOAddr:      addr,
			InitialAmount: amt,
		}
	}
	return out
}

// TestFromConfig_SupplyCapInvariant locks the fail-closed monetary invariant
// in FromConfig: the genesis allocation total must never exceed the network's
// declared supply cap, and a total that overflows uint64 must be a hard build
// error rather than a silently-wrapped value that slips under the cap.
//
// This is the regression guard for the mainnet genesis supply overshoot: 100
// allocations of 5e17 base units (stale 9-decimal magnitudes read under the
// 6-decimal unit) summed to 50T LUX = 25x the 2T cap AND overflowed uint64.
func TestFromConfig_SupplyCapInvariant(t *testing.T) {
	const teraLux = uint64(1_000_000_000_000_000_000) // 1T LUX at 6 decimals
	cap := GetStakingConfig(constants.MainnetID).RewardConfig.SupplyCap
	if cap == 0 {
		t.Fatal("mainnet SupplyCap is 0 — invariant would be a no-op")
	}
	if cap != 2*teraLux {
		t.Fatalf("unexpected mainnet SupplyCap: got %d want %d (2T)", cap, 2*teraLux)
	}

	t.Run("overflow_50T_refused", func(t *testing.T) {
		// The exact bug: 100 x 5e17 = 5e19 base units, which overflows uint64.
		cfg := &genesiscfg.Config{
			NetworkID:   constants.MainnetID,
			Allocations: mkUniformAllocs(500_000_000_000_000_000, 100),
		}
		_, _, err := FromConfig(cfg)
		if err == nil {
			t.Fatal("expected refusal of the 50T overshoot, got nil error")
		}
		if !strings.Contains(err.Error(), "overflow") {
			t.Fatalf("expected overflow error, got: %v", err)
		}
	})

	t.Run("over_cap_3T_refused", func(t *testing.T) {
		// 3T base units: over the 2T cap but does not overflow uint64, so it
		// must be caught by the cap comparison, not the overflow guard.
		cfg := &genesiscfg.Config{
			NetworkID:   constants.MainnetID,
			Allocations: mkUniformAllocs(3*teraLux, 1),
		}
		_, _, err := FromConfig(cfg)
		if err == nil {
			t.Fatal("expected refusal of the 3T overshoot, got nil error")
		}
		if !strings.Contains(err.Error(), "supply cap") {
			t.Fatalf("expected supply cap error, got: %v", err)
		}
	})

	t.Run("at_cap_2T_allowed", func(t *testing.T) {
		// Exactly at the cap must be permitted (<= is the contract).
		cfg := &genesiscfg.Config{
			NetworkID:   constants.MainnetID,
			Allocations: mkUniformAllocs(2*teraLux, 1),
		}
		_, _, err := FromConfig(cfg)
		if err != nil && (strings.Contains(err.Error(), "supply cap") || strings.Contains(err.Error(), "overflow")) {
			t.Fatalf("2T (== cap) wrongly tripped the monetary invariant: %v", err)
		}
	})

	t.Run("corrected_50B_allowed", func(t *testing.T) {
		// The canonical fix: 1000 x 5e13 = 5e16 base units = 50B LUX, well
		// under the 2T cap. Must clear the supply invariant (any later build
		// error from missing stakers is unrelated and tolerated here).
		cfg := &genesiscfg.Config{
			NetworkID:   constants.MainnetID,
			Allocations: mkUniformAllocs(50_000_000_000_000, 1000),
		}
		_, _, err := FromConfig(cfg)
		if err != nil && (strings.Contains(err.Error(), "supply cap") || strings.Contains(err.Error(), "overflow")) {
			t.Fatalf("corrected 50B set wrongly tripped the monetary invariant: %v", err)
		}
	})
}
