// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The NFT feature extension.
//!
//! An NFT is a group id and a payload owned by an address set. It has no
//! amount, so it never appears as a transferable output — it lives in an
//! asset's initial state and moves by operation. Its credentials are secp256k1
//! signatures; the family byte names the fx, not the curve.

use crate::error::{Error, Result};
use crate::wire::{self, shapes, TypeKind};

use super::secp256k1::{Credential as SecpCredential, Input, Owners};

/// The largest payload an NFT may carry.
pub const MAX_PAYLOAD_SIZE: usize = 1024;

/// The family byte every nftfx primitive travels under.
pub const TYPE_KIND: TypeKind = TypeKind::Nft;

/// Who may mint into a group.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MintOutput {
    pub group_id: u32,
    pub owners: Owners,
}

impl MintOutput {
    pub fn verify(&self) -> Result<()> {
        self.owners.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_nft_mint_output(
            TYPE_KIND,
            self.group_id,
            self.owners.locktime,
            self.owners.threshold,
            &self.owners.addrs,
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<MintOutput> {
        let v = shapes::wrap_nft_mint_output(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(MintOutput {
            group_id: v.group_id(),
            owners: Owners {
                locktime: v.locktime(),
                threshold: v.threshold(),
                addrs: v.addresses().all(),
            },
        })
    }
}

/// One held NFT.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferOutput {
    pub group_id: u32,
    pub payload: Vec<u8>,
    pub owners: Owners,
}

impl TransferOutput {
    pub fn verify(&self) -> Result<()> {
        if self.payload.len() > MAX_PAYLOAD_SIZE {
            return Err(Error::PayloadTooLarge);
        }
        self.owners.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_nft_transfer_output(
            TYPE_KIND,
            self.group_id,
            &self.payload,
            self.owners.locktime,
            self.owners.threshold,
            &self.owners.addrs,
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<TransferOutput> {
        let v = shapes::wrap_nft_transfer_output(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(TransferOutput {
            group_id: v.group_id(),
            payload: v.payload().to_vec(),
            owners: Owners {
                locktime: v.locktime(),
                threshold: v.threshold(),
                addrs: v.addresses().all(),
            },
        })
    }
}

/// Minting NFTs into a group: one payload, handed to one or more owner sets.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MintOperation {
    pub mint_input: Input,
    pub group_id: u32,
    pub payload: Vec<u8>,
    pub outputs: Vec<Owners>,
}

impl MintOperation {
    pub fn cost(&self) -> Result<u64> {
        self.mint_input.cost()
    }

    /// One transfer output per owner set — the whole point of the operation.
    pub fn outs(&self) -> Vec<TransferOutput> {
        self.outputs
            .iter()
            .map(|o| TransferOutput {
                group_id: self.group_id,
                payload: self.payload.clone(),
                owners: o.clone(),
            })
            .collect()
    }

    pub fn verify(&self) -> Result<()> {
        if self.payload.len() > MAX_PAYLOAD_SIZE {
            return Err(Error::PayloadTooLarge);
        }
        for o in &self.outputs {
            o.verify()?;
        }
        self.mint_input.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        let owners: Vec<Vec<u8>> = self.outputs.iter().map(|o| o.bytes()).collect();
        shapes::new_nft_mint_operation(
            TYPE_KIND,
            &self.mint_input.sig_indices,
            self.group_id,
            &self.payload,
            &owners,
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<MintOperation> {
        let v = shapes::wrap_nft_mint_operation(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        let n = v.owners_count() as usize;
        let mut blob = v.owners_bytes();
        let mut outputs = Vec::with_capacity(n.min(1024));
        for _ in 0..n {
            let (env, rest) = wire::next_envelope(blob)?;
            outputs.push(Owners::from_envelope(env)?);
            blob = rest;
        }
        Ok(MintOperation {
            mint_input: Input {
                sig_indices: v.sig_indices(),
            },
            group_id: v.group_id(),
            payload: v.payload().to_vec(),
            outputs,
        })
    }
}

/// Moving one held NFT to a new owner set.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferOperation {
    pub input: Input,
    pub output: TransferOutput,
}

impl TransferOperation {
    pub fn cost(&self) -> Result<u64> {
        self.input.cost()
    }

    pub fn outs(&self) -> Vec<TransferOutput> {
        vec![self.output.clone()]
    }

    pub fn verify(&self) -> Result<()> {
        self.input.verify()?;
        self.output.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_nft_transfer_operation(TYPE_KIND, &self.input.sig_indices, &self.output.bytes())
    }

    pub fn from_envelope(b: &[u8]) -> Result<TransferOperation> {
        let v = shapes::wrap_nft_transfer_operation(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(TransferOperation {
            input: Input {
                sig_indices: v.sig_indices(),
            },
            output: TransferOutput::from_envelope(v.output_bytes())?,
        })
    }
}

/// An nftfx credential: secp256k1 signatures under the nft family byte.
pub fn credential_bytes(c: &SecpCredential) -> Vec<u8> {
    c.bytes_for(TYPE_KIND)
}

pub fn credential_from_envelope(b: &[u8]) -> Result<SecpCredential> {
    SecpCredential::from_envelope_for(b, TYPE_KIND)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ids::ShortId;

    fn owners() -> Owners {
        Owners::new(1, vec![ShortId::prefixed_bytes(&[1])])
    }

    #[test]
    fn a_payload_over_the_limit_is_refused_everywhere_it_can_appear() {
        let big = vec![0u8; MAX_PAYLOAD_SIZE + 1];
        let out = TransferOutput {
            group_id: 0,
            payload: big.clone(),
            owners: owners(),
        };
        assert_eq!(out.verify().unwrap_err(), Error::PayloadTooLarge);

        let op = MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            group_id: 0,
            payload: big,
            outputs: vec![owners()],
        };
        assert_eq!(op.verify().unwrap_err(), Error::PayloadTooLarge);

        // Exactly at the limit is fine.
        let out = TransferOutput {
            group_id: 0,
            payload: vec![0u8; MAX_PAYLOAD_SIZE],
            owners: owners(),
        };
        assert!(out.verify().is_ok());
    }

    #[test]
    fn a_mint_operation_produces_one_output_per_owner_set() {
        let op = MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            group_id: 7,
            payload: vec![1, 2],
            outputs: vec![
                owners(),
                Owners::new(1, vec![ShortId::prefixed_bytes(&[2])]),
            ],
        };
        let outs = op.outs();
        assert_eq!(outs.len(), 2);
        assert!(outs
            .iter()
            .all(|o| o.group_id == 7 && o.payload == vec![1, 2]));
    }

    #[test]
    fn every_nft_primitive_round_trips_through_its_envelope() {
        let m = MintOutput {
            group_id: 3,
            owners: owners(),
        };
        assert_eq!(MintOutput::from_envelope(&m.bytes()).unwrap(), m);

        let t = TransferOutput {
            group_id: 3,
            payload: vec![9, 9, 9],
            owners: owners(),
        };
        assert_eq!(TransferOutput::from_envelope(&t.bytes()).unwrap(), t);

        let op = MintOperation {
            mint_input: Input {
                sig_indices: vec![0, 1],
            },
            group_id: 3,
            payload: vec![1],
            outputs: vec![
                owners(),
                Owners::new(1, vec![ShortId::prefixed_bytes(&[2])]),
            ],
        };
        assert_eq!(MintOperation::from_envelope(&op.bytes()).unwrap(), op);

        let xop = TransferOperation {
            input: Input {
                sig_indices: vec![0],
            },
            output: t,
        };
        assert_eq!(TransferOperation::from_envelope(&xop.bytes()).unwrap(), xop);
    }

    #[test]
    fn an_nft_credential_travels_under_the_nft_family_byte() {
        let c = SecpCredential {
            sigs: vec![[4u8; 65]],
        };
        let raw = credential_bytes(&c);
        assert_eq!(raw[0], TYPE_KIND.as_u8());
        assert_eq!(credential_from_envelope(&raw).unwrap(), c);
        // Read as a secp256k1 credential, it is refused: the family byte is
        // what says which fx answers for the signature.
        assert!(SecpCredential::from_envelope(&raw).is_err());
    }
}
