// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// tx_test.cpp — every structural rule a transaction has to satisfy, and every
// way one can fail to.
//
// Ported case for case from the Go F-Chain's tx_test.go. Each case is a
// well-formed transaction with exactly one thing wrong, so a rule that stopped
// being enforced fails here instead of shipping.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/fhevm/auth.hpp"
#include "lux/fhevm/transaction.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

Bytes doc(const std::string& s) { return Bytes(s.begin(), s.end()); }

Transaction tx_with(std::uint8_t type, std::string_view scheme, const Id& subject,
                    std::uint64_t nonce, const std::string& payload) {
    Transaction tx;
    tx.type = type;
    tx.scheme = std::string(scheme);
    tx.subject = subject;
    tx.nonce = nonce;
    tx.payload = doc(payload);
    return tx;
}

void a_well_formed_transaction_is_accepted() {
    TestKey k = new_test_key();
    accepted(register_tx(k, kTestScheme, digest_of("ok"), 1).syntactic_verify(),
             "the baseline every rejection below is measured against");
}

void every_structural_rule_refuses() {
    TestKey k = new_test_key();
    Id digest = digest_of("subject");
    Id handle = derive_handle(digest, kTestScheme);

    auto reg = [&](const RegisterPayload& p) { return marshal(p); };
    RegisterPayload sound;
    sound.digest = digest;
    sound.type = 4;
    sound.level = 3;
    sound.size = 4096;

    {
        Transaction t;
        t.type = 99;
        t.nonce = 1;
        refused(t.syntactic_verify(), Err::InvalidTxType, "an unknown transaction type");
    }
    {
        RegisterPayload p;
        p.digest = digest;
        p.size = 1;
        refused(tx_with(kTxRegisterCiphertext, "paillier", kEmptyId, 1, reg(p)).syntactic_verify(),
                Err::UnknownScheme, "an unknown scheme");
    }
    {
        RegisterPayload p;
        p.digest = digest;
        p.size = 1;
        refused(tx_with(kTxRegisterCiphertext, kTestScheme, handle, 0, reg(p)).syntactic_verify(),
                Err::BadNonce, "a nonce below the first one");
    }
    refused(tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1, "not json").syntactic_verify(),
            Err::InvalidPayload, "an undecodable payload");
    {
        RegisterPayload p;
        p.size = 1;
        refused(tx_with(kTxRegisterCiphertext, kTestScheme,
                        derive_handle(kEmptyId, kTestScheme), 1, reg(p))
                    .syntactic_verify(),
                Err::InvalidPayload, "a registration with no digest");
    }
    {
        RegisterPayload p;
        p.digest = digest;
        refused(tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1, reg(p)).syntactic_verify(),
                Err::InvalidPayload, "a registration with zero size");
    }
    {
        RegisterPayload p = sound;
        p.size = kMaxCiphertextSize;
        accepted(tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1, reg(p)).syntactic_verify(),
                 "a size exactly at the bound is servable");
        // One more than the bound describes nothing anyone could serve.
        RegisterPayload over = sound;
        over.size = kMaxCiphertextSize + 1;
        refused(tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1, reg(over))
                    .syntactic_verify(),
                Err::InvalidPayload, "a size beyond anything servable");
    }
    {
        RegisterPayload p = sound;
        p.level = -1;
        refused(tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1, reg(p)).syntactic_verify(),
                Err::InvalidPayload, "a negative multiplicative level");
    }
    {
        Id wrong{0xde, 0xad};
        refused(tx_with(kTxRegisterCiphertext, kTestScheme, wrong, 1, reg(sound))
                    .syntactic_verify(),
                Err::HandleMismatch, "a subject that is not the derived handle");
        refused(tx_with(kTxRegisterCiphertext, "bfv-n13", handle, 1, reg(sound)).syntactic_verify(),
                Err::HandleMismatch, "a scheme other than the one the handle names");
    }
    {
        GrantPayload p;
        p.grantee = k.addr;
        p.operations = 0;
        refused(tx_with(kTxGrantPermit, "", handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "a grant conferring nothing");
        p.operations = 1u << 20;
        refused(tx_with(kTxGrantPermit, "", handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "a grant with unknown capability bits");
        p.operations = 1u << 31;
        refused(tx_with(kTxGrantPermit, "", handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "and one with the top bit set");
        p.operations = kPermitOpDecrypt;
        p.expiry = -1;
        refused(tx_with(kTxGrantPermit, "", handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "a grant with a negative expiry");
        p.expiry = 0;
        accepted(tx_with(kTxGrantPermit, "", handle, 1, marshal(p)).syntactic_verify(),
                 "the control: a grant conferring exactly one capability");
        p.operations = kPermitOpMask;
        accepted(tx_with(kTxGrantPermit, "", handle, 1, marshal(p)).syntactic_verify(),
                 "and one conferring every bit the runtime defines");
    }
    refused(tx_with(kTxRevokePermit, "", handle, 1, "[]").syntactic_verify(), Err::InvalidPayload,
            "a revocation whose payload is not an object");
    {
        RequestPayload p;
        refused(tx_with(kTxRequestDecrypt, kTestScheme, handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "a request naming no permit");
        p.permit_id = Id{1};
        p.expiry = -1;
        refused(tx_with(kTxRequestDecrypt, kTestScheme, handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "a request with a negative expiry");
    }
    {
        FulfillPayload p;
        refused(tx_with(kTxFulfillDecrypt, "", handle, 1, marshal(p)).syntactic_verify(),
                Err::InvalidPayload, "a fulfilment carrying no result handle");
    }
    refused(tx_with(kTxAdvanceEpoch, "", kEmptyId, 1, "{").syntactic_verify(),
            Err::InvalidPayload, "an epoch proposal that does not decode");
}

void the_advance_carries_the_whole_committee_check() {
    // An epoch proposal carries the committee check with it, so a committee that
    // could never sign cannot be voted in.
    Committee good = new_committee(3);
    Bytes pk{'n', 'e', 'x', 't'};

    auto advance = [&](const std::vector<CommitteeMember>& c, std::int64_t threshold,
                       const Bytes& key) {
        AdvancePayload p;
        p.epoch = 1;
        p.committee = c;
        p.committee_nil = c.empty();
        p.threshold = threshold;
        p.public_key = key;
        p.public_key_nil = key.empty();
        Transaction tx;
        tx.type = kTxAdvanceEpoch;
        tx.nonce = 1;
        tx.subject = committee_digest(1, threshold, view(key), c);
        tx.payload = doc(marshal(p));
        return tx;
    };

    accepted(advance(good.members, 2, pk).syntactic_verify(), "the control: an installable one");
    refused(advance({}, 1, pk).syntactic_verify(), Err::InvalidCommittee, "an empty committee");
    refused(advance(good.members, 0, pk).syntactic_verify(), Err::InvalidThreshold,
            "a threshold of nobody");
    refused(advance(good.members, 4, pk).syntactic_verify(), Err::InvalidThreshold,
            "a threshold beyond the committee");
    refused(advance(good.members, 2, {}).syntactic_verify(), Err::InvalidCommittee,
            "no network public key");

    // Out of canonical order: two members proposing the same set would hash it
    // differently and never agree, so the order is part of the value.
    std::vector<CommitteeMember> shuffled = good.members;
    std::swap(shuffled[0], shuffled[2]);
    refused(advance(shuffled, 2, pk).syntactic_verify(), Err::InvalidCommittee,
            "members out of canonical order");

    // A member whose key cannot be parsed could never attest — installing it
    // would wedge the chain, because a committee that cannot speak also cannot
    // be replaced.
    std::vector<CommitteeMember> unusable = good.members;
    constexpr std::string_view junk = "not-an-mldsa-65-key";
    unusable[1].public_key.assign(junk.begin(), junk.end());
    refused(advance(unusable, 2, pk).syntactic_verify(), Err::InvalidCommittee,
            "a member whose key could never sign");

    // A proposal whose subject does not hash its own contents is refused, so
    // the signature always covers the committee actually being voted for.
    Transaction mismatched = advance(good.members, 2, pk);
    mismatched.subject = Id{0xff};
    refused(mismatched.syntactic_verify(), Err::HandleMismatch,
            "a subject that is not the proposal it carries");
}

void a_committee_is_bounded_and_deduplicated() {
    Bytes pk{'k'};
    Committee big = new_committee(kMaxCommittee + 1);
    refused(validate_committee(big.members, 2, view(pk)), Err::InvalidCommittee,
            "a committee over the member bound");
    Committee at = new_committee(kMaxCommittee);
    accepted(validate_committee(at.members, 2, view(pk)), "and one exactly at it");

    Committee c = new_committee(3);
    std::vector<CommitteeMember> shuffled{c.members[2], c.members[0], c.members[1]};
    refused(validate_committee(shuffled, 2, view(pk)), Err::InvalidCommittee,
            "members out of order");
    std::vector<CommitteeMember> repeated{c.members[0], c.members[0], c.members[1]};
    refused(validate_committee(repeated, 2, view(pk)), Err::InvalidCommittee,
            "one seat twice is not two seats");

    // A member's VOTING IDENTITY is address_of(public_key). n seats sharing one
    // key passed every check and seated a threshold only one account could ever
    // vote toward — so no decryption could complete and the committee could
    // never rotate itself out, while the chain reported itself fully seated.
    TestKey one = new_test_key();
    std::vector<CommitteeMember> shared(3);
    for (std::size_t i = 0; i < shared.size(); ++i) {
        shared[i].node_id[0] = std::uint8_t(i + 1);
        shared[i].public_key = one.pub;
        shared[i].public_key_nil = false;
        shared[i].weight = 1;
        shared[i].index = std::int64_t(i);
    }
    refused(validate_committee(shared, 3, view(pk)), Err::InvalidCommittee,
            "three seats and one voter is not a three-of-three committee");

    accepted(validate_committee(c.members, 2, view(pk)), "the control");
}

void authentication_refuses_everything_but_a_genuine_signature() {
    TestKey k = new_test_key();
    TestKey other = new_test_key();
    Id chain = test_chain_id();

    accepted(register_tx(k, kTestScheme, digest_of("auth"), 1).authenticate(chain),
             "a genuine signature by the claimed identity");

    Transaction unsigned_tx = register_tx(k, kTestScheme, digest_of("auth"), 1);
    unsigned_tx.auth.clear();
    unsigned_tx.sig.clear();
    refused(unsigned_tx.authenticate(chain), Err::UnsignedTx, "an unsigned transaction");

    Transaction tampered = register_tx(k, kTestScheme, digest_of("auth"), 1);
    tampered.nonce = 7;
    refused(tampered.authenticate(chain), Err::BadSignature, "a tampered one");

    // Signed by `other` but claiming k's address: the address is derived from
    // the attached public key, so the two cannot be made to agree.
    Transaction impersonating = register_tx(k, kTestScheme, digest_of("auth"), 1);
    other.sign(impersonating);
    refused(impersonating.authenticate(chain), Err::PayerMismatch, "an impersonating one");

    // Auth bytes that are not a public key at all.
    Transaction garbage = register_tx(k, kTestScheme, digest_of("auth"), 1);
    constexpr std::string_view junk = "garbage";
    garbage.auth.assign(junk.begin(), junk.end());
    garbage.payer = address_of(view(garbage.auth));
    check(!garbage.authenticate(chain).has_value(), "auth bytes that are not a key at all");

    // A single flipped bit in the signature.
    Transaction forged = register_tx(k, kTestScheme, digest_of("forged"), 1);
    forged.sig[0] ^= 0xff;
    refused(forged.authenticate(chain), Err::BadSignature, "a single flipped signature bit");

    // Another chain's signature does not authenticate here, and this chain's
    // does not authenticate there.
    Id away{'a', 'w', 'a', 'y'};
    Transaction foreign = register_tx(k, kTestScheme, digest_of("elsewhere"), 1);
    k.sign_for(foreign, away);
    refused(foreign.authenticate(chain), Err::BadSignature,
            "a transaction signed for another chain");
    accepted(foreign.authenticate(away), "and it works on the chain it was signed for");
}

void the_id_is_the_content_hash() {
    TestKey k = new_test_key();
    Transaction tx = register_tx(k, kTestScheme, digest_of("id"), 1);
    Id first = tx.id();
    check(first != kEmptyId, "a transaction names itself");
    check_eq(hex_of(tx.id()), hex_of(first), "and the name is stable once computed");
    check_eq(hex_of(first), hex_of(sha256(view(tx.bytes()))), "it is the hash of the wire");

    Transaction other = register_tx(k, kTestScheme, digest_of("id"), 2);
    check(other.id() != first, "and it changes with any field");
}

void the_effect_names_the_right_thing() {
    // The in-flight uniqueness key: two votes by one member on one decision
    // collide, votes by different members do not, and two grants by one grantor
    // do not.
    TestKey k = new_test_key();
    TestKey other = new_test_key();
    Id req{7};

    Transaction v1 = fulfill_tx(k, req, Id{1}, 1);
    Transaction v2 = fulfill_tx(k, req, Id{2}, 2);
    check_eq(hex_of(v1.effect()), hex_of(v2.effect()),
             "one member cannot have two attestations to one request in flight");

    Transaction v3 = fulfill_tx(other, req, Id{1}, 1);
    check(v3.effect() != v1.effect(), "a different member is a different effect");

    Transaction r1 = register_tx(k, kTestScheme, digest_of("one"), 1);
    Transaction r2 = register_tx(k, kTestScheme, digest_of("two"), 2);
    Transaction r1_again = register_tx(other, kTestScheme, digest_of("one"), 1);
    check(r1.effect() != r2.effect(), "two ciphertexts are two effects");
    check_eq(hex_of(r1.effect()), hex_of(r1_again.effect()),
             "and a handle is the effect, whoever claims it");

    Id handle = derive_handle(digest_of("g"), kTestScheme);
    Transaction g1 = grant_tx(k, handle, other.addr, kPermitOpDecrypt, 0, 1);
    Transaction g2 = grant_tx(k, handle, other.addr, kPermitOpDecrypt, 0, 2);
    check(g1.effect() != g2.effect(),
          "two grants over one handle are distinct — the nonce names the permits");

    // An epoch advance is a vote on the DECISION, and exactly one epoch is ever
    // open, so one member's two votes are one effect however they differ. The
    // proposal must not enter the effect: if it did, a member could vote twice,
    // both votes would pass verify against committed state, and acceptance would
    // apply one and refuse the other — a block every validator certifies and no
    // validator can apply.
    Committee a = new_committee(3);
    Committee b = new_committee(3);
    Bytes pk{'k'};
    Transaction a1 = advance_tx(k, 1, a.members, 2, pk, 1);
    Transaction a2 = advance_tx(k, 1, b.members, 2, pk, 2);
    check(a1.subject != a2.subject, "two genuinely different proposals");
    check_eq(hex_of(a1.effect()), hex_of(a2.effect()), "but one member, one vote");

    Transaction a3 = advance_tx(other, 1, a.members, 2, pk, 1);
    check(a3.effect() != a1.effect(), "a different member voting is a different effect");
    check(a1.effect() != v1.effect(), "and a vote never collides with an attestation");

    // A revocation is named by the permit alone: one withdrawal per permit,
    // whoever asks for it.
    Transaction rv1 = revoke_tx(k, handle, 1);
    Transaction rv2 = revoke_tx(other, handle, 5);
    check_eq(hex_of(rv1.effect()), hex_of(rv2.effect()),
             "one withdrawal per permit, whoever asks");
}

void unauthenticated_work_is_bounded_by_size() {
    // What an UNAUTHENTICATED transaction can make this node do before its
    // signature is checked must be bounded by its own declared size.
    TestKey k = new_test_key();

    // The costly branch: an epoch proposal makes syntactic verification parse a
    // public key per member, and that runs before authentication. The member
    // bound is what stops it.
    Committee over = new_committee(kMaxCommittee + 1);
    AdvancePayload p;
    p.epoch = 1;
    p.committee = over.members;
    p.committee_nil = false;
    p.threshold = 2;
    p.public_key = Bytes{'p', 'k'};
    p.public_key_nil = false;
    Transaction huge;
    huge.type = kTxAdvanceEpoch;
    huge.nonce = 1;
    huge.subject = committee_digest(1, 2, view(p.public_key), over.members);
    huge.payload = doc(marshal(p));
    refused(huge.syntactic_verify(), Err::InvalidCommittee,
            "a committee is refused by COUNT, before any of its keys is parsed");

    std::string revoke = marshal(RevokePayload{});
    {
        Transaction t;
        t.type = kTxRevokePermit;
        t.nonce = 1;
        t.payload.assign(kMaxPayload + 1, 'A');
        refused(t.syntactic_verify(), Err::InvalidPayload, "a payload over the bound");
    }
    {
        Transaction t = tx_with(kTxRevokePermit, std::string(kMaxScheme + 1, 'A'), kEmptyId, 1,
                                revoke);
        refused(t.syntactic_verify(), Err::InvalidPayload, "a scheme over the bound");
    }
    {
        Transaction t = tx_with(kTxRevokePermit, "", kEmptyId, 1, revoke);
        t.auth.assign(9, 'A');
        refused(t.syntactic_verify(), Err::InvalidPayload, "auth of the wrong width");
        t.auth.clear();
        t.sig.assign(9, 'A');
        refused(t.syntactic_verify(), Err::InvalidPayload, "a signature of the wrong width");
    }
    {
        // The control: exactly at each bound is accepted, so what was refused is
        // the size and nothing else.
        Transaction t = tx_with(kTxRevokePermit, std::string(kMaxScheme, 'A'), kEmptyId, 1, revoke);
        accepted(t.syntactic_verify(), "a scheme exactly at the bound");
        t.scheme.clear();
        t.auth.assign(auth::kPublicKeySize, 'A');
        t.sig.assign(auth::kSignatureSize, 'A');
        accepted(t.syntactic_verify(), "and auth and signature at their fixed widths");
    }

    // A payload that decodes to exactly the schema is refused for its SIZE
    // alone: whitespace is not content, so nothing but the bound can refuse it.
    Id digest = digest_of("smuggled");
    RegisterPayload r;
    r.digest = digest;
    r.type = 4;
    r.level = 3;
    r.size = 4096;
    std::string padded = marshal(r) + std::string(kMaxPayload, ' ');
    Transaction fat = tx_with(kTxRegisterCiphertext, kTestScheme,
                              derive_handle(digest, kTestScheme), 1, padded);
    k.sign(fat);
    refused(fat.syntactic_verify(), Err::InvalidPayload, "bulk is bounded whatever shape it claims");
    Transaction slim = tx_with(kTxRegisterCiphertext, kTestScheme,
                               derive_handle(digest, kTestScheme), 1, marshal(r));
    accepted(slim.syntactic_verify(), "the control: the same value, unpadded");
}

void a_superset_of_the_schema_is_not_a_payload() {
    // The package claimed it holds no ciphertext body "structurally, not as a
    // matter of discipline". It was discipline: encoding/json ignored members it
    // did not know, and acceptance persists the transaction verbatim — so a
    // megabyte of body rode onto the chain inside a register payload and came
    // back out of the block store.
    TestKey k = new_test_key();
    Id digest = digest_of("smuggled");
    Id handle = derive_handle(digest, kTestScheme);

    std::string fat = R"({"digest":[)";
    for (std::size_t i = 0; i < digest.size(); ++i) {
        fat += (i ? "," : "") + std::to_string(unsigned(digest[i]));
    }
    fat += R"(],"type":4,"level":3,"size":4096,"body":")" + base64(view(Bytes(1024, 'X'))) + R"("})";
    Transaction smuggle = tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1, fat);
    k.sign(smuggle);
    refused(smuggle.syntactic_verify(), Err::InvalidPayload,
            "a payload the schema does not describe is not a payload");

    RegisterPayload sound;
    sound.digest = digest;
    sound.type = 4;
    sound.level = 3;
    sound.size = 4096;
    Transaction trailing = tx_with(kTxRegisterCiphertext, kTestScheme, handle, 1,
                                   marshal(sound) + R"({"body":"more"})");
    k.sign(trailing);
    refused(trailing.syntactic_verify(), Err::InvalidPayload,
            "and trailing bytes after a well-formed one are refused too");
}

}  // namespace

int main() {
    std::printf("fhevm — the rules a transaction has to satisfy\n\n");
    a_well_formed_transaction_is_accepted();
    every_structural_rule_refuses();
    the_advance_carries_the_whole_committee_check();
    a_committee_is_bounded_and_deduplicated();
    authentication_refuses_everything_but_a_genuine_signature();
    the_id_is_the_content_hash();
    the_effect_names_the_right_thing();
    unauthenticated_work_is_bounded_by_size();
    a_superset_of_the_schema_is_not_a_payload();
    return report("tx");
}
