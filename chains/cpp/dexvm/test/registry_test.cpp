// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// registry_test — the four properties the reference states as its whole reason
// to exist, ported case for case from chains/dexvm/registry/registry_test.go:
//
//   no synthetic asset can register
//   no synthetic market can start
//   an ERC-20's id is its real token address
//   a UTXO asset's id is its real source-chain assetID
//
// Every refusal here is a REAL refusal: the verifier is an in-memory chain
// snapshot that answers only for what was seeded, and each case proves the
// positive too — a real, seeded asset registers — so the refusals cannot be a
// verifier that says no to everything.

#include "lux/dexvm/gate.hpp"
#include "lux/dexvm/registry.hpp"

#include "check.hpp"
#include "fixtures.hpp"

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

std::vector<AssetKind> all_kinds() {
    return {AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO};
}

void no_synthetic_asset_can_register() {
    std::printf("no synthetic asset can register\n");
    FakeChain fc;
    const Id c_chain = test_id(10);
    Registry reg(all_kinds());

    // (a) an ERC-20 that does not exist on-chain — never seeded.
    Asset ghost;
    ghost.network_id = kMainnetID;
    ghost.chain_id = c_chain;
    ghost.kind = AssetKind::ERC20;
    ghost.canonical_ref = addr20(0x42);
    ghost.decimals = 18;
    ghost.symbol = "GHOST";
    ghost.name = "Ghost token";
    ghost.enabled = true;
    ghost.risk_tier = RiskTier::Tier2;
    refused_any(reg.register_asset(ghost, fc), "an ERC-20 with no code behind it");

    // (b) a UTXO asset that is not on its source chain.
    Asset phantom;
    phantom.network_id = kMainnetID;
    phantom.chain_id = test_id(11);
    phantom.kind = AssetKind::UTXO;
    phantom.canonical_ref = to_bytes(view(test_id(12)));
    phantom.decimals = 9;
    phantom.symbol = "PUTXO";
    phantom.name = "Phantom UTXO";
    phantom.enabled = true;
    phantom.risk_tier = RiskTier::Tier2;
    refused_any(reg.register_asset(phantom, fc), "a UTXO asset not on its source chain");

    // (c) the invalid/zero kind — the closest a caller can get to declaring a
    // synthetic class, and there is no such class.
    Asset d_native;
    d_native.network_id = kMainnetID;
    d_native.chain_id = c_chain;
    d_native.kind = AssetKind::Invalid;
    d_native.canonical_ref = addr20(0x01);
    d_native.decimals = 18;
    d_native.symbol = "DNAT";
    d_native.enabled = true;
    refused(reg.register_asset(d_native, fc), Err::InvalidKind, "a declared synthetic class");

    // (d) a REAL, seeded ERC-20 registers — so the refusals above are genuine.
    const Asset real = real_erc20(fc, c_chain, view(addr20(0x99)), "USDC");
    admitted(reg.register_asset(real, fc), "a real seeded ERC-20 registers");
    check(reg.len() == 1, "and it is the only thing in the registry");
}

void no_synthetic_market_can_start() {
    std::printf("no synthetic market can start\n");
    FakeChain fc;
    const Id c_chain = test_id(20);
    Registry reg(all_kinds());

    const Asset usdc = real_erc20(fc, c_chain, view(addr20(0x10)), "USDC");
    auto usdc_id = reg.register_asset(usdc, fc);
    admitted(usdc_id, "the quote asset registers");

    // An AssetID for a base that was never registered: a synthetic asset.
    auto synthetic_base = derive_asset_id(kMainnetID, c_chain, AssetKind::ERC20, view(addr20(0x7e)));
    admitted(synthetic_base, "a synthetic id can be DERIVED (deriving is not admitting)");

    Market over_synthetic;
    over_synthetic.network_id = kMainnetID;
    over_synthetic.base_asset_id = *synthetic_base;
    over_synthetic.quote_asset_id = *usdc_id;
    over_synthetic.enabled = true;
    refused(reg.create_market(over_synthetic), Err::UnknownAsset,
            "a market over a synthetic base does not resolve");

    // A market over two real assets is created.
    const Asset wlux = real_erc20(fc, c_chain, view(addr20(0x20)), "WLUX");
    auto wlux_id = reg.register_asset(wlux, fc);
    admitted(wlux_id, "the base asset registers");
    Market real;
    real.network_id = kMainnetID;
    real.base_asset_id = *wlux_id;
    real.quote_asset_id = *usdc_id;
    real.enabled = true;
    admitted(reg.create_market(real), "a market over two real assets is created");

    // The boot gate passes for the all-real registry.
    admitted(refuse_under_synthetic_config(NetworkClass::Mainnet, default_dex_asset_policy(), reg,
                                           nullptr),
             "the boot gate passes for an all-real registry");

    // Now smuggle a synthetic market past create_market — a corrupted or forced
    // config — and the gate must still refuse to start. This is what makes the
    // gate a second line of defence rather than a restatement of the first.
    reg.restore_market(test_id(21), over_synthetic);
    refused(refuse_under_synthetic_config(NetworkClass::Mainnet, default_dex_asset_policy(), reg,
                                          nullptr),
            Err::EnabledMarketUnknownAsset, "the gate refuses a smuggled synthetic market");
}

void market_admission_order() {
    std::printf("a market's remaining refusals, in the order the reference checks them\n");
    FakeChain fc;
    const Id c_chain = test_id(30);
    Registry reg(all_kinds());

    auto a_id = reg.register_asset(real_erc20(fc, c_chain, view(addr20(0x30)), "AAA"), fc);
    auto b_id = reg.register_asset(real_erc20(fc, c_chain, view(addr20(0x31)), "BBB"), fc);
    admitted(a_id, "two real assets register");
    admitted(b_id, "…the second of them too");

    // A disabled asset stays registered and auditable, but admits no market.
    Asset off = real_erc20(fc, c_chain, view(addr20(0x32)), "OFF");
    off.enabled = false;
    auto off_id = reg.register_asset(off, fc);
    admitted(off_id, "a disabled asset still registers");
    Market with_disabled{kMainnetID, *off_id, *a_id, {}, true};
    refused(reg.create_market(with_disabled), Err::AssetDisabled,
            "but a market over it is refused");

    Market self_pair{kMainnetID, *a_id, *a_id, {}, true};
    refused(reg.create_market(self_pair), Err::SameAsset, "an asset cannot trade against itself");

    Market wrong_net{7, *a_id, *b_id, {}, true};
    refused(reg.create_market(wrong_net), Err::NetworkMismatch, "a market does not span networks");

    Market ok{kMainnetID, *a_id, *b_id, {}, true};
    auto id = reg.create_market(ok);
    admitted(id, "the well-formed market is created");
    refused(reg.create_market(ok), Err::DuplicateMarket, "and creating it twice is refused");

    auto found = reg.resolve_market(*id);
    check(found.has_value(), "the market resolves by its derived id");
    check(reg.market_len() == 1, "and it is the only one");
}

void erc20_id_is_the_real_address() {
    std::printf("an ERC-20's id is its real token address, never its ticker\n");
    const Id c_chain = test_id(40);
    const Bytes addr = addr20(0x11);

    // The same address with entirely different display metadata is the SAME
    // asset: symbol, name and decimals are not in the preimage.
    Asset one{kMainnetID, c_chain, AssetKind::ERC20, addr, 6, "USDC", "USD Coin", true,
              RiskTier::Tier0};
    Asset two{kMainnetID, c_chain, AssetKind::ERC20, addr, 18, "ZZZ", "Renamed", true,
              RiskTier::Tier3};
    auto id1 = one.id();
    auto id2 = two.id();
    check(id1 && id2 && *id1 == *id2, "renaming an asset does not rename its id");

    auto direct = derive_asset_id(kMainnetID, c_chain, AssetKind::ERC20, view(addr));
    check(direct && *direct == *id1, "and Asset::id agrees with the derivation");
}

void utxo_id_is_the_real_asset_id() {
    std::printf("a UTXO asset's id is its real source-chain assetID\n");
    FakeChain fc;
    const Id source = test_id(50);
    const Id real_asset = test_id(51);
    const Id other_asset = test_id(52);

    auto id_real = derive_asset_id(kMainnetID, source, AssetKind::UTXO, view(real_asset));
    auto id_other = derive_asset_id(kMainnetID, source, AssetKind::UTXO, view(other_asset));
    check(id_real && id_other && *id_real != *id_other, "two source assets are two DEX assets");

    fc.seed_utxo(kMainnetID, source, real_asset, 9);
    Registry reg({AssetKind::UTXO});
    Asset a;
    a.network_id = kMainnetID;
    a.chain_id = source;
    a.kind = AssetKind::UTXO;
    a.canonical_ref = to_bytes(view(real_asset));
    a.decimals = 9;
    a.symbol = "XAV";
    a.name = "X-Chain asset";
    a.enabled = true;
    a.risk_tier = RiskTier::Tier1;
    auto got = reg.register_asset(a, fc);
    admitted(got, "the real seeded UTXO asset registers");
    check(got && *got == *id_real, "under exactly the id its source assetID derives");

    // The policy is a second, narrower gate on top of reality: a real asset of a
    // kind the policy does not allow is still refused.
    Registry narrow({AssetKind::ERC20});
    refused(narrow.register_asset(a, fc), Err::KindNotAllowed,
            "a real UTXO asset under an ERC20-only policy");

    // An empty policy admits nothing at all.
    Registry closed;
    refused(closed.register_asset(a, fc), Err::KindNotAllowed, "and an empty policy admits nothing");

    // Registering the same real asset twice is refused.
    refused(reg.register_asset(a, fc), Err::DuplicateAsset, "the same asset twice");
}

void decimals_must_match_the_chain() {
    std::printf("declared decimals are cross-checked against the chain\n");
    FakeChain fc;
    const Id c_chain = test_id(60);
    Registry reg({AssetKind::ERC20});

    const Bytes addr = addr20(0x44);
    fc.seed_erc20(kMainnetID, c_chain, view(addr), 6);
    Asset mismatch{kMainnetID, c_chain, AssetKind::ERC20, addr, 18, "USDC", "", true,
                   RiskTier::Tier0};
    refused_any(reg.register_asset(mismatch, fc), "18 declared against 6 on-chain");

    Asset ok = mismatch;
    ok.decimals = 6;
    admitted(reg.register_asset(ok, fc), "and the same asset with 6 registers");
}

}  // namespace

int main() {
    no_synthetic_asset_can_register();
    no_synthetic_market_can_start();
    market_admission_order();
    erc20_id_is_the_real_address();
    utxo_id_is_the_real_asset_id();
    decimals_must_match_the_chain();
    return report("registry");
}
