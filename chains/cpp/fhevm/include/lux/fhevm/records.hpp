// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// records.hpp — everything the F-Chain persists, and the derivations that name
// it.
//
// CIPHERTEXT-BODY INVARIANT (the F-Chain's reason to exist).
//
// F-Chain is the COORDINATION plane for confidential compute. It stores the
// PUBLIC coordinates of an encrypted value — a 32-byte handle, the digest of
// the ciphertext body, who owns it, who may act on it, and which threshold
// decryptions were asked for and answered. It never stores a ciphertext body,
// never holds the FHE secret key, and never holds a decryption share: the
// bodies live in off-chain storage and the key shares live on the threshold
// committee, whose members are named here only by public node id and public
// signing key.
//
// Three things hold that line, because none of them holds it alone. The four
// record types below are everything F persists, and every field of them is a
// public coordinate: hashes, addresses, bitmasks, sizes, epochs, timestamps —
// invariant_test pins that field list exactly. A transaction's payload decodes
// as EXACTLY its schema, so a member the schema does not describe is refused
// rather than ignored. And the bytes a transaction does carry are bounded and
// priced by the byte, so bulk is refused before it is stored.
//
// The records mirror the FHE runtime's own types (Go: github.com/luxfi/chains/
// mpcvm/fhe) so the chain and the runtime speak one vocabulary. F owns the
// PERSISTENCE, because persistence in a chain is consensus: every field F
// writes comes from the accepting block and never from a validator's wall
// clock, so two validators replaying one block write the same bytes.

#pragma once

#include "lux/fhevm/id.hpp"

#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

namespace lux::fhevm {

// Permit lifecycle states. A permit is the only revocable thing on F: a
// registration is a fact about a ciphertext that exists, and a fact does not
// stop being true, whereas the authority to act on it can be withdrawn at any
// time.
inline constexpr std::string_view kStatusActive = "active";
inline constexpr std::string_view kStatusRevoked = "revoked";

// The capability bits a permit may confer (Go: fhe.PermitOp*).
inline constexpr std::uint32_t kPermitOpDecrypt = 1u << 0;
inline constexpr std::uint32_t kPermitOpReencrypt = 1u << 1;
inline constexpr std::uint32_t kPermitOpCompute = 1u << 2;
inline constexpr std::uint32_t kPermitOpTransfer = 1u << 3;
// permitOpMask is every capability bit the FHE runtime defines. A grant that
// sets a bit outside it is refused rather than silently conferring nothing.
inline constexpr std::uint32_t kPermitOpMask =
    kPermitOpDecrypt | kPermitOpReencrypt | kPermitOpCompute | kPermitOpTransfer;

enum class RequestStatus : std::uint8_t {
    Pending = 0,
    Processing,
    Completed,
    Failed,
    Expired,
};
std::string_view request_status_string(RequestStatus s);

enum class EpochStatus : std::uint8_t {
    Active = 0,
    Ended,
    Pending,
};

// CommitteeMember is one seat: a public node id, the public signing key that
// votes from it, and the weight and index the DKG assigned.
struct CommitteeMember {
    NodeId node_id{};
    Bytes public_key;
    std::uint64_t weight = 0;
    std::int64_t index = 0;
    // public_key_nil distinguishes a Go nil slice from an empty one, because
    // the two marshal differently (null vs "").
    bool public_key_nil = true;

    bool operator==(const CommitteeMember&) const = default;
};

// CiphertextRecord is F's record of one registered encrypted value. The body it
// describes is off-chain; digest binds this record to it, and handle — derived
// from digest and scheme — is the name every other operation uses for it.
struct CiphertextRecord {
    Id handle{};
    Account owner{};
    std::uint8_t type = 0;
    std::int64_t level = 0;
    std::uint64_t epoch = 0;
    std::int64_t registered_at = 0;
    std::uint32_t size = 0;
    Id chain_id{};
    std::string scheme;
    Id digest{};

    bool operator==(const CiphertextRecord&) const = default;
};

// PermitRecord is a capability: the grantor lets the grantee perform a set of
// operations on one handle until expiry. It is the only thing that authorizes a
// decryption request, so it is checked at admission, in consensus, and again at
// application.
struct PermitRecord {
    Id permit_id{};
    Id handle{};
    Account grantee{};
    Account grantor{};
    std::uint32_t operations = 0;
    std::int64_t expiry = 0;
    std::int64_t created_at = 0;
    Bytes attestation;
    Id chain_id{};
    std::string status;

    bool operator==(const PermitRecord&) const = default;
};

// Attestation is one committee member's vote for one 32-byte value. It backs
// both of F's threshold decisions — which plaintext handle a decryption
// produced, and which committee the next epoch has — because both are the same
// question: did threshold distinct members say the same thing?
struct Attestation {
    Account member{};
    Id value{};

    bool operator==(const Attestation&) const = default;
};

// DecryptRecord is a threshold-decryption request and the committee's answer to
// it. F does not decrypt: the committee combines its shares off-chain and each
// member ATTESTS the resulting public handle here. The request completes when
// threshold distinct members attest the SAME result, which is why attestations
// is a list of (member, result) pairs and not a single field — a lone member
// posting a wrong result buys nothing but its own burnt fee.
struct DecryptRecord {
    Id request_id{};
    Id ciphertext_handle{};
    Account requester{};
    std::array<std::uint8_t, 20> callback{};
    std::array<std::uint8_t, 4> callback_selector{};
    Id source_chain{};
    std::uint64_t epoch = 0;
    std::uint64_t nonce = 0;
    std::int64_t expiry = 0;
    RequestStatus status = RequestStatus::Pending;
    std::int64_t created_at = 0;
    std::int64_t completed_at = 0;
    Id result_handle{};
    std::string error;
    Id permit_id{};
    std::vector<Attestation> attestations;

    bool operator==(const DecryptRecord&) const = default;
};

// EpochRecord is the committee that holds the FHE key shares for one epoch,
// together with the network public key they jointly generated. Epoch 0 comes
// from genesis; every later epoch is installed once threshold members of the
// CURRENT committee attest the same successor.
struct EpochRecord {
    std::uint64_t epoch = 0;
    std::int64_t start_time = 0;
    std::int64_t end_time = 0;
    std::vector<CommitteeMember> committee;
    std::int64_t threshold = 0;
    Bytes public_key;
    bool public_key_nil = true;
    EpochStatus status = EpochStatus::Active;
    std::vector<Attestation> attestations;
    // committee_nil keeps a Go nil slice distinguishable from an empty one.
    bool committee_nil = true;

    // member_of reports whether acct holds a seat. A member's F-Chain account is
    // derived from its PUBLIC signing key by exactly the derivation that
    // authenticates a payer, so committee membership and payer identity cannot
    // disagree.
    bool member_of(const Account& acct) const;
};

// ---- threshold arithmetic ---------------------------------------------------

// tally counts how many DISTINCT members attested value, which is the only
// question a threshold decision asks.
std::int64_t tally(const std::vector<Attestation>& as, const Id& value);

// vote records member's choice, REPLACING whatever it chose before. One member,
// one vote — so a member can neither raise a value's count by repeating itself
// nor hedge across two values — but a vote is not spent by being cast. It used
// to be spent, and a committee that split its vote could then never converge.
void vote(std::vector<Attestation>& as, const Account& member, const Id& value);

// committee_order is the canonical ordering of a committee: ascending node id.
// A committee is hashed to decide an epoch advance, so two members proposing the
// same set must produce the same bytes — the order is part of the value.
bool committee_order(const std::vector<CommitteeMember>& c);

// ---- derivations ------------------------------------------------------------

// address_of derives an account from an ML-DSA public key. F is internally
// consistent: it derives the same address it checks a payer against, and the
// same address it recognises a committee member by. A PUBLIC, one-way
// derivation — no secret involved.
Account address_of(ByteView public_key);

// committee_digest is the value committee members attest when advancing an
// epoch. It hashes the SEMANTIC proposal rather than the transaction's payload
// bytes, so two members whose clients encode the same proposal differently
// still vote for the same thing.
Id committee_digest(std::uint64_t epoch, std::int64_t threshold, ByteView public_key,
                    const std::vector<CommitteeMember>& c);

// derive_handle names a ciphertext by its CONTENT: the digest of the off-chain
// body under a given scheme. Two registrations of the same body under the same
// scheme collide by construction and the second is refused, and a handle cannot
// be squatted by someone who does not have the body to hash.
Id derive_handle(const Id& digest, std::string_view scheme);

// derive_permit_id names a grant by everything that distinguishes it, including
// the grantor's nonce, so a grantor may re-grant the same capability later
// without colliding with the earlier permit.
Id derive_permit_id(const Id& handle, const Account& grantor, const Account& grantee,
                    std::uint32_t ops, std::int64_t expiry, std::uint64_t nonce);

// derive_request_id names a decryption request by handle, requester and the
// requester's nonce — all fields the payer signed — so the id is deterministic
// across validators and unique per request.
Id derive_request_id(const Id& handle, const Account& requester, std::uint64_t nonce);

// ---- persistence: the records as the Go chain writes and reads them ---------

std::string marshal(const CiphertextRecord& r);
std::string marshal(const PermitRecord& r);
std::string marshal(const DecryptRecord& r);
std::string marshal(const EpochRecord& r);

bool unmarshal(std::string_view json, CiphertextRecord* out, std::string* err);
bool unmarshal(std::string_view json, PermitRecord* out, std::string* err);
bool unmarshal(std::string_view json, DecryptRecord* out, std::string* err);
bool unmarshal(std::string_view json, EpochRecord* out, std::string* err);

// A committee member is written and read on its own too: it travels inside an
// epoch-advance payload as well as inside an epoch record, and the two must
// agree byte for byte.
namespace json {
class Writer;
struct Value;
}  // namespace json
void write_member(json::Writer& w, const CommitteeMember& m);
bool read_member(const json::Value& v, CommitteeMember* out, std::string* err);
// write_attestations / read_attestations are shared by the decrypt record and
// the epoch record, which tally the same votes.
void write_attestations(json::Writer& w, const std::vector<Attestation>& as);
bool read_attestations(const json::Value* v, std::vector<Attestation>* out, std::string* err);

}  // namespace lux::fhevm
