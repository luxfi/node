// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What this chain charges to admit a user transaction.
//!
//! A DECLARATION at the boundary, orthogonal to settlement: this says what the
//! chain costs to submit to, while the per-operation debit happens inside
//! consensus against the payer's balance. The Z-Chain accepts user-submitted
//! shielded transactions, so it declares the floor, and the entry point checks
//! it BEFORE the mempool — a zero-fee transaction is refused before pool
//! pressure changes.
//!
//! THE ZERO POLICY ADMITS NOTHING. A chain that forgot to declare one refuses
//! every caller rather than admitting every caller.

use crate::error::{Error, Result};
use crate::hash::sha256;
use crate::ids::Id;

/// The minimum any user-facing chain charges, in the base unit (1e-6 LUX).
pub const MIN_TX_FEE_FLOOR: u64 = 1_000_000;

/// What a chain charges.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Policy {
    /// A flat floor in the network's UTXO asset.
    Flat { fee: u64, asset: Id },
    /// This chain accepts no user transactions at all — one driven by
    /// validators through consensus, or one that only reads. Every caller is
    /// refused EXPLICITLY, rather than by omission.
    NoUserTxs,
}

impl Policy {
    /// The canonical declaration for a chain that accepts user work.
    pub fn floor(network_id: u32) -> Policy {
        Policy::Flat {
            fee: MIN_TX_FEE_FLOOR,
            asset: utxo_asset_id(network_id),
        }
    }

    /// Refuse a payment this declaration does not cover.
    pub fn admit(&self, paid: u64) -> Result<()> {
        match self {
            Policy::NoUserTxs => Err(Error::ChainAcceptsNoUserTxs),
            Policy::Flat { fee, .. } if paid < *fee => {
                Err(Error::InsufficientFee { paid, floor: *fee })
            }
            Policy::Flat { .. } => Ok(()),
        }
    }

    /// The boot-time gate: a user-facing chain declaring a zero floor is a
    /// chain that charges nothing, which is a misconfiguration rather than a
    /// choice. The sentinel is exempt because it admits nobody.
    pub fn validate(&self) -> Result<()> {
        match self {
            Policy::NoUserTxs => Ok(()),
            Policy::Flat { fee: 0, .. } => Err(Error::BadRequest(
                "fee policy declares zero min tx fee on a user-facing chain".into(),
            )),
            Policy::Flat { .. } => Ok(()),
        }
    }

    pub fn min_fee(&self) -> u64 {
        match self {
            Policy::Flat { fee, .. } => *fee,
            Policy::NoUserTxs => 0,
        }
    }
}

/// The network-scoped primary UTXO asset.
///
/// Domain-separated by network id so the same address bytes on two networks
/// own outputs with distinct asset ids. Mainnet keeps its existing on-chain
/// value, because renaming it would rename every record already written under
/// it.
pub fn utxo_asset_id(network_id: u32) -> Id {
    const MAINNET: u32 = 1;
    if network_id == MAINNET {
        let mut id = [0u8; 32];
        id[..12].copy_from_slice(b"lux asset id");
        return id;
    }
    let mut preimage = [0u8; 16];
    preimage[..12].copy_from_slice(b"lux asset id");
    preimage[12..].copy_from_slice(&network_id.to_be_bytes());
    sha256(&preimage)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_floor_refuses_what_is_beneath_it() {
        let p = Policy::floor(96369);
        assert_eq!(
            p.admit(MIN_TX_FEE_FLOOR - 1),
            Err(Error::InsufficientFee {
                paid: MIN_TX_FEE_FLOOR - 1,
                floor: MIN_TX_FEE_FLOOR
            })
        );
        assert!(p.admit(MIN_TX_FEE_FLOOR).is_ok());
        assert!(p.admit(u64::MAX).is_ok());
        assert_eq!(
            p.admit(0),
            Err(Error::InsufficientFee {
                paid: 0,
                floor: MIN_TX_FEE_FLOOR
            })
        );
    }

    #[test]
    fn the_sentinel_admits_nobody_and_still_passes_the_boot_gate() {
        assert_eq!(
            Policy::NoUserTxs.admit(1_000_000_000),
            Err(Error::ChainAcceptsNoUserTxs)
        );
        assert!(Policy::NoUserTxs.validate().is_ok());
    }

    #[test]
    fn a_zero_floor_is_caught_at_boot() {
        let p = Policy::Flat {
            fee: 0,
            asset: [0u8; 32],
        };
        assert!(p.validate().is_err());
        assert!(Policy::floor(1).validate().is_ok());
    }

    #[test]
    fn mainnet_keeps_the_asset_id_already_written_down() {
        let mut want = [0u8; 32];
        want[..12].copy_from_slice(b"lux asset id");
        assert_eq!(utxo_asset_id(1), want);
        // Every other network derives its own, so two networks never share it.
        assert_ne!(utxo_asset_id(2), want);
        assert_ne!(utxo_asset_id(2), utxo_asset_id(3));
    }
}
