// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.hpp — the Q-chain as the node's VM seam sees it.
//
// Rendered from Go chains/quantumvm/vm.go, block.go and feegate.go against
// lux/node/vm.hpp — the interface every chain this node runs implements. C, P,
// X and Q differ in what they put in a block and how they execute it; they do
// not differ in the questions that header asks, so this file answers them and
// nothing more.
//
// THE INVARIANTS THIS FILE EXISTS TO HOLD
//
//   A commit EXTENDS the tip. The block names the last-accepted block as its
//   parent and sits one height above it. Without that, any block that merely
//   verified could be committed — a verified sibling of an old block rewound
//   the chain to its height and left every height above indexed to an abandoned
//   branch, which the height index then served to bootstrapping peers as
//   canonical.
//
//   A commit is ONE write. Block, height index and tip pointer move together or
//   not at all, so a node never restarts holding a tip pointer to a block it
//   did not store. And the staging layer holds nothing across the call: writes
//   not discarded here are not discarded at all, they are flushed by the next
//   commit that succeeds.
//
//   A FAILED READ IS NOT AN EMPTY CHAIN. tip() answers "no block" for exactly
//   one reason — the store holds no tip pointer — and an error for every other
//   reason it could not read one. Collapsing those destroyed a chain: seeding
//   reads "no block" as a fresh chain, so one transient error at boot committed
//   genesis over a live tip and Initialize returned success.
//
//   GENESIS IS A CONSTANT OF ITS CHAIN. Fixed timestamp, height 0, empty
//   parent, no transactions, this chain and this network — so every node
//   computes one id alone, without talking to any other. Wall-clock time here
//   would give each node a different id for the same block and make the repair
//   a fork.

#pragma once

#include "lux/quantumvm/block.hpp"
#include "lux/quantumvm/clock.hpp"
#include "lux/quantumvm/config.hpp"
#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/quasar.hpp"
#include "lux/quantumvm/signer.hpp"
#include "lux/quantumvm/store.hpp"
#include "lux/quantumvm/transaction.hpp"

#include "lux/node/vm.hpp"

#include <atomic>
#include <memory>
#include <shared_mutex>
#include <string>
#include <variant>
#include <vector>

namespace lux::quantumvm {

inline constexpr const char* kVersion = "1.0.0";

// The chain's RPC alias — the path segment a JSON-RPC caller reaches it under.
inline constexpr const char* kAlias = "Q";

// Store keys. Every key is a distinct LENGTH, so no two can collide: a block id
// is 32 bytes, a height index entry is 9, "lastAccepted" is 12 and "height" is 6.
extern const Bytes kLastAcceptedKey;
extern const Bytes kTipHeightKey;

// Indexes the accepted block at a height, so a peer asking for height n gets an
// answer without walking the chain.
Bytes height_key(std::uint64_t h);

// Q-Chain has NO user-payable blockspace: finality-cert inclusion is a
// validator obligation paid through P-Chain reward distribution, never a user
// fee (LP-0130 §6). A fee market on Q would make finality hostage to blockspace
// pricing — the exact failure mode LP-0130 §6 eliminates — so the policy is the
// explicit committee-only sentinel, and it refuses every amount.
struct FeePolicy {
    virtual ~FeePolicy() = default;
    virtual Status validate_fee(std::uint64_t amount) const = 0;
};

struct NoUserTxPolicy final : FeePolicy {
    Status validate_fee(std::uint64_t amount) const override {
        (void)amount;
        return fail(Err::ChainAcceptsNoUserTxs);
    }
};

// What the node hands the VM at boot.
struct Init {
    store::Store* db = nullptr;
    // The node's own identity, which is what a validator signature is
    // attributed to, and the chain it is serving, which is what its blocks
    // belong to. A VM with neither runs, signs as nobody and produces blocks
    // every other Q-Chain will also accept — so it does not run.
    std::string node_id;
    Id chain_id{};
    std::uint32_t network_id = 0;
    Bytes genesis;
};

// A stamp is a validator's attestation to a message. Go's finality bridge takes
// it as an untyped interface; the three shapes it can actually be are named
// here, so no arm can be reached with a value it cannot check.
using Stamp = std::variant<std::monostate, quasar::QuasarSig, quasar::AggregatedSignature,
                           quantum::QuantumSignature>;

struct Health {
    bool healthy = false;
    std::string version;
    bool quantum_enabled = false;
    bool corona_enabled = false;
    std::size_t pending_txs = 0;
};

class QuantumVM;

// A block as consensus holds it: the chain's block, plus what its execution
// produced.
class VmBlock final : public lux::node::Block {
  public:
    VmBlock(QuantumVM* vm, BlockPtr blk) : vm_(vm), blk_(std::move(blk)) {}

    lux::node::Id id() const override { return blk_->id(); }
    lux::node::Id parent() const override { return blk_->parent_id(); }
    std::uint64_t height() const override { return blk_->height(); }
    std::span<const std::uint8_t> bytes() const override { return blk_->bytes(); }
    lux::node::Id root() const override { return blk_->execution_root(); }

    bool verify() override;
    void accept() override;

    // The other half of being decided. This block will never be accepted, and
    // on the Q-chain there is nothing to hand back: build_block COPIES from the
    // pool's queue rather than draining it, and only accept() removes anything,
    // so every transaction the block carried is still pending. Go says the same
    // in quantumvm's Block.Reject, which records the decision and returns.
    //
    // What it must NOT do is run after accept(), or twice: a block is decided
    // once, and more than one path can reach a decision.
    void reject() override;

    // Why the last verify() said no. Empty when it said yes.
    const std::string& refusal() const { return refusal_; }
    const BlockPtr& inner() const { return blk_; }

    // Which way this block was decided, for a caller that has to ask. Nothing
    // in the seam reads it; the two decisions read it to stay inert.
    bool accepted() const { return accepted_; }
    bool rejected() const { return rejected_; }

  private:
    QuantumVM* vm_ = nullptr;
    BlockPtr blk_;
    std::string refusal_;
    bool accepted_ = false;
    bool rejected_ = false;
};

class QuantumVM final : public lux::node::VM {
  public:
    explicit QuantumVM(config::Config cfg);
    ~QuantumVM() override;

    Status initialize(const Init& init);

    // ── the seam
    lux::node::Id chain_id() const override { return chain_id_; }
    std::string alias() const override { return kAlias; }
    std::shared_ptr<lux::node::Block> build() override;
    std::shared_ptr<lux::node::Block> parse(std::span<const std::uint8_t>) override;
    std::shared_ptr<lux::node::Block> get(const lux::node::Id&) const override;
    // Q-Chain finalizes on a verified threshold signature rather than
    // preference, so this records the preference and decides nothing.
    void prefer(const lux::node::Id& id) override { preferred_ = id; }
    lux::node::Id last_accepted() const override;
    std::uint64_t last_accepted_height() const override;

    // ── the chain, at its own types
    Result<BlockPtr> build_block();
    Result<BlockPtr> parse_block(ByteView data) const;
    Result<BlockPtr> block(const Id& block_id) const;
    // block_at takes NO lock, so a caller already holding one can use it —
    // which is the whole point: block() takes the read lock, and a caller that
    // took it too would deadlock against any writer that arrived in between.
    Result<BlockPtr> block_at(const Id& block_id) const;

    Result<Id> tip() const;
    Result<std::uint64_t> tip_height() const;
    Result<Id> block_id_at_height(std::uint64_t height) const;

    // Makes a block the tip: admits it, applies it and persists it as one step.
    Status commit_block(const Block& b);
    // The linear-chain invariant: exactly one block may follow the one this
    // node last accepted.
    Status extends_tip(const Block& b) const;
    Status seed_genesis();

    Status shutdown();
    bool shutting_down() const { return shutting_down_.load(std::memory_order_relaxed); }
    Health health() const;

    // Attests to a message for the finality bridge: a Quasar validator
    // signature when the bridge is up, an ML-DSA signature otherwise.
    Result<Stamp> stamp_block(const Id& block_id, std::uint64_t p_chain_height, ByteView message);
    // Checks a stamp against the message it claims to attest. The message is
    // the argument that makes this a verification: without it each arm could
    // only look at the stamp's own SHAPE, and shape is what the sender chose.
    Status verify_stamp(ByteView message, const Stamp& stamp) const;

    // The user-tx admission point, which admits nothing.
    Status issue_tx(const Transaction& tx) const;
    const FeePolicy* fee_policy() const { return fee_policy_.get(); }

    // Blocks until there is a transaction to build a block from, or the caller
    // gives up. Waiting on nothing would mean build is never called and the
    // chain never leaves genesis, however many transactions the pool accepted.
    bool wait_for_event(Duration timeout) { return pool_->wait_for_work(timeout); }

    TransactionPool& pool() { return *pool_; }
    const TransactionPool& pool() const { return *pool_; }
    store::Version& state() { return *state_; }
    const store::Version& state() const { return *state_; }
    Clock& clock() { return clock_; }
    const config::Config& configuration() const { return config_; }
    const quantum::QuantumSigner& signer() const { return *signer_; }
    const std::shared_ptr<quasar::Quasar>& bridge() const { return bridge_; }
    void drop_bridge() { bridge_.reset(); }
    std::uint32_t network_id() const { return network_id_; }
    std::shared_mutex& lock() const { return lock_; }

    // The batch verification the builder runs over the pool's selection, split
    // by the configured batch size.
    Verdict process_transactions(const std::vector<TxPtr>& txs) const;

    // Signs a block for the CONSENSUS layer, which is where a block-level
    // signature belongs: a cert is a quorum's statement about a block,
    // verifiable by anyone holding the validator set. It is deliberately NOT a
    // field on the block — the block id is sha256 of its own bytes, so a stamp
    // on the wire would make the id depend on WHO signed, and two honest nodes
    // would compute two ids for one block.
    void sign_block_with_quasar(const Block& b);

  private:
    Result<BlockPtr> build_block_locked();

    config::Config config_;
    store::Store* db_ = nullptr;
    std::unique_ptr<store::Version> state_;
    Id chain_id_{};
    Id preferred_{};
    std::uint32_t network_id_ = 0;
    std::string node_id_;

    std::unique_ptr<quantum::QuantumSigner> signer_;
    std::shared_ptr<quasar::Quasar> bridge_;
    std::unique_ptr<FeePolicy> fee_policy_;

    std::unique_ptr<TransactionPool> pool_;
    Clock clock_;

    std::atomic<bool> shutting_down_{false};
    mutable std::shared_mutex lock_;
};

}  // namespace lux::quantumvm
