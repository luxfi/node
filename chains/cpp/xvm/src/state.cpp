// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/state.hpp"

#include "lux/xvm/zap.hpp"

namespace lux::xvm::state {
namespace {

// The metadata object: the three answers that are not a row of their own.
constexpr int kMetaOffTimestamp = 0;      // u64
constexpr int kMetaOffInitialized = 8;    // u8
constexpr int kMetaOffLastAccepted = 16;  // 32 inline bytes
constexpr int kMetaSize = 48;

Bytes key_of(std::uint8_t tag, ByteView rest) {
    Bytes k;
    k.reserve(1 + rest.size());
    k.push_back(tag);
    k.insert(k.end(), rest.begin(), rest.end());
    return k;
}

Bytes key_of(std::uint8_t tag) { return Bytes{tag}; }

// A height's key is big-endian so the store's ascending byte order IS ascending
// height order.
Bytes height_key(std::uint64_t height) {
    Bytes k{kTagHeight};
    for (int i = 7; i >= 0; --i) k.push_back(std::uint8_t(height >> (8 * i)));
    return k;
}

Bytes meta_bytes(std::uint64_t timestamp, bool initialized, const Id& last_accepted) {
    zap::Builder b(zap::kHeaderSize + kMetaSize + 32);
    auto ob = b.start_object(kMetaSize);
    ob.set_u64(kMetaOffTimestamp, timestamp);
    ob.set_u8(kMetaOffInitialized, initialized ? 1 : 0);
    ob.set_bytes_fixed(kMetaOffLastAccepted, view(last_accepted));
    ob.finish_as_root();
    return b.finish();
}

}  // namespace

// ================= State =================

Result<void> State::load() {
    utxos_.clear();
    txs_.clear();
    blocks_.clear();
    block_ids_.clear();
    staged_utxos_.clear();
    staged_txs_.clear();
    staged_blocks_.clear();
    staged_block_ids_.clear();
    staged_meta_ = false;
    last_accepted_ = kEmptyId;
    timestamp_ = 0;
    initialized_ = false;

    std::string fail;
    store_->each(view(key_of(kTagUtxo)), [&](ByteView key, ByteView val) {
        if (key.size() != 33) {
            fail = "utxo key is not a tag and an id";
            return false;
        }
        auto u = txs::parse_utxo(val);
        if (!u) {
            fail = "load utxo " + hex(key.subspan(1)) + ": " + u.error();
            return false;
        }
        utxos_[u->utxo_id.input_id()] = *u;
        return true;
    });
    if (!fail.empty()) return std::unexpected(fail);

    store_->each(view(key_of(kTagTx)), [&](ByteView key, ByteView val) {
        auto tx = txs::parse(val);
        if (!tx) {
            fail = "load tx " + hex(key.subspan(1)) + ": " + tx.error();
            return false;
        }
        txs_[(*tx)->id()] = *tx;
        return true;
    });
    if (!fail.empty()) return std::unexpected(fail);

    store_->each(view(key_of(kTagBlock)), [&](ByteView key, ByteView val) {
        auto blk = block::parse(val);
        if (!blk) {
            fail = "load block " + hex(key.subspan(1)) + ": " + blk.error();
            return false;
        }
        blocks_[(*blk)->block_id] = *blk;
        return true;
    });
    if (!fail.empty()) return std::unexpected(fail);

    store_->each(view(key_of(kTagHeight)), [&](ByteView key, ByteView val) {
        if (key.size() != 9 || val.size() != 32) {
            fail = "height row is not a height and an id";
            return false;
        }
        std::uint64_t h = 0;
        for (std::size_t i = 1; i < 9; ++i) h = (h << 8) | key[i];
        Id blk_id{};
        std::copy(val.begin(), val.end(), blk_id.begin());
        block_ids_[h] = blk_id;
        return true;
    });
    if (!fail.empty()) return std::unexpected(fail);

    if (auto meta = store_->get(view(key_of(kTagMeta)))) {
        zap::Message msg;
        std::string err;
        if (!zap::Message::parse(view(*meta), &msg, &err))
            return std::unexpected("load state metadata: " + err);
        const zap::Object root = msg.root();
        timestamp_ = root.u64(kMetaOffTimestamp);
        initialized_ = root.u8(kMetaOffInitialized) != 0;
        const auto la = root.bytes_fixed_slice(kMetaOffLastAccepted, 32);
        if (la.size() != 32) return std::unexpected("load state metadata: truncated last accepted");
        std::copy(la.begin(), la.end(), last_accepted_.begin());
    }
    return {};
}

Result<void> State::commit() {
    for (const auto& [utxo_id, val] : staged_utxos_) {
        const Bytes k = key_of(kTagUtxo, view(utxo_id));
        if (!val.has_value()) {
            store_->erase(view(k));
            continue;
        }
        auto b = val->wire_bytes();
        if (!b) return std::unexpected("commit utxo " + hex(utxo_id) + ": " + b.error());
        store_->put(view(k), view(*b));
    }
    staged_utxos_.clear();

    for (const auto& [tx_id, tx] : staged_txs_)
        store_->put(view(key_of(kTagTx, view(tx_id))), view(tx->bytes()));
    staged_txs_.clear();

    for (const auto& [blk_id, blk] : staged_blocks_)
        store_->put(view(key_of(kTagBlock, view(blk_id))), view(blk->bytes));
    staged_blocks_.clear();

    for (const auto& [height, blk_id] : staged_block_ids_)
        store_->put(view(height_key(height)), view(blk_id));
    staged_block_ids_.clear();

    if (staged_meta_) {
        const Bytes m = meta_bytes(timestamp_, initialized_, last_accepted_);
        store_->put(view(key_of(kTagMeta)), view(m));
        staged_meta_ = false;
    }
    return store_->commit();
}

void State::set_initialized() {
    initialized_ = true;
    staged_meta_ = true;
}

Result<txs::UTXO> State::get_utxo(const Id& utxo_id) const {
    auto it = utxos_.find(utxo_id);
    if (it == utxos_.end()) return std::unexpected(kErrNotFound);
    return it->second;
}

std::vector<txs::UTXO> State::utxos(const Id& start, int limit) const {
    std::vector<txs::UTXO> out;
    auto it = start == kEmptyId ? utxos_.begin() : utxos_.upper_bound(start);
    for (; it != utxos_.end(); ++it) {
        if (limit > 0 && int(out.size()) >= limit) break;
        out.push_back(it->second);
    }
    return out;
}

Result<std::shared_ptr<txs::Tx>> State::get_tx(const Id& tx_id) const {
    auto it = txs_.find(tx_id);
    if (it == txs_.end()) return std::unexpected(kErrNotFound);
    return it->second;
}

Result<Id> State::get_block_id_at_height(std::uint64_t height) const {
    auto it = block_ids_.find(height);
    if (it == block_ids_.end()) return std::unexpected(kErrNotFound);
    return it->second;
}

Result<std::shared_ptr<block::StandardBlock>> State::get_block(const Id& blk_id) const {
    auto it = blocks_.find(blk_id);
    if (it == blocks_.end()) return std::unexpected(kErrNotFound);
    return it->second;
}

void State::add_utxo(const txs::UTXO& utxo) {
    const Id key = utxo.utxo_id.input_id();
    utxos_[key] = utxo;
    staged_utxos_[key] = utxo;
}
void State::delete_utxo(const Id& utxo_id) {
    utxos_.erase(utxo_id);
    staged_utxos_[utxo_id] = std::nullopt;
}
void State::add_tx(std::shared_ptr<txs::Tx> tx) {
    const Id key = tx->id();
    staged_txs_[key] = tx;
    txs_[key] = std::move(tx);
}
void State::add_block(std::shared_ptr<block::StandardBlock> blk) {
    block_ids_[blk->height] = blk->block_id;
    staged_block_ids_[blk->height] = blk->block_id;
    const Id key = blk->block_id;
    staged_blocks_[key] = blk;
    blocks_[key] = std::move(blk);
}
void State::set_last_accepted(const Id& blk_id) {
    last_accepted_ = blk_id;
    staged_meta_ = true;
}
void State::set_timestamp(std::uint64_t t) {
    timestamp_ = t;
    staged_meta_ = true;
}

// ================= Diff =================

Diff::Diff(const Id& parent_id, Versions& versions, Chain& parent)
    : parent_id_(parent_id), versions_(&versions), parent_(&parent) {
    last_accepted_ = parent.get_last_accepted();
    timestamp_ = parent.get_timestamp();
}

Diff::Diff(const Id& parent_id, Chain& parent)
    : parent_id_(parent_id), versions_(nullptr), parent_(&parent) {
    last_accepted_ = parent.get_last_accepted();
    timestamp_ = parent.get_timestamp();
}

Result<std::unique_ptr<Diff>> Diff::create(const Id& parent_id, Versions& versions) {
    Chain* parent = versions.get_state(parent_id);
    if (parent == nullptr) return std::unexpected(kErrMissingParentState);
    return std::unique_ptr<Diff>(new Diff(parent_id, versions, *parent));
}

std::unique_ptr<Diff> Diff::on(Chain& parent) {
    // A diff over a chain rather than over a block id: there is nothing to
    // resolve, so there is no Versions to resolve it with, and the parent's own
    // last accepted block is the position this one records over.
    auto d = std::unique_ptr<Diff>(new Diff(parent.get_last_accepted(), parent));
    return d;
}

Result<txs::UTXO> Diff::get_utxo(const Id& utxo_id) const {
    auto it = modified_utxos_.find(utxo_id);
    if (it != modified_utxos_.end()) {
        // A recorded nullopt is a DELETE, and it must read as absent rather than
        // fall through to the parent — otherwise a spent UTXO reappears.
        if (!it->second.has_value()) return std::unexpected(kErrNotFound);
        return *it->second;
    }
    return parent_->get_utxo(utxo_id);
}

std::vector<txs::UTXO> Diff::utxos(const Id& start, int limit) const {
    // The occupied set as this diff sees it: the parent's, with the recorded
    // deletions removed and the recorded additions merged in — still in
    // ascending UTXOID order, because that order is what an execution root
    // folds over.
    std::map<Id, txs::UTXO> merged;
    for (const auto& u : parent_->utxos(kEmptyId, 0)) merged[u.utxo_id.input_id()] = u;
    for (const auto& [key, val] : modified_utxos_) {
        if (val.has_value()) {
            merged[key] = *val;
        } else {
            merged.erase(key);
        }
    }
    std::vector<txs::UTXO> out;
    auto it = start == kEmptyId ? merged.begin() : merged.upper_bound(start);
    for (; it != merged.end(); ++it) {
        if (limit > 0 && int(out.size()) >= limit) break;
        out.push_back(it->second);
    }
    return out;
}

Result<std::shared_ptr<txs::Tx>> Diff::get_tx(const Id& tx_id) const {
    auto it = added_txs_.find(tx_id);
    if (it != added_txs_.end()) return it->second;
    return parent_->get_tx(tx_id);
}

Result<Id> Diff::get_block_id_at_height(std::uint64_t height) const {
    auto it = added_block_ids_.find(height);
    if (it != added_block_ids_.end()) return it->second;
    return parent_->get_block_id_at_height(height);
}

Result<std::shared_ptr<block::StandardBlock>> Diff::get_block(const Id& blk_id) const {
    auto it = added_blocks_.find(blk_id);
    if (it != added_blocks_.end()) return it->second;
    return parent_->get_block(blk_id);
}

void Diff::add_utxo(const txs::UTXO& utxo) { modified_utxos_[utxo.utxo_id.input_id()] = utxo; }
void Diff::delete_utxo(const Id& utxo_id) { modified_utxos_[utxo_id] = std::nullopt; }
void Diff::add_tx(std::shared_ptr<txs::Tx> tx) { added_txs_[tx->id()] = std::move(tx); }
void Diff::add_block(std::shared_ptr<block::StandardBlock> blk) {
    added_block_ids_[blk->height] = blk->block_id;
    added_blocks_[blk->block_id] = std::move(blk);
}

void Diff::apply(Chain& target) const {
    for (const auto& [key, val] : modified_utxos_) {
        if (val.has_value()) {
            target.add_utxo(*val);
        } else {
            target.delete_utxo(key);
        }
    }
    for (const auto& [_, tx] : added_txs_) target.add_tx(tx);
    for (const auto& [_, blk] : added_blocks_) target.add_block(blk);
    target.set_last_accepted(last_accepted_);
    target.set_timestamp(timestamp_);
}

// ================= consume / produce =================

void consume(Chain& chain, const std::vector<txs::TransferableInput>& ins) {
    for (const auto& in : ins) chain.delete_utxo(in.utxo_id.input_id());
}

void produce(Chain& chain, const Id& tx_id, const std::vector<txs::TransferableOutput>& outs) {
    for (std::size_t i = 0; i < outs.size(); ++i) {
        chain.add_utxo(txs::UTXO{txs::UTXOID{tx_id, std::uint32_t(i), false}, outs[i].asset_id,
                                 std::static_pointer_cast<fx::FxOutput>(outs[i].out)});
    }
}

}  // namespace lux::xvm::state
