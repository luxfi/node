// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// embedded_test — the committed manifests, and what the binary derives from
// them.
//
// This is a CROSS-LANGUAGE assertion, not a self-check. The hashes, the C-Chain
// ids and the AssetIDs below were printed by the GO reference reading the SAME
// three files. If the C++ side reads different bytes, spells an id differently,
// or derives another AssetID, one of the two homes is wrong about which assets
// the DEX admits — which is the whole property this chain exists to hold.

#include "lux/dexvm/embedded.hpp"
#include "lux/dexvm/runtime_verifier.hpp"

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

void the_three_committed_manifests() {
    std::printf("the committed manifests, against what Go reads from the same files\n");
    for (const golden::EmbeddedManifest& want :
         {golden::kMainnet, golden::kTestnet, golden::kDevnet}) {
        const std::string at = want.name;
        auto d = embedded_manifest_for(want.evm_chain_id);
        admitted(d, at + " is carried in the binary");
        if (!d) continue;

        check_eq(d->sha256_hex, want.sha256, at + ": the bytes hash as Go hashes them");
        check_eq(d->manifest.network, at, at + ": names itself");
        check(d->manifest.network_id == want.network_id, at + ": declares the right networkID");
        check(d->manifest.evm_chain_id == want.evm_chain_id, at + ": and the right EVM chainID");
        check_eq(cb58(d->manifest.c_chain_id), want.c_chain_cb58,
                 at + ": spells its C-Chain id as Go spells it");

        check(d->manifest.assets.size() == 1, at + ": carries its one asset");
        if (d->manifest.assets.empty()) continue;
        auto id = d->manifest.assets[0].id();
        admitted(id, at + ": the asset derives an id");
        if (id) check_eq(hex(*id), want.asset_id, at + ": and it is the id Go derives");

        // Re-encoding is checked against GO'S OWN re-encode of the same file,
        // not against the file: these manifests were hand-written, so they carry
        // a trailing newline and unsorted chainLabels that no marshaller
        // reproduces — Go's re-encode does not equal Go's file either. What must
        // hold is that the two WRITERS agree.
        const std::string re = d->manifest.encode();
        const Bytes raw(re.begin(), re.end());
        check_eq(hex(view(sha256(view(raw)))), want.reencode_sha256,
                 at + ": and re-encoding writes exactly what Go's writer writes");
    }
}

void an_unknown_chain_is_fail_closed() {
    std::printf("a chain with no committed manifest\n");
    refused(embedded_manifest_for(kLocalnetEVMChainID), Err::NoEmbeddedManifest,
            "localnet has none — its C-Chain id is not fixed");
    refused(embedded_manifest_for(8675309), Err::NoEmbeddedManifest,
            "and neither does an unknown sovereign chain");
}

void localnet_is_synthesised_from_live_ids() {
    std::printf("localnet's manifest is synthesised from the node's LIVE ids\n");
    const Id live_c = test_id(400);
    auto m = localnet_native_manifest(1337, live_c);
    admitted(m, "it synthesises");
    if (!m) return;

    check(m->assets.size() == 1, "holding exactly the native coin");
    check(m->assets[0].kind == AssetKind::EVMNative, "…which is EVM_NATIVE");
    check(m->assets[0].chain_id == live_c, "rooted at the LIVE C-Chain, not a constant");
    check(m->markets_is_null, "and no markets");
    check(m->evm_chain_id == kLocalnetEVMChainID, "declaring localnet's EVM chainID");

    // Two different localnets are two different assets, which is the point of
    // rooting it in the runtime id.
    auto other = localnet_native_manifest(1337, test_id(401));
    admitted(other, "another localnet synthesises too");
    if (other) {
        auto a = m->assets[0].id();
        auto b = other->assets[0].id();
        check(a && b && *a != *b, "and its native coin is a different asset");
    }

    refused_any(localnet_native_manifest(1337, kEmptyId),
                "with no live C-Chain id there is nothing to root, so it refuses");

    // It admits end to end through the node's own verifier.
    auto rv = RuntimeVerifier::make(1337, live_c, kEmptyId, *m);
    admitted(rv, "the runtime verifier binds to it");
    if (rv) {
        Registry reg({AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});
        admitted(m->apply_to(reg, **rv, default_dex_asset_policy()),
                 "and localnet's native coin admits, so swaps are live out of the box");
        check(reg.len() == 1, "with nothing else admitted — an ERC-20 must still be registered");
    }
}

void a_committed_manifest_admits_through_the_node_verifier() {
    std::printf("a committed manifest admits end to end at a node running its chain\n");
    auto d = embedded_manifest_for(kMainnetEVMChainID);
    admitted(d, "mainnet loads");
    if (!d) return;

    auto rv = RuntimeVerifier::make(d->manifest.network_id, d->manifest.c_chain_id, kEmptyId,
                                    d->manifest);
    admitted(rv, "a node running that C-Chain binds to it");
    if (!rv) return;

    Registry reg({AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});
    admitted(d->manifest.apply_to(reg, **rv, default_dex_asset_policy()),
             "and it admits and passes the boot gate");
    check(reg.len() == d->manifest.assets.size(), "with every declared asset in");

    // A node running a DIFFERENT C-Chain must not bind it.
    refused_any(RuntimeVerifier::make(d->manifest.network_id, test_id(410), kEmptyId, d->manifest),
                "a node on another C-Chain refuses it");
}

}  // namespace

int main() {
    the_three_committed_manifests();
    an_unknown_chain_is_fail_closed();
    localnet_is_synthesised_from_live_ids();
    a_committed_manifest_admits_through_the_node_verifier();
    return report("embedded");
}
