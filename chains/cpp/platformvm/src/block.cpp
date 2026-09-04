// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.cpp — building and reading the block wire.
//
// Rendered from Go vms/platformvm/block/blockwire.go. The tx list is written
// into the builder's variable section BEFORE the object, so the object's list
// pointer is a backward reference — the same order the transaction package
// writes its lists in, and the reason both produce identical bytes.

#include "lux/platformvm/block.hpp"

#include <cstring>

namespace lux::platformvm::block {

// The one place a block's bytes are bound and its id derived.
struct Wire {
    static Status bind(Block& b, std::vector<std::uint8_t> v) { return b.set_bytes(std::move(v)); }
    static zap::Object root(const Block& b) { return b.root(); }
};

namespace {

// The per-tx byte lengths as a u32 list, plus their bytes concatenated. A tx is
// already self-describing, so the block stores lengths rather than re-encoding.
Result<std::pair<std::pair<std::int64_t, std::int64_t>, std::vector<std::uint8_t>>> write_tx_list(
    zap::Builder& b, const std::vector<txs::Tx>& decision_txs) {
    if (decision_txs.empty()) return std::make_pair(std::make_pair(std::int64_t{0}, std::int64_t{0}),
                                                    std::vector<std::uint8_t>{});
    std::vector<std::uint8_t> blob;
    auto lb = b.start_list(kTxLenStride);
    for (std::size_t i = 0; i < decision_txs.size(); ++i) {
        const auto& raw = decision_txs[i].bytes;
        if (raw.empty()) return fail(Err::NilBlockTx, "tx " + std::to_string(i) + " carries no bytes");
        lb.add_u32(static_cast<std::uint32_t>(raw.size()));
        blob.insert(blob.end(), raw.begin(), raw.end());
    }
    return std::make_pair(std::make_pair(lb.offset(), lb.count()), std::move(blob));
}

std::int64_t size_of(Kind k) {
    switch (k) {
        case Kind::Standard: return kSizeStandard;
        case Kind::Proposal: return kSizeProposal;
        case Kind::Abort:
        case Kind::Commit: return kSizeDecided;
    }
    return kSizeDecided;
}

// The one place block fields become bytes.
Result<std::vector<std::uint8_t>> build(Kind k, const Id& parent, std::uint64_t height, std::uint64_t ts,
                                        const std::vector<txs::Tx>& decision_txs, const txs::Tx* proposal_tx) {
    zap::Builder b(zap::kHeaderSize + 256);

    std::int64_t len_off = 0, len_count = 0;
    std::vector<std::uint8_t> blob;
    const bool has_tx_list = k == Kind::Standard || k == Kind::Proposal;
    if (has_tx_list) {
        auto w = write_tx_list(b, decision_txs);
        if (!w) return std::unexpected(w.error());
        len_off = w->first.first;
        len_count = w->first.second;
        blob = std::move(w->second);
    }

    auto ob = b.start_object(size_of(k));
    ob.set_u8(kOffKind, static_cast<std::uint8_t>(k));
    ob.set_bytes_fixed(kOffParent, parent.span());
    ob.set_u64(kOffHeight, height);
    ob.set_u64(kOffTime, ts);
    if (has_tx_list) {
        ob.set_list(kOffTxLengths, len_off, len_count);
        ob.set_bytes(kOffTxBlob, blob);
    }
    if (k == Kind::Proposal) {
        // The slot must carry BYTES, not merely a non-null pointer: a reader of
        // an empty slot sees nothing there, so the question is asked here, where
        // the block is made, rather than at the dereference.
        if (proposal_tx == nullptr || proposal_tx->bytes.empty()) return fail(Err::NoProposalTx);
        ob.set_bytes(kOffProposalTx, proposal_tx->bytes);
    }
    ob.finish_as_root();
    return b.finish();
}

// Re-split the blob by the stored lengths and hand each slice to txs::parse.
Result<std::vector<txs::Tx>> read_tx_list(const zap::Object& obj) {
    const auto lengths = obj.list_stride(kOffTxLengths, kTxLenStride);
    const int n = lengths.size();
    if (n == 0) return std::vector<txs::Tx>{};
    const auto blob = obj.bytes(kOffTxBlob);
    std::vector<txs::Tx> out;
    out.reserve(static_cast<std::size_t>(n));
    std::size_t cursor = 0;
    for (int i = 0; i < n; ++i) {
        const std::size_t size = lengths.u32(i);
        if (cursor + size > blob.size())
            return fail(Err::TxOverrunsBlob, "tx " + std::to_string(i) + " length " + std::to_string(size) +
                                                 " overruns blob of " + std::to_string(blob.size()));
        auto tx = txs::parse(blob.subspan(cursor, size));
        if (!tx) return std::unexpected(tx.error());
        out.push_back(std::move(tx.value()));
        cursor += size;
    }
    return out;
}

}  // namespace

Status Block::set_bytes(std::vector<std::uint8_t> b) {
    const auto msg = zap::Message::parse(b);
    if (!msg) return fail(Err::BufferTooSmall, "block buffer is not a zap message");
    // zap::Message truncates to the declared size, so a buffer with a tail wraps
    // the same message under a different id. Refuse it.
    if (msg->size() != b.size()) return fail(Err::BlockExtraSpace);
    id_ = id_from_hash(sha256(b));
    buf_ = std::move(b);
    return ok();
}

zap::Object Block::root() const {
    const auto msg = zap::Message::parse({buf_.data(), buf_.size()});
    return msg ? msg->root() : zap::Object{};
}

Id Block::parent() const {
    return Id::from(root().bytes_fixed(kOffParent, kIdLen));
}
std::uint64_t Block::height() const { return root().u64(kOffHeight); }
std::uint64_t Block::timestamp() const { return root().u64(kOffTime); }

namespace {
// One shape for all four constructors: build, then bind bytes and id.
template <class B>
Result<std::shared_ptr<B>> make(Kind k, std::uint64_t ts, const Id& parent, std::uint64_t height,
                                const std::vector<txs::Tx>& decision_txs, const txs::Tx* proposal_tx) {
    auto bytes = build(k, parent, height, ts, decision_txs, proposal_tx);
    if (!bytes) return std::unexpected(bytes.error());
    auto blk = std::make_shared<B>();
    if (auto s = Wire::bind(*blk, std::move(bytes.value())); !s)
        return std::unexpected(s.error());
    return blk;
}
}  // namespace

Result<std::shared_ptr<AbortBlock>> AbortBlock::create(std::uint64_t timestamp, const Id& parent,
                                                       std::uint64_t height) {
    return make<AbortBlock>(Kind::Abort, timestamp, parent, height, {}, nullptr);
}

Result<std::shared_ptr<CommitBlock>> CommitBlock::create(std::uint64_t timestamp, const Id& parent,
                                                         std::uint64_t height) {
    return make<CommitBlock>(Kind::Commit, timestamp, parent, height, {}, nullptr);
}

Result<std::shared_ptr<StandardBlock>> StandardBlock::create(std::uint64_t timestamp, const Id& parent,
                                                             std::uint64_t height,
                                                             const std::vector<txs::Tx>& decision_txs) {
    return make<StandardBlock>(Kind::Standard, timestamp, parent, height, decision_txs, nullptr);
}

Result<std::shared_ptr<ProposalBlock>> ProposalBlock::create(std::uint64_t timestamp, const Id& parent,
                                                             std::uint64_t height, const txs::Tx& proposal_tx,
                                                             const std::vector<txs::Tx>& decision_txs) {
    return make<ProposalBlock>(Kind::Proposal, timestamp, parent, height, decision_txs, &proposal_tx);
}

std::vector<txs::Tx> StandardBlock::decision_txs() const {
    auto r = read_tx_list(root());
    return r ? std::move(r.value()) : std::vector<txs::Tx>{};
}

std::vector<txs::Tx> ProposalBlock::decision_txs() const {
    auto r = read_tx_list(root());
    return r ? std::move(r.value()) : std::vector<txs::Tx>{};
}

Result<txs::Tx> ProposalBlock::tx() const {
    const auto raw = root().bytes(kOffProposalTx);
    if (raw.empty()) return fail(Err::NoProposalTx);
    return txs::parse(raw);
}

Status AbortBlock::visit(Visitor& v) const { return v.abort_block(*this); }
Status CommitBlock::visit(Visitor& v) const { return v.commit_block(*this); }
Status StandardBlock::visit(Visitor& v) const { return v.standard_block(*this); }
Status ProposalBlock::visit(Visitor& v) const { return v.proposal_block(*this); }

Result<std::shared_ptr<Block>> parse(std::span<const std::uint8_t> b) {
    const auto msg = zap::Message::parse(b);
    if (!msg) return fail(Err::BufferTooSmall, "block buffer is not a zap message");
    std::vector<std::uint8_t> owned(b.begin(), b.end());
    switch (static_cast<Kind>(msg->root().u8(kOffKind))) {
        case Kind::Abort: {
            auto blk = std::make_shared<AbortBlock>();
            if (auto s = Wire::bind(*blk, std::move(owned)); !s)
                return std::unexpected(s.error());
            return blk;
        }
        case Kind::Commit: {
            auto blk = std::make_shared<CommitBlock>();
            if (auto s = Wire::bind(*blk, std::move(owned)); !s)
                return std::unexpected(s.error());
            return blk;
        }
        case Kind::Standard: {
            auto blk = std::make_shared<StandardBlock>();
            if (auto s = Wire::bind(*blk, std::move(owned)); !s)
                return std::unexpected(s.error());
            // The tx list must decode, or the block is not a block.
            if (auto l = read_tx_list(Wire::root(*blk)); !l) return std::unexpected(l.error());
            return blk;
        }
        case Kind::Proposal: {
            auto blk = std::make_shared<ProposalBlock>();
            if (auto s = Wire::bind(*blk, std::move(owned)); !s)
                return std::unexpected(s.error());
            if (auto t = blk->tx(); !t) return std::unexpected(t.error());
            if (auto l = read_tx_list(Wire::root(*blk)); !l) return std::unexpected(l.error());
            return blk;
        }
    }
    return fail(Err::UnknownBlockKind,
                "block kind " + std::to_string(static_cast<int>(msg->root().u8(kOffKind))));
}

}  // namespace lux::platformvm::block
