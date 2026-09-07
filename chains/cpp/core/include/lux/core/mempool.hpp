// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.hpp — what a node holds that is not yet in a block, and the four
// refusals that are the POOL's own.
//
// The pool knows nothing about a chain. It cannot tell a valid transaction from
// an invalid one and does not pretend to: it holds bytes with a name, in the
// order they arrived, and refuses only what it can see for itself —
//
//   Duplicate   it is already here
//   TooLarge    it is bigger than anything worth relaying
//   Full        there is no room
//   Conflict    something here already spends one of its inputs
//
// Whether a transaction is VALID is the chain's question, asked before the pool
// is reached. Keeping the two apart is what lets both chains share this: the
// P-chain refuses its own reward transaction and the X-chain runs its execution
// against the last accepted state, and neither of those is a fact about a pool.
//
// ORDER IS INSERTION ORDER and it is load-bearing: the builder takes the oldest
// first, so two nodes that received the same transactions in the same order
// build the same block. A pool that reordered by fee would be a policy, and
// policy about whose transaction goes first is not a thing to decide quietly.
//
// REACHING THE TRANSACTION. The pool holds whatever the chain calls one and
// reaches it through three functions found beside that type:
//
//   Id           pool_id(const Tx&)      what it is called
//   std::size_t  pool_size(const Tx&)    what it weighs
//   R<Id>        pool_inputs(const Tx&)  what it would spend (any range of Id)
//
// That is the whole seam. A chain that can answer those three questions can use
// this pool, and the pool never learns what else a transaction is.

#pragma once

#include "lux/core/id.hpp"

#include <cstddef>
#include <expected>
#include <functional>
#include <list>
#include <map>
#include <optional>
#include <set>
#include <vector>

namespace lux::core::mempool {

// The most a single transaction may weigh, and the most the whole pool may. The
// transaction bound is large enough for a chain's genesis blob, which is the
// biggest thing anyone legitimately submits.
inline constexpr std::size_t kMaxTxSize = 2 * 1024 * 1024;
inline constexpr std::size_t kMaxPoolSize = 64 * 1024 * 1024;

// How many refusals are remembered. A reason that falls out lets the
// transaction be offered again, which is the point: one refused because the
// chain moved on deserves another look, and one that is garbage costs a single
// verification to refuse again.
inline constexpr std::size_t kDroppedRemembered = 64;

// Why a pool refused. Four answers, and all four are about the pool.
enum class Refusal {
    Duplicate,
    TooLarge,
    Full,
    Conflict,
};

inline const char* reason(Refusal r) {
    switch (r) {
        case Refusal::Duplicate: return "duplicate tx";
        case Refusal::TooLarge: return "tx too large";
        case Refusal::Full: return "mempool is full";
        case Refusal::Conflict: return "tx conflicts with other tx";
    }
    return "refused";
}

template <class Tx>
class Pool {
public:
    // The capacity is an argument so a caller can hold a smaller pool, and so a
    // test can fill one without writing 64 MiB of transactions.
    explicit Pool(std::size_t capacity = kMaxPoolSize) : available_(capacity) {}

    // add is STRUCTURAL admission and nothing else.
    std::expected<void, Refusal> add(Tx tx) {
        const Id tx_id = pool_id(tx);
        if (index_.count(tx_id) != 0) return std::unexpected(Refusal::Duplicate);

        const std::size_t size = pool_size(tx);
        if (size > kMaxTxSize) return std::unexpected(Refusal::TooLarge);
        if (size > available_) return std::unexpected(Refusal::Full);

        // An input already spoken for makes this transaction unplaceable: a
        // block cannot hold both, so holding both would only have the builder
        // rediscover the conflict once a round.
        std::set<Id> inputs;
        for (const Id& in : pool_inputs(tx)) inputs.insert(in);
        for (const Id& in : inputs)
            if (consumer_.count(in) != 0) return std::unexpected(Refusal::Conflict);

        available_ -= size;
        order_.push_back(std::move(tx));
        index_[tx_id] = std::prev(order_.end());
        for (const Id& in : inputs) consumer_[in] = tx_id;
        consumed_[tx_id] = std::move(inputs);
        return {};
    }

    // get returns nullptr when the transaction is not here.
    const Tx* get(const Id& tx_id) const {
        const auto it = index_.find(tx_id);
        if (it == index_.end()) return nullptr;
        return &*it->second;
    }
    bool has(const Id& tx_id) const { return index_.count(tx_id) != 0; }

    // peek is the OLDEST transaction waiting, or nullptr.
    const Tx* peek() const { return order_.empty() ? nullptr : &order_.front(); }

    // each walks the pool oldest first until `f` returns false.
    void each(const std::function<bool(const Tx&)>& f) const {
        for (const auto& tx : order_)
            if (!f(tx)) return;
    }

    // erase takes one transaction out, by name.
    void erase(const Id& tx_id) {
        const auto it = index_.find(tx_id);
        if (it == index_.end()) return;
        available_ += pool_size(*it->second);
        order_.erase(it->second);
        index_.erase(it);
        const auto c = consumed_.find(tx_id);
        if (c != consumed_.end()) {
            for (const Id& in : c->second) consumer_.erase(in);
            consumed_.erase(c);
        }
    }

    // remove drops these transactions AND anything that conflicts with them.
    // Once a transaction is in a block, every pooled rival for its inputs is
    // dead: it can never be accepted, and leaving it in would have the builder
    // try it every round.
    void remove(const std::vector<Tx>& gone) {
        for (const auto& tx : gone) {
            const Id tx_id = pool_id(tx);
            if (index_.count(tx_id) != 0) {
                erase(tx_id);
                continue;
            }
            std::vector<Id> rivals;
            for (const Id& in : pool_inputs(tx)) {
                const auto c = consumer_.find(in);
                if (c != consumer_.end()) rivals.push_back(c->second);
            }
            for (const Id& id : rivals) erase(id);
        }
    }

    std::size_t size() const { return index_.size(); }
    bool empty() const { return index_.empty(); }
    std::size_t bytes_available() const { return available_; }

private:
    using Order = std::list<Tx>;

    Order order_;
    std::map<Id, typename Order::iterator> index_;
    // The two halves of one relation: what each pooled transaction would spend,
    // and which pooled transaction would spend each input. The reverse index is
    // what makes the conflict test a lookup rather than a scan.
    std::map<Id, std::set<Id>> consumed_;
    std::map<Id, Id> consumer_;
    std::size_t available_;
};

// Dropped remembers why a transaction is NOT in a pool, for a while.
//
// A refusal that cannot be reported reaches whoever submitted it as silence,
// and silence is indistinguishable from being lost. It is a courtesy, though,
// not a record the chain owes anyone — so it is bounded, and the oldest is
// forgotten first.
//
// `Why` is the chain's own reason, whatever shape that is. The pool's four
// refusals are not it: a chain that says only "conflicts with other tx" has
// told a submitter less than it knows.
template <class Why>
class Dropped {
public:
    explicit Dropped(std::size_t remembered = kDroppedRemembered) : remembered_(remembered) {}

    void mark(const Id& tx_id, Why why) {
        const auto it = by_id_.find(tx_id);
        if (it != by_id_.end()) {
            it->second.first = std::move(why);
            return;
        }
        order_.push_back(tx_id);
        by_id_.emplace(tx_id, std::make_pair(std::move(why), std::prev(order_.end())));
        if (order_.size() > remembered_) {
            by_id_.erase(order_.front());
            order_.pop_front();
        }
    }

    // forget is what a transaction's ARRIVAL does to its refusal: one that is
    // waiting is not one that was dropped, whatever was said about it before.
    void forget(const Id& tx_id) {
        const auto it = by_id_.find(tx_id);
        if (it == by_id_.end()) return;
        order_.erase(it->second.second);
        by_id_.erase(it);
    }

    std::optional<Why> why(const Id& tx_id) const {
        const auto it = by_id_.find(tx_id);
        if (it == by_id_.end()) return std::nullopt;
        return it->second.first;
    }

    std::size_t size() const { return by_id_.size(); }

private:
    std::size_t remembered_;
    std::list<Id> order_;
    std::map<Id, std::pair<Why, std::list<Id>::iterator>> by_id_;
};

}  // namespace lux::core::mempool
