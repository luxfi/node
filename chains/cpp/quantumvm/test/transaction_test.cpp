// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// transaction_test.cpp — ported from Go chains/quantumvm/transaction_test.go.

#include "fixtures.hpp"

#include <thread>
#include <vector>

using namespace qvmtest;

// TestPoolTellsConsensusThereIsWork: a transaction the pool accepted is worth
// nothing until consensus is told about it. Consensus builds only when the wait
// returns, so accepting work and reporting it are one step.
TEST(PoolTellsConsensusThereIsWork) {
    TransactionPool pool(8);
    REQUIRE_MSG(!pool.wait_for_work(std::chrono::milliseconds(50)),
                "an empty pool reported work to build");

    std::atomic<bool> woke{false};
    std::thread waiter([&] { woke = pool.wait_for_work(std::chrono::seconds(5)); });

    REQUIRE_OK(pool.add(stamped_tx(1, "op")));
    waiter.join();
    REQUIRE_MSG(woke.load(), "a transaction was accepted and consensus was never told; "
                             "this chain cannot produce a block");
}

// TestWorkLeftOverStillWakesABuilder.
//
// The latch holds one signal, so transactions arriving together wake one build.
// A build that takes fewer than the pool holds must say there is still work, or
// the remainder waits for an unrelated arrival to wake it — and on a chain that
// has gone quiet, that never comes.
TEST(WorkLeftOverStillWakesABuilder) {
    config::Config cfg = quiet_config();
    cfg.parallel_batch_size = 1;  // one transaction per block
    store::Memory db;
    Booted vm = boot_vm_on(cfg, &db);
    REQUIRE_OK(vm.status);

    for (int i = 0; i < 3; ++i) REQUIRE_OK(vm->pool().add(stamped_tx(static_cast<std::uint64_t>(i), "op")));
    REQUIRE(vm->wait_for_event(std::chrono::seconds(5)));

    auto blk = vm->build_block();
    REQUIRE_OK(blk);
    REQUIRE_EQ(std::size_t{1}, (*blk)->transactions().size());
    REQUIRE_OK((*blk)->accept());

    // Two are still pending. The builder has to hear about them.
    REQUIRE_MSG(vm->wait_for_event(std::chrono::seconds(5)),
                "two transactions sat in the pool with nothing left to wake a builder: "
                "the chain stops here");
}

// The id is the content hash, so a resend is the same slot. Admitting it twice
// would let one transaction fill the pool by itself.
TEST(PoolRefusesTheSameTransactionTwice) {
    TransactionPool pool(8);
    const auto tx = stamped_tx(1, "op");

    REQUIRE_OK(pool.add(tx));
    REQUIRE_ERR(pool.add(tx), Err::DuplicateTx);
    // A separately constructed copy is the same content, so it is the same slot.
    REQUIRE_ERR(pool.add(stamped_tx(1, "op")), Err::DuplicateTx);
    REQUIRE_EQ(std::size_t{1}, pool.count());
}

// Nothing unverifiable ever occupies a slot in the first place.
TEST(PoolRefusesAnUnsignedTransaction) {
    TransactionPool pool(8);
    REQUIRE_ERR(pool.add(std::make_shared<BaseTransaction>(0, 1, Bytes{})), Err::MissingStamp);
    REQUIRE_EQ(std::size_t{0}, pool.count());
}

// The pool is memory a peer can fill, so it has a ceiling and the ceiling holds.
TEST(PoolIsBounded) {
    TransactionPool pool(3);
    for (int i = 0; i < 3; ++i) REQUIRE_OK(pool.add(stamped_tx(static_cast<std::uint64_t>(i), "op")));
    REQUIRE_ERR(pool.add(stamped_tx(99, "op")), Err::PoolFull);
    REQUIRE_EQ(std::size_t{3}, pool.count());

    // A slot freed is a slot usable again.
    REQUIRE_OK(pool.remove(stamped_tx(0, "op")->id()));
    REQUIRE_OK(pool.add(stamped_tx(99, "op")));
}

// The map decides admission and the queue decides selection; a transaction left
// in either one is either an unusable slot or a transaction that gets built into
// a second block.
TEST(PoolRemoveClearsBothViews) {
    TransactionPool pool(8);
    const auto keep = stamped_tx(1, "keep");
    const auto drop = stamped_tx(2, "drop");
    REQUIRE_OK(pool.add(keep));
    REQUIRE_OK(pool.add(drop));

    REQUIRE_OK(pool.remove(drop->id()));
    REQUIRE_MSG(pool.count() == 1, "the map still holds a removed transaction");

    const auto selected = pool.pending(0);
    REQUIRE_MSG(selected.size() == 1, "the queue still offers a removed transaction");
    REQUIRE_EQ(keep->id(), selected[0]->id());

    REQUIRE_ERR(pool.remove(drop->id()), Err::TxNotInPool);
}

// After shutdown the pool is not a place work can be left: accepting it would be
// a promise to build a block that never comes.
TEST(ClosedPoolAcceptsNothing) {
    TransactionPool pool(8);
    REQUIRE_OK(pool.add(stamped_tx(1, "op")));

    pool.close();
    pool.close();  // the VM may shut down more than once

    REQUIRE_ERR(pool.add(stamped_tx(2, "op")), Err::PoolClosed);
    REQUIRE_EQ(std::size_t{0}, pool.count());
    REQUIRE(pool.pending(10).empty());
    REQUIRE_FAILS(pool.remove(stamped_tx(1, "op")->id()));
    pool.signal_if_work();  // must not fault on an emptied pool
}

// Admission, selection and removal all run from different threads in the live
// VM — the engine builds while gossip arrives.
TEST(PoolHoldsUpUnderConcurrentUse) {
    TransactionPool pool(256);

    std::vector<std::thread> threads;
    for (int i = 0; i < 32; ++i) {
        threads.emplace_back([&pool, i] { (void)pool.add(stamped_tx(static_cast<std::uint64_t>(i), "op")); });
        threads.emplace_back([&pool] { (void)pool.pending(8); });
        threads.emplace_back(
            [&pool, i] { (void)pool.remove(stamped_tx(static_cast<std::uint64_t>(i), "op")->id()); });
    }
    for (auto& t : threads) t.join();

    // However the races fell out, the two views agree.
    REQUIRE_EQ(pool.count(), pool.pending(0).size());
}

// TestProcessBatchSeparatesGoodFromBad. Both halves are load-bearing: the good
// go in the block, the bad leave the pool. A batch that reported only the good
// left the bad behind to hold their slots for good.
TEST(ProcessBatchSeparatesGoodFromBad) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);  // stamps ON
    REQUIRE_OK(vm.status);

    const auto good = signed_tx(*vm, 1, "honest");
    const auto forged = signed_tx(*vm, 2, "forged");
    forged->mutable_signature()->signature[0] ^= 0xFF;
    const auto unsigned_tx = std::make_shared<BaseTransaction>(kChainTime, 3, bytes_of("bare"));

    Verdict v = vm->process_transactions({good, forged, unsigned_tx});
    REQUIRE_EQ(std::size_t{1}, v.valid.size());
    REQUIRE_EQ(good->id(), v.valid[0]->id());
    REQUIRE_MSG(v.rejected.size() == 2,
                "a forged and an unsigned transaction must both be reported back");

    // Nothing at all is neither valid nor an error.
    Verdict empty_v = vm->process_transactions({});
    REQUIRE(empty_v.valid.empty());
    REQUIRE(empty_v.rejected.empty());
}

// With stamps off the crypto is skipped, but a transaction with no signature is
// still refused — the pool's own admission rule, applied again at build time.
TEST(ProcessBatchWithStampsOffSkipsCrypto) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    Verdict v = vm->process_transactions(
        {stamped_tx(1, "a"), std::make_shared<BaseTransaction>(kChainTime, 2, Bytes{})});
    REQUIRE_EQ(std::size_t{1}, v.valid.size());
    REQUIRE_EQ(std::size_t{1}, v.rejected.size());
}

// TestProcessBatchAppliesNothing.
//
// Selecting transactions for a block is not applying them. It ran them here, so
// a block the network never accepted had already applied its effects, a rebuild
// applied them twice, and a node that received the block rather than building it
// applied them never. Selection asks whether a signature checks out; nothing
// more.
TEST(ProcessBatchAppliesNothing) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    int runs = 0;
    auto refusing = std::make_shared<CountingTx>(stamped_tx(2, "would refuse"), &runs, true);

    Verdict v = vm->process_transactions({stamped_tx(1, "runs"), refusing});
    REQUIRE_MSG(v.valid.size() == 2, "a transaction was dropped for a reason selection cannot know");
    REQUIRE(v.rejected.empty());
    REQUIRE_MSG(runs == 0, "selecting a transaction applied it");

    // The same transaction is refused where it is applied — in accept.
    REQUIRE_OK(vm->pool().add(refusing));
    auto blk = vm->build_block();
    REQUIRE_OK(blk);
    REQUIRE_ERR((*blk)->accept(), Err::Execute);
    REQUIRE_EQ(1, runs);
}

// The batch size divides the selection, and every batch's survivors reach the
// block.
TEST(BuildBlockSpansSeveralBatches) {
    config::Config cfg = quiet_config();
    cfg.parallel_batch_size = 2;
    store::Memory db;
    Booted vm = boot_vm_on(cfg, &db);
    REQUIRE_OK(vm.status);

    for (int i = 0; i < 2; ++i) REQUIRE_OK(vm->pool().add(stamped_tx(static_cast<std::uint64_t>(i), "op")));
    Verdict v = vm->process_transactions(vm->pool().pending(0));
    REQUIRE_EQ(std::size_t{2}, v.valid.size());
    REQUIRE(v.rejected.empty());
}

// The id keys the pool, so two different transactions sharing one would take a
// single slot between them.
TEST(TransactionIdentityIsItsContent) {
    const auto base = stamped_tx(1, "payload");
    REQUIRE(!empty(base->id()));
    REQUIRE_EQ(base->id(), base->id());

    REQUIRE_MSG(base->id() != stamped_tx(2, "payload")->id(), "the nonce moves the id");
    REQUIRE_MSG(base->id() != stamped_tx(1, "other")->id(), "the payload moves the id");

    auto later = std::make_shared<BaseTransaction>(kChainTime + 1, 1, bytes_of("payload"));
    REQUIRE_MSG(base->id() != later->id(), "the timestamp moves the id");

    REQUIRE_EQ(kChainTime, base->timestamp());
    REQUIRE(base->signature() != nullptr);
    REQUIRE_OK(base->execute());
    REQUIRE_OK(base->verify());
}
