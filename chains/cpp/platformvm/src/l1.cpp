// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// l1.cpp — the expiry key, and the continuous fee curve.
//
// Rendered from Go vms/platformvm/state/expiry.go and
// vms/platformvm/validators/fee/fee.go.

#include "lux/platformvm/l1.hpp"

#include "lux/platformvm/safemath.hpp"

#include <algorithm>
#include <cstring>

namespace lux::platformvm::l1 {

std::vector<std::uint8_t> ExpiryEntry::marshal() const {
    std::vector<std::uint8_t> out(8 + kIdLen);
    // Big-endian, so that sorting the BYTES sorts the timestamps. The database
    // walks these in key order and the executor needs them in time order; making
    // those the same order is the whole point of the endianness here.
    for (int i = 0; i < 8; ++i) out[i] = static_cast<std::uint8_t>(timestamp >> (56 - 8 * i));
    std::memcpy(out.data() + 8, validation_id.data(), kIdLen);
    return out;
}

Result<ExpiryEntry> ExpiryEntry::unmarshal(std::span<const std::uint8_t> b) {
    if (b.size() != 8 + kIdLen)
        return fail(Err::BufferTooSmall, "an expiry entry is " + std::to_string(8 + kIdLen) + " bytes");
    ExpiryEntry e;
    for (int i = 0; i < 8; ++i)
        e.timestamp = (e.timestamp << 8) | static_cast<std::uint64_t>(b[static_cast<std::size_t>(i)]);
    e.validation_id = id_from(b.subspan(8, kIdLen));
    return e;
}

FeeState FeeState::advance_time(gas::Gas target, std::uint64_t seconds) const {
    FeeState out = *this;
    if (current < target) {
        out.excess = gas::sub_per_second(excess, target - current, seconds);
    } else if (current > target) {
        out.excess = gas::add_per_second(excess, current - target, seconds);
    }
    return out;
}

std::uint64_t FeeState::cost_of(const FeeConfig& c, std::uint64_t seconds) const {
    // At target the price does not move, so the whole span is one multiply.
    if (current == c.target) {
        const gas::Price price = gas::calculate_price(c.min_price, excess, c.excess_conversion_constant);
        auto cost = mul64(seconds, price);
        return cost ? cost.value() : UINT64_MAX;
    }

    std::uint64_t cost = 0;
    FeeState s = *this;
    for (std::uint64_t i = 0; i < seconds; ++i) {
        s = s.advance_time(c.target, 1);

        // Advancing holds the excess, raises it, or lowers it — never mixes. So
        // once it reaches zero it stays there, and the rest of the span is the
        // floor price times the remaining seconds.
        if (s.excess == 0) {
            auto rest = mul64(c.min_price, seconds - i);
            if (!rest) return UINT64_MAX;
            auto total = add64(cost, rest.value());
            return total ? total.value() : UINT64_MAX;
        }

        const gas::Price price = gas::calculate_price(c.min_price, s.excess, c.excess_conversion_constant);
        auto total = add64(cost, price);
        if (!total) return UINT64_MAX;
        cost = total.value();
    }
    return cost;
}

std::uint64_t FeeState::seconds_remaining(const FeeConfig& c, std::uint64_t max_seconds,
                                          std::uint64_t funds) const {
    // A floor price of zero buys forever, and dividing by it would not.
    if (c.min_price == 0) return max_seconds;

    if (current == c.target) {
        const std::uint64_t price = gas::calculate_price(c.min_price, excess, c.excess_conversion_constant);
        return std::min(funds / price, max_seconds);
    }

    FeeState s = *this;
    std::uint64_t remaining = funds;
    for (std::uint64_t seconds = 0; seconds < max_seconds; ++seconds) {
        s = s.advance_time(c.target, 1);
        if (s.excess == 0) {
            const std::uint64_t at_floor = remaining / c.min_price;
            auto total = add64(seconds, at_floor);
            return total ? std::min(total.value(), max_seconds) : max_seconds;
        }
        const std::uint64_t price = gas::calculate_price(c.min_price, s.excess, c.excess_conversion_constant);
        if (price > remaining) return seconds;
        remaining -= price;
    }
    return max_seconds;
}

}  // namespace lux::platformvm::l1
