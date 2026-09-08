// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// chain.hpp — the Lux O-Chain: what it holds, how each of those travels, and
// the three things it decides.
//
// WHERE THE CHAIN IS. `chains/oraclevm` is 213 lines and none of them are the
// chain: it re-exports `github.com/luxfi/oracle/vm`, which is 1628. This port
// is written from that source.
//
// THE WIRE IS JSON. `ParseBlock` is `json.Unmarshal` and `Bytes` is
// `json.Marshal`, so the O-chain's wire is whatever `encoding/json` writes for
// its structs, and the block id is the SHA-256 of the block marshalled AGAIN —
// not of the bytes it was handed. Every rendering below is therefore consensus
// and not decoration: field order, `omitempty`, CB58 for an id, base64 for a
// byte slice, a list of numbers for a fixed array, RFC 3339 for a time.
//
// WHAT IS REUSED. The JSON grammar and Go's rules on top of it, and the
// identifier renderings, are `lux::fhevm::json` and `lux::fhevm::id` — compiled
// from those files, not copied. They are chain-neutral (Go's decoder and the
// cb58 alphabet know nothing about F) and they have already been held against
// the Go reference by the F-chain's own differential rows, which is the whole
// reason to reach for them rather than write a second opinion about what
// `encoding/json` accepts. That they still live under the F-chain's name is a
// naming debt: the right home is a shared one, and moving them is a rename
// across fifteen of that chain's files.
//
// THE ATTESTATION'S TWO ADJACENT RULES. OracleAttestation is the artifact the
// O-chain hands the X-chain, and a block may carry a list of them. Its
// `valueCommitment` is a [32]byte tagged omitempty, and omitempty does NOTHING
// to a Go array — an array has no empty form — so it is written on every
// attestation, all zeros included, while the three byte SLICES beside it
// vanish when they are empty. Two rules in adjacent fields and only one fires.

#pragma once

#include "lux/fhevm/id.hpp"
#include "lux/fhevm/json.hpp"
#include "lux/oraclevm/gotime.hpp"

#include <cstdint>
#include <map>
#include <optional>
#include <string>
#include <vector>

namespace lux::oraclevm {

using lux::fhevm::Bytes;
using lux::fhevm::Id;
using lux::fhevm::NodeId;
namespace json = lux::fhevm::json;

// A Go []byte: absent means the nil slice, which writes as null.
using Slice = std::optional<Bytes>;

// id_string / id_from_string are the Id renderings Go's ids type marshals
// with: CB58, except for the thirteen ids that are thirty-one zero bytes and a
// chain letter, which travel as an alias carrying no checksum at all.
std::string id_string(const Id& id);
bool id_from_string(std::string_view s, Id* out, std::string* err);

struct Observation {
    Id feed_id{};
    Slice value;
    Time timestamp;
    std::array<std::uint8_t, 32> source_meta{};
    NodeId operator_id{};
    std::uint8_t scheme = 0;
    Slice signature;

    static bool read(const json::Value& v, Observation* out, std::string* err);
    void write(json::Writer* w) const;
};

struct AggregatedValue {
    Id feed_id{};
    std::uint64_t epoch = 0;
    Slice value;
    Time timestamp;
    std::int64_t observations = 0;
    Slice agg_proof;
    Slice quorum_cert;

    static bool read(const json::Value& v, AggregatedValue* out, std::string* err);
    void write(json::Writer* w) const;
};

struct Feed {
    Id id{};
    std::string name;
    std::string description;
    std::optional<std::vector<std::string>> sources;
    // A time.Duration, which is an int64 count of nanoseconds on the wire.
    std::int64_t update_freq = 0;
    std::array<std::uint8_t, 32> policy_hash{};
    std::optional<std::vector<NodeId>> operators;
    Time created_at;
    std::string status;
    std::optional<std::map<std::string, std::string>> metadata;

    static bool read(const json::Value& v, Feed* out, std::string* err);
    void write(json::Writer* w) const;
    bool admits(const NodeId& op) const;
};

struct Attestation {
    std::uint32_t version = 0;
    std::uint8_t sig_suite = 0;
    Id domain_id{};
    Id feed_id{};
    std::uint64_t epoch = 0;
    Slice value;
    std::array<std::uint8_t, 32> value_commitment{};
    Slice agg_proof;
    Slice quorum_cert;
    Time valid_from;
    Time valid_to;
    std::array<std::uint8_t, 32> policy_hash{};

    static bool read(const json::Value& v, Attestation* out, std::string* err);
    void write(json::Writer* w) const;
};

struct Block {
    Id id{};
    Id parent_id{};
    std::uint64_t height = 0;
    Time timestamp;
    std::optional<std::vector<Observation>> observations;
    std::optional<std::vector<AggregatedValue>> aggregations;
    std::optional<std::vector<Feed>> feed_updates;
    std::optional<std::vector<Attestation>> attestations;

    static bool read(const json::Value& v, Block* out, std::string* err);
    // The bytes Marshal writes. omitempty is spelled out rather than
    // approximated: an empty list is not written at all, so a block that
    // arrived carrying one takes the id of the block that never had it.
    std::string write() const;
    // The id the chain derives: marshal again, hash THAT.
    Id compute_id() const;
};

struct Genesis {
    std::int64_t version = 0;
    std::string message;
    std::int64_t timestamp = 0;
    std::optional<std::vector<Feed>> initial_feeds;

    static bool read(const json::Value& v, Genesis* out, std::string* err);
    std::string write() const;
};

inline constexpr std::uint8_t kKindWrite = 0;
inline constexpr std::uint8_t kKindRead = 1;

struct OracleRequest {
    std::array<std::uint8_t, 32> request_id{};
    Id service_id{};
    Id session_id{};
    std::uint32_t step = 0;
    std::uint32_t retry = 0;
    Id tx_id{};
    std::uint8_t kind = 0;
    Slice target;
    std::array<std::uint8_t, 32> payload_hash{};
    std::array<std::uint8_t, 32> schema_hash{};
    std::uint64_t deadline_height = 0;
    std::optional<std::vector<NodeId>> executors;
    Time created_at;
    std::uint8_t status = 0;

    static bool read(const json::Value& v, OracleRequest* out, std::string* err);
    bool admits(const NodeId& executor) const;
};

struct OracleRecord {
    std::array<std::uint8_t, 32> request_id{};
    NodeId executor{};
    std::uint64_t timestamp = 0;
    std::string endpoint;
    std::array<std::uint8_t, 32> body_hash{};
    std::uint32_t result_code = 0;
    Slice external_ref;
    std::uint8_t scheme = 0;
    Slice signature;

    static bool read(const json::Value& v, OracleRecord* out, std::string* err);
};

// ---- what the chain decides -------------------------------------------------

// The window an observation must be inside, in seconds: the chain's default
// configuration, `ObservationWindow: "1m"`, read against the wall clock.
inline constexpr std::int64_t kObservationWindowSeconds = 60;

// request_id = sha256("LUX:OracleRequest:v1" ‖ service ‖ session ‖ be32(step)
// ‖ be32(retry) ‖ tx).
//
// Nothing separates the three ids, so their ORDER is the whole of what keeps
// two requests apart, and the two counters are big-endian and not
// interchangeable. A request is minted on the P-chain and executed here, and
// both sides agree on WHICH request only because both derive these bytes.
std::array<std::uint8_t, 32> compute_request_id(const Id& service, const Id& session, const Id& tx,
                                                std::uint32_t step, std::uint32_t retry);

// The root over a request's records. A lone last leaf is paired WITH ITSELF, so
// a set of records and that set with its last member repeated commit to one
// root. That is the chain's tree and this port reproduces it: a port that
// corrected the malleability would derive a different root for every odd record
// count and fork the chain in the act of improving it.
std::array<std::uint8_t, 32> records_merkle_root(const std::vector<OracleRecord>& records);

struct Commit {
    std::array<std::uint8_t, 32> root{};
    std::uint32_t count = 0;
    std::uint64_t window_start = 0;
    std::uint64_t window_end = 0;
};

// An O-chain holding the feeds its genesis named and nothing else.
class Vm {
public:
    explicit Vm(const std::vector<Feed>& feeds);

    // The feed lookup is the chain's FIRST read, so every refusal here is past
    // the corpus's syntactic boundary.
    bool submit_observation(const Observation& obs, std::string* err);
    // The deterministic-id check runs before the request map is read, which is
    // why it is the one refusal on this chain that answers syntactic too.
    bool register_request(const OracleRequest& req, std::string* err);
    bool submit_record(const OracleRecord& rec, std::string* err);
    bool commit_records(const std::array<std::uint8_t, 32>& request_id, Commit* out,
                        std::string* err);

private:
    std::map<Id, Feed> feeds_;
    std::map<std::array<std::uint8_t, 32>, OracleRequest> requests_;
    std::map<std::array<std::uint8_t, 32>, std::vector<OracleRecord>> records_;
    // The height of the last accepted block. A chain seeded from genesis and
    // nothing else is at zero, which is what the deadline rule reads.
    std::uint64_t last_height_ = 0;
};

}  // namespace lux::oraclevm
