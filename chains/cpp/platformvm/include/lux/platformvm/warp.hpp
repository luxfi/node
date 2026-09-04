// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// warp.hpp — a message another chain signed, and the proof enough of it did.
//
// Rendered from Go vms/platformvm/warp (unsigned_message.go, message.go,
// signature.go, validator.go, wire.go).
//
// A warp message is how one chain tells another something it will act on. The
// proof is one aggregated BLS signature plus a bit vector saying which
// validators of the source chain contributed to it, so the cost of checking it
// is one pairing rather than one per signer — that is the whole reason the
// scheme exists.
//
// TWO THINGS ARE LOAD-BEARING AND BOTH ARE ABOUT WEIGHT, NOT SIGNATURES.
//
// The bit vector is a big-endian big integer, and a vector with a leading zero
// byte is REFUSED. Two byte strings that denote the same set would be two
// messages with one meaning, and the message id is a hash of the bytes.
//
// The total weight includes validators with NO key, while only keyed validators
// can sign. That is deliberate: a keyless validator's stake still counts toward
// what a quorum has to beat, so a chain cannot cheapen its own quorum by
// registering validators that cannot vote.
//
// WHAT IS HERE: the message wire, and the BitSet (aggregated BLS) signature.
// The post-quantum Corona signature and the teleport payloads are separate
// schemes — threshold BLS over ML-KEM — and are NOT ported. Their wire kinds
// parse to a refusal rather than to a signature that verifies trivially.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/signer.hpp"

#include <cstdint>
#include <map>
#include <optional>
#include <span>
#include <vector>

namespace lux::platformvm::warp {

// Go: warp.UnsignedMessage. Wire: NetworkID u32 @0, SourceChainID 32B @4,
// Payload bytes @36 — a 44-byte object with no kind of its own, because it is
// only ever parsed in a context that already knows what it is.
struct UnsignedMessage {
    std::uint32_t network_id = 0;
    Id source_chain_id{};
    std::vector<std::uint8_t> payload;

    // The bytes this message IS, and their hash. Both are bound by build() or
    // parse(); nothing is re-encoded to be hashed.
    std::vector<std::uint8_t> bytes;
    Id id{};

    friend bool operator==(const UnsignedMessage& a, const UnsignedMessage& b) {
        return a.network_id == b.network_id && a.source_chain_id == b.source_chain_id &&
               a.payload == b.payload;
    }

    static Result<UnsignedMessage> build(std::uint32_t network_id, const Id& source_chain_id,
                                         std::span<const std::uint8_t> payload);
    static Result<UnsignedMessage> parse(std::span<const std::uint8_t> b);
};

// Go: warp.BitSetSignature. Wire kind 0x00: kind u8 @0, Signature 96B @1,
// Signers bytes @97.
struct BitSetSignature {
    // A big-endian big integer whose i'th bit says validator i signed.
    std::vector<std::uint8_t> signers;
    signer::SignatureBytes signature{};

    friend bool operator==(const BitSetSignature&, const BitSetSignature&) = default;

    // How many validators contributed. A vector with unnecessary leading zero
    // bytes is refused rather than counted.
    Result<int> num_signers() const;
};

// Go: warp.Message. Wire: the unsigned buffer and the signature buffer, as two
// opaque byte fields. The container needs no kind of its own; the signature it
// carries is dispatched by its own.
struct Message {
    UnsignedMessage unsigned_message;
    BitSetSignature signature;
    std::vector<std::uint8_t> bytes;

    static Result<Message> build(const UnsignedMessage& unsigned_message, const BitSetSignature& sig);
    static Result<Message> parse(std::span<const std::uint8_t> b);
};

// One validator as a warp proof names it: by its KEY, not by its node. Two
// nodes sharing a key are one signer with the sum of their weights, because one
// aggregate cannot distinguish them.
struct Validator {
    signer::PublicKeyBytes public_key{};
    std::uint64_t weight = 0;
    std::vector<NodeId> node_ids;

    friend bool operator==(const Validator&, const Validator&) = default;
};

// The source chain's set, in the canonical order a bit vector indexes.
struct CanonicalValidatorSet {
    std::vector<Validator> validators;  // ordered by public key, ascending
    // Includes validators with no key: their stake still counts toward what a
    // quorum has to beat.
    std::uint64_t total_weight = 0;
};

// Go: warp.FlattenValidatorSet. Merges duplicate keys, sums the total, and
// sorts by key. Takes the UNCOMPRESSED keys the validator set carries.
Result<CanonicalValidatorSet> flatten(const std::map<NodeId, std::pair<std::vector<std::uint8_t>,
                                                                       std::uint64_t>>& set);

// Go: warp.FilterValidators. The validators whose bit is set. An index past the
// end of the set is a refusal — a signature cannot name a validator that is not
// there.
Result<std::vector<Validator>> filter(std::span<const std::uint8_t> signers,
                                      const std::vector<Validator>& validators);

Result<std::uint64_t> sum_weight(const std::vector<Validator>& validators);

// Go: warp.VerifyWeight. nil iff sig_weight ≥ total_weight · num / den, computed
// without dividing so nothing rounds in the attacker's favour.
Status verify_weight(std::uint64_t sig_weight, std::uint64_t total_weight, std::uint64_t quorum_num,
                     std::uint64_t quorum_den);

// Go: BitSetSignature.Verify. The signature must be by at least num/den of the
// source chain's weight at the height the caller resolved the set at.
Status verify(const BitSetSignature& sig, const UnsignedMessage& msg, std::uint32_t network_id,
              const CanonicalValidatorSet& validators, std::uint64_t quorum_num,
              std::uint64_t quorum_den);

}  // namespace lux::platformvm::warp
