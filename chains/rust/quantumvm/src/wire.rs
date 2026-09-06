// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Native ZAP, struct-is-wire. The block and its transactions own their
//! serialization over ZAP objects at FIXED field offsets — the same offsets the
//! Go chain writes, because a Q-Chain block built here has to be the same bytes
//! a Go peer builds.
//!
//! ## Canonical means one byte string, not one encoder
//!
//! [`parse_block`] re-serializes what it decoded and refuses anything that does
//! not come back byte-identical. Nothing weaker holds: a ZAP message declares
//! its own size and its own root offset, so padding after the content, a
//! relocated root struct and a root pointed at the wire header all decode to
//! the same logical block under different SHA-256s. The block id IS
//! `sha256(bytes)`, so each of those would be a distinct id for one block — a
//! fork the network builds by itself.
//!
//! ## Parse reconstructs the whole transaction set
//!
//! Signature included. The block's verify checks every transaction's ML-DSA
//! signature over that set, so binding the check to a field the parser skipped
//! is what made it run on locally built blocks and never on received ones. A
//! signature check a parser can switch off is not a check.
//!
//! ## The id is not on the wire
//!
//! It is the content hash of these bytes, which is what makes `id ==
//! sha256(bytes)` hold unconditionally, the same way every other VM in this
//! repo names a block.

use std::sync::Arc;

use crate::block::Block;
use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::qchain_zap::{
    self as wire, BlockInput, BodyInput, TxInput, BLOCK_SIZE, BODY_SIZE, TX_SIZE,
};
use crate::quantum::Stamp;
use crate::tx::Tx;
use lux_zap::zap;

/// What a block may weigh on the wire.
///
/// Without a bound a peer decides how much memory this node allocates and how
/// much its store holds: parse, verify and commit each walk whatever arrived.
pub const MAX_BLOCK_SIZE: usize = 2 << 20;

/// How far ahead of the verifying node's clock a proposer may stamp a block.
///
/// Peers' clocks differ and this is the allowance for that. An uncapped
/// timestamp is a proposer writing chain time, which is what decides whether
/// the NEXT block may be stamped at all.
pub const MAX_FUTURE_SKEW: i64 = 60;

// The block, the signature preimage and the envelope that carries it are
// three structs in `chains/schema/qchain.zap`; their offsets and their
// accessors come from there. What is here is what a Q block MEANS.

/// The smallest an envelope can be: the ZAP header plus the fixed section, with
/// every variable field null.
pub const MIN_TX_WIRE: usize = zap::HEADER_SIZE + TX_SIZE;

/// Go's zero `time.Time` in Unix nanoseconds.
///
/// An unsigned transaction still has an envelope, and the reference fills its
/// time field from a zero `time.Time`, whose `UnixNano` is this number (year 1,
/// far outside the range the conversion is defined for, but a fixed number all
/// the same). Canonicality is byte equality, so the value a Go peer writes
/// there is the value this chain writes there.
pub const ABSENT_STAMP_NANOS: i64 = -6795364578871345152;

/// The envelope's view of a transaction that carries no signature.
pub fn absent_stamp() -> Stamp {
    Stamp {
        algorithm: 0,
        stamped: ABSENT_STAMP_NANOS,
        public_key: Vec::new(),
        signature: Vec::new(),
        quantum_stamp: Vec::new(),
    }
}

/// The transaction's canonical wire: the signature preimage.
pub fn tx_body(tx: &Tx) -> Vec<u8> {
    wire::new_body(&BodyInput {
        time: tx.timestamp,
        nonce: tx.nonce,
        data: &tx.data,
    })
}

/// The transaction and the signature over it, as it rides in a block.
pub fn tx_envelope(tx: &Tx) -> Vec<u8> {
    let body = tx.bytes().to_vec();
    let owned;
    let sig = match tx.stamp.as_ref() {
        Some(s) => s,
        None => {
            owned = absent_stamp();
            &owned
        }
    };

    wire::new_tx(&TxInput {
        body: &body,
        algorithm: sig.algorithm,
        stamped: sig.stamped,
        public_key: &sig.public_key,
        signature: &sig.signature,
        stamp: &sig.quantum_stamp,
    })
}

/// The block's canonical wire.
pub fn block_bytes(b: &Block) -> Vec<u8> {
    let mut lens: Vec<u32> = Vec::with_capacity(b.txs.len());
    let mut blob: Vec<u8> = Vec::new();
    for tx in &b.txs {
        let wire = tx_envelope(tx);
        lens.push(wire.len() as u32);
        blob.extend_from_slice(&wire);
    }

    wire::new_block(&BlockInput {
        time: b.timestamp,
        height: b.height,
        parent: &b.parent,
        chain: &b.chain,
        network: b.network,
        tx_lengths: &lens,
        tx_blob: &blob,
    })
}

/// Decode a block, transaction set included, and accept the bytes only if they
/// are the canonical encoding of what came out.
pub fn parse_block(data: &[u8]) -> Result<Block> {
    if data.len() > MAX_BLOCK_SIZE {
        return Err(Error::TooLarge {
            bytes: data.len(),
            limit: MAX_BLOCK_SIZE,
        });
    }
    let msg = zap::Message::parse(data)?;
    if msg.size() != data.len() {
        return Err(Error::Trailing("block"));
    }
    let block_view = wire::Block::new(msg.root());
    let o = block_view.object();
    // A field read past the end of the buffer answers zero rather than failing,
    // so a wire too short to hold the header does not decode to nothing — it
    // decodes to height 0, time 0 and the empty parent. Every truncation would
    // name that one value, under as many different ids as there are ways to
    // truncate.
    if o.offset() + BLOCK_SIZE > msg.size() {
        return Err(Error::Truncated {
            what: "block",
            have: msg.size().saturating_sub(o.offset()),
            need: BLOCK_SIZE,
        });
    }

    let block = Block::new(
        block_view.time(),
        block_view.height(),
        *block_view.parent(),
        *block_view.chain(),
        block_view.network(),
        parse_tx_set(block_view.tx_lengths(), block_view.tx_blob())?,
    );

    // Canonical or nothing. Re-serializing what was decoded and comparing is
    // the only check that covers every degree of freedom the container has —
    // the declared size, the root offset, and anything appended past the
    // content — rather than the handful anyone thought to enumerate.
    if block.bytes() != data {
        return Err(Error::NonCanonical);
    }
    Ok(block)
}

/// Rebuild the transactions from the length list and the blob those lengths
/// partition.
///
/// The lengths must cover the blob EXACTLY: bytes no length names are bytes the
/// block commits to and nothing reads.
fn parse_tx_set(lens: zap::List<'_>, blob: &[u8]) -> Result<Vec<Arc<Tx>>> {
    let n = lens.len();
    if n == 0 {
        return Ok(Vec::new());
    }
    // A list length is attacker-chosen and only clamped to the message size, so
    // bound it by what the blob can actually hold before allocating for it.
    if n > blob.len() / MIN_TX_WIRE {
        return Err(Error::TxCountAbsurd {
            count: n,
            bytes: blob.len(),
        });
    }

    let mut txs = Vec::with_capacity(n);
    let mut off = 0usize;
    for i in 0..n {
        let size = lens.u32(i) as usize;
        if size < MIN_TX_WIRE || off + size > blob.len() {
            return Err(Error::TxBlobMismatch(format!(
                "entry {i} claims {size} of {} remaining",
                blob.len() - off
            )));
        }
        let tx = parse_tx_envelope(&blob[off..off + size]).map_err(|e| Error::TxMalformed {
            index: i,
            why: Box::new(e),
        })?;
        txs.push(Arc::new(tx));
        off += size;
    }
    if off != blob.len() {
        return Err(Error::TxBlobMismatch(format!(
            "{} bytes past the last transaction",
            blob.len() - off
        )));
    }
    Ok(txs)
}

/// [`tx_envelope`]'s inverse.
///
/// The signature's key is the envelope's key: the signer sets one key and the
/// wire carries it once, so nothing here has to reconcile two copies of it.
pub fn parse_tx_envelope(data: &[u8]) -> Result<Tx> {
    let msg = zap::Message::parse(data)?;
    if msg.size() != data.len() {
        return Err(Error::Trailing("transaction"));
    }
    let view = wire::Tx::new(msg.root());
    let at = view.object().offset();
    if at + TX_SIZE > msg.size() {
        return Err(Error::Truncated {
            what: "transaction",
            have: msg.size().saturating_sub(at),
            need: TX_SIZE,
        });
    }

    let mut tx = parse_tx_body(view.body())?;
    tx.stamp = Some(Stamp {
        algorithm: view.algorithm(),
        stamped: view.stamped(),
        public_key: view.public_key().to_vec(),
        signature: view.signature().to_vec(),
        quantum_stamp: view.stamp().to_vec(),
    });
    Ok(tx)
}

/// Decode the signature preimage.
pub fn parse_tx_body(body: &[u8]) -> Result<Tx> {
    let msg = zap::Message::parse(body)?;
    if msg.size() != body.len() {
        return Err(Error::Trailing("transaction body"));
    }
    let view = wire::Body::new(msg.root());
    let at = view.object().offset();
    if at + BODY_SIZE > msg.size() {
        return Err(Error::Truncated {
            what: "transaction body",
            have: msg.size().saturating_sub(at),
            need: BODY_SIZE,
        });
    }
    Ok(Tx::new(view.time(), view.nonce(), view.data().to_vec()))
}

/// The id of a block wire, without holding the block.
pub fn block_id(data: &[u8]) -> Id {
    ids::hash(data)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::quantum::{Quantum, ALGORITHM_MLDSA65};
    use std::time::Duration;

    fn tx(nonce: u64) -> Tx {
        Tx::new(1000, nonce, b"quantum-instruction-payload".to_vec())
    }

    fn block(txs: Vec<Arc<Tx>>) -> Block {
        Block::new(1000, 1, ids::filled(1), ids::filled(30), 1, txs)
    }

    // The Go reference's own encoding of the corpus transaction body, taken
    // from `chains/quantumvm` itself. If this ever stops matching, a Go peer
    // and this chain no longer agree on what a transaction IS.
    const GO_TX_BODY: &str = "5a415000020000001000000043000000e8030000000000006400000000000000080000001b0000007175616e74756d2d696e737472756374696f6e2d7061796c6f6164";

    // …and the envelope the reference writes around it when the transaction
    // carries no signature.
    const GO_TX_ENVELOPE: &str = "5a4150000200000010000000830000003000000043000000000000000000000000001a3deb03b2a10000000000000000000000000000000000000000000000005a415000020000001000000043000000e8030000000000006400000000000000080000001b0000007175616e74756d2d696e737472756374696f6e2d7061796c6f6164";

    fn unhex(s: &str) -> Vec<u8> {
        (0..s.len() / 2)
            .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).unwrap())
            .collect()
    }

    #[test]
    fn a_transaction_body_is_the_bytes_the_reference_writes() {
        assert_eq!(tx_body(&tx(100)), unhex(GO_TX_BODY));
    }

    #[test]
    fn an_unsigned_envelope_is_the_bytes_the_reference_writes() {
        assert_eq!(tx_envelope(&tx(100)), unhex(GO_TX_ENVELOPE));
    }

    #[test]
    fn a_body_round_trips_through_its_own_wire() {
        let original = tx(7);
        let back = parse_tx_body(original.bytes()).unwrap();
        assert_eq!(back.timestamp, 1000);
        assert_eq!(back.nonce, 7);
        assert_eq!(back.data, original.data);
        assert_eq!(back.id(), original.id());
    }

    #[test]
    fn an_envelope_round_trips_with_its_signature() {
        let q = Quantum::new(ALGORITHM_MLDSA65, Duration::from_secs(60)).unwrap();
        let key = q.generate().unwrap();
        let mut t = tx(3);
        t.stamp = Some(q.sign(t.bytes(), &key).unwrap());

        let wire = tx_envelope(&t);
        let back = parse_tx_envelope(&wire).unwrap();
        assert_eq!(back.id(), t.id());
        assert_eq!(back.stamp, t.stamp);
        // And what came back still verifies — the signature survived the wire.
        q.verify(back.bytes(), back.stamp.as_ref()).unwrap();
        assert_eq!(tx_envelope(&back), wire);
    }

    #[test]
    fn an_unsigned_envelope_round_trips_to_the_same_bytes() {
        let t = tx(1);
        let wire = tx_envelope(&t);
        let back = parse_tx_envelope(&wire).unwrap();
        // Parsing always yields a stamp, even an empty one — which is what
        // makes re-encoding reproduce the same bytes.
        assert_eq!(back.stamp, Some(absent_stamp()));
        assert_eq!(tx_envelope(&back), wire);
    }

    #[test]
    fn a_block_round_trips_and_keeps_its_id() {
        let b = block(vec![Arc::new(tx(1)), Arc::new(tx(2))]);
        let wire = b.bytes().to_vec();
        let back = parse_block(&wire).unwrap();
        assert_eq!(back.id(), b.id());
        assert_eq!(back.height, 1);
        assert_eq!(back.txs.len(), 2);
        assert_eq!(back.bytes(), wire.as_slice());
        assert_eq!(back.id(), ids::hash(&wire));
    }

    #[test]
    fn an_empty_block_round_trips_which_is_what_genesis_is() {
        let b = Block::new(0, 0, ids::EMPTY, ids::filled(30), 1, Vec::new());
        let wire = b.bytes().to_vec();
        let back = parse_block(&wire).unwrap();
        assert!(back.txs.is_empty());
        assert_eq!(back.id(), b.id());
    }

    // Every degree of freedom the container has, refused by one rule.
    #[test]
    fn padding_after_the_content_is_not_the_same_block() {
        let b = block(vec![Arc::new(tx(1))]);
        let mut wire = b.bytes().to_vec();
        wire.push(0);
        // The declared size no longer matches the buffer.
        assert!(matches!(parse_block(&wire), Err(Error::Trailing(_))));

        // …and a size field grown to cover the padding is refused as
        // non-canonical, because re-encoding does not reproduce it.
        let mut wire2 = b.bytes().to_vec();
        wire2.push(0);
        let size = wire2.len() as u32;
        wire2[12..16].copy_from_slice(&size.to_le_bytes());
        assert!(matches!(parse_block(&wire2), Err(Error::NonCanonical)));
    }

    #[test]
    fn a_truncated_block_is_refused_rather_than_read_as_height_zero() {
        let b = block(vec![Arc::new(tx(1))]);
        let wire = b.bytes().to_vec();
        let mut short = wire[..zap::HEADER_SIZE + 40].to_vec();
        let size = short.len() as u32;
        short[12..16].copy_from_slice(&size.to_le_bytes());
        let err = parse_block(&short).unwrap_err();
        assert!(
            matches!(err, Error::Truncated { .. }),
            "a truncation must not decode to a block at height 0: {err}"
        );
    }

    #[test]
    fn a_declared_transaction_count_no_blob_could_hold_is_refused() {
        let b = block(vec![Arc::new(tx(1))]);
        let mut wire = b.bytes().to_vec();
        // The list length field sits at the root object's offset + 92.
        let root = u32::from_le_bytes(wire[8..12].try_into().unwrap()) as usize;
        let at = root + wire::BLOCK_TX_LENGTHS + 4;

        // A count the reader's own clamp still lets through — four bytes an
        // entry, and this many entries do fit — that no blob of this size
        // could hold, because each entry names at least MIN_TX_WIRE bytes.
        // This is the one the allocation bound has to catch.
        let blob = tx_envelope(&tx(1)).len();
        let absurd_count = (blob / MIN_TX_WIRE + 1) as u32;
        let mut absurd = wire.clone();
        absurd[at..at + 4].copy_from_slice(&absurd_count.to_le_bytes());
        let err = parse_block(&absurd).unwrap_err();
        assert!(matches!(err, Error::TxCountAbsurd { .. }), "{err}");

        // And a count no run of four-byte entries could fit never reaches an
        // allocation at all: the reader answers the null list, the block
        // decodes with no transactions, and re-encoding it does not reproduce
        // these bytes.
        wire[at..at + 4].copy_from_slice(&1_000_000u32.to_le_bytes());
        assert!(matches!(parse_block(&wire), Err(Error::NonCanonical)));
    }

    #[test]
    fn lengths_that_do_not_partition_the_blob_are_refused() {
        let b = block(vec![Arc::new(tx(1))]);
        let mut wire = b.bytes().to_vec();
        let root = u32::from_le_bytes(wire[8..12].try_into().unwrap()) as usize;
        // Shorten the one length so the blob has bytes nothing names.
        let lens_rel = i32::from_le_bytes(
            wire[root + wire::BLOCK_TX_LENGTHS..root + wire::BLOCK_TX_LENGTHS + 4]
                .try_into()
                .unwrap(),
        );
        let lens_at = (root as i64 + wire::BLOCK_TX_LENGTHS as i64 + lens_rel as i64) as usize;
        let shorter = (MIN_TX_WIRE + 4) as u32;
        wire[lens_at..lens_at + 4].copy_from_slice(&shorter.to_le_bytes());
        assert!(parse_block(&wire).is_err());
    }

    #[test]
    fn a_block_over_the_wire_bound_is_refused_before_it_is_parsed() {
        let huge = vec![0u8; MAX_BLOCK_SIZE + 1];
        assert!(matches!(parse_block(&huge), Err(Error::TooLarge { .. })));
    }

    #[test]
    fn bytes_that_are_not_zap_are_refused() {
        assert!(matches!(
            parse_block(b"not a message at all"),
            Err(Error::Zap(_))
        ));
        assert!(parse_tx_envelope(&[]).is_err());
    }

    #[test]
    fn the_transaction_wire_floor_is_the_header_plus_the_fixed_section() {
        assert_eq!(MIN_TX_WIRE, zap::HEADER_SIZE + TX_SIZE);
        assert_eq!(MIN_TX_WIRE, 64);
    }
}
