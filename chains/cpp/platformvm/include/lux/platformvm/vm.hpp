// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.hpp — the P-chain as the node's VM seam sees it.
//
// Rendered from Go vms/platformvm/vm.go and block/executor (verifier.go,
// acceptor.go, manager.go), against lux/node/vm.hpp — the interface every chain
// this node runs implements. C, P and X differ in what they put in a block and
// how they execute it; they do not differ in the questions that header asks.
//
// The shape of the thing:
//
//   verify   builds the state this block WOULD produce, on a layer over its
//            parent's, and keeps it. Nothing is written to the accepted state.
//   accept   applies that layer, and only then is the block the chain's.
//
// A proposal block keeps THREE layers: the decision layer its own decision txs
// produced, and the commit and abort layers its proposal produced on top. The
// option block that follows picks one, which is why both are computed before
// anyone votes.
//
// EXECUTION IS NOT OPTIONAL: root() is the sha256 commitment to the state the
// block produced, computed by running the block, never copied from a proposer.
// A validator that signed a block it had not executed would be certifying a
// name rather than a result.

#pragma once

#include "lux/platformvm/block.hpp"
#include "lux/platformvm/executor.hpp"
#include "lux/platformvm/mempool.hpp"
#include "lux/platformvm/state.hpp"
#include "lux/platformvm/txs.hpp"
#include "lux/platformvm/validators.hpp"

#include "lux/node/vm.hpp"

#include <map>
#include <memory>
#include <string>
#include <vector>

namespace lux::platformvm::vm {

// What genesis says: the money, the validators and the clock a chain starts at.
struct Genesis {
    std::uint64_t timestamp = 0;
    std::uint64_t initial_supply = 0;
    // The chain's starting fee position: how much gas it may spend before it has
    // to wait for the clock.
    gas::State fee_state{};
    std::vector<UTXO> utxos;
    // Validators that exist from the first block, with the transaction that
    // created each so a reward can later name it.
    std::vector<std::pair<state::Staker, txs::Tx>> validators;
};

class PlatformVM;

// A block, plus the state its execution produced. The node holds one of these
// through consensus; the layer inside is what accept() applies.
class VmBlock final : public lux::node::Block {
  public:
    VmBlock(PlatformVM* vm, std::shared_ptr<block::Block> blk) : vm_(vm), blk_(std::move(blk)) {}

    lux::node::Id id() const override;
    lux::node::Id parent() const override;
    std::uint64_t height() const override { return blk_->height(); }
    std::span<const std::uint8_t> bytes() const override { return blk_->bytes(); }
    lux::node::Id root() const override;

    bool verify() override;
    void accept() override;

    const block::Block& inner() const { return *blk_; }
    // Why the last verify() said no. Empty when it said yes.
    const std::string& refusal() const { return refusal_; }

  private:
    PlatformVM* vm_;
    std::shared_ptr<block::Block> blk_;
    lux::node::Id root_{};  // the node's id type: the commitment as consensus reads it
    std::string refusal_;
};

// The chain.
class PlatformVM final : public lux::node::VM {
  public:
    PlatformVM(const Id& chain_id, executor::Backend backend, const Genesis& genesis);

    lux::node::Id chain_id() const override;
    std::string alias() const override { return "P"; }

    std::shared_ptr<lux::node::Block> build() override;
    std::shared_ptr<lux::node::Block> parse(std::span<const std::uint8_t> b) override;
    std::shared_ptr<lux::node::Block> get(const lux::node::Id& id) const override;
    void prefer(const lux::node::Id& id) override;
    lux::node::Id last_accepted() const override;
    std::uint64_t last_accepted_height() const override { return last_accepted_height_; }

    // A transaction waiting to go into a block. What is refused HERE is what a
    // node will not spend anything on: a duplicate, an oversized one, one that
    // would overfill the pool, one that rivals something already waiting, and
    // the chain's own reward transaction, which nobody submits. Whether it
    // EXECUTES is decided when a block carrying it is built.
    Status submit(const txs::Tx& tx) { return mempool_.add(tx); }
    std::size_t mempool_size() const { return mempool_.size(); }
    const mempool::Pool& mempool() const { return mempool_; }

    // Who validates a network, as of the last accepted block, and the
    // commitment a vote binds that set with. This is what the node samples to
    // decide whose vote counts and for how much — the P-chain's whole reason to
    // exist from the node's point of view.
    Result<std::map<NodeId, validators::Validator>> validator_set(const Id& network_id) const {
        return validators::current_set(state_, network_id);
    }
    // Who validated a network at a height that has already passed. A message
    // signed then is checked now, so this is what makes an old signature
    // checkable at all. It is the set today with everything since undone, which
    // is why a height the chain has not reached is a refusal rather than a
    // guess.
    Result<std::map<NodeId, validators::Validator>> validator_set_at(const Id& network_id,
                                                                     std::uint64_t height) const {
        auto set = validator_set(network_id);
        if (!set) return set;
        if (auto st = history_.rewind(set.value(), network_id, last_accepted_height_, height); !st)
            return std::unexpected(st.error());
        return set;
    }

    Result<Id> validator_set_root(const Id& network_id) const {
        auto set = validator_set(network_id);
        if (!set) return std::unexpected(set.error());
        return validators::set_root(set.value());
    }

    // What the last accepted block asked the shared memory to do. The node
    // applies these alongside the state it just wrote; a block that imports or
    // exports nothing leaves this empty.
    const std::map<Id, atomic::Requests>& pending_atomic_requests() const { return atomic_requests_; }

    // What every accepted height changed about the validator sets.
    const validators::History& history() const { return history_; }

    // The accepted state — what the chain remembers.
    const state::MemState& accepted() const { return state_; }
    state::MemState& accepted() { return state_; }

    executor::Backend& backend() { return backend_; }
    const executor::Backend& backend() const { return backend_; }

    // Go: block/executor/options.go. A proposal block has two children — commit
    // and abort — and this is which one this node prefers. The answer is the
    // validator's own uptime against the requirement in force when it BONDED,
    // never the one in force now: judging it by today's rule would let a stake
    // majority raise the bar the day before a rival's stake matures and take its
    // reward.
    //
    // Returns {preferred, alternate}. A node that cannot compute the uptime
    // prefers COMMIT, erring toward over-rewarding rather than under-rewarding —
    // the same fallback the reference takes, and for the same reason: the error
    // can be caused by the proposer.
    Result<std::pair<std::shared_ptr<lux::node::Block>, std::shared_ptr<lux::node::Block>>> options(
        const block::ProposalBlock& b);

    // The wall clock the sync bound is judged against. Explicit, because a
    // consensus rule that reads the machine's clock is a rule that cannot be
    // tested.
    void set_wall_clock(std::uint64_t t) { wall_clock_ = t; }
    std::uint64_t wall_clock() const { return wall_clock_; }

  private:
    friend class VmBlock;

    // The layers one verified block is holding, before anyone knows whether it
    // will be accepted.
    struct Verified {
        std::shared_ptr<block::Block> blk;
        // A standard or option block has one layer; a proposal block has the
        // decision layer plus the two outcomes its option block chooses between.
        std::shared_ptr<state::Diff> on_accept;
        std::shared_ptr<state::Diff> on_decision;
        std::shared_ptr<state::Diff> on_commit;
        std::shared_ptr<state::Diff> on_abort;
        Id root{};
        std::uint64_t timestamp = 0;
        // What this block asks the shared memory to do once it is accepted.
        std::map<Id, atomic::Requests> atomic_requests;
    };

    Status verify_block(const block::Block& b, Verified& out);
    Status accept_block(const block::Block& b);

    // The state a child of `parent_id` is verified against: the accepted state
    // if the parent is the last accepted block, otherwise the parent's layer.
    state::Chain* state_after(const Id& parent_id);
    Result<std::uint64_t> height_of(const Id& id) const;

    Id chain_id_{};
    executor::Backend backend_;
    state::MemState state_;
    std::map<Id, std::shared_ptr<block::Block>> blocks_;
    std::map<Id, Verified> verified_;
    mempool::Pool mempool_;
    Id last_accepted_{};
    std::uint64_t last_accepted_height_ = 0;
    validators::History history_;
    Id preferred_{};
    std::uint64_t wall_clock_ = 0;
    std::map<Id, atomic::Requests> atomic_requests_;
};

}  // namespace lux::platformvm::vm
