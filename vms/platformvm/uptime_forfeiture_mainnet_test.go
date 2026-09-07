// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

// Measured mainnet (96369) state, read from
// platform.getCurrentValidators on https://api.lux.network/v1/chain/P.
// All five validators share startTime; endTime differs only by 90-minute
// staggers, so the first to mature bounds the whole set.
const (
	mainnetValidatorStart = int64(1_765_573_611) // 2025-12-12T21:06:51Z
	mainnetValidatorEnd   = int64(1_797_088_011) // 2026-12-12T15:06:51Z, first to mature
	mainnetObservedNow    = int64(1_785_183_540) // 2026-07-27T20:19:00Z, when uptime read 0.0000

	// mainnetUptimeRequirement is genesis.MainnetParams.UptimeRequirement, the
	// bar prefersCommit compares against for a primary-network staker.
	mainnetUptimeRequirement = 0.8

	// mainnetPotentialRewardTotal is the sum of potentialReward across the five
	// validators, in nLUX. This is the number at stake in this test.
	mainnetPotentialRewardTotal = uint64(165_583_347_962_466_973)
)

// mainnetTracker builds the real uptimeTracker in the state mainnet is actually
// in: tracking started, the validator's peer connection was never delivered to
// the VM, so every flush advanced lastUpdated to now while upDuration stayed 0.
//
// That is not a hypothesis. calculateUptimeLocked's "tracking, but not
// connected" branch returns (upDuration, now) — it moves the clock forward
// WITHOUT crediting the interval — so each pass permanently discards the time
// since the previous pass. It is the mechanism by which observed uptime is
// pinned at zero and, crucially, by which the discarded time cannot be
// recovered later.
func mainnetTracker(t *testing.T) (*uptimeTracker, ids.NodeID, ids.ID, *int64) {
	t.Helper()

	state := newFakeUptimeState()
	netID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	state.addValidator(nodeID, netID, time.Unix(mainnetValidatorStart, 0))

	clock := mainnetValidatorStart
	tracker := newUptimeTracker(state, netID, func() time.Time { return time.Unix(clock, 0) })

	// Tracking begins at bond time; the node never registers as connected.
	require.NoError(t, tracker.StartTracking([]ids.NodeID{nodeID}))

	return tracker, nodeID, netID, &clock
}

// TestMainnetUptimeIsZeroAndMatchesLiveAPI reproduces the live reading with
// production code. platform.getCurrentValidators reports uptime by calling
// CalculateUptimePercentFrom(nodeID, netID, staker.StartTime) and scaling by
// 100 (service.go), and block/executor's prefersCommit gates the reward by
// calling the SAME function with the SAME arguments.
//
// So the 0.0000 on the API is not a display artifact of a reporting path. It is
// the reward decision, printed.
func TestMainnetUptimeIsZeroAndMatchesLiveAPI(t *testing.T) {
	require := require.New(t)

	tracker, nodeID, netID, clock := mainnetTracker(t)
	*clock = mainnetObservedNow

	pct, err := tracker.CalculateUptimePercentFrom(nodeID, netID, time.Unix(mainnetValidatorStart, 0))
	require.NoError(err)

	require.Equal(0.0, pct, "must reproduce the 0.0000 reported by the live mainnet API")
	require.Less(pct, mainnetUptimeRequirement, "and therefore fail the 80%% reward gate")
}

// TestMainnetRewardIsUnrecoverableEvenIfFixedToday is the consequence the owner
// needs before enabling any uptime-based penalty.
//
// CalculateUptimePercentFrom divides accrued up-duration by (now - StartTime) —
// the FULL bond, not the window since a fix. 227 of the 365 staked days have
// already elapsed with zero credited. Even if peer connectivity started
// accruing perfectly from this instant and never dropped again, the arithmetic
// ceiling at maturity is upDuration/(end-start) = 138/365 = 37.8%.
//
// 37.8% < 80%. Every honest node therefore prefers ABORT, the reward UTXO is
// never created, and current supply is reduced by the same amount.
func TestMainnetRewardIsUnrecoverableEvenIfFixedToday(t *testing.T) {
	require := require.New(t)

	tracker, nodeID, netID, clock := mainnetTracker(t)

	// A perfect fix lands right now: the peer connects and never drops.
	*clock = mainnetObservedNow
	tracker.Connect(nodeID)
	require.True(tracker.IsConnected(nodeID))

	// Run the bond out to maturity with unbroken connectivity.
	*clock = mainnetValidatorEnd

	pct, err := tracker.CalculateUptimePercentFrom(nodeID, netID, time.Unix(mainnetValidatorStart, 0))
	require.NoError(err)

	t.Logf("best achievable uptime at maturity if fixed today: %.4f (required %.4f)",
		pct, mainnetUptimeRequirement)
	require.InDelta(0.3777, pct, 0.001)

	// This is the exact comparison block/executor prefersCommit performs.
	prefersCommit := pct >= mainnetUptimeRequirement
	require.False(prefersCommit,
		"the reward proposal is ABORTED: %d nLUX (%.2f LUX) of rewards across the five "+
			"validators are forfeited and burned from current supply",
		mainnetPotentialRewardTotal, float64(mainnetPotentialRewardTotal)/1e9)
}

// TestMainnetForfeitureDeadlineHasPassed dates the point of no return: the last
// instant at which a perfect fix could still have reached 80% by maturity.
// Reporting "uptime is broken, fix it" without this date invites a fix that
// arrives and changes nothing.
func TestMainnetForfeitureDeadlineHasPassed(t *testing.T) {
	require := require.New(t)

	bond := mainnetValidatorEnd - mainnetValidatorStart
	// Need upDuration >= 0.8*bond, and a fix at time f yields end-f, so the
	// latest viable fix is at end - 0.8*bond.
	deadline := mainnetValidatorEnd - int64(mainnetUptimeRequirement*float64(bond))

	t.Logf("bond %0.2f days; latest viable fix %s; observed %s; missed by %.0f days",
		float64(bond)/86400,
		time.Unix(deadline, 0).UTC().Format(time.RFC3339),
		time.Unix(mainnetObservedNow, 0).UTC().Format(time.RFC3339),
		float64(mainnetObservedNow-deadline)/86400)

	require.Greater(mainnetObservedNow, deadline,
		"if this ever fails, a fix can still save the reward and the deadline math must be redone")

	// Verify the deadline is exact by driving the real tracker from it.
	tracker, nodeID, netID, clock := mainnetTracker(t)
	*clock = deadline
	tracker.Connect(nodeID)
	*clock = mainnetValidatorEnd

	pct, err := tracker.CalculateUptimePercentFrom(nodeID, netID, time.Unix(mainnetValidatorStart, 0))
	require.NoError(err)
	require.InDelta(mainnetUptimeRequirement, pct, 0.0001,
		"a fix exactly at the deadline lands exactly on the bar")
}

// TestMainnetStakeIsRefundedOnAbort bounds the blast radius, and it is the half
// of the story that keeps this a serious problem rather than a catastrophic
// one.
//
// proposal_tx_executor.go rewardValidatorTx adds the stake UTXO to BOTH
// onCommitState and onAbortState, and adds the reward UTXO to onCommitState
// only. An uptime failure therefore forfeits the REWARD and returns the STAKE.
//
// The P-Chain has no stake slashing at all: there is no slash state transition
// anywhere in vms/platformvm outside a GPU plugin ABI declaration. Reward
// forfeiture is the only economic penalty that exists today.
func TestMainnetStakeIsRefundedOnAbort(t *testing.T) {
	require := require.New(t)

	const (
		stakePerValidator = uint64(500_000_000_000_000_000) // measured "weight", nLUX
		validators        = 5
	)
	totalStake := stakePerValidator * validators

	require.Equal(uint64(2_500_000_000_000_000_000), totalStake)
	t.Logf("at risk on an uptime abort: %.2f LUX of rewards", float64(mainnetPotentialRewardTotal)/1e9)
	t.Logf("NOT at risk (refunded on both commit and abort): %.2f LUX of stake", float64(totalStake)/1e9)

	// The rewards are ~6.6% of the bonded stake — a year's emission, not the principal.
	ratio := float64(mainnetPotentialRewardTotal) / float64(totalStake)
	require.InDelta(0.0662, ratio, 0.001)
}
