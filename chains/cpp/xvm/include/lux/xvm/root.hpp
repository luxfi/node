// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// root.hpp — the X-Chain execution root: what a validator actually signs over.
//
// A block's identity says WHICH transactions ran; the execution root says WHAT
// STATE they produced. Binding it is what makes a quorum certificate agreement
// about a result rather than about a name — a node whose execution diverged
// computes a different root, signs a different message, and simply is not in the
// quorum.
//
// The root is a pure function of canonical leaf FIELD VALUES, never of an
// in-memory layout:
//
//   utxo_root   RFC-6962 tagged binary Merkle fold over the OCCUPIED UTXO set,
//               in ascending UTXOID order
//   asset_root  the same over assets (empty today — see below)
//   tx_root     the same over every tx in the block, in block order
//   execution_root = keccak256(parent ‖ utxo ‖ asset ‖ tx ‖ height_u64_le)
//
// The hash is Ethereum Keccak-256 (0x01 pad), NOT FIPS-202 SHA3, and integers
// are little-endian — both because this must be byte-identical to the Go
// definition and to the GPU kernels that mirror it.

#pragma once

#include "lux/xvm/id.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/txs.hpp"

#include <vector>

namespace lux::xvm::root {

using Digest = Id;  // a keccak256 output, same width as an Id

// keccak256 of the concatenation of the given byte runs.
Digest keccak(const std::vector<ByteView>& parts);

// ---- the RFC-6962 tagged binary Merkle fold ----
inline constexpr std::uint8_t kLeafTag = 0x00;  // leaf_hash(d)    = keccak(0x00 ‖ d)
inline constexpr std::uint8_t kNodeTag = 0x01;  // node_hash(L, R) = keccak(0x01 ‖ L ‖ R)

Digest leaf_hash(const Digest& d);
Digest node_hash(const Digest& l, const Digest& r);
Digest empty_root();  // keccak256("") — the root of the empty leaf set
// merkle_root folds a DENSE, already-compacted, ascending leaf-digest list.
// A lone right node is promoted, not paired with itself.
Digest merkle_root(const std::vector<Digest>& leaves);

// ---- leaves, in the exact preimage order the kernels hash ----

inline constexpr std::uint32_t kUTXOOccupied = 0x1;

struct UTXOLeaf {
    Id utxo_id{};
    Id asset_id{};
    std::uint64_t amount_lo = 0;
    std::uint64_t amount_hi = 0;  // X-Chain amounts are uint64; the high limb is always zero
    Digest owner_root{};
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::uint32_t status = 0;
};

struct AssetLeaf {
    Id asset_id{};
    std::uint64_t total_supply_lo = 0;
    std::uint64_t total_supply_hi = 0;
    Digest mint_authority_root{};
    std::uint32_t freeze_flag = 0;
    std::uint32_t denomination = 0;
    std::uint32_t occupied = 0;
};

struct TxLeaf {
    Id tx_id{};
    std::uint32_t kind = 0;
    std::uint32_t status = 0;
    std::uint32_t reject_reason = 0;
    Digest proof_digest{};
};

Digest utxo_leaf_digest(const UTXOLeaf& u, std::uint32_t i);
Digest asset_leaf_digest(const AssetLeaf& a, std::uint32_t i);
Digest tx_leaf_digest(const TxLeaf& t, std::uint32_t i);

Digest utxo_root(const std::vector<UTXOLeaf>& utxos);
Digest asset_root(const std::vector<AssetLeaf>& assets);
Digest tx_root(const std::vector<TxLeaf>& txs);

// compose is the FIXED-SHAPE final hash — not a Merkle node, which is why it is
// un-tagged.
Digest compose(const Digest& parent_execution_root, const Digest& utxo, const Digest& asset,
               const Digest& tx, std::uint64_t height);

// ---- the projection from chain state to leaves ----

// Owners is an output's spend authority reduced to what the root commits to: a
// threshold and the owner key set in canonical ascending byte order.
struct Owners {
    std::uint32_t threshold = 0;
    std::uint64_t locktime = 0;
    std::vector<Bytes> keys;
};

// owner_root = keccak256(threshold_le ‖ count_le ‖ key0 ‖ key1 ‖ …)
Digest owner_root(const Owners& o);

// extract_owners reads an output's owner model. An output with no owner model
// has no canonical owner_root, and that is an error rather than a zero — a zero
// would silently make two different outputs commit to the same thing.
wire::Result<Owners> extract_owners(const fx::FxOutput& out);

// block_execution_root is the whole computation for one block: the tx leaves in
// block order, the post-block occupied UTXO set in ascending UTXOID order, and
// the parent root and height.
wire::Result<Id> block_execution_root(const Id& parent_execution_root,
                                      const std::vector<std::shared_ptr<txs::Tx>>& blk_txs,
                                      const state::ReadOnlyChain& post_state,
                                      std::uint64_t height);

}  // namespace lux::xvm::root
