// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm.hpp — the X-Chain as the node's VM seam sees it.
//
// The seam (lux/node/vm.hpp) asks a chain five questions — build, parse, get,
// prefer, last-accepted — and asks a block four: where it sits, what its
// execution produced, whether this node's own execution accepts it, and that it
// was decided. Everything else in this port exists to answer those.
//
// EXECUTION IS NOT OPTIONAL. `root()` is on the Block because a block that
// cannot say what state it produced cannot be built: it would ask validators to
// certify a name rather than a result. The X-Chain's answer is the execution
// root in root.hpp, computed by running the block, never copied from a proposer.

#pragma once

#include "lux/node/vm.hpp"
#include "lux/xvm/block.hpp"
#include "lux/xvm/executor.hpp"
#include "lux/xvm/fx.hpp"
#include "lux/xvm/genesis.hpp"
#include "lux/xvm/mempool.hpp"
#include "lux/xvm/root.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/store.hpp"
#include "lux/xvm/txs.hpp"

#include <map>
#include <memory>
#include <set>
#include <string>
#include <vector>

namespace lux::xvm {

// A block proposed more than this far ahead of local time is refused. It is the
// one place the chain trusts a clock, and it trusts it only as a bound.
inline constexpr std::uint64_t kSyncBoundSeconds = 10;

inline constexpr const char* kErrUnexpectedMerkleRoot = "unexpected merkle root";
inline constexpr const char* kErrTimestampBeyondSyncBound =
    "proposed timestamp is too far in the future relative to local time";
inline constexpr const char* kErrEmptyBlock = "block contains no transactions";
inline constexpr const char* kErrChildBlockEarlierThanParent =
    "proposed timestamp before current chain time";
inline constexpr const char* kErrConflictingBlockTxs = "block contains conflicting transactions";
inline constexpr const char* kErrConflictingParentTxs = "block contains conflicting transactions";
inline constexpr const char* kErrIncorrectHeight = "block has incorrect height";
inline constexpr const char* kErrBlockNotFound = "block not found";
inline constexpr const char* kErrChainNotSynced = "chain not synced";
inline constexpr const char* kErrNoTransactions = "no transactions";

// The size a block is built up to. A transaction may weigh at most
// mempool::kMaxTxSize, which is smaller than this, so the builder stops only
// when the pool is empty or the block is within one transaction of full.
inline constexpr std::size_t kTargetBlockSize = 128 * 1024;

struct VmConfig {
    std::uint32_t network_id = 0;
    Id chain_id{};
    Id net_id{};
    Id fee_asset_id{};
    std::uint64_t tx_fee = 0;
    std::uint64_t create_asset_tx_fee = 0;
    // The path segment a JSON-RPC caller reaches this chain under.
    std::string alias = "X";
};

class Vm;

// VmBlock is a parsed block plus the verdict this node reached about it. The
// verdict lives here rather than in the block bytes because it is THIS node's,
// not the proposer's.
class VmBlock final : public lux::node::Block {
public:
    VmBlock(Vm& vm, std::shared_ptr<block::StandardBlock> blk) : vm_(&vm), blk_(std::move(blk)) {}

    lux::node::Id id() const override { return blk_->block_id; }
    lux::node::Id parent() const override { return blk_->parent_id; }
    std::uint64_t height() const override { return blk_->height; }
    std::span<const std::uint8_t> bytes() const override { return view(blk_->bytes); }
    lux::node::Id root() const override { return blk_->root; }

    // verify runs this node's OWN execution of the block. False is a refusal to
    // vote, never a crash: an honest node does not vote for a block its own
    // execution rejects. The reason is kept for the caller that wants it.
    bool verify() override;
    void accept() override;

    // reject is the OTHER half of being decided, and it is not optional.
    //
    // Consensus chose a sibling; this block will never be accepted. Its
    // transactions, though, were never refused — they lost a race, not a
    // verification — so each one that still holds against the accepted state
    // goes back into the mempool. A chain that dropped them instead would
    // disagree with every other node about what is still pending and would
    // propose a different block. Go: block/executor.Block.Reject.
    //
    // The node's seam (lux/node/vm.hpp) declares accept but not yet reject, so
    // this is offered at the port's own surface: the transactions come back
    // where the node can see them, through the pool the next build() draws
    // from. When the seam grows `virtual void reject() = 0`, this is the
    // override — the body does not change.
    void reject();

    const std::string& error() const { return error_; }
    const std::shared_ptr<block::StandardBlock>& standard() const { return blk_; }

private:
    Vm* vm_;
    std::shared_ptr<block::StandardBlock> blk_;
    std::string error_;
};

class Vm final : public lux::node::VM, public state::Versions, public mempool::Verifier {
public:
    // The store is where this chain's state rests, and it is a constructor
    // argument rather than something the VM makes for itself: whether this node
    // survives a restart is the host's decision, and the VM must not be able to
    // quietly answer it with "no".
    Vm(VmConfig config, std::vector<executor::ParsedFx> fxs, store::Store& store = state::State::default_store());

    // ---- lifecycle ----

    // initialize brings the chain up.
    //
    // On a FIRST boot it installs the genesis transactions as the initial state
    // and seals a genesis block over them. A genesis tx is applied directly: it
    // has no inputs to authorize and no fee to pay, so there is nothing for the
    // verifiers to check that is not already true by construction.
    //
    // On EVERY LATER boot the store already holds that, and more: the state
    // is read back and genesis is NOT re-installed. Which of the two happens is
    // the store's answer, not a flag the caller passes — Go asks the same
    // question the same way (state.IsInitialized).
    wire::Result<void> initialize(std::vector<std::shared_ptr<txs::Tx>> genesis_txs,
                                  std::uint64_t genesis_time);

    // initialize_from_genesis is the form a HOST calls: it hands the VM the
    // genesis buffer it was configured with, not a list of objects. The fee
    // asset is the first asset in that buffer — a convention of position, so
    // there is no second field naming it that could disagree — and it is
    // adopted here rather than left to the caller to restate.
    wire::Result<void> initialize_from_genesis(ByteView genesis_bytes,
                                               std::uint64_t genesis_time);

    // alias_of resolves an asset's local name to its id. The names come from
    // the genesis buffer; an asset created later has no name, only an id.
    wire::Result<Id> alias_of(const std::string& alias) const;

    // bootstrapped tells the fxs that history has finished replaying, which is
    // when signature verification switches on.
    void set_bootstrapped(bool v);
    bool bootstrapped() const { return backend_.bootstrapped; }

    // now is the node's clock, injected. A test states the time rather than
    // waiting for it.
    void set_now(std::uint64_t unix_seconds);
    std::uint64_t now() const { return now_; }

    // ---- the mempool ----

    // issue offers a transaction to this node. It is the ONE door: the RPC and
    // the gossip handler both come through here, and the admission policy is
    // stated once, in mempool.hpp. Go: Network.IssueTxFromRPC.
    wire::Result<void> issue(std::shared_ptr<txs::Tx> tx);

    // verify_tx is this node's own opinion of a transaction — syntax, then
    // semantics and execution against the LAST ACCEPTED state. It is what the
    // admission gate asks, and what reject asks before returning a transaction
    // to the pool. Go: block/executor.manager.VerifyTx.
    wire::Result<void> verify_tx(txs::Tx& tx) override;

    std::size_t mempool_size() const { return pool_.len(); }
    mempool::Pool& pool() { return pool_; }
    mempool::Gossip& gossip() { return gossip_; }

    // ---- the seam ----

    lux::node::Id chain_id() const override { return config_.chain_id; }
    std::string alias() const override { return config_.alias; }

    std::shared_ptr<lux::node::Block> build() override;
    std::shared_ptr<lux::node::Block> parse(std::span<const std::uint8_t>) override;
    std::shared_ptr<lux::node::Block> get(const lux::node::Id&) const override;
    void prefer(const lux::node::Id&) override;
    lux::node::Id last_accepted() const override { return last_accepted_; }
    std::uint64_t last_accepted_height() const override;

    // ---- what the port exposes beyond the seam ----

    state::State& chain_state() { return state_; }
    const executor::Backend& backend() const { return backend_; }
    executor::Backend& backend() { return backend_; }
    const std::string& last_error() const { return last_error_; }

    // get_state resolves a parent block id to the state as of that block: the
    // accepted store for the last accepted block, or a verified block's own
    // pending diff. This is state::Versions.
    state::Chain* get_state(const Id& blk_id) override;

private:
    friend class VmBlock;

    // The bookkeeping one verified-but-undecided block carries.
    struct Pending {
        std::shared_ptr<block::StandardBlock> blk;
        std::unique_ptr<state::Diff> on_accept;
        std::set<Id> imported_inputs;
        std::map<Id, executor::AtomicRequests> atomic_requests;
    };

    wire::Result<void> verify_block(const std::shared_ptr<block::StandardBlock>& blk);
    void accept_block(const Id& blk_id);
    void reject_block(const Id& blk_id);
    wire::Result<void> verify_unique_inputs(const Id& blk_id, const std::set<Id>& inputs) const;
    wire::Result<std::shared_ptr<block::StandardBlock>> stateless_block(const Id& blk_id) const;

    VmConfig config_;
    executor::Backend backend_;
    fx::Clock clock_;
    state::State state_;

    std::map<Id, Pending> pending_;
    mempool::Pool pool_;
    mempool::Gossip gossip_;
    std::map<std::string, Id> aliases_;

    Id last_accepted_{};
    Id preferred_{};
    std::uint64_t now_ = 0;
    std::string last_error_;
};

}  // namespace lux::xvm
