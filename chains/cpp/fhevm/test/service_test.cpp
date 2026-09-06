// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// service_test.cpp — the read surface.
//
// Mutating operations are submitted as client-signed transactions and take
// effect only through fee-settled consensus blocks. Everything else is a
// read-only query of PUBLIC state. Ported case for case from the Go F-Chain's
// service_test.go.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/service.hpp"

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
    return s;
}

void the_public_parameters_are_the_runtimes() {
    // The parameters F reports come from the FHE runtime's own threshold
    // configuration — so a client encrypting for F and a node evaluating for F
    // agree by construction — while the threshold and network key come from the
    // seated epoch on chain.
    Committee com = new_committee(3);
    Chain c = new_test_vm({}, com.members, 2);
    Service svc(*c.vm);

    auto p = svc.public_params();
    check_eq(std::uint64_t(p.log_n), 14, "the ring dimension is the runtime's");
    check_eq(std::uint64_t(p.log_qp), 435, "and so is log(QP)");
    check_eq(std::uint64_t(p.log_scale), 45, "and the default scale");
    check_eq(p.epoch, 0, "the epoch comes from the chain");
    check_eq(std::uint64_t(p.threshold), 2, "and so does the threshold");
    check_eq(p.public_key, hex(view(network_public_key())),
             "and the network key the committee generated");
    check_eq(p.chain_id, id_string(Id(c.vm->chain_id())), "and the chain names itself");
}

void the_committee_is_readable_per_epoch() {
    Committee com = new_committee(3);
    Chain c = new_test_vm({}, com.members, 2);
    Service svc(*c.vm);

    CommitteeView v = svc.current_committee();
    check_eq(v.epoch, 0, "the seated epoch");
    check_eq(std::uint64_t(v.threshold), 2, "at its threshold");
    check(v.members.size() == 3, "with its members");
    for (std::size_t i = 0; i < v.members.size() && i < com.members.size(); ++i) {
        check_eq(v.members[i].node_id, node_id_string(com.members[i].node_id),
                 "member " + std::to_string(i) + " names its node");
        check_eq(v.members[i].public_key, hex(view(com.members[i].public_key)),
                 "and carries its public key");
        check_eq(std::uint64_t(v.members[i].index), i, "at the index it was seated with");
    }

    refused(svc.committee(9), Err::EpochNotFound,
            "an epoch nobody ever seated is an error, not an empty answer");
}

void a_ciphertext_reads_back_with_its_digest() {
    // A registered value reads back through the read surface WITH its digest —
    // so a client can check a body it fetched from off-chain storage — and the
    // listing filters work.
    TestKey a = new_test_key();
    TestKey b = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({a, b}), com.members, 1);
    Service svc(*c.vm);

    Id digest = digest_of("value-one");
    accept_one(c, register_tx(a, kTestScheme, digest, 1), "a registration");
    accept_one(c, register_tx(b, "bfv-n13", digest_of("value-two"), 1), "and another");

    Id handle = derive_handle(digest, kTestScheme);
    auto got = svc.ciphertext(hex_of(handle));
    accepted(got, "the ciphertext reads back");
    if (got) {
        check_eq(got->handle, hex_of(handle), "under its handle");
        check_eq(got->digest, hex_of(digest), "carrying the body's digest");
        check_eq(got->owner, account_string(a.addr), "owned by its registrant");
        check_eq(got->scheme, std::string(kTestScheme), "under the scheme it named");
        check_eq(std::uint64_t(got->size), 4096, "at the size it declared");
        check_eq(got->chain_id, id_string(Id(c.vm->chain_id())), "on this chain");
    }

    auto all = svc.ciphertexts("", "");
    check(all && all->size() == 2, "the listing shows both");
    auto by_scheme = svc.ciphertexts("", "bfv-n13");
    check(by_scheme && by_scheme->size() == 1, "filtering by scheme narrows it");
    check(by_scheme && !by_scheme->empty() && (*by_scheme)[0].scheme == "bfv-n13",
          "to the one that matches");
    auto by_owner = svc.ciphertexts(a.hex_addr(), "");
    check(by_owner && by_owner->size() == 1, "and so does filtering by owner");
    check(by_owner && !by_owner->empty() && (*by_owner)[0].owner == account_string(a.addr),
          "to the one it owns");

    refused(svc.ciphertext(hex_of(digest_of("absent"))), Err::CiphertextNotFound,
            "a handle nobody registered");
    // A filter that does not DECODE is a refusal, not an empty listing: an empty
    // listing reads as "this owner has nothing".
    refused(svc.ciphertexts("not-an-address", ""), Err::InvalidPayload,
            "and a filter that does not decode");
}

void a_permit_reads_back_with_its_capabilities() {
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({owner, grantee}), com.members, 1);
    Service svc(*c.vm);

    std::uint32_t ops = kPermitOpDecrypt | kPermitOpCompute;
    Seed s = seed_permit(c, owner, grantee, ops, 0);

    auto got = svc.permit(hex_of(s.permit_id));
    accepted(got, "the permit reads back");
    if (got) {
        check_eq(got->permit_id, hex_of(s.permit_id), "under its id");
        check_eq(got->handle, hex_of(s.handle), "over the handle it grants on");
        check_eq(got->grantor, account_string(owner.addr), "from its grantor");
        check_eq(got->grantee, account_string(grantee.addr), "to its grantee");
        check_eq(std::uint64_t(got->operations), ops, "with the capability bits it confers");
        check_eq(got->status, std::string(kStatusActive), "and it is active");
    }

    accept_one(c, revoke_tx(owner, s.permit_id, 3), "the revocation");
    got = svc.permit(hex_of(s.permit_id));
    check(got && got->status == kStatusRevoked, "after which it reads back revoked");

    refused(svc.permit(hex_of(digest_of("absent"))), Err::PermitNotFound,
            "a permit nobody granted");
}

void a_request_shows_how_far_the_committee_has_got() {
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Committee com = new_committee(3);
    std::vector<TestKey> funded = com.keys;
    funded.insert(funded.end(), {owner, grantee});
    Chain c = new_test_vm(fund_all(funded), com.members, 2);
    Service svc(*c.vm);

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);

    auto got = svc.decrypt(hex_of(request_id));
    accepted(got, "the request reads back");
    if (got) {
        check_eq(got->request_id, hex_of(request_id), "under its id");
        check_eq(got->handle, hex_of(s.handle), "over the handle asked about");
        check_eq(got->permit_id, hex_of(s.permit_id), "under the permit that authorized it");
        check_eq(got->requester, account_string(grantee.addr), "from its requester");
        check_eq(got->status, "pending", "still pending");
        check_eq(std::uint64_t(got->threshold), 2, "against the epoch's threshold");
        check(got->attestations.empty(), "with nothing attested yet");
        check(got->result_handle.empty(), "and a pending request has no result");
        check_eq(got->callback, "ca11000000000000000000000000000000000000",
                 "carrying the callback it named");
        check_eq(got->selector, "01020304", "and the selector");
    }

    Id result = digest_of("answer");
    accept_one(c, fulfill_tx(com.keys[0], request_id, result, 1), "one attestation");
    got = svc.decrypt(hex_of(request_id));
    check(got && got->attestations.size() == 1, "which shows up");
    check(got && !got->attestations.empty() &&
              got->attestations[0].member == account_string(com.keys[0].addr),
          "naming the member that cast it");
    check(got && !got->attestations.empty() && got->attestations[0].value == hex_of(result),
          "and the value it attested");
    check(got && got->status == "pending", "one member is not a threshold");

    accept_one(c, fulfill_tx(com.keys[1], request_id, result, 1), "and the second");
    got = svc.decrypt(hex_of(request_id));
    check(got && got->status == "completed", "which completes it");
    check(got && got->result_handle == hex_of(result), "on the result the committee agreed");
    check(got && got->completed_at != 0, "and records when");

    refused(svc.decrypt(hex_of(digest_of("absent"))), Err::RequestNotFound,
            "a request nobody made");
}

void the_one_mutating_call_takes_a_signed_transaction() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);
    Service svc(*c.vm);

    Transaction tx = register_tx(k, kTestScheme, digest_of("submitted"), 1);
    auto id = svc.submit_transaction(view(tx.bytes()));
    accepted(id, "a client-signed transaction is taken");
    check(id && *id == tx.id(), "and answers with its id");

    Transaction next = register_tx(k, kTestScheme, digest_of("prefixed"), 2);
    accepted(svc.submit_transaction(view(next.bytes())), "and so is the next");

    accept_queued(c, "both go out");
    check(c.vm->ciphertext(tx.subject) != nullptr, "and take effect");

    refused(svc.submit_transaction(view(Bytes{0xde, 0xad, 0xbe, 0xef})), Err::InvalidPayload,
            "bytes that are not a transaction");
    refused(svc.submit_transaction(view(tx.bytes())), Err::BadNonce,
            "and a well-formed one the chain refuses");
}

void the_account_surface_reports_what_consensus_did() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);
    Service svc(*c.vm);

    Transaction tx = register_tx(k, kTestScheme, digest_of("charged"), 1);
    auto spent = fee_for(tx);
    accept_one(c, tx, "an operation");

    auto bal = svc.balance(k.hex_addr());
    accepted(bal, "the balance reads");
    check(bal && spent && bal->balance_nlux == kTestFund - *spent, "debited by the metered fee");
    check(bal && spent && bal->burned_nlux == *spent, "which was burned");

    refused(svc.balance("nothex"), Err::InvalidPayload, "an address that is not hex");
    refused(svc.balance("0011"), Err::InvalidPayload, "and one of the wrong width");

    auto h = svc.health();
    accepted(h, "health answers");
    check(h && h->healthy, "and the chain is healthy");
    check(h && h->details["ciphertexts"] == "1", "with one ciphertext");
    check(h && h->details["height"] == "1", "at height one");
}

void the_fee_schedule_lets_a_client_compute_the_burn() {
    Committee com = new_committee(1);
    Chain c = new_test_vm({}, com.members, 1);
    Service svc(*c.vm);

    FeeSchedule s = svc.fee_schedule();
    check_eq(s.gas_price, kGasPrice, "the schedule names the chain's gas price");

    std::map<std::string, int> seen;
    bool priced = true;
    for (const auto& e : s.entries) {
        ++seen[e.operation];
        if (e.fee_nlux != e.gas * kGasPrice) priced = false;
        if (e.fee_nlux < min_scheduled_fee()) priced = false;
    }
    check(priced, "every entry's fee is its gas at the chain's price, and meets the floor");

    for (std::uint8_t op : {kTxRegisterCiphertext, kTxGrantPermit, kTxRevokePermit,
                            kTxRequestDecrypt, kTxFulfillDecrypt, kTxAdvanceEpoch}) {
        std::string name(Service::operation_name(op));
        int want = uses_scheme(op) ? int(schemes().size()) : 1;
        check_eq(std::uint64_t(seen[name]), std::uint64_t(want),
                 name + " is listed once per scheme it can be priced under");
    }
}

void a_malformed_identifier_is_refused() {
    // The 32-byte identifier decoder fails closed on anything that is not
    // exactly 32 bytes of hex, rather than padding or truncating into a
    // valid-looking lookup.
    Committee com = new_committee(1);
    Chain c = new_test_vm({}, com.members, 1);
    Service svc(*c.vm);

    for (const std::string& bad : {std::string(""), std::string("zz"), std::string("00"),
                                   hex(view(Bytes(31, 0))), hex(view(Bytes(33, 0)))}) {
        check(!svc.ciphertext(bad).has_value(), "a handle of \"" + bad + "\"");
        check(!svc.permit(bad).has_value(), "a permit id of \"" + bad + "\"");
        check(!svc.decrypt(bad).has_value(), "a request id of \"" + bad + "\"");
    }
}

void the_surface_offers_no_way_to_decrypt() {
    // Decryption is a consensus transaction answered by the committee, never a
    // synchronous call. The one decrypt-named read hands back the REQUEST
    // record, which carries a result HANDLE and never a plaintext.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Committee com = new_committee(1);
    std::vector<TestKey> funded = com.keys;
    funded.insert(funded.end(), {owner, grantee});
    Chain c = new_test_vm(fund_all(funded), com.members, 1);
    Service svc(*c.vm);

    Seed s = seed_permit(c, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(c, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1), "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);
    Id result = digest_of("the-answer");
    accept_one(c, fulfill_tx(com.keys[0], request_id, result, 1), "and the answer");

    auto got = svc.decrypt(hex_of(request_id));
    accepted(got, "the answered request reads back");
    check(got && got->result_handle == hex_of(result),
          "carrying the HANDLE the committee agreed on");
    check(got && got->result_handle.size() == 64,
          "which is thirty-two bytes, not a plaintext of unknown size");
}

}  // namespace

int main() {
    std::printf("fhevm — the read surface, and what it will not answer\n\n");
    the_public_parameters_are_the_runtimes();
    the_committee_is_readable_per_epoch();
    a_ciphertext_reads_back_with_its_digest();
    a_permit_reads_back_with_its_capabilities();
    a_request_shows_how_far_the_committee_has_got();
    the_one_mutating_call_takes_a_signed_transaction();
    the_account_surface_reports_what_consensus_did();
    the_fee_schedule_lets_a_client_compute_the_burn();
    a_malformed_identifier_is_refused();
    the_surface_offers_no_way_to_decrypt();
    return report("service");
}
