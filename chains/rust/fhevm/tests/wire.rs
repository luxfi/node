// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The wire: what is transmitted, what is signed, and the rule that keeps the two
//! from coming apart.

mod common;

use common::*;

use lux_fhevm::block::Block;
use lux_fhevm::ids::{self, Id};
use lux_fhevm::transaction::{Transaction, TX_GRANT_PERMIT, TX_REVOKE_PERMIT};
use lux_fhevm::wire::{self, MAX_BLOCK_SIZE};
use lux_fhevm::zap;

fn other_chain() -> Id {
    let mut id = ids::EMPTY;
    id[..5].copy_from_slice(b"other");
    id
}

/// Bytes → parse is a faithful, idempotent round trip, and the id is the content
/// hash of the canonical wire.
#[test]
fn the_wire_round_trips_and_the_id_is_the_hash_of_it() {
    let tx = sample_tx();
    let data = tx.bytes();

    let parsed = wire::parse_transaction(&data).expect("the canonical form parses");
    assert_eq!(parsed.bytes(), data, "re-serialization is not canonical");
    assert_eq!(parsed.id(), <[u8; 32]>::from(sha2::Sha256::digest(&data)));
    assert_eq!(parsed, tx, "a field changed across the seam");
}

use sha2::Digest;

/// What is authenticated and what is transmitted cannot come apart: the content
/// object is a genuine byte-prefix of the wire, the length field names exactly its
/// length, and the signed preimage ENDS with that same prefix. So the only thing
/// the signature covers that the wire does not carry is the chain id, which the
/// verifier supplies.
#[test]
fn the_content_is_the_prefix_the_signature_covers() {
    let tx = sample_tx();
    let content = tx.content();
    let full = tx.bytes();
    assert!(full.starts_with(&content), "content is not a prefix of the wire");

    let n = wire::zap_len(&full).expect("the length field reads");
    assert_eq!(n, content.len(), "the length field names something else");

    let pre = tx.signing_bytes(&test_chain_id());
    assert!(pre.ends_with(&content), "the preimage does not end with the transmitted content");
    assert_eq!(pre.len(), wire::TX_DOMAIN.len() + 32 + content.len());
}

/// A preimage names ONE chain. Without it, a transaction signed on one F-Chain
/// authenticates on every other — the payer's address is the hash of its public
/// key, so the same account exists on all of them, and the replay burns a balance
/// there for an operation nobody asked for.
#[test]
fn the_signed_preimage_binds_the_chain_and_is_domain_separated() {
    let tx = sample_tx();
    assert_ne!(
        tx.signing_bytes(&test_chain_id()),
        tx.signing_bytes(&other_chain()),
        "two chains produced one preimage: a signature would replay across them"
    );
    assert!(tx.signing_bytes(&test_chain_id()).starts_with(wire::TX_DOMAIN));
}

/// The preimage changes when ANY semantically meaningful field changes —
/// including the subject, which names the object the operation acts on. A field
/// the signature did not cover could be swapped in flight.
#[test]
fn the_signed_preimage_binds_every_field_but_the_signature_itself() {
    let chain = test_chain_id();
    let base = sample_tx().signing_bytes(&chain);

    type Mutation = (&'static str, fn(&mut Transaction));
    let mutations: Vec<Mutation> = vec![
        ("type", |tx| tx.tx_type = TX_GRANT_PERMIT),
        ("scheme", |tx| tx.scheme = b"bfv-n13".to_vec()),
        ("payer", |tx| tx.payer[0] ^= 0xff),
        ("subject", |tx| tx.subject[0] ^= 0xff),
        ("gas limit", |tx| tx.gas_limit += 1),
        ("nonce", |tx| tx.nonce += 1),
        ("payload", |tx| tx.payload.push(b'x')),
    ];
    for (name, mutate) in mutations {
        let mut tx = sample_tx();
        mutate(&mut tx);
        assert_ne!(tx.signing_bytes(&chain), base, "changing the {name} left the preimage alone");
    }

    // Auth and sig are deliberately EXCLUDED: the signature cannot cover itself.
    let mut tx = sample_tx();
    tx.auth = b"a-different-public-key".to_vec();
    tx.sig = b"a-different-signature".to_vec();
    assert_eq!(tx.signing_bytes(&chain), base, "auth and sig must not appear in the preimage");
}

/// The canonical gate. zap follows the root offset and ignores unreferenced
/// padding inside a message's declared size, so a twin buffer can decode to
/// identical fields yet hash differently. Parse must refuse any input that is not
/// already byte-equal to its re-serialized form — exactly one byte-string
/// authenticates per logical transaction.
#[test]
fn a_padded_twin_is_refused_so_there_is_no_second_encoding() {
    let tx = sample_tx();
    let data = tx.bytes();
    let n = wire::zap_len(&data).expect("the length field reads");

    // Append eight unreferenced bytes to the trailing object and bump its declared
    // size, so a naive trailing-bytes check still passes. The root does not
    // reference the padding, so the fields decode identically.
    let mut sig = data[n..].to_vec();
    sig.extend_from_slice(&[0u8; 8]);
    let len = sig.len() as u32;
    sig[12..16].copy_from_slice(&len.to_le_bytes());
    let mut twin = data[..n].to_vec();
    twin.extend_from_slice(&sig);

    assert_ne!(twin, data, "the twin construction failed to differ");
    assert!(
        wire::parse_transaction(&twin).is_err(),
        "a padded twin was accepted: id-malleability is not closed"
    );
}

#[test]
fn a_truncated_buffer_is_refused_rather_than_read_past() {
    let data = sample_tx().bytes();
    for cut in [0usize, 4, zap::HEADER_SIZE, data.len() - 1] {
        assert!(
            wire::parse_transaction(&data[..cut]).is_err(),
            "a buffer cut to {cut} bytes was accepted"
        );
    }
}

#[test]
fn bytes_after_a_complete_transaction_are_refused_rather_than_ignored() {
    let mut data = sample_tx().bytes();
    data.extend_from_slice(&[0xde, 0xad]);
    assert!(wire::parse_transaction(&data).is_err());
}

/// An absent field comes back empty rather than as something, so a
/// re-serialization of what was parsed is byte-identical to what arrived — which
/// is the whole canonical rule.
#[test]
fn a_transaction_with_nothing_in_its_variable_fields_round_trips() {
    let bare = Transaction { tx_type: TX_REVOKE_PERMIT, nonce: 1, ..Transaction::default() };
    let again = wire::parse_transaction(&bare.bytes()).expect("a bare transaction round-trips");
    assert!(again.scheme.is_empty());
    assert!(again.payload.is_empty());
    assert!(again.auth.is_empty());
    assert!(again.sig.is_empty());
    assert_eq!(again, bare);
}

// ---- blocks ----------------------------------------------------------------

fn wire_block(txs: Vec<Transaction>, parent: u8, height: u64, ts: i64) -> Block {
    let mut parent_id = ids::EMPTY;
    parent_id[0] = parent;
    let mut blk = Block {
        id: ids::EMPTY,
        parent_id,
        height,
        timestamp: time_at(ts),
        transactions: txs,
    };
    blk.id = blk.compute_id(&test_chain_id());
    blk
}

#[test]
fn a_block_serializes_and_re_parses_with_its_transactions_intact() {
    let a = sample_tx();
    let mut b = sample_tx();
    b.nonce = 43;
    b.payload = b"second".to_vec();
    let blk = wire_block(vec![a.clone(), b.clone()], 1, 7, 1_700_000_000);

    let parsed = wire::parse_block(&test_chain_id(), &blk.bytes()).expect("the block parses");
    assert_eq!(parsed.id, blk.id, "the block id changed across the wire");
    assert_eq!(parsed.height, blk.height);
    assert_eq!(parsed.parent_id, blk.parent_id);
    assert_eq!(parsed.timestamp, blk.timestamp);
    assert_eq!(parsed.transactions.len(), 2);
    assert_eq!(parsed.transactions[0].id(), a.id());
    assert_eq!(parsed.transactions[1].id(), b.id());
}

#[test]
fn bytes_after_a_complete_block_are_refused() {
    let blk = wire_block(Vec::new(), 1, 1, 1);
    let mut raw = blk.bytes();
    raw.push(0xff);
    assert!(wire::parse_block(&test_chain_id(), &raw).is_err());
}

/// The same block bytes name DIFFERENT blocks on different chains — and, the case
/// that matters most, two chains sharing a genesis timestamp do not share a
/// genesis id. They did: the id hashed parent, height, time and transactions, none
/// of which distinguishes one F-Chain from another, so every chain's height-0
/// block had the same name and a block built on one resolved its parent on all of
/// them.
#[test]
fn a_block_id_binds_the_chain_it_belongs_to() {
    let genesis = Block {
        id: ids::EMPTY,
        parent_id: ids::EMPTY,
        height: 0,
        timestamp: time_at(TEST_GENESIS_TIME),
        transactions: Vec::new(),
    };
    let mine = genesis.compute_id(&test_chain_id());
    assert_ne!(mine, genesis.compute_id(&other_chain()), "two chains share a genesis id");
    assert_eq!(mine, genesis.compute_id(&test_chain_id()), "an id is a function of its content");
}

/// The relation selection depends on: the running total — an empty block plus
/// each transaction's wire length plus the per-entry cost — is never LESS than
/// what the block actually serializes to. If it could understate, a proposer would
/// select its way past the size bound and build a block its own verification
/// refuses, which is the halt-shaped bug of a builder and a checker that disagree.
///
/// Payload lengths vary per step so the eight-byte alignment the per-entry cost
/// covers is exercised at every offset, not only where the numbers happen to
/// divide.
#[test]
fn the_selection_budget_never_understates_the_block_it_describes() {
    let mut txs: Vec<Transaction> = Vec::new();
    let mut budget = wire::empty_block_size();
    for n in 0..=24u64 {
        let blk = wire_block(txs.clone(), 7, n, 1);
        let actual = blk.bytes().len();
        assert!(budget >= actual, "{n} txs: budget {budget} understates {actual} bytes");
        let mut tx = sample_tx();
        tx.nonce = n;
        tx.payload = vec![n as u8; n as usize];
        budget += tx.bytes().len() + wire::TX_ENTRY;
        txs.push(tx);
    }
}

/// What a peer can send that is not a block. Each is refused with an error rather
/// than a partial decode, and none of them costs a signature check.
#[test]
fn every_malformed_block_is_refused() {
    let chain = test_chain_id();
    let tx = sample_tx();
    let blk = wire_block(vec![tx.clone()], 3, 4, 1_700_000_000);
    let sound = blk.bytes();

    // A header that is not a zap message at all.
    let mut garbage = vec![0u8; zap::HEADER_SIZE + 16];
    let len = garbage.len() as u32;
    garbage[12..16].copy_from_slice(&len.to_le_bytes());
    assert!(wire::parse_block(&chain, &garbage).is_err(), "a message that decodes to nothing");

    // A length entry that runs past the blob it indexes.
    let blob = tx.bytes();
    let over = blk.bytes_with_tx_lens(&[(blob.len() + 64) as u32], &blob);
    assert!(wire::parse_block(&chain, &over).is_err(), "a length running past the blob");

    // A length entry that stops short, so the transaction under it does not decode.
    let short = blk.bytes_with_tx_lens(&[(blob.len() - 8) as u32], &blob);
    assert!(wire::parse_block(&chain, &short).is_err(), "a truncated transaction");

    // A block over the size bound is refused before it is decoded at all.
    assert!(wire::parse_block(&chain, &vec![0u8; MAX_BLOCK_SIZE + 1]).is_err());

    // The control: the sound encoding parses and names the same block.
    let got = wire::parse_block(&chain, &sound).expect("the control parses");
    assert_eq!(got.id, blk.compute_id(&chain));
}

/// A block has exactly one encoding. zap ignores padding the root does not
/// reference, so without this a block has as many byte-strings as an attacker
/// cares to make, and one id.
#[test]
fn a_padded_block_is_refused_so_a_block_has_one_encoding() {
    let chain = test_chain_id();
    let blk = wire_block(vec![sample_tx()], 5, 2, 1_700_000_000);
    let data = blk.bytes();

    let mut padded = data.clone();
    padded.extend_from_slice(&[0u8; 16]);
    let len = padded.len() as u32;
    padded[12..16].copy_from_slice(&len.to_le_bytes());
    assert_ne!(padded, data);
    assert!(wire::parse_block(&chain, &padded).is_err(), "a block with two encodings");
}

/// The seam between a transaction's two concatenated messages: the trailing
/// auth/sig object must itself be a message.
#[test]
fn a_trailing_object_that_is_not_a_message_is_refused() {
    let data = sample_tx().bytes();
    let n = wire::zap_len(&data).expect("the length field reads");
    let mut broken = data[..n].to_vec();
    broken.extend_from_slice(&[0u8; zap::HEADER_SIZE]);
    let size = zap::HEADER_SIZE as u32;
    broken[n + 12..n + 16].copy_from_slice(&size.to_le_bytes());
    assert!(wire::parse_transaction(&broken).is_err());
}

/// The second of a transaction's two messages must actually be there. A declared
/// length covering the whole buffer leaves nothing for the auth/sig object, and an
/// empty remainder is not a message.
#[test]
fn a_transaction_with_no_auth_object_is_refused() {
    let mut data = sample_tx().bytes();
    let len = data.len() as u32;
    data[12..16].copy_from_slice(&len.to_le_bytes());
    assert!(wire::parse_transaction(&data).is_err());
}

/// The leading message is held to more than its declared length. The length field
/// alone can name a plausible size on a buffer that is not a message at all — a
/// wrong magic, or a wire version this build does not speak.
#[test]
fn a_content_object_that_is_not_one_is_refused() {
    let sound = sample_tx().bytes();
    type Corruption = (&'static str, fn(&mut [u8]));
    let corruptions: Vec<Corruption> = vec![
        ("a magic this is not", |b| b[0] ^= 0xff),
        ("a version we do not speak", |b| b[4..6].copy_from_slice(&0xbeefu16.to_le_bytes())),
    ];
    for (name, corrupt) in corruptions {
        let mut data = sound.clone();
        corrupt(&mut data);
        assert!(wire::zap_len(&data).is_ok(), "{name}: the length field is still sound");
        assert!(wire::parse_transaction(&data).is_err(), "{name}: a non-message was accepted");
    }
    assert!(wire::parse_transaction(&sound).is_ok(), "the control failed");
}

/// A fixed-width field read past the end of its message copies NOTHING, leaving
/// the destination zeroed — the shape that silently turns a short signature into a
/// well-formed unverifiable envelope. Here it cannot pass: a zeroed field
/// re-serializes to bytes that are not the bytes that arrived, and the canonical
/// rule refuses the difference.
#[test]
fn a_truncated_fixed_field_is_refused_not_zero_filled() {
    let sound = sample_tx().bytes();
    let n = wire::zap_len(&sound).expect("the length field reads");

    for cut in [wire::TX_PAYER + 4, wire::TX_SUBJECT + 4, wire::TX_SUBJECT + 20] {
        let mut truncated = sound.clone();
        let size = zap::HEADER_SIZE + cut;
        assert!(size < n, "the cut must fall inside the message");
        truncated[12..16].copy_from_slice(&(size as u32).to_le_bytes());
        assert!(wire::zap_len(&truncated).is_ok(), "cut {cut}: the length field is sound");
        assert!(
            wire::parse_transaction(&truncated).is_err(),
            "cut {cut}: a field read past the end must refuse, not zero-fill"
        );
    }

    let parsed = wire::parse_transaction(&sound).expect("the control parses");
    assert_eq!(parsed.payer, sample_tx().payer);
    assert_eq!(parsed.subject, sample_tx().subject);
}
