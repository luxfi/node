// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/vm.hpp"

#include "lux/fhevm/json.hpp"

#include <chrono>
#include <cstring>

namespace lux::fhevm {
namespace {

Bytes u64_be(std::uint64_t v) {
    Bytes b(8);
    for (int i = 0; i < 8; ++i) b[std::size_t(i)] = std::uint8_t(v >> (56 - 8 * i));
    return b;
}

std::uint64_t be_u64(ByteView b) {
    std::uint64_t v = 0;
    for (std::uint8_t c : b) v = (v << 8) | c;
    return v;
}

std::string to_string(std::uint64_t v) { return std::to_string(v); }

}  // namespace

std::int64_t Clock::real_now() {
    return std::int64_t(std::chrono::duration_cast<std::chrono::seconds>(
                            std::chrono::system_clock::now().time_since_epoch())
                            .count());
}

Id vm_id() {
    Id id{};
    constexpr std::string_view kWord = "fhevm";
    std::memcpy(id.data(), kWord.data(), kWord.size());
    return id;
}

// ---- genesis -----------------------------------------------------------------

// parse_genesis reads the genesis the way the reference reads it — plain
// json.Unmarshal, not a Decoder. So a member this build does not know is
// IGNORED rather than refused, and any non-space byte after the value is an
// error. Refusing the unknown member here meant a genesis Go starts a chain on
// stopped this one from starting at all, which is not a divergence about one
// transaction but about the whole chain.
Result<Genesis> parse_genesis(std::string_view s) {
    json::Value v;
    std::string err;
    std::size_t consumed = 0;
    if (!json::parse(s, &v, &consumed, &err)) return fail(Err::InvalidPayload, err);
    if (json::trailing(s, consumed)) return fail(Err::InvalidPayload, "genesis: trailing content");
    json::Reader r(v, {"version", "message", "timestamp", "alloc", "committee", "threshold",
                       "publicKey"},
                   &err, json::Unknown::Ignore);
    if (!r.ok()) return fail(Err::InvalidPayload, "genesis: " + err);

    Genesis g;
    if (!read_i64(r.find("version"), "version", &g.version, &err)) {
        return fail(Err::InvalidPayload, "genesis: " + err);
    }
    if (!read_string(r.find("message"), "message", &g.message, &err)) {
        return fail(Err::InvalidPayload, "genesis: " + err);
    }
    if (!read_i64(r.find("timestamp"), "timestamp", &g.timestamp, &err)) {
        return fail(Err::InvalidPayload, "genesis: " + err);
    }
    const json::Value* alloc = r.find("alloc");
    if (alloc != nullptr && !alloc->null()) {
        if (alloc->kind != json::Kind::Object) {
            return fail(Err::InvalidPayload, "genesis: alloc is not an object");
        }
        g.alloc_nil = false;
        for (const auto& [k, amount] : alloc->members) {
            std::uint64_t a = 0;
            if (!read_u64(&amount, "alloc", ~std::uint64_t(0), &a, &err)) {
                return fail(Err::InvalidPayload, "genesis: " + err);
            }
            g.alloc[k] = a;
        }
    }
    const json::Value* c = r.find("committee");
    if (c != nullptr && !c->null()) {
        if (c->kind != json::Kind::Array) {
            return fail(Err::InvalidPayload, "genesis: committee is not an array");
        }
        g.committee_nil = false;
        for (const auto& el : c->array) {
            CommitteeMember m;
            if (!read_member(el, &m, &err, json::Unknown::Ignore)) {
                return fail(Err::InvalidPayload, "genesis: " + err);
            }
            g.committee.push_back(std::move(m));
        }
    }
    if (!read_i64(r.find("threshold"), "threshold", &g.threshold, &err)) {
        return fail(Err::InvalidPayload, "genesis: " + err);
    }
    const json::Value* pk = r.find("publicKey");
    if (pk != nullptr && !pk->null()) {
        if (!read_bytes(pk, "publicKey", &g.public_key, &err)) {
            return fail(Err::InvalidPayload, "genesis: " + err);
        }
        g.public_key_nil = false;
    }
    return g;
}

std::string marshal(const Genesis& g) {
    json::Writer w;
    w.begin_object();
    w.key("version");
    w.i64(g.version);
    w.key("message");
    w.string(g.message);
    w.key("timestamp");
    w.i64(g.timestamp);
    w.key("alloc");
    if (g.alloc.empty() && g.alloc_nil) {
        w.null();
    } else {
        w.begin_object();
        // Go marshals a map with its keys sorted, and std::map already is.
        for (const auto& [addr, amount] : g.alloc) {
            w.key(addr);
            w.u64(amount);
        }
        w.end_object();
    }
    w.key("committee");
    if (g.committee.empty() && g.committee_nil) {
        w.null();
    } else {
        w.begin_array();
        for (const auto& m : g.committee) write_member(w, m);
        w.end_array();
    }
    w.key("threshold");
    w.i64(g.threshold);
    w.key("publicKey");
    w.bytes(g.public_key, g.public_key_nil);
    w.end_object();
    return w.str();
}

// ---- the VM ------------------------------------------------------------------

VM::VM(Store* store, Config cfg)
    : store_(store),
      network_id_(cfg.network_id),
      chain_id_(cfg.chain_id),
      alias_(std::move(cfg.alias)),
      ledger_(store) {}

Result<void> VM::initialize(std::string_view genesis_json) {
    // Every signature this chain accepts and every block id it computes is
    // bound to this value, so a chain with no identity would share both with
    // every other chain that also had none. There is no default for it.
    if (chain_id_ == kEmptyId) return fail(Err::InvalidBlock, "no chain id");

    fee_policy_ = fee::FlatPolicy{fee::kMinTxFeeFloor, fee::utxo_asset_id_for(network_id_)};
    auto pol = fee::validate(fee_policy_);
    if (!pol) return pol;

    Genesis g;
    if (!genesis_json.empty()) {
        auto parsed = parse_genesis(genesis_json);
        if (!parsed) return std::unexpected(parsed.error());
        g = std::move(*parsed);
    }

    // The genesis block sits at height 0 and names itself the way every later
    // block does, chain id included.
    auto genesis_block = std::make_shared<Block>(this, kEmptyId, 0, g.timestamp,
                                                 std::vector<Transaction>{});
    last_accepted_ = genesis_block->compute_id();
    last_block_ = genesis_block;

    auto seeded = seed_genesis(g, last_accepted_);
    if (!seeded) return seeded;
    return load_state();
}

Result<void> VM::seed_genesis(const Genesis& g, const Id& genesis_block_id) {
    auto applied = store_->has(view(kGenesisMarker));
    if (!applied) return std::unexpected(applied.error());
    if (*applied) return {};

    for (const auto& [addr_hex, amount] : g.alloc) {
        Bytes b;
        if (!from_hex(addr_hex, &b) || b.size() != std::tuple_size_v<Account>) {
            return fail(Err::InvalidPayload, "alloc " + addr_hex);
        }
        Account acct{};
        std::copy(b.begin(), b.end(), acct.begin());
        auto c = ledger_.credit(acct, amount);
        if (!c) return c;
    }
    // A committee in genesis goes through exactly the check the epoch-advance
    // transaction applies, so a chain cannot be born with a committee consensus
    // would refuse to install later.
    if (!g.committee.empty()) {
        auto ok = validate_committee(g.committee, g.threshold, view(g.public_key));
        if (!ok) return ok;
        EpochRecord rec;
        rec.epoch = 0;
        rec.start_time = g.timestamp;
        rec.committee = g.committee;
        rec.committee_nil = g.committee_nil;
        rec.threshold = g.threshold;
        rec.public_key = g.public_key;
        rec.public_key_nil = g.public_key_nil;
        rec.status = EpochStatus::Active;
        auto w = store_->put(view(key(kEpochPrefix, rec.epoch)), view(marshal(rec)));
        if (!w) return w;
        w = store_->put(view(kCurrentEpochKey), view(u64_be(0)));
        if (!w) return w;
    }
    auto w = store_->put(view(key(kHeightPrefix, std::uint64_t(0))), view(genesis_block_id));
    if (!w) return w;
    w = store_->put(view(kGenesisMarker), ByteView(reinterpret_cast<const std::uint8_t*>("\x01"), 1));
    if (!w) return w;
    return store_->commit();
}

Result<std::optional<Bytes>> VM::read(ByteView k, std::size_t width) const {
    auto b = store_->get(k);
    if (!b) return std::unexpected(b.error());
    if (!b->has_value()) return std::optional<Bytes>{};
    if ((*b)->size() != width) {
        return fail(Err::InvalidPayload, "state entry is the wrong width");
    }
    return *b;
}

Result<void> VM::load_state() {
    ciphertexts_.clear();
    permits_.clear();
    decrypts_.clear();
    epochs_.clear();
    epoch_ = 0;
    skipped_ = 0;

    std::string err;
    // One unreadable ROW does not stop a node booting; an unreadable INDEX does.
    // The first says one record cannot be trusted, the second says nothing can —
    // so a row that does not decode is skipped and counted, and an iteration
    // that fails is returned.
    auto walk = [&](std::string_view prefix, const std::function<bool(std::string_view)>& take) {
        return store_->each(view(prefix), [&](ByteView, ByteView value) {
            std::string_view s(reinterpret_cast<const char*>(value.data()), value.size());
            if (!take(s)) ++skipped_;
            return true;
        });
    };

    auto r = walk(kCiphertextPrefix, [&](std::string_view s) {
        CiphertextRecord rec;
        if (!unmarshal(s, &rec, &err)) return false;
        ciphertexts_[rec.handle] = std::move(rec);
        return true;
    });
    if (!r) return r;
    r = walk(kPermitPrefix, [&](std::string_view s) {
        PermitRecord rec;
        if (!unmarshal(s, &rec, &err)) return false;
        permits_[rec.permit_id] = std::move(rec);
        return true;
    });
    if (!r) return r;
    r = walk(kDecryptPrefix, [&](std::string_view s) {
        DecryptRecord rec;
        if (!unmarshal(s, &rec, &err)) return false;
        decrypts_[rec.request_id] = std::move(rec);
        return true;
    });
    if (!r) return r;
    r = walk(kEpochPrefix, [&](std::string_view s) {
        EpochRecord rec;
        if (!unmarshal(s, &rec, &err)) return false;
        epochs_[rec.epoch] = std::move(rec);
        return true;
    });
    if (!r) return r;

    auto ep = read(view(kCurrentEpochKey), 8);
    if (!ep) return std::unexpected(ep.error());
    if (ep->has_value()) epoch_ = be_u64(view(**ep));

    auto tip = read(view(kLastAcceptedKey), 32);
    if (!tip) return std::unexpected(tip.error());
    if (tip->has_value()) {
        Id id{};
        std::copy((*tip)->begin(), (*tip)->end(), id.begin());
        last_accepted_ = id;
        auto blk = get_block(id);
        if (!blk) return std::unexpected(blk.error());
        last_block_ = *blk;
        height_ = last_block_->height();
    }
    return {};
}

// ---- PUBLIC state accessors ---------------------------------------------------

const CiphertextRecord* VM::ciphertext(const Id& handle) const {
    auto it = ciphertexts_.find(handle);
    return it == ciphertexts_.end() ? nullptr : &it->second;
}

const PermitRecord* VM::permit(const Id& id) const {
    auto it = permits_.find(id);
    return it == permits_.end() ? nullptr : &it->second;
}

const DecryptRecord* VM::decrypt(const Id& id) const {
    auto it = decrypts_.find(id);
    return it == decrypts_.end() ? nullptr : &it->second;
}

const EpochRecord* VM::epoch(std::uint64_t n) const {
    auto it = epochs_.find(n);
    return it == epochs_.end() ? nullptr : &it->second;
}

EpochRecord VM::current_epoch() const {
    auto it = epochs_.find(epoch_);
    if (it != epochs_.end()) return it->second;
    EpochRecord empty;
    empty.epoch = epoch_;
    empty.status = EpochStatus::Active;
    return empty;
}

std::vector<const CiphertextRecord*> VM::ciphertexts() const {
    std::vector<const CiphertextRecord*> out;
    out.reserve(ciphertexts_.size());
    for (const auto& [_, r] : ciphertexts_) out.push_back(&r);
    return out;
}

Result<void> VM::put_ciphertext(const CiphertextRecord& r) {
    auto w = store_->put(view(key(kCiphertextPrefix, view(r.handle))), view(marshal(r)));
    if (!w) return w;
    ciphertexts_[r.handle] = r;
    return {};
}

Result<void> VM::put_permit(const PermitRecord& r) {
    auto w = store_->put(view(key(kPermitPrefix, view(r.permit_id))), view(marshal(r)));
    if (!w) return w;
    permits_[r.permit_id] = r;
    return {};
}

Result<void> VM::put_decrypt(const DecryptRecord& r) {
    auto w = store_->put(view(key(kDecryptPrefix, view(r.request_id))), view(marshal(r)));
    if (!w) return w;
    decrypts_[r.request_id] = r;
    return {};
}

Result<void> VM::put_epoch(const EpochRecord& r) {
    auto w = store_->put(view(key(kEpochPrefix, r.epoch)), view(marshal(r)));
    if (!w) return w;
    epochs_[r.epoch] = r;
    return {};
}

Result<void> VM::set_current_epoch(std::uint64_t n) {
    auto w = store_->put(view(kCurrentEpochKey), view(u64_be(n)));
    if (!w) return w;
    epoch_ = n;
    return {};
}

Result<std::uint64_t> VM::nonce_of(const Account& payer) const {
    auto b = read(view(key(kNoncePrefix, view(payer))), 8);
    if (!b) return std::unexpected(b.error());
    if (!b->has_value()) return std::uint64_t(0);
    return be_u64(view(**b));
}

Result<void> VM::set_nonce(const Account& payer, std::uint64_t n) {
    return store_->put(view(key(kNoncePrefix, view(payer))), view(u64_be(n)));
}

Result<std::uint64_t> VM::balance(const Account& acct) const { return ledger_.balance(acct); }
Result<std::uint64_t> VM::burned() const { return ledger_.burned(); }

Result<Id> VM::block_id_at_height(std::uint64_t h) const {
    auto b = store_->get(view(key(kHeightPrefix, h)));
    if (!b) return std::unexpected(b.error());
    if (!b->has_value()) return fail(Err::Database, "height " + to_string(h) + " is not indexed");
    if ((*b)->size() != 32) {
        return fail(Err::InvalidPayload, "height index entry is the wrong width");
    }
    Id id{};
    std::copy((*b)->begin(), (*b)->end(), id.begin());
    return id;
}

std::string VM::dump() const {
    // The prefixes Go's replay comparison walks, in a fixed sequence, each
    // iterator returning its keys in order — so the result depends on the DATA
    // and on nothing else.
    std::string out;
    for (std::string_view prefix : {kCiphertextPrefix, kPermitPrefix, kDecryptPrefix, kEpochPrefix,
                                    kBlockPrefix, kNoncePrefix, kHeightPrefix,
                                    std::string_view("fee/")}) {
        auto walked = store_->each(view(prefix), [&](ByteView k, ByteView v) {
            out += hex(k);
            out.push_back('=');
            out += hex(v);
            out.push_back('\n');
            return true;
        });
        if (!walked) return "unreadable: " + walked.error().message();
    }
    return out;
}

Id VM::records_root() const {
    Hasher h;
    h.write("fhevm/records/");
    h.len_prefixed(view(dump()));
    return h.sum();
}

Result<HealthReport> VM::health() const {
    HealthReport out;
    EpochRecord ep = current_epoch();
    auto b = burned();
    // A ledger this node cannot read is not a ledger reporting zero burned. The
    // number would be indistinguishable from a chain that has never settled a
    // fee, which is the one reading an operator would take as fine.
    if (!b) return std::unexpected(b.error());
    out.healthy = !shutting_down_ && !ep.committee.empty();
    out.details["version"] = std::string(kVersion);
    out.details["ciphertexts"] = to_string(ciphertexts_.size());
    out.details["permits"] = to_string(permits_.size());
    out.details["decrypts"] = to_string(decrypts_.size());
    out.details["epoch"] = to_string(ep.epoch);
    out.details["committee"] = to_string(ep.committee.size());
    out.details["threshold"] = std::to_string(ep.threshold);
    out.details["height"] = to_string(height_);
    out.details["burnedNLUX"] = to_string(*b);
    return out;
}

void VM::note_revert(const Transaction& tx, const Error& why) {
    reverts_.push_back(Revert{tx.id(), why.code});
}

// ---- admission ----------------------------------------------------------------

Result<Id> VM::submit_tx(const Transaction& tx) {
    auto ok = tx.syntactic_verify();
    if (!ok) return std::unexpected(ok.error());
    ok = tx.authenticate(chain_id_);
    if (!ok) return std::unexpected(ok.error());
    auto amount = fee_for(tx);
    if (!amount) return std::unexpected(amount.error());

    if (mempool_.size() >= kMaxMempool) return fail(Err::MempoolFull);

    // The replay/order guard runs BEFORE authorization, so a replayed
    // transaction is reported as replayed whatever else is true of it.
    auto committed = nonce_of(tx.payer);
    if (!committed) return std::unexpected(committed.error());
    std::uint64_t want = *committed + 1;
    auto q = queued_.find(tx.payer);
    if (q != queued_.end() && q->second >= want) want = q->second + 1;
    if (tx.nonce != want) return fail(Err::BadNonce);

    // Refuse a second claim on an effect already queued. Without this a payer
    // could enqueue two transactions that each pass every check alone and
    // together spend a block on work only one of them can do.
    Id eff = tx.effect();
    if (claims_.count(eff) != 0) return fail(Err::DuplicateEffect);

    ok = check_auth(tx, *this, clock_.now());
    if (!ok) return std::unexpected(ok.error());
    ok = fee::can_pay(ledger_, tx.payer, *amount);
    if (!ok) return std::unexpected(ok.error());

    claims_.insert(eff);
    queued_[tx.payer] = tx.nonce;
    mempool_.push_back(tx);
    return tx.id();
}

void VM::release(const std::vector<Transaction>& txs) {
    if (txs.empty()) return;
    std::set<Id> accepted;
    for (const auto& tx : txs) accepted.insert(tx.id());

    std::vector<Transaction> kept;
    kept.reserve(mempool_.size());
    for (const auto& tx : mempool_) {
        if (accepted.count(tx.id()) == 0) kept.push_back(tx);
    }
    mempool_ = std::move(kept);
    claims_.clear();
    queued_.clear();
    for (const auto& tx : mempool_) {
        claims_.insert(tx.effect());
        auto it = queued_.find(tx.payer);
        if (it == queued_.end() || tx.nonce > it->second) queued_[tx.payer] = tx.nonce;
    }
}

// ---- blocks -------------------------------------------------------------------

Result<std::shared_ptr<Block>> VM::build_block() {
    if (shutting_down_) return fail(Err::VMShutdown);
    if (!last_block_) return fail(Err::NoParentBlock);

    // Chain time never runs backwards, so a proposer whose clock has not moved
    // since its own tip advances by the smallest step instead of proposing a
    // block its own verify would refuse. A clock so far behind that even that
    // step lands outside the skew allowance is a broken clock: such a node
    // declines to propose rather than produce a block nobody can verify.
    std::int64_t ts = clock_.now();
    if (ts <= last_block_->timestamp()) ts = last_block_->timestamp() + 1;
    if (ts > clock_.now() + kMaxFutureSkew) return fail(Err::ClockBehind);

    if (mempool_.empty()) return fail(Err::NoPendingTxs);

    // Selection stops at whichever bound comes first: the transaction count a
    // block may carry, or the bytes it may occupy on the wire. Stopping only on
    // the count would let 1024 ordinary transactions — each carrying an
    // ML-DSA-65 key and signature — build a 5 MB block this node's own parser
    // refuses.
    Batch batch(*this);
    std::vector<Transaction> picked;
    picked.reserve(mempool_.size());
    std::size_t size = empty_block_size();
    for (const auto& tx : mempool_) {
        if (picked.size() == kMaxBlockTxs) break;
        std::size_t grown = size + tx.bytes().size() + kTxEntry;
        if (grown > kMaxBlockSize) break;
        if (!batch.admit(tx)) continue;
        picked.push_back(tx);
        size = grown;
    }
    if (picked.empty()) return fail(Err::NoPendingTxs);

    auto blk = std::make_shared<Block>(this, last_accepted_, last_block_->height() + 1, ts,
                                       std::move(picked));
    track_verified(blk);
    return blk;
}

Result<std::shared_ptr<Block>> VM::parse_block(ByteView b) {
    auto h = parse_block_bytes(b);
    if (!h) return std::unexpected(h.error());
    return std::make_shared<Block>(this, h->parent, h->height, h->timestamp,
                                   std::move(h->transactions));
}

Result<std::shared_ptr<Block>> VM::get_block(const Id& id) const {
    auto it = pending_.find(id);
    if (it != pending_.end()) return it->second;
    if (last_block_ && Id(last_block_->id()) == id) return last_block_;
    auto b = store_->get(view(key(kBlockPrefix, view(id))));
    if (!b) return std::unexpected(b.error());
    if (!b->has_value()) return fail(Err::Database, "block " + hex(view(id)) + " is not stored");
    auto h = parse_block_bytes(view(**b));
    if (!h) return std::unexpected(h.error());
    return std::make_shared<Block>(const_cast<VM*>(this), h->parent, h->height, h->timestamp,
                                   std::move(h->transactions));
}

Result<void> VM::on_tip(const Id& parent) const {
    if (parent == last_accepted_) return {};
    if (pending_.count(parent) != 0) return {};
    return fail(Err::NotOnTip, "parent " + hex(view(parent)) + " is neither the tip nor in flight");
}

void VM::track_verified(const std::shared_ptr<Block>& b) {
    if (b->height() <= height_) return;
    // Prune, because nothing else will: the engine may drop a block it never
    // accepts and never rejects, so a tracker that only ever grew would leak.
    // Anything at or below the last accepted height is already decided or
    // orphaned, which bounds this to the blocks actually in flight above it.
    for (auto it = pending_.begin(); it != pending_.end();) {
        if (it->second->height() <= height_) {
            it = pending_.erase(it);
        } else {
            ++it;
        }
    }
    pending_[Id(b->id())] = b;
}

void VM::set_accepted(const std::shared_ptr<Block>& b) {
    last_accepted_ = Id(b->id());
    last_block_ = b;
    height_ = b->height();
}

void VM::drop_pending(const Id& id) { pending_.erase(id); }

// ---- the node's seam ----------------------------------------------------------

std::shared_ptr<lux::node::Block> VM::build() {
    auto b = build_block();
    if (!b) return nullptr;
    return *b;
}

std::shared_ptr<lux::node::Block> VM::parse(std::span<const std::uint8_t> b) {
    auto blk = parse_block(b);
    if (!blk) return nullptr;
    return *blk;
}

std::shared_ptr<lux::node::Block> VM::get(const lux::node::Id& id) const {
    auto b = get_block(id);
    if (!b) return nullptr;
    return *b;
}

// ---- the batch ------------------------------------------------------------------

Result<void> Batch::admit(const Transaction& tx) {
    auto ok = tx.syntactic_verify();
    if (!ok) return ok;
    ok = tx.authenticate(vm_->chain_id());
    if (!ok) return ok;

    // Replay/order guard: a payer's nonces are consecutive from its committed
    // one, counting the transactions already taken from it in this block.
    std::uint64_t want = 0;
    auto seen = nonce_.find(tx.payer);
    if (seen != nonce_.end()) {
        want = seen->second;
    } else {
        auto committed = vm_->nonce_of(tx.payer);
        if (!committed) return std::unexpected(committed.error());
        want = *committed + 1;
    }
    if (tx.nonce != want) return fail(Err::BadNonce);

    Id eff = tx.effect();
    if (claimed_.count(eff) != 0) return fail(Err::DuplicateEffect);

    auto gas_used = gas_for(tx);
    if (!gas_used) return std::unexpected(gas_used.error());
    if (*gas_used > tx.gas_limit) return fail(Err::OutOfGas, "gas exceeds the payer's limit");
    auto amount = fee::cost(*gas_used, kGasPrice);
    if (!amount) return std::unexpected(amount.error());
    auto bal = vm_->ledger().balance(tx.payer);
    if (!bal) return std::unexpected(bal.error());
    std::uint64_t already = 0;
    auto s = spent_.find(tx.payer);
    if (s != spent_.end()) already = s->second;
    std::uint64_t next = already + *amount;
    if (next < already || *bal < next) return fail(Err::InsufficientFunds);

    nonce_[tx.payer] = want + 1;
    spent_[tx.payer] = next;
    claimed_.insert(eff);
    return {};
}

}  // namespace lux::fhevm
