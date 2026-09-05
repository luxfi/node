// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.hpp — the pending transactions, the order a proposer takes them in,
// and the latch that tells consensus there is something to build.
//
// A transaction the pool accepted is worth nothing until consensus is told
// about it: consensus builds only when wait_for_event returns, so accepting work
// and reporting it are one step.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/wire.hpp"

#include <chrono>
#include <condition_variable>
#include <map>
#include <mutex>
#include <vector>

namespace lux::zkvm {

inline constexpr const char* kErrNullifierInPool = "nullifier already in mempool";
inline constexpr const char* kErrPoolFull =
    "mempool is full and the transaction pays less than what it would displace";

// Latch is "there is work". A signal that arrives before anyone waits is kept,
// so a transaction accepted between two waits is not lost — which would leave
// the chain sitting at genesis with a full pool.
class Latch {
public:
    void signal();
    bool wait_for(std::chrono::milliseconds timeout);

private:
    std::mutex mu_;
    std::condition_variable cv_;
    bool ready_ = false;
};

class Mempool {
public:
    Mempool(std::size_t max_size) : max_size_(max_size) {}

    // add derives the id from the CONTENT, not from the caller. A client that
    // supplies its own id would otherwise have a proposer compute a block id its
    // peers do not — the block it built would not be the block they see.
    //
    // A full pool gives up whatever pays least, so what it holds is always the
    // best it has been offered. An arrival that is itself the cheapest is
    // refused: taking it would mean dropping a better transaction for a worse
    // one.
    wire::Result<void> add(Transaction tx);

    void remove(const Id& tx_id);
    bool has(const Id& tx_id) const;
    std::size_t size() const;

    // pending returns up to limit transactions, best-paying first. Ties break on
    // the id, so two nodes with the same pool assemble the same block — Go's
    // heap leaves equal fees in an unspecified order, which is a proposer that
    // cannot reproduce its own choice.
    std::vector<Transaction> pending(std::size_t limit) const;

    // prune_expired drops transactions the chain has passed. Nothing else does:
    // a transaction that can never enter a block occupies a slot forever, and a
    // pool full of those refuses every honest arrival that pays the same floor.
    std::size_t prune_expired(std::uint64_t current_height);

    bool wait_for_event(std::chrono::milliseconds timeout) { return work_.wait_for(timeout); }

private:
    void remove_locked(const Id& tx_id);

    mutable std::mutex mu_;
    std::size_t max_size_;
    std::map<Id, Transaction> txs_;
    ByteMap<Id> nullifiers_;
    Latch work_;
};

}  // namespace lux::zkvm
