// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakingparams

import (
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const day = 24 * time.Hour

// TestParamsGovernsNoMonetaryField is the build-time guard on the money/policy
// split. Params must contain exactly the operational fields and nothing else;
// if someone later adds SupplyCap, MintingPeriod or a consumption rate, this
// test fails and the reviewer has to argue for making inflation votable rather
// than slipping it in.
func TestParamsGovernsNoMonetaryField(t *testing.T) {
	require := require.New(t)

	want := []string{
		"MaxStakeDuration", "MaxValidatorStake", "MinDelegationFee",
		"MinStakeDuration", "MinValidatorStake", "UptimeRequirement",
	}

	tp := reflect.TypeOf(Params{})
	got := make([]string, 0, tp.NumField())
	for i := 0; i < tp.NumField(); i++ {
		got = append(got, tp.Field(i).Name)
	}
	sort.Strings(got)
	require.Equal(want, got,
		"Params is the governable set. Monetary fields (SupplyCap, MintingPeriod, "+
			"Min/MaxConsumptionRate, the fee-split ratio) must never appear here.")

	// And the projection table must cover every field, or a field would be
	// silently ungoverned by the bounds and rate rules.
	require.Len(fields(), tp.NumField())
}

// TestNoAuthoritySurface asserts the mechanism has nowhere to put an admin key.
// Params, Bounds and Rate are the complete state of the decision; none carries
// an owner, role, threshold or address, and Accept takes no caller identity.
func TestNoAuthoritySurface(t *testing.T) {
	require := require.New(t)

	for _, typ := range []reflect.Type{
		reflect.TypeOf(Params{}), reflect.TypeOf(Bounds{}), reflect.TypeOf(Rate{}), reflect.TypeOf(Entry{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			for _, banned := range []string{"Owner", "Admin", "Auth", "Role", "Guardian", "Council", "Pauser", "Upgrader", "Threshold", "Address"} {
				require.NotContains(name, banned,
					"%s.%s introduces an authority surface; governance must be keyless", typ.Name(), name)
			}
		}
	}

	// Accept's signature carries no identity argument.
	ft := reflect.TypeOf(Accept)
	require.Equal(5, ft.NumIn(), "Accept must take exactly (cur, next, bounds, rate, elapsed)")
}

func TestMainnetConstitutionIsCoherentAndContainsToday(t *testing.T) {
	require := require.New(t)
	require.NoError(MainnetBounds.Valid())
	require.NoError(MainnetGenesis.Valid())
	require.NoError(MainnetBounds.Contains(MainnetGenesis),
		"today's live mainnet policy must be inside the envelope, or adopting this would change the network on day one")
}

// TestLooseningIsInstant proves the permissionless-ward direction is free: a
// proposal that lowers every barrier at once, by any amount inside Bounds, is
// admissible in a single step.
func TestLooseningIsInstant(t *testing.T) {
	require := require.New(t)

	wideOpen := Params{
		MinValidatorStake: 1 * Lux,            // 2000 LUX -> 1 LUX, a 2000x drop
		MaxValidatorStake: 5 * GigaLux,        // unchanged
		MinStakeDuration:  60 * 60,            // 14 days -> 1 hour
		MaxStakeDuration:  365 * 24 * 60 * 60, // unchanged
		MinDelegationFee:  0,                  // 2% -> 0
		UptimeRequirement: 0,                  // 80% -> 0
	}
	require.NoError(Accept(MainnetGenesis, wideOpen, MainnetBounds, MainnetRate, MainnetRate.MinInterval))
}

// TestExclusionaryStepIsRateLimited proves the other direction costs time: the
// same magnitude of change, pointed at excluding people, is refused.
func TestExclusionaryStepIsRateLimited(t *testing.T) {
	require := require.New(t)

	// A cartel tries to jump the floor straight to the ceiling.
	squeeze := MainnetGenesis
	squeeze.MinValidatorStake = 100 * KiloLux
	require.ErrorIs(Accept(MainnetGenesis, squeeze, MainnetBounds, MainnetRate, 365*day), ErrStepTooLarge)

	// Exactly 10% of current is the largest admissible step.
	ok := MainnetGenesis
	ok.MinValidatorStake = MainnetGenesis.MinValidatorStake + MainnetGenesis.MinValidatorStake/10
	require.NoError(Accept(MainnetGenesis, ok, MainnetBounds, MainnetRate, MainnetRate.MinInterval))

	// One nLUX beyond it is not.
	over := ok
	over.MinValidatorStake++
	require.ErrorIs(Accept(MainnetGenesis, over, MainnetBounds, MainnetRate, MainnetRate.MinInterval), ErrStepTooLarge)
}

func TestMinIntervalIsEnforced(t *testing.T) {
	require := require.New(t)
	next := MainnetGenesis
	next.UptimeRequirement = 700_000 // a loosening, so only the interval can refuse it

	require.ErrorIs(Accept(MainnetGenesis, next, MainnetBounds, MainnetRate, MainnetRate.MinInterval-time.Second), ErrTooSoon)
	require.NoError(Accept(MainnetGenesis, next, MainnetBounds, MainnetRate, MainnetRate.MinInterval))
}

// TestBoundsAreAbsolute proves no vote, however patient, escapes the envelope.
// This is the Bitcoin half: to move these, operators must adopt a new release.
func TestBoundsAreAbsolute(t *testing.T) {
	require := require.New(t)

	for _, tc := range []struct {
		name string
		mut  func(*Params)
	}{
		{"minValidatorStake above Hi", func(p *Params) { p.MinValidatorStake = MainnetBounds.Hi.MinValidatorStake + 1 }},
		{"minDelegationFee above 20%", func(p *Params) { p.MinDelegationFee = 200_001 }},
		{"uptimeRequirement above 95%", func(p *Params) { p.UptimeRequirement = 950_001 }},
		{"minStakeDuration above 30d", func(p *Params) { p.MinStakeDuration = 30*24*60*60 + 1 }},
		{"maxValidatorStake below Lo", func(p *Params) { p.MaxValidatorStake = MainnetBounds.Lo.MaxValidatorStake - 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := MainnetGenesis
			tc.mut(&p)
			require.ErrorIs(Accept(MainnetGenesis, p, MainnetBounds, MainnetRate, 10*365*day), ErrOutOfBounds)
		})
	}
}

// TestSqueezeOutCostsYears measures the security margin: driving the validator
// floor from today's value to the most exclusionary value the constitution
// permits, at the maximum rate, in public, takes this long.
//
// It is the number the design lives or dies on — it is the warning window in
// which anyone being pushed out can organise, exit, or fork.
func TestSqueezeOutCostsYears(t *testing.T) {
	require := require.New(t)

	cur := MainnetGenesis
	steps := 0
	for cur.MinValidatorStake < MainnetBounds.Hi.MinValidatorStake {
		next := cur
		next.MinValidatorStake = min(
			cur.MinValidatorStake+stepLimit(cur.MinValidatorStake, MainnetRate.MaxStep),
			MainnetBounds.Hi.MinValidatorStake,
		)
		require.NoError(Accept(cur, next, MainnetBounds, MainnetRate, MainnetRate.MinInterval),
			"max-rate step must always be admissible")
		cur = next
		steps++
		require.Less(steps, 1000, "loop guard")
	}

	elapsed := time.Duration(steps) * MainnetRate.MinInterval
	t.Logf("squeeze-out from %d nLUX to %d nLUX: %d accepted changes, %.0f days (%.2f years)",
		MainnetGenesis.MinValidatorStake, cur.MinValidatorStake, steps,
		elapsed.Hours()/24, elapsed.Hours()/24/365)

	require.Greater(elapsed, 365*day,
		"a stake majority must need over a year of public, on-chain effort to reach the most exclusionary policy")

	// And having spent it, the network is STILL open to a 100k LUX holder.
	require.Equal(MainnetBounds.Hi.MinValidatorStake, cur.MinValidatorStake)
	require.NoError(MainnetBounds.Contains(cur))
}

// TestHistoryBindsOnlyTheFuture is the non-retroactivity proof, and it is the
// property that separates governance from expropriation.
//
// UptimeRequirement is the one governed field read at REWARD time, long after a
// validator bonded. A validator that bonded under an 80% rule is judged at 80%
// even after stake votes the rule to 95% — so a majority cannot raise the bar
// the day before a rival's stake matures and take its reward.
func TestHistoryBindsOnlyTheFuture(t *testing.T) {
	require := require.New(t)

	const (
		bondedAt = 1_765_573_611 // the real start time of today's mainnet validators
		votedAt  = 1_785_000_000 // a later vote
	)

	tightened := MainnetGenesis
	tightened.UptimeRequirement = 880_000 // 88%

	h := History{
		{Activation: 0, Params: MainnetGenesis},
		{Activation: votedAt, Params: tightened},
	}
	require.NoError(h.Valid())

	require.Equal(uint32(800_000), h.At(bondedAt).UptimeRequirement,
		"a validator bonded before the vote is judged on the terms it accepted")
	require.Equal(uint32(880_000), h.At(votedAt+1).UptimeRequirement,
		"a validator bonding after the vote is judged on the new terms")
	require.Equal(tightened, h.Current())

	// Before any activation, genesis params bind.
	require.Equal(MainnetGenesis, h.At(-1))
}

func TestHistoryRejectsNonMonotonic(t *testing.T) {
	require := require.New(t)
	require.ErrorIs(History{}.Valid(), ErrEmptyHistory)
	require.ErrorIs(History{{Activation: 10}, {Activation: 10}}.Valid(), ErrNotMonotonic)
	require.ErrorIs(History{{Activation: 10}, {Activation: 9}}.Valid(), ErrNotMonotonic)
}

func TestIncoherentProposalRefused(t *testing.T) {
	require := require.New(t)
	p := MainnetGenesis
	p.MinValidatorStake = 5 * GigaLux
	p.MaxValidatorStake = 1 * MegaLux
	require.ErrorIs(Accept(MainnetGenesis, p, MainnetBounds, MainnetRate, 365*day), ErrIncoherent)
}

func TestNoOpProposalRefused(t *testing.T) {
	require := require.New(t)
	require.ErrorIs(Accept(MainnetGenesis, MainnetGenesis, MainnetBounds, MainnetRate, 365*day), ErrNoChange)
}

// TestStepLimitNoOverflow guards the arithmetic at the extremes the P-Chain can
// actually reach: weights run to ~1.8e19 and the denominator is 1e6, so the
// naive cur*step form overflows uint64.
func TestStepLimitNoOverflow(t *testing.T) {
	require := require.New(t)

	require.Equal(uint64(math.MaxUint64/10), stepLimit(math.MaxUint64, MainnetRate.MaxStep))
	require.Equal(uint64(200*Lux), stepLimit(2_000*Lux, MainnetRate.MaxStep))
	// A field parked at zero is not frozen there.
	require.Equal(uint64(1), stepLimit(0, MainnetRate.MaxStep))
}

// TestClampRescuesWithoutAKey proves the migration path when a release narrows
// the constitution under live params: every node clamps identically and the
// chain keeps running. No guardian, no emergency multisig, no halt.
func TestClampRescuesWithoutAKey(t *testing.T) {
	require := require.New(t)

	live := MainnetGenesis
	live.UptimeRequirement = 940_000 // legal today

	narrowed := MainnetBounds
	narrowed.Hi.UptimeRequirement = 900_000 // a later release tightens the ceiling

	require.Error(narrowed.Contains(live))
	clamped := narrowed.Clamp(live)
	require.NoError(narrowed.Contains(clamped))
	require.Equal(uint32(900_000), clamped.UptimeRequirement)
	require.NoError(clamped.Valid())
}
