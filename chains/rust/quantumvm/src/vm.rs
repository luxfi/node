// SPDX-License-Identifier: BSD-3-Clause-Eco

//! QuantumVM instance and node integration seam.

use crate::block::Block;
use crate::transaction::Transaction;
use std::collections::HashMap;

pub struct QuantumVm {
    pub chain_id: [u8; 32],
    pub network_id: u32,
    pub last_accepted: [u8; 32],
    pub last_accepted_height: u64,
    pub blocks: HashMap<[u8; 32], Block>,
    pub mempool: Vec<Transaction>,
}

impl QuantumVm {
    pub fn new(chain_id: [u8; 32], network_id: u32) -> Self {
        Self {
            chain_id,
            network_id,
            last_accepted: [0; 32],
            last_accepted_height: 0,
            blocks: HashMap::new(),
            mempool: Vec::new(),
        }
    }

    pub fn alias(&self) -> &'static str {
        "Q"
    }

    pub fn issue_tx(&mut self, tx: Transaction) -> Result<[u8; 32], String> {
        tx.verify()?;
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
            timestamp,
            height,
            self.last_accepted,
            self.chain_id,
            self.network_id,
            txs,
        );
        blk.verify()?;
        self.blocks.insert(blk.id(), blk.clone());
        Ok(blk)
    }

    pub fn accept_block(&mut self, id: &[u8; 32]) -> Result<(), String> {
        let blk = self.blocks.get(id).ok_or_else(|| "block not found".to_string())?;
        self.last_accepted = *id;
        self.last_accepted_height = blk.height;
        Ok(())
    }

    pub fn reject_block(&mut self, id: &[u8; 32]) -> Result<(), String> {
        if let Some(blk) = self.blocks.remove(id) {
            // Return transactions to mempool
            for tx in blk.txs {
                self.mempool.push(tx);
            }
        }
        Ok(())
    }
}
