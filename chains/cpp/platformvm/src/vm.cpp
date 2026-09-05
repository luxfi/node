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
    std::memcpy(out.data(), id.data(), kIdLen);
    return out;
}

Id from_node_id(const lux::node::Id& id) {
    Id out{};
    std::memcpy(out.data(), id.data(), kIdLen);
    return out;
}

// Go: verifier.processStandardTxs, the complexity gate. The whole block's
// complexity is priced and taken out of the chain's capacity before a single
// transaction runs, so an oversized block is refused as a block rather than
// discovered halfway through.
Status consume_block_gas(const executor::Backend& backend, state::Chain& layer,
                         const std::vector<txs::Tx>& block_txs) {
    gas::Dimensions complexity;
    for (const auto& tx : block_txs) {
        auto c = fee::tx_complexity(*tx.unsigned_tx);
        if (!c) return std::unexpected(c.error());
        auto s = complexity.add(c.value());
        if (!s) return std::unexpected(s.error());
        complexity = s.value();
    }
    auto g = complexity.to_gas(backend.gas_config.weights);
    if (!g) return std::unexpected(g.error());
    auto consumed = layer.fee_state().consume(g.value());
    if (!consumed) return std::unexpected(consumed.error());
    layer.set_fee_state(consumed.value());
    return ok();
}

// Two transactions in one block can both touch the same peer chain; their
// requests are one request when the block is accepted.
void merge(std::map<Id, atomic::Requests>& into, const std::map<Id, atomic::Requests>& from) {
    for (const auto& [chain, req] : from) {
        auto& dst = into[chain];
        dst.remove.insert(dst.remove.end(), req.remove.begin(), req.remove.end());
        dst.put.insert(dst.put.end(), req.put.begin(), req.put.end());
    }
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
void VmBlock::reject() { (void)vm_->reject_block(*blk_); }

// ── the chain

PlatformVM::PlatformVM(const Id& chain_id, executor::Backend backend, const Genesis& genesis)
    : chain_id_(chain_id), backend_(std::move(backend)) {
    state_.set_timestamp(genesis.timestamp);
    state_.set_current_supply(kPrimaryNetworkId, genesis.initial_supply);
    state_.set_fee_state(genesis.fee_state);
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
    if (it == blocks_.end()) return fail(Err::ParentNotFound, hex(id));
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
            if (parent_state == nullptr) return fail(Err::ParentNotFound, hex(parent_id));
            if (auto st = executor::verify_new_chain_time(b.timestamp(), wall_clock_, *parent_state); !st)
                return st;

            auto layer = std::make_shared<state::Diff>(parent_state);
            executor::Backend backend = backend_;
            backend.now = b.timestamp();
            auto changed = executor::advance_time_to(backend, *layer, b.timestamp());
            if (!changed) return std::unexpected(changed.error());

            const auto block_txs = b.decision_txs();
            // The price comes off the chain's own excess, and the block's gas is
            // taken out of the chain's capacity BEFORE anything executes: a block
            // that asks for more than the chain has is refused whole.
            const executor::DynamicFee fees = executor::pick_fee_calculator(backend.gas_config, *layer);
            backend.fees = &fees;
            if (auto st = consume_block_gas(backend, *layer, block_txs); !st) return st;

            std::set<Id> inputs;
            for (const auto& tx : block_txs) {
                auto effects = executor::standard_tx(backend, tx, *layer);
                if (!effects) return std::unexpected(effects.error());
                // Two transactions in one block may not spend the same output:
                // both would verify against the layer as it stood before either
                // ran, so the overlap has to be refused explicitly. An import's
                // consumed outputs are in there too, which is why the executor
                // reports them rather than the caller re-deriving them.
                for (const auto& in : effects.value().inputs)
                    if (!inputs.insert(in).second) return fail(Err::ConflictingBlockTxs);
                merge(out.atomic_requests, effects.value().atomic_requests);
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
            if (parent_state == nullptr) return fail(Err::ParentNotFound, hex(parent_id));
            if (auto st = executor::verify_new_chain_time(b.timestamp(), wall_clock_, *parent_state); !st)
                return st;

            auto decision = std::make_shared<state::Diff>(parent_state);
            executor::Backend backend = backend_;
            backend.now = b.timestamp();
            auto changed = executor::advance_time_to(backend, *decision, b.timestamp());
            if (!changed) return std::unexpected(changed.error());

            const auto decision_txs = b.decision_txs();
            const executor::DynamicFee fees = executor::pick_fee_calculator(backend.gas_config, *decision);
            backend.fees = &fees;
            if (auto st = consume_block_gas(backend, *decision, decision_txs); !st) return st;

            std::set<Id> inputs;
            for (const auto& tx : decision_txs) {
                auto effects = executor::standard_tx(backend, tx, *decision);
                if (!effects) return std::unexpected(effects.error());
                for (const auto& in : effects.value().inputs)
                    if (!inputs.insert(in).second) return fail(Err::ConflictingBlockTxs);
                merge(out.atomic_requests, effects.value().atomic_requests);
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
    //
    // What each layer changes about the validator sets is recorded against this
    // height as the layer goes in, because it is the only moment both the layer
    // and the state it lands on are in hand. Recorded before applying, since
    // afterwards the change is indistinguishable from what was always there.
    auto note = [&](const state::Diff& layer) -> Status {
        auto c = validators::changes(layer, state_);
        if (!c) return std::unexpected(c.error());
        return history_.record(b.height(), c.value());
    };

    const bool is_option = b.kind() == block::Kind::Abort || b.kind() == block::Kind::Commit;
    if (is_option) {
        const auto parent = verified_.find(b.parent());
        if (parent == verified_.end()) return fail(Err::ParentNotFound);
        if (parent->second.on_decision) {
            if (auto st = note(*parent->second.on_decision); !st) return st;
            if (auto st = parent->second.on_decision->apply(state_); !st) return st;
        }
    }

    if (it->second.on_accept) {
        if (auto st = note(*it->second.on_accept); !st) return st;
        if (auto st = it->second.on_accept->apply(state_); !st) return st;
    }

    if (is_option) verified_.erase(b.parent());

    atomic_requests_ = it->second.atomic_requests;
    last_accepted_ = b.id();
    last_accepted_height_ = b.height();
    preferred_ = last_accepted_;
    backend_.now = b.timestamp();
    blocks_[b.id()] = it->second.blk ? it->second.blk : blocks_[b.id()];

    // A proposal block keeps its layers until its option block chooses one.
    if (b.kind() != block::Kind::Proposal) verified_.erase(it);

    // Anything the block carried is no longer waiting — nor is anything that
    // was waiting to spend the same outputs, which can never be accepted now.
    mempool_.remove(b.decision_txs());
    return ok();
}

Status PlatformVM::reject_block(const block::Block& b) {
    // THE LAYERS GO FIRST, and this is the half that is never optional. The
    // block can never be accepted, so the state its execution pinned is a state
    // nothing may be verified against — leave it and a child of a block the
    // network decided against still finds a parent to build on. This is Go's
    // `free`, and like Go's it frees exactly ONE block: an option block that
    // lost releases its own layer, while the proposal block above it keeps its
    // three until the winning option is accepted, which is where accept_block
    // releases them. The block's BYTES stay reachable through blocks_, as they
    // do in Go — being decided against is not being forgotten.
    verified_.erase(b.id());

    // ONLY THE DECISION TRANSACTIONS COME BACK. They were submitted by someone
    // else and are still theirs to have included, so they belong in the pool the
    // next build() draws from. A proposal block's own transaction is not
    // reachable from here at all: it belongs to the height that was rejected,
    // and the next builder emits a fresh one from current state. Go's rejector
    // says exactly this by iterating DecisionTxs, which never contains it.
    //
    // Not re-verified. Go's P-chain rejector re-issues unconditionally and lets
    // the next build discover that a transaction no longer executes — where it
    // is skipped rather than dragged into a block that would be refused. (X's
    // rejector DOES re-verify; the two chains genuinely differ, and each port
    // follows its own.)
    //
    // Usually there is nothing to do, and that is not this call being pointless
    // — it is where the invariant is already held. Go's builder takes a
    // transaction OUT of the pool as it packs it (executeTx → mempool.Remove),
    // so on that side reject is the only thing that puts it back; this builder
    // peeks and removes at ACCEPT instead, so a block that never reached accept
    // never emptied anything. Two roads, one invariant: after a decision the
    // pool holds exactly what is still pending. What is asked for here is that
    // invariant rather than Go's mechanism, so anything already waiting is left
    // alone — re-adding it would report a duplicate and record a refusal
    // against a transaction that is, in fact, waiting.
    for (const auto& tx : b.decision_txs()) {
        if (mempool_.has(tx.id())) continue;
        // A genuine refusal — the pool is full, or something waiting already
        // rivals this for the same outputs. Remembered, the way every other
        // refusal here is, so whoever submitted it can be told rather than left
        // with silence.
        if (auto st = mempool_.add(tx); !st) mempool_.mark_dropped(tx.id(), st.error());
    }

    return ok();
}

namespace {

// Go: options.prefersCommit. True iff the staker being settled met the uptime
// requirement that bound it.
Result<bool> prefers_commit(const executor::Backend& backend, const state::Chain& s,
                            const block::ProposalBlock& b) {
    auto tx = b.tx();
    if (!tx) return std::unexpected(tx.error());
    const auto* reward_tx = dynamic_cast<const txs::RewardValidatorTx*>(tx.value().unsigned_tx.get());
    if (reward_tx == nullptr) return fail(Err::WrongTxType, "proposal is not a reward");

    auto staker_tx = s.get_tx(reward_tx->tx_id());
    if (!staker_tx) return std::unexpected(staker_tx.error());
    auto view = txs::staker_of(*staker_tx.value().first.unsigned_tx);
    if (!view) return std::unexpected(view.error());
    if (!view.value()) return fail(Err::WrongTxType, "the settled transaction admits no staker");

    const NodeId node_id = view.value()->node_id;
    const Id chain_id = view.value()->chain_id;

    // The uptime rule is read against the PRIMARY network entry, because that is
    // the term the node actually bonded for.
    auto primary = s.get_current_validator(kPrimaryNetworkId, node_id);
    if (!primary) return std::unexpected(primary.error());

    // Judge the validator on the uptime rule that was in force when it BONDED,
    // not the one in force now. Reading it at the staker's start time is what
    // stops a governed requirement from being retroactive.
    const auto bound_by = backend.policy_at(static_cast<std::int64_t>(primary.value().start_time));
    double required = static_cast<double>(bound_by.uptime_requirement) /
                      static_cast<double>(reward::kPercentDenominator);
    if (!(chain_id == kPrimaryNetworkId)) {
        auto transform = s.network_transformation(chain_id);
        if (!transform) return std::unexpected(transform.error());
        const auto* t = dynamic_cast<const txs::TransformChainTx*>(transform.value().unsigned_tx.get());
        if (t == nullptr) return fail(Err::IsNotTransformChainTx);
        required = static_cast<double>(t->uptime_requirement()) /
                   static_cast<double>(reward::kPercentDenominator);
    }

    if (backend.uptimes == nullptr) return fail(Err::InvalidState, "no uptime calculator");
    auto measured = backend.uptimes->percent_from(node_id, chain_id, primary.value().start_time);
    if (!measured) return std::unexpected(measured.error());
    return measured.value() >= required;
}

}  // namespace

Result<std::pair<std::shared_ptr<lux::node::Block>, std::shared_ptr<lux::node::Block>>> PlatformVM::options(
    const block::ProposalBlock& b) {
    const Id parent = b.id();
    const std::uint64_t height = b.height() + 1;
    auto commit = block::CommitBlock::create(b.timestamp(), parent, height);
    if (!commit) return std::unexpected(commit.error());
    auto abort = block::AbortBlock::create(b.timestamp(), parent, height);
    if (!abort) return std::unexpected(abort.error());
    blocks_[commit.value()->id()] = commit.value();
    blocks_[abort.value()->id()] = abort.value();

    state::Chain* s = state_after(b.parent());
    if (s == nullptr) s = &state_;
    // A node that cannot answer prefers commit: the failure can be caused by the
    // proposer, and erring toward over-rewarding cannot take a validator's
    // reward away from it.
    const auto answer = prefers_commit(backend_, *s, b);
    const bool prefer_commit = answer ? answer.value() : true;

    auto preferred = std::make_shared<VmBlock>(this, prefer_commit ? std::static_pointer_cast<block::Block>(
                                                                         commit.value())
                                                                   : std::static_pointer_cast<block::Block>(
                                                                         abort.value()));
    auto alternate = std::make_shared<VmBlock>(this, prefer_commit ? std::static_pointer_cast<block::Block>(
                                                                         abort.value())
                                                                   : std::static_pointer_cast<block::Block>(
                                                                         commit.value()));
    return std::make_pair(std::static_pointer_cast<lux::node::Block>(preferred),
                          std::static_pointer_cast<lux::node::Block>(alternate));
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
    const executor::DynamicFee fees = executor::pick_fee_calculator(backend.gas_config, trial);
    backend.fees = &fees;

    std::vector<txs::Tx> included;
    std::set<Id> inputs;
    for (const auto& tx : mempool_.peek(mempool_.size())) {
        bool overlaps = false;
        for (const auto& in : tx.input_ids())
            if (inputs.count(in) != 0) overlaps = true;
        if (overlaps) continue;
        state::Diff probe(&trial);
        if (!executor::standard_tx(backend, tx, probe)) continue;
        if (!probe.apply(trial)) continue;
        // Charge the block's own gas as it is filled, so a block this builder
        // offers is a block this verifier accepts.
        if (!consume_block_gas(backend, trial, {tx})) continue;
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
