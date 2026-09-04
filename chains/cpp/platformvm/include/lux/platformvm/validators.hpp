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

// ── the set at a height that has already passed
//
// Rendered from Go state/state_diffs.go (calculateValidatorDiffs,
// writeValidatorDiffs, applyWeightDiff, ApplyValidatorPublicKeyDiffs) and
// validators/manager.go (makeValidatorSet).
//
// A message signed at height H is checked long after H, so a node must be able
// to say who validated then. Keeping every past set costs the whole history.
// Keeping what each height CHANGED costs the changes — and the set at H is the
// set now with every change since UNDONE. That is why the record is written
// backwards-facing: a weight diff is inverted on the way down, and the key
// stored is the one the node signed with BEFORE the change, not after.

// Where a change lands: one node's entry in one network's set.
struct Where {
    Id chain_id{};
    NodeId node_id{};

    friend bool operator==(const Where&, const Where&) = default;
    bool operator<(const Where& o) const {
        if (!(chain_id == o.chain_id)) return chain_id < o.chain_id;
        return node_id < o.node_id;
    }
};

// What one layer did to one entry.
struct Change {
    state::WeightDiff weight{};
    // The name the entry is known by: the transaction that added a staker, or
    // the registration that created an L1 validator. Carried so that undoing a
    // removal restores the name the entry had, which an L1 validator can
    // otherwise lose when it is removed and re-registered in one block.
    Id validation{};
    bool had_removal = false;
    // The uncompressed key, before and after. Empty means none.
    std::vector<std::uint8_t> key_before;
    std::vector<std::uint8_t> key_after;

    friend bool operator==(const Change&, const Change&) = default;
};

// Go: calculateValidatorDiffs. What a layer changed, over the state it layers
// on. Both the pre-LP-77 stakers and the L1 validators land in the same record,
// because they are entries in the same set.
Result<std::map<Where, Change>> changes(const state::Diff& layer, const state::Chain& parent);

// The record of what every height changed.
class History {
  public:
    // Go: writeValidatorDiffs. One height can be written more than once — a
    // proposal block settles its decision and then the outcome its option chose
    // — so a change at a place already recorded at that height COMPOSES with it
    // rather than replacing it. Replacing would lose the first half of what the
    // height did.
    Status record(std::uint64_t height, const std::map<Where, Change>& c);

    // Go: makeValidatorSet. `set` arrives as the set at `from` and leaves as the
    // set at `to`. Refuses to look forward: a height this chain has not reached
    // has no set, and answering one would be inventing it.
    Status rewind(std::map<NodeId, Validator>& set, const Id& chain_id, std::uint64_t from,
                  std::uint64_t to) const;

    bool empty() const { return by_height_.empty(); }

  private:
    // Go: writeValidatorDiffs only stores a weight when it moved, or when an
    // entry was removed and re-added under a new name, and only stores a key
    // when it changed. The rule is applied on the way OUT rather than on the way
    // in, because a change that is not worth storing on its own can still be
    // half of one that is.
    static bool weight_moved(const Change& c);
    static bool key_moved(const Change& c) { return c.key_before != c.key_after; }

    std::map<std::uint64_t, std::map<Where, Change>> by_height_;
};

}  // namespace lux::platformvm::validators
