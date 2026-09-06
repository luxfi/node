// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// records_test.cpp — what the chain persists, the derivations that name it, and
// the threshold arithmetic every decision on F reduces to.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/records.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

void identifiers_are_deterministic() {
    // Every id F derives comes from signed transaction fields alone, so two
    // validators applying one block agree — and changing any input changes it.
    Id d = digest_of("determinism");
    Account a = new_test_key().addr;
    Account b = new_test_key().addr;

    check_eq(hex_of(derive_handle(d, kTestScheme)), hex_of(derive_handle(d, kTestScheme)),
             "a handle is a function of its inputs");
    check(derive_handle(d, kTestScheme) != derive_handle(d, "bfv-n13"), "the scheme binds");
    check(derive_handle(d, kTestScheme) != derive_handle(digest_of("other"), kTestScheme),
          "the digest binds");

    Id h = derive_handle(d, kTestScheme);
    check_eq(hex_of(derive_permit_id(h, a, b, 1, 0, 1)), hex_of(derive_permit_id(h, a, b, 1, 0, 1)),
             "a permit id is a function of its inputs");
    check(derive_permit_id(h, a, b, 1, 0, 1) != derive_permit_id(h, a, b, 1, 0, 2),
          "a grantor may re-grant the same capability without colliding");
    check(derive_permit_id(h, a, b, 1, 0, 1) != derive_permit_id(h, b, a, 1, 0, 1),
          "and grantor and grantee are not interchangeable");
    check(derive_permit_id(h, a, b, 1, 0, 1) != derive_permit_id(h, a, b, 2, 0, 1),
          "the capability bits bind");
    check(derive_permit_id(h, a, b, 1, 0, 1) != derive_permit_id(h, a, b, 1, 5, 1),
          "and so does the expiry");

    check_eq(hex_of(derive_request_id(h, a, 3)), hex_of(derive_request_id(h, a, 3)),
             "a request id is a function of its inputs");
    check(derive_request_id(h, a, 3) != derive_request_id(h, a, 4), "the nonce binds");
    check(derive_request_id(h, a, 3) != derive_request_id(h, b, 3), "and the requester binds");
}

void the_committee_digest_is_semantic() {
    // The value members attest describes the PROPOSAL, not the bytes one client
    // happened to encode: it changes with every meaningful field and with the
    // members themselves.
    Committee c = new_committee(3);
    Bytes pk = network_public_key();
    Id base = committee_digest(1, 2, view(pk), c.members);

    check_eq(hex_of(committee_digest(1, 2, view(pk), c.members)), hex_of(base),
             "the digest is a function of the proposal");
    check(committee_digest(2, 2, view(pk), c.members) != base, "the epoch binds");
    check(committee_digest(1, 3, view(pk), c.members) != base, "the threshold binds");
    Bytes other{'o', 't', 'h', 'e', 'r'};
    check(committee_digest(1, 2, view(other), c.members) != base, "the network key binds");

    std::vector<CommitteeMember> fewer(c.members.begin(), c.members.begin() + 2);
    check(committee_digest(1, 2, view(pk), fewer) != base, "membership binds");

    // Length-prefixing means a member's fields cannot be re-split across the
    // boundary to forge a colliding digest.
    std::vector<CommitteeMember> shifted = c.members;
    shifted[0].public_key.insert(shifted[0].public_key.end(), c.members[1].public_key.begin(),
                                 c.members[1].public_key.end());
    check(committee_digest(1, 2, view(pk), shifted) != base,
          "concatenated fields cannot be re-split to collide");
}

void tally_counts_distinct_members() {
    // A threshold decision counts MEMBERS, not messages: one member repeating
    // itself never raises a count, and votes for different values are counted
    // apart.
    Account a = new_test_key().addr;
    Account b = new_test_key().addr;
    Id x{1};
    Id y{2};

    std::vector<Attestation> as;
    vote(as, a, x);
    vote(as, a, x);
    check(as.size() == 1, "a repeated member holds one entry");
    check_eq(std::uint64_t(tally(as, x)), 1, "and counts once");

    vote(as, b, x);
    check_eq(std::uint64_t(tally(as, x)), 2, "two members are two votes");
    check_eq(std::uint64_t(tally(as, y)), 0, "votes for another value do not count");

    // Moving a vote moves it: the old value keeps nothing.
    vote(as, a, y);
    check(as.size() == 2, "still one entry per member");
    check_eq(std::uint64_t(tally(as, x)), 1, "the value it left keeps only the other member");
    check_eq(std::uint64_t(tally(as, y)), 1, "and the value it moved to gains one");

    // The count is over DISTINCT members whatever the list holds, so a record
    // that somehow carried a duplicate could not inflate a threshold.
    std::vector<Attestation> doubled{{a, x}, {a, x}};
    check_eq(std::uint64_t(tally(doubled, x)), 1, "a duplicated entry counts once");
}

void membership_is_by_voting_identity() {
    // A member's F-Chain account is derived from its PUBLIC signing key by
    // exactly the derivation that authenticates a payer, so committee
    // membership and payer identity cannot disagree.
    Committee c = new_committee(3);
    EpochRecord rec;
    rec.committee = c.members;
    for (std::size_t i = 0; i < c.keys.size(); ++i) {
        check(rec.member_of(c.keys[i].addr),
              "member " + std::to_string(i) + " is recognised by its payer address");
    }
    check(!rec.member_of(new_test_key().addr), "a stranger is not a member");
}

void canonical_order() {
    Committee c = new_committee(3);
    check(committee_order(c.members), "a committee arrives in ascending node-id order");
    std::vector<CommitteeMember> shuffled{c.members[2], c.members[0], c.members[1]};
    check(!committee_order(shuffled), "and any other order is not canonical");
    check(committee_order({}), "the empty committee is trivially ordered");
}

void records_survive_their_own_round_trip() {
    // Whatever the writer emits, the reader takes back — for every record, so
    // the caches a node rebuilds on boot are the records it wrote.
    Committee c = new_committee(2);

    CiphertextRecord ct;
    ct.handle = digest_of("h");
    ct.owner = c.keys[0].addr;
    ct.type = 9;
    ct.level = -1;
    ct.epoch = 3;
    ct.registered_at = 1700;
    ct.size = 4096;
    ct.chain_id = test_chain_id();
    ct.scheme = "ckks-n15";
    ct.digest = digest_of("d");
    CiphertextRecord ct_back;
    std::string err;
    check(unmarshal(marshal(ct), &ct_back, &err) && ct_back == ct,
          "a ciphertext record round-trips");

    PermitRecord pm;
    pm.permit_id = digest_of("p");
    pm.handle = ct.handle;
    pm.grantee = c.keys[1].addr;
    pm.grantor = c.keys[0].addr;
    pm.operations = kPermitOpMask;
    pm.expiry = 99;
    pm.created_at = 1700;
    pm.attestation = Bytes{1, 2, 3};
    pm.chain_id = test_chain_id();
    pm.status = std::string(kStatusRevoked);
    PermitRecord pm_back;
    check(unmarshal(marshal(pm), &pm_back, &err) && pm_back == pm,
          "a permit record round-trips, its attestation included");

    DecryptRecord dr;
    dr.request_id = digest_of("r");
    dr.ciphertext_handle = ct.handle;
    dr.requester = c.keys[1].addr;
    dr.callback[0] = 0xca;
    dr.callback_selector = {1, 2, 3, 4};
    dr.source_chain = test_chain_id();
    dr.epoch = 3;
    dr.nonce = 7;
    dr.expiry = 1800;
    dr.status = RequestStatus::Completed;
    dr.created_at = 1700;
    dr.completed_at = 1750;
    dr.result_handle = digest_of("result");
    dr.error = "none";
    dr.permit_id = pm.permit_id;
    dr.attestations.push_back(Attestation{c.keys[0].addr, dr.result_handle});
    DecryptRecord dr_back;
    check(unmarshal(marshal(dr), &dr_back, &err) && dr_back == dr,
          "a decrypt record round-trips, attestations included");

    EpochRecord ep;
    ep.epoch = 3;
    ep.start_time = 1700;
    ep.end_time = 1800;
    ep.committee = c.members;
    ep.committee_nil = false;
    ep.threshold = 2;
    ep.public_key = network_public_key();
    ep.public_key_nil = false;
    ep.status = EpochStatus::Ended;
    ep.attestations.push_back(Attestation{c.keys[1].addr, digest_of("next")});
    EpochRecord ep_back;
    bool ok = unmarshal(marshal(ep), &ep_back, &err);
    check(ok && ep_back.epoch == ep.epoch && ep_back.end_time == ep.end_time &&
              ep_back.committee == ep.committee && ep_back.public_key == ep.public_key &&
              ep_back.status == ep.status && ep_back.attestations == ep.attestations,
          "an epoch record round-trips, committee and votes included");
}

void omitempty_is_gos_omitempty() {
    // Go's `omitempty` drops an empty SLICE and a zero number — and never drops
    // an ARRAY, which has a fixed length and is therefore never empty. A port
    // that dropped result_handle when it was all zeros would write a different
    // record from the Go chain for every pending request there has ever been.
    PermitRecord pm;
    pm.status = std::string(kStatusActive);
    std::string s = marshal(pm);
    check(s.find("\"attestation\"") == std::string::npos,
          "an empty attestation slice is omitted");

    DecryptRecord dr;
    check(marshal(dr).find("\"completed_at\"") == std::string::npos,
          "a zero completed_at is omitted");
    check(marshal(dr).find("\"error\"") == std::string::npos, "and an empty error is omitted");
    check(marshal(dr).find("\"result_handle\":[0,0,0") != std::string::npos,
          "but an all-zero result handle is written, because a Go array is never empty");

    dr.completed_at = 5;
    dr.error = "why";
    check(marshal(dr).find("\"completed_at\":5") != std::string::npos,
          "and both appear once they are set");
    check(marshal(dr).find("\"error\":\"why\"") != std::string::npos, "including the error");

    EpochRecord ep;
    check(marshal(ep).find("\"end_time\"") == std::string::npos,
          "an epoch that has not ended omits its end time");
}

void a_record_the_schema_does_not_describe_is_refused() {
    // The same rule payloads follow: what a node reads back is what it wrote,
    // and a row carrying anything else is refused rather than partly loaded.
    CiphertextRecord back;
    std::string err;
    std::string doc = marshal(CiphertextRecord{});
    doc.insert(doc.size() - 1, ",\"body\":\"AAAA\"");
    check(!unmarshal(doc, &back, &err), "a record with a member the schema lacks is refused");
    check(!unmarshal("{not json", &back, &err), "and so is one that does not parse");
    check(!unmarshal(marshal(CiphertextRecord{}) + "{}", &back, &err),
          "and one with a second document after it");
}

void the_persisted_schema_is_pinned() {
    // The C++ analogue of the Go package's reflective schema pin. Go walks the
    // struct; here the marshaller IS the schema — every field a record persists
    // appears as a key, in order — so the key list is pinned instead, and a
    // field added, removed or renamed fails until someone writes it down.
    //
    // What this pin catches is what the Go one catches: a change to what F
    // STORES. A field added to a record and not marshalled would slip past it,
    // which is why sizeof is pinned beside it — the two together mean a silent
    // addition cannot happen either way.
    auto keys = [](const std::string& doc) {
        std::string out;
        std::size_t i = 0;
        int depth = 0;
        bool in_string = false;
        std::string current;
        // A flat scan is enough: the only nested objects a record holds are its
        // committee members and its attestations, and those are pinned by their
        // own records below.
        for (; i < doc.size(); ++i) {
            char c = doc[i];
            if (in_string) {
                if (c == '\\') {
                    ++i;
                    continue;
                }
                if (c == '"') {
                    in_string = false;
                    if (depth == 1 && i + 1 < doc.size() && doc[i + 1] == ':') {
                        if (!out.empty()) out += ",";
                        out += current;
                    }
                    current.clear();
                    continue;
                }
                current.push_back(c);
                continue;
            }
            if (c == '"') {
                in_string = true;
                continue;
            }
            if (c == '{' || c == '[') ++depth;
            if (c == '}' || c == ']') --depth;
        }
        return out;
    };

    // A record with every optional field SET, so the pin covers the whole
    // schema rather than only the fields a zero value happens to emit.
    PermitRecord pm;
    pm.attestation = Bytes{1};
    DecryptRecord dr;
    dr.completed_at = 1;
    dr.error = "e";
    EpochRecord ep;
    ep.end_time = 1;

    check_eq(keys(marshal(CiphertextRecord{})),
             "handle,owner,type,level,epoch,registered_at,size,chain_id,scheme,digest",
             "CiphertextRecord stores what it was written down as storing");
    check_eq(keys(marshal(pm)),
             "permit_id,handle,grantee,grantor,operations,expiry,created_at,attestation,chain_id,"
             "status",
             "PermitRecord stores what it was written down as storing");
    check_eq(keys(marshal(dr)),
             "request_id,ciphertext_handle,requester,callback,callback_selector,source_chain,epoch,"
             "nonce,expiry,status,created_at,completed_at,result_handle,error,permitId,attestations",
             "DecryptRecord stores what it was written down as storing");
    check_eq(keys(marshal(ep)),
             "epoch,start_time,end_time,committee,threshold,public_key,status,attestations",
             "EpochRecord stores what it was written down as storing");

    // And the sizes, which catch a field that was added and NOT marshalled —
    // the one way the key list above could be fooled. If one of these fails,
    // the question to answer is the one the Go pin exists to force: should F be
    // storing that?
    check_eq(sizeof(CiphertextRecord), std::size_t(184), "CiphertextRecord holds no more than it declares");
    check_eq(sizeof(PermitRecord), std::size_t(216), "PermitRecord holds no more than it declares");
    check_eq(sizeof(DecryptRecord), std::size_t(312), "DecryptRecord holds no more than it declares");
    check_eq(sizeof(EpochRecord), std::size_t(120), "EpochRecord holds no more than it declares");
}

}  // namespace

int main() {
    std::printf("fhevm — the records, the derivations, and the threshold arithmetic\n\n");
    identifiers_are_deterministic();
    the_committee_digest_is_semantic();
    tally_counts_distinct_members();
    membership_is_by_voting_identity();
    canonical_order();
    records_survive_their_own_round_trip();
    omitempty_is_gos_omitempty();
    a_record_the_schema_does_not_describe_is_refused();
    the_persisted_schema_is_pinned();
    return report("records");
}
