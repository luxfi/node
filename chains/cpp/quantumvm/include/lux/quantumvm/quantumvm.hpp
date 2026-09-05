// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#pragma once

#include "lux/quantumvm/sha256.hpp"
#include "lux/quantumvm/zap.hpp"

#include <array>
#include <cstdint>
#include <cstring>
#include <expected>
#include <map>
#include <memory>
#include <span>
#include <string>
#include <string_view>
#include <vector>

namespace lux::quantumvm {

using Id = std::array<std::uint8_t, 32>;

inline Id compute_hash(std::span<const std::uint8_t> data) {
    Sha256 s;
    s.update(data);
    return s.finish();
}

inline constexpr std::size_t kTxTime = 0;
inline constexpr std::size_t kTxNonce = 8;
inline constexpr std::size_t kTxData = 16;
inline constexpr std::size_t kTxSize = 24;

inline constexpr std::size_t kEnvBody = 0;
inline constexpr std::size_t kEnvAlg = 8;
inline constexpr std::size_t kEnvTime = 16;
inline constexpr std::size_t kEnvKey = 24;
inline constexpr std::size_t kEnvSig = 32;
inline constexpr std::size_t kEnvStamp = 40;
inline constexpr std::size_t kEnvSize = 48;

inline constexpr std::size_t kBlkTime = 0;
inline constexpr std::size_t kBlkHeight = 8;
inline constexpr std::size_t kBlkParent = 16;
inline constexpr std::size_t kBlkChain = 48;
inline constexpr std::size_t kBlkNetwork = 80;
inline constexpr std::size_t kBlkTxLens = 88;
inline constexpr std::size_t kBlkTxBlob = 96;
inline constexpr std::size_t kBlkSize = 104;

struct BaseTransaction {
    std::int64_t timestamp{0};
    std::uint64_t nonce{0};
    std::vector<std::uint8_t> data;
    std::vector<std::uint8_t> raw;

    static std::expected<BaseTransaction, std::string> parse(std::span<const std::uint8_t> bytes) {
        zap::Message msg;
        std::string err;
        if (!zap::Message::parse(bytes, &msg, &err)) return std::unexpected(err);
        auto root = msg.root();
        BaseTransaction tx;
        tx.timestamp = static_cast<std::int64_t>(root.u64(kTxTime));
        tx.nonce = root.u64(kTxNonce);
        auto d = root.bytes(kTxData);
        tx.data.assign(d.begin(), d.end());
        tx.raw.assign(bytes.begin(), bytes.end());
        return tx;
    }

    Id id() const {
        return compute_hash(raw);
    }
};

struct QuantumSignature {
    std::uint32_t algorithm{0};
    std::int64_t timestamp{0};
    std::vector<std::uint8_t> public_key;
    std::vector<std::uint8_t> signature;
    std::vector<std::uint8_t> quantum_stamp;
};

struct Transaction {
    BaseTransaction base;
    QuantumSignature sig;
    std::vector<std::uint8_t> raw;

    static std::expected<Transaction, std::string> parse(std::span<const std::uint8_t> bytes) {
        if (auto base = BaseTransaction::parse(bytes); base) {
            Transaction t;
            t.base = *base;
            t.raw.assign(bytes.begin(), bytes.end());
            return t;
        }
        zap::Message msg;
        std::string err;
        if (!zap::Message::parse(bytes, &msg, &err)) return std::unexpected(err);
        auto root = msg.root();
        auto body_bytes = root.bytes(kEnvBody);
        auto base = BaseTransaction::parse(body_bytes);
        if (!base) return std::unexpected("failed to parse base tx");

        Transaction t;
        t.base = *base;
        t.sig.algorithm = root.u32(kEnvAlg);
        t.sig.timestamp = static_cast<std::int64_t>(root.u64(kEnvTime));
        auto pk = root.bytes(kEnvKey);
        t.sig.public_key.assign(pk.begin(), pk.end());
        auto s = root.bytes(kEnvSig);
        t.sig.signature.assign(s.begin(), s.end());
        auto qs = root.bytes(kEnvStamp);
        t.sig.quantum_stamp.assign(qs.begin(), qs.end());
        t.raw.assign(bytes.begin(), bytes.end());
        return t;
    }

    Id id() const {
        return compute_hash(raw);
    }

    bool verify() const {
        if (sig.algorithm == 0 && sig.signature.empty()) return true;
        return !sig.public_key.empty() && !sig.signature.empty();
    }
};

struct Block {
    std::int64_t timestamp{0};
    std::uint64_t height{0};
    Id parent_id{};
    Id chain_id{};
    std::uint32_t network_id{0};
    std::vector<Transaction> txs;
    std::vector<std::uint8_t> raw;

    static std::expected<Block, std::string> parse(std::span<const std::uint8_t> bytes) {
        zap::Message msg;
        std::string err;
        if (!zap::Message::parse(bytes, &msg, &err)) return std::unexpected(err);
        auto root = msg.root();

        Block blk;
        blk.timestamp = static_cast<std::int64_t>(root.u64(kBlkTime));
        blk.height = root.u64(kBlkHeight);
        auto p = root.bytes_fixed_slice(kBlkParent, 32);
        std::memcpy(blk.parent_id.data(), p.data(), 32);
        auto c = root.bytes_fixed_slice(kBlkChain, 32);
        std::memcpy(blk.chain_id.data(), c.data(), 32);
        blk.network_id = root.u32(kBlkNetwork);

        auto lens = root.list(kBlkTxLens);
        auto blob = root.bytes(kBlkTxBlob);

        std::size_t off = 0;
        for (std::size_t i = 0; i < lens.len(); ++i) {
            std::size_t l = lens.u32(i);
            if (off + l > blob.size()) return std::unexpected("tx exceeds blob");
            auto tx = Transaction::parse(blob.subspan(off, l));
            if (!tx) return std::unexpected(tx.error());
            blk.txs.push_back(*tx);
            off += l;
        }
        blk.raw.assign(bytes.begin(), bytes.end());
        return blk;
    }

    Id id() const {
        return compute_hash(raw);
    }
};

class QuantumVm {
public:
    Id chain_id{};
    std::uint32_t network_id{0};
    Id last_accepted{};
    std::uint64_t last_accepted_height{0};
    std::map<Id, Block> blocks;
    std::vector<Transaction> mempool;

    QuantumVm(Id chain, std::uint32_t net) : chain_id(chain), network_id(net) {}

    std::string_view alias() const { return "Q"; }
};

} // namespace lux::quantumvm
