// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// staking.cpp — the terms of validating, and what may change them.
//
// Rendered from Go vms/platformvm/stakingparams/params.go and constitution.go.

#include "lux/platformvm/staking.hpp"

#include "lux/platformvm/reward.hpp"

#include <algorithm>
#include <string>

namespace lux::platformvm::staking {
namespace {

// One governed value, projected out of Params so every rule is written once and
// applied uniformly. `higher_is_permissive` records which direction widens the
// set of people who can take part; that single bit is what makes the asymmetry
// a rule rather than six special cases.
struct Field {
    const char* name;
    std::uint64_t (*get)(const Params&);
    bool higher_is_permissive;
};

const std::vector<Field>& fields() {
    static const std::vector<Field> f = {
        {"minValidatorStake", [](const Params& p) { return p.min_validator_stake; }, false},
        {"maxValidatorStake", [](const Params& p) { return p.max_validator_stake; }, true},
        {"minStakeDuration", [](const Params& p) { return std::uint64_t{p.min_stake_duration}; }, false},
        {"maxStakeDuration", [](const Params& p) { return std::uint64_t{p.max_stake_duration}; }, true},
        {"minDelegationFee", [](const Params& p) { return std::uint64_t{p.min_delegation_fee}; }, false},
        {"uptimeRequirement", [](const Params& p) { return std::uint64_t{p.uptime_requirement}; }, false},
    };
    return f;
}

}  // namespace

Status Params::valid() const {
    if (min_validator_stake > max_validator_stake)
        return fail(Err::Incoherent, "minValidatorStake " + std::to_string(min_validator_stake) + " > " +
                                         "maxValidatorStake " + std::to_string(max_validator_stake));
    if (min_stake_duration > max_stake_duration)
        return fail(Err::Incoherent, "minStakeDuration " + std::to_string(min_stake_duration) + " > " +
                                         "maxStakeDuration " + std::to_string(max_stake_duration));
    return ok();
}

// A release that ships a nonsensical envelope fails loudly rather than silently
// admitting or rejecting everything.
Status Bounds::valid() const {
    for (const auto& f : fields())
        if (f.get(lo) > f.get(hi))
            return fail(Err::BoundsIncoherent, std::string(f.name) + " lo " + std::to_string(f.get(lo)) +
                                                   " > hi " + std::to_string(f.get(hi)));
    return ok();
}

Status Bounds::contains(const Params& p) const {
    for (const auto& f : fields()) {
        const auto v = f.get(p), l = f.get(lo), h = f.get(hi);
        if (v < l || v > h)
            return fail(Err::OutOfBounds, std::string(f.name) + " " + std::to_string(v) + " not in [" +
                                              std::to_string(l) + ", " + std::to_string(h) + "]");
    }
    return ok();
}

Params Bounds::clamp(const Params& p) const {
    Params out;
    out.min_validator_stake =
        std::min(std::max(p.min_validator_stake, lo.min_validator_stake), hi.min_validator_stake);
    out.max_validator_stake =
        std::min(std::max(p.max_validator_stake, lo.max_validator_stake), hi.max_validator_stake);
    out.min_stake_duration =
        std::min(std::max(p.min_stake_duration, lo.min_stake_duration), hi.min_stake_duration);
    out.max_stake_duration =
        std::min(std::max(p.max_stake_duration, lo.max_stake_duration), hi.max_stake_duration);
    out.min_delegation_fee =
        std::min(std::max(p.min_delegation_fee, lo.min_delegation_fee), hi.min_delegation_fee);
    out.uptime_requirement =
        std::min(std::max(p.uptime_requirement, lo.uptime_requirement), hi.uptime_requirement);
    return out;
}

std::uint64_t step_limit(std::uint64_t current, std::uint32_t max_step) {
    constexpr std::uint64_t denom = reward::kPercentDenominator;
    const std::uint64_t s = max_step;
    std::uint64_t lim = (current / denom) * s + (current % denom) * s / denom;
    if (lim == 0) lim = 1;
    return lim;
}

Status accept(const Params& current, const Params& next, const Bounds& b, const Rate& r,
              std::uint64_t elapsed) {
    if (auto st = b.valid(); !st) return st;
    if (auto st = next.valid(); !st) return st;
    if (auto st = b.contains(next); !st) return st;
    if (current == next) return fail(Err::NoChange, "the proposal is what is already in force");
    if (elapsed < r.min_interval)
        return fail(Err::TooSoon, std::to_string(elapsed) + "s elapsed, " +
                                      std::to_string(r.min_interval) + "s required");

    for (const auto& f : fields()) {
        const auto from = f.get(current), to = f.get(next);
        if (from == to) continue;
        // Toward more permissionless: free and instant. Widening who may take
        // part can never be used to exclude anyone, so it needs no brake — only
        // the envelope, which has already been checked.
        if ((to > from) == f.higher_is_permissive) continue;
        // Toward less permissionless: rate limited. This is the only direction
        // a stake majority can use against a minority, so it is the only one
        // that costs time.
        const std::uint64_t delta = to > from ? to - from : from - to;
        const std::uint64_t lim = step_limit(from, r.max_step);
        if (delta > lim)
            return fail(Err::StepTooLarge, std::string(f.name) + " moved " + std::to_string(delta) +
                                               " from " + std::to_string(from) + ", limit " +
                                               std::to_string(lim) + " per change");
    }
    return ok();
}

Status History::valid() const {
    if (entries.empty()) return fail(Err::EmptyHistory, "a policy history with no policy in it");
    for (std::size_t i = 1; i < entries.size(); ++i)
        if (entries[i].activation <= entries[i - 1].activation)
            return fail(Err::NotMonotonic, "entry " + std::to_string(i) + " activates at " +
                                               std::to_string(entries[i].activation) + ", not after " +
                                               std::to_string(entries[i - 1].activation));
    return ok();
}

Params History::at(std::int64_t t) const {
    if (entries.empty()) return Params{};
    Params p = entries.front().params;
    for (const auto& e : entries) {
        if (e.activation > t) break;
        p = e.params;
    }
    return p;
}

Params History::current() const {
    if (entries.empty()) return Params{};
    return entries.back().params;
}

History mainnet_history() {
    Params raised = kMainnetGenesis;
    raised.min_validator_stake = 1'000'000 * kLux;
    return History{{Entry{0, kMainnetGenesis}, Entry{kActivation, raised}}};
}

}  // namespace lux::platformvm::staking
