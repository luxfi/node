// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/vm.hpp"

#include <algorithm>

namespace lux::xvm {

Vm::Vm(VmConfig config, std::vector<executor::ParsedFx> fxs, store::Store& store)
    : config_(std::move(config)), state_(store), gossip_(pool_, *this) {
    backend_.config.tx_fee = config_.tx_fee;
    backend_.config.create_asset_tx_fee = config_.create_asset_tx_fee;
    backend_.fee_asset_id = config_.fee_asset_id;
    backend_.network_id = config_.network_id;
    backend_.chain_id = config_.chain_id;
    backend_.net_id = config_.net_id;
    backend_.fxs = std::move(fxs);
    // An fx's POSITION in the list is the index a CreateAssetTx declares, so the
    // index is derived from the list rather than configured beside it.
    for (std::size_t i = 0; i < backend_.fxs.size(); ++i) {
        auto& parsed = backend_.fxs[i];
        if (parsed.fx == nullptr) continue;
        parsed.fx->clock = &clock_;
        if (dynamic_cast<fx::Secp256k1Fx*>(parsed.fx.get()) != nullptr)
            backend_.fx_index.set(wire::TypeKind::Secp256k1, int(i));
        else if (dynamic_cast<fx::NFTFx*>(parsed.fx.get()) != nullptr)
            backend_.fx_index.set(wire::TypeKind::NFT, int(i));
        else if (dynamic_cast<fx::PropertyFx*>(parsed.fx.get()) != nullptr)
            backend_.fx_index.set(wire::TypeKind::Property, int(i));
    }
}

void Vm::set_bootstrapped(bool v) {
    backend_.bootstrapped = v;
    for (auto& parsed : backend_.fxs) {
        if (parsed.fx == nullptr) continue;
        if (v) {
            parsed.fx->bootstrapped();
        } else {
            parsed.fx->bootstrapping();
        }
    }
}

void Vm::set_now(std::uint64_t unix_seconds) {
    now_ = unix_seconds;
    clock_.unix_time = unix_seconds;
}

wire::Result<void> Vm::initialize(std::vector<std::shared_ptr<txs::Tx>> genesis_txs,
                                  std::uint64_t genesis_time) {
    // Whatever the store already holds IS this chain, so it is read first. A
    // node that installed genesis on top of it would rewrite history at every
    // restart.
    if (auto r = state_.load(); !r) return std::unexpected(r.error());

    if (state_.initialized()) {
        last_accepted_ = state_.get_last_accepted();
        preferred_ = last_accepted_;
        // The last accepted block must actually be there; a store that names a
        // block it does not hold is a store this node cannot boot from, and
        // saying so is better than signing from an empty chain.
        if (auto blk = state_.get_block(last_accepted_); !blk)
            return std::unexpected("last accepted block " + hex(last_accepted_) +
                                   " is not in the store: " + blk.error());
        return {};
    }

    for (const auto& tx : genesis_txs) {
        if (tx == nullptr) return std::unexpected("nil genesis tx");
        state_.add_tx(tx);
        for (const auto& utxo : tx->utxos()) state_.add_utxo(utxo);
    }
    state_.set_timestamp(genesis_time);

    auto blk = block::build(kEmptyId, 0, genesis_time, kEmptyId, std::move(genesis_txs));
    if (!blk) return std::unexpected(blk.error());
    state_.add_block(*blk);
    state_.set_last_accepted((*blk)->block_id);
    state_.set_initialized();
    last_accepted_ = (*blk)->block_id;
    preferred_ = last_accepted_;
    // Genesis is durable before this returns: the height it closes is the one
    // every later block is measured from.
    if (auto r = state_.commit(); !r) return std::unexpected(r.error());
    return {};
}

wire::Result<void> Vm::initialize_from_genesis(ByteView genesis_bytes,
                                               std::uint64_t genesis_time) {
    auto g = genesis::parse(genesis_bytes);
    if (!g) return std::unexpected(g.error());
    if (g->assets.empty()) return std::unexpected(genesis::kErrNoAssets);

    auto tx_list = genesis::as_txs(*g);
    if (!tx_list) return std::unexpected(tx_list.error());

    for (std::size_t i = 0; i < g->assets.size(); ++i) {
        aliases_[g->assets[i].alias] = (*tx_list)[i]->id();
    }
    // The FIRST asset is the fee asset. Reading it from position rather than
    // from a field is what keeps the buffer from carrying two answers.
    config_.fee_asset_id = (*tx_list)[0]->id();
    backend_.fee_asset_id = config_.fee_asset_id;

    return initialize(std::move(*tx_list), genesis_time);
}

wire::Result<Id> Vm::alias_of(const std::string& alias) const {
    auto it = aliases_.find(alias);
    if (it == aliases_.end()) return std::unexpected("unknown asset alias " + alias);
    return it->second;
}

state::Chain* Vm::get_state(const Id& blk_id) {
    if (blk_id == last_accepted_) return &state_;
    auto it = pending_.find(blk_id);
    if (it == pending_.end()) return nullptr;
    return it->second.on_accept.get();
}

std::uint64_t Vm::last_accepted_height() const {
    auto blk = state_.get_block(last_accepted_);
    if (!blk) return 0;
    return (*blk)->height;
}

wire::Result<std::shared_ptr<block::StandardBlock>> Vm::stateless_block(const Id& blk_id) const {
    auto it = pending_.find(blk_id);
    if (it != pending_.end()) return it->second.blk;
    return state_.get_block(blk_id);
}

wire::Result<void> Vm::verify_tx(txs::Tx& tx) {
    // A node still replaying history has no opinion worth having: its state is
    // behind, so every verdict it reached would be about a chain that no longer
    // exists. Go refuses outright rather than answering from a stale state.
    if (!backend_.bootstrapped) return std::unexpected(kErrChainNotSynced);

    executor::SyntacticVerifier syn(backend_, tx);
    if (auto r = tx.unsigned_tx->visit(syn); !r) return std::unexpected(r.error());

    // Against the LAST ACCEPTED state, which is Go's choice and not an
    // arbitrary one: two nodes preferring different tips would otherwise admit
    // different transactions, and a node's mempool would depend on which block
    // it happened to be looking at.
    auto diff = state::Diff::create(last_accepted_, *this);
    if (!diff) return std::unexpected(diff.error());

    executor::SemanticVerifier sem(backend_, **diff, tx);
    if (auto r = tx.unsigned_tx->visit(sem); !r) return std::unexpected(r.error());

    executor::Executor exec(**diff, tx);
    return tx.unsigned_tx->visit(exec);
}

wire::Result<void> Vm::issue(std::shared_ptr<txs::Tx> tx) {
    return gossip_.add(std::move(tx));
}

std::shared_ptr<lux::node::Block> Vm::build() {
    last_error_.clear();
    if (pool_.len() == 0) {
        // Nothing to build is "no", not a failure — the house form for the whole
        // seam.
        last_error_ = kErrEmptyBlock;
        return nullptr;
    }

    auto parent = stateless_block(preferred_);
    if (!parent) {
        last_error_ = parent.error();
        return nullptr;
    }

    // The block's timestamp never moves backwards, and never runs ahead of this
    // node's clock.
    state::Chain* parent_state = get_state(preferred_);
    if (parent_state == nullptr) {
        last_error_ = state::kErrMissingParentState;
        return nullptr;
    }
    std::uint64_t timestamp = std::max(now_, parent_state->get_timestamp());

    // Execute the mempool against a diff to learn the root BEFORE the block is
    // sealed — the root is a result, so it cannot be filled in afterwards.
    auto diff = state::Diff::create(preferred_, *this);
    if (!diff) {
        last_error_ = diff.error();
        return nullptr;
    }
    (*diff)->set_timestamp(timestamp);

    std::vector<std::shared_ptr<txs::Tx>> included;
    std::set<Id> imported;
    std::size_t remaining = kTargetBlockSize;
    while (true) {
        auto tx = pool_.peek();
        if (tx == nullptr || tx->size() > remaining) break;
        // Taken out of the pool BEFORE it is tried: a transaction that fails
        // here is finished, and one that succeeds is in the block. Either way
        // the next round must not meet it again at the head of the queue.
        pool_.remove(tx);

        // Its own diff, so a transaction that fails halfway leaves nothing
        // behind in the block's. Go: state.NewDiffOn.
        auto tx_diff = state::Diff::on(**diff);

        executor::SemanticVerifier sem(backend_, *tx_diff, *tx);
        if (auto r = tx->unsigned_tx->visit(sem); !r) {
            pool_.mark_dropped(tx->id(), r.error());
            continue;
        }
        executor::Executor exec(*tx_diff, *tx);
        if (auto r = tx->unsigned_tx->visit(exec); !r) {
            pool_.mark_dropped(tx->id(), r.error());
            continue;
        }

        bool conflicts = false;
        for (const auto& in : exec.inputs) {
            if (imported.count(in) != 0) conflicts = true;
        }
        if (conflicts) {
            pool_.mark_dropped(tx->id(), kErrConflictingBlockTxs);
            continue;
        }
        if (auto r = verify_unique_inputs(preferred_, exec.inputs); !r) {
            pool_.mark_dropped(tx->id(), r.error());
            continue;
        }
        for (const auto& in : exec.inputs) imported.insert(in);

        tx_diff->add_tx(tx);
        tx_diff->apply(**diff);

        remaining -= tx->size();
        included.push_back(tx);
    }
    if (included.empty()) {
        last_error_ = kErrEmptyBlock;
        return nullptr;
    }

    const std::uint64_t height = (*parent)->height + 1;
    auto root = root::block_execution_root((*parent)->root, included, **diff, height);
    if (!root) {
        last_error_ = root.error();
        return nullptr;
    }

    auto blk = block::build((*parent)->block_id, height, timestamp, *root, included);
    if (!blk) {
        last_error_ = blk.error();
        return nullptr;
    }

    // The block this node just built still goes through the same verification
    // every other block does. One path, so a proposer cannot accept something a
    // follower would refuse.
    if (auto r = verify_block(*blk); !r) {
        last_error_ = r.error();
        return nullptr;
    }
    return std::make_shared<VmBlock>(*this, *blk);
}

std::shared_ptr<lux::node::Block> Vm::parse(std::span<const std::uint8_t> bytes) {
    last_error_.clear();
    auto blk = block::parse(bytes);
    if (!blk) {
        last_error_ = blk.error();
        return nullptr;
    }
    return std::make_shared<VmBlock>(*this, *blk);
}

std::shared_ptr<lux::node::Block> Vm::get(const lux::node::Id& blk_id) const {
    auto it = pending_.find(blk_id);
    if (it != pending_.end())
        return std::make_shared<VmBlock>(const_cast<Vm&>(*this), it->second.blk);
    auto blk = state_.get_block(blk_id);
    if (!blk) return nullptr;
    return std::make_shared<VmBlock>(const_cast<Vm&>(*this), *blk);
}

void Vm::prefer(const lux::node::Id& blk_id) { preferred_ = blk_id; }

wire::Result<void> Vm::verify_unique_inputs(const Id& blk_id, const std::set<Id>& inputs) const {
    if (inputs.empty()) return {};
    // Walk the pinned ancestry. A block whose state is not pinned is already
    // accepted, and an accepted ancestor's imported inputs are already gone from
    // shared memory — so the walk stops there rather than continuing forever.
    Id cursor = blk_id;
    while (true) {
        auto it = pending_.find(cursor);
        if (it == pending_.end()) return {};
        for (const auto& in : it->second.imported_inputs) {
            if (inputs.count(in) != 0) return std::unexpected(kErrConflictingParentTxs);
        }
        cursor = it->second.blk->parent_id;
    }
}

wire::Result<void> Vm::verify_block(const std::shared_ptr<block::StandardBlock>& blk) {
    const Id blk_id = blk->block_id;
    if (pending_.count(blk_id) != 0) return {};  // already verified

    if (blk->time > now_ + kSyncBoundSeconds)
        return std::unexpected(kErrTimestampBeyondSyncBound);
    if (blk->transactions.empty()) return std::unexpected(kErrEmptyBlock);

    for (const auto& tx : blk->transactions) {
        executor::SyntacticVerifier syn(backend_, *tx);
        if (auto r = tx->unsigned_tx->visit(syn); !r) {
            pool_.mark_dropped(tx->id(), r.error());
            return std::unexpected("failed to syntactically verify tx " + hex(tx->id()) + ": " +
                                   r.error());
        }
    }

    auto parent = stateless_block(blk->parent_id);
    if (!parent)
        return std::unexpected("failed to get parent " + hex(blk->parent_id) + ": " +
                               parent.error());
    if ((*parent)->height + 1 != blk->height) return std::unexpected(kErrIncorrectHeight);

    auto diff = state::Diff::create(blk->parent_id, *this);
    if (!diff)
        return std::unexpected("failed to initialize state diff on state at " +
                               hex(blk->parent_id) + ": " + diff.error());

    if (blk->time < (*diff)->get_timestamp())
        return std::unexpected(kErrChildBlockEarlierThanParent);
    (*diff)->set_timestamp(blk->time);

    Pending pending;
    pending.blk = blk;

    for (const auto& tx : blk->transactions) {
        executor::SemanticVerifier sem(backend_, **diff, *tx);
        if (auto r = tx->unsigned_tx->visit(sem); !r) {
            pool_.mark_dropped(tx->id(), r.error());
            return std::unexpected("failed to semantically verify tx " + hex(tx->id()) + ": " +
                                   r.error());
        }

        executor::Executor exec(**diff, *tx);
        if (auto r = tx->unsigned_tx->visit(exec); !r) {
            pool_.mark_dropped(tx->id(), r.error());
            return std::unexpected("failed to execute tx " + hex(tx->id()) + ": " + r.error());
        }

        for (const auto& in : exec.inputs) {
            if (pending.imported_inputs.count(in) != 0) {
                pool_.mark_dropped(tx->id(), kErrConflictingBlockTxs);
                return std::unexpected(kErrConflictingBlockTxs);
            }
        }
        for (const auto& in : exec.inputs) pending.imported_inputs.insert(in);

        (*diff)->add_tx(tx);

        for (auto& [chain_id, reqs] : exec.atomic_requests) {
            auto& dst = pending.atomic_requests[chain_id];
            dst.put_requests.insert(dst.put_requests.end(), reqs.put_requests.begin(),
                                    reqs.put_requests.end());
            dst.remove_requests.insert(dst.remove_requests.end(), reqs.remove_requests.begin(),
                                       reqs.remove_requests.end());
        }
    }

    if (auto r = verify_unique_inputs(blk->parent_id, pending.imported_inputs); !r)
        return std::unexpected("failed to verify unique inputs on state at " +
                               hex(blk->parent_id) + ": " + r.error());

    // THE ROOT IS RECOMPUTED, never read from the proposer. A block whose
    // declared root disagrees with this node's execution is refused here, which
    // is what turns a quorum certificate into agreement about a result.
    auto expected =
        root::block_execution_root((*parent)->root, blk->transactions, **diff, blk->height);
    if (!expected)
        return std::unexpected("failed to compute expected block execution root: " +
                               expected.error());
    if (blk->root != *expected)
        return std::unexpected(std::string(kErrUnexpectedMerkleRoot) + ": block root " +
                               hex(blk->root) + ", expected " + hex(*expected));

    (*diff)->set_last_accepted(blk_id);
    (*diff)->add_block(blk);

    pending.on_accept = std::move(*diff);
    pending_[blk_id] = std::move(pending);

    // Everything in the block leaves the mempool: it is either in the chain or
    // it conflicts with something that is. `remove` takes the conflicts too,
    // which is why it is one call rather than an erase per id.
    pool_.remove(blk->transactions);
    return {};
}

void Vm::accept_block(const Id& blk_id) {
    auto it = pending_.find(blk_id);
    if (it == pending_.end()) {
        last_error_ = kErrBlockNotFound;
        return;
    }
    pool_.remove(it->second.blk->transactions);
    it->second.on_accept->apply(state_);
    last_accepted_ = blk_id;
    preferred_ = blk_id;

    // Only THIS block's pinned state is freed. A sibling is freed when consensus
    // rejects it, and rejecting is what returns its transactions — dropping it
    // here instead would silently discard them. Go frees exactly one block too
    // (manager.free).
    pending_.erase(it);

    // Durable before this returns. The seam says so: last_accepted() must
    // report this block after a restart, and that is what closes the height to
    // a second signature.
    if (auto r = state_.commit(); !r) last_error_ = r.error();
}

void Vm::reject_block(const Id& blk_id) {
    last_error_.clear();
    auto blk = stateless_block(blk_id);
    if (!blk) {
        last_error_ = blk.error();
        return;
    }
    // The block's pinned state goes first — it can never be accepted now, and a
    // later block must not be able to build on it.
    pending_.erase(blk_id);

    for (const auto& tx : (*blk)->transactions) {
        // Losing a race is not the same as being wrong. Each transaction is
        // asked again, against the state that actually won; the ones that still
        // hold go back into the pool, and the ones that no longer do are simply
        // let go.
        //
        // NOTHING IS MARKED DROPPED HERE, and that is Go's behaviour rather than
        // an omission. A drop reason is a CACHED refusal: while it is
        // remembered, the same transaction offered again is refused from the
        // cache without being re-verified. A transaction invalidated by a
        // reorganisation is exactly the kind that can become valid again, so
        // caching a refusal for it would have this node refuse what Go admits —
        // the same divergence in the pending set that dropping the block's
        // transactions altogether would cause, only quieter. Go's
        // block/executor.Block.Reject logs both failures and remembers neither;
        // MarkDropped belongs to the admission gate (gossipMempool.Add) and to
        // the builder, where a refusal really is about the transaction.
        if (auto r = verify_tx(*tx); !r) continue;
        (void)pool_.add(tx);
    }
}

bool VmBlock::verify() {
    auto r = vm_->verify_block(blk_);
    if (!r) {
        error_ = r.error();
        return false;
    }
    error_.clear();
    return true;
}

void VmBlock::accept() { vm_->accept_block(blk_->block_id); }

void VmBlock::reject() { vm_->reject_block(blk_->block_id); }

}  // namespace lux::xvm
