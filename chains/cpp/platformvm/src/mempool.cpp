// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.cpp — the P-chain's door onto the node's one pool.
//
// Rendered from Go vms/txs/mempool/mempool.go and
// vms/platformvm/txs/mempool/mempool.go. The pool's own part of that — order,
// the conflict index, the byte budget, the bounded refusal cache — moved to
// lux/core/mempool.hpp, where the X-chain reaches the same one.

#include "lux/platformvm/mempool.hpp"

namespace lux::platformvm::mempool {

Error refused(Refusal r, const Id& tx_id) {
    switch (r) {
        case Refusal::Duplicate:
            return Error{Err::DuplicateTx, hex(tx_id)};
        case Refusal::TooLarge:
            return Error{Err::TxTooLarge,
                         hex(tx_id) + " is over " + std::to_string(kMaxTxSize) + " bytes"};
        case Refusal::Full:
            return Error{Err::MempoolFull, hex(tx_id) + " does not fit"};
        case Refusal::Conflict:
            return Error{Err::ConflictsWithOtherTx, hex(tx_id)};
    }
    return Error{Err::InvalidState, hex(tx_id)};
}

Status admit(Pool& pool, Dropped& dropped, const txs::Tx& tx) {
    auto refuse = [&](const Error& why) -> Status {
        // A full pool says nothing about the transaction, only about the moment
        // it arrived, so it is not held against it.
        //
        // Nor is a refusal recorded against a transaction the pool is HOLDING:
        // it is waiting, and that is the fact. This is what a duplicate is —
        // the caller is told, and nothing is written down about a transaction
        // that is already here.
        if (why.code != Err::MempoolFull && !pool.has(tx.tx_id)) dropped.mark(tx.tx_id, why);
        return std::unexpected(why);
    };

    if (tx.unsigned_tx == nullptr)
        return refuse(Error{Err::InvalidState, "a transaction with nothing in it"});

    // The chain's own transaction. Nobody submits it: the builder produces it
    // from state, so one arriving from outside is either a mistake or an
    // attempt to choose the chain's own business.
    if (tx.unsigned_tx->kind() == txs::Kind::RewardValidator)
        return refuse(Error{Err::CantIssueRewardValidatorTx, hex(tx.tx_id)});

    if (auto r = pool.add(tx); !r) return refuse(refused(r.error(), tx.tx_id));

    // A transaction that is HERE is not a transaction that was refused.
    dropped.forget(tx.tx_id);
    return ok();
}

std::vector<Id> drop_expired_stakers(Pool& pool, std::uint64_t min_start_time) {
    std::vector<Id> expired;
    pool.each([&](const txs::Tx& tx) {
        if (tx.unsigned_tx == nullptr) return true;
        auto staker = txs::staker_of(*tx.unsigned_tx);
        // A transaction that admits no staker has no start to be past, and one
        // whose staker cannot even be read is somebody else's refusal to make.
        if (!staker || !staker.value()) return true;
        if (staker.value()->start < min_start_time) expired.push_back(tx.tx_id);
        return true;
    });
    for (const auto& id : expired) pool.erase(id);
    return expired;
}

std::vector<txs::Tx> oldest(const Pool& pool, std::size_t n) {
    std::vector<txs::Tx> out;
    out.reserve(n < pool.size() ? n : pool.size());
    pool.each([&](const txs::Tx& tx) {
        if (out.size() >= n) return false;
        out.push_back(tx);
        return true;
    });
    return out;
}

}  // namespace lux::platformvm::mempool
