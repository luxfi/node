// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.hpp — the D-Chain behind the node's VM seam.
//
// The seam asks five questions and this chain answers them the way every chain
// does: build a block, parse one, fetch one, prefer one, and say what was last
// accepted durably. What is specific to this chain is only what a block CONTAINS
// — registry admissions — and what executing one PRODUCES: the admitted set, and
// the root folded over it.
//
// Two properties the seam insists on, and why they hold here:
//
//   The root is EXECUTED, never copied. A block's bytes carry a parent, a height
//   and transactions; they do not carry a root. A receiving node derives the
//   root by applying the transactions itself, so a proposer cannot assert a
//   result it did not produce.
//
//   Rejection HANDS BACK. A block decided against returns its transactions to
//   the pool the next build draws from, because they were never refused — they
//   lost a race — and a node that dropped them would disagree with every other
//   node about what is still pending. It is inert after accept, and accept is
//   inert after it: a block is decided once, either way.

#pragma once

#include "lux/dexvm/state.hpp"
#include "lux/node/vm.hpp"

#include <deque>
#include <map>
#include <memory>

namespace lux::dexvm {

class VM;

// Block is one D-Chain block over the node's seam. It holds the diff its own
// execution produced, so accepting writes exactly what verifying proved.
class Block final : public node::Block {
public:
    Block(VM& vm, BlockBody body, Bytes bytes);

    node::Id id() const override;
    node::Id parent() const override;
    std::uint64_t height() const override;
    std::span<const std::uint8_t> bytes() const override;
    node::Id root() const override;
    bool verify() override;
    void accept() override;
    void reject() override;

    const BlockBody& body() const { return body_; }
    // Why this node refused to vote, when it did. A refusal is not an error
    // return on the seam, so the reason has to be readable from somewhere.
    const std::string& refusal() const { return refusal_; }

private:
    void ensure_executed() const;

    VM& vm_;
    BlockBody body_;
    Bytes bytes_;
    Id id_;
    mutable bool executed_ = false;
    // What execution concluded, kept SEPARATELY from the write set. Acceptance
    // consumes the write set, and a block must still be able to say what state
    // it produced afterwards — consensus signed over that root, so a block that
    // forgot it once accepted could not answer for what it had certified.
    mutable bool verified_ = false;
    mutable Id root_{};
    mutable std::optional<Diff> diff_;
    mutable std::string refusal_;
    bool decided_ = false;
};

class VM final : public node::VM {
public:
    // make builds a chain over a store that already holds whatever it holds. The
    // verifier is the node's own — the RuntimeVerifier, bound to the ids this
    // node is actually running — and it is what every admission in every block
    // is proven against.
    static Result<std::unique_ptr<VM>> make(const Id& chain_id, store::Store& s,
                                            NetworkClass network_class, DexAssetPolicy policy,
                                            std::shared_ptr<ChainVerifier> verifier,
                                            std::function<std::string(const Id&)> chain_label_for);

    node::Id chain_id() const override { return chain_id_; }
    std::string alias() const override { return "D"; }

    std::shared_ptr<node::Block> build() override;
    std::shared_ptr<node::Block> parse(std::span<const std::uint8_t> b) override;
    std::shared_ptr<node::Block> get(const node::Id& id) const override;
    void prefer(const node::Id& id) override;

    node::Id last_accepted() const override { return state_->last_accepted(); }
    std::uint64_t last_accepted_height() const override { return state_->last_accepted_height(); }

    // The highest height this node itself decided. These chains advance only
    // through accept(), and none of them can be handed history — there is no
    // import path that moves the tip without a certificate under it — so the
    // decided frontier IS the accepted tip. A chain that grows one must stop
    // answering with this and start answering with what it certified.
    std::uint64_t frontier() const override { return last_accepted_height(); }

    // issue adds a transaction to the pool the next build draws from. It does
    // NOT admit anything: admission happens when a block carrying it executes.
    void issue(Tx tx);
    std::size_t pending() const { return pool_.size(); }

    State& state() { return *state_; }
    const State& state() const { return *state_; }
    ChainVerifier& verifier() { return *verifier_; }

private:
    friend class Block;

    VM(const Id& chain_id, std::shared_ptr<ChainVerifier> v)
        : chain_id_(chain_id), verifier_(std::move(v)) {}

    // What a decided block does to the pool: accepted transactions leave it,
    // rejected ones go back to the front, ahead of anything issued since, so a
    // block that lost a race is rebuilt before newer work.
    void drop_from_pool(const std::vector<Tx>& txs);
    void return_to_pool(const std::vector<Tx>& txs);

    Id chain_id_{};
    std::shared_ptr<ChainVerifier> verifier_;
    std::unique_ptr<State> state_;
    std::deque<Tx> pool_;
    std::map<Id, std::shared_ptr<Block>> blocks_;
    Id preference_{};
};

}  // namespace lux::dexvm
