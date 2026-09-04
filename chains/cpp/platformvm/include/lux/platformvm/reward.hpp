// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// reward.hpp — the emission curve: what a validator is owed for the time it
// stood, and how that is divided with its delegators.
//
// Rendered from Go vms/platformvm/reward (calculator.go, config.go).
//
//   RemainingSupply          = SupplyCap - ExistingSupply
//   PortionOfExistingSupply  = StakedAmount / ExistingSupply
//   PortionOfStakingDuration = StakingDuration / MaximumStakingDuration
//   MintingRate              = MinRate + (MaxRate - MinRate) * PortionOfDuration
//   Reward = RemainingSupply * PortionOfSupply * MintingRate * PortionOfDuration
//
// Every intermediate is computed at full width and truncated once, at the end,
// exactly as the reference does with math/big — an emission schedule two
// implementations round differently is two different currencies.
//
// THE SUBTRACTION IS UNGUARDED, deliberately. `SupplyCap - ExistingSupply`
// wraps once supply passes the cap, and the Lux mainnet has passed it: the
// live chain's potentialReward values are computed from the wrapped value. A
// clamp here would be a monetary change, and this is a port, not a proposal —
// so the behaviour is reproduced and pinned by a test that recovers a real
// mainnet reward from real mainnet inputs. Changing it belongs in a node
// release operators choose to adopt.

#pragma once

#include "lux/platformvm/safemath.hpp"

#include <cstdint>

namespace lux::platformvm::reward {

// Percentages on this chain are parts per million; a "share" of 1'000'000 is
// all of it.
inline constexpr std::uint64_t kPercentDenominator = 1'000'000;

// Durations are nanoseconds, matching Go's time.Duration, because the reference
// multiplies raw Duration values into the curve.
using Duration = std::int64_t;
inline constexpr Duration kSecond = 1'000'000'000;
inline constexpr Duration kMinute = 60 * kSecond;
inline constexpr Duration kHour = 60 * kMinute;
inline constexpr Duration kDay = 24 * kHour;

struct Config {
    std::uint64_t max_consumption_rate = 0;
    std::uint64_t min_consumption_rate = 0;
    Duration minting_period = 0;
    std::uint64_t supply_cap = 0;
};

class Calculator {
  public:
    explicit Calculator(const Config& c)
        : max_sub_min_(c.max_consumption_rate - c.min_consumption_rate),
          min_(c.min_consumption_rate),
          minting_period_(static_cast<std::uint64_t>(c.minting_period)),
          supply_cap_(c.supply_cap) {}

    std::uint64_t calculate(Duration staked_duration, std::uint64_t staked_amount,
                            std::uint64_t current_supply) const {
        const std::uint64_t dur = static_cast<std::uint64_t>(staked_duration);

        // adjustedConsumptionRateNumerator = maxSubMin*duration + min*mintingPeriod
        BigUint numer = BigUint::from_u64(max_sub_min_);
        numer.mul_u64(dur);
        BigUint min_term = BigUint::from_u64(min_);
        min_term.mul_u64(minting_period_);
        numer.add(min_term);

        // adjustedConsumptionRateDenominator = mintingPeriod * PercentDenominator
        BigUint denom = BigUint::from_u64(minting_period_);
        denom.mul_u64(kPercentDenominator);

        const std::uint64_t remaining_supply = supply_cap_ - current_supply;  // wraps; see header

        BigUint reward = BigUint::from_u64(remaining_supply);
        reward.mul(numer);
        reward.mul_u64(staked_amount);
        reward.mul_u64(dur);
        reward.div(denom);
        reward.div_u64(current_supply);
        reward.div_u64(minting_period_);

        if (!reward.fits_u64()) return remaining_supply;
        const std::uint64_t final_reward = reward.to_u64();
        return remaining_supply < final_reward ? remaining_supply : final_reward;
    }

  private:
    std::uint64_t max_sub_min_;
    std::uint64_t min_;
    std::uint64_t minting_period_;
    std::uint64_t supply_cap_;
};

// Split divides an amount into the part the shares claim and the remainder.
// Rounding is delayed as long as it can be: the exact product is used whenever
// it fits, and only a total large enough to overflow falls back to dividing
// first. Invariant: shares <= kPercentDenominator.
struct SplitResult {
    std::uint64_t from_shares = 0;
    std::uint64_t remainder = 0;
};

inline SplitResult split(std::uint64_t total_amount, std::uint32_t shares) {
    const std::uint64_t remainder_shares = kPercentDenominator - static_cast<std::uint64_t>(shares);
    std::uint64_t remainder_amount = remainder_shares * (total_amount / kPercentDenominator);
    if (const auto optimistic = mul64(remainder_shares, total_amount); optimistic.has_value()) {
        remainder_amount = *optimistic / kPercentDenominator;
    }
    return SplitResult{total_amount - remainder_amount, remainder_amount};
}

}  // namespace lux::platformvm::reward
