// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/platformvm/stakingparams"
)

// mainnetInternal is the compiled-in policy a mainnet node runs today.
func mainnetInternal() *Internal {
	return &Internal{
		MinValidatorStake: 2_000 * stakingparams.Lux,
		MaxValidatorStake: 5 * stakingparams.GigaLux,
		MinDelegationFee:  20_000,
		UptimePercentage:  0.8,
		MinStakeDuration:  2 * 7 * 24 * time.Hour,
		MaxStakeDuration:  365 * 24 * time.Hour,
	}
}

// TestUngovernedNodeIsUnchanged is the adoption safety property. A node with no
// staking history must resolve exactly the constants it was built with, so
// shipping this seam changes nothing on any live network until stake votes.
func TestUngovernedNodeIsUnchanged(t *testing.T) {
	require := require.New(t)

	c := mainnetInternal()
	require.Empty(c.StakingParams)

	got := c.StakingPolicyAt(time.Now().Unix())
	require.Equal(stakingparams.MainnetGenesis, got,
		"an ungoverned node must resolve today's compiled-in mainnet policy verbatim")

	// And the answer must not depend on the instant asked about.
	require.Equal(got, c.StakingPolicyAt(0))
	require.Equal(got, c.StakingPolicyAt(1<<40))
}

// TestGovernedPolicyBindsOnlyTheFuture drives the real seam function through
// the exact scenario the reward gate faces: a validator that bonded under the
// old rule, and a vote that lands afterwards.
func TestGovernedPolicyBindsOnlyTheFuture(t *testing.T) {
	require := require.New(t)

	const (
		bondedAt = int64(1_765_573_611) // real mainnet validator StartTime
		votedAt  = int64(1_785_000_000)
	)

	tightened := stakingparams.MainnetGenesis
	tightened.UptimeRequirement = 880_000 // 88%

	c := mainnetInternal()
	c.StakingParams = stakingparams.History{
		{Activation: 0, Params: stakingparams.MainnetGenesis},
		{Activation: votedAt, Params: tightened},
	}
	require.NoError(c.StakingParams.Valid())

	// This is the call block/executor prefersCommit makes, with the staker's
	// bond time. The already-bonded validator keeps its 80% bar.
	require.Equal(uint32(800_000), c.StakingPolicyAt(bondedAt).UptimeRequirement)

	// A validator bonding after the vote gets the new bar.
	require.Equal(uint32(880_000), c.StakingPolicyAt(votedAt+1).UptimeRequirement)
}

// TestGovernedAdmissionThresholdsAreLive proves the other seam: a vote that
// opens the network up is visible to admission immediately.
func TestGovernedAdmissionThresholdsAreLive(t *testing.T) {
	require := require.New(t)

	const openedAt = int64(1_785_000_000)

	opened := stakingparams.MainnetGenesis
	opened.MinValidatorStake = 100 * stakingparams.Lux // 2000 LUX -> 100 LUX

	// The proposal must itself be admissible under the constitution.
	require.NoError(stakingparams.Accept(
		stakingparams.MainnetGenesis, opened,
		stakingparams.MainnetBounds, stakingparams.MainnetRate,
		stakingparams.MainnetRate.MinInterval,
	))

	c := mainnetInternal()
	c.StakingParams = stakingparams.History{
		{Activation: 0, Params: stakingparams.MainnetGenesis},
		{Activation: openedAt, Params: opened},
	}

	// getValidatorRules resolves at the CURRENT chain time.
	require.Equal(2_000*stakingparams.Lux, c.StakingPolicyAt(openedAt-1).MinValidatorStake)
	require.Equal(100*stakingparams.Lux, c.StakingPolicyAt(openedAt).MinValidatorStake)
}

// TestDurationRoundTripDoesNotTruncate guards the one lossy conversion in the
// seam: config carries time.Duration, the governed value carries seconds.
func TestDurationRoundTripDoesNotTruncate(t *testing.T) {
	require := require.New(t)

	c := mainnetInternal()
	p := c.StakingPolicyAt(0)

	require.Equal(uint32(14*24*60*60), p.MinStakeDuration)
	require.Equal(uint32(365*24*60*60), p.MaxStakeDuration)
	require.Equal(c.MinStakeDuration, time.Duration(p.MinStakeDuration)*time.Second)
	require.Equal(c.MaxStakeDuration, time.Duration(p.MaxStakeDuration)*time.Second)
}
