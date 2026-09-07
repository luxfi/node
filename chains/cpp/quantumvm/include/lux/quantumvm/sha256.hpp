// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// sha256.hpp — SHA-256 (FIPS 180-4), the one hash the Q-chain identifies with.
//
// A transaction's id is sha256 of its signed bytes; a UTXO's id is sha256 of a
// big-endian output index followed by the producing tx id. Both are consensus.
//
// The body is not written here. It comes from the one place a Lux chain asks
// for a primitive — a first-party, from-the-standard implementation shared with
// the X-chain and with Go, and the place where the choice between computing a
// primitive and handing it to an installed kernel library is made. See
// gpu/README.md. The P-chain used to carry its own copy of SHA-256, which made
// three implementations of one hash in one C++ tree and three places for the
// chains to drift apart.

#pragma once

#include "lux/gpu/gpu.hpp"

#include <array>
#include <cstdint>
#include <span>

namespace lux::quantumvm {

using Hash256 = std::array<std::uint8_t, 32>;

inline Hash256 sha256(std::span<const std::uint8_t> data) { return lux::gpu::sha256(data); }

}  // namespace lux::quantumvm
