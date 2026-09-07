// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the fixed-width names every chain in this node uses, and the hashes
// that derive them. ONE definition, because a name that means one thing on the
// P-chain and another on the X-chain is two chains wearing one node's binary.
//
//   Id      32 bytes — a transaction, a block, a chain, a network, an asset
//   ShortId 20 bytes — an address
//   NodeId  20 bytes — a validator's identity
//
// WHY Id IS A RAW ARRAY AND NodeId IS NOT.
//
// Id is std::array<uint8_t,32>, which is EXACTLY lux::consensus::Id — the type
// the node's VM seam speaks. A block id therefore crosses into consensus with
// no conversion and no second definition, and a conversion that does not exist
// is a conversion that cannot be got wrong. That is a cross-module contract, so
// it decides Id's shape.
//
// NodeId crosses no seam, and it shares its width with an address. Passing one
// where the other belongs is exactly the confusion a validator set cannot
// afford, so NodeId is a distinct type and the compiler refuses the swap. One
// name is strengthened, for a reason that can be stated in a sentence; the rest
// are the bytes they are.
//
// Every operation is a FREE function — hex(x), view(x), prefix_id(x, n) — so
// all three names are handled the same way and none of them carries an API of
// its own.

#pragma once

#include <array>
#include <compare>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <span>
#include <string>
#include <vector>

namespace lux::core {

inline constexpr std::size_t kIdLen = 32;
inline constexpr std::size_t kShortIdLen = 20;
inline constexpr std::size_t kNodeIdLen = kShortIdLen;

using Bytes = std::vector<std::uint8_t>;
using ByteView = std::span<const std::uint8_t>;

using Id = std::array<std::uint8_t, kIdLen>;
using ShortId = std::array<std::uint8_t, kShortIdLen>;

// A validator's identity. Twenty bytes, like an address, and deliberately not
// an address: the one member is named `b` so the bytes are reachable, and there
// is no implicit conversion in either direction.
struct NodeId {
    std::array<std::uint8_t, kNodeIdLen> b{};

    friend bool operator==(const NodeId&, const NodeId&) = default;
    friend std::strong_ordering operator<=>(const NodeId& x, const NodeId& y) {
        const int r = std::memcmp(x.b.data(), y.b.data(), kNodeIdLen);
        return r < 0   ? std::strong_ordering::less
               : r > 0 ? std::strong_ordering::greater
                       : std::strong_ordering::equal;
    }
};

inline constexpr Id kEmptyId{};
inline constexpr ShortId kEmptyShortId{};
inline constexpr NodeId kEmptyNodeId{};

// ── viewing

inline ByteView view(const Bytes& b) { return ByteView(b.data(), b.size()); }
template <std::size_t N>
inline ByteView view(const std::array<std::uint8_t, N>& a) {
    return ByteView(a.data(), a.size());
}
inline ByteView view(const NodeId& n) { return ByteView(n.b.data(), n.b.size()); }

// ── making one out of however many bytes are on hand
//
// Short input is right-padded with zeros and long input is truncated, which is
// what every caller here wants: the bytes come from a wire field that is
// already the right width, or from a test that wrote three of them.

template <std::size_t N>
inline std::array<std::uint8_t, N> bytes_from(ByteView src) {
    std::array<std::uint8_t, N> out{};
    const std::size_t n = src.size() < N ? src.size() : N;
    if (n > 0) std::memcpy(out.data(), src.data(), n);
    return out;
}

inline Id id_from(ByteView src) { return bytes_from<kIdLen>(src); }
inline ShortId short_id_from(ByteView src) { return bytes_from<kShortIdLen>(src); }
inline NodeId node_id_from(ByteView src) { return NodeId{bytes_from<kNodeIdLen>(src)}; }

// ── rendering. Not a wire format: this is what a failure message says.

std::string hex(ByteView b);
template <std::size_t N>
inline std::string hex(const std::array<std::uint8_t, N>& a) {
    return hex(view(a));
}
inline std::string hex(const NodeId& n) { return hex(view(n)); }

// ── the hashes names are derived by

// sha256 of the input. The one hash a name is made with.
Id sha256(ByteView data);

// sha256(be64(prefix) || id) — how a UTXO gets its name from the transaction
// that produced it and the index it sat at. Go: ids.ID.Prefix.
Id prefix_id(const Id& id, std::uint64_t prefix);

// sha256(id || be32(suffix)) — hashing the counter AFTER the id, where
// prefix_id hashes it before. How a genesis L1 validator gets its name from its
// network and its index, so the names are derived rather than assigned.
Id append_id(const Id& id, std::uint32_t suffix);

// ripemd160(sha256(key)) — the Lux address of a 33-byte compressed public key.
// Go: hash.PubkeyBytesToAddress.
ShortId pubkey_to_address(ByteView compressed_pubkey);

}  // namespace lux::core
