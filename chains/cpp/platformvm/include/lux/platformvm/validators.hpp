// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// validators.hpp — who validates a network, and with what.
//
// Rendered from Go vms/platformvm/validators/manager.go (getCurrentValidatorSet)
// and state/state_validators.go (GetCurrentValidators). This is the answer
// consensus samples: it decides whose vote counts and for how much, so it is the
// P-chain's whole reason to exist from the node's point of view.
//
// TWO THINGS HERE ARE EASY TO GET WRONG AND BOTH ARE SILENT.
//
// The key is UNCOMPRESSED. A proof of possession signs the 48-byte compressed
// form, and the set commitment hashes the 96-byte uncompressed one. Using the
// wrong one produces a root that verifies against nothing — and a validator
// whose signed message differs from its peers' has its votes dropped rather
// than disputed.
//
// A validator of a network that is not the primary one holds NO key of its own —
// the transaction that registered it carries no signer — and signs with the key
// of its primary-network entry. Surfacing it keyless would leave its weight in
// the denominator with no way for anyone to ever vote toward it, which is a
// quorum that cannot be reached.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/signer.hpp"
#include "lux/platformvm/state.hpp"

#include <cstdint>
#include <map>
#include <optional>
#include <vector>

namespace lux::platformvm::validators {

// One validator, as consensus reads it.
struct Validator {
    NodeId node_id{};
    // The BLS key this validator signs with, uncompressed (96 bytes for G1).
    // Absent for a validator registered before keys existed.
    std::optional<std::vector<std::uint8_t>> public_key;
    std::uint64_t weight = 0;
    Id tx_id{};

    friend bool operator==(const Validator&, const Validator&) = default;
};

// The set validating `chain_id` right now, keyed by node.
Result<std::map<NodeId, Validator>> current_set(const state::Chain& chain, const Id& chain_id);

// The 48-byte compressed key as its 96-byte uncompressed form. Go:
// bls.PublicKeyToUncompressedBytes.
Result<std::vector<std::uint8_t>> uncompress_public_key(const signer::PublicKeyBytes& compressed);

// The canonical commitment to a set: sha256 over, per validator sorted by raw
// node id ascending, node id ‖ weight(8, big-endian) ‖ len(key)(8, big-endian) ‖
// key. An empty set commits to the all-zero id, which is the explicit "unbound"
// answer rather than an error.
//
// Rendered from Go's hashValidatorSet (luxfi/node chains/quorum.go). It is
// copied field for field because "compatible" is not a property one can
// approximate.
Id set_root(const std::map<NodeId, Validator>& set);

}  // namespace lux::platformvm::validators
