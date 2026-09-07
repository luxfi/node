// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What names a chain, a block, a transaction and an account.
//!
//! Three widths and one spelling. An [`Id`] is 32 bytes and names a chain, a
//! block or a transaction; a [`ShortId`] is 20 bytes and names an account; a
//! [`NodeId`] is a `ShortId` under another name and names a validator. All three
//! are written the same way — cb58, which is base58 over the bytes followed by
//! the last four of their SHA-256 — because that is how `luxfi/ids` writes them,
//! and a record this chain persists is read by clients that expect that word.
//!
//! The one exception is a native chain: the thirteen ids that are zero except
//! for a letter in their last byte print as that letter's fixed word rather than
//! as a base58 number. Go takes the same short cut in the same place
//! (`ids.NativeChainString`), and a port that skipped it would write a different
//! `chain_id` into every record F stores.

use sha2::{Digest, Sha256};

/// What names a chain, a block, a transaction. The host's own id type.
pub use lux_consensus::finality::Id;

/// The width of an account, in bytes.
pub const SHORT_ID_LEN: usize = 20;

/// What names an account. 20 bytes, the canonical Lux address.
pub type ShortId = [u8; SHORT_ID_LEN];

/// What names a validator. The same 20 bytes, printed with a prefix.
pub type NodeId = [u8; SHORT_ID_LEN];

/// The id no chain has.
pub const EMPTY: Id = [0u8; 32];

/// What a node id's printed form starts with.
pub const NODE_ID_PREFIX: &str = "NodeID-";

/// The byte a native chain is told apart by.
const NATIVE_CHAIN_LETTER_POS: usize = 31;

/// Thirty-two ones: what a native chain's word starts with.
const NATIVE_CHAIN_PREFIX: &str = "11111111111111111111111111111111";

/// cb58: base58 over the bytes with the last four of their SHA-256 appended.
pub fn cb58(bytes: &[u8]) -> String {
    let mut checked = Vec::with_capacity(bytes.len() + 4);
    checked.extend_from_slice(bytes);
    let sum = Sha256::digest(bytes);
    checked.extend_from_slice(&sum[sum.len() - 4..]);
    bs58::encode(&checked).into_string()
}

/// The inverse of [`cb58`]: the bytes, if the checksum agrees.
pub fn from_cb58(s: &str) -> Result<Vec<u8>, &'static str> {
    let decoded = bs58::decode(s).into_vec().map_err(|_| "base58 decoding error")?;
    if decoded.len() < 4 {
        return Err("input string is smaller than the checksum size");
    }
    let (raw, checksum) = decoded.split_at(decoded.len() - 4);
    let sum = Sha256::digest(raw);
    if checksum != &sum[sum.len() - 4..] {
        return Err("invalid input checksum");
    }
    Ok(raw.to_vec())
}

/// The fixed word a native chain prints as, if this is one.
pub fn native_chain_string(id: &Id) -> Option<String> {
    if id[..NATIVE_CHAIN_LETTER_POS].iter().any(|b| *b != 0) {
        return None;
    }
    let letter = id[NATIVE_CHAIN_LETTER_POS];
    match letter {
        b'P' | b'C' | b'X' | b'Q' | b'A' | b'B' | b'M' | b'F' | b'Z' | b'G' | b'I' | b'K'
        | b'D' => Some(format!("{}{}", NATIVE_CHAIN_PREFIX, letter as char)),
        _ => None,
    }
}

/// How an id is written down: its native word if it has one, else cb58.
pub fn id_string(id: &Id) -> String {
    native_chain_string(id).unwrap_or_else(|| cb58(id))
}

/// How an account is written down.
pub fn short_id_string(id: &ShortId) -> String {
    cb58(id)
}

/// How a validator is written down.
pub fn node_id_string(id: &NodeId) -> String {
    format!("{}{}", NODE_ID_PREFIX, cb58(id))
}

/// Reads back what [`short_id_string`] wrote.
pub fn short_id_from_string(s: &str) -> Result<ShortId, &'static str> {
    let raw = from_cb58(s)?;
    if raw.len() != SHORT_ID_LEN {
        return Err("wrong address length");
    }
    let mut out = [0u8; SHORT_ID_LEN];
    out.copy_from_slice(&raw);
    Ok(out)
}

/// Reads back what [`node_id_string`] wrote. The prefix is required: a bare
/// word is a different thing with the same digits.
pub fn node_id_from_string(s: &str) -> Result<NodeId, &'static str> {
    let body = s.strip_prefix(NODE_ID_PREFIX).ok_or("node id is missing its prefix")?;
    short_id_from_string(body)
}

/// Ascending byte order, which is what a committee is sorted by.
pub fn compare(a: &[u8], b: &[u8]) -> std::cmp::Ordering {
    a.cmp(b)
}

#[cfg(test)]
mod tests {
    use super::*;

    // The F-Chain's own vmID, in the encoding the node's plugin registry uses.
    // It is written out literally in the Go package's vmid_test.go, so it is the
    // one place this port can check its cb58 against a value it did not compute.
    #[test]
    fn cb58_writes_the_word_the_go_registry_reads() {
        let mut vm_id: Id = [0u8; 32];
        vm_id[..5].copy_from_slice(b"fhevm");
        assert_eq!(cb58(&vm_id), "n6sSsSfbpQBrU9sY4R29U6z8VrmnTo2CntW6da4rRS7qmnGdv");
        assert_eq!(from_cb58(&cb58(&vm_id)).unwrap(), vm_id.to_vec());
    }

    #[test]
    fn a_native_chain_prints_as_its_letter() {
        let mut f: Id = [0u8; 32];
        f[31] = b'F';
        assert_eq!(id_string(&f), "11111111111111111111111111111111F");
        // One bit anywhere else and it is an ordinary id again.
        let mut not: Id = f;
        not[0] = 1;
        assert_eq!(id_string(&not), cb58(&not));
    }

    #[test]
    fn a_bad_checksum_is_refused() {
        let word = cb58(&[1u8, 2, 3, 4]);
        let mut bytes = bs58::decode(&word).into_vec().unwrap();
        let last = bytes.len() - 1;
        bytes[last] ^= 0xff;
        assert!(from_cb58(&bs58::encode(&bytes).into_string()).is_err());
    }

    #[test]
    fn a_node_id_round_trips_through_its_prefix() {
        let n: NodeId = [7u8; SHORT_ID_LEN];
        let s = node_id_string(&n);
        assert!(s.starts_with(NODE_ID_PREFIX));
        assert_eq!(node_id_from_string(&s).unwrap(), n);
        assert!(node_id_from_string(&cb58(&n)).is_err());
    }
}
