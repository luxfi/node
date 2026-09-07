// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// records.cpp — one record, as bytes.

#include "lux/platformvm/records.hpp"

#include "lux/platformvm/zap.hpp"

#include <cstring>

namespace lux::platformvm::records {
namespace {

void append(Bytes& out, std::span<const std::uint8_t> b) { out.insert(out.end(), b.begin(), b.end()); }

void append_be64(Bytes& out, std::uint64_t v) {
    for (int i = 7; i >= 0; --i) out.push_back(static_cast<std::uint8_t>(v >> (8 * i)));
}

// ── the field layouts
//
// A record is a single ZAP object. The offsets are spelled once, here, and the
// encoder and the decoder both read them from the same place.

// Meta: timestamp @0, accrued fees @8, gas capacity @16, gas excess @24,
// L1 fee excess @32.
constexpr std::int64_t kMetaTime = 0, kMetaFees = 8, kMetaCapacity = 16, kMetaExcess = 24,
                       kMetaL1Excess = 32, kMetaSize = 40;

// LastAccepted: id 32B @0, height @32.
constexpr std::int64_t kLaId = 0, kLaHeight = 32, kLaSize = 40;

// A lone number.
constexpr std::int64_t kNumValue = 0, kNumSize = 8;

// Staker: tx 32B @0, node 20B @32, chain 32B @52, weight @84, start @92,
// end @100, reward @108, next @116, priority @124, has-key @125, key 48B @126.
constexpr std::int64_t kStTx = 0, kStNode = 32, kStChain = 52, kStWeight = 84, kStStart = 92,
                       kStEnd = 100, kStReward = 108, kStNext = 116, kStPriority = 124,
                       kStHasKey = 125, kStKey = 126, kStSize = 174;

// A run of UTXOs: their lengths as a u32 list @0, their wire bytes @8.
constexpr std::int64_t kUsLens = 0, kUsBlob = 8, kUsSize = 16;

// Owner: locktime @0, threshold @8, addresses @16 (a 20-byte-stride list).
constexpr std::int64_t kOwLocktime = 0, kOwThreshold = 8, kOwAddrs = 16, kOwSize = 24;

// Conversion: chain 32B @0, validation 32B @32, address bytes @64.
constexpr std::int64_t kCvChain = 0, kCvValidation = 32, kCvAddr = 64, kCvSize = 72;

// An L1 validator: validation 32B @0, chain 32B @32, node 20B @64, start @84,
// weight @92, nonce @100, fee mark @108, key @116, balance owner @124,
// deactivation owner @132.
constexpr std::int64_t kLvValidation = 0, kLvChain = 32, kLvNode = 64, kLvStart = 84, kLvWeight = 92,
                       kLvNonce = 100, kLvFee = 108, kLvKey = 116, kLvBalanceOwner = 124,
                       kLvDeactivationOwner = 132, kLvSize = 140;

// A transaction and what the chain decided about it: its own bytes @0,
// status @8.
constexpr std::int64_t kTxBytes = 0, kTxStatus = 8, kTxSize = 16;

// One entry's move over one height: the name @0, decrease @32, had-removal @33,
// amount @40, key before @48, key after @56.
constexpr std::int64_t kChValidation = 0, kChDecrease = 32, kChRemoval = 33, kChAmount = 40,
                       kChBefore = 48, kChAfter = 56, kChSize = 64;

zap::Object root_of(std::span<const std::uint8_t> b, bool& ok_out) {
    const auto m = zap::Message::parse(b);
    ok_out = m.has_value();
    if (!ok_out) return {};
    return m->root();
}

Bytes bytes_of(std::span<const std::uint8_t> b) { return Bytes(b.begin(), b.end()); }

}  // namespace

// ── keys

Bytes key(Tag t) { return Bytes{static_cast<std::uint8_t>(t)}; }

Bytes key(Tag t, const Id& a) {
    Bytes k{static_cast<std::uint8_t>(t)};
    append(k, a.span());
    return k;
}

Bytes key(Tag t, const Id& a, const NodeId& n) {
    Bytes k = key(t, a);
    append(k, n.span());
    return k;
}

Bytes key(Tag t, const Id& a, const NodeId& n, const Id& b) {
    Bytes k = key(t, a, n);
    append(k, b.span());
    return k;
}

Bytes key(Tag t, const Id& a, const Id& b) {
    Bytes k = key(t, a);
    append(k, b.span());
    return k;
}

Bytes key(Tag t, std::span<const std::uint8_t> raw) {
    Bytes k{static_cast<std::uint8_t>(t)};
    append(k, raw);
    return k;
}

Bytes history_key(std::uint64_t height, const Id& chain, const NodeId& node) {
    Bytes k{static_cast<std::uint8_t>(Tag::History)};
    append_be64(k, height);
    append(k, chain.span());
    append(k, node.span());
    return k;
}

Bytes block_key(std::uint64_t height, const Id& id) {
    Bytes k{static_cast<std::uint8_t>(Tag::Block)};
    append_be64(k, height);
    append(k, id.span());
    return k;
}

// ── values

Bytes encode_meta(const state::Chain& s) {
    zap::Builder b(zap::kHeaderSize + kMetaSize);
    auto ob = b.start_object(kMetaSize);
    ob.set_u64(kMetaTime, s.timestamp());
    ob.set_u64(kMetaFees, s.accrued_fees());
    ob.set_u64(kMetaCapacity, s.fee_state().capacity);
    ob.set_u64(kMetaExcess, s.fee_state().excess);
    ob.set_u64(kMetaL1Excess, s.l1_validator_excess());
    ob.finish_as_root();
    return b.finish();
}

Status decode_meta(std::span<const std::uint8_t> b, state::MemState& into) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "meta is not a zap message");
    into.set_timestamp(r.u64(kMetaTime));
    into.set_accrued_fees(r.u64(kMetaFees));
    into.set_fee_state(gas::State{r.u64(kMetaCapacity), r.u64(kMetaExcess)});
    into.set_l1_validator_excess(r.u64(kMetaL1Excess));
    return ok();
}

Bytes encode_last_accepted(const Id& id, std::uint64_t height) {
    zap::Builder b(zap::kHeaderSize + kLaSize);
    auto ob = b.start_object(kLaSize);
    ob.set_bytes_fixed(kLaId, id.span());
    ob.set_u64(kLaHeight, height);
    ob.finish_as_root();
    return b.finish();
}

Result<std::pair<Id, std::uint64_t>> decode_last_accepted(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "the last accepted record is not a zap message");
    return std::make_pair(Id::from(r.bytes_fixed(kLaId, kIdLen)), r.u64(kLaHeight));
}

Bytes encode_u64(std::uint64_t v) {
    zap::Builder b(zap::kHeaderSize + kNumSize);
    auto ob = b.start_object(kNumSize);
    ob.set_u64(kNumValue, v);
    ob.finish_as_root();
    return b.finish();
}

Result<std::uint64_t> decode_u64(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "a number is not a zap message");
    return r.u64(kNumValue);
}

Bytes encode_staker(const state::Staker& s) {
    zap::Builder b(zap::kHeaderSize + kStSize);
    auto ob = b.start_object(kStSize);
    ob.set_bytes_fixed(kStTx, s.tx_id.span());
    ob.set_bytes_fixed(kStNode, s.node_id.span());
    ob.set_bytes_fixed(kStChain, s.chain_id.span());
    ob.set_u64(kStWeight, s.weight);
    ob.set_u64(kStStart, s.start_time);
    ob.set_u64(kStEnd, s.end_time);
    ob.set_u64(kStReward, s.potential_reward);
    ob.set_u64(kStNext, s.next_time);
    ob.set_u8(kStPriority, static_cast<std::uint8_t>(s.priority));
    ob.set_u8(kStHasKey, s.public_key ? 1 : 0);
    if (s.public_key) ob.set_bytes_fixed(kStKey, {s.public_key->data(), s.public_key->size()});
    ob.finish_as_root();
    return b.finish();
}

Result<state::Staker> decode_staker(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "a staker is not a zap message");
    state::Staker s;
    s.tx_id = Id::from(r.bytes_fixed(kStTx, kIdLen));
    s.node_id = NodeId::from(r.bytes_fixed(kStNode, kNodeIdLen));
    s.chain_id = Id::from(r.bytes_fixed(kStChain, kIdLen));
    s.weight = r.u64(kStWeight);
    s.start_time = r.u64(kStStart);
    s.end_time = r.u64(kStEnd);
    s.potential_reward = r.u64(kStReward);
    s.next_time = r.u64(kStNext);
    s.priority = static_cast<txs::Priority>(r.u8(kStPriority));
    if (r.u8(kStHasKey) != 0) {
        const auto raw = r.bytes_fixed(kStKey, static_cast<std::int64_t>(signer::kPublicKeyLen));
        if (raw.size() != signer::kPublicKeyLen)
            return fail(Err::StoreCorrupt, "a staker's key is the wrong length");
        signer::PublicKeyBytes k{};
        std::memcpy(k.data(), raw.data(), signer::kPublicKeyLen);
        s.public_key = k;
    }
    return s;
}

Bytes encode_utxos(const std::vector<UTXO>& us) {
    zap::Builder b(zap::kHeaderSize + kUsSize + 128);
    Bytes blob;
    auto lens = b.start_list(4);
    for (const auto& u : us) {
        const auto w = u.wire_bytes();
        lens.add_u32(static_cast<std::uint32_t>(w.size()));
        blob.insert(blob.end(), w.begin(), w.end());
    }
    const std::int64_t lens_off = lens.offset();
    const std::int64_t lens_count = lens.count();
    auto ob = b.start_object(kUsSize);
    ob.set_list(kUsLens, lens_off, lens_count);
    ob.set_bytes(kUsBlob, blob);
    ob.finish_as_root();
    return b.finish();
}

Result<std::vector<UTXO>> decode_utxos(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "an output run is not a zap message");
    const auto lens = r.list(kUsLens);
    const auto blob = r.bytes(kUsBlob);
    std::vector<UTXO> out;
    std::size_t at = 0;
    for (int i = 0; i < lens.size(); ++i) {
        const std::uint32_t n = lens.u32(i);
        if (at + n > blob.size()) return fail(Err::StoreCorrupt, "an output runs past its blob");
        auto u = UTXO::from_wire_bytes(blob.subspan(at, n));
        if (!u) return std::unexpected(u.error());
        out.push_back(std::move(u.value()));
        at += n;
    }
    return out;
}

Bytes encode_owner(const txs::Owner& o) {
    zap::Builder b(zap::kHeaderSize + kOwSize + 64);
    auto lb = b.start_list(static_cast<std::int64_t>(kShortIdLen));
    for (const auto& a : o.addrs) lb.add_bytes(a.span());
    const std::int64_t addrs_off = lb.offset();
    auto ob = b.start_object(kOwSize);
    ob.set_u64(kOwLocktime, o.locktime);
    ob.set_u32(kOwThreshold, o.threshold);
    ob.set_list(kOwAddrs, addrs_off, static_cast<std::int64_t>(o.addrs.size()));
    ob.finish_as_root();
    return b.finish();
}

Result<txs::Owner> decode_owner(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "an owner is not a zap message");
    txs::Owner o;
    o.locktime = r.u64(kOwLocktime);
    o.threshold = r.u32(kOwThreshold);
    const auto addrs = r.list_stride(kOwAddrs, static_cast<std::uint32_t>(kShortIdLen));
    for (int i = 0; i < addrs.size(); ++i)
        o.addrs.push_back(ShortId::from(addrs.object(i, kShortIdLen).bytes_fixed(0, kShortIdLen)));
    return o;
}

Bytes encode_conversion(const state::NetToL1Conversion& c) {
    zap::Builder b(zap::kHeaderSize + kCvSize + 64);
    auto ob = b.start_object(kCvSize);
    ob.set_bytes_fixed(kCvChain, c.chain_id.span());
    ob.set_bytes_fixed(kCvValidation, c.validation_id.span());
    ob.set_bytes(kCvAddr, c.addr);
    ob.finish_as_root();
    return b.finish();
}

Result<state::NetToL1Conversion> decode_conversion(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "a conversion is not a zap message");
    state::NetToL1Conversion c;
    c.chain_id = Id::from(r.bytes_fixed(kCvChain, kIdLen));
    c.validation_id = Id::from(r.bytes_fixed(kCvValidation, kIdLen));
    c.addr = bytes_of(r.bytes(kCvAddr));
    return c;
}

Bytes encode_l1_validator(const l1::Validator& v) {
    zap::Builder b(zap::kHeaderSize + kLvSize + 256);
    auto ob = b.start_object(kLvSize);
    ob.set_bytes_fixed(kLvValidation, v.validation_id.span());
    ob.set_bytes_fixed(kLvChain, v.chain_id.span());
    ob.set_bytes_fixed(kLvNode, v.node_id.span());
    ob.set_u64(kLvStart, v.start_time);
    ob.set_u64(kLvWeight, v.weight);
    ob.set_u64(kLvNonce, v.min_nonce);
    ob.set_u64(kLvFee, v.end_accumulated_fee);
    ob.set_bytes(kLvKey, v.public_key);
    ob.set_bytes(kLvBalanceOwner, v.remaining_balance_owner);
    ob.set_bytes(kLvDeactivationOwner, v.deactivation_owner);
    ob.finish_as_root();
    return b.finish();
}

Result<l1::Validator> decode_l1_validator(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "an L1 validator is not a zap message");
    l1::Validator v;
    v.validation_id = Id::from(r.bytes_fixed(kLvValidation, kIdLen));
    v.chain_id = Id::from(r.bytes_fixed(kLvChain, kIdLen));
    v.node_id = NodeId::from(r.bytes_fixed(kLvNode, kNodeIdLen));
    v.start_time = r.u64(kLvStart);
    v.weight = r.u64(kLvWeight);
    v.min_nonce = r.u64(kLvNonce);
    v.end_accumulated_fee = r.u64(kLvFee);
    v.public_key = bytes_of(r.bytes(kLvKey));
    v.remaining_balance_owner = bytes_of(r.bytes(kLvBalanceOwner));
    v.deactivation_owner = bytes_of(r.bytes(kLvDeactivationOwner));
    return v;
}

Bytes encode_tx(const txs::Tx& tx, status::Status st) {
    zap::Builder b(zap::kHeaderSize + kTxSize + tx.bytes.size() + 16);
    auto ob = b.start_object(kTxSize);
    ob.set_bytes(kTxBytes, tx.bytes);
    ob.set_u8(kTxStatus, static_cast<std::uint8_t>(st));
    ob.finish_as_root();
    return b.finish();
}

Result<std::pair<txs::Tx, status::Status>> decode_tx(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "a transaction record is not a zap message");
    auto tx = txs::parse(r.bytes(kTxBytes));
    if (!tx) return std::unexpected(tx.error());
    return std::make_pair(std::move(tx.value()), static_cast<status::Status>(r.u8(kTxStatus)));
}

Bytes encode_change(const validators::Change& c) {
    zap::Builder b(zap::kHeaderSize + kChSize + 256);
    auto ob = b.start_object(kChSize);
    ob.set_bytes_fixed(kChValidation, c.validation.span());
    ob.set_u8(kChDecrease, c.weight.decrease ? 1 : 0);
    ob.set_u8(kChRemoval, c.had_removal ? 1 : 0);
    ob.set_u64(kChAmount, c.weight.amount);
    ob.set_bytes(kChBefore, c.key_before);
    ob.set_bytes(kChAfter, c.key_after);
    ob.finish_as_root();
    return b.finish();
}

Result<validators::Change> decode_change(std::span<const std::uint8_t> b) {
    bool ok_parse = false;
    const auto r = root_of(b, ok_parse);
    if (!ok_parse) return fail(Err::StoreCorrupt, "a validator change is not a zap message");
    validators::Change c;
    c.validation = Id::from(r.bytes_fixed(kChValidation, kIdLen));
    c.weight.decrease = r.u8(kChDecrease) != 0;
    c.had_removal = r.u8(kChRemoval) != 0;
    c.weight.amount = r.u64(kChAmount);
    c.key_before = bytes_of(r.bytes(kChBefore));
    c.key_after = bytes_of(r.bytes(kChAfter));
    return c;
}

// ── the height's record

Status write_history(store::Store& to, std::uint64_t height,
                     const std::map<validators::Where, validators::Change>& c) {
    for (const auto& [where, change] : c) {
        const auto k = history_key(height, where.chain_id, where.node_id);
        const auto v = encode_change(change);
        to.put(k, v);
    }
    return ok();
}

// ── coming back

namespace {

// The natural key of a record, after the tag byte.
std::span<const std::uint8_t> tail(std::span<const std::uint8_t> k) { return k.subspan(1); }

std::uint64_t be64(std::span<const std::uint8_t> b) {
    std::uint64_t v = 0;
    for (std::size_t i = 0; i < 8 && i < b.size(); ++i) v = (v << 8) | b[i];
    return v;
}

}  // namespace

Result<Restored> load(const store::Store& from, state::MemState& into, validators::History& history) {
    Restored out;
    bool saw_last_accepted = false;
    Status trouble = ok();
    const auto note = [&](Status s) {
        if (!s && trouble) trouble = s;
    };

    // One walk, in key order, which is tag order — so the current validators
    // land before the delegatee rewards that only exist for a validator that is
    // in the set, and the blocks arrive in height order.
    const Bytes all;
    from.scan(all, [&](std::span<const std::uint8_t> k, std::span<const std::uint8_t> v) {
        if (k.empty()) return;
        switch (static_cast<Tag>(k[0])) {
            case Tag::Meta:
                note(decode_meta(v, into));
                return;
            case Tag::LastAccepted: {
                auto la = decode_last_accepted(v);
                if (!la) {
                    note(std::unexpected(la.error()));
                    return;
                }
                out.last_accepted = la->first;
                out.height = la->second;
                saw_last_accepted = true;
                return;
            }
            case Tag::Supply: {
                auto n = decode_u64(v);
                if (!n) {
                    note(std::unexpected(n.error()));
                    return;
                }
                into.set_current_supply(Id::from(tail(k)), n.value());
                return;
            }
            case Tag::Utxo: {
                auto us = decode_utxos(v);
                if (!us) {
                    note(std::unexpected(us.error()));
                    return;
                }
                for (const auto& u : us.value()) into.add_utxo(u);
                return;
            }
            case Tag::RewardUtxos: {
                auto us = decode_utxos(v);
                if (!us) {
                    note(std::unexpected(us.error()));
                    return;
                }
                const Id tx = Id::from(tail(k));
                for (const auto& u : us.value()) into.add_reward_utxo(tx, u);
                return;
            }
            case Tag::CurrentValidator:
            case Tag::CurrentDelegator:
            case Tag::PendingValidator:
            case Tag::PendingDelegator: {
                auto s = decode_staker(v);
                if (!s) {
                    note(std::unexpected(s.error()));
                    return;
                }
                switch (static_cast<Tag>(k[0])) {
                    case Tag::CurrentValidator: into.load_current_validator(s.value()); break;
                    case Tag::CurrentDelegator: into.load_current_delegator(s.value()); break;
                    case Tag::PendingValidator: into.load_pending_validator(s.value()); break;
                    default: into.load_pending_delegator(s.value()); break;
                }
                return;
            }
            case Tag::DelegateeReward: {
                auto n = decode_u64(v);
                if (!n) {
                    note(std::unexpected(n.error()));
                    return;
                }
                const auto t = tail(k);
                if (t.size() < kIdLen + kNodeIdLen) {
                    note(fail(Err::StoreCorrupt, "a delegatee reward key is short"));
                    return;
                }
                note(into.set_delegatee_reward(Id::from(t.first(kIdLen)),
                                               NodeId::from(t.subspan(kIdLen, kNodeIdLen)), n.value()));
                return;
            }
            case Tag::Network:
                into.add_network(Id::from(tail(k)));
                return;
            case Tag::NetworkOwner: {
                auto o = decode_owner(v);
                if (!o) {
                    note(std::unexpected(o.error()));
                    return;
                }
                into.set_network_owner(Id::from(tail(k)), o.value());
                return;
            }
            case Tag::NetworkConversion: {
                auto c = decode_conversion(v);
                if (!c) {
                    note(std::unexpected(c.error()));
                    return;
                }
                into.set_network_conversion(Id::from(tail(k)), c.value());
                return;
            }
            case Tag::Transformation:
            case Tag::Chain:
            case Tag::Tx: {
                auto tx = decode_tx(v);
                if (!tx) {
                    note(std::unexpected(tx.error()));
                    return;
                }
                switch (static_cast<Tag>(k[0])) {
                    case Tag::Transformation: into.add_network_transformation(tx->first); break;
                    case Tag::Chain: into.add_chain(tx->first); break;
                    default: into.add_tx(tx->first, tx->second); break;
                }
                return;
            }
            case Tag::L1Validator: {
                auto lv = decode_l1_validator(v);
                if (!lv) {
                    note(std::unexpected(lv.error()));
                    return;
                }
                note(into.put_l1_validator(lv.value()));
                return;
            }
            case Tag::Expiry: {
                auto e = l1::ExpiryEntry::unmarshal(tail(k));
                if (!e) {
                    note(std::unexpected(e.error()));
                    return;
                }
                into.put_expiry(e.value());
                return;
            }
            case Tag::History: {
                const auto t = tail(k);
                if (t.size() < 8 + kIdLen + kNodeIdLen) {
                    note(fail(Err::StoreCorrupt, "a history key is short"));
                    return;
                }
                auto c = decode_change(v);
                if (!c) {
                    note(std::unexpected(c.error()));
                    return;
                }
                validators::Where where;
                where.chain_id = Id::from(t.subspan(8, kIdLen));
                where.node_id = NodeId::from(t.subspan(8 + kIdLen, kNodeIdLen));
                note(history.record(be64(t), {{where, c.value()}}));
                return;
            }
            case Tag::Block:
                out.blocks.emplace_back(v.begin(), v.end());
                return;
        }
        // A tag this chain does not write. Refusing is the point: what came
        // back is not what this chain wrote, and a boot that guesses is a node
        // that signs from a state nobody wrote.
        note(fail(Err::StoreCorrupt, "unknown record tag " + std::to_string(k[0])));
    });

    if (!trouble) return std::unexpected(trouble.error());
    if (!saw_last_accepted) return fail(Err::NotFound, "the store holds no accepted block");
    return out;
}

}  // namespace lux::platformvm::records
