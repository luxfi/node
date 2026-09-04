// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The bridge from a block to the root it must carry.
//!
//! [`state::root`](crate::state::root) knows how to hash leaves; it does not
//! know what a UTXO or a transaction is. This is where the two meet: the
//! post-block state is projected onto the leaf layout, the block's transactions
//! onto theirs, and the result is folded.
//!
//! There is exactly ONE function, and both the builder and the verifier call
//! it. That is the whole point: a producer that stamped a root by one rule and
//! a verifier that recomputed it by another would be two chains.
//!
//! THE ASSET FAMILY IS EMPTY, deliberately. The chain's state is unspent
//! outputs and nothing else: an asset exists only as the id stamped on the
//! outputs its creating transaction produced, and its name, symbol and
//! denomination live in that transaction's history. There is no asset record to
//! enumerate, so projecting one would mean inventing state the executor does
//! not keep — summing balances for a supply, guessing a mint authority. The
//! asset binding is not lost: every UTXO leaf carries its asset id, so it is
//! committed through the UTXO family.

use crate::error::{Error, Result};
use crate::ids::Id;
use crate::ids::EMPTY;
use crate::state::root::{self, AssetLeaf, TxLeaf, UtxoLeaf, UTXO_OCCUPIED};
use crate::state::ReadOnlyChain;
use crate::txs::Tx;
use crate::utxo::Utxo;

/// A transaction in an accepted block has this status in its leaf.
const STATUS_ACCEPTED: u32 = 1;

/// The root a block at `height` must carry, over the state it leaves behind.
///
/// `parent_root` is the parent block's root — the empty root at genesis.
/// `post_state` is the state AFTER every transaction in the block has been
/// applied.
pub fn block_execution_root(
    parent_root: Id,
    blk_txs: &[Tx],
    post_state: &dyn ReadOnlyChain,
    height: u64,
) -> Result<Id> {
    let leaf_txs = tx_leaves(blk_txs);
    let utxos = post_block_utxo_leaves(post_state)?;
    let assets: Vec<AssetLeaf> = Vec::new();
    let (e, _, _, _) = root::execution_root(&parent_root, &utxos, &assets, &leaf_txs, height);
    Ok(e)
}

/// The occupied UTXO set, in ascending id order, as leaves.
///
/// The enumeration order IS the leaf order, and it is the same order every
/// layer answers with, so the leaf index in the preimage is a position in a
/// total order rather than a property of any one node's storage.
fn post_block_utxo_leaves(post_state: &dyn ReadOnlyChain) -> Result<Vec<UtxoLeaf>> {
    let utxos = post_state.utxos(&EMPTY, 0)?;
    utxos.iter().map(utxo_leaf).collect()
}

/// One unspent output as the leaf that commits to it.
///
/// The amount is the output's value, or zero for an output that holds no value
/// — a mint authority, an NFT. The high limb is always zero: an amount here is
/// one u64, and pretending otherwise would be committing to a field that
/// cannot vary.
fn utxo_leaf(utxo: &Utxo) -> Result<UtxoLeaf> {
    let owners = utxo.out.owners();
    if owners.threshold == 0 && owners.addrs.is_empty() {
        // An output nobody can spend has no owner commitment to make. That is
        // not a leaf this projection knows how to write, and a root computed
        // under an invented rule is worse than a refusal.
        return Err(Error::UnsupportedOwnerModel);
    }
    Ok(UtxoLeaf {
        utxo_id: utxo.input_id(),
        asset_id: utxo.asset_id(),
        amount_lo: utxo.out.amount(),
        amount_hi: 0,
        owner_root: root::owner_root(owners.threshold, &owners.addresses()),
        locktime: owners.locktime,
        threshold: owners.threshold,
        status: UTXO_OCCUPIED,
    })
}

/// The root of what a block CARRIES, as distinct from the state it leaves.
///
/// It is the transaction family's own fold — the same leaves, the same order,
/// the same tagged tree that go into the execution root. A certificate that
/// named a state but not the payload that produced it would be a certificate
/// two different blocks could satisfy, so the two roots are stated separately
/// and both are derived from the block alone.
pub fn payload_root(blk_txs: &[Tx]) -> Id {
    root::tx_root(&tx_leaves(blk_txs))
}

/// The block's transactions as leaves, in block order.
///
/// Every one of them is accepted — they are in a block that is being built or
/// verified — and carries no rejection and no proof digest at this layer.
fn tx_leaves(blk_txs: &[Tx]) -> Vec<TxLeaf> {
    blk_txs
        .iter()
        .map(|tx| TxLeaf {
            tx_id: tx.id(),
            status: STATUS_ACCEPTED,
            ..TxLeaf::default()
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ids;
    use crate::fx::secp256k1::TransferOutput;
    use crate::fx::{Owners, State};
    use crate::ids::ShortId;
    use crate::state::{Chain, Store};
    use crate::utxo::{Asset, UtxoId};

    fn a_utxo(n: u8) -> Utxo {
        Utxo {
            utxo_id: UtxoId::new(ids::prefixed(&[n]), 0),
            asset: Asset {
                id: ids::prefixed(&[1]),
            },
            out: State::Transfer(TransferOutput {
                amt: 100 + n as u64,
                owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[n])]),
            }),
        }
    }

    #[test]
    fn an_empty_chain_folds_to_the_compose_of_three_empty_families() {
        let s = Store::new();
        let got = block_execution_root(ids::prefixed(&[0xEE]), &[], &s, 7).unwrap();
        let e = root::empty();
        let want = root::compose(&ids::prefixed(&[0xEE]), &e, &e, &e, 7);
        assert_eq!(got, want);
    }

    #[test]
    fn the_root_moves_when_the_state_does() {
        let mut s = Store::new();
        let before = block_execution_root(EMPTY, &[], &s, 1).unwrap();
        s.add_utxo(a_utxo(1));
        let after = block_execution_root(EMPTY, &[], &s, 1).unwrap();
        assert_ne!(before, after);
    }

    #[test]
    fn the_root_moves_with_the_height_and_the_parent() {
        let s = Store::new();
        let a = block_execution_root(EMPTY, &[], &s, 1).unwrap();
        let b = block_execution_root(EMPTY, &[], &s, 2).unwrap();
        let c = block_execution_root(ids::prefixed(&[1]), &[], &s, 1).unwrap();
        assert_ne!(a, b);
        assert_ne!(a, c);
    }

    #[test]
    fn insertion_order_does_not_change_the_root() {
        // The leaves are the enumeration, and the enumeration is sorted, so the
        // order the outputs happened to arrive in cannot reach the root.
        let mut a = Store::new();
        let mut b = Store::new();
        for n in [3u8, 1, 2] {
            a.add_utxo(a_utxo(n));
        }
        for n in [1u8, 2, 3] {
            b.add_utxo(a_utxo(n));
        }
        assert_eq!(
            block_execution_root(EMPTY, &[], &a, 9).unwrap(),
            block_execution_root(EMPTY, &[], &b, 9).unwrap()
        );
    }

    #[test]
    fn an_unspendable_output_is_refused_rather_than_hashed_under_a_guess() {
        let mut s = Store::new();
        let mut u = a_utxo(1);
        u.out = State::Transfer(TransferOutput {
            amt: 1,
            owners: Owners::new(0, vec![]),
        });
        s.add_utxo(u);
        assert_eq!(
            block_execution_root(EMPTY, &[], &s, 1),
            Err(Error::UnsupportedOwnerModel)
        );
    }
}
