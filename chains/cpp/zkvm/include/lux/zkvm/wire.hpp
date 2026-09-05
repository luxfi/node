// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire.hpp — native-ZAP struct-is-wire for the Z-Chain, and the two rules every
// frame on this chain is held to.
//
//   CANONICAL   one value has one byte string. parse_frame refuses a buffer the
//               frame does not entirely account for, so a peer cannot append
//               bytes to a transaction the node already holds and hand back a
//               "new" one under a fresh identity.
//
//   AGREEMENT   a length vector and the blob it indexes both come from the peer
//               and both are checked. A length the blob cannot back is refused
//               rather than truncated, and blob bytes no length claims are
//               refused too — either way the value read is not the value sent.
//
// Neither rule is a nicety. Together they are why one logical transaction has
// exactly one encoding, which is what consensus decides between blocks with.
//
// A DECLARED LENGTH NEVER SIZES AN ALLOCATION. Reservations come from the
// length vector, which the frame already bounds; a peer's 0xFFFFFFFF is a
// refusal, not 4 GiB of memory.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/zap.hpp"

#include <expected>
#include <functional>
#include <string>
#include <vector>

namespace lux::zkvm::wire {

template <class T>
using Result = std::expected<T, std::string>;

// The frame declares fewer bytes than were handed to the parser. One value has
// one byte string, so the remainder belongs to nobody. (Go: errTrailingBytes.)
inline constexpr const char* kErrTrailingBytes = "zkvm wire: trailing bytes";

// A declared length vector does not exactly cover the blob it indexes: either a
// length reaches past the end, or blob bytes are left that no length claims.
// (Go: errLength.)
inline constexpr const char* kErrLength =
    "zkvm wire: declared length does not match blob";

// parse_frame returns the root object of a zap frame and refuses one that does
// not account for every byte handed in. Every parser in this port goes through
// it, so canonicality is decided in one place rather than per type.
inline Result<zap::Object> parse_frame(ByteView data, zap::Message* keep) {
    std::string err;
    if (!zap::Message::parse(data, keep, &err)) return std::unexpected(err);
    if (keep->size() != data.size()) return std::unexpected(kErrTrailingBytes);
    return keep->root();
}

inline int write_u32_list(zap::Builder& b, const std::vector<std::uint32_t>& xs) {
    auto lb = b.start_list(4);
    for (std::uint32_t x : xs) lb.add_u32(x);
    return lb.finish().first;
}

inline std::vector<std::uint32_t> read_u32_list(const zap::Object& o, int ptr_off) {
    auto l = o.list_stride(ptr_off, 4);
    std::vector<std::uint32_t> out(std::size_t(l.len()));
    for (int i = 0; i < l.len(); ++i) out[std::size_t(i)] = l.u32(i);
    return out;
}

inline Bytes cp(ByteView b) { return bytes_of(b); }

inline Id read_id(const zap::Object& o, int off) {
    Id id{};
    auto s = o.bytes_fixed_slice(off, 32);
    if (s.size() == 32) std::copy(s.begin(), s.end(), id.begin());
    return id;
}

// pack_bytes_list flattens a list of byte runs into (lengths, concat blob).
inline void pack_bytes_list(const std::vector<Bytes>& xs, std::vector<std::uint32_t>& lens,
                            Bytes& blob) {
    lens.clear();
    lens.reserve(xs.size());
    for (const auto& x : xs) {
        lens.push_back(std::uint32_t(x.size()));
        blob.insert(blob.end(), x.begin(), x.end());
    }
}

// unpack_bytes_list re-splits a concatenated blob by its declared lengths under
// the agreement rule above.
inline Result<std::vector<Bytes>> unpack_bytes_list(const std::vector<std::uint32_t>& lens,
                                                    ByteView blob) {
    if (lens.empty()) {
        if (!blob.empty()) return std::unexpected(kErrLength);
        return std::vector<Bytes>{};
    }
    std::vector<Bytes> out;
    out.reserve(lens.size());  // from the length VECTOR, which the frame bounds
    std::size_t pos = 0;
    for (std::size_t i = 0; i < lens.size(); ++i) {
        const std::size_t l = lens[i];
        if (l > blob.size() - pos)
            return std::unexpected(std::string(kErrLength) + ": entry " + std::to_string(i));
        out.push_back(cp(blob.subspan(pos, l)));
        pos += l;
    }
    if (pos != blob.size()) return std::unexpected(kErrLength);
    return out;
}

// pack_objs marshals each item and returns (per-item lengths, concat blob).
template <class T, class Marshal>
inline void pack_objs(const std::vector<T>& items, Marshal marshal,
                      std::vector<std::uint32_t>& lens, Bytes& blob) {
    lens.clear();
    lens.reserve(items.size());
    for (const auto& it : items) {
        const Bytes m = marshal(it);
        lens.push_back(std::uint32_t(m.size()));
        blob.insert(blob.end(), m.begin(), m.end());
    }
}

// unpack_objs re-splits a packed blob by lengths and parses each sub-object,
// under the same agreement rule as unpack_bytes_list.
template <class T, class Parse>
inline Result<std::vector<T>> unpack_objs(const std::vector<std::uint32_t>& lens, ByteView blob,
                                          Parse parse) {
    if (lens.empty()) {
        if (!blob.empty()) return std::unexpected(kErrLength);
        return std::vector<T>{};
    }
    std::vector<T> out;
    out.reserve(lens.size());
    std::size_t pos = 0;
    for (std::size_t i = 0; i < lens.size(); ++i) {
        const std::size_t l = lens[i];
        if (l > blob.size() - pos)
            return std::unexpected(std::string(kErrLength) + ": item " + std::to_string(i));
        auto v = parse(blob.subspan(pos, l));
        if (!v) return std::unexpected(v.error());
        out.push_back(std::move(*v));
        pos += l;
    }
    if (pos != blob.size()) return std::unexpected(kErrLength);
    return out;
}

}  // namespace lux::zkvm::wire
