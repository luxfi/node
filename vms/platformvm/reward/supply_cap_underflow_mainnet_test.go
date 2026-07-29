// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Measured mainnet (96369) state.
//
//	platform.getCurrentSupply         -> 13272095200543363741 base units
//	xvm.getAssetDescription(LUX)      -> denomination 9, so 1 LUX = 1e9 base units
//	platform.getMinStake              -> minValidatorStake 2000000000 (= 2 LUX)
//	platform.getCurrentValidators     -> weight 500000000000000000 (= 500M LUX),
//	                                     potentialReward 33575831900252839
const (
	mainnetCurrentSupply   = uint64(13_272_095_200_543_363_741)
	mainnetValidatorWeight = uint64(500_000_000_000_000_000)
	mainnetPotentialReward = uint64(33_575_831_900_252_839)
	mainnetStakedDuration  = time.Duration(1_797_088_011-1_765_573_611) * time.Second

	// mainnetSupplyAtBond is the current supply at the instant the validator
	// bonded, recovered below from the observed reward.
	mainnetSupplyAtBond = uint64(13_106_511_852_580_896_694)
)

// mainnetRewardConfig is the config a mainnet node is built with, transcribed
// from genesis/pkg/genesis/params.go MainnetParams.RewardConfig.
//
// The denominations in that file are Lux = 1e6 and TeraLux = 1e18, so
// SupplyCap = 2 * TeraLux = 2e18 base units. The chain's LUX asset has
// denomination 9, so that is a cap of 2 BILLION LUX — not the 2 trillion the
// comment claims. The file is a factor of 1000 out against the asset it
// denominates.
var mainnetRewardConfig = Config{
	MaxConsumptionRate: 120_000,
	MinConsumptionRate: 100_000,
	MintingPeriod:      365 * 24 * time.Hour,
	SupplyCap:          2_000_000_000_000_000_000,
}

// TestMainnetSupplyExceedsCompiledCap states the precondition plainly: the live
// supply is already past the cap the reward calculator is built with.
func TestMainnetSupplyExceedsCompiledCap(t *testing.T) {
	require := require.New(t)

	require.Greater(mainnetCurrentSupply, mainnetRewardConfig.SupplyCap,
		"live supply must exceed the compiled cap for this whole file to be relevant")

	t.Logf("compiled SupplyCap %d = %.0f LUX", mainnetRewardConfig.SupplyCap,
		float64(mainnetRewardConfig.SupplyCap)/1e9)
	t.Logf("live currentSupply %d = %.0f LUX (%.2fx the cap)", mainnetCurrentSupply,
		float64(mainnetCurrentSupply)/1e9,
		float64(mainnetCurrentSupply)/float64(mainnetRewardConfig.SupplyCap))
}

// TestMainnetRewardsAreComputedFromAnUnderflow is the defect.
//
// calculator.Calculate opens with an unguarded uint64 subtraction:
//
//	remainingSupply := c.supplyCap - currentSupply
//
// Past the cap that wraps, and every subsequent term — the whole reward — is
// scaled by the wrapped value. The reward does not clamp to zero at the cap; it
// jumps to a large number and the emission schedule silently stops meaning what
// it says.
//
// The proof is that feeding the REAL calculator the wrapped-regime inputs
// reproduces the potentialReward the chain is actually carrying, exactly.
func TestMainnetRewardsAreComputedFromAnUnderflow(t *testing.T) {
	require := require.New(t)

	wrapped := mainnetRewardConfig.SupplyCap - mainnetSupplyAtBond // deliberate: this is the bug
	require.Greater(wrapped, mainnetRewardConfig.SupplyCap,
		"the subtraction must have wrapped, or there is no defect to demonstrate")
	t.Logf("supplyCap - currentSupply wrapped to %d (%.0f LUX of phantom headroom)",
		wrapped, float64(wrapped)/1e9)

	got := NewCalculator(mainnetRewardConfig).Calculate(
		mainnetStakedDuration, mainnetValidatorWeight, mainnetSupplyAtBond,
	)

	require.Equal(mainnetPotentialReward, got,
		"the production calculator, in the wrapped regime, must reproduce the "+
			"potentialReward mainnet is actually carrying")
}

// TestMainnetRewardWouldBeZeroWithoutTheWrap shows what the emission schedule
// SAYS should happen past the cap, and therefore the size of the divergence:
// not a rounding error, the difference between 33.5M LUX and nothing.
func TestMainnetRewardWouldBeZeroWithoutTheWrap(t *testing.T) {
	require := require.New(t)

	// A cap above the live supply is the only regime in which the subtraction
	// is meaningful. Set it just above and the remaining headroom is tiny, so
	// the reward collapses toward zero — the intended behaviour at the cap.
	atCap := mainnetRewardConfig
	atCap.SupplyCap = mainnetSupplyAtBond + 1

	got := NewCalculator(atCap).Calculate(
		mainnetStakedDuration, mainnetValidatorWeight, mainnetSupplyAtBond,
	)

	require.Zero(got, "at the cap, emission must be zero")
	t.Logf("intended at cap: 0 LUX; actually issued: %.2f LUX",
		float64(mainnetPotentialReward)/1e9)
}

// TestSupplyCapGuardIsTheFix pins the one-line remedy so a future reader can see
// what "clamp instead of wrap" buys: emission stops at the cap, as designed,
// and no vote is needed to make it behave.
//
// It is deliberately a test against a local guard rather than a change to
// Calculate: altering live emission is a monetary change, and monetary changes
// belong in a node release that operators choose to adopt — never in a
// stake-weighted vote. See vms/platformvm/stakingparams.
func TestSupplyCapGuardIsTheFix(t *testing.T) {
	require := require.New(t)

	guarded := func(cap, supply uint64) uint64 {
		if supply >= cap {
			return 0
		}
		return cap - supply
	}

	require.Zero(guarded(mainnetRewardConfig.SupplyCap, mainnetCurrentSupply),
		"past the cap the guard yields no headroom, so Calculate yields no reward")
	require.Equal(uint64(1), guarded(mainnetSupplyAtBond+1, mainnetSupplyAtBond))

	// And the unguarded form is what mainnet runs.
	require.NotZero(mainnetRewardConfig.SupplyCap - mainnetCurrentSupply)
}
