// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the one identifier the Z-Chain names things with, and the one hash
// that derives it.
//
// Id is deliberately std::array<uint8_t,32>, which is the SAME type the node's
// VM seam calls lux::consensus::Id — so a block id crosses into consensus with
// no conversion and no second definition of what an id is.
//
// SHA-256 is the ONLY hash on this chain: a transaction's id, a block's id, a
// vertex's id, the state root and the chain binding are all sha256 folds. There
// is no second digest and no hardware-conditional one — a root that depended on
// whether a node has an accelerator would split the validator set in two.

#pragma once

#include <algorithm>
#include <array>
#include <cstdint>
#include <cstring>
#include <map>
#include <set>
#include <span>
#include <string>
#include <vector>

namespace lux::zkvm {

using Id = std::array<std::uint8_t, 32>;

inline constexpr Id kEmptyId{};

using Bytes = std::vector<std::uint8_t>;
using ByteView = std::span<const std::uint8_t>;

inline ByteView view(const Bytes& b) { return ByteView(b.data(), b.size()); }

// bytes_of copies a view into an owned buffer, sizing it from the view's own
// size rather than from the distance between two iterators — which is the one
// form a compiler can bound, and the one that keeps a keyed map's comparison
// provably in range.
inline Bytes bytes_of(ByteView v) {
    Bytes out(v.size());
    std::copy(v.begin(), v.end(), out.begin());
    return out;
}
template <std::size_t N>
inline ByteView view(const std::array<std::uint8_t, N>& a) {
    return ByteView(a.data(), a.size());
}

// ByteOrder is the order every byte-keyed collection on this chain is kept in:
// lexicographic over the bytes, and a prefix sorts before what extends it. It is
// STATED rather than inherited from the container's element type, because the
// order is a promise — the sets rebuilt at boot are enumerated by it — and
// because a comparison over a fixed length is one a reader and a compiler can
// both bound.
//
// It is transparent, so a lookup takes a view and copies nothing.
struct ByteOrder {
    using is_transparent = void;
    bool operator()(ByteView a, ByteView b) const {
        const std::size_t n = a.size() < b.size() ? a.size() : b.size();
        const int c = n == 0 ? 0 : std::memcmp(a.data(), b.data(), n);
        return c != 0 ? c < 0 : a.size() < b.size();
    }
};

template <class V>
using ByteMap = std::map<Bytes, V, ByteOrder>;
using ByteSet = std::set<Bytes, ByteOrder>;

// sha256 of the input (Go: crypto/sha256 over the same preimage).
Id sha256(ByteView data);

// Hasher is the streaming form, so a preimage written field by field in Go —
// `h := sha256.New(); h.Write(...)` — is written field by field here too and
// the two cannot drift by a buffering difference.
class Hasher {
public:
    void write(ByteView b) { buf_.insert(buf_.end(), b.begin(), b.end()); }
    void write_byte(std::uint8_t b) { buf_.push_back(b); }

    // num is Go's binary.Write(h, BigEndian, uint64) — and int64 goes through
    // the same eight bytes, two's complement, which is what Go writes.
    void num(std::uint64_t v);
    void num(std::int64_t v) { num(static_cast<std::uint64_t>(v)); }
    void num32(std::uint32_t v);

    // blob is a length-prefixed byte run: num(len) then the bytes. Every
    // variable-length field goes through it, so a byte cannot move from the end
    // of one field to the start of the next without the digest noticing.
    void blob(ByteView b) {
        num(static_cast<std::uint64_t>(b.size()));
        write(b);
    }

    void count(std::size_t n) { num(static_cast<std::uint64_t>(n)); }
    void present(bool yes) { write_byte(yes ? 1 : 0); }

    Id sum() const;
    const Bytes& preimage() const { return buf_; }

private:
    Bytes buf_;
};

// hex renders bytes for a test failure message and for the golden corpus. Not a
// wire format.
std::string hex(ByteView b);
inline std::string hex(const Id& id) { return hex(view(id)); }

// unhex is the inverse, for reading the golden corpus. An odd length or a
// non-hex digit yields an empty vector, which no golden value is.
Bytes unhex(std::string_view s);

}  // namespace lux::zkvm
