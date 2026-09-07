// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A REAL verifier backed by an in-memory snapshot of what exists on-chain.
//!
//! It is not a stub: it answers "real" only for entries that were explicitly
//! seeded, and refuses otherwise — so the registry's rejection paths exercise
//! genuine refusal, never a verifier rigged to always succeed. This is the
//! in-test analogue of the production JSON-RPC and local-chain verifiers.

use crate::error::{Error, Result};
use crate::ids::Id;
use crate::registry::ChainVerifier;
use std::collections::BTreeMap;

#[derive(Default)]
pub(crate) struct FakeChain {
    erc20: BTreeMap<(u32, Id, Vec<u8>), u8>,
    native: BTreeMap<(u32, Id), u8>,
    utxo: BTreeMap<(u32, Id, Id), u8>,
    /// When set, the chain also confirms a single declared C-Chain triple — the
    /// `fakeChainConfirm` of the Go suite.
    confirm: Option<(u32, u64, Id)>,
}

impl FakeChain {
    pub(crate) fn new() -> Self {
        FakeChain::default()
    }

    /// Record a real ERC-20 deployment at `addr` on `(network_id, c_chain_id)`.
    pub(crate) fn seed_erc20(
        &mut self,
        network_id: u32,
        c_chain_id: Id,
        addr: &[u8],
        decimals: u8,
    ) {
        self.erc20
            .insert((network_id, c_chain_id, addr.to_vec()), decimals);
    }

    /// Record that `c_chain_id` is the real C-Chain for `network_id`.
    #[allow(dead_code)]
    pub(crate) fn seed_native(&mut self, network_id: u32, c_chain_id: Id, decimals: u8) {
        self.native.insert((network_id, c_chain_id), decimals);
    }

    /// Record a real UTXO asset on `(network_id, source_chain_id)`.
    pub(crate) fn seed_utxo(
        &mut self,
        network_id: u32,
        source_chain_id: Id,
        asset_id: Id,
        decimals: u8,
    ) {
        self.utxo
            .insert((network_id, source_chain_id, asset_id), decimals);
    }

    /// Make this chain also answer the manifest-level identity confirm, for the
    /// single declared triple.
    pub(crate) fn confirming(mut self, network_id: u32, evm_chain_id: u64, c_chain_id: Id) -> Self {
        self.confirm = Some((network_id, evm_chain_id, c_chain_id));
        self
    }
}

/// What a fake chain says when it has never heard of the thing.
pub(crate) fn not_on_chain() -> Error {
    Error::other("fakechain: no such object on this network/chain")
}

impl ChainVerifier for FakeChain {
    fn verify_erc20(&self, network_id: u32, c_chain_id: Id, addr: &[u8]) -> Result<u8> {
        self.erc20
            .get(&(network_id, c_chain_id, addr.to_vec()))
            .copied()
            .ok_or_else(not_on_chain)
    }

    fn verify_evm_native(&self, network_id: u32, c_chain_id: Id) -> Result<u8> {
        self.native
            .get(&(network_id, c_chain_id))
            .copied()
            .ok_or_else(not_on_chain)
    }

    fn verify_utxo_asset(&self, network_id: u32, source_chain_id: Id, asset_id: Id) -> Result<u8> {
        self.utxo
            .get(&(network_id, source_chain_id, asset_id))
            .copied()
            .ok_or_else(not_on_chain)
    }

    fn confirm_c_chain(&self, network_id: u32, evm_chain_id: u64, c_chain_id: Id) -> Result<()> {
        match self.confirm {
            // A chain that declares no triple confirms nothing, which is what a
            // Go verifier that does not implement CChainConfirmer does.
            None => Ok(()),
            Some((n, e, c)) => {
                if network_id != n || evm_chain_id != e || c_chain_id != c {
                    return Err(not_on_chain());
                }
                Ok(())
            }
        }
    }
}

/// A deterministic non-zero 20-byte token address from a seed byte.
pub(crate) fn addr20(seed: u8) -> Vec<u8> {
    (0..20u8)
        // +1 keeps it non-zero even for seed 0, exactly as the Go helper does.
        .map(|i| seed.wrapping_add(i).wrapping_add(1))
        .collect()
}
