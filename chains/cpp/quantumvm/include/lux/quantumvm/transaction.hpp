// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// transaction.hpp — what goes in a block, and where it waits until it does.
//
// Rendered from Go chains/quantumvm/transaction.go.
//
// A transaction is TWO byte strings and the difference between them is the
// whole security story: bytes() is the signature PREIMAGE — what the ML-DSA
// signature covers, and therefore what may never contain the signature — and
// the envelope (wire.hpp) is the preimage plus the signature over it, which is
// what rides in a block.
//
// The id is the content hash of the preimage, sha256(bytes()). It keys the
// pool, so two different transactions sharing one id would take a single slot
// between them; the prior derivation silently yielded the empty id for every
// transaction and collapsed the pool to one entry.

#pragma once

#include "lux/quantumvm/clock.hpp"
#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/signer.hpp"

#include <condition_variable>
#include <cstdint>
#include <deque>
#include <map>
#include <memory>
#include <mutex>
#include <optional>
#include <vector>

namespace lux::quantumvm {

class Transaction {
  public:
    virtual ~Transaction() = default;

    virtual Id id() const = 0;
    // The signature preimage. Stable across calls — it is what the id is
    // derived from and what a signature covers.
    virtual ByteView bytes() const = 0;
    virtual Status verify() const = 0;
    virtual Status execute() const = 0;
    virtual const quantum::QuantumSignature* signature() const = 0;
    virtual Seconds timestamp() const = 0;
    // The user-paid burn, in nLUX. Q-Chain's policy is the committee-only
    // sentinel, which refuses every amount (LP-0130 §6).
    virtual std::uint64_t fee() const = 0;
};

using TxPtr = std::shared_ptr<Transaction>;

class BaseTransaction : public Transaction {
  public:
    BaseTransaction() = default;
    BaseTransaction(Seconds ts, std::uint64_t nonce, Bytes data)
        : timestamp_(ts), nonce_(nonce), data_(std::move(data)) {}

    Id id() const override;
    ByteView bytes() const override;
    Status verify() const override;
    Status execute() const override { return ok(); }
    const quantum::QuantumSignature* signature() const override {
        return sig_ ? &*sig_ : nullptr;
    }
    Seconds timestamp() const override { return timestamp_; }
    std::uint64_t fee() const override { return fee_; }

    void set_signature(quantum::QuantumSignature sig) {
        sig_ = std::move(sig);
        // The signature is not part of the preimage, so the cached wire and id
        // stay valid. Saying so here is what keeps them from being invalidated
        // out of caution and silently changing the id.
    }
    quantum::QuantumSignature* mutable_signature() { return sig_ ? &*sig_ : nullptr; }
    void set_fee(std::uint64_t f) { fee_ = f; }

    std::uint64_t nonce() const { return nonce_; }
    ByteView data() const { return view(data_); }

  private:
    Seconds timestamp_ = 0;
    std::uint64_t nonce_ = 0;
    Bytes data_;
    std::uint64_t fee_ = 0;
    std::optional<quantum::QuantumSignature> sig_;

    mutable Bytes wire_;
    mutable bool wire_built_ = false;
    mutable std::optional<Id> id_;
};

// The pool is memory a peer can fill, so it has a ceiling, and a transaction
// the pool accepted is worth nothing until consensus is TOLD about it —
// consensus builds only when the wait returns.
class TransactionPool {
  public:
    explicit TransactionPool(std::size_t max_size) : max_size_(max_size) {}

    Status add(const TxPtr& tx);
    Status remove(const Id& tx_id);

    // Up to `limit` transactions in arrival order; a limit of zero or more than
    // the queue holds means all of them.
    std::vector<TxPtr> pending(std::size_t limit) const;
    std::size_t count() const;

    // Blocks until there is work, or the deadline passes. Returns false on the
    // deadline, which is a caller that gave up, never a claim that there is
    // work.
    bool wait_for_work(Duration timeout);

    // Re-arms the builder while the pool still holds anything.
    //
    // The latch carries ONE signal, so N arrivals wake one build and a build
    // that takes fewer than N leaves the rest with nothing to wake them: they
    // sit in the pool until some unrelated transaction arrives, which on a
    // quiet chain is never. Whoever drains the pool says so afterwards.
    void signal_if_work();

    void close();

  private:
    void signal_locked();

    mutable std::mutex mu_;
    std::condition_variable work_;
    bool signalled_ = false;

    std::map<Id, TxPtr> pending_;
    std::deque<TxPtr> queue_;
    std::size_t max_size_ = 0;
    bool closed_ = false;
};

class QuantumVM;

// The worker VERIFIES a batch, reporting what survived and what did not. Both
// halves matter: the survivors go in the block, and the rest have to leave the
// pool — a transaction whose quantum stamp has aged out will never verify
// again, and left in place it holds its slot for good.
//
// It does not execute anything. Effects belong to accept, on every node, once:
// running them here ran them on a block the network may never accept, ran them
// twice when the builder rebuilt, and ran them nowhere at all on a node that
// received the block rather than building it.
struct Verdict {
    std::vector<TxPtr> valid;
    std::vector<TxPtr> rejected;
};

Verdict process_batch(const std::vector<TxPtr>& txs, const quantum::QuantumSigner& signer,
                      bool stamps_enabled, int gpu_threshold);

}  // namespace lux::quantumvm
