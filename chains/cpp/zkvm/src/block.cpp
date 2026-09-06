// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/block.hpp"

#include "lux/zkvm/vm.hpp"
#include "lux/zkvm/zap.hpp"

#include <set>

namespace lux::zkvm {
namespace {

// Block: ParentID 32B@0, Height u64@32, Timestamp i64@40, TxLens list@48,
//        TxBlob bytes@56, StateRoot bytes@64
constexpr int kParent = 0, kHeight = 32, kTime = 40, kTxLens = 48, kTxBlob = 56, kStateRoot = 64;

// Genesis: Timestamp i64@0, TxLens list@8, TxBlob bytes@16
constexpr int kGenTime = 0, kGenTxLens = 8, kGenTxBlob = 16;
constexpr int kGenSize = 24;

}  // namespace

Id Block::compute_id() const {
    Hasher h;
    if (vm_) h.write(view(vm_->bind()));
    h.write(view(parent_id));
    h.num(block_height);
    h.num(block_timestamp);
    for (const auto& tx : txs) {
        const Id tx_id = tx.compute_id();
        h.write(view(tx_id));
    }
    h.write(view(state_root));
    return h.sum();
}

lux::node::Id Block::id() const {
    if (id_ == kEmptyId) id_ = compute_id();
    return id_;
}

std::span<const std::uint8_t> Block::bytes() const {
    if (bytes_.empty()) bytes_ = marshal();
    return view(bytes_);
}

lux::node::Id Block::root() const {
    Id r{};
    if (state_root.size() == 32) std::copy(state_root.begin(), state_root.end(), r.begin());
    return r;
}

Bytes Block::marshal() const {
    std::vector<std::uint32_t> tx_lens;
    Bytes tx_blob;
    wire::pack_objs(txs, [](const Transaction& t) { return t.marshal(); }, tx_lens, tx_blob);

    zap::Builder b(zap::kHeaderSize + kBlkSize + int(tx_blob.size()) + int(state_root.size()) +
                   4 * int(tx_lens.size()) + 256);
    const int tx_off = wire::write_u32_list(b, tx_lens);
    auto ob = b.start_object(kBlkSize);
    ob.set_bytes_fixed(kParent, view(parent_id));
    ob.set_u64(kHeight, block_height);
    ob.set_u64(kTime, static_cast<std::uint64_t>(block_timestamp));
    ob.set_list(kTxLens, tx_off, int(tx_lens.size()));
    ob.set_bytes(kTxBlob, view(tx_blob));
    ob.set_bytes(kStateRoot, view(state_root));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<void> parse_block_bytes(ByteView data, Block& blk) {
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());

    blk.parent_id = wire::read_id(*o, kParent);
    blk.block_height = o->u64(kHeight);
    blk.block_timestamp = static_cast<std::int64_t>(o->u64(kTime));
    blk.state_root = wire::cp(o->bytes(kStateRoot));

    auto txs = wire::unpack_objs<Transaction>(wire::read_u32_list(*o, kTxLens),
                                              o->bytes(kTxBlob), parse_transaction);
    if (!txs) return std::unexpected(txs.error());
    blk.txs = std::move(*txs);
    return {};
}

wire::Result<void> Block::syntactic_verify() const {
    if (vm_ == nullptr) return std::unexpected("zkvm: block is not bound to a chain");

    // Height 0 is genesis, and genesis has no parent. A height-0 block that
    // names one makes two claims about which block it is.
    if (block_height == 0 && parent_id != kEmptyId) return std::unexpected(kErrInvalidBlock);

    // A block off the wire is held to the bound a block this node builds is held
    // to, so a proposer cannot produce one its own peers refuse. The bound is
    // configuration, fixed at genesis — no block moves it.
    if (txs.size() > vm_->z_config().max_tx_per_block)
        return std::unexpected(std::string(kErrInvalidBlock) + ": " +
                               std::to_string(txs.size()) + " transactions over the " +
                               std::to_string(vm_->z_config().max_tx_per_block) + " cap");

    // The node's own clock, not the ledger. Held against the parent's timestamp
    // too, but that is a different rule and it lives below with the parent.
    if (block_timestamp > vm_->now() + kMaxClockSkew) return std::unexpected(kErrFutureBlock);

    // Block-level shape: every nullifier in the block must be distinct.
    // verify_transaction only sees nullifiers already spent in ACCEPTED state,
    // so without this two transactions in one block — or one transaction listing
    // a nullifier twice — spend the same shielded note and inflate supply. The
    // block's transactions against each other, so the spent set is not consulted.
    ByteSet spent_here;
    for (const auto& tx : txs) {
        for (const auto& n : tx.nullifiers) {
            if (!spent_here.insert(n).second) return std::unexpected(kErrDuplicateNullifier);
        }
    }

    // Each transaction's own shape, and its expiry against the height THIS block
    // claims — which is on the wire in front of us, so no parent is needed to
    // know a transaction has outlived its window.
    for (const auto& tx : txs) {
        if (auto r = vm_->syntactic_verify(tx, block_height); !r) return r;
    }
    return {};
}

wire::Result<void> Block::check() {
    // Everything decidable from the block in hand goes first. A peer's block
    // that cannot be true of any chain is refused before this node reads a
    // single key, and the mempool asks the same question of a transaction
    // before one is ever assembled.
    if (auto r = syntactic_verify(); !r) return r;

    // Then the ledger. admit re-asks the shape half above, which has already
    // passed: a handful of size comparisons next to a STARK, and the price of
    // check asking assembly's predicate VERBATIM rather than a copy of its two
    // halves that a later edit could let drift.
    for (const auto& tx : txs) {
        if (auto r = vm_->admit(tx, block_height); !r) return r;
    }

    if (block_height > 0) {
        auto parent = vm_->block(parent_id);
        if (!parent) return std::unexpected(parent.error());

        // The parent must be one this chain can still build on: the accepted
        // tip, or a block verified above it and not yet decided. Height alone is
        // not that check — a block whose parent is an OLD accepted block
        // satisfies height == parent+1 perfectly well, and accepting it rewinds
        // the tip and leaves the height index naming an orphan as the block at
        // that height to every peer that bootstraps from it.
        const Id tip = vm_->chain().tip();
        const std::uint64_t tip_height = vm_->chain().height();
        if ((*parent)->id() != tip && (*parent)->block_height <= tip_height)
            return std::unexpected(std::string(kErrNotOnTip) + ": parent " +
                                   hex((*parent)->id()) + " at height " +
                                   std::to_string((*parent)->block_height) +
                                   " is beneath the tip at " + std::to_string(tip_height));

        if (block_height != (*parent)->block_height + 1)
            return std::unexpected(kErrInvalidHeight);
        if (block_timestamp < (*parent)->block_timestamp)
            return std::unexpected(kErrInvalidTimestamp);
    }

    const Id want = vm_->compute_state_root(txs);
    if (state_root.size() != want.size() ||
        !std::equal(state_root.begin(), state_root.end(), want.begin()))
        return std::unexpected(kErrInvalidStateRoot);

    // A block that verifies is one the engine may build on, so it has to be
    // findable by id — including one parsed from a peer rather than built here.
    // Tracking only self-built blocks leaves a follower able to verify the first
    // block of a run and unable to verify the second.
    vm_->chain().track(std::static_pointer_cast<Decision>(shared_from_this()));
    return {};
}

bool Block::verify() {
    auto r = check();
    if (!r) {
        error_ = r.error();
        return false;
    }
    error_.clear();
    return true;
}

// commit applies the block. Everything is staged and committed in one batch with
// the block and the tip, so a spend that cannot be recorded takes the whole block
// with it.
//
// This used to mark the block accepted and move the tip before writing anything,
// then issue one write per nullifier and per output, each returning early. A
// failure partway left some notes spent and some outputs created, under a tip the
// chain had already advanced — a shielded pool half applied, with no way back and
// no way to apply the block again.
wire::Result<void> Block::commit() {
    if (vm_ == nullptr) return std::unexpected("zkvm: block is not bound to a chain");
    return vm_->chain().accept(std::static_pointer_cast<Decision>(shared_from_this()));
}

void Block::accept() {
    // Decided once. A block already rejected can never be accepted, and one
    // already accepted is durable — re-running the commit would be asked to
    // spend notes this block has already spent.
    if (status_ != Status::Processing) return;
    auto r = commit();
    if (!r) error_ = r.error();
}

wire::Result<void> Block::write(store::Store& view_store) {
    (void)view_store;  // the three stores were built over this same view
    for (const auto& tx : txs) {
        for (const auto& n : tx.nullifiers) {
            if (auto r = vm_->nullifiers().mark_spent(view(n), block_height); !r) return r;
        }
        for (std::size_t i = 0; i < tx.outputs.size(); ++i) {
            Utxo u;
            u.tx_id = tx.id;
            u.output_index = std::uint32_t(i);
            u.commitment = tx.outputs[i].commitment;
            u.ciphertext = tx.outputs[i].encrypted_note;
            u.ephemeral_pk = tx.outputs[i].ephemeral_pubkey;
            u.height = block_height;
            if (auto r = vm_->utxos().add(u); !r) return r;
        }
    }
    Id next{};
    if (state_root.size() == 32) std::copy(state_root.begin(), state_root.end(), next.begin());
    return vm_->state_root().finalize(next);
}

// publish marks the block accepted and releases the transactions it carried. It
// runs after the commit, so a transaction is only dropped from the pool once the
// block that spends it is durable.
void Block::publish() {
    status_ = Status::Accepted;
    for (const auto& tx : txs) vm_->mempool().remove(tx.id);

    // The chain has passed this height, so anything expiring at or below it can
    // never enter a block. Nothing else drops those, and a pool full of them
    // refuses every honest arrival paying the same floor.
    vm_->mempool().prune_expired(block_height);
}

// reject hands back what the block was carrying. The transactions were never
// refused — they lost a race — so they go back into the pool, exactly as the
// reference's Reject does.
//
// It is inert on a block that is already decided. Rejecting an ACCEPTED block
// would return transactions whose notes this chain has already spent, and the
// next block this node assembled would carry a double spend that every peer
// refuses.
void Block::reject() {
    if (status_ != Status::Processing) return;
    status_ = Status::Rejected;
    vm_->chain().drop(id());
    for (const auto& tx : txs) (void)vm_->mempool().add(tx);
}

Bytes Genesis::marshal() const {
    std::vector<std::uint32_t> tx_lens;
    Bytes tx_blob;
    wire::pack_objs(initial_txs, [](const Transaction& t) { return t.marshal(); }, tx_lens,
                    tx_blob);

    zap::Builder b(zap::kHeaderSize + kGenSize + int(tx_blob.size()) + 4 * int(tx_lens.size()) +
                   128);
    const int tx_off = wire::write_u32_list(b, tx_lens);
    auto ob = b.start_object(kGenSize);
    ob.set_u64(kGenTime, static_cast<std::uint64_t>(timestamp));
    ob.set_list(kGenTxLens, tx_off, int(tx_lens.size()));
    ob.set_bytes(kGenTxBlob, view(tx_blob));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<Genesis> parse_genesis(ByteView data) {
    // An empty configuration is a chain starting at timestamp 0 with nothing
    // allocated — which is a chain, and is what the reference makes of empty
    // genesis bytes.
    if (data.empty()) return Genesis{};

    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());

    Genesis g;
    g.timestamp = static_cast<std::int64_t>(o->u64(kGenTime));
    auto txs = wire::unpack_objs<Transaction>(wire::read_u32_list(*o, kGenTxLens),
                                              o->bytes(kGenTxBlob), parse_transaction);
    if (!txs) return std::unexpected(txs.error());
    g.initial_txs = std::move(*txs);
    return g;
}

BlockSummary summarize(const Block& b) {
    BlockSummary s;
    s.id = b.id();
    s.height = b.block_height;
    s.timestamp = b.block_timestamp;
    s.tx_count = b.txs.size();
    s.state_root = b.state_root;
    return s;
}

}  // namespace lux::zkvm
