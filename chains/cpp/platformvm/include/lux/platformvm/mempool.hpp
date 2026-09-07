// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.hpp — the P-chain's door onto the node's one pool.
//
// The pool itself is in lux/core/mempool.hpp and knows nothing about a chain:
// it refuses a duplicate, one too large, one there is no room for, and one that
// rivals something already waiting. Nothing here re-implements any of that.
//
// What IS here is the P-chain's own part, and it is two things:
//
//   admit()  the chain's rule about who may submit at all, then the pool's
//            four. There is ONE of these and every door into the pool goes
//            through it — a submitter's, and the one that puts a rejected
//            block's transactions back. A rule enforced at one of two doors is
//            a rule an attacker uses the other door for.
//
//   drop_expired_stakers()  a bond that would have started before now can no
//            longer be admitted, so holding it is holding something that can
//            only ever be refused. That is a fact about staking, which is why
//            it is not in the pool.
//
// Nothing here is consensus: two nodes holding different transactions is the
// normal state of a network. It is a DEFENCE. Anyone can submit, so the pool is
// the surface an attacker reaches first, and every rule is about what a node
// will spend on a stranger before anything has been paid for.

#pragma once

#include "lux/core/mempool.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstddef>
#include <vector>

namespace lux::platformvm::mempool {

using core::mempool::kDroppedRemembered;
using core::mempool::kMaxPoolSize;
using core::mempool::kMaxTxSize;
using core::mempool::Refusal;

// What the pool needs to know about a P-chain transaction, and all it may.
// These are found by the pool through the transaction's own namespace.
using Pool = core::mempool::Pool<txs::Tx>;
using Dropped = core::mempool::Dropped<Error>;

// refused turns the pool's four refusals into this chain's error vocabulary, so
// a submitter is told in the words the rest of the chain speaks.
Error refused(Refusal r, const Id& tx_id);

// admit is the P-chain's ONE admission. Every path that puts a transaction into
// the pool calls it.
//
// A full pool is NOT remembered as a reason: it says nothing about the
// transaction, only about the moment it arrived, and holding it against the
// transaction would turn a busy minute into a permanent refusal.
Status admit(Pool& pool, Dropped& dropped, const txs::Tx& tx);

// Go: DropExpiredStakerTxs. Returns what was dropped.
std::vector<Id> drop_expired_stakers(Pool& pool, std::uint64_t min_start_time);

// The oldest n transactions waiting, which is what a builder fills a block from.
std::vector<txs::Tx> oldest(const Pool& pool, std::size_t n);

}  // namespace lux::platformvm::mempool

namespace lux::platformvm::txs {

// The three questions the pool asks about a transaction. They are declared
// beside the transaction rather than beside the pool because the pool must not
// know what a P-chain transaction is.
inline Id pool_id(const Tx& tx) { return tx.tx_id; }
inline std::size_t pool_size(const Tx& tx) { return tx.bytes.size(); }
inline std::vector<Id> pool_inputs(const Tx& tx) {
    if (tx.unsigned_tx == nullptr) return {};
    return tx.unsigned_tx->input_ids();
}

}  // namespace lux::platformvm::txs
