// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/transaction.hpp"

#include "lux/fhevm/auth.hpp"
#include "lux/fhevm/gas.hpp"
#include "lux/fhevm/json.hpp"
#include "lux/fhevm/vm.hpp"

#include <set>

namespace lux::fhevm {
namespace {

std::string_view as_string(ByteView b) {
    return std::string_view(reinterpret_cast<const char*>(b.data()), b.size());
}

// one_value is the decode discipline every payload shares: exactly one JSON
// value, and nothing after it.
bool one_value(ByteView payload, json::Value* v, std::string* err) {
    std::size_t consumed = 0;
    if (!json::parse(as_string(payload), v, &consumed, err)) return false;
    if (json::more(as_string(payload), consumed)) {
        *err = "trailing content";
        return false;
    }
    return true;
}

std::unexpected<Error> bad_payload(std::string_view op, const std::string& why) {
    return fail(Err::InvalidPayload, std::string(op) + ": " + why);
}

}  // namespace

// ---- payload encodings --------------------------------------------------------

std::string marshal(const RegisterPayload& p) {
    json::Writer w;
    w.begin_object();
    w.key("digest");
    w.byte_array(view(p.digest));
    w.key("type");
    w.u64(p.type);
    w.key("level");
    w.i64(p.level);
    w.key("size");
    w.u64(p.size);
    w.end_object();
    return w.str();
}

std::string marshal(const GrantPayload& p) {
    json::Writer w;
    w.begin_object();
    w.key("grantee");
    w.string(account_string(p.grantee));
    w.key("operations");
    w.u64(p.operations);
    w.key("expiry");
    w.i64(p.expiry);
    w.end_object();
    return w.str();
}

std::string marshal(const RevokePayload& p) {
    json::Writer w;
    w.begin_object();
    w.key("reason");
    w.string(p.reason);
    w.end_object();
    return w.str();
}

std::string marshal(const RequestPayload& p) {
    json::Writer w;
    w.begin_object();
    w.key("permitId");
    w.byte_array(view(p.permit_id));
    w.key("callback");
    w.byte_array(view(p.callback));
    w.key("selector");
    w.byte_array(view(p.selector));
    w.key("expiry");
    w.i64(p.expiry);
    w.end_object();
    return w.str();
}

std::string marshal(const FulfillPayload& p) {
    json::Writer w;
    w.begin_object();
    w.key("result");
    w.byte_array(view(p.result));
    w.end_object();
    return w.str();
}

std::string marshal(const AdvancePayload& p) {
    json::Writer w;
    w.begin_object();
    w.key("epoch");
    w.u64(p.epoch);
    w.key("committee");
    if (p.committee.empty() && p.committee_nil) {
        w.null();
    } else {
        w.begin_array();
        for (const auto& m : p.committee) write_member(w, m);
        w.end_array();
    }
    w.key("threshold");
    w.i64(p.threshold);
    w.key("publicKey");
    w.bytes(p.public_key, p.public_key_nil);
    w.end_object();
    return w.str();
}

// ---- payload decoders ---------------------------------------------------------

Result<RegisterPayload> decode_register(ByteView payload) {
    json::Value v;
    std::string err;
    if (!one_value(payload, &v, &err)) return bad_payload("register", err);
    json::Reader r(v, {"digest", "type", "level", "size"}, &err);
    if (!r.ok()) return bad_payload("register", err);
    RegisterPayload p;
    std::uint64_t u = 0;
    if (!read_byte_array(r.find("digest"), "digest", p.digest.data(), p.digest.size(), &err)) {
        return bad_payload("register", err);
    }
    if (!read_u64(r.find("type"), "type", 255, &u, &err)) return bad_payload("register", err);
    p.type = std::uint8_t(u);
    if (!read_i64(r.find("level"), "level", &p.level, &err)) return bad_payload("register", err);
    u = 0;
    if (!read_u64(r.find("size"), "size", 0xffffffffu, &u, &err)) return bad_payload("register", err);
    p.size = std::uint32_t(u);
    return p;
}

Result<GrantPayload> decode_grant(ByteView payload) {
    json::Value v;
    std::string err;
    if (!one_value(payload, &v, &err)) return bad_payload("grant", err);
    json::Reader r(v, {"grantee", "operations", "expiry"}, &err);
    if (!r.ok()) return bad_payload("grant", err);
    GrantPayload p;
    if (!read_account(r.find("grantee"), "grantee", &p.grantee, &err)) {
        return bad_payload("grant", err);
    }
    std::uint64_t u = 0;
    if (!read_u64(r.find("operations"), "operations", 0xffffffffu, &u, &err)) {
        return bad_payload("grant", err);
    }
    p.operations = std::uint32_t(u);
    if (!read_i64(r.find("expiry"), "expiry", &p.expiry, &err)) return bad_payload("grant", err);
    return p;
}

Result<RevokePayload> decode_revoke(ByteView payload) {
    json::Value v;
    std::string err;
    if (!one_value(payload, &v, &err)) return bad_payload("revoke", err);
    json::Reader r(v, {"reason"}, &err);
    if (!r.ok()) return bad_payload("revoke", err);
    RevokePayload p;
    if (!read_string(r.find("reason"), "reason", &p.reason, &err)) return bad_payload("revoke", err);
    return p;
}

Result<RequestPayload> decode_request(ByteView payload) {
    json::Value v;
    std::string err;
    if (!one_value(payload, &v, &err)) return bad_payload("request", err);
    json::Reader r(v, {"permitId", "callback", "selector", "expiry"}, &err);
    if (!r.ok()) return bad_payload("request", err);
    RequestPayload p;
    if (!read_byte_array(r.find("permitId"), "permitId", p.permit_id.data(), p.permit_id.size(),
                         &err)) {
        return bad_payload("request", err);
    }
    if (!read_byte_array(r.find("callback"), "callback", p.callback.data(), p.callback.size(),
                         &err)) {
        return bad_payload("request", err);
    }
    if (!read_byte_array(r.find("selector"), "selector", p.selector.data(), p.selector.size(),
                         &err)) {
        return bad_payload("request", err);
    }
    if (!read_i64(r.find("expiry"), "expiry", &p.expiry, &err)) return bad_payload("request", err);
    return p;
}

Result<FulfillPayload> decode_fulfill(ByteView payload) {
    json::Value v;
    std::string err;
    if (!one_value(payload, &v, &err)) return bad_payload("fulfill", err);
    json::Reader r(v, {"result"}, &err);
    if (!r.ok()) return bad_payload("fulfill", err);
    FulfillPayload p;
    if (!read_byte_array(r.find("result"), "result", p.result.data(), p.result.size(), &err)) {
        return bad_payload("fulfill", err);
    }
    return p;
}

Result<AdvancePayload> decode_advance(ByteView payload) {
    json::Value v;
    std::string err;
    if (!one_value(payload, &v, &err)) return bad_payload("advance", err);
    json::Reader r(v, {"epoch", "committee", "threshold", "publicKey"}, &err);
    if (!r.ok()) return bad_payload("advance", err);
    AdvancePayload p;
    if (!read_u64(r.find("epoch"), "epoch", ~std::uint64_t(0), &p.epoch, &err)) {
        return bad_payload("advance", err);
    }
    const json::Value* c = r.find("committee");
    if (c != nullptr && !c->null()) {
        if (c->kind != json::Kind::Array) return bad_payload("advance", "committee is not an array");
        p.committee_nil = false;
        for (const auto& el : c->array) {
            CommitteeMember m;
            if (!read_member(el, &m, &err)) return bad_payload("advance", err);
            p.committee.push_back(std::move(m));
        }
    }
    if (!read_i64(r.find("threshold"), "threshold", &p.threshold, &err)) {
        return bad_payload("advance", err);
    }
    const json::Value* pk = r.find("publicKey");
    if (pk != nullptr && !pk->null()) {
        if (!read_bytes(pk, "publicKey", &p.public_key, &err)) return bad_payload("advance", err);
        p.public_key_nil = false;
    }
    return p;
}

// ---- committee validation -----------------------------------------------------

Result<void> validate_committee(const std::vector<CommitteeMember>& c, std::int64_t threshold,
                                ByteView public_key) {
    if (c.empty()) return fail(Err::InvalidCommittee, "empty committee");
    if (c.size() > kMaxCommittee) {
        return fail(Err::InvalidCommittee, "committee exceeds the member bound");
    }
    if (threshold <= 0 || threshold > std::int64_t(c.size())) {
        return fail(Err::InvalidThreshold, "threshold is not within the committee");
    }
    if (public_key.empty()) return fail(Err::InvalidCommittee, "no network public key");
    if (!committee_order(c)) {
        return fail(Err::InvalidCommittee, "members not in canonical node-ID order");
    }
    // A seat is only a vote if a distinct account can cast it. Membership is
    // tested by address_of(public_key) — the same derivation that authenticates
    // a payer — so that is what must be unique. Deduplicating node id instead
    // let n seats share one key: the committee passed every check, reported
    // itself fully seated, and could never reach its own threshold, for a
    // decryption or for rotating itself out.
    std::set<Account> voters;
    for (std::size_t i = 0; i < c.size(); ++i) {
        const auto& m = c[i];
        if (i > 0 && c[i - 1].node_id == m.node_id) {
            return fail(Err::InvalidCommittee, "duplicate member " + node_id_string(m.node_id));
        }
        if (!auth::public_key_valid(view(m.public_key))) {
            return fail(Err::InvalidCommittee, "member " + node_id_string(m.node_id) + " key");
        }
        if (!voters.insert(address_of(view(m.public_key))).second) {
            return fail(Err::InvalidCommittee, "member " + node_id_string(m.node_id) +
                                                   " shares a voting identity with another seat");
        }
    }
    return {};
}

// ---- syntactic verification ---------------------------------------------------

Result<void> Transaction::syntactic_verify() const {
    switch (type) {
        case kTxRegisterCiphertext:
        case kTxGrantPermit:
        case kTxRevokePermit:
        case kTxRequestDecrypt:
        case kTxFulfillDecrypt:
        case kTxAdvanceEpoch:
            break;
        default:
            return fail(Err::InvalidTxType);
    }
    // Bounds FIRST, before anything is decoded or parsed, so the work an
    // unauthenticated transaction can demand is bounded by its own size.
    if (payload.size() > kMaxPayload) {
        return fail(Err::InvalidPayload, "payload exceeds the payload bound");
    }
    if (scheme.size() > kMaxScheme) {
        return fail(Err::InvalidPayload, "scheme exceeds the name bound");
    }
    // Auth and sig are fixed-width by algorithm. Pinning them here keeps them
    // from becoming a third byte channel the base cost would carry for free.
    if (!auth.empty() && auth.size() != auth::kPublicKeySize) {
        return fail(Err::InvalidPayload, "auth is the wrong width for ML-DSA-65");
    }
    if (!sig.empty() && sig.size() != auth::kSignatureSize) {
        return fail(Err::InvalidPayload, "signature is the wrong width for ML-DSA-65");
    }
    // Pricing also validates scheme membership for scheme-bearing ops.
    auto g = gas_for(*this);
    if (!g) return std::unexpected(g.error());
    if (nonce == 0) return fail(Err::BadNonce, "nonce starts at 1");

    switch (type) {
        case kTxRegisterCiphertext: {
            auto p = decode_register(view(payload));
            if (!p) return std::unexpected(p.error());
            if (p->digest == kEmptyId) {
                return fail(Err::InvalidPayload, "register: empty ciphertext digest");
            }
            if (p->size == 0 || p->size > kMaxCiphertextSize) {
                return fail(Err::InvalidPayload, "register: ciphertext size out of range");
            }
            if (p->level < 0) return fail(Err::InvalidPayload, "register: negative level");
            if (subject != derive_handle(p->digest, scheme)) {
                return fail(Err::HandleMismatch, "handle does not match digest+scheme");
            }
            break;
        }
        case kTxGrantPermit: {
            auto p = decode_grant(view(payload));
            if (!p) return std::unexpected(p.error());
            if (p->operations == 0) {
                return fail(Err::InvalidPayload, "grant confers no operation");
            }
            if ((p->operations & ~kPermitOpMask) != 0) {
                return fail(Err::InvalidPayload, "unknown permit operation bits");
            }
            if (p->expiry < 0) return fail(Err::InvalidPayload, "negative expiry");
            break;
        }
        case kTxRevokePermit: {
            auto p = decode_revoke(view(payload));
            if (!p) return std::unexpected(p.error());
            break;
        }
        case kTxRequestDecrypt: {
            auto p = decode_request(view(payload));
            if (!p) return std::unexpected(p.error());
            if (p->permit_id == kEmptyId) {
                return fail(Err::InvalidPayload, "request names no permit");
            }
            if (p->expiry < 0) return fail(Err::InvalidPayload, "negative expiry");
            break;
        }
        case kTxFulfillDecrypt: {
            auto p = decode_fulfill(view(payload));
            if (!p) return std::unexpected(p.error());
            if (p->result == kEmptyId) {
                return fail(Err::InvalidPayload, "fulfill carries no result handle");
            }
            break;
        }
        case kTxAdvanceEpoch: {
            auto p = decode_advance(view(payload));
            if (!p) return std::unexpected(p.error());
            auto v = validate_committee(p->committee, p->threshold, view(p->public_key));
            if (!v) return v;
            if (subject != committee_digest(p->epoch, p->threshold, view(p->public_key),
                                            p->committee)) {
                return fail(Err::HandleMismatch, "subject does not match the proposed committee");
            }
            break;
        }
        default:
            break;
    }
    return {};
}

Result<void> Transaction::authenticate(const Id& chain) const {
    if (auth.empty() || sig.empty()) return fail(Err::UnsignedTx);
    if (address_of(view(auth)) != payer) return fail(Err::PayerMismatch);
    if (!auth::public_key_valid(view(auth))) {
        return fail(Err::InvalidPayload, "payer public key");
    }
    if (!auth::verify(view(auth), view(signing_bytes(chain)), view(sig))) {
        return fail(Err::BadSignature);
    }
    return {};
}

Id Transaction::effect() const {
    Hasher h;
    std::uint8_t t = type;
    h.write(ByteView(&t, 1));
    switch (type) {
        case kTxAdvanceEpoch:
            // The DECISION, not the proposal. Exactly one epoch is ever open —
            // check_auth refuses any target but the sitting epoch's successor —
            // so a member's vote is named by the member alone. Naming it by
            // subject instead made two votes for two different committees two
            // different effects: both passed admission, both passed verify
            // against committed state, and acceptance applied the first and
            // refused the second. A block every validator certifies and no
            // validator can apply halts the chain at that height, which is the
            // whole thing this function exists to stop.
            h.write(view(payer));
            break;

        case kTxRegisterCiphertext:
        case kTxRevokePermit:
            // The entry is named by subject alone: one registration per handle,
            // one withdrawal per permit, whoever asks for it.
            h.write(view(subject));
            break;

        case kTxFulfillDecrypt:
            // One write per member per request. The voted value lives in the
            // payload, so the effect is the member's vote on THIS request
            // rather than the result it names.
            h.write(view(subject));
            h.write(view(payer));
            break;

        default:
            // A grant and a request CREATE an entry whose id already carries
            // the payer and its nonce, so no two of them collide — the same
            // owner may grant twice over one handle, to different grantees, in
            // one block. Qualifying by the same two fields keeps that true.
            h.write(view(subject));
            h.write(view(payer));
            h.be64(nonce);
            break;
    }
    return h.sum();
}

// ---- authorization ------------------------------------------------------------

Result<void> check_auth(const Transaction& tx, const VM& vm, std::int64_t now) {
    switch (tx.type) {
        case kTxRegisterCiphertext: {
            if (vm.ciphertext(tx.subject) != nullptr) return fail(Err::CiphertextExists);
            return {};
        }

        case kTxGrantPermit: {
            const CiphertextRecord* ct = vm.ciphertext(tx.subject);
            if (ct == nullptr) return fail(Err::CiphertextNotFound);
            // Only the owner may confer a capability over its ciphertext — and,
            // because only the owner grants, only the owner revokes.
            if (ct->owner != tx.payer) return fail(Err::Unauthorized);
            return {};
        }

        case kTxRevokePermit: {
            const PermitRecord* pm = vm.permit(tx.subject);
            if (pm == nullptr) return fail(Err::PermitNotFound);
            if (pm->status != kStatusActive) return fail(Err::PermitRevoked);
            if (pm->grantor != tx.payer) return fail(Err::Unauthorized);
            return {};
        }

        case kTxRequestDecrypt: {
            auto p = decode_request(view(tx.payload));
            if (!p) return std::unexpected(p.error());
            if (vm.ciphertext(tx.subject) == nullptr) return fail(Err::CiphertextNotFound);
            const PermitRecord* pm = vm.permit(p->permit_id);
            if (pm == nullptr) return fail(Err::PermitNotFound);
            if (pm->status != kStatusActive) return fail(Err::PermitRevoked);
            if (pm->handle != tx.subject) {
                return fail(Err::PermitInvalid, "permit is for another handle");
            }
            if (pm->grantee != tx.payer) return fail(Err::Unauthorized);
            if (pm->expiry != 0 && now > pm->expiry) return fail(Err::PermitExpired);
            if ((pm->operations & kPermitOpDecrypt) == 0) {
                return fail(Err::PermitInvalid, "permit does not confer decrypt");
            }
            // The request's id carries the requester's nonce, and a nonce is
            // used once, so a request cannot collide with an existing one. That
            // is the whole uniqueness argument — there is no second check to
            // keep in step with it.
            return {};
        }

        case kTxFulfillDecrypt: {
            const DecryptRecord* req = vm.decrypt(tx.subject);
            if (req == nullptr) return fail(Err::RequestNotFound);
            if (req->status != RequestStatus::Pending) return fail(Err::RequestClosed);
            if (req->expiry != 0 && now > req->expiry) return fail(Err::RequestExpired);
            // The permit that authorized the ask must still authorize it.
            // Revocation is a withdrawal of consent and reaches a request
            // already in flight — otherwise an owner who revoked would watch
            // the committee answer anyway and deliver the plaintext to the
            // callback.
            //
            // Expiry is deliberately NOT re-checked. It bounds when the grantee
            // may ASK; the grantee asked in time, and the committee answering
            // afterwards is not the grantee acting. Re-checking it would make
            // any permit shorter than a decryption round useless.
            const PermitRecord* pm = vm.permit(req->permit_id);
            if (pm == nullptr) return fail(Err::PermitNotFound);
            if (pm->status != kStatusActive) return fail(Err::PermitRevoked);
            // Only the committee of the epoch the request was made in may
            // answer it: a later committee holds different shares and never saw
            // the permit.
            const EpochRecord* ep = vm.epoch(req->epoch);
            if (ep == nullptr) return fail(Err::EpochNotFound);
            if (!ep->member_of(tx.payer)) return fail(Err::NotCommittee);
            return {};
        }

        case kTxAdvanceEpoch: {
            auto p = decode_advance(view(tx.payload));
            if (!p) return std::unexpected(p.error());
            EpochRecord cur = vm.current_epoch();
            if (p->epoch != cur.epoch + 1) {
                return fail(Err::EpochMismatch, "the proposal is not the next epoch");
            }
            // Only the sitting committee decides its successor.
            if (!cur.member_of(tx.payer)) return fail(Err::NotCommittee);
            return {};
        }

        default:
            return fail(Err::InvalidTxType);
    }
}

// ---- application ---------------------------------------------------------------

namespace {

Result<void> apply_register(const Transaction& tx, VM& vm, std::int64_t now) {
    auto p = decode_register(view(tx.payload));
    if (!p) return std::unexpected(p.error());
    CiphertextRecord rec;
    rec.handle = tx.subject;
    rec.owner = tx.payer;
    rec.type = p->type;
    rec.level = p->level;
    rec.epoch = vm.current_epoch().epoch;
    rec.registered_at = now;
    rec.size = p->size;
    rec.chain_id = vm.chain_id();
    rec.scheme = tx.scheme;
    rec.digest = p->digest;
    return vm.put_ciphertext(rec);
}

Result<void> apply_grant(const Transaction& tx, VM& vm, std::int64_t now) {
    auto p = decode_grant(view(tx.payload));
    if (!p) return std::unexpected(p.error());
    PermitRecord rec;
    rec.permit_id = derive_permit_id(tx.subject, tx.payer, p->grantee, p->operations, p->expiry,
                                     tx.nonce);
    rec.handle = tx.subject;
    rec.grantee = p->grantee;
    rec.grantor = tx.payer;
    rec.operations = p->operations;
    rec.expiry = p->expiry;
    rec.created_at = now;
    rec.chain_id = vm.chain_id();
    rec.status = std::string(kStatusActive);
    return vm.put_permit(rec);
}

Result<void> apply_revoke(const Transaction& tx, VM& vm) {
    const PermitRecord* pm = vm.permit(tx.subject);
    if (pm == nullptr) return fail(Err::PermitNotFound);
    PermitRecord next = *pm;
    next.status = std::string(kStatusRevoked);
    return vm.put_permit(next);
}

Result<void> apply_request(const Transaction& tx, VM& vm, std::int64_t now) {
    auto p = decode_request(view(tx.payload));
    if (!p) return std::unexpected(p.error());
    std::int64_t expiry = p->expiry;
    if (expiry == 0) expiry = now + kDefaultRequestWindow;
    DecryptRecord rec;
    rec.request_id = derive_request_id(tx.subject, tx.payer, tx.nonce);
    rec.ciphertext_handle = tx.subject;
    rec.requester = tx.payer;
    rec.callback = p->callback;
    rec.callback_selector = p->selector;
    rec.source_chain = vm.chain_id();
    rec.epoch = vm.current_epoch().epoch;
    rec.nonce = tx.nonce;
    rec.expiry = expiry;
    rec.status = RequestStatus::Pending;
    rec.created_at = now;
    rec.permit_id = p->permit_id;
    return vm.put_decrypt(rec);
}

Result<void> apply_fulfill(const Transaction& tx, VM& vm, std::int64_t now) {
    auto p = decode_fulfill(view(tx.payload));
    if (!p) return std::unexpected(p.error());
    const DecryptRecord* found = vm.decrypt(tx.subject);
    if (found == nullptr) return fail(Err::RequestNotFound);
    DecryptRecord req = *found;
    const EpochRecord* ep = vm.epoch(req.epoch);
    if (ep == nullptr) return fail(Err::EpochNotFound);
    vote(req.attestations, tx.payer, p->result);
    // The request completes the moment a threshold of DISTINCT members have
    // named the same handle. A member that names a different one is counted
    // against that value alone, so it delays nothing and pays for the privilege.
    if (tally(req.attestations, p->result) >= ep->threshold) {
        req.status = RequestStatus::Completed;
        req.result_handle = p->result;
        req.completed_at = now;
    }
    return vm.put_decrypt(req);
}

Result<void> apply_advance(const Transaction& tx, VM& vm, std::int64_t now) {
    auto p = decode_advance(view(tx.payload));
    if (!p) return std::unexpected(p.error());
    EpochRecord cur = vm.current_epoch();
    vote(cur.attestations, tx.payer, tx.subject);
    if (tally(cur.attestations, tx.subject) < cur.threshold) {
        // Not yet decided: record the vote against the sitting epoch and stop.
        return vm.put_epoch(cur);
    }
    // Decided. The transaction that carries the deciding vote also carries the
    // proposal itself, so no proposal body is ever stored while it is pending —
    // the digest the members attested is the whole record of what they agreed.
    cur.end_time = now;
    cur.status = EpochStatus::Ended;
    auto w = vm.put_epoch(cur);
    if (!w) return w;
    EpochRecord next;
    next.epoch = p->epoch;
    next.start_time = now;
    next.committee = p->committee;
    next.committee_nil = p->committee_nil;
    next.threshold = p->threshold;
    next.public_key = p->public_key;
    next.public_key_nil = p->public_key_nil;
    next.status = EpochStatus::Active;
    w = vm.put_epoch(next);
    if (!w) return w;
    return vm.set_current_epoch(p->epoch);
}

}  // namespace

Result<bool> apply(const Transaction& tx, VM& vm, std::int64_t now) {
    auto ok = check_auth(tx, vm, now);
    if (!ok) {
        vm.note_revert(tx, ok.error());
        return false;
    }
    Result<void> err;
    switch (tx.type) {
        case kTxRegisterCiphertext: err = apply_register(tx, vm, now); break;
        case kTxGrantPermit: err = apply_grant(tx, vm, now); break;
        case kTxRevokePermit: err = apply_revoke(tx, vm); break;
        case kTxRequestDecrypt: err = apply_request(tx, vm, now); break;
        case kTxFulfillDecrypt: err = apply_fulfill(tx, vm, now); break;
        case kTxAdvanceEpoch: err = apply_advance(tx, vm, now); break;
        default:
            // Unreachable: syntactic_verify refuses an unknown type before a
            // transaction can reach a block. Treated as a refusal, not a halt.
            return false;
    }
    if (!err) return std::unexpected(err.error());
    return true;
}

}  // namespace lux::fhevm
