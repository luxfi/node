// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/gate.hpp"

#include "lux/dexvm/forbidden.hpp"

#include <optional>

namespace lux::dexvm {

std::string_view to_string(NetworkClass c) {
    switch (c) {
        case NetworkClass::Mainnet: return "mainnet";
        case NetworkClass::Testnet: return "testnet";
        default:                    return "dev";
    }
}

bool value_bearing(NetworkClass c) {
    return c == NetworkClass::Mainnet || c == NetworkClass::Testnet;
}

NetworkClass network_class_for(std::uint32_t network_id) {
    switch (network_id) {
        case 1: return NetworkClass::Mainnet;
        case 2: return NetworkClass::Testnet;
        default: return NetworkClass::Dev;
    }
}

std::vector<AssetKind> DexAssetPolicy::kinds() const {
    if (allowed_asset_kinds.empty())
        return {AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO};
    return allowed_asset_kinds;
}

bool DexAssetPolicy::any_synthetic_flag() const {
    return allow_synthetic_assets || allow_synthetic_markets || allow_mock_liquidity;
}

DexAssetPolicy default_dex_asset_policy() {
    DexAssetPolicy p;
    p.allowed_asset_kinds = {AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO};
    return p;
}

Result<void> refuse_under_synthetic_config(
    NetworkClass network_class, const DexAssetPolicy& policy, const Registry& reg,
    const std::function<std::string(const Id&)>& chain_label_for) {
    auto b = [](bool v) { return std::string(v ? "true" : "false"); };

    // (1) the allowed kinds must be a subset of the three real kinds.
    for (AssetKind k : policy.kinds()) {
        if (!valid(k))
            return fail(Err::BadAllowedKind, "\"" + std::string(to_string(k)) + "\"");
    }

    // (2) no synthetic flags on a value-bearing network.
    if (value_bearing(network_class) && policy.any_synthetic_flag())
        return fail_note(Err::SyntheticOnValueNet,
                         "network=" + std::string(to_string(network_class)) + " synthAssets=" +
                             b(policy.allow_synthetic_assets) + " synthMarkets=" +
                             b(policy.allow_synthetic_markets) + " mockLiq=" +
                             b(policy.allow_mock_liquidity));

    // (3) deny-scan every registered asset. Run before the market scan so a
    // tainted asset is reported at its source rather than at a market over it.
    std::optional<Error> scan_err;
    reg.each([&](const Id& id, const Asset& a) {
        if (scan_err) return;
        const std::string label = chain_label_for ? chain_label_for(a.chain_id) : std::string();
        if (auto r = assert_no_forbidden_asset_refs(a, label); !r)
            scan_err = r.error().wrap("registered asset " + cb58(id));
    });
    if (scan_err) return std::unexpected(*scan_err);

    // (4) every ENABLED market must resolve both sides to a registered, enabled
    // asset on the matching network. A market over a synthetic asset has no
    // AssetID to resolve, so this catches it structurally.
    reg.each_market([&](const Id& id, const Market& m) {
        if (scan_err || !m.enabled) return;
        auto base = reg.must_resolve_enabled(m.base_asset_id);
        if (!base) {
            scan_err = Error{Err::EnabledMarketUnknownAsset,
                             std::string(text_of(Err::EnabledMarketUnknownAsset)) + ": market " +
                                 cb58(id) + " base: " + base.error().text};
            return;
        }
        auto quote = reg.must_resolve_enabled(m.quote_asset_id);
        if (!quote) {
            scan_err = Error{Err::EnabledMarketUnknownAsset,
                             std::string(text_of(Err::EnabledMarketUnknownAsset)) + ": market " +
                                 cb58(id) + " quote: " + quote.error().text};
            return;
        }
        if (m.network_id != base->network_id || m.network_id != quote->network_id)
            scan_err = Error{Err::EnabledMarketUnknownAsset,
                             std::string(text_of(Err::EnabledMarketUnknownAsset)) + ": market " +
                                 cb58(id) + " network mismatch (market=" +
                                 std::to_string(m.network_id) + " base=" +
                                 std::to_string(base->network_id) + " quote=" +
                                 std::to_string(quote->network_id) + ")"};
    });
    if (scan_err) return std::unexpected(*scan_err);
    return {};
}

}  // namespace lux::dexvm
