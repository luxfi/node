// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fhe_test.cpp — the operations F actually exposes, and the threshold rules
// that make each of them safe.
//
// Registering an encrypted value, granting and withdrawing access to it, asking
// the committee to decrypt it, and rotating the committee. Ported case for case
// from the Go F-Chain's fhe_test.go.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/service.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

// new_decrypt_vm seats an n-member committee at threshold t and funds the
// members plus the given users.
struct DecryptChain {
    Chain chain;
    Committee committee;
};

DecryptChain new_decrypt_vm(std::size_t n, std::int64_t threshold,
                            const std::vector<TestKey>& users = {}) {
    DecryptChain d;
    d.committee = new_committee(n);
    std::vector<TestKey> funded = d.committee.keys;
    funded.insert(funded.end(), users.begin(), users.end());
    d.chain = new_test_vm(fund_all(funded), d.committee.members, threshold);
    return d;
}

// seed_permit registers a ciphertext owned by owner and grants grantee a permit
// over it. It is the starting state every decryption test needs, and it spends
// the owner's nonces 1 and 2 — so the owner's next transaction carries nonce 3.
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

    Transaction grant = grant_tx(owner, s.handle, grantee.addr, ops, expiry, 2);
    accept_one(c, grant, "seed: grant");
    s.permit_id = derive_permit_id(s.handle, owner.addr, grantee.addr, ops, expiry, 2);
    check(c.vm->permit(s.permit_id) != nullptr,
          "the grant creates the permit its inputs derive");
    return s;
}

void the_confidential_lifecycle() {
    // One encrypted value from registration to a completed threshold
    // decryption — the whole reason F exists, end to end.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;

    // 1. The owner registers an encrypted value. F stores its digest, never it.
    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    const CiphertextRecord* ct = c.vm->ciphertext(s.handle);
    check(ct != nullptr, "the ciphertext is registered");
    check(ct != nullptr && ct->epoch == 0,
          "and bound to the epoch it was registered in");

    // 2. The grantee, holding the permit, asks the committee to decrypt.
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    const DecryptRecord* rec = c.vm->decrypt(request_id);
    check(rec != nullptr, "the request is recorded");
    if (rec == nullptr) return;
    check(rec->status == RequestStatus::Pending, "pending");
    check(rec->ciphertext_handle == s.handle, "over the handle asked for");
    check(rec->permit_id == s.permit_id, "under the permit that authorized it");
    check(rec->epoch == 0, "in the seated epoch");
    check(rec->expiry > rec->created_at,
          "an unbounded request would outlive the committee that can answer it");
    check_eq(std::uint64_t(rec->expiry), std::uint64_t(rec->created_at + kDefaultRequestWindow),
             "and the default window is what bounds it");

    // 3. Two of three members attest the same result handle. F never sees the
    //    plaintext — the committee combines its shares off-chain and agrees on
    //    the handle of what came out.
    Id result = digest_of("decrypted-result-handle");
    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, result, 1), "first attestation");
    rec = c.vm->decrypt(request_id);
    check(rec->status == RequestStatus::Pending, "one member is not a threshold");
    check(rec->attestations.size() == 1, "and its vote is recorded");

    accept_one(c, fulfill_tx(d.committee.keys[1], request_id, result, 1), "second attestation");
    rec = c.vm->decrypt(request_id);
    check(rec->status == RequestStatus::Completed, "the threshold answers the request");
    check(rec->result_handle == result, "with the handle the committee agreed on");
    check(rec->attestations.size() == 2, "over two votes");
    check(rec->completed_at != 0, "and it records when");

    // 4. The answer is final: a third attestation is refused.
    refused(c.vm->submit_tx(fulfill_tx(d.committee.keys[2], request_id, result, 1)),
            Err::RequestClosed, "a third attestation to an answered request");
}

void a_conflicting_attestation_cannot_stall() {
    // A lying member buys nothing but its own burnt fee. It attests first, and
    // wrong; the honest members still reach the threshold on the true result,
    // because votes are tallied per value rather than pinned by whoever spoke
    // first.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;
    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    Id liar = digest_of("forged");
    Id truth = digest_of("true-result");

    accept_one(c, fulfill_tx(d.committee.keys[2], request_id, liar, 1), "the false vote");
    check(c.vm->decrypt(request_id)->status == RequestStatus::Pending,
          "a false vote decides nothing");

    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, truth, 1), "an honest vote");
    accept_one(c, fulfill_tx(d.committee.keys[1], request_id, truth, 1), "and another");

    const DecryptRecord* rec = c.vm->decrypt(request_id);
    check(rec->status == RequestStatus::Completed, "the honest majority answers");
    check(rec->result_handle == truth, "with the true result");
    check(rec->attestations.size() == 3,
          "the false vote is recorded, and counted against its own value");

    auto bal = c.vm->balance(d.committee.keys[2].addr);
    check(bal && *bal < kTestFund, "and the liar paid for the privilege");
}

void only_the_committee_answers() {
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    TestKey stranger = new_test_key();
    Committee com = new_committee(3);
    std::vector<TestKey> funded = com.keys;
    funded.insert(funded.end(), {owner, grantee, stranger});
    Chain c = new_test_vm(fund_all(funded), com.members, 2);

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);
    Id result = digest_of("r");

    refused(c.vm->submit_tx(fulfill_tx(stranger, request_id, result, 1)), Err::NotCommittee,
            "a stranger cannot answer");
    refused(c.vm->submit_tx(fulfill_tx(grantee, request_id, result, 2)), Err::NotCommittee,
            "and neither can the requester, who has every right to the answer");

    accept_one(c, fulfill_tx(com.keys[0], request_id, result, 1), "a member's vote");

    // One member, one vote — but the vote is not spent by being cast. A second
    // attestation REPLACES the first rather than adding to the tally, so a
    // member can correct itself without ever counting twice.
    accept_one(c, fulfill_tx(com.keys[0], request_id, digest_of("other"), 2), "and its second");
    const DecryptRecord* rec = c.vm->decrypt(request_id);
    check(rec->attestations.size() == 1, "one entry per member, however often it votes");
    check(rec->attestations[0].value == digest_of("other"), "holding its latest choice");
    check_eq(std::uint64_t(tally(rec->attestations, result)), 0,
             "the withdrawn vote counts for nothing");
    check(rec->status == RequestStatus::Pending, "and one member is still not a threshold");
}

void an_unknown_request_cannot_be_answered() {
    DecryptChain d = new_decrypt_vm(3, 2);
    refused(d.chain.vm->submit_tx(
                fulfill_tx(d.committee.keys[0], digest_of("never-asked"), digest_of("r"), 1)),
            Err::RequestNotFound, "the committee cannot invent answers");
}

void an_expired_request_cannot_be_answered() {
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;

    std::int64_t now = kTestGenesisTime;
    c.vm->clock().set(now);
    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);

    std::int64_t expiry = now + 60;
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, expiry, 1), "request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    Transaction fulfil = fulfill_tx(d.committee.keys[0], request_id, digest_of("r"), 1);
    accepted(check_auth(fulfil, *c.vm, expiry), "still answerable inside the window");

    c.vm->clock().set(expiry + 1);
    refused(c.vm->submit_tx(fulfil), Err::RequestExpired, "one second past it, closed");

    // And the read surface says so, rather than reporting it as still pending.
    Service svc(*c.vm);
    auto view = svc.decrypt(hex_of(request_id));
    check(view && view->status == "expired",
          "the read surface reports it expired rather than pending");
}

void the_permit_gates_decryption() {
    // The permit is the whole authority for a decryption request: it must
    // exist, be unrevoked, be unexpired, name THIS handle, name THIS grantee,
    // and confer decrypt specifically.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    TestKey stranger = new_test_key();
    Committee com = new_committee(1);
    std::vector<TestKey> funded = com.keys;
    funded.insert(funded.end(), {owner, grantee, stranger});
    Chain c = new_test_vm(fund_all(funded), com.members, 1);

    std::int64_t now = kTestGenesisTime;
    c.vm->clock().set(now);
    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);

    refused(c.vm->submit_tx(request_tx(grantee, kTestScheme, s.handle, digest_of("nope"), 0, 1)),
            Err::PermitNotFound, "no such permit");
    refused(c.vm->submit_tx(request_tx(stranger, kTestScheme, s.handle, s.permit_id, 0, 1)),
            Err::Unauthorized, "a permit that belongs to someone else");

    Transaction other = register_tx(owner, kTestScheme, digest_of("other-value"), 3);
    accept_one(c, other, "a second ciphertext");
    refused(c.vm->submit_tx(request_tx(grantee, kTestScheme, other.subject, s.permit_id, 0, 1)),
            Err::PermitInvalid, "a permit for another handle");

    refused(c.vm->submit_tx(
                request_tx(grantee, kTestScheme, digest_of("unregistered"), s.permit_id, 0, 1)),
            Err::CiphertextNotFound, "a ciphertext that was never registered");

    Transaction compute_only = grant_tx(owner, s.handle, grantee.addr, kPermitOpCompute, 0, 4);
    accept_one(c, compute_only, "a compute-only grant");
    Id compute_id = derive_permit_id(s.handle, owner.addr, grantee.addr, kPermitOpCompute, 0, 4);
    refused(c.vm->submit_tx(request_tx(grantee, kTestScheme, s.handle, compute_id, 0, 1)),
            Err::PermitInvalid, "a permit conferring compute but not decrypt");

    std::int64_t expiry = now + 30;
    Transaction short_lived = grant_tx(owner, s.handle, grantee.addr, kPermitOpDecrypt, expiry, 5);
    accept_one(c, short_lived, "a short-lived grant");
    Id short_id = derive_permit_id(s.handle, owner.addr, grantee.addr, kPermitOpDecrypt, expiry, 5);
    c.vm->clock().set(expiry + 1);
    refused(c.vm->submit_tx(request_tx(grantee, kTestScheme, s.handle, short_id, 0, 1)),
            Err::PermitExpired, "a permit that has expired");
    c.vm->clock().set(now);

    accept_one(c, revoke_tx(owner, s.permit_id, 6), "the revocation");
    const PermitRecord* pm = c.vm->permit(s.permit_id);
    check(pm != nullptr && pm->status == kStatusRevoked, "the permit reads back revoked");
    refused(c.vm->submit_tx(request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1)),
            Err::PermitRevoked, "a permit that was revoked");
}

void only_the_owner_grants_and_revokes() {
    // A capability over an encrypted value comes from its owner and from nobody
    // else — including the grantee, who cannot pass its access on, and cannot
    // keep it after the owner withdraws it.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    TestKey stranger = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({owner, grantee, stranger}), com.members, 1);

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);

    refused(c.vm->submit_tx(grant_tx(stranger, s.handle, stranger.addr, kPermitOpDecrypt, 0, 1)),
            Err::Unauthorized, "a stranger cannot grant over someone else's ciphertext");
    refused(c.vm->submit_tx(grant_tx(grantee, s.handle, stranger.addr, kPermitOpDecrypt, 0, 1)),
            Err::Unauthorized, "nor can the grantee pass its own access along");
    refused(c.vm->submit_tx(
                grant_tx(owner, digest_of("ghost"), grantee.addr, kPermitOpDecrypt, 0, 3)),
            Err::CiphertextNotFound, "granting over a ciphertext nobody registered");
    refused(c.vm->submit_tx(revoke_tx(stranger, s.permit_id, 1)), Err::Unauthorized,
            "a stranger cannot withdraw someone else's grant");
    refused(c.vm->submit_tx(revoke_tx(owner, digest_of("no-permit"), 3)), Err::PermitNotFound,
            "revoking something that was never granted");

    accept_one(c, revoke_tx(owner, s.permit_id, 3), "the owner can");
    refused(c.vm->submit_tx(revoke_tx(owner, s.permit_id, 4)), Err::PermitRevoked,
            "and once withdrawn it stays withdrawn");
}

void registration_is_content_addressed() {
    // A handle names its content: the same body under the same scheme is the
    // same handle, so it cannot be registered twice, and a handle cannot be
    // claimed by someone who does not have the body to hash.
    TestKey a = new_test_key();
    TestKey b = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({a, b}), com.members, 1);

    Id body = digest_of("the-encrypted-body");
    accept_one(c, register_tx(a, kTestScheme, body, 1), "the first registration");

    refused(c.vm->submit_tx(register_tx(b, kTestScheme, body, 1)), Err::CiphertextExists,
            "a different account cannot re-register the same content");

    // The same content under a DIFFERENT scheme is a different value, and is
    // registrable — a CKKS ciphertext and a BFV one are not the same object.
    accept_one(c, register_tx(b, "bfv-n13", body, 1), "the same body under another scheme");
    check(c.vm->ciphertexts().size() == 2, "which is a second registration");

    const CiphertextRecord* one = c.vm->ciphertext(derive_handle(body, kTestScheme));
    const CiphertextRecord* two = c.vm->ciphertext(derive_handle(body, "bfv-n13"));
    check(one != nullptr && one->scheme == kTestScheme, "each names its own scheme");
    check(two != nullptr && two->scheme == "bfv-n13", "and they are not the same record");
}

void the_committee_installs_its_successor() {
    DecryptChain d = new_decrypt_vm(3, 2);
    Chain& c = d.chain;
    Committee next = new_committee(3);
    Bytes next_pk{'e', 'p', 'o', 'c', 'h', '-', '1'};

    // Fund the incoming committee so it can answer once seated.
    for (const auto& k : next.keys) {
        accepted(c.vm->ledger().credit(k.addr, kTestFund), "funding the incoming committee");
    }
    accepted(c.store->commit(), "and committing it");

    accept_one(c, advance_tx(d.committee.keys[0], 1, next.members, 2, next_pk, 1), "one vote");
    check_eq(c.vm->current_epoch_number(), 0, "one member is not a threshold");
    const EpochRecord* cur = c.vm->epoch(0);
    check(cur != nullptr && cur->attestations.size() == 1, "but the vote is recorded");
    check(cur != nullptr && cur->status == EpochStatus::Active, "and the epoch is still sitting");

    accepted(c.vm->submit_tx(advance_tx(d.committee.keys[1], 1, next.members, 2, next_pk, 1)),
             "the deciding vote is admitted");
    auto blk = accept_queued(c, "the deciding vote");
    check_eq(c.vm->current_epoch_number(), 1, "and installs the successor");

    const EpochRecord* old = c.vm->epoch(0);
    check(old != nullptr && old->status == EpochStatus::Ended, "the old epoch is closed");
    check(blk && old != nullptr && old->end_time == blk->timestamp(),
          "at the accepting block's time");

    const EpochRecord* installed = c.vm->epoch(1);
    check(installed != nullptr, "the successor is seated");
    if (installed != nullptr) {
        check(installed->status == EpochStatus::Active, "and active");
        check_eq(std::uint64_t(installed->threshold), 2, "at the threshold proposed");
        check(installed->public_key == next_pk, "with the network key proposed");
        check(installed->committee.size() == 3, "and the members proposed");
        check(installed->attestations.empty(), "a fresh epoch starts with no votes cast");
    }

    // The outgoing committee no longer decides anything.
    Committee further = new_committee(3);
    Bytes k{'k'};
    refused(c.vm->submit_tx(advance_tx(d.committee.keys[2], 2, further.members, 2, k, 1)),
            Err::NotCommittee, "the outgoing committee decides nothing");
    accepted(check_auth(advance_tx(next.keys[0], 2, further.members, 2, k, 1), *c.vm,
                        c.vm->clock().now()),
             "and the incoming one does");
}

void epoch_advance_refusals() {
    TestKey stranger = new_test_key();
    Committee com = new_committee(3);
    std::vector<TestKey> funded = com.keys;
    funded.push_back(stranger);
    Chain c = new_test_vm(fund_all(funded), com.members, 2);
    Committee next = new_committee(3);
    Bytes pk{'k'};

    refused(c.vm->submit_tx(advance_tx(stranger, 1, next.members, 2, pk, 1)), Err::NotCommittee,
            "a stranger cannot propose a successor");
    refused(c.vm->submit_tx(advance_tx(com.keys[0], 2, next.members, 2, pk, 1)),
            Err::EpochMismatch, "nor can a member skip an epoch");
    refused(c.vm->submit_tx(advance_tx(com.keys[0], 0, next.members, 2, pk, 1)),
            Err::EpochMismatch, "nor re-elect the current one");

    // One member, one vote — a member that moves to a rival proposal moves its
    // single vote rather than splitting the tally in its own favour.
    accept_one(c, advance_tx(com.keys[0], 1, next.members, 2, pk, 1), "a first vote");
    Committee rival = new_committee(3);
    accept_one(c, advance_tx(com.keys[0], 1, rival.members, 2, pk, 2), "and a change of mind");

    const EpochRecord* ep = c.vm->epoch(0);
    check(ep != nullptr && ep->attestations.size() == 1,
          "one entry per member, however often it votes");
    check_eq(std::uint64_t(tally(ep->attestations, committee_digest(1, 2, view(pk), next.members))),
             0, "the abandoned proposal keeps none of its support");
    check_eq(std::uint64_t(tally(ep->attestations, committee_digest(1, 2, view(pk), rival.members))),
             1, "and the new one has it");
    check_eq(c.vm->current_epoch_number(), 0, "and one member is still not a threshold");
}

void a_request_binds_to_the_seated_epoch() {
    // An answer comes from the committee that was seated when the request was
    // made. A later committee holds different key shares and never saw the
    // permit, so it must not be able to answer.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;
    Committee next = new_committee(3);
    for (const auto& k : next.keys) {
        accepted(c.vm->ledger().credit(k.addr, kTestFund), "funding the incoming committee");
    }
    accepted(c.store->commit(), "and committing it");

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    Bytes pk{'k'};
    accept_one(c, advance_tx(d.committee.keys[0], 1, next.members, 2, pk, 1), "rotation, one");
    accept_one(c, advance_tx(d.committee.keys[1], 1, next.members, 2, pk, 1), "rotation, two");
    check_eq(c.vm->current_epoch_number(), 1, "the committee rotated");

    refused(c.vm->submit_tx(fulfill_tx(next.keys[0], request_id, digest_of("r"), 1)),
            Err::NotCommittee, "the NEW committee cannot answer the OLD request");

    Id result = digest_of("r");
    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, result, 2), "the old committee can");
    accept_one(c, fulfill_tx(d.committee.keys[1], request_id, result, 2), "and reaches threshold");
    check(c.vm->decrypt(request_id)->status == RequestStatus::Completed, "so the request completes");

    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 2), "a fresh request");
    const DecryptRecord* fresh = c.vm->decrypt(derive_request_id(s.handle, grantee.addr, 2));
    check(fresh != nullptr && fresh->epoch == 1, "belongs to the new epoch");
}

void a_committeeless_chain_answers_nothing() {
    // A chain whose genesis seated nobody refuses every threshold decision
    // rather than accepting one from anybody.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Chain c = new_test_vm(fund_all({owner, grantee}), {}, 0);

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    refused(c.vm->submit_tx(fulfill_tx(owner, request_id, digest_of("r"), 3)), Err::EpochNotFound,
            "there is no epoch record to name a committee");

    Committee next = new_committee(3);
    Bytes pk{'k'};
    refused(c.vm->submit_tx(advance_tx(owner, 1, next.members, 2, pk, 3)), Err::NotCommittee,
            "and nobody can seat themselves");
}

void a_split_vote_can_be_resolved() {
    // One vote per member, no revote, no timeout, no reset — and the tally only
    // cleared on the advance that could not happen. A committee that split its
    // vote could never rotate again, which at unanimity one member could do
    // alone, and which an honest DKG race could do with no adversary at all. A
    // member's LATEST vote is now its vote.
    DecryptChain d = new_decrypt_vm(3, 3);  // unanimity: the worst case
    Chain& c = d.chain;
    Committee agreed = new_committee(3);
    Committee stale = new_committee(3);
    Bytes pk{'k'};

    accept_one(c, advance_tx(d.committee.keys[0], 1, stale.members, 3, pk, 1), "the dissenter");
    accept_one(c, advance_tx(d.committee.keys[1], 1, agreed.members, 3, pk, 1), "the majority");
    accept_one(c, advance_tx(d.committee.keys[2], 1, agreed.members, 3, pk, 1), "and the rest");
    check_eq(c.vm->current_epoch_number(), 0, "no proposal has unanimity yet");
    check(c.vm->epoch(0)->attestations.size() == 3, "one entry per member, not one per vote");

    accept_one(c, advance_tx(d.committee.keys[0], 1, agreed.members, 3, pk, 2),
               "the dissenter changes its mind");
    check_eq(c.vm->current_epoch_number(), 1, "the committee converged and rotated");
    const EpochRecord* installed = c.vm->epoch(1);
    check(installed != nullptr && installed->committee.size() == 3, "the successor is seated");
    check(installed != nullptr && installed->attestations.empty(),
          "a fresh epoch starts with no votes cast");
    const EpochRecord* old = c.vm->epoch(0);
    check(old != nullptr && old->attestations.size() == 3,
          "a member's second vote replaced its first");
    check_eq(std::uint64_t(tally(old->attestations,
                                 committee_digest(1, 3, view(pk), agreed.members))),
             3, "and the agreed proposal holds every vote");
}

void a_member_can_correct_its_attestation() {
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 3, {owner, grantee});
    Chain& c = d.chain;

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    Id wrong = digest_of("wrong");
    Id right = digest_of("right");
    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, wrong, 1), "a wrong attestation");
    accept_one(c, fulfill_tx(d.committee.keys[1], request_id, right, 1), "and two right ones");
    accept_one(c, fulfill_tx(d.committee.keys[2], request_id, right, 1), "");
    check(c.vm->decrypt(request_id)->status == RequestStatus::Pending,
          "two of three is not unanimity");

    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, right, 2), "the correction");
    const DecryptRecord* rec = c.vm->decrypt(request_id);
    check(rec->status == RequestStatus::Completed, "which completes it");
    check(rec->result_handle == right, "on the right result");
    check(rec->attestations.size() == 3, "one entry per member");
    check_eq(std::uint64_t(tally(rec->attestations, wrong)), 0,
             "the withdrawn vote counts for nothing");
}

void revocation_stops_an_in_flight_decryption() {
    // The permit that authorized the ask must still authorize it. Revocation is
    // a withdrawal of consent and reaches a request already in flight —
    // otherwise an owner who revoked would watch the committee answer anyway
    // and deliver the plaintext to the callback.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    Id result = digest_of("plaintext-handle");
    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, result, 1), "one answer already in");
    accept_one(c, revoke_tx(owner, s.permit_id, 3), "then consent is withdrawn");

    refused(c.vm->submit_tx(fulfill_tx(d.committee.keys[1], request_id, result, 1)),
            Err::PermitRevoked, "the committee can no longer complete it");

    // And a peer forcing it into a block gets a revert, not an answer.
    Transaction forced = fulfill_tx(d.committee.keys[1], request_id, result, 1);
    auto blk = force_block(c, {forced});
    accepted(blk->check(), "the block is well-formed, so it verifies");
    accepted(blk->accept_block(), "and applies");
    const DecryptRecord* rec = c.vm->decrypt(request_id);
    check(rec->status == RequestStatus::Pending, "a revoked permit answers nothing");
    check(rec->attestations.size() == 1, "the second attestation never landed");
}

void expiry_does_not_strand_an_answered_request() {
    // Expiry bounds the ASK, not the ANSWER. A permit that ran out after a
    // request was properly made does not strand it — the grantee asked in time,
    // and the committee answering later is not the grantee acting.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;

    std::int64_t base = kTestGenesisTime;
    c.vm->clock().set(base);
    std::int64_t permit_expiry = base + 60;
    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, permit_expiry);

    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, base + 10'000, 1),
               "the request, made in time");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    c.vm->clock().set(permit_expiry + 1);
    Id result = digest_of("answer");
    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, result, 1), "answered afterwards");
    accept_one(c, fulfill_tx(d.committee.keys[1], request_id, result, 1), "and again");

    const DecryptRecord* rec = c.vm->decrypt(request_id);
    check(rec->status == RequestStatus::Completed,
          "the grantee asked while it could; a lapsed permit is not a withdrawn one");
    check(rec->result_handle == result, "and the answer stands");
}

void an_unknown_operation_is_refused() {
    // Authorization fails closed on an operation it does not know, rather than
    // falling through to an effect. Application has the same default and it is
    // UNREACHABLE — authorization runs first — but without it an unknown type
    // would report the transaction APPLIED, which is the one answer that must
    // never be given by accident.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);

    Transaction alien;
    alien.type = 99;
    alien.payer = k.addr;
    alien.nonce = 1;
    refused(check_auth(alien, *c.vm, 0), Err::InvalidTxType, "authorization refuses it");

    auto applied = apply(alien, *c.vm, 0);
    check(applied.has_value(), "an unknown operation reverts; it does not halt the block");
    check(applied.has_value() && !*applied, "and it did not take effect");
}

void state_survives_a_reload() {
    // The caches are a projection of the database and not the truth: rebuilding
    // them from disk yields the same chain.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    DecryptChain d = new_decrypt_vm(3, 2, {owner, grantee});
    Chain& c = d.chain;

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);
    accept_one(c, fulfill_tx(d.committee.keys[0], request_id, digest_of("r"), 1), "one answer");

    DecryptRecord before = *c.vm->decrypt(request_id);
    std::uint64_t height = c.vm->height();
    std::uint64_t epoch = c.vm->current_epoch_number();

    accepted(c.vm->load_state(), "the caches reload from the store");

    check_eq(c.vm->height(), height, "the chain is where it was");
    check_eq(c.vm->current_epoch_number(), epoch, "at the epoch it was at");
    const DecryptRecord* after = c.vm->decrypt(request_id);
    check(after != nullptr && after->attestations == before.attestations,
          "with the same attestations");
    check(after != nullptr && after->status == before.status, "and the same status");
    check(c.vm->ciphertext(s.handle) != nullptr, "the ciphertext is still there");
    const PermitRecord* pm = c.vm->permit(s.permit_id);
    check(pm != nullptr && pm->status == kStatusActive, "the permit is still active");
    check(c.vm->epoch(0) != nullptr && c.vm->epoch(0)->committee.size() == 3,
          "and the committee is still seated");
}

}  // namespace

int main() {
    std::printf("fhevm — the confidential lifecycle, and the rules that guard it\n\n");
    the_confidential_lifecycle();
    a_conflicting_attestation_cannot_stall();
    only_the_committee_answers();
    an_unknown_request_cannot_be_answered();
    an_expired_request_cannot_be_answered();
    the_permit_gates_decryption();
    only_the_owner_grants_and_revokes();
    registration_is_content_addressed();
    the_committee_installs_its_successor();
    epoch_advance_refusals();
    a_request_binds_to_the_seated_epoch();
    a_committeeless_chain_answers_nothing();
    a_split_vote_can_be_resolved();
    a_member_can_correct_its_attestation();
    revocation_stops_an_in_flight_decryption();
    expiry_does_not_strand_an_answered_request();
    an_unknown_operation_is_refused();
    state_survives_a_reload();
    return report("fhe");
}
