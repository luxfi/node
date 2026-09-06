// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/vm.hpp"

#include <algorithm>

namespace lux::dexvm {

Block::Block(VM& vm, BlockBody body, Bytes bytes)
    : vm_(vm), body_(std::move(body)), bytes_(std::move(bytes)), id_(sha256(view(bytes_))) {}

node::Id Block::id() const { return id_; }
node::Id Block::parent() const { return body_.parent; }
std::uint64_t Block::height() const { return body_.height; }
std::span<const std::uint8_t> Block::bytes() const { return view(bytes_); }

void Block::ensure_executed() const {
    if (executed_) return;
    executed_ = true;

    // Position first. A block that does not sit on what this node last accepted
    // is not a block this node can execute: its parent's effects are not in the
    // set, so applying it would produce a result no one else would reach.
    if (body_.parent != vm_.state_->last_accepted()) {
        refusal_ = "dexvm: block " + cb58(id_) + " builds on " + cb58(body_.parent) +
                   " but this node last accepted " + cb58(vm_.state_->last_accepted());
        return;
    }
    if (body_.height != vm_.state_->last_accepted_height() + 1) {
        refusal_ = "dexvm: block height " + std::to_string(body_.height) + " does not follow " +
                   std::to_string(vm_.state_->last_accepted_height());
        return;
    }

    auto diff = vm_.state_->execute(body_, *vm_.verifier_);
    if (!diff) {
        refusal_ = diff.error().text;
        return;
    }
    root_ = diff->root;
    verified_ = true;
    diff_ = std::move(*diff);
}

node::Id Block::root() const {
    ensure_executed();
    // A block this node's own execution refused has no state to name. Returning
    // the empty id says exactly that, rather than naming a root nothing produced.
    return verified_ ? root_ : kEmptyId;
}

bool Block::verify() {
    ensure_executed();
    // False is a refusal to vote, never an error: an honest node does not vote
    // for a block its own execution rejects.
    return verified_;
}

void Block::accept() {
    if (decided_) return;
    ensure_executed();
    if (!verified_ || !diff_) return;  // never accept what this node's execution refused
    if (auto r = vm_.state_->commit(body_, std::move(*diff_)); !r) {
        // The store could not make it durable. Saying accepted here would let a
        // restart re-sign the height, so the decision does not stand.
        refusal_ = r.error().text;
        verified_ = false;
        diff_.reset();
        return;
    }
    decided_ = true;
    diff_.reset();
    vm_.drop_from_pool(body_.txs);
    vm_.preference_ = id_;
}

void Block::reject() {
    if (decided_) return;
    decided_ = true;
    // The transactions were never refused — they lost a race — so they go back
    // to whatever the next build draws from, and the state this execution pinned
    // is released.
    diff_.reset();
    vm_.return_to_pool(body_.txs);
}

// ---- the chain ------------------------------------------------------------

Result<std::unique_ptr<VM>> VM::make(const Id& chain_id, store::Store& s,
                                     NetworkClass network_class, DexAssetPolicy policy,
                                     std::shared_ptr<ChainVerifier> verifier,
                                     std::function<std::string(const Id&)> chain_label_for) {
    if (!verifier)
        return fail("dexvm: a chain needs a ChainVerifier (refusing to admit unverified assets)");
    std::unique_ptr<VM> vm(new VM(chain_id, std::move(verifier)));
    auto st = State::load(s, network_class, policy, std::move(chain_label_for));
    if (!st) return std::unexpected(st.error());
    vm->state_ = std::move(*st);
    vm->preference_ = vm->state_->last_accepted();
    return vm;
}

void VM::issue(Tx tx) { pool_.push_back(std::move(tx)); }

void VM::drop_from_pool(const std::vector<Tx>& txs) {
    for (const Tx& t : txs) {
        const Id id = t.id();
        auto it = std::find_if(pool_.begin(), pool_.end(),
                               [&](const Tx& p) { return p.id() == id; });
        if (it != pool_.end()) pool_.erase(it);
    }
}

void VM::return_to_pool(const std::vector<Tx>& txs) {
    for (auto it = txs.rbegin(); it != txs.rend(); ++it) {
        const Id id = it->id();
        const bool already = std::any_of(pool_.begin(), pool_.end(),
                                         [&](const Tx& p) { return p.id() == id; });
        if (!already) pool_.push_front(*it);
    }
}

std::shared_ptr<node::Block> VM::build() {
    if (pool_.empty()) return nullptr;

    // Each candidate is tried against the set the ones before it produced, so a
    // transaction that cannot apply is left in the pool rather than making the
    // whole block unbuildable. That is also why a market may follow the two
    // assets it names in the same block.
    BlockBody body;
    body.parent = state_->last_accepted();
    body.height = state_->last_accepted_height() + 1;
    for (const Tx& t : pool_) {
        BlockBody trial = body;
        trial.txs.push_back(t);
        if (state_->execute(trial, *verifier_)) body = std::move(trial);
    }
    if (body.txs.empty()) return nullptr;  // nothing to build is "no", not a failure

    Bytes bytes = body.encode();
    auto blk = std::make_shared<Block>(*this, std::move(body), std::move(bytes));
    blocks_[blk->id()] = blk;
    return blk;
}

std::shared_ptr<node::Block> VM::parse(std::span<const std::uint8_t> b) {
    auto body = decode_block(b);
    if (!body) return nullptr;  // not a block is "no"
    auto blk = std::make_shared<Block>(*this, std::move(*body), to_bytes(b));
    // Re-encoding must reproduce the bytes exactly: a block whose encoding is
    // not canonical would hash to an id its own contents do not produce, and two
    // nodes would name the same block differently.
    if (blk->body().encode() != to_bytes(b)) return nullptr;
    blocks_[blk->id()] = blk;
    return blk;
}

std::shared_ptr<node::Block> VM::get(const node::Id& id) const {
    auto it = blocks_.find(id);
    return it == blocks_.end() ? nullptr : it->second;  // an unknown id is "no"
}

void VM::prefer(const node::Id& id) { preference_ = id; }

}  // namespace lux::dexvm
