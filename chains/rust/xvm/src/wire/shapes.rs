// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The fx primitive shapes.
//!
//! Each is a fixed section of scalars plus, where the schema says so, a
//! variable tail: an address list, a signature run, a nested envelope. The
//! offsets here are the format — they are not free to move, in either language.
//!
//! The readers are permissive by design: ZAP answers a zero for a field that is
//! not there. The gates that decide whether a set of values means anything —
//! a threshold of zero, an empty address list, a phantom zero address — live in
//! the `syntactic_verify` methods, and every consumer that treats a threshold as
//! a quorum has to call one before believing it.

use super::{open, write_envelope_prefix, Error, ShapeKind, TypeKind};
use crate::ids::{ShortId, SHORT_ID_LEN};
use crate::zap::{self, Builder, Object};

/// One address in an address list.
pub const ADDRESS_STRIDE: u32 = SHORT_ID_LEN as u32;
/// One signature index.
pub const SIG_INDEX_STRIDE: u32 = 4;

// ------------------------------------------------------------ OutputOwners --
//
//  Locktime    u64  @ 0
//  Threshold   u32  @ 8
//  AddressList list @ 12
pub const OFF_OWNERS_LOCKTIME: usize = 0;
pub const OFF_OWNERS_THRESHOLD: usize = 8;
pub const OFF_OWNERS_ADDRESSES: usize = 12;
pub const SIZE_OUTPUT_OWNERS: usize = 20;

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

/// A view on an address list.
#[derive(Clone, Copy, Debug)]
pub struct AddressList<'a> {
    list: zap::List<'a>,
}

impl<'a> AddressList<'a> {
    pub fn len(&self) -> usize {
        self.list.len()
    }

    pub fn is_empty(&self) -> bool {
        self.list.len() == 0
    }

    /// Address `i`, or the zero address when out of range.
    ///
    /// The zero address IS a phantom signer, which is why a length-padded list
    /// has to be refused by [`verify_owners`] before anyone iterates one.
    pub fn at(&self, i: usize) -> ShortId {
        if i >= self.list.len() {
            return ShortId::default();
        }
        let obj = self.list.object(i, SHORT_ID_LEN);
        let mut out = [0u8; SHORT_ID_LEN];
        for (j, slot) in out.iter_mut().enumerate() {
            *slot = obj.u8(j);
        }
        ShortId(out)
    }

    pub fn all(&self) -> Vec<ShortId> {
        (0..self.len()).map(|i| self.at(i)).collect()
    }
}

/// The gate every owner-bearing shape shares: a non-empty set, a threshold
/// between one and the size of the set, and no zero address in it.
pub fn verify_owners(threshold: u32, addrs: &AddressList<'_>) -> Result<(), Error> {
    let n = addrs.len();
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
        if addrs.at(i).is_empty() {
            return Err(ERR_OWNER_ADDR_ZERO);
        }
    }
    Ok(())
}

fn write_address_list(b: &mut Builder, addrs: &[ShortId]) -> (usize, usize) {
    if addrs.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for a in addrs {
        lb.add_bytes(b, &a.0);
    }
    let (off, _) = lb.finish();
    (off, addrs.len())
}

fn write_sig_indices(b: &mut Builder, sigs: &[u32]) -> (usize, usize) {
    if sigs.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for s in sigs {
        lb.add_u32(b, *s);
    }
    let (off, _) = lb.finish();
    (off, sigs.len())
}

fn write_byte_list(b: &mut Builder, data: &[u8]) -> (usize, usize) {
    if data.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for v in data {
        lb.add_u8(b, *v);
    }
    let (off, _) = lb.finish();
    (off, data.len())
}

fn read_sig_indices(obj: &Object<'_>, field: usize) -> Vec<u32> {
    let l = obj.list_stride(field, SIG_INDEX_STRIDE);
    (0..l.len()).map(|i| l.u32(i)).collect()
}

/// A parsed OutputOwners. Owners are not fx-owned: the same payload is shared
/// by every fx with a multi-address owner group, so the family byte is
/// reserved.
#[derive(Clone, Copy, Debug)]
pub struct OutputOwners<'a> {
    obj: Object<'a>,
}

impl<'a> OutputOwners<'a> {
    pub fn locktime(&self) -> u64 {
        self.obj.u64(OFF_OWNERS_LOCKTIME)
    }
    pub fn threshold(&self) -> u32 {
        self.obj.u32(OFF_OWNERS_THRESHOLD)
    }
    pub fn addresses(&self) -> AddressList<'a> {
        AddressList {
            list: self.obj.list_stride(OFF_OWNERS_ADDRESSES, ADDRESS_STRIDE),
        }
    }
    pub fn syntactic_verify(&self) -> Result<(), Error> {
        verify_owners(self.threshold(), &self.addresses())
    }
}

pub fn wrap_output_owners(b: &[u8]) -> Result<OutputOwners<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::OutputOwners, false)?;
    Ok(OutputOwners { obj: msg.root() })
}

pub fn new_output_owners(locktime: u64, threshold: u32, addrs: &[ShortId]) -> Vec<u8> {
    let mut b = Builder::default();
    let (addr_off, addr_n) = write_address_list(&mut b, addrs);
    let ob = b.start_object(SIZE_OUTPUT_OWNERS);
    ob.set_u64(&mut b, OFF_OWNERS_LOCKTIME, locktime);
    ob.set_u32(&mut b, OFF_OWNERS_THRESHOLD, threshold);
    ob.set_list(&mut b, OFF_OWNERS_ADDRESSES, addr_off, addr_n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::OutputOwners, b.finish())
}

// ----------------------------------------------------------- TransferOutput --
//
//  Amount      u64  @ 0
//  Locktime    u64  @ 8
//  Threshold   u32  @ 16
//  AddressList list @ 20
pub const OFF_TRANSFER_OUT_AMOUNT: usize = 0;
pub const OFF_TRANSFER_OUT_LOCKTIME: usize = 8;
pub const OFF_TRANSFER_OUT_THRESHOLD: usize = 16;
pub const OFF_TRANSFER_OUT_ADDRESSES: usize = 20;
pub const SIZE_TRANSFER_OUTPUT: usize = 28;

/// A parsed TransferOutput. Every fx family — classical and post-quantum —
/// shares this shape; they differ only in how big a signature is.
#[derive(Clone, Copy, Debug)]
pub struct TransferOutput<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> TransferOutput<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn amount(&self) -> u64 {
        self.obj.u64(OFF_TRANSFER_OUT_AMOUNT)
    }
    pub fn locktime(&self) -> u64 {
        self.obj.u64(OFF_TRANSFER_OUT_LOCKTIME)
    }
    pub fn threshold(&self) -> u32 {
        self.obj.u32(OFF_TRANSFER_OUT_THRESHOLD)
    }
    pub fn addresses(&self) -> AddressList<'a> {
        AddressList {
            list: self
                .obj
                .list_stride(OFF_TRANSFER_OUT_ADDRESSES, ADDRESS_STRIDE),
        }
    }
    pub fn syntactic_verify(&self) -> Result<(), Error> {
        verify_owners(self.threshold(), &self.addresses())
    }
}

pub fn wrap_transfer_output(b: &[u8]) -> Result<TransferOutput<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::TransferOutput, true)?;
    Ok(TransferOutput {
        tk,
        obj: msg.root(),
    })
}

pub fn new_transfer_output(
    tk: TypeKind,
    amount: u64,
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let mut b = Builder::default();
    let (addr_off, addr_n) = write_address_list(&mut b, addrs);
    let ob = b.start_object(SIZE_TRANSFER_OUTPUT);
    ob.set_u64(&mut b, OFF_TRANSFER_OUT_AMOUNT, amount);
    ob.set_u64(&mut b, OFF_TRANSFER_OUT_LOCKTIME, locktime);
    ob.set_u32(&mut b, OFF_TRANSFER_OUT_THRESHOLD, threshold);
    ob.set_list(&mut b, OFF_TRANSFER_OUT_ADDRESSES, addr_off, addr_n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::TransferOutput, b.finish())
}

// ------------------------------------------------------------ TransferInput --
//
//  Amount         u64  @ 0
//  SigIndicesList list @ 8
pub const OFF_TRANSFER_IN_AMOUNT: usize = 0;
pub const OFF_TRANSFER_IN_SIG_INDICES: usize = 8;
pub const SIZE_TRANSFER_INPUT: usize = 16;

#[derive(Clone, Copy, Debug)]
pub struct TransferInput<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> TransferInput<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn amount(&self) -> u64 {
        self.obj.u64(OFF_TRANSFER_IN_AMOUNT)
    }
    pub fn sig_indices(&self) -> Vec<u32> {
        read_sig_indices(&self.obj, OFF_TRANSFER_IN_SIG_INDICES)
    }
}

pub fn wrap_transfer_input(b: &[u8]) -> Result<TransferInput<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::TransferInput, true)?;
    Ok(TransferInput {
        tk,
        obj: msg.root(),
    })
}

pub fn new_transfer_input(tk: TypeKind, amount: u64, sig_indices: &[u32]) -> Vec<u8> {
    let mut b = Builder::default();
    let (off, n) = write_sig_indices(&mut b, sig_indices);
    let ob = b.start_object(SIZE_TRANSFER_INPUT);
    ob.set_u64(&mut b, OFF_TRANSFER_IN_AMOUNT, amount);
    ob.set_list(&mut b, OFF_TRANSFER_IN_SIG_INDICES, off, n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::TransferInput, b.finish())
}

// --------------------------------------------------------------- MintOutput --
//
// The same payload as OutputOwners. Only the shape byte separates "may mint"
// from "holds value".
pub const SIZE_MINT_OUTPUT: usize = SIZE_OUTPUT_OWNERS;

#[derive(Clone, Copy, Debug)]
pub struct MintOutput<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> MintOutput<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn locktime(&self) -> u64 {
        self.obj.u64(OFF_OWNERS_LOCKTIME)
    }
    pub fn threshold(&self) -> u32 {
        self.obj.u32(OFF_OWNERS_THRESHOLD)
    }
    pub fn addresses(&self) -> AddressList<'a> {
        AddressList {
            list: self.obj.list_stride(OFF_OWNERS_ADDRESSES, ADDRESS_STRIDE),
        }
    }
    pub fn syntactic_verify(&self) -> Result<(), Error> {
        verify_owners(self.threshold(), &self.addresses())
    }
}

pub fn wrap_mint_output(b: &[u8]) -> Result<MintOutput<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::MintOutput, true)?;
    Ok(MintOutput {
        tk,
        obj: msg.root(),
    })
}

pub fn new_mint_output(tk: TypeKind, locktime: u64, threshold: u32, addrs: &[ShortId]) -> Vec<u8> {
    let mut b = Builder::default();
    let (addr_off, addr_n) = write_address_list(&mut b, addrs);
    let ob = b.start_object(SIZE_MINT_OUTPUT);
    ob.set_u64(&mut b, OFF_OWNERS_LOCKTIME, locktime);
    ob.set_u32(&mut b, OFF_OWNERS_THRESHOLD, threshold);
    ob.set_list(&mut b, OFF_OWNERS_ADDRESSES, addr_off, addr_n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::MintOutput, b.finish())
}

// -------------------------------------------------------------- OwnedOutput --
//
// A property fx state output. Bare owners again; a third shape byte.
pub const SIZE_OWNED_OUTPUT: usize = SIZE_OUTPUT_OWNERS;

#[derive(Clone, Copy, Debug)]
pub struct OwnedOutput<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> OwnedOutput<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn locktime(&self) -> u64 {
        self.obj.u64(OFF_OWNERS_LOCKTIME)
    }
    pub fn threshold(&self) -> u32 {
        self.obj.u32(OFF_OWNERS_THRESHOLD)
    }
    pub fn addresses(&self) -> AddressList<'a> {
        AddressList {
            list: self.obj.list_stride(OFF_OWNERS_ADDRESSES, ADDRESS_STRIDE),
        }
    }
}

pub fn wrap_owned_output(b: &[u8]) -> Result<OwnedOutput<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::OwnedOutput, true)?;
    Ok(OwnedOutput {
        tk,
        obj: msg.root(),
    })
}

pub fn new_owned_output(tk: TypeKind, locktime: u64, threshold: u32, addrs: &[ShortId]) -> Vec<u8> {
    let mut b = Builder::default();
    let (addr_off, addr_n) = write_address_list(&mut b, addrs);
    let ob = b.start_object(SIZE_OWNED_OUTPUT);
    ob.set_u64(&mut b, OFF_OWNERS_LOCKTIME, locktime);
    ob.set_u32(&mut b, OFF_OWNERS_THRESHOLD, threshold);
    ob.set_list(&mut b, OFF_OWNERS_ADDRESSES, addr_off, addr_n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::OwnedOutput, b.finish())
}

// --------------------------------------------------------------- Credential --
//
//  SecurityLevel u8   @ 0   (0 for the classical families)
//  _pad          3B   @ 1
//  SignatureList list @ 4   (a byte run — length counts BYTES)
//  PubKeyList    list @ 12
pub const OFF_CRED_SECURITY_LEVEL: usize = 0;
pub const OFF_CRED_SIGNATURES: usize = 4;
pub const OFF_CRED_PUBKEYS: usize = 12;
pub const SIZE_CREDENTIAL: usize = 20;

#[derive(Clone, Copy, Debug)]
pub struct Credential<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> Credential<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn security_level(&self) -> u8 {
        self.obj.u8(OFF_CRED_SECURITY_LEVEL)
    }

    /// The signature run, as one blob. Divide by the family's signature width
    /// to get a count.
    pub fn signature_bytes(&self) -> Vec<u8> {
        let l = self.obj.list_stride(OFF_CRED_SIGNATURES, 1);
        (0..l.len()).map(|i| l.u8(i)).collect()
    }

    /// How many signatures of `sig_size` bytes the run holds — zero when it
    /// does not divide, because a partial signature is not a signature.
    pub fn signature_count(&self, sig_size: usize) -> usize {
        if sig_size == 0 {
            return 0;
        }
        let total = self.obj.list_stride(OFF_CRED_SIGNATURES, 1).len();
        if !total.is_multiple_of(sig_size) {
            return 0;
        }
        total / sig_size
    }

    pub fn signature_at(&self, i: usize, sig_size: usize) -> Option<Vec<u8>> {
        if sig_size == 0 {
            return None;
        }
        let all = self.signature_bytes();
        let start = i.checked_mul(sig_size)?;
        let end = start.checked_add(sig_size)?;
        if end > all.len() {
            return None;
        }
        Some(all[start..end].to_vec())
    }

    /// The public keys, for the families that put them on the wire. The
    /// classical and post-quantum ones leave this empty.
    pub fn pubkey_bytes(&self) -> Vec<u8> {
        let l = self.obj.list_stride(OFF_CRED_PUBKEYS, 1);
        (0..l.len()).map(|i| l.u8(i)).collect()
    }
}

pub fn wrap_credential(b: &[u8]) -> Result<Credential<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::Credential, true)?;
    Ok(Credential {
        tk,
        obj: msg.root(),
    })
}

pub fn new_credential(
    tk: TypeKind,
    security_level: u8,
    signatures: &[u8],
    pubkeys: &[u8],
) -> Vec<u8> {
    let mut b = Builder::default();
    let (sigs_off, sigs_n) = write_byte_list(&mut b, signatures);
    let (pk_off, pk_n) = write_byte_list(&mut b, pubkeys);
    let ob = b.start_object(SIZE_CREDENTIAL);
    ob.set_u8(&mut b, OFF_CRED_SECURITY_LEVEL, security_level);
    ob.set_list(&mut b, OFF_CRED_SIGNATURES, sigs_off, sigs_n);
    ob.set_list(&mut b, OFF_CRED_PUBKEYS, pk_off, pk_n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::Credential, b.finish())
}

// ------------------------------------------------------------ MintOperation --
//
//  SigIndicesList      list  @ 0
//  MintOutputBytes     bytes @ 8
//  TransferOutputBytes bytes @ 16
pub const OFF_MINT_OP_SIG_INDICES: usize = 0;
pub const OFF_MINT_OP_MINT_OUTPUT: usize = 8;
pub const OFF_MINT_OP_TRANSFER_OUTPUT: usize = 16;
pub const SIZE_MINT_OPERATION: usize = 24;

#[derive(Clone, Copy, Debug)]
pub struct MintOperation<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> MintOperation<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn sig_indices(&self) -> Vec<u32> {
        read_sig_indices(&self.obj, OFF_MINT_OP_SIG_INDICES)
    }
    pub fn mint_output_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_MINT_OP_MINT_OUTPUT)
    }
    pub fn transfer_output_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_MINT_OP_TRANSFER_OUTPUT)
    }
}

pub fn wrap_mint_operation(b: &[u8]) -> Result<MintOperation<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::MintOperation, true)?;
    Ok(MintOperation {
        tk,
        obj: msg.root(),
    })
}

pub fn new_mint_operation(
    tk: TypeKind,
    sig_indices: &[u32],
    mint_output: &[u8],
    transfer_output: &[u8],
) -> Vec<u8> {
    let mut b = Builder::default();
    let (off, n) = write_sig_indices(&mut b, sig_indices);
    let ob = b.start_object(SIZE_MINT_OPERATION);
    ob.set_list(&mut b, OFF_MINT_OP_SIG_INDICES, off, n);
    ob.set_bytes(&mut b, OFF_MINT_OP_MINT_OUTPUT, mint_output);
    ob.set_bytes(&mut b, OFF_MINT_OP_TRANSFER_OUTPUT, transfer_output);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::MintOperation, b.finish())
}

// ------------------------------------------------------------ BurnOperation --
//
//  SigIndicesList list @ 0
pub const OFF_BURN_OP_SIG_INDICES: usize = 0;
pub const SIZE_BURN_OPERATION: usize = 8;

#[derive(Clone, Copy, Debug)]
pub struct BurnOperation<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> BurnOperation<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn sig_indices(&self) -> Vec<u32> {
        read_sig_indices(&self.obj, OFF_BURN_OP_SIG_INDICES)
    }
}

pub fn wrap_burn_operation(b: &[u8]) -> Result<BurnOperation<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::BurnOperation, true)?;
    Ok(BurnOperation {
        tk,
        obj: msg.root(),
    })
}

pub fn new_burn_operation(tk: TypeKind, sig_indices: &[u32]) -> Vec<u8> {
    let mut b = Builder::default();
    let (off, n) = write_sig_indices(&mut b, sig_indices);
    let ob = b.start_object(SIZE_BURN_OPERATION);
    ob.set_list(&mut b, OFF_BURN_OP_SIG_INDICES, off, n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::BurnOperation, b.finish())
}

// ------------------------------------------------------------ NFTMintOutput --
//
//  GroupID     u32  @ 0
//  Locktime    u64  @ 4
//  Threshold   u32  @ 12
//  AddressList list @ 16
pub const OFF_NFT_MINT_OUT_GROUP: usize = 0;
pub const OFF_NFT_MINT_OUT_LOCKTIME: usize = 4;
pub const OFF_NFT_MINT_OUT_THRESHOLD: usize = 12;
pub const OFF_NFT_MINT_OUT_ADDRESSES: usize = 16;
pub const SIZE_NFT_MINT_OUTPUT: usize = 24;

#[derive(Clone, Copy, Debug)]
pub struct NftMintOutput<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> NftMintOutput<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn group_id(&self) -> u32 {
        self.obj.u32(OFF_NFT_MINT_OUT_GROUP)
    }
    pub fn locktime(&self) -> u64 {
        self.obj.u64(OFF_NFT_MINT_OUT_LOCKTIME)
    }
    pub fn threshold(&self) -> u32 {
        self.obj.u32(OFF_NFT_MINT_OUT_THRESHOLD)
    }
    pub fn addresses(&self) -> AddressList<'a> {
        AddressList {
            list: self
                .obj
                .list_stride(OFF_NFT_MINT_OUT_ADDRESSES, ADDRESS_STRIDE),
        }
    }
}

pub fn wrap_nft_mint_output(b: &[u8]) -> Result<NftMintOutput<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::NftMintOutput, true)?;
    Ok(NftMintOutput {
        tk,
        obj: msg.root(),
    })
}

pub fn new_nft_mint_output(
    tk: TypeKind,
    group_id: u32,
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let mut b = Builder::default();
    let (addr_off, addr_n) = write_address_list(&mut b, addrs);
    let ob = b.start_object(SIZE_NFT_MINT_OUTPUT);
    ob.set_u32(&mut b, OFF_NFT_MINT_OUT_GROUP, group_id);
    ob.set_u64(&mut b, OFF_NFT_MINT_OUT_LOCKTIME, locktime);
    ob.set_u32(&mut b, OFF_NFT_MINT_OUT_THRESHOLD, threshold);
    ob.set_list(&mut b, OFF_NFT_MINT_OUT_ADDRESSES, addr_off, addr_n);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::NftMintOutput, b.finish())
}

// -------------------------------------------------------- NFTTransferOutput --
//
//  GroupID     u32   @ 0
//  Locktime    u64   @ 4
//  Threshold   u32   @ 12
//  AddressList list  @ 16
//  Payload     bytes @ 24
pub const OFF_NFT_XFER_OUT_GROUP: usize = 0;
pub const OFF_NFT_XFER_OUT_LOCKTIME: usize = 4;
pub const OFF_NFT_XFER_OUT_THRESHOLD: usize = 12;
pub const OFF_NFT_XFER_OUT_ADDRESSES: usize = 16;
pub const OFF_NFT_XFER_OUT_PAYLOAD: usize = 24;
pub const SIZE_NFT_TRANSFER_OUTPUT: usize = 32;

#[derive(Clone, Copy, Debug)]
pub struct NftTransferOutput<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> NftTransferOutput<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn group_id(&self) -> u32 {
        self.obj.u32(OFF_NFT_XFER_OUT_GROUP)
    }
    pub fn locktime(&self) -> u64 {
        self.obj.u64(OFF_NFT_XFER_OUT_LOCKTIME)
    }
    pub fn threshold(&self) -> u32 {
        self.obj.u32(OFF_NFT_XFER_OUT_THRESHOLD)
    }
    pub fn addresses(&self) -> AddressList<'a> {
        AddressList {
            list: self
                .obj
                .list_stride(OFF_NFT_XFER_OUT_ADDRESSES, ADDRESS_STRIDE),
        }
    }
    pub fn payload(&self) -> &'a [u8] {
        self.obj.bytes(OFF_NFT_XFER_OUT_PAYLOAD)
    }
}

pub fn wrap_nft_transfer_output(b: &[u8]) -> Result<NftTransferOutput<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::NftTransferOutput, true)?;
    Ok(NftTransferOutput {
        tk,
        obj: msg.root(),
    })
}

pub fn new_nft_transfer_output(
    tk: TypeKind,
    group_id: u32,
    payload: &[u8],
    locktime: u64,
    threshold: u32,
    addrs: &[ShortId],
) -> Vec<u8> {
    let mut b = Builder::default();
    let (addr_off, addr_n) = write_address_list(&mut b, addrs);
    let ob = b.start_object(SIZE_NFT_TRANSFER_OUTPUT);
    ob.set_u32(&mut b, OFF_NFT_XFER_OUT_GROUP, group_id);
    ob.set_u64(&mut b, OFF_NFT_XFER_OUT_LOCKTIME, locktime);
    ob.set_u32(&mut b, OFF_NFT_XFER_OUT_THRESHOLD, threshold);
    ob.set_list(&mut b, OFF_NFT_XFER_OUT_ADDRESSES, addr_off, addr_n);
    ob.set_bytes(&mut b, OFF_NFT_XFER_OUT_PAYLOAD, payload);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::NftTransferOutput, b.finish())
}

// --------------------------------------------------------- NFTMintOperation --
//
//  SigIndicesList list  @ 0
//  GroupID        u32   @ 8
//  Payload        bytes @ 12
//  OwnersCount    u32   @ 20
//  OwnersBytes    bytes @ 24   (a packed run of OutputOwners envelopes)
pub const OFF_NFT_MINT_OP_SIG_INDICES: usize = 0;
pub const OFF_NFT_MINT_OP_GROUP: usize = 8;
pub const OFF_NFT_MINT_OP_PAYLOAD: usize = 12;
pub const OFF_NFT_MINT_OP_OWNERS_COUNT: usize = 20;
pub const OFF_NFT_MINT_OP_OWNERS_BYTES: usize = 24;
pub const SIZE_NFT_MINT_OPERATION: usize = 36;

#[derive(Clone, Copy, Debug)]
pub struct NftMintOperation<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> NftMintOperation<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn sig_indices(&self) -> Vec<u32> {
        read_sig_indices(&self.obj, OFF_NFT_MINT_OP_SIG_INDICES)
    }
    pub fn group_id(&self) -> u32 {
        self.obj.u32(OFF_NFT_MINT_OP_GROUP)
    }
    pub fn payload(&self) -> &'a [u8] {
        self.obj.bytes(OFF_NFT_MINT_OP_PAYLOAD)
    }
    pub fn owners_count(&self) -> u32 {
        self.obj.u32(OFF_NFT_MINT_OP_OWNERS_COUNT)
    }
    pub fn owners_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_NFT_MINT_OP_OWNERS_BYTES)
    }
}

pub fn wrap_nft_mint_operation(b: &[u8]) -> Result<NftMintOperation<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::NftMintOperation, true)?;
    Ok(NftMintOperation {
        tk,
        obj: msg.root(),
    })
}

pub fn new_nft_mint_operation(
    tk: TypeKind,
    sig_indices: &[u32],
    group_id: u32,
    payload: &[u8],
    owners: &[Vec<u8>],
) -> Vec<u8> {
    let mut b = Builder::default();
    let (off, n) = write_sig_indices(&mut b, sig_indices);
    let mut owners_blob = Vec::new();
    for o in owners {
        owners_blob.extend_from_slice(o);
    }
    let ob = b.start_object(SIZE_NFT_MINT_OPERATION);
    ob.set_list(&mut b, OFF_NFT_MINT_OP_SIG_INDICES, off, n);
    ob.set_u32(&mut b, OFF_NFT_MINT_OP_GROUP, group_id);
    ob.set_bytes(&mut b, OFF_NFT_MINT_OP_PAYLOAD, payload);
    ob.set_u32(&mut b, OFF_NFT_MINT_OP_OWNERS_COUNT, owners.len() as u32);
    ob.set_bytes(&mut b, OFF_NFT_MINT_OP_OWNERS_BYTES, &owners_blob);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::NftMintOperation, b.finish())
}

// ----------------------------------------------------- NFTTransferOperation --
//
//  SigIndicesList list  @ 0
//  OutputBytes    bytes @ 8
pub const OFF_NFT_XFER_OP_SIG_INDICES: usize = 0;
pub const OFF_NFT_XFER_OP_OUTPUT: usize = 8;
pub const SIZE_NFT_TRANSFER_OP: usize = 16;

#[derive(Clone, Copy, Debug)]
pub struct NftTransferOperation<'a> {
    tk: TypeKind,
    obj: Object<'a>,
}

impl<'a> NftTransferOperation<'a> {
    pub fn type_kind(&self) -> TypeKind {
        self.tk
    }
    pub fn sig_indices(&self) -> Vec<u32> {
        read_sig_indices(&self.obj, OFF_NFT_XFER_OP_SIG_INDICES)
    }
    pub fn output_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_NFT_XFER_OP_OUTPUT)
    }
}

pub fn wrap_nft_transfer_operation(b: &[u8]) -> Result<NftTransferOperation<'_>, Error> {
    let (tk, msg) = open(b, ShapeKind::NftTransferOp, true)?;
    Ok(NftTransferOperation {
        tk,
        obj: msg.root(),
    })
}

pub fn new_nft_transfer_operation(tk: TypeKind, sig_indices: &[u32], output: &[u8]) -> Vec<u8> {
    let mut b = Builder::default();
    let (off, n) = write_sig_indices(&mut b, sig_indices);
    let ob = b.start_object(SIZE_NFT_TRANSFER_OP);
    ob.set_list(&mut b, OFF_NFT_XFER_OP_SIG_INDICES, off, n);
    ob.set_bytes(&mut b, OFF_NFT_XFER_OP_OUTPUT, output);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(tk, ShapeKind::NftTransferOp, b.finish())
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
        let v = wrap_transfer_output(&raw).unwrap();
        assert_eq!(v.type_kind(), TypeKind::Secp256k1);
        assert_eq!(v.amount(), 12345);
        assert_eq!(v.locktime(), 7);
        assert_eq!(v.threshold(), 2);
        assert_eq!(v.addresses().all(), vec![addr(1), addr(2)]);
    }

    #[test]
    fn a_transfer_input_round_trips_its_signature_indices() {
        let raw = new_transfer_input(TypeKind::Secp256k1, 54321, &[0, 2, 5]);
        let v = wrap_transfer_input(&raw).unwrap();
        assert_eq!(v.amount(), 54321);
        assert_eq!(v.sig_indices(), vec![0, 2, 5]);
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

    #[test]
    fn the_owner_gate_refuses_every_unsatisfiable_quorum() {
        // Empty set.
        let raw = new_output_owners(0, 1, &[]);
        assert_eq!(
            wrap_output_owners(&raw)
                .unwrap()
                .syntactic_verify()
                .unwrap_err(),
            ERR_OWNER_ADDRS_EMPTY
        );
        // Threshold zero: nobody has to sign.
        let raw = new_output_owners(0, 0, &[addr(1)]);
        assert_eq!(
            wrap_output_owners(&raw)
                .unwrap()
                .syntactic_verify()
                .unwrap_err(),
            ERR_OWNER_THRESHOLD_ZERO
        );
        // Threshold past the set: nobody can.
        let raw = new_output_owners(0, 2, &[addr(1)]);
        assert_eq!(
            wrap_output_owners(&raw)
                .unwrap()
                .syntactic_verify()
                .unwrap_err(),
            ERR_OWNER_THRESHOLD_EXCEEDS
        );
        // A zero address is a phantom signer.
        let raw = new_output_owners(0, 1, &[ShortId::default()]);
        assert_eq!(
            wrap_output_owners(&raw)
                .unwrap()
                .syntactic_verify()
                .unwrap_err(),
            ERR_OWNER_ADDR_ZERO
        );
        // And the good case passes.
        let raw = new_output_owners(0, 1, &[addr(1)]);
        assert!(wrap_output_owners(&raw).unwrap().syntactic_verify().is_ok());
    }

    #[test]
    fn a_credential_run_that_does_not_divide_holds_no_signatures() {
        let raw = new_credential(TypeKind::Secp256k1, 0, &[0u8; 100], &[]);
        let c = wrap_credential(&raw).unwrap();
        // The COUNT is zero: 100 bytes is not a whole number of 65-byte
        // signatures, so the run says nothing about how many it holds.
        assert_eq!(c.signature_count(65), 0);
        // Indexing is a separate question and answers by span alone — the
        // first 65 bytes are there, so they are handed back. Nothing reads a
        // signature it did not first count, which is why the two are allowed
        // to disagree; this pins that they do, so a reader that skipped the
        // count would be visibly wrong rather than quietly.
        assert_eq!(c.signature_at(0, 65).unwrap(), vec![0u8; 65]);
        assert_eq!(c.signature_at(1, 65), None);

        let raw = new_credential(TypeKind::Secp256k1, 0, &[7u8; 130], &[]);
        let c = wrap_credential(&raw).unwrap();
        assert_eq!(c.signature_count(65), 2);
        assert_eq!(c.signature_at(1, 65).unwrap(), vec![7u8; 65]);
        assert_eq!(c.signature_at(2, 65), None);
    }

    #[test]
    fn the_nft_shapes_round_trip_group_and_payload() {
        let raw = new_nft_transfer_output(TypeKind::Nft, 3, &[0xde, 0xad], 0, 1, &[addr(1)]);
        let v = wrap_nft_transfer_output(&raw).unwrap();
        assert_eq!(v.group_id(), 3);
        assert_eq!(v.payload(), &[0xde, 0xad]);
        assert_eq!(v.addresses().all(), vec![addr(1)]);

        let owners = vec![new_output_owners(0, 1, &[addr(1)])];
        let raw = new_nft_mint_operation(TypeKind::Nft, &[0], 3, &[1, 2, 3], &owners);
        let v = wrap_nft_mint_operation(&raw).unwrap();
        assert_eq!(v.sig_indices(), vec![0]);
        assert_eq!(v.group_id(), 3);
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
        let v = wrap_mint_operation(&raw).unwrap();
        assert_eq!(v.sig_indices(), vec![0]);
        assert_eq!(v.mint_output_bytes(), &mint[..]);
        assert_eq!(v.transfer_output_bytes(), &xfer[..]);
    }

    #[test]
    fn a_burn_operation_is_signature_indices_and_nothing_else() {
        let raw = new_burn_operation(TypeKind::Property, &[0, 1]);
        let v = wrap_burn_operation(&raw).unwrap();
        assert_eq!(v.type_kind(), TypeKind::Property);
        assert_eq!(v.sig_indices(), vec![0, 1]);
    }
}
