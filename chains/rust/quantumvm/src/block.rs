// SPDX-License-Identifier: BSD-3-Clause-Eco

//! QuantumVM Block: native ZAP wire encoding and execution.

use crate::transaction::Transaction;
use crate::zap::{Builder, Message, HEADER_SIZE};
use sha2::{Digest, Sha256};

pub const BLK_TIME: usize = 0;
pub const BLK_HEIGHT: usize = 8;
pub const BLK_PARENT: usize = 16;
pub const BLK_CHAIN: usize = 48;
pub const BLK_NETWORK: usize = 80;
pub const BLK_TX_LENS: usize = 88;
pub const BLK_TX_BLOB: usize = 96;
pub const BLK_SIZE: usize = 104;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Block {
    pub timestamp: i64,
    pub height: u64,
    pub parent_id: [u8; 32],
    pub chain_id: [u8; 32],
    pub network_id: u32,
    pub txs: Vec<Transaction>,
    pub raw: Vec<u8>,
}

impl Block {
    pub fn new(
        timestamp: i64,
        height: u64,
        parent_id: [u8; 32],
        chain_id: [u8; 32],
        network_id: u32,
        txs: Vec<Transaction>,
    ) -> Self {
        let mut tx_blob = Vec::new();
        for tx in &txs {
            tx_blob.extend_from_slice(&tx.raw);
        }

        let mut b = Builder::new(
            HEADER_SIZE + BLK_SIZE + tx_blob.len() + 4 * txs.len() + 128,
        );
        let mut lb = b.start_list();
        for tx in &txs {
            lb.add_u32(&mut b, tx.raw.len() as u32);
        }
        let (tx_lens_off, tx_lens_len) = lb.finish();

        let ob = b.start_object(BLK_SIZE);
        ob.set_u64(&mut b, BLK_TIME, timestamp as u64);
        ob.set_u64(&mut b, BLK_HEIGHT, height);
        ob.set_bytes_fixed(&mut b, BLK_PARENT, &parent_id);
        ob.set_bytes_fixed(&mut b, BLK_CHAIN, &chain_id);
        ob.set_u32(&mut b, BLK_NETWORK, network_id);
        ob.set_list(&mut b, BLK_TX_LENS, tx_lens_off, tx_lens_len);
        ob.set_bytes(&mut b, BLK_TX_BLOB, &tx_blob);
        ob.finish_as_root(&mut b);
        let raw = b.finish();

        Self {
            timestamp,
            height,
            parent_id,
            chain_id,
            network_id,
            txs,
            raw,
        }
    }

    pub fn parse(raw: &[u8]) -> Result<Self, String> {
        let msg = Message::parse(raw).map_err(|e| format!("{:?}", e))?;
        let root = msg.root();
        let timestamp = root.u64(BLK_TIME) as i64;
        let height = root.u64(BLK_HEIGHT);
        
        let parent_slice = root.bytes_fixed(BLK_PARENT, 32);
        let mut parent_id = [0u8; 32];
        parent_id.copy_from_slice(parent_slice);

        let chain_slice = root.bytes_fixed(BLK_CHAIN, 32);
        let mut chain_id = [0u8; 32];
        chain_id.copy_from_slice(chain_slice);

        let network_id = root.u32(BLK_NETWORK);

        let list = root.list(BLK_TX_LENS);
        let tx_blob = root.bytes(BLK_TX_BLOB);

        let mut txs = Vec::with_capacity(list.len());
        let mut off = 0;
        for i in 0..list.len() {
            let len = list.u32(i) as usize;
            let end = off + len;
            if end > tx_blob.len() {
                return Err("tx length exceeds blob boundary".into());
            }
            let tx_slice = &tx_blob[off..end];
            let tx = Transaction::parse(tx_slice)?;
            txs.push(tx);
            off = end;
        }

        if off != tx_blob.len() {
            return Err("trailing bytes in tx blob".into());
        }

        Ok(Self {
            timestamp,
            height,
            parent_id,
            chain_id,
            network_id,
            txs,
            raw: raw.to_vec(),
        })
    }

    pub fn id(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(&self.raw);
        h.finalize().into()
    }

    pub fn verify(&self) -> Result<(), String> {
        for tx in &self.txs {
            tx.verify()?;
        }
        Ok(())
    }
}
