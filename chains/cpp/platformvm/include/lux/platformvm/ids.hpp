// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// ids.hpp — the three fixed-width names this chain uses.
//
//   Id      32 bytes — a transaction, a chain, a network, an asset
//   ShortId 20 bytes — an address
//   NodeId  20 bytes — a validator's identity (the same width, a different kind)
//
// They are distinct types on purpose: an address and a node id are both twenty
// bytes and passing one where the other belongs is exactly the confusion a
// validator-set implementation cannot afford.
//
// Prefix() is the UTXO id derivation: big-endian prefixes, then the id, then
// sha256. Rendered from Go ids.ID.Prefix / hash.ComputeHash256Array.

#pragma once

#include "lux/platformvm/sha256.hpp"

#include <array>
#include <compare>
#include <cstdint>
#include <cstring>
#include <span>
#include <string>
#include <vector>

namespace lux::platformvm {

inline constexpr std::size_t kIdLen = 32;
inline constexpr std::size_t kShortIdLen = 20;
inline constexpr std::size_t kNodeIdLen = kShortIdLen;

template <std::size_t N, int Tag>
struct FixedId {
    std::array<std::uint8_t, N> b{};

    static constexpr std::size_t size() { return N; }
    const std::uint8_t* data() const { return b.data(); }
    std::uint8_t* data() { return b.data(); }
    std::span<const std::uint8_t> span() const { return {b.data(), N}; }

    bool empty() const {
        for (auto x : b)
            if (x != 0) return false;
        return true;
    }

    static FixedId from(std::span<const std::uint8_t> src) {
        FixedId out{};
        const std::size_t n = src.size() < N ? src.size() : N;
        if (n > 0) std::memcpy(out.b.data(), src.data(), n);
        return out;
    }

    friend bool operator==(const FixedId& a, const FixedId& c) { return a.b == c.b; }
    friend std::strong_ordering operator<=>(const FixedId& a, const FixedId& c) {
        const int r = std::memcmp(a.b.data(), c.b.data(), N);
        return r < 0 ? std::strong_ordering::less : (r > 0 ? std::strong_ordering::greater : std::strong_ordering::equal);
    }

    std::string hex() const {
        static const char* d = "0123456789abcdef";
        std::string s;
        s.reserve(2 * N);
        for (auto x : b) {
            s.push_back(d[x >> 4]);
            s.push_back(d[x & 0xf]);
        }
        return s;
    }
};

using Id = FixedId<kIdLen, 0>;
using ShortId = FixedId<kShortIdLen, 1>;
using NodeId = FixedId<kNodeIdLen, 2>;

inline const Id kEmptyId{};
inline const NodeId kEmptyNodeId{};

// The primary network is the identity element of the network hierarchy: an L1's
// parent, and the network every P-chain validator validates. It is the zero id.
inline const Id kPrimaryNetworkId{};

// Prefix hashes big-endian prefixes ahead of the id. This is how a UTXO gets its
// name from the tx that produced it and the index it sat at.
inline Id prefix_id(const Id& id, std::uint64_t p) {
    std::vector<std::uint8_t> buf(8 + kIdLen);
    for (int i = 0; i < 8; ++i) buf[i] = static_cast<std::uint8_t>(p >> (56 - 8 * i));
    std::memcpy(buf.data() + 8, id.b.data(), kIdLen);
    const Hash256 h = sha256(buf);
    Id out{};
    std::memcpy(out.b.data(), h.data(), kIdLen);
    return out;
}

inline Id id_from_hash(const Hash256& h) {
    Id out{};
    std::memcpy(out.b.data(), h.data(), kIdLen);
    return out;
}

}  // namespace lux::platformvm

namespace std {
template <std::size_t N, int Tag>
struct hash<lux::platformvm::FixedId<N, Tag>> {
    std::size_t operator()(const lux::platformvm::FixedId<N, Tag>& v) const noexcept {
        std::size_t acc = 1469598103934665603ull;
        for (auto x : v.b) {
            acc ^= x;
            acc *= 1099511628211ull;
        }
        return acc;
    }
};
}  // namespace std
