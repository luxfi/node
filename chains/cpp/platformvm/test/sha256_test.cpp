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
// implementation goes wrong; walk it. NIST's million-'a' vector is the long
// case, and every length either side of a 64-byte block is the short one.
TEST(Sha256BlockBoundaries) {
    const std::string a(1000000, 'a');
    REQUIRE(hex(sha256(str(a))) == "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0");

    // Every length either side of a 64-byte block and of the 56-byte padding
    // boundary, against digests produced OUTSIDE this tree (GNU coreutils
    // sha256sum over the same inputs). A hash checked only against itself is
    // not checked.
    struct Case {
        std::size_t n;
        const char* want;
    };
    static const Case cases[] = {
        {1, "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"},
        {55, "9f4390f8d30c2dd92ec9f095b65e2b9ae9b0a925a5258e241c9f1e910f734318"},
        {56, "b35439a4ac6f0948b6d6f9e3c6af0f5f590ce20f1bde7090ef7970686ec6738a"},
        {63, "7d3e74a05d7db15bce4ad9ec0658ea98e3f06eeecf16b4c6fff2da457ddc2f34"},
        {64, "ffe054fe7ae0cb6dc65c3af9b61d5209f439851db43d0ba5997337df154668eb"},
        {65, "635361c48bb9eab14198e76ea8ab7f1a41685d6ad62aa9146d301d4f17eb0ae0"},
        {119, "31eba51c313a5c08226adf18d4a359cfdfd8d2e816b13f4af952f7ea6584dcfb"},
        {120, "2f3d335432c70b580af0e8e1b3674a7c020d683aa5f73aaaedfdc55af904c21c"},
        {127, "c57e9278af78fa3cab38667bef4ce29d783787a2f731d4e12200270f0c32320a"},
        {128, "6836cf13bac400e9105071cd6af47084dfacad4e5e302c94bfed24e013afb73e"},
    };
    for (const auto& c : cases) {
        const std::string s(c.n, 'a');
        REQUIRE(hex(sha256(str(s))) == std::string(c.want));
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
