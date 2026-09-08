// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The names on the O-chain's wire, and the two renderings they travel in.
//!
//! A 32-byte id is written as CB58 — base 58 over the bytes followed by the
//! last four bytes of their SHA-256 — EXCEPT for the thirteen ids that are
//! thirty-one zeros and a chain letter, which are written as an alias with no
//! checksum at all. Both forms are read back. A reader that knew only CB58
//! would refuse a block naming the P-chain as its parent; a writer that knew
//! only CB58 would derive a different id for one.
//!
//! A 20-byte node id is the same base with the word `NodeID-` in front.

use sha2::{Digest, Sha256};

pub type Id = [u8; 32];
pub type NodeId = [u8; 20];

pub const EMPTY: Id = [0u8; 32];

const ALIAS_PREFIX: &str = "11111111111111111111111111111111";
/// The thirteen letters that name a chain. The table is the chain's, and a
/// letter missing from it is an ordinary id that renders as CB58.
const ALIAS_LETTERS: &[u8] = b"PCXQABMFZGIKD";
pub const NODE_PREFIX: &str = "NodeID-";

/// cb58 is base 58 over `bytes ‖ sha256(bytes)[28..32]`.
pub fn cb58(bytes: &[u8]) -> String {
    let mut buf = bytes.to_vec();
    let sum = Sha256::digest(bytes);
    buf.extend_from_slice(&sum[28..32]);
    bs58::encode(buf).into_string()
}

pub fn cb58_decode(s: &str) -> Result<Vec<u8>, String> {
    let raw = bs58::decode(s)
        .into_vec()
        .map_err(|e| format!("couldn't decode ID to bytes: {e}"))?;
    if raw.len() < 4 {
        return Err("input is smaller than the checksum size".into());
    }
    let split = raw.len() - 4;
    let sum = Sha256::digest(&raw[..split]);
    if raw[split..] != sum[28..32] {
        return Err("invalid input checksum".into());
    }
    Ok(raw[..split].to_vec())
}

/// The alias for a native chain id, or None for every other id. Thirty-one
/// zero bytes and a letter from the table; anything else, including the
/// all-zero id, is not one.
pub fn native_chain_alias(id: &Id) -> Option<String> {
    if id[..31].iter().any(|b| *b != 0) {
        return None;
    }
    if ALIAS_LETTERS.contains(&id[31]) {
        return Some(format!("{ALIAS_PREFIX}{}", id[31] as char));
    }
    None
}

/// The inverse: the full alias, or the single letter in either case, which the
/// chain also accepts.
pub fn native_chain_from_str(s: &str) -> Option<Id> {
    let letter = if s.len() == 1 {
        s.as_bytes()[0].to_ascii_uppercase()
    } else if s.len() == ALIAS_PREFIX.len() + 1 && s.starts_with(ALIAS_PREFIX) {
        s.as_bytes()[ALIAS_PREFIX.len()]
    } else {
        return None;
    };
    if !ALIAS_LETTERS.contains(&letter) {
        return None;
    }
    let mut id = EMPTY;
    id[31] = letter;
    Some(id)
}

pub fn id_string(id: &Id) -> String {
    match native_chain_alias(id) {
        Some(a) => a,
        None => cb58(id),
    }
}

pub fn id_from_str(s: &str) -> Result<Id, String> {
    // The empty string is the empty id, and is not an error.
    if s.is_empty() {
        return Ok(EMPTY);
    }
    if let Some(id) = native_chain_from_str(s) {
        return Ok(id);
    }
    let raw = cb58_decode(s)?;
    if raw.len() != 32 {
        return Err(format!("expected 32 bytes but got {}", raw.len()));
    }
    let mut id = EMPTY;
    id.copy_from_slice(&raw);
    Ok(id)
}

pub fn node_id_string(n: &NodeId) -> String {
    format!("{NODE_PREFIX}{}", cb58(n))
}

pub fn node_id_from_str(s: &str) -> Result<NodeId, String> {
    if s.is_empty() {
        return Ok([0u8; 20]);
    }
    let rest = s
        .strip_prefix(NODE_PREFIX)
        .ok_or_else(|| format!("ID: {s} is missing the prefix: {NODE_PREFIX}"))?;
    let raw = cb58_decode(rest)?;
    if raw.len() != 20 {
        return Err(format!("expected 20 bytes but got {}", raw.len()));
    }
    let mut n = [0u8; 20];
    n.copy_from_slice(&raw);
    Ok(n)
}

pub fn sha256(data: &[u8]) -> Id {
    let mut out = EMPTY;
    out.copy_from_slice(&Sha256::digest(data));
    out
}
