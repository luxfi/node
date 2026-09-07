// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.cpp — what a block MEANS, over the wire the schema states.
//
// Three shapes share one prefix, and the kind byte says which was written:
// a decided block stops at its timestamp, a standard block carries the
// transactions it applies, and a proposal block carries one more of its own.
// The offsets are not here — they are in schema/wire.zap, through zapgen.
//
// A transaction is already self-describing, so a block stores the per-tx
// LENGTHS beside one concatenated blob rather than re-encoding anything: the
// bytes a signer signed are the bytes the block carries.

#include "lux/platformvm/block.hpp"

#include "lux/platformvm/txs_wire.hpp"

#include <cstring>

namespace lux::platformvm::block {

// The one place a block's bytes are bound and its id derived.
struct Wire {
    static Status bind(Block& b, std::vector<std::uint8_t> v) { return b.set_bytes(std::move(v)); }
    static zap::Object root(const Block& b) { return b.root(); }
};

namespace {

// The per-tx byte lengths and their bytes concatenated.
struct TxList {
    std::vector<std::uint32_t> lengths;
    std::vector<std::uint8_t> blob;
};

Result<TxList> tx_list(const std::vector<txs::Tx>& decision_txs) {
    TxList l;
    l.lengths.reserve(decision_txs.size());
    for (std::size_t i = 0; i < decision_txs.size(); ++i) {
        const auto& raw = decision_txs[i].bytes;
        if (raw.empty()) return fail(Err::NilBlockTx, "tx " + std::to_string(i) + " carries no bytes");
        l.lengths.push_back(static_cast<std::uint32_t>(raw.size()));
        l.blob.insert(l.blob.end(), raw.begin(), raw.end());
    }
    return l;
}

// The one place block fields become bytes.
Result<std::vector<std::uint8_t>> build(Kind k, const Id& parent, std::uint64_t height, std::uint64_t ts,
                                        const std::vector<txs::Tx>& decision_txs, const txs::Tx* proposal_tx) {
    const auto kind = static_cast<std::uint8_t>(k);
    if (k == Kind::Abort || k == Kind::Commit)
        return wire::NewDecided(wire::DecidedInput{
            .Kind = kind, .Parent = parent, .Height = height, .Time = ts});

    auto l = tx_list(decision_txs);
    if (!l) return std::unexpected(l.error());

    if (k == Kind::Standard)
        return wire::NewStandard(wire::StandardInput{.Kind = kind,
                                                     .Parent = parent,
                                                     .Height = height,
                                                     .Time = ts,
                                                     .TxLengths = std::move(l->lengths),
                                                     .TxBlob = {l->blob.data(), l->blob.size()}});

    // The proposal slot must carry BYTES, not merely a non-null pointer: a
    // reader of an empty slot sees nothing there, so the question is asked
    // here, where the block is made, rather than at the dereference.
    if (proposal_tx == nullptr || proposal_tx->bytes.empty()) return fail(Err::NoProposalTx);
    return wire::NewProposal(wire::ProposalInput{
        .Kind = kind,
        .Parent = parent,
        .Height = height,
        .Time = ts,
        .TxLengths = std::move(l->lengths),
        .TxBlob = {l->blob.data(), l->blob.size()},
        .ProposalTx = {proposal_tx->bytes.data(), proposal_tx->bytes.size()}});
}

// Re-split the blob by the stored lengths and hand each slice to txs::parse.
// A proposal block's list is at a standard block's offsets — that is what the
// shared prefix means — so one reader answers for both.
Result<std::vector<txs::Tx>> read_tx_list(const zap::Object& obj) {
    const wire::Standard b(obj);
    const auto lengths = b.TxLengths();
    const int n = lengths.size();
    if (n == 0) return std::vector<txs::Tx>{};
    const auto blob = b.TxBlob();
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
    id_ = sha256(b);
    buf_ = std::move(b);
    return ok();
}

zap::Object Block::root() const {
    const auto msg = zap::Message::parse({buf_.data(), buf_.size()});
    return msg ? msg->root() : zap::Object{};
}

// The four fields every kind opens with, read through the shortest kind that
// has them: the prefix is where all three shapes agree.
Id Block::parent() const { return id_from(wire::Decided(root()).Parent()); }
std::uint64_t Block::height() const { return wire::Decided(root()).Height(); }
std::uint64_t Block::timestamp() const { return wire::Decided(root()).Time(); }

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
    const auto raw = wire::Proposal(root()).ProposalTx();
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
    switch (static_cast<Kind>(wire::Decided(msg->root()).Kind())) {
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
                "block kind " + std::to_string(static_cast<int>(wire::Decided(msg->root()).Kind())));
}

}  // namespace lux::platformvm::block
