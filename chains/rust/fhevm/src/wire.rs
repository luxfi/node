// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Struct-is-wire for the F-Chain: no hand-rolled big-endian, no cursor codec.
//! A transaction and a block each own their encoding over [`crate::zap`]
//! objects at FIXED field offsets, and the on-wire format is exactly those
//! offsets.
//!
//! BOTH PARSERS ARE CANONICAL. Each re-serializes what it decoded and refuses
//! input that is not already byte-identical to it, so exactly one byte-string
//! decodes to each transaction and each block. Without that, zap follows the
//! root offset and ignores unreferenced padding inside a message's declared
//! size, so distinct byte-strings decode to identical fields — and since the id
//! is the hash of the BYTES, one logical transaction would have as many ids as
//! there are ways to pad it.
//!
//! THE SIGNING ARCHITECTURE, in three objects:
//!
//! - [`content`] is one zap object binding every semantically meaningful field
//!   — type, payer, subject, gas limit, nonce, scheme, payload — and EXCLUDING
//!   auth and signature. Because the payer is bound here and authentication
//!   requires payer == address_of(auth), an attacker cannot swap in a different
//!   public key. And because the subject is bound here — and syntactic
//!   verification requires it to equal what the payload derives — the signature
//!   covers the OBJECT acted on, not merely the arguments that produce it.
//!
//! - [`signing_bytes`] is the preimage the payer signs: that content, bound to
//!   the ONE chain it is meant for. The chain id does not travel; each side
//!   supplies its own. Without that binding a captured transaction replays
//!   verbatim onto every other F-Chain, where the same payer exists — an
//!   address is the hash of a public key — burning its balance there for an
//!   operation it never asked for.
//!
//! - [`bytes`] is the content followed by an appended object carrying auth and
//!   signature, and the id is the hash of the whole thing. What is
//!   authenticated is still exactly what is transmitted: the preimage is a pure
//!   function of the chain and the transmitted content prefix.

use crate::error::{Code, Error, Result};
use crate::id::Id;
use crate::tx::{Transaction, TX_DOMAIN};
use crate::zap;

// ---- the transaction's content object ----
//
//  Type     u8    @ 0
//  Payer    20B   @ 1
//  Subject  32B   @ 21
//  GasLimit u64   @ 53
//  Nonce    u64   @ 61
//  Scheme   bytes @ 69
//  Payload  bytes @ 77
const TX_TYPE: usize = 0;
const TX_PAYER: usize = 1;
const TX_SUBJECT: usize = 21;
const TX_GAS: usize = 53;
const TX_NONCE: usize = 61;
const TX_SCHEME: usize = 69;
const TX_PAYLOAD: usize = 77;
const TX_SIZE: usize = 85;

// ---- the appended auth object ----
//
//  Auth bytes @ 0
//  Sig  bytes @ 8
const SG_AUTH: usize = 0;
const SG_SIG: usize = 8;
const SG_SIZE: usize = 16;

// ---- the block ----
//
//  ParentID  32B   @ 0
//  Height    u64   @ 32
//  Timestamp i64   @ 40   Unix seconds — F's block-time resolution
//  TxLens    list  @ 48   one u32 per transaction wire length
//  TxBlob    bytes @ 56   the concatenated transaction objects
const BLK_PARENT: usize = 0;
const BLK_HEIGHT: usize = 32;
const BLK_TIME: usize = 40;
const BLK_TX_LENS: usize = 48;
const BLK_TX_BLOB: usize = 56;
const BLK_SIZE: usize = 64;

/// What a block may occupy on the wire.
///
/// Without it a peer decides how much this node parses, hashes and allocates
/// before anything about the block has been checked: an 8 MB message carrying
/// 1,524 transactions parsed to completion, because the transaction-count bound
/// is applied by verification and verification runs after the parse. Both
/// bounds belong at the first byte, and neither implies the other — this one
/// bounds the bytes read, [`MAX_BLOCK_TXS`] bounds the signatures verified, and
/// a small block can still declare a great many tiny transactions.
pub const MAX_BLOCK_SIZE: usize = 2 << 20;

/// How many transactions one block carries. Without it a proposer's block size
/// is whatever the mempool happens to hold.
pub const MAX_BLOCK_TXS: usize = 1024;

/// What one transaction costs a block BEYOND its own bytes: four for its entry
/// in the length list, and up to four more of zap's eight-byte alignment.
///
/// It is an upper bound by construction, which is the direction that matters:
/// a builder adds it per selected transaction, so its running total can never
/// come in under the block it describes and a proposer cannot select its way
/// past a size its own verification refuses.
pub const TX_ENTRY: usize = 8;

/// The deterministic encoding of the transaction's semantic fields — including
/// payer and subject, excluding auth and signature.
pub fn content(tx: &Transaction) -> Vec<u8> {
    let mut b = zap::Builder::new(zap::HEADER_SIZE + TX_SIZE + tx.scheme.len() + tx.payload.len() + 64);
    let o = b.start_object(TX_SIZE);
    o.set_u8(&mut b, TX_TYPE, tx.tx_type);
    o.set_bytes_fixed(&mut b, TX_PAYER, &tx.payer);
    o.set_bytes_fixed(&mut b, TX_SUBJECT, &tx.subject);
    o.set_u64(&mut b, TX_GAS, tx.gas_limit);
    o.set_u64(&mut b, TX_NONCE, tx.nonce);
    o.set_bytes(&mut b, TX_SCHEME, &tx.scheme);
    o.set_bytes(&mut b, TX_PAYLOAD, &tx.payload);
    let root = o.offset();
    b.finish(root)
}

/// The preimage the payer signs: the content, bound to the chain it is for.
pub fn signing_bytes(tx: &Transaction, chain: &Id) -> Vec<u8> {
    let c = content(tx);
    let mut out = Vec::with_capacity(TX_DOMAIN.len() + chain.len() + c.len());
    out.extend_from_slice(TX_DOMAIN);
    out.extend_from_slice(chain);
    out.extend_from_slice(&c);
    out
}

/// The full wire encoding: the content object, then the auth object.
pub fn bytes(tx: &Transaction) -> Vec<u8> {
    let mut out = content(tx);

    let mut b = zap::Builder::new(zap::HEADER_SIZE + SG_SIZE + tx.auth.len() + tx.sig.len() + 32);
    let o = b.start_object(SG_SIZE);
    o.set_bytes(&mut b, SG_AUTH, &tx.auth);
    o.set_bytes(&mut b, SG_SIG, &tx.sig);
    let root = o.offset();
    out.extend_from_slice(&b.finish(root));
    out
}

/// Decode a transaction: the leading content object, then the appended auth
/// object, then the canonical check.
pub fn parse_transaction(data: &[u8]) -> Result<Transaction> {
    let n = split(data)?;
    let content_msg = zap::parse(&data[..n])?;
    let auth_msg = zap::parse(&data[n..])?;
    if n + auth_msg.size() != data.len() {
        return Err(Error::detail(Code::InvalidPayload, "trailing bytes"));
    }

    let c = content_msg.root();
    let a = auth_msg.root();
    let mut tx = Transaction {
        tx_type: c.u8(TX_TYPE),
        scheme: c.bytes(TX_SCHEME).to_vec(),
        payer: [0u8; 20],
        subject: [0u8; 32],
        gas_limit: c.u64(TX_GAS),
        nonce: c.u64(TX_NONCE),
        payload: c.bytes(TX_PAYLOAD).to_vec(),
        auth: a.bytes(SG_AUTH).to_vec(),
        sig: a.bytes(SG_SIG).to_vec(),
    };
    tx.payer.copy_from_slice(&pad::<20>(c.bytes_fixed(TX_PAYER, 20)));
    tx.subject.copy_from_slice(&pad::<32>(c.bytes_fixed(TX_SUBJECT, 32)));

    // Canonical wire, and it is what the id is bound to. Anything that is not
    // already the form these fields re-serialize to is refused, so exactly one
    // byte-string authenticates per logical transaction — no id malleability,
    // fail closed.
    if data != bytes(&tx) {
        return Err(Error::detail(Code::InvalidPayload, "non-canonical tx encoding"));
    }
    Ok(tx)
}

/// A parsed block's fields. The block type itself lives in [`crate::block`];
/// this is what the wire says, before any rule is applied to it.
#[derive(Debug)]
pub struct BlockFields {
    pub parent: Id,
    pub height: u64,
    pub timestamp: i64,
    pub transactions: Vec<Transaction>,
}

/// The block's encoding: parent, height, timestamp, transactions.
pub fn block_bytes(parent: &Id, height: u64, timestamp: i64, txs: &[Transaction]) -> Vec<u8> {
    let mut lens: Vec<u32> = Vec::with_capacity(txs.len());
    let mut blob: Vec<u8> = Vec::new();
    for tx in txs {
        let b = bytes(tx);
        lens.push(b.len() as u32);
        blob.extend_from_slice(&b);
    }

    let mut b = zap::Builder::new(
        zap::HEADER_SIZE + BLK_SIZE + blob.len() + 4 * lens.len() + 128,
    );
    // The length list is written BEFORE the object that points at it, which is
    // what makes that pointer backwards and the encoding deterministic.
    let mut list = b.start_list();
    for l in &lens {
        list.add_u32(&mut b, *l);
    }
    let (list_off, list_len) = list.finish();

    let o = b.start_object(BLK_SIZE);
    o.set_bytes_fixed(&mut b, BLK_PARENT, parent);
    o.set_u64(&mut b, BLK_HEIGHT, height);
    o.set_i64(&mut b, BLK_TIME, timestamp);
    o.set_list(&mut b, BLK_TX_LENS, list_off, list_len);
    o.set_bytes(&mut b, BLK_TX_BLOB, &blob);
    let root = o.offset();
    b.finish(root)
}

/// Decode a block. Bounded at the first byte, then canonical like a
/// transaction.
pub fn parse_block(data: &[u8]) -> Result<BlockFields> {
    if data.len() > MAX_BLOCK_SIZE {
        return Err(Error::detail(
            Code::InvalidPayload,
            format!("block is {} bytes, over {}", data.len(), MAX_BLOCK_SIZE),
        ));
    }
    let msg = zap::parse(data)?;
    if msg.size() != data.len() {
        return Err(Error::detail(Code::InvalidPayload, "block trailing bytes"));
    }
    let o = msg.root();
    let parent: Id = pad::<32>(o.bytes_fixed(BLK_PARENT, 32));
    let height = o.u64(BLK_HEIGHT);
    let timestamp = o.i64(BLK_TIME);

    let lens = o.list(BLK_TX_LENS, 4);
    if lens.len() > MAX_BLOCK_TXS {
        return Err(Error::detail(
            Code::InvalidPayload,
            format!("block declares {} transactions, over {}", lens.len(), MAX_BLOCK_TXS),
        ));
    }
    let blob = o.bytes(BLK_TX_BLOB);
    let mut transactions = Vec::with_capacity(lens.len());
    let mut at = 0usize;
    for i in 0..lens.len() {
        let l = lens.u32(i) as usize;
        if at + l > blob.len() {
            return Err(Error::detail(Code::InvalidPayload, "tx blob out of bounds"));
        }
        transactions.push(parse_transaction(&blob[at..at + l])?);
        at += l;
    }

    // The whole encoding must be the one this block serializes to, the same
    // rule a transaction is held to. Bytes that no length covers are refused by
    // it too, and not separately: a blob with unread bytes re-serializes
    // SHORTER than it arrived, so the comparison fails.
    if data != block_bytes(&parent, height, timestamp, &transactions) {
        return Err(Error::detail(Code::InvalidPayload, "non-canonical block encoding"));
    }
    Ok(BlockFields { parent, height, timestamp, transactions })
}

/// The wire cost of a block carrying no transactions: the header, the root
/// object, and an empty length list.
pub fn empty_block_size() -> usize {
    block_bytes(&[0u8; 32], 0, 0, &[]).len()
}

/// The length of the leading self-delimiting message — the split point between
/// the signing prefix and the appended auth object.
fn split(b: &[u8]) -> Result<usize> {
    if b.len() < zap::HEADER_SIZE {
        return Err(Error::detail(Code::InvalidPayload, "short buffer"));
    }
    zap::message_len(b).ok_or_else(|| Error::detail(Code::InvalidPayload, "bad zap length"))
}

/// A fixed-width field read back from a buffer that may be short. zap answers
/// an empty slice for a span it cannot cover, and the zero value is what an
/// absent inline field means.
fn pad<const N: usize>(b: &[u8]) -> [u8; N] {
    let mut out = [0u8; N];
    let n = b.len().min(N);
    out[..n].copy_from_slice(&b[..n]);
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tx::TX_REGISTER_CIPHERTEXT;

    fn sample() -> Transaction {
        Transaction {
            tx_type: TX_REGISTER_CIPHERTEXT,
            scheme: b"ckks-n14".to_vec(),
            payer: [0xe9; 20],
            subject: [0x92; 32],
            gas_limit: 5_000_000,
            nonce: 1,
            payload: br#"{"digest":[1],"type":1,"level":3,"size":4096}"#.to_vec(),
            auth: vec![0xa1; 1952],
            sig: vec![0xb2; 3309],
        }
    }

    #[test]
    fn a_transaction_reads_back_as_itself() {
        let t = sample();
        let w = bytes(&t);
        assert_eq!(parse_transaction(&w).expect("parses"), t);
    }

    #[test]
    fn the_content_is_the_leading_message_and_the_auth_object_follows_it() {
        let t = sample();
        let w = bytes(&t);
        let c = content(&t);
        assert_eq!(&w[..c.len()], &c[..]);
        assert_eq!(split(&w).unwrap(), c.len());
        // The whole encoding is exactly the two messages.
        let auth = zap::parse(&w[c.len()..]).unwrap();
        assert_eq!(c.len() + auth.size(), w.len());
    }

    #[test]
    fn the_content_excludes_auth_and_signature() {
        // The signature cannot cover itself. Change either and the preimage
        // does not move.
        let t = sample();
        let mut other = t.clone();
        other.sig[0] ^= 0xff;
        other.auth[0] ^= 0xff;
        assert_eq!(content(&t), content(&other));
        assert_ne!(bytes(&t), bytes(&other));
    }

    #[test]
    fn the_preimage_is_bound_to_one_chain() {
        let t = sample();
        assert_ne!(t.signing_bytes(&[50u8; 32]), t.signing_bytes(&[51u8; 32]));
        // And it is the domain, the chain, then the content — nothing else.
        let s = t.signing_bytes(&[50u8; 32]);
        assert_eq!(&s[..TX_DOMAIN.len()], TX_DOMAIN);
        assert_eq!(&s[TX_DOMAIN.len()..TX_DOMAIN.len() + 32], &[50u8; 32]);
        assert_eq!(&s[TX_DOMAIN.len() + 32..], &content(&t)[..]);
    }

    #[test]
    fn every_semantic_field_moves_the_preimage() {
        let t = sample();
        let base = t.signing_bytes(&[50u8; 32]);
        let mut v = t.clone();
        v.tx_type = 2;
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
        let mut v = t.clone();
        v.payer[0] ^= 1;
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
        let mut v = t.clone();
        v.subject[0] ^= 1;
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
        let mut v = t.clone();
        v.gas_limit += 1;
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
        let mut v = t.clone();
        v.nonce += 1;
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
        let mut v = t.clone();
        v.scheme = b"ckks-n13".to_vec();
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
        let mut v = t.clone();
        v.payload.push(b' ');
        assert_ne!(v.signing_bytes(&[50u8; 32]), base);
    }

    #[test]
    fn trailing_bytes_are_refused() {
        let t = sample();
        let mut w = bytes(&t);
        w.extend_from_slice(&[0xff, 0xff]);
        let e = parse_transaction(&w).unwrap_err();
        assert!(e.detail.contains("trailing bytes"), "{}", e.detail);
    }

    #[test]
    fn every_truncation_is_refused() {
        let t = sample();
        let w = bytes(&t);
        for cut in [0, 1, w.len() / 4, w.len() / 2, w.len() - 1] {
            assert!(parse_transaction(&w[..cut]).is_err(), "cut at {cut}");
        }
    }

    #[test]
    fn an_unsigned_transaction_still_encodes_and_reads_back() {
        let mut t = sample();
        t.auth.clear();
        t.sig.clear();
        let w = bytes(&t);
        assert_eq!(parse_transaction(&w).expect("parses"), t);
        // The auth object is a bare header plus its fixed section.
        assert_eq!(w.len(), content(&t).len() + zap::HEADER_SIZE + SG_SIZE);
    }

    #[test]
    fn padding_inside_the_declared_size_is_refused_rather_than_ignored() {
        // zap follows the root offset and would read the same fields out of a
        // longer buffer, which is exactly how one transaction gets two ids.
        let t = sample();
        let w = bytes(&t);
        let n = split(&w).unwrap();
        let mut padded = w[..n].to_vec();
        padded.extend_from_slice(&[0u8; 8]);
        let size = padded.len() as u32;
        padded[12..16].copy_from_slice(&size.to_le_bytes());
        padded.extend_from_slice(&w[n..]);
        let e = parse_transaction(&padded).unwrap_err();
        assert!(e.detail.contains("non-canonical"), "{}", e.detail);
    }

    #[test]
    fn a_block_reads_back_as_itself() {
        let txs = vec![sample(), Transaction { nonce: 2, ..sample() }];
        let w = block_bytes(&[7u8; 32], 3, 1000, &txs);
        let b = parse_block(&w).expect("parses");
        assert_eq!(b.parent, [7u8; 32]);
        assert_eq!(b.height, 3);
        assert_eq!(b.timestamp, 1000);
        assert_eq!(b.transactions, txs);
    }

    #[test]
    fn an_empty_block_is_the_header_the_object_and_a_null_list() {
        assert_eq!(empty_block_size(), zap::HEADER_SIZE + BLK_SIZE);
        let w = block_bytes(&[0u8; 32], 0, 0, &[]);
        let b = parse_block(&w).expect("parses");
        assert!(b.transactions.is_empty());
    }

    #[test]
    fn a_block_carrying_bytes_no_length_covers_is_refused() {
        // The blob re-serializes SHORTER than it arrived, so the canonical
        // check catches it without a rule of its own.
        let txs = vec![sample()];
        let mut w = block_bytes(&[7u8; 32], 3, 1000, &txs);
        let extra = vec![0u8; 8];
        // Grow the blob in place: append to the buffer and widen the declared
        // sizes so the reader accepts the span.
        let blob_len_at = {
            let msg = zap::parse(&w).unwrap();
            let root = msg.root();
            let _ = root;
            // The blob's (offset, length) pair sits at the root's field 56.
            let root_off = u32::from_le_bytes(w[8..12].try_into().unwrap()) as usize;
            root_off + BLK_TX_BLOB + 4
        };
        let old = u32::from_le_bytes(w[blob_len_at..blob_len_at + 4].try_into().unwrap());
        w.extend_from_slice(&extra);
        w[blob_len_at..blob_len_at + 4]
            .copy_from_slice(&(old + extra.len() as u32).to_le_bytes());
        let total = w.len() as u32;
        w[12..16].copy_from_slice(&total.to_le_bytes());
        assert!(parse_block(&w).is_err());
    }

    #[test]
    fn a_block_over_its_size_bound_is_refused_at_the_first_byte() {
        let too_big = vec![0u8; MAX_BLOCK_SIZE + 1];
        let e = parse_block(&too_big).unwrap_err();
        assert!(e.detail.contains("over 2097152"), "{}", e.detail);
    }
}
