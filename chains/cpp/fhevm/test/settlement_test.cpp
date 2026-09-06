// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// settlement_test.cpp — that an F-Chain operation is paid for by BURNING real
// on-chain balance inside a consensus block, and not by an unbacked integer a
// caller writes into a request.
//
// Ported case for case from the Go F-Chain's settlement_test.go.

#include "check.hpp"
#include "fixtures.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

struct Seed {
    Id handle{};
    Id permit_id{};
};

Seed seed_permit(Chain& c, const TestKey& owner, const TestKey& grantee, std::uint32_t ops,
                 std::int64_t expiry) {
    Seed s;
    Transaction reg = register_tx(owner, kTestScheme, digest_of("seed-" + owner.hex_addr()), 1);
    accept_one(c, reg, "seed: register");
    s.handle = reg.subject;
    accept_one(c, grant_tx(owner, s.handle, grantee.addr, ops, expiry, 2), "seed: grant");
    s.permit_id = derive_permit_id(s.handle, owner.addr, grantee.addr, ops, expiry, 2);
    check(c.vm->permit(s.permit_id) != nullptr, "the grant creates the permit its inputs derive");
    return s;
}

std::uint64_t fee_of(const Transaction& tx) {
    auto f = fee_for(tx);
    return f ? *f : 0;
}

void a_fee_is_settled_through_consensus() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    auto bal = c.vm->balance(k.addr);
    check(bal && *bal == kTestFund, "genesis funds the payer");
    auto burned0 = c.vm->burned();
    check(burned0 && *burned0 == 0, "and nothing has been burned yet");

    Transaction tx = register_tx(k, kTestScheme, digest_of("treasury"), 1);

    // The fee is computed from the SCHEDULE, not supplied by the caller:
    // register + ckks-n14 = (21000 + 60000) gas, plus 16 gas for every byte the
    // transaction puts on the chain, all at 1000 nLUX per gas.
    std::uint64_t expected = fee_of(tx);
    std::uint64_t stored = tx.payload.size() + tx.scheme.size();
    check_eq(expected, (21'000 + 60'000 + stored * kGasPerByte) * kGasPrice,
             "the fee is exactly what the schedule prices");
    check(expected > 81'000'000, "and stored bytes are not free");

    auto id = c.vm->submit_tx(tx);
    accepted(id, "the transaction is admitted");
    check(id && *id == tx.id(), "under its own id");

    auto blk = c.vm->build_block();
    accepted(blk, "a block is built");
    if (!blk) return;
    accepted((*blk)->check(), "and verifies");

    auto pre = c.vm->balance(k.addr);
    check(pre && *pre == kTestFund, "verifying must not debit");

    accepted((*blk)->accept_block(), "acceptance settles it");
    auto post = c.vm->balance(k.addr);
    check(post && *post == kTestFund - expected, "the payer is debited the metered fee");
    auto burned1 = c.vm->burned();
    check(burned1 && *burned1 == expected, "and the same amount is burned, not credited anywhere");

    const CiphertextRecord* rec = c.vm->ciphertext(tx.subject);
    check(rec != nullptr, "the operation took effect through consensus");
    if (rec != nullptr) {
        check(rec->owner == k.addr, "owned by the payer");
        check(rec->scheme == kTestScheme, "under the scheme it named");
        check(rec->digest == digest_of("treasury"), "over the digest it carried");
        check(rec->registered_at == (*blk)->timestamp(),
              "and stamped with the accepting block's time, never a validator's clock");
    }

    check_eq(hex_of(Id(c.vm->last_accepted())), hex_of(Id((*blk)->id())),
             "the block is the new tip");
    check(c.vm->mempool().empty(), "and the queue is clear");
}

void an_unfunded_payer_cannot_settle() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    // Funded with far less than one operation's fee.
    Chain c = new_test_vm({{k.hex_addr(), 1'000}}, com.members, 1);

    Transaction tx = register_tx(k, kTestScheme, digest_of("x"), 1);
    refused(c.vm->submit_tx(tx), Err::InsufficientFunds,
            "admission refuses an unaffordable transaction");

    // Forced into a block anyway: consensus must still refuse it.
    refused(force_block(c, {tx})->check(), Err::InsufficientFunds, "and so does consensus");

    check(c.vm->ciphertext(tx.subject) == nullptr, "state is untouched");
    auto burned = c.vm->burned();
    check(burned && *burned == 0, "and nothing was burned");
}

void fees_accumulate_within_a_block() {
    // Affordability is checked against the payer's RUNNING debit inside the
    // block, not its opening balance: two operations that each fit alone but do
    // not fit together never share a block, and a peer proposing both is
    // refused.
    TestKey k = new_test_key();
    Committee com = new_committee(1);

    Transaction a = register_tx(k, kTestScheme, digest_of("a"), 1);
    Transaction b = register_tx(k, kTestScheme, digest_of("b"), 2);
    // Enough for one operation and one nLUX, never for two.
    Chain c = new_test_vm({{k.hex_addr(), fee_of(a) + 1}}, com.members, 1);

    accepted(c.vm->submit_tx(a), "the first is affordable on its own");
    accepted(c.vm->submit_tx(b), "and so is the second, at admission");

    auto blk = c.vm->build_block();
    accepted(blk, "the proposer takes what fits");
    check(blk && (*blk)->transactions().size() == 1, "which is only the affordable one");
    if (blk) accepted((*blk)->check(), "and the block it built verifies");
    check(c.vm->mempool().size() == 2, "selection does not drain the queue");

    refused(force_block(c, {a, b})->check(), Err::InsufficientFunds,
            "a peer proposing both is refused");
}

void the_gas_limit_is_the_payers_ceiling() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    Transaction tx = register_tx(k, kTestScheme, digest_of("tight"), 1);
    tx.gas_limit = 1;  // far below the scheduled 81,000
    k.sign(tx);

    accepted(c.vm->submit_tx(tx), "admission prices the operation but does not meter it");
    refused(c.vm->build_block(), Err::NoPendingTxs,
            "so the proposer has nothing it can actually build");
    refused(force_block(c, {tx})->check(), Err::OutOfGas, "and a peer proposing it is refused");

    auto burned = c.vm->burned();
    check(burned && *burned == 0, "an unmetered operation is never charged");
}

void a_tampered_or_unsigned_transaction_cannot_spend() {
    TestKey k = new_test_key();
    TestKey other = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    Transaction tampered = register_tx(k, kTestScheme, digest_of("x"), 1);
    tampered.nonce = 2;
    refused(c.vm->submit_tx(tampered), Err::BadSignature, "a tampered transaction");

    Transaction unsigned_tx = register_tx(k, kTestScheme, digest_of("y"), 1);
    unsigned_tx.auth.clear();
    unsigned_tx.sig.clear();
    refused(c.vm->submit_tx(unsigned_tx), Err::UnsignedTx, "an unsigned one");

    Transaction impersonating = register_tx(k, kTestScheme, digest_of("z"), 1);
    other.sign(impersonating);
    refused(c.vm->submit_tx(impersonating), Err::PayerMismatch, "an impersonating one");

    auto burned = c.vm->burned();
    check(burned && *burned == 0, "and none of them spent anything");
}

void a_replay_is_refused_twice() {
    // A captured signed transaction cannot be resubmitted to drain the payer
    // through repeated fee burns: the nonce rule refuses it at admission and
    // again in consensus, and no second burn occurs.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    Transaction tx = register_tx(k, kTestScheme, digest_of("replay"), 1);
    accept_one(c, tx, "the original");
    auto after = c.vm->balance(k.addr);

    refused(c.vm->submit_tx(tx), Err::BadNonce, "the replay is refused at admission");
    refused(force_block(c, {tx})->check(), Err::BadNonce, "and in consensus");

    auto again = c.vm->balance(k.addr);
    check(after && again && *after == *again, "the payer is not burned a second time");
}

void nonces_must_be_consecutive() {
    // Admission enforces the SAME nonce rule consensus does, counted over what
    // is already queued. It used to accept any nonce above the committed one,
    // so a payer could queue a gap that made every block containing it
    // unverifiable — and a verify-failed block is discarded without a rejection,
    // so the queue behind it went too.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    refused(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("skip"), 2)), Err::BadNonce,
            "a gap is refused outright");
    check(c.vm->mempool().empty(), "and never enters the queue");

    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("one"), 1)), "the first queues");
    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("two"), 2)),
             "the second follows the first that is already queued");
    refused(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("four"), 4)), Err::BadNonce,
            "and a gap after them is still a gap");

    auto blk = accept_queued(c, "both go out together");
    check(blk && blk->transactions().size() == 2, "in one block");
    check(c.vm->mempool().empty(), "acceptance is what clears the queue");

    accepted(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("three"), 3)),
             "after which the payer continues from the committed nonce");
}

void one_payer_cannot_claim_an_effect_twice() {
    // Without this guard both transactions pass every individual check and only
    // collide in acceptance, aborting the block that carried them.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    Transaction first = register_tx(k, kTestScheme, digest_of("same"), 1);
    Transaction second = register_tx(k, kTestScheme, digest_of("same"), 2);  // same handle

    accepted(c.vm->submit_tx(first), "the first claims the handle");
    refused(c.vm->submit_tx(second), Err::DuplicateEffect,
            "admission refuses the second claim on it");
    check(c.vm->mempool().size() == 1, "so only one is queued");

    refused(force_block(c, {first, second})->check(), Err::DuplicateEffect,
            "and consensus refuses a block carrying both");

    accept_queued(c, "the honest first transaction is unaffected");
    check(c.vm->ciphertext(first.subject) != nullptr, "and takes effect");

    refused(c.vm->submit_tx(register_tx(k, kTestScheme, digest_of("same"), 2)),
            Err::CiphertextExists, "once committed, a later duplicate is refused by state");
}

void an_unauthorized_transaction_reverts_and_still_pays() {
    // The pair here passes verify honestly — at that moment the permit is still
    // active — and only conflicts once the revocation lands, which is exactly
    // the case no per-transaction check can see coming. Aborting the block there
    // would mean one every validator certified and none could apply, which halts
    // the chain; reverting the one transaction does not.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({owner, grantee}), com.members, 1);

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    auto burned_before = c.vm->burned();
    auto grantee_before = c.vm->balance(grantee.addr);

    // Revocation first, then a request the revocation invalidates. The owner has
    // already spent nonces 1 and 2 seeding the permit.
    Transaction revoke = revoke_tx(owner, s.permit_id, 3);
    Transaction request = request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1);

    auto blk = force_block(c, {revoke, request});
    accepted(blk->check(),
             "both are well-formed, ordered and paid for — authorization is not verify's question");
    accepted(blk->accept_block(),
             "the block applies; only the transaction that lost its authority reverts");

    const PermitRecord* pm = c.vm->permit(s.permit_id);
    check(pm != nullptr && pm->status == kStatusRevoked, "the revocation took effect");
    check(c.vm->decrypt(derive_request_id(s.handle, grantee.addr, 1)) == nullptr,
          "and the request left no record");

    std::uint64_t request_fee = fee_of(request);
    std::uint64_t revoke_fee = fee_of(revoke);
    auto burned_after = c.vm->burned();
    check(burned_before && burned_after && *burned_after == *burned_before + request_fee + revoke_fee,
          "but it paid: a revert is not free block space");
    auto grantee_after = c.vm->balance(grantee.addr);
    check(grantee_before && grantee_after && *grantee_after == *grantee_before - request_fee,
          "and the fee came out of the payer that sent it");

    refused(c.vm->submit_tx(request), Err::BadNonce,
            "the nonce advanced, so the reverted transaction cannot be retried as-is");

    // The chain records WHY it reverted, so an operator is not left guessing.
    check(!c.vm->reverts().empty(), "the revert is recorded");
    check(!c.vm->reverts().empty() && c.vm->reverts().back().reason == Err::PermitRevoked,
          "with the reason authorization gave");
}

void an_abort_rolls_back_the_whole_block() {
    // The commit boundary: when settlement cannot proceed at all — here a nonce
    // verify would have caught, reached by accepting without verifying — the
    // store is rolled back and the caches reloaded, so no part of the block
    // survives.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{k.hex_addr(), kTestFund}}, com.members, 1);

    Transaction good = register_tx(k, kTestScheme, digest_of("good"), 1);
    Transaction gapped = register_tx(k, kTestScheme, digest_of("gapped"), 3);  // 2 is missing

    auto blk = force_block(c, {good, gapped});
    refused(blk->check(), Err::BadNonce, "consensus would never accept this block");
    refused(blk->accept_block(), Err::BadNonce, "and acceptance refuses it too");

    // The first transaction had already been applied and burned in memory when
    // the second failed. Neither survives.
    check(c.vm->ciphertext(good.subject) == nullptr, "the first did not survive");
    check(c.vm->ciphertext(gapped.subject) == nullptr, "nor the second");
    auto burned = c.vm->burned();
    check(burned && *burned == 0, "nothing was burned");
    auto bal = c.vm->balance(k.addr);
    check(bal && *bal == kTestFund, "the payer is whole");
    auto nonce = c.vm->nonce_of(k.addr);
    check(nonce && *nonce == 0, "and its nonce was not consumed");

    accept_one(c, good, "and the chain still works");
    check(c.vm->ciphertext(good.subject) != nullptr, "the same transaction applies afterwards");
}

void what_no_validator_can_proceed_past_aborts_the_block() {
    // The failures that abort a whole block rather than reverting one
    // transaction. Each is reached by accepting a block WITHOUT verifying it
    // first — which is exactly the case these checks exist for, since a peer's
    // block reaches acceptance only after verify passed, and a check that only
    // ever runs behind another is not a check.
    Committee com = new_committee(1);
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm(fund_all({k}), com.members, 1);
        Transaction tx;
        tx.type = 99;
        tx.payer = k.addr;
        tx.gas_limit = kTestGas;
        tx.nonce = 1;
        k.sign(tx);
        refused(force_block(c, {tx})->accept_block(), Err::InvalidTxType,
                "an unpriceable operation");
    }
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm(fund_all({k}), com.members, 1);
        Transaction tx = register_tx(k, kTestScheme, digest_of("starved"), 1);
        tx.gas_limit = 1;
        k.sign(tx);
        refused(force_block(c, {tx})->accept_block(), Err::OutOfGas,
                "gas beyond the payer's own limit");
    }
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm({{k.hex_addr(), 1}}, com.members, 1);
        refused(force_block(c, {register_tx(k, kTestScheme, digest_of("broke"), 1)})
                    ->accept_block(),
                Err::InsufficientFunds, "a fee the payer cannot pay");
    }
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm(fund_all({k}), com.members, 1);
        refused(force_block(c, {register_tx(k, kTestScheme, digest_of("gap"), 7)})->accept_block(),
                Err::BadNonce, "a nonce verify would have caught");
    }
}

void admission_reports_what_it_cannot_decide() {
    // The batch's own refusals over a peer's block: the shape checks it makes
    // before it reads any state.
    Committee com = new_committee(1);
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm(fund_all({k}), com.members, 1);
        Transaction tx;
        tx.type = 99;
        tx.payer = k.addr;
        tx.gas_limit = kTestGas;
        tx.nonce = 1;
        k.sign(tx);
        refused(force_block(c, {tx})->check(), Err::InvalidTxType, "a malformed transaction");
    }
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm(fund_all({k}), com.members, 1);
        Transaction tx = register_tx(k, kTestScheme, digest_of("overgas"), 1);
        tx.gas_limit = 1;
        k.sign(tx);
        refused(force_block(c, {tx})->check(), Err::OutOfGas, "gas over the declared limit");
    }
    {
        TestKey k = new_test_key();
        Chain c = new_test_vm({{k.hex_addr(), 1}}, com.members, 1);
        refused(force_block(c, {register_tx(k, kTestScheme, digest_of("poor"), 1)})->check(),
                Err::InsufficientFunds, "an unaffordable one");
    }
}

void one_unfit_transaction_does_not_destroy_the_block() {
    // Building runs the same admission rule verify runs and simply leaves out
    // what does not fit.
    TestKey rich = new_test_key();
    TestKey poor = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm({{rich.hex_addr(), kTestFund}, {poor.hex_addr(), 1'000}}, com.members, 1);

    Transaction good = register_tx(rich, kTestScheme, digest_of("good"), 1);
    accepted(c.vm->submit_tx(good), "the honest transaction is admitted");

    // The poor payer's transaction is refused at admission, but a peer could
    // still gossip it into this node's pool; put it there directly to prove
    // selection copes.
    c.vm->pool().push_back(register_tx(poor, kTestScheme, digest_of("broke"), 1));

    auto blk = c.vm->build_block();
    accepted(blk, "the proposer still builds");
    check(blk && (*blk)->transactions().size() == 1, "leaving the unaffordable one out");
    if (blk) {
        accepted((*blk)->check(), "a proposer cannot build a block its own verify would reject");
        accepted((*blk)->accept_block(), "and it applies");
    }
    check(c.vm->ciphertext(good.subject) != nullptr,
          "the honest transaction is not collateral damage");
}

void a_poisoned_queue_is_impossible() {
    // A payer could queue a nonce gap. Admission took it, verify refused the
    // block for it, and the engine discards a verify-failed block WITHOUT
    // rejecting it — so the transaction vanished and its effect claim did not.
    // Registration's effect is payer-independent, so any funded account could
    // permanently block any handle on that node, for free, and take the rest of
    // the queue with it every round.
    TestKey attacker = new_test_key();
    TestKey victim = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({attacker, victim}), com.members, 1);

    Id target = digest_of("a-handle-someone-else-wants");
    refused(c.vm->submit_tx(register_tx(attacker, kTestScheme, target, 3)), Err::BadNonce,
            "the gap never enters the queue");
    check(c.vm->mempool().empty(), "so it can never poison a block");
    check(c.vm->claims().empty(), "and a refused transaction claims nothing");

    Transaction honest = register_tx(victim, kTestScheme, target, 1);
    accept_one(c, honest, "the victim's honest registration");
    const CiphertextRecord* rec = c.vm->ciphertext(honest.subject);
    check(rec != nullptr && rec->owner == victim.addr, "goes through untouched");
}

void a_claim_tracks_the_queue_exactly() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    Transaction tx = register_tx(k, kTestScheme, digest_of("claimed"), 1);
    accepted(c.vm->submit_tx(tx), "the transaction is queued");
    check(c.vm->claims().size() == 1, "holding exactly its own claim");

    // Build and then walk away from the proposal entirely — no acceptance, no
    // rejection, which the engine is free to do.
    accepted(c.vm->build_block(), "a proposal is built");
    check(c.vm->mempool().size() == 1, "the transaction is still queued");
    check(c.vm->claims().size() == 1, "and still holds exactly its own claim");

    accept_queued(c, "acceptance is what clears both");
    check(c.vm->mempool().empty(), "the queue is empty");
    check(c.vm->claims().empty(), "and so is the claim set");
    check(c.vm->ciphertext(tx.subject) != nullptr,
          "a claim can no longer outlive the transaction that made it");
}

void a_member_cannot_double_vote_on_an_epoch() {
    // The effect qualified an epoch-advance vote by the PROPOSAL, but the
    // decision a member votes on is the epoch, of which exactly one is ever
    // open. Two votes from one member for two different committees therefore
    // had two different effects, survived admission and verify, and collided in
    // acceptance — and a block that passes verify on every validator and then
    // fails to apply on every validator halts the chain at that height.
    Committee com = new_committee(3);
    Chain c = new_test_vm(fund_all(com.keys), com.members, 2);
    Committee a = new_committee(3);
    Committee b = new_committee(3);
    Bytes pk{'k'};

    Transaction vote_a = advance_tx(com.keys[0], 1, a.members, 2, pk, 1);
    Transaction vote_b = advance_tx(com.keys[0], 1, b.members, 2, pk, 2);
    check_eq(hex_of(vote_a.effect()), hex_of(vote_b.effect()),
             "one member, one open decision, one effect — whatever it votes for");

    accepted(c.vm->submit_tx(vote_a), "the first vote is admitted");
    refused(c.vm->submit_tx(vote_b), Err::DuplicateEffect, "the second is refused");
    refused(force_block(c, {vote_a, vote_b})->check(), Err::DuplicateEffect,
            "and consensus refuses a block carrying both");
}

void proposer_freedom_is_exactly_the_gap_since_the_last_block() {
    // Chain time is monotone and bounded ahead, and that is ALL a chain can
    // promise about it. Within [parent timestamp, local clock + skew] the
    // proposer still chooses, so a permit that lapsed during a gap in block
    // production can still authorize one request in the block that closes the
    // gap. This measures that freedom rather than leaving it unexamined.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Committee com = new_committee(3);
    std::vector<TestKey> funded = com.keys;
    funded.insert(funded.end(), {owner, grantee});
    Chain c = new_test_vm(fund_all(funded), com.members, 2);

    std::int64_t base = kTestGenesisTime;
    c.vm->clock().set(base);
    std::int64_t expiry = base + 60;
    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, expiry);
    auto parent = c.vm->get_block(Id(c.vm->last_accepted())).value();

    // Long past the permit's expiry, admission refuses the request.
    c.vm->clock().set(expiry + 100'000);
    Transaction req = request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1);
    refused(c.vm->submit_tx(req), Err::PermitExpired, "admission refuses a lapsed permit");

    auto at = [&](std::int64_t ts) {
        return std::make_shared<Block>(c.vm.get(), Id(c.vm->last_accepted()),
                                       parent->height() + 1, ts, std::vector<Transaction>{req});
    };

    refused(at(parent->timestamp() - 1)->check(), Err::InvalidBlock,
            "the proposer cannot go below the parent, which is the bound that exists");

    // Inside the window it can, and the request is authorized — by a permit
    // that has, in wall-clock terms, expired. The block is valid; the
    // transaction applies. This is the residual, and it is exactly this large.
    auto inside = at(expiry - 1);
    accepted(inside->check(), "inside the window the block is valid");
    accepted(inside->accept_block(), "and it applies");
    check(c.vm->decrypt(derive_request_id(s.handle, grantee.addr, 1)) != nullptr,
          "so the request took effect under a permit wall-clock time had passed");

    // And it closes behind itself: chain time has now passed the expiry, so no
    // later block can reach back.
    Transaction later = request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 2);
    auto next = std::make_shared<Block>(c.vm.get(), Id(c.vm->last_accepted()),
                                        c.vm->height() + 1,
                                        c.vm->get_block(Id(c.vm->last_accepted()))
                                                .value()
                                                ->timestamp() + 2,
                                        std::vector<Transaction>{later});
    accepted(next->check(), "a later block is well-formed");
    accepted(next->accept_block(), "and applies");
    check(c.vm->decrypt(derive_request_id(s.handle, grantee.addr, 2)) == nullptr,
          "but the expired permit authorizes nothing once chain time passes it");
}

}  // namespace

int main() {
    std::printf("fhevm — what an operation costs, and who actually pays it\n\n");
    a_fee_is_settled_through_consensus();
    an_unfunded_payer_cannot_settle();
    fees_accumulate_within_a_block();
    the_gas_limit_is_the_payers_ceiling();
    a_tampered_or_unsigned_transaction_cannot_spend();
    a_replay_is_refused_twice();
    nonces_must_be_consecutive();
    one_payer_cannot_claim_an_effect_twice();
    an_unauthorized_transaction_reverts_and_still_pays();
    an_abort_rolls_back_the_whole_block();
    what_no_validator_can_proceed_past_aborts_the_block();
    admission_reports_what_it_cannot_decide();
    one_unfit_transaction_does_not_destroy_the_block();
    a_poisoned_queue_is_impossible();
    a_claim_tracks_the_queue_exactly();
    a_member_cannot_double_vote_on_an_epoch();
    proposer_freedom_is_exactly_the_gap_since_the_last_block();
    return report("settlement");
}
