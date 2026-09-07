// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/block.hpp"

#include "lux/quantumvm/vm.hpp"

#include <shared_mutex>
#include <string>

namespace lux::quantumvm {

ByteView Block::bytes() const {
    if (!wire_built_) {
        wire_ = wire::block_bytes(f_);
        wire_built_ = true;
    }
    return view(wire_);
}

const Id& Block::id() const {
    if (!id_) id_ = of(bytes());
    return *id_;
}

Id Block::execution_root() const {
    // The rows accepting this block writes, in the order commit_block writes
    // them. It is a pure function of the block, so it can be computed before
    // the block is accepted — which is what a validator needs, since this is
    // what it signs — and it is arithmetic over the block's own content rather
    // than a number a proposer supplied.
    // SHA-256 of the concatenation, which is what a run of updates was.
    const Id block_id = id();
    const Bytes hk = height_key(f_.height);
    Bytes pre;
    pre.reserve(block_id.size() * 2 + hk.size() + 8);
    pre.insert(pre.end(), block_id.begin(), block_id.end());  // the block, stored under its id
    pre.insert(pre.end(), hk.begin(), hk.end());              // its height index entry
    pre.insert(pre.end(), block_id.begin(), block_id.end());  // the tip the chain moves to
    for (int i = 0; i < 8; ++i)                               // and the height it moves to
        pre.push_back(static_cast<std::uint8_t>(f_.height >> (56 - 8 * i)));
    return sha256(view(pre));
}

Status Block::on_this_chain() const {
    if (f_.chain_id != vm_->chain_id() || f_.network_id != vm_->network_id())
        return fail(Err::ForeignChain,
                    "chain " + text(f_.chain_id) + " network " + std::to_string(f_.network_id) +
                        ", this node serves chain " + text(vm_->chain_id()) + " network " +
                        std::to_string(vm_->network_id()));
    return ok();
}

Status Block::apply() const {
    for (const auto& tx : f_.transactions) {
        if (auto r = tx->execute(); !r)
            return fail(Err::Execute, text(tx->id()) + " in block " + text(id()) + ": " +
                                          r.error().message());
    }
    return ok();
}

Status Block::verify() const {
    std::shared_lock guard(vm_->lock());

    if (auto r = on_this_chain(); !r) return r;

    // Genesis is written by the VM, never proposed.
    if (f_.height == 0) return fail(Err::InvalidBlockHeight, "height 0 is written, not proposed");

    // A proposed block carries work. Refusing an empty one is also what keeps
    // the signature check below from being satisfiable by removing its subject.
    if (f_.transactions.empty()) return fail(Err::EmptyBlock);

    if (bytes().size() > wire::kMaxBlockSize)
        return fail(Err::BlockTooLarge, std::to_string(bytes().size()) + " bytes over " +
                                            std::to_string(wire::kMaxBlockSize));

    // block_at, not block(): block() takes the same read lock this function
    // already holds, and a writer arriving between the two acquisitions blocks
    // the second one forever — a pending writer queues ahead of later readers,
    // so a recursive read lock is a deadlock, not a no-op.
    auto parent = vm_->block_at(f_.parent_id);
    if (!parent) return fail(Err::InvalidParentID, text(f_.parent_id));

    if (f_.height != (*parent)->height() + 1)
        return fail(Err::InvalidBlockHeight,
                    std::to_string(f_.height) + " does not follow parent " +
                        std::to_string((*parent)->height()));
    if (f_.timestamp < (*parent)->timestamp())
        return fail(Err::TimeBeforeParent, std::to_string(f_.timestamp) + " precedes " +
                                               std::to_string((*parent)->timestamp()));
    if (f_.timestamp > vm_->clock().seconds() + kMaxFutureSkewSeconds)
        return fail(Err::TimeTooFarAhead,
                    std::to_string(f_.timestamp) + " exceeds now+" +
                        std::to_string(kMaxFutureSkewSeconds) + "s");

    if (vm_->configuration().quantum_stamp_enabled) {
        std::vector<Bytes> msgs(f_.transactions.size());
        std::vector<const quantum::QuantumSignature*> sigs(f_.transactions.size());
        for (std::size_t i = 0; i < f_.transactions.size(); ++i) {
            const ByteView b = f_.transactions[i]->bytes();
            msgs[i].assign(b.begin(), b.end());
            sigs[i] = f_.transactions[i]->signature();
        }
        if (auto r = vm_->signer().parallel_verify(msgs, sigs); !r)
            return fail(Err::BlockVerificationFailed, r.error().message());
    }

    return ok();
}

Status Block::accept() {
    std::unique_lock guard(vm_->lock());

    // Nothing is dropped from the mempool until the commit succeeds. Evicting
    // first would lose the transactions of a block that then failed to persist.
    if (auto r = vm_->commit_block(*this); !r) return r;

    for (const auto& tx : f_.transactions) {
        // A block's transactions are settled by its commit; one that is no
        // longer in the pool is not a reason to fail a block that is already
        // durable.
        (void)vm_->pool().remove(tx->id());
    }
    return ok();
}

Status Block::reject() const {
    // Nothing ran — execution belongs to accept — and the transactions are
    // still in the mempool, because building copies from the queue rather than
    // draining it and only accept removes anything. So there is nothing to undo
    // and nothing to give back.
    return ok();
}

std::uint8_t Block::status() const {
    std::shared_lock guard(vm_->lock());
    const auto held = vm_->state().has(view(id()));
    return (held && *held) ? 1 : 0;
}

}  // namespace lux::quantumvm
