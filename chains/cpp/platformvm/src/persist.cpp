// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// persist.cpp — what the P-chain remembers after the process that accepted it
// is gone.
//
// Until this file the P-chain's state was maps and nothing else: it forgot the
// whole validator set on exit, which is not a chain. A node that comes back
// without its stakers does not know who may vote, and one that comes back
// without its UTXO set has forgotten what has been spent — the same failure as
// a nullifier set that resets, where a thing already spent can be spent again.
//
// TWO FUNCTIONS AND ONE SHAPE. `rows` renders the whole state as the store's
// byte-keyed map and `load` reads it back. The on-disk shape is spelled in one
// place, so a field that is written and not read, or read and not written, is a
// diff of two adjacent functions rather than a bug that only appears after a
// restart.
//
// KEYS. Every key begins with a one-byte family tag, and all of them are
// distinct, because a flat store needs its families disjoint: a scan for one
// family must not answer with another. Where order matters the key carries it
// big-endian, so the store's ascending byte order IS the order the chain wants.
//
// VALUES ARE ZAP, like everything else here. Where a type already has a wire
// form the chain agreed on — a UTXO, a signed transaction, an owner, an expiry
// — that form is what is stored. Inventing a second encoding for a UTXO would
// be inventing a second UTXO.

#include "lux/platformvm/state.hpp"

#include "lux/core/zap.hpp"
#include "lux/platformvm/l1.hpp"

#include <algorithm>
#include <utility>

namespace lux::platformvm::state {
namespace {

namespace zap = lux::core::zap;

// ── the families

constexpr std::uint8_t kTagMeta = 'a';      //                        → the clock and the fee position
constexpr std::uint8_t kTagUtxo = 'u';      // + utxo id (32)         → UTXO wire bytes
constexpr std::uint8_t kTagReward = 'r';    // + tx id (32)           → the reward UTXOs of that tx
constexpr std::uint8_t kTagTx = 't';        // + tx id (32)           → status and signed bytes
constexpr std::uint8_t kTagNetwork = 'n';   // + network id (32)      → membership, and the owner if any
constexpr std::uint8_t kTagConvert = 'v';   // + network id (32)      → what it became when promoted
constexpr std::uint8_t kTagTransform = 'f'; // + network id (32)      → the transformation tx
constexpr std::uint8_t kTagChain = 'c';     // + network (32)+tx (32) → a chain created on it
constexpr std::uint8_t kTagSupply = 'p';    // + network id (32)      → its current supply
constexpr std::uint8_t kTagDelegatee = 'd'; // + net (32)+node (20)   → rewards a validator has accrued
constexpr std::uint8_t kTagStaker = 'k';    // + tx id (32)           → one staker, and which set it is in
constexpr std::uint8_t kTagL1 = 'l';        // + validation id (32)   → an L1 validator
constexpr std::uint8_t kTagExpiry = 'e';    // + be64(time)+id (32)   → nothing; the key is the value

// ── the meta object: the answers that are not a row of their own

constexpr int kMetaTimestamp = 0;
constexpr int kMetaAccruedFees = 8;
constexpr int kMetaCapacity = 16;
constexpr int kMetaExcess = 24;
constexpr int kMetaL1Excess = 32;
constexpr int kMetaSize = 40;

// ── the staker object

constexpr int kStakerSet = 0;        // 0 = current, 1 = pending
constexpr int kStakerRole = 1;       // 0 = validator, 1 = delegator
constexpr int kStakerPriority = 2;
constexpr int kStakerHasKey = 3;
constexpr int kStakerWeight = 8;
constexpr int kStakerStart = 16;
constexpr int kStakerEnd = 24;
constexpr int kStakerReward = 32;
constexpr int kStakerNext = 40;
constexpr int kStakerTxId = 48;      // 32 inline
constexpr int kStakerChainId = 80;   // 32 inline
constexpr int kStakerNodeId = 112;   // 20 inline
constexpr int kStakerKey = 132;      // the BLS key, when there is one
constexpr int kStakerSize = 140;

// ── the L1 validator object

constexpr int kL1StartTime = 0;
constexpr int kL1Weight = 8;
constexpr int kL1MinNonce = 16;
constexpr int kL1EndFee = 24;
constexpr int kL1ValidationId = 32;  // 32 inline
constexpr int kL1ChainId = 64;       // 32 inline
constexpr int kL1NodeId = 96;        // 20 inline
constexpr int kL1PublicKey = 116;
constexpr int kL1BalanceOwner = 124;
constexpr int kL1DeactivationOwner = 132;
constexpr int kL1Size = 140;

// ── the network row, the conversion row, the transaction row, the scalar row

constexpr int kNetHasOwner = 0;
constexpr int kNetOwner = 8;
constexpr int kNetSize = 16;

constexpr int kConvChainId = 0;   // 32 inline
constexpr int kConvValidId = 32;  // 32 inline
constexpr int kConvAddr = 64;
constexpr int kConvSize = 72;

constexpr int kTxStatus = 0;
constexpr int kTxBytes = 8;
constexpr int kTxSize = 16;

constexpr int kScalarValue = 0;
constexpr int kScalarSize = 8;

// A list of byte runs: the lengths, then one blob. The same shape the store's
// own record uses, for the same reason — a length list and a blob beat a
// nesting of messages nobody needs to seek into.
constexpr int kRunLens = 0;
constexpr int kRunBlob = 8;
constexpr int kRunSize = 16;
constexpr std::uint32_t kU32Stride = 4;

// ── keys

Bytes key(std::uint8_t tag) { return Bytes{tag}; }

Bytes key(std::uint8_t tag, ByteView a) {
    Bytes k;
    k.reserve(1 + a.size());
    k.push_back(tag);
    k.insert(k.end(), a.begin(), a.end());
    return k;
}

Bytes key(std::uint8_t tag, ByteView a, ByteView b) {
    Bytes k = key(tag, a);
    k.insert(k.end(), b.begin(), b.end());
    return k;
}

void put_be64(Bytes& k, std::uint64_t v) {
    for (int i = 7; i >= 0; --i) k.push_back(std::uint8_t(v >> (8 * i)));
}

// ── values

Bytes scalar_bytes(std::uint64_t v) {
    zap::Builder b(zap::kHeaderSize + kScalarSize);
    auto ob = b.start_object(kScalarSize);
    ob.set_u64(kScalarValue, v);
    ob.finish_as_root();
    return b.finish();
}

Bytes meta_bytes(std::uint64_t timestamp, std::uint64_t fees, const gas::State& gas,
                 std::uint64_t l1_excess) {
    zap::Builder b(zap::kHeaderSize + kMetaSize);
    auto ob = b.start_object(kMetaSize);
    ob.set_u64(kMetaTimestamp, timestamp);
    ob.set_u64(kMetaAccruedFees, fees);
    ob.set_u64(kMetaCapacity, gas.capacity);
    ob.set_u64(kMetaExcess, gas.excess);
    ob.set_u64(kMetaL1Excess, l1_excess);
    ob.finish_as_root();
    return b.finish();
}

Bytes run_bytes(const std::vector<Bytes>& runs) {
    Bytes blob;
    std::vector<std::uint32_t> lens;
    lens.reserve(runs.size());
    for (const auto& r : runs) {
        lens.push_back(std::uint32_t(r.size()));
        blob.insert(blob.end(), r.begin(), r.end());
    }
    zap::Builder b(zap::kHeaderSize + kRunSize + int(blob.size()) + 4 * int(lens.size()) + 64);
    auto lb = b.start_list(int(kU32Stride));
    for (std::uint32_t n : lens) lb.add_u32(n);
    const int lens_off = lb.finish().first;
    auto ob = b.start_object(kRunSize);
    ob.set_list(kRunLens, lens_off, int(lens.size()));
    ob.set_bytes(kRunBlob, view(blob));
    ob.finish_as_root();
    return b.finish();
}

std::vector<Bytes> read_runs(const zap::Object& o) {
    const auto lens = o.list_stride(kRunLens, kU32Stride);
    const auto blob = o.bytes(kRunBlob);
    std::vector<Bytes> out;
    std::size_t pos = 0;
    for (int i = 0; i < lens.len(); ++i) {
        const std::size_t n = lens.u32(i);
        if (pos + n > blob.size()) return {};
        out.emplace_back(blob.begin() + std::ptrdiff_t(pos), blob.begin() + std::ptrdiff_t(pos + n));
        pos += n;
    }
    return out;
}

Bytes staker_bytes(const Staker& s, std::uint8_t set, std::uint8_t role) {
    zap::Builder b(zap::kHeaderSize + kStakerSize + 64);
    auto ob = b.start_object(kStakerSize);
    ob.set_u8(kStakerSet, set);
    ob.set_u8(kStakerRole, role);
    ob.set_u8(kStakerPriority, static_cast<std::uint8_t>(s.priority));
    ob.set_u8(kStakerHasKey, s.public_key ? 1 : 0);
    ob.set_u64(kStakerWeight, s.weight);
    ob.set_u64(kStakerStart, s.start_time);
    ob.set_u64(kStakerEnd, s.end_time);
    ob.set_u64(kStakerReward, s.potential_reward);
    ob.set_u64(kStakerNext, s.next_time);
    ob.set_bytes_fixed(kStakerTxId, view(s.tx_id));
    ob.set_bytes_fixed(kStakerChainId, view(s.chain_id));
    ob.set_bytes_fixed(kStakerNodeId, view(s.node_id));
    if (s.public_key) ob.set_bytes(kStakerKey, view(*s.public_key));
    ob.finish_as_root();
    return b.finish();
}

Bytes l1_bytes(const l1::Validator& v) {
    zap::Builder b(zap::kHeaderSize + kL1Size + 256);
    auto ob = b.start_object(kL1Size);
    ob.set_u64(kL1StartTime, v.start_time);
    ob.set_u64(kL1Weight, v.weight);
    ob.set_u64(kL1MinNonce, v.min_nonce);
    ob.set_u64(kL1EndFee, v.end_accumulated_fee);
    ob.set_bytes_fixed(kL1ValidationId, view(v.validation_id));
    ob.set_bytes_fixed(kL1ChainId, view(v.chain_id));
    ob.set_bytes_fixed(kL1NodeId, view(v.node_id));
    ob.set_bytes(kL1PublicKey, view(v.public_key));
    ob.set_bytes(kL1BalanceOwner, view(v.remaining_balance_owner));
    ob.set_bytes(kL1DeactivationOwner, view(v.deactivation_owner));
    ob.finish_as_root();
    return b.finish();
}

Bytes network_bytes(const std::optional<txs::Owner>& owner) {
    Bytes marshalled;
    if (owner) marshalled = txs::marshal_owner(*owner);
    zap::Builder b(zap::kHeaderSize + kNetSize + int(marshalled.size()) + 32);
    auto ob = b.start_object(kNetSize);
    ob.set_u8(kNetHasOwner, owner ? 1 : 0);
    if (owner) ob.set_bytes(kNetOwner, view(marshalled));
    ob.finish_as_root();
    return b.finish();
}

Bytes conversion_bytes(const NetToL1Conversion& c) {
    zap::Builder b(zap::kHeaderSize + kConvSize + int(c.addr.size()) + 32);
    auto ob = b.start_object(kConvSize);
    ob.set_bytes_fixed(kConvChainId, view(c.chain_id));
    ob.set_bytes_fixed(kConvValidId, view(c.validation_id));
    ob.set_bytes(kConvAddr, view(c.addr));
    ob.finish_as_root();
    return b.finish();
}

Bytes tx_bytes(const txs::Tx& tx, status::Status st) {
    zap::Builder b(zap::kHeaderSize + kTxSize + int(tx.bytes.size()) + 64);
    auto ob = b.start_object(kTxSize);
    ob.set_u32(kTxStatus, static_cast<std::uint32_t>(st));
    ob.set_bytes(kTxBytes, view(tx.bytes));
    ob.finish_as_root();
    return b.finish();
}

// A parsed root, or nothing. Every reader goes through it, so a corrupt row is
// reported the same way wherever it appears.
std::optional<zap::Object> root_of(zap::Message& msg, ByteView value, std::string* err) {
    if (!zap::Message::parse(value, &msg, err)) return std::nullopt;
    return msg.root();
}

Id id_at(const zap::Object& o, int off) { return id_from(o.bytes_fixed_slice(off, int(kIdLen))); }
NodeId node_id_at(const zap::Object& o, int off) {
    return node_id_from(o.bytes_fixed_slice(off, int(kNodeIdLen)));
}
Bytes bytes_at(const zap::Object& o, int off) {
    const auto v = o.bytes(off);
    return Bytes(v.begin(), v.end());
}

}  // namespace

// ================= writing =================

std::map<Bytes, Bytes> State::rows() const {
    std::map<Bytes, Bytes> out;

    out[key(kTagMeta)] = meta_bytes(timestamp_, accrued_fees_, fee_state_, l1_excess_);

    for (const auto& [id, u] : utxos_) out[key(kTagUtxo, view(id))] = u.wire_bytes();

    for (const auto& [tx_id, us] : reward_utxos_) {
        std::vector<Bytes> runs;
        runs.reserve(us.size());
        for (const auto& u : us) runs.push_back(u.wire_bytes());
        out[key(kTagReward, view(tx_id))] = run_bytes(runs);
    }

    for (const auto& [tx_id, held] : txs_)
        out[key(kTagTx, view(tx_id))] = tx_bytes(held.first, held.second);

    for (const auto& net : networks_) {
        std::optional<txs::Owner> owner;
        if (auto it = net_owners_.find(net); it != net_owners_.end()) owner = it->second;
        out[key(kTagNetwork, view(net))] = network_bytes(owner);
    }

    for (const auto& [net, c] : conversions_) out[key(kTagConvert, view(net))] = conversion_bytes(c);

    for (const auto& [net, tx] : transformations_)
        out[key(kTagTransform, view(net))] = tx.bytes;

    for (const auto& [net, created] : chains_)
        for (const auto& tx : created)
            out[key(kTagChain, view(net), view(tx.tx_id))] = tx.bytes;

    for (const auto& [net, amount] : supply_) out[key(kTagSupply, view(net))] = scalar_bytes(amount);

    for (const auto& [net, nodes] : delegatee_rewards_)
        for (const auto& [node, amount] : nodes)
            out[key(kTagDelegatee, view(net), view(node))] = scalar_bytes(amount);

    // Every staker of both sets, keyed by the transaction that created it —
    // which is the one name a validator and a delegator both have, and which is
    // unique across all four sets.
    for (const auto& s : current_.staker_list()) {
        const bool is_validator = current_.get_validator(s.chain_id, s.node_id).has_value() &&
                                  current_.get_validator(s.chain_id, s.node_id)->tx_id == s.tx_id;
        out[key(kTagStaker, view(s.tx_id))] = staker_bytes(s, 0, is_validator ? 0 : 1);
    }
    for (const auto& s : pending_.staker_list()) {
        const bool is_validator = pending_.get_validator(s.chain_id, s.node_id).has_value() &&
                                  pending_.get_validator(s.chain_id, s.node_id)->tx_id == s.tx_id;
        out[key(kTagStaker, view(s.tx_id))] = staker_bytes(s, 1, is_validator ? 0 : 1);
    }

    for (const auto& [id, v] : l1_validators_) out[key(kTagL1, view(id))] = l1_bytes(v);

    for (const auto& e : expiries_) {
        Bytes k{kTagExpiry};
        put_be64(k, e.timestamp);
        k.insert(k.end(), e.validation_id.begin(), e.validation_id.end());
        out[k] = Bytes{};
    }

    return out;
}

Status State::commit() {
    const std::map<Bytes, Bytes> want = rows();

    // Whatever the store holds that the state no longer does is gone. This is
    // what makes a REMOVAL durable — a validator whose stake ended, a UTXO that
    // was spent — and forgetting it is the failure that lets a spent thing be
    // spent again.
    std::vector<Bytes> gone;
    store_->each({}, [&](ByteView k, ByteView) {
        Bytes have(k.begin(), k.end());
        if (want.find(have) == want.end()) gone.push_back(std::move(have));
        return true;
    });
    for (const auto& k : gone) store_->erase(view(k));

    // Only rows that actually differ reach the disk; the rest are already
    // exactly these bytes.
    for (const auto& [k, v] : want) {
        const auto held = store_->get(view(k));
        if (!held || *held != v) store_->put(view(k), view(v));
    }

    if (auto r = store_->commit(); !r)
        return fail(Err::NotFound, "commit state: " + r.error());
    return ok();
}

// ================= reading =================

Status State::load() {
    timestamp_ = 0;
    accrued_fees_ = 0;
    fee_state_ = {};
    l1_excess_ = 0;
    supply_.clear();
    utxos_.clear();
    reward_utxos_.clear();
    current_ = {};
    pending_ = {};
    l1_validators_.clear();
    expiries_.clear();
    delegatee_rewards_.clear();
    networks_.clear();
    net_owners_.clear();
    conversions_.clear();
    transformations_.clear();
    chains_.clear();
    chain_names_.clear();
    txs_.clear();

    std::string bad;
    auto note = [&bad](const std::string& what, const std::string& why) {
        if (bad.empty()) bad = what + ": " + why;
        return false;
    };

    if (auto m = store_->get(view(key(kTagMeta)))) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, view(*m), &err);
        if (!root) return fail(Err::NotFound, "load state metadata: " + err);
        timestamp_ = root->u64(kMetaTimestamp);
        accrued_fees_ = root->u64(kMetaAccruedFees);
        fee_state_ = gas::State{root->u64(kMetaCapacity), root->u64(kMetaExcess)};
        l1_excess_ = root->u64(kMetaL1Excess);
    }

    store_->each(view(key(kTagUtxo)), [&](ByteView k, ByteView v) {
        auto u = UTXO::from_wire_bytes(v);
        if (!u) return note("load utxo " + hex(k.subspan(1)), u.error().message());
        utxos_[u->id()] = *u;
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagReward)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load reward utxos " + hex(k.subspan(1)), err);
        const Id tx_id = id_from(k.subspan(1));
        for (const auto& run : read_runs(*root)) {
            auto u = UTXO::from_wire_bytes(view(run));
            if (!u) return note("load reward utxo of " + hex(tx_id), u.error().message());
            reward_utxos_[tx_id].push_back(*u);
        }
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagTx)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load tx " + hex(k.subspan(1)), err);
        auto tx = txs::parse(root->bytes(kTxBytes));
        if (!tx) return note("load tx " + hex(k.subspan(1)), tx.error().message());
        txs_.insert_or_assign(tx->tx_id,
                              std::make_pair(*tx, static_cast<status::Status>(root->u32(kTxStatus))));
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagNetwork)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load network " + hex(k.subspan(1)), err);
        const Id net = id_from(k.subspan(1));
        networks_.insert(net);
        if (root->u8(kNetHasOwner) != 0) {
            auto o = txs::unmarshal_owner(root->bytes(kNetOwner));
            if (!o) return note("load network owner " + hex(net), o.error().message());
            net_owners_[net] = *o;
        }
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagConvert)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load conversion " + hex(k.subspan(1)), err);
        NetToL1Conversion c;
        c.chain_id = id_at(*root, kConvChainId);
        c.validation_id = id_at(*root, kConvValidId);
        c.addr = bytes_at(*root, kConvAddr);
        conversions_[id_from(k.subspan(1))] = std::move(c);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagTransform)), [&](ByteView k, ByteView v) {
        auto tx = txs::parse(v);
        if (!tx) return note("load transformation " + hex(k.subspan(1)), tx.error().message());
        add_network_transformation(*tx);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    // add_chain rebuilds the taken-name set as it goes, so the names are never
    // a second thing that could disagree with the chains they came from.
    store_->each(view(key(kTagChain)), [&](ByteView k, ByteView v) {
        auto tx = txs::parse(v);
        if (!tx) return note("load chain " + hex(k.subspan(1)), tx.error().message());
        add_chain(*tx);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagSupply)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load supply " + hex(k.subspan(1)), err);
        supply_[id_from(k.subspan(1))] = root->u64(kScalarValue);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    // Stakers before delegatee rewards: putting a current validator CREATES its
    // reward ledger at zero, so a reward read first would be overwritten.
    store_->each(view(key(kTagStaker)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load staker " + hex(k.subspan(1)), err);
        Staker s;
        s.tx_id = id_at(*root, kStakerTxId);
        s.chain_id = id_at(*root, kStakerChainId);
        s.node_id = node_id_at(*root, kStakerNodeId);
        s.weight = root->u64(kStakerWeight);
        s.start_time = root->u64(kStakerStart);
        s.end_time = root->u64(kStakerEnd);
        s.potential_reward = root->u64(kStakerReward);
        s.next_time = root->u64(kStakerNext);
        s.priority = static_cast<Priority>(root->u8(kStakerPriority));
        if (root->u8(kStakerHasKey) != 0) {
            const auto pk = root->bytes(kStakerKey);
            if (pk.size() != signer::kPublicKeyLen)
                return note("load staker " + hex(s.tx_id), "public key is the wrong width");
            signer::PublicKeyBytes key{};
            std::copy(pk.begin(), pk.end(), key.begin());
            s.public_key = key;
        }
        const bool pending = root->u8(kStakerSet) != 0;
        const bool delegator = root->u8(kStakerRole) != 0;
        if (pending) {
            delegator ? load_pending_delegator(s) : load_pending_validator(s);
        } else {
            delegator ? load_current_delegator(s) : load_current_validator(s);
        }
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagDelegatee)), [&](ByteView k, ByteView v) {
        if (k.size() != 1 + kIdLen + kNodeIdLen)
            return note("load delegatee reward", "key is not a network and a node");
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load delegatee reward", err);
        const Id net = id_from(k.subspan(1, kIdLen));
        const NodeId node = node_id_from(k.subspan(1 + kIdLen, kNodeIdLen));
        delegatee_rewards_[net][node] = root->u64(kScalarValue);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagL1)), [&](ByteView k, ByteView v) {
        zap::Message msg;
        std::string err;
        auto root = root_of(msg, v, &err);
        if (!root) return note("load l1 validator " + hex(k.subspan(1)), err);
        l1::Validator val;
        val.validation_id = id_at(*root, kL1ValidationId);
        val.chain_id = id_at(*root, kL1ChainId);
        val.node_id = node_id_at(*root, kL1NodeId);
        val.public_key = bytes_at(*root, kL1PublicKey);
        val.remaining_balance_owner = bytes_at(*root, kL1BalanceOwner);
        val.deactivation_owner = bytes_at(*root, kL1DeactivationOwner);
        val.start_time = root->u64(kL1StartTime);
        val.weight = root->u64(kL1Weight);
        val.min_nonce = root->u64(kL1MinNonce);
        val.end_accumulated_fee = root->u64(kL1EndFee);
        l1_validators_[val.validation_id] = std::move(val);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    store_->each(view(key(kTagExpiry)), [&](ByteView k, ByteView) {
        auto e = l1::ExpiryEntry::unmarshal(k.subspan(1));
        if (!e) return note("load expiry", e.error().message());
        expiries_.insert(*e);
        return true;
    });
    if (!bad.empty()) return fail(Err::NotFound, bad);

    return ok();
}

}  // namespace lux::platformvm::state
