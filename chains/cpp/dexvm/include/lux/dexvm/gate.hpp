// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gate.hpp — the single fail-closed boot gate, and the policy it reads.
//
// The gate runs ONCE at Initialize, before the chain accepts any work, over the
// already-populated registry. It returns success only when the registry is
// fully real and the policy is locked down for the network; anything ambiguous
// refuses, and a refusal is a hard init failure rather than a warning.
//
// Every policy field's SAFE value is its zero value, so a zero-initialised
// policy is the locked-down policy. The flags live in the VM config and are read
// once — a front end cannot relax them.

#pragma once

#include "lux/dexvm/registry.hpp"

#include <functional>
#include <string>
#include <vector>

namespace lux::dexvm {

// NetworkClass separates the value-bearing networks, where synthetic anything is
// forbidden, from dev networks, where a developer may opt into it — and only
// there.
enum class NetworkClass : std::uint8_t {
    Dev = 0,      // devnet / localnet — synthetic flags MAY be set
    Testnet = 1,  // synthetic flags FORBIDDEN
    Mainnet = 2,  // synthetic flags FORBIDDEN
};

std::string_view to_string(NetworkClass c);

// value_bearing reports whether this class forbids every synthetic flag. Mainnet
// and testnet both do.
bool value_bearing(NetworkClass c);

// network_class_for maps a Lux networkID to its class using the convention-fixed
// ids (1 mainnet, 2 testnet, 3 local, 1337 localnet) and treats every other id —
// every sovereign L1 — as dev-class for the purpose of the synthetic flags.
// Value on those is still guarded by the consensus-mode guard; this class
// governs only the flags.
NetworkClass network_class_for(std::uint32_t network_id);

// DexAssetPolicy is the backend-enforced, fail-closed configuration for the
// real-assets-only posture:
//
//   allow_synthetic_assets   = false
//   allow_synthetic_markets  = false
//   allow_mock_liquidity     = false
//   allowed_asset_kinds      = [EVM_NATIVE, ERC20, UTXO]
struct DexAssetPolicy {
    // On a value-bearing network any true value fails startup. On a dev network
    // a true value is a permitted developer opt-in, still subject to the
    // forbidden-reference scan — an off-network universe is never allowed.
    bool allow_synthetic_assets = false;
    bool allow_synthetic_markets = false;
    bool allow_mock_liquidity = false;
    // The active allowed-kinds set. Empty means the canonical default; an
    // explicit set may only ever be a SUBSET of the three real kinds.
    std::vector<AssetKind> allowed_asset_kinds;

    // kinds is the effective set: the canonical three when unset, else exactly
    // what was configured.
    std::vector<AssetKind> kinds() const;
    // any_synthetic_flag is the predicate the node uses to MACHINE-DERIVE the
    // labeled-CFT real-assets-only assertion rather than trusting an
    // operator-supplied claim.
    bool any_synthetic_flag() const;
};

// default_dex_asset_policy is the canonical locked-down policy: no synthetic
// anything, all three real kinds allowed.
DexAssetPolicy default_dex_asset_policy();

// refuse_under_synthetic_config is THE boot gate. It refuses if any of:
//
//   1. the allowed-kinds list holds a non-real kind (it may only ever be a
//      subset of the three);
//   2. the network is value-bearing AND any synthetic flag is true;
//   3. any registered asset carries a forbidden reference — an off-network
//      white-label universe chain, mock/synthetic/phantom liquidity, or an
//      ASCII-ticker asset id;
//   4. any ENABLED market references an asset that is not registered
//      (unknown/synthetic), references a disabled asset, or spans a network
//      mismatch.
//
// The asset deny-scan runs before the market scan so a tainted asset is reported
// at its source. chain_label_for maps a source chain id to its human label so
// the off-network scan can run; an empty label simply has no brand to match, and
// the asset already passed the reality gate at registration.
Result<void> refuse_under_synthetic_config(
    NetworkClass network_class, const DexAssetPolicy& policy, const Registry& reg,
    const std::function<std::string(const Id&)>& chain_label_for);

}  // namespace lux::dexvm
