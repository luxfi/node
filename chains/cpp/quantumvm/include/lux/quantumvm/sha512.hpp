// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// sha512.hpp — SHA-512 (FIPS 180-4), which the quantum stamp is built from.
//
// The implementation is the first-party one already in luxcpp/crypto
// (ed25519/cpp/sha512_minimal.hpp, a faithful FIPS 180-4 port); this is a
// facade over it in this chain's own types, not a second copy of the hash.

#pragma once

#include "sha512_minimal.hpp"

#include <array>
#include <cstdint>
#include <span>

namespace lux::quantumvm {

using Hash512 = std::array<std::uint8_t, 64>;

inline Hash512 sha512(std::span<const std::uint8_t> data) {
    Hash512 out{};
    lux::crypto::ed25519::detail::sha512(data.data(), data.size(), out.data());
    return out;
}

}  // namespace lux::quantumvm
