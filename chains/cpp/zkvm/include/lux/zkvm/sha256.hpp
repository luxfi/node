// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#pragma once

#include <array>
#include <cstdint>
#include <cstring>
#include <span>

namespace lux::zkvm {

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
        while (i + 64 <= data.size()) {
            compress(data.data() + i);
            i += 64;
        }
        if (i < data.size()) {
            std::memcpy(block_.data(), data.data() + i, data.size() - i);
            fill_ = data.size() - i;
        }
    }

    Hash256 finish() {
        block_[fill_++] = 0x80;
        if (fill_ > 56) {
            std::memset(block_.data() + fill_, 0, 64 - fill_);
            compress(block_.data());
            fill_ = 0;
        }
        std::memset(block_.data() + fill_, 0, 56 - fill_);
        const std::uint64_t bit_len = len_ * 8;
        for (int j = 0; j < 8; ++j) {
            block_[56 + j] = static_cast<std::uint8_t>(bit_len >> (56 - j * 8));
        }
        compress(block_.data());

        Hash256 out;
        for (int j = 0; j < 8; ++j) {
            out[j * 4 + 0] = static_cast<std::uint8_t>(h_[j] >> 24);
            out[j * 4 + 1] = static_cast<std::uint8_t>(h_[j] >> 16);
            out[j * 4 + 2] = static_cast<std::uint8_t>(h_[j] >> 8);
            out[j * 4 + 3] = static_cast<std::uint8_t>(h_[j]);
        }
        return out;
    }

    static Hash256 hash(std::span<const std::uint8_t> data) {
        Sha256 s;
        s.update(data);
        return s.finish();
    }

  private:
    void compress(const std::uint8_t* chunk) {
        std::uint32_t w[64];
        for (int t = 0; t < 16; ++t) {
            w[t] = (static_cast<std::uint32_t>(chunk[t * 4 + 0]) << 24) |
                   (static_cast<std::uint32_t>(chunk[t * 4 + 1]) << 16) |
                   (static_cast<std::uint32_t>(chunk[t * 4 + 2]) << 8) |
                   (static_cast<std::uint32_t>(chunk[t * 4 + 3]));
        }
        for (int t = 16; t < 64; ++t) {
            const std::uint32_t s0 = detail::rotr(w[t - 15], 7) ^ detail::rotr(w[t - 15], 18) ^ (w[t - 15] >> 3);
            const std::uint32_t s1 = detail::rotr(w[t - 2], 17) ^ detail::rotr(w[t - 2], 19) ^ (w[t - 2] >> 10);
            w[t] = w[t - 16] + s0 + w[t - 7] + s1;
        }

        std::uint32_t a = h_[0], b = h_[1], c = h_[2], d = h_[3];
        std::uint32_t e = h_[4], f = h_[5], g = h_[6], h = h_[7];

        for (int t = 0; t < 64; ++t) {
            const std::uint32_t s1 = detail::rotr(e, 6) ^ detail::rotr(e, 11) ^ detail::rotr(e, 25);
            const std::uint32_t ch = (e & f) ^ (~e & g);
            const std::uint32_t temp1 = h + s1 + ch + detail::kSha256K[t] + w[t];
            const std::uint32_t s0 = detail::rotr(a, 2) ^ detail::rotr(a, 13) ^ detail::rotr(a, 22);
            const std::uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
            const std::uint32_t temp2 = s0 + maj;

            h = g;
            g = f;
            f = e;
            e = d + temp1;
            d = c;
            c = b;
            b = a;
            a = temp1 + temp2;
        }

        h_[0] += a;
        h_[1] += b;
        h_[2] += c;
        h_[3] += d;
        h_[4] += e;
        h_[5] += f;
        h_[6] += g;
        h_[7] += h;
    }

    std::array<std::uint32_t, 8> h_;
    std::uint64_t len_{0};
    std::array<std::uint8_t, 64> block_;
    std::size_t fill_{0};
};

}  // namespace lux::zkvm
