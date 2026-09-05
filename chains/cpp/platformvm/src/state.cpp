// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state.cpp — the staker set, its diffs, and the layer a block is verified on.
//
// Rendered from Go vms/platformvm/state (stakers.go, staker.go,
// staker_diff_iterator.go, diff.go).

#include "lux/platformvm/state.hpp"

#include <algorithm>

namespace lux::platformvm::state {
namespace {

// Merge two ordered staker lists under Staker::less, dropping anything whose tx
// id is in `deleted`. This is Go's iterator.Filter(iterator.Merge(...)), which
// every caller immediately collected anyway.
StakerList merge_filter(const StakerList& a, const StakerList& b, const std::map<Id, Staker>& deleted) {
    StakerList out;
    out.reserve(a.size() + b.size());
    std::size_t i = 0, j = 0;
    while (i < a.size() || j < b.size()) {
        const Staker* pick = nullptr;
        if (i < a.size() && (j >= b.size() || !b[j].less(a[i]))) {
            pick = &a[i++];
        } else {
            pick = &b[j++];
        }
        if (deleted.find(pick->tx_id) == deleted.end()) out.push_back(*pick);
    }
    return out;
}

StakerList to_list(const StakerTree& t) { return StakerList(t.begin(), t.end()); }

}  // namespace

// ── constructing a staker from the transaction that admits one

Result<Staker> new_current_staker(const Id& tx_id, const txs::StakerView& view, std::uint64_t start_time,
                                  std::uint64_t potential_reward) {
    Staker s;
    s.tx_id = tx_id;
    s.node_id = view.node_id;
    s.public_key = view.public_key;
    s.chain_id = view.chain_id;
    s.weight = view.weight;
    s.start_time = start_time;
    s.end_time = view.end;
    s.potential_reward = potential_reward;
    s.next_time = view.end;
    s.priority = view.current_priority;
    return s;
}

Result<Staker> new_pending_staker(const Id& tx_id, const txs::StakerView& view) {
    if (!view.scheduled)
        return fail(Err::WrongTxType, "transaction has no start time of its own, so it cannot be pending");
    Staker s;
    s.tx_id = tx_id;
    s.node_id = view.node_id;
    s.public_key = view.public_key;
    s.chain_id = view.chain_id;
    s.weight = view.weight;
    s.start_time = view.start;
    s.end_time = view.end;
    s.next_time = view.start;
    s.priority = view.pending_priority;
    return s;
}

// ── the weight one validator entry moved by

// Go: ValidatorWeightDiff.addOrSub. A signed quantity kept as (sign, magnitude):
// same sign accumulates, opposite sign cancels — and cancelling EXACTLY leaves
// zero carrying the new sign, which is what the reference does and what a test
// comparing two diffs sees.
Status WeightDiff::add_or_sub(bool sub, std::uint64_t amount) {
    if (decrease == sub) {
        if (this->amount > UINT64_MAX - amount) return fail(Err::Overflow, "validator weight diff");
        this->amount += amount;
        return ok();
    }
    if (this->amount > amount) {
        this->amount -= amount;
    } else {
        this->amount = amount - this->amount;
        decrease = sub;
    }
    return ok();
}

Status WeightDiff::add(std::uint64_t w) { return add_or_sub(false, w); }
Status WeightDiff::sub(std::uint64_t w) { return add_or_sub(true, w); }

Result<WeightDiff> ValidatorDiff::weight_diff() const {
    WeightDiff d;
    d.decrease = status == DiffStatus::Deleted;
    if (status != DiffStatus::Unmodified) {
        if (!validator) return fail(Err::InvalidState, "validator diff has a status but no validator");
        d.amount = validator->weight;
    }
    for (const auto& [id, s] : deleted_delegators) {
        (void)id;
        if (auto st = d.sub(s.weight); !st) return std::unexpected(st.error());
    }
    for (const auto& s : added_delegators) {
        if (auto st = d.add(s.weight); !st) return std::unexpected(st.error());
    }
    return d;
}

// ── the base staker set

BaseStakers::Entry& BaseStakers::entry_for(const Id& chain_id, const NodeId& node_id) {
    return validators_[chain_id][node_id];
}

ValidatorDiff& BaseStakers::diff_for(const Id& chain_id, const NodeId& node_id) {
    return diffs_[chain_id][node_id];
}

// A node with neither a validator nor a delegator left is not an entry: it is
// nothing, and leaving it behind would make the set's emptiness a lie.
void BaseStakers::prune(const Id& chain_id, const NodeId& node_id) {
    auto chain = validators_.find(chain_id);
    if (chain == validators_.end()) return;
    auto node = chain->second.find(node_id);
    if (node == chain->second.end()) return;
    if (node->second.validator.has_value()) return;
    if (!node->second.delegators.empty()) return;
    chain->second.erase(node);
    if (chain->second.empty()) validators_.erase(chain);
}

Result<Staker> BaseStakers::get_validator(const Id& chain_id, const NodeId& node_id) const {
    const auto chain = validators_.find(chain_id);
    if (chain == validators_.end()) return fail(Err::NotFound);
    const auto node = chain->second.find(node_id);
    if (node == chain->second.end()) return fail(Err::NotFound);
    if (!node->second.validator) return fail(Err::NotFound);
    return *node->second.validator;
}

void BaseStakers::put_validator(const Staker& s) {
    entry_for(s.chain_id, s.node_id).validator = s;
    auto& d = diff_for(s.chain_id, s.node_id);
    d.status = DiffStatus::Added;
    d.validator = s;
    stakers_.erase(s);
    stakers_.insert(s);
}

void BaseStakers::delete_validator(const Staker& s) {
    entry_for(s.chain_id, s.node_id).validator.reset();
    prune(s.chain_id, s.node_id);
    auto& d = diff_for(s.chain_id, s.node_id);
    d.status = DiffStatus::Deleted;
    d.validator = s;
    stakers_.erase(s);
}

StakerList BaseStakers::delegator_list(const Id& chain_id, const NodeId& node_id) const {
    const auto chain = validators_.find(chain_id);
    if (chain == validators_.end()) return {};
    const auto node = chain->second.find(node_id);
    if (node == chain->second.end()) return {};
    return to_list(node->second.delegators);
}

void BaseStakers::put_delegator(const Staker& s) {
    auto& e = entry_for(s.chain_id, s.node_id);
    e.delegators.erase(s);
    e.delegators.insert(s);
    auto& d = diff_for(s.chain_id, s.node_id);
    d.added_delegators.erase(s);
    d.added_delegators.insert(s);
    stakers_.erase(s);
    stakers_.insert(s);
}

void BaseStakers::delete_delegator(const Staker& s) {
    entry_for(s.chain_id, s.node_id).delegators.erase(s);
    prune(s.chain_id, s.node_id);
    diff_for(s.chain_id, s.node_id).deleted_delegators[s.tx_id] = s;
    stakers_.erase(s);
}

StakerList BaseStakers::staker_list() const { return to_list(stakers_); }

void BaseStakers::load_validator(const Staker& s) {
    entry_for(s.chain_id, s.node_id).validator = s;
    stakers_.erase(s);
    stakers_.insert(s);
}

void BaseStakers::load_delegator(const Staker& s) {
    auto& e = entry_for(s.chain_id, s.node_id);
    e.delegators.erase(s);
    e.delegators.insert(s);
    stakers_.erase(s);
    stakers_.insert(s);
}

// ── one layer of staker changes

ValidatorDiff& DiffStakers::diff_for(const Id& chain_id, const NodeId& node_id) {
    return diffs_[chain_id][node_id];
}

std::pair<std::optional<Staker>, DiffStatus> DiffStakers::get_validator(const Id& chain_id,
                                                                       const NodeId& node_id) const {
    const auto chain = diffs_.find(chain_id);
    if (chain == diffs_.end()) return {std::nullopt, DiffStatus::Unmodified};
    const auto node = chain->second.find(node_id);
    if (node == chain->second.end()) return {std::nullopt, DiffStatus::Unmodified};
    if (node->second.status == DiffStatus::Added) return {node->second.validator, DiffStatus::Added};
    return {std::nullopt, node->second.status};
}

Status DiffStakers::put_validator(const Staker& s) {
    auto& d = diff_for(s.chain_id, s.node_id);
    // A validator that was deleted in this layer cannot come back in it: the
    // deletion is what the layer says happened, and saying both is saying
    // nothing.
    if (d.status == DiffStatus::Deleted) return fail(Err::AddingStakerAfterDeletion);
    d.status = DiffStatus::Added;
    d.validator = s;
    added_.erase(s);
    added_.insert(s);
    return ok();
}

void DiffStakers::delete_validator(const Staker& s) {
    auto& d = diff_for(s.chain_id, s.node_id);
    if (d.status == DiffStatus::Added) {
        // Added and removed within one layer: the layer says nothing happened.
        d.status = DiffStatus::Unmodified;
        if (d.validator) added_.erase(*d.validator);
        d.validator.reset();
        return;
    }
    d.status = DiffStatus::Deleted;
    d.validator = s;
    deleted_[s.tx_id] = s;
}

StakerList DiffStakers::delegator_list(StakerList parent, const Id& chain_id, const NodeId& node_id) const {
    StakerList added;
    std::map<Id, Staker> deleted;
    const auto chain = diffs_.find(chain_id);
    if (chain != diffs_.end()) {
        const auto node = chain->second.find(node_id);
        if (node != chain->second.end()) {
            added = to_list(node->second.added_delegators);
            deleted = node->second.deleted_delegators;
        }
    }
    return merge_filter(parent, added, deleted);
}

void DiffStakers::put_delegator(const Staker& s) {
    auto& d = diff_for(s.chain_id, s.node_id);
    d.added_delegators.erase(s);
    d.added_delegators.insert(s);
    added_.erase(s);
    added_.insert(s);
}

void DiffStakers::delete_delegator(const Staker& s) {
    diff_for(s.chain_id, s.node_id).deleted_delegators[s.tx_id] = s;
    deleted_[s.tx_id] = s;
}

StakerList DiffStakers::staker_list(StakerList parent) const {
    return merge_filter(parent, to_list(added_), deleted_);
}

// ── the staker-diff walk

MutableStakerWalk::MutableStakerWalk(StakerList source) : source_(std::move(source)) {}

void MutableStakerWalk::add(const Staker& s) {
    heap_.push_back(s);
    std::push_heap(heap_.begin(), heap_.end(), Greater{});
}

// Go: mutableStakerIterator.Next. The heap holds what has been pulled from the
// source plus anything pushed in; the source is pulled lazily, so an element
// pushed at an earlier time is delivered before a later source element.
bool MutableStakerWalk::next() {
    if (!heap_.empty()) {
        std::pop_heap(heap_.begin(), heap_.end(), Greater{});
        heap_.pop_back();
    }
    if (at_ >= source_.size()) return !heap_.empty();
    const Staker next_source = source_[at_];
    if (heap_.empty() || next_source.less(heap_.front())) {
        add(next_source);
        ++at_;
    }
    return true;
}

void MutableStakerWalk::release() {
    at_ = source_.size();
    heap_.clear();
}

StakerDiffWalk::StakerDiffWalk(StakerList current, StakerList pending)
    : current_(std::move(current)), pending_(std::move(pending)) {
    current_exhausted_ = !current_.next();
    pending_exhausted_ = pending_.empty();
}

void StakerDiffWalk::advance_current() {
    modified_ = current_.value();
    is_added_ = false;
    current_exhausted_ = !current_.next();
}

void StakerDiffWalk::advance_pending() {
    modified_ = pending_[pending_at_];
    is_added_ = true;
    ++pending_at_;
    pending_exhausted_ = pending_at_ >= pending_.size();

    // The staker just added will have to leave again at its end time, so queue
    // that removal now — which is why the current side must be mutable.
    Staker to_remove = modified_;
    to_remove.next_time = to_remove.end_time;
    to_remove.priority = txs::pending_to_current(to_remove.priority);
    current_exhausted_ = false;
    current_.add(to_remove);
}

bool StakerDiffWalk::next() {
    if (current_exhausted_ && pending_exhausted_) return false;
    if (current_exhausted_) {
        advance_pending();
    } else if (pending_exhausted_) {
        advance_current();
    } else {
        const Staker removed = current_.value();
        const Staker& added = pending_[pending_at_];
        // At one instant a staker is ADDED before another is removed, so the tie
        // defaults to advancing the pending side.
        if (removed.end_time < added.start_time) {
            advance_current();
        } else {
            advance_pending();
        }
    }
    return true;
}

void StakerDiffWalk::release() {
    current_exhausted_ = true;
    pending_exhausted_ = true;
    current_.release();
    pending_at_ = pending_.size();
    modified_ = Staker{};
}

// ── the materialised state

Result<std::uint64_t> MemState::current_supply(const Id& chain_id) const {
    const auto it = supply_.find(chain_id);
    if (it == supply_.end()) return fail(Err::NotFound);
    return it->second;
}

Result<UTXO> MemState::get_utxo(const Id& utxo_id) const {
    const auto it = utxos_.find(utxo_id);
    if (it == utxos_.end()) return fail(Err::NotFound);
    return it->second;
}

std::vector<UTXO> MemState::reward_utxos(const Id& tx_id) const {
    const auto it = reward_utxos_.find(tx_id);
    if (it == reward_utxos_.end()) return {};
    return it->second;
}

// Go: metadata.GetDelegateeReward / SetDelegateeReward. A validator that is not
// in the set has no ledger, and asking about one is a refusal rather than a
// zero: zero is a real balance, and confusing the two would silently pay a
// staker that is not there.
std::vector<UTXO> MemState::utxos() const {
    std::vector<UTXO> out;
    out.reserve(utxos_.size());
    for (const auto& [id, u] : utxos_) {
        (void)id;
        out.push_back(u);
    }
    return out;
}

Result<std::uint64_t> MemState::delegatee_reward(const Id& chain_id, const NodeId& node_id) const {
    const auto chain = delegatee_rewards_.find(chain_id);
    if (chain == delegatee_rewards_.end()) return fail(Err::NotFound);
    const auto node = chain->second.find(node_id);
    if (node == chain->second.end()) return fail(Err::NotFound);
    return node->second;
}

Status MemState::set_delegatee_reward(const Id& chain_id, const NodeId& node_id, std::uint64_t amount) {
    auto chain = delegatee_rewards_.find(chain_id);
    if (chain == delegatee_rewards_.end()) return fail(Err::NotFound);
    auto node = chain->second.find(node_id);
    if (node == chain->second.end()) return fail(Err::NotFound);
    node->second = amount;
    return ok();
}

Result<txs::Owner> MemState::network_owner(const Id& network_id) const {
    const auto it = net_owners_.find(network_id);
    if (it == net_owners_.end()) return fail(Err::NotFound);
    return it->second;
}

Result<NetToL1Conversion> MemState::network_conversion(const Id& network_id) const {
    const auto it = conversions_.find(network_id);
    if (it == conversions_.end()) return fail(Err::NotFound);
    return it->second;
}

Result<txs::Tx> MemState::network_transformation(const Id& network_id) const {
    const auto it = transformations_.find(network_id);
    if (it == transformations_.end()) return fail(Err::NotFound);
    return it->second;
}

void MemState::add_network_transformation(const txs::Tx& tx) {
    const auto* t = dynamic_cast<const txs::TransformChainTx*>(tx.unsigned_tx.get());
    if (t == nullptr) return;
    transformations_.insert_or_assign(t->chain(), tx);
}

void MemState::add_chain(const txs::Tx& create_chain_tx) {
    const auto* c = dynamic_cast<const txs::CreateChainTx*>(create_chain_tx.unsigned_tx.get());
    if (c == nullptr) return;
    chains_[c->chain_id()].push_back(create_chain_tx);
    chain_names_.insert(c->blockchain_name());
}

std::vector<txs::Tx> MemState::chains(const Id& network_id) const {
    const auto it = chains_.find(network_id);
    if (it == chains_.end()) return {};
    return it->second;
}

Result<std::pair<txs::Tx, status::Status>> MemState::get_tx(const Id& tx_id) const {
    const auto it = txs_.find(tx_id);
    if (it == txs_.end()) return fail(Err::NotFound);
    return it->second;
}

// ── the L1 validator set, in memory

// Go: state.PutL1Validator. Three invariants, all of them about a validation id
// naming ONE validator for its whole life.
Status MemState::put_l1_validator(const l1::Validator& v) {
    const auto existing = l1_validators_.find(v.validation_id);
    if (existing != l1_validators_.end() && !existing->second.immutable_fields_unmodified(v))
        return fail(Err::MutatedL1Validator, "a constant field of " + hex(v.validation_id) + " changed");

    if (v.is_deleted()) {
        l1_validators_.erase(v.validation_id);
        return ok();
    }

    // One (chain, node) pair at a time: two validators sharing it would be one
    // node voting twice.
    for (const auto& [id, other] : l1_validators_) {
        if (id == v.validation_id) continue;
        if (other.chain_id == v.chain_id && other.node_id == v.node_id)
            return fail(Err::DuplicateL1Validator,
                        hex(v.node_id) + " already validates " + hex(v.chain_id));
    }

    // And the chain's total must stay a number.
    std::uint64_t total = 0;
    for (const auto& [id, other] : l1_validators_) {
        if (id == v.validation_id) continue;
        if (!(other.chain_id == v.chain_id)) continue;
        auto sum = add64(total, other.weight);
        if (!sum) return fail(Err::Overflow, "the chain's L1 weight overflows");
        total = sum.value();
    }
    if (!add64(total, v.weight)) return fail(Err::Overflow, "the chain's L1 weight overflows");

    l1_validators_[v.validation_id] = v;
    return ok();
}

Result<l1::Validator> MemState::get_l1_validator(const Id& validation_id) const {
    const auto it = l1_validators_.find(validation_id);
    if (it == l1_validators_.end()) return fail(Err::NotFound);
    return it->second;
}

bool MemState::has_l1_validator(const Id& chain_id, const NodeId& node_id) const {
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (v.chain_id == chain_id && v.node_id == node_id) return true;
    }
    return false;
}

// In increasing EndAccumulatedFee, so advancing the clock deactivates exactly
// the prefix that can no longer pay and stops at the first one that can.
std::vector<l1::Validator> MemState::active_l1_validators() const {
    std::vector<l1::Validator> out;
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (v.is_active()) out.push_back(v);
    }
    std::sort(out.begin(), out.end(), l1::ValidatorLess{});
    return out;
}

std::vector<l1::Validator> MemState::l1_validators(const Id& chain_id) const {
    std::vector<l1::Validator> out;
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (v.chain_id == chain_id) out.push_back(v);
    }
    return out;
}

std::size_t MemState::num_active_l1_validators() const {
    std::size_t n = 0;
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (v.is_active()) ++n;
    }
    return n;
}

// Active AND inactive: an inactive validator still weighs on the set it is in.
Result<std::uint64_t> MemState::weight_of_l1_validators(const Id& chain_id) const {
    std::uint64_t total = 0;
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (!(v.chain_id == chain_id)) continue;
        auto sum = add64(total, v.weight);
        if (!sum) return fail(Err::Overflow, "the chain\'s L1 weight overflows");
        total = sum.value();
    }
    return total;
}

// ── the diff layer

Result<std::uint64_t> Diff::current_supply(const Id& chain_id) const {
    const auto it = supply_.find(chain_id);
    if (it != supply_.end()) return it->second;
    return parent_->current_supply(chain_id);
}

Result<UTXO> Diff::get_utxo(const Id& utxo_id) const {
    if (deleted_utxos_.count(utxo_id) != 0) return fail(Err::NotFound);
    const auto it = added_utxos_.find(utxo_id);
    if (it != added_utxos_.end()) return it->second;
    return parent_->get_utxo(utxo_id);
}

void Diff::add_utxo(const UTXO& u) {
    deleted_utxos_.erase(u.id());
    added_utxos_[u.id()] = u;
}

void Diff::delete_utxo(const Id& utxo_id) {
    added_utxos_.erase(utxo_id);
    deleted_utxos_.insert(utxo_id);
}

std::vector<UTXO> Diff::reward_utxos(const Id& tx_id) const {
    const auto it = reward_utxos_.find(tx_id);
    if (it != reward_utxos_.end()) return it->second;
    return parent_->reward_utxos(tx_id);
}

// The parent's set, minus what this layer deleted, plus what it added — in id
// order, so two nodes fold the same bytes.
std::vector<UTXO> Diff::utxos() const {
    std::map<Id, UTXO> merged;
    for (const auto& u : parent_->utxos()) merged[u.id()] = u;
    for (const auto& id : deleted_utxos_) merged.erase(id);
    for (const auto& [id, u] : added_utxos_) merged[id] = u;
    std::vector<UTXO> out;
    out.reserve(merged.size());
    for (const auto& [id, u] : merged) {
        (void)id;
        out.push_back(u);
    }
    return out;
}

std::vector<Id> Diff::networks() const {
    std::set<Id> merged;
    for (const auto& id : parent_->networks()) merged.insert(id);
    for (const auto& id : added_networks_) merged.insert(id);
    return {merged.begin(), merged.end()};
}

Result<Staker> Diff::get_current_validator(const Id& chain_id, const NodeId& node_id) const {
    const auto [s, status] = current_.get_validator(chain_id, node_id);
    if (status == DiffStatus::Added) return *s;
    if (status == DiffStatus::Deleted) return fail(Err::NotFound);
    return parent_->get_current_validator(chain_id, node_id);
}

StakerList Diff::current_delegators(const Id& c, const NodeId& n) const {
    return current_.delegator_list(parent_->current_delegators(c, n), c, n);
}

StakerList Diff::current_stakers() const { return current_.staker_list(parent_->current_stakers()); }

Result<Staker> Diff::get_pending_validator(const Id& chain_id, const NodeId& node_id) const {
    const auto [s, status] = pending_.get_validator(chain_id, node_id);
    if (status == DiffStatus::Added) return *s;
    if (status == DiffStatus::Deleted) return fail(Err::NotFound);
    return parent_->get_pending_validator(chain_id, node_id);
}

StakerList Diff::pending_delegators(const Id& c, const NodeId& n) const {
    return pending_.delegator_list(parent_->pending_delegators(c, n), c, n);
}

StakerList Diff::pending_stakers() const { return pending_.staker_list(parent_->pending_stakers()); }

Result<std::uint64_t> Diff::delegatee_reward(const Id& chain_id, const NodeId& node_id) const {
    const auto chain = delegatee_rewards_.find(chain_id);
    if (chain != delegatee_rewards_.end()) {
        const auto node = chain->second.find(node_id);
        if (node != chain->second.end()) return node->second;
    }
    return parent_->delegatee_reward(chain_id, node_id);
}

Status Diff::set_delegatee_reward(const Id& chain_id, const NodeId& node_id, std::uint64_t amount) {
    delegatee_rewards_[chain_id][node_id] = amount;
    return ok();
}

// The layer's L1 view: what this layer says, then what the parent says. A
// deletion is recorded as a zero-weight record rather than as an absence, so a
// read cannot fall through to the parent and resurrect it.
Result<l1::Validator> Diff::get_l1_validator(const Id& validation_id) const {
    const auto it = l1_validators_.find(validation_id);
    if (it != l1_validators_.end()) {
        if (it->second.is_deleted()) return fail(Err::NotFound);
        return it->second;
    }
    return parent_->get_l1_validator(validation_id);
}

bool Diff::has_l1_validator(const Id& chain_id, const NodeId& node_id) const {
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (v.chain_id == chain_id && v.node_id == node_id) return !v.is_deleted();
    }
    return parent_->has_l1_validator(chain_id, node_id);
}

Status Diff::put_l1_validator(const l1::Validator& v) {
    if (auto existing = get_l1_validator(v.validation_id);
        existing && !existing.value().immutable_fields_unmodified(v))
        return fail(Err::MutatedL1Validator, "a constant field of " + hex(v.validation_id) + " changed");
    l1_validators_[v.validation_id] = v;
    return ok();
}

std::vector<l1::Validator> Diff::active_l1_validators() const {
    std::map<Id, l1::Validator> merged;
    for (const auto& v : parent_->active_l1_validators()) merged[v.validation_id] = v;
    for (const auto& [id, v] : l1_validators_) {
        if (v.is_active()) {
            merged[id] = v;
        } else {
            merged.erase(id);
        }
    }
    std::vector<l1::Validator> out;
    out.reserve(merged.size());
    for (const auto& [id, v] : merged) {
        (void)id;
        out.push_back(v);
    }
    std::sort(out.begin(), out.end(), l1::ValidatorLess{});
    return out;
}

std::vector<l1::Validator> Diff::l1_validators(const Id& chain_id) const {
    std::map<Id, l1::Validator> merged;
    for (const auto& v : parent_->l1_validators(chain_id)) merged[v.validation_id] = v;
    for (const auto& [id, v] : l1_validators_) {
        if (!(v.chain_id == chain_id)) continue;
        if (v.is_deleted())
            merged.erase(id);
        else
            merged[id] = v;
    }
    std::vector<l1::Validator> out;
    out.reserve(merged.size());
    for (const auto& [id, v] : merged) {
        (void)id;
        out.push_back(v);
    }
    return out;
}

std::size_t Diff::num_active_l1_validators() const { return active_l1_validators().size(); }

Result<std::uint64_t> Diff::weight_of_l1_validators(const Id& chain_id) const {
    auto base = parent_->weight_of_l1_validators(chain_id);
    if (!base) return base;
    std::uint64_t total = base.value();
    for (const auto& [id, v] : l1_validators_) {
        if (!(v.chain_id == chain_id)) continue;
        // What the layer says replaces what the parent said about the same id.
        if (auto old = parent_->get_l1_validator(id); old && old.value().chain_id == chain_id) {
            auto without = sub64(total, old.value().weight);
            if (!without) return fail(Err::Underflow, "the chain\'s L1 weight underflows");
            total = without.value();
        }
        auto sum = add64(total, v.weight);
        if (!sum) return fail(Err::Overflow, "the chain\'s L1 weight overflows");
        total = sum.value();
    }
    return total;
}

std::vector<l1::ExpiryEntry> Diff::expiries() const {
    std::set<l1::ExpiryEntry, l1::ExpiryLess> merged;
    for (const auto& e : parent_->expiries()) merged.insert(e);
    for (const auto& [e, added] : expiry_diff_) {
        if (added) {
            merged.insert(e);
        } else {
            merged.erase(e);
        }
    }
    return {merged.begin(), merged.end()};
}

bool Diff::has_expiry(const l1::ExpiryEntry& e) const {
    const auto it = expiry_diff_.find(e);
    if (it != expiry_diff_.end()) return it->second;
    return parent_->has_expiry(e);
}

void Diff::put_expiry(const l1::ExpiryEntry& e) { expiry_diff_[e] = true; }
void Diff::delete_expiry(const l1::ExpiryEntry& e) { expiry_diff_[e] = false; }

bool Diff::has_network(const Id& network_id) const {
    return added_networks_.count(network_id) != 0 || parent_->has_network(network_id);
}

Result<txs::Owner> Diff::network_owner(const Id& network_id) const {
    const auto it = net_owners_.find(network_id);
    if (it != net_owners_.end()) return it->second;
    return parent_->network_owner(network_id);
}

Result<NetToL1Conversion> Diff::network_conversion(const Id& network_id) const {
    const auto it = conversions_.find(network_id);
    if (it != conversions_.end()) return it->second;
    return parent_->network_conversion(network_id);
}

Result<txs::Tx> Diff::network_transformation(const Id& network_id) const {
    const auto it = transformations_.find(network_id);
    if (it != transformations_.end()) return it->second;
    return parent_->network_transformation(network_id);
}

void Diff::add_network_transformation(const txs::Tx& tx) {
    const auto* t = dynamic_cast<const txs::TransformChainTx*>(tx.unsigned_tx.get());
    if (t == nullptr) return;
    transformations_.insert_or_assign(t->chain(), tx);
}

void Diff::add_chain(const txs::Tx& create_chain_tx) {
    const auto* c = dynamic_cast<const txs::CreateChainTx*>(create_chain_tx.unsigned_tx.get());
    if (c == nullptr) return;
    chains_[c->chain_id()].push_back(create_chain_tx);
    chain_names_.insert(c->blockchain_name());
}

std::vector<txs::Tx> Diff::chains(const Id& network_id) const {
    auto out = parent_->chains(network_id);
    const auto it = chains_.find(network_id);
    if (it != chains_.end()) out.insert(out.end(), it->second.begin(), it->second.end());
    return out;
}

bool Diff::chain_name_taken(const std::string& name) const {
    return chain_names_.count(name) != 0 || parent_->chain_name_taken(name);
}

Result<std::pair<txs::Tx, status::Status>> Diff::get_tx(const Id& tx_id) const {
    const auto it = added_txs_.find(tx_id);
    if (it != added_txs_.end()) return it->second;
    return parent_->get_tx(tx_id);
}

// Go: diff.Apply. THE ORDER IS THE REFERENCE'S ORDER, and it is load-bearing:
// the delegatee-reward ledger exists only for a validator that is in the set, so
// the current staker changes must land before the rewards that name them, and
// the pending changes after.
Status Diff::apply(Chain& target) const {
    target.set_timestamp(timestamp_);
    target.set_accrued_fees(accrued_fees_);
    target.set_fee_state(fee_state_);
    target.set_l1_validator_excess(l1_excess_);

    // Every DELETION lands before any addition, so a (chain, node) pair that was
    // removed and re-added in one layer cannot be rejected as a duplicate of
    // itself. The reference orders these for the same reason.
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (!v.is_deleted()) continue;
        if (auto s = target.put_l1_validator(v); !s) return s;
    }
    for (const auto& [id, v] : l1_validators_) {
        (void)id;
        if (v.is_deleted()) continue;
        if (auto s = target.put_l1_validator(v); !s) return s;
    }
    for (const auto& [e, added] : expiry_diff_) {
        if (added) {
            target.put_expiry(e);
        } else {
            target.delete_expiry(e);
        }
    }
    for (const auto& [chain_id, s] : supply_) target.set_current_supply(chain_id, s);

    for (const auto& [chain_id, nodes] : current_.validator_diffs()) {
        (void)chain_id;
        for (const auto& [node_id, d] : nodes) {
            (void)node_id;
            switch (d.status) {
                case DiffStatus::Added:
                    if (auto s = target.put_current_validator(*d.validator); !s)
                        return std::unexpected(s.error());
                    break;
                case DiffStatus::Deleted:
                    target.delete_current_validator(*d.validator);
                    break;
                case DiffStatus::Unmodified:
                    break;
            }
            for (const auto& add : d.added_delegators) target.put_current_delegator(add);
            for (const auto& [id, del] : d.deleted_delegators) {
                (void)id;
                target.delete_current_delegator(del);
            }
        }
    }
    for (const auto& [chain_id, nodes] : delegatee_rewards_)
        for (const auto& [node_id, amount] : nodes)
            if (auto s = target.set_delegatee_reward(chain_id, node_id, amount); !s)
                return std::unexpected(s.error());

    for (const auto& [chain_id, nodes] : pending_.validator_diffs()) {
        (void)chain_id;
        for (const auto& [node_id, d] : nodes) {
            (void)node_id;
            switch (d.status) {
                case DiffStatus::Added:
                    if (auto s = target.put_pending_validator(*d.validator); !s)
                        return std::unexpected(s.error());
                    break;
                case DiffStatus::Deleted:
                    target.delete_pending_validator(*d.validator);
                    break;
                case DiffStatus::Unmodified:
                    break;
            }
            for (const auto& add : d.added_delegators) target.put_pending_delegator(add);
            for (const auto& [id, del] : d.deleted_delegators) {
                (void)id;
                target.delete_pending_delegator(del);
            }
        }
    }

    for (const auto& id : added_networks_) target.add_network(id);
    for (const auto& [id, tx] : transformations_) {
        (void)id;
        target.add_network_transformation(tx);
    }
    for (const auto& [id, list] : chains_) {
        (void)id;
        for (const auto& tx : list) target.add_chain(tx);
    }
    for (const auto& [id, entry] : added_txs_) {
        (void)id;
        target.add_tx(entry.first, entry.second);
    }
    for (const auto& [tx_id, list] : reward_utxos_)
        for (const auto& u : list) target.add_reward_utxo(tx_id, u);
    for (const auto& [id, u] : added_utxos_) {
        (void)id;
        target.add_utxo(u);
    }
    for (const auto& id : deleted_utxos_) target.delete_utxo(id);
    for (const auto& [id, o] : net_owners_) target.set_network_owner(id, o);
    for (const auto& [id, c] : conversions_) target.set_network_conversion(id, c);
    return ok();
}

// ── when the staker set next changes

std::uint64_t next_staker_change_time(const Chain& chain, std::uint64_t upper) {
    std::uint64_t next = upper;
    const auto current = chain.current_stakers();
    if (!current.empty()) next = std::min(next, current.front().next_time);
    const auto pending = chain.pending_stakers();
    if (!pending.empty()) next = std::min(next, pending.front().next_time);
    return next;
}

// ── the commitment a block carries

namespace {

struct Fold {
    std::vector<std::uint8_t> b;
    void u8(std::uint8_t v) { b.push_back(v); }
    void u64(std::uint64_t v) {
        for (int i = 0; i < 8; ++i) b.push_back(static_cast<std::uint8_t>(v >> (8 * i)));
    }
    void u32(std::uint32_t v) {
        for (int i = 0; i < 4; ++i) b.push_back(static_cast<std::uint8_t>(v >> (8 * i)));
    }
    void bytes(std::span<const std::uint8_t> v) {
        u64(v.size());
        b.insert(b.end(), v.begin(), v.end());
    }
    void owners(const OutputOwners& o) {
        u64(o.locktime);
        u32(o.threshold);
        u64(o.addrs.size());
        for (const auto& a : o.addrs) b.insert(b.end(), a.begin(), a.end());
    }
    void staker(const Staker& s) {
        b.insert(b.end(), s.tx_id.begin(), s.tx_id.end());
        b.insert(b.end(), s.node_id.b.begin(), s.node_id.b.end());
        b.insert(b.end(), s.chain_id.begin(), s.chain_id.end());
        u8(s.public_key ? 1 : 0);
        if (s.public_key) b.insert(b.end(), s.public_key->begin(), s.public_key->end());
        u64(s.weight);
        u64(s.start_time);
        u64(s.end_time);
        u64(s.potential_reward);
        u64(s.next_time);
        u8(static_cast<std::uint8_t>(s.priority));
    }
};

}  // namespace

Id state_root(const Chain& chain) {
    Fold f;
    f.u64(chain.timestamp());
    f.u64(chain.accrued_fees());
    f.u64(chain.fee_state().capacity);
    f.u64(chain.fee_state().excess);
    f.u64(chain.l1_validator_excess());

    const auto nets = chain.networks();
    f.u64(nets.size());
    for (const auto& n : nets) {
        f.b.insert(f.b.end(), n.begin(), n.end());
        if (const auto o = chain.network_owner(n); o) {
            f.u8(1);
            f.owners(o.value());
        } else {
            f.u8(0);
        }
        if (const auto s = chain.current_supply(n); s) {
            f.u8(1);
            f.u64(s.value());
        } else {
            f.u8(0);
        }
    }
    // The primary network's supply is not reachable through networks(): it is
    // the chain itself, not something a transaction created.
    if (const auto s = chain.current_supply(kPrimaryNetworkId); s) {
        f.u8(1);
        f.u64(s.value());
    } else {
        f.u8(0);
    }

    const auto current = chain.current_stakers();
    f.u64(current.size());
    for (const auto& s : current) f.staker(s);

    const auto pending = chain.pending_stakers();
    f.u64(pending.size());
    for (const auto& s : pending) f.staker(s);

    // L1 validators and the expiries that gate their registration are state a
    // block changed, so the commitment covers them too.
    const auto l1s = chain.active_l1_validators();
    f.u64(l1s.size());
    for (const auto& v : l1s) {
        f.b.insert(f.b.end(), v.validation_id.begin(), v.validation_id.end());
        f.b.insert(f.b.end(), v.chain_id.begin(), v.chain_id.end());
        f.b.insert(f.b.end(), v.node_id.b.begin(), v.node_id.b.end());
        f.bytes(v.public_key);
        f.bytes(v.remaining_balance_owner);
        f.bytes(v.deactivation_owner);
        f.u64(v.start_time);
        f.u64(v.weight);
        f.u64(v.min_nonce);
        f.u64(v.end_accumulated_fee);
    }
    const auto exp = chain.expiries();
    f.u64(exp.size());
    for (const auto& e : exp) {
        f.u64(e.timestamp);
        f.b.insert(f.b.end(), e.validation_id.begin(), e.validation_id.end());
    }

    const auto utxos = chain.utxos();
    f.u64(utxos.size());
    for (const auto& u : utxos) {
        const Id id = u.id();
        f.b.insert(f.b.end(), id.begin(), id.end());
        f.b.insert(f.b.end(), u.asset.begin(), u.asset.end());
        f.u64(u.stake_lock);
        f.u64(u.out.amt);
        f.owners(u.out.owners);
    }
    return sha256(f.b);
}

}  // namespace lux::platformvm::state
