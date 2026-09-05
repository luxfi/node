// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// config_test.cpp — ported from Go chains/quantumvm/config/config_test.go.

#include "fixtures.hpp"

using namespace qvmtest;
using lux::quantumvm::config::Config;

// TestValidateRefusesAnAlgorithmThatDoesNotExist.
//
// An unrecognised number used to fall through to ML-DSA-65 inside the signer,
// so an operator who configured 7 got a chain signing under a parameter set
// nobody chose — and no message saying so. The number is the one config field
// where a default is worse than a refusal.
TEST(ValidateRefusesAnAlgorithmThatDoesNotExist) {
    for (std::uint32_t version : {4u, 7u, 99u, 1u << 20}) {
        Config c = config::default_config();
        c.quantum_algorithm_version = version;
        REQUIRE_MSG(!c.validate(),
                    "algorithm " + std::to_string(version) +
                        " was accepted; the chain would sign under a parameter set nobody chose");
    }

    for (std::uint32_t version : {1u, 2u, 3u}) {
        Config c = config::default_config();
        c.quantum_algorithm_version = version;
        REQUIRE_OK(c.validate());
        REQUIRE_EQ(version, c.quantum_algorithm_version);
    }
}

// TestValidateSettlesAnEmptyConfig. Zero is not a weaker setting: a zero batch
// size batches nothing and a zero pool accepts nothing. A VM handed the empty
// config has to come out of validate usable.
TEST(ValidateSettlesAnEmptyConfig) {
    Config c{};
    REQUIRE_OK(c.validate());

    REQUIRE_EQ(config::kAlgorithmDefault, c.quantum_algorithm_version);
    REQUIRE_MSG(c.max_parallel_txs > 0, "MaxParallelTxs settled on a value that does no work");
    REQUIRE_MSG(c.parallel_batch_size > 0, "ParallelBatchSize settled on a value that does no work");
    REQUIRE_MSG(c.gpu_batch_threshold > 0, "GPUBatchThreshold settled on a value that does no work");
    REQUIRE_MSG(c.quantum_stamp_window > Duration::zero(),
                "the stamp window settled on zero: every stamp would be born expired");
    REQUIRE_EQ(config::kCommitteeMin, c.committee);
}

// TestValidateReplacesNegativeSizes the same way it replaces zero — the sign is
// not the point, usability is.
TEST(ValidateReplacesNegativeSizes) {
    Config c = config::default_config();
    c.max_parallel_txs = -1;
    c.parallel_batch_size = -10;
    c.gpu_batch_threshold = -8;
    c.quantum_stamp_window = Duration(-1'000'000'000);

    REQUIRE_OK(c.validate());
    REQUIRE(c.max_parallel_txs > 0);
    REQUIRE(c.parallel_batch_size > 0);
    REQUIRE(c.gpu_batch_threshold > 0);
    REQUIRE(c.quantum_stamp_window > Duration::zero());
}

// TestDefaultConfigIsAlreadyValid: the shipped defaults must not need repair, or
// the defaults are not the defaults.
TEST(DefaultConfigIsAlreadyValid) {
    const Config before = config::default_config();
    Config after = before;
    REQUIRE_OK(after.validate());
    REQUIRE_MSG(after == before, "validate changed the defaults");
}

// The threshold is derived from the committee, and ⌊2n/3⌋+1 must land strictly
// between 2 and n for every committee the chain will accept.
TEST(QuorumIsTwoThirdsPlusOne) {
    struct Case {
        int committee;
        int threshold;
    };
    for (const Case& c : {Case{4, 3}, Case{5, 4}, Case{7, 5}, Case{10, 7}, Case{100, 67}}) {
        REQUIRE_EQ(c.threshold, config::quorum(c.committee));
        REQUIRE_MSG(config::quorum(c.committee) < c.committee, "a quorum of everyone is not a quorum");
        REQUIRE(config::quorum(c.committee) >= 2);
    }

    // Below CommitteeMin the quorum IS the whole committee, which is why the
    // chain refuses a committee that small rather than deriving from it.
    for (int n : {1, 2, 3}) REQUIRE_EQ(n, config::quorum(n));
}
