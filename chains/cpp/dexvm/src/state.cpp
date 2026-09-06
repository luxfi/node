// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/state.hpp"

#include <algorithm>
#include <set>

namespace lux::dexvm {
namespace {

// The row payloads reuse the transaction encoding: a stored asset and a
// submitted one are the same record, so they have one format rather than two
// that could drift.
constexpr const char* kErrCorruptState = "dexvm: stored row does not parse";

Bytes prefixed_key(std::uint8_t prefix, const Id& id) {
    Bytes k;
    k.reserve(1 + id.size());
    k.push_back(prefix);
    k.insert(k.end(), id.begin(), id.end());
    return k;
}

ByteView key_view(std::string_view s) {
    return ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size());
}

// rows_of walks a registry in ascending key order — assets first, because 'a'
// sorts before 'm', which is the same order the store's own scan yields.
std::vector<std::pair<Bytes, Bytes>> rows_of(const Registry& reg) {
    std::vector<std::pair<Bytes, Bytes>> rows;
    reg.each([&](const Id& id, const Asset& a) {
        rows.emplace_back(asset_key(id), encode_asset_row(a));
    });
    reg.each_market([&](const Id& id, const Market& m) {
        rows.emplace_back(market_key(id), encode_market_row(m));
    });
    std::sort(rows.begin(), rows.end(),
              [](const auto& l, const auto& r) { return l.first < r.first; });
    return rows;
}

}  // namespace

Bytes asset_key(const Id& id) { return prefixed_key(kPrefixAsset, id); }
Bytes market_key(const Id& id) { return prefixed_key(kPrefixMarket, id); }

Bytes encode_asset_row(const Asset& a) { return Tx::register_asset(a).encode(); }
Bytes encode_market_row(const Market& m) { return Tx::create_market(m).encode(); }

Result<Asset> decode_asset_row(ByteView b) {
    auto tx = decode_tx(b);
    if (!tx) return std::unexpected(tx.error().wrap(kErrCorruptState));
    if (tx->kind != TxKind::RegisterAsset)
        return fail(std::string(kErrCorruptState) + ": an asset row holds a market");
    return tx->asset;
}

Result<Market> decode_market_row(ByteView b) {
    auto tx = decode_tx(b);
    if (!tx) return std::unexpected(tx.error().wrap(kErrCorruptState));
    if (tx->kind != TxKind::CreateMarket)
        return fail(std::string(kErrCorruptState) + ": a market row holds an asset");
    return tx->market;
}

Id fold_rows(const std::vector<std::pair<Bytes, Bytes>>& rows) {
    Folder f;
    f.tag("lux:dex:state:v1");
    f.u64(rows.size());
    for (const auto& [k, v] : rows) {
        f.bytes(view(k));
        f.bytes(view(v));
    }
    return f.sum();
}

Result<std::unique_ptr<State>> State::load(store::Store& s, NetworkClass network_class,
                                           const DexAssetPolicy& policy,
                                           std::function<std::string(const Id&)> chain_label_for) {
    std::unique_ptr<State> st(new State(s, network_class, policy, std::move(chain_label_for)));
    st->reg_ = std::make_unique<Registry>(policy.kinds());

    // The rows are restored directly rather than re-admitted: they were admitted
    // once, against the verifier of the block that carried them, and re-proving
    // them here would ask a boot-time verifier about a decision a past height
    // already made. What IS re-run is the gate, below — the check that the whole
    // restored set is still one this chain may start from.
    std::optional<Error> err;
    const std::uint8_t asset_prefix = kPrefixAsset;
    s.each(ByteView(&asset_prefix, 1), [&](ByteView k, ByteView v) {
        auto a = decode_asset_row(v);
        if (!a) {
            err = a.error();
            return false;
        }
        auto id = a->id();
        if (!id) {
            err = id.error();
            return false;
        }
        if (asset_key(*id) != to_bytes(k)) {
            err = Error{Err::Other, std::string(kErrCorruptState) +
                                        ": an asset row is filed under an id it does not derive"};
            return false;
        }
        st->reg_->restore_asset(*id, *a);
        return true;
    });
    if (err) return std::unexpected(*err);

    const std::uint8_t market_prefix = kPrefixMarket;
    s.each(ByteView(&market_prefix, 1), [&](ByteView k, ByteView v) {
        auto m = decode_market_row(v);
        if (!m) {
            err = m.error();
            return false;
        }
        if (market_key(m->id()) != to_bytes(k)) {
            err = Error{Err::Other, std::string(kErrCorruptState) +
                                        ": a market row is filed under an id it does not derive"};
            return false;
        }
        st->reg_->restore_market(m->id(), *m);
        return true;
    });
    if (err) return std::unexpected(*err);

    if (auto r = refuse_under_synthetic_config(st->network_class_, policy, *st->reg_,
                                               st->chain_label_for_);
        !r)
        return std::unexpected(r.error().wrap("dexvm: restored state"));

    if (auto h = s.get(key_view(kKeyLastAccepted)); h.has_value()) {
        if (h->size() != 40)
            return fail(std::string(kErrCorruptState) + ": the last-accepted row is " +
                        std::to_string(h->size()) + " bytes, not 40");
        std::copy_n(h->begin(), 32, st->last_accepted_.begin());
        std::uint64_t height = 0;
        for (int i = 0; i < 8; ++i) height = (height << 8) | (*h)[std::size_t(32 + i)];
        st->last_accepted_height_ = height;
    }
    return st;
}

Result<Diff> State::execute(const BlockBody& body, ChainVerifier& v) const {
    Diff diff;
    diff.next = reg_->copy();

    std::set<Bytes> before;
    for (const auto& [k, _] : rows_of(*reg_)) before.insert(k);

    for (std::size_t i = 0; i < body.txs.size(); ++i) {
        const Tx& tx = body.txs[i];
        const std::string at = "dexvm: block tx[" + std::to_string(i) + "]";
        switch (tx.kind) {
            case TxKind::RegisterAsset: {
                auto id = diff.next->register_asset(tx.asset, v);
                if (!id) return std::unexpected(id.error().wrap(at));
                break;
            }
            case TxKind::CreateMarket: {
                auto id = diff.next->create_market(tx.market);
                if (!id) return std::unexpected(id.error().wrap(at));
                break;
            }
            default:
                return fail(at + ": unknown transaction kind");
        }
    }

    const auto after = rows_of(*diff.next);
    for (const auto& row : after) {
        if (!before.count(row.first)) diff.rows.push_back(row);
    }
    diff.root = fold_rows(after);

    // A block whose result this chain would refuse to boot from must not be
    // accepted: the gate that guards the door on restart guards it here too.
    if (auto r = refuse_under_synthetic_config(network_class_, policy_, *diff.next,
                                               chain_label_for_);
        !r)
        return std::unexpected(r.error().wrap("dexvm: block would produce a set the gate refuses"));

    return diff;
}

Result<void> State::commit(const BlockBody& body, Diff&& diff) {
    for (const auto& [k, v] : diff.rows) store_.put(view(k), view(v));

    Bytes marker;
    marker.reserve(40);
    const Id id = body.id();
    marker.insert(marker.end(), id.begin(), id.end());
    for (int i = 7; i >= 0; --i) marker.push_back(std::uint8_t((body.height >> (8 * i)) & 0xff));
    store_.put(key_view(kKeyLastAccepted), view(marker));

    if (auto r = store_.commit(); !r) return r;

    reg_ = std::move(diff.next);
    last_accepted_ = id;
    last_accepted_height_ = body.height;
    return {};
}

Id State::root() const { return fold_rows(rows_of(*reg_)); }

}  // namespace lux::dexvm
