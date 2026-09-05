// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/runtime_verifier.hpp"

namespace lux::dexvm {

Result<std::unique_ptr<RuntimeVerifier>> RuntimeVerifier::make(std::uint32_t network_id,
                                                               const Id& c_chain_id,
                                                               const Id& x_chain_id,
                                                               const Manifest& m) {
    if (m.network_id != network_id)
        return fail("registry: manifest networkID " + std::to_string(m.network_id) +
                    " != running network " + std::to_string(network_id) + " (wrong-net manifest)");
    if (c_chain_id == kEmptyId)
        return fail("registry: running C-Chain id is empty (cannot bind manifest)");
    if (m.c_chain_id != c_chain_id)
        return fail("registry: manifest cChainID " + cb58(m.c_chain_id) + " != running C-Chain " +
                    cb58(c_chain_id) + " (wrong-chain manifest)");

    std::unique_ptr<RuntimeVerifier> rv(new RuntimeVerifier());
    rv->network_id_ = network_id;
    rv->c_chain_id_ = c_chain_id;
    rv->x_chain_id_ = x_chain_id;
    for (const Asset& a : m.assets) {
        auto id = a.id();
        if (!id) return std::unexpected(id.error().wrap("registry: manifest asset id"));
        rv->declared_decimals_[*id] = a.decimals;
    }
    return rv;
}

Result<void> RuntimeVerifier::confirm_c_chain(std::uint32_t network_id, std::uint64_t,
                                              const Id& c_chain_id) {
    if (network_id != network_id_)
        return fail("manifest network " + std::to_string(network_id) + " != running " +
                    std::to_string(network_id_));
    if (c_chain_id != c_chain_id_)
        return fail("manifest C-Chain " + cb58(c_chain_id) + " != running " + cb58(c_chain_id_));
    return {};
}

Result<std::uint8_t> RuntimeVerifier::decimals_for(std::uint32_t network_id, const Id& chain_id,
                                                   AssetKind kind, ByteView ref) const {
    auto id = derive_asset_id(network_id, chain_id, kind, ref);
    if (!id) return std::unexpected(id.error());
    auto it = declared_decimals_.find(*id);
    if (it == declared_decimals_.end()) {
        // Not in the manifest this verifier was built from: it cannot be
        // admitted here, because reality for it was never CI-proven.
        return fail("asset " + cb58(*id) + " not in the runtime manifest");
    }
    return it->second;
}

Result<std::uint8_t> RuntimeVerifier::verify_erc20(std::uint32_t network_id, const Id& c_chain_id,
                                                   ByteView addr) {
    if (network_id != network_id_)
        return fail("ERC20 network " + std::to_string(network_id) + " != running " +
                    std::to_string(network_id_));
    if (c_chain_id != c_chain_id_)
        return fail("ERC20 rooted at C-Chain " + cb58(c_chain_id) + " but node runs " +
                    cb58(c_chain_id_));
    return decimals_for(network_id, c_chain_id, AssetKind::ERC20, addr);
}

Result<std::uint8_t> RuntimeVerifier::verify_evm_native(std::uint32_t network_id,
                                                        const Id& c_chain_id) {
    if (network_id != network_id_)
        return fail("EVM_NATIVE network " + std::to_string(network_id) + " != running " +
                    std::to_string(network_id_));
    if (c_chain_id != c_chain_id_)
        return fail("EVM_NATIVE rooted at C-Chain " + cb58(c_chain_id) + " but node runs " +
                    cb58(c_chain_id_));
    return decimals_for(network_id, c_chain_id, AssetKind::EVMNative, view(kEVMNativeMarker));
}

Result<std::uint8_t> RuntimeVerifier::verify_utxo_asset(std::uint32_t network_id,
                                                        const Id& source_chain_id,
                                                        const Id& asset_id) {
    if (network_id != network_id_)
        return fail("UTXO network " + std::to_string(network_id) + " != running " +
                    std::to_string(network_id_));
    if (x_chain_id_ != kEmptyId && source_chain_id != x_chain_id_)
        return fail("UTXO rooted at source chain " + cb58(source_chain_id) + " but node X-Chain is " +
                    cb58(x_chain_id_));
    return decimals_for(network_id, source_chain_id, AssetKind::UTXO, view(asset_id));
}

}  // namespace lux::dexvm
