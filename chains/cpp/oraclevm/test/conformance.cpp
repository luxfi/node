// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// The C++ O-chain's answers to the shared corpus.
//
// One of three evaluators — Go, Rust, C++ — that read the same
// conformance/corpus/vectors.tsv and print the same seven fields per vector.
// The runner compares the three sets of lines; this program never sees another
// implementation's answer and has nothing to agree with.
//
// WHAT IS ANSWERED. Six ops, each a different plane of the chain: `block` is
// the JSON wire and the id taken over its re-marshal, `genesis` is the
// configuration the observation vectors are judged under, `requestid` is the
// derivation that gives a request its name, and `request`, `commit` and
// `observation` are the three places this chain refuses anything.
//
// WHERE THE TWO COMPARED HALVES DIVIDE. The corpus's rule, from
// conformance/README.md, is that `syntactic` is the verdict a single pass
// reached BEFORE it read the chain. On O there is exactly one such refusal — a
// request whose id its own arguments do not derive, checked ahead of the
// request map — and everything else is past a lookup and leaves `syntactic` OK.
// The reference's Block::Verify is unconditional, so a block that parses
// answers OK to both.
//
// Usage: oraclevm_conformance <vectors.tsv> [repeats]

#include "lux/oraclevm/chain.hpp"

#include <algorithm>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>

namespace {

using namespace lux::oraclevm;
namespace json = lux::fhevm::json;

constexpr const char* kNone = "-";
constexpr const char* kInternal = "INTERNAL";
constexpr const char* kOk = "OK";
constexpr const char* kMalformed = "MALFORMED";
constexpr const char* kSyntactic = "SYNTACTIC";
constexpr const char* kOverflow = "OVERFLOW";
constexpr const char* kLedger = "LEDGER";
constexpr const char* kAuth = "AUTH";
constexpr const char* kWarp = "WARP";
constexpr const char* kUnsupported = "UNSUPPORTED";

// The chain this evaluator was built for, and the network it belongs to. Both
// are corpus contract; O_CHAIN_IDENTITY asks them back and this answers with
// the ones it HOLDS, never the ones the corpus asked about.
constexpr std::uint8_t kChainByte = 79;  // 'O'
constexpr std::uint32_t kNetworkId = 1;

// The feeds every observation vector is judged against. These bytes are the
// corpus's O_GENESIS vector, and they are here so that an implementation
// seeded differently is named by that row rather than by the four underneath.
constexpr const char* kGenesis =
    R"({"version":1,"message":"conformance","timestamp":1000,)"
    R"("initialFeeds":[{"id":"2Kw2XL8QVSQHwJYKpWLktaxtwKuz7iYF5pqcauUHpmcrSjRPp",)"
    R"("name":"lux-usd","description":"","sources":null,"updateFreq":0,)"
    R"("policyHash":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],)"
    R"("operators":["NodeID-6Jswqk47s9PUcyCc88MMVwzgvHQxfLnA"],)"
    R"("createdAt":"1970-01-01T00:00:05Z","status":"active","metadata":null}]})";

struct Row {
    std::string id;
    std::string parse;
    std::string kind = kNone;
    std::string hash = kNone;
    std::string syntactic = kNone;
    std::string exec = kNone;
    std::string note;

    explicit Row(std::string vector_id) : id(std::move(vector_id)) {}

    Row& malformed(const std::string& why) {
        parse = kMalformed;
        syntactic = kMalformed;
        exec = kMalformed;
        note = why;
        return *this;
    }

    Row& internal(const std::string& why) {
        parse = kInternal;
        syntactic = kInternal;
        exec = kInternal;
        note = why;
        return *this;
    }

    std::string line() const {
        std::string n = note;
        std::replace(n.begin(), n.end(), '\t', ' ');
        std::replace(n.begin(), n.end(), '\n', ' ');
        if (n.empty()) n = kNone;
        return "R\t" + id + "\t" + parse + "\t" + kind + "\t" + hash + "\t" + syntactic + "\t" +
               exec + "\t" + n;
    }
};

std::string lower(std::string s) {
    for (char& c : s) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    return s;
}

// The shared verdict vocabulary, reached from this implementation's own refusal
// words. The table is the same table in the same ORDER as the Go and Rust
// evaluators carry: a message that refuses a kind by name usually lists the
// kinds it would accept, so the refusal-by-name words are tested before the
// ledger words.
const char* classify(const std::string& msg) {
    const std::string s = lower(msg);
    const auto has = [&](const char* w) { return s.find(w) != std::string::npos; };
    if (has("overflow") || has("underflow")) return kOverflow;
    if (has("wrong transaction type") || has("wrong tx type") || has("not permitted") ||
        has("not held") || has("unsupported") || has("forbidden") || has("unknown") ||
        has("parameter set"))
        return kUnsupported;
    if (has("credential") || has("signature") || has("unauthorized") || has("not authorised") ||
        has("not authorized") || has("proof verification failed") || has("does not match auth"))
        return kAuth;
    if (has("warp")) return kWarp;
    if (has("utxo") || has("funds") || has("insufficient") || has("burn") || has("consumed") ||
        has("produced") || has("flow") || has("fee") || has("not found") || has("doesn't exist") ||
        has("does not exist") || has("isn't a current") || has("not validator") || has("no such") ||
        has("could not load") || has("shared memory") || has("state root") ||
        has("precedes its parent") || has("skew allowance"))
        return kLedger;
    return kSyntactic;
}

std::string trim(const std::string& s) {
    std::string out = s;
    std::replace(out.begin(), out.end(), '\n', ' ');
    if (out.size() > 160) out.resize(160);
    return out;
}

bool from_hex_wire(const std::string& wire, lux::fhevm::Bytes* out) {
    if (wire == kNone) {
        out->clear();
        return true;
    }
    return lux::fhevm::from_hex(wire, out);
}

// parse_value reads one JSON value and refuses any non-space byte after it,
// which is what json.Unmarshal does.
bool parse_value(const lux::fhevm::Bytes& bytes, json::Value* out, std::string* err) {
    const std::string_view in(reinterpret_cast<const char*>(bytes.data()), bytes.size());
    std::size_t consumed = 0;
    if (!json::parse(in, out, &consumed, err)) return false;
    if (json::trailing(in, consumed)) {
        *err = "invalid character after top-level value";
        return false;
    }
    return true;
}

bool seeded_vm(std::unique_ptr<Vm>* out, std::string* err) {
    json::Value v;
    lux::fhevm::Bytes bytes(kGenesis, kGenesis + std::char_traits<char>::length(kGenesis));
    if (!parse_value(bytes, &v, err)) return false;
    Genesis g;
    if (!Genesis::read(v, &g, err)) return false;
    const std::vector<Feed> feeds = g.initial_feeds.value_or(std::vector<Feed>{});
    *out = std::make_unique<Vm>(feeds);
    return true;
}

// What a block CARRIES. The chain has one block type, so naming the type would
// compare a constant against itself.
std::string kind_of(const Block& b) {
    std::vector<std::string> parts;
    const auto add = [&](const char* name, std::size_t n) {
        if (n == 0) return;
        if (n == 1) {
            parts.emplace_back(name);
            return;
        }
        parts.emplace_back(std::string(name) + "x" + std::to_string(n));
    };
    add("Observation", b.observations.has_value() ? b.observations->size() : 0);
    add("Aggregation", b.aggregations.has_value() ? b.aggregations->size() : 0);
    add("FeedUpdate", b.feed_updates.has_value() ? b.feed_updates->size() : 0);
    add("Attestation", b.attestations.has_value() ? b.attestations->size() : 0);
    if (parts.empty()) return "Empty";
    std::string out = parts[0];
    for (std::size_t i = 1; i < parts.size(); i++) out += "+" + parts[i];
    return out;
}

Row identity(const std::string& id) {
    Id chain{};
    chain.fill(kChainByte);
    Row r(id);
    r.parse = "ok";
    r.kind = "ChainIdentity";
    r.hash = lux::fhevm::hex(chain);
    r.syntactic = "network=" + std::to_string(kNetworkId);
    r.exec = kOk;
    r.note = "the identity this evaluator derives ids under";
    return r;
}

Row block_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    json::Value v;
    std::string err;
    if (!parse_value(bytes, &v, &err)) return r.malformed(trim(err));
    Block b;
    if (!Block::read(v, &b, &err)) return r.malformed(trim(err));
    r.parse = "ok";
    r.kind = kind_of(b);
    r.hash = lux::fhevm::hex(b.compute_id());
    // The reference's Verify is unconditional, so a block that parses is a
    // block this chain accepts. Anything else here would be this port's
    // opinion rather than the chain's.
    r.syntactic = kOk;
    r.exec = kOk;
    char note[192];
    std::snprintf(note, sizeof(note), "height=%llu parent=%s obs=%zu agg=%zu feeds=%zu att=%zu",
                  static_cast<unsigned long long>(b.height),
                  lux::fhevm::hex(lux::fhevm::ByteView(b.parent_id.data(), 4)).c_str(),
                  b.observations.has_value() ? b.observations->size() : 0,
                  b.aggregations.has_value() ? b.aggregations->size() : 0,
                  b.feed_updates.has_value() ? b.feed_updates->size() : 0,
                  b.attestations.has_value() ? b.attestations->size() : 0);
    r.note = note;
    return r;
}

Row genesis_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    json::Value v;
    std::string err;
    if (!parse_value(bytes, &v, &err)) return r.malformed(trim(err));
    Genesis g;
    if (!Genesis::read(v, &g, &err)) return r.malformed(trim(err));
    r.parse = "ok";
    r.kind = "Genesis";
    const std::string again = g.write();
    r.hash = lux::fhevm::hex(lux::fhevm::sha256(lux::fhevm::view(std::string_view(again))));
    r.syntactic =
        "feeds=" + std::to_string(g.initial_feeds.has_value() ? g.initial_feeds->size() : 0);
    r.exec = kOk;
    r.note = "timestamp=" + std::to_string(g.timestamp) + " message=" +
             json::escape_string(g.message);
    return r;
}

std::vector<std::string> split(const std::string& s, char sep) {
    std::vector<std::string> out;
    std::string cur;
    for (const char c : s) {
        if (c == sep) {
            out.push_back(cur);
            cur.clear();
        } else {
            cur.push_back(c);
        }
    }
    out.push_back(cur);
    return out;
}

bool parse_u32(const std::string& s, std::uint32_t* out) {
    if (s.empty()) return false;
    std::uint64_t v = 0;
    for (const char c : s) {
        if (c < '0' || c > '9') return false;
        v = v * 10 + static_cast<std::uint64_t>(c - '0');
        if (v > 0xFFFFFFFFull) return false;
    }
    *out = static_cast<std::uint32_t>(v);
    return true;
}

// One 32-byte id, hex, the way the corpus writes a derivation's arguments.
bool arg_id(const std::string& s, Id* out) {
    lux::fhevm::Bytes raw;
    if (!lux::fhevm::from_hex(s, &raw)) return false;
    if (raw.size() != 32) return false;
    std::copy(raw.begin(), raw.end(), out->begin());
    return true;
}

bool parse_u64(const std::string& s, std::uint64_t* out) {
    if (s.empty()) return false;
    std::uint64_t v = 0;
    for (const char c : s) {
        if (c < '0' || c > '9') return false;
        const std::uint64_t d = static_cast<std::uint64_t>(c - '0');
        if (v > (~std::uint64_t(0) - d) / 10) return false;
        v = v * 10 + d;
    }
    *out = v;
    return true;
}

Row artifact_id_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    const std::string text(reinterpret_cast<const char*>(bytes.data()), bytes.size());
    const std::vector<std::string> f = split(text, '|');
    if (f.size() != 2) {
        return r.malformed("an artifact id takes two arguments, got " + std::to_string(f.size()));
    }
    Id feed{};
    if (!arg_id(f[0], &feed)) return r.malformed("an id is 32 bytes of hex");
    std::uint64_t epoch = 0;
    if (!parse_u64(f[1], &epoch)) return r.malformed("epoch is not a uint64");
    r.parse = "ok";
    r.kind = "OracleAttestation";
    r.hash = lux::fhevm::hex(lux::fhevm::view(attestation_id(feed, epoch)));
    r.syntactic = kOk;
    r.exec = kOk;
    r.note = "epoch=" + std::to_string(epoch);
    return r;
}

Row request_id_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    const std::string text(reinterpret_cast<const char*>(bytes.data()), bytes.size());
    const std::vector<std::string> f = split(text, '|');
    if (f.size() != 5) {
        return r.malformed("a request id takes five arguments, got " + std::to_string(f.size()));
    }
    Id ids_[3];
    for (int i = 0; i < 3; i++) {
        if (!arg_id(f[static_cast<std::size_t>(i)], &ids_[i])) {
            return r.malformed("an id is 32 bytes of hex");
        }
    }
    std::uint32_t step = 0;
    std::uint32_t retry = 0;
    if (!parse_u32(f[3], &step)) return r.malformed("step is not a uint32");
    if (!parse_u32(f[4], &retry)) return r.malformed("retry is not a uint32");
    r.parse = "ok";
    r.kind = "RequestID";
    r.hash = lux::fhevm::hex(
        lux::fhevm::view(compute_request_id(ids_[0], ids_[1], ids_[2], step, retry)));
    r.syntactic = kOk;
    r.exec = kOk;
    r.note = "step=" + std::to_string(step) + " retry=" + std::to_string(retry);
    return r;
}

Row request_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    json::Value v;
    std::string err;
    if (!parse_value(bytes, &v, &err)) return r.malformed(trim(err));
    OracleRequest q;
    if (!OracleRequest::read(v, &q, &err)) return r.malformed(trim(err));
    r.parse = "ok";
    r.kind = q.kind == kKindWrite ? "Write" : (q.kind == kKindRead ? "Read" : "unknown");
    r.hash = lux::fhevm::hex(lux::fhevm::view(q.request_id));
    std::unique_ptr<Vm> vm;
    if (!seeded_vm(&vm, &err)) return r.internal(trim(err));
    std::string why;
    if (!vm->register_request(q, &why)) {
        const char* cls = classify(why);
        // The one refusal on this chain reached before the chain is read
        // answers both halves; everything else leaves syntactic OK.
        r.syntactic = why.rfind("invalid request_id", 0) == 0 ? cls : kOk;
        r.exec = cls;
        r.note = trim(why);
        return r;
    }
    r.syntactic = kOk;
    r.exec = kOk;
    r.note = "executors=" +
             std::to_string(q.executors.has_value() ? q.executors->size() : 0) +
             " deadline=" + std::to_string(q.deadline_height);
    return r;
}

Row commit_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    json::Value v;
    std::string err;
    if (!parse_value(bytes, &v, &err)) return r.malformed(trim(err));
    json::Reader run(v, {"request", "records"}, &err, json::Unknown::Ignore);
    if (!run.ok()) return r.malformed(trim(err));
    const json::Value* rq = run.find("request");
    if (rq == nullptr || rq->null()) return r.malformed("a commit run carries a request");
    OracleRequest q;
    if (!OracleRequest::read(*rq, &q, &err)) return r.malformed(trim(err));
    std::vector<OracleRecord> records;
    const json::Value* rs = run.find("records");
    if (rs != nullptr && !rs->null()) {
        if (rs->kind != json::Kind::Array) return r.malformed("records is not a list");
        for (const auto& e : rs->array) {
            OracleRecord rec;
            if (!OracleRecord::read(e, &rec, &err)) return r.malformed(trim(err));
            records.push_back(std::move(rec));
        }
    }
    r.parse = "ok";
    r.kind = "OracleCommit";
    r.syntactic = "records=" + std::to_string(records.size());

    std::unique_ptr<Vm> vm;
    if (!seeded_vm(&vm, &err)) return r.internal(trim(err));
    std::string why;
    if (!vm->register_request(q, &why)) {
        r.exec = classify(why);
        r.note = trim(why);
        return r;
    }
    for (const auto& rec : records) {
        if (!vm->submit_record(rec, &why)) {
            r.exec = classify(why);
            r.note = trim(why);
            return r;
        }
    }
    Commit c;
    if (!vm->commit_records(q.request_id, &c, &why)) {
        r.exec = classify(why);
        r.note = trim(why);
        return r;
    }
    r.hash = lux::fhevm::hex(lux::fhevm::view(c.root));
    r.exec = kOk;
    r.note = "count=" + std::to_string(c.count) + " window=[" + std::to_string(c.window_start) +
             "," + std::to_string(c.window_end) + "]";
    return r;
}

Row observation_row(const std::string& id, const lux::fhevm::Bytes& bytes) {
    Row r(id);
    json::Value v;
    std::string err;
    if (!parse_value(bytes, &v, &err)) return r.malformed(trim(err));
    Observation obs;
    if (!Observation::read(v, &obs, &err)) return r.malformed(trim(err));
    r.parse = "ok";
    r.kind = "Observation";
    r.hash = lux::fhevm::hex(obs.feed_id);
    r.syntactic = kOk;
    std::unique_ptr<Vm> vm;
    if (!seeded_vm(&vm, &err)) return r.internal(trim(err));
    std::string why;
    if (!vm->submit_observation(obs, &why)) {
        r.exec = classify(why);
        r.note = trim(why);
        return r;
    }
    r.exec = kOk;
    r.note = "accepted into the pending set";
    return r;
}

Row evaluate(const std::string& id, const std::string& op, const std::string& wire) {
    lux::fhevm::Bytes bytes;
    if (!from_hex_wire(wire, &bytes)) return Row(id).internal("corpus wire is not hex");
    if (op == "identity") return identity(id);
    if (op == "block") return block_row(id, bytes);
    if (op == "genesis") return genesis_row(id, bytes);
    if (op == "artifactid") return artifact_id_row(id, bytes);
    if (op == "requestid") return request_id_row(id, bytes);
    if (op == "request") return request_row(id, bytes);
    if (op == "commit") return commit_row(id, bytes);
    if (op == "observation") return observation_row(id, bytes);
    return Row(id).internal("unknown O op " + op);
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: %s <vectors.tsv> [repeats]\n", argv[0]);
        return 2;
    }
    std::uint64_t repeats = 0;
    if (argc > 2) {
        repeats = std::strtoull(argv[2], nullptr, 10);
        if (repeats < 1) {
            std::fprintf(stderr, "usage: %s <vectors.tsv> [repeats]\n", argv[0]);
            return 2;
        }
    }

    std::ifstream in(argv[1]);
    if (!in) {
        std::fprintf(stderr, "conformance: %s: cannot read\n", argv[1]);
        return 1;
    }

    struct Vector {
        std::string id, op, wire;
    };
    std::vector<Vector> vectors;
    std::string line;
    while (std::getline(in, line)) {
        if (line.empty() || line[0] == '#') continue;
        const std::vector<std::string> f = split(line, '\t');
        if (f.size() != 5 || f[0] != "V") {
            std::fprintf(stderr, "conformance: not a vector line: %s\n", line.c_str());
            return 1;
        }
        // This evaluator is the O-chain; the other chains' vectors are theirs.
        if (f[2] != "O") continue;
        vectors.push_back({f[1], f[3], f[4]});
    }

    std::vector<Row> rows;
    const auto start = std::chrono::steady_clock::now();
    for (std::uint64_t round = 0; round < (repeats > 0 ? repeats : 1); round++) {
        rows.clear();
        rows.reserve(vectors.size());
        for (const auto& v : vectors) rows.push_back(evaluate(v.id, v.op, v.wire));
    }
    const double elapsed =
        std::chrono::duration<double>(std::chrono::steady_clock::now() - start).count();

    std::string out;
    for (const auto& r : rows) {
        out += r.line();
        out.push_back('\n');
    }
    std::fwrite(out.data(), 1, out.size(), stdout);
    if (repeats > 0) {
        std::fprintf(stderr, "B\tcpp/oraclevm\t%zu\t%llu\t%.6f\n", vectors.size(),
                     static_cast<unsigned long long>(repeats), elapsed);
    }
    return 0;
}
