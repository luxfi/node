// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.hpp — what is waiting to go into a block.
//
// Rendered from Go vms/txs/mempool/mempool.go and
// vms/platformvm/txs/mempool/mempool.go.
//
// Nothing here is consensus: two nodes holding different transactions is the
// normal state of a network. It is a DEFENCE. Anyone can submit, so the pool is
// the surface an attacker reaches first, and every rule below is about what a
// node will spend on a stranger before anything has been paid for:
//
//  - a transaction that is already waiting is not taken twice;
//  - one too large to be worth relaying is refused outright;
//  - the pool holds a fixed number of bytes and no more;
//  - two transactions spending the same output cannot both wait, because at
//    most one of them can ever be accepted and holding both is holding one for
//    nothing;
//  - the chain's own reward transaction is refused, because nobody submits it —
//    the builder produces it from state, so one arriving from outside is either
//    a mistake or an attempt to choose the chain's own business.
//
// Order is insertion order. A pool that reordered by fee would be a policy, and
// policy about whose transaction goes first is exactly the thing a node should
// not be deciding quietly.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstddef>
#include <list>
#include <map>
#include <optional>
#include <set>
#include <vector>

namespace lux::platformvm::mempool {

// Go: MaxTxSize. Large enough for a chain's genesis blob, which is the biggest
// thing anyone legitimately submits.
inline constexpr std::size_t kMaxTxSize = 2 * 1024 * 1024;
// Go: maxMempoolSize.
inline constexpr std::size_t kMaxSize = 64 * 1024 * 1024;
// Go: droppedTxIDsCacheSize. Why a transaction was refused is worth remembering
// for a while, and only for a while: it is a courtesy to whoever submitted it,
// not a record the chain owes anyone.
inline constexpr std::size_t kDroppedRemembered = 64;

class Pool {
  public:
    // The capacity is an argument so a caller can hold a smaller pool, and so a
    // test can fill one without writing 64 MiB of transactions.
    explicit Pool(std::size_t capacity = kMaxSize) : available_(capacity) {}

    Status add(const txs::Tx& tx);

    std::optional<txs::Tx> get(const Id& tx_id) const;
    bool has(const Id& tx_id) const { return by_id_.count(tx_id) != 0; }
    std::size_t size() const { return by_id_.size(); }
    bool empty() const { return by_id_.empty(); }
    std::size_t bytes_available() const { return available_; }

    // The oldest transaction waiting, and the oldest n.
    std::optional<txs::Tx> peek() const;
    std::vector<txs::Tx> peek(std::size_t n) const;

    // Remove these, AND anything that conflicts with them. A transaction that
    // was accepted makes every rival for its inputs unacceptable forever, so
    // leaving those behind would be holding transactions that can never go
    // anywhere.
    void remove(const std::vector<txs::Tx>& gone);

    // Why a transaction is not here. A refusal that cannot be reported reaches
    // whoever submitted it as silence, and silence is indistinguishable from
    // being lost.
    //
    // A full pool is NOT remembered as a reason: it says nothing about the
    // transaction, only about the moment it arrived, and holding it against the
    // transaction would turn a busy minute into a permanent refusal.
    void mark_dropped(const Id& tx_id, const Error& reason);
    std::optional<Error> drop_reason(const Id& tx_id) const;

    // Go: DropExpiredStakerTxs. A bond that would have started before this
    // moment can no longer be admitted, so holding it is holding something that
    // can only ever be refused. Returns what was dropped.
    std::vector<Id> drop_expired_stakers(std::uint64_t min_start_time);

  private:
    struct Held {
        txs::Tx tx;
        std::list<Id>::iterator at;
        std::vector<Id> inputs;
    };

    std::list<Id> order_;
    std::map<Id, Held> by_id_;
    // Which output each waiting transaction would spend, so a conflict is a
    // lookup rather than a walk.
    std::map<Id, Id> consumed_;  // utxo id -> the transaction waiting to spend it
    std::size_t available_ = kMaxSize;

    std::list<Id> dropped_order_;
    std::map<Id, Error> dropped_;

    void erase(const Id& tx_id);
};

}  // namespace lux::platformvm::mempool
