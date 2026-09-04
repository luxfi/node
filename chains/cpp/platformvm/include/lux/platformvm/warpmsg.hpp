// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// warpmsg.hpp — what a warp message SAYS.
//
// Rendered from Go vms/platformvm/warp/payload (the envelope) and
// vms/platformvm/warp/message (the four things an L1 says to the P-chain).
//
// Two layers, because they answer different questions. The envelope says WHO
// sent this and from what address — a hash, or an addressed call. The message
// says WHAT: register this validator, this is its new weight, this network has
// converted, this validation id is (not) registered.
//
// The validation id is the HASH of the registration message. That is the whole
// identity scheme: a validator's name on the P-chain is the message that
// registered it, so two registrations differing in any field are two different
// validators and a replay of one cannot become the other.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/signer.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstdint>
#include <optional>
#include <span>
#include <variant>
#include <vector>

namespace lux::platformvm::warpmsg {

// ── the envelope (Go: warp/payload)

// Go: payload.Hash. Wire kind 0: kind u8 @0, Hash 32B @1.
struct Hash {
    Id hash{};
    std::vector<std::uint8_t> bytes;

    static Result<Hash> build(const Id& hash);
};

// Go: payload.AddressedCall. Wire kind 1: kind u8 @0, SourceAddress bytes @1,
// Payload bytes @9. If a destination is expected it is encoded IN the payload —
// the envelope only says where it came from.
struct AddressedCall {
    std::vector<std::uint8_t> source_address;
    std::vector<std::uint8_t> payload;
    std::vector<std::uint8_t> bytes;

    static Result<AddressedCall> build(std::span<const std::uint8_t> source_address,
                                       std::span<const std::uint8_t> payload);
};

using Envelope = std::variant<Hash, AddressedCall>;
Result<Envelope> parse_envelope(std::span<const std::uint8_t> b);

// ── what an L1 says (Go: warp/message)

// A group that may act on an L1 validator's balance or its disabling. Locktime
// is not part of it — that is the whole difference from an output's owner.
using PChainOwner = txs::PChainOwner;

// Go: message.RegisterL1Validator. Wire kind 1.
struct RegisterL1Validator {
    Id chain_id{};
    std::vector<std::uint8_t> node_id;
    signer::PublicKeyBytes bls_public_key{};
    std::uint64_t expiry = 0;
    PChainOwner remaining_balance_owner{};
    PChainOwner disable_owner{};
    std::uint64_t weight = 0;
    std::vector<std::uint8_t> bytes;

    static Result<RegisterL1Validator> build(const Id& chain_id, const NodeId& node_id,
                                             const signer::PublicKeyBytes& key, std::uint64_t expiry,
                                             const PChainOwner& remaining_balance_owner,
                                             const PChainOwner& disable_owner, std::uint64_t weight);

    Status verify() const;
    // The validator's name on the P-chain: the hash of the message that
    // registered it. Two registrations differing anywhere are two validators.
    Id validation_id() const;
};

// Go: message.L1ValidatorRegistration. Wire kind 2. `registered` false means
// this validation id is not and can NEVER become a validator — which is what
// makes an expiry final rather than a retry.
struct L1ValidatorRegistration {
    Id validation_id{};
    bool registered = false;
    std::vector<std::uint8_t> bytes;

    static Result<L1ValidatorRegistration> build(const Id& validation_id, bool registered);
};

// Go: message.L1ValidatorWeight. Wire kind 3. The nonce is what stops an old
// weight from being replayed over a newer one.
struct L1ValidatorWeight {
    Id validation_id{};
    std::uint64_t nonce = 0;
    std::uint64_t weight = 0;
    std::vector<std::uint8_t> bytes;

    static Result<L1ValidatorWeight> build(const Id& validation_id, std::uint64_t nonce,
                                           std::uint64_t weight);
};

// Go: message.ChainToL1Conversion. Wire kind 0. It carries only the id of the
// conversion — the data itself is hashed to that id and lives on the chain that
// converted.
struct ChainToL1Conversion {
    Id id{};
    std::vector<std::uint8_t> bytes;

    static Result<ChainToL1Conversion> build(const Id& id);
};

// The conversion's own data, as a standalone hash preimage with no kind byte:
// what a network became when it went sovereign.
struct ConversionValidator {
    std::vector<std::uint8_t> node_id;
    signer::PublicKeyBytes bls_public_key{};
    std::uint64_t weight = 0;

    friend bool operator==(const ConversionValidator&, const ConversionValidator&) = default;
};

struct ConversionData {
    Id chain_id{};
    Id manager_chain_id{};
    std::vector<std::uint8_t> manager_address;
    std::vector<ConversionValidator> validators;

    // The id every later message about this L1 refers to.
    Result<Id> conversion_id() const;
    Result<std::vector<std::uint8_t>> encode() const;
};

using Message = std::variant<ChainToL1Conversion, RegisterL1Validator, L1ValidatorRegistration,
                             L1ValidatorWeight>;
Result<Message> parse_message(std::span<const std::uint8_t> b);

}  // namespace lux::platformvm::warpmsg
