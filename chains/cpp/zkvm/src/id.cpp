// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/id.hpp"

#include "sha256.hpp"  // luxcpp/crypto, reused in place — not vendored

#include <cstddef>

namespace lux::zkvm {

Id sha256(ByteView data) {
    Id out{};
    cevm::crypto::sha256(reinterpret_cast<std::byte*>(out.data()),
                         reinterpret_cast<const std::byte*>(data.data()), data.size());
    return out;
}

void Hasher::num(std::uint64_t v) {
    for (int i = 7; i >= 0; --i) buf_.push_back(std::uint8_t(v >> (8 * i)));
}

void Hasher::num32(std::uint32_t v) {
    for (int i = 3; i >= 0; --i) buf_.push_back(std::uint8_t(v >> (8 * i)));
}

Id Hasher::sum() const { return sha256(view(buf_)); }

std::string hex(ByteView b) {
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(b.size() * 2);
    for (std::uint8_t x : b) {
        out.push_back(d[x >> 4]);
        out.push_back(d[x & 0x0F]);
    }
    return out;
}

Bytes unhex(std::string_view s) {
    if (s.size() % 2 != 0) return {};
    Bytes out;
    out.reserve(s.size() / 2);
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    for (std::size_t i = 0; i < s.size(); i += 2) {
        const int hi = nib(s[i]), lo = nib(s[i + 1]);
        if (hi < 0 || lo < 0) return {};
        out.push_back(std::uint8_t((hi << 4) | lo));
    }
    return out;
}

}  // namespace lux::zkvm
