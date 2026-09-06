// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A shielded transaction: what it says, what it is called, and the whole of
//! what can be decided about it without asking the chain anything.
//!
//! Note construction — deriving a nullifier, committing to a note, encrypting
//! one to a recipient — is a WALLET's work and is not here. A validator holds
//! no spending keys and builds no notes: it checks proofs and the spent set.
//! What it needs of a note is the commitment and the nullifier the transaction
//! already carries.

use crate::error::{Error, Result};
use crate::hash::Fold;
use crate::ids::{Id, EMPTY};

/// A transaction on the way in is bounded, so a peer cannot make this node hold
/// what it would never build.
pub const MAX_TX_SIZE: usize = 1 << 20;

/// What a transaction does.
///
/// A byte, not an enum, because that is what the wire carries: the parser reads
/// it straight off and only [`Tx::validate_basic`] decides whether it is one of
/// the five. Modelling it as an enum would make an out-of-range value a PARSE
/// failure, and the reference reads such a transaction back perfectly well and
/// refuses it a step later — which is a different verdict in a different phase.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct Kind(pub u8);

impl Kind {
    /// Shielded in, shielded out.
    pub const TRANSFER: Kind = Kind(0);
    pub const MINT: Kind = Kind(1);
    pub const BURN: Kind = Kind(2);
    /// Transparent in, shielded out.
    pub const SHIELD: Kind = Kind(3);
    /// Shielded in, transparent out. The last one this chain knows, so it is
    /// also the bound every other value is refused against.
    pub const UNSHIELD: Kind = Kind(4);

    /// The name this kind travels under, or `None` for a byte the chain does
    /// not recognise.
    pub fn name(&self) -> Option<&'static str> {
        Some(match *self {
            Kind::TRANSFER => "Transfer",
            Kind::MINT => "Mint",
            Kind::BURN => "Burn",
            Kind::SHIELD => "Shield",
            Kind::UNSHIELD => "Unshield",
            _ => return None,
        })
    }

    pub fn known(&self) -> bool {
        self.0 <= Kind::UNSHIELD.0
    }
}

impl std::fmt::Display for Kind {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self.name() {
            Some(n) => f.write_str(n),
            None => f.write_str("unknown"),
        }
    }
}

/// An unshielded input.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct TransparentIn {
    pub tx: Id,
    pub output: u32,
    pub amount: u64,
    pub address: Vec<u8>,
}

/// An unshielded output.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransparentOut {
    pub amount: u64,
    pub address: Vec<u8>,
    pub asset: Id,
}

impl Default for TransparentOut {
    fn default() -> Self {
        TransparentOut {
            amount: 0,
            address: Vec::new(),
            asset: EMPTY,
        }
    }
}

/// A confidential output: a commitment to a note, the note encrypted to its
/// recipient, the key it was encrypted under, and a proof of the amount's
/// range.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Shielded {
    pub commitment: Vec<u8>,
    pub note: Vec<u8>,
    pub ephemeral: Vec<u8>,
    pub range_proof: Vec<u8>,
}

/// A zero-knowledge proof, and which system it is under.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Proof {
    /// `stark`, `groth16`, `plonk` — a name, checked against the chain's
    /// profile rather than guessed at from the bytes.
    ///
    /// Bytes, not a string. A peer chooses these bytes and they are folded into
    /// the transaction's identity, so replacing an unpaired byte with anything
    /// — which is what a lossy decode does — would give this node a different
    /// id for the block than the peer that sent it. The gate below compares
    /// bytes and never needs them to be text.
    pub system: Vec<u8>,
    pub data: Vec<u8>,
    pub public: Vec<Vec<u8>>,
}

impl Proof {
    /// The system name for a person to read. Only ever a log line or a verdict
    /// note; no rule is decided on it.
    pub fn system_text(&self) -> std::borrow::Cow<'_, str> {
        String::from_utf8_lossy(&self.system)
    }
}

/// A confidential transaction.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Tx {
    pub kind: Kind,
    pub version: u8,

    /// For shield and unshield: the transparent half.
    pub transparent_in: Vec<TransparentIn>,
    pub transparent_out: Vec<TransparentOut>,

    /// Spent note nullifiers, and the new shielded outputs.
    pub nullifiers: Vec<Vec<u8>>,
    pub outputs: Vec<Shielded>,

    pub proof: Option<Proof>,

    pub fee: u64,
    /// The block height this transaction stops being valid at.
    pub expiry: u64,
    pub memo: Vec<u8>,
}

impl Default for Kind {
    fn default() -> Self {
        Kind::TRANSFER
    }
}

impl Tx {
    /// The transaction's identity: a hash over everything the transaction
    /// means.
    ///
    /// It is NOT carried on the wire and it is NOT a field of this struct —
    /// there is nowhere to put one — because an identity a peer supplies is an
    /// identity a peer chooses, and the proof cache is keyed on it. Copy an
    /// accepted transaction's id, proof and public inputs onto a transaction
    /// spending different notes and a cache keyed on the wire answers "already
    /// verified" before anything binds the proof to what it spends.
    ///
    /// The proof is part of the preimage for the same reason: a transaction
    /// whose proof was tampered with must be a DIFFERENT transaction, or the
    /// cache hands the untampered one's verdict to it.
    pub fn id(&self) -> Id {
        let mut h = Fold::new();
        h.raw(&[self.kind.0, self.version]);

        h.count(self.transparent_in.len());
        for tin in &self.transparent_in {
            h.raw(&tin.tx)
                .num(u64::from(tin.output))
                .num(tin.amount)
                .blob(&tin.address);
        }

        h.count(self.transparent_out.len());
        for tout in &self.transparent_out {
            h.num(tout.amount).raw(&tout.asset).blob(&tout.address);
        }

        h.count(self.nullifiers.len());
        for n in &self.nullifiers {
            h.blob(n);
        }

        h.count(self.outputs.len());
        for out in &self.outputs {
            h.blob(&out.commitment)
                .blob(&out.note)
                .blob(&out.ephemeral)
                .blob(&out.range_proof);
        }

        h.present(self.proof.is_some());
        if let Some(p) = &self.proof {
            h.blob(&p.system).blob(&p.data).count(p.public.len());
            for input in &p.public {
                h.blob(input);
            }
        }

        h.num(self.fee).num(self.expiry).blob(&self.memo);
        h.id()
    }

    /// Every output's commitment, in order. What a proof is bound to on the
    /// output side.
    pub fn commitments(&self) -> Vec<&[u8]> {
        self.outputs.iter().map(|o| o.commitment.as_slice()).collect()
    }

    /// The whole of what a transaction says about itself.
    ///
    /// Nothing here reads the chain, which is what makes it the shape half of
    /// the split the differential compares.
    pub fn validate_basic(&self) -> Result<()> {
        if !self.kind.known() {
            return Err(Error::InvalidTransactionType);
        }

        if self.nullifiers.is_empty() && self.transparent_in.is_empty() {
            return Err(Error::NoInputs);
        }
        if self.outputs.is_empty() && self.transparent_out.is_empty() {
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

        match self.kind {
            Kind::TRANSFER if self.nullifiers.is_empty() || self.outputs.is_empty() => {
                Err(Error::InvalidTransfer)
            }
            Kind::SHIELD if self.transparent_in.is_empty() || self.outputs.is_empty() => {
                Err(Error::InvalidShield)
            }
            Kind::UNSHIELD if self.nullifiers.is_empty() || self.transparent_out.is_empty() => {
                Err(Error::InvalidUnshield)
            }
            _ => Ok(()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bytes(b: u8, n: usize) -> Vec<u8> {
        vec![b; n]
    }

    pub(crate) fn proof(system: &str, seed: u8) -> Proof {
        Proof {
            system: system.as_bytes().to_vec(),
            data: bytes(seed, 192),
            public: vec![bytes(seed + 1, 32), bytes(seed + 2, 32)],
        }
    }

    pub(crate) fn transfer(nullifier: u8) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            version: 1,
            nullifiers: vec![bytes(nullifier, 32)],
            outputs: vec![Shielded {
                commitment: bytes(0x50, 32),
                note: bytes(0x51, 64),
                ephemeral: bytes(0x52, 32),
                range_proof: bytes(0x53, 64),
            }],
            proof: Some(proof("stark", 0x60)),
            fee: 1,
            expiry: 1000,
            ..Tx::default()
        }
    }

    #[test]
    fn the_five_kinds_are_named_and_everything_else_is_not() {
        assert_eq!(Kind::TRANSFER.name(), Some("Transfer"));
        assert_eq!(Kind::MINT.name(), Some("Mint"));
        assert_eq!(Kind::BURN.name(), Some("Burn"));
        assert_eq!(Kind::SHIELD.name(), Some("Shield"));
        assert_eq!(Kind::UNSHIELD.name(), Some("Unshield"));
        assert_eq!(Kind(5).name(), None);
        assert_eq!(Kind(11).to_string(), "unknown");
        assert!(!Kind(11).known());
    }

    #[test]
    fn a_well_formed_transfer_passes() {
        assert_eq!(transfer(0x10).validate_basic(), Ok(()));
    }

    #[test]
    fn a_kind_the_chain_does_not_run_is_refused_before_anything_else() {
        let mut tx = transfer(0x10);
        tx.kind = Kind(Kind::UNSHIELD.0 + 7);
        // Also missing nothing else: the type is what refuses it.
        assert_eq!(tx.validate_basic(), Err(Error::InvalidTransactionType));
    }

    #[test]
    fn a_transaction_with_neither_kind_of_input_has_no_inputs() {
        let mut tx = transfer(0x10);
        tx.nullifiers.clear();
        assert_eq!(tx.validate_basic(), Err(Error::NoInputs));
    }

    #[test]
    fn a_transaction_with_neither_kind_of_output_has_no_outputs() {
        let mut tx = transfer(0x10);
        tx.outputs.clear();
        assert_eq!(tx.validate_basic(), Err(Error::NoOutputs));
    }

    #[test]
    fn a_shielded_transaction_without_a_proof_is_refused() {
        let mut tx = transfer(0x10);
        tx.proof = None;
        assert_eq!(tx.validate_basic(), Err(Error::MissingProof));
    }

    #[test]
    fn a_transaction_that_never_expires_is_refused() {
        let mut tx = transfer(0x10);
        tx.expiry = 0;
        assert_eq!(tx.validate_basic(), Err(Error::NoExpiry));
    }

    /// A transparent input satisfies "has inputs", and a TRANSFER still needs
    /// the shielded kind. The two rules are not the same rule.
    #[test]
    fn a_transfer_needs_shielded_inputs_and_outputs_specifically() {
        let mut tx = transfer(0x10);
        tx.nullifiers.clear();
        tx.transparent_in.push(TransparentIn {
            tx: crate::ids::repeated(0x70),
            output: 0,
            amount: 500,
            address: bytes(0x70, 20),
        });
        assert_eq!(tx.validate_basic(), Err(Error::InvalidTransfer));
    }

    #[test]
    fn a_shield_needs_a_transparent_input() {
        let mut tx = transfer(0x10);
        tx.kind = Kind::SHIELD;
        assert_eq!(tx.validate_basic(), Err(Error::InvalidShield));
    }

    #[test]
    fn an_unshield_needs_a_transparent_output() {
        let mut tx = transfer(0x10);
        tx.kind = Kind::UNSHIELD;
        assert_eq!(tx.validate_basic(), Err(Error::InvalidUnshield));
    }

    /// Mint and Burn carry no shape rule of their own beyond the general ones,
    /// which is why they are absent from the match.
    #[test]
    fn mint_and_burn_are_held_only_to_the_general_rules() {
        for kind in [Kind::MINT, Kind::BURN] {
            let mut tx = transfer(0x10);
            tx.kind = kind;
            assert_eq!(tx.validate_basic(), Ok(()));
            tx.outputs.clear();
            assert_eq!(tx.validate_basic(), Err(Error::NoOutputs));
        }
    }

    /// The pair the corpus carries as `Z_BLOCK_TRANSFER` and
    /// `Z_TX_TAMPERED_PROOF`: one byte of the proof changed, and the identity
    /// has to move. An id that left the proof out would hand the tampered
    /// transaction the untampered one's cached verdict.
    #[test]
    fn tampering_with_a_proof_changes_the_identity() {
        let good = transfer(0x10);
        let mut tampered = transfer(0x10);
        tampered.proof.as_mut().unwrap().data[0] ^= 0xFF;
        assert_ne!(good.id(), tampered.id());
    }

    #[test]
    fn every_field_is_in_the_identity() {
        let base = transfer(0x10);
        let mut seen = std::collections::BTreeSet::new();
        seen.insert(base.id());

        let mut moved = Vec::new();
        let mut with = |f: fn(&mut Tx)| {
            let mut tx = transfer(0x10);
            f(&mut tx);
            moved.push(tx.id());
        };
        with(|t| t.kind = Kind::MINT);
        with(|t| t.version = 2);
        with(|t| t.fee = 2);
        with(|t| t.expiry = 1001);
        with(|t| t.memo = b"m".to_vec());
        with(|t| t.nullifiers[0][0] ^= 1);
        with(|t| t.outputs[0].commitment[0] ^= 1);
        with(|t| t.outputs[0].note[0] ^= 1);
        with(|t| t.outputs[0].ephemeral[0] ^= 1);
        with(|t| t.outputs[0].range_proof[0] ^= 1);
        with(|t| t.proof.as_mut().unwrap().system = b"groth16".to_vec());
        with(|t| t.proof.as_mut().unwrap().public[0][0] ^= 1);
        with(|t| t.proof = None);
        with(|t| {
            t.transparent_out.push(TransparentOut::default());
        });
        with(|t| {
            t.transparent_in.push(TransparentIn::default());
        });

        for id in moved {
            assert!(seen.insert(id), "a field changed and the identity did not");
        }
    }

    /// The prefix rule, at the transaction level: moving a byte from the end of
    /// one nullifier to the start of the next must not leave the identity
    /// where it was.
    #[test]
    fn two_nullifier_lists_that_concatenate_alike_are_two_transactions() {
        let mut a = transfer(0x10);
        a.nullifiers = vec![vec![1, 2], vec![3]];
        let mut b = transfer(0x10);
        b.nullifiers = vec![vec![1], vec![2, 3]];
        assert_ne!(a.id(), b.id());
    }
}
