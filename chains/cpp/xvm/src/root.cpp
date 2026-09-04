// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/root.hpp"

#include "lux/crypto/keccak.h"

#include <algorithm>
#include <cstring>

namespace lux::xvm::root {
namespace {

void le32(Bytes& b, std::uint32_t v) {
    for (int i = 0; i < 4; ++i) b.push_back(std::uint8_t(v >> (8 * i)));
}
void le64(Bytes& b, std::uint64_t v) {
    for (int i = 0; i < 8; ++i) b.push_back(std::uint8_t(v >> (8 * i)));
}
void append(Bytes& b, const Id& v) { b.insert(b.end(), v.begin(), v.end()); }

Digest keccak_of(const Bytes& b) {
    Digest out{};
    keccak256(b.data(), b.size(), out.data());
    return out;
}

}  // namespace

Digest keccak(const std::vector<ByteView>& parts) {
    Bytes joined;
    for (const auto& p : parts) joined.insert(joined.end(), p.begin(), p.end());
    return keccak_of(joined);
}

Digest leaf_hash(const Digest& d) {
    Bytes b;
    b.push_back(kLeafTag);
    append(b, d);
    return keccak_of(b);
}

Digest node_hash(const Digest& l, const Digest& r) {
    Bytes b;
    b.push_back(kNodeTag);
    append(b, l);
    append(b, r);
    return keccak_of(b);
}

Digest empty_root() { return keccak_of(Bytes{}); }

Digest merkle_root(const std::vector<Digest>& leaves) {
    if (leaves.empty()) return empty_root();

    std::vector<Digest> level;
    level.reserve(leaves.size());
    for (const auto& d : leaves) level.push_back(leaf_hash(d));

    while (level.size() > 1) {
        const std::size_t cnt = level.size();
        const std::size_t parents = (cnt + 1) / 2;
        const std::size_t pairs = cnt / 2;
        std::vector<Digest> next(parents);
        for (std::size_t j = 0; j < pairs; ++j) next[j] = node_hash(level[2 * j], level[2 * j + 1]);
        // RFC 6962 lone-right promotion: an odd last node moves up unchanged
        // rather than being paired with itself, which would make two distinct
        // trees share a root.
        if (cnt % 2 == 1) next[parents - 1] = level[cnt - 1];
        level = std::move(next);
    }
    return level[0];
}

Digest utxo_leaf_digest(const UTXOLeaf& u, std::uint32_t i) {
    Bytes b;
    b.reserve(32 + 32 + 8 + 8 + 32 + 8 + 4 + 4 + 4);
    append(b, u.utxo_id);
    append(b, u.asset_id);
    le64(b, u.amount_lo);
    le64(b, u.amount_hi);
    append(b, u.owner_root);
    le64(b, u.locktime);
    le32(b, u.threshold);
    le32(b, u.status);
    le32(b, i);
    return keccak_of(b);
}

Digest asset_leaf_digest(const AssetLeaf& a, std::uint32_t i) {
    Bytes b;
    b.reserve(32 + 8 + 8 + 32 + 4 + 4 + 4);
    append(b, a.asset_id);
    le64(b, a.total_supply_lo);
    le64(b, a.total_supply_hi);
    append(b, a.mint_authority_root);
    le32(b, a.freeze_flag);
    le32(b, a.denomination);
    le32(b, i);
    return keccak_of(b);
}

Digest tx_leaf_digest(const TxLeaf& t, std::uint32_t i) {
    Bytes b;
    b.reserve(32 + 4 + 4 + 4 + 32 + 4);
    append(b, t.tx_id);
    le32(b, t.kind);
    le32(b, t.status);
    le32(b, t.reject_reason);
    append(b, t.proof_digest);
    le32(b, i);
    return keccak_of(b);
}

Digest utxo_root(const std::vector<UTXOLeaf>& utxos) {
    std::vector<Digest> leaves;
    leaves.reserve(utxos.size());
    for (std::size_t i = 0; i < utxos.size(); ++i) {
        // The SLOT INDEX is bound into the leaf, and an unoccupied slot is
        // skipped rather than renumbered — so the fold is over positions, not
        // over a compacted list.
        if ((utxos[i].status & kUTXOOccupied) == 0) continue;
        leaves.push_back(utxo_leaf_digest(utxos[i], std::uint32_t(i)));
    }
    return merkle_root(leaves);
}

Digest asset_root(const std::vector<AssetLeaf>& assets) {
    std::vector<Digest> leaves;
    leaves.reserve(assets.size());
    for (std::size_t i = 0; i < assets.size(); ++i) {
        if (assets[i].occupied == 0) continue;
        leaves.push_back(asset_leaf_digest(assets[i], std::uint32_t(i)));
    }
    return merkle_root(leaves);
}

Digest tx_root(const std::vector<TxLeaf>& txs) {
    std::vector<Digest> leaves;
    leaves.reserve(txs.size());
    for (std::size_t i = 0; i < txs.size(); ++i)
        leaves.push_back(tx_leaf_digest(txs[i], std::uint32_t(i)));
    return merkle_root(leaves);
}

Digest compose(const Digest& parent_execution_root, const Digest& utxo, const Digest& asset,
               const Digest& tx, std::uint64_t height) {
    Bytes b;
    append(b, parent_execution_root);
    append(b, utxo);
    append(b, asset);
    append(b, tx);
    le64(b, height);
    return keccak_of(b);
}

// ---- the projection ----

Digest owner_root(const Owners& o) {
    Bytes b;
    le32(b, o.threshold);
    le32(b, std::uint32_t(o.keys.size()));
    for (const auto& k : o.keys) b.insert(b.end(), k.begin(), k.end());
    return keccak_of(b);
}

wire::Result<Owners> extract_owners(const fx::FxOutput& out) {
    const fx::OutputOwners* owners = out.owners();
    if (owners == nullptr)
        return std::unexpected(
            "xvm execution_root: output owner model has no canonical owner_root");
    Owners o;
    o.threshold = owners->threshold;
    o.locktime = owners->locktime;
    o.keys.reserve(owners->addrs.size());
    for (const auto& a : owners->addrs) o.keys.emplace_back(a.begin(), a.end());
    std::sort(o.keys.begin(), o.keys.end());
    return o;
}

wire::Result<Id> block_execution_root(const Id& parent_execution_root,
                                      const std::vector<std::shared_ptr<txs::Tx>>& blk_txs,
                                      const state::ReadOnlyChain& post_state,
                                      std::uint64_t height) {
    // tx leaves, in BLOCK order — the block already fixed that order.
    constexpr std::uint32_t kStatusAccepted = 1;
    std::vector<TxLeaf> tx_leaves;
    tx_leaves.reserve(blk_txs.size());
    for (const auto& tx : blk_txs) tx_leaves.push_back(TxLeaf{tx->id(), 0, kStatusAccepted, 0, {}});

    // UTXO leaves, over the POST-BLOCK occupied set in ascending UTXOID order.
    std::vector<UTXOLeaf> utxo_leaves;
    for (const auto& u : post_state.utxos(kEmptyId, 0)) {
        if (u.out == nullptr)
            return std::unexpected("xvm execution_root: utxo has no output");
        auto o = extract_owners(*u.out);
        if (!o) return std::unexpected(o.error());
        std::uint64_t amount = 0;
        if (const auto* amt = dynamic_cast<const fx::FxTransferOut*>(u.out.get()))
            amount = amt->amount();
        utxo_leaves.push_back(UTXOLeaf{u.utxo_id.input_id(), u.asset_id, amount, 0, owner_root(*o),
                                       o->locktime, o->threshold, kUTXOOccupied});
    }

    // Assets carry no leaf today: the asset family's snapshot layout has no
    // producer in the X-Chain executor yet, so the projection is EMPTY rather
    // than invented — an invented leaf would commit to a value nothing else
    // computes.
    std::vector<AssetLeaf> asset_leaves;

    return compose(parent_execution_root, utxo_root(utxo_leaves), asset_root(asset_leaves),
                   tx_root(tx_leaves), height);
}

}  // namespace lux::xvm::root
