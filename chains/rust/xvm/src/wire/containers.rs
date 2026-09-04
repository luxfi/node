// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The containers: what binds an asset to an fx primitive, what a stored UTXO
//! looks like, and the two envelopes a transaction is made of.
//!
//! A transferable output is not a bare fx output. The X-Chain settles many
//! assets, so every output NAMES the asset it moves and every input names both
//! the asset and the UTXO it spends. The container itself is fx-agnostic — the
//! family byte travels on the INNER envelope, where the polymorphism actually
//! is — so a container needs no discriminator of its own and can live inline in
//! its parent's buffer, reached by a pointer.

use super::{open, write_envelope_prefix, Error, ShapeKind, TypeKind};
use crate::ids::Id;
use crate::zap::{self, Builder, Object};

/// One 4-byte pointer slot in an out-of-line object list.
pub const OBJ_PTR_STRIDE: u32 = 4;

// ------------------------------------------------------- TransferableOutput --
//
//  AssetID 32B   @ 0
//  Output  bytes @ 32   (the inner fx Output envelope)
pub const OFF_TRANSFERABLE_OUT_ASSET: usize = 0;
pub const OFF_TRANSFERABLE_OUT_OUTPUT: usize = 32;
pub const SIZE_TRANSFERABLE_OUT: usize = 40;

/// A view on a transferable output nested in a parent buffer.
#[derive(Clone, Copy, Debug)]
pub struct TransferableOut<'a> {
    obj: Object<'a>,
}

impl<'a> TransferableOut<'a> {
    pub fn from_object(obj: Object<'a>) -> Self {
        TransferableOut { obj }
    }

    pub fn asset_id(&self) -> Id {
        Id::prefixed_bytes(self.obj.bytes_fixed(OFF_TRANSFERABLE_OUT_ASSET, 32))
    }

    /// The inner fx Output envelope. Dispatch on its own discriminator.
    pub fn output_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_TRANSFERABLE_OUT_OUTPUT)
    }

    pub fn is_null(&self) -> bool {
        self.obj.is_null()
    }
}

/// Write a transferable output into an open buffer; the offset is what a
/// pointer slot names.
pub fn append_transferable_out(b: &mut Builder, asset_id: &Id, inner: &[u8]) -> usize {
    let ob = b.start_object(SIZE_TRANSFERABLE_OUT);
    ob.set_bytes_fixed(b, OFF_TRANSFERABLE_OUT_ASSET, &asset_id.0);
    ob.set_bytes(b, OFF_TRANSFERABLE_OUT_OUTPUT, inner);
    ob.offset()
}

// -------------------------------------------------------- TransferableInput --
//
//  TxID        32B    @ 0
//  OutputIndex u32    @ 32
//  AssetID     32B    @ 36
//  Input       bytes  @ 68
pub const OFF_TRANSFERABLE_IN_TXID: usize = 0;
pub const OFF_TRANSFERABLE_IN_INDEX: usize = 32;
pub const OFF_TRANSFERABLE_IN_ASSET: usize = 36;
pub const OFF_TRANSFERABLE_IN_INPUT: usize = 68;
pub const SIZE_TRANSFERABLE_IN: usize = 76;

#[derive(Clone, Copy, Debug)]
pub struct TransferableIn<'a> {
    obj: Object<'a>,
}

impl<'a> TransferableIn<'a> {
    pub fn from_object(obj: Object<'a>) -> Self {
        TransferableIn { obj }
    }

    pub fn tx_id(&self) -> Id {
        Id::prefixed_bytes(self.obj.bytes_fixed(OFF_TRANSFERABLE_IN_TXID, 32))
    }
    pub fn output_index(&self) -> u32 {
        self.obj.u32(OFF_TRANSFERABLE_IN_INDEX)
    }
    pub fn asset_id(&self) -> Id {
        Id::prefixed_bytes(self.obj.bytes_fixed(OFF_TRANSFERABLE_IN_ASSET, 32))
    }
    pub fn input_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_TRANSFERABLE_IN_INPUT)
    }
    pub fn is_null(&self) -> bool {
        self.obj.is_null()
    }
}

pub fn append_transferable_in(
    b: &mut Builder,
    tx_id: &Id,
    output_index: u32,
    asset_id: &Id,
    inner: &[u8],
) -> usize {
    let ob = b.start_object(SIZE_TRANSFERABLE_IN);
    ob.set_bytes_fixed(b, OFF_TRANSFERABLE_IN_TXID, &tx_id.0);
    ob.set_u32(b, OFF_TRANSFERABLE_IN_INDEX, output_index);
    ob.set_bytes_fixed(b, OFF_TRANSFERABLE_IN_ASSET, &asset_id.0);
    ob.set_bytes(b, OFF_TRANSFERABLE_IN_INPUT, inner);
    ob.offset()
}

// --------------------------------------------------------------------- UTXO --
//
//  TxID        32B   @ 0
//  OutputIndex u32   @ 32
//  AssetID     32B   @ 36
//  Output      bytes @ 68
pub const OFF_UTXO_TXID: usize = 0;
pub const OFF_UTXO_INDEX: usize = 32;
pub const OFF_UTXO_ASSET: usize = 36;
pub const OFF_UTXO_OUTPUT: usize = 68;
pub const SIZE_UTXO: usize = 76;

/// A stored UTXO, as it travels between chains and as it sits on disk. The same
/// bytes both ways: a second encoding would be a second thing to agree about.
#[derive(Clone, Copy, Debug)]
pub struct Utxo<'a> {
    obj: Object<'a>,
}

impl<'a> Utxo<'a> {
    pub fn tx_id(&self) -> Id {
        Id::prefixed_bytes(self.obj.bytes_fixed(OFF_UTXO_TXID, 32))
    }
    pub fn output_index(&self) -> u32 {
        self.obj.u32(OFF_UTXO_INDEX)
    }
    pub fn asset_id(&self) -> Id {
        Id::prefixed_bytes(self.obj.bytes_fixed(OFF_UTXO_ASSET, 32))
    }
    pub fn output_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_UTXO_OUTPUT)
    }
}

pub fn wrap_utxo(b: &[u8]) -> Result<Utxo<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::Utxo, false)?;
    Ok(Utxo { obj: msg.root() })
}

pub fn new_utxo(tx_id: &Id, output_index: u32, asset_id: &Id, output: &[u8]) -> Vec<u8> {
    let mut b = Builder::default();
    let ob = b.start_object(SIZE_UTXO);
    ob.set_bytes_fixed(&mut b, OFF_UTXO_TXID, &tx_id.0);
    ob.set_u32(&mut b, OFF_UTXO_INDEX, output_index);
    ob.set_bytes_fixed(&mut b, OFF_UTXO_ASSET, &asset_id.0);
    ob.set_bytes(&mut b, OFF_UTXO_OUTPUT, output);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::Utxo, b.finish())
}

// ---------------------------------------------------------------- XVMBaseTx --
//
//  NetworkID    u32        @ 0
//  BlockchainID 32B        @ 8
//  Outs         objptrlist @ 40
//  Ins          objptrlist @ 48
//  Memo         bytes      @ 56
pub const OFF_BASE_NETWORK: usize = 0;
pub const OFF_BASE_CHAIN: usize = 8;
pub const OFF_BASE_OUTS: usize = 40;
pub const OFF_BASE_INS: usize = 48;
pub const OFF_BASE_MEMO: usize = 56;
pub const SIZE_XVM_BASE_TX: usize = 64;

/// The multi-asset spending envelope every X-Chain transaction carries.
///
/// The outputs and inputs are pointer lists into this same buffer, not a
/// concatenation of separately-framed blobs: one buffer, one pass, and each
/// leaf's inner envelope read where it lies.
#[derive(Clone, Copy, Debug)]
pub struct XvmBaseTx<'a> {
    obj: Object<'a>,
}

impl<'a> XvmBaseTx<'a> {
    pub fn network_id(&self) -> u32 {
        self.obj.u32(OFF_BASE_NETWORK)
    }
    pub fn blockchain_id(&self) -> Id {
        Id::prefixed_bytes(self.obj.bytes_fixed(OFF_BASE_CHAIN, 32))
    }
    pub fn outs_count(&self) -> usize {
        self.obj.list_stride(OFF_BASE_OUTS, OBJ_PTR_STRIDE).len()
    }
    pub fn out_at(&self, i: usize) -> Result<TransferableOut<'a>, Error> {
        let l = self.obj.list_stride(OFF_BASE_OUTS, OBJ_PTR_STRIDE);
        if i >= l.len() {
            return Err(Error::ShortEnvelope);
        }
        let o = l.object_ptr(i);
        if o.is_null() {
            return Err(Error::ShortEnvelope);
        }
        Ok(TransferableOut::from_object(o))
    }
    pub fn ins_count(&self) -> usize {
        self.obj.list_stride(OFF_BASE_INS, OBJ_PTR_STRIDE).len()
    }
    pub fn in_at(&self, i: usize) -> Result<TransferableIn<'a>, Error> {
        let l = self.obj.list_stride(OFF_BASE_INS, OBJ_PTR_STRIDE);
        if i >= l.len() {
            return Err(Error::ShortEnvelope);
        }
        let o = l.object_ptr(i);
        if o.is_null() {
            return Err(Error::ShortEnvelope);
        }
        Ok(TransferableIn::from_object(o))
    }
    pub fn memo(&self) -> &'a [u8] {
        self.obj.bytes(OFF_BASE_MEMO)
    }
}

pub fn wrap_xvm_base_tx(b: &[u8]) -> Result<XvmBaseTx<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::XvmBaseTx, false)?;
    Ok(XvmBaseTx { obj: msg.root() })
}

/// One output going into the envelope: an asset and an already-built inner
/// envelope.
pub struct OutSpec<'a> {
    pub asset_id: Id,
    pub output: &'a [u8],
}

/// One input going into the envelope.
pub struct InSpec<'a> {
    pub tx_id: Id,
    pub output_index: u32,
    pub asset_id: Id,
    pub input: &'a [u8],
}

pub fn new_xvm_base_tx(
    network_id: u32,
    blockchain_id: &Id,
    outs: &[OutSpec<'_>],
    ins: &[InSpec<'_>],
    memo: &[u8],
) -> Vec<u8> {
    let mut b = Builder::default();

    // Tail each leaf first, then the pointer arrays, then the root — so every
    // pointer aims backwards at something already written.
    let out_offs: Vec<usize> = outs
        .iter()
        .map(|o| append_transferable_out(&mut b, &o.asset_id, o.output))
        .collect();
    let in_offs: Vec<usize> = ins
        .iter()
        .map(|i| append_transferable_in(&mut b, &i.tx_id, i.output_index, &i.asset_id, i.input))
        .collect();

    let mut ol = b.start_list();
    for off in &out_offs {
        ol.add_object_ptr(&mut b, *off);
    }
    let (outs_off, outs_len) = ol.finish();
    let mut il = b.start_list();
    for off in &in_offs {
        il.add_object_ptr(&mut b, *off);
    }
    let (ins_off, ins_len) = il.finish();

    let ob = b.start_object(SIZE_XVM_BASE_TX);
    ob.set_u32(&mut b, OFF_BASE_NETWORK, network_id);
    ob.set_bytes_fixed(&mut b, OFF_BASE_CHAIN, &blockchain_id.0);
    ob.set_list(&mut b, OFF_BASE_OUTS, outs_off, outs_len);
    ob.set_list(&mut b, OFF_BASE_INS, ins_off, ins_len);
    ob.set_bytes(&mut b, OFF_BASE_MEMO, memo);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::XvmBaseTx, b.finish())
}

// ----------------------------------------------------------------- SignedTx --
//
//  UnsignedBytes   bytes @ 0
//  CredentialCount u32   @ 8
//  CredentialBytes bytes @ 12   (a packed run of credential envelopes)
pub const OFF_SIGNED_UNSIGNED: usize = 0;
pub const OFF_SIGNED_CRED_COUNT: usize = 8;
pub const OFF_SIGNED_CRED_BYTES: usize = 12;
pub const SIZE_SIGNED_TX: usize = 20;

/// The outer envelope: the bytes that were signed, and the signatures over
/// them. Credential `i` answers for input `i`.
#[derive(Clone, Copy, Debug)]
pub struct SignedTx<'a> {
    obj: Object<'a>,
}

impl<'a> SignedTx<'a> {
    /// The signing target — every fx signature is over a hash of exactly these
    /// bytes.
    pub fn unsigned_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_SIGNED_UNSIGNED)
    }
    pub fn credential_count(&self) -> u32 {
        self.obj.u32(OFF_SIGNED_CRED_COUNT)
    }
    /// The packed credential run. Walk it with `next_envelope`; each
    /// credential's own ZAP header carries its length.
    pub fn credential_bytes(&self) -> &'a [u8] {
        self.obj.bytes(OFF_SIGNED_CRED_BYTES)
    }

    /// Every credential envelope, in order.
    pub fn credential_envelopes(&self) -> Result<Vec<&'a [u8]>, Error> {
        let n = self.credential_count() as usize;
        let mut blob = self.credential_bytes();
        let mut out = Vec::with_capacity(n.min(1024));
        for _ in 0..n {
            let (env, rest) = super::next_envelope(blob)?;
            out.push(env);
            blob = rest;
        }
        Ok(out)
    }
}

pub fn wrap_signed_tx(b: &[u8]) -> Result<SignedTx<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::SignedTx, false)?;
    Ok(SignedTx { obj: msg.root() })
}

pub fn new_signed_tx(unsigned: &[u8], credentials: &[Vec<u8>]) -> Vec<u8> {
    let total: usize = credentials.iter().map(|c| c.len()).sum();
    let mut cred_blob = Vec::with_capacity(total);
    for c in credentials {
        cred_blob.extend_from_slice(c);
    }
    let mut b = Builder::default();
    let ob = b.start_object(SIZE_SIGNED_TX);
    ob.set_bytes(&mut b, OFF_SIGNED_UNSIGNED, unsigned);
    ob.set_u32(&mut b, OFF_SIGNED_CRED_COUNT, credentials.len() as u32);
    ob.set_bytes(&mut b, OFF_SIGNED_CRED_BYTES, &cred_blob);
    ob.finish_as_root(&mut b);
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::SignedTx, b.finish())
}

// ------------------------------------------------------- packed blob lists --

/// One entry in the u32 length list that prefixes a packed blob list.
pub const ITEM_LEN_STRIDE: u32 = 4;

/// Pack self-contained buffers as a u32 length list plus one concatenated run.
///
/// The caller writes the list into the buffer and holds the run to set as a
/// bytes field — that keeps the list before the object that points at it.
pub fn write_blob_list(b: &mut Builder, bufs: &[Vec<u8>]) -> (usize, usize, Vec<u8>) {
    if bufs.is_empty() {
        return (0, 0, Vec::new());
    }
    let mut blob = Vec::new();
    let mut lb = b.start_list();
    for buf in bufs {
        lb.add_u32(b, buf.len() as u32);
        blob.extend_from_slice(buf);
    }
    let (off, count) = lb.finish();
    (off, count, blob)
}

/// Slice a packed run by its length list. Each element aliases the parent.
pub fn read_blob_list<'a>(
    obj: &Object<'a>,
    len_field: usize,
    blob_field: usize,
) -> Result<Vec<&'a [u8]>, Error> {
    let lengths = obj.list_stride(len_field, ITEM_LEN_STRIDE);
    let n = lengths.len();
    if n == 0 {
        return Ok(Vec::new());
    }
    let blob = obj.bytes(blob_field);
    let mut out = Vec::with_capacity(n.min(1024));
    let mut cursor = 0usize;
    for i in 0..n {
        let size = lengths.u32(i) as usize;
        let end = match cursor.checked_add(size) {
            Some(v) => v,
            None => return Err(Error::ShortEnvelope),
        };
        if end > blob.len() {
            return Err(Error::ShortEnvelope);
        }
        out.push(&blob[cursor..end]);
        cursor = end;
    }
    Ok(out)
}

/// A UTXO reference in a fixed 36-byte-stride list: 32-byte id, u32 index.
pub const UTXO_ID_STRIDE: u32 = 36;

pub fn write_utxo_ids(b: &mut Builder, ids: &[(Id, u32)]) -> (usize, usize) {
    if ids.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for (tx_id, index) in ids {
        let mut e = [0u8; 36];
        e[0..32].copy_from_slice(&tx_id.0);
        e[32..36].copy_from_slice(&index.to_le_bytes());
        lb.add_bytes(b, &e);
    }
    let (off, _) = lb.finish();
    (off, ids.len())
}

pub fn read_utxo_ids(obj: &Object<'_>, field: usize) -> Vec<(Id, u32)> {
    let list = obj.list_stride(field, UTXO_ID_STRIDE);
    (0..list.len())
        .map(|i| {
            let e = list.object(i, UTXO_ID_STRIDE as usize);
            (Id::prefixed_bytes(e.bytes_fixed(0, 32)), e.u32(32))
        })
        .collect()
}

/// A message whose bytes are exactly its declared size and nothing more.
///
/// A trailing byte would leave the same meaning with a different hash, which is
/// a second id for one transaction.
pub fn refuse_trailing_bytes(b: &[u8]) -> Result<(), Error> {
    let msg = zap::Message::parse(b)?;
    if msg.size() != b.len() {
        return Err(Error::TrailingBytes);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ids::ShortId;
    use crate::wire::shapes;

    fn asset(n: u8) -> Id {
        Id::prefixed_bytes(&[n])
    }

    #[test]
    fn a_base_tx_envelope_round_trips_its_outputs_and_inputs() {
        let out_inner = shapes::new_transfer_output(
            TypeKind::Secp256k1,
            7,
            0,
            1,
            &[ShortId::prefixed_bytes(&[1])],
        );
        let in_inner = shapes::new_transfer_input(TypeKind::Secp256k1, 9, &[0]);
        let raw = new_xvm_base_tx(
            10,
            &asset(5),
            &[OutSpec {
                asset_id: asset(1),
                output: &out_inner,
            }],
            &[InSpec {
                tx_id: asset(2),
                output_index: 3,
                asset_id: asset(1),
                input: &in_inner,
            }],
            &[0xAA, 0xBB],
        );

        let v = wrap_xvm_base_tx(&raw).unwrap();
        assert_eq!(v.network_id(), 10);
        assert_eq!(v.blockchain_id(), asset(5));
        assert_eq!(v.memo(), &[0xAA, 0xBB]);
        assert_eq!(v.outs_count(), 1);
        let o = v.out_at(0).unwrap();
        assert_eq!(o.asset_id(), asset(1));
        assert_eq!(o.output_bytes(), &out_inner[..]);
        assert_eq!(v.ins_count(), 1);
        let i = v.in_at(0).unwrap();
        assert_eq!(i.tx_id(), asset(2));
        assert_eq!(i.output_index(), 3);
        assert_eq!(i.asset_id(), asset(1));
        assert_eq!(i.input_bytes(), &in_inner[..]);
        // Past the end is a refusal, not a zero object.
        assert!(v.out_at(1).is_err());
        assert!(v.in_at(1).is_err());
    }

    #[test]
    fn a_signed_tx_hands_back_the_signing_target_and_every_credential() {
        let c0 = shapes::new_credential(TypeKind::Secp256k1, 0, &[1u8; 65], &[]);
        let c1 = shapes::new_credential(TypeKind::Nft, 0, &[2u8; 65], &[]);
        let raw = new_signed_tx(&[9, 9, 9], &[c0.clone(), c1.clone()]);
        let v = wrap_signed_tx(&raw).unwrap();
        assert_eq!(v.unsigned_bytes(), &[9, 9, 9]);
        assert_eq!(v.credential_count(), 2);
        assert_eq!(v.credential_envelopes().unwrap(), vec![&c0[..], &c1[..]]);
    }

    #[test]
    fn a_utxo_round_trips_and_its_output_stays_byte_identical() {
        let out = shapes::new_transfer_output(
            TypeKind::Secp256k1,
            7,
            0,
            1,
            &[ShortId::prefixed_bytes(&[1])],
        );
        let raw = new_utxo(&asset(3), 2, &asset(4), &out);
        let v = wrap_utxo(&raw).unwrap();
        assert_eq!(v.tx_id(), asset(3));
        assert_eq!(v.output_index(), 2);
        assert_eq!(v.asset_id(), asset(4));
        assert_eq!(v.output_bytes(), &out[..]);
    }

    #[test]
    fn a_packed_blob_list_slices_back_to_exactly_what_went_in() {
        let items = vec![vec![1u8, 2, 3], vec![], vec![9u8; 40]];
        let mut b = Builder::default();
        let (off, n, blob) = write_blob_list(&mut b, &items);
        let ob = b.start_object(16);
        ob.set_list(&mut b, 0, off, n);
        ob.set_bytes(&mut b, 8, &blob);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = zap::Message::parse(&raw).unwrap().root();
        let got = read_blob_list(&root, 0, 8).unwrap();
        assert_eq!(got.len(), 3);
        assert_eq!(got[0], &[1, 2, 3]);
        assert_eq!(got[1], &[] as &[u8]);
        assert_eq!(got[2], &[9u8; 40][..]);
    }

    #[test]
    fn a_utxo_id_list_round_trips_at_its_fixed_stride() {
        let ids = vec![(asset(1), 0u32), (asset(2), 7), (asset(3), u32::MAX)];
        let mut b = Builder::default();
        let (off, n) = write_utxo_ids(&mut b, &ids);
        let ob = b.start_object(8);
        ob.set_list(&mut b, 0, off, n);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = zap::Message::parse(&raw).unwrap().root();
        assert_eq!(read_utxo_ids(&root, 0), ids);
    }

    #[test]
    fn a_trailing_byte_is_a_second_id_for_one_transaction_so_it_is_refused() {
        let raw = new_signed_tx(&[1], &[]);
        let msg = &raw[super::super::ENVELOPE_PREFIX..];
        assert!(refuse_trailing_bytes(msg).is_ok());
        let mut with_tail = msg.to_vec();
        with_tail.push(0);
        assert_eq!(
            refuse_trailing_bytes(&with_tail).unwrap_err(),
            Error::TrailingBytes
        );
    }
}
