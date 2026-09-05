// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// runtime_verifier.hpp — the node-side verifier used at Initialize.
//
// It is a REAL check. It binds a manifest's DECLARED chain identities to the
// node's ACTUAL running chain ids, so a manifest built for the wrong network, or
// one pointing an "ERC20" at a chain this node is not running, is refused at
// boot. It returns a shape-validated asset's decimals only after that binding
// holds, and refuses outright any asset the manifest does not contain.
//
// The division of proof is deliberate: this side proves IDENTITY and policy with
// no external RPC in the path, so a validator can start when a remote endpoint
// is briefly unreachable, while CI proves each token EXISTS on the live target
// net before the artifact ships. Both are real; neither is "always true". A
// manifest that passes both is admissible, and either failing refuses it.

#pragma once

#include "lux/dexvm/manifest.hpp"
#include "lux/dexvm/registry.hpp"

#include <map>
#include <memory>

namespace lux::dexvm {

class RuntimeVerifier final : public ChainVerifier {
public:
    // Builds a verifier bound to the node's running ids and pre-loaded with the
    // manifest's shape-validated decimals. m must already have passed
    // validate_shape. A manifest whose network or C-Chain is not the one the
    // node runs is refused HERE, before any asset is looked at.
    static Result<std::unique_ptr<RuntimeVerifier>> make(std::uint32_t network_id,
                                                         const Id& c_chain_id, const Id& x_chain_id,
                                                         const Manifest& m);

    // The manifest's network and C-Chain must equal the node's running ids. The
    // EVM chainID is CI's to check by RPC; at boot the consensus C-Chain id is
    // the authoritative cross-chain identity, so that is what binds.
    bool confirms_c_chain() const override { return true; }
    Result<void> confirm_c_chain(std::uint32_t network_id, std::uint64_t evm_chain_id,
                                 const Id& c_chain_id) override;

    Result<std::uint8_t> verify_erc20(std::uint32_t network_id, const Id& c_chain_id,
                                      ByteView addr) override;
    Result<std::uint8_t> verify_evm_native(std::uint32_t network_id, const Id& c_chain_id) override;
    Result<std::uint8_t> verify_utxo_asset(std::uint32_t network_id, const Id& source_chain_id,
                                           const Id& asset_id) override;

    std::uint32_t network_id() const { return network_id_; }
    const Id& c_chain_id() const { return c_chain_id_; }
    const Id& x_chain_id() const { return x_chain_id_; }

private:
    RuntimeVerifier() = default;

    Result<std::uint8_t> decimals_for(std::uint32_t network_id, const Id& chain_id, AssetKind kind,
                                      ByteView ref) const;

    std::uint32_t network_id_ = 0;
    Id c_chain_id_{};
    // When set, a UTXO asset rooted off it is refused: you cannot import a UTXO
    // asset from a chain this node does not run.
    Id x_chain_id_{};
    // The manifest's own decimals, keyed by canonical AssetID, so the decimals
    // cross-check compares the manifest against itself consistently and the
    // identity bind is what gates admission.
    std::map<Id, std::uint8_t> declared_decimals_;
};

}  // namespace lux::dexvm
