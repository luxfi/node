// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/oraclevm/chain.hpp"

#include <algorithm>
#include <chrono>
#include <cstring>

namespace lux::oraclevm {
namespace {

using lux::fhevm::view;

constexpr std::string_view kAliasPrefix = "11111111111111111111111111111111";
// The thirteen letters that name a chain. A letter missing from the table is
// an ordinary id and renders as CB58.
constexpr std::string_view kAliasLetters = "PCXQABMFZGIKD";

bool read_id_member(const json::Value* v, std::string_view field, Id* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::String) {
        *err = "json: cannot unmarshal into Go struct field " + std::string(field) +
               " of type ids.ID";
        return false;
    }
    return id_from_string(v->str, out, err);
}

bool read_node_ids(const json::Value* v, std::string_view field,
                   std::optional<std::vector<NodeId>>* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::Array) {
        *err = "json: cannot unmarshal into Go struct field " + std::string(field) +
               " of type []ids.NodeID";
        return false;
    }
    std::vector<NodeId> items;
    items.reserve(v->array.size());
    for (const auto& e : v->array) {
        NodeId n{};
        if (!json::read_node_id(&e, field, &n, err)) return false;
        items.push_back(n);
    }
    *out = std::move(items);
    return true;
}

bool read_strings(const json::Value* v, std::string_view field,
                  std::optional<std::vector<std::string>>* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::Array) {
        *err = "json: cannot unmarshal into Go struct field " + std::string(field) +
               " of type []string";
        return false;
    }
    std::vector<std::string> items;
    items.reserve(v->array.size());
    for (const auto& e : v->array) {
        std::string s;
        if (!json::read_string(&e, field, &s, err)) return false;
        items.push_back(std::move(s));
    }
    *out = std::move(items);
    return true;
}

bool read_map(const json::Value* v, std::string_view field,
              std::optional<std::map<std::string, std::string>>* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::Object) {
        *err = "json: cannot unmarshal into Go struct field " + std::string(field) +
               " of type map[string]string";
        return false;
    }
    std::map<std::string, std::string> m;
    for (const auto& [k, val] : v->members) {
        std::string s;
        if (!json::read_string(&val, field, &s, err)) return false;
        // A later member with the same key replaces the earlier one, which is
        // what Go's decoder does with a map.
        m[k] = std::move(s);
    }
    *out = std::move(m);
    return true;
}

bool read_time(const json::Value* v, std::string_view field, Time* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::String) {
        *err = "json: cannot unmarshal into Go struct field " + std::string(field) +
               " of type time.Time";
        return false;
    }
    return parse_time(v->str, out, err);
}

bool read_u8(const json::Value* v, std::string_view field, std::uint8_t* out, std::string* err) {
    std::uint64_t n = 0;
    if (!json::read_u64(v, field, 0xFF, &n, err)) return false;
    *out = static_cast<std::uint8_t>(n);
    return true;
}

bool read_u32(const json::Value* v, std::string_view field, std::uint32_t* out, std::string* err) {
    std::uint64_t n = 0;
    if (!json::read_u64(v, field, 0xFFFFFFFFull, &n, err)) return false;
    *out = static_cast<std::uint32_t>(n);
    return true;
}

bool read_u64f(const json::Value* v, std::string_view field, std::uint64_t* out, std::string* err) {
    return json::read_u64(v, field, UINT64_MAX, out, err);
}

// A Go []byte member: base64, or null for the nil slice.
bool read_slice(const json::Value* v, std::string_view field, Slice* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    Bytes b;
    if (!json::read_bytes(v, field, &b, err)) return false;
    *out = std::move(b);
    return true;
}

void write_slice(json::Writer* w, std::string_view key, const Slice& s) {
    w->key(key);
    if (!s.has_value()) {
        w->null();
        return;
    }
    w->bytes(*s, false);
}

void write_time(json::Writer* w, std::string_view key, const Time& t) {
    w->key(key);
    w->string(t.render());
}

void write_id(json::Writer* w, std::string_view key, const Id& id) {
    w->key(key);
    w->string(id_string(id));
}

void be32(lux::fhevm::Hasher* h, std::uint32_t v) {
    const std::uint8_t b[4] = {static_cast<std::uint8_t>(v >> 24), static_cast<std::uint8_t>(v >> 16),
                               static_cast<std::uint8_t>(v >> 8), static_cast<std::uint8_t>(v)};
    h->write(lux::fhevm::ByteView(b, 4));
}

void be64(lux::fhevm::Hasher* h, std::uint64_t v) { h->be64(v); }

std::string hex32(const std::array<std::uint8_t, 32>& a) { return lux::fhevm::hex(view(a)); }

}  // namespace

std::string id_string(const Id& id) {
    const std::string native = lux::fhevm::native_chain_string(id);
    if (!native.empty()) return native;
    return lux::fhevm::cb58(view(id));
}

bool id_from_string(std::string_view s, Id* out, std::string* err) {
    // The empty string is the empty id, and is not an error.
    if (s.empty()) {
        *out = Id{};
        return true;
    }
    // The alias, either in full or as the single letter in either case.
    char letter = 0;
    if (s.size() == 1) {
        letter = static_cast<char>(std::toupper(static_cast<unsigned char>(s[0])));
    } else if (s.size() == kAliasPrefix.size() + 1 && s.substr(0, kAliasPrefix.size()) == kAliasPrefix) {
        letter = s[kAliasPrefix.size()];
    }
    if (letter != 0 && kAliasLetters.find(letter) != std::string_view::npos) {
        Id id{};
        id[31] = static_cast<std::uint8_t>(letter);
        *out = id;
        return true;
    }
    Bytes raw;
    if (!lux::fhevm::cb58_decode(s, &raw)) {
        *err = "couldn't decode ID to bytes";
        return false;
    }
    if (raw.size() != 32) {
        *err = "expected 32 bytes but got " + std::to_string(raw.size());
        return false;
    }
    Id id{};
    std::memcpy(id.data(), raw.data(), 32);
    *out = id;
    return true;
}

// ---- Observation ------------------------------------------------------------

bool Observation::read(const json::Value& v, Observation* out, std::string* err) {
    json::Reader r(v,
                   {"feedId", "value", "timestamp", "sourceMetaHash", "operatorId", "scheme",
                    "signature"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    Observation o;
    if (!read_id_member(r.find("feedId"), "feedId", &o.feed_id, err)) return false;
    if (!read_slice(r.find("value"), "value", &o.value, err)) return false;
    if (!read_time(r.find("timestamp"), "timestamp", &o.timestamp, err)) return false;
    if (!json::read_byte_array(r.find("sourceMetaHash"), "sourceMetaHash", o.source_meta.data(), 32,
                               err))
        return false;
    if (!json::read_node_id(r.find("operatorId"), "operatorId", &o.operator_id, err)) return false;
    if (!read_u8(r.find("scheme"), "scheme", &o.scheme, err)) return false;
    if (!read_slice(r.find("signature"), "signature", &o.signature, err)) return false;
    *out = std::move(o);
    return true;
}

void Observation::write(json::Writer* w) const {
    w->begin_object();
    write_id(w, "feedId", feed_id);
    write_slice(w, "value", value);
    write_time(w, "timestamp", timestamp);
    w->key("sourceMetaHash");
    w->byte_array(view(source_meta));
    w->key("operatorId");
    w->string(lux::fhevm::node_id_string(operator_id));
    w->key("scheme");
    w->u64(scheme);
    write_slice(w, "signature", signature);
    w->end_object();
}

// ---- AggregatedValue --------------------------------------------------------

bool AggregatedValue::read(const json::Value& v, AggregatedValue* out, std::string* err) {
    json::Reader r(
        v, {"feedId", "epoch", "value", "timestamp", "observationCount", "aggProof", "quorumCert"},
        err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    AggregatedValue a;
    if (!read_id_member(r.find("feedId"), "feedId", &a.feed_id, err)) return false;
    if (!read_u64f(r.find("epoch"), "epoch", &a.epoch, err)) return false;
    if (!read_slice(r.find("value"), "value", &a.value, err)) return false;
    if (!read_time(r.find("timestamp"), "timestamp", &a.timestamp, err)) return false;
    if (!json::read_i64(r.find("observationCount"), "observationCount", &a.observations, err))
        return false;
    if (!read_slice(r.find("aggProof"), "aggProof", &a.agg_proof, err)) return false;
    if (!read_slice(r.find("quorumCert"), "quorumCert", &a.quorum_cert, err)) return false;
    *out = std::move(a);
    return true;
}

void AggregatedValue::write(json::Writer* w) const {
    w->begin_object();
    write_id(w, "feedId", feed_id);
    w->key("epoch");
    w->u64(epoch);
    write_slice(w, "value", value);
    write_time(w, "timestamp", timestamp);
    w->key("observationCount");
    w->i64(observations);
    // omitempty: a nil OR empty slice is not written at all.
    if (agg_proof.has_value() && !agg_proof->empty()) write_slice(w, "aggProof", agg_proof);
    if (quorum_cert.has_value() && !quorum_cert->empty()) write_slice(w, "quorumCert", quorum_cert);
    w->end_object();
}

// ---- Feed -------------------------------------------------------------------

bool Feed::read(const json::Value& v, Feed* out, std::string* err) {
    json::Reader r(v,
                   {"id", "name", "description", "sources", "updateFreq", "policyHash", "operators",
                    "createdAt", "status", "metadata"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    Feed f;
    if (!read_id_member(r.find("id"), "id", &f.id, err)) return false;
    if (!json::read_string(r.find("name"), "name", &f.name, err)) return false;
    if (!json::read_string(r.find("description"), "description", &f.description, err)) return false;
    if (!read_strings(r.find("sources"), "sources", &f.sources, err)) return false;
    if (!json::read_i64(r.find("updateFreq"), "updateFreq", &f.update_freq, err)) return false;
    if (!json::read_byte_array(r.find("policyHash"), "policyHash", f.policy_hash.data(), 32, err))
        return false;
    if (!read_node_ids(r.find("operators"), "operators", &f.operators, err)) return false;
    if (!read_time(r.find("createdAt"), "createdAt", &f.created_at, err)) return false;
    if (!json::read_string(r.find("status"), "status", &f.status, err)) return false;
    if (!read_map(r.find("metadata"), "metadata", &f.metadata, err)) return false;
    *out = std::move(f);
    return true;
}

void Feed::write(json::Writer* w) const {
    w->begin_object();
    write_id(w, "id", id);
    w->key("name");
    w->string(name);
    w->key("description");
    w->string(description);
    w->key("sources");
    if (!sources.has_value()) {
        w->null();
    } else {
        w->begin_array();
        for (const auto& s : *sources) w->string(s);
        w->end_array();
    }
    w->key("updateFreq");
    w->i64(update_freq);
    w->key("policyHash");
    w->byte_array(view(policy_hash));
    w->key("operators");
    if (!operators.has_value()) {
        w->null();
    } else {
        w->begin_array();
        for (const auto& n : *operators) w->string(lux::fhevm::node_id_string(n));
        w->end_array();
    }
    write_time(w, "createdAt", created_at);
    w->key("status");
    w->string(status);
    w->key("metadata");
    if (!metadata.has_value()) {
        w->null();
    } else {
        // std::map is ordered, which is the sorted order encoding/json writes
        // a Go map's keys in and therefore the order the id is taken over.
        w->begin_object();
        for (const auto& [k, val] : *metadata) {
            w->key(k);
            w->string(val);
        }
        w->end_object();
    }
    w->end_object();
}

bool Feed::admits(const NodeId& op) const {
    if (!operators.has_value()) return false;
    return std::find(operators->begin(), operators->end(), op) != operators->end();
}

// ---- Block ------------------------------------------------------------------

namespace {

template <typename T>
bool read_list(const json::Value* v, std::string_view field, std::optional<std::vector<T>>* out,
               std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != json::Kind::Array) {
        *err = "json: cannot unmarshal into Go struct field " + std::string(field) + " of type slice";
        return false;
    }
    std::vector<T> items;
    items.reserve(v->array.size());
    for (const auto& e : v->array) {
        T item;
        if (!T::read(e, &item, err)) return false;
        items.push_back(std::move(item));
    }
    *out = std::move(items);
    return true;
}

}  // namespace

bool Block::read(const json::Value& v, Block* out, std::string* err) {
    json::Reader r(v,
                   {"id", "parentID", "height", "timestamp", "observations", "aggregations",
                    "feedUpdates", "attestations"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    const json::Value* att = r.find("attestations");
    if (att != nullptr && !att->null()) {
        *err = "oraclevm: this port does not model an attestation";
        return false;
    }
    Block b;
    if (!read_id_member(r.find("id"), "id", &b.id, err)) return false;
    if (!read_id_member(r.find("parentID"), "parentID", &b.parent_id, err)) return false;
    if (!read_u64f(r.find("height"), "height", &b.height, err)) return false;
    if (!read_time(r.find("timestamp"), "timestamp", &b.timestamp, err)) return false;
    if (!read_list<Observation>(r.find("observations"), "observations", &b.observations, err))
        return false;
    if (!read_list<AggregatedValue>(r.find("aggregations"), "aggregations", &b.aggregations, err))
        return false;
    if (!read_list<Feed>(r.find("feedUpdates"), "feedUpdates", &b.feed_updates, err)) return false;
    *out = std::move(b);
    return true;
}

std::string Block::write() const {
    json::Writer w;
    w.begin_object();
    write_id(&w, "id", id);
    write_id(&w, "parentID", parent_id);
    w.key("height");
    w.u64(height);
    write_time(&w, "timestamp", timestamp);
    if (observations.has_value() && !observations->empty()) {
        w.key("observations");
        w.begin_array();
        for (const auto& o : *observations) o.write(&w);
        w.end_array();
    }
    if (aggregations.has_value() && !aggregations->empty()) {
        w.key("aggregations");
        w.begin_array();
        for (const auto& a : *aggregations) a.write(&w);
        w.end_array();
    }
    if (feed_updates.has_value() && !feed_updates->empty()) {
        w.key("feedUpdates");
        w.begin_array();
        for (const auto& f : *feed_updates) f.write(&w);
        w.end_array();
    }
    w.end_object();
    return w.str();
}

Id Block::compute_id() const {
    const std::string bytes = write();
    return lux::fhevm::sha256(view(std::string_view(bytes)));
}

// ---- Genesis ----------------------------------------------------------------

bool Genesis::read(const json::Value& v, Genesis* out, std::string* err) {
    json::Reader r(v, {"version", "message", "timestamp", "initialFeeds"}, err,
                   json::Unknown::Ignore);
    if (!r.ok()) return false;
    Genesis g;
    if (!json::read_i64(r.find("version"), "version", &g.version, err)) return false;
    if (!json::read_string(r.find("message"), "message", &g.message, err)) return false;
    if (!json::read_i64(r.find("timestamp"), "timestamp", &g.timestamp, err)) return false;
    if (!read_list<Feed>(r.find("initialFeeds"), "initialFeeds", &g.initial_feeds, err))
        return false;
    *out = std::move(g);
    return true;
}

std::string Genesis::write() const {
    json::Writer w;
    w.begin_object();
    w.key("version");
    w.i64(version);
    w.key("message");
    w.string(message);
    w.key("timestamp");
    w.i64(timestamp);
    if (initial_feeds.has_value() && !initial_feeds->empty()) {
        w.key("initialFeeds");
        w.begin_array();
        for (const auto& f : *initial_feeds) f.write(&w);
        w.end_array();
    }
    w.end_object();
    return w.str();
}

// ---- OracleRequest / OracleRecord -------------------------------------------

bool OracleRequest::read(const json::Value& v, OracleRequest* out, std::string* err) {
    json::Reader r(v,
                   {"requestId", "serviceId", "sessionId", "step", "retry", "txId", "kind",
                    "target", "payloadHash", "schemaHash", "deadlineHeight", "executors",
                    "createdAt", "status"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    OracleRequest q;
    if (!json::read_byte_array(r.find("requestId"), "requestId", q.request_id.data(), 32, err))
        return false;
    if (!read_id_member(r.find("serviceId"), "serviceId", &q.service_id, err)) return false;
    if (!read_id_member(r.find("sessionId"), "sessionId", &q.session_id, err)) return false;
    if (!read_u32(r.find("step"), "step", &q.step, err)) return false;
    if (!read_u32(r.find("retry"), "retry", &q.retry, err)) return false;
    if (!read_id_member(r.find("txId"), "txId", &q.tx_id, err)) return false;
    if (!read_u8(r.find("kind"), "kind", &q.kind, err)) return false;
    if (!read_slice(r.find("target"), "target", &q.target, err)) return false;
    if (!json::read_byte_array(r.find("payloadHash"), "payloadHash", q.payload_hash.data(), 32, err))
        return false;
    if (!json::read_byte_array(r.find("schemaHash"), "schemaHash", q.schema_hash.data(), 32, err))
        return false;
    if (!read_u64f(r.find("deadlineHeight"), "deadlineHeight", &q.deadline_height, err))
        return false;
    if (!read_node_ids(r.find("executors"), "executors", &q.executors, err)) return false;
    if (!read_time(r.find("createdAt"), "createdAt", &q.created_at, err)) return false;
    if (!read_u8(r.find("status"), "status", &q.status, err)) return false;
    *out = std::move(q);
    return true;
}

bool OracleRequest::admits(const NodeId& executor) const {
    if (!executors.has_value()) return false;
    return std::find(executors->begin(), executors->end(), executor) != executors->end();
}

bool OracleRecord::read(const json::Value& v, OracleRecord* out, std::string* err) {
    json::Reader r(v,
                   {"requestId", "executor", "timestamp", "endpoint", "bodyHash", "resultCode",
                    "externalRef", "scheme", "signature"},
                   err, json::Unknown::Ignore);
    if (!r.ok()) return false;
    OracleRecord rec;
    if (!json::read_byte_array(r.find("requestId"), "requestId", rec.request_id.data(), 32, err))
        return false;
    if (!json::read_node_id(r.find("executor"), "executor", &rec.executor, err)) return false;
    if (!read_u64f(r.find("timestamp"), "timestamp", &rec.timestamp, err)) return false;
    if (!json::read_string(r.find("endpoint"), "endpoint", &rec.endpoint, err)) return false;
    if (!json::read_byte_array(r.find("bodyHash"), "bodyHash", rec.body_hash.data(), 32, err))
        return false;
    if (!read_u32(r.find("resultCode"), "resultCode", &rec.result_code, err)) return false;
    if (!read_slice(r.find("externalRef"), "externalRef", &rec.external_ref, err)) return false;
    if (!read_u8(r.find("scheme"), "scheme", &rec.scheme, err)) return false;
    if (!read_slice(r.find("signature"), "signature", &rec.signature, err)) return false;
    *out = std::move(rec);
    return true;
}

// ---- derivations ------------------------------------------------------------

std::array<std::uint8_t, 32> compute_request_id(const Id& service, const Id& session, const Id& tx,
                                                std::uint32_t step, std::uint32_t retry) {
    lux::fhevm::Hasher h;
    h.write(std::string_view("LUX:OracleRequest:v1"));
    h.write(view(service));
    h.write(view(session));
    be32(&h, step);
    be32(&h, retry);
    h.write(view(tx));
    return h.sum();
}

namespace {

// The leaf a record hashes to. The signature is NOT in it: two records that
// differ only there commit to one root.
std::array<std::uint8_t, 32> record_leaf(const OracleRecord& r) {
    lux::fhevm::Hasher h;
    h.write(view(r.request_id));
    h.write(view(r.executor));
    be64(&h, r.timestamp);
    h.write(std::string_view(r.endpoint));
    h.write(view(r.body_hash));
    be32(&h, r.result_code);
    if (r.external_ref.has_value()) h.write(view(*r.external_ref));
    return h.sum();
}

}  // namespace

std::array<std::uint8_t, 32> records_merkle_root(const std::vector<OracleRecord>& records) {
    if (records.empty()) return std::array<std::uint8_t, 32>{};
    std::vector<std::array<std::uint8_t, 32>> level;
    level.reserve(records.size());
    for (const auto& r : records) level.push_back(record_leaf(r));
    while (level.size() > 1) {
        std::vector<std::array<std::uint8_t, 32>> next;
        next.reserve((level.size() + 1) / 2);
        for (std::size_t i = 0; i < level.size(); i += 2) {
            lux::fhevm::Hasher h;
            h.write(view(level[i]));
            h.write(view(i + 1 < level.size() ? level[i + 1] : level[i]));
            next.push_back(h.sum());
        }
        level = std::move(next);
    }
    return level[0];
}

// ---- the chain --------------------------------------------------------------

Vm::Vm(const std::vector<Feed>& feeds) {
    for (const auto& f : feeds) feeds_[f.id] = f;
}

bool Vm::submit_observation(const Observation& obs, std::string* err) {
    const auto it = feeds_.find(obs.feed_id);
    if (it == feeds_.end()) {
        *err = "feed not found";
        return false;
    }
    const std::int64_t now = std::chrono::duration_cast<std::chrono::seconds>(
                                 std::chrono::system_clock::now().time_since_epoch())
                                 .count();
    if (now - obs.timestamp.unix() > kObservationWindowSeconds) {
        *err = "stale observation";
        return false;
    }
    if (!it->second.admits(obs.operator_id)) {
        *err = "operator " + lux::fhevm::node_id_string(obs.operator_id) +
               " not authorized for feed " + it->second.name;
        return false;
    }
    return true;
}

bool Vm::register_request(const OracleRequest& req, std::string* err) {
    const auto expected =
        compute_request_id(req.service_id, req.session_id, req.tx_id, req.step, req.retry);
    if (expected != req.request_id) {
        *err = "invalid request_id: expected " + hex32(expected) + ", got " + hex32(req.request_id);
        return false;
    }
    if (requests_.count(req.request_id) != 0) {
        *err = "request " + hex32(req.request_id) + " already exists";
        return false;
    }
    requests_[req.request_id] = req;
    records_[req.request_id] = {};
    return true;
}

bool Vm::submit_record(const OracleRecord& rec, std::string* err) {
    const auto it = requests_.find(rec.request_id);
    if (it == requests_.end()) {
        *err = "request " + hex32(rec.request_id) + " not found";
        return false;
    }
    if (!it->second.admits(rec.executor)) {
        *err = "executor " + lux::fhevm::node_id_string(rec.executor) +
               " not authorized for request " + hex32(rec.request_id);
        return false;
    }
    if (last_height_ > it->second.deadline_height) {
        *err = "request " + hex32(rec.request_id) + " has expired";
        return false;
    }
    records_[rec.request_id].push_back(rec);
    return true;
}

bool Vm::commit_records(const std::array<std::uint8_t, 32>& request_id, Commit* out,
                        std::string* err) {
    if (requests_.count(request_id) == 0) {
        *err = "request " + hex32(request_id) + " not found";
        return false;
    }
    const auto& records = records_[request_id];
    if (records.empty()) {
        *err = "no records for request " + hex32(request_id);
        return false;
    }
    Commit c;
    c.root = records_merkle_root(records);
    c.count = static_cast<std::uint32_t>(records.size());
    // The window walks the records the way the chain walks them, zero
    // included: a record timestamped zero does not open the window, because
    // the start is only replaced while it is still zero.
    for (const auto& r : records) {
        if (c.window_start == 0 || r.timestamp < c.window_start) c.window_start = r.timestamp;
        if (r.timestamp > c.window_end) c.window_end = r.timestamp;
    }
    *out = c;
    return true;
}

}  // namespace lux::oraclevm
