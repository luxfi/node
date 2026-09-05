// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// ids_test.cpp — the names this chain derives, against the Go reference's own.
//
// The hash underneath is the node's and is checked against FIPS 180-4 where it
// lives (core/test/id_test.cpp). What is checked HERE is the derivation the
// P-chain builds on it: a UTXO's name is ids.ID.Prefix over the transaction
// that produced it, and two implementations that name the same UTXO
// differently have forked. The vectors were produced by the Go reference and
// are asserted verbatim.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/ids.hpp"

#include <string>

using namespace lux::platformvm;

TEST(UtxoIdDerivation) {
    Id zero{};
    REQUIRE(hex(prefix_id(zero, 0)) == pvmgold::prefix_zero_0);
    REQUIRE(hex(prefix_id(zero, 1)) == pvmgold::prefix_zero_1);

    Id seq{};
    for (std::size_t i = 0; i < kIdLen; ++i) seq[i] = static_cast<std::uint8_t>(i + 1);
    REQUIRE(hex(prefix_id(seq, 7)) == pvmgold::prefix_seq_7);
}
