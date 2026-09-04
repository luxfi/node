// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The feature extensions, and the closed sum types over them.
//!
//! The X-Chain runs three: secp256k1 (value), nft (a payload in a group) and
//! property (bare ownership). That set is CLOSED and known at compile time, so
//! dispatch is a match, not a registry — the compiler checks that every shape
//! is handled and there is no slot map to grow, no reflection, and no way to
//! register a fourth thing at runtime that the verifier has never seen.
//!
//! Four sums, one per role a primitive can play:
//!
//! - [`State`]   — an output that can sit in a UTXO or an asset's initial state
//! - [`FxIn`]    — an input that spends one
//! - [`Op`]      — an operation over existing UTXOs
//! - [`Cred`]    — the signatures answering for an input or an operation
//!
//! Each carries the family byte it travels under, so `bytes()` and
//! `from_envelope()` are inverses and a primitive can always say what it is.

pub mod nft;
pub mod property;
pub mod secp256k1;

use crate::error::{Error, Result};
use crate::wire::{self, peek_discriminator, ShapeKind, TypeKind};

pub use secp256k1::{Credential, FxContext, Input, Owners};

/// One of the three feature extensions this chain runs.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub enum Family {
    Secp256k1,
    Nft,
    Property,
}

impl Family {
    pub fn type_kind(self) -> TypeKind {
        match self {
            Family::Secp256k1 => TypeKind::Secp256k1,
            Family::Nft => TypeKind::Nft,
            Family::Property => TypeKind::Property,
        }
    }

    pub fn from_type_kind(tk: TypeKind) -> Option<Family> {
        match tk {
            TypeKind::Secp256k1 => Some(Family::Secp256k1),
            TypeKind::Nft => Some(Family::Nft),
            TypeKind::Property => Some(Family::Property),
            _ => None,
        }
    }
}

// ---------------------------------------------------------------- outputs --

/// An fx output: something that can be held.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum State {
    Transfer(secp256k1::TransferOutput),
    Mint(secp256k1::MintOutput),
    NftMint(nft::MintOutput),
    NftTransfer(nft::TransferOutput),
    PropertyMint(property::MintOutput),
    PropertyOwned(property::OwnedOutput),
}

impl State {
    pub fn family(&self) -> Family {
        match self {
            State::Transfer(_) | State::Mint(_) => Family::Secp256k1,
            State::NftMint(_) | State::NftTransfer(_) => Family::Nft,
            State::PropertyMint(_) | State::PropertyOwned(_) => Family::Property,
        }
    }

    pub fn verify(&self) -> Result<()> {
        match self {
            State::Transfer(o) => o.verify(),
            State::Mint(o) => o.verify(),
            State::NftMint(o) => o.verify(),
            State::NftTransfer(o) => o.verify(),
            State::PropertyMint(o) => o.verify(),
            State::PropertyOwned(o) => o.verify(),
        }
    }

    /// What this output is worth. Only a value output has an amount; the rest
    /// contribute nothing to the flow check, which is why the answer is zero
    /// rather than an error.
    pub fn amount(&self) -> u64 {
        match self {
            State::Transfer(o) => o.amt,
            _ => 0,
        }
    }

    /// The owner condition, for the leaf the state root commits to.
    pub fn owners(&self) -> &Owners {
        match self {
            State::Transfer(o) => &o.owners,
            State::Mint(o) => &o.owners,
            State::NftMint(o) => &o.owners,
            State::NftTransfer(o) => &o.owners,
            State::PropertyMint(o) => &o.owners,
            State::PropertyOwned(o) => &o.owners,
        }
    }

    /// The addresses this output is indexed by when it crosses to another
    /// chain.
    pub fn addresses(&self) -> Vec<Vec<u8>> {
        self.owners().addresses()
    }

    pub fn bytes(&self) -> Vec<u8> {
        match self {
            State::Transfer(o) => o.bytes(),
            State::Mint(o) => o.bytes(),
            State::NftMint(o) => o.bytes(),
            State::NftTransfer(o) => o.bytes(),
            State::PropertyMint(o) => o.bytes(),
            State::PropertyOwned(o) => o.bytes(),
        }
    }

    /// Read an output from its envelope, dispatched on the (family, shape)
    /// pair. A pair this chain has no primitive for is a refusal, not a guess.
    pub fn from_envelope(env: &[u8]) -> Result<State> {
        let (tk, sk) = peek_discriminator(env)?;
        match (tk, sk) {
            (TypeKind::Secp256k1, ShapeKind::TransferOutput) => Ok(State::Transfer(
                secp256k1::TransferOutput::from_envelope(env)?,
            )),
            (TypeKind::Secp256k1, ShapeKind::MintOutput) => {
                Ok(State::Mint(secp256k1::MintOutput::from_envelope(env)?))
            }
            (TypeKind::Nft, ShapeKind::NftMintOutput) => {
                Ok(State::NftMint(nft::MintOutput::from_envelope(env)?))
            }
            (TypeKind::Nft, ShapeKind::NftTransferOutput) => {
                Ok(State::NftTransfer(nft::TransferOutput::from_envelope(env)?))
            }
            (TypeKind::Property, ShapeKind::MintOutput) => Ok(State::PropertyMint(
                property::MintOutput::from_envelope(env)?,
            )),
            (TypeKind::Property, ShapeKind::OwnedOutput) => Ok(State::PropertyOwned(
                property::OwnedOutput::from_envelope(env)?,
            )),
            _ => Err(Error::UnknownFxPrimitive(tk.as_u8(), sk.as_u8())),
        }
    }

    /// Whether this output may sit in a transferable output.
    ///
    /// A transferable output moves an AMOUNT of an asset, so only a value
    /// output qualifies. An NFT or a mint authority in that slot would be a
    /// transfer of nothing.
    pub fn as_transfer_out(&self) -> Result<&secp256k1::TransferOutput> {
        match self {
            State::Transfer(o) => Ok(o),
            _ => Err(Error::NotATransferableOut),
        }
    }
}

// ----------------------------------------------------------------- inputs --

/// An fx input.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum FxIn {
    Transfer(secp256k1::TransferInput),
}

impl FxIn {
    pub fn family(&self) -> Family {
        match self {
            FxIn::Transfer(_) => Family::Secp256k1,
        }
    }

    pub fn amount(&self) -> u64 {
        match self {
            FxIn::Transfer(i) => i.amt,
        }
    }

    pub fn cost(&self) -> Result<u64> {
        match self {
            FxIn::Transfer(i) => i.cost(),
        }
    }

    pub fn verify(&self) -> Result<()> {
        match self {
            FxIn::Transfer(i) => i.verify(),
        }
    }

    pub fn bytes(&self) -> Vec<u8> {
        match self {
            FxIn::Transfer(i) => i.bytes(),
        }
    }

    pub fn from_envelope(env: &[u8]) -> Result<FxIn> {
        let (tk, sk) = peek_discriminator(env)?;
        match (tk, sk) {
            (TypeKind::Secp256k1, ShapeKind::TransferInput) => Ok(FxIn::Transfer(
                secp256k1::TransferInput::from_envelope(env)?,
            )),
            _ => Err(Error::UnknownFxPrimitive(tk.as_u8(), sk.as_u8())),
        }
    }
}

// ------------------------------------------------------------- operations --

/// An operation over existing UTXOs.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Op {
    Mint(secp256k1::MintOperation),
    NftMint(nft::MintOperation),
    NftTransfer(nft::TransferOperation),
    PropertyMint(property::MintOperation),
    PropertyBurn(property::BurnOperation),
}

impl Op {
    pub fn family(&self) -> Family {
        match self {
            Op::Mint(_) => Family::Secp256k1,
            Op::NftMint(_) | Op::NftTransfer(_) => Family::Nft,
            Op::PropertyMint(_) | Op::PropertyBurn(_) => Family::Property,
        }
    }

    pub fn cost(&self) -> Result<u64> {
        match self {
            Op::Mint(o) => o.cost(),
            Op::NftMint(o) => o.cost(),
            Op::NftTransfer(o) => o.cost(),
            Op::PropertyMint(o) => o.cost(),
            Op::PropertyBurn(o) => o.cost(),
        }
    }

    pub fn verify(&self) -> Result<()> {
        match self {
            Op::Mint(o) => o.verify(),
            Op::NftMint(o) => o.verify(),
            Op::NftTransfer(o) => o.verify(),
            Op::PropertyMint(o) => o.verify(),
            Op::PropertyBurn(o) => o.verify(),
        }
    }

    /// What the operation produces. These become UTXOs at the ids the
    /// executor assigns.
    pub fn outs(&self) -> Vec<State> {
        match self {
            Op::Mint(o) => vec![
                State::Mint(o.mint_output.clone()),
                State::Transfer(o.transfer_output.clone()),
            ],
            Op::NftMint(o) => o.outs().into_iter().map(State::NftTransfer).collect(),
            Op::NftTransfer(o) => o.outs().into_iter().map(State::NftTransfer).collect(),
            Op::PropertyMint(o) => vec![
                State::PropertyMint(o.mint_output.clone()),
                State::PropertyOwned(o.owned_output.clone()),
            ],
            Op::PropertyBurn(_) => Vec::new(),
        }
    }

    pub fn bytes(&self) -> Vec<u8> {
        match self {
            Op::Mint(o) => o.bytes(),
            Op::NftMint(o) => o.bytes(),
            Op::NftTransfer(o) => o.bytes(),
            Op::PropertyMint(o) => o.bytes(),
            Op::PropertyBurn(o) => o.bytes(),
        }
    }

    pub fn from_envelope(env: &[u8]) -> Result<Op> {
        let (tk, sk) = peek_discriminator(env)?;
        match (tk, sk) {
            (TypeKind::Secp256k1, ShapeKind::MintOperation) => {
                Ok(Op::Mint(secp256k1::MintOperation::from_envelope(env)?))
            }
            (TypeKind::Nft, ShapeKind::NftMintOperation) => {
                Ok(Op::NftMint(nft::MintOperation::from_envelope(env)?))
            }
            (TypeKind::Nft, ShapeKind::NftTransferOp) => {
                Ok(Op::NftTransfer(nft::TransferOperation::from_envelope(env)?))
            }
            (TypeKind::Property, ShapeKind::MintOperation) => Ok(Op::PropertyMint(
                property::MintOperation::from_envelope(env)?,
            )),
            (TypeKind::Property, ShapeKind::BurnOperation) => Ok(Op::PropertyBurn(
                property::BurnOperation::from_envelope(env)?,
            )),
            _ => Err(Error::UnknownFxPrimitive(tk.as_u8(), sk.as_u8())),
        }
    }
}

// ------------------------------------------------------------ credentials --

/// A credential, tagged with the fx that answers for it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Cred {
    pub family: Family,
    pub cred: Credential,
}

impl Cred {
    pub fn new(family: Family, cred: Credential) -> Self {
        Cred { family, cred }
    }

    pub fn verify(&self) -> Result<()> {
        self.cred.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        self.cred.bytes_for(self.family.type_kind())
    }

    pub fn from_envelope(env: &[u8]) -> Result<Cred> {
        let (tk, sk) = peek_discriminator(env)?;
        if sk != ShapeKind::Credential {
            return Err(Error::UnknownFxPrimitive(tk.as_u8(), sk.as_u8()));
        }
        let family =
            Family::from_type_kind(tk).ok_or(Error::UnknownFxPrimitive(tk.as_u8(), sk.as_u8()))?;
        Ok(Cred {
            family,
            cred: Credential::from_envelope_for(env, tk)?,
        })
    }
}

// -------------------------------------------------------------- fx lookup --

/// The number of family bytes an [`FxIndex`] can hold — the dense enum plus
/// room, indexed by the byte itself.
const NUM_TYPE_KINDS: usize = 16;

/// Where each fx family sits in a chain's fx list.
///
/// An asset says which fx it supports by INDEX, so the index has to be stable
/// and has to be looked up by the family a primitive names on the wire. An
/// array indexed by the family byte: one bounds-checked load, no hashing, and
/// nothing to register at runtime.
#[derive(Clone, Debug)]
pub struct FxIndex {
    by_kind: [i32; NUM_TYPE_KINDS],
}

impl Default for FxIndex {
    fn default() -> Self {
        FxIndex::new()
    }
}

impl FxIndex {
    /// Every family unregistered.
    pub fn new() -> Self {
        FxIndex {
            by_kind: [-1; NUM_TYPE_KINDS],
        }
    }

    /// The index for a chain running the three fx in their canonical order:
    /// secp256k1, nft, property.
    pub fn standard() -> Self {
        let mut fi = FxIndex::new();
        fi.set(TypeKind::Secp256k1, 0);
        fi.set(TypeKind::Nft, 1);
        fi.set(TypeKind::Property, 2);
        fi
    }

    pub fn set(&mut self, kind: TypeKind, idx: usize) {
        let k = kind.as_u8() as usize;
        if k < NUM_TYPE_KINDS {
            self.by_kind[k] = idx as i32;
        }
    }

    pub fn get(&self, kind: TypeKind) -> Option<usize> {
        let k = kind.as_u8() as usize;
        if k >= NUM_TYPE_KINDS {
            return None;
        }
        let idx = self.by_kind[k];
        if idx < 0 {
            return None;
        }
        Some(idx as usize)
    }

    pub fn get_family(&self, family: Family) -> Option<usize> {
        self.get(family.type_kind())
    }

    /// How many families are registered.
    pub fn len(&self) -> usize {
        self.by_kind.iter().filter(|i| **i >= 0).count()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

// ------------------------------------------------------------ the fx rules --

/// Can this input, with this credential, spend this output?
///
/// The fx that answers is the one the CREDENTIAL names — the same fx the
/// caller resolved an index for — and it insists the other two values are the
/// shapes it knows. An nft or property fx refuses outright: neither moves
/// value, so neither has a transfer rule at all.
pub fn verify_transfer(
    family: Family,
    ctx: &FxContext,
    unsigned_bytes: &[u8],
    input: &FxIn,
    cred: &Cred,
    utxo: &State,
) -> Result<()> {
    match family {
        Family::Nft | Family::Property => Err(Error::CantTransfer),
        Family::Secp256k1 => {
            let FxIn::Transfer(in_) = input;
            if cred.family != Family::Secp256k1 {
                return Err(Error::WrongCredentialType);
            }
            let out = match utxo {
                State::Transfer(o) => o,
                _ => return Err(Error::WrongUtxoType),
            };
            out.verify()?;
            in_.verify()?;
            cred.cred.verify()?;
            if out.amt != in_.amt {
                return Err(Error::MismatchedAmounts(out.amt, in_.amt));
            }
            secp256k1::verify_credentials(ctx, unsigned_bytes, &in_.input, &cred.cred, &out.owners)
        }
    }
}

/// Can this operation, with this credential, consume these UTXOs?
///
/// Every one of the five operations takes exactly one UTXO, and each insists
/// that UTXO is the authority or the holding it claims to act on — an nft mint
/// against the group it names, a property mint against the same owner set it
/// re-issues, an nft transfer against the very payload it is moving.
pub fn verify_operation(
    family: Family,
    ctx: &FxContext,
    unsigned_bytes: &[u8],
    op: &Op,
    cred: &Cred,
    utxos: &[State],
) -> Result<()> {
    if cred.family != family {
        return Err(Error::WrongCredentialType);
    }
    if utxos.len() != 1 {
        return Err(Error::WrongNumberOfUtxos);
    }
    let utxo = &utxos[0];

    match (family, op) {
        (Family::Secp256k1, Op::Mint(op)) => {
            let out = match utxo {
                State::Mint(o) => o,
                _ => return Err(Error::WrongUtxoType),
            };
            op.verify()?;
            cred.cred.verify()?;
            out.verify()?;
            if !out.owners.equals(&op.mint_output.owners) {
                return Err(Error::WrongMintCreated);
            }
            secp256k1::verify_credentials(
                ctx,
                unsigned_bytes,
                &op.mint_input,
                &cred.cred,
                &out.owners,
            )
        }
        (Family::Nft, Op::NftMint(op)) => {
            let out = match utxo {
                State::NftMint(o) => o,
                _ => return Err(Error::WrongUtxoType),
            };
            op.verify()?;
            cred.cred.verify()?;
            out.verify()?;
            if out.group_id != op.group_id {
                return Err(Error::WrongUniqueId);
            }
            secp256k1::verify_credentials(
                ctx,
                unsigned_bytes,
                &op.mint_input,
                &cred.cred,
                &out.owners,
            )
        }
        (Family::Nft, Op::NftTransfer(op)) => {
            let out = match utxo {
                State::NftTransfer(o) => o,
                _ => return Err(Error::WrongUtxoType),
            };
            op.verify()?;
            cred.cred.verify()?;
            out.verify()?;
            if out.group_id != op.output.group_id {
                return Err(Error::WrongUniqueId);
            }
            if out.payload != op.output.payload {
                return Err(Error::WrongBytes);
            }
            secp256k1::verify_credentials(ctx, unsigned_bytes, &op.input, &cred.cred, &out.owners)
        }
        (Family::Property, Op::PropertyMint(op)) => {
            let out = match utxo {
                State::PropertyMint(o) => o,
                _ => return Err(Error::WrongUtxoType),
            };
            op.verify()?;
            cred.cred.verify()?;
            out.verify()?;
            if !out.owners.equals(&op.mint_output.owners) {
                return Err(Error::WrongMintOutput);
            }
            secp256k1::verify_credentials(
                ctx,
                unsigned_bytes,
                &op.mint_input,
                &cred.cred,
                &out.owners,
            )
        }
        (Family::Property, Op::PropertyBurn(op)) => {
            let out = match utxo {
                State::PropertyOwned(o) => o,
                _ => return Err(Error::WrongUtxoType),
            };
            op.verify()?;
            cred.cred.verify()?;
            out.verify()?;
            secp256k1::verify_credentials(ctx, unsigned_bytes, &op.input, &cred.cred, &out.owners)
        }
        // The operation belongs to a different fx than the one asked to run it.
        _ => Err(Error::WrongOpType),
    }
}

/// The wire error a caller sees when it hands a `Cred` bytes that are not one.
pub fn credential_shape_error() -> Error {
    Error::Wire(wire::Error::WrongShapeKind)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::hash::sha256;
    use crate::ids::ShortId;

    fn owners_of(secret: &[u8; 32]) -> Owners {
        Owners::new(1, vec![secp256k1::address_of(secret).unwrap()])
    }

    fn ctx() -> FxContext {
        FxContext {
            now: 1000,
            bootstrapped: true,
        }
    }

    fn cred_over(secret: &[u8; 32], bytes: &[u8], family: Family) -> Cred {
        let sig = secp256k1::sign_hash(secret, &sha256(bytes)).unwrap();
        Cred::new(family, Credential { sigs: vec![sig] })
    }

    #[test]
    fn every_state_output_round_trips_through_the_sum_type() {
        let o = Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]);
        for s in [
            State::Transfer(secp256k1::TransferOutput {
                amt: 5,
                owners: o.clone(),
            }),
            State::Mint(secp256k1::MintOutput { owners: o.clone() }),
            State::NftMint(nft::MintOutput {
                group_id: 1,
                owners: o.clone(),
            }),
            State::NftTransfer(nft::TransferOutput {
                group_id: 1,
                payload: vec![1],
                owners: o.clone(),
            }),
            State::PropertyMint(property::MintOutput { owners: o.clone() }),
            State::PropertyOwned(property::OwnedOutput { owners: o.clone() }),
        ] {
            assert_eq!(State::from_envelope(&s.bytes()).unwrap(), s);
        }
    }

    #[test]
    fn every_operation_round_trips_through_the_sum_type() {
        let o = Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]);
        let i = Input {
            sig_indices: vec![0],
        };
        for op in [
            Op::Mint(secp256k1::MintOperation {
                mint_input: i.clone(),
                mint_output: secp256k1::MintOutput { owners: o.clone() },
                transfer_output: secp256k1::TransferOutput {
                    amt: 1,
                    owners: o.clone(),
                },
            }),
            Op::NftMint(nft::MintOperation {
                mint_input: i.clone(),
                group_id: 1,
                payload: vec![2],
                outputs: vec![o.clone()],
            }),
            Op::NftTransfer(nft::TransferOperation {
                input: i.clone(),
                output: nft::TransferOutput {
                    group_id: 1,
                    payload: vec![2],
                    owners: o.clone(),
                },
            }),
            Op::PropertyMint(property::MintOperation {
                mint_input: i.clone(),
                mint_output: property::MintOutput { owners: o.clone() },
                owned_output: property::OwnedOutput { owners: o.clone() },
            }),
            Op::PropertyBurn(property::BurnOperation { input: i.clone() }),
        ] {
            assert_eq!(Op::from_envelope(&op.bytes()).unwrap(), op);
        }
    }

    #[test]
    fn a_credential_keeps_the_family_that_answers_for_it() {
        for family in [Family::Secp256k1, Family::Nft, Family::Property] {
            let c = Cred::new(
                family,
                Credential {
                    sigs: vec![[1u8; 65]],
                },
            );
            assert_eq!(Cred::from_envelope(&c.bytes()).unwrap(), c);
        }
    }

    #[test]
    fn a_pair_this_chain_has_no_primitive_for_is_refused() {
        // A P-chain style locked output: reserved family, a shape the X-Chain
        // does not carry.
        let env = [
            TypeKind::Reserved.as_u8(),
            ShapeKind::LockedOutput.as_u8(),
            0,
            0,
        ];
        assert!(matches!(
            State::from_envelope(&env).unwrap_err(),
            Error::UnknownFxPrimitive(_, _)
        ));
    }

    #[test]
    fn only_a_value_output_may_sit_in_a_transferable_output() {
        let o = Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]);
        assert!(State::Transfer(secp256k1::TransferOutput {
            amt: 1,
            owners: o.clone()
        })
        .as_transfer_out()
        .is_ok());
        assert_eq!(
            State::Mint(secp256k1::MintOutput { owners: o })
                .as_transfer_out()
                .unwrap_err(),
            Error::NotATransferableOut
        );
    }

    #[test]
    fn the_fx_index_answers_by_family_and_says_so_when_it_cannot() {
        let fi = FxIndex::standard();
        assert_eq!(fi.get_family(Family::Secp256k1), Some(0));
        assert_eq!(fi.get_family(Family::Nft), Some(1));
        assert_eq!(fi.get_family(Family::Property), Some(2));
        assert_eq!(fi.get(TypeKind::Mldsa), None);
        assert_eq!(fi.get(TypeKind::Unknown), None);
        assert_eq!(fi.len(), 3);
    }

    #[test]
    fn a_value_output_is_spent_by_its_owner_and_by_nobody_else() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let utxo = State::Transfer(secp256k1::TransferOutput {
            amt: 10,
            owners: owners.clone(),
        });
        let input = FxIn::Transfer(secp256k1::TransferInput {
            amt: 10,
            input: Input {
                sig_indices: vec![0],
            },
        });
        let tx = b"the unsigned bytes";
        let good = cred_over(&secret, tx, Family::Secp256k1);
        assert!(verify_transfer(Family::Secp256k1, &ctx(), tx, &input, &good, &utxo).is_ok());

        let stranger = cred_over(&[12u8; 32], tx, Family::Secp256k1);
        assert_eq!(
            verify_transfer(Family::Secp256k1, &ctx(), tx, &input, &stranger, &utxo).unwrap_err(),
            Error::WrongSig
        );
    }

    #[test]
    fn an_input_that_claims_a_different_amount_than_the_utxo_holds_is_refused() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let utxo = State::Transfer(secp256k1::TransferOutput { amt: 10, owners });
        let input = FxIn::Transfer(secp256k1::TransferInput {
            amt: 9,
            input: Input {
                sig_indices: vec![0],
            },
        });
        let tx = b"tx";
        let cred = cred_over(&secret, tx, Family::Secp256k1);
        assert_eq!(
            verify_transfer(Family::Secp256k1, &ctx(), tx, &input, &cred, &utxo).unwrap_err(),
            Error::MismatchedAmounts(10, 9)
        );
    }

    #[test]
    fn the_nft_and_property_fx_have_no_transfer_rule_at_all() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let utxo = State::Transfer(secp256k1::TransferOutput { amt: 1, owners });
        let input = FxIn::Transfer(secp256k1::TransferInput {
            amt: 1,
            input: Input {
                sig_indices: vec![0],
            },
        });
        let cred = cred_over(&secret, b"tx", Family::Nft);
        for family in [Family::Nft, Family::Property] {
            assert_eq!(
                verify_transfer(family, &ctx(), b"tx", &input, &cred, &utxo).unwrap_err(),
                Error::CantTransfer
            );
        }
    }

    #[test]
    fn a_mint_operation_must_re_issue_the_very_authority_it_consumed() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let other = Owners::new(1, vec![ShortId::prefixed_bytes(&[9])]);
        let tx = b"tx";
        let cred = cred_over(&secret, tx, Family::Secp256k1);
        let utxo = State::Mint(secp256k1::MintOutput {
            owners: owners.clone(),
        });

        let good = Op::Mint(secp256k1::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: secp256k1::MintOutput {
                owners: owners.clone(),
            },
            transfer_output: secp256k1::TransferOutput {
                amt: 1,
                owners: owners.clone(),
            },
        });
        assert!(verify_operation(
            Family::Secp256k1,
            &ctx(),
            tx,
            &good,
            &cred,
            std::slice::from_ref(&utxo)
        )
        .is_ok());

        let stolen = Op::Mint(secp256k1::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: secp256k1::MintOutput { owners: other },
            transfer_output: secp256k1::TransferOutput { amt: 1, owners },
        });
        assert_eq!(
            verify_operation(Family::Secp256k1, &ctx(), tx, &stolen, &cred, &[utxo]).unwrap_err(),
            Error::WrongMintCreated
        );
    }

    #[test]
    fn an_operation_takes_exactly_one_utxo() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let cred = cred_over(&secret, b"tx", Family::Secp256k1);
        let op = Op::Mint(secp256k1::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: secp256k1::MintOutput {
                owners: owners.clone(),
            },
            transfer_output: secp256k1::TransferOutput {
                amt: 1,
                owners: owners.clone(),
            },
        });
        let utxo = State::Mint(secp256k1::MintOutput { owners });
        for utxos in [vec![], vec![utxo.clone(), utxo]] {
            assert_eq!(
                verify_operation(Family::Secp256k1, &ctx(), b"tx", &op, &cred, &utxos).unwrap_err(),
                Error::WrongNumberOfUtxos
            );
        }
    }

    #[test]
    fn an_nft_mint_must_name_the_group_the_authority_covers() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let tx = b"tx";
        let cred = cred_over(&secret, tx, Family::Nft);
        let utxo = State::NftMint(nft::MintOutput {
            group_id: 3,
            owners: owners.clone(),
        });
        let wrong_group = Op::NftMint(nft::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            group_id: 4,
            payload: vec![1],
            outputs: vec![owners.clone()],
        });
        assert_eq!(
            verify_operation(
                Family::Nft,
                &ctx(),
                tx,
                &wrong_group,
                &cred,
                std::slice::from_ref(&utxo)
            )
            .unwrap_err(),
            Error::WrongUniqueId
        );
        let right = Op::NftMint(nft::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            group_id: 3,
            payload: vec![1],
            outputs: vec![owners],
        });
        assert!(verify_operation(Family::Nft, &ctx(), tx, &right, &cred, &[utxo]).is_ok());
    }

    #[test]
    fn an_nft_transfer_must_move_the_very_payload_it_consumed() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let tx = b"tx";
        let cred = cred_over(&secret, tx, Family::Nft);
        let utxo = State::NftTransfer(nft::TransferOutput {
            group_id: 3,
            payload: vec![1, 2, 3],
            owners: owners.clone(),
        });
        let changed = Op::NftTransfer(nft::TransferOperation {
            input: Input {
                sig_indices: vec![0],
            },
            output: nft::TransferOutput {
                group_id: 3,
                payload: vec![9],
                owners: owners.clone(),
            },
        });
        assert_eq!(
            verify_operation(
                Family::Nft,
                &ctx(),
                tx,
                &changed,
                &cred,
                std::slice::from_ref(&utxo)
            )
            .unwrap_err(),
            Error::WrongBytes
        );
        let kept = Op::NftTransfer(nft::TransferOperation {
            input: Input {
                sig_indices: vec![0],
            },
            output: nft::TransferOutput {
                group_id: 3,
                payload: vec![1, 2, 3],
                owners,
            },
        });
        assert!(verify_operation(Family::Nft, &ctx(), tx, &kept, &cred, &[utxo]).is_ok());
    }

    #[test]
    fn a_property_burn_consumes_an_owned_output_and_produces_nothing() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let tx = b"tx";
        let cred = cred_over(&secret, tx, Family::Property);
        let utxo = State::PropertyOwned(property::OwnedOutput { owners });
        let op = Op::PropertyBurn(property::BurnOperation {
            input: Input {
                sig_indices: vec![0],
            },
        });
        assert!(verify_operation(Family::Property, &ctx(), tx, &op, &cred, &[utxo]).is_ok());
        assert!(op.outs().is_empty());
    }

    #[test]
    fn an_operation_run_by_the_wrong_fx_is_refused() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let cred = cred_over(&secret, b"tx", Family::Nft);
        let op = Op::Mint(secp256k1::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: secp256k1::MintOutput {
                owners: owners.clone(),
            },
            transfer_output: secp256k1::TransferOutput {
                amt: 1,
                owners: owners.clone(),
            },
        });
        let utxo = State::NftMint(nft::MintOutput {
            group_id: 1,
            owners,
        });
        assert_eq!(
            verify_operation(Family::Nft, &ctx(), b"tx", &op, &cred, &[utxo]).unwrap_err(),
            Error::WrongOpType
        );
    }

    #[test]
    fn a_credential_from_another_fx_cannot_answer_for_this_one() {
        let secret = [11u8; 32];
        let owners = owners_of(&secret);
        let cred = cred_over(&secret, b"tx", Family::Nft);
        let utxo = State::Mint(secp256k1::MintOutput {
            owners: owners.clone(),
        });
        let op = Op::Mint(secp256k1::MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: secp256k1::MintOutput {
                owners: owners.clone(),
            },
            transfer_output: secp256k1::TransferOutput { amt: 1, owners },
        });
        assert_eq!(
            verify_operation(Family::Secp256k1, &ctx(), b"tx", &op, &cred, &[utxo]).unwrap_err(),
            Error::WrongCredentialType
        );
    }
}
