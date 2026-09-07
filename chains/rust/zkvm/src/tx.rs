// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A shielded transaction: what it spends, what it creates, and the proof that
//! stands for the rest.
//!
//! Note construction — deriving a nullifier, committing to a note, encrypting
//! one to a recipient — is a WALLET's work and is deliberately absent. A
//! validator holds no spending keys and builds no notes: it checks proofs and
//! the spent set. What it needs of a note is the commitment and the nullifier
//! the transaction already carries.

use crate::error::{Error, Result};
use crate::hash::Writer;
use crate::ids::{Id, EMPTY};

/// A transaction on the way in is bounded, so a peer cannot make this node
/// hold what it would never build.
pub const MAX_TX_SIZE: usize = 1 << 20;

/// What a transaction does.
///
/// The numbers are the wire's, and the wire carries them as one byte, so a
/// value past the last variant is a value a peer can send. [`TransactionType::
/// from_u8`] therefore answers with an option rather than a variant.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
#[repr(u8)]
pub enum TransactionType {
    Transfer = 0,
    Mint = 1,
    Burn = 2,
    /// Transparent value in, shielded value out.
    Shield = 3,
    /// Shielded value in, transparent value out.
    Unshield = 4,
}

impl TransactionType {
    /// The last value a transaction may name. A peer's byte above this is not
    /// a type of transaction that exists.
    pub const LAST: u8 = TransactionType::Unshield as u8;

    pub fn from_u8(v: u8) -> Option<Self> {
        use TransactionType::*;
        Some(match v {
            0 => Transfer,
            1 => Mint,
            2 => Burn,
            3 => Shield,
            4 => Unshield,
            _ => return None,
        })
    }

    /// The circuit name a verifying key is looked up under.
    ///
    /// Go keys the map with `string(TransactionType)`, which is Go's
    /// integer-to-string conversion: the ONE code point whose value is the
    /// type's number, UTF-8 encoded. For 0..=4 that is one byte, 0x00..0x04 —
    /// not the decimal spelling, and not the variant's name. A key written any
    /// other way here would look up nothing in a map an operator wrote for the
    /// Go node.
    pub fn circuit_key(self) -> String {
        char::from_u32(self as u32)
            .expect("0..=4 are code points")
            .to_string()
    }
}

/// A transparent input: value entering the shielded pool.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct TransparentInput {
    pub tx_id: Id,
    pub output_idx: u32,
    pub amount: u64,
    pub address: Vec<u8>,
}

/// A transparent output: value leaving it.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct TransparentOutput {
    pub amount: u64,
    pub asset_id: Id,
    pub address: Vec<u8>,
}

/// A confidential output: a commitment, the note encrypted to its recipient,
/// and the range proof for the amount inside it.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct ShieldedOutput {
    pub commitment: Vec<u8>,
    pub encrypted_note: Vec<u8>,
    pub ephemeral_pub_key: Vec<u8>,
    pub output_proof: Vec<u8>,
}

/// The zero-knowledge proof, and the statement it is about.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct ZkProof {
    /// `groth16`, `plonk`, `bulletproofs`, `stark`.
    pub proof_type: String,
    pub proof_data: Vec<u8>,
    pub public_inputs: Vec<Vec<u8>>,
}

/// One shielded transaction.
///
/// `id` is NOT a field a peer fills in. It is derived — see [`Transaction::
/// compute_id`] — and every constructor here that reads bytes off a wire
/// overwrites whatever arrived.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Transaction {
    pub id: Id,
    pub kind: u8,
    pub version: u8,

    pub transparent_inputs: Vec<TransparentInput>,
    pub transparent_outputs: Vec<TransparentOutput>,

    /// The notes this transaction spends, named only by what makes them
    /// unspendable again.
    pub nullifiers: Vec<Vec<u8>>,
    /// The notes it creates.
    pub outputs: Vec<ShieldedOutput>,

    pub proof: Option<ZkProof>,

    pub fee: u64,
    /// The block height at which this stops being valid.
    pub expiry: u64,
    pub memo: Vec<u8>,
}

impl Transaction {
    /// The transaction's identity: a hash over everything it means.
    ///
    /// It is NOT carried on the wire, because an identity a peer supplies is
    /// an identity a peer CHOOSES — and the proof cache is keyed on it. Copy
    /// an accepted transaction's id, proof and public inputs onto a
    /// transaction spending different notes, and a cache keyed on the wire's
    /// id answers "verified" before anything binds the proof to what it
    /// spends.
    ///
    /// Every variable-length field is written with its length first and every
    /// list with its count. See [`crate::hash::Writer`] for why.
    pub fn compute_id(&self) -> Id {
        let mut h = Writer::new();
        h.raw(&[self.kind, self.version]);

        h.count(self.transparent_inputs.len());
        for input in &self.transparent_inputs {
            h.raw(&input.tx_id)
                .num(input.output_idx as u64)
                .num(input.amount)
                .blob(&input.address);
        }

        h.count(self.transparent_outputs.len());
        for out in &self.transparent_outputs {
            h.num(out.amount).raw(&out.asset_id).blob(&out.address);
        }

        h.count(self.nullifiers.len());
        for n in &self.nullifiers {
            h.blob(n);
        }

        h.count(self.outputs.len());
        for o in &self.outputs {
            h.blob(&o.commitment)
                .blob(&o.encrypted_note)
                .blob(&o.ephemeral_pub_key)
                .blob(&o.output_proof);
        }

        h.present(self.proof.is_some());
        if let Some(p) = &self.proof {
            h.blob(p.proof_type.as_bytes()).blob(&p.proof_data);
            h.count(p.public_inputs.len());
            for pi in &p.public_inputs {
                h.blob(pi);
            }
        }

        h.num(self.fee).num(self.expiry).blob(&self.memo);
        h.finish()
    }

    /// The commitments this transaction creates, in output order.
    pub fn output_commitments(&self) -> Vec<&[u8]> {
        self.outputs
            .iter()
            .map(|o| o.commitment.as_slice())
            .collect()
    }

    /// The type this transaction names, or nothing when it names a number that
    /// is not a type. The wire carries a byte, so this is a real possibility
    /// rather than a defensive one.
    pub fn typed(&self) -> Option<TransactionType> {
        TransactionType::from_u8(self.kind)
    }

    /// What can be decided about a transaction on its own, before anything is
    /// looked up.
    ///
    /// Assembly and consensus both run it — see [`crate::vm::ZkVm::admit`] —
    /// so a proposer cannot batch a transaction its own peers refuse. That
    /// used to be skipped on the build path, and a transaction with an
    /// out-of-range type (which the parser reads straight off the wire) was
    /// assembled into every block and then refused by every node's verify,
    /// including the proposer's. Nothing evicted it, so that proposer never
    /// produced another block.
    pub fn validate_basic(&self) -> Result<()> {
        let kind = self.typed().ok_or(Error::InvalidTransactionType)?;

        if self.nullifiers.is_empty() && self.transparent_inputs.is_empty() {
            return Err(Error::NoInputs);
        }
        if self.outputs.is_empty() && self.transparent_outputs.is_empty() {
            return Err(Error::NoOutputs);
        }
        if self.proof.is_none() {
            return Err(Error::MissingProof);
        }

        // A transaction names the height it stops being valid at. Without one
        // it sits in a bounded pool forever: it can never enter a block,
        // nothing evicts it, and once the pool is full of them every honest
        // arrival paying the same floor is refused.
        if self.expiry == 0 {
            return Err(Error::NoExpiry);
        }

        match kind {
            TransactionType::Transfer => {
                if self.nullifiers.is_empty() || self.outputs.is_empty() {
                    return Err(Error::InvalidTransferTransaction);
                }
            }
            TransactionType::Shield => {
                if self.transparent_inputs.is_empty() || self.outputs.is_empty() {
                    return Err(Error::InvalidShieldTransaction);
                }
            }
            TransactionType::Unshield => {
                if self.nullifiers.is_empty() || self.transparent_outputs.is_empty() {
                    return Err(Error::InvalidUnshieldTransaction);
                }
            }
            TransactionType::Mint | TransactionType::Burn => {}
        }
        Ok(())
    }

    /// A transaction with its derived identity in place. Every path that reads
    /// bytes goes through this, so there is one place the id comes from.
    pub fn with_id(mut self) -> Self {
        self.id = self.compute_id();
        self
    }

    /// Has the id been derived? An id still empty is one nothing set.
    pub fn has_id(&self) -> bool {
        self.id != EMPTY
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn transfer() -> Transaction {
        Transaction {
            kind: TransactionType::Transfer as u8,
            version: 1,
            nullifiers: vec![b"n".to_vec()],
            outputs: vec![ShieldedOutput {
                commitment: b"c".to_vec(),
                ..Default::default()
            }],
            proof: Some(ZkProof::default()),
            fee: 1,
            expiry: 10,
            ..Default::default()
        }
    }

    #[test]
    fn a_type_off_the_end_is_not_a_type() {
        let mut tx = transfer();
        tx.kind = 5;
        assert_eq!(tx.validate_basic(), Err(Error::InvalidTransactionType));
        assert_eq!(tx.typed(), None);
    }

    #[test]
    fn every_shape_rule_is_checked() {
        assert!(transfer().validate_basic().is_ok());

        let mut no_in = transfer();
        no_in.nullifiers.clear();
        assert_eq!(no_in.validate_basic(), Err(Error::NoInputs));

        let mut no_out = transfer();
        no_out.outputs.clear();
        assert_eq!(no_out.validate_basic(), Err(Error::NoOutputs));

        let mut no_proof = transfer();
        no_proof.proof = None;
        assert_eq!(no_proof.validate_basic(), Err(Error::MissingProof));

        let mut no_expiry = transfer();
        no_expiry.expiry = 0;
        assert_eq!(no_expiry.validate_basic(), Err(Error::NoExpiry));
    }

    #[test]
    fn a_shield_needs_transparent_value_in_and_shielded_value_out() {
        let mut tx = transfer();
        tx.kind = TransactionType::Shield as u8;
        // It has shielded inputs, which a shield does not.
        assert_eq!(tx.validate_basic(), Err(Error::InvalidShieldTransaction));
        tx.transparent_inputs.push(TransparentInput::default());
        assert!(tx.validate_basic().is_ok());
    }

    #[test]
    fn an_unshield_needs_shielded_value_in_and_transparent_value_out() {
        let mut tx = transfer();
        tx.kind = TransactionType::Unshield as u8;
        assert_eq!(tx.validate_basic(), Err(Error::InvalidUnshieldTransaction));
        tx.transparent_outputs.push(TransparentOutput::default());
        assert!(tx.validate_basic().is_ok());
    }

    #[test]
    fn the_identity_covers_every_field() {
        // Move one bit of each field in turn; the identity must move with it.
        let base = transfer();
        let id = base.compute_id();
        let mut seen = std::collections::HashSet::new();
        seen.insert(id);

        type Move = Box<dyn Fn(&mut Transaction)>;
        let mutate: Vec<Move> = vec![
            Box::new(|t| t.kind = TransactionType::Mint as u8),
            Box::new(|t| t.version = 2),
            Box::new(|t| t.fee += 1),
            Box::new(|t| t.expiry += 1),
            Box::new(|t| t.memo = b"m".to_vec()),
            Box::new(|t| t.nullifiers.push(b"x".to_vec())),
            Box::new(|t| t.outputs[0].commitment = b"d".to_vec()),
            Box::new(|t| t.outputs[0].encrypted_note = b"e".to_vec()),
            Box::new(|t| t.outputs[0].ephemeral_pub_key = b"e".to_vec()),
            Box::new(|t| t.outputs[0].output_proof = b"e".to_vec()),
            Box::new(|t| t.transparent_inputs.push(TransparentInput::default())),
            Box::new(|t| t.transparent_outputs.push(TransparentOutput::default())),
            Box::new(|t| t.proof = None),
            Box::new(|t| t.proof.as_mut().unwrap().proof_type = "stark".into()),
            Box::new(|t| t.proof.as_mut().unwrap().proof_data = b"p".to_vec()),
            Box::new(|t| t.proof.as_mut().unwrap().public_inputs.push(b"i".to_vec())),
        ];
        for m in mutate {
            let mut tx = base.clone();
            m(&mut tx);
            assert!(
                seen.insert(tx.compute_id()),
                "a field left the identity alone"
            );
        }
    }

    #[test]
    fn the_identity_is_not_the_id_field() {
        // Whatever a peer put in `id`, the identity is a function of content.
        let a = transfer();
        let mut b = a.clone();
        b.id = [0xFF; 32];
        assert_eq!(a.compute_id(), b.compute_id());
    }

    #[test]
    fn a_circuit_key_is_the_go_string_conversion() {
        // Go's `string(TransactionType(3))` is the code point U+0003, one
        // byte. Not "3", and not "Shield".
        assert_eq!(TransactionType::Shield.circuit_key().as_bytes(), &[3u8]);
        assert_eq!(TransactionType::Transfer.circuit_key().as_bytes(), &[0u8]);
    }
}
