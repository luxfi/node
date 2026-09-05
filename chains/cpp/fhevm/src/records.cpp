// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/records.hpp"

#include "lux/fhevm/json.hpp"

#include <algorithm>
#include <set>

namespace lux::fhevm {

std::string_view request_status_string(RequestStatus s) {
    switch (s) {
        case RequestStatus::Pending: return "pending";
        case RequestStatus::Processing: return "processing";
        case RequestStatus::Completed: return "completed";
        case RequestStatus::Failed: return "failed";
        case RequestStatus::Expired: return "expired";
    }
    return "unknown";
}

bool EpochRecord::member_of(const Account& acct) const {
    for (const auto& m : committee) {
        if (address_of(view(m.public_key)) == acct) return true;
    }
    return false;
}

std::int64_t tally(const std::vector<Attestation>& as, const Id& value) {
    std::set<Account> seen;
    for (const auto& a : as) {
        if (a.value != value) continue;
        seen.insert(a.member);
    }
    return std::int64_t(seen.size());
}

void vote(std::vector<Attestation>& as, bool& is_nil, const Account& member, const Id& value) {
    // Casting a vote makes the list non-nil, which is what Go's append does to a
    // nil slice — and the difference is on the wire: Marshal writes null for a
    // nil slice and [] for an empty one.
    is_nil = false;
    for (auto& a : as) {
        if (a.member == member) {
            a.value = value;
            return;
        }
    }
    as.push_back(Attestation{member, value});
}

bool committee_order(const std::vector<CommitteeMember>& c) {
    return std::is_sorted(c.begin(), c.end(), [](const CommitteeMember& a, const CommitteeMember& b) {
        return a.node_id < b.node_id;
    });
}

Account address_of(ByteView public_key) {
    Id h = sha256(public_key);
    Account a{};
    std::copy(h.begin(), h.begin() + std::int64_t(a.size()), a.begin());
    return a;
}

Id committee_digest(std::uint64_t epoch, std::int64_t threshold, ByteView public_key,
                    const std::vector<CommitteeMember>& c) {
    Hasher h;
    h.write("fhevm/epoch/");
    h.be64(epoch);
    h.be64(std::uint64_t(threshold));
    h.len_prefixed(public_key);
    h.be64(std::uint64_t(c.size()));
    for (const auto& m : c) {
        h.write(view(m.node_id));
        h.len_prefixed(view(m.public_key));
        h.be64(m.weight);
        h.be64(std::uint64_t(m.index));
    }
    return h.sum();
}

Id derive_handle(const Id& digest, std::string_view scheme) {
    Hasher h;
    h.write("fhevm/ct/");
    h.write(view(digest));
    h.len_prefixed(view(scheme));
    return h.sum();
}

Id derive_permit_id(const Id& handle, const Account& grantor, const Account& grantee,
                    std::uint32_t ops, std::int64_t expiry, std::uint64_t nonce) {
    Hasher h;
    h.write("fhevm/permit/");
    h.write(view(handle));
    h.write(view(grantor));
    h.write(view(grantee));
    h.be64(std::uint64_t(ops));
    h.be64(std::uint64_t(expiry));
    h.be64(nonce);
    return h.sum();
}

Id derive_request_id(const Id& handle, const Account& requester, std::uint64_t nonce) {
    Hasher h;
    h.write("fhevm/decrypt/");
    h.write(view(handle));
    h.write(view(requester));
    h.be64(nonce);
    return h.sum();
}

// ---- persistence -------------------------------------------------------------

void write_member(json::Writer& w, const CommitteeMember& m) {
    w.begin_object();
    w.key("node_id");
    w.string(node_id_string(m.node_id));
    w.key("public_key");
    w.bytes(m.public_key, m.public_key_nil);
    w.key("weight");
    w.u64(m.weight);
    w.key("index");
    w.i64(m.index);
    w.end_object();
}

bool read_member(const json::Value& v, CommitteeMember* out, std::string* err,
                 json::Unknown unknown) {
    json::Reader r(v, {"node_id", "public_key", "weight", "index"}, err, unknown);
    if (!r.ok()) return false;
    if (!read_node_id(r.find("node_id"), "node_id", &out->node_id, err)) return false;
    const json::Value* pk = r.find("public_key");
    if (pk != nullptr && !pk->null()) {
        if (!read_bytes(pk, "public_key", &out->public_key, err)) return false;
        out->public_key_nil = false;
    }
    if (!read_u64(r.find("weight"), "weight", ~std::uint64_t(0), &out->weight, err)) return false;
    if (!read_i64(r.find("index"), "index", &out->index, err)) return false;
    return true;
}

// write_attestations writes a Go []Attestation, and `is_nil` is the difference
// between a nil slice and an empty one: Go marshals the first as null and the
// second as []. Nothing this chain WRITES produces an empty non-nil list, but a
// row that arrives holding one has to come back out as it went in.
void write_attestations(json::Writer& w, const std::vector<Attestation>& as, bool is_nil) {
    if (as.empty() && is_nil) {
        w.null();
        return;
    }
    w.begin_array();
    for (const auto& a : as) {
        w.begin_object();
        w.key("member");
        w.string(account_string(a.member));
        w.key("value");
        w.byte_array(view(a.value));
        w.end_object();
    }
    w.end_array();
}

bool read_attestations(const json::Value* v, std::vector<Attestation>* out, bool* is_nil,
                       std::string* err, json::Unknown unknown) {
    if (v == nullptr || v->null()) return true;
    *is_nil = false;
    if (v->kind != json::Kind::Array) {
        *err = "cannot unmarshal into attestations";
        return false;
    }
    for (const auto& el : v->array) {
        json::Reader r(el, {"member", "value"}, err, unknown);
        if (!r.ok()) return false;
        Attestation a;
        if (!read_account(r.find("member"), "member", &a.member, err)) return false;
        if (!read_byte_array(r.find("value"), "value", a.value.data(), a.value.size(), err)) {
            return false;
        }
        out->push_back(a);
    }
    return true;
}

std::string marshal(const CiphertextRecord& r) {
    json::Writer w;
    w.begin_object();
    w.key("handle");
    w.byte_array(view(r.handle));
    w.key("owner");
    w.byte_array(view(r.owner));
    w.key("type");
    w.u64(r.type);
    w.key("level");
    w.i64(r.level);
    w.key("epoch");
    w.u64(r.epoch);
    w.key("registered_at");
    w.i64(r.registered_at);
    w.key("size");
    w.u64(r.size);
    w.key("chain_id");
    w.string(id_string(r.chain_id));
    w.key("scheme");
    w.string(r.scheme);
    w.key("digest");
    w.byte_array(view(r.digest));
    w.end_object();
    return w.str();
}

std::string marshal(const PermitRecord& r) {
    json::Writer w;
    w.begin_object();
    w.key("permit_id");
    w.byte_array(view(r.permit_id));
    w.key("handle");
    w.byte_array(view(r.handle));
    w.key("grantee");
    w.byte_array(view(r.grantee));
    w.key("grantor");
    w.byte_array(view(r.grantor));
    w.key("operations");
    w.u64(r.operations);
    w.key("expiry");
    w.i64(r.expiry);
    w.key("created_at");
    w.i64(r.created_at);
    // attestation is `omitempty`: an empty slice is not written at all.
    if (!r.attestation.empty()) {
        w.key("attestation");
        w.bytes(r.attestation, false);
    }
    w.key("chain_id");
    w.string(id_string(r.chain_id));
    w.key("status");
    w.string(r.status);
    w.end_object();
    return w.str();
}

std::string marshal(const DecryptRecord& r) {
    json::Writer w;
    w.begin_object();
    w.key("request_id");
    w.byte_array(view(r.request_id));
    w.key("ciphertext_handle");
    w.byte_array(view(r.ciphertext_handle));
    w.key("requester");
    w.byte_array(view(r.requester));
    w.key("callback");
    w.byte_array(view(r.callback));
    w.key("callback_selector");
    w.byte_array(view(r.callback_selector));
    w.key("source_chain");
    w.string(id_string(r.source_chain));
    w.key("epoch");
    w.u64(r.epoch);
    w.key("nonce");
    w.u64(r.nonce);
    w.key("expiry");
    w.i64(r.expiry);
    w.key("status");
    w.u64(std::uint8_t(r.status));
    w.key("created_at");
    w.i64(r.created_at);
    if (r.completed_at != 0) {
        w.key("completed_at");
        w.i64(r.completed_at);
    }
    // result_handle carries `omitempty`, but a Go ARRAY is never empty, so it
    // is always written — including all-zero, which is what a pending request
    // has. Dropping it here would be a silent divergence from the Go record.
    w.key("result_handle");
    w.byte_array(view(r.result_handle));
    if (!r.error.empty()) {
        w.key("error");
        w.string(r.error);
    }
    w.key("permitId");
    w.byte_array(view(r.permit_id));
    w.key("attestations");
    write_attestations(w, r.attestations, r.attestations_nil);
    w.end_object();
    return w.str();
}

std::string marshal(const EpochRecord& r) {
    json::Writer w;
    w.begin_object();
    w.key("epoch");
    w.u64(r.epoch);
    w.key("start_time");
    w.i64(r.start_time);
    if (r.end_time != 0) {
        w.key("end_time");
        w.i64(r.end_time);
    }
    w.key("committee");
    if (r.committee.empty() && r.committee_nil) {
        w.null();
    } else {
        w.begin_array();
        for (const auto& m : r.committee) write_member(w, m);
        w.end_array();
    }
    w.key("threshold");
    w.i64(r.threshold);
    w.key("public_key");
    w.bytes(r.public_key, r.public_key_nil);
    w.key("status");
    w.u64(std::uint8_t(r.status));
    w.key("attestations");
    write_attestations(w, r.attestations, r.attestations_nil);
    w.end_object();
    return w.str();
}

namespace {

// one_value parses a whole document as a single JSON value: the shape every
// record read has.
//
// A record is read the way the reference reads one — plain json.Unmarshal, not
// a Decoder — so the rules here are Unmarshal's and not a payload's. Unmarshal
// scans the whole document, so ANY non-space byte after the value is an error,
// a stray closing bracket included; and it IGNORES a member the schema does not
// describe, which is why every read below passes Unknown::Ignore. A record row
// carrying a member this build does not know is one an older or newer build
// wrote, and the reference loads it.
bool one_value(std::string_view s, json::Value* v, std::string* err) {
    std::size_t consumed = 0;
    if (!json::parse(s, v, &consumed, err)) return false;
    if (json::trailing(s, consumed)) {
        *err = "trailing content";
        return false;
    }
    return true;
}

bool read_id(const json::Value* v, std::string_view field, Id* out, std::string* err) {
    return read_byte_array(v, field, out->data(), out->size(), err);
}

bool read_chain_id(const json::Value* v, std::string_view field, Id* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::String) {
        *err = "cannot unmarshal into " + std::string(field);
        return false;
    }
    // ids.ID.UnmarshalJSON reads "" as the zero id — unlike an address, which
    // refuses it. Two types, two rules; json.cpp's read_account keeps the other.
    if (v->str.empty()) {
        *out = Id{};
        return true;
    }
    // A native chain answers with its own name rather than cb58, so the reader
    // has to know both spellings — the same two the writer chooses between —
    // AND the one-letter alias, which the reference also takes and which the
    // writer never emits. A reader that took less than the writer of the OTHER
    // implementation emits is how one node skips a row the other loads.
    if (v->str.size() == 1) {
        char c = v->str[0];
        if (c >= 'a' && c <= 'z') c = char(c - 'a' + 'A');
        if (std::string_view("PCXQABMFZGIKD").find(c) != std::string_view::npos) {
            Id candidate{};
            candidate[31] = std::uint8_t(c);
            *out = candidate;
            return true;
        }
        // A single letter that names no chain falls through to cb58, which
        // refuses it — the same order the reference resolves in.
    }
    for (char letter : std::string_view("PCXQABMFZGIKD")) {
        Id candidate{};
        candidate[31] = std::uint8_t(letter);
        if (native_chain_string(candidate) == v->str) {
            *out = candidate;
            return true;
        }
    }
    Bytes b;
    if (!cb58_decode(v->str, &b) || b.size() != out->size()) {
        *err = "couldn't decode " + std::string(field) + " to bytes";
        return false;
    }
    std::copy(b.begin(), b.end(), out->begin());
    return true;
}

}  // namespace

bool unmarshal(std::string_view s, CiphertextRecord* out, std::string* err) {
    json::Value v;
    if (!one_value(s, &v, err)) return false;
    json::Reader r(v, {"handle", "owner", "type", "level", "epoch", "registered_at", "size",
                       "chain_id", "scheme", "digest"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    std::uint64_t u = 0;
    if (!read_id(r.find("handle"), "handle", &out->handle, err)) return false;
    if (!read_byte_array(r.find("owner"), "owner", out->owner.data(), out->owner.size(), err)) {
        return false;
    }
    if (!read_u64(r.find("type"), "type", 255, &u, err)) return false;
    out->type = std::uint8_t(u);
    if (!read_i64(r.find("level"), "level", &out->level, err)) return false;
    if (!read_u64(r.find("epoch"), "epoch", ~std::uint64_t(0), &out->epoch, err)) return false;
    if (!read_i64(r.find("registered_at"), "registered_at", &out->registered_at, err)) return false;
    u = 0;
    if (!read_u64(r.find("size"), "size", 0xffffffffu, &u, err)) return false;
    out->size = std::uint32_t(u);
    if (!read_chain_id(r.find("chain_id"), "chain_id", &out->chain_id, err)) return false;
    if (!read_string(r.find("scheme"), "scheme", &out->scheme, err)) return false;
    if (!read_id(r.find("digest"), "digest", &out->digest, err)) return false;
    return true;
}

bool unmarshal(std::string_view s, PermitRecord* out, std::string* err) {
    json::Value v;
    if (!one_value(s, &v, err)) return false;
    json::Reader r(v, {"permit_id", "handle", "grantee", "grantor", "operations", "expiry",
                       "created_at", "attestation", "chain_id", "status"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    if (!read_id(r.find("permit_id"), "permit_id", &out->permit_id, err)) return false;
    if (!read_id(r.find("handle"), "handle", &out->handle, err)) return false;
    if (!read_byte_array(r.find("grantee"), "grantee", out->grantee.data(), out->grantee.size(),
                         err)) {
        return false;
    }
    if (!read_byte_array(r.find("grantor"), "grantor", out->grantor.data(), out->grantor.size(),
                         err)) {
        return false;
    }
    std::uint64_t u = 0;
    if (!read_u64(r.find("operations"), "operations", 0xffffffffu, &u, err)) return false;
    out->operations = std::uint32_t(u);
    if (!read_i64(r.find("expiry"), "expiry", &out->expiry, err)) return false;
    if (!read_i64(r.find("created_at"), "created_at", &out->created_at, err)) return false;
    if (!read_bytes(r.find("attestation"), "attestation", &out->attestation, err)) return false;
    if (!read_chain_id(r.find("chain_id"), "chain_id", &out->chain_id, err)) return false;
    if (!read_string(r.find("status"), "status", &out->status, err)) return false;
    return true;
}

bool unmarshal(std::string_view s, DecryptRecord* out, std::string* err) {
    json::Value v;
    if (!one_value(s, &v, err)) return false;
    json::Reader r(v, {"request_id", "ciphertext_handle", "requester", "callback",
                       "callback_selector", "source_chain", "epoch", "nonce", "expiry", "status",
                       "created_at", "completed_at", "result_handle", "error", "permitId",
                       "attestations"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    if (!read_id(r.find("request_id"), "request_id", &out->request_id, err)) return false;
    if (!read_id(r.find("ciphertext_handle"), "ciphertext_handle", &out->ciphertext_handle, err)) {
        return false;
    }
    if (!read_byte_array(r.find("requester"), "requester", out->requester.data(),
                         out->requester.size(), err)) {
        return false;
    }
    if (!read_byte_array(r.find("callback"), "callback", out->callback.data(), out->callback.size(),
                         err)) {
        return false;
    }
    if (!read_byte_array(r.find("callback_selector"), "callback_selector",
                         out->callback_selector.data(), out->callback_selector.size(), err)) {
        return false;
    }
    if (!read_chain_id(r.find("source_chain"), "source_chain", &out->source_chain, err)) return false;
    if (!read_u64(r.find("epoch"), "epoch", ~std::uint64_t(0), &out->epoch, err)) return false;
    if (!read_u64(r.find("nonce"), "nonce", ~std::uint64_t(0), &out->nonce, err)) return false;
    if (!read_i64(r.find("expiry"), "expiry", &out->expiry, err)) return false;
    std::uint64_t u = 0;
    if (!read_u64(r.find("status"), "status", 255, &u, err)) return false;
    out->status = RequestStatus(u);
    if (!read_i64(r.find("created_at"), "created_at", &out->created_at, err)) return false;
    if (!read_i64(r.find("completed_at"), "completed_at", &out->completed_at, err)) return false;
    if (!read_id(r.find("result_handle"), "result_handle", &out->result_handle, err)) return false;
    if (!read_string(r.find("error"), "error", &out->error, err)) return false;
    if (!read_id(r.find("permitId"), "permitId", &out->permit_id, err)) return false;
    if (!read_attestations(r.find("attestations"), &out->attestations, &out->attestations_nil,
                           err)) {
        return false;
    }
    return true;
}

bool unmarshal(std::string_view s, EpochRecord* out, std::string* err) {
    json::Value v;
    if (!one_value(s, &v, err)) return false;
    json::Reader r(v, {"epoch", "start_time", "end_time", "committee", "threshold", "public_key",
                       "status", "attestations"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    if (!read_u64(r.find("epoch"), "epoch", ~std::uint64_t(0), &out->epoch, err)) return false;
    if (!read_i64(r.find("start_time"), "start_time", &out->start_time, err)) return false;
    if (!read_i64(r.find("end_time"), "end_time", &out->end_time, err)) return false;
    const json::Value* c = r.find("committee");
    if (c != nullptr && !c->null()) {
        if (c->kind != json::Kind::Array) {
            *err = "cannot unmarshal into committee";
            return false;
        }
        out->committee_nil = false;
        for (const auto& el : c->array) {
            CommitteeMember m;
            if (!read_member(el, &m, err, json::Unknown::Ignore)) return false;
            out->committee.push_back(std::move(m));
        }
    }
    if (!read_i64(r.find("threshold"), "threshold", &out->threshold, err)) return false;
    const json::Value* pk = r.find("public_key");
    if (pk != nullptr && !pk->null()) {
        if (!read_bytes(pk, "public_key", &out->public_key, err)) return false;
        out->public_key_nil = false;
    }
    std::uint64_t u = 0;
    if (!read_u64(r.find("status"), "status", 255, &u, err)) return false;
    out->status = EpochStatus(u);
    if (!read_attestations(r.find("attestations"), &out->attestations, &out->attestations_nil,
                           err)) {
        return false;
    }
    return true;
}

}  // namespace lux::fhevm
