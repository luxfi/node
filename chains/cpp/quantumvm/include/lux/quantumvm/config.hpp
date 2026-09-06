// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// config.hpp — every foundational parameter of the Q-chain.
//
// Rendered from Go chains/quantumvm/config/config.go.
//
// Every field here governs behaviour. Q-Chain charges no user fee at all
// (LP-0130 §6 — finality-cert inclusion is a validator obligation, not
// purchasable blockspace), so the config declares no fee schedule: a number
// nothing reads is a price the chain does not actually charge, and reporting
// one over RPC tells operators otherwise.

#pragma once

#include "lux/quantumvm/clock.hpp"
#include "lux/quantumvm/error.hpp"

#include <chrono>
#include <cstdint>

namespace lux::quantumvm::config {

using lux::quantumvm::Duration;

// AlgorithmDefault is the parameter set an unset config settles on: ML-DSA-65,
// NIST level 3. default_config() names the same constant, so a config the
// operator filled in and one left blank land on the same parameter set — two
// spellings of the default meant a chain signed under ML-DSA-44 or ML-DSA-65
// depending on which door it came through.
inline constexpr std::uint32_t kAlgorithmDefault = 2;

// CommitteeMin is the smallest committee that survives one Byzantine validator.
// BFT tolerates f faults out of n ≥ 3f+1, so f ≥ 1 needs n ≥ 4; below that the
// quorum ⌊2n/3⌋+1 is the whole committee and one absent validator stops the
// chain while one dishonest validator decides it.
inline constexpr int kCommitteeMin = 4;

// Quorum is how many validators must agree, for a committee of n. It is the
// classical ⌊2n/3⌋+1, which for n ≥ kCommitteeMin always lands strictly between
// 2 and n — the range the consensus core accepts.
inline constexpr int quorum(int n) { return n * 2 / 3 + 1; }

struct Config {
    // Maximum parallel transactions to process.
    int max_parallel_txs = 0;

    // Quantum signature algorithm version: 1=ML-DSA-44, 2=ML-DSA-65,
    // 3=ML-DSA-87. The parameter set fixes every key and signature width.
    // Zero means unset and settles on ML-DSA-65; anything else is refused.
    std::uint32_t quantum_algorithm_version = 0;

    // Enable quantum stamp validation.
    bool quantum_stamp_enabled = false;

    // How long a quantum stamp stays valid after it is made.
    Duration quantum_stamp_window{0};

    // Parallel processing batch size.
    int parallel_batch_size = 0;

    // Enable Corona key support.
    bool corona_enabled = false;

    // Minimum batch size before GPU acceleration kicks in.
    int gpu_batch_threshold = 0;

    // Committee is how many validators the finality committee holds. The
    // threshold is derived from it, so it is the one number that decides how
    // many faults the chain survives. Zero means unset and settles on
    // kCommitteeMin; anything below kCommitteeMin is refused.
    int committee = 0;

    friend bool operator==(const Config&, const Config&) = default;

    // Validate replaces any non-positive sizing with its default and refuses an
    // algorithm that does not exist.
    //
    // Each sizing divides or bounds a loop, so zero is not a weaker setting — it
    // is a VM that batches nothing, caches nothing and builds empty blocks. The
    // algorithm is different: an unrecognised number used to fall through to
    // ML-DSA-65 inside the signer, so an operator who asked for something else
    // got a chain signing under a parameter set nobody chose, and never heard
    // about it.
    Status validate();
};

Config default_config();

}  // namespace lux::quantumvm::config
