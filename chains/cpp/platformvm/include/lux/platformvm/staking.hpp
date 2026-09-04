// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// staking.hpp — the terms of validating, and the rule that decides whether a
// proposed change to them is admissible.
//
// Rendered from Go vms/platformvm/stakingparams (params.go, constitution.go).
// It is the sibling of security.hpp: security says how a network's set is
// ADMITTED, this says on what TERMS.
//
// WHAT IS HERE. Validator admission thresholds, stake durations, the
// delegation-fee floor, and the uptime requirement. Operational policy — a
// legitimate collective choice of the people running the network.
//
// WHAT IS DELIBERATELY NOT, AND NEVER TO BE ADDED. The consumption rates, the
// minting period, the supply cap and the fee split. Those are money. A votable
// emission schedule is a capture surface, not a feature; predictable money is
// the product. They stay compiled in and change the way Bitcoin's rules change:
// by people choosing to run a new release.
//
// WHO DECIDES. Nobody. There is no admin key, owner, council, guardian or
// upgrader here — no field to hold one and no argument to pass one. `accept` is
// a pure function of (current, proposed, compiled-in bounds, rate, elapsed).
// Every node runs it independently and the stake-weighted consensus settles the
// outcome, exactly as it already does for a validator's reward. A dissenting
// node cannot veto, only vote and lose; a proposing node holds no privilege,
// because its preference binds no one.
//
// THE ONE INVARIANT WORTH REMEMBERING. Moving toward MORE permissionless is
// free and instant. Moving toward LESS permissionless is bounded and rate
// limited. A cartel that captures a stake majority therefore cannot ambush the
// minority out of the validator set: every exclusionary step is small, capped,
// and visible for the interval it takes — which is the window in which the
// excluded can organise, exit, or fork. That asymmetry is the whole security
// argument, and it is one rule applied uniformly to every field.

#pragma once

#include "lux/platformvm/error.hpp"

#include <cstdint>
#include <vector>

namespace lux::platformvm::staking {

// The unit every threshold below is denominated in. One definition, because a
// second one would silently rescale all of them.
inline constexpr std::uint64_t kLux = 1'000'000;  // six decimals
inline constexpr std::uint64_t kKiloLux = 1'000 * kLux;
inline constexpr std::uint64_t kMegaLux = 1'000 * kKiloLux;
inline constexpr std::uint64_t kGigaLux = 1'000 * kMegaLux;

inline constexpr std::uint64_t kDay = 24 * 60 * 60;

// The policy in force. Percent-scaled fields use reward::kPercentDenominator
// (1'000'000), so 800'000 is 80%. Durations are seconds, matching the wire.
struct Params {
    std::uint64_t min_validator_stake = 0;
    std::uint64_t max_validator_stake = 0;
    std::uint32_t min_stake_duration = 0;
    std::uint32_t max_stake_duration = 0;
    std::uint32_t min_delegation_fee = 0;
    std::uint32_t uptime_requirement = 0;

    friend bool operator==(const Params&, const Params&) = default;

    // Internally coherent: every floor at or below its paired ceiling.
    // Coherence is checked apart from the bounds because a proposal can be
    // inside the envelope on every field and still be nonsense as a pair.
    Status valid() const;
};

// The constitution: the closed interval outside which no proposal is ever
// admissible, whatever the vote. Compiled in — changing it takes a release that
// operators choose to run, which is the only route by which the governable
// envelope itself can move. That is the point: the envelope is the thing a
// stake majority must not be able to widen for itself.
struct Bounds {
    Params lo{};
    Params hi{};

    Status valid() const;
    Status contains(const Params& p) const;
    // Every field pulled inside the envelope. This is the migration path when a
    // release narrows the bounds under params that are already live: the chain
    // does not halt and no key is needed to rescue it — the constitution simply
    // binds, deterministically and identically on every node.
    Params clamp(const Params& p) const;
};

// How fast policy may move against the people it governs. `max_step` is in
// percent-denominator units of the CURRENT value (100'000 = 10% of current per
// change); `min_interval` is the least chain time between two accepted changes.
// Together they set the cost of a squeeze-out: reaching a target from a start
// takes ceil(log(target/start) / log(1+step)) changes, each interval apart, all
// of it on-chain and observable.
struct Rate {
    std::uint32_t max_step = 0;
    std::uint64_t min_interval = 0;  // seconds
};

// The largest admissible exclusionary move away from `current`, computed
// without overflowing. A current of zero admits a move to one, so a field
// parked at zero is not frozen there forever.
std::uint64_t step_limit(std::uint64_t current, std::uint32_t max_step);

// The entire governance decision, as a pure function.
//
// It takes no key, no caller, no role and no owner — there is nothing to hold
// and nothing to compromise. Given the same arguments every node on earth
// reaches the same verdict, which is what lets the existing stake-weighted
// machinery settle it with no leader.
//
// `elapsed` is the chain time since the last accepted change.
Status accept(const Params& current, const Params& next, const Bounds& b, const Rate& r,
              std::uint64_t elapsed);

// One activation of a policy: the params, and the chain time from which they
// bind.
struct Entry {
    std::int64_t activation = 0;
    Params params{};

    friend bool operator==(const Entry&, const Entry&) = default;
};

// The append-only record of every policy that has been in force, oldest first.
// It exists for one reason: a validator must be judged on the terms it agreed
// to when it bonded, never on terms voted in afterwards.
//
// Without it the uptime requirement — the one governed field read at REWARD
// time rather than at admission time — would be retroactive, and a stake
// majority could raise the bar the day before a rival's stake matures and
// confiscate the reward. With it, governance can only bind the future, which is
// the difference between setting policy and expropriating people.
struct History {
    std::vector<Entry> entries;

    Status valid() const;
    // The params in force at unix time `t`: the latest entry activating at or
    // before it. Before the first activation the first entry applies, so
    // genesis params bind from the beginning of time.
    Params at(std::int64_t t) const;
    Params current() const;
    bool empty() const { return entries.empty(); }
};

// ── mainnet
//
// What Lux mainnet runs under today, and the envelope it may never leave.

inline constexpr Params kMainnetGenesis{
    2'000 * kLux, 5 * kGigaLux, 2 * 7 * static_cast<std::uint32_t>(kDay),
    365 * static_cast<std::uint32_t>(kDay), 20'000, 800'000,
};

// Each bound answers one question: what is the most exclusionary value a
// hostile stake majority could ever be allowed to reach, and is the network
// still open to an arbitrary participant there?
//
//  - min_validator_stake hi = 100'000 LUX. At the most hostile setting the
//    mechanism permits, a holder of 100k LUX — 0.000005% of the supply cap —
//    can still validate. Below lo = 1 LUX there is nothing to bond.
//  - max_validator_stake lo = 1 MegaLux stops a cartel shrinking the ceiling to
//    exclude large honest stakers.
//  - min_stake_duration hi = 30 days caps how long anyone can be forced to lock.
//  - max_stake_duration hi = 365 days, because the emission curve's minting
//    period is one year and a longer bond has no defined reward. That bound is
//    a correctness constraint, not a preference.
//  - min_delegation_fee hi = 20% caps how much of a delegator's yield
//    validators can vote themselves. Delegators are the one constituency that
//    cannot defend itself by voting, because the vote is the validator's.
//  - uptime_requirement hi = 95%. Above that, ordinary maintenance forfeits a
//    reward and the field stops being a liveness incentive and becomes a way to
//    eject people.
inline constexpr Bounds kMainnetBounds{
    Params{1 * kLux, 1 * kMegaLux, 60 * 60, 7 * static_cast<std::uint32_t>(kDay), 0, 0},
    Params{100 * kKiloLux, 5 * kGigaLux, 30 * static_cast<std::uint32_t>(kDay),
           365 * static_cast<std::uint32_t>(kDay), 200'000, 950'000},
};

// At most 10% of the current value per accepted change, and at most one change
// per 14 days. Fourteen days is mainnet's own minimum stake duration, so no
// bond can be entered and matured inside a single governance step. With the 10%
// cap that puts roughly 1.6 years of continuous, public, on-chain effort
// between today's floor and the ceiling — the window in which anyone being
// squeezed out can organise, exit, or fork.
inline constexpr Rate kMainnetRate{100'000, 14 * kDay};

// The one instant every mainnet rule turns on at, so the staking floor cannot
// activate at a moment the rest of the schedule does not know about.
inline constexpr std::int64_t kActivation = 1'766'708'400;

// Oldest first. The first entry is what the chain was born with; the second
// raises the validator floor. Anyone who bonded at the old floor before the
// activation is judged on the terms they agreed to.
History mainnet_history();

}  // namespace lux::platformvm::staking
