// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/block.hpp"

#include <cstring>

namespace lux::xvm::block {
namespace {

constexpr int kOffParent = 0;   // 32B
constexpr int kOffHeight = 32;  // u64
constexpr int kOffTime = 40;    // u64
constexpr int kOffRoot = 48;    // 32B
constexpr int kOffTxLen = 80;   // u32 list ptr — one entry per tx
constexpr int kOffTxBlob = 88;  // bytes ptr — concat of each tx's own bytes
constexpr int kSizeBlk = 96;

constexpr std::uint32_t kTxLenStride = 4;

Id id_at(const zap::Object& obj, int off) {
    Id out{};
    auto s = obj.bytes_fixed_slice(off, 32);
    if (s.size() == 32) std::memcpy(out.data(), s.data(), 32);
    return out;
}

}  // namespace

Result<std::shared_ptr<StandardBlock>> build(const Id& parent_id, std::uint64_t height,
                                             std::uint64_t unix_time, const Id& root,
                                             std::vector<std::shared_ptr<txs::Tx>> transactions) {
    zap::Builder b(zap::kHeaderSize + kSizeBlk + 256);

    Bytes blob;
    int len_off = 0, len_count = 0;
    if (!transactions.empty()) {
        auto lb = b.start_list(int(kTxLenStride));
        for (std::size_t i = 0; i < transactions.size(); ++i) {
            const auto& tx = transactions[i];
            if (tx == nullptr) return std::unexpected("nil tx at index " + std::to_string(i));
            // Block txs arrive from the mempool already initialized. An empty
            // byte slot means an uninitialized tx reached block construction —
            // a caller bug, not something to paper over with a hidden
            // re-serialization here.
            if (tx->bytes().empty())
                return std::unexpected("tx " + std::to_string(i) +
                                       " has no wire bytes (not initialized before block build)");
            lb.add_u32(std::uint32_t(tx->bytes().size()));
            blob.insert(blob.end(), tx->bytes().begin(), tx->bytes().end());
        }
        auto [off, count] = lb.finish();
        len_off = off;
        len_count = count;
    }

    auto ob = b.start_object(kSizeBlk);
    ob.set_bytes_fixed(kOffParent, view(parent_id));
    ob.set_u64(kOffHeight, height);
    ob.set_u64(kOffTime, unix_time);
    ob.set_bytes_fixed(kOffRoot, view(root));
    ob.set_list(kOffTxLen, len_off, len_count);
    ob.set_bytes(kOffTxBlob, view(blob));
    ob.finish_as_root();

    auto blk = std::make_shared<StandardBlock>();
    blk->parent_id = parent_id;
    blk->height = height;
    blk->time = unix_time;
    blk->root = root;
    blk->transactions = std::move(transactions);
    blk->bytes = b.finish();
    blk->block_id = sha256(view(blk->bytes));
    return blk;
}

Result<std::shared_ptr<StandardBlock>> parse(ByteView bytes) {
    zap::Message msg;
    std::string err;
    if (!zap::Message::parse(bytes, &msg, &err))
        return std::unexpected("couldn't parse block: " + err);
    auto obj = msg.root();

    auto blk = std::make_shared<StandardBlock>();
    blk->parent_id = id_at(obj, kOffParent);
    blk->height = obj.u64(kOffHeight);
    blk->time = obj.u64(kOffTime);
    blk->root = id_at(obj, kOffRoot);

    auto lengths = obj.list_stride(kOffTxLen, kTxLenStride);
    ByteView blob = obj.bytes(kOffTxBlob);
    std::size_t cursor = 0;
    for (int i = 0; i < lengths.len(); ++i) {
        std::size_t size = std::size_t(lengths.u32(i));
        if (cursor + size > blob.size())
            return std::unexpected("block: tx " + std::to_string(i) + " length overruns blob");
        auto tx = txs::parse(blob.subspan(cursor, size));
        if (!tx)
            return std::unexpected("block: parse tx " + std::to_string(i) + ": " + tx.error());
        blk->transactions.push_back(*tx);
        cursor += size;
    }

    blk->bytes.assign(bytes.begin(), bytes.end());
    blk->block_id = sha256(bytes);
    return blk;
}

}  // namespace lux::xvm::block
