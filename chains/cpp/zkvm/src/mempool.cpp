// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/mempool.hpp"

#include <algorithm>

namespace lux::zkvm {

void Latch::signal() {
    {
        std::lock_guard<std::mutex> g(mu_);
        ready_ = true;
    }
    cv_.notify_one();
}

bool Latch::wait_for(std::chrono::milliseconds timeout) {
    std::unique_lock<std::mutex> g(mu_);
    if (!cv_.wait_for(g, timeout, [&] { return ready_; })) return false;
    ready_ = false;
    return true;
}

wire::Result<void> Mempool::add(Transaction tx) {
    {
        std::lock_guard<std::mutex> g(mu_);

        tx.id = tx.compute_id();

        if (txs_.count(tx.id)) return {};  // already held

        for (const auto& n : tx.nullifiers) {
            if (nullifiers_.count(n)) return std::unexpected(kErrNullifierInPool);
        }

        if (txs_.size() >= max_size_) {
            // The one paying least is what a full pool gives up.
            const Transaction* cheapest = nullptr;
            for (const auto& [id, held] : txs_) {
                if (cheapest == nullptr || held.fee < cheapest->fee) cheapest = &held;
            }
            if (cheapest == nullptr || cheapest->fee >= tx.fee)
                return std::unexpected(kErrPoolFull);
            remove_locked(cheapest->id);
        }

        for (const auto& n : tx.nullifiers) nullifiers_[n] = tx.id;
        const Id id = tx.id;
        txs_.emplace(id, std::move(tx));
    }

    // Consensus builds nothing until it is told there is something to build.
    work_.signal();
    return {};
}

void Mempool::remove(const Id& tx_id) {
    std::lock_guard<std::mutex> g(mu_);
    remove_locked(tx_id);
}

void Mempool::remove_locked(const Id& tx_id) {
    auto it = txs_.find(tx_id);
    if (it == txs_.end()) return;
    for (const auto& n : it->second.nullifiers) nullifiers_.erase(n);
    txs_.erase(it);
}

bool Mempool::has(const Id& tx_id) const {
    std::lock_guard<std::mutex> g(mu_);
    return txs_.count(tx_id) != 0;
}

std::size_t Mempool::size() const {
    std::lock_guard<std::mutex> g(mu_);
    return txs_.size();
}

std::vector<Transaction> Mempool::pending(std::size_t limit) const {
    std::lock_guard<std::mutex> g(mu_);
    std::vector<const Transaction*> order;
    order.reserve(txs_.size());
    for (const auto& [id, tx] : txs_) order.push_back(&tx);
    std::sort(order.begin(), order.end(), [](const Transaction* a, const Transaction* b) {
        if (a->fee != b->fee) return a->fee > b->fee;
        return a->id < b->id;
    });

    std::vector<Transaction> out;
    out.reserve(std::min(limit, order.size()));
    for (std::size_t i = 0; i < order.size() && i < limit; ++i) out.push_back(*order[i]);
    return out;
}

std::size_t Mempool::prune_expired(std::uint64_t current_height) {
    std::lock_guard<std::mutex> g(mu_);
    std::vector<Id> doomed;
    for (const auto& [id, tx] : txs_) {
        if (tx.expiry > 0 && tx.expiry < current_height) doomed.push_back(id);
    }
    for (const Id& id : doomed) remove_locked(id);
    return doomed.size();
}

}  // namespace lux::zkvm
