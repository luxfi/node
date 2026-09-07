// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The node-side verifier, used at initialisation.
//!
//! It is a REAL check. It binds a manifest's DECLARED chain identities to the
//! node's ACTUAL running chain ids, so a manifest built for the wrong network —
//! or one that points an "ERC20" at a chain the node is not running — is
//! REFUSED at startup. It returns the manifest's shape-validated decimals only
//! AFTER that binding holds.
//!
//! The proof is deliberately divided:
//!
//! - The NODE (here) proves chain-IDENTITY binding, structure and policy at
//!   boot, with NO external RPC dependency — so a validator can start even if a
//!   remote RPC is briefly unreachable. It catches a wrong-net or wrong-chain
//!   manifest.
//! - CI ([`crate::rpcverify`]) proves each token EXISTS on the live target net
//!   BEFORE the manifest artifact ships. It catches a fabricated or typo'd token
//!   address.
//!
//! Both are real; neither is always-true. A manifest that passes CI (reality)
//! and this verifier (identity and policy) is admissible; either failing refuses
//! it.

use crate::asset::{derive_asset_id, AssetKind, EVM_NATIVE_MARKER};
use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::manifest::Manifest;
use crate::registry::ChainVerifier;
use std::collections::BTreeMap;

/// A verifier bound to the node's running chain ids.
#[derive(Debug, Clone)]
pub struct RuntimeVerifier {
    /// The node's running network id.
    pub network_id: u32,
    /// The node's running C-Chain consensus id. EVM_NATIVE and ERC20 assets
    /// must be rooted here.
    pub c_chain_id: Id,
    /// The node's running X-Chain id. When it is set, a UTXO asset rooted off
    /// it is refused — you cannot import a UTXO asset from a chain this node
    /// does not run.
    pub x_chain_id: Id,
    /// The manifest's shape-validated decimals, keyed by canonical AssetID, so
    /// the verifier can answer with the asset's decimals after the identity bind
    /// without re-reading the file.
    declared_decimals: BTreeMap<Id, u8>,
}

impl RuntimeVerifier {
    /// A verifier bound to the node's running chain ids and pre-loaded with the
    /// manifest's decimals. `m` must already have passed shape validation.
    pub fn new(
        network_id: u32,
        c_chain_id: Id,
        x_chain_id: Id,
        m: &Manifest,
    ) -> Result<RuntimeVerifier> {
        if m.network_id != network_id {
            return Err(Error::other(format!(
                "registry: manifest networkID {} != running network {network_id} (wrong-net manifest)",
                m.network_id
            )));
        }
        if ids::is_empty(&c_chain_id) {
            return Err(Error::other(
                "registry: running C-Chain id is empty (cannot bind manifest)",
            ));
        }
        if m.c_chain_id != c_chain_id {
            return Err(Error::other(format!(
                "registry: manifest cChainID {} != running C-Chain {} (wrong-chain manifest)",
                ids::cb58(&m.c_chain_id),
                ids::cb58(&c_chain_id)
            )));
        }
        let mut declared_decimals = BTreeMap::new();
        for a in &m.assets {
            let id = a.id().map_err(|e| e.wrap("registry: manifest asset id"))?;
            declared_decimals.insert(id, a.decimals);
        }
        Ok(RuntimeVerifier {
            network_id,
            c_chain_id,
            x_chain_id,
            declared_decimals,
        })
    }

    fn decimals_for(&self, network_id: u32, chain_id: Id, kind: AssetKind, r: &[u8]) -> Result<u8> {
        let id = derive_asset_id(network_id, chain_id, kind, r)?;
        self.declared_decimals.get(&id).copied().ok_or_else(|| {
            // The asset is not in the manifest this verifier was built from — it
            // cannot be admitted at the node, because reality for it was never
            // CI-proven.
            Error::other(format!(
                "asset {} not in the runtime manifest",
                ids::cb58(&id)
            ))
        })
    }
}

impl ChainVerifier for RuntimeVerifier {
    /// Bind the ERC-20 to the node's running C-Chain. Per-token code existence
    /// is the CI gate; here the asset must be on the chain the node runs.
    fn verify_erc20(&self, network_id: u32, c_chain_id: Id, addr: &[u8]) -> Result<u8> {
        if network_id != self.network_id {
            return Err(Error::other(format!(
                "ERC20 network {network_id} != running {}",
                self.network_id
            )));
        }
        if c_chain_id != self.c_chain_id {
            return Err(Error::other(format!(
                "ERC20 rooted at C-Chain {} but node runs {}",
                ids::cb58(&c_chain_id),
                ids::cb58(&self.c_chain_id)
            )));
        }
        self.decimals_for(network_id, c_chain_id, AssetKind::Erc20, addr)
    }

    /// Bind the native coin to the node's running C-Chain.
    fn verify_evm_native(&self, network_id: u32, c_chain_id: Id) -> Result<u8> {
        if network_id != self.network_id {
            return Err(Error::other(format!(
                "EVM_NATIVE network {network_id} != running {}",
                self.network_id
            )));
        }
        if c_chain_id != self.c_chain_id {
            return Err(Error::other(format!(
                "EVM_NATIVE rooted at C-Chain {} but node runs {}",
                ids::cb58(&c_chain_id),
                ids::cb58(&self.c_chain_id)
            )));
        }
        self.decimals_for(
            network_id,
            c_chain_id,
            AssetKind::EvmNative,
            &EVM_NATIVE_MARKER,
        )
    }

    /// Bind the UTXO asset to a UTXO source chain the node runs.
    fn verify_utxo_asset(&self, network_id: u32, source_chain_id: Id, asset_id: Id) -> Result<u8> {
        if network_id != self.network_id {
            return Err(Error::other(format!(
                "UTXO network {network_id} != running {}",
                self.network_id
            )));
        }
        if !ids::is_empty(&self.x_chain_id) && source_chain_id != self.x_chain_id {
            return Err(Error::other(format!(
                "UTXO rooted at source chain {} but node X-Chain is {}",
                ids::cb58(&source_chain_id),
                ids::cb58(&self.x_chain_id)
            )));
        }
        self.decimals_for(network_id, source_chain_id, AssetKind::Utxo, &asset_id)
    }

    /// The manifest's network and C-Chain id must equal the node's running ids.
    /// The EVM chainID is RPC-checked by CI; at boot the consensus C-Chain id is
    /// bound, which is the authoritative cross-chain identity.
    fn confirm_c_chain(&self, network_id: u32, _evm_chain_id: u64, c_chain_id: Id) -> Result<()> {
        if network_id != self.network_id {
            return Err(Error::other(format!(
                "manifest network {network_id} != running {}",
                self.network_id
            )));
        }
        if c_chain_id != self.c_chain_id {
            return Err(Error::other(format!(
                "manifest C-Chain {} != running {}",
                ids::cb58(&c_chain_id),
                ids::cb58(&self.c_chain_id)
            )));
        }
        Ok(())
    }
}
