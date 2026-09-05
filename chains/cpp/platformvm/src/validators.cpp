// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// validators.cpp — the set consensus samples, and its commitment.
//
// Rendered from Go vms/platformvm/validators/manager.go,
// state/state_validators.go (GetCurrentValidators, getInheritedPublicKey) and
// luxfi/node chains/quorum.go (hashValidatorSet).

#include "lux/platformvm/validators.hpp"

#include "lux/platformvm/safemath.hpp"

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
    if (!primary) return fail(Err::NotValidator, hex(node_id) + " is not a primary network validator");
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

    // Then the L1 validators of the same network. They hold their own key
    // rather than inheriting one, and they are named by the registration that
    // created them rather than by a transaction that added them.
    for (const auto& v : chain.l1_validators(chain_id)) {
        Validator out;
        out.node_id = v.node_id;
        out.weight = v.weight;
        out.tx_id = v.validation_id;
        if (!v.public_key.empty()) out.public_key = v.public_key;
        set.insert_or_assign(out.node_id, std::move(out));
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
    return sha256(preimage);
}

// ── the set at a height that has already passed

namespace {

// Go: state.getInheritedPublicKey, as it is used while a layer is being
// written: the primary-network entry as it stands, whether that entry is
// already there or is being changed by this very layer. A staker of another
// network signs with that key and has none of its own, so this is the only
// place its key can come from.
Result<std::optional<signer::PublicKeyBytes>> key_being_written(const state::Diff& layer,
                                                                const state::Chain& parent,
                                                                const NodeId& node_id) {
    if (auto existing = parent.get_current_validator(kPrimaryNetworkId, node_id))
        return existing.value().public_key;
    const auto& diffs = layer.current_validator_diffs();
    const auto chain = diffs.find(kPrimaryNetworkId);
    if (chain != diffs.end()) {
        const auto node = chain->second.find(node_id);
        if (node != chain->second.end() && node->second.validator)
            return node->second.validator->public_key;
    }
    return fail(Err::NotValidator, hex(node_id) + " is not a primary network validator");
}

}  // namespace

Result<std::map<Where, Change>> changes(const state::Diff& layer, const state::Chain& parent) {
    std::map<Where, Change> out;

    // The pre-LP-77 stakers.
    for (const auto& [chain_id, nodes] : layer.current_validator_diffs()) {
        for (const auto& [node_id, diff] : nodes) {
            auto weight = diff.weight_diff();
            if (!weight) return std::unexpected(weight.error());

            auto key = key_being_written(layer, parent, node_id);
            if (!key) return std::unexpected(key.error());

            Change c;
            c.weight = weight.value();
            if (diff.validator && weight.value().amount != 0) c.validation = diff.validator->tx_id;
            if (key.value()) {
                auto uncompressed = uncompress_public_key(*key.value());
                if (!uncompressed) return std::unexpected(uncompressed.error());
                // A key the entry did not have before it was added, and does not
                // have after it was deleted.
                if (diff.status != state::DiffStatus::Added) c.key_before = uncompressed.value();
                if (diff.status != state::DiffStatus::Deleted) c.key_after = uncompressed.value();
            }
            out.emplace(Where{chain_id, node_id}, std::move(c));
        }
    }

    // The L1 validators, removals before additions. One validator can be
    // removed and re-registered under a new name in one block, and the entry it
    // leaves behind must be the one it had, not the one it is getting.
    for (const auto& [validation_id, v] : layer.l1_changes()) {
        auto prior = parent.get_l1_validator(validation_id);
        if (!prior) continue;
        const auto& before = prior.value();
        Change& c = out[Where{before.chain_id, before.effective_node_id()}];
        c.validation = validation_id;
        c.had_removal = true;
        if (auto st = c.weight.sub(before.weight); !st) return std::unexpected(st.error());
        c.key_before = before.effective_public_key();
    }
    for (const auto& [validation_id, v] : layer.l1_changes()) {
        if (v.is_deleted()) continue;
        Change& c = out[Where{v.chain_id, v.effective_node_id()}];
        if (c.validation == kEmptyId) c.validation = validation_id;
        if (auto st = c.weight.add(v.weight); !st) return std::unexpected(st.error());
        c.key_after = v.effective_public_key();
    }
    return out;
}

bool History::weight_moved(const Change& c) {
    return c.weight.amount != 0 || (c.had_removal && !(c.validation == kEmptyId));
}

Status History::record(std::uint64_t height, const std::map<Where, Change>& c) {
    auto& at = by_height_[height];
    for (const auto& [where, change] : c) {
        const auto existing = at.find(where);
        if (existing == at.end()) {
            at.emplace(where, change);
            continue;
        }
        Change& merged = existing->second;
        if (auto st = merged.weight.add_or_sub(change.weight.decrease, change.weight.amount); !st)
            return st;
        if (merged.validation == kEmptyId) merged.validation = change.validation;
        merged.had_removal = merged.had_removal || change.had_removal;
        // The key the height started with, and the one it ended with.
        merged.key_after = change.key_after;
    }
    return ok();
}

Status History::rewind(std::map<NodeId, Validator>& set, const Id& chain_id, std::uint64_t from,
                       std::uint64_t to) const {
    if (to > from)
        return fail(Err::InvalidState, "height " + std::to_string(to) +
                                           " has not been reached from height " + std::to_string(from));
    if (to == from) return ok();

    // The changes to undo are those at heights (to, from]. The record is
    // inclusive, so the walk stops at to + 1.
    const std::uint64_t last = to + 1;

    // The weights, from the newest change to the oldest. A diff says what the
    // height DID, so undoing it inverts it.
    for (auto h = by_height_.upper_bound(from); h != by_height_.begin();) {
        --h;
        if (h->first < last) break;
        for (const auto& [where, e] : h->second) {
            if (!(where.chain_id == chain_id) || !weight_moved(e)) continue;
            auto it = set.find(where.node_id);
            if (it == set.end()) it = set.emplace(where.node_id, Validator{where.node_id, {}, 0, {}}).first;
            if (!(e.validation == kEmptyId)) it->second.tx_id = e.validation;
            auto moved = e.weight.decrease ? add64(it->second.weight, e.weight.amount)
                                           : sub64(it->second.weight, e.weight.amount);
            if (!moved) return std::unexpected(moved.error());
            it->second.weight = moved.value();
            // Weight zero at the earlier height means the node was not in the
            // set then, and an entry of no weight is not an entry.
            if (it->second.weight == 0) set.erase(it);
        }
    }

    // Then the keys, in the same direction, so the oldest recorded key — the one
    // the node signed with at the height being asked about — is the one that
    // stands. A key is only restored onto an entry that is there: a node that
    // was not in the set had no key in it either.
    for (auto h = by_height_.upper_bound(from); h != by_height_.begin();) {
        --h;
        if (h->first < last) break;
        for (const auto& [where, e] : h->second) {
            if (!(where.chain_id == chain_id) || !key_moved(e)) continue;
            auto it = set.find(where.node_id);
            if (it == set.end()) continue;
            if (e.key_before.empty())
                it->second.public_key.reset();
            else
                it->second.public_key = e.key_before;
        }
    }
    return ok();
}

}  // namespace lux::platformvm::validators
