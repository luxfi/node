// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// staking_test.cpp — the terms of validating, and what may change them.
//
// Ported from Go vms/platformvm/stakingparams/params_test.go and
// constitution_test.go. Two of the Go cases are written there with reflection —
// "Params governs no monetary field" and "there is nowhere to put an admin
// key". Reflection does not exist here, so they are written as assertions about
// the TYPES instead: a seventh field, or a signature that takes a caller, stops
// the file compiling. That is a stronger check than the Go one, not a weaker
// substitute for it.

#include "harness.hpp"
#include "lux/platformvm/reward.hpp"
#include "lux/platformvm/staking.hpp"

#include <type_traits>

using namespace lux::platformvm;
using namespace lux::platformvm::staking;

namespace {

constexpr std::uint64_t kYear = 365 * kDay;

// The governable set is exactly six values, and they are these. A seventh field
// — a supply cap, a minting period, a consumption rate, a fee split — would
// make this fail to compile, which is the point: money is not votable.
static_assert(std::is_aggregate_v<Params>);
static_assert(std::is_constructible_v<Params, std::uint64_t, std::uint64_t, std::uint32_t, std::uint32_t,
                                      std::uint32_t, std::uint32_t>);
static_assert(!std::is_constructible_v<Params, std::uint64_t, std::uint64_t, std::uint32_t, std::uint32_t,
                                       std::uint32_t, std::uint32_t, std::uint32_t>);

// And there is nowhere to put an authority. Bounds is two corner Params and
// nothing else; Rate is a step and an interval; an Entry is a time and a
// policy. None of them has room for an owner, a role, a threshold or an
// address.
static_assert(sizeof(Bounds) == 2 * sizeof(Params));
static_assert(std::is_constructible_v<Rate, std::uint32_t, std::uint64_t>);
static_assert(!std::is_constructible_v<Rate, std::uint32_t, std::uint64_t, std::uint64_t>);
static_assert(std::is_constructible_v<Entry, std::int64_t, Params>);
static_assert(!std::is_constructible_v<Entry, std::int64_t, Params, std::int64_t>);

// The decision itself takes the two policies, the compiled-in envelope, the
// rate, and how long it has been. No key, no caller, no identity — there is
// nothing to hold and nothing to compromise.
static_assert(std::is_same_v<decltype(&accept), Status (*)(const Params&, const Params&, const Bounds&,
                                                           const Rate&, std::uint64_t)>);

}  // namespace

// One unit for the whole chain. The thresholds are stated in LUX and have to
// resolve through the same constant everything else uses.
TEST(TheUnitIsTheChainsUnit) {
    REQUIRE_U64(1'000'000u, kLux);
    REQUIRE_U64(2'000u * kLux, kMainnetGenesis.min_validator_stake);
    REQUIRE_U64(5 * kGigaLux, kMainnetBounds.hi.max_validator_stake);
    REQUIRE_U64(1'000'000'000u * kLux, kGigaLux);
}

// Today's live policy is inside the envelope, or adopting this mechanism would
// change the network on the day it shipped.
TEST(TheConstitutionIsCoherentAndHoldsToday) {
    REQUIRE_OK(kMainnetBounds.valid());
    REQUIRE_OK(kMainnetGenesis.valid());
    REQUIRE_OK(kMainnetBounds.contains(kMainnetGenesis));
    REQUIRE_OK(mainnet_history().valid());
}

// The permissionless-ward direction is free: a proposal that lowers every
// barrier at once, by any amount inside the envelope, passes in one step.
TEST(LooseningIsInstant) {
    const Params wide_open{
        1 * kLux,                                   // 2,000 LUX -> 1 LUX, a 2000x drop
        5 * kGigaLux,                               // unchanged
        60 * 60,                                    // 14 days -> 1 hour
        static_cast<std::uint32_t>(kYear),          // unchanged
        0,                                          // 2% -> 0
        0,                                          // 80% -> 0
    };
    REQUIRE_OK(accept(kMainnetGenesis, wide_open, kMainnetBounds, kMainnetRate, kMainnetRate.min_interval));
}

// The other direction costs time: the same magnitude of change, pointed at
// excluding people, is refused.
TEST(AnExclusionaryStepIsRateLimited) {
    // A cartel tries to jump the floor straight to the ceiling.
    Params squeeze = kMainnetGenesis;
    squeeze.min_validator_stake = 100 * kKiloLux;
    REQUIRE_ERR(accept(kMainnetGenesis, squeeze, kMainnetBounds, kMainnetRate, kYear), Err::StepTooLarge);

    // Exactly a tenth of the current value is the largest admissible step.
    Params largest = kMainnetGenesis;
    largest.min_validator_stake =
        kMainnetGenesis.min_validator_stake + kMainnetGenesis.min_validator_stake / 10;
    REQUIRE_OK(accept(kMainnetGenesis, largest, kMainnetBounds, kMainnetRate, kMainnetRate.min_interval));

    // One µLUX beyond it is not.
    Params over = largest;
    over.min_validator_stake++;
    REQUIRE_ERR(accept(kMainnetGenesis, over, kMainnetBounds, kMainnetRate, kMainnetRate.min_interval),
                Err::StepTooLarge);
}

// Even a loosening waits its turn, so changes stay countable.
TEST(TheIntervalIsEnforced) {
    Params next = kMainnetGenesis;
    next.uptime_requirement = 700'000;  // a loosening, so only the interval can refuse it
    REQUIRE_ERR(accept(kMainnetGenesis, next, kMainnetBounds, kMainnetRate, kMainnetRate.min_interval - 1),
                Err::TooSoon);
    REQUIRE_OK(accept(kMainnetGenesis, next, kMainnetBounds, kMainnetRate, kMainnetRate.min_interval));
}

// No vote, however patient, escapes the envelope. This is the half that moves
// the way Bitcoin's rules move: by people choosing to run a new release.
TEST(TheBoundsAreAbsolute) {
    const std::uint64_t patient = 10 * kYear;
    {
        Params p = kMainnetGenesis;
        p.min_validator_stake = kMainnetBounds.hi.min_validator_stake + 1;
        REQUIRE_ERR(accept(kMainnetGenesis, p, kMainnetBounds, kMainnetRate, patient), Err::OutOfBounds);
    }
    {
        Params p = kMainnetGenesis;
        p.min_delegation_fee = 200'001;  // above 20%
        REQUIRE_ERR(accept(kMainnetGenesis, p, kMainnetBounds, kMainnetRate, patient), Err::OutOfBounds);
    }
    {
        Params p = kMainnetGenesis;
        p.uptime_requirement = 950'001;  // above 95%
        REQUIRE_ERR(accept(kMainnetGenesis, p, kMainnetBounds, kMainnetRate, patient), Err::OutOfBounds);
    }
    {
        Params p = kMainnetGenesis;
        p.min_stake_duration = static_cast<std::uint32_t>(30 * kDay) + 1;
        REQUIRE_ERR(accept(kMainnetGenesis, p, kMainnetBounds, kMainnetRate, patient), Err::OutOfBounds);
    }
    {
        Params p = kMainnetGenesis;
        p.max_validator_stake = kMainnetBounds.lo.max_validator_stake - 1;
        REQUIRE_ERR(accept(kMainnetGenesis, p, kMainnetBounds, kMainnetRate, patient), Err::OutOfBounds);
    }
}

// The number the design lives or dies on: driving the validator floor from
// today's value to the most exclusionary value the constitution permits, at the
// maximum rate, in public, takes this long. It is the warning window in which
// anyone being pushed out can organise, exit, or fork.
TEST(ASqueezeOutCostsYears) {
    Params current = kMainnetGenesis;
    std::uint64_t steps = 0;
    while (current.min_validator_stake < kMainnetBounds.hi.min_validator_stake) {
        Params next = current;
        next.min_validator_stake =
            std::min(current.min_validator_stake + step_limit(current.min_validator_stake, kMainnetRate.max_step),
                     kMainnetBounds.hi.min_validator_stake);
        REQUIRE_MSG(accept(current, next, kMainnetBounds, kMainnetRate, kMainnetRate.min_interval).has_value(),
                    "a step at exactly the rate limit must always be admissible");
        current = next;
        ++steps;
        REQUIRE_MSG(steps < 1000, "the walk did not terminate");
    }

    // Over a year of continuous, public, on-chain effort.
    REQUIRE(steps * kMainnetRate.min_interval > kYear);
    // And having spent it, the network is STILL open to a 100k LUX holder.
    REQUIRE_U64(kMainnetBounds.hi.min_validator_stake, current.min_validator_stake);
    REQUIRE_OK(kMainnetBounds.contains(current));
}

// The non-retroactivity proof, and the property that separates governance from
// expropriation. The uptime requirement is the one governed field read at
// REWARD time, long after a validator bonded — so a validator that bonded under
// an 80% rule is judged at 80% even after stake votes it to 88%.
TEST(AHistoryBindsOnlyTheFuture) {
    constexpr std::int64_t bonded_at = 1'765'573'611;  // a real mainnet validator's start
    constexpr std::int64_t voted_at = 1'785'000'000;   // a later vote

    Params tightened = kMainnetGenesis;
    tightened.uptime_requirement = 880'000;  // 88%

    const History h{{Entry{0, kMainnetGenesis}, Entry{voted_at, tightened}}};
    REQUIRE_OK(h.valid());

    REQUIRE_U64(800'000u, h.at(bonded_at).uptime_requirement);
    REQUIRE_U64(880'000u, h.at(voted_at + 1).uptime_requirement);
    REQUIRE(h.current() == tightened);
    // Before any activation, the genesis policy binds.
    REQUIRE(h.at(-1) == kMainnetGenesis);
}

// A record of what was in force is only a record if it moves one way.
TEST(AHistoryMustMoveForward) {
    REQUIRE_ERR(History{}.valid(), Err::EmptyHistory);
    REQUIRE_ERR((History{{Entry{10, {}}, Entry{10, {}}}}.valid()), Err::NotMonotonic);
    REQUIRE_ERR((History{{Entry{10, {}}, Entry{9, {}}}}.valid()), Err::NotMonotonic);
}

// A proposal can be inside the envelope on every field and still be nonsense as
// a pair.
TEST(AnIncoherentProposalIsRefused) {
    Params p = kMainnetGenesis;
    p.min_validator_stake = 5 * kGigaLux;
    p.max_validator_stake = 1 * kMegaLux;
    REQUIRE_ERR(accept(kMainnetGenesis, p, kMainnetBounds, kMainnetRate, kYear), Err::Incoherent);
}

// Proposing what is already in force is not a change, and spending the interval
// on one would be a way to hold the brake down.
TEST(ANoOpProposalIsRefused) {
    REQUIRE_ERR(accept(kMainnetGenesis, kMainnetGenesis, kMainnetBounds, kMainnetRate, kYear),
                Err::NoChange);
}

// The arithmetic at the extremes the chain can actually reach: weights run to
// ~1.8e19 and the denominator is 1e6, so the naive current * step form
// overflows.
TEST(TheStepLimitDoesNotOverflow) {
    REQUIRE_U64(UINT64_MAX / 10, step_limit(UINT64_MAX, kMainnetRate.max_step));
    REQUIRE_U64(200u * kLux, step_limit(2'000 * kLux, kMainnetRate.max_step));
    // A field parked at zero is not frozen there.
    REQUIRE_U64(1u, step_limit(0, kMainnetRate.max_step));
}

// The migration path when a release narrows the constitution under params that
// are already live: every node clamps identically and the chain keeps running.
// No guardian, no emergency key, no halt.
TEST(TheConstitutionRescuesWithoutAKey) {
    Params live = kMainnetGenesis;
    live.uptime_requirement = 940'000;  // legal today

    Bounds narrowed = kMainnetBounds;
    narrowed.hi.uptime_requirement = 900'000;  // a later release tightens the ceiling

    REQUIRE_ERR(narrowed.contains(live), Err::OutOfBounds);
    const Params clamped = narrowed.clamp(live);
    REQUIRE_OK(narrowed.contains(clamped));
    REQUIRE_U64(900'000u, clamped.uptime_requirement);
    REQUIRE_OK(clamped.valid());
}

// The floor moves at the activation and nowhere else: a validator bonding one
// second before it is held to 2,000 LUX; one bonding at it must bring
// 1,000,000.
TEST(TheFloorMovesAtTheActivation) {
    const auto h = mainnet_history();
    REQUIRE_OK(h.valid());
    REQUIRE_U64(2'000u * kLux, h.at(kActivation - 1).min_validator_stake);
    REQUIRE_U64(1'000'000u * kLux, h.at(kActivation).min_validator_stake);
    REQUIRE_U64(1'000'000u * kLux, h.current().min_validator_stake);
    REQUIRE_U64(5u * kGigaLux, h.current().max_validator_stake);

    // Nothing else moved: the change is the floor and only the floor.
    Params before = h.at(kActivation - 1);
    Params after = h.at(kActivation);
    before.min_validator_stake = 0;
    after.min_validator_stake = 0;
    REQUIRE(before == after);
}
