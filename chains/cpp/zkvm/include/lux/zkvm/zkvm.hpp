// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#pragma once

#include "lux/zkvm/sha256.hpp"
#include "lux/zkvm/zap.hpp"

#include <array>
#include <cstdint>
#include <cstring>
#include <expected>
#include <map>
#include <memory>
#include <set>
#include <span>
#include <string>
#include <string_view>
#include <vector>

namespace lux::zkvm {

using Id = std::array<std::uint8_t, 32>;

inline Id compute_hash(std::span<const std::uint8_t> data) {
    return Sha256::hash(data);
}

// Transaction fields
inline constexpr int kTxType = 0;
inline constexpr int kTxVersion = 1;
inline constexpr int kTxFee = 2;
inline constexpr int kTxExpiry = 10;
inline constexpr int kTxTinLens = 18;
inline constexpr int kTxTinBlob = 26;
inline constexpr int kTxToutLens = 34;
inline constexpr int kTxToutBlob = 42;
inline constexpr int kTxNullLens = 50;
inline constexpr int kTxNullBlob = 58;
inline constexpr int kTxSoutLens = 66;
inline constexpr int kTxSoutBlob = 74;
inline constexpr int kTxProof = 82;
inline constexpr int kTxMemo = 90;
inline constexpr int kTxSize = 98;

// Block fields
inline constexpr int kBlkParent = 0;
inline constexpr int kBlkHeight = 32;
inline constexpr int kBlkTime = 40;
inline constexpr int kBlkTxLens = 48;
inline constexpr int kBlkTxBlob = 56;
inline constexpr int kBlkStateRoot = 64;
inline constexpr int kBlkSize = 72;

// UTXO fields
inline constexpr int kUtxoAmount = 0;
inline constexpr int kUtxoAsset = 8;
inline constexpr int kUtxoLocktime = 40;
inline constexpr int kUtxoThreshold = 48;
inline constexpr int kUtxoOwner = 52;
inline constexpr int kUtxoSize = 68;

struct Utxo {
    std::uint64_t amount{0};
    Id asset_id{};
    std::uint64_t locktime{0};
    std::uint32_t threshold{0};
    std::vector<std::uint8_t> owner;
};

struct Transaction {
    std::uint8_t tx_type{0};
    std::uint8_t version{0};
    std::uint64_t fee{0};
    std::uint64_t expiry{0};
    std::vector<std::vector<std::uint8_t>> nullifiers;
    std::vector<std::uint8_t> proof;
    std::vector<std::uint8_t> memo;
    std::vector<std::uint8_t> raw;

    static std::expected<Transaction, std::string> parse(std::span<const std::uint8_t> bytes) {
        zap::Message msg;
        std::string err;
        if (!zap::Message::parse(bytes, &msg, &err)) return std::unexpected(err);
        auto root = msg.root();

        Transaction tx;
        tx.tx_type = root.u8(kTxType);
        tx.version = root.u8(kTxVersion);
        tx.fee = root.u64(kTxFee);
        tx.expiry = root.u64(kTxExpiry);

        auto memo_span = root.bytes(kTxMemo);
        tx.memo.assign(memo_span.begin(), memo_span.end());

        auto proof_span = root.bytes(kTxProof);
        tx.proof.assign(proof_span.begin(), proof_span.end());

        auto null_lens = root.list(kTxNullLens);
        auto null_blob = root.bytes(kTxNullBlob);

        std::size_t off = 0;
        for (int i = 0; i < null_lens.len(); ++i) {
            std::size_t len = null_lens.u32(i);
            std::size_t end = off + len;
            if (end > null_blob.size()) {
                return std::unexpected("nullifier length exceeds blob");
            }
            tx.nullifiers.emplace_back(null_blob.begin() + off, null_blob.begin() + end);
            off = end;
        }

        tx.raw.assign(bytes.begin(), bytes.end());
        return tx;
    }

    Id id() const {
        return compute_hash(raw);
    }

    std::expected<void, std::string> verify() const {
        if (tx_type == 0) {
            return std::unexpected("invalid transaction type");
        }
        return {};
    }
};

struct Block {
    Id parent_id{};
    std::uint64_t height{0};
    std::int64_t timestamp{0};
    std::vector<std::uint8_t> state_root;
    std::vector<Transaction> txs;
    std::vector<std::uint8_t> raw;

    static std::expected<Block, std::string> parse(std::span<const std::uint8_t> bytes) {
        zap::Message msg;
        std::string err;
        if (!zap::Message::parse(bytes, &msg, &err)) return std::unexpected(err);
        auto root = msg.root();

        Block blk;
        auto p_span = root.bytes_fixed_slice(kBlkParent, 32);
        if (p_span.size() == 32) {
            std::memcpy(blk.parent_id.data(), p_span.data(), 32);
        }
        blk.height = root.u64(kBlkHeight);
        blk.timestamp = static_cast<std::int64_t>(root.u64(kBlkTime));

        auto sr_span = root.bytes(kBlkStateRoot);
        blk.state_root.assign(sr_span.begin(), sr_span.end());

        auto list = root.list(kBlkTxLens);
        auto tx_blob = root.bytes(kBlkTxBlob);

        std::size_t off = 0;
        for (int i = 0; i < list.len(); ++i) {
            std::size_t len = list.u32(i);
            std::size_t end = off + len;
            if (end > tx_blob.size()) {
                return std::unexpected("tx length exceeds blob boundary");
            }
            std::span<const std::uint8_t> tx_slice(tx_blob.data() + off, len);
            auto tx = Transaction::parse(tx_slice);
            if (!tx) {
                return std::unexpected("failed to parse inner tx: " + tx.error());
            }
            blk.txs.push_back(std::move(*tx));
            off = end;
        }

        if (off != tx_blob.size()) {
            return std::unexpected("trailing bytes in tx blob");
        }

        blk.raw.assign(bytes.begin(), bytes.end());
        return blk;
    }

    Id id() const {
        return compute_hash(raw);
    }

    std::expected<void, std::string> verify() const {
        for (const auto& tx : txs) {
            auto res = tx.verify();
            if (!res) return res;
        }
        return {};
    }
};

class ZkVm {
public:
    Id last_accepted{};
    std::uint64_t last_accepted_height{0};
    std::set<std::vector<std::uint8_t>> nullifiers;
    std::map<Id, Block> blocks;
    std::vector<Transaction> mempool;

    ZkVm() = default;

    std::string_view alias() const { return "Z"; }

    std::expected<Id, std::string> issue_tx(Transaction tx) {
        auto res = tx.verify();
        if (!res) return std::unexpected(res.error());

        for (const auto& n : tx.nullifiers) {
            if (nullifiers.contains(n)) {
                return std::unexpected("nullifier already spent");
            }
        }
        auto tx_id = tx.id();
        mempool.push_back(std::move(tx));
        return tx_id;
    }

    std::expected<void, std::string> accept_block(const Id& id) {
        auto it = blocks.find(id);
        if (it == blocks.end()) return std::unexpected("block not found");
        for (const auto& tx : it->second.txs) {
            for (const auto& n : tx.nullifiers) {
                nullifiers.insert(n);
            }
        }
        last_accepted = id;
        last_accepted_height = it->second.height;
        return {};
    }

    std::expected<void, std::string> reject_block(const Id& id) {
        auto it = blocks.find(id);
        if (it != blocks.end()) {
            for (auto& tx : it->second.txs) {
                mempool.push_back(std::move(tx));
            }
            blocks.erase(it);
        }
        return {};
    }
};

}  // namespace lux::zkvm
