// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// embedded.hpp — the committed manifests, carried IN the binary.
//
// A validator carries the exact CI-approved asset set with it and never has to
// find a file at boot: there is no on-disk manifest to tamper with, because
// there is no on-disk manifest. This is the registry's trust-root data.
//
// Selection is by the C-Chain's EVM chainID, the unambiguous per-network
// identity:
//
//   96369 -> mainnet   96368 -> testnet   96370 -> devnet   1337 -> localnet
//
// The first three ship a committed manifest. Localnet's C-Chain id is
// environment-specific — it differs per genesis — so no committed file can pin
// it, and localnet's native-only manifest is synthesised at boot from the node's
// LIVE runtime ids instead. That is the whole reason the two paths differ, and
// the synthesised one is still identity-bound: the chain id comes from the
// runtime, never from a constant.

#pragma once

#include "lux/dexvm/manifest.hpp"

namespace lux::dexvm {

// The eth_chainId values — NOT the consensus networkIDs (1/2/3).
inline constexpr std::uint64_t kMainnetEVMChainID = 96369;
inline constexpr std::uint64_t kTestnetEVMChainID = 96368;
inline constexpr std::uint64_t kDevnetEVMChainID = 96370;
inline constexpr std::uint64_t kLocalnetEVMChainID = 1337;

// embedded_manifest_for loads, content-hashes and shape-validates the committed
// manifest for an EVM chainID. An id with no committed manifest — localnet, or
// an unknown sovereign chain — is Err::NoEmbeddedManifest, and the caller then
// either synthesises the localnet manifest or stays fail-closed.
Result<DecodedManifest> embedded_manifest_for(std::uint64_t evm_chain_id);

// localnet_native_manifest synthesises localnet's manifest in memory, holding
// ONLY the C-Chain native coin, rooted at the node's LIVE ids. The native coin
// is always a real, known asset on any localnet C-Chain — it is the chain's own
// coin — so admitting exactly it keeps localnet swaps live out of the box, while
// every ERC-20 must still be registered explicitly because its address is not
// knowable until it is deployed.
Result<Manifest> localnet_native_manifest(std::uint32_t network_id, const Id& c_chain_id);

}  // namespace lux::dexvm
