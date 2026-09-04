// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the two identifiers the X-Chain names things with, and the hashes
// that derive them. Id is 32 bytes (a tx, a block, an asset, a chain); ShortId
// is 20 bytes (an address).
//
// Id is deliberately std::array<uint8_t,32>, which is the SAME type the node's
// VM seam calls lux::consensus::Id — so a block id crosses into consensus with
// no conversion and no second definition.

#pragma once

#include <array>
#include <cstdint>
#include <span>
#include <string>
#include <vector>

namespace lux::xvm {

using Id = std::array<std::uint8_t, 32>;
using ShortId = std::array<std::uint8_t, 20>;

inline constexpr Id kEmptyId{};
inline constexpr ShortId kEmptyShortId{};

using Bytes = std::vector<std::uint8_t>;
using ByteView = std::span<const std::uint8_t>;

inline ByteView view(const Bytes& b) { return ByteView(b.data(), b.size()); }
template <std::size_t N>
inline ByteView view(const std::array<std::uint8_t, N>& a) {
    return ByteView(a.data(), a.size());
}

// sha256 of the input; the X-Chain's ONE hash (Go: hash.ComputeHash256Array).
Id sha256(ByteView data);

// ripemd160(sha256(key)) — the Lux address of a 33-byte compressed public key
// (Go: hash.PubkeyBytesToAddress).
ShortId pubkey_to_address(ByteView compressed_pubkey);

// id_prefix appends this id under a big-endian uint64 prefix and re-hashes:
// sha256(be64(prefix) || id). This is Go's ids.ID.Prefix, and it is what turns
// a (TxID, OutputIndex) pair into the UTXO's InputID.
Id id_prefix(const Id& id, std::uint64_t prefix);

// hex renders an id for a test failure message. Not a wire format.
std::string hex(ByteView b);
inline std::string hex(const Id& id) { return hex(view(id)); }

}  // namespace lux::xvm
