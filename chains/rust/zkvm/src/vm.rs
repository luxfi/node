// SPDX-License-Identifier: BSD-3-Clause-Eco

//! ZKVM instance and state management.

use crate::block::Block;
use crate::transaction::Transaction;
use std::collections::{HashMap, HashSet};

pub struct ZkVm {
    pub last_accepted: [u8; 32],
    pub last_accepted_height: u64,
    pub nullifiers: HashSet<Vec<u8>>,
    pub blocks: HashMap<[u8; 32], Block>,
    pub mempool: Vec<Transaction>,
}

impl ZkVm {
    pub fn new() -> Self {
        Self {
            last_accepted: [0; 32],
            last_accepted_height: 0,
            nullifiers: HashSet::new(),
            blocks: HashMap::new(),
            mempool: Vec::new(),
        }
    }

    pub fn alias(&self) -> &'static str {
        "Z"
    }

    pub fn issue_tx(&mut self, tx: Transaction) -> Result<[u8; 32], String> {
        tx.verify()?;
        // Check nullifier double spend
        for n in &tx.nullifiers {
            if self.nullifiers.contains(n) {
                return Err("nullifier already spent".into());
            }
        }
        let id = tx.id();
        self.mempool.push(tx);
        Ok(id)
    }

    pub fn build_block(&mut self, timestamp: i64) -> Result<Block, String> {
        if self.mempool.is_empty() {
            return Err("no transactions to build block".into());
        }
        let txs = std::mem::take(&mut self.mempool);
        let height = self.last_accepted_height + 1;
        let blk = Block::new(
            self.last_accepted,
            height,
            timestamp,
            vec![0u8; 32],
            txs,
        );
        blk.verify()?;
        self.blocks.insert(blk.id(), blk.clone());
        Ok(blk)
    }

    pub fn accept_block(&mut self, id: &[u8; 32]) -> Result<(), String> {
        let blk = self.blocks.get(id).ok_or_else(|| "block not found".to_string())?;
        for tx in &blk.txs {
            for n in &tx.nullifiers {
                self.nullifiers.insert(n.clone());
            }
        }
        self.last_accepted = *id;
        self.last_accepted_height = blk.height;
        Ok(())
    }

    pub fn reject_block(&mut self, id: &[u8; 32]) -> Result<(), String> {
        if let Some(blk) = self.blocks.remove(id) {
            for tx in blk.txs {
                self.mempool.push(tx);
            }
        }
        Ok(())
    }
}
