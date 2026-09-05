// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the names the F-Chain uses, and the encodings that render them.
//
//   Id       32 bytes: a chain, a block, a transaction, a handle, a digest
//   Account  20 bytes: a fee payer, an owner, a grantee (Go fee.Account)
//   NodeId   20 bytes: a committee member's node (Go ids.NodeID)
//
// Id is deliberately std::array<uint8_t,32>, the SAME type the node's VM seam
// calls lux::consensus::Id, so a block id crosses into consensus with no
// conversion and no second definition.
//
// The renderings are here because they are consensus-visible: a record persists
// its chain id as cb58 and a committee member as "NodeID-<cb58>", so a port that
// renders either differently writes a different database from the chain it is
// meant to agree with.

#pragma once

#include <array>
#include <cstdint>
#include <span>
#include <string>
#include <string_view>
#include <vector>

namespace lux::fhevm {

using Id = std::array<std::uint8_t, 32>;
using Account = std::array<std::uint8_t, 20>;
using NodeId = std::array<std::uint8_t, 20>;
using Bytes = std::vector<std::uint8_t>;
using ByteView = std::span<const std::uint8_t>;

inline constexpr Id kEmptyId{};
inline constexpr Account kEmptyAccount{};

inline ByteView view(const Bytes& b) { return ByteView(b.data(), b.size()); }
template <std::size_t N>
inline ByteView view(const std::array<std::uint8_t, N>& a) {
    return ByteView(a.data(), a.size());
}
inline ByteView view(std::string_view s) {
    return ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size());
}

// ---- sha256, the F-Chain's one hash ----------------------------------------

Id sha256(ByteView data);

// Hasher is sha256 fed in pieces — every derivation in this chain hashes a
// domain tag then a sequence of fields, and one incremental hasher keeps that
// from becoming a concatenation the caller has to get right.
class Hasher {
public:
    Hasher();
    void write(ByteView b);
    void write(std::string_view s) { write(view(s)); }
    // be64 is the eight-byte big-endian field every derivation counts with.
    void be64(std::uint64_t v);
    // len_prefixed writes len(b) then b, so concatenated fields cannot be
    // re-split at a different boundary to forge a colliding digest.
    void len_prefixed(ByteView b);
    Id sum();

private:
    std::array<std::uint32_t, 8> h_{};
    std::array<std::uint8_t, 64> block_{};
    std::size_t block_len_ = 0;
    std::uint64_t total_ = 0;
};

// ---- renderings -------------------------------------------------------------

std::string hex(ByteView b);
inline std::string hex(const Id& id) { return hex(view(id)); }
inline std::string hex(const Account& a) { return hex(view(a)); }
// from_hex accepts an optional "0x" prefix and refuses anything that is not an
// even run of hex digits.
bool from_hex(std::string_view s, Bytes* out);

// cb58 is base58 over the payload followed by the first four bytes of its
// sha256 — Go's cb58.Encode. It is what an Account and an Id render as in JSON.
std::string cb58(ByteView b);
bool cb58_decode(std::string_view s, Bytes* out);

// base64 is standard-alphabet, padded — what encoding/json writes a []byte as.
std::string base64(ByteView b);
bool base64_decode(std::string_view s, Bytes* out);

// account_string / node_id_string / id_string are the three renderings the
// chain's JSON uses, each matching its Go counterpart exactly — including the
// native-chain names an Id answers with instead of cb58.
std::string account_string(const Account& a);
std::string node_id_string(const NodeId& n);
bool node_id_from_string(std::string_view s, NodeId* out);
std::string id_string(const Id& id);
// native_chain_string is Go ids.NativeChainString: the well-known name of a
// chain id that is 31 zero bytes and a letter, or "" for anything else.
std::string native_chain_string(const Id& id);

}  // namespace lux::fhevm
