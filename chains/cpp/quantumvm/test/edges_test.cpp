// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// edges_test.cpp — ported from Go chains/quantumvm/edges_test.go.

#include "fixtures.hpp"

#include <set>
#include <vector>

using namespace qvmtest;

namespace {

BlockPtr build_on(QuantumVM& vm) {
    (void)vm.pool().add(stamped_tx(next_nonce(), "op"));
    auto blk = vm.build_block();
    return blk ? *blk : nullptr;
}

BlockPtr advance(QuantumVM& vm, int n) {
    BlockPtr last;
    for (int i = 0; i < n; ++i) {
        last = build_on(vm);
        if (!last || !last->verify() || !last->accept()) return nullptr;
    }
    return last;
}

}  // namespace

// TestTheVMSignsEveryBlockItBuilds.
//
// The node signs with its own key and tracks the block, so a peer's signature
// has somewhere to land. That is the whole bridge: with the node unregistered it
// signed nothing, tracked nothing, and every peer signature was answered
// "pending block not found" — a finality engine that could never finalize, one
// warning line per block.
TEST(TheVMSignsEveryBlockItBuilds) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const auto bridge = vm->bridge();

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_MSG(bridge->tracking(blk->id()),
                "the node built a block and recorded no signature for it");
    const auto sigs = bridge->signatures_of(blk->id());
    REQUIRE_EQ(std::size_t{1}, sigs.size());
    REQUIRE_MSG(bridge->verify_signature(blk->bytes(), &sigs[0]),
                "the recorded signature does not check out over the block it signs");

    // The block itself is unaffected: signing is a consensus-layer statement
    // about a block, not a field on it.
    REQUIRE_OK(blk->verify());
    REQUIRE_OK(blk->accept());

    // The finality bridge's stamp is that same signature.
    auto stamp = vm->stamp_block(blk->id(), 42, blk->bytes());
    REQUIRE_OK(stamp);
    REQUIRE(std::holds_alternative<quasar::QuasarSig>(*stamp));
    REQUIRE_OK(vm->verify_stamp(blk->bytes(), *stamp));
}

// Signing is best-effort, so a VM with no bridge produces blocks rather than
// stopping.
TEST(BuildBlockWithNoBridgeStillBuilds) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    vm->drop_bridge();

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_OK(blk->verify());
    REQUIRE_OK(blk->accept());
}

// A block's transactions are settled by its commit; one that is no longer in the
// pool is not a reason to fail a block that is already durable.
TEST(AcceptSurvivesATransactionAlreadyGone) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    auto tip = vm->block_at(tip_of(*vm));
    REQUIRE_OK(tip);

    BlockPtr blk = block_on(*vm, **tip, stamped_tx(7, "never in this pool"));
    REQUIRE_OK(blk->accept());
    REQUIRE_EQ(blk->id(), tip_of(*vm));
}

// The builder reads its parent to set height and time; a tip pointer naming
// bytes it cannot read is a refusal, never a block built on a guess.
TEST(BuildBlockRefusesWhenTheTipIsUnreadable) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    const Id tip = tip_of(*vm);
    REQUIRE_OK(vm->state().put(view(tip), view(bytes_of("no longer a block"))));

    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));
    REQUIRE_FAILS(vm->build_block());
}

// Genesis is written at boot; a VM that could not write it would come up naming
// no block, which is the one state bootstrap cannot recover from.
TEST(InitializeRefusesAStoreItCannotCommitTo) {
    bool always = true;
    store::Memory base;
    RefusingStore db(&base, &always);

    QuantumVM vm{config::default_config()};
    Init init;
    init.db = &db;
    init.node_id = random_node_id();
    init.chain_id = kTestChain;
    init.network_id = kTestNetwork;
    REQUIRE_ERR(vm.initialize(init), Err::StoreUnwritable);
    REQUIRE_MSG(empty(tip_of(vm)), "a VM that could not write genesis still names a block");
}

// Shutdown runs on the way out of a node that may already be tearing down, so it
// has to finish rather than fail and leave the process hanging.
TEST(ShutdownCompletesWhenTheStoreIsAlreadyGone) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_OK(db.close());
    REQUIRE_OK(vm->shutdown());
}

// TestAReadThatFailedIsNotAChainThatIsEmpty.
//
// The tip was read as "empty on any error, and on any value that is not 32
// bytes" — and seeding reads empty as "a fresh chain". So ONE transient read
// failure at boot committed genesis over a live tip, and initialize returned
// success: a node holding five blocks came back holding one and told its peers
// so. A short read did exactly the same.
TEST(AReadThatFailedIsNotAChainThatIsEmpty) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr tip = advance(*vm, 5);
    REQUIRE(tip != nullptr);
    REQUIRE_EQ(std::uint64_t{5}, height_of(*vm));

    // A store that cannot answer.
    UnreadableStore blind_db(&db);
    QuantumVM blind{quiet_config()};
    Init blind_init;
    blind_init.db = &blind_db;
    blind_init.node_id = random_node_id();
    blind_init.chain_id = kTestChain;
    blind_init.network_id = kTestNetwork;
    REQUIRE_ERR(blind.initialize(blind_init), Err::TipUnreadable);

    // A short read is the same fact: an answer that is not an id.
    store::Memory short_db;
    Booted short_vm = boot_vm_on(quiet_config(), &short_db);
    REQUIRE_OK(short_vm.status);
    REQUIRE_OK(short_vm->state().put(view(kLastAcceptedKey), view(bytes_of("not an id"))));
    REQUIRE_ERR(short_vm->tip(), Err::TipUnreadable);
    REQUIRE_ERR(short_vm->seed_genesis(), Err::TipUnreadable);

    // The chain underneath is untouched: nothing rewound it.
    Booted reopened = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(reopened.status);
    REQUIRE_EQ(tip->id(), tip_of(*reopened.vm));
    REQUIRE_EQ(std::uint64_t{5}, height_of(*reopened.vm));
}

// The tip height decides what the next block's height is, so bytes that are not
// a height must refuse rather than read as the start of the chain.
TEST(AMalformedHeightIsNotAHeightOfZero) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE(advance(*vm, 1) != nullptr);
    REQUIRE_EQ(std::uint64_t{1}, height_of(*vm));

    REQUIRE_OK(vm->state().put(view(kTipHeightKey), view(Bytes{1, 2, 3})));
    REQUIRE_ERR(vm->tip_height(), Err::TipUnreadable);

    REQUIRE_OK(vm->pool().add(stamped_tx(9, "op")));
    auto blk = vm->build_block();
    REQUIRE_OK(blk);
    REQUIRE_ERR((*blk)->accept(), Err::TipUnreadable);
}

// The same fact one level up: a node that cannot read its own tip does not
// start, because the alternative is starting on a chain it has just destroyed.
TEST(InitializeRefusesToBootOnAStoreItCannotRead) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE(advance(*vm, 3) != nullptr);

    UnreadableStore blind(&db);
    QuantumVM fresh{quiet_config()};
    Init init;
    init.db = &blind;
    init.node_id = random_node_id();
    init.chain_id = kTestChain;
    init.network_id = kTestNetwork;
    REQUIRE_ERR(fresh.initialize(init), Err::TipUnreadable);
}

// TestStoreKeysCannotCollide. Block ids, the height index and the two tip
// pointers all live in one namespace, so a height whose key happened to equal
// the tip pointer's would overwrite the chain's head.
TEST(StoreKeysCannotCollide) {
    std::set<Bytes> seen{kLastAcceptedKey, kTipHeightKey};
    REQUIRE_EQ(std::size_t{2}, seen.size());

    for (std::uint64_t h : {std::uint64_t{0}, std::uint64_t{1}, std::uint64_t{255},
                            std::uint64_t{1} << 32, ~std::uint64_t{0}}) {
        const Bytes k = height_key(h);
        REQUIRE_MSG(seen.insert(k).second, "height " + std::to_string(h) + " collides");
        REQUIRE_MSG(k.size() != kIdLen, "a height key is indistinguishable from a block id");
    }
}

// The fast path, where the batch verifier answers once for all of them.
TEST(ProcessBatchTakesTheWholeBatchWhenEverySignatureIsGood) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);  // stamps ON
    REQUIRE_OK(vm.status);

    std::vector<TxPtr> txs;
    for (std::uint64_t i = 0; i < 6; ++i) txs.push_back(signed_tx(*vm, i, "honest"));

    Verdict v = vm->process_transactions(txs);
    REQUIRE_EQ(txs.size(), v.valid.size());
    REQUIRE(v.rejected.empty());
}

// A block that came back from the store is the same block that went in, and a
// chain that restarts twice still names the same tip.
TEST(TheChainSurvivesMoreThanOneRestart) {
    store::Memory db;
    {
        Booted vm = boot_vm_on(quiet_config(), &db);
        REQUIRE_OK(vm.status);
        REQUIRE(advance(*vm, 2) != nullptr);
    }

    Id tip{};
    for (int i = 0; i < 3; ++i) {
        Booted vm = boot_vm_on(quiet_config(), &db);
        REQUIRE_OK(vm.status);
        if (i == 0) tip = tip_of(*vm);
        REQUIRE_EQ(tip, tip_of(*vm));
        REQUIRE_EQ(std::uint64_t{2}, height_of(*vm));
        auto stored = vm->block(tip);
        REQUIRE_OK(stored);
        REQUIRE_EQ(tip, (*stored)->id());
    }
}
