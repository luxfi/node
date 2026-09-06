// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// transaction.hpp — the six operations that are the whole life of a
// confidential value on F, and the rules each of them has to satisfy.
//
// A value is registered, capabilities over it are granted and revoked, its
// decryption is requested, and the committee answers; the committee that
// answers is itself rotated by the sixth. Each is a MUTATING operation that may
// only take effect through a fee-settled consensus block — never through a
// synchronous RPC.
//
// THE SIGNING ARCHITECTURE, which is where the security actually is:
//
//   - content() is one ZAP object binding every semantically-meaningful field
//     (type, payer, subject, gas limit, nonce, scheme, payload) and EXCLUDING
//     auth/sig. Because payer is bound here and authenticate() requires
//     payer == address_of(auth), an attacker cannot swap in a different public
//     key. And because subject is bound here — and syntactic_verify requires it
//     to equal what the payload derives — the signature covers the OBJECT acted
//     on, not merely the arguments that produce it.
//
//   - signing_bytes(chain) is the preimage the payer SIGNS: the content, bound
//     to the ONE chain it is meant for. The chain id does not travel — each
//     side supplies its own — so a transaction signed for the testnet F-Chain
//     cannot authenticate on the mainnet one. Without that binding a captured
//     transaction replays verbatim onto every other F-Chain, where the same
//     payer address exists, burning its balance there for an operation it never
//     asked for.
//
//   - bytes() is content() followed by an appended object carrying auth and
//     sig, and id() is sha256 of it. What is authenticated is exactly what is
//     transmitted, and parse_transaction accepts ONLY input byte-identical to
//     what the parsed fields re-serialize to: exactly one byte-string decodes
//     to each transaction, so an id cannot be made malleable.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/id.hpp"
#include "lux/fhevm/records.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace lux::fhevm {

class VM;

// The six operations.
inline constexpr std::uint8_t kTxRegisterCiphertext = 1;  // record a ciphertext's PUBLIC handle
inline constexpr std::uint8_t kTxGrantPermit = 2;         // owner grants a capability
inline constexpr std::uint8_t kTxRevokePermit = 3;        // owner withdraws one it granted
inline constexpr std::uint8_t kTxRequestDecrypt = 4;      // permitted grantee asks for a decryption
inline constexpr std::uint8_t kTxFulfillDecrypt = 5;      // committee member attests the result
inline constexpr std::uint8_t kTxAdvanceEpoch = 6;        // committee installs its successor

// Size bounds. Every one of these is a byte channel onto a chain that keeps
// what it is given, so each is bounded before anything is decoded and priced
// once it is.
//
//   kMaxPayload        set by the largest legitimate payload — a full-committee
//                      epoch proposal, whose members each carry an ML-DSA-65
//                      public key — with room to spare.
//   kMaxScheme         scheme names are short labels ("ckks-n14"); anything
//                      longer is a channel, not a name.
//   kMaxCommittee      bounds the epoch payload, and with it the public-key
//                      parsing an UNAUTHENTICATED transaction can demand before
//                      its own signature is checked.
//   kMaxCiphertextSize the body is not stored here, but a size nobody could
//                      ever serve describes nothing.
inline constexpr std::size_t kMaxPayload = 128 * 1024;
inline constexpr std::size_t kMaxScheme = 32;
inline constexpr std::size_t kMaxCommittee = 32;
inline constexpr std::uint32_t kMaxCiphertextSize = 1u << 30;

// kDefaultRequestWindow is how long a decryption request stays answerable when
// the requester names no expiry. A request no committee can still answer is
// dead weight in state, and an unbounded one would be answerable by a future
// committee that never saw the permit that authorized it.
inline constexpr std::int64_t kDefaultRequestWindow = 3600;

// ---- operation payloads. All fields are PUBLIC. -----------------------------

// RegisterPayload records an encrypted value's PUBLIC coordinates. digest is
// the hash of the ciphertext BODY, which lives in off-chain storage — F stores
// the hash so anyone can check a fetched body against the chain, and never the
// body itself.
struct RegisterPayload {
    Id digest{};
    std::uint8_t type = 0;   // FHE plaintext type tag (bool, uint8, …)
    std::int64_t level = 0;  // remaining multiplicative level
    std::uint32_t size = 0;  // ciphertext body size in bytes
};

// GrantPayload grants a capability over one handle to one grantee until expiry.
// operations is a bitmask of kPermitOp* values.
struct GrantPayload {
    Account grantee{};
    std::uint32_t operations = 0;
    std::int64_t expiry = 0;  // unix seconds; 0 = no expiry
};

// RevokePayload withdraws a permit.
struct RevokePayload {
    std::string reason;
};

// RequestPayload asks the committee to threshold-decrypt a handle under the
// authority of a permit the requester holds. callback and selector name where
// the answer should be delivered on the source chain.
struct RequestPayload {
    Id permit_id{};
    std::array<std::uint8_t, 20> callback{};
    std::array<std::uint8_t, 4> selector{};
    std::int64_t expiry = 0;  // unix seconds; 0 = the chain's default window
};

// FulfillPayload is one committee member's attestation of the PUBLIC handle a
// threshold decryption produced. The plaintext itself is delivered off-chain to
// the callback; F records only which handle the committee agreed on.
struct FulfillPayload {
    Id result{};
};

// AdvancePayload proposes the next epoch's committee and the network public key
// it jointly generated. Every approving member sends the identical proposal;
// the epoch installs when threshold of the CURRENT committee have.
struct AdvancePayload {
    std::uint64_t epoch = 0;
    std::vector<CommitteeMember> committee;
    std::int64_t threshold = 0;
    Bytes public_key;
    bool public_key_nil = true;
    bool committee_nil = true;
};

// The payload encoders write exactly what Go's json.Marshal writes, so a client
// built on either side produces the same bytes and therefore the same subject.
std::string marshal(const RegisterPayload& p);
std::string marshal(const GrantPayload& p);
std::string marshal(const RevokePayload& p);
std::string marshal(const RequestPayload& p);
std::string marshal(const FulfillPayload& p);
std::string marshal(const AdvancePayload& p);

// The decoders accept EXACTLY the schema: a member the schema does not
// describe, or bytes after the value, is refused rather than ignored.
Result<RegisterPayload> decode_register(ByteView payload);
Result<GrantPayload> decode_grant(ByteView payload);
Result<RevokePayload> decode_revoke(ByteView payload);
Result<RequestPayload> decode_request(ByteView payload);
Result<FulfillPayload> decode_fulfill(ByteView payload);
Result<AdvancePayload> decode_advance(ByteView payload);

// ---- the transaction ---------------------------------------------------------

struct Transaction {
    std::uint8_t type = 0;
    std::string scheme;   // FHE scheme + ring dimension; "" for scheme-independent ops
    Account payer{};      // fee payer + authorization subject (public address)
    Id subject{};         // the object this operation acts on or creates
    std::uint64_t gas_limit = 0;
    std::uint64_t nonce = 0;
    Bytes payload;  // op-specific PUBLIC encoding
    Bytes auth;     // payer ML-DSA-65 PUBLIC key
    Bytes sig;      // payer signature over signing_bytes()

    // subject names the object of the operation, and is ALWAYS checked against
    // what the payload derives — so the signature covers the object, not just
    // the arguments that happen to produce it:
    //
    //   register  the handle the payload's digest+scheme derive
    //   grant     the handle being granted over
    //   revoke    the permit being withdrawn
    //   request   the handle whose decryption is asked for
    //   fulfill   the request being answered
    //   advance   the digest of the successor committee being approved

    Id id() const;
    Bytes content() const;
    Bytes signing_bytes(const Id& chain) const;
    Bytes bytes() const;

    // syntactic_verify checks the transaction is well-formed and priceable,
    // without any state. It rejects unknown types, unpriceable schemes,
    // undecodable or out-of-range payloads, and any subject that disagrees with
    // what the payload derives — all fail-closed.
    Result<void> syntactic_verify() const;

    // authenticate verifies the payer authorized this transaction ON THIS
    // CHAIN. PUBLIC ONLY: parse the payer's key, require it hashes to payer,
    // verify the signature over signing_bytes(chain). chain is the verifying
    // node's OWN chain id, never one the transaction carries.
    Result<void> authenticate(const Id& chain) const;

    // effect names, in 32 bytes, the state entry this transaction writes — and,
    // where an entry legitimately takes a write from each of several actors,
    // which actor writes it. Two transactions sharing an effect cannot both
    // take place: the second would find the ciphertext already registered, the
    // permit already withdrawn, or the member already counted, and abort the
    // block that carried them both.
    //
    // Nonces do not catch that on their own: a payer's nonces n and n+1 are
    // both valid, so one payer can build two transactions that individually
    // pass every check and together spend a block on work only one of them can
    // do. Naming the effect lets admission refuse the second before it is
    // queued and lets consensus refuse a peer's block that contains both. One
    // function, both layers, no drift.
    Id effect() const;

    // invalidate_id drops the cached id, for a caller that has just changed a
    // field — signing is the only one that does.
    void invalidate_id() const { id_cached_ = false; }

private:
    mutable Id id_{};
    mutable bool id_cached_ = false;
};

// parse_transaction decodes a transaction from its wire encoding: the leading
// content object, then the appended auth/sig object. Canonical — it refuses
// trailing bytes and any encoding that is not the one these fields produce.
Result<Transaction> parse_transaction(ByteView data);

// validate_committee checks a committee is installable: non-empty, canonically
// ordered by node id, free of duplicates, with a real threshold and with every
// member carrying a parseable ML-DSA-65 public key. The last check is what
// stops F being wedged by an epoch whose members can never sign an attestation
// — the committee that cannot speak can never be replaced either. Genesis and
// the epoch-advance transaction both go through here, so the two can never
// disagree.
Result<void> validate_committee(const std::vector<CommitteeMember>& c, std::int64_t threshold,
                                ByteView public_key);

// check_auth is the single, read-only authorization predicate: it decides
// whether tx may take effect against the CURRENT committed state at time now.
// It mutates nothing. It is the one place F's access model is enforced, called
// at three layers so unauthorized transactions are rejected at the earliest
// gate and never charged: admission, consensus, and — as defense in depth —
// application.
Result<void> check_auth(const Transaction& tx, const VM& vm, std::int64_t now);

// apply is where authorization is DECIDED and where state changes. It runs
// inside block acceptance, writing through the VM's store so an effect commits
// atomically with the fee burn. now is the accepting block's unix time — every
// timestamp F stores comes from here, never from a validator's clock.
//
// It reports whether the transaction TOOK EFFECT. A transaction that fails
// check_auth REVERTS: the result is false with no error, state is untouched,
// and the caller still burns the fee and consumes the nonce. This is the only
// place the verdict is reached, so every validator reaches it from the same
// committed state in the same order. Deciding it earlier, in verify, would let
// a block pass consensus on state that an earlier transaction in the same block
// then changes — and a block every validator certifies and no validator can
// apply halts the chain.
//
// An error means the write itself failed, which no validator can proceed past.
Result<bool> apply(const Transaction& tx, VM& vm, std::int64_t now);

}  // namespace lux::fhevm
