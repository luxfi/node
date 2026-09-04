// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// validators.cpp — the set consensus samples, and its commitment.
//
// Rendered from Go vms/platformvm/validators/manager.go,
// state/state_validators.go (GetCurrentValidators, getInheritedPublicKey) and
// luxfi/node chains/quorum.go (hashValidatorSet).

#include "lux/platformvm/validators.hpp"

#include "lux/platformvm/sha256.hpp"

#include <blst.h>

#include <algorithm>

namespace lux::platformvm::validators {
namespace {

void put_be64(std::vector<std::uint8_t>& out, std::uint64_t v) {
    for (int i = 7; i >= 0; --i) out.push_back(static_cast<std::uint8_t>(v >> (8 * i)));
}

// Go: state.getInheritedPublicKey. A validator of a network that is not the
// primary one signs with the key of its primary-network entry.
Result<std::optional<signer::PublicKeyBytes>> inherited_key(const state::Chain& chain,
                                                            const NodeId& node_id) {
    auto primary = chain.get_current_validator(kPrimaryNetworkId, node_id);
    if (!primary) return fail(Err::NotValidator, node_id.hex() + " is not a primary network validator");
    return primary.value().public_key;
}

}  // namespace

Result<std::vector<std::uint8_t>> uncompress_public_key(const signer::PublicKeyBytes& compressed) {
    blst_p1_affine p{};
    if (blst_p1_uncompress(&p, compressed.data()) != BLST_SUCCESS)
        return fail(Err::InvalidPublicKey, "the registered key does not decompress");
    std::vector<std::uint8_t> out(96);
    blst_p1_affine_serialize(out.data(), &p);
    return out;
}

Result<std::map<NodeId, Validator>> current_set(const state::Chain& chain, const Id& chain_id) {
    std::map<NodeId, Validator> set;
    for (const auto& staker : chain.current_stakers()) {
        if (!(staker.chain_id == chain_id)) continue;
        // Only VALIDATORS carry weight in the set a vote is sampled from; a
        // delegator's stake sits under its validator's entry rather than being
        // an entry of its own.
        if (!txs::is_current_validator(staker.priority)) continue;

        Validator v;
        v.node_id = staker.node_id;
        v.weight = staker.weight;
        v.tx_id = staker.tx_id;

        std::optional<signer::PublicKeyBytes> key = staker.public_key;
        if (!key && !(chain_id == kPrimaryNetworkId)) {
            auto inherited = inherited_key(chain, staker.node_id);
            if (!inherited) return std::unexpected(inherited.error());
            key = inherited.value();
        }
        if (key) {
            auto uncompressed = uncompress_public_key(*key);
            if (!uncompressed) return std::unexpected(uncompressed.error());
            v.public_key = uncompressed.value();
        }
        set.emplace(v.node_id, std::move(v));
    }
    return set;
}

Id set_root(const std::map<NodeId, Validator>& set) {
    // An empty set is "unbound", not an error, and it commits to the zero id.
    // Hashing nothing would give sha256("") — a perfectly good hash of the
    // wrong thing.
    if (set.empty()) return kEmptyId;

    // std::map over NodeId is already ascending by raw bytes, which is the order
    // the commitment is defined in.
    std::vector<std::uint8_t> preimage;
    preimage.reserve(set.size() * (kNodeIdLen + 8 + 8 + 96));
    for (const auto& [node_id, v] : set) {
        preimage.insert(preimage.end(), node_id.b.begin(), node_id.b.end());
        put_be64(preimage, v.weight);
        put_be64(preimage, v.public_key ? v.public_key->size() : 0);
        if (v.public_key) preimage.insert(preimage.end(), v.public_key->begin(), v.public_key->end());
    }
    return id_from_hash(sha256(preimage));
}

}  // namespace lux::platformvm::validators
