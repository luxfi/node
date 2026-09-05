// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/registry.hpp"

#include "lux/dexvm/forbidden.hpp"

#include <algorithm>
#include <mutex>

namespace lux::dexvm {

Result<Id> Asset::id() const {
    return derive_asset_id(network_id, chain_id, kind, view(canonical_ref));
}

Result<void> Asset::validate_shape() const {
    if (!valid(kind)) return fail(Err::InvalidKind);
    if (!valid(risk_tier))
        return fail("registry: risk tier " + std::to_string(std::uint32_t(std::uint8_t(risk_tier))) +
                    " out of range");
    // canonical_ref_for enforces per-kind ref shape (length, non-zero, marker).
    if (auto r = canonical_ref_for(kind, view(canonical_ref)); !r)
        return std::unexpected(r.error());
    if (chain_id == kEmptyId) return fail(Err::EmptyChainID);
    // A symbol that looks like an asset IDENTITY rather than a display label is
    // the ASCII-ticker-as-id anti-pattern; symbols stay pure display.
    if (looks_like_ascii_ticker_id(symbol))
        return fail("registry: symbol \"" + symbol +
                    "\" looks like an ASCII-ticker asset id; symbols are display-only, assets "
                    "are keyed by AssetID");
    return {};
}

Result<void> verify_on_chain(const Asset& a, ChainVerifier& v) {
    if (auto r = a.validate_shape(); !r) return r;

    Result<std::uint8_t> got = fail(Err::InvalidKind);
    switch (a.kind) {
        case AssetKind::ERC20:
            got = v.verify_erc20(a.network_id, a.chain_id, view(a.canonical_ref));
            break;
        case AssetKind::EVMNative:
            got = v.verify_evm_native(a.network_id, a.chain_id);
            break;
        case AssetKind::UTXO: {
            Id asset_id{};
            std::copy_n(a.canonical_ref.begin(),
                        std::min<std::size_t>(asset_id.size(), a.canonical_ref.size()),
                        asset_id.begin());
            got = v.verify_utxo_asset(a.network_id, a.chain_id, asset_id);
            break;
        }
        default:
            return fail(Err::InvalidKind);
    }
    if (!got)
        return std::unexpected(Error{got.error().code,
                                     "registry: asset " + std::string(to_string(a.kind)) +
                                         " not real on network " + std::to_string(a.network_id) +
                                         ": " + got.error().text});
    if (*got != a.decimals)
        return fail("registry: declared decimals " + std::to_string(std::uint32_t(a.decimals)) +
                    " != on-chain decimals " + std::to_string(std::uint32_t(*got)) + " for " +
                    std::string(to_string(a.kind)) + " asset");
    return {};
}

Registry::Registry(std::vector<AssetKind> allowed) {
    for (AssetKind k : allowed) {
        if (valid(k)) allowed_kinds_.insert(k);
    }
}

bool Registry::allows_kind(AssetKind k) const {
    std::shared_lock lock(mu_);
    return allowed_kinds_.count(k) != 0;
}

Result<Id> Registry::register_asset(const Asset& a, ChainVerifier& v) {
    if (auto r = a.validate_shape(); !r) return std::unexpected(r.error());
    if (!allows_kind(a.kind)) return fail(Err::KindNotAllowed, to_string(a.kind));
    if (auto r = verify_on_chain(a, v); !r) return std::unexpected(r.error());
    auto id = a.id();
    if (!id) return std::unexpected(id.error());

    std::unique_lock lock(mu_);
    if (by_id_.count(*id)) return fail(Err::DuplicateAsset, cb58(*id));
    by_id_[*id] = a;
    return *id;
}

std::optional<Asset> Registry::resolve(const Id& id) const {
    std::shared_lock lock(mu_);
    auto it = by_id_.find(id);
    if (it == by_id_.end()) return std::nullopt;
    return it->second;
}

Result<Asset> Registry::must_resolve_enabled(const Id& id) const {
    auto a = resolve(id);
    if (!a) return fail(Err::UnknownAsset, cb58(id));
    if (!a->enabled) return fail(Err::AssetDisabled, cb58(id));
    return *a;
}

std::size_t Registry::len() const {
    std::shared_lock lock(mu_);
    return by_id_.size();
}

void Registry::each(const std::function<void(const Id&, const Asset&)>& fn) const {
    // The rows are copied out under the lock and walked outside it, so a callback
    // that touches the registry (the boot gate's market scan does) cannot
    // deadlock against the reader it was invoked from.
    std::vector<std::pair<Id, Asset>> rows;
    {
        std::shared_lock lock(mu_);
        rows.assign(by_id_.begin(), by_id_.end());
    }
    for (const auto& [id, a] : rows) fn(id, a);
}

Result<Id> Registry::create_market(const Market& m) {
    auto base = must_resolve_enabled(m.base_asset_id);
    if (!base) return std::unexpected(base.error().wrap("market base side"));
    auto quote = must_resolve_enabled(m.quote_asset_id);
    if (!quote) return std::unexpected(quote.error().wrap("market quote side"));
    if (m.base_asset_id == m.quote_asset_id) return fail(Err::SameAsset);
    if (m.network_id != base->network_id || m.network_id != quote->network_id)
        return fail(Err::NetworkMismatch,
                    "market=" + std::to_string(m.network_id) + " base=" +
                        std::to_string(base->network_id) + " quote=" +
                        std::to_string(quote->network_id));

    const Id id = m.id();
    std::unique_lock lock(mu_);
    if (markets_.count(id)) return fail(Err::DuplicateMarket, cb58(id));
    markets_[id] = m;
    return id;
}

std::optional<Market> Registry::resolve_market(const Id& id) const {
    std::shared_lock lock(mu_);
    auto it = markets_.find(id);
    if (it == markets_.end()) return std::nullopt;
    return it->second;
}

void Registry::each_market(const std::function<void(const Id&, const Market&)>& fn) const {
    std::vector<std::pair<Id, Market>> rows;
    {
        std::shared_lock lock(mu_);
        rows.assign(markets_.begin(), markets_.end());
    }
    for (const auto& [id, m] : rows) fn(id, m);
}

std::size_t Registry::market_len() const {
    std::shared_lock lock(mu_);
    return markets_.size();
}

std::unique_ptr<Registry> Registry::copy() const {
    auto out = std::make_unique<Registry>();
    std::shared_lock lock(mu_);
    out->allowed_kinds_ = allowed_kinds_;
    out->by_id_ = by_id_;
    out->markets_ = markets_;
    return out;
}

void Registry::restore_asset(const Id& id, const Asset& a) {
    std::unique_lock lock(mu_);
    by_id_[id] = a;
}

void Registry::restore_market(const Id& id, const Market& m) {
    std::unique_lock lock(mu_);
    markets_[id] = m;
}

}  // namespace lux::dexvm
