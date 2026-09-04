// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// sha256_test.cpp — the hash a transaction is named by, against the standard's
// own vectors (FIPS 180-4 / NIST CAVP), plus the UTXO-id derivation the P-chain
// builds on it.
//
// The id vectors were produced by the Go reference (ids.ID.Prefix over
// hash.ComputeHash256Array) and are asserted here verbatim: two implementations
// that name the same UTXO differently have forked.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/sha256.hpp"

#include <cstring>
#include <string>
#include <vector>

using namespace lux::platformvm;

namespace {
std::string hex(const Hash256& h) {
    static const char* d = "0123456789abcdef";
    std::string s;
    for (auto b : h) {
        s.push_back(d[b >> 4]);
        s.push_back(d[b & 0xf]);
    }
    return s;
}
std::span<const std::uint8_t> str(const std::string& s) {
    return {reinterpret_cast<const std::uint8_t*>(s.data()), s.size()};
}
}  // namespace

TEST(Sha256StandardVectors) {
    REQUIRE(hex(sha256(str(""))) == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    REQUIRE(hex(sha256(str("abc"))) == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    REQUIRE(hex(sha256(str("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"))) ==
            "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
    REQUIRE(hex(sha256(str("abcdefghbcdefghicdefghijdefghijkefghijklfghijklmghijklmnhijklmno"
                           "ijklmnopjklmnopqklmnopqrlmnopqrsmnopqrstnopqrstu"))) ==
            "cf5b16a778af8380036ce59e7b0492370b249b11e8f07a51afac45037afee9d1");
}

// The block boundary at 55/56/63/64 bytes is where a length-padding
// implementation goes wrong; walk it.
TEST(Sha256BlockBoundaries) {
    const std::string a(1000000, 'a');
    REQUIRE(hex(sha256(str(a))) == "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0");

    // Streaming in odd chunks must equal hashing at once.
    for (std::size_t chunk : {1u, 7u, 55u, 56u, 63u, 64u, 65u, 127u}) {
        Sha256 h;
        for (std::size_t i = 0; i < a.size(); i += chunk) {
            const std::size_t n = std::min(chunk, a.size() - i);
            h.update({reinterpret_cast<const std::uint8_t*>(a.data()) + i, n});
        }
        REQUIRE(hex(h.finish()) == "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0");
    }
}

// The UTXO id derivation, against ids.ID.Prefix run in the Go reference.
TEST(UtxoIdDerivation) {
    Id zero{};
    REQUIRE(prefix_id(zero, 0).hex() == pvmgold::prefix_zero_0);
    REQUIRE(prefix_id(zero, 1).hex() == pvmgold::prefix_zero_1);

    Id seq{};
    for (std::size_t i = 0; i < kIdLen; ++i) seq.b[i] = static_cast<std::uint8_t>(i + 1);
    REQUIRE(prefix_id(seq, 7).hex() == pvmgold::prefix_seq_7);
}
