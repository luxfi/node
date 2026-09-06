// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm_test.cpp — the chain's lifecycle: booting it, queueing for it, building on
// it, and the bounds that keep a peer from deciding how much of it runs.
//
// Ported from the Go F-Chain's vm_test.go and the block-shaped half of its
// regression suite.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/service.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

std::shared_ptr<Block> block_at(Chain& c, const Id& parent, std::uint64_t height,
                                std::int64_t ts, std::vector<Transaction> txs) {
    return std::make_shared<Block>(c.vm.get(), parent, height, ts, std::move(txs));
}

void a_chain_boots_seated() {
    Committee com = new_committee(3);
    Chain c = new_test_vm({}, com.members, 2);

    auto h = c.vm->health();
    accepted(h, "a seated chain reports its health");
    if (h) {
        check(h->healthy, "and is healthy");
        check_eq(h->details["committee"], "3", "with three members");
        check_eq(h->details["threshold"], "2", "at threshold two");
        check_eq(h->details["epoch"], "0", "in epoch zero");
        check_eq(h->details["version"], std::string(kVersion), "and it names its version");
    }
    check(c.vm->epoch(0) != nullptr, "genesis seats epoch 0");
    check(c.vm->epoch(0) != nullptr && c.vm->epoch(0)->committee.size() == 3, "with its members");
    check_eq(c.vm->current_epoch_number(), 0, "which is the sitting epoch");
    check_eq(c.vm->alias(), "F", "and the chain answers under its own name");
    check_eq(hex_of(vm_id()), hex_of(Id{'f', 'h', 'e', 'v', 'm'}),
             "the vm id is the one the P-Chain stores forever");
}

void a_chain_without_a_committee_is_unhealthy() {
    // With nobody seated, decryptions can be requested but never answered,
    // which is a degraded chain and is reported as such rather than as healthy.
    Chain c = new_test_vm({}, {}, 0);
    auto h = c.vm->health();
    accepted(h, "it still answers");
    check(h && !h->healthy, "but reports itself degraded");
    check(h && h->details["committee"] == "0", "with nobody seated");
}

void genesis_refuses_a_committee_consensus_would_refuse() {
    // Genesis goes through exactly the check the epoch-advance transaction
    // applies, so a chain cannot be born holding a committee consensus would
    // refuse to install later.
    Memory store;
    VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
    Genesis g;
    g.version = 1;
    g.timestamp = kTestGenesisTime;
    CommitteeMember bad;
    bad.node_id[0] = 1;
    constexpr std::string_view junk = "not-an-mldsa-key";
    bad.public_key.assign(junk.begin(), junk.end());
    bad.public_key_nil = false;
    g.committee.push_back(bad);
    g.committee_nil = false;
    g.threshold = 1;
    g.public_key = Bytes{'p', 'k'};
    g.public_key_nil = false;
    refused(vm.initialize(marshal(g)), Err::InvalidCommittee,
            "a chain cannot be born unable to answer");
}

void a_chain_with_no_identity_binds_nothing() {
    // Every signature this chain accepts and every block id it computes is
    // bound to its id, so a chain with none would share both with every other
    // chain that also had none.
    Memory store;
    VM vm(&store, VM::Config{96369, kEmptyId, "F"});
    refused(vm.initialize(""), Err::InvalidBlock, "a chain with no id does not boot");
}

void the_height_index_names_the_accepted_chain() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    auto genesis = c.vm->block_id_at_height(0);
    check(genesis && *genesis == Id(c.vm->last_accepted()),
          "height 0 names the genesis block");

    accept_one(c, register_tx(k, kTestScheme, digest_of("a"), 1), "the first block");
    auto one = c.vm->block_id_at_height(1);
    check(one && *one == Id(c.vm->last_accepted()), "height 1 names the first block");

    accept_one(c, register_tx(k, kTestScheme, digest_of("b"), 2), "the second");
    auto two = c.vm->block_id_at_height(2);
    check(two && *two == Id(c.vm->last_accepted()), "and height 2 the second");
    check(one && two && *one != *two, "which are different blocks");

    check(!c.vm->block_id_at_height(99).has_value(),
          "a height the chain never reached is an error, not a zero id");

    // A damaged entry surfaces as an error rather than a truncated id.
    accepted(c.store->put(view(key(kHeightPrefix, std::uint64_t(5))), view(Bytes{1, 2, 3})),
             "a corrupt index entry is written");
    accepted(c.store->commit(), "and committed");
    refused(c.vm->block_id_at_height(5), Err::InvalidPayload,
            "the index is validated when it is read");
}

void a_discarded_proposal_costs_nothing() {
    // Building SELECTS from the mempool rather than draining it, so a block
    // that is rejected — or that the engine simply discards, which it may do
    // without ever calling reject — cannot take the queue with it.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    Transaction tx = register_tx(k, kTestScheme, digest_of("discarded"), 1);
    accepted(c.vm->submit_tx(tx), "the transaction is queued");

    auto blk = c.vm->build_block();
    accepted(blk, "and a block is built from it");
    check(c.vm->mempool().size() == 1, "building selects, it does not drain");
    check(c.vm->claims().size() == 1, "and the claim is still held");

    if (blk) (*blk)->reject();
    check(c.vm->mempool().size() == 1, "rejection leaves the queue alone");
    check(c.vm->ciphertext(tx.subject) == nullptr, "a rejected block applies nothing");
    auto burned = c.vm->burned();
    check(burned && *burned == 0, "and burns nothing");

    accept_queued(c, "the next block carries it");
    check(c.vm->ciphertext(tx.subject) != nullptr, "the transaction is still good");
    check(c.vm->mempool().empty(), "acceptance is the only thing that clears the queue");
    check(c.vm->claims().empty(), "and the only thing that releases a claim");
}

void a_block_reports_processing_until_it_is_accepted() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("s"), 1)), "queued");
    auto blk = c.vm->build_block();
    accepted(blk, "built");
    if (!blk) return;
    check_eq(std::uint64_t((*blk)->status()), 0, "a built block is processing");
    accepted((*blk)->check(), "it verifies");
    accepted((*blk)->accept_block(), "and is accepted");
    check_eq(std::uint64_t((*blk)->status()), 1, "after which it reports accepted");
}

void an_accepted_block_reparses_from_its_stored_bytes() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    Transaction tx = register_tx(k, kTestScheme, digest_of("rt"), 1);
    auto blk = accept_one(c, tx, "the block");
    if (!blk) return;

    auto parsed = c.vm->parse_block(blk->bytes());
    accepted(parsed, "an accepted block re-parses from its bytes");
    if (parsed) {
        check_eq(hex_of(Id((*parsed)->id())), hex_of(Id(blk->id())), "with the same id");
        check_eq((*parsed)->height(), blk->height(), "the same height");
        check_eq(hex_of(Id((*parsed)->parent())), hex_of(Id(blk->parent())),
                 "and the same parent");
    }
    auto got = c.vm->get_block(Id(blk->id()));
    check(got && Id((*got)->id()) == Id(blk->id()), "and the store hands it back by id");
}

void verify_refuses_a_block_that_is_not_one() {
    // Verify's structural refusals: a block claiming to be genesis, one whose
    // parent nobody has, an empty one, and one carrying more transactions than
    // may ever be verified. None of these needs state, so each is refused
    // before any signature is checked.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);
    accept_one(c, register_tx(k, kTestScheme, digest_of("tip"), 1), "a tip to build on");

    Transaction next = register_tx(k, kTestScheme, digest_of("payload"), 2);
    Id tip = Id(c.vm->last_accepted());
    std::int64_t ts = c.vm->get_block(tip).value()->timestamp() + 1;

    refused(block_at(c, tip, 0, ts, {next})->check(), Err::InvalidBlock,
            "genesis is not a proposed block");
    check(!block_at(c, Id{0xaa}, c.vm->height() + 1, ts, {next})->check().has_value(),
          "a parent nobody has");
    refused(block_at(c, tip, c.vm->height() + 1, ts, {})->check(), Err::InvalidBlock,
            "an empty block buys block space for nothing");

    Transaction tiny;
    tiny.type = kTxRevokePermit;
    tiny.nonce = 1;
    std::string revoke = marshal(RevokePayload{});
    tiny.payload.assign(revoke.begin(), revoke.end());
    std::vector<Transaction> crowd(kMaxBlockTxs + 1, tiny);
    refused(block_at(c, tip, c.vm->height() + 1, ts, crowd)->check(), Err::InvalidBlock,
            "more transactions than may ever be verified");

    accepted(block_at(c, tip, c.vm->height() + 1, ts, {next})->check(),
             "the control: without the mutation it verifies");
}

void a_block_reports_the_header_it_was_built_from() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);
    auto blk = accept_one(c, register_tx(k, kTestScheme, digest_of("hdr"), 1), "a block");
    if (!blk) return;

    auto unstamped = block_at(c, Id(blk->parent()), blk->height(), blk->timestamp(),
                              blk->transactions());
    check_eq(hex_of(Id(unstamped->id())), hex_of(Id(blk->id())),
             "an unstamped block names itself the same way");

    accept_one(c, register_tx(k, kTestScheme, digest_of("hdr2"), 2), "a second block");
    check(Id(blk->id()) != Id(c.vm->last_accepted()), "the first is no longer the tip");
    check_eq(std::uint64_t(blk->status()), 1,
             "but reports accepted, because the store holds it");
}

void the_mempool_is_bounded() {
    // Admission is open to anyone who can pay, so without a bound the queue is
    // whatever an adversary chooses to make it.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    c.vm->pool().assign(kMaxMempool, Transaction{});
    refused(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("overflow"), 1)),
            Err::MempoolFull, "the queue stops at its bound");
}

void the_in_flight_set_tracks_only_what_is_still_in_flight() {
    // A rule enforced only by its callers is one no test can show still works,
    // so the tracker's contract is driven directly.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);
    accept_one(c, register_tx(k, kTestScheme, digest_of("a"), 1), "a block");
    accept_one(c, register_tx(k, kTestScheme, digest_of("b"), 2), "and another");
    check_eq(c.vm->height(), 2, "the chain is at height two");

    // A block at or below the accepted height is decided or orphaned. Tracking
    // one would make it resolvable as a parent, which is how an orphan gets
    // built on.
    for (std::uint64_t h : {std::uint64_t(1), std::uint64_t(2)}) {
        auto decided = block_at(c, Id(c.vm->last_accepted()), h, c.vm->clock().now(), {});
        c.vm->track_verified(decided);
        check(c.vm->pending_blocks().count(Id(decided->id())) == 0,
              "a decided height is not in flight");
    }

    auto live = block_at(c, Id(c.vm->last_accepted()), 3, c.vm->clock().now(), {});
    c.vm->track_verified(live);
    check(c.vm->pending_blocks().count(Id(live->id())) == 1, "one above it is tracked");

    // The engine may drop a block it never accepts and never rejects, so
    // nothing else prunes: anything at or below the accepted height goes.
    accept_one(c, register_tx(k, kTestScheme, digest_of("c"), 3), "a third block");
    auto later = block_at(c, Id(c.vm->last_accepted()), c.vm->height() + 1, c.vm->clock().now(), {});
    c.vm->track_verified(later);
    check(c.vm->pending_blocks().count(Id(live->id())) == 0,
          "the set is pruned, not merely appended to");
    check(c.vm->pending_blocks().count(Id(later->id())) == 1, "and the live one is kept");
}

void building_fails_closed() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);

    {
        // An idle chain proposes nothing.
        Chain c = new_test_vm({}, com.members, 1);
        refused(c.vm->build_block(), Err::NoPendingTxs, "an idle chain proposes nothing");
    }
    {
        // A stopping chain proposes nothing.
        Chain c = new_test_vm(fund_all({k}), com.members, 1);
        accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("x"), 1)), "queued");
        c.vm->shutdown();
        refused(c.vm->build_block(), Err::VMShutdown, "a stopping chain proposes nothing");
    }
    {
        // A chain with no tip fails closed rather than dereferencing one.
        Memory store;
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        vm.pool().push_back(register_tx(k, kTestScheme, digest_of("orphaned"), 1));
        refused(vm.build_block(), Err::NoParentBlock, "and one with no tip does too");
        check(vm.mempool().size() == 1, "leaving the transactions where they were");
    }
}

void chain_time_and_height_are_bounded() {
    // Chain time drives every expiry F enforces, so without a bound a proposer
    // could rewind time to revive an expired permit, jump forward to expire
    // everything at once, or skip heights entirely.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    accept_one(c, register_tx(k, kTestScheme, digest_of("tip"), 1), "a tip");
    auto parent = c.vm->get_block(Id(c.vm->last_accepted())).value();
    c.vm->clock().set(parent->timestamp() + 5);

    Transaction next = register_tx(k, kTestScheme, digest_of("next"), 2);
    accepted(c.vm->submit_tx(next), "a transaction to carry");

    auto at = [&](std::int64_t ts, std::uint64_t height) {
        return block_at(c, Id(c.vm->last_accepted()), height, ts, {next});
    };

    refused(at(parent->timestamp() - 1, parent->height() + 1)->check(), Err::InvalidBlock,
            "time may not run backwards past the parent");
    refused(at(c.vm->clock().now() + kMaxFutureSkew + 1, parent->height() + 1)->check(),
            Err::InvalidBlock, "nor leap beyond the skew allowance");
    refused(at(std::int64_t(1) << 40, parent->height() + 1)->check(), Err::InvalidBlock,
            "nor to a year nobody will see");
    refused(at(c.vm->clock().now(), parent->height() + 2)->check(), Err::InvalidBlock,
            "heights are consecutive");
    refused(at(c.vm->clock().now(), parent->height())->check(), Err::InvalidBlock,
            "in both directions");

    accepted(at(parent->timestamp(), parent->height() + 1)->check(),
             "a block exactly at the parent's timestamp is fine");
    accepted(at(c.vm->clock().now() + kMaxFutureSkew, parent->height() + 1)->check(),
             "and one at the edge of the allowance");
}

void a_proposer_never_builds_a_block_it_would_reject() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    std::int64_t base = kTestGenesisTime + 1000;
    c.vm->clock().set(base);
    accept_one(c, register_tx(k, kTestScheme, digest_of("first"), 1), "a first block");
    auto parent = c.vm->get_block(Id(c.vm->last_accepted())).value();

    // The clock has not moved: the ordinary two-blocks-in-one-tick case.
    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("second"), 2)), "queued");
    auto blk = c.vm->build_block();
    accepted(blk, "the proposer still builds");
    if (blk) {
        check((*blk)->timestamp() > parent->timestamp(),
              "chain time advances even when the proposer's clock does not");
        accepted((*blk)->check(), "and the block it built verifies");
        accepted((*blk)->accept_block(), "and applies");
    }

    // A clock an hour behind its own tip cannot step forward legally, so it
    // proposes nothing rather than a block it would itself reject.
    c.vm->clock().set(base - 3600);
    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("third"), 3)), "queued");
    refused(c.vm->build_block(), Err::ClockBehind,
            "a clock too far behind its own tip declines to propose");
}

void an_orphan_cannot_rewind_the_chain() {
    // Verify required only that a block's height was its parent's plus one, and
    // never that the parent was the TIP. A block at height 2 whose parent is
    // the long-since-accepted block at height 1 satisfies that perfectly, so a
    // chain at height 3 verified it, accepted it, and rewound.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    accept_one(c, register_tx(k, kTestScheme, digest_of("one"), 1), "block one");
    Id fork_point = Id(c.vm->last_accepted());
    std::int64_t fork_time = c.vm->get_block(fork_point).value()->timestamp();
    accept_one(c, register_tx(k, kTestScheme, digest_of("two"), 2), "block two");
    accept_one(c, register_tx(k, kTestScheme, digest_of("three"), 3), "block three");
    Id tip = Id(c.vm->last_accepted());
    std::uint64_t height = c.vm->height();
    check_eq(height, 3, "the chain is at height three");

    // Everything else about this block is impeccable: the height follows its
    // parent, the time follows its parent and is inside the skew allowance, and
    // its transaction carries the payer's next nonce against committed state.
    auto orphan = block_at(c, fork_point, 2, fork_time + 1,
                           {register_tx(k, kTestScheme, digest_of("rewind"), 4)});
    refused(orphan->check(), Err::NotOnTip, "an orphan does not extend the tip");
    refused(orphan->accept_block(), Err::NotOnTip,
            "and acceptance decides it too: verify judged an earlier tip");

    check_eq(c.vm->height(), height, "the chain did not rewind");
    check_eq(hex_of(Id(c.vm->last_accepted())), hex_of(tip), "and its tip did not move");
    auto at2 = c.vm->block_id_at_height(2);
    check(at2 && *at2 != Id(orphan->id()), "the height index still names the accepted chain");
    check(c.vm->ciphertext(orphan->transactions()[0].subject) == nullptr, "and applied nothing");

    // The control: the same transaction in a block that DOES extend the tip is
    // accepted. What was refused is the parent, not the contents.
    accept_one(c, register_tx(k, kTestScheme, digest_of("rewind"), 4), "the same transaction");
    check_eq(c.vm->height(), 4, "which extends the chain");
}

void a_tip_that_moves_after_verify_is_caught_by_accept() {
    // The engine may verify a block, accept a sibling, and only then accept the
    // first — at which point it no longer extends anything.
    TestKey first = new_test_key();
    TestKey second = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({first, second}), com.members, 1);

    auto a = force_block(c, {register_tx(first, kTestScheme, digest_of("a"), 1)});
    auto b = force_block(c, {register_tx(second, kTestScheme, digest_of("b"), 1)});
    check(Id(a->id()) != Id(b->id()), "two distinct siblings on one parent");

    accepted(a->check(), "the first verifies");
    accepted(b->check(), "and so does the second, against the same tip");
    accepted(a->accept_block(), "the first is accepted");
    refused(b->accept_block(), Err::NotOnTip,
            "the sibling no longer extends the tip, whatever verify said earlier");
    check_eq(c.vm->height(), 1, "and the chain moved exactly once");
}

void a_follower_can_verify_against_a_parent_it_only_parsed() {
    // The in-flight block set was written in exactly one place — the builder —
    // so a block arriving from a peer was never findable by id, and its child
    // failed with "verify parent: not found" no matter what it contained.
    TestKey first = new_test_key();
    TestKey second = new_test_key();
    Committee com = new_committee(1);
    Chain proposer = new_test_vm(fund_all({first, second}), com.members, 1);
    Chain follower = new_test_vm(fund_all({first, second}), com.members, 1);

    auto build = [&](const TestKey& k, const char* handle) -> std::shared_ptr<Block> {
        auto id = proposer.vm->submit_tx(register_tx(k, kTestScheme, digest_of(handle), 1));
        accepted(id, "the proposer queues a transaction");
        auto blk = proposer.vm->build_block();
        accepted(blk, "and builds a block");
        return blk ? *blk : nullptr;
    };

    auto b1 = build(first, "one");
    if (b1) accepted(b1->accept_block(), "the proposer accepts the first");
    auto b2 = build(second, "two");  // built on b1, still in flight
    if (!b1 || !b2) return;

    auto p1 = follower.vm->parse_block(b1->bytes());
    accepted(p1, "the follower parses the first");
    if (p1) check((*p1)->verify(), "and verifies it");

    auto p2 = follower.vm->parse_block(b2->bytes());
    accepted(p2, "then parses its child");
    if (p2) {
        accepted((*p2)->check(), "a verified parent must be findable, whoever built it");
    }
}

void a_block_is_bounded_in_bytes_and_in_count() {
    // Both bounds belong at the first byte, and neither implies the other: the
    // byte bound limits what is READ, the count bound limits what is VERIFIED,
    // and a small message can still declare a great many tiny transactions.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    Transaction fat = register_tx(k, kTestScheme, digest_of("fat"), 1);
    fat.payload.assign(kMaxPayload, 'A');
    k.sign(fat);
    std::vector<Transaction> bulk(32, fat);
    auto huge = block_at(c, Id(c.vm->last_accepted()), 1, c.vm->clock().now(), bulk);
    check(huge->bytes().size() > kMaxBlockSize, "the block really is oversized");
    refused(c.vm->parse_block(huge->bytes()), Err::InvalidPayload,
            "an oversize block is refused at the first byte");
    refused(huge->check(), Err::InvalidBlock,
            "and a block this node builds is held to the same bound");

    Transaction tiny;
    tiny.type = kTxRevokePermit;
    tiny.nonce = 1;
    std::string revoke = marshal(RevokePayload{});
    tiny.payload.assign(revoke.begin(), revoke.end());
    std::vector<Transaction> many(kMaxBlockTxs + 1, tiny);
    auto crowd = block_at(c, Id(c.vm->last_accepted()), 1, c.vm->clock().now(), many);
    check(crowd->bytes().size() <= kMaxBlockSize,
          "this one is small; only the count is out of bounds");
    refused(c.vm->parse_block(crowd->bytes()), Err::InvalidPayload,
            "so the count is bounded at the parse too");

    accept_one(c, register_tx(k, kTestScheme, digest_of("ordinary"), 1), "the control");
    auto tip = c.vm->get_block(Id(c.vm->last_accepted()));
    check(tip.has_value(), "one transaction under both bounds");
    if (tip) accepted(c.vm->parse_block((*tip)->bytes()), "round-trips");
}

void selection_stops_at_the_byte_bound() {
    // Stopping only on the count let 1024 ordinary transactions — each carrying
    // an ML-DSA-65 public key and signature, about 5 KB apiece, which no payload
    // bound touches — build a 5 MB block this node's own parser refuses.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), std::uint64_t(1) << 50}}, com.members, 1);

    constexpr std::uint64_t kQueued = 450;
    for (std::uint64_t nonce = 1; nonce <= kQueued; ++nonce) {
        auto id = c.vm->submit_tx(register_tx(k, kTestScheme, digest_of(std::to_string(nonce)),
                                              nonce));
        if (!id) {
            std::printf("  FAIL  queueing %llu: %s\n", (unsigned long long)nonce,
                        id.error().message().c_str());
            ++g_fail;
            return;
        }
    }
    check(c.vm->mempool().size() == kQueued, "the whole run is queued");

    auto blk = c.vm->build_block();
    accepted(blk, "the proposer builds");
    if (!blk) return;
    check((*blk)->bytes().size() <= kMaxBlockSize,
          "it stopped at the size its own parser enforces");
    check((*blk)->transactions().size() < kQueued, "so it could not take them all");
    check((*blk)->transactions().size() < kMaxBlockTxs,
          "and the BYTE bound is what stopped it, well before the count bound");

    accepted((*blk)->check(), "what it built, it verifies");
    accepted(c.vm->parse_block((*blk)->bytes()), "and what it built, it can parse");
    accepted((*blk)->accept_block(), "and accept");
    check(c.vm->mempool().size() == kQueued - (*blk)->transactions().size(),
          "the rest is still queued");
    auto next = accept_queued(c, "and goes out in the next block");
    check(next && !next->transactions().empty(), "which is not empty");
}

void the_wire_verdict_is_the_memory_verdict() {
    // The receive path must decide exactly what the build path decides. A
    // sibling VM's parser discarded the transaction set, so a check gated on
    // that set ran only on blocks the node had built itself: the same block was
    // refused in memory and accepted off the wire.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    auto both_ways = [&](const std::shared_ptr<Block>& blk, const char* what, bool want_ok,
                         Err want) {
        auto in_memory = blk->check();
        auto parsed = c.vm->parse_block(blk->bytes());
        if (!parsed) {
            // A block that does not even parse is refused off the wire, which is
            // itself an agreement when memory refused it too.
            check(!want_ok, std::string(what) + ": refused off the wire at the parse");
            return;
        }
        auto off_wire = (*parsed)->check();
        if (want_ok) {
            accepted(in_memory, std::string(what) + ": accepted in memory");
            accepted(off_wire, std::string(what) + ": and off the wire");
            return;
        }
        refused(in_memory, want, std::string(what) + ": refused in memory");
        refused(off_wire, want, std::string(what) + ": and off the wire, for the same reason");
    };

    auto build = [&](std::vector<Transaction> txs) {
        std::int64_t ts = c.vm->clock().now();
        auto parent = c.vm->get_block(Id(c.vm->last_accepted()));
        if (parent && ts <= (*parent)->timestamp()) ts = (*parent)->timestamp() + 1;
        return block_at(c, Id(c.vm->last_accepted()), c.vm->height() + 1, ts, std::move(txs));
    };

    both_ways(build({register_tx(k, kTestScheme, digest_of("sound"), 1)}), "a sound block", true,
              Err::InvalidBlock);

    Transaction forged = register_tx(k, kTestScheme, digest_of("forged"), 1);
    forged.sig[0] ^= 0xff;
    forged.invalidate_id();
    both_ways(build({forged}), "a forged signature", false, Err::BadSignature);

    Transaction unsigned_tx = register_tx(k, kTestScheme, digest_of("unsigned"), 1);
    unsigned_tx.auth.clear();
    unsigned_tx.sig.clear();
    unsigned_tx.invalidate_id();
    both_ways(build({unsigned_tx}), "an unsigned transaction", false, Err::UnsignedTx);

    both_ways(build({register_tx(k, kTestScheme, digest_of("gap"), 9)}), "a nonce out of order",
              false, Err::BadNonce);

    refused(build({})->check(), Err::InvalidBlock,
            "and an empty block does not become non-empty by being parsed");
}

void a_signature_and_a_block_name_one_chain() {
    // An address is the hash of a public key, so the same payer exists on every
    // F-Chain; a transaction lifted from one authenticated verbatim on the
    // others. Blocks were worse: every chain whose genesis carried the same
    // timestamp had the SAME genesis id.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Id other{'a', 'n', 'o', 't', 'h', 'e', 'r'};

    Chain home = new_test_vm(fund_all({k}), com.members, 1);

    Chain away = new_test_vm(fund_all({k}), com.members, 1, nullptr, other);
    check(true, "a second chain boots on the same genesis bytes, under another id");

    check(Id(home.vm->last_accepted()) != Id(away.vm->last_accepted()),
          "two chains must not share a genesis block");

    Transaction tx = register_tx(k, kTestScheme, digest_of("mine"), 1);
    accepted(tx.authenticate(Id(home.vm->chain_id())), "signed for home, it authenticates at home");
    refused(tx.authenticate(Id(away.vm->chain_id())), Err::BadSignature, "and not away");
    refused(away.vm->submit_tx(tx), Err::BadSignature, "so the replay is refused at admission");

    auto forced = force_block(away, {tx});
    refused(forced->check(), Err::BadSignature, "and inside a block too");

    accept_one(home, tx, "home accepts its own");
    auto home_tip = home.vm->get_block(Id(home.vm->last_accepted()));
    check(home_tip.has_value(), "home has a tip");
    if (home_tip) {
        auto parsed = away.vm->parse_block((*home_tip)->bytes());
        accepted(parsed, "the bytes are well formed; it is the chain that differs");
        if (parsed) check(!(*parsed)->check().has_value(), "but away cannot verify them");
    }
    check_eq(away.vm->height(), 0, "nothing from another chain moved this one");

    Transaction native = register_tx(k, kTestScheme, digest_of("theirs"), 1);
    k.sign_for(native, other);
    accepted(away.vm->submit_tx(native), "the control: signed for away, it works away");
}

void the_lifecycle_surface_is_answered() {
    // The consensus surface F implements without holding anything. They are
    // no-ops, and a no-op that crashes is not one.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    c.vm->prefer(c.vm->last_accepted());
    check(true, "a preference notification is answered");
    check_eq(c.vm->last_accepted_height(), c.vm->height(), "the seam reports the same height");
    check(c.vm->get(Id{0xde, 0xad}) == nullptr, "an unknown id is 'no', not a failure");
    check(c.vm->parse(ByteView{}) == nullptr, "and neither is a buffer that is not a block");
    check(c.vm->build() == nullptr, "an idle chain builds nothing over the seam");

    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("seam"), 1)), "queued");
    auto built = c.vm->build();
    check(built != nullptr, "and once there is something to carry, it builds");
    if (built != nullptr) {
        check(built->verify(), "the seam's verify accepts it");
        built->accept();
        check_eq(hex_of(Id(c.vm->last_accepted())), hex_of(Id(built->id())),
                 "and the seam's accept moves the tip");
        check_eq(hex_of(Id(built->root())), hex_of(kEmptyId),
                 "F commits no state root, and says so rather than inventing one");
    }
}

}  // namespace

int main() {
    std::printf("fhevm — the chain's lifecycle, and the bounds that keep it one\n\n");
    a_chain_boots_seated();
    a_chain_without_a_committee_is_unhealthy();
    genesis_refuses_a_committee_consensus_would_refuse();
    a_chain_with_no_identity_binds_nothing();
    the_height_index_names_the_accepted_chain();
    a_discarded_proposal_costs_nothing();
    a_block_reports_processing_until_it_is_accepted();
    an_accepted_block_reparses_from_its_stored_bytes();
    verify_refuses_a_block_that_is_not_one();
    a_block_reports_the_header_it_was_built_from();
    the_mempool_is_bounded();
    the_in_flight_set_tracks_only_what_is_still_in_flight();
    building_fails_closed();
    chain_time_and_height_are_bounded();
    a_proposer_never_builds_a_block_it_would_reject();
    an_orphan_cannot_rewind_the_chain();
    a_tip_that_moves_after_verify_is_caught_by_accept();
    a_follower_can_verify_against_a_parent_it_only_parsed();
    a_block_is_bounded_in_bytes_and_in_count();
    selection_stops_at_the_byte_bound();
    the_wire_verdict_is_the_memory_verdict();
    a_signature_and_a_block_name_one_chain();
    the_lifecycle_surface_is_answered();
    return report("vm");
}
