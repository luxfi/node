// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/transaction.hpp"

#include "lux/quantumvm/wire.hpp"

#include <algorithm>

namespace lux::quantumvm {

ByteView BaseTransaction::bytes() const {
    if (!wire_built_) {
        wire_ = wire::tx_body_bytes(timestamp_, nonce_, view(data_));
        wire_built_ = true;
    }
    return view(wire_);
}

Id BaseTransaction::id() const {
    if (!id_) id_ = of(bytes());
    return *id_;
}

Status BaseTransaction::verify() const {
    if (!sig_) return fail(Err::MissingStamp);
    return ok();
}

// ── the pool

void TransactionPool::signal_locked() {
    signalled_ = true;
    work_.notify_one();
}

Status TransactionPool::add(const TxPtr& tx) {
    std::unique_lock lock(mu_);

    if (closed_) return fail(Err::PoolClosed);
    if (pending_.size() >= max_size_) return fail(Err::PoolFull);

    const Id tx_id = tx->id();
    if (pending_.find(tx_id) != pending_.end()) return fail(Err::DuplicateTx);
    if (auto v = tx->verify(); !v) return v;

    pending_.emplace(tx_id, tx);
    queue_.push_back(tx);

    // Consensus builds nothing until it is told there is something to build.
    signal_locked();
    return ok();
}

Status TransactionPool::remove(const Id& tx_id) {
    std::unique_lock lock(mu_);

    auto it = pending_.find(tx_id);
    if (it == pending_.end()) return fail(Err::TxNotInPool);
    pending_.erase(it);

    // The map decides admission and the queue decides selection; a transaction
    // left in either one is either an unusable slot or a transaction that gets
    // built into a second block.
    queue_.erase(std::remove_if(queue_.begin(), queue_.end(),
                                [&](const TxPtr& tx) { return tx->id() == tx_id; }),
                 queue_.end());
    return ok();
}

std::vector<TxPtr> TransactionPool::pending(std::size_t limit) const {
    std::unique_lock lock(mu_);
    if (limit == 0 || limit > queue_.size()) limit = queue_.size();
    return std::vector<TxPtr>(queue_.begin(), queue_.begin() + static_cast<std::ptrdiff_t>(limit));
}

std::size_t TransactionPool::count() const {
    std::unique_lock lock(mu_);
    return pending_.size();
}

bool TransactionPool::wait_for_work(Duration timeout) {
    std::unique_lock lock(mu_);
    if (!work_.wait_for(lock, timeout, [&] { return signalled_; })) return false;
    signalled_ = false;
    return true;
}

void TransactionPool::signal_if_work() {
    std::unique_lock lock(mu_);
    if (!queue_.empty()) signal_locked();
}

void TransactionPool::close() {
    std::unique_lock lock(mu_);
    closed_ = true;
    pending_.clear();
    queue_.clear();
}

// ── selection

Verdict process_batch(const std::vector<TxPtr>& txs, const quantum::QuantumSigner& signer,
                      bool stamps_enabled, int gpu_threshold) {
    Verdict out;

    // Phase 1: basic validation. A missing signature is caught here, by the
    // transaction itself; phase 2 decides whether the one present is good.
    std::vector<TxPtr> verified;
    for (const auto& tx : txs) {
        if (!tx->verify()) {
            out.rejected.push_back(tx);
            continue;
        }
        verified.push_back(tx);
    }
    if (verified.empty()) return out;

    // Phase 2: quantum signature verification.
    std::vector<bool> sig_valid(verified.size(), !stamps_enabled);
    if (stamps_enabled) {
        std::vector<Bytes> msgs(verified.size());
        std::vector<const quantum::QuantumSignature*> sigs(verified.size());
        for (std::size_t i = 0; i < verified.size(); ++i) {
            const ByteView b = verified[i]->bytes();
            msgs[i].assign(b.begin(), b.end());
            sigs[i] = verified[i]->signature();
        }

        if (signer.parallel_verify_with_threshold(msgs, sigs, gpu_threshold)) {
            sig_valid.assign(verified.size(), true);
        } else {
            // The batch failed — verify individually to find which ones are bad.
            for (std::size_t i = 0; i < verified.size(); ++i)
                sig_valid[i] = signer.verify(view(msgs[i]), sigs[i]).has_value();
        }
    }

    // Phase 3: separate what verified from what did not.
    for (std::size_t i = 0; i < verified.size(); ++i) {
        if (!sig_valid[i]) {
            out.rejected.push_back(verified[i]);
            continue;
        }
        out.valid.push_back(verified[i]);
    }
    return out;
}

}  // namespace lux::quantumvm
