// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/vm.hpp"

#include <algorithm>
#include <cstring>
#include <mutex>
#include <shared_mutex>
#include <string>

namespace lux::quantumvm {
namespace {

Bytes key_of(const char* s) {
    const auto* p = reinterpret_cast<const std::uint8_t*>(s);
    return Bytes(p, p + std::strlen(s));
}

Bytes be64(std::uint64_t v) {
    Bytes out(8, 0);
    for (int i = 0; i < 8; ++i) out[static_cast<std::size_t>(i)] = static_cast<std::uint8_t>(v >> (56 - 8 * i));
    return out;
}

std::uint64_t read_be64(ByteView b) {
    std::uint64_t v = 0;
    for (std::size_t i = 0; i < 8; ++i) v = (v << 8) | b[i];
    return v;
}

}  // namespace

const Bytes kLastAcceptedKey = key_of("lastAccepted");
const Bytes kTipHeightKey = key_of("height");

Bytes height_key(std::uint64_t h) {
    Bytes k(9, 0);
    k[0] = 'h';
    const Bytes be = be64(h);
    std::copy(be.begin(), be.end(), k.begin() + 1);
    return k;
}

// ── the block as consensus holds it

bool VmBlock::verify() {
    const auto st = blk_->verify();
    if (!st) {
        // False is a refusal to vote, never an error: an honest node does not
        // vote for a block its own execution rejects. The reason is kept so a
        // human can be told why.
        refusal_ = st.error().message();
        return false;
    }
    refusal_.clear();
    return true;
}

void VmBlock::accept() {
    if (accepted_ || rejected_) return;
    accepted_ = true;
    (void)blk_->accept();
}

void VmBlock::reject() {
    if (accepted_ || rejected_) return;
    rejected_ = true;
    (void)blk_->reject();
}

// ── the chain

QuantumVM::QuantumVM(config::Config cfg) : config_(cfg) {
    pool_ = std::make_unique<TransactionPool>(
        config_.max_parallel_txs > 0 ? static_cast<std::size_t>(config_.max_parallel_txs) : 100);
}

QuantumVM::~QuantumVM() = default;

Status QuantumVM::initialize(const Init& init) {
    std::unique_lock guard(lock_);

    // One place normalises the config, so nothing downstream has to ask again
    // whether a batch size or a window is usable.
    if (auto r = config_.validate(); !r) return r;

    if (init.db == nullptr) return fail(Err::NotConfigured, "no store");
    db_ = init.db;

    // A node that cannot say who it is cannot be one of a number of distinct
    // signers, so it does not start.
    if (init.node_id.empty()) return fail(Err::NoIdentity);
    node_id_ = init.node_id;
    network_id_ = init.network_id;
    chain_id_ = init.chain_id;

    auto signer = quantum::QuantumSigner::make(config_.quantum_algorithm_version,
                                               config_.quantum_stamp_window);
    if (!signer) return std::unexpected(signer.error());
    signer_ = std::make_unique<quantum::QuantumSigner>(*signer);

    pool_ = std::make_unique<TransactionPool>(static_cast<std::size_t>(config_.max_parallel_txs));

    // Q-Chain sells no blockspace, so the policy is the committee-only
    // sentinel — a constant, which is why nothing here re-validates it.
    fee_policy_ = std::make_unique<NoUserTxPolicy>();

    state_ = std::make_unique<store::Version>(db_);

    // A chain with no block cannot answer the frontier query bootstrap starts
    // with, so write height 0 before anything asks.
    if (auto r = seed_genesis(); !r) return r;
    if (auto seeded = tip(); seeded) preferred_ = *seeded;

    // The validator id is the NODE's id, not the chain's. The threshold counts
    // DISTINCT validator ids, so naming the chain here would give every node on
    // Q-Chain the same signer identity: each peer's signature would arrive as a
    // duplicate of the first, the count would never pass one, and quantum
    // finality would be unreachable on any threshold above 1.
    auto bridge = quasar::Quasar::make(quasar::Config{node_id_, config_.committee});
    if (!bridge) return std::unexpected(bridge.error());
    bridge_ = *bridge;

    return ok();
}

lux::node::Id QuantumVM::last_accepted() const {
    std::shared_lock guard(lock_);
    auto t = tip();
    return t ? *t : kEmptyId;
}

std::uint64_t QuantumVM::last_accepted_height() const {
    std::shared_lock guard(lock_);
    auto h = tip_height();
    return h ? *h : 0;
}

Result<Id> QuantumVM::tip() const {
    // ids.Empty with no error for exactly one reason — the chain holds no block
    // at all — and an error for every other reason it could not read one.
    auto raw = state_->get(view(kLastAcceptedKey));
    if (!raw) {
        if (raw.error().code == Err::NotFound) return kEmptyId;
        return fail(Err::TipUnreadable, "last accepted: " + raw.error().message());
    }
    if (raw->size() != kIdLen)
        return fail(Err::TipUnreadable,
                    "last accepted holds " + std::to_string(raw->size()) + " bytes");
    return id_from(view(*raw));
}

Result<std::uint64_t> QuantumVM::tip_height() const {
    // A read that failed and a height of zero are different facts, and this
    // reports them differently. Answering 0 for both is what let one transient
    // failure look like a chain that had not started: the next block was built
    // at height 1 on a chain a thousand blocks long.
    auto raw = state_->get(view(kTipHeightKey));
    if (!raw) {
        if (raw.error().code == Err::NotFound) return std::uint64_t{0};
        return fail(Err::TipUnreadable, "height: " + raw.error().message());
    }
    if (raw->size() != 8)
        return fail(Err::TipUnreadable, "height holds " + std::to_string(raw->size()) + " bytes");
    return read_be64(view(*raw));
}

Result<Id> QuantumVM::block_id_at_height(std::uint64_t height) const {
    std::shared_lock guard(lock_);
    const Bytes k = height_key(height);
    auto raw = state_->get(view(k));
    if (!raw)
        return fail(Err::NoBlockAtHeight, std::to_string(height) + ": " + raw.error().message());
    if (raw->size() != kIdLen)
        return fail(Err::NoBlockAtHeight, std::to_string(height) + ": index holds " +
                                              std::to_string(raw->size()) + " bytes");
    return id_from(view(*raw));
}

Result<BlockPtr> QuantumVM::block_at(const Id& block_id) const {
    auto raw = state_->get(view(block_id));
    if (!raw)
        return fail(raw.error().code, "failed to get block " + text(block_id) + ": " +
                                          raw.error().message());
    auto fields = wire::parse_block_bytes(view(*raw));
    if (!fields) return std::unexpected(fields.error());
    return std::make_shared<Block>(const_cast<QuantumVM*>(this), std::move(*fields));
}

Result<BlockPtr> QuantumVM::block(const Id& block_id) const {
    std::shared_lock guard(lock_);
    return block_at(block_id);
}

Result<BlockPtr> QuantumVM::parse_block(ByteView data) const {
    auto fields = wire::parse_block_bytes(data);
    if (!fields) return std::unexpected(fields.error());
    return std::make_shared<Block>(const_cast<QuantumVM*>(this), std::move(*fields));
}

Status QuantumVM::extends_tip(const Block& b) const {
    auto t = tip();
    if (!t) return std::unexpected(t.error());
    if (b.parent_id() != *t)
        return fail(Err::NotTheTip, "parent " + text(b.parent_id()) + ", tip " + text(*t));

    std::uint64_t next = 0;
    if (!empty(*t)) {
        auto h = tip_height();
        if (!h) return std::unexpected(h.error());
        next = *h + 1;
    }
    if (b.height() != next)
        return fail(Err::NotTheTip, "height " + std::to_string(b.height()) + ", tip expects " +
                                        std::to_string(next));
    return ok();
}

Status QuantumVM::commit_block(const Block& b) {
    // The staging layer holds nothing across this call: writes not discarded
    // here are not discarded at all, they are flushed by the next commit that
    // succeeds — an orphan block and a live height index arriving with an
    // unrelated block.
    struct Abort {
        store::Version* s;
        ~Abort() { s->abort(); }
    } abort{state_.get()};

    if (auto r = b.on_this_chain(); !r) return r;
    if (auto r = extends_tip(b); !r) return r;
    if (auto r = b.apply(); !r) return r;

    const Id block_id = b.id();
    const ByteView wire = b.bytes();
    const Bytes hk = height_key(b.height());
    const Bytes hv = be64(b.height());

    if (auto r = state_->put(view(block_id), wire); !r) return r;
    if (auto r = state_->put(view(hk), view(block_id)); !r) return r;
    if (auto r = state_->put(view(kLastAcceptedKey), view(block_id)); !r) return r;
    if (auto r = state_->put(view(kTipHeightKey), view(hv)); !r) return r;

    // Block, height index and tip pointer land as ONE write, or none of them do.
    if (auto r = state_->commit(); !r)
        return fail(r.error().code, "commit block " + text(block_id) + ": " + r.error().message());
    return ok();
}

Status QuantumVM::seed_genesis() {
    auto t = tip();
    if (!t) return std::unexpected(t.error());
    if (!empty(*t)) return ok();

    wire::BlockFields f;
    f.timestamp = 0;  // a constant of the chain, never the wall clock
    f.height = 0;
    f.parent_id = kEmptyId;
    f.chain_id = chain_id_;
    f.network_id = network_id_;

    Block genesis(this, std::move(f));
    return commit_block(genesis);
}

Verdict QuantumVM::process_transactions(const std::vector<TxPtr>& txs) const {
    Verdict out;
    const std::size_t batch = static_cast<std::size_t>(config_.parallel_batch_size);
    for (std::size_t i = 0; i < txs.size(); i += batch) {
        const std::size_t end = std::min(i + batch, txs.size());
        const std::vector<TxPtr> slice(txs.begin() + static_cast<std::ptrdiff_t>(i),
                                       txs.begin() + static_cast<std::ptrdiff_t>(end));
        Verdict part = process_batch(slice, *signer_, config_.quantum_stamp_enabled,
                                     config_.gpu_batch_threshold);
        out.valid.insert(out.valid.end(), part.valid.begin(), part.valid.end());
        out.rejected.insert(out.rejected.end(), part.rejected.begin(), part.rejected.end());
    }
    return out;
}

Result<BlockPtr> QuantumVM::build_block_locked() {
    if (shutting_down()) return fail(Err::VMShutdown);

    auto pending = pool_->pending(static_cast<std::size_t>(config_.parallel_batch_size));
    if (pending.empty()) return fail(Err::NoPendingTxs);

    Verdict verdict = process_transactions(pending);

    // A transaction that cannot verify now will not verify later — a quantum
    // stamp only gets staler. Left in place it holds a pool slot for good, and
    // enough of them fill the pool and stop the chain accepting anything.
    for (const auto& tx : verdict.rejected) (void)pool_->remove(tx->id());
    if (verdict.valid.empty()) return fail(Err::ParallelProcessingFailed);

    auto parent_id = tip();
    if (!parent_id) return std::unexpected(parent_id.error());
    auto parent = block_at(*parent_id);
    if (!parent)
        return fail(parent.error().code,
                    "read tip " + text(*parent_id) + ": " + parent.error().message());

    // Never stamp behind the parent, and never stamp where verify will refuse
    // it. A clock that trails the tip by less than the skew allowance is a peer
    // that ran fast, and clamping forward covers it. A clock that trails by MORE
    // is this node's clock being wrong: every block it could build now carries a
    // timestamp its own verify rejects for exceeding now+skew, so it says so
    // rather than producing blocks nobody — itself included — accepts.
    const Seconds now = clock_.seconds();
    const Seconds parent_time = (*parent)->timestamp();
    if (parent_time > now + kMaxFutureSkewSeconds)
        return fail(Err::ClockBehindTip, "tip is stamped " + std::to_string(parent_time) +
                                             ", this node reads " + std::to_string(now));

    wire::BlockFields f;
    f.timestamp = std::max(now, parent_time);
    f.height = (*parent)->height() + 1;
    f.parent_id = *parent_id;
    f.chain_id = chain_id_;
    f.network_id = network_id_;
    f.transactions = std::move(verdict.valid);

    auto block = std::make_shared<Block>(this, std::move(f));
    if (block->bytes().size() > wire::kMaxBlockSize)
        return fail(Err::BlockTooLarge, std::to_string(block->bytes().size()) + " bytes over " +
                                            std::to_string(wire::kMaxBlockSize));
    return block;
}

Result<BlockPtr> QuantumVM::build_block() {
    Result<BlockPtr> built = fail(Err::NoPendingTxs);
    {
        std::unique_lock guard(lock_);
        built = build_block_locked();
        // Whatever this call leaves behind in the pool has to wake a builder
        // again: the latch holds one signal, so two transactions arriving
        // together wake one build, and work the batch limit left over would
        // otherwise sit there until some unrelated transaction happened to
        // arrive.
        pool_->signal_if_work();
    }
    if (!built) return built;

    // Signing reaches the consensus core and waits on it, so it happens with no
    // VM lock held — a verify arriving meanwhile must not queue behind it.
    sign_block_with_quasar(**built);
    return built;
}

void QuantumVM::sign_block_with_quasar(const Block& b) {
    const auto bridge = bridge_;
    if (!bridge) return;
    (void)bridge->sign_block(b.id(), b.bytes(), b.height());
}

std::shared_ptr<lux::node::Block> QuantumVM::build() {
    auto blk = build_block();
    if (!blk) return nullptr;
    return std::make_shared<VmBlock>(this, *blk);
}

std::shared_ptr<lux::node::Block> QuantumVM::parse(std::span<const std::uint8_t> data) {
    auto blk = parse_block(data);
    if (!blk) return nullptr;
    return std::make_shared<VmBlock>(this, *blk);
}

std::shared_ptr<lux::node::Block> QuantumVM::get(const lux::node::Id& id) const {
    auto blk = block(id);
    if (!blk) return nullptr;
    return std::make_shared<VmBlock>(const_cast<QuantumVM*>(this), *blk);
}

Status QuantumVM::shutdown() {
    const bool already = shutting_down_.exchange(true, std::memory_order_relaxed);
    if (already) return ok();

    std::unique_lock guard(lock_);
    if (pool_) pool_->close();
    if (state_) (void)state_->close();
    return ok();
}

Health QuantumVM::health() const {
    Health h;
    h.healthy = !shutting_down();
    h.version = kVersion;
    h.quantum_enabled = config_.quantum_stamp_enabled;
    h.corona_enabled = config_.corona_enabled;
    h.pending_txs = pool_ ? pool_->count() : 0;
    return h;
}

Result<Stamp> QuantumVM::stamp_block(const Id& block_id, std::uint64_t p_chain_height,
                                     ByteView message) {
    if (bridge_ && !empty(block_id)) {
        auto sig = bridge_->sign_block(block_id, message, p_chain_height);
        if (sig) return Stamp(*sig);
        // The bridge could not sign; fall through to the ML-DSA stamp rather
        // than reporting a failure the finality bridge cannot act on.
    }

    auto key = signer_->generate_key();
    if (!key) return std::unexpected(key.error());
    auto sig = signer_->sign(message, &*key);
    if (!sig) return std::unexpected(sig.error());
    return Stamp(*sig);
}

Status QuantumVM::verify_stamp(ByteView message, const Stamp& stamp) const {
    if (std::holds_alternative<std::monostate>(stamp)) return fail(Err::NoStamp);

    if (const auto* sig = std::get_if<quasar::QuasarSig>(&stamp)) {
        if (!bridge_) return fail(Err::UnverifiedSigner, "no Quasar bridge to check against");
        if (!bridge_->verify_signature(message, sig))
            return fail(Err::UnverifiedSigner,
                        "Quasar signature from " + sig->validator_id + " does not check out");
        return ok();
    }
    if (const auto* agg = std::get_if<quasar::AggregatedSignature>(&stamp)) {
        if (!bridge_) return fail(Err::AggregateRefused, "no Quasar bridge to check against");
        if (!bridge_->verify_aggregate(message, agg))
            return fail(Err::AggregateRefused, "aggregated signature does not check out");
        return ok();
    }
    const auto* qs = std::get_if<quantum::QuantumSignature>(&stamp);
    return signer_->verify(message, qs);
}

Status QuantumVM::issue_tx(const Transaction& tx) const {
    // Under the committee-only policy every amount is refused (LP-0130 §6), so
    // there is no path from here into the pool — Q-Chain state advances only
    // through consensus-internal cert aggregation, which reaches the pool
    // directly and never passes this way.
    if (!fee_policy_) return fail(Err::NoFeePolicy);
    return fee_policy_->validate_fee(tx.fee());
}

}  // namespace lux::quantumvm
