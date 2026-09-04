// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// safemath.hpp — unsigned 64-bit arithmetic that refuses to wrap, and the wide
// integer the reward curve needs.
//
// Rendered from github.com/luxfi/math (Add/Sub/Mul) and from math/big as the
// reward calculator uses it. Wrapping is not an option on a chain: a validator
// weight that overflows to a small number is a validator set that no longer says
// what it means, so every sum a transaction can influence goes through here.
//
// BigUint is fixed-width rather than heap-allocating. The reward product is
// bounded by the four factors the curve multiplies — remaining supply, the
// consumption-rate numerator, the stake and the duration — which together stay
// under 2^260 for any config the chain accepts, so 512 bits is headroom, not a
// guess. Division is shift-and-subtract: one obviously-correct algorithm rather
// than a fast one whose corner cases would need their own proof.

#pragma once

#include "lux/platformvm/error.hpp"

#include <array>
#include <cstdint>
#include <limits>

namespace lux::platformvm {

inline Result<std::uint64_t> add64(std::uint64_t a, std::uint64_t b) {
    if (a > std::numeric_limits<std::uint64_t>::max() - b) return fail(Err::Overflow);
    return a + b;
}

inline Result<std::uint64_t> sub64(std::uint64_t a, std::uint64_t b) {
    if (a < b) return fail(Err::Underflow);
    return a - b;
}

inline Result<std::uint64_t> mul64(std::uint64_t a, std::uint64_t b) {
    if (a == 0 || b == 0) return static_cast<std::uint64_t>(0);
    if (a > std::numeric_limits<std::uint64_t>::max() / b) return fail(Err::Overflow);
    return a * b;
}

class BigUint {
  public:
    static constexpr std::size_t kWords = 8;         // 512 bits
    static constexpr std::size_t kBits = kWords * 64;

    BigUint() = default;
    explicit BigUint(std::uint64_t v) { w_[0] = v; }

    static BigUint from_u64(std::uint64_t v) { return BigUint(v); }
    static BigUint from_u128(unsigned __int128 v) {
        BigUint b;
        b.w_[0] = static_cast<std::uint64_t>(v);
        b.w_[1] = static_cast<std::uint64_t>(v >> 64);
        return b;
    }

    bool is_zero() const {
        for (auto x : w_)
            if (x != 0) return false;
        return true;
    }
    bool fits_u64() const {
        if (over_) return false;
        for (std::size_t i = 1; i < kWords; ++i)
            if (w_[i] != 0) return false;
        return true;
    }
    std::uint64_t to_u64() const { return w_[0]; }
    bool overflowed() const { return over_; }

    bool bit(std::size_t i) const { return (w_[i / 64] >> (i % 64)) & 1u; }
    void set_bit(std::size_t i) { w_[i / 64] |= (std::uint64_t{1} << (i % 64)); }

    BigUint& add(const BigUint& o) {
        unsigned __int128 carry = 0;
        for (std::size_t i = 0; i < kWords; ++i) {
            const unsigned __int128 s =
                static_cast<unsigned __int128>(w_[i]) + static_cast<unsigned __int128>(o.w_[i]) + carry;
            w_[i] = static_cast<std::uint64_t>(s);
            carry = s >> 64;
        }
        if (carry != 0 || o.over_) over_ = true;
        return *this;
    }

    BigUint& sub(const BigUint& o) {
        unsigned __int128 borrow = 0;
        for (std::size_t i = 0; i < kWords; ++i) {
            const unsigned __int128 d =
                static_cast<unsigned __int128>(w_[i]) - static_cast<unsigned __int128>(o.w_[i]) - borrow;
            w_[i] = static_cast<std::uint64_t>(d);
            borrow = (d >> 64) ? 1 : 0;
        }
        return *this;
    }

    BigUint& mul_u64(std::uint64_t m) {
        if (m == 0) {
            w_.fill(0);
            return *this;
        }
        unsigned __int128 carry = 0;
        for (std::size_t i = 0; i < kWords; ++i) {
            const unsigned __int128 p = static_cast<unsigned __int128>(w_[i]) * m + carry;
            w_[i] = static_cast<std::uint64_t>(p);
            carry = p >> 64;
        }
        if (carry != 0) over_ = true;
        return *this;
    }

    BigUint& mul(const BigUint& o) {
        std::array<std::uint64_t, kWords> r{};
        bool over = over_ || o.over_;
        for (std::size_t i = 0; i < kWords; ++i) {
            if (w_[i] == 0) continue;
            unsigned __int128 carry = 0;
            for (std::size_t j = 0; j + i < kWords; ++j) {
                const unsigned __int128 p = static_cast<unsigned __int128>(w_[i]) * o.w_[j] +
                                            static_cast<unsigned __int128>(r[i + j]) + carry;
                r[i + j] = static_cast<std::uint64_t>(p);
                carry = p >> 64;
            }
            if (carry != 0) over = true;
            for (std::size_t j = kWords - i; j < kWords; ++j)
                if (o.w_[j] != 0) over = true;
        }
        w_ = r;
        over_ = over;
        return *this;
    }

    int cmp(const BigUint& o) const {
        for (std::size_t i = kWords; i-- > 0;) {
            if (w_[i] != o.w_[i]) return w_[i] < o.w_[i] ? -1 : 1;
        }
        return 0;
    }

    void shl1() {
        std::uint64_t carry = 0;
        for (std::size_t i = 0; i < kWords; ++i) {
            const std::uint64_t next = w_[i] >> 63;
            w_[i] = (w_[i] << 1) | carry;
            carry = next;
        }
    }

    // Truncating division, shift-and-subtract. A zero divisor yields zero; the
    // reward config rejects a zero minting period before this can be reached.
    BigUint& div(const BigUint& d) {
        if (d.is_zero()) {
            w_.fill(0);
            return *this;
        }
        BigUint q, r;
        for (std::size_t i = kBits; i-- > 0;) {
            r.shl1();
            if (bit(i)) r.w_[0] |= 1;
            if (r.cmp(d) >= 0) {
                r.sub(d);
                q.set_bit(i);
            }
        }
        const bool over = over_;
        *this = q;
        over_ = over;
        return *this;
    }

    BigUint& div_u64(std::uint64_t d) {
        if (d == 0) {
            w_.fill(0);
            return *this;
        }
        unsigned __int128 rem = 0;
        for (std::size_t i = kWords; i-- > 0;) {
            const unsigned __int128 cur = (rem << 64) | w_[i];
            w_[i] = static_cast<std::uint64_t>(cur / d);
            rem = cur % d;
        }
        return *this;
    }

  private:
    std::array<std::uint64_t, kWords> w_{};
    bool over_ = false;
};

}  // namespace lux::platformvm
