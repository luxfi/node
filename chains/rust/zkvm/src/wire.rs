// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The bytes, at the offsets the Go chain writes.
//!
//! The offsets are in `chains/schema/zchain.zap` and `zchain_zap` is what
//! `zapgen` wrote out of it, so what is left here is the part a schema cannot
//! state: how a run of variable-width items is laid out, and what counts as a
//! whole message. A nested list `[T]` is packed as a u32 length vector plus
//! the concatenation of the elements' own frames; an optional pointer is one
//! bytes field that is empty exactly when the value is absent; a `[[u8]]` is a
//! length vector plus a concatenated blob.
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
use crate::zchain_zap as w;
use lux_zap::zap;

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

/// A length run, as the numbers that partition a blob.
fn u32s(l: zap::List<'_>) -> Vec<u32> {
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

// ------------------------------------------------------- transparent input --
//
//  TxID 32B@0, OutputIdx u32@32, Amount u64@36, Address bytes@44

pub fn write_transparent_in(t: &TransparentIn) -> Vec<u8> {
    w::new_transparent_in(&w::TransparentInInput {
        tx_id: &t.tx,
        output: t.output,
        amount: t.amount,
        address: &t.address,
    })
}

pub fn read_transparent_in(data: &[u8]) -> Result<TransparentIn> {
    let v = w::TransparentIn::new(frame(data)?);
    Ok(TransparentIn {
        tx: *v.tx_id(),
        output: v.output(),
        amount: v.amount(),
        address: v.address().to_vec(),
    })
}

// ------------------------------------------------------ transparent output --
//
//  Amount u64@0, AssetID 32B@8, Address bytes@40

pub fn write_transparent_out(t: &TransparentOut) -> Vec<u8> {
    w::new_transparent_out(&w::TransparentOutInput {
        amount: t.amount,
        asset: &t.asset,
        address: &t.address,
    })
}

pub fn read_transparent_out(data: &[u8]) -> Result<TransparentOut> {
    let v = w::TransparentOut::new(frame(data)?);
    Ok(TransparentOut {
        amount: v.amount(),
        asset: *v.asset(),
        address: v.address().to_vec(),
    })
}

// -------------------------------------------------------- shielded output --
//
//  four byte fields @0/8/16/24

pub fn write_shielded(s: &Shielded) -> Vec<u8> {
    w::new_shielded(&w::ShieldedInput {
        commitment: &s.commitment,
        note: &s.note,
        ephemeral: &s.ephemeral,
        range_proof: &s.range_proof,
    })
}

pub fn read_shielded(data: &[u8]) -> Result<Shielded> {
    let v = w::Shielded::new(frame(data)?);
    Ok(Shielded {
        commitment: v.commitment().to_vec(),
        note: v.note().to_vec(),
        ephemeral: v.ephemeral().to_vec(),
        range_proof: v.range_proof().to_vec(),
    })
}

// ------------------------------------------------------------------- proof --
//
//  System bytes@0, Data bytes@8, PublicLens list@16, PublicBlob bytes@24

/// A proof's frame, or no bytes at all when there is no proof. Absence is
/// carried as an empty bytes field, so the two states are one field rather than
/// a flag and a value that can disagree.
pub fn write_proof(p: Option<&Proof>) -> Vec<u8> {
    let Some(p) = p else {
        return Vec::new();
    };
    let (lens, blob) = pack_blobs(&p.public);
    w::new_proof(&w::ProofInput {
        system: &p.system,
        data: &p.data,
        public_lens: &lens,
        public_blob: &blob,
    })
}

pub fn read_proof(data: &[u8]) -> Result<Option<Proof>> {
    if data.is_empty() {
        return Ok(None);
    }
    let v = w::Proof::new(frame(data)?);
    let public = unpack_blobs(&u32s(v.public_lens()), v.public_blob(), "entry")?;
    Ok(Some(Proof {
        system: v.system().to_vec(),
        data: v.data().to_vec(),
        public,
    }))
}

// -------------------------------------------------------------------- utxo --
//
//  TxID 32B@0, OutputIndex u32@32, Height u64@36,
//  Commitment bytes@44, Ciphertext bytes@52, EphemeralPK bytes@60

pub fn write_utxo(u: &Utxo) -> Vec<u8> {
    w::new_utxo(&w::UtxoInput {
        tx_id: &u.tx,
        output: u.output,
        height: u.height,
        commitment: &u.commitment,
        ciphertext: &u.ciphertext,
        ephemeral: &u.ephemeral,
    })
}

pub fn read_utxo(data: &[u8]) -> Result<Utxo> {
    let v = w::Utxo::new(frame(data)?);
    Ok(Utxo {
        tx: *v.tx_id(),
        output: v.output(),
        height: v.height(),
        commitment: v.commitment().to_vec(),
        ciphertext: v.ciphertext().to_vec(),
        ephemeral: v.ephemeral().to_vec(),
    })
}

// ------------------------------------------------------------- transaction --
//
//  Kind u8@0, Version u8@1, Fee u64@2, Expiry u64@10,
//  TInLens list@18,  TInBlob bytes@26,  TOutLens list@34, TOutBlob bytes@42,
//  NullLens list@50, NullBlob bytes@58, SOutLens list@66, SOutBlob bytes@74,
//  Proof bytes@82, Memo bytes@90

pub fn write_tx(tx: &Tx) -> Vec<u8> {
    let (tin_lens, tin_blob) = pack_frames(&tx.transparent_in, write_transparent_in);
    let (tout_lens, tout_blob) = pack_frames(&tx.transparent_out, write_transparent_out);
    let (null_lens, null_blob) = pack_blobs(&tx.nullifiers);
    let (sout_lens, sout_blob) = pack_frames(&tx.outputs, write_shielded);
    let proof = write_proof(tx.proof.as_ref());

    w::new_tx(&w::TxInput {
        kind: tx.kind.0,
        version: tx.version,
        fee: tx.fee,
        expiry: tx.expiry,
        in_lens: &tin_lens,
        in_blob: &tin_blob,
        out_lens: &tout_lens,
        out_blob: &tout_blob,
        null_lens: &null_lens,
        null_blob: &null_blob,
        shielded_lens: &sout_lens,
        shielded_blob: &sout_blob,
        proof: &proof,
        memo: &tx.memo,
    })
}

pub fn read_tx(data: &[u8]) -> Result<Tx> {
    let v = w::Tx::new(frame(data)?);
    Ok(Tx {
        kind: Kind(v.kind()),
        version: v.version(),
        fee: v.fee(),
        expiry: v.expiry(),
        transparent_in: unpack_frames(
            &u32s(v.in_lens()),
            v.in_blob(),
            "item",
            read_transparent_in,
        )?,
        transparent_out: unpack_frames(
            &u32s(v.out_lens()),
            v.out_blob(),
            "item",
            read_transparent_out,
        )?,
        nullifiers: unpack_blobs(&u32s(v.null_lens()), v.null_blob(), "entry")?,
        outputs: unpack_frames(&u32s(v.shielded_lens()), v.shielded_blob(), "item", read_shielded)?,
        proof: read_proof(v.proof())?,
        memo: v.memo().to_vec(),
    })
}

// ------------------------------------------------------------------- block --
//
//  ParentID 32B@0, Height u64@32, Timestamp i64@40,
//  TxLens list@48, TxBlob bytes@56, StateRoot bytes@64, BlockProof bytes@72

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
    w::new_block(&w::BlockInput {
        parent: &b.parent,
        height: b.height,
        time: b.timestamp,
        tx_lens: &tx_lens,
        tx_blob: &tx_blob,
        state_root: &b.state_root,
        proof: &proof,
    })
}

pub fn read_block(data: &[u8]) -> Result<BlockBody> {
    let v = w::Block::new(frame(data)?);
    Ok(BlockBody {
        parent: *v.parent(),
        height: v.height(),
        timestamp: v.time(),
        state_root: v.state_root().to_vec(),
        txs: unpack_frames(&u32s(v.tx_lens()), v.tx_blob(), "item", read_tx)?,
        proof: read_proof(v.proof())?,
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
