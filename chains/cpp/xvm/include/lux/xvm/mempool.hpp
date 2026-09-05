// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool.hpp — what this node holds that is not yet in a block, and the ONE
// gate that decides what may enter.
//
// Ported from vms/txs/mempool (the pool) and vms/xvm/network/gossip.go (the
// gate). The split is Go's and it is the whole design:
//
//   Pool    STRUCTURAL admission — duplicate, size, space, conflict. It knows
//           nothing about the chain, so it can never say a tx is valid.
//   Gossip  CHAIN admission — duplicate, previously-dropped, and this node's
//           own execution of the tx against its last accepted state. Everything
//           that reaches the pool from a peer or an RPC caller goes through it.
//
// THE ADMISSION POLICY, STATED (Go: gossipMempool.Add):
//
//   1. already in the pool           → duplicate tx
//   2. previously dropped            → the ORIGINAL drop reason, not a new one
//   3. Verifier::verify_tx           → syntax, then semantics and execution
//                                      against the LAST ACCEPTED state; a
//                                      failure is remembered as the drop reason
//   4. Pool::add                     → size ≤ 2 MiB, space ≤ 64 MiB, and no
//                                      input already consumed by a pooled tx
//
// THE AUTH POLICY, STATED: there is none here, and that is not an omission.
// A transaction authorizes itself — every input names an fx credential that
// must sign the tx's own bytes — and that check happens inside step 3, in the
// fx spend gates. A tx from an unknown peer and a tx from the local RPC are
// admitted by exactly the same four steps: no peer is trusted, and no caller
// is privileged. What the p2p layer above DOES restrict is who may ASK
// (Go serves pull-gossip requests only to validators, and throttles them);
// that is the transport's rule about requests, not the pool's rule about
// transactions, so it lives with the transport.
//
// Step 3 verifies against the LAST ACCEPTED state, which is Go's choice
// (manager.VerifyTx builds its diff on m.lastAccepted). Verifying against the
// preferred tip instead would admit a different set — the two disagree exactly
// while a block is in flight — and a node that admits a different set proposes
// a different block.

#pragma once

#include "lux/core/mempool.hpp"
#include "lux/xvm/id.hpp"
#include "lux/xvm/txs.hpp"

#include <cstddef>
#include <functional>
#include <list>
#include <map>
#include <memory>
#include <optional>
#include <set>
#include <span>
#include <string>
#include <utility>
#include <vector>

namespace lux::xvm::mempool {

template <class T>
using Result = wire::Result<T>;

// ================= the pool =================
//
// The pool itself is the node's (lux/core/mempool.hpp) and knows nothing about
// a chain: it refuses a duplicate, one too large, one there is no room for, and
// one that rivals something already waiting. Nothing below re-implements any of
// that; what is here is the X-Chain's vocabulary for those four answers and the
// gossip layer above them.

using core::mempool::kDroppedRemembered;
using core::mempool::kMaxPoolSize;
using core::mempool::kMaxTxSize;
using core::mempool::Refusal;

using Pool = core::mempool::Pool<std::shared_ptr<txs::Tx>>;
using Dropped = core::mempool::Dropped<std::string>;

inline constexpr const char* kErrDuplicateTx = "duplicate tx";
inline constexpr const char* kErrTxTooLarge = "tx too large";
inline constexpr const char* kErrPoolFull = "mempool is full";
inline constexpr const char* kErrConflictsWithOtherTx = "tx conflicts with other tx";

// refused turns the pool's four refusals into this chain's words, which is what
// a caller and a peer are told.
std::string refused(Refusal r, const Id& tx_id);

// ================= the bloom filter =================

// Bloom is the membership summary a node publishes so a peer can ask only for
// what it is missing. Ported from p2p/gossip/bloom.go, whose marshalled shape is
// the wire contract: one byte of hash count, that many big-endian seeds, then
// the bits.
//
// The salt and the seeds are RANDOM per filter, which is what stops a peer from
// choosing transactions that collide. `fill` is how those bytes are obtained, so
// a test can state them instead of drawing them.
class Bloom {
public:
    using Fill = std::function<void(std::span<std::uint8_t>)>;

    static constexpr int kMinHashes = 1;
    static constexpr int kMaxHashes = 16;
    static constexpr int kMinEntries = 1;
    static constexpr int kHashRotation = 17;

    // fill defaults to the system's random source.
    Bloom(int min_target_elements, double target_false_positive,
          double reset_false_positive, Fill fill = {});

    void add(const Id& gossip_id);
    bool has(const Id& gossip_id) const;

    // reset_if_needed rebuilds the filter once it holds more than it can hold at
    // the reset probability, sizing it for `target_elements`. Returns whether it
    // was rebuilt — the caller then re-adds what it still holds.
    bool reset_if_needed(std::size_t target_elements);

    // marshal is the wire form; salt is the other half a peer needs.
    Bytes marshal() const;
    const Id& salt() const { return salt_; }
    std::size_t count() const { return count_; }
    std::size_t max_count() const { return max_count_; }
    int num_hashes() const { return int(seeds_.size()); }
    std::size_t num_entries() const { return entries_.size(); }

    // The sizing arithmetic, exposed because it is the filter's whole shape and
    // a test that cannot state it cannot pin it.
    static int optimal_hashes(std::size_t num_entries, std::size_t count);
    static std::size_t optimal_entries(std::size_t count, double false_positive);
    static std::size_t estimate_count(int num_hashes, std::size_t num_entries,
                                      double false_positive);

private:
    void reset(std::size_t target_elements);

    int min_target_elements_;
    double target_false_positive_;
    double reset_false_positive_;
    Fill fill_;

    std::vector<std::uint64_t> seeds_;
    Bytes entries_;
    Id salt_{};
    std::size_t count_ = 0;
    std::size_t max_count_ = 0;
};

// random_fill draws bytes from the system's random source. It is the default
// `Bloom::Fill`, named so a caller can see what it is replacing.
void random_fill(std::span<std::uint8_t> out);

// ================= the gate =================

// Verifier is the chain's own opinion of a transaction: Go's network.TxVerifier,
// implemented by the VM (manager.VerifyTx). It is an interface because the pool
// must not know what a chain is.
struct Verifier {
    virtual ~Verifier() = default;
    virtual Result<void> verify_tx(txs::Tx& tx) = 0;
};

inline constexpr int kBloomChurnMultiplier = 3;

struct BloomParams {
    int min_target_elements = 8 * 1024;
    double target_false_positive = 0.01;
    double reset_false_positive = 0.05;
};

// Gossip is the gossip set: the pool plus the admission gate plus the filter a
// peer samples against. Go calls it gossipMempool, and every path that puts a
// transaction into this node — push gossip, pull gossip, the RPC — goes through
// `add`.
class Gossip {
public:
    using TxPtr = std::shared_ptr<txs::Tx>;

    Gossip(Pool& pool, Verifier& verifier, BloomParams params = {},
           Bloom::Fill fill = {});

    // add is the admission policy stated at the top of this header.
    Result<void> add(TxPtr tx);

    // add_unverified skips step 3 ONLY. It exists because Go exposes it
    // (IssueTxFromRPCWithoutVerification) for a caller that has already
    // verified — never as a way past the gate for a peer.
    Result<void> add_unverified(TxPtr tx);

    bool has(const Id& tx_id) const { return pool_->get(tx_id) != nullptr; }
    void each(const std::function<bool(const TxPtr&)>& f) const { pool_->each(f); }

    // filter is what a peer needs to ask for what it lacks: the marshalled bloom
    // and its salt.
    std::pair<Bytes, Id> filter() const { return {bloom_.marshal(), bloom_.salt()}; }

    Pool& pool() { return *pool_; }
    const Pool& pool() const { return *pool_; }

    // Why a transaction was refused, or "" if nothing is remembered.
    std::string drop_reason(const Id& tx_id) const {
        return dropped_.why(tx_id).value_or(std::string{});
    }

    // mark_dropped records why a transaction is not here — the builder's own
    // execution refused it, or the gate did. A transaction the pool is HOLDING
    // is never marked, whatever anyone says about it: it is waiting, and that
    // is the fact.
    void mark_dropped(const Id& tx_id, const std::string& why) { remember(tx_id, why); }
    const Bloom& bloom() const { return bloom_; }

private:
    // A transaction that is HELD is not a transaction that was refused,
    // whatever anyone says about it: the pool has it, and that is the fact.
    void remember(const Id& tx_id, const std::string& why) {
        if (pool_->has(tx_id)) return;
        dropped_.mark(tx_id, why);
    }

    Pool* pool_;
    Verifier* verifier_;
    Bloom bloom_;
    // Why a transaction is NOT in the pool. It lives with the gate rather than
    // with the pool because the reasons are the CHAIN's — "the pool is full" is
    // one of four things a pool can say, and everything else a submitter is
    // told came from an execution the pool knows nothing about.
    Dropped dropped_;
};

// ---- what a transaction looks like on the gossip wire ----
//
// Go's txParser: marshalling is the tx's own bytes and unmarshalling is the
// parser. There is no gossip envelope, which is why a gossiped tx and a tx in a
// block are the same bytes and hash to the same id.
inline ByteView marshal(const txs::Tx& tx) { return view(tx.bytes()); }
inline Result<std::shared_ptr<txs::Tx>> unmarshal(ByteView b) { return txs::parse(b); }

}  // namespace lux::xvm::mempool
