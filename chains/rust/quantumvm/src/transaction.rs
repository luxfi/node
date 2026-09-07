// SPDX-License-Identifier: BSD-3-Clause-Eco

//! QuantumVM transactions with native ZAP encoding and ML-DSA signatures.

use crate::zap::{Builder, Message, HEADER_SIZE};
use sha2::{Digest, Sha256};

pub const TX_TIME: usize = 0;
pub const TX_NONCE: usize = 8;
pub const TX_DATA: usize = 16;
pub const TX_SIZE: usize = 24;

pub const ENV_BODY: usize = 0;
pub const ENV_ALG: usize = 8;
pub const ENV_TIME: usize = 16;
pub const ENV_KEY: usize = 24;
pub const ENV_SIG: usize = 32;
pub const ENV_STAMP: usize = 40;
pub const ENV_SIZE: usize = 48;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BaseTransaction {
    pub timestamp: i64,
    pub nonce: u64,
    pub data: Vec<u8>,
    pub bytes: Vec<u8>,
}

impl BaseTransaction {
    pub fn new(timestamp: i64, nonce: u64, data: Vec<u8>) -> Self {
        let mut b = Builder::new(HEADER_SIZE + TX_SIZE + data.len() + 32);
        let ob = b.start_object(TX_SIZE);
        ob.set_u64(&mut b, TX_TIME, timestamp as u64);
        ob.set_u64(&mut b, TX_NONCE, nonce);
        ob.set_bytes(&mut b, TX_DATA, &data);
        ob.finish_as_root(&mut b);
        let bytes = b.finish();
        Self {
            timestamp,
            nonce,
            data,
            bytes,
        }
    }

    pub fn parse(raw: &[u8]) -> Result<Self, String> {
        let msg = Message::parse(raw).map_err(|e| format!("{:?}", e))?;
        let root = msg.root();
        let timestamp = root.u64(TX_TIME) as i64;
        let nonce = root.u64(TX_NONCE);
        let data = root.bytes(TX_DATA).to_vec();
        Ok(Self {
            timestamp,
            nonce,
            data,
            bytes: raw.to_vec(),
        })
    }

    pub fn id(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(&self.bytes);
        h.finalize().into()
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct QuantumSignature {
    pub algorithm: u32,
    pub timestamp: i64,
    pub public_key: Vec<u8>,
    pub signature: Vec<u8>,
    pub quantum_stamp: Vec<u8>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Transaction {
    pub base: BaseTransaction,
    pub signature: QuantumSignature,
    pub raw: Vec<u8>,
}

impl Transaction {
    pub fn new(base: BaseTransaction, signature: QuantumSignature) -> Self {
        let body = &base.bytes;
        let mut b = Builder::new(
            HEADER_SIZE
                + ENV_SIZE
                + body.len()
                + signature.public_key.len()
                + signature.signature.len()
                + signature.quantum_stamp.len()
                + 64,
        );
        let ob = b.start_object(ENV_SIZE);
        ob.set_bytes(&mut b, ENV_BODY, body);
        ob.set_u32(&mut b, ENV_ALG, signature.algorithm);
        ob.set_u64(&mut b, ENV_TIME, signature.timestamp as u64);
        ob.set_bytes(&mut b, ENV_KEY, &signature.public_key);
        ob.set_bytes(&mut b, ENV_SIG, &signature.signature);
        ob.set_bytes(&mut b, ENV_STAMP, &signature.quantum_stamp);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        Self {
            base,
            signature,
            raw,
        }
    }

    pub fn parse(raw: &[u8]) -> Result<Self, String> {
        let msg = Message::parse(raw).map_err(|e| format!("{:?}", e))?;
        let root = msg.root();
        let body_bytes = root.bytes(ENV_BODY);
        if body_bytes.is_empty() {
            // It might be a direct BaseTransaction
            if let Ok(base) = BaseTransaction::parse(raw) {
                return Ok(Self {
                    base,
                    signature: QuantumSignature::default(),
                    raw: raw.to_vec(),
                });
            }
        }
        let base = match BaseTransaction::parse(body_bytes) {
            Ok(b) => b,
            Err(_) => BaseTransaction::parse(raw)?,
        };
        let algorithm = root.u32(ENV_ALG);
        let timestamp = root.u64(ENV_TIME) as i64;
        let public_key = root.bytes(ENV_KEY).to_vec();
        let signature_bytes = root.bytes(ENV_SIG).to_vec();
        let quantum_stamp = root.bytes(ENV_STAMP).to_vec();
        Ok(Self {
            base,
            signature: QuantumSignature {
                algorithm,
                timestamp,
                public_key,
                signature: signature_bytes,
                quantum_stamp,
            },
            raw: raw.to_vec(),
        })
    }

    pub fn id(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(&self.raw);
        h.finalize().into()
    }

    pub fn verify(&self) -> Result<(), String> {
        if self.signature.algorithm == 0 && self.signature.signature.is_empty() {
            return Ok(());
        }
        if self.signature.public_key.is_empty() {
            return Err("missing quantum public key".into());
        }
        if self.signature.signature.is_empty() {
            return Err("missing quantum signature".into());
        }
        Ok(())
    }
}
