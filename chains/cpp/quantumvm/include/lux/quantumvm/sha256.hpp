// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// sha256.hpp — SHA-256 (FIPS 180-4), the one hash the Q-chain identifies with.
//
// A transaction's id is sha256 of its signed bytes; a UTXO's id is sha256 of a
// big-endian output index followed by the producing tx id. Both are consensus,
// so this is a from-the-standard implementation with the standard's own test
// vectors beside it, not a call into whatever a platform happens to ship.

#pragma once

#include <array>
#include <cstdint>
#include <cstring>
#include <span>

namespace lux::quantumvm {

using Hash256 = std::array<std::uint8_t, 32>;

namespace detail {

inline constexpr std::uint32_t kSha256K[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2};

inline std::uint32_t rotr(std::uint32_t x, unsigned n) { return (x >> n) | (x << (32 - n)); }

}  // namespace detail

class Sha256 {
  public:
    Sha256() { reset(); }

    void reset() {
        h_ = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19};
        len_ = 0;
        fill_ = 0;
    }

    void update(std::span<const std::uint8_t> data) {
        len_ += data.size();
        std::size_t i = 0;
        if (fill_ > 0) {
            const std::size_t want = 64 - fill_;
            const std::size_t take = data.size() < want ? data.size() : want;
            std::memcpy(block_.data() + fill_, data.data(), take);
            fill_ += take;
            i = take;
            if (fill_ < 64) return;
            compress(block_.data());
            fill_ = 0;
        }
        for (; i + 64 <= data.size(); i += 64) compress(data.data() + i);
        if (i < data.size()) {
            fill_ = data.size() - i;
            std::memcpy(block_.data(), data.data() + i, fill_);
        }
    }

    Hash256 finish() {
        const std::uint64_t bits = static_cast<std::uint64_t>(len_) * 8;
        std::uint8_t pad[72] = {0x80};
        const std::size_t pad_len = (fill_ < 56) ? (56 - fill_) : (120 - fill_);
        update({pad, pad_len});
        std::uint8_t be[8];
        for (int i = 0; i < 8; ++i) be[i] = static_cast<std::uint8_t>(bits >> (56 - 8 * i));
        update({be, 8});
        Hash256 out{};
        for (int i = 0; i < 8; ++i) {
            out[4 * i + 0] = static_cast<std::uint8_t>(h_[i] >> 24);
            out[4 * i + 1] = static_cast<std::uint8_t>(h_[i] >> 16);
            out[4 * i + 2] = static_cast<std::uint8_t>(h_[i] >> 8);
            out[4 * i + 3] = static_cast<std::uint8_t>(h_[i]);
        }
        return out;
    }

  private:
    void compress(const std::uint8_t* p) {
        std::uint32_t w[64];
        for (int i = 0; i < 16; ++i) {
            w[i] = (static_cast<std::uint32_t>(p[4 * i]) << 24) | (static_cast<std::uint32_t>(p[4 * i + 1]) << 16) |
                   (static_cast<std::uint32_t>(p[4 * i + 2]) << 8) | static_cast<std::uint32_t>(p[4 * i + 3]);
        }
        for (int i = 16; i < 64; ++i) {
            const std::uint32_t s0 = detail::rotr(w[i - 15], 7) ^ detail::rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
            const std::uint32_t s1 = detail::rotr(w[i - 2], 17) ^ detail::rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16] + s0 + w[i - 7] + s1;
        }
        std::uint32_t a = h_[0], b = h_[1], c = h_[2], d = h_[3];
        std::uint32_t e = h_[4], f = h_[5], g = h_[6], hh = h_[7];
        for (int i = 0; i < 64; ++i) {
            const std::uint32_t S1 = detail::rotr(e, 6) ^ detail::rotr(e, 11) ^ detail::rotr(e, 25);
            const std::uint32_t ch = (e & f) ^ (~e & g);
            const std::uint32_t t1 = hh + S1 + ch + detail::kSha256K[i] + w[i];
            const std::uint32_t S0 = detail::rotr(a, 2) ^ detail::rotr(a, 13) ^ detail::rotr(a, 22);
            const std::uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
            const std::uint32_t t2 = S0 + maj;
            hh = g; g = f; f = e; e = d + t1;
            d = c; c = b; b = a; a = t1 + t2;
        }
        h_[0] += a; h_[1] += b; h_[2] += c; h_[3] += d;
        h_[4] += e; h_[5] += f; h_[6] += g; h_[7] += hh;
    }

    std::array<std::uint32_t, 8> h_{};
    std::array<std::uint8_t, 64> block_{};
    std::size_t fill_ = 0;
    std::size_t len_ = 0;
};

inline Hash256 sha256(std::span<const std::uint8_t> data) {
    Sha256 h;
    h.update(data);
    return h.finish();
}

}  // namespace lux::quantumvm
