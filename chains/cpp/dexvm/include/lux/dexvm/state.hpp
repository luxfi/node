// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state.hpp — the admitted set, on disk, and what executing a block does to it.
//
// The registry is the decision; the state is where the decision rests between
// processes. They stay separate: State never re-decides anything — every write
// it makes went through Registry::register_asset or Registry::create_market
// first — and Registry never touches a file.
//
// The execution root is a length-prefixed SHA-256 fold over the rows in
// ascending key order, using the same Folder the AssetID uses. Two nodes that
// admitted the same set therefore produce the same root, and a node that
// admitted one asset more or fewer produces a different one. It is computed by
// running the block, never copied from a proposer.

#pragma once

#include "lux/dexvm/gate.hpp"
#include "lux/dexvm/registry.hpp"
#include "lux/dexvm/store.hpp"
#include "lux/dexvm/tx.hpp"

#include <memory>

namespace lux::dexvm {

// The key prefixes. One byte each, and disjoint, so a scan over one never sees
// the other.
inline constexpr std::uint8_t kPrefixAsset = 'a';
inline constexpr std::uint8_t kPrefixMarket = 'm';
// The single row that says what this node last accepted. Read on boot, before
// this node signs anything.
inline constexpr std::string_view kKeyLastAccepted = "H";

// Diff is what executing a block produced: the rows it added, the root of the
// set it produced, and that set itself. It is held between verify and accept, so
// accepting writes exactly what verifying proved rather than executing twice.
//
// Only ADDED rows, because the two transactions only ever add: nothing in this
// chain mutates or removes an admitted record, so the new keys are the whole
// difference.
struct Diff {
    std::vector<std::pair<Bytes, Bytes>> rows;
    Id root{};
    std::unique_ptr<Registry> next;
};

class State {
public:
    // load reads the durable set back into a registry under the given policy's
    // allowed kinds. A row that does not parse is a corrupt store, not an empty
    // one, and says so. chain_label_for is the deny-scan lookup the gate runs
    // with — the node passes its manifest's labels.
    static Result<std::unique_ptr<State>> load(
        store::Store& s, NetworkClass network_class, const DexAssetPolicy& policy,
        std::function<std::string(const Id&)> chain_label_for);

    Registry& registry() { return *reg_; }
    const Registry& registry() const { return *reg_; }

    Id last_accepted() const { return last_accepted_; }
    std::uint64_t last_accepted_height() const { return last_accepted_height_; }

    // execute applies a block's transactions IN ORDER to a COPY of the current
    // set. It decides nothing itself: each transaction is the registry's own
    // admission call, so a transaction the registry would refuse makes the whole
    // block invalid. The boot gate then runs over the result, because a block
    // that produced a set this chain would refuse to start from is a block that
    // must not be accepted. Nothing is written.
    Result<Diff> execute(const BlockBody& body, ChainVerifier& v) const;

    // commit writes the diff's rows and the new last-accepted marker durably,
    // then adopts the executed set as the live one. It returns only after the
    // store has flushed, which is what closes the height to a second signature.
    Result<void> commit(const BlockBody& body, Diff&& diff);

    // root is the fold over the current set.
    Id root() const;

private:
    State(store::Store& s, NetworkClass network_class, DexAssetPolicy policy,
          std::function<std::string(const Id&)> chain_label_for)
        : store_(s),
          network_class_(network_class),
          policy_(std::move(policy)),
          chain_label_for_(std::move(chain_label_for)) {}

    store::Store& store_;
    NetworkClass network_class_;
    DexAssetPolicy policy_;
    std::function<std::string(const Id&)> chain_label_for_;
    std::unique_ptr<Registry> reg_;
    Id last_accepted_{};
    std::uint64_t last_accepted_height_ = 0;
};

// The row encodings, exposed because a format is only pinned by a test that can
// state it. A key is its prefix byte followed by the 32-byte id.
Bytes asset_key(const Id& id);
Bytes market_key(const Id& id);
Bytes encode_asset_row(const Asset& a);
Bytes encode_market_row(const Market& m);
Result<Asset> decode_asset_row(ByteView b);
Result<Market> decode_market_row(ByteView b);

// fold_rows is the execution root over an ordered row sequence.
Id fold_rows(const std::vector<std::pair<Bytes, Bytes>>& rows);

}  // namespace lux::dexvm
