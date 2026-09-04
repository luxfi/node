// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gas.hpp — what a transaction costs the chain, in four dimensions.
//
// Rendered from Go vms/components/gas (dimensions.go, gas.go, state.go,
// config.go), the LP-103 dynamic fee mechanism.
//
// A transaction costs four different things — bytes on the wire, reads, writes,
// and time to verify — and pricing them as one number is what lets a chain be
// congested in one dimension and idle in another without noticing. So they are
// counted separately and merged by weights only at the end.
//
// The price follows the excess: the chain has a target consumption per second,
// and the price is minPrice · e^(excess / K), approximated by the EIP-4844
// fake-exponential series. Every intermediate stays under 2^193, which is why a
// 512-bit integer is headroom rather than a guess.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/safemath.hpp"

#include <array>
#include <cstdint>

namespace lux::platformvm::gas {

enum Dimension : std::size_t { Bandwidth = 0, DBRead = 1, DBWrite = 2, Compute = 3, kNumDimensions = 4 };

using Gas = std::uint64_t;
using Price = std::uint64_t;

// The four counts a transaction runs up.
struct Dimensions {
    std::array<std::uint64_t, kNumDimensions> d{};

    std::uint64_t& operator[](std::size_t i) { return d[i]; }
    std::uint64_t operator[](std::size_t i) const { return d[i]; }
    friend bool operator==(const Dimensions&, const Dimensions&) = default;

    Result<Dimensions> add(const Dimensions& o) const {
        Dimensions out = *this;
        for (std::size_t i = 0; i < kNumDimensions; ++i) {
            auto s = add64(out.d[i], o.d[i]);
            if (!s) return std::unexpected(s.error());
            out.d[i] = s.value();
        }
        return out;
    }

    Result<Dimensions> sub(const Dimensions& o) const {
        Dimensions out = *this;
        for (std::size_t i = 0; i < kNumDimensions; ++i) {
            auto s = sub64(out.d[i], o.d[i]);
            if (!s) return std::unexpected(s.error());
            out.d[i] = s.value();
        }
        return out;
    }

    // The single number the four dimensions merge to, under the chain's weights.
    Result<Gas> to_gas(const Dimensions& weights) const {
        std::uint64_t res = 0;
        for (std::size_t i = 0; i < kNumDimensions; ++i) {
            auto v = mul64(d[i], weights.d[i]);
            if (!v) return std::unexpected(v.error());
            auto s = add64(res, v.value());
            if (!s) return std::unexpected(s.error());
            res = s.value();
        }
        return res;
    }
};

inline Result<std::uint64_t> cost(Gas g, Price p) { return mul64(g, p); }

// Saturating, because these are clock arithmetic rather than money: a capacity
// that would overflow is simply the largest capacity there is.
inline Gas add_per_second(Gas g, Gas per_second, std::uint64_t seconds) {
    auto added = mul64(per_second, seconds);
    if (!added) return UINT64_MAX;
    auto total = add64(g, added.value());
    if (!total) return UINT64_MAX;
    return total.value();
}

inline Gas sub_per_second(Gas g, Gas per_second, std::uint64_t seconds) {
    auto removed = mul64(per_second, seconds);
    if (!removed) return 0;
    auto total = sub64(g, removed.value());
    if (!total) return 0;
    return total.value();
}

struct Config {
    Dimensions weights{};
    Gas max_capacity = 0;
    Gas max_per_second = 0;
    Gas target_per_second = 0;
    Price min_price = 0;
    Gas excess_conversion_constant = 0;
};

// The chain's fee position: how much it may still consume, and how far above
// target it has been running.
struct State {
    Gas capacity = 0;
    Gas excess = 0;

    friend bool operator==(const State&, const State&) = default;

    State advance_time(Gas max_capacity, Gas max_per_second, Gas target_per_second,
                       std::uint64_t duration) const {
        const Gas grown = add_per_second(capacity, max_per_second, duration);
        return State{grown < max_capacity ? grown : max_capacity,
                     sub_per_second(excess, target_per_second, duration)};
    }

    // Spending gas costs capacity and raises the excess, which raises the price.
    // A block that asks for more than the chain has is refused.
    Result<State> consume(Gas g) const {
        auto new_capacity = sub64(capacity, g);
        if (!new_capacity)
            return fail(Err::InsufficientCapacity,
                        "capacity " + std::to_string(capacity) + " < gas " + std::to_string(g));
        auto new_excess = add64(excess, g);
        // Excess is a signal, not a balance: saturating it loses nothing.
        return State{new_capacity.value(), new_excess ? new_excess.value() : UINT64_MAX};
    }
};

// minPrice · e^(excess / K), by the EIP-4844 fake-exponential series. Go:
// gas.CalculatePrice.
Price calculate_price(Price min_price, Gas excess, Gas excess_conversion_constant);

}  // namespace lux::platformvm::gas
