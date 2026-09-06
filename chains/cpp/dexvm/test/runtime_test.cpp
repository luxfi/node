// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// runtime_test — the node-side verifier, ported from
// chains/dexvm/registry/runtime_verifier_test.go.
//
// The verifier's job is the half of the proof the node can make alone: bind a
// manifest's DECLARED chain identities to the ids this node is ACTUALLY running.
// It is not "always true" — every refusal below is a manifest that would have
// been admitted by a verifier that only checked shape.

#include "lux/dexvm/runtime_verifier.hpp"

#include "check.hpp"
#include "fixtures.hpp"

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

// One native + one ERC-20 + one UTXO asset: the three kinds, so all three bind
// paths are exercised by a single manifest.
Manifest three_kinds(std::uint32_t network_id, const Id& c_chain, const Id& x_chain,
                     const Id& utxo_asset, const Bytes& erc20) {
    Manifest m;
    m.network = "testnet";
    m.network_id = network_id;
    m.evm_chain_id = 96368;
    m.c_chain_id = c_chain;
    m.assets_is_null = false;
    m.assets.push_back(Asset{network_id, c_chain, AssetKind::EVMNative,
                             to_bytes(view(kEVMNativeMarker)), 18, "LUX", "Lux", true,
                             RiskTier::Tier0});
    m.assets.push_back(
        Asset{network_id, c_chain, AssetKind::ERC20, erc20, 6, "USDC", "USD Coin", true,
              RiskTier::Tier0});
    m.assets.push_back(Asset{network_id, x_chain, AssetKind::UTXO, to_bytes(view(utxo_asset)), 9,
                             "XAV", "X asset", true, RiskTier::Tier0});
    return m;
}

void binds_to_the_running_chain() {
    std::printf("a manifest bound to the ids this node runs admits all three kinds\n");
    const Id c_chain = test_id(300);
    const Id x_chain = test_id(301);
    const Id utxo_asset = test_id(302);
    const Bytes erc20 = addr20(0x21);
    const std::uint32_t net = 2;

    const Manifest m = three_kinds(net, c_chain, x_chain, utxo_asset, erc20);
    admitted(m.validate_shape(), "the manifest is well-shaped");

    auto rv = RuntimeVerifier::make(net, c_chain, x_chain, m);
    admitted(rv, "the verifier binds to the running ids");
    if (!rv) return;

    Registry reg({AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});
    admitted(m.apply_to(reg, **rv, default_dex_asset_policy()),
             "and the manifest admits through it");
    check(reg.len() == 3, "all three assets are in");
}

void refuses_the_wrong_c_chain() {
    std::printf("a manifest rooted at a C-Chain this node does not run\n");
    const Id manifest_c = test_id(310);
    const Id running_c = test_id(311);
    const Manifest m = three_kinds(2, manifest_c, test_id(312), test_id(313), addr20(0x21));

    auto rv = RuntimeVerifier::make(2, running_c, kEmptyId, m);
    refused_any(rv, "the verifier refuses to build");
    if (!rv)
        check(rv.error().text.find("wrong-chain") != std::string::npos,
              "and says it is a wrong-chain manifest");
}

void refuses_the_wrong_network() {
    std::printf("a manifest built for another network\n");
    const Id c_chain = test_id(320);
    const Manifest m = three_kinds(2, c_chain, test_id(321), test_id(322), addr20(0x21));
    auto rv = RuntimeVerifier::make(1 /* the node runs mainnet */, c_chain, kEmptyId, m);
    refused_any(rv, "the verifier refuses to build");
    if (!rv)
        check(rv.error().text.find("wrong-net") != std::string::npos,
              "and says it is a wrong-net manifest");

    // An empty running C-Chain cannot bind anything, so it is refused too.
    refused_any(RuntimeVerifier::make(2, kEmptyId, kEmptyId, m),
                "and a node with no C-Chain id binds nothing");
}

void refuses_a_utxo_off_the_running_x_chain() {
    std::printf("a UTXO asset from a chain this node does not run\n");
    const Id c_chain = test_id(330);
    const Id manifest_x = test_id(331);
    const Id running_x = test_id(332);
    const Manifest m = three_kinds(2, c_chain, manifest_x, test_id(333), addr20(0x21));

    auto rv = RuntimeVerifier::make(2, c_chain, running_x, m);
    admitted(rv, "the verifier builds — the C-Chain does match");
    if (!rv) return;

    Registry reg({AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});
    refused_any(m.apply_to(reg, **rv, default_dex_asset_policy()),
                "but the UTXO asset is refused at admission");

    // With the X-Chain left unset, a UTXO asset from anywhere in the manifest
    // binds — the node is saying it does not know, not that it agrees.
    auto loose = RuntimeVerifier::make(2, c_chain, kEmptyId, m);
    admitted(loose, "an unset X-Chain builds");
    if (loose) {
        Registry reg2({AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});
        admitted(m.apply_to(reg2, **loose, default_dex_asset_policy()),
                 "and then the UTXO asset admits");
    }
}

void refuses_an_asset_the_manifest_never_carried() {
    std::printf("an asset absent from the manifest was never CI-proven\n");
    const Id c_chain = test_id(340);
    const Manifest m = three_kinds(2, c_chain, test_id(341), test_id(342), addr20(0x21));
    auto rv = RuntimeVerifier::make(2, c_chain, kEmptyId, m);
    admitted(rv, "the verifier builds");
    if (!rv) return;

    Asset other{2, c_chain, AssetKind::ERC20, addr20(0x99), 6, "OTH", "", true, RiskTier::Tier0};
    Registry reg({AssetKind::ERC20});
    refused_any(reg.register_asset(other, **rv),
                "a token the manifest does not carry is refused at the node");

    // And the identity confirm answers for exactly the ids it was built with.
    admitted((*rv)->confirm_c_chain(2, 96368, c_chain), "the confirm accepts the bound ids");
    refused_any((*rv)->confirm_c_chain(1, 96368, c_chain), "…refuses another network");
    refused_any((*rv)->confirm_c_chain(2, 96368, test_id(343)), "…and another C-Chain");
}

}  // namespace

int main() {
    binds_to_the_running_chain();
    refuses_the_wrong_c_chain();
    refuses_the_wrong_network();
    refuses_a_utxo_off_the_running_x_chain();
    refuses_an_asset_the_manifest_never_carried();
    return report("runtime");
}
