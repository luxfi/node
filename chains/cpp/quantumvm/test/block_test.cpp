// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block_test.cpp — ported from Go chains/quantumvm/block_test.go.

#include "fixtures.hpp"

#include <thread>
#include <vector>

using namespace qvmtest;

namespace {

// Builds the next block on the tip with one transaction in it.
BlockPtr build_on(QuantumVM& vm) {
    (void)vm.pool().add(stamped_tx(next_nonce(), "op"));
    auto blk = vm.build_block();
    return blk ? *blk : nullptr;
}

// Builds, verifies and accepts n blocks, returning the last.
BlockPtr advance(QuantumVM& vm, int n) {
    BlockPtr last;
    for (int i = 0; i < n; ++i) {
        last = build_on(vm);
        if (!last) return nullptr;
        if (!last->verify() || !last->accept()) return nullptr;
    }
    return last;
}

// advance for a VM with quantum stamping ON, where a transaction has to carry a
// signature that actually checks out.
BlockPtr advance_signed(QuantumVM& vm, int n) {
    BlockPtr last;
    for (int i = 0; i < n; ++i) {
        (void)vm.pool().add(signed_tx(vm, next_nonce(), "op"));
        auto built = vm.build_block();
        if (!built) return nullptr;
        last = *built;
        if (!last->verify() || !last->accept()) return nullptr;
    }
    return last;
}

}  // namespace

// TestAcceptedBlockSurvivesRestart is the property the chain could not run
// without, and the one nothing asserted.
//
// Accept wrote through the staging layer and never committed, so every accepted
// block lived in one process's memory. A node that had accepted a thousand
// blocks came back from a restart naming genesis — while its peers held it to
// the tip it had already told them about.
TEST(AcceptedBlockSurvivesRestart) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr tip = advance(*vm, 3);
    REQUIRE(tip != nullptr);
    REQUIRE_EQ(std::uint64_t{3}, height_of(*vm));

    Booted restarted = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(restarted.status);
    REQUIRE_MSG(restarted->last_accepted() == tip->id(),
                "the tip did not survive the restart: the accepted blocks were never committed");
    REQUIRE_EQ(std::uint64_t{3}, height_of(*restarted.vm));

    auto stored = restarted->block(tip->id());
    REQUIRE_OK(stored);
    REQUIRE_EQ(tip->id(), (*stored)->id());
}

// TestAcceptLeavesNothingBehindWhenAWriteFails: a block is stored, indexed by
// height and made the tip. Those are one fact about the chain, so a failure part
// way through must leave none of them.
TEST(AcceptLeavesNothingBehindWhenAWriteFails) {
    bool refuse = false;
    store::Memory base;
    RefusingStore db(&base, &refuse);
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    const Id before = tip_of(*vm);
    const std::uint64_t before_height = height_of(*vm);

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_OK(blk->verify());

    refuse = true;
    REQUIRE_ERR(blk->accept(), Err::StoreUnwritable);

    REQUIRE_MSG(before == tip_of(*vm), "the tip moved to a block whose writes failed");
    REQUIRE_MSG(before_height == height_of(*vm), "the height moved to a block whose writes failed");
    REQUIRE_MSG(!vm->block(blk->id()), "a partially written block is readable");
    REQUIRE_MSG(!vm->block_id_at_height(blk->height()), "a partially written block is indexed");
}

// TestAFailedCommitStagesNothingForTheNextOne.
//
// Staged writes that are not discarded are not discarded LATER either — they are
// flushed, wholesale, by the next commit that succeeds. So a block whose accept
// returned an error to the engine still reached the store, riding into it on an
// unrelated block minutes afterwards.
TEST(AFailedCommitStagesNothingForTheNextOne) {
    bool refuse = false;
    store::Memory base;
    RefusingStore db(&base, &refuse);
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr tip = advance(*vm, 1);
    REQUIRE(tip != nullptr);

    refuse = true;
    BlockPtr orphan = build_on(*vm);
    REQUIRE(orphan != nullptr);
    REQUIRE_ERR(orphan->accept(), Err::StoreUnwritable);

    // The store comes back, and an unrelated block commits.
    refuse = false;
    vm->clock().set(from_seconds(kChainTime + 3600));
    BlockPtr next = build_on(*vm);
    REQUIRE(next != nullptr);
    REQUIRE_MSG(orphan->id() != next->id(), "precondition: two distinct blocks");
    REQUIRE_OK(next->accept());

    auto held = vm->state().has(view(orphan->id()));
    REQUIRE_OK(held);
    REQUIRE_MSG(!*held,
                "the refused block was flushed into the store by the next successful commit");

    REQUIRE_EQ(next->id(), tip_of(*vm));
    auto at = vm->block_id_at_height(tip->height() + 1);
    REQUIRE_OK(at);
    REQUIRE_MSG(*at == next->id(), "the height index names a block that never committed");
}

// The transactions of a block that did not persist must still be there to go
// into the next one.
TEST(AcceptKeepsTheMempoolWhenTheWriteFails) {
    bool refuse = false;
    store::Memory base;
    RefusingStore db(&base, &refuse);
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_EQ(std::size_t{1}, vm->pool().count());

    refuse = true;
    REQUIRE_FAILS(blk->accept());
    REQUIRE_MSG(vm->pool().count() == 1,
                "the block never persisted but its transactions were dropped from the pool");
}

// The other half: once the block IS durable, its transactions are settled and
// must not be built into another block.
TEST(AcceptEvictsWhatItCommitted) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_EQ(std::size_t{1}, vm->pool().count());
    REQUIRE_OK(blk->accept());
    REQUIRE_MSG(vm->pool().count() == 0, "an accepted block's transactions stayed pending");
}

// TestAcceptTakesOnlyTheBlockThatFollowsTheTip.
//
// Accept wrote the tip pointer unconditionally, and verify only ever compared a
// block against its PARENT — never against the chain's head. So any block that
// verified could be accepted, at any height, at any time: a height-1 block
// accepted onto a height-5 chain rewound it, an accepted block accepted again
// did the same, and two siblings both committed with the second silently
// replacing the first.
TEST(AcceptTakesOnlyTheBlockThatFollowsTheTip) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    std::vector<BlockPtr> chain;
    for (int i = 0; i < 5; ++i) {
        BlockPtr b = advance(*vm, 1);
        REQUIRE(b != nullptr);
        chain.push_back(b);
    }
    const BlockPtr tip = chain.back();
    REQUIRE_EQ(std::uint64_t{5}, height_of(*vm));

    // An old block, re-offered.
    REQUIRE_ERR(chain[0]->accept(), Err::NotTheTip);
    REQUIRE_EQ(tip->id(), tip_of(*vm));
    REQUIRE_EQ(std::uint64_t{5}, height_of(*vm));
    for (std::size_t i = 0; i < chain.size(); ++i) {
        auto at = vm->block_id_at_height(static_cast<std::uint64_t>(i) + 1);
        REQUIRE_OK(at);
        REQUIRE_MSG(*at == chain[i]->id(),
                    "height " + std::to_string(i + 1) + " was re-indexed to an abandoned branch");
    }

    // The tip itself, re-offered.
    REQUIRE_ERR(tip->accept(), Err::NotTheTip);

    // Two siblings. Both verify — that is what makes consensus a choice — and
    // exactly one may be accepted.
    BlockPtr first = build_on(*vm);
    REQUIRE(first != nullptr);
    REQUIRE_OK(first->verify());
    vm->clock().set(from_seconds(kChainTime + 60));
    BlockPtr second = build_on(*vm);
    REQUIRE(second != nullptr);
    REQUIRE_OK(second->verify());
    REQUIRE_MSG(first->id() != second->id(), "precondition: two distinct siblings");
    REQUIRE_MSG(first->parent_id() == second->parent_id(), "precondition: they are siblings");

    REQUIRE_OK(first->accept());
    REQUIRE_ERR(second->accept(), Err::NotTheTip);
    REQUIRE_EQ(first->id(), tip_of(*vm));
}

// TestTransactionsRunWhenTheBlockIsAccepted, and only then.
TEST(TransactionsRunWhenTheBlockIsAccepted) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    int runs = 0;
    auto tx = std::make_shared<CountingTx>(stamped_tx(1, "apply me"), &runs, false);
    REQUIRE_OK(vm->pool().add(tx));
    auto built = vm->build_block();
    REQUIRE_OK(built);
    BlockPtr blk = *built;
    REQUIRE_MSG(runs == 0, "building a block applied its transactions");

    // Building again over the same pool contents must not apply them again.
    auto rebuilt = vm->build_block();
    REQUIRE_OK(rebuilt);
    REQUIRE_MSG(runs == 0, "a rebuild applied the same transaction a second time");

    REQUIRE_OK(blk->verify());
    REQUIRE_OK(blk->accept());
    REQUIRE_MSG(runs == 1, "an accepted block applied nothing");

    // The sibling can no longer be accepted, so nothing runs twice.
    REQUIRE_FAILS((*rebuilt)->accept());
    REQUIRE_EQ(1, runs);
}

// A block that loses never ran, so there is nothing for reject to undo — which
// is why reject undoing nothing is correct rather than a gap.
TEST(RejectAppliesNothing) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    int runs = 0;
    REQUIRE_OK(vm->pool().add(std::make_shared<CountingTx>(stamped_tx(1, "never"), &runs, false)));
    auto blk = vm->build_block();
    REQUIRE_OK(blk);
    REQUIRE_OK((*blk)->reject());
    REQUIRE_MSG(runs == 0, "a rejected block had already applied its transactions");
}

// A node that cannot apply an agreed block stops rather than committing a chain
// its state no longer matches.
TEST(AcceptCommitsNothingWhenATransactionCannotBeApplied) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const Id before = tip_of(*vm);

    int runs = 0;
    REQUIRE_OK(vm->pool().add(
        std::make_shared<CountingTx>(stamped_tx(1, "cannot apply"), &runs, /*refuse=*/true)));
    auto blk = vm->build_block();
    REQUIRE_OK(blk);
    REQUIRE_ERR((*blk)->accept(), Err::Execute);
    REQUIRE_MSG(before == tip_of(*vm), "the tip advanced past a block that could not be applied");
}

// Checking only that the parent exists ABOVE height one meant a height-one block
// could name any parent at all, including one no node has ever seen.
TEST(VerifyRefusesAnOrphan) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    wire::BlockFields f;
    f.timestamp = kChainTime;
    f.height = 1;
    f.parent_id = random_id();  // a block this node has never held
    f.chain_id = vm->chain_id();
    f.network_id = vm->network_id();
    f.transactions = {stamped_tx(1, "op")};
    Block orphan(vm.vm.get(), std::move(f));

    REQUIRE_ERR(orphan.verify(), Err::InvalidParentID);
}

// Height is the parent's, advanced by one. Without that a proposer names genesis
// as the parent of a height-500 block and the 499 heights in between simply do
// not exist.
TEST(VerifyRefusesAHeightThatSkipsItsParent) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    auto genesis = vm->block_at(tip_of(*vm));
    REQUIRE_OK(genesis);

    for (std::uint64_t height : {std::uint64_t{2}, std::uint64_t{7}, std::uint64_t{500}}) {
        BlockPtr blk = block_on(*vm, **genesis, stamped_tx(height, "op"));
        wire::BlockFields f = blk->fields();
        f.height = height;
        blk->restate(std::move(f));
        REQUIRE_ERR(blk->verify(), Err::InvalidBlockHeight);
    }
}

// Height zero is written by the VM at start, identically on every node. A
// proposed one would be a second genesis.
TEST(VerifyRefusesGenesisAsAProposal) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    wire::BlockFields f;
    f.timestamp = kChainTime;
    f.height = 0;
    f.parent_id = kEmptyId;
    f.chain_id = vm->chain_id();
    f.network_id = vm->network_id();
    Block blk(vm.vm.get(), std::move(f));
    REQUIRE_ERR(blk.verify(), Err::InvalidBlockHeight);
}

// A proposal carries work, and refusing an empty one is also what stops the
// signature check being satisfied by removing its subject.
TEST(VerifyRefusesAnEmptyBlock) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    auto genesis = vm->block_at(tip_of(*vm));
    REQUIRE_OK(genesis);

    REQUIRE_ERR(block_on(*vm, **genesis)->verify(), Err::EmptyBlock);
}

// Chain time is what orders the chain, and a proposer that rewinds it decides
// what the block after it may be stamped.
TEST(VerifyRefusesTimeRunningBackwards) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance(*vm, 1);
    REQUIRE(parent != nullptr);

    BlockPtr blk = block_on(*vm, *parent, stamped_tx(2, "op"));
    wire::BlockFields f = blk->fields();
    f.timestamp = parent->timestamp() - 1;
    blk->restate(std::move(f));
    REQUIRE_ERR(blk->verify(), Err::TimeBeforeParent);

    // The parent's own timestamp is fine — time may stand still, only not
    // reverse.
    REQUIRE_OK(block_on(*vm, *parent, stamped_tx(3, "op"))->verify());
}

// Uncapped, one proposer stamping a block far in the future decides chain time
// for everyone behind it.
TEST(VerifyRefusesTimeJumpingAhead) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance(*vm, 1);
    REQUIRE(parent != nullptr);

    BlockPtr blk = block_on(*vm, *parent, stamped_tx(2, "op"));
    wire::BlockFields f = blk->fields();
    f.timestamp = vm->clock().seconds() + kMaxFutureSkewSeconds + 1;
    blk->restate(std::move(f));
    REQUIRE_ERR(blk->verify(), Err::TimeTooFarAhead);

    // Just inside the allowance is a clock that is merely fast, not a rewrite of
    // chain time, and it verifies.
    BlockPtr okay = block_on(*vm, *parent, stamped_tx(3, "op"));
    wire::BlockFields g = okay->fields();
    g.timestamp = vm->clock().seconds() + kMaxFutureSkewSeconds;
    okay->restate(std::move(g));
    REQUIRE_OK(okay->verify());
}

// The signatures over the transactions are checked, and a block carrying one
// that does not verify is refused whole.
TEST(VerifyRefusesABlockWhoseStampsDoNotCheck) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);  // stamps ON
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance_signed(*vm, 1);
    REQUIRE(parent != nullptr);

    const auto good = signed_tx(*vm, 1, "honest");
    BlockPtr blk = block_on(*vm, *parent, good);
    REQUIRE_OK(blk->verify());

    // One flipped bit in the signature is the whole difference.
    good->mutable_signature()->signature[0] ^= 0xFF;
    REQUIRE_ERR(blk->verify(), Err::BlockVerificationFailed);
}

// TestVerifyChecksTheSignaturesOfABlockItDidNotBuild.
//
// This is the one that mattered. The parser did not reconstruct the transaction
// set, so a received block held ZERO transactions, and the signature check was
// gated on there being some — so it ran only on blocks this node built itself
// and never once on the receive path. Identical bytes got opposite verdicts.
TEST(VerifyChecksTheSignaturesOfABlockItDidNotBuild) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);  // stamps ON
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance_signed(*vm, 1);
    REQUIRE(parent != nullptr);

    BlockPtr built = block_on(*vm, *parent, signed_tx(*vm, 1, "honest"));
    REQUIRE_OK(built->verify());

    // The same bytes, arriving over the network.
    auto received = vm->parse_block(built->bytes());
    REQUIRE_OK(received);
    REQUIRE_MSG((*received)->id() == built->id(), "one block, two ids");
    REQUIRE_MSG((*received)->transactions().size() == 1, "the parser dropped the transaction set");
    REQUIRE_OK((*received)->verify());

    // And a block whose signature does not check must be refused on that same
    // path — the verdict cannot depend on who assembled the object.
    auto forged_tx = signed_tx(*vm, 2, "forged");
    forged_tx->mutable_signature()->signature[0] ^= 0xFF;
    BlockPtr forged = block_on(*vm, *parent, forged_tx);

    auto parsed = vm->parse_block(forged->bytes());
    REQUIRE_MSG(parsed.has_value(), "precondition: the bytes are a well-formed block");
    REQUIRE_ERR((*parsed)->verify(), Err::BlockVerificationFailed);
    REQUIRE_ERR(forged->verify(), Err::BlockVerificationFailed);
}

// The transactions ride in the block's bytes; a byte changed in there either
// stops being a block or stops verifying, and never rides through as the same
// block.
TEST(TheTransactionBlobIsNotMutable) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);  // stamps ON
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance_signed(*vm, 1);
    REQUIRE(parent != nullptr);

    BlockPtr blk = block_on(*vm, *parent, signed_tx(*vm, 1, "honest"));
    const ByteView wire = blk->bytes();

    Bytes tampered(wire.begin(), wire.end());
    tampered.back() ^= 0xFF;

    auto parsed = vm->parse_block(view(tampered));
    REQUIRE_MSG(parsed.has_value(), "precondition: the tampered bytes still decode");
    REQUIRE_MSG((*parsed)->id() != blk->id(), "a changed byte did not move the id");
    REQUIRE_ERR((*parsed)->verify(), Err::BlockVerificationFailed);
}

// TestVerifyRefusesABlockFromAnotherChain.
//
// The wire carried nothing naming a chain, so a block was a block anywhere: one
// built on chain A was accepted verbatim by chain B, and every Q-Chain shared a
// genesis id to build that on.
TEST(VerifyRefusesABlockFromAnotherChain) {
    store::Memory mine_db, theirs_db;
    Booted mine = boot_vm_on(quiet_config(), &mine_db);
    REQUIRE_OK(mine.status);
    Booted theirs = boot_vm_as(quiet_config(), &theirs_db, kTestNetwork, kOtherChain);
    REQUIRE_OK(theirs.status);

    auto tip = mine->block_at(tip_of(*mine));
    REQUIRE_OK(tip);

    // A block naming this node's tip as its parent, stamped for another chain.
    BlockPtr foreign = block_on(*mine, **tip, stamped_tx(1, "op"));
    wire::BlockFields f = foreign->fields();
    f.chain_id = theirs->chain_id();
    foreign->restate(std::move(f));

    REQUIRE_ERR(foreign->verify(), Err::ForeignChain);
    REQUIRE_ERR(foreign->accept(), Err::ForeignChain);
    REQUIRE_EQ((*tip)->id(), tip_of(*mine));

    // The same for a block from another network on this chain id.
    BlockPtr elsewhere = block_on(*mine, **tip, stamped_tx(2, "op"));
    wire::BlockFields g = elsewhere->fields();
    g.network_id = mine->network_id() + 1;
    elsewhere->restate(std::move(g));
    REQUIRE_ERR(elsewhere->verify(), Err::ForeignChain);
    REQUIRE_ERR(elsewhere->accept(), Err::ForeignChain);
}

// TestVerifyDoesNotDeadlockAgainstABuilder.
//
// Verify held the VM's read lock and then called the exported block reader,
// which takes it again. A pending writer queues ahead of any later reader, so a
// build arriving between the two acquisitions blocked the second one — and the
// builder waited on the reader that was waiting on the builder. Nothing times
// out; the chain stops.
TEST(VerifyDoesNotDeadlockAgainstABuilder) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance(*vm, 1);
    REQUIRE(parent != nullptr);

    BlockPtr blk = block_on(*vm, *parent, stamped_tx(9999, "op"));

    std::atomic<bool> done{false};
    std::thread runner([&] {
        std::vector<std::thread> threads;
        for (int i = 0; i < 50; ++i) {
            threads.emplace_back([&] { (void)blk->verify(); });
            threads.emplace_back([&, i] {
                (void)vm->pool().add(stamped_tx(static_cast<std::uint64_t>(i), "x"));
                (void)vm->build_block();
            });
        }
        for (auto& t : threads) t.join();
        done = true;
    });

    // Fixed, it finishes in milliseconds; with the recursive acquisition back it
    // does not finish at all, so this waits with a deadline rather than forever.
    for (int i = 0; i < 200 && !done.load(); ++i)
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    REQUIRE_MSG(done.load(),
                "verify and build deadlocked: the chain stops here and no timeout releases it");
    runner.join();
}

// Building copies from the queue rather than draining it, so a rejected block's
// transactions were never taken out.
TEST(RejectLeavesTheMempoolAlone) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_EQ(std::size_t{1}, vm->pool().count());

    REQUIRE_OK(blk->reject());
    REQUIRE_MSG(vm->pool().count() == 1,
                "a rejected block's transactions must still be available to the next one");

    auto next = vm->build_block();
    REQUIRE_OK(next);
    REQUIRE_EQ(std::size_t{1}, (*next)->transactions().size());
}

// 0 while proposed, 1 once committed.
TEST(StatusReportsStorage) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_EQ(std::uint8_t{0}, blk->status());
    REQUIRE_OK(blk->accept());
    REQUIRE_EQ(std::uint8_t{1}, blk->status());
}

// The values the consensus engine reads off a block.
TEST(BlockAccessors) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr blk = advance(*vm, 1);
    REQUIRE(blk != nullptr);

    REQUIRE_EQ(std::uint64_t{1}, blk->height());
    REQUIRE_EQ(blk->id(), of(blk->bytes()));
    REQUIRE_EQ(text(blk->id()), text(blk->id()));

    // The seam reads the same block through its own interface.
    VmBlock seen(vm.vm.get(), blk);
    REQUIRE_EQ(blk->id(), seen.id());
    REQUIRE_EQ(blk->parent_id(), seen.parent());
    REQUIRE_EQ(blk->height(), seen.height());
    REQUIRE_EQ(blk->execution_root(), seen.root());
    REQUIRE(seen.verify());
    REQUIRE(seen.refusal().empty());
}

// The execution root is what a validator signs, so it must move with the block's
// content and be derivable before the block is accepted.
TEST(TheExecutionRootCommitsToWhatTheBlockProduces) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    auto genesis = vm->block_at(tip_of(*vm));
    REQUIRE_OK(genesis);

    BlockPtr a = block_on(*vm, **genesis, stamped_tx(1, "a"));
    BlockPtr b = block_on(*vm, **genesis, stamped_tx(2, "b"));
    REQUIRE_MSG(a->execution_root() != b->execution_root(),
                "two different blocks produced one execution root");
    REQUIRE_MSG(!empty(a->execution_root()), "the execution root is the empty id");

    // It is stable, and it is available before the block is accepted — which is
    // the whole point: it is what a validator votes on.
    const Id before = a->execution_root();
    REQUIRE_OK(a->verify());
    REQUIRE_OK(a->accept());
    REQUIRE_EQ(before, a->execution_root());
}
