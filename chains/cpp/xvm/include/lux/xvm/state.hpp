// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state.hpp — what the X-Chain knows: the occupied UTXO set, the transactions
// that produced it, and the accepted blocks.
//
// Two layers, and the split is the whole design:
//
//   Chain  the mutable view a transaction executes against
//   Diff   a Chain that RECORDS its changes over a parent instead of applying
//          them, so a block can be verified without being accepted
//
// Verification runs against a Diff. Acceptance is `apply` — the moment the
// recorded changes reach the parent. A block that fails verification simply
// drops its Diff, so nothing it touched was ever visible.

#pragma once

#include "lux/xvm/block.hpp"
#include "lux/xvm/id.hpp"
#include "lux/xvm/txs.hpp"

#include <map>
#include <memory>
#include <optional>
#include <vector>

namespace lux::xvm::state {

template <class T>
using Result = wire::Result<T>;

inline constexpr const char* kErrNotFound = "not found";
inline constexpr const char* kErrMissingParentState = "missing parent state";

struct ReadOnlyChain {
    virtual ~ReadOnlyChain() = default;

    virtual Result<txs::UTXO> get_utxo(const Id& utxo_id) const = 0;
    // utxos enumerates the OCCUPIED set in ascending UTXOID order, starting
    // strictly after `start` (kEmptyId starts at the first). A non-positive
    // limit means "all remaining". Deterministic order is what lets an execution
    // root be a fold over the set rather than over an insertion history.
    virtual std::vector<txs::UTXO> utxos(const Id& start, int limit) const = 0;
    virtual Result<std::shared_ptr<txs::Tx>> get_tx(const Id& tx_id) const = 0;
    virtual Result<Id> get_block_id_at_height(std::uint64_t height) const = 0;
    virtual Result<std::shared_ptr<block::StandardBlock>> get_block(const Id& blk_id) const = 0;
    virtual Id get_last_accepted() const = 0;
    virtual std::uint64_t get_timestamp() const = 0;
};

struct Chain : ReadOnlyChain {
    virtual void add_utxo(const txs::UTXO& utxo) = 0;
    virtual void delete_utxo(const Id& utxo_id) = 0;
    virtual void add_tx(std::shared_ptr<txs::Tx> tx) = 0;
    virtual void add_block(std::shared_ptr<block::StandardBlock> blk) = 0;
    virtual void set_last_accepted(const Id& blk_id) = 0;
    virtual void set_timestamp(std::uint64_t t) = 0;
};

// State is the chain's own store. It is in-memory here: durability belongs to
// the host that embeds this VM, and inventing a second database inside the VM
// would be a second answer to a question the host already answers.
class State final : public Chain {
public:
    Result<txs::UTXO> get_utxo(const Id& utxo_id) const override;
    std::vector<txs::UTXO> utxos(const Id& start, int limit) const override;
    Result<std::shared_ptr<txs::Tx>> get_tx(const Id& tx_id) const override;
    Result<Id> get_block_id_at_height(std::uint64_t height) const override;
    Result<std::shared_ptr<block::StandardBlock>> get_block(const Id& blk_id) const override;
    Id get_last_accepted() const override { return last_accepted_; }
    std::uint64_t get_timestamp() const override { return timestamp_; }

    void add_utxo(const txs::UTXO& utxo) override;
    void delete_utxo(const Id& utxo_id) override;
    void add_tx(std::shared_ptr<txs::Tx> tx) override;
    void add_block(std::shared_ptr<block::StandardBlock> blk) override;
    void set_last_accepted(const Id& blk_id) override { last_accepted_ = blk_id; }
    void set_timestamp(std::uint64_t t) override { timestamp_ = t; }

    std::size_t utxo_count() const { return utxos_.size(); }

private:
    std::map<Id, txs::UTXO> utxos_;
    std::map<Id, std::shared_ptr<txs::Tx>> txs_;
    std::map<std::uint64_t, Id> block_ids_;
    std::map<Id, std::shared_ptr<block::StandardBlock>> blocks_;
    Id last_accepted_{};
    std::uint64_t timestamp_ = 0;
};

// Versions resolves a parent block id to the state as of that block — how a
// Diff finds what it is a diff OF.
struct Versions {
    virtual ~Versions() = default;
    virtual Chain* get_state(const Id& blk_id) = 0;
};

// Diff records changes over a parent Chain. A nullopt in modified_utxos_ marks a
// DELETED UTXO, which is why the map holds an optional rather than a pointer
// that could also mean "absent".
class Diff final : public Chain {
public:
    static Result<std::unique_ptr<Diff>> create(const Id& parent_id, Versions& versions);

    Result<txs::UTXO> get_utxo(const Id& utxo_id) const override;
    std::vector<txs::UTXO> utxos(const Id& start, int limit) const override;
    Result<std::shared_ptr<txs::Tx>> get_tx(const Id& tx_id) const override;
    Result<Id> get_block_id_at_height(std::uint64_t height) const override;
    Result<std::shared_ptr<block::StandardBlock>> get_block(const Id& blk_id) const override;
    Id get_last_accepted() const override { return last_accepted_; }
    std::uint64_t get_timestamp() const override { return timestamp_; }

    void add_utxo(const txs::UTXO& utxo) override;
    void delete_utxo(const Id& utxo_id) override;
    void add_tx(std::shared_ptr<txs::Tx> tx) override;
    void add_block(std::shared_ptr<block::StandardBlock> blk) override;
    void set_last_accepted(const Id& blk_id) override { last_accepted_ = blk_id; }
    void set_timestamp(std::uint64_t t) override { timestamp_ = t; }

    // apply writes everything recorded into `target`. This is acceptance.
    void apply(Chain& target) const;

    const Id& parent_id() const { return parent_id_; }

private:
    Diff(const Id& parent_id, Versions& versions, Chain& parent);

    Id parent_id_;
    Versions* versions_;
    Chain* parent_;

    std::map<Id, std::optional<txs::UTXO>> modified_utxos_;
    std::map<Id, std::shared_ptr<txs::Tx>> added_txs_;
    std::map<std::uint64_t, Id> added_block_ids_;
    std::map<Id, std::shared_ptr<block::StandardBlock>> added_blocks_;
    Id last_accepted_{};
    std::uint64_t timestamp_ = 0;
};

// ---- executing a transaction against a chain ----

// consume removes every UTXO the inputs name.
void consume(Chain& chain, const std::vector<txs::TransferableInput>& ins);
// produce writes the outputs as UTXOs of `tx_id`, indexed from 0.
void produce(Chain& chain, const Id& tx_id, const std::vector<txs::TransferableOutput>& outs);

}  // namespace lux::xvm::state
