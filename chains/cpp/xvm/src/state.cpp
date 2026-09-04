// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/state.hpp"

namespace lux::xvm::state {

// ================= State =================

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

void State::add_utxo(const txs::UTXO& utxo) { utxos_[utxo.utxo_id.input_id()] = utxo; }
void State::delete_utxo(const Id& utxo_id) { utxos_.erase(utxo_id); }
void State::add_tx(std::shared_ptr<txs::Tx> tx) { txs_[tx->id()] = std::move(tx); }
void State::add_block(std::shared_ptr<block::StandardBlock> blk) {
    block_ids_[blk->height] = blk->block_id;
    blocks_[blk->block_id] = std::move(blk);
}

// ================= Diff =================

Diff::Diff(const Id& parent_id, Versions& versions, Chain& parent)
    : parent_id_(parent_id), versions_(&versions), parent_(&parent) {
    last_accepted_ = parent.get_last_accepted();
    timestamp_ = parent.get_timestamp();
}

Result<std::unique_ptr<Diff>> Diff::create(const Id& parent_id, Versions& versions) {
    Chain* parent = versions.get_state(parent_id);
    if (parent == nullptr) return std::unexpected(kErrMissingParentState);
    return std::unique_ptr<Diff>(new Diff(parent_id, versions, *parent));
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
