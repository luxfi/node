// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The fx primitive shapes.
//!
//! Each is a struct in `chains/schema/xchain.zap`; its offsets and its
//! accessors come from there, through `zapgen`. What is here is the part that
//! is not the format: the two-byte envelope every primitive travels behind —
//! the family that owns it and the shape within that family — and the gates
//! that decide whether a set of values means anything.
//!
//! The readers are permissive by design: ZAP answers a zero for a field that is
//! not there. The gates that decide whether a threshold is a quorum live in
//! [`verify_owners`], and every consumer that treats a threshold as a quorum
//! has to call it before believing one.

use super::{open, write_envelope_prefix, Error, ShapeKind, TypeKind};
use crate::ids::{ShortId, SHORT_ID_LEN};
use crate::xchain_zap as wire;
use lux_zap::zap;

pub use wire::{
    BurnOperation, Credential, MintOperation, NftMintOperation, NftMintOutput,
    NftTransferOperation, NftTransferOutput, Owners, TransferInput, TransferOutput,
};

/// The addresses of an owner list, as the chain's own type.
pub fn addrs(list: zap::List<'_>) -> Vec<ShortId> {
    (0..list.len()).map(|i| addr_at(list, i)).collect()
}

/// Address `i`, or the zero address when out of range.
///
/// The zero address IS a phantom signer, which is why a length-padded list has
/// to be refused by [`verify_owners`] before anyone iterates one.
pub fn addr_at(list: zap::List<'_>, i: usize) -> ShortId {
    let mut out = [0u8; SHORT_ID_LEN];
    let e = list.object(i, SHORT_ID_LEN);
    out.copy_from_slice(e.bytes_fixed(0, SHORT_ID_LEN));
    ShortId(out)
}

/// A signature-index run, as the chain's own type.
pub fn indices(list: zap::List<'_>) -> Vec<u32> {
    (0..list.len()).map(|i| list.u32(i)).collect()
}

/// What an owner set can be refused for. One canonical set, so every fx that
/// has a multi-address owner group refuses for the same reasons.
pub const ERR_OWNER_ADDRS_EMPTY: Error =
    Error::Owner("wire: OutputOwners.Addresses is empty — signer set undefined");
pub const ERR_OWNER_THRESHOLD_ZERO: Error =
    Error::Owner("wire: OutputOwners.Threshold must be > 0; threshold=0 disables authorization");
pub const ERR_OWNER_THRESHOLD_EXCEEDS: Error = Error::Owner(
    "wire: OutputOwners.Threshold exceeds Addresses.Len() — unsatisfiable signer quorum",
);
pub const ERR_OWNER_ADDR_ZERO: Error =
    Error::Owner("wire: OutputOwners.Addresses contains the zero ShortID — phantom signer");

/// Whether a threshold over these addresses is a quorum anyone could reach.
pub fn verify_owners(threshold: u32, list: zap::List<'_>) -> Result<(), Error> {
    let n = list.len();
    if n == 0 {
        return Err(ERR_OWNER_ADDRS_EMPTY);
    }
    if threshold == 0 {
        return Err(ERR_OWNER_THRESHOLD_ZERO);
    }
    if threshold as u64 > n as u64 {
        return Err(ERR_OWNER_THRESHOLD_EXCEEDS);
    }
    for i in 0..n {
        if addr_at(list, i).is_empty() {
            return Err(ERR_OWNER_ADDR_ZERO);
        }
    }
    Ok(())
}

/// The addresses, as the schema's own element type.
fn packed(addrs: &[ShortId]) -> Vec<[u8; SHORT_ID_LEN]> {
    addrs.iter().map(|a| a.0).collect()
}

/// How many signatures of `width` bytes a credential's run holds — zero when
/// it does not divide, because a partial signature is not a signature.
pub fn signature_count(c: &Credential<'_>, width: usize) -> usize {
    if width == 0 {
        return 0;
    }
    let total = c.signatures().len();
    if !total.is_multiple_of(width) {
        return 0;
    }
    total / width
}

/// Signature `i` of a credential's run.
///
/// Indexing answers by span alone, which is why it and [`signature_count`] are
/// allowed to disagree: nothing reads a signature it did not first count.
pub fn signature_at(c: &Credential<'_>, i: usize, width: usize) -> Option<Vec<u8>> {
    if width == 0 {
        return None;
    }
    let all = c.signatures().bytes();
    let start = i.checked_mul(width)?;
    let end = start.checked_add(width)?;
    if end > all.len() {
        return None;
    }
    Some(all[start..end].to_vec())
}

// ---- reading: the family byte, and the view the schema wrote --------------
/// A view of a Owners. Owners are not fx-owned: the same payload is shared by
/// every fx with a multi-address owner group, so the family byte is reserved.
pub fn wrap_output_owners(b: &[u8]) -> Result<Owners<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::OutputOwners, false)?;
    Ok(Owners::new(msg.root()))
}
/// The family that owns this TransferOutput, and a view of it.
pub fn wrap_transfer_output(b: &[u8]) -> Result<(TypeKind, TransferOutput<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::TransferOutput, true)?;
    Ok((tk, TransferOutput::new(msg.root())))
}
/// The family that owns this TransferInput, and a view of it.
pub fn wrap_transfer_input(b: &[u8]) -> Result<(TypeKind, TransferInput<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::TransferInput, true)?;
    Ok((tk, TransferInput::new(msg.root())))
}
/// The family that owns this Owners, and a view of it.
pub fn wrap_mint_output(b: &[u8]) -> Result<(TypeKind, Owners<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::MintOutput, true)?;
    Ok((tk, Owners::new(msg.root())))
}
/// The family that owns this Owners, and a view of it.
pub fn wrap_owned_output(b: &[u8]) -> Result<(TypeKind, Owners<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::OwnedOutput, true)?;
    Ok((tk, Owners::new(msg.root())))
}
/// The family that owns this Credential, and a view of it.
pub fn wrap_credential(b: &[u8]) -> Result<(TypeKind, Credential<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::Credential, true)?;
    Ok((tk, Credential::new(msg.root())))
}
/// The family that owns this MintOperation, and a view of it.
pub fn wrap_mint_operation(b: &[u8]) -> Result<(TypeKind, MintOperation<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::MintOperation, true)?;
    Ok((tk, MintOperation::new(msg.root())))
}
/// The family that owns this BurnOperation, and a view of it.
pub fn wrap_burn_operation(b: &[u8]) -> Result<(TypeKind, BurnOperation<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::BurnOperation, true)?;
    Ok((tk, BurnOperation::new(msg.root())))
}
/// The family that owns this NftMintOutput, and a view of it.
pub fn wrap_nft_mint_output(b: &[u8]) -> Result<(TypeKind, NftMintOutput<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::NftMintOutput, true)?;
    Ok((tk, NftMintOutput::new(msg.root())))
}
/// The family that owns this NftTransferOutput, and a view of it.
pub fn wrap_nft_transfer_output(b: &[u8]) -> Result<(TypeKind, NftTransferOutput<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::NftTransferOutput, true)?;
    Ok((tk, NftTransferOutput::new(msg.root())))
}
/// The family that owns this NftMintOperation, and a view of it.
pub fn wrap_nft_mint_operation(b: &[u8]) -> Result<(TypeKind, NftMintOperation<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::NftMintOperation, true)?;
    Ok((tk, NftMintOperation::new(msg.root())))
}
/// The family that owns this NftTransferOperation, and a view of it.
pub fn wrap_nft_transfer_operation(b: &[u8]) -> Result<(TypeKind, NftTransferOperation<'_>), Error> {
    let (tk, msg) = open(b, ShapeKind::NftTransferOp, true)?;
    Ok((tk, NftTransferOperation::new(msg.root())))
}

// ---- writing: the message the schema writes, behind its two bytes --------

pub fn new_output_owners(locktime: u64, threshold: u32, addrs: &[ShortId]) -> Vec<u8> {
    owners_envelope(TypeKind::Reserved, ShapeKind::OutputOwners, locktime, threshold, addrs)
}

pub fn new_mint_output(tk: TypeKind, locktime: u64, threshold: u32, addrs: &[ShortId]) -> Vec<u8> {
    owners_envelope(tk, ShapeKind::MintOutput, locktime, threshold, addrs)
}

pub fn new_owned_output(tk: TypeKind, locktime: u64, threshold: u32, addrs: &[ShortId]) -> Vec<u8> {
    owners_envelope(tk, ShapeKind::OwnedOutput, locktime, threshold, addrs)
}

/// One owner set, under whichever shape byte the caller means it as.
fn owners_envelope(
    tk: TypeKind,
    sk: ShapeKind,
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let a = packed(addrs);
    let msg = wire::new_owners(&wire::OwnersInput {
        locktime,
        threshold,
        addresses: &a,
    });
    write_envelope_prefix(tk, sk, msg)
}

pub fn new_transfer_output(
    tk: TypeKind,
    amount: u64,
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let a = packed(addrs);
    let msg = wire::new_transfer_output(&wire::TransferOutputInput {
        amount,
        locktime,
        threshold,
        addresses: &a,
    });
    write_envelope_prefix(tk, ShapeKind::TransferOutput, msg)
}

pub fn new_transfer_input(tk: TypeKind, amount: u64, sig_indices: &[u32]) -> Vec<u8> {
    let msg = wire::new_transfer_input(&wire::TransferInputInput {
        amount,
        sig_indices,
    });
    write_envelope_prefix(tk, ShapeKind::TransferInput, msg)
}

pub fn new_credential(
    tk: TypeKind,
    security_level: u8,
    signatures: &[u8],
    pub_keys: &[u8],
) -> Vec<u8> {
    let msg = wire::new_credential(&wire::CredentialInput {
        security_level,
        signatures,
        pub_keys,
    });
    write_envelope_prefix(tk, ShapeKind::Credential, msg)
}

pub fn new_mint_operation(
    tk: TypeKind,
    sig_indices: &[u32],
    mint_output: &[u8],
    transfer_output: &[u8],
) -> Vec<u8> {
    let msg = wire::new_mint_operation(&wire::MintOperationInput {
        sig_indices,
        mint_output,
        transfer_output,
    });
    write_envelope_prefix(tk, ShapeKind::MintOperation, msg)
}

pub fn new_burn_operation(tk: TypeKind, sig_indices: &[u32]) -> Vec<u8> {
    let msg = wire::new_burn_operation(&wire::BurnOperationInput { sig_indices });
    write_envelope_prefix(tk, ShapeKind::BurnOperation, msg)
}

pub fn new_nft_mint_output(
    tk: TypeKind,
    group: u32,
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let a = packed(addrs);
    let msg = wire::new_nft_mint_output(&wire::NftMintOutputInput {
        group,
        locktime,
        threshold,
        addresses: &a,
    });
    write_envelope_prefix(tk, ShapeKind::NftMintOutput, msg)
}

pub fn new_nft_transfer_output(
    tk: TypeKind,
    group: u32,
    payload: &[u8],
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let a = packed(addrs);
    let msg = wire::new_nft_transfer_output(&wire::NftTransferOutputInput {
        group,
        locktime,
        threshold,
        addresses: &a,
        payload,
    });
    write_envelope_prefix(tk, ShapeKind::NftTransferOutput, msg)
}

pub fn new_nft_mint_operation(
    tk: TypeKind,
    sig_indices: &[u32],
    group: u32,
    payload: &[u8],
    owners: &[Vec<u8>],
) -> Vec<u8> {
    let mut blob = Vec::new();
    for o in owners {
        blob.extend_from_slice(o);
    }
    let msg = wire::new_nft_mint_operation(&wire::NftMintOperationInput {
        sig_indices,
        group,
        payload,
        owners_count: owners.len() as u32,
        owners_bytes: &blob,
        ..Default::default()
    });
    write_envelope_prefix(tk, ShapeKind::NftMintOperation, msg)
}

pub fn new_nft_transfer_operation(tk: TypeKind, sig_indices: &[u32], output: &[u8]) -> Vec<u8> {
    let msg = wire::new_nft_transfer_operation(&wire::NftTransferOperationInput {
        sig_indices,
        output,
    });
    write_envelope_prefix(tk, ShapeKind::NftTransferOp, msg)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn addr(n: u8) -> ShortId {
        ShortId::prefixed_bytes(&[n])
    }

    #[test]
    fn a_transfer_output_round_trips_every_field() {
        let raw = new_transfer_output(TypeKind::Secp256k1, 12345, 7, 2, &[addr(1), addr(2)]);
        let (tk, v) = wrap_transfer_output(&raw).unwrap();
        assert_eq!(tk, TypeKind::Secp256k1);
        assert_eq!(v.amount(), 12345);
        assert_eq!(v.locktime(), 7);
        assert_eq!(v.threshold(), 2);
        assert_eq!(addrs(v.addresses()), vec![addr(1), addr(2)]);
    }

    #[test]
    fn a_transfer_input_round_trips_its_signature_indices() {
        let raw = new_transfer_input(TypeKind::Secp256k1, 54321, &[0, 2, 5]);
        let (_, v) = wrap_transfer_input(&raw).unwrap();
        assert_eq!(v.amount(), 54321);
        assert_eq!(indices(v.sig_indices()), vec![0, 2, 5]);
    }

    #[test]
    fn reading_a_shape_as_another_shape_is_refused_not_answered_with_garbage() {
        let out = new_transfer_output(TypeKind::Secp256k1, 1, 0, 1, &[addr(1)]);
        assert_eq!(
            wrap_transfer_input(&out).unwrap_err(),
            Error::WrongShapeKind
        );
        let owners = new_output_owners(0, 1, &[addr(1)]);
        assert_eq!(
            wrap_mint_output(&owners).unwrap_err(),
            Error::WrongShapeKind
        );
    }

    #[test]
    fn an_fx_owned_shape_refuses_the_reserved_family_byte() {
        let mut raw = new_transfer_output(TypeKind::Secp256k1, 1, 0, 1, &[addr(1)]);
        raw[0] = TypeKind::Reserved.as_u8();
        assert_eq!(
            wrap_transfer_output(&raw).unwrap_err(),
            Error::WrongTypeKind
        );
    }

    #[test]
    fn owners_are_not_fx_owned_so_the_reserved_byte_is_the_right_one() {
        let raw = new_output_owners(0, 1, &[addr(1)]);
        assert_eq!(raw[0], TypeKind::Reserved.as_u8());
        assert!(wrap_output_owners(&raw).is_ok());
    }

    /// The gate, on each way a quorum can be unreachable.
    fn refusal(locktime: u64, threshold: u32, set: &[ShortId]) -> Error {
        let raw = new_output_owners(locktime, threshold, set);
        let v = wrap_output_owners(&raw).unwrap();
        verify_owners(v.threshold(), v.addresses()).unwrap_err()
    }

    #[test]
    fn the_owner_gate_refuses_every_unsatisfiable_quorum() {
        // Empty set.
        assert_eq!(refusal(0, 1, &[]), ERR_OWNER_ADDRS_EMPTY);
        // Threshold zero: nobody has to sign.
        assert_eq!(refusal(0, 0, &[addr(1)]), ERR_OWNER_THRESHOLD_ZERO);
        // Threshold past the set: nobody can.
        assert_eq!(refusal(0, 2, &[addr(1)]), ERR_OWNER_THRESHOLD_EXCEEDS);
        // A zero address is a phantom signer.
        assert_eq!(refusal(0, 1, &[ShortId::default()]), ERR_OWNER_ADDR_ZERO);
        // And the good case passes.
        let raw = new_output_owners(0, 1, &[addr(1)]);
        let v = wrap_output_owners(&raw).unwrap();
        assert!(verify_owners(v.threshold(), v.addresses()).is_ok());
    }

    #[test]
    fn a_credential_run_that_does_not_divide_holds_no_signatures() {
        let raw = new_credential(TypeKind::Secp256k1, 0, &[0u8; 100], &[]);
        let (_, c) = wrap_credential(&raw).unwrap();
        // The COUNT is zero: 100 bytes is not a whole number of 65-byte
        // signatures, so the run says nothing about how many it holds.
        assert_eq!(signature_count(&c, 65), 0);
        // Indexing is a separate question and answers by span alone — the
        // first 65 bytes are there, so they are handed back. Nothing reads a
        // signature it did not first count, which is why the two are allowed
        // to disagree; this pins that they do, so a reader that skipped the
        // count would be visibly wrong rather than quietly.
        assert_eq!(signature_at(&c, 0, 65).unwrap(), vec![0u8; 65]);
        assert_eq!(signature_at(&c, 1, 65), None);

        let raw = new_credential(TypeKind::Secp256k1, 0, &[7u8; 130], &[]);
        let (_, c) = wrap_credential(&raw).unwrap();
        assert_eq!(signature_count(&c, 65), 2);
        assert_eq!(signature_at(&c, 1, 65).unwrap(), vec![7u8; 65]);
        assert_eq!(signature_at(&c, 2, 65), None);
    }

    #[test]
    fn the_nft_shapes_round_trip_group_and_payload() {
        let raw = new_nft_transfer_output(TypeKind::Nft, 3, &[0xde, 0xad], 0, 1, &[addr(1)]);
        let (_, v) = wrap_nft_transfer_output(&raw).unwrap();
        assert_eq!(v.group(), 3);
        assert_eq!(v.payload(), &[0xde, 0xad]);
        assert_eq!(addrs(v.addresses()), vec![addr(1)]);

        let owners = vec![new_output_owners(0, 1, &[addr(1)])];
        let raw = new_nft_mint_operation(TypeKind::Nft, &[0], 3, &[1, 2, 3], &owners);
        let (_, v) = wrap_nft_mint_operation(&raw).unwrap();
        assert_eq!(indices(v.sig_indices()), vec![0]);
        assert_eq!(v.group(), 3);
        assert_eq!(v.payload(), &[1, 2, 3]);
        assert_eq!(v.owners_count(), 1);
        let (env, rest) = super::super::next_envelope(v.owners_bytes()).unwrap();
        assert!(rest.is_empty());
        assert_eq!(wrap_output_owners(env).unwrap().threshold(), 1);
    }

    #[test]
    fn a_mint_operation_carries_both_nested_outputs() {
        let mint = new_mint_output(TypeKind::Secp256k1, 0, 1, &[addr(1)]);
        let xfer = new_transfer_output(TypeKind::Secp256k1, 5, 0, 1, &[addr(1)]);
        let raw = new_mint_operation(TypeKind::Secp256k1, &[0], &mint, &xfer);
        let (_, v) = wrap_mint_operation(&raw).unwrap();
        assert_eq!(indices(v.sig_indices()), vec![0]);
        assert_eq!(v.mint_output(), &mint[..]);
        assert_eq!(v.transfer_output(), &xfer[..]);
    }

    #[test]
    fn a_burn_operation_is_signature_indices_and_nothing_else() {
        let raw = new_burn_operation(TypeKind::Property, &[0, 1]);
        let (tk, v) = wrap_burn_operation(&raw).unwrap();
        assert_eq!(tk, TypeKind::Property);
        assert_eq!(indices(v.sig_indices()), vec![0, 1]);
    }
}
