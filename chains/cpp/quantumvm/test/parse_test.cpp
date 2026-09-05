// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// parse_test.cpp — ported from Go chains/quantumvm/parse_roundtrip_test.go.
//
// The two doors into one format: a responder answers a catch-up request with the
// bytes it stored, and the requester admits the reply through the parser. A
// check on only one of them means the fleet serves what it will not accept.

#include "fixtures.hpp"

using namespace qvmtest;

// TestParseAcceptsWhatWeSerialize is the property this chain could not move
// without, and the one nothing asserted.
//
// The parser is the sole entry point for catch-up, gossip and Put. If it refuses
// the bytes this VM itself produces, then every node serves a block every node
// refuses, and the chain accepts nothing for as long as it runs — which is what
// happened: zero accepted certs on every validator, on every network, forever,
// while the package's own tests stayed green because none of them ever sent a
// block through the wire and back.
TEST(ParseAcceptsWhatWeSerialize) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);
    REQUIRE_OK(vm.status);

    wire::BlockFields f;
    f.timestamp = 1000;
    f.height = 7;
    f.parent_id = random_id();
    f.chain_id = vm->chain_id();
    f.network_id = vm->network_id();
    f.transactions = {stamped_tx(1, "op")};
    Block blk(vm.vm.get(), std::move(f));

    auto got = vm->parse_block(blk.bytes());
    REQUIRE_MSG(got.has_value(),
                "the parser refused bytes this VM serialized: a block every node serves and "
                "every node refuses is a chain that cannot move");
    REQUIRE_EQ(blk.id(), (*got)->id());
    REQUIRE_EQ(std::uint64_t{7}, (*got)->height());
}

// TestServeAndReceiveAgree pins the asymmetry itself. Whatever either door does
// to a block, both must reach the same verdict.
TEST(ServeAndReceiveAgree) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);
    REQUIRE_OK(vm.status);

    wire::BlockFields f;
    f.timestamp = 2000;
    f.height = 1;
    f.parent_id = tip_of(*vm);
    f.chain_id = vm->chain_id();
    f.network_id = vm->network_id();
    f.transactions = {stamped_tx(1, "op")};
    auto blk = std::make_shared<Block>(vm.vm.get(), std::move(f));
    REQUIRE_OK(blk->accept());

    auto served = vm->block(blk->id());            // responder side
    auto received = vm->parse_block(blk->bytes());  // requester side
    REQUIRE_MSG(served.has_value() == received.has_value(),
                "the two paths disagree on identical bytes: every node would serve this block "
                "and every node would refuse it");
    REQUIRE_OK(served);
    REQUIRE_MSG((*served)->id() == (*received)->id(), "one block read back under two ids");
}

// A block that came off the wire is a block that can be served again unchanged:
// parse ∘ serialize is the identity on the bytes, which is what makes a block id
// a fact about the block rather than about the node holding it.
TEST(ServingWhatWasReceivedChangesNothing) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));
    auto built = vm->build_block();
    REQUIRE_OK(built);
    const ByteView first = (*built)->bytes();
    const Bytes wire(first.begin(), first.end());

    auto received = vm->parse_block(view(wire));
    REQUIRE_OK(received);
    const ByteView again = (*received)->bytes();
    REQUIRE_MSG(again.size() == wire.size() && std::equal(again.begin(), again.end(), wire.begin()),
                "a block changed shape by passing through the parser");
    REQUIRE_EQ((*built)->id(), (*received)->id());
}
