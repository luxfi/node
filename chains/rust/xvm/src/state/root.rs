// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The execution root: one 32-byte commitment to everything the chain holds
//! after a block.
//!
//! Three families are folded separately — the unspent outputs, the assets, the
//! transactions — and then composed with the parent root and the height. Each
//! family is an RFC 6962 tagged Merkle tree over per-leaf Keccak digests, in
//! ascending slot order, skipping the slots a family calls unoccupied.
//!
//! The leaf layouts here are not a design choice — they are the layout the GPU
//! state-root kernels hash, field for field and in this order, with integers
//! little-endian. A leaf preimage that differs by one byte is a root that
//! differs entirely, and a node whose root disagrees is a node that is voting
//! against its own network.
//!
//! The final compose is a plain untagged Keccak, NOT a Merkle node: it is a
//! fixed-shape record of five known fields, not a tree over an unknown number
//! of leaves, and tagging it would say otherwise.

use crate::hash::{empty_root, keccak256, merkle_root, Hash256};
use crate::ids::Id;

/// The width of every root and every leaf digest.
pub const SIZE: usize = 32;

/// The bit in [`UtxoLeaf::status`] that says a slot holds something. A slot
/// without it is skipped, exactly as the kernel skips it.
pub const UTXO_OCCUPIED: u32 = 0x1;

/// One unspent output, in the layout the root hashes.
///
/// Preimage order: `utxo_id ‖ asset_id ‖ amount_lo ‖ amount_hi ‖ owner_root ‖
/// locktime ‖ threshold ‖ status ‖ index`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct UtxoLeaf {
    pub utxo_id: [u8; 32],
    pub asset_id: [u8; 32],
    pub amount_lo: u64,
    pub amount_hi: u64,
    pub owner_root: [u8; 32],
    pub locktime: u64,
    pub threshold: u32,
    pub status: u32,
}

/// One asset, in the layout the root hashes.
///
/// Preimage order: `asset_id ‖ total_supply_lo ‖ total_supply_hi ‖
/// mint_authority_root ‖ freeze_flag ‖ denomination ‖ index`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct AssetLeaf {
    pub asset_id: [u8; 32],
    pub total_supply_lo: u64,
    pub total_supply_hi: u64,
    pub mint_authority_root: [u8; 32],
    pub freeze_flag: u32,
    pub denomination: u32,
    pub occupied: u32,
}

/// One transaction, in the layout the root hashes.
///
/// Preimage order: `tx_id ‖ kind ‖ status ‖ reject_reason ‖ proof_digest ‖
/// index`. Every transaction is folded — this family has no skip rule.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct TxLeaf {
    pub tx_id: [u8; 32],
    pub kind: u32,
    pub status: u32,
    pub reject_reason: u32,
    pub proof_digest: [u8; 32],
}

fn le32(b: &mut Vec<u8>, v: u32) {
    b.extend_from_slice(&v.to_le_bytes());
}

fn le64(b: &mut Vec<u8>, v: u64) {
    b.extend_from_slice(&v.to_le_bytes());
}

/// The digest of one UTXO leaf at slot `i`.
pub fn utxo_leaf_digest(u: &UtxoLeaf, i: u32) -> Hash256 {
    let mut b = Vec::with_capacity(32 + 32 + 8 + 8 + 32 + 8 + 4 + 4 + 4);
    b.extend_from_slice(&u.utxo_id);
    b.extend_from_slice(&u.asset_id);
    le64(&mut b, u.amount_lo);
    le64(&mut b, u.amount_hi);
    b.extend_from_slice(&u.owner_root);
    le64(&mut b, u.locktime);
    le32(&mut b, u.threshold);
    le32(&mut b, u.status);
    le32(&mut b, i);
    keccak256(&[&b])
}

/// The digest of one asset leaf at slot `i`.
pub fn asset_leaf_digest(a: &AssetLeaf, i: u32) -> Hash256 {
    let mut b = Vec::with_capacity(32 + 8 + 8 + 32 + 4 + 4 + 4);
    b.extend_from_slice(&a.asset_id);
    le64(&mut b, a.total_supply_lo);
    le64(&mut b, a.total_supply_hi);
    b.extend_from_slice(&a.mint_authority_root);
    le32(&mut b, a.freeze_flag);
    le32(&mut b, a.denomination);
    le32(&mut b, i);
    keccak256(&[&b])
}

/// The digest of one transaction leaf at slot `i`.
pub fn tx_leaf_digest(t: &TxLeaf, i: u32) -> Hash256 {
    let mut b = Vec::with_capacity(32 + 4 + 4 + 4 + 32 + 4);
    b.extend_from_slice(&t.tx_id);
    le32(&mut b, t.kind);
    le32(&mut b, t.status);
    le32(&mut b, t.reject_reason);
    b.extend_from_slice(&t.proof_digest);
    le32(&mut b, i);
    keccak256(&[&b])
}

/// The UTXO family root: the occupied slots, in ascending slot index.
///
/// The slot index — not the position among the occupied ones — is what goes
/// into the preimage, so removing an output changes the leaves that follow it
/// only by omission, never by renumbering.
pub fn utxo_root(utxos: &[UtxoLeaf]) -> Hash256 {
    let leaves: Vec<Hash256> = utxos
        .iter()
        .enumerate()
        .filter(|(_, u)| u.status & UTXO_OCCUPIED != 0)
        .map(|(i, u)| utxo_leaf_digest(u, i as u32))
        .collect();
    merkle_root(&leaves)
}

/// The asset family root: the occupied slots, in ascending slot index.
pub fn asset_root(assets: &[AssetLeaf]) -> Hash256 {
    let leaves: Vec<Hash256> = assets
        .iter()
        .enumerate()
        .filter(|(_, a)| a.occupied != 0)
        .map(|(i, a)| asset_leaf_digest(a, i as u32))
        .collect();
    merkle_root(&leaves)
}

/// The transaction family root: every transaction, in order.
pub fn tx_root(txs: &[TxLeaf]) -> Hash256 {
    let leaves: Vec<Hash256> = txs
        .iter()
        .enumerate()
        .map(|(i, t)| tx_leaf_digest(t, i as u32))
        .collect();
    merkle_root(&leaves)
}

/// `keccak256(parent ‖ utxo ‖ asset ‖ tx ‖ height_u64_le)`.
///
/// Untagged and fixed-shape: this is a record of five known fields, not a
/// Merkle node.
pub fn compose(
    parent: &Hash256,
    utxo: &Hash256,
    asset: &Hash256,
    tx: &Hash256,
    height: u64,
) -> Hash256 {
    let h = height.to_le_bytes();
    keccak256(&[&parent[..], &utxo[..], &asset[..], &tx[..], &h[..]])
}

/// The whole root: the three families, then the compose.
pub fn execution_root(
    parent: &Hash256,
    utxos: &[UtxoLeaf],
    assets: &[AssetLeaf],
    txs: &[TxLeaf],
    height: u64,
) -> (Hash256, Hash256, Hash256, Hash256) {
    let u = utxo_root(utxos);
    let a = asset_root(assets);
    let t = tx_root(txs);
    let e = compose(parent, &u, &a, &t, height);
    (e, u, a, t)
}

/// The canonical owner commitment a UTXO leaf carries:
/// `keccak256(threshold_u32_le ‖ key_count_u32_le ‖ key[0] ‖ … )`.
///
/// The keys are in ascending byte order, so the commitment is to the owner SET
/// and not to the order somebody happened to list it in. The count is a length
/// prefix, so two different (threshold, key-set) pairs cannot concatenate to
/// the same bytes.
///
/// Locktime is deliberately NOT folded in here: the leaf binds it in its own
/// field, and binding it twice would say it was two things. Threshold IS bound
/// twice, here and in the leaf — matching the kernel's struct, which carries
/// both.
pub fn owner_root(threshold: u32, keys: &[Vec<u8>]) -> Hash256 {
    let mut sorted: Vec<&Vec<u8>> = keys.iter().collect();
    sorted.sort();
    let mut b = Vec::with_capacity(8 + sorted.iter().map(|k| k.len()).sum::<usize>());
    le32(&mut b, threshold);
    le32(&mut b, sorted.len() as u32);
    for k in sorted {
        b.extend_from_slice(k);
    }
    keccak256(&[&b])
}

/// An [`Id`] read as a root, and back. The root is what a block carries in its
/// header, where it is an id like any other.
pub fn id_to_root(id: &Id) -> Hash256 {
    id.0
}

pub fn root_to_id(root: &Hash256) -> Id {
    Id(*root)
}

/// The root of a chain that has committed to nothing.
pub fn empty() -> Hash256 {
    empty_root()
}

#[cfg(test)]
mod tests {
    use super::*;

    // The canonical fixture: eight UTXO slots with four occupied, four asset
    // slots with three occupied, four transactions with a mixed status
    // pattern, a parent of 0xEE.. and height 100. Byte for byte the input the
    // Go package and every GPU backend hash.
    const KAT_PARENT_BYTE0: u8 = 0xEE;
    const KAT_HEIGHT: u64 = 100;
    const STATUS_ACCEPTED: u32 = 1;
    const STATUS_REJECTED: u32 = 2;

    fn kat_utxos() -> Vec<UtxoLeaf> {
        let mut us = vec![UtxoLeaf::default(); 8];
        for (i, u) in us.iter_mut().enumerate().take(4) {
            for k in 0..32usize {
                u.utxo_id[k] = ((i as u32 * 11 + k as u32) ^ 0x71) as u8;
                u.asset_id[k] = (0xA0u32 + (k as u32 & 0xF)) as u8;
                u.owner_root[k] = (0xC0u32 + (k as u32 & 0xF)) as u8;
            }
            u.amount_lo = 1000 + i as u64;
            u.amount_hi = 0;
            u.locktime = 0;
            u.threshold = 1;
            u.status = UTXO_OCCUPIED;
        }
        us
    }

    fn kat_assets() -> Vec<AssetLeaf> {
        let mut as_ = vec![AssetLeaf::default(); 4];
        for (i, a) in as_.iter_mut().enumerate().take(3) {
            for k in 0..32usize {
                a.asset_id[k] = ((i as u32 * 7 + k as u32) ^ 0xC3) as u8;
                a.mint_authority_root[k] = (0xD0u32 + (k as u32 & 0xF)) as u8;
            }
            a.total_supply_lo = 1_000_000 + i as u64;
            a.total_supply_hi = 0;
            a.freeze_flag = 1;
            a.denomination = 8;
            a.occupied = 1;
        }
        as_
    }

    fn kat_txs() -> Vec<TxLeaf> {
        let mut ts = vec![TxLeaf::default(); 4];
        for (i, t) in ts.iter_mut().enumerate() {
            for k in 0..32usize {
                t.tx_id[k] = (i + k) as u8;
                t.proof_digest[k] = (i * 5 + k) as u8;
            }
            t.kind = i as u32;
            match i % 3 {
                0 => t.status = STATUS_ACCEPTED,
                1 => {
                    t.status = STATUS_REJECTED;
                    t.reject_reason = 42;
                }
                _ => {}
            }
        }
        ts
    }

    fn kat_parent() -> Hash256 {
        let mut p = [0u8; 32];
        for (k, slot) in p.iter_mut().enumerate() {
            *slot = KAT_PARENT_BYTE0.wrapping_add(k as u8);
        }
        p
    }

    #[test]
    fn the_execution_root_matches_the_value_go_and_every_gpu_backend_produce() {
        let (exec, utxo, asset, tx) = execution_root(
            &kat_parent(),
            &kat_utxos(),
            &kat_assets(),
            &kat_txs(),
            KAT_HEIGHT,
        );
        assert_eq!(
            hex::encode(exec),
            "4f144ef76dd14d4447ccf9c746d747c21dd4a6b0945e2180bfac489f21af2d77"
        );
        assert_eq!(&hex::encode(utxo)[..8], "16663d25");
        assert_eq!(&hex::encode(asset)[..8], "12f47742");
        assert_eq!(&hex::encode(tx)[..8], "c210388d");
    }

    #[test]
    fn the_compose_is_an_untagged_keccak_not_a_merkle_node() {
        // A regression to a tagged node hash would change these bytes.
        let u = utxo_root(&kat_utxos());
        let a = asset_root(&kat_assets());
        let t = tx_root(&kat_txs());
        assert_eq!(
            hex::encode(compose(&kat_parent(), &u, &a, &t, KAT_HEIGHT)),
            "4f144ef76dd14d4447ccf9c746d747c21dd4a6b0945e2180bfac489f21af2d77"
        );
    }

    #[test]
    fn an_empty_family_folds_to_the_keccak_of_nothing_not_to_zeros() {
        const EMPTY_KECCAK: &str =
            "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470";
        assert_eq!(hex::encode(utxo_root(&[])), EMPTY_KECCAK);
        assert_eq!(hex::encode(asset_root(&[])), EMPTY_KECCAK);
        assert_eq!(hex::encode(tx_root(&[])), EMPTY_KECCAK);
        // A slate of slots that are all unoccupied is the same as no slots.
        assert_eq!(
            hex::encode(utxo_root(&vec![UtxoLeaf::default(); 4])),
            EMPTY_KECCAK
        );
    }

    #[test]
    fn an_unoccupied_slot_is_skipped_but_still_holds_its_index() {
        // Occupying slots 0 and 2 must differ from occupying 0 and 1, because
        // the slot index is part of each preimage.
        let mut a = vec![UtxoLeaf::default(); 3];
        a[0].status = UTXO_OCCUPIED;
        a[2].status = UTXO_OCCUPIED;
        let mut b = vec![UtxoLeaf::default(); 3];
        b[0].status = UTXO_OCCUPIED;
        b[1].status = UTXO_OCCUPIED;
        assert_ne!(utxo_root(&a), utxo_root(&b));
    }

    #[test]
    fn the_owner_root_does_not_depend_on_the_order_keys_were_listed_in() {
        let a = vec![vec![3u8; 20], vec![1u8; 20], vec![2u8; 20]];
        let b = vec![vec![1u8; 20], vec![2u8; 20], vec![3u8; 20]];
        assert_eq!(owner_root(2, &a), owner_root(2, &b));
    }

    #[test]
    fn the_owner_root_binds_the_threshold() {
        let keys = vec![vec![1u8; 20], vec![2u8; 20]];
        assert_ne!(owner_root(1, &keys), owner_root(2, &keys));
    }

    #[test]
    fn the_owner_root_has_a_length_prefix_so_two_sets_cannot_collide() {
        // Without the count, {AB} and {A, B} would concatenate identically.
        let one = vec![vec![1u8, 2, 3, 4]];
        let two = vec![vec![1u8, 2], vec![3u8, 4]];
        assert_ne!(owner_root(1, &one), owner_root(1, &two));
    }

    #[test]
    fn the_owner_root_is_the_keccak_of_the_stated_preimage() {
        let keys = vec![vec![9u8; 20]];
        let mut want = Vec::new();
        want.extend_from_slice(&2u32.to_le_bytes());
        want.extend_from_slice(&1u32.to_le_bytes());
        want.extend_from_slice(&keys[0]);
        assert_eq!(owner_root(2, &keys), keccak256(&[&want]));
    }

    #[test]
    fn changing_any_leaf_field_changes_the_family_root() {
        let base = kat_utxos();
        for mutate in [
            (|u: &mut UtxoLeaf| u.amount_lo += 1) as fn(&mut UtxoLeaf),
            |u: &mut UtxoLeaf| u.amount_hi += 1,
            |u: &mut UtxoLeaf| u.locktime += 1,
            |u: &mut UtxoLeaf| u.threshold += 1,
            |u: &mut UtxoLeaf| u.owner_root[0] ^= 1,
            |u: &mut UtxoLeaf| u.asset_id[0] ^= 1,
            |u: &mut UtxoLeaf| u.utxo_id[0] ^= 1,
        ] {
            let mut changed = base.clone();
            mutate(&mut changed[0]);
            assert_ne!(utxo_root(&base), utxo_root(&changed));
        }
    }

    #[test]
    fn the_root_binds_the_height_and_the_parent() {
        let u = utxo_root(&kat_utxos());
        let a = asset_root(&kat_assets());
        let t = tx_root(&kat_txs());
        let p = kat_parent();
        assert_ne!(compose(&p, &u, &a, &t, 100), compose(&p, &u, &a, &t, 101));
        let mut other = p;
        other[0] ^= 1;
        assert_ne!(
            compose(&p, &u, &a, &t, 100),
            compose(&other, &u, &a, &t, 100)
        );
    }

    #[test]
    fn a_root_and_an_id_are_the_same_thirty_two_bytes() {
        let id = Id::prefixed_bytes(&[1, 2, 3]);
        assert_eq!(root_to_id(&id_to_root(&id)), id);
    }
}
