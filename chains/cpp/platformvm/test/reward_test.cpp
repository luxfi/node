// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// reward_test.cpp — the emission curve, ported from Go
// vms/platformvm/reward/calculator_test.go, example_test.go and
// supply_cap_underflow_mainnet_test.go.
//
// Every expected number is the Go test's own, unchanged. A reward calculator
// that rounds one microLUX differently is a different currency, so these are
// equality assertions, not tolerances.

#include "harness.hpp"
#include "lux/platformvm/reward.hpp"

using namespace lux::platformvm;
using namespace lux::platformvm::reward;

namespace {

// github.com/luxfi/constants units: 1 LUX = 10^6 microLUX.
constexpr std::uint64_t kMicroLux = 1;
constexpr std::uint64_t kMilliLux = 1000 * kMicroLux;
constexpr std::uint64_t kLux = 1000 * kMilliLux;
constexpr std::uint64_t kKiloLux = 1000 * kLux;
constexpr std::uint64_t kMegaLux = 1000 * kKiloLux;

constexpr Duration kMinStakingDuration = 24 * kHour;
constexpr Duration kMaxStakingDuration = 365 * 24 * kHour;
constexpr std::uint64_t kMinValidatorStake = 5 * kMilliLux;

const Config kDefaultConfig{
    /*max_consumption_rate=*/120000,  // .12 * PercentDenominator
    /*min_consumption_rate=*/100000,  // .10 * PercentDenominator
    /*minting_period=*/365 * 24 * kHour,
    /*supply_cap=*/720 * kMegaLux,
};

}  // namespace

// Go: TestLongerDurationBonus
TEST(LongerDurationBonus) {
    const Calculator c(kDefaultConfig);
    const Duration short_duration = 24 * kHour;
    const Duration total_duration = 365 * 24 * kHour;
    std::uint64_t short_balance = kKiloLux;
    for (int i = 0; i < static_cast<int>(total_duration / short_duration); ++i) {
        short_balance += c.calculate(short_duration, short_balance, 359 * kMegaLux + short_balance);
    }
    short_balance += c.calculate(total_duration % short_duration, short_balance, 359 * kMegaLux + short_balance);

    std::uint64_t long_balance = kKiloLux;
    long_balance += c.calculate(total_duration, long_balance, 359 * kMegaLux + long_balance);
    REQUIRE_MSG(short_balance < long_balance, "should promote stakers to stake longer");
}

// Go: TestRewards
TEST(Rewards) {
    const Calculator c(kDefaultConfig);
    struct Case {
        Duration duration;
        std::uint64_t stake;
        std::uint64_t existing;
        std::uint64_t expected;
    };
    const Case cases[] = {
        // Max duration
        {kMaxStakingDuration, kMegaLux, 360 * kMegaLux, 120 * kKiloLux},
        {kMaxStakingDuration, kMegaLux, 400 * kMegaLux, 96 * kKiloLux},
        {kMaxStakingDuration, 2 * kMegaLux, 400 * kMegaLux, 192 * kKiloLux},
        {kMaxStakingDuration, kMegaLux, kDefaultConfig.supply_cap, 0},
        // Min duration
        {kMinStakingDuration, kMegaLux, 360 * kMegaLux, 274122724},
        {kMinStakingDuration, kMinValidatorStake, 360 * kMegaLux, 1},
        {kMinStakingDuration, kMegaLux, 400 * kMegaLux, 219298179},
        {kMinStakingDuration, 2 * kMegaLux, 400 * kMegaLux, 438596359},
        {kMinStakingDuration, kMegaLux, kDefaultConfig.supply_cap, 0},
    };
    for (const auto& t : cases) REQUIRE_U64(t.expected, c.calculate(t.duration, t.stake, t.existing));
}

// Go: TestRewardsOverflow
TEST(RewardsOverflow) {
    const std::uint64_t max_supply = UINT64_MAX;
    const std::uint64_t initial_supply = 1;
    const Calculator c(Config{kPercentDenominator, kPercentDenominator, kMinStakingDuration, max_supply});
    REQUIRE_U64(max_supply - initial_supply, c.calculate(kMinStakingDuration, max_supply, initial_supply));
}

// Go: TestRewardsMint
TEST(RewardsMint) {
    const std::uint64_t max_supply = 1000;
    const std::uint64_t initial_supply = 1;
    const Calculator c(Config{kPercentDenominator, kPercentDenominator, kMinStakingDuration, max_supply});
    REQUIRE_U64(max_supply - initial_supply, c.calculate(kMinStakingDuration, max_supply, initial_supply));
}

// Go: TestSplit
TEST(Split) {
    struct Case {
        std::uint64_t amount;
        std::uint32_t shares;
        std::uint64_t expected;
    };
    const Case cases[] = {
        {1000, kPercentDenominator / 2, 500},
        {1, kPercentDenominator, 1},
        {1, kPercentDenominator - 1, 1},
        {1, 1, 1},
        {1, 0, 0},
        {9223374036974675809ull, 2, 18446748749757ull},
        {9223374036974675809ull, kPercentDenominator, 9223374036974675809ull},
        {9223372036855275808ull, kPercentDenominator - 2, 9223353590111202098ull},
        {9223372036855275808ull, 2, 18446744349518ull},
    };
    for (const auto& t : cases) {
        const auto r = split(t.amount, t.shares);
        REQUIRE_U64(t.expected, r.from_shares);
        REQUIRE_U64(t.amount - t.expected, r.remainder);
    }
}

// Go: ExampleNewCalculator — the documented worked example, as an assertion.
TEST(WorkedExample) {
    const Duration day = 24 * kHour;
    const Duration week = 7 * day;
    const Duration staking_duration = 4 * week;
    const std::uint64_t stake_amount = 100000 * kLux;
    const std::uint64_t current_supply = 447903490ull * kLux;
    const Calculator c(kDefaultConfig);
    REQUIRE_U64(473168954ull, c.calculate(staking_duration, stake_amount, current_supply));
}

// ── the supply-cap underflow, ported from
// supply_cap_underflow_mainnet_test.go. These pin a REAL DEFECT that the live
// chain runs on: past the cap, `supplyCap - currentSupply` wraps and the reward
// is computed from the wrapped value. The port reproduces it exactly, because
// changing emission is a monetary decision and not a translation.

namespace {
constexpr std::uint64_t kMainnetCurrentSupply = 13272095200543363741ull;
constexpr std::uint64_t kMainnetValidatorWeight = 500000000000000000ull;
constexpr std::uint64_t kMainnetPotentialReward = 33575831900252839ull;
constexpr Duration kMainnetStakedDuration = static_cast<Duration>(1797088011ll - 1765573611ll) * kSecond;
constexpr std::uint64_t kMainnetSupplyAtBond = 13106511852580896694ull;

const Config kMainnetRewardConfig{120000, 100000, 365 * 24 * kHour, 2000000000000000000ull};
}  // namespace

// Go: TestMainnetSupplyExceedsCompiledCap
TEST(MainnetSupplyExceedsCompiledCap) {
    REQUIRE_MSG(kMainnetCurrentSupply > kMainnetRewardConfig.supply_cap,
                "live supply must exceed the compiled cap for this whole file to be relevant");
}

// Go: TestMainnetRewardsAreComputedFromAnUnderflow
TEST(MainnetRewardsAreComputedFromAnUnderflow) {
    const std::uint64_t wrapped = kMainnetRewardConfig.supply_cap - kMainnetSupplyAtBond;
    REQUIRE_MSG(wrapped > kMainnetRewardConfig.supply_cap,
                "the subtraction must have wrapped, or there is no defect to demonstrate");
    const Calculator c(kMainnetRewardConfig);
    REQUIRE_U64(kMainnetPotentialReward,
                c.calculate(kMainnetStakedDuration, kMainnetValidatorWeight, kMainnetSupplyAtBond));
}

// Go: TestMainnetRewardWouldBeZeroWithoutTheWrap
TEST(MainnetRewardWouldBeZeroWithoutTheWrap) {
    Config at_cap = kMainnetRewardConfig;
    at_cap.supply_cap = kMainnetSupplyAtBond + 1;
    const Calculator c(at_cap);
    REQUIRE_U64(0, c.calculate(kMainnetStakedDuration, kMainnetValidatorWeight, kMainnetSupplyAtBond));
}

// Go: TestSupplyCapGuardIsTheFix — the one-line remedy, stated but not applied.
TEST(SupplyCapGuardIsTheFix) {
    const auto guarded = [](std::uint64_t cap, std::uint64_t supply) -> std::uint64_t {
        return supply >= cap ? 0 : cap - supply;
    };
    REQUIRE_U64(0, guarded(kMainnetRewardConfig.supply_cap, kMainnetCurrentSupply));
    REQUIRE_U64(1, guarded(kMainnetSupplyAtBond + 1, kMainnetSupplyAtBond));
    REQUIRE(kMainnetRewardConfig.supply_cap - kMainnetCurrentSupply != 0);
}
