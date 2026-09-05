// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/block.hpp"

#include "lux/fhevm/vm.hpp"

namespace lux::fhevm {

Block::Block(VM* vm, Id parent, std::uint64_t height, std::int64_t timestamp,
             std::vector<Transaction> txs)
    : vm_(vm),
      parent_(parent),
      height_(height),
      timestamp_(timestamp),
      transactions_(std::move(txs)) {}

// compute_id names the block by its content AND by the chain it belongs to. The
// chain id does not travel — a peer supplies its own — so the same bytes name
// different blocks on different chains, and chain B cannot resolve chain A's
// parent. Without it every F-Chain with the same genesis timestamp shared one
// genesis id, and a block built on one was accepted verbatim by the others.
Id Block::compute_id() const {
    Hasher h;
    Id chain = vm_->chain_id();
    h.write(view(chain));
    h.write(view(parent_));
    h.be64(height_);
    h.be64(std::uint64_t(timestamp_));
    for (const auto& tx : transactions_) {
        Id id = tx.id();
        h.write(view(id));
    }
    return h.sum();
}

lux::node::Id Block::id() const {
    if (!id_cached_) {
        id_ = compute_id();
        id_cached_ = true;
    }
    return id_;
}

std::span<const std::uint8_t> Block::bytes() const {
    if (!bytes_cached_) {
        bytes_ = block_bytes(parent_, height_, timestamp_, transactions_);
        bytes_cached_ = true;
    }
    return view(bytes_);
}

std::uint8_t Block::status() const {
    if (Id(id()) == Id(vm_->last_accepted())) return 1;
    auto stored = vm_->store()->has(view(key(kBlockPrefix, view(Id(id())))));
    if (stored && *stored) return 1;
    return 0;
}

// check is verify with its reason. It proves the block can be accepted WITHOUT
// mutating state: it sits correctly on its parent in height and time, and every
// transaction is well-formed, authenticated, correctly ordered and paid for. A
// block that fails any check is never accepted (fail closed); verifying a block
// never moves funds.
Result<void> Block::check() const {
    if (height_ == 0) return fail(Err::InvalidBlock, "genesis is not a proposed block");
    auto parent = vm_->get_block(parent_);
    if (!parent) return fail(Err::InvalidBlock, "verify parent: " + parent.error().message());
    // Chain time and height are the parent's, advanced. Without this a proposer
    // picks both freely: it can rewind time to revive an expired permit, or jump
    // forward to expire everything at once.
    if (height_ != (*parent)->height() + 1) {
        return fail(Err::InvalidBlock, "height does not follow the parent");
    }
    if (timestamp_ < (*parent)->timestamp()) {
        return fail(Err::InvalidBlock, "timestamp precedes the parent");
    }
    if (timestamp_ > vm_->clock().now() + kMaxFutureSkew) {
        return fail(Err::InvalidBlock, "timestamp is beyond the skew allowance");
    }
    if (transactions_.empty()) return fail(Err::InvalidBlock, "empty block");
    if (transactions_.size() > kMaxBlockTxs) {
        return fail(Err::InvalidBlock, "transaction count exceeds the block bound");
    }
    // A block this node built in memory is held to the same size a block off
    // the wire is held to, so a proposer cannot produce one its own peers
    // refuse to parse.
    if (bytes().size() > kMaxBlockSize) {
        return fail(Err::InvalidBlock, "block exceeds the wire bound");
    }

    // The parent must be one this chain can still build on: the accepted tip,
    // or a block verified above it and not yet decided.
    auto tip = vm_->on_tip(parent_);
    if (!tip) return tip;

    Batch batch(*vm_);
    for (const auto& tx : transactions_) {
        auto ok = batch.admit(tx);
        if (!ok) return ok;
    }
    return {};
}

bool Block::verify() {
    auto ok = check();
    if (!ok) {
        error_ = ok.error().message();
        return false;
    }
    error_.clear();
    // A block that verifies is one the engine may build on, so it has to be
    // findable by id — including one parsed from a peer rather than built here.
    // Tracking only self-built blocks meant a follower verified b1 and then
    // failed b2 with "verify parent: not found", which is the ordinary shape
    // whenever more than one block is in flight.
    //
    vm_->track_verified(shared_from_this());
    return true;
}

// accept_block settles and applies the block atomically. For each transaction
// it METERS the operation's gas, BURNS the fee from the payer, then APPLIES the
// state effect — all written through the VM's store, which is committed exactly
// once. Any failure aborts the whole block (no partial application, no unpaid
// operation): the store is rolled back and the caches are reloaded from the
// unchanged base.
Result<void> Block::accept_block() {
    const std::int64_t now = timestamp_;

    // A block extends the tip or it is not accepted. verify reached the same
    // verdict earlier, against the tip AT THAT TIME; between the two the chain
    // moves, and it is this check that decides. Without it acceptance writes
    // the height index, the last-accepted pointer and the height for a block on
    // an abandoned branch — the chain rewinds, and every peer bootstrapping
    // from the index is served an orphan as canonical.
    if (parent_ != Id(vm_->last_accepted())) {
        return fail(Err::NotOnTip, "parent is not the accepted tip");
    }

    auto settled = settle_and_apply(now);
    if (!settled) {
        abort();
        return settled;
    }

    // The block, the height index and the last-accepted pointer land in the
    // same commit as the effects they describe.
    Id self = Id(id());
    auto w = vm_->store()->put(view(key(kBlockPrefix, view(self))), bytes());
    if (!w) {
        abort();
        return w;
    }
    w = vm_->store()->put(view(key(kHeightPrefix, height_)), view(self));
    if (!w) {
        abort();
        return w;
    }
    w = vm_->store()->put(view(kLastAcceptedKey), view(self));
    if (!w) {
        abort();
        return w;
    }
    w = vm_->store()->commit();
    if (!w) {
        abort();
        return w;
    }

    vm_->set_accepted(shared_from_this());
    vm_->drop_pending(self);
    vm_->release(transactions_);
    return {};
}

void Block::accept() {
    auto ok = accept_block();
    if (!ok) error_ = ok.error().message();
}

// reject discards the block. Its transactions were never removed from the
// mempool — the builder SELECTS from the mempool rather than draining it — so
// there is nothing to give back and nothing that can be lost by an engine that
// drops a block without rejecting it.
void Block::reject() { vm_->drop_pending(Id(id())); }

void Block::abort() {
    vm_->store()->abort();
    auto reloaded = vm_->load_state();
    if (!reloaded) error_ = reloaded.error().message();
}

// settle_and_apply burns each transaction's fee and applies its effect.
//
// A transaction that fails AUTHORIZATION here REVERTS rather than aborting the
// block: its fee is burned, its nonce is consumed, and its state effect does
// not happen. Every validator reaches that verdict from the same committed
// state in the same order, so a reverted transaction is not a disagreement —
// and the alternative, aborting, would mean a block every validator certified
// and no validator could apply, which halts the chain at that height. The payer
// pays for the block space it used either way, so a revert is not free.
//
// The error return is therefore reserved for what a validator genuinely cannot
// proceed past: a failed write, a failed burn, a nonce verify should have
// caught. Those abort the block and roll it back whole.
Result<void> Block::settle_and_apply(std::int64_t now) {
    for (const auto& tx : transactions_) {
        // Replay/order guard: the nonce must be exactly the payer's next. It
        // reads through the store, so earlier transactions in this same block
        // are seen.
        auto committed = vm_->nonce_of(tx.payer);
        if (!committed) return std::unexpected(committed.error());
        if (tx.nonce != *committed + 1) return fail(Err::BadNonce);

        auto gas_used = gas_for(tx);
        if (!gas_used) return std::unexpected(gas_used.error());
        // Meter the operation against the payer's declared gas limit.
        fee::GasMeter meter(tx.gas_limit);
        auto consumed = meter.consume(*gas_used);
        if (!consumed) return consumed;
        auto amount = fee::cost(meter.used(), kGasPrice);
        if (!amount) return std::unexpected(amount.error());
        // Debit and burn the fee from the payer's balance.
        auto charged = fee::charge(vm_->ledger(), tx.payer, *amount);
        if (!charged) return charged;
        // Apply the operation's state effect (atomically with the burn) — or,
        // if authorization refuses it, leave state untouched. apply reports
        // which.
        auto applied = apply(tx, *vm_, now);
        if (!applied) return std::unexpected(applied.error());
        // Advance the payer's nonce (atomically with the burn and the effect).
        auto n = vm_->set_nonce(tx.payer, tx.nonce);
        if (!n) return n;
    }
    return {};
}

}  // namespace lux::fhevm
