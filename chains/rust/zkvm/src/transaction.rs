// SPDX-License-Identifier: BSD-3-Clause-Eco

//! ZKVM Shielded Transactions over native ZAP.

use crate::zap::{Builder, Message, HEADER_SIZE};
use sha2::{Digest, Sha256};

pub const TX_TYPE: usize = 0;
pub const TX_VERSION: usize = 1;
pub const TX_FEE: usize = 2;
pub const TX_EXPIRY: usize = 10;
pub const TX_TIN_LENS: usize = 18;
pub const TX_TIN_BLOB: usize = 26;
pub const TX_TOUT_LENS: usize = 34;
pub const TX_TOUT_BLOB: usize = 42;
pub const TX_NULL_LENS: usize = 50;
pub const TX_NULL_BLOB: usize = 58;
pub const TX_SOUT_LENS: usize = 66;
pub const TX_SOUT_BLOB: usize = 74;
pub const TX_PROOF: usize = 82;
pub const TX_MEMO: usize = 90;
pub const TX_SIZE: usize = 98;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Transaction {
    pub tx_type: u8,
    pub version: u8,
    pub fee: u64,
    pub expiry: u64,
    pub nullifiers: Vec<Vec<u8>>,
    pub proof: Vec<u8>,
    pub memo: Vec<u8>,
    pub raw: Vec<u8>,
}

impl Transaction {
    pub fn new(
        tx_type: u8,
        version: u8,
        fee: u64,
        expiry: u64,
        nullifiers: Vec<Vec<u8>>,
        proof: Vec<u8>,
        memo: Vec<u8>,
    ) -> Self {
        let mut null_blob = Vec::new();
        let mut null_lens = Vec::with_capacity(nullifiers.len());
        for n in &nullifiers {
            null_lens.push(n.len() as u32);
            null_blob.extend_from_slice(n);
        }

        let mut b = Builder::new(
            HEADER_SIZE + TX_SIZE + null_blob.len() + 4 * null_lens.len() + proof.len() + memo.len() + 256,
        );
        let mut lb = b.start_list();
        for l in &null_lens {
            lb.add_u32(&mut b, *l);
        }
        let (null_off, null_len) = lb.finish();

        let ob = b.start_object(TX_SIZE);
        ob.set_u8(&mut b, TX_TYPE, tx_type);
        ob.set_u8(&mut b, TX_VERSION, version);
        ob.set_u64(&mut b, TX_FEE, fee);
        ob.set_u64(&mut b, TX_EXPIRY, expiry);
        ob.set_list(&mut b, TX_NULL_LENS, null_off, null_len);
        ob.set_bytes(&mut b, TX_NULL_BLOB, &null_blob);
        ob.set_bytes(&mut b, TX_PROOF, &proof);
        ob.set_bytes(&mut b, TX_MEMO, &memo);
        ob.finish_as_root(&mut b);
        let raw = b.finish();

        Self {
            tx_type,
            version,
            fee,
            expiry,
            nullifiers,
            proof,
            memo,
            raw,
        }
    }

    pub fn parse(raw: &[u8]) -> Result<Self, String> {
        let msg = Message::parse(raw).map_err(|e| format!("{:?}", e))?;
        let root = msg.root();
        let tx_type = root.u8(TX_TYPE);
        let version = root.u8(TX_VERSION);
        let fee = root.u64(TX_FEE);
        let expiry = root.u64(TX_EXPIRY);
        let memo = root.bytes(TX_MEMO).to_vec();
        let proof = root.bytes(TX_PROOF).to_vec();

        let null_lens = root.list(TX_NULL_LENS);
        let null_blob = root.bytes(TX_NULL_BLOB);

        let mut nullifiers = Vec::with_capacity(null_lens.len());
        let mut off = 0;
        for i in 0..null_lens.len() {
            let len = null_lens.u32(i) as usize;
            let end = off + len;
            if end > null_blob.len() {
                return Err("nullifier length exceeds blob".into());
            }
            nullifiers.push(null_blob[off..end].to_vec());
            off = end;
        }

        Ok(Self {
            tx_type,
            version,
            fee,
            expiry,
            nullifiers,
            proof,
            memo,
            raw: raw.to_vec(),
        })
    }

    pub fn id(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(&self.raw);
        h.finalize().into()
    }

    pub fn verify(&self) -> Result<(), String> {
        if self.tx_type == 0 {
            return Err("invalid transaction type".into());
        }
        Ok(())
    }
}
