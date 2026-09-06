// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis_test.cpp — ported from Go chains/quantumvm/genesis_test.go, with the
// golden id kept VERBATIM.

#include "fixtures.hpp"

using namespace qvmtest;

namespace {

// The smallest VM seed_genesis needs: a store and the chain it serves.
// Constructing the whole VM would test the constructor, not the property under
// test — so this is the same shortcut the Go suite takes (seedVM).
struct Seeded {
    std::unique_ptr<store::Memory> db;
    std::unique_ptr<QuantumVM> vm;
};

Seeded seed_vm_on(std::uint32_t network_id, const Id& chain_id) {
    Seeded s;
    s.db = std::make_unique<store::Memory>();
    s.vm = std::make_unique<QuantumVM>(quiet_config());
    Init init;
    init.db = s.db.get();
    init.node_id = random_node_id();
    init.chain_id = chain_id;
    init.network_id = network_id;
    // initialize() seeds genesis itself, which is the production path; the Go
    // fixture reaches seedGenesis directly because Go's VM struct can be filled
    // in by hand. Both are the same call.
    (void)s.vm->initialize(init);
    return s;
}

Seeded seed_vm() { return seed_vm_on(kTestNetwork, kTestChain); }

// genesisID is the one block every node of testChain must name at height 0.
//
// It is PINNED rather than recomputed because the property is not "two VMs in
// this test agree" — two VMs seeded in the same second agree even with a
// wall-clock timestamp, since the wire carries Unix SECONDS, so that assertion
// passes for a build that forks in production where nodes start minutes apart.
// A constant is the only assertion that fails for every input a node could
// disagree on. If this value must change, the chain is being re-genesised and
// every node has to move together.
//
// The value is Go's, verbatim (genesis_test.go).
constexpr const char* kGenesisID = "zAaMUekpcRPgjqxqT8kSm9P5xRKJuz2k5v3zN2WfnmwdBdojU";

}  // namespace

// TestSeedGenesisNamesATip is the property the chain could not start without.
//
// Bootstrap asks each beacon which block it holds, and an answer naming no
// block is not counted as a responder — so if every node names nothing, the
// response floor is unreachable and the chain waits on itself for as long as it
// runs. The assertion is therefore not "a block exists" but "the VM can NAME
// one".
TEST(SeedGenesisNamesATip) {
    store::Memory db;
    QuantumVM fresh(quiet_config());
    Init init;
    init.db = &db;
    init.node_id = random_node_id();
    init.chain_id = kTestChain;
    init.network_id = kTestNetwork;

    // Before it is seeded, the store names no block.
    store::Version probe(&db);
    QuantumVM* unused = &fresh;
    (void)unused;
    auto raw = probe.get(view(kLastAcceptedKey));
    REQUIRE_MSG(!raw && raw.error().code == Err::NotFound,
                "precondition: a fresh store should name no block");

    REQUIRE_OK(fresh.initialize(init));
    REQUIRE_MSG(!empty(tip_of(fresh)),
                "VM still names no block after seeding — every beacon reply would be dropped "
                "and the chain could never reach its bootstrap quorum");
    REQUIRE_EQ(std::uint64_t{0}, height_of(fresh));
}

// TestSeedGenesisAgreesAcrossNodes is the safety half. Each node seeds alone,
// without talking to any other, so if the block were a function of anything
// local — wall-clock time above all — the nodes would name different genesis
// blocks and the repair would be a fork rather than a fix.
TEST(SeedGenesisAgreesAcrossNodes) {
    Seeded a = seed_vm();
    Seeded b = seed_vm();

    REQUIRE_MSG(tip_of(*a.vm) == tip_of(*b.vm),
                "nodes disagree on genesis: " + text(tip_of(*a.vm)) + " vs " +
                    text(tip_of(*b.vm)) + " — this would fork the chain");
    REQUIRE_MSG(text(tip_of(*a.vm)) == kGenesisID,
                "genesis id = " + text(tip_of(*a.vm)) + ", want " + kGenesisID +
                    " — the block is not a constant, so nodes that start at different times "
                    "will name different genesis blocks");
}

// TestGenesisIsPerChain is the other half of agreement: nodes of ONE chain
// agree, and nodes of two chains do not.
//
// The block carried nothing chain-specific, so genesis was a global constant
// and every Q-Chain in existence shared it. Two chains agreeing on their first
// block is two chains whose blocks are interchangeable from the first height up.
TEST(GenesisIsPerChain) {
    Seeded mine = seed_vm_on(kTestNetwork, kTestChain);
    Seeded theirs = seed_vm_on(kTestNetwork, kOtherChain);
    Seeded elsewhere = seed_vm_on(kOtherNetwork, kTestChain);

    REQUIRE_MSG(tip_of(*mine.vm) != tip_of(*theirs.vm),
                "two chains share a genesis block, so a block built on one is a block on the other");
    REQUIRE_MSG(tip_of(*mine.vm) != tip_of(*elsewhere.vm),
                "two networks share a genesis block, so testnet blocks are mainnet blocks");
}

// TestSeedGenesisLeavesAnExistingChainAlone: initialize runs on every start, so
// seeding must be a no-op once a chain has a tip. Overwriting it would rewind a
// running chain to height 0 on restart.
TEST(SeedGenesisLeavesAnExistingChainAlone) {
    Seeded s = seed_vm();
    const Id first = tip_of(*s.vm);
    REQUIRE(!empty(first));

    auto genesis = s.vm->block_at(first);
    REQUIRE_OK(genesis);

    // Advance the chain, as a running node would.
    auto blk = block_on(*s.vm, **genesis, stamped_tx(1, "op"));
    wire::BlockFields f = blk->fields();
    f.timestamp = 100;
    blk->restate(std::move(f));
    REQUIRE_OK(blk->accept());

    REQUIRE_OK(s.vm->seed_genesis());
    REQUIRE_MSG(tip_of(*s.vm) == blk->id(), "seeding rewound a live chain");
    REQUIRE_EQ(std::uint64_t{1}, height_of(*s.vm));
}

// TestSeedGenesisSurvivesRestart covers the commit. A tip held only in the
// staging layer is gone at the next start, and the node is back to naming no
// block — the same deadlock, one restart later.
TEST(SeedGenesisSurvivesRestart) {
    store::Memory db;
    Booted first = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(first.status);
    const Id want = tip_of(*first.vm);

    // Restart: a new staging layer over the SAME underlying store.
    Booted restarted = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(restarted.status);
    REQUIRE_MSG(tip_of(*restarted.vm) == want,
                "genesis did not survive restart: " + text(tip_of(*restarted.vm)) + " != " +
                    text(want) + " (uncommitted)");
}

// TestGenesisParses: genesis is the block every node must parse, and it is
// signed by nobody — seeding deliberately writes no signature, because a
// per-node signature would give each node a different genesis id. Any gate that
// demands one therefore refuses the one block the whole chain is anchored to.
TEST(GenesisParses) {
    Seeded s = seed_vm();
    const Id gid = tip_of(*s.vm);
    auto raw = s.vm->state().get(view(gid));
    REQUIRE_OK(raw);

    auto parsed = s.vm->parse_block(view(*raw));
    REQUIRE_OK(parsed);
    REQUIRE_EQ(std::uint64_t{0}, (*parsed)->height());
    REQUIRE_EQ(gid, (*parsed)->id());
}
