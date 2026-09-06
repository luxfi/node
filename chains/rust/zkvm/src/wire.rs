// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The bytes: struct-is-wire over ZAP, at the offsets the Go chain writes.
//!
//! No codec registry, no reflection, no schema file. Each type owns its
//! encoding over a [`zap`] object; a nested list `[T]` is packed as a u32
//! length vector plus the concatenation of the elements' own frames; an
//! optional pointer is one bytes field that is empty exactly when the value is
//! absent; a `[[u8]]` is a length vector plus a concatenated blob.
//!
//! TWO RULES DECIDE WHAT IS ADMITTED, and both are about bytes nobody claims.
//!
//! [`frame`] refuses a message that does not account for every byte handed in.
//! One value has one byte string; a frame that declares fewer bytes than it was
//! given leaves a remainder that belongs to nobody, and two encodings of one
//! block are two ids for one block.
//!
//! [`unpack_blobs`] and [`unpack_frames`] refuse a length vector that does not
//! exactly partition the blob it indexes — in either direction. A length that
//! reaches past the end is refused rather than clamped, and blob bytes no
//! length claims are refused rather than dropped, because either way the value
//! read is not the value that was sent. The capacity always comes from the
//! length vector, which the frame already bounds, never from a declared length.
//!
//! THE IDENTITY IS NOT HERE. A transaction's id and a block's id are folds over
//! content ([`crate::txs::Tx::id`], [`crate::block::Block::id`]), so there is
//! no field on the wire for a peer to choose one with.

use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::txs::{Kind, Proof, Shielded, TransparentIn, TransparentOut, Tx};
use crate::utxo::Utxo;
use crate::zap;

// ------------------------------------------------------------------ frames --

/// The root object of a ZAP frame, refusing one that does not account for every
/// byte it was handed.
///
/// Every parser in this file goes through it, so canonicality is decided in one
/// place rather than per type.
pub fn frame(data: &[u8]) -> Result<zap::Object<'_>> {
    let m = zap::Message::parse(data)?;
    if m.size() != data.len() {
        return Err(Error::TrailingBytes);
    }
    Ok(m.root())
}

fn write_u32s(b: &mut zap::Builder, xs: &[u32]) -> usize {
    let mut lb = b.start_list();
    for x in xs {
        lb.add_u32(b, *x);
    }
    lb.finish().0
}

fn read_u32s(o: &zap::Object<'_>, field: usize) -> Vec<u32> {
    let l = o.list_stride(field, 4);
    (0..l.len()).map(|i| l.u32(i)).collect()
}

/// Flatten `[[u8]]` into a length vector and one blob.
fn pack_blobs(xs: &[Vec<u8>]) -> (Vec<u32>, Vec<u8>) {
    let mut lens = Vec::with_capacity(xs.len());
    let mut blob = Vec::new();
    for x in xs {
        lens.push(x.len() as u32);
        blob.extend_from_slice(x);
    }
    (lens, blob)
}

/// Re-split a concatenated blob by its declared lengths.
///
/// Both halves come from the peer and both are checked. The `what` and the
/// index travel into the error because a corpus row's note is read by a person,
/// and "entry 3" is the difference between a diagnosis and a guess.
fn unpack_blobs(lens: &[u32], blob: &[u8], what: &'static str) -> Result<Vec<Vec<u8>>> {
    if lens.is_empty() {
        if !blob.is_empty() {
            return Err(Error::Length(None));
        }
        return Ok(Vec::new());
    }
    let mut out = Vec::with_capacity(lens.len());
    let mut pos = 0usize;
    for (i, l) in lens.iter().enumerate() {
        let l = *l as usize;
        if l > blob.len() - pos {
            return Err(Error::Length(Some((what, i))));
        }
        out.push(blob[pos..pos + l].to_vec());
        pos += l;
    }
    if pos != blob.len() {
        return Err(Error::Length(None));
    }
    Ok(out)
}

/// The same rule for a blob of sub-frames, each parsed by `read`.
fn unpack_frames<T>(
    lens: &[u32],
    blob: &[u8],
    what: &'static str,
    read: impl Fn(&[u8]) -> Result<T>,
) -> Result<Vec<T>> {
    if lens.is_empty() {
        if !blob.is_empty() {
            return Err(Error::Length(None));
        }
        return Ok(Vec::new());
    }
    let mut out = Vec::with_capacity(lens.len());
    let mut pos = 0usize;
    for (i, l) in lens.iter().enumerate() {
        let l = *l as usize;
        if l > blob.len() - pos {
            return Err(Error::Length(Some((what, i))));
        }
        out.push(read(&blob[pos..pos + l])?);
        pos += l;
    }
    if pos != blob.len() {
        return Err(Error::Length(None));
    }
    Ok(out)
}

/// Marshal each item and return the per-item lengths beside the concatenation.
fn pack_frames<T>(items: &[T], write: impl Fn(&T) -> Vec<u8>) -> (Vec<u32>, Vec<u8>) {
    let mut lens = Vec::with_capacity(items.len());
    let mut blob = Vec::new();
    for it in items {
        let m = write(it);
        lens.push(m.len() as u32);
        blob.extend_from_slice(&m);
    }
    (lens, blob)
}

fn read_id(o: &zap::Object<'_>, field: usize) -> Id {
    ids::from_slice(o.bytes_fixed(field, ids::ID_LEN))
}

// ------------------------------------------------------- transparent input --
//
//  TxID 32B@0, OutputIdx u32@32, Amount u64@36, Address bytes@44

const TIN: usize = 52;

pub fn write_transparent_in(t: &TransparentIn) -> Vec<u8> {
    let mut b = zap::Builder::new(zap::HEADER_SIZE + TIN + t.address.len() + 32);
    let ob = b.start_object(TIN);
    ob.set_bytes_fixed(&mut b, 0, &t.tx);
    ob.set_u32(&mut b, 32, t.output);
    ob.set_u64(&mut b, 36, t.amount);
    ob.set_bytes(&mut b, 44, &t.address);
    ob.finish_as_root(&mut b);
    b.finish()
}

pub fn read_transparent_in(data: &[u8]) -> Result<TransparentIn> {
    let o = frame(data)?;
    Ok(TransparentIn {
        tx: read_id(&o, 0),
        output: o.u32(32),
        amount: o.u64(36),
        address: o.bytes(44).to_vec(),
    })
}

// ------------------------------------------------------ transparent output --
//
//  Amount u64@0, AssetID 32B@8, Address bytes@40

const TOUT: usize = 48;

pub fn write_transparent_out(t: &TransparentOut) -> Vec<u8> {
    let mut b = zap::Builder::new(zap::HEADER_SIZE + TOUT + t.address.len() + 32);
    let ob = b.start_object(TOUT);
    ob.set_u64(&mut b, 0, t.amount);
    ob.set_bytes_fixed(&mut b, 8, &t.asset);
    ob.set_bytes(&mut b, 40, &t.address);
    ob.finish_as_root(&mut b);
    b.finish()
}

pub fn read_transparent_out(data: &[u8]) -> Result<TransparentOut> {
    let o = frame(data)?;
    Ok(TransparentOut {
        amount: o.u64(0),
        asset: read_id(&o, 8),
        address: o.bytes(40).to_vec(),
    })
}

// -------------------------------------------------------- shielded output --
//
//  four byte fields @0/8/16/24

const SOUT: usize = 32;

pub fn write_shielded(s: &Shielded) -> Vec<u8> {
    let mut b = zap::Builder::new(
        zap::HEADER_SIZE
            + SOUT
            + s.commitment.len()
            + s.note.len()
            + s.ephemeral.len()
            + s.range_proof.len()
            + 64,
    );
    let ob = b.start_object(SOUT);
    ob.set_bytes(&mut b, 0, &s.commitment);
    ob.set_bytes(&mut b, 8, &s.note);
    ob.set_bytes(&mut b, 16, &s.ephemeral);
    ob.set_bytes(&mut b, 24, &s.range_proof);
    ob.finish_as_root(&mut b);
    b.finish()
}

pub fn read_shielded(data: &[u8]) -> Result<Shielded> {
    let o = frame(data)?;
    Ok(Shielded {
        commitment: o.bytes(0).to_vec(),
        note: o.bytes(8).to_vec(),
        ephemeral: o.bytes(16).to_vec(),
        range_proof: o.bytes(24).to_vec(),
    })
}

// ------------------------------------------------------------------- proof --
//
//  System bytes@0, Data bytes@8, PublicLens list@16, PublicBlob bytes@24

const PROOF: usize = 32;

/// A proof's frame, or no bytes at all when there is no proof. Absence is
/// carried as an empty bytes field, so the two states are one field rather than
/// a flag and a value that can disagree.
pub fn write_proof(p: Option<&Proof>) -> Vec<u8> {
    let Some(p) = p else {
        return Vec::new();
    };
    let (lens, blob) = pack_blobs(&p.public);
    let mut b = zap::Builder::new(
        zap::HEADER_SIZE + PROOF + p.system.len() + p.data.len() + blob.len() + 4 * lens.len() + 64,
    );
    let lens_off = write_u32s(&mut b, &lens);
    let ob = b.start_object(PROOF);
    ob.set_bytes(&mut b, 0, &p.system);
    ob.set_bytes(&mut b, 8, &p.data);
    ob.set_list(&mut b, 16, lens_off, lens.len());
    ob.set_bytes(&mut b, 24, &blob);
    ob.finish_as_root(&mut b);
    b.finish()
}

pub fn read_proof(data: &[u8]) -> Result<Option<Proof>> {
    if data.is_empty() {
        return Ok(None);
    }
    let o = frame(data)?;
    let public = unpack_blobs(&read_u32s(&o, 16), o.bytes(24), "entry")?;
    Ok(Some(Proof {
        system: o.bytes(0).to_vec(),
        data: o.bytes(8).to_vec(),
        public,
    }))
}

// -------------------------------------------------------------------- utxo --
//
//  TxID 32B@0, OutputIndex u32@32, Height u64@36,
//  Commitment bytes@44, Ciphertext bytes@52, EphemeralPK bytes@60

const UTXO: usize = 68;

pub fn write_utxo(u: &Utxo) -> Vec<u8> {
    let mut b = zap::Builder::new(
        zap::HEADER_SIZE
            + UTXO
            + u.commitment.len()
            + u.ciphertext.len()
            + u.ephemeral.len()
            + 64,
    );
    let ob = b.start_object(UTXO);
    ob.set_bytes_fixed(&mut b, 0, &u.tx);
    ob.set_u32(&mut b, 32, u.output);
    ob.set_u64(&mut b, 36, u.height);
    ob.set_bytes(&mut b, 44, &u.commitment);
    ob.set_bytes(&mut b, 52, &u.ciphertext);
    ob.set_bytes(&mut b, 60, &u.ephemeral);
    ob.finish_as_root(&mut b);
    b.finish()
}

pub fn read_utxo(data: &[u8]) -> Result<Utxo> {
    let o = frame(data)?;
    Ok(Utxo {
        tx: read_id(&o, 0),
        output: o.u32(32),
        height: o.u64(36),
        commitment: o.bytes(44).to_vec(),
        ciphertext: o.bytes(52).to_vec(),
        ephemeral: o.bytes(60).to_vec(),
    })
}

// ------------------------------------------------------------- transaction --
//
//  Kind u8@0, Version u8@1, Fee u64@2, Expiry u64@10,
//  TInLens list@18,  TInBlob bytes@26,  TOutLens list@34, TOutBlob bytes@42,
//  NullLens list@50, NullBlob bytes@58, SOutLens list@66, SOutBlob bytes@74,
//  Proof bytes@82, Memo bytes@90

const TX: usize = 98;

pub fn write_tx(tx: &Tx) -> Vec<u8> {
    let (tin_lens, tin_blob) = pack_frames(&tx.transparent_in, write_transparent_in);
    let (tout_lens, tout_blob) = pack_frames(&tx.transparent_out, write_transparent_out);
    let (null_lens, null_blob) = pack_blobs(&tx.nullifiers);
    let (sout_lens, sout_blob) = pack_frames(&tx.outputs, write_shielded);
    let proof = write_proof(tx.proof.as_ref());

    let mut b = zap::Builder::new(
        zap::HEADER_SIZE
            + TX
            + tin_blob.len()
            + tout_blob.len()
            + null_blob.len()
            + sout_blob.len()
            + proof.len()
            + tx.memo.len()
            + 4 * (tin_lens.len() + tout_lens.len() + null_lens.len() + sout_lens.len())
            + 512,
    );
    let tin_off = write_u32s(&mut b, &tin_lens);
    let tout_off = write_u32s(&mut b, &tout_lens);
    let null_off = write_u32s(&mut b, &null_lens);
    let sout_off = write_u32s(&mut b, &sout_lens);

    let ob = b.start_object(TX);
    ob.set_u8(&mut b, 0, tx.kind.0);
    ob.set_u8(&mut b, 1, tx.version);
    ob.set_u64(&mut b, 2, tx.fee);
    ob.set_u64(&mut b, 10, tx.expiry);
    ob.set_list(&mut b, 18, tin_off, tin_lens.len());
    ob.set_bytes(&mut b, 26, &tin_blob);
    ob.set_list(&mut b, 34, tout_off, tout_lens.len());
    ob.set_bytes(&mut b, 42, &tout_blob);
    ob.set_list(&mut b, 50, null_off, null_lens.len());
    ob.set_bytes(&mut b, 58, &null_blob);
    ob.set_list(&mut b, 66, sout_off, sout_lens.len());
    ob.set_bytes(&mut b, 74, &sout_blob);
    ob.set_bytes(&mut b, 82, &proof);
    ob.set_bytes(&mut b, 90, &tx.memo);
    ob.finish_as_root(&mut b);
    b.finish()
}

pub fn read_tx(data: &[u8]) -> Result<Tx> {
    let o = frame(data)?;
    Ok(Tx {
        kind: Kind(o.u8(0)),
        version: o.u8(1),
        fee: o.u64(2),
        expiry: o.u64(10),
        transparent_in: unpack_frames(
            &read_u32s(&o, 18),
            o.bytes(26),
            "item",
            read_transparent_in,
        )?,
        transparent_out: unpack_frames(
            &read_u32s(&o, 34),
            o.bytes(42),
            "item",
            read_transparent_out,
        )?,
        nullifiers: unpack_blobs(&read_u32s(&o, 50), o.bytes(58), "entry")?,
        outputs: unpack_frames(&read_u32s(&o, 66), o.bytes(74), "item", read_shielded)?,
        proof: read_proof(o.bytes(82))?,
        memo: o.bytes(90).to_vec(),
    })
}

// ------------------------------------------------------------------- block --
//
//  ParentID 32B@0, Height u64@32, Timestamp i64@40,
//  TxLens list@48, TxBlob bytes@56, StateRoot bytes@64, BlockProof bytes@72

const BLOCK: usize = 80;

/// What a block's bytes are, before it is anything else. Held separately from
/// [`crate::block::Block`] so this module can be read as the wire and nothing
/// else.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct BlockBody {
    pub parent: Id,
    pub height: u64,
    pub timestamp: i64,
    pub txs: Vec<Tx>,
    pub state_root: Vec<u8>,
    pub proof: Option<Proof>,
}

pub fn write_block(b: &BlockBody) -> Vec<u8> {
    let (tx_lens, tx_blob) = pack_frames(&b.txs, write_tx);
    let proof = write_proof(b.proof.as_ref());
    let mut bld = zap::Builder::new(
        zap::HEADER_SIZE
            + BLOCK
            + tx_blob.len()
            + b.state_root.len()
            + proof.len()
            + 4 * tx_lens.len()
            + 256,
    );
    let tx_off = write_u32s(&mut bld, &tx_lens);
    let ob = bld.start_object(BLOCK);
    ob.set_bytes_fixed(&mut bld, 0, &b.parent);
    ob.set_u64(&mut bld, 32, b.height);
    ob.set_u64(&mut bld, 40, b.timestamp as u64);
    ob.set_list(&mut bld, 48, tx_off, tx_lens.len());
    ob.set_bytes(&mut bld, 56, &tx_blob);
    ob.set_bytes(&mut bld, 64, &b.state_root);
    ob.set_bytes(&mut bld, 72, &proof);
    ob.finish_as_root(&mut bld);
    bld.finish()
}

pub fn read_block(data: &[u8]) -> Result<BlockBody> {
    let o = frame(data)?;
    Ok(BlockBody {
        parent: read_id(&o, 0),
        height: o.u64(32),
        timestamp: o.u64(40) as i64,
        state_root: o.bytes(64).to_vec(),
        txs: unpack_frames(&read_u32s(&o, 48), o.bytes(56), "item", read_tx)?,
        proof: read_proof(o.bytes(72))?,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bytes(b: u8, n: usize) -> Vec<u8> {
        vec![b; n]
    }

    fn shielded(seed: u8) -> Shielded {
        Shielded {
            commitment: bytes(seed, 32),
            note: bytes(seed + 1, 64),
            ephemeral: bytes(seed + 2, 32),
            range_proof: bytes(seed + 3, 64),
        }
    }

    fn proof(system: &str, seed: u8) -> Proof {
        Proof {
            system: system.as_bytes().to_vec(),
            data: bytes(seed, 192),
            public: vec![bytes(seed + 1, 32), bytes(seed + 2, 32)],
        }
    }

    fn transfer(nullifier: u8) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            version: 1,
            nullifiers: vec![bytes(nullifier, 32)],
            outputs: vec![shielded(0x50)],
            proof: Some(proof("stark", 0x60)),
            fee: 1,
            expiry: 1000,
            ..Tx::default()
        }
    }

    fn block(txs: Vec<Tx>) -> BlockBody {
        BlockBody {
            parent: ids::repeated(9),
            height: 1,
            timestamp: 1000,
            txs,
            state_root: bytes(0xAB, 32),
            proof: None,
        }
    }

    #[test]
    fn a_transparent_input_round_trips() {
        let t = TransparentIn {
            tx: ids::repeated(0x70),
            output: 3,
            amount: 500,
            address: bytes(0x70, 20),
        };
        assert_eq!(read_transparent_in(&write_transparent_in(&t)).unwrap(), t);
    }

    #[test]
    fn a_transparent_output_round_trips() {
        let t = TransparentOut {
            amount: 400,
            address: bytes(0x71, 20),
            asset: ids::repeated(50),
        };
        assert_eq!(read_transparent_out(&write_transparent_out(&t)).unwrap(), t);
    }

    #[test]
    fn a_shielded_output_round_trips() {
        let s = shielded(0x50);
        assert_eq!(read_shielded(&write_shielded(&s)).unwrap(), s);
    }

    #[test]
    fn a_proof_round_trips_and_absence_is_no_bytes_at_all() {
        let p = proof("stark", 0x60);
        assert_eq!(read_proof(&write_proof(Some(&p))).unwrap(), Some(p));
        assert!(write_proof(None).is_empty());
        assert_eq!(read_proof(&[]).unwrap(), None);
    }

    #[test]
    fn a_transaction_round_trips_with_every_list_populated() {
        let mut tx = transfer(0x10);
        tx.transparent_in.push(TransparentIn {
            tx: ids::repeated(0x70),
            output: 0,
            amount: 500,
            address: bytes(0x70, 20),
        });
        tx.transparent_out.push(TransparentOut {
            amount: 400,
            address: bytes(0x71, 20),
            asset: ids::repeated(50),
        });
        tx.nullifiers.push(bytes(0x11, 32));
        tx.outputs.push(shielded(0x54));
        tx.memo = b"a memo".to_vec();
        assert_eq!(read_tx(&write_tx(&tx)).unwrap(), tx);
    }

    #[test]
    fn a_transaction_with_every_list_empty_round_trips() {
        let tx = Tx {
            kind: Kind::MINT,
            version: 1,
            fee: 7,
            expiry: 9,
            ..Tx::default()
        };
        assert_eq!(read_tx(&write_tx(&tx)).unwrap(), tx);
    }

    #[test]
    fn a_block_round_trips_empty_and_full() {
        for txs in [vec![], vec![transfer(0x10)], vec![transfer(0x10), transfer(0x11)]] {
            let b = block(txs);
            assert_eq!(read_block(&write_block(&b)).unwrap(), b);
        }
    }

    #[test]
    fn a_utxo_round_trips() {
        let u = Utxo {
            tx: ids::repeated(3),
            output: 2,
            height: 11,
            commitment: bytes(0x50, 32),
            ciphertext: bytes(0x51, 64),
            ephemeral: bytes(0x52, 32),
        };
        assert_eq!(read_utxo(&write_utxo(&u)).unwrap(), u);
    }

    /// Every byte handed in belongs to the value, or the value read is not the
    /// value that was sent.
    #[test]
    fn a_frame_with_bytes_past_its_declared_size_is_refused() {
        let mut raw = write_block(&block(vec![transfer(0x10)]));
        raw.extend_from_slice(&[0xFF, 0xFF]);
        assert_eq!(read_block(&raw), Err(Error::TrailingBytes));
    }

    #[test]
    fn a_truncated_frame_is_refused_rather_than_read_past() {
        let raw = write_block(&block(vec![transfer(0x10)]));
        for cut in [0, 1, raw.len() / 4, raw.len() / 2, raw.len() - 1] {
            assert_eq!(
                read_block(&raw[..cut]),
                Err(Error::Zap(zap::Error::BufferTooSmall)),
                "a block cut to {cut} bytes"
            );
        }
    }

    #[test]
    fn a_length_that_reaches_past_the_blob_is_refused() {
        assert_eq!(
            unpack_blobs(&[3], &[1, 2], "entry"),
            Err(Error::Length(Some(("entry", 0))))
        );
    }

    #[test]
    fn blob_bytes_no_length_claims_are_refused() {
        assert_eq!(unpack_blobs(&[1], &[1, 2], "entry"), Err(Error::Length(None)));
        assert_eq!(unpack_blobs(&[], &[1], "entry"), Err(Error::Length(None)));
    }

    #[test]
    fn an_exact_partition_is_admitted() {
        assert_eq!(
            unpack_blobs(&[1, 2], &[1, 2, 3], "entry"),
            Ok(vec![vec![1], vec![2, 3]])
        );
        assert_eq!(unpack_blobs(&[], &[], "entry"), Ok(Vec::new()));
    }

    /// A declared count no buffer of this size could hold must not become an
    /// allocation. The capacity comes from the length vector, which the frame
    /// bounds, and the vector itself is refused by the stride clamp.
    #[test]
    fn an_absurd_list_length_allocates_nothing() {
        let mut raw = write_tx(&transfer(0x10));
        let root = u32::from_le_bytes(raw[8..12].try_into().unwrap()) as usize;
        // The nullifier length vector's count sits at field 50 + 4.
        raw[root + 54..root + 58].copy_from_slice(&u32::MAX.to_le_bytes());
        // The list reads as null, so the blob is then bytes nothing claims.
        assert_eq!(read_tx(&raw), Err(Error::Length(None)));
    }
}
