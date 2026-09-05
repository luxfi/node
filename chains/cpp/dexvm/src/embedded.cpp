// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/embedded.hpp"

#include "manifest_bytes.hpp"  // generated at configure time from manifests/*.json

#include <string_view>

namespace lux::dexvm {
namespace {

// The generated header carries each manifest as the hex of its EXACT file bytes,
// so the .json files remain the single source of truth and the build cannot
// paraphrase them. The decode is the same from_hex the manifest's own canonical
// references go through.
Result<Bytes> embedded_bytes(std::uint64_t evm_chain_id, std::string* name) {
    switch (evm_chain_id) {
        case kMainnetEVMChainID:
            *name = "manifests/assets.mainnet.json";
            return from_hex(generated::kMainnetManifestHex);
        case kTestnetEVMChainID:
            *name = "manifests/assets.testnet.json";
            return from_hex(generated::kTestnetManifestHex);
        case kDevnetEVMChainID:
            *name = "manifests/assets.devnet.json";
            return from_hex(generated::kDevnetManifestHex);
        default:
            return fail_note(Err::NoEmbeddedManifest,
                             "evmChainID=" + std::to_string(evm_chain_id));
    }
}

}  // namespace

Result<DecodedManifest> embedded_manifest_for(std::uint64_t evm_chain_id) {
    std::string name;
    auto raw = embedded_bytes(evm_chain_id, &name);
    if (!raw) return std::unexpected(raw.error());
    return decode_manifest_bytes(view(*raw), name);
}

Result<Manifest> localnet_native_manifest(std::uint32_t network_id, const Id& c_chain_id) {
    if (c_chain_id == kEmptyId)
        return fail("registry: localnet native manifest requires a non-empty live C-Chain id");

    Manifest m;
    m.network = "localnet";
    m.network_id = network_id;
    m.evm_chain_id = kLocalnetEVMChainID;
    m.c_chain_id = c_chain_id;

    Asset a;
    a.network_id = network_id;
    a.chain_id = c_chain_id;
    a.kind = AssetKind::EVMNative;
    a.canonical_ref = to_bytes(view(kEVMNativeMarker));
    a.decimals = 18;
    a.symbol = "LUX";
    a.name = "Lux";
    a.enabled = true;
    a.risk_tier = RiskTier::Tier0;
    m.assets.push_back(std::move(a));
    m.assets_is_null = false;
    // Markets stays nil, which is what a manifest with no markets holds.

    if (auto r = m.validate_shape(); !r)
        return std::unexpected(r.error().wrap("registry: synthesised localnet native manifest invalid"));
    return m;
}

}  // namespace lux::dexvm
