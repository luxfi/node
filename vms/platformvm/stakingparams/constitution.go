// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakingparams

import (
	"time"

	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/reward"
)

// One unit for the whole node. A second definition here once said 1e9 and
// described a chain that does not exist: every threshold in this file was
// a thousand times what the live P-Chain enforces.
const (
	Lux     = units.Lux
	KiloLux = units.KiloLux
	MegaLux = units.MegaLux
	GigaLux = units.GigaLux
)

// MainnetGenesis is the policy Lux mainnet runs under today, transcribed from
// the constants presently compiled into the node (MinValidatorStake 2000 LUX,
// MaxValidatorStake 5 GigaLux, MinStakeDuration 2 weeks, MaxStakeDuration
// 1 year, MinDelegationFee 2%, UptimeRequirement 80%).
//
// Seeding the history with exactly today's values is what makes adopting this
// mechanism a no-op on day one: nothing about the live network changes until
// stake votes to change it.
var MainnetGenesis = Params{
	MinValidatorStake: 2_000 * Lux,
	MaxValidatorStake: 5 * GigaLux,
	MinStakeDuration:  2 * 7 * 24 * 60 * 60,
	MaxStakeDuration:  365 * 24 * 60 * 60,
	MinDelegationFee:  20_000,  // 2%
	UptimeRequirement: 800_000, // 80%
}

// MainnetBounds is the constitution — the envelope no vote can leave.
//
// Each bound is chosen against one question: what is the most exclusionary
// value a hostile stake majority could ever be allowed to reach, and is the
// network still open to an arbitrary global participant there? If the answer at
// Hi (or Lo, for the widening fields) is "no", the bound is wrong.
//
//   - minValidatorStake Hi = 100_000 LUX. At the most hostile setting the
//     mechanism permits, a holder of 100k LUX — 0.000005% of the 2T supply cap —
//     can still validate. Below Lo = 1 LUX there is nothing to bond.
//   - maxValidatorStake Lo = 1 MegaLux stops a cartel shrinking the ceiling to
//     exclude large honest stakers; Hi holds today's 5 GigaLux.
//   - minStakeDuration Hi = 30 days caps how long anyone can be forced to lock.
//   - maxStakeDuration Hi = 365 days: the reward calculator's MintingPeriod is
//     one year, so a longer bond has no defined emission. This bound is a
//     correctness constraint, not a policy preference.
//   - minDelegationFee Hi = 20% caps how much of a delegator's yield validators
//     can vote themselves. Delegators are the one constituency that cannot
//     defend itself by voting, because the vote is the validator's.
//   - uptimeRequirement Hi = 95%. Above that, ordinary maintenance forfeits a
//     reward, and the field stops being a liveness incentive and becomes an
//     ejection weapon.
var MainnetBounds = Bounds{
	Lo: Params{
		MinValidatorStake: 1 * Lux,
		MaxValidatorStake: 1 * MegaLux,
		MinStakeDuration:  60 * 60,
		MaxStakeDuration:  7 * 24 * 60 * 60,
		MinDelegationFee:  0,
		UptimeRequirement: 0,
	},
	Hi: Params{
		MinValidatorStake: 100 * KiloLux,
		MaxValidatorStake: 5 * GigaLux,
		MinStakeDuration:  30 * 24 * 60 * 60,
		MaxStakeDuration:  365 * 24 * 60 * 60,
		MinDelegationFee:  200_000, // 20%
		UptimeRequirement: 950_000, // 95%
	},
}

// MainnetRate is the brake on exclusionary change: at most 10% of the current
// value per accepted change, and at most one accepted change per 14 days.
//
// 14 days is not arbitrary — it is mainnet's MinStakeDuration, so no bond can
// be entered and matured inside a single governance step. Combined with the 10%
// cap it puts roughly 1.6 years of continuous, public, on-chain effort between
// today's 2000 LUX floor and the 100k LUX ceiling, which is the window in which
// anyone being squeezed out can organise, exit, or fork.
var MainnetRate = Rate{
	MaxStep:     reward.PercentDenominator / 10, // 10% of current
	MinInterval: 14 * 24 * time.Hour,
}

// MainnetHistory is the policy in force on Lux mainnet, oldest first. The
// first entry is what the chain was born with; the second raises the validator
// floor to 1,000,000 LUX from its activation — the same floor testnet and local
// have always had, and the one that makes an operator holding 50M a fifty-fold
// participant rather than a fifty-thousand-fold one. Anyone bonding at the old
// floor before then is judged on the terms they agreed to. Every node reads the
// same activation, which is what makes the floor change at one height everywhere.
var MainnetHistory = History{
	{Activation: 0, Params: MainnetGenesis},
	{Activation: 1787443200, Params: Params{ // 2026-08-23T00:00:00Z
		MinValidatorStake: 1_000_000 * Lux,
		MaxValidatorStake: MainnetGenesis.MaxValidatorStake,
		MinStakeDuration:  MainnetGenesis.MinStakeDuration,
		MaxStakeDuration:  MainnetGenesis.MaxStakeDuration,
		MinDelegationFee:  MainnetGenesis.MinDelegationFee,
		UptimeRequirement: MainnetGenesis.UptimeRequirement,
	}},
}
