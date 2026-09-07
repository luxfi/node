// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/core/id.hpp"

#include "ripemd160.hpp"
#include "sha256.hpp"

namespace lux::core {
namespace {

// Big-endian, because that is the order the reference hashes a counter in and
// the order is part of the name.
void put_be(std::uint8_t* p, std::uint64_t v, int width) {
    for (int i = 0; i < width; ++i) p[i] = std::uint8_t(v >> (8 * (width - 1 - i)));
}

}  // namespace

Id sha256(ByteView data) {
    Id out{};
    cevm::crypto::sha256(reinterpret_cast<std::byte*>(out.data()),
                         reinterpret_cast<const std::byte*>(data.data()), data.size());
    return out;
}

Id prefix_id(const Id& id, std::uint64_t prefix) {
    std::array<std::uint8_t, 8 + kIdLen> buf{};
    put_be(buf.data(), prefix, 8);
    std::memcpy(buf.data() + 8, id.data(), kIdLen);
    return sha256(view(buf));
}

Id append_id(const Id& id, std::uint32_t suffix) {
    std::array<std::uint8_t, kIdLen + 4> buf{};
    std::memcpy(buf.data(), id.data(), kIdLen);
    put_be(buf.data() + kIdLen, suffix, 4);
    return sha256(view(buf));
}

ShortId pubkey_to_address(ByteView compressed_pubkey) {
    const Id h = sha256(compressed_pubkey);
    ShortId out{};
    cevm::crypto::ripemd160(reinterpret_cast<std::byte*>(out.data()),
                            reinterpret_cast<const std::byte*>(h.data()), h.size());
    return out;
}

std::string hex(ByteView b) {
    static const char* d = "0123456789abcdef";
    std::string s;
    s.reserve(2 * b.size());
    for (auto x : b) {
        s.push_back(d[x >> 4]);
        s.push_back(d[x & 0xf]);
    }
    return s;
}

}  // namespace lux::core
