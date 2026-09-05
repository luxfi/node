// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.hpp — the Z-Chain as the node's VM seam sees it.
//
// The seam asks a chain five questions — build, parse, get, prefer,
// last-accepted — and asks a block four: where it sits, what its execution
// produced, whether this node's own execution accepts it, and that it was
// decided. Everything else in this port exists to answer those.
//
// ONE PREDICATE. admit is what assembly asks before putting a transaction in a
// block, and it is what verify asks about a block that arrived. A proposer that
// assembled what its own peers refuse is a halt, free, for whoever sends the
// transaction — so there is one predicate and both paths ask it.
//
// EXECUTION IS NOT OPTIONAL. root() is on the Block because a block that cannot
// say what state it produced cannot be built: it would ask validators to certify
// a name rather than a result. The Z-Chain's answer is the state root in
// root.hpp, computed by running the block, never copied from a proposer.

#pragma once

#include "lux/node/vm.hpp"

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/chainstore.hpp"
#include "lux/zkvm/fee.hpp"
#include "lux/zkvm/mempool.hpp"
#include "lux/zkvm/nullifier.hpp"
#include "lux/zkvm/root.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/utxo.hpp"
#include "lux/zkvm/verifier.hpp"
#include "lux/zkvm/vertex.hpp"

#include <map>
#include <memory>
#include <string>

namespace lux::zkvm {

struct VmConfig {
    Id chain_id{};
    std::uint32_t network_id = 0;
    // The path segment a JSON-RPC caller reaches this chain under.
    std::string alias = "Z";

    // The Z-Chain is DEFINITIVELY strict-PQ: it is the shielded-settlement chain
    // pinned to the canonical Lux strict-PQ security profile, so a configuration
    // that says nothing gets the strict one. A permissive deployment MUST say so
    // explicitly; it is never the default for this chain.
    ZConfig z = ZConfig{{}, true, kDefaultMaxTxPerBlock, kDefaultProofCacheSize};

    std::size_t mempool_size = 1000;
};

struct Health {
    bool healthy = true;
    std::uint64_t utxo_count = 0;
    std::uint64_t nullifier_count = 0;
    std::uint64_t last_block_height = 0;
    std::size_t mempool_size = 0;
    std::size_t proof_cache_size = 0;
};

class Vm final : public lux::node::VM {
public:
    // The store is a constructor argument rather than something the VM makes for
    // itself: whether this node survives a restart is the host's decision, and
    // the VM must not be able to quietly answer it with "no".
    Vm(VmConfig config, store::Store& base);

    // initialize brings the chain up: opens the three stores over the chain
    // store's view, builds the proof verifier, seals the genesis block, and — on
    // a FIRST boot only — allocates what genesis names.
    wire::Result<void> initialize(const Genesis& genesis);

    // ---- the node's seam ----
    lux::node::Id chain_id() const override { return config_.chain_id; }
    std::string alias() const override { return config_.alias; }

    std::shared_ptr<lux::node::Block> build() override;
    std::shared_ptr<lux::node::Block> parse(std::span<const std::uint8_t> raw) override;
    std::shared_ptr<lux::node::Block> get(const lux::node::Id& id) const override;
    void prefer(const lux::node::Id& id) override { chain_->prefer(id); }
    lux::node::Id last_accepted() const override { return chain_->tip(); }
    std::uint64_t last_accepted_height() const override { return chain_->height(); }

    // ---- the same, with their reasons ----
    wire::Result<std::shared_ptr<Block>> build_block();
    wire::Result<std::shared_ptr<Block>> parse_block(ByteView raw);
    wire::Result<std::shared_ptr<Block>> block(const Id& id) const;

    // ---- the DAG shape ----
    wire::Result<std::shared_ptr<Vertex>> build_vertex();
    wire::Result<std::shared_ptr<Vertex>> parse_vertex(ByteView raw);

    // ---- the door a user transaction comes in by ----
    //
    // The fee gate fires FIRST, before mempool pressure changes: a zero-fee
    // transaction is refused at the entry, not after it has taken a slot. Then
    // the shape, because a transaction that cannot be built into any block must
    // not occupy a slot in a bounded pool. Internal replay reaches the pool
    // directly and bypasses the gate, as it does in the reference.
    wire::Result<Id> issue(Transaction tx);

    // admit is the ONE predicate. Assembly and consensus both ask it.
    wire::Result<void> admit(const Transaction& tx, std::uint64_t height) const;

    // verify_transaction is the spent-set check and the proof. A spent-set read
    // that FAILED refuses the transaction: reporting "not spent" for a set that
    // could not be read is how an already-spent note gets spent again.
    wire::Result<void> verify_transaction(const Transaction& tx) const;

    // compute_state_root reads the tree without mutating it, so assembly and
    // verify agree and a rejected block leaves nothing behind.
    Id compute_state_root(const std::vector<Transaction>& txs) const;

    Health health() const;

    // now is the node's clock, injected. A test states the time rather than
    // waiting for it.
    void set_now(std::int64_t unix_seconds) { now_ = unix_seconds; }
    std::int64_t now() const;

    const Id& bind() const { return bind_; }
    const ZConfig& z_config() const { return config_.z; }
    bool strict_pq() const { return config_.z.strict_pq; }
    const Fee& fee() const { return fee_; }

    ChainStore& chain() { return *chain_; }
    const ChainStore& chain() const { return *chain_; }
    Mempool& mempool() { return pool_; }
    UtxoDb& utxos() { return *utxo_db_; }
    NullifierDb& nullifiers() { return *nullifier_db_; }
    Root& state_root() { return *root_; }
    ProofVerifier& proofs() { return *verifier_; }

    const std::shared_ptr<Block>& genesis_block() const { return genesis_; }

    // reload rebuilds the caches the three stores keep beside the records. It
    // runs after a decision's writes have been discarded, so a cache that had
    // already recorded that decision's spends, outputs or root stops claiming
    // them.
    wire::Result<void> reload();

private:
    wire::Result<void> apply_genesis(const Genesis& genesis, store::Store& view);

    VmConfig config_;
    // bind is sha256(ChainID ‖ NetworkID). It is hashed into every block and
    // vertex id and into the public inputs every shielded proof is checked
    // against, and it is NOT on the wire — so a block or a proof made for
    // another chain does not name a block of this one and does not verify here,
    // rather than passing a check someone could forget to write.
    Id bind_{};

    std::unique_ptr<ChainStore> chain_;
    std::unique_ptr<UtxoDb> utxo_db_;
    std::unique_ptr<NullifierDb> nullifier_db_;
    std::unique_ptr<Root> root_;
    std::unique_ptr<ProofVerifier> verifier_;
    Mempool pool_;
    Fee fee_ = Fee::floor();
    std::shared_ptr<Block> genesis_;
    std::int64_t now_ = 0;
};

// chain_binding is sha256(ChainID ‖ big-endian NetworkID) — the value every id
// on this chain opens with.
Id chain_binding(const Id& chain_id, std::uint32_t network_id);

}  // namespace lux::zkvm
