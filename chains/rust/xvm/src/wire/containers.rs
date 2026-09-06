// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The containers: what binds an asset to an fx primitive, what a stored UTXO
//! looks like, and the two envelopes a transaction is made of.
//!
//! Each is a struct in `chains/schema/xchain.zap`; its offsets and its
//! accessors come from there. What is here is what a container MEANS.
//!
//! A transferable output is not a bare fx output. The X-Chain settles many
//! assets, so every output NAMES the asset it moves and every input names both
//! the asset and the UTXO it spends. The container itself is fx-agnostic — the
//! family byte travels on the INNER envelope, where the polymorphism actually
//! is — so a container needs no discriminator of its own and can live inline in
//! its parent's buffer, reached by a pointer.

use super::{open, write_envelope_prefix, Error, ShapeKind, TypeKind};
use crate::ids::{self, Id};
use crate::xchain_zap as wire;
use lux_zap::zap;

pub use wire::{Envelope, Signed, TransferableIn, TransferableOut, Utxo};

/// A view on a stored UTXO.
pub fn wrap_utxo(b: &[u8]) -> Result<Utxo<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::Utxo, false)?;
    Ok(Utxo::new(msg.root()))
}

pub fn new_utxo(tx_id: &Id, output_index: u32, asset_id: &Id, output: &[u8]) -> Vec<u8> {
    let msg = wire::new_utxo(&wire::UtxoInput {
        tx_id,
        index: output_index,
        asset: asset_id,
        output,
    });
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::Utxo, msg)
}

/// The multi-asset spending envelope every X-Chain transaction carries.
///
/// Its outputs and inputs are pointer runs into this same buffer, not a
/// concatenation of separately-framed blobs: one buffer, one pass, and each
/// leaf's inner envelope read where it lies.
pub fn wrap_envelope(b: &[u8]) -> Result<Envelope<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::XvmBaseTx, false)?;
    Ok(Envelope::new(msg.root()))
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

pub fn new_envelope(
    network_id: u32,
    blockchain_id: &Id,
    outs: &[OutSpec<'_>],
    ins: &[InSpec<'_>],
    memo: &[u8],
) -> Vec<u8> {
    let out_inputs: Vec<wire::TransferableOutInput<'_>> = outs
        .iter()
        .map(|o| wire::TransferableOutInput {
            asset: &o.asset_id,
            output: o.output,
        })
        .collect();
    let in_inputs: Vec<wire::TransferableInInput<'_>> = ins
        .iter()
        .map(|i| wire::TransferableInInput {
            tx_id: &i.tx_id,
            index: i.output_index,
            asset: &i.asset_id,
            input: i.input,
        })
        .collect();
    let msg = wire::new_envelope(&wire::EnvelopeInput {
        network: network_id,
        chain: blockchain_id,
        outs: &out_inputs,
        ins: &in_inputs,
        memo,
    });
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::XvmBaseTx, msg)
}

/// The out at `i`, or the refusal that it is not there. A pointer that names
/// nothing is a short envelope, not an empty output.
pub fn out_at<'a>(v: &Envelope<'a>, i: usize) -> Result<TransferableOut<'a>, Error> {
    if i >= v.outs().len() {
        return Err(Error::ShortEnvelope);
    }
    let o = v.outs_at(i);
    if o.object().is_null() {
        return Err(Error::ShortEnvelope);
    }
    Ok(o)
}

/// The in at `i`, under the same rule.
pub fn in_at<'a>(v: &Envelope<'a>, i: usize) -> Result<TransferableIn<'a>, Error> {
    if i >= v.ins().len() {
        return Err(Error::ShortEnvelope);
    }
    let o = v.ins_at(i);
    if o.object().is_null() {
        return Err(Error::ShortEnvelope);
    }
    Ok(o)
}

/// The outer envelope: the bytes that were signed, and the signatures over
/// them. Credential `i` answers for input `i`.
pub fn wrap_signed(b: &[u8]) -> Result<Signed<'_>, Error> {
    let (_, msg) = open(b, ShapeKind::SignedTx, false)?;
    Ok(Signed::new(msg.root()))
}

pub fn new_signed(unsigned: &[u8], credentials: &[Vec<u8>]) -> Vec<u8> {
    let total: usize = credentials.iter().map(|c| c.len()).sum();
    let mut blob = Vec::with_capacity(total);
    for c in credentials {
        blob.extend_from_slice(c);
    }
    let msg = wire::new_signed(&wire::SignedInput {
        unsigned,
        credential_count: credentials.len() as u32,
        credential_bytes: &blob,
    });
    write_envelope_prefix(TypeKind::Reserved, ShapeKind::SignedTx, msg)
}

/// Every credential envelope on a signed transaction, in order.
pub fn credential_envelopes<'a>(v: &Signed<'a>) -> Result<Vec<&'a [u8]>, Error> {
    let n = v.credential_count() as usize;
    let mut blob = v.credential_bytes();
    let mut out = Vec::with_capacity(n.min(1024));
    for _ in 0..n {
        let (env, rest) = super::next_envelope(blob)?;
        out.push(env);
        blob = rest;
    }
    Ok(out)
}

// ------------------------------------------------------- packed blob lists --

/// Pack self-contained buffers as a u32 length list plus one concatenated run.
///
/// The caller writes the list into the buffer and holds the run to set as a
/// bytes field — that keeps the list before the object that points at it.
pub fn write_blob_list(b: &mut zap::Builder, bufs: &[Vec<u8>]) -> (usize, usize, Vec<u8>) {
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
pub fn read_blob_list<'a>(lengths: zap::List<'a>, blob: &'a [u8]) -> Result<Vec<&'a [u8]>, Error> {
    let n = lengths.len();
    if n == 0 {
        return Ok(Vec::new());
    }
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

/// A UTXO reference in a fixed-stride list: 32-byte id, u32 index.
pub fn write_utxo_ids(b: &mut zap::Builder, ids: &[(Id, u32)]) -> (usize, usize) {
    if ids.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for (tx_id, index) in ids {
        let r = wire::pack_utxo_id(&wire::UtxoIdInput {
            tx_id,
            index: *index,
        });
        lb.add_bytes(b, &r);
    }
    let (off, _) = lb.finish();
    (off, ids.len())
}

pub fn read_utxo_ids(list: zap::List<'_>) -> Vec<(Id, u32)> {
    (0..list.len())
        .map(|i| {
            let e = wire::UtxoId::new(list.object(i, wire::UTXO_ID_SIZE));
            (ids::prefixed(e.tx_id()), e.index())
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
        ids::prefixed(&[n])
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
        let raw = new_envelope(
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

        let v = wrap_envelope(&raw).unwrap();
        assert_eq!(v.network(), 10);
        assert_eq!(ids::prefixed(v.chain()), asset(5));
        assert_eq!(v.memo(), &[0xAA, 0xBB]);
        assert_eq!(v.outs().len(), 1);
        let o = out_at(&v, 0).unwrap();
        assert_eq!(ids::prefixed(o.asset()), asset(1));
        assert_eq!(o.output(), &out_inner[..]);
        assert_eq!(v.ins().len(), 1);
        let i = in_at(&v, 0).unwrap();
        assert_eq!(ids::prefixed(i.tx_id()), asset(2));
        assert_eq!(i.index(), 3);
        assert_eq!(ids::prefixed(i.asset()), asset(1));
        assert_eq!(i.input(), &in_inner[..]);
        // Past the end is a refusal, not a zero object.
        assert!(out_at(&v, 1).is_err());
        assert!(in_at(&v, 1).is_err());
    }

    #[test]
    fn a_signed_tx_hands_back_the_signing_target_and_every_credential() {
        let c0 = shapes::new_credential(TypeKind::Secp256k1, 0, &[1u8; 65], &[]);
        let c1 = shapes::new_credential(TypeKind::Nft, 0, &[2u8; 65], &[]);
        let raw = new_signed(&[9, 9, 9], &[c0.clone(), c1.clone()]);
        let v = wrap_signed(&raw).unwrap();
        assert_eq!(v.unsigned(), &[9, 9, 9]);
        assert_eq!(v.credential_count(), 2);
        assert_eq!(credential_envelopes(&v).unwrap(), vec![&c0[..], &c1[..]]);
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
        assert_eq!(ids::prefixed(v.tx_id()), asset(3));
        assert_eq!(v.index(), 2);
        assert_eq!(ids::prefixed(v.asset()), asset(4));
        assert_eq!(v.output(), &out[..]);
    }

    #[test]
    fn a_packed_blob_list_slices_back_to_exactly_what_went_in() {
        let items = vec![vec![1u8, 2, 3], vec![], vec![9u8; 40]];
        let mut b = zap::Builder::new_v2(256);
        let (off, n, blob) = write_blob_list(&mut b, &items);
        let mut ob = b.start_object(16);
        ob.set_list(&mut b, 0, off, n);
        ob.set_bytes(&mut b, 8, &blob);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = zap::Message::parse(&raw).unwrap().root();
        let got = read_blob_list(root.list_stride(0, 4), root.bytes(8)).unwrap();
        assert_eq!(got.len(), 3);
        assert_eq!(got[0], &[1, 2, 3]);
        assert_eq!(got[1], &[] as &[u8]);
        assert_eq!(got[2], &[9u8; 40][..]);
    }

    #[test]
    fn a_utxo_id_list_round_trips_at_its_fixed_stride() {
        let ids_in = vec![(asset(1), 0u32), (asset(2), 7), (asset(3), u32::MAX)];
        let mut b = zap::Builder::new_v2(256);
        let (off, n) = write_utxo_ids(&mut b, &ids_in);
        let mut ob = b.start_object(8);
        ob.set_list(&mut b, 0, off, n);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = zap::Message::parse(&raw).unwrap().root();
        assert_eq!(
            read_utxo_ids(root.list_stride(0, wire::UTXO_ID_SIZE)),
            ids_in
        );
    }

    #[test]
    fn a_trailing_byte_is_a_second_id_for_one_transaction_so_it_is_refused() {
        let raw = new_signed(&[1], &[]);
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
