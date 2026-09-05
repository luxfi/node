// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/config.hpp"

#include <string>

namespace lux::quantumvm::config {

using namespace std::chrono_literals;

Config default_config() {
    Config c;
    c.max_parallel_txs = 100;
    c.quantum_algorithm_version = kAlgorithmDefault;
    c.quantum_stamp_enabled = true;
    c.quantum_stamp_window = std::chrono::duration_cast<Duration>(30s);
    c.parallel_batch_size = 10;
    c.corona_enabled = true;
    c.gpu_batch_threshold = 8;
    c.committee = kCommitteeMin;
    return c;
}

Status Config::validate() {
    switch (quantum_algorithm_version) {
        case 0:
            quantum_algorithm_version = kAlgorithmDefault;
            break;
        case 1:
        case 2:
        case 3:
            break;
        default:
            return fail(Err::UnsupportedAlgorithm,
                        "quantum algorithm " + std::to_string(quantum_algorithm_version) +
                            " does not exist (1=ML-DSA-44, 2=ML-DSA-65, 3=ML-DSA-87)");
    }
    if (max_parallel_txs <= 0) max_parallel_txs = 100;
    if (parallel_batch_size <= 0) parallel_batch_size = 10;
    if (gpu_batch_threshold <= 0) gpu_batch_threshold = 8;
    if (quantum_stamp_window <= Duration::zero())
        quantum_stamp_window = std::chrono::duration_cast<Duration>(30s);
    if (committee == 0) committee = kCommitteeMin;
    return ok();
}

}  // namespace lux::quantumvm::config
