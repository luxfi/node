// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package reward

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/luxd/utils/units"
)

const (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultMinValidatorStake = 5 * units.MilliLux
)

var defaultConfig = Config{
	MaxConsumptionRate: .12 * PercentDenominator,
	MinConsumptionRate: .10 * PercentDenominator,
	MintingPeriod:      365 * 24 * time.Hour,
	SupplyCap:          720 * units.MegaLux,
}

func TestLongerDurationBonus(t *testing.T) {
	c := NewCalculator(defaultConfig)
	shortDuration := 24 * time.Hour
	totalDuration := 365 * 24 * time.Hour
	shortBalance := units.KiloLux
	for i := 0; i < int(totalDuration/shortDuration); i++ {
		r := c.Calculate(shortDuration, shortBalance, 359*units.MegaLux+shortBalance)
		shortBalance += r
	}
	r := c.Calculate(totalDuration%shortDuration, shortBalance, 359*units.MegaLux+shortBalance)
	shortBalance += r

	longBalance := units.KiloLux
	longBalance += c.Calculate(totalDuration, longBalance, 359*units.MegaLux+longBalance)

	if shortBalance >= longBalance {
		t.Fatalf("should promote stakers to stake longer")
	}
}

func TestRewards(t *testing.T) {
	c := NewCalculator(defaultConfig)
	tests := []struct {
		duration       time.Duration
		stakeAmount    uint64
		existingAmount uint64
		expectedReward uint64
	}{
		// Max duration:
		{ // (720M - 360M) * (1M / 360M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    units.MegaLux,
			existingAmount: 360 * units.MegaLux,
			expectedReward: 120 * units.KiloLux,
		},
		{ // (720M - 400M) * (1M / 400M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    units.MegaLux,
			existingAmount: 400 * units.MegaLux,
			expectedReward: 96 * units.KiloLux,
		},
		{ // (720M - 400M) * (2M / 400M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    2 * units.MegaLux,
			existingAmount: 400 * units.MegaLux,
			expectedReward: 192 * units.KiloLux,
		},
		{ // (720M - 720M) * (1M / 720M) * 12%
			duration:       defaultMaxStakingDuration,
			stakeAmount:    units.MegaLux,
			existingAmount: defaultConfig.SupplyCap,
			expectedReward: 0,
		},
		// Min duration:
		// (720M - 360M) * (1M / 360M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    units.MegaLux,
			existingAmount: 360 * units.MegaLux,
			expectedReward: 274122724713,
		},
		// (720M - 360M) * (.005 / 360M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    defaultMinValidatorStake,
			existingAmount: 360 * units.MegaLux,
			expectedReward: 1370,
		},
		// (720M - 400M) * (1M / 400M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    units.MegaLux,
			existingAmount: 400 * units.MegaLux,
			expectedReward: 219298179771,
		},
		// (720M - 400M) * (2M / 400M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    2 * units.MegaLux,
			existingAmount: 400 * units.MegaLux,
			expectedReward: 438596359542,
		},
		// (720M - 720M) * (1M / 720M) * (10% + 2% * MinimumStakingDuration / MaximumStakingDuration) * MinimumStakingDuration / MaximumStakingDuration
		{
			duration:       defaultMinStakingDuration,
			stakeAmount:    units.MegaLux,
			existingAmount: defaultConfig.SupplyCap,
			expectedReward: 0,
		},
	}
	for _, test := range tests {
		name := fmt.Sprintf("reward(%s,%d,%d)==%d",
			test.duration,
			test.stakeAmount,
			test.existingAmount,
			test.expectedReward,
		)
		t.Run(name, func(t *testing.T) {
			r := c.Calculate(
				test.duration,
				test.stakeAmount,
				test.existingAmount,
			)
			if r != test.expectedReward {
				t.Fatalf("expected %d; got %d", test.expectedReward, r)
			}
		})
	}
}

func TestRewardsOverflow(t *testing.T) {
	require := require.New(t)

	var (
		maxSupply     uint64 = math.MaxUint64
		initialSupply uint64 = 1
	)
	c := NewCalculator(Config{
		MaxConsumptionRate: PercentDenominator,
		MinConsumptionRate: PercentDenominator,
		MintingPeriod:      defaultMinStakingDuration,
		SupplyCap:          maxSupply,
	})
	rewards := c.Calculate(
		defaultMinStakingDuration,
		maxSupply, // The staked amount is larger than the current supply
		initialSupply,
	)
	require.Equal(maxSupply-initialSupply, rewards)
}

func TestRewardsMint(t *testing.T) {
	require := require.New(t)

	var (
		maxSupply     uint64 = 1000
		initialSupply uint64 = 1
	)
	c := NewCalculator(Config{
		MaxConsumptionRate: PercentDenominator,
		MinConsumptionRate: PercentDenominator,
		MintingPeriod:      defaultMinStakingDuration,
		SupplyCap:          maxSupply,
	})
	rewards := c.Calculate(
		defaultMinStakingDuration,
		maxSupply, // The staked amount is larger than the current supply
		initialSupply,
	)
	require.Equal(maxSupply-initialSupply, rewards)
}
