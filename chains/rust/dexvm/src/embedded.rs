// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The manifests a node binary carries with it.
//!
//! Embedding them means a validator holds the exact, CI-approved asset set in
//! its binary and never has to locate a file on disk at boot. This is the
//! registry's trust-root data.
//!
//! Selection is by the C-Chain's EVM chainID, the unambiguous per-network
//! identity:
//!
//! ```text
//! 96369 -> mainnet  (networkID 1)
//! 96368 -> testnet  (networkID 2)
//! 96370 -> devnet   (networkID 3)
//!  1337 -> localnet (networkID 1337)
//! ```
//!
//! The three value and dev networks ship a committed manifest. Localnet's
//! C-Chain id is environment-specific — it differs per genesis — so localnet has
//! no committed manifest: its native-only manifest is synthesised at boot from
//! the node's LIVE runtime C-Chain id, never from a constant.

use crate::asset::{AssetKind, EVM_NATIVE_MARKER};
use crate::error::{Code, Error, Result};
use crate::ids::{self, Id};
use crate::manifest::{decode_manifest_bytes, Manifest};
use crate::registry::{Asset, RISK_TIER0};

/// The `eth_chainId` of each network that ships a committed manifest. These are
/// EVM chainIDs, not the consensus networkIDs.
pub const MAINNET_EVM_CHAIN_ID: u64 = 96369;
pub const TESTNET_EVM_CHAIN_ID: u64 = 96368;
pub const DEVNET_EVM_CHAIN_ID: u64 = 96370;
/// Localnet's, handled separately because its C-Chain id is not fixed.
pub const LOCALNET_EVM_CHAIN_ID: u64 = 1337;

const MAINNET_JSON: &[u8] = include_bytes!("../manifests/assets.mainnet.json");
const TESTNET_JSON: &[u8] = include_bytes!("../manifests/assets.testnet.json");
const DEVNET_JSON: &[u8] = include_bytes!("../manifests/assets.devnet.json");

/// The committed manifest bytes for an EVM chainID, and the name to report them
/// under. A chainID with no committed manifest is absent — the caller then
/// either synthesises the localnet native manifest or stays fail-closed.
fn embedded_manifest(evm_chain_id: u64) -> Option<(&'static str, &'static [u8])> {
    match evm_chain_id {
        MAINNET_EVM_CHAIN_ID => Some(("manifests/assets.mainnet.json", MAINNET_JSON)),
        TESTNET_EVM_CHAIN_ID => Some(("manifests/assets.testnet.json", TESTNET_JSON)),
        DEVNET_EVM_CHAIN_ID => Some(("manifests/assets.devnet.json", DEVNET_JSON)),
        _ => None,
    }
}

/// Decode, content-hash and shape-validate the committed manifest for an EVM
/// chainID, returning the manifest and its bytes' SHA-256 in lowercase hex.
///
/// It is the node-side entry point that replaces a filesystem load: the bytes
/// are the ones compiled into the binary, so there is no on-disk file to tamper
/// with. A chainID with no committed manifest is refused with
/// [`Code::NoEmbeddedManifest`] — the localnet and unknown-chain case the caller
/// handles explicitly.
pub fn embedded_manifest_for(evm_chain_id: u64) -> Result<(Manifest, String)> {
    let (name, raw) = embedded_manifest(evm_chain_id).ok_or_else(|| {
        Error::note(
            Code::NoEmbeddedManifest,
            format!("evmChainID={evm_chain_id}"),
        )
    })?;
    let (sum, m) = decode_manifest_bytes(raw, name);
    Ok((m?, sum))
}

/// Synthesise the localnet manifest IN MEMORY, containing ONLY the C-Chain
/// native coin, rooted at the node's LIVE runtime ids.
///
/// It exists because localnet's C-Chain consensus id is not fixed across genesis
/// runs, so no committed manifest can pin it — but the native coin is ALWAYS a
/// real, known asset on any localnet C-Chain: it is the chain's own coin.
/// Admitting exactly it and nothing else keeps localnet swaps live out of the
/// box, while every ERC-20 must still be registered explicitly, since its
/// address is unknown until it is deployed.
///
/// `c_chain_id` comes from the runtime, NEVER a constant — so the resulting
/// registry is identity-bound exactly like the committed-manifest path.
pub fn localnet_native_manifest(network_id: u32, c_chain_id: Id) -> Result<Manifest> {
    if ids::is_empty(&c_chain_id) {
        return Err(Error::other(
            "registry: localnet native manifest requires a non-empty live C-Chain id",
        ));
    }
    let m = Manifest {
        network: "localnet".to_string(),
        network_id,
        evm_chain_id: LOCALNET_EVM_CHAIN_ID,
        c_chain_id,
        chain_labels: None,
        assets: vec![Asset {
            network_id,
            chain_id: c_chain_id,
            kind: AssetKind::EvmNative,
            canonical_ref: EVM_NATIVE_MARKER.as_slice().into(),
            decimals: 18,
            symbol: "LUX".to_string(),
            name: "Lux".to_string(),
            enabled: true,
            risk_tier: RISK_TIER0,
        }],
        markets: Vec::new(),
    };
    m.validate_shape()
        .map_err(|e| e.wrap("registry: synthesised localnet native manifest invalid"))?;
    Ok(m)
}
