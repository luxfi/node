// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.cpp — what is waiting to go into a block.
//
// Rendered from Go vms/txs/mempool/mempool.go and
// vms/platformvm/txs/mempool/mempool.go.

#include "lux/platformvm/mempool.hpp"

#include <algorithm>

namespace lux::platformvm::mempool {

Status Pool::add(const txs::Tx& tx) {
    if (tx.unsigned_tx == nullptr) return fail(Err::InvalidState, "a transaction with nothing in it");

    // The chain's own transaction. Nobody submits it: the builder produces it
    // from state, so one arriving from outside is either a mistake or an
    // attempt to choose the chain's own business.
    if (tx.unsigned_tx->kind() == txs::Kind::RewardValidator)
        return fail(Err::CantIssueRewardValidatorTx, tx.tx_id.hex());

    if (by_id_.count(tx.tx_id) != 0) return fail(Err::DuplicateTx, tx.tx_id.hex());

    const std::size_t size = tx.bytes.size();
    if (size > kMaxTxSize)
        return fail(Err::TxTooLarge, tx.tx_id.hex() + " is " + std::to_string(size) + " bytes, over " +
                                         std::to_string(kMaxTxSize));
    if (size > available_)
        return fail(Err::MempoolFull, tx.tx_id.hex() + " is " + std::to_string(size) +
                                          " bytes, and there is room for " + std::to_string(available_));

    // Two transactions spending one output cannot both be accepted, so holding
    // both is holding one of them for nothing.
    const auto inputs = tx.unsigned_tx->input_ids();
    for (const auto& in : inputs)
        if (consumed_.count(in) != 0) return fail(Err::ConflictsWithOtherTx, tx.tx_id.hex());

    auto at = order_.insert(order_.end(), tx.tx_id);
    by_id_.emplace(tx.tx_id, Held{tx, at, inputs});
    for (const auto& in : inputs) consumed_.emplace(in, tx.tx_id);
    available_ -= size;

    // A transaction that is here is not a transaction that was refused.
    if (dropped_.erase(tx.tx_id) != 0)
        dropped_order_.erase(std::find(dropped_order_.begin(), dropped_order_.end(), tx.tx_id));
    return ok();
}

std::optional<txs::Tx> Pool::get(const Id& tx_id) const {
    const auto it = by_id_.find(tx_id);
    if (it == by_id_.end()) return std::nullopt;
    return it->second.tx;
}

std::optional<txs::Tx> Pool::peek() const {
    if (order_.empty()) return std::nullopt;
    return by_id_.at(order_.front()).tx;
}

std::vector<txs::Tx> Pool::peek(std::size_t n) const {
    std::vector<txs::Tx> out;
    out.reserve(std::min(n, by_id_.size()));
    for (const auto& id : order_) {
        if (out.size() >= n) break;
        out.push_back(by_id_.at(id).tx);
    }
    return out;
}

void Pool::erase(const Id& tx_id) {
    const auto it = by_id_.find(tx_id);
    if (it == by_id_.end()) return;
    for (const auto& in : it->second.inputs) consumed_.erase(in);
    available_ += it->second.tx.bytes.size();
    order_.erase(it->second.at);
    by_id_.erase(it);
}

void Pool::remove(const std::vector<txs::Tx>& gone) {
    for (const auto& tx : gone) {
        if (by_id_.count(tx.tx_id) != 0) {
            erase(tx.tx_id);
            continue;
        }
        // Not here itself, so take out whatever was waiting to spend the same
        // outputs: it can never be accepted now.
        if (tx.unsigned_tx == nullptr) continue;
        std::vector<Id> rivals;
        for (const auto& in : tx.unsigned_tx->input_ids()) {
            const auto it = consumed_.find(in);
            if (it != consumed_.end()) rivals.push_back(it->second);
        }
        for (const auto& id : rivals) erase(id);
    }
}

void Pool::mark_dropped(const Id& tx_id, const Error& reason) {
    // A full pool says nothing about the transaction, only about the moment it
    // arrived.
    if (reason.code == Err::MempoolFull) return;
    // And a transaction that is waiting is not dropped, whatever anyone says
    // about it: the pool holds it, and that is the fact.
    if (by_id_.count(tx_id) != 0) return;
    if (dropped_.find(tx_id) == dropped_.end()) {
        dropped_order_.push_back(tx_id);
        if (dropped_order_.size() > kDroppedRemembered) {
            dropped_.erase(dropped_order_.front());
            dropped_order_.pop_front();
        }
    }
    dropped_[tx_id] = reason;
}

std::optional<Error> Pool::drop_reason(const Id& tx_id) const {
    const auto it = dropped_.find(tx_id);
    if (it == dropped_.end()) return std::nullopt;
    return it->second;
}

std::vector<Id> Pool::drop_expired_stakers(std::uint64_t min_start_time) {
    std::vector<Id> dropped;
    for (const auto& id : order_) {
        const auto& held = by_id_.at(id);
        auto view = txs::staker_of(*held.tx.unsigned_tx);
        // A transaction that admits no staker has no start to be past, and one
        // whose staker cannot even be read is somebody else's refusal to make.
        if (!view || !view.value()) continue;
        if (view.value()->start < min_start_time) dropped.push_back(id);
    }
    for (const auto& id : dropped) erase(id);
    return dropped;
}

}  // namespace lux::platformvm::mempool
