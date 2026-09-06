// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A block: a parent, a height, a time, a state root, and the transactions
//! that get it there.
//!
//! There is no codec and no block kinds. A block is a ZAP buffer whose bytes
//! are authoritative — its id is the SHA-256 of exactly those bytes — and the
//! transactions inside are self-describing, so the block stores their lengths
//! and their bytes end to end and hands each slice back to the transaction
//! parser.
//!
//! The root the block carries is the execution root over the state the block
//! LEAVES BEHIND. The builder stamps it and the verifier recomputes it through
//! the same function, so a producer and a verifier cannot hold different
//! opinions about what a block did.

pub mod builder;
pub mod manager;
pub mod root;

use crate::error::{Error, Result};
use crate::hash::sha256;
use crate::ids::{self, Id};
use crate::txs::Tx;
use crate::wire::containers::read_blob_list;
use crate::xchain_zap::{self as wire, BlockInput};
use lux_zap::zap;

/// One block.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Block {
    parent_id: Id,
    height: u64,
    /// Seconds since the epoch.
    time: u64,
    root: Id,
    txs: Vec<Tx>,
    block_id: Id,
    bytes: Vec<u8>,
}

impl Block {
    /// Build a block and seal it: serialize, then take the id from the bytes.
    ///
    /// A transaction that reached here without its wire bytes is a caller bug,
    /// and it is refused rather than papered over — a block whose contents were
    /// re-encoded on the way in is a block whose id nobody else computes.
    pub fn new(
        parent_id: Id,
        height: u64,
        timestamp: u64,
        root: Id,
        txs: Vec<Tx>,
    ) -> Result<Block> {
        let bytes = serialize(parent_id, height, timestamp, root, &txs)?;
        Ok(Block {
            parent_id,
            height,
            time: timestamp,
            root,
            txs,
            block_id: sha256(&bytes),
            bytes,
        })
    }

    pub fn id(&self) -> Id {
        self.block_id
    }

    pub fn parent(&self) -> Id {
        self.parent_id
    }

    pub fn height(&self) -> u64 {
        self.height
    }

    /// Seconds since the epoch.
    pub fn timestamp(&self) -> u64 {
        self.time
    }

    /// The execution root over the state this block leaves behind.
    pub fn merkle_root(&self) -> Id {
        self.root
    }

    pub fn txs(&self) -> &[Tx] {
        &self.txs
    }

    /// The canonical encoding — what a peer is sent, and what the id is over.
    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    /// Read a block off the wire, byte-preserving.
    pub fn parse(bytes: &[u8]) -> Result<Block> {
        let v = wire::Block::new(zap::Message::parse(bytes)?.root());
        let tx_bufs = read_blob_list(v.tx_lengths(), v.tx_blob())?;
        let mut txs = Vec::with_capacity(tx_bufs.len());
        for buf in tx_bufs {
            txs.push(Tx::parse(buf)?);
        }
        Ok(Block {
            parent_id: ids::prefixed(v.parent()),
            height: v.height(),
            time: v.time(),
            root: ids::prefixed(v.root()),
            txs,
            block_id: sha256(bytes),
            bytes: bytes.to_vec(),
        })
    }
}

fn serialize(parent_id: Id, height: u64, time: u64, root: Id, txs: &[Tx]) -> Result<Vec<u8>> {
    let mut raw: Vec<Vec<u8>> = Vec::with_capacity(txs.len());
    for (i, tx) in txs.iter().enumerate() {
        if tx.bytes().is_empty() {
            return Err(Error::UninitializedTx(i));
        }
        raw.push(tx.bytes().to_vec());
    }
    let lengths: Vec<u32> = raw.iter().map(|r| r.len() as u32).collect();
    let blob: Vec<u8> = raw.concat();
    Ok(wire::new_block(&BlockInput {
        parent: &parent_id,
        height,
        time,
        root: &root,
        tx_lengths: &lengths,
        tx_blob: &blob,
    }))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::{TransferInput, TransferOutput};
    use crate::fx::{self, Input, Owners, State};
    use crate::ids::ShortId;
    use crate::txs::{BaseTx, Unsigned};
    use crate::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, UtxoId};

    fn asset(n: u8) -> Id {
        ids::prefixed(&[n])
    }

    fn a_tx(amt: u64) -> Tx {
        let mut tx = Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: 10,
                blockchain_id: asset(5),
                outs: vec![TransferableOutput {
                    asset: Asset { id: asset(1) },
                    out: State::Transfer(TransferOutput {
                        amt,
                        owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(asset(2), 0),
                    asset: Asset { id: asset(1) },
                    input: fx::FxIn::Transfer(TransferInput {
                        amt: amt + 1,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: vec![],
            },
        }));
        tx.sign(fx::Family::Secp256k1, &[vec![[7u8; 32]]]).unwrap();
        tx
    }

    #[test]
    fn a_block_round_trips_and_keeps_its_identity() {
        let txs = vec![a_tx(1), a_tx(2)];
        let blk = Block::new(asset(9), 42, 123_456, asset(0xAA), txs.clone()).unwrap();
        let parsed = Block::parse(blk.bytes()).unwrap();
        assert_eq!(parsed, blk);
        assert_eq!(parsed.id(), blk.id());
        assert_eq!(parsed.parent(), asset(9));
        assert_eq!(parsed.height(), 42);
        assert_eq!(parsed.timestamp(), 123_456);
        assert_eq!(parsed.merkle_root(), asset(0xAA));
        assert_eq!(parsed.txs().len(), 2);
        assert_eq!(parsed.txs()[0].id(), txs[0].id());
        assert_eq!(parsed.txs()[1].id(), txs[1].id());
    }

    #[test]
    fn the_id_is_the_hash_of_exactly_the_bytes() {
        let blk = Block::new(asset(1), 1, 1, asset(2), vec![a_tx(1)]).unwrap();
        assert_eq!(blk.id(), sha256(blk.bytes()));
    }

    #[test]
    fn a_block_with_no_transactions_still_encodes_and_parses() {
        // Emptiness is refused by the verifier, not by the encoding — the
        // encoding has to be able to represent what it refuses.
        let blk = Block::new(asset(1), 0, 0, asset(0), vec![]).unwrap();
        let parsed = Block::parse(blk.bytes()).unwrap();
        assert!(parsed.txs().is_empty());
        assert_eq!(parsed.id(), blk.id());
    }

    #[test]
    fn changing_any_header_field_changes_the_id() {
        let base = Block::new(asset(1), 5, 100, asset(2), vec![a_tx(1)]).unwrap();
        for other in [
            Block::new(asset(9), 5, 100, asset(2), vec![a_tx(1)]).unwrap(),
            Block::new(asset(1), 6, 100, asset(2), vec![a_tx(1)]).unwrap(),
            Block::new(asset(1), 5, 101, asset(2), vec![a_tx(1)]).unwrap(),
            Block::new(asset(1), 5, 100, asset(3), vec![a_tx(1)]).unwrap(),
            Block::new(asset(1), 5, 100, asset(2), vec![a_tx(2)]).unwrap(),
        ] {
            assert_ne!(base.id(), other.id());
        }
    }

    #[test]
    fn a_transaction_always_carries_its_wire_bytes_so_a_block_can_hold_it() {
        // The refusal in `serialize` guards a caller bug; a transaction built
        // or parsed the ordinary way can never trip it, and that is the
        // invariant worth pinning.
        assert!(!a_tx(1).bytes().is_empty());
        assert!(!Tx::new(a_tx(1).unsigned.clone()).bytes().is_empty());
    }

    #[test]
    fn a_truncated_block_is_refused_rather_than_half_read() {
        let blk = Block::new(asset(1), 1, 1, asset(2), vec![a_tx(1)]).unwrap();
        let raw = blk.bytes();
        assert!(Block::parse(&raw[..raw.len() / 2]).is_err());
    }
}
