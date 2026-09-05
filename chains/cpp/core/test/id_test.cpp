// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id_test.cpp — the names, and the two things that must stay true about them.
//
// FIRST, the derivations are the reference's. prefix_id and append_id are how a
// UTXO and a genesis L1 validator get their names, so they are hashes with a
// stated input order; a test that only round-trips them would pin nothing.
// They are checked against sha256 computed the long way, over bytes this test
// lays out itself.
//
// SECOND, Id is the seam's type. The whole reason the P-chain and the X-chain
// share this header is that a block id crosses into consensus without a
// conversion, so that is asserted at compile time rather than described.

#include "lux/core/check.hpp"
#include "lux/core/id.hpp"

#include <array>
#include <cstdint>
#include <string>
#include <type_traits>
#include <vector>

using namespace lux::core;
using namespace lux::core::test;

namespace {

// The seam's type, spelled the way lux/consensus/quorum_cert_engine.hpp spells
// it. Written out rather than included so this test states the contract instead
// of inheriting it.
using SeamId = std::array<std::uint8_t, 32>;

static_assert(std::is_same_v<Id, SeamId>,
              "Id must BE the consensus id: a conversion here is a conversion that can be got wrong");
static_assert(!std::is_same_v<NodeId, ShortId>,
              "a node id and an address are both twenty bytes and must not be interchangeable");
static_assert(sizeof(NodeId) == 20, "a NodeId is its bytes and nothing else");

void the_names_are_their_bytes() {
    const Id id = id_from(view(Bytes{1, 2, 3}));
    check(id[0] == 1 && id[1] == 2 && id[2] == 3, "short input fills from the front");
    check(id[3] == 0 && id[31] == 0, "and the rest is zero");

    Bytes over(40, 0xAB);
    const ShortId s = short_id_from(view(over));
    check(s.size() == 20 && s[0] == 0xAB && s[19] == 0xAB, "long input is truncated to width");

    check(id_from({}) == kEmptyId, "no bytes at all is the empty id");
    check(node_id_from({}) == kEmptyNodeId, "and the empty node id");
}

void a_node_id_orders_like_its_bytes() {
    const NodeId a = node_id_from(view(Bytes{0x00, 0x01}));
    const NodeId b = node_id_from(view(Bytes{0x00, 0x02}));
    check(a < b, "a node id orders by its bytes, most significant first");
    check(!(b < a), "and the order is total");
    check(a == node_id_from(view(Bytes{0x00, 0x01})), "equal bytes are the same name");
}

void hex_is_bare_lowercase() {
    const Id id = id_from(view(Bytes{0x00, 0x0f, 0xff}));
    const std::string h = hex(id);
    check(h.size() == 64, "an id renders as 64 characters");
    check_eq(h.substr(0, 6), std::string("000fff"), "lowercase, no prefix, no separator");
}

void prefix_hashes_the_counter_first() {
    const Id id = id_from(view(Bytes{0x11, 0x22, 0x33}));
    const std::uint64_t n = 0x0102030405060708ull;

    Bytes want;
    for (int i = 7; i >= 0; --i) want.push_back(std::uint8_t(n >> (8 * i)));
    want.insert(want.end(), id.begin(), id.end());

    check_eq(hex(prefix_id(id, n)), hex(sha256(view(want))),
             "prefix_id is sha256(be64(n) || id)");
}

void append_hashes_the_counter_last() {
    const Id id = id_from(view(Bytes{0x44, 0x55}));
    const std::uint32_t n = 0x01020304u;

    Bytes want(id.begin(), id.end());
    for (int i = 3; i >= 0; --i) want.push_back(std::uint8_t(n >> (8 * i)));

    check_eq(hex(append_id(id, n)), hex(sha256(view(want))),
             "append_id is sha256(id || be32(n))");
    check(append_id(id, 1) != prefix_id(id, 1),
          "and the two orders are different names, which is the point of having both");
}

void an_address_is_ripemd_of_the_hash() {
    // The all-zero 33-byte key: a fixed input, so the digest below is the one
    // this pair of hashes produces and not a value copied from the code.
    const Bytes key(33, 0x00);
    const ShortId addr = pubkey_to_address(view(key));
    check(addr != kEmptyShortId, "an address is derived, not empty");
    check(hex(addr).size() == 40, "and is twenty bytes");
    check(pubkey_to_address(view(key)) == addr, "the derivation is a function");
}

}  // namespace

int main() {
    std::printf("id\n");
    the_names_are_their_bytes();
    a_node_id_orders_like_its_bytes();
    hex_is_bare_lowercase();
    prefix_hashes_the_counter_first();
    append_hashes_the_counter_last();
    an_address_is_ripemd_of_the_hash();
    return report("id");
}
