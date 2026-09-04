// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/id.hpp"

#include "ripemd160.hpp"
#include "sha256.hpp"

#include <cstdio>

namespace lux::xvm {

Id sha256(ByteView data) {
    Id out{};
    cevm::crypto::sha256(reinterpret_cast<std::byte*>(out.data()),
                         reinterpret_cast<const std::byte*>(data.data()), data.size());
    return out;
}

ShortId pubkey_to_address(ByteView compressed_pubkey) {
    const Id h = sha256(compressed_pubkey);
    ShortId out{};
    cevm::crypto::ripemd160(reinterpret_cast<std::byte*>(out.data()),
                            reinterpret_cast<const std::byte*>(h.data()), h.size());
    return out;
}

Id id_prefix(const Id& id, std::uint64_t prefix) {
    std::array<std::uint8_t, 8 + 32> buf{};
    for (int i = 0; i < 8; ++i) buf[std::size_t(i)] = std::uint8_t(prefix >> (8 * (7 - i)));
    for (std::size_t i = 0; i < id.size(); ++i) buf[8 + i] = id[i];
    return sha256(ByteView(buf.data(), buf.size()));
}

std::string hex(ByteView b) {
    std::string s;
    s.reserve(b.size() * 2 + 2);
    s += "0x";
    char t[3];
    for (auto c : b) {
        std::snprintf(t, sizeof(t), "%02x", c);
        s += t;
    }
    return s;
}

}  // namespace lux::xvm
