// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs.hpp — the Z-Chain's one transaction, and the identity it is decided by.
//
// Note construction — deriving a nullifier, committing to a note, encrypting
// one to a recipient — is a WALLET's work and is not here. A validator holds no
// spending keys and builds no notes: it checks proofs and the spent set. What
// it needs of a note is the commitment and the nullifier the transaction
// already carries.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/wire.hpp"

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

namespace lux::zkvm {

// MaxTxSize bounds a transaction on the way in, so a peer cannot make this node
// hold what it would never build.
inline constexpr std::size_t kMaxTxSize = 1u << 20;

enum class TxType : std::uint8_t {
    Transfer = 0,
    Mint = 1,
    Burn = 2,
    Shield = 3,    // transparent -> shielded
    Unshield = 4,  // shielded -> transparent
};

// The verifying-key map and the "no key for this circuit" refusal are keyed by
// the SAME string Go keys them by: string(TransactionType), which is the single
// byte of the type value. It is a byte, not a name, in both.
inline std::string circuit_key(TxType t) {
    return std::string(1, char(static_cast<std::uint8_t>(t)));
}

struct TransparentInput {
    Id tx_id{};
    std::uint32_t output_idx = 0;
    std::uint64_t amount = 0;
    Bytes address;

    bool operator==(const TransparentInput&) const = default;
};

struct TransparentOutput {
    std::uint64_t amount = 0;
    Id asset_id{};
    Bytes address;

    bool operator==(const TransparentOutput&) const = default;
};

struct ShieldedOutput {
    Bytes commitment;
    Bytes encrypted_note;
    Bytes ephemeral_pubkey;
    Bytes output_proof;

    bool operator==(const ShieldedOutput&) const = default;
};

struct ZkProof {
    std::string proof_type;  // "groth16", "plonk", "stark", ...
    Bytes proof_data;
    std::vector<Bytes> public_inputs;

    bool operator==(const ZkProof&) const = default;
};

// Errors ValidateBasic reports, spelled as Go spells them.
inline constexpr const char* kErrInvalidTxType = "invalid transaction type";
inline constexpr const char* kErrNoInputs = "transaction has no inputs";
inline constexpr const char* kErrNoOutputs = "transaction has no outputs";
inline constexpr const char* kErrMissingProof = "transaction missing proof";
inline constexpr const char* kErrNoExpiry = "transaction names no expiry height";
inline constexpr const char* kErrExpired = "transaction has expired";
inline constexpr const char* kErrInvalidTransfer = "invalid transfer transaction";
inline constexpr const char* kErrInvalidShield = "invalid shield transaction";
inline constexpr const char* kErrInvalidUnshield = "invalid unshield transaction";

struct Transaction {
    // id is DERIVED, never read off the wire. It is not a field of the frame:
    // an identity a peer supplies is an identity a peer chooses, and the proof
    // cache is keyed on it.
    Id id{};

    TxType type = TxType::Transfer;
    std::uint8_t version = 0;

    std::vector<TransparentInput> transparent_inputs;
    std::vector<TransparentOutput> transparent_outputs;

    std::vector<Bytes> nullifiers;
    std::vector<ShieldedOutput> outputs;

    std::optional<ZkProof> proof;

    std::uint64_t fee = 0;
    std::uint64_t expiry = 0;  // block height
    Bytes memo;

    // compute_id is the transaction's identity: a hash over everything the
    // transaction means.
    //
    // Every variable-length field is written with its length first and every
    // list with its count. Concatenated raw, a byte could move from the end of
    // one field to the start of the next without the hash noticing —
    // ["ab","c"] and ["a","bc"] are the same bytes — and two transactions
    // sharing an identity is what consensus decides between blocks with.
    Id compute_id() const;

    // output_commitments is what the proof's public inputs are checked against,
    // in output order.
    std::vector<Bytes> output_commitments() const;

    // validate_basic is the shape check: type in range, something spent,
    // something created, a proof present, an expiry named, and the per-type
    // rule for transfer / shield / unshield.
    wire::Result<void> validate_basic() const;

    Bytes marshal() const;

    bool operator==(const Transaction&) const = default;
};

// parse_transaction decodes ONE canonical transaction frame. The identity is
// derived on the way in; the frame carries none.
wire::Result<Transaction> parse_transaction(ByteView data);

// The sub-frames, exposed because a wire format is only pinned by tests that
// can state it.
Bytes marshal_transparent_input(const TransparentInput& t);
wire::Result<TransparentInput> parse_transparent_input(ByteView data);
Bytes marshal_transparent_output(const TransparentOutput& t);
wire::Result<TransparentOutput> parse_transparent_output(ByteView data);
Bytes marshal_shielded_output(const ShieldedOutput& s);
wire::Result<ShieldedOutput> parse_shielded_output(ByteView data);
// An absent proof is the EMPTY byte string, and only that: a present proof
// always has a frame, so "no proof" and "a proof of nothing" are distinct.
Bytes marshal_zkproof(const std::optional<ZkProof>& z);
wire::Result<std::optional<ZkProof>> parse_zkproof(ByteView data);

// Field offsets, shared with the tests that hand-build a hostile frame.
inline constexpr int kTiSize = 52;
inline constexpr int kToSize = 48;
inline constexpr int kSoSize = 32;
inline constexpr int kZkpSize = 32;
inline constexpr int kTxSize = 98;

}  // namespace lux::zkvm
