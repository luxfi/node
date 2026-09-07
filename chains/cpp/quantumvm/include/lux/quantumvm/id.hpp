// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the name of a thing on this chain, and how it is written down.
//
// There is ONE id type and it is the node's: `lux::node::Id`, which the VM seam
// (lux/node/vm.hpp) is already written in. A chain that declared its own
// 32-byte name would have to convert at every call into the node, and the
// conversion is the place two id types drift apart. So this header imports
// rather than restates, and everything below is a free function over that type.
//
// A block id, a transaction id and the chain's own id are all content hashes,
// so `of()` is sha256 and there is nothing else to derive.
//
// text() renders an id the way Go's ids.ID.String() does — CB58: base58 over
// the 32 bytes followed by the last four bytes of their sha256. That is what
// the golden genesis id in the Go tests is written in, so a C++ port that
// spells its ids any other way cannot check itself against them.

#pragma once

#include "lux/quantumvm/sha256.hpp"

#include "lux/node/vm.hpp"

#include <algorithm>
#include <array>
#include <cstdint>
#include <span>
#include <string>
#include <vector>

namespace lux::quantumvm {

using Id = lux::node::Id;
using Bytes = std::vector<std::uint8_t>;
using ByteView = std::span<const std::uint8_t>;

inline constexpr std::size_t kIdLen = 32;
static_assert(std::tuple_size<Id>::value == kIdLen, "the node's id is 32 bytes");

inline const Id kEmptyId{};

inline bool empty(const Id& id) {
    for (auto b : id)
        if (b != 0) return false;
    return true;
}

// The content id of a byte string: sha256 over exactly those bytes. Every id
// this chain produces comes from here, so `id == sha256(bytes)` holds for a
// block and a transaction alike.
inline Id of(ByteView data) { return sha256(data); }

// An id read off the wire. Fewer than 32 bytes is not an id: the caller checked
// the width, and a short read that silently zero-padded would name a different
// block.
inline Id id_from(ByteView src) {
    Id out{};
    const std::size_t n = std::min(src.size(), kIdLen);
    if (n > 0) std::copy_n(src.begin(), n, out.begin());
    return out;
}

inline ByteView view(const Id& id) { return ByteView(id.data(), id.size()); }
inline ByteView view(const Bytes& b) { return ByteView(b.data(), b.size()); }

// ── CB58, the way an id is written down
//
// base58(payload || sha256(payload)[28:32]), over the Bitcoin alphabet. The
// checksum is what makes a mistyped id a refusal rather than a different block.
inline constexpr char kBase58Alphabet[] =
    "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";

inline std::string base58(ByteView data) {
    // Leading zero bytes are not a number, they are positions, and base58
    // spells each of them '1'.
    std::size_t zeros = 0;
    while (zeros < data.size() && data[zeros] == 0) ++zeros;

    std::vector<std::uint8_t> digits;
    digits.reserve(data.size() * 138 / 100 + 1);
    for (std::size_t i = zeros; i < data.size(); ++i) {
        int carry = data[i];
        for (auto it = digits.rbegin(); it != digits.rend(); ++it) {
            carry += 256 * *it;
            *it = static_cast<std::uint8_t>(carry % 58);
            carry /= 58;
        }
        while (carry > 0) {
            digits.insert(digits.begin(), static_cast<std::uint8_t>(carry % 58));
            carry /= 58;
        }
    }

    std::string out(zeros, '1');
    out.reserve(zeros + digits.size());
    for (auto d : digits) out += kBase58Alphabet[d];
    return out;
}

// The native-chain shortcut Go's ids.ID.String() takes first: an id that is
// thirty-one zero bytes and one letter is a primary-network chain, and is
// written as that letter behind thirty-two '1's. No content hash is ever that,
// so this arm exists for the chain ids a node is configured with, not for
// anything this VM derives.
inline std::string text(const Id& id) {
    bool native = true;
    for (std::size_t i = 0; i < 31; ++i)
        if (id[i] != 0) {
            native = false;
            break;
        }
    if (native) {
        const char c = static_cast<char>(id[31]);
        static constexpr std::string_view kLetters = "PCXQABMFZGIKD";
        if (kLetters.find(c) != std::string_view::npos)
            return std::string(32, '1') + c;
    }

    std::array<std::uint8_t, kIdLen + 4> checked{};
    std::copy(id.begin(), id.end(), checked.begin());
    const Id sum = of(view(id));
    std::copy(sum.end() - 4, sum.end(), checked.begin() + kIdLen);
    return base58(ByteView(checked.data(), checked.size()));
}

inline std::string hex(ByteView b) {
    static constexpr char kDigits[] = "0123456789abcdef";
    std::string s;
    s.reserve(b.size() * 2);
    for (auto c : b) {
        s += kDigits[c >> 4];
        s += kDigits[c & 0x0f];
    }
    return s;
}

}  // namespace lux::quantumvm
