// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/vm.hpp"

#include <chrono>
#include <set>

namespace lux::zkvm {

Id chain_binding(const Id& chain_id, std::uint32_t network_id) {
    Hasher h;
    h.write(view(chain_id));
    h.num32(network_id);
    return h.sum();
}

Vm::Vm(VmConfig config, store::Store& base)
    : config_(std::move(config)), pool_(config_.mempool_size) {
    bind_ = chain_binding(config_.chain_id, config_.network_id);
    chain_ = std::make_unique<ChainStore>(base, [this] { return reload(); });
}

std::int64_t Vm::now() const {
    if (now_ != 0) return now_;
    return std::chrono::duration_cast<std::chrono::seconds>(
               std::chrono::system_clock::now().time_since_epoch())
        .count();
}

wire::Result<void> Vm::reload() {
    if (auto r = nullifier_db_->reload(); !r) return r;
    if (auto r = utxo_db_->reload(); !r) return r;
    return root_->reload();
}

wire::Result<void> Vm::initialize(const Genesis& genesis) {
    if (config_.z.proof_cache_size == 0) config_.z.proof_cache_size = kDefaultProofCacheSize;
    if (config_.z.max_tx_per_block == 0) config_.z.max_tx_per_block = kDefaultMaxTxPerBlock;

    // The three stores write through the chain store's view, so their records
    // commit with the decision that made them and are discarded with one that
    // fails.
    auto utxo = UtxoDb::open(chain_->state());
    if (!utxo) return std::unexpected("failed to initialize UTXO DB: " + utxo.error());
    utxo_db_ = std::move(*utxo);

    auto nullifiers = NullifierDb::open(chain_->state());
    if (!nullifiers)
        return std::unexpected("failed to initialize nullifier DB: " + nullifiers.error());
    nullifier_db_ = std::move(*nullifiers);

    auto root = Root::open(chain_->state());
    if (!root) return std::unexpected("failed to initialize state root: " + root.error());
    root_ = std::move(*root);

    auto verifier = ProofVerifier::open(config_.z, bind_);
    if (!verifier)
        return std::unexpected("failed to initialize proof verifier: " + verifier.error());
    verifier_ = std::move(*verifier);

    // The Z-Chain accepts user-submitted shielded transactions, so it declares
    // the floor; the boot-time gate refuses a zero-fee user-facing chain.
    fee_ = Fee::floor();
    if (auto r = fee_.validate(); !r) return std::unexpected("zkvm: fee policy: " + r.error());

    genesis_ = std::make_shared<Block>();
    genesis_->bind_vm(*this);
    genesis_->block_height = 0;
    genesis_->block_timestamp = genesis.timestamp;
    genesis_->txs = genesis.initial_txs;
    genesis_->set_id(genesis_->compute_id());

    auto fresh = chain_->open(std::static_pointer_cast<Decision>(genesis_),
                              [this](ByteView raw) -> wire::Result<std::shared_ptr<Decision>> {
                                  auto b = parse_block(raw);
                                  if (!b) return std::unexpected(b.error());
                                  return std::static_pointer_cast<Decision>(*b);
                              });
    if (!fresh) return std::unexpected(fresh.error());

    // The genesis allocation is the one mutation outside a block, and it is
    // committed on its own: staged, it would ride on whichever block landed
    // first and vanish from a chain that never accepted one. seed records the
    // tip in that same commit, so a chain that has allocated says so on the next
    // boot and is not asked to allocate again — which ended in "UTXO already
    // exists", a node unable to restart until it produced a block.
    if (*fresh) {
        if (auto r = chain_->seed([&](store::Store& view_store) {
                return apply_genesis(genesis, view_store);
            });
            !r)
            return r;
    }
    return {};
}

wire::Result<void> Vm::apply_genesis(const Genesis& genesis, store::Store& view_store) {
    (void)view_store;
    for (const auto& tx : genesis.initial_txs) {
        for (std::size_t i = 0; i < tx.outputs.size(); ++i) {
            Utxo u;
            u.tx_id = tx.id;
            u.output_index = std::uint32_t(i);
            u.commitment = tx.outputs[i].commitment;
            u.ciphertext = tx.outputs[i].encrypted_note;
            u.ephemeral_pk = tx.outputs[i].ephemeral_pubkey;
            u.height = 0;  // genesis height
            if (auto r = utxo_db_->add(u); !r) return r;
        }
    }
    // Commit genesis to the root so block 1 builds on it, rather than leaving
    // genesis to be re-folded into every later block's root.
    return root_->finalize(root_->after(genesis.initial_txs));
}

Id Vm::compute_state_root(const std::vector<Transaction>& txs) const {
    return root_->after(txs);
}

wire::Result<void> Vm::admit(const Transaction& tx, std::uint64_t height) const {
    if (auto r = syntactic_verify(tx, height); !r) return r;
    return verify_transaction(tx);
}

wire::Result<void> Vm::syntactic_verify(const Transaction& tx, std::uint64_t height) const {
    if (auto r = tx.validate_basic(); !r) return r;
    if (tx.expiry < height) return std::unexpected(kErrExpired);
    return {};
}

wire::Result<void> Vm::verify_transaction(const Transaction& tx) const {
    for (const auto& n : tx.nullifiers) {
        auto spend = nullifier_db_->spent_at(view(n));
        if (!spend) return std::unexpected("zkvm: read spent set: " + spend.error());
        if (spend->spent) return std::unexpected(kErrNullifierSpent);
    }
    if (auto r = verifier_->verify(tx); !r)
        return std::unexpected("proof verification failed: " + r.error());
    return {};
}

wire::Result<Id> Vm::issue(Transaction tx) {
    if (auto r = fee_.admit(tx.fee); !r) return std::unexpected(r.error());
    if (auto r = tx.validate_basic(); !r) return std::unexpected(r.error());
    const Id id = tx.compute_id();
    if (auto r = pool_.add(std::move(tx)); !r) return std::unexpected(r.error());
    // The pool derives the id from the content, so this reports the transaction
    // the chain will carry rather than the one the caller named.
    return id;
}

wire::Result<std::shared_ptr<Block>> Vm::parse_block(ByteView raw) {
    auto blk = std::make_shared<Block>();
    blk->bind_vm(*this);
    if (auto r = parse_block_bytes(raw, *blk); !r) return std::unexpected(r.error());
    blk->set_bytes(bytes_of(raw));
    blk->set_id(blk->compute_id());
    return blk;
}

// block resolves a block id to a block. It is the ONE place this chain decides
// that a decision it made is a block: a vertex is a decision too, and asking for
// one by block id is answered as a miss rather than with something the caller
// cannot use.
wire::Result<std::shared_ptr<Block>> Vm::block(const Id& id) const {
    if (genesis_ && id == genesis_->id()) return genesis_;
    auto d = chain_->block(id, [this](ByteView raw) -> wire::Result<std::shared_ptr<Decision>> {
        auto b = const_cast<Vm*>(this)->parse_block(raw);
        if (!b) return std::unexpected(b.error());
        return std::static_pointer_cast<Decision>(*b);
    });
    if (!d) return std::unexpected(d.error());
    auto blk = std::dynamic_pointer_cast<Block>(*d);
    if (!blk) return std::unexpected(std::string(kErrNoBlock) + ": " + hex(id) + " is not a block");
    return blk;
}

wire::Result<std::shared_ptr<Block>> Vm::build_block() {
    auto built = chain_->propose(
        [&](const Decision& parent) -> wire::Result<std::shared_ptr<Decision>> {
            auto txs = pool_.pending(config_.z.max_tx_per_block);
            if (txs.empty()) return std::unexpected(kErrNoTransactions);

            // Assembly runs the SAME predicate verify runs, and drops what it
            // cannot build. It used to skip the shape check, so a transaction
            // with an out-of-range type — which the parser reads straight off
            // the wire — was assembled into every block and then refused by
            // every node's verify, including the proposer's. Nothing evicted it,
            // so that proposer never produced another block.
            std::vector<Transaction> valid;
            valid.reserve(txs.size());
            for (const auto& tx : txs) {
                if (auto r = admit(tx, parent.height() + 1); !r) {
                    pool_.remove(tx.id);
                    continue;
                }
                valid.push_back(tx);
            }
            if (valid.empty()) return std::unexpected(kErrNoTransactions);

            // Chain time only moves forward, and verify refuses a block below
            // its parent. A parent may legally be up to the clock skew ahead of
            // this node's clock, so an unclamped clock read here builds a block
            // this node's own verify then refuses.
            std::int64_t timestamp = now();
            const auto* parent_block = dynamic_cast<const Block*>(&parent);
            if (parent_block != nullptr && timestamp < parent_block->block_timestamp)
                timestamp = parent_block->block_timestamp;

            auto blk = std::make_shared<Block>();
            blk->bind_vm(*this);
            blk->parent_id = parent.id();
            blk->block_height = parent.height() + 1;
            blk->block_timestamp = timestamp;
            blk->txs = std::move(valid);
            const Id root = compute_state_root(blk->txs);
            blk->state_root.assign(root.begin(), root.end());
            blk->set_id(blk->compute_id());
            return std::static_pointer_cast<Decision>(blk);
        });
    if (!built) return std::unexpected(built.error());
    return std::static_pointer_cast<Block>(*built);
}

// build_vertex drains the pool, batches non-conflicting transactions and returns
// a vertex. admit is the same predicate verify runs, so nothing is batched that
// a peer will refuse.
wire::Result<std::shared_ptr<Vertex>> Vm::build_vertex() {
    const Id parent = chain_->tip();
    const std::uint64_t height = chain_->height();

    auto candidates = pool_.pending(config_.z.max_tx_per_block);
    if (candidates.empty()) return std::unexpected(kErrNoTransactions);

    ByteSet used;
    std::vector<Transaction> batch;
    for (const auto& tx : candidates) {
        if (auto r = admit(tx, height + 1); !r) {
            pool_.remove(tx.id);
            continue;
        }
        bool conflict = false;
        for (const auto& n : tx.nullifiers) {
            if (used.count(n)) {
                conflict = true;
                break;
            }
        }
        if (conflict) continue;
        for (const auto& n : tx.nullifiers) used.insert(n);
        batch.push_back(tx);
    }
    if (batch.empty()) return std::unexpected(kErrNoTransactions);

    auto v = std::make_shared<Vertex>();
    v->bind_vm(*this);
    v->vertex_height = height + 1;
    v->epoch = 0;
    v->parents = {parent};
    v->txs = std::move(batch);
    v->finish();
    return v;
}

wire::Result<std::shared_ptr<Vertex>> Vm::parse_vertex(ByteView raw) {
    return deserialize_vertex(raw, *this);
}

Health Vm::health() const {
    Health h;
    h.healthy = true;
    h.utxo_count = utxo_db_->count();
    h.nullifier_count = nullifier_db_->count();
    h.last_block_height = chain_->height();
    h.mempool_size = pool_.size();
    h.proof_cache_size = verifier_->cache_size();
    return h;
}

// ---- the node's seam ----

std::shared_ptr<lux::node::Block> Vm::build() {
    auto b = build_block();
    if (!b) return nullptr;  // nothing to build is "no", not a failure
    return std::static_pointer_cast<lux::node::Block>(*b);
}

std::shared_ptr<lux::node::Block> Vm::parse(std::span<const std::uint8_t> raw) {
    auto b = parse_block(raw);
    if (!b) return nullptr;
    return std::static_pointer_cast<lux::node::Block>(*b);
}

std::shared_ptr<lux::node::Block> Vm::get(const lux::node::Id& id) const {
    auto b = block(id);
    if (!b) return nullptr;
    return std::static_pointer_cast<lux::node::Block>(*b);
}

}  // namespace lux::zkvm
