// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! An F-Chain block: an ordered batch of fee-settled confidential-compute
//! operations.
//!
//! A BLOCK IS NAMED BY ITS CHAIN AS WELL AS BY ITS CONTENT. The chain id does
//! not travel — a peer supplies its own — so the same bytes name different
//! blocks on different chains, and chain B cannot resolve chain A's parent.
//! Without it every F-Chain with the same genesis timestamp shared one genesis
//! id, and a block built on one was accepted verbatim by the others.

use sha2::{Digest, Sha256};

use crate::id::Id;
use crate::tx::Transaction;
use crate::wire;

/// How far ahead of the verifying node's clock a block's timestamp may be.
///
/// Chain time drives every expiry F enforces, so without a cap a proposer
/// stamping year 36812 would expire every permit and every pending request at
/// once.
pub const MAX_FUTURE_SKEW: i64 = 60;

/// One block.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Block {
    id: Id,
    parent: Id,
    height: u64,
    /// Unix seconds. F's block-time resolution, and the only clock any record
    /// it writes is stamped from.
    timestamp: i64,
    transactions: Vec<Transaction>,
}

impl Block {
    /// A block, named at construction. The id is a function of the chain and
    /// the content, so there is no moment at which a block exists without one.
    pub fn new(
        chain: &Id,
        parent: Id,
        height: u64,
        timestamp: i64,
        transactions: Vec<Transaction>,
    ) -> Block {
        let id = compute_id(chain, &parent, height, timestamp, &transactions);
        Block { id, parent, height, timestamp, transactions }
    }

    pub fn id(&self) -> Id {
        self.id
    }
    pub fn parent(&self) -> Id {
        self.parent
    }
    pub fn height(&self) -> u64 {
        self.height
    }
    pub fn timestamp(&self) -> i64 {
        self.timestamp
    }
    pub fn transactions(&self) -> &[Transaction] {
        &self.transactions
    }

    /// The block's wire encoding. The chain id is NOT in it — each side
    /// supplies its own when it names the block.
    pub fn bytes(&self) -> Vec<u8> {
        wire::block_bytes(&self.parent, self.height, self.timestamp, &self.transactions)
    }
}

/// The name a chain gives a block: its own id, then the parent, the height, the
/// time, and every transaction id in order.
pub fn compute_id(
    chain: &Id,
    parent: &Id,
    height: u64,
    timestamp: i64,
    txs: &[Transaction],
) -> Id {
    let mut h = Sha256::new();
    h.update(chain);
    h.update(parent);
    h.update(height.to_be_bytes());
    h.update((timestamp as u64).to_be_bytes());
    for tx in txs {
        h.update(tx.id());
    }
    h.finalize().into()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::id::{hex, EMPTY};

    #[test]
    fn the_genesis_block_is_named_by_its_chain_and_its_timestamp() {
        // The id the corpus's F_GENESIS vector asks for: chain 50, no parent,
        // height 0, timestamp 1000, no transactions.
        let chain: Id = [50u8; 32];
        let b = Block::new(&chain, EMPTY, 0, 1000, Vec::new());
        assert_eq!(
            hex(&b.id()),
            "67de330c19ffcc6b46eaf213575b4bbc28536518580a491af3eaf3227625723c"
        );
    }

    #[test]
    fn two_chains_name_the_same_content_differently() {
        let a = Block::new(&[50u8; 32], EMPTY, 0, 1000, Vec::new());
        let b = Block::new(&[51u8; 32], EMPTY, 0, 1000, Vec::new());
        assert_ne!(a.id(), b.id(), "a block built on one F-chain is not a block on another");
    }

    #[test]
    fn every_field_of_a_block_is_in_its_name() {
        let chain: Id = [50u8; 32];
        let base = Block::new(&chain, EMPTY, 1, 1000, Vec::new()).id();
        assert_ne!(Block::new(&chain, [1u8; 32], 1, 1000, Vec::new()).id(), base);
        assert_ne!(Block::new(&chain, EMPTY, 2, 1000, Vec::new()).id(), base);
        assert_ne!(Block::new(&chain, EMPTY, 1, 1001, Vec::new()).id(), base);
        let tx = Transaction { nonce: 1, ..Transaction::default() };
        assert_ne!(Block::new(&chain, EMPTY, 1, 1000, vec![tx]).id(), base);
    }

    #[test]
    fn a_blocks_bytes_read_back_as_the_block() {
        let chain: Id = [50u8; 32];
        let txs = vec![Transaction {
            tx_type: 1,
            scheme: b"ckks-n14".to_vec(),
            nonce: 1,
            gas_limit: 5_000_000,
            ..Transaction::default()
        }];
        let b = Block::new(&chain, [7u8; 32], 3, 1000, txs.clone());
        let f = wire::parse_block(&b.bytes()).expect("parses");
        assert_eq!(f.parent, [7u8; 32]);
        assert_eq!(f.height, 3);
        assert_eq!(f.timestamp, 1000);
        assert_eq!(f.transactions, txs);
        // And a chain that reads it back names it the same way.
        assert_eq!(
            Block::new(&chain, f.parent, f.height, f.timestamp, f.transactions).id(),
            b.id()
        );
    }
}
