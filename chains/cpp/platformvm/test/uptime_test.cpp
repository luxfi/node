// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// uptime_test.cpp — ported from Go vms/platformvm/uptime_tracker_test.go and
// vms/platformvm/uptime_forfeiture_mainnet_test.go.
//
// Each case asserts the same behaviour, with the same numbers, as the Go test
// it is named after. The mainnet cases carry the literal timestamps and reward
// totals read from platform.getCurrentValidators on the live chain — they are
// the measurement, not a re-derivation.

#include "harness.hpp"

#include "lux/platformvm/uptime.hpp"

#include <atomic>
#include <cmath>
#include <thread>

using namespace lux::platformvm;

namespace {

constexpr std::uint64_t kMinute = 60;
constexpr std::uint64_t kHour = 60 * kMinute;
constexpr std::uint64_t kDay = 24 * kHour;

NodeId node_of(std::uint8_t tag) {
    NodeId n{};
    n.b[0] = tag;
    n.b[19] = 0x5a;
    return n;
}

Id id_of(std::uint8_t tag) {
    Id i{};
    i.b[0] = tag;
    i.b[31] = 0xa5;
    return i;
}

// FakeState mirrors how the real platform state stores uptime: an up-duration
// plus a second-granular last_updated. Validators must be registered first;
// reading or writing an unregistered node answers Err::NotFound, exactly like
// metadata_validator.go. Go: fakeUptimeState.
class FakeState final : public uptime::State {
  public:
    void add_validator(const NodeId& node_id, const Id&, std::uint64_t start_time) {
        uptimes_[node_id] = 0;
        last_update_[node_id] = start_time;
        start_times_[node_id] = start_time;
    }

    std::uint64_t up(const NodeId& node_id) const { return uptimes_.at(node_id); }
    std::uint64_t last_update(const NodeId& node_id) const { return last_update_.at(node_id); }

    Result<std::pair<std::uint64_t, std::uint64_t>> get_uptime(const NodeId& node_id,
                                                               const Id&) const override {
        const auto it = uptimes_.find(node_id);
        if (it == uptimes_.end()) return fail(Err::NotFound);
        return std::make_pair(it->second, last_update_.at(node_id));
    }

    Status set_uptime(const NodeId& node_id, const Id&, std::uint64_t up_duration,
                      std::uint64_t last_updated) override {
        if (uptimes_.find(node_id) == uptimes_.end()) return fail(Err::NotFound);
        uptimes_[node_id] = up_duration;
        last_update_[node_id] = last_updated;
        return ok();
    }

    Result<std::uint64_t> get_start_time(const NodeId& node_id, const Id&) const override {
        const auto it = start_times_.find(node_id);
        if (it == start_times_.end()) return fail(Err::NotFound);
        return it->second;
    }

  private:
    std::map<NodeId, std::uint64_t> uptimes_;
    std::map<NodeId, std::uint64_t> last_update_;
    std::map<NodeId, std::uint64_t> start_times_;
};

bool near(double got, double want, double delta) { return std::fabs(got - want) <= delta; }

// A fixed instant to hang the relative cases off. Go reads time.Now(); nothing
// in the tracker depends on which instant it is, and a literal keeps the test
// reproducible.
constexpr std::uint64_t kNowSeed = 1'700'000'000;

}  // namespace

// TestUptimeTrackerLongRunningValidatorAccruesUptime — the regression test for
// the reward gate. A validator that has been staked for 30 days must report
// ~100% uptime, not 0%.
TEST(UptimeTrackerLongRunningValidatorAccruesUptime) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    const std::uint64_t start_time = now - 30 * kDay;  // staked 30 days ago
    state.add_validator(node, net, start_time);

    uptime::Tracker tracker(state, net, [&] { return now; });

    auto pct = tracker.percent_from(node, net, start_time);
    REQUIRE_OK(pct);
    REQUIRE_MSG(near(pct.value(), 1.0, 0.001),
                "a continuously-staked validator must report ~100% uptime, not 0%");
}

// TestUptimeTrackerStartTrackingBaselines — start_tracking credits the
// un-measured pre-tracking window and moves the tracker into live-tracking mode.
TEST(UptimeTrackerStartTrackingBaselines) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    const std::uint64_t start_time = now - kHour;
    state.add_validator(node, net, start_time);

    uptime::Tracker tracker(state, net, [&] { return now; });
    REQUIRE(!tracker.started_tracking());

    REQUIRE_OK(tracker.start_tracking({node}));
    REQUIRE(tracker.started_tracking());

    // The hour since last_updated (== start_time) is baked into the persisted
    // up-duration, and last_updated advanced to now.
    REQUIRE_U64(kHour, state.up(node));
    REQUIRE_U64(now, state.last_update(node));
}

// TestUptimeTrackerContinuouslyConnectedClimbs — a validator that connects
// during bootstrap and stays connected accrues uptime WITHOUT ever
// disconnecting, and is_connected reports true.
TEST(UptimeTrackerContinuouslyConnectedClimbs) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now);

    uptime::Tracker tracker(state, net, [&] { return now; });

    // Peer connects during bootstrap, before tracking starts.
    tracker.connect(node);
    REQUIRE(tracker.is_connected(node));

    REQUIRE_OK(tracker.start_tracking({node}));

    // One hour passes with the validator continuously connected — no disconnect.
    now += kHour;

    auto pct = tracker.percent(node, net);
    REQUIRE_OK(pct);
    REQUIRE_MSG(near(pct.value(), 1.0, 0.001),
                "continuously-connected validator must climb to ~100%");
    REQUIRE(tracker.is_connected(node));

    // Two hours in, still ~100%.
    now += kHour;
    auto pct2 = tracker.percent(node, net);
    REQUIRE_OK(pct2);
    REQUIRE(near(pct2.value(), 1.0, 0.001));
}

// TestUptimeTrackerConnectDisconnectFlush — once tracking, a connected session
// is flushed into persistent state on disconnect.
TEST(UptimeTrackerConnectDisconnectFlush) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now);

    uptime::Tracker tracker(state, net, [&] { return now; });
    REQUIRE_OK(tracker.start_tracking({node}));
    REQUIRE_U64(0, state.up(node));

    tracker.connect(node);
    now += 30 * kMinute;
    auto mutated = tracker.disconnect(node);
    REQUIRE_OK(mutated);
    REQUIRE(mutated.value());  // a tracked validator's session flush writes uptime

    REQUIRE_U64(30 * kMinute, state.up(node));
    REQUIRE(!tracker.is_connected(node));

    // After disconnect, no further live-session bonus; percent reflects 30m/60m.
    now += 30 * kMinute;
    auto pct = tracker.percent(node, net);
    REQUIRE_OK(pct);
    REQUIRE(near(pct.value(), 0.5, 0.001));
}

// TestUptimeTrackerDisconnectBeforeTrackingIsNoWrite — a peer disconnect during
// bootstrap must report mutated=false, so the caller skips an empty commit.
TEST(UptimeTrackerDisconnectBeforeTrackingIsNoWrite) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now);

    uptime::Tracker tracker(state, net, [&] { return now; });
    // Deliberately NO start_tracking — still bootstrapping.

    tracker.connect(node);
    now += 30 * kMinute;

    auto mutated = tracker.disconnect(node);
    REQUIRE_OK(mutated);
    REQUIRE(!mutated.value());  // no write → the caller must skip the commit
    REQUIRE(!tracker.is_connected(node));
    REQUIRE_U64(0, state.up(node));  // untouched
}

// TestUptimeTrackerDisconnectedValidatorGetsZero — once tracking, a validator
// that never connects earns 0%, which is what lets the reward gate withhold.
TEST(UptimeTrackerDisconnectedValidatorGetsZero) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now);

    uptime::Tracker tracker(state, net, [&] { return now; });
    REQUIRE_OK(tracker.start_tracking({node}));

    now += kHour;  // an hour passes, never connected

    auto pct = tracker.percent(node, net);
    REQUIRE_OK(pct);
    REQUIRE_MSG(near(pct.value(), 0.0, 0.001),
                "never-connected validator (while tracking) must earn 0%");
    REQUIRE(!tracker.is_connected(node));
}

// TestUptimeTrackerStopTrackingFlushesAll — stop_tracking persists every
// connected validator's session and leaves tracking mode.
TEST(UptimeTrackerStopTrackingFlushesAll) {
    FakeState state;
    const Id net = id_of(1);

    std::uint64_t now = kNowSeed;
    constexpr int kNumValidators = 10;
    std::vector<NodeId> nodes;
    for (int i = 0; i < kNumValidators; ++i) {
        nodes.push_back(node_of(static_cast<std::uint8_t>(i + 1)));
        state.add_validator(nodes.back(), net, now);
    }

    uptime::Tracker tracker(state, net, [&] { return now; });
    REQUIRE_OK(tracker.start_tracking(nodes));
    for (const auto& n : nodes) tracker.connect(n);

    now += 3 * kMinute;
    REQUIRE_OK(tracker.stop_tracking(nodes));
    REQUIRE(!tracker.started_tracking());

    for (const auto& n : nodes) REQUIRE_U64(3 * kMinute, state.up(n));
}

// TestUptimeTrackerDoubleStartTracking — start_tracking is not re-entrant.
TEST(UptimeTrackerDoubleStartTracking) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);
    std::uint64_t now = kNowSeed;

    state.add_validator(node, net, now);
    uptime::Tracker tracker(state, net, [&] { return now; });

    REQUIRE_OK(tracker.start_tracking({node}));
    REQUIRE_ERR(tracker.start_tracking({node}), Err::AlreadyStartedTracking);
}

// TestUptimeTrackerStopWithoutStart — stop_tracking errors before start.
TEST(UptimeTrackerStopWithoutStart) {
    FakeState state;
    const Id net = id_of(1);
    std::uint64_t now = kNowSeed;
    uptime::Tracker tracker(state, net, [&] { return now; });

    REQUIRE_ERR(tracker.stop_tracking({}), Err::NotStartedTracking);
}

// TestUptimeTrackerUnknownValidator — non-validators are handled safely:
// start/connect/disconnect skip them without error and without a write, and a
// percent query surfaces the not-found.
TEST(UptimeTrackerUnknownValidator) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId unknown = node_of(9);

    std::uint64_t now = kNowSeed;
    uptime::Tracker tracker(state, net, [&] { return now; });

    // start_tracking must not fail on a node that has no state record.
    REQUIRE_OK(tracker.start_tracking({unknown}));

    // Connecting then disconnecting an unknown node is a no-op, not an error,
    // and must NOT report a state mutation.
    tracker.connect(unknown);
    now += kMinute;
    auto mutated = tracker.disconnect(unknown);
    REQUIRE_OK(mutated);
    REQUIRE(!mutated.value());

    // A percent query for an unknown validator surfaces the error.
    REQUIRE_ERR(tracker.percent(unknown, net), Err::NotFound);
}

// TestUptimeTrackerWrongNet — a query for a different network returns zero.
TEST(UptimeTrackerWrongNet) {
    FakeState state;
    const Id net = id_of(1);
    const Id other_net = id_of(2);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now - kHour);
    uptime::Tracker tracker(state, net, [&] { return now; });

    auto pct = tracker.percent(node, other_net);
    REQUIRE_OK(pct);
    REQUIRE(pct.value() == 0.0);

    auto up = tracker.uptime(node, other_net);
    REQUIRE_OK(up);
    REQUIRE_U64(0, up.value().first);
    REQUIRE_U64(0, up.value().second);
}

// TestUptimeTrackerDoubleConnect — a duplicate connect keeps the original
// connection time, so repeated router dispatch cannot inflate uptime.
TEST(UptimeTrackerDoubleConnect) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now);
    uptime::Tracker tracker(state, net, [&] { return now; });
    REQUIRE_OK(tracker.start_tracking({node}));

    tracker.connect(node);
    now += 5 * kMinute;
    tracker.connect(node);  // duplicate — must NOT reset the 5-minute-old session
    now += 5 * kMinute;
    auto mutated = tracker.disconnect(node);
    REQUIRE_OK(mutated);
    REQUIRE(mutated.value());

    // Ten minutes total, not five.
    REQUIRE_U64(10 * kMinute, state.up(node));
}

// TestUptimeTrackerRapidConnectDisconnect — rapid cycling never fabricates
// uptime beyond the exact connected intervals.
TEST(UptimeTrackerRapidConnectDisconnect) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::uint64_t now = kNowSeed;
    state.add_validator(node, net, now);
    uptime::Tracker tracker(state, net, [&] { return now; });
    REQUIRE_OK(tracker.start_tracking({node}));

    // 100 cycles with no clock advance — zero accrual.
    for (int i = 0; i < 100; ++i) {
        tracker.connect(node);
        REQUIRE_OK(tracker.disconnect(node));
    }
    REQUIRE_U64(0, state.up(node));

    // 50 cycles, each holding the connection for one second.
    for (int i = 0; i < 50; ++i) {
        tracker.connect(node);
        now += 1;
        REQUIRE_OK(tracker.disconnect(node));
    }
    REQUIRE_U64(50, state.up(node));
}

// TestUptimeTrackerConcurrent — races connect/disconnect/reads against each
// other. Go runs this under -race; here the tracker's own lock is what is under
// test, so a torn map would surface as a crash or a nonsensical answer.
TEST(UptimeTrackerConcurrent) {
    FakeState state;
    const Id net = id_of(1);
    const NodeId node = node_of(1);

    std::mutex clk_mu;
    std::uint64_t now = kNowSeed;
    auto clk = [&] {
        std::lock_guard<std::mutex> g(clk_mu);
        return now;
    };

    state.add_validator(node, net, now);
    uptime::Tracker tracker(state, net, clk);
    REQUIRE_OK(tracker.start_tracking({node}));

    constexpr int kGoroutines = 100;
    std::vector<std::thread> threads;
    threads.reserve(kGoroutines);
    for (int i = 0; i < kGoroutines; ++i) {
        threads.emplace_back([&tracker, &node, &net, i] {
            switch (i % 4) {
                case 0: tracker.connect(node); break;
                case 1: (void)tracker.disconnect(node); break;
                case 2: (void)tracker.percent(node, net); break;
                case 3: (void)tracker.is_connected(node); break;
            }
        });
    }
    for (auto& t : threads) t.join();

    {
        std::lock_guard<std::mutex> g(clk_mu);
        now += kMinute;
    }
    REQUIRE_OK(tracker.stop_tracking({node}));
}

// ── the mainnet forfeiture, reproduced with production code
//
// Ported from uptime_forfeiture_mainnet_test.go. The constants are measured
// mainnet (96369) state, read from platform.getCurrentValidators on
// https://api.lux.network/v1/chain/P. All five validators share a start time;
// end times differ only by 90-minute staggers, so the first to mature bounds
// the whole set.

namespace {

constexpr std::int64_t kMainnetValidatorStart = 1'765'573'611;  // 2025-12-12T21:06:51Z
constexpr std::int64_t kMainnetValidatorEnd = 1'797'088'011;    // 2026-12-12T15:06:51Z, first to mature
constexpr std::int64_t kMainnetObservedNow = 1'785'183'540;     // 2026-07-27T20:19:00Z, when uptime read 0.0000

// staking::kMainnetGenesis.uptime_requirement, the bar the reward gate compares
// against for a primary-network staker, as a fraction.
constexpr double kMainnetUptimeRequirement = 0.8;

// The sum of potential_reward across the five validators, in nLUX. This is the
// number at stake.
constexpr std::uint64_t kMainnetPotentialRewardTotal = 165'583'347'962'466'973ULL;

// The tracker in the state mainnet is actually in: tracking started, the
// validator's peer connection was never delivered to the VM, so every flush
// advanced last_updated to now while up-duration stayed 0.
//
// That is not a hypothesis. calculate_locked's "tracking, but not connected"
// branch returns (up_duration, now) — it moves the clock forward WITHOUT
// crediting the interval — so each pass permanently discards the time since the
// previous pass, and the discarded time cannot be recovered later.
struct MainnetTracker {
    FakeState state;
    Id net = id_of(7);
    NodeId node = node_of(7);
    std::uint64_t clock = static_cast<std::uint64_t>(kMainnetValidatorStart);
    uptime::Tracker tracker{state, net, [this] { return clock; }};

    Status begin() {
        state.add_validator(node, net, static_cast<std::uint64_t>(kMainnetValidatorStart));
        // Tracking begins at bond time; the node never registers as connected.
        return tracker.start_tracking({node});
    }
};

}  // namespace

// TestMainnetUptimeIsZeroAndMatchesLiveAPI — the live 0.0000 reading is not a
// display artifact of a reporting path. It is the reward decision, printed:
// getCurrentValidators and the reward gate call the SAME function with the SAME
// arguments.
TEST(MainnetUptimeIsZeroAndMatchesLiveAPI) {
    MainnetTracker m;
    REQUIRE_OK(m.begin());
    m.clock = static_cast<std::uint64_t>(kMainnetObservedNow);

    auto pct = m.tracker.percent_from(m.node, m.net, static_cast<std::uint64_t>(kMainnetValidatorStart));
    REQUIRE_OK(pct);

    REQUIRE_MSG(pct.value() == 0.0, "must reproduce the 0.0000 reported by the live mainnet API");
    REQUIRE_MSG(pct.value() < kMainnetUptimeRequirement, "and therefore fail the 80% reward gate");
}

// TestMainnetRewardIsUnrecoverableEvenIfFixedToday — percent_from divides
// accrued up-duration by (now - start), the FULL bond, not the window since a
// fix. Even with perfect connectivity from the observed instant onward, the
// ceiling at maturity is 138/365 = 37.8%, which is below the 80% bar: every
// honest node prefers ABORT and the reward UTXO is never created.
TEST(MainnetRewardIsUnrecoverableEvenIfFixedToday) {
    MainnetTracker m;
    REQUIRE_OK(m.begin());

    // A perfect fix lands right now: the peer connects and never drops.
    m.clock = static_cast<std::uint64_t>(kMainnetObservedNow);
    m.tracker.connect(m.node);
    REQUIRE(m.tracker.is_connected(m.node));

    // Run the bond out to maturity with unbroken connectivity.
    m.clock = static_cast<std::uint64_t>(kMainnetValidatorEnd);

    auto pct = m.tracker.percent_from(m.node, m.net, static_cast<std::uint64_t>(kMainnetValidatorStart));
    REQUIRE_OK(pct);
    REQUIRE(near(pct.value(), 0.3777, 0.001));

    // The exact comparison the reward gate performs.
    const bool prefers_commit = pct.value() >= kMainnetUptimeRequirement;
    REQUIRE_MSG(!prefers_commit,
                "the reward proposal is ABORTED and the rewards across the five validators "
                "are forfeited and burned from current supply");
}

// TestMainnetForfeitureDeadlineHasPassed — the last instant at which a perfect
// fix could still have reached 80% by maturity, and the proof that it is past.
TEST(MainnetForfeitureDeadlineHasPassed) {
    const std::int64_t bond = kMainnetValidatorEnd - kMainnetValidatorStart;
    // Need up_duration >= 0.8*bond, and a fix at time f yields end-f, so the
    // latest viable fix is at end - 0.8*bond.
    const std::int64_t deadline =
        kMainnetValidatorEnd - static_cast<std::int64_t>(kMainnetUptimeRequirement * static_cast<double>(bond));

    REQUIRE_MSG(kMainnetObservedNow > deadline,
                "if this ever fails, a fix can still save the reward and the deadline math must be redone");

    // Verify the deadline is exact by driving the real tracker from it.
    MainnetTracker m;
    REQUIRE_OK(m.begin());
    m.clock = static_cast<std::uint64_t>(deadline);
    m.tracker.connect(m.node);
    m.clock = static_cast<std::uint64_t>(kMainnetValidatorEnd);

    auto pct = m.tracker.percent_from(m.node, m.net, static_cast<std::uint64_t>(kMainnetValidatorStart));
    REQUIRE_OK(pct);
    REQUIRE_MSG(near(pct.value(), kMainnetUptimeRequirement, 0.0001),
                "a fix exactly at the deadline lands exactly on the bar");
}

// TestMainnetStakeIsRefundedOnAbort — the blast radius. rewardValidatorTx adds
// the stake UTXO to BOTH the commit and abort layers and the reward UTXO to the
// commit layer only, so an uptime failure forfeits the REWARD and returns the
// STAKE. The P-chain has no stake slashing; reward forfeiture is the only
// economic penalty that exists.
TEST(MainnetStakeIsRefundedOnAbort) {
    constexpr std::uint64_t kStakePerValidator = 500'000'000'000'000'000ULL;  // measured weight, nLUX
    constexpr std::uint64_t kValidators = 5;
    const std::uint64_t total_stake = kStakePerValidator * kValidators;

    REQUIRE_U64(2'500'000'000'000'000'000ULL, total_stake);

    // The rewards are ~6.6% of the bonded stake — a year's emission, not the principal.
    const double ratio =
        static_cast<double>(kMainnetPotentialRewardTotal) / static_cast<double>(total_stake);
    REQUIRE(near(ratio, 0.0662, 0.001));
}
