// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/vm.hpp"

#include <algorithm>

namespace lux::xvm {

Vm::Vm(VmConfig config, std::vector<executor::ParsedFx> fxs) : config_(std::move(config)) {
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
    last_accepted_ = (*blk)->block_id;
    preferred_ = last_accepted_;
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

wire::Result<void> Vm::issue(std::shared_ptr<txs::Tx> tx) {
    if (tx == nullptr) return std::unexpected(txs::kErrNilTx);

    executor::SyntacticVerifier syn(backend_, *tx);
    if (auto r = tx->unsigned_tx->visit(syn); !r)
        return std::unexpected("failed to syntactically verify tx: " + r.error());

    // Semantic verification runs against the PREFERRED state, which is the state
    // the next block would be built on — verifying against the last accepted one
    // would accept a tx that conflicts with a block already in flight.
    state::Chain* preferred = get_state(preferred_);
    if (preferred == nullptr) return std::unexpected(state::kErrMissingParentState);
    executor::SemanticVerifier sem(backend_, *preferred, *tx);
    if (auto r = tx->unsigned_tx->visit(sem); !r)
        return std::unexpected("failed to semantically verify tx: " + r.error());

    mempool_.push_back(std::move(tx));
    return {};
}

std::shared_ptr<lux::node::Block> Vm::build() {
    last_error_.clear();
    if (mempool_.empty()) {
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
    for (const auto& tx : mempool_) {
        executor::SemanticVerifier sem(backend_, **diff, *tx);
        if (auto r = tx->unsigned_tx->visit(sem); !r) continue;
        executor::Executor exec(**diff, *tx);
        if (auto r = tx->unsigned_tx->visit(exec); !r) continue;
        bool conflicts = false;
        for (const auto& in : exec.inputs) {
            if (imported.count(in) != 0) conflicts = true;
        }
        if (conflicts) continue;
        for (const auto& in : exec.inputs) imported.insert(in);
        (*diff)->add_tx(tx);
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
        if (auto r = tx->unsigned_tx->visit(syn); !r)
            return std::unexpected("failed to syntactically verify tx " + hex(tx->id()) + ": " +
                                   r.error());
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
        if (auto r = tx->unsigned_tx->visit(sem); !r)
            return std::unexpected("failed to semantically verify tx " + hex(tx->id()) + ": " +
                                   r.error());

        executor::Executor exec(**diff, *tx);
        if (auto r = tx->unsigned_tx->visit(exec); !r)
            return std::unexpected("failed to execute tx " + hex(tx->id()) + ": " + r.error());

        for (const auto& in : exec.inputs) {
            if (pending.imported_inputs.count(in) != 0)
                return std::unexpected(kErrConflictingBlockTxs);
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
    // it conflicts with something that is.
    for (const auto& tx : blk->transactions) {
        std::erase_if(mempool_, [&](const std::shared_ptr<txs::Tx>& m) {
            return m->id() == tx->id();
        });
    }
    return {};
}

void Vm::accept_block(const Id& blk_id) {
    auto it = pending_.find(blk_id);
    if (it == pending_.end()) {
        last_error_ = kErrBlockNotFound;
        return;
    }
    it->second.on_accept->apply(state_);
    last_accepted_ = blk_id;
    preferred_ = blk_id;

    auto blk = it->second.blk;
    pending_.erase(it);

    // Drop every sibling: a block that is not on the accepted chain can no
    // longer be accepted, and keeping its diff pinned would let a later block
    // build on state that will never exist.
    for (auto p = pending_.begin(); p != pending_.end();) {
        if (p->second.blk->height <= blk->height) {
            p = pending_.erase(p);
        } else {
            ++p;
        }
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

}  // namespace lux::xvm
