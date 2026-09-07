// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The per-network declaration of what trades.
//!
//! There is exactly one manifest per network. Every entry must be real on the
//! named network; CI proves this against the network's RPC before any deploy,
//! and the node re-proves the identity binding at startup.
//!
//! The manifest does NOT carry derived AssetIDs or MarketIDs — those are
//! computed from the canonical fields, so the file cannot disagree with the
//! identity.
//!
//! A manifest's bytes are its own pin. The content SHA-256 is taken over the
//! EXACT bytes read, so a pinned-hash check binds the loaded manifest to the
//! CI-approved artifact byte-for-byte. That is what stops a locally edited file
//! — a fabricated token address — from being loaded by a node that holds no EVM
//! state of its own and so cannot look the token up itself.

use crate::asset::AssetKind;
use crate::error::{Code, Error, Result};
use crate::gate::{network_class_for, refuse_under_synthetic_config, DexAssetPolicy};
use crate::ids::{self, Id};
use crate::market::Market;
use crate::registry::{Asset, ChainVerifier, Registry};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

/// The on-disk declaration of the REAL assets and markets the DEX admits on one
/// network.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, default)]
pub struct Manifest {
    /// The canonical network name. It must match the deploy target; a mismatch
    /// is a hard error — you cannot ship the testnet manifest to mainnet.
    pub network: String,
    /// The Lux networkID every asset and market in this manifest must declare.
    #[serde(rename = "networkID")]
    pub network_id: u32,
    /// The C-Chain's EVM chainID (`eth_chainId`). It is the AUTHORITATIVE,
    /// RPC-checkable identity of the C-Chain: the CI validator confirms the
    /// target RPC reports this before admitting any ERC-20 or native entry, so
    /// a manifest can never be validated against the wrong chain.
    #[serde(rename = "evmChainID")]
    pub evm_chain_id: u64,
    /// The canonical C-Chain CONSENSUS id, used in the AssetID preimage so a
    /// derived AssetID lives in the same identity space as the on-chain atomic
    /// objects. EVM_NATIVE and ERC20 entries are rooted here.
    #[serde(rename = "cChainID", with = "crate::ids::serde_cb58")]
    pub c_chain_id: Id,
    /// Source chain id (as HEX, which is how Go keys it) to human label,
    /// consumed by the forbidden-reference deny-scan. Optional; an unlabeled
    /// chain simply has no white-label name to match.
    #[serde(
        rename = "chainLabels",
        skip_serializing_if = "Option::is_none",
        default
    )]
    pub chain_labels: Option<BTreeMap<String, String>>,
    /// The declared real entries.
    pub assets: Vec<Asset>,
    pub markets: Vec<Market>,
}

impl Default for Manifest {
    fn default() -> Self {
        Manifest {
            network: String::new(),
            network_id: 0,
            evm_chain_id: 0,
            c_chain_id: ids::EMPTY,
            chain_labels: None,
            assets: Vec::new(),
            markets: Vec::new(),
        }
    }
}

impl Manifest {
    /// Whether the manifest is internally consistent, before any chain I/O:
    /// network name present, every asset and market on the manifest's network,
    /// every C-Chain asset rooted at the manifest's C-Chain, and every asset
    /// structurally valid.
    pub(crate) fn validate_shape(&self) -> Result<()> {
        if self.network.is_empty() {
            return Err(Error::other("manifest: empty network name"));
        }
        if self.network_id == 0 {
            return Err(Error::other("manifest: networkID must be non-zero"));
        }
        if self.evm_chain_id == 0 {
            return Err(Error::other(
                "manifest: evmChainID must be non-zero (the RPC-checkable C-Chain identity)",
            ));
        }
        if ids::is_empty(&self.c_chain_id) {
            return Err(Error::other(
                "manifest: cChainID must be set (the C-Chain consensus id)",
            ));
        }
        for (i, a) in self.assets.iter().enumerate() {
            if a.network_id != self.network_id {
                return Err(Error::other(format!(
                    "manifest: asset[{i}] networkID {} != manifest networkID {}",
                    a.network_id, self.network_id
                )));
            }
            match a.kind {
                AssetKind::EvmNative | AssetKind::Erc20 => {
                    if a.chain_id != self.c_chain_id {
                        return Err(Error::other(format!(
                            "manifest: asset[{i}] ({}) chainID must be the C-Chain {}, got {}",
                            a.kind,
                            ids::cb58(&self.c_chain_id),
                            ids::cb58(&a.chain_id)
                        )));
                    }
                }
                // A UTXO asset is rooted at a UTXO source chain, not the C-Chain.
                AssetKind::Utxo => {}
                AssetKind::Invalid => {
                    return Err(Error::other(format!("manifest: asset[{i}] invalid kind")))
                }
            }
            a.validate_shape()
                .map_err(|e| e.wrap(format!("manifest: asset[{i}]")))?;
        }
        for (i, m) in self.markets.iter().enumerate() {
            if m.network_id != self.network_id {
                return Err(Error::other(format!(
                    "manifest: market[{i}] networkID {} != manifest networkID {}",
                    m.network_id, self.network_id
                )));
            }
        }
        Ok(())
    }

    /// The deny-scan label lookup for this manifest's declared chain labels.
    /// The map is keyed by an id's HEX, which is what Go writes there — reading
    /// it as cb58 would find nothing and silently pass every scan.
    pub fn chain_label_for(&self) -> impl Fn(Id) -> String + '_ {
        move |id: Id| match &self.chain_labels {
            None => String::new(),
            Some(m) => m.get(&ids::hex(&id)).cloned().unwrap_or_default(),
        }
    }

    /// Register every manifest asset (each proven real) and create every
    /// manifest market (each pinned to two registered assets), WITHOUT running
    /// the startup gate.
    ///
    /// It is the admission half of [`Manifest::apply_to`], separated so a caller
    /// that owns the gate — the node, which runs it once with its own network
    /// class and policy — does not run it twice.
    pub fn admit_into(&self, reg: &Registry, v: &dyn ChainVerifier) -> Result<()> {
        self.validate_shape()?;
        v.confirm_c_chain(self.network_id, self.evm_chain_id, self.c_chain_id)
            .map_err(|e| {
                e.wrap(format!(
                    "manifest {}: C-Chain identity confirm",
                    self.network
                ))
            })?;
        for (i, a) in self.assets.iter().enumerate() {
            reg.register(a, v).map_err(|e| {
                e.wrap(format!(
                    "manifest {}: asset[{i}] ({})",
                    self.network, a.kind
                ))
            })?;
        }
        for (i, m) in self.markets.iter().enumerate() {
            reg.create_market(m)
                .map_err(|e| e.wrap(format!("manifest {}: market[{i}]", self.network)))?;
        }
        Ok(())
    }

    /// Admit everything, then run the fail-closed startup gate for this
    /// manifest's network class and the given policy.
    ///
    /// This is the ONE routine that turns a manifest into a live, gated registry
    /// — used identically by CI with an RPC verifier and by node startup with
    /// the local-chain verifier. Any failure aborts; `reg` is left partially
    /// populated only on error, and callers discard it.
    pub fn apply_to(
        &self,
        reg: &Registry,
        v: &dyn ChainVerifier,
        policy: &DexAssetPolicy,
    ) -> Result<()> {
        self.admit_into(reg, v)?;
        let class = network_class_for(self.network_id);
        let label = self.chain_label_for();
        refuse_under_synthetic_config(class, policy, reg, Some(&label))
            .map_err(|e| e.wrap(format!("manifest {}: startup gate", self.network)))
    }

    /// The full CI check for one manifest against one verifier: prove every
    /// entry real and gate-clean in a fresh registry under the canonical
    /// locked-down policy. The populated registry is returned so a caller can
    /// report what was admitted.
    pub fn validate(&self, v: &dyn ChainVerifier) -> Result<Registry> {
        let reg = Registry::new(&[AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]);
        self.apply_to(&reg, v, &DexAssetPolicy::default_policy())?;
        Ok(reg)
    }
}

/// Content-hash and decode raw manifest bytes — from a file or from the
/// binary's own embedded copy — returning the manifest and the bytes' SHA-256 in
/// lowercase hex.
///
/// This is the SINGLE manifest decoder, so shape validation, unknown-field
/// rejection and content-hash discipline are identical for both. `label` is
/// error context only: a path, or an embed name.
pub fn decode_manifest_bytes(raw: &[u8], label: &str) -> (String, Result<Manifest>) {
    let hex_sum = ids::hex(&ids::sha256(raw));
    // A typo'd field (`asssets`) fails closed — `deny_unknown_fields` on every
    // struct in the tree is what Go's DisallowUnknownFields does for the whole
    // decode.
    let decoded: std::result::Result<Manifest, _> = serde_json::from_slice(raw);
    let m = match decoded {
        Ok(m) => m,
        Err(e) => {
            return (
                hex_sum,
                Err(Error::other(format!("manifest: decode {label}: {e}"))),
            )
        }
    };
    if let Err(e) = m.validate_shape() {
        return (hex_sum, Err(e.wrap(format!("manifest {label}"))));
    }
    (hex_sum, Ok(m))
}

/// Read a manifest file and its content hash.
fn load_manifest_bytes(path: &std::path::Path) -> (String, Result<Manifest>) {
    let raw = match std::fs::read(path) {
        Ok(r) => r,
        Err(e) => {
            return (
                String::new(),
                Err(Error::other(format!(
                    "manifest: read {}: {e}",
                    path.display()
                ))),
            )
        }
    };
    decode_manifest_bytes(&raw, &path.display().to_string())
}

/// Read and decode a manifest file. It does NOT verify against chain state —
/// that is [`Manifest::apply_to`], which needs a verifier — only parse and
/// structurally validate. A malformed kind, ref or tier fails here.
pub fn load_manifest(path: &std::path::Path) -> Result<Manifest> {
    load_manifest_bytes(path).1
}

/// Read a manifest and REFUSE it unless its content SHA-256 equals
/// `expected_sha256` (lowercase hex, with or without a `0x` or `sha256:`
/// prefix).
///
/// An empty expectation means "no pin configured" and falls back to
/// [`load_manifest`] — pinning is opt-in per deployment, but once a hash is set
/// the file must match it exactly. A malformed expectation is itself an error:
/// you cannot pin against garbage.
pub fn load_manifest_pinned(path: &std::path::Path, expected_sha256: &str) -> Result<Manifest> {
    if expected_sha256.is_empty() {
        return load_manifest(path);
    }
    let want = normalize_sha256(expected_sha256)?;
    let (got, m) = load_manifest_bytes(path);
    let m = m?;
    if got != want {
        return Err(Error::note(
            Code::ManifestHashMismatch,
            format!("path={} want={want} got={got}", path.display()),
        ));
    }
    Ok(m)
}

/// Lowercase, strip an optional `0x` or `sha256:` prefix, and check the value is
/// a 64-hex-character digest. A non-conforming value fails closed.
pub fn normalize_sha256(h: &str) -> Result<String> {
    let s = h.trim().to_lowercase();
    let s = s.strip_prefix("sha256:").unwrap_or(&s);
    let s = s.strip_prefix("0x").unwrap_or(s);
    if s.len() != 64 {
        return Err(Error::other(format!(
            "registry: pinned manifest hash must be a 32-byte SHA-256 (64 hex chars), got {} chars",
            s.len()
        )));
    }
    if !s.bytes().all(|c| c.is_ascii_hexdigit()) {
        return Err(Error::other(
            "registry: pinned manifest hash is not valid hex",
        ));
    }
    Ok(s.to_string())
}
