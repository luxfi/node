// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.cpp — verify, accept, and build.
//
// Rendered from Go vms/platformvm/block/executor (verifier.go, acceptor.go) and
// block/builder/builder.go.

#include "lux/platformvm/vm.hpp"

#include <algorithm>
#include <cstring>
#include <set>
#include <tuple>

namespace lux::platformvm::vm {
namespace {

// The node's id and this chain's id are the same 32 bytes under two names —
// one is std::array, one is a distinct type so an address and a chain id cannot
// be swapped. These two are the whole conversion.
lux::node::Id to_node_id(const Id& id) {
    lux::node::Id out{};
    static_assert(std::tuple_size<lux::node::Id>::value == kIdLen, "the node's id is 32 bytes");
    std::memcpy(out.data(), id.b.data(), kIdLen);
    return out;
}

Id from_node_id(const lux::node::Id& id) {
    Id out{};
    std::memcpy(out.b.data(), id.data(), kIdLen);
    return out;
}

}  // namespace

// ── the block as consensus sees it

lux::node::Id VmBlock::id() const { return to_node_id(blk_->id()); }
lux::node::Id VmBlock::parent() const { return to_node_id(blk_->parent()); }
lux::node::Id VmBlock::root() const { return root_; }

bool VmBlock::verify() {
    PlatformVM::Verified v;
    const auto st = vm_->verify_block(*blk_, v);
    if (!st) {
        // False is a refusal to vote, never an error: an honest node does not
        // vote for a block its own execution rejects. The reason is kept so a
        // human can be told why.
        refusal_ = st.error().message();
        return false;
    }
    refusal_.clear();
    root_ = to_node_id(v.root);
    vm_->verified_[blk_->id()] = std::move(v);
    return true;
}

void VmBlock::accept() { (void)vm_->accept_block(*blk_); }

// ── the chain

PlatformVM::PlatformVM(const Id& chain_id, executor::Backend backend, const Genesis& genesis)
    : chain_id_(chain_id), backend_(std::move(backend)) {
    state_.set_timestamp(genesis.timestamp);
    state_.set_current_supply(kPrimaryNetworkId, genesis.initial_supply);
    for (const auto& u : genesis.utxos) state_.add_utxo(u);
    for (const auto& [staker, tx] : genesis.validators) {
        (void)state_.put_current_validator(staker);
        state_.add_tx(tx, status::Status::Committed);
    }
    backend_.now = genesis.timestamp;
    wall_clock_ = genesis.timestamp;

    // The chain starts at a block, so that every later block has a parent and a
    // height to be one more than.
    auto g = block::StandardBlock::create(genesis.timestamp, kEmptyId, 0, {});
    // A standard block with no txs at height 0 is the one block that is allowed
    // to change nothing: it is where the chain begins rather than a step in it.
    blocks_[g.value()->id()] = g.value();
    last_accepted_ = g.value()->id();
    last_accepted_height_ = 0;
    preferred_ = last_accepted_;
}

lux::node::Id PlatformVM::chain_id() const { return to_node_id(chain_id_); }
lux::node::Id PlatformVM::last_accepted() const { return to_node_id(last_accepted_); }

std::shared_ptr<lux::node::Block> PlatformVM::parse(std::span<const std::uint8_t> b) {
    auto blk = block::parse(b);
    if (!blk) return nullptr;
    blocks_[blk.value()->id()] = blk.value();
    return std::make_shared<VmBlock>(this, blk.value());
}

std::shared_ptr<lux::node::Block> PlatformVM::get(const lux::node::Id& id) const {
    const auto it = blocks_.find(from_node_id(id));
    if (it == blocks_.end()) return nullptr;
    return std::make_shared<VmBlock>(const_cast<PlatformVM*>(this), it->second);
}

void PlatformVM::prefer(const lux::node::Id& id) { preferred_ = from_node_id(id); }

state::Chain* PlatformVM::state_after(const Id& parent_id) {
    if (parent_id == last_accepted_) return &state_;
    const auto it = verified_.find(parent_id);
    if (it == verified_.end()) return nullptr;
    // A proposal block's own layers are the two outcomes; a child of it is an
    // option block, which reaches them by name rather than through this.
    if (it->second.on_accept) return it->second.on_accept.get();
    return nullptr;
}

Result<std::uint64_t> PlatformVM::height_of(const Id& id) const {
    const auto it = blocks_.find(id);
    if (it == blocks_.end()) return fail(Err::ParentNotFound, id.hex());
    return it->second->height();
}

Status PlatformVM::verify_block(const block::Block& b, Verified& out) {
    const Id parent_id = b.parent();
    const auto parent_height = height_of(parent_id);
    if (!parent_height) return std::unexpected(parent_height.error());
    if (b.height() != parent_height.value() + 1)
        return fail(Err::InvalidState, "block height is " + std::to_string(b.height()) + ", expected " +
                                           std::to_string(parent_height.value() + 1));

    out.blk = blocks_.count(b.id()) ? blocks_[b.id()] : nullptr;
    out.timestamp = b.timestamp();

    switch (b.kind()) {
        case block::Kind::Abort:
        case block::Kind::Commit: {
            // An option block is uniquely generated from its parent proposal, so
            // its timestamp is the parent's — anything else is a second spelling
            // of one outcome.
            const auto p = verified_.find(parent_id);
            if (p == verified_.end()) return fail(Err::ParentNotFound, "no verified proposal parent");
            if (!p->second.on_commit || !p->second.on_abort)
                return fail(Err::InvalidState, "parent is not a proposal block");
            if (b.timestamp() != p->second.timestamp)
                return fail(Err::InvalidState, "option block timestamp does not match its parent");
            out.on_accept = b.kind() == block::Kind::Commit ? p->second.on_commit : p->second.on_abort;
            out.root = state::state_root(*out.on_accept);
            return ok();
        }

        case block::Kind::Standard: {
            state::Chain* parent_state = state_after(parent_id);
            if (parent_state == nullptr) return fail(Err::ParentNotFound, parent_id.hex());
            if (auto st = executor::verify_new_chain_time(b.timestamp(), wall_clock_, *parent_state); !st)
                return st;

            auto layer = std::make_shared<state::Diff>(parent_state);
            executor::Backend backend = backend_;
            backend.now = b.timestamp();
            auto changed = executor::advance_time_to(backend, *layer, b.timestamp());
            if (!changed) return std::unexpected(changed.error());

            const auto block_txs = b.decision_txs();
            std::set<Id> inputs;
            for (const auto& tx : block_txs) {
                if (auto st = executor::standard_tx(backend, tx, *layer); !st) return st;
                // Two transactions in one block may not spend the same output:
                // both would verify against the layer as it stood before either
                // ran, so the overlap has to be refused explicitly.
                for (const auto& in : tx.input_ids())
                    if (!inputs.insert(in).second) return fail(Err::ConflictingBlockTxs);
                layer->add_tx(tx, status::Status::Committed);
            }

            // A block that changes nothing should never have been issued.
            if (!changed.value() && block_txs.empty()) return fail(Err::EmptyBlock);

            out.on_accept = layer;
            out.root = state::state_root(*layer);
            return ok();
        }

        case block::Kind::Proposal: {
            state::Chain* parent_state = state_after(parent_id);
            if (parent_state == nullptr) return fail(Err::ParentNotFound, parent_id.hex());
            if (auto st = executor::verify_new_chain_time(b.timestamp(), wall_clock_, *parent_state); !st)
                return st;

            auto decision = std::make_shared<state::Diff>(parent_state);
            executor::Backend backend = backend_;
            backend.now = b.timestamp();
            auto changed = executor::advance_time_to(backend, *decision, b.timestamp());
            if (!changed) return std::unexpected(changed.error());

            std::set<Id> inputs;
            for (const auto& tx : b.decision_txs()) {
                if (auto st = executor::standard_tx(backend, tx, *decision); !st) return st;
                for (const auto& in : tx.input_ids())
                    if (!inputs.insert(in).second) return fail(Err::ConflictingBlockTxs);
                decision->add_tx(tx, status::Status::Committed);
            }

            const auto& proposal = static_cast<const block::ProposalBlock&>(b);
            auto tx = proposal.tx();
            if (!tx) return std::unexpected(tx.error());

            auto on_commit = std::make_shared<state::Diff>(decision.get());
            auto on_abort = std::make_shared<state::Diff>(decision.get());
            if (auto st = executor::proposal_tx(backend, tx.value(), *on_commit, *on_abort); !st) return st;
            on_commit->add_tx(tx.value(), status::Status::Committed);
            on_abort->add_tx(tx.value(), status::Status::Aborted);

            out.on_decision = decision;
            out.on_commit = on_commit;
            out.on_abort = on_abort;
            // A proposal block's own root is what its decision layer produced:
            // the outcome is not decided yet, and the two that follow carry
            // their own.
            out.root = state::state_root(*decision);
            return ok();
        }
    }
    return fail(Err::UnknownBlockKind);
}

Status PlatformVM::accept_block(const block::Block& b) {
    const auto it = verified_.find(b.id());
    if (it == verified_.end()) return fail(Err::InvalidState, "accepting a block that was never verified");

    // An option block's parent proposal is accepted first, and its decision
    // layer applied before the outcome the option picked. The parent's entry is
    // released only AFTER that outcome has been applied: the option's own layer
    // is a layer over the decision layer, so dropping the parent first would
    // pull the ground out from under it.
    const bool is_option = b.kind() == block::Kind::Abort || b.kind() == block::Kind::Commit;
    if (is_option) {
        const auto parent = verified_.find(b.parent());
        if (parent == verified_.end()) return fail(Err::ParentNotFound);
        if (parent->second.on_decision)
            if (auto st = parent->second.on_decision->apply(state_); !st) return st;
    }

    if (it->second.on_accept)
        if (auto st = it->second.on_accept->apply(state_); !st) return st;

    if (is_option) verified_.erase(b.parent());

    last_accepted_ = b.id();
    last_accepted_height_ = b.height();
    preferred_ = last_accepted_;
    backend_.now = b.timestamp();
    blocks_[b.id()] = it->second.blk ? it->second.blk : blocks_[b.id()];

    // A proposal block keeps its layers until its option block chooses one.
    if (b.kind() != block::Kind::Proposal) verified_.erase(it);

    // Anything the block carried is no longer waiting.
    const auto included = b.decision_txs();
    mempool_.erase(std::remove_if(mempool_.begin(), mempool_.end(),
                                  [&](const txs::Tx& t) {
                                      return std::any_of(included.begin(), included.end(),
                                                         [&](const txs::Tx& c) { return c.tx_id == t.tx_id; });
                                  }),
                   mempool_.end());
    return ok();
}

std::shared_ptr<lux::node::Block> PlatformVM::build() {
    state::Chain* parent_state = state_after(preferred_);
    if (parent_state == nullptr) return nullptr;
    const auto parent_height = height_of(preferred_);
    if (!parent_height) return nullptr;
    const std::uint64_t height = parent_height.value() + 1;

    const std::uint64_t chain_time = parent_state->timestamp();
    // The clock may move to the next staker change, and no further than the
    // sync bound allows.
    const std::uint64_t bound = wall_clock_ + executor::kSyncBound;
    std::uint64_t next = state::next_staker_change_time(*parent_state, bound);
    if (next < chain_time) next = chain_time;
    const std::uint64_t timestamp = std::max(chain_time, std::min(next, bound));

    // A staker whose time is up is paid by a proposal block, and that takes
    // priority: it is the chain's own business, and it blocks everything else
    // until it is settled.
    const auto current = parent_state->current_stakers();
    if (!current.empty() && current.front().end_time <= timestamp &&
        current.front().priority != txs::Priority::ChainPermissionedValidatorCurrent) {
        txs::Tx reward;
        reward.unsigned_tx = txs::RewardValidatorTx::create(current.front().tx_id);
        if (!reward.initialize()) return nullptr;
        auto blk = block::ProposalBlock::create(current.front().end_time, preferred_, height, reward, {});
        if (!blk) return nullptr;
        blocks_[blk.value()->id()] = blk.value();
        return std::make_shared<VmBlock>(this, blk.value());
    }

    // Otherwise a standard block of whatever the mempool holds that still
    // executes. A transaction that no longer does is dropped rather than
    // dragged into a block that would be refused.
    state::Diff trial(parent_state);
    executor::Backend backend = backend_;
    backend.now = timestamp;
    auto changed = executor::advance_time_to(backend, trial, timestamp);
    if (!changed) return nullptr;

    std::vector<txs::Tx> included;
    std::set<Id> inputs;
    for (const auto& tx : mempool_) {
        bool overlaps = false;
        for (const auto& in : tx.input_ids())
            if (inputs.count(in) != 0) overlaps = true;
        if (overlaps) continue;
        state::Diff probe(&trial);
        if (!executor::standard_tx(backend, tx, probe)) continue;
        if (!probe.apply(trial)) continue;
        trial.add_tx(tx, status::Status::Committed);
        for (const auto& in : tx.input_ids()) inputs.insert(in);
        included.push_back(tx);
    }

    if (!changed.value() && included.empty()) return nullptr;

    auto blk = block::StandardBlock::create(timestamp, preferred_, height, included);
    if (!blk) return nullptr;
    blocks_[blk.value()->id()] = blk.value();
    return std::make_shared<VmBlock>(this, blk.value());
}

}  // namespace lux::platformvm::vm
