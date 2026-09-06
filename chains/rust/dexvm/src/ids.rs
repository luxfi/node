// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the chain names things with, and the one hash that derives a name.
//!
//! [`Id`] is 32 bytes — an asset, a market, a source chain. It is the same
//! shape the atomic objects on the other chains already carry
//! (`AtomicInput.Asset`, `AtomicOutput.Asset`), so a registered AssetID is
//! directly comparable to the asset a real cross-chain object names. It is
//! never a string ticker.
//!
//! `Id` is an array rather than a newtype, so the operations on one are free
//! functions here. In particular there is no `is_empty` METHOD: `[u8; 32]`
//! derefs to a slice whose inherent `is_empty` answers "does this array have
//! zero elements", which is always false and would silently answer a different
//! question.

use sha2::{Digest, Sha256};

/// A 32-byte name.
pub type Id = [u8; ID_LEN];

/// Bytes in an [`Id`].
pub const ID_LEN: usize = 32;

/// The all-zero id. An asset whose source chain is this one has named no
/// chain, which is the one thing [`crate::asset::derive_asset_id`] refuses
/// before it looks at anything else.
pub const EMPTY: Id = [0u8; ID_LEN];

/// SHA-256 over the bytes. The chain's one hash: a second would be a second
/// answer to "which asset is this".
pub fn sha256(bytes: &[u8]) -> Id {
    let mut h = Sha256::new();
    h.update(bytes);
    let out = h.finalize();
    let mut id = EMPTY;
    id.copy_from_slice(&out);
    id
}

/// An id whose every byte is `b` — the shape the corpus generator writes as
/// `id(3)` for the C-Chain and `id(2)` for the X-Chain.
pub fn filled(b: u8) -> Id {
    [b; ID_LEN]
}

/// An id from a slice, or `None` if it is not 32 bytes.
///
/// A short read is a different fact from the empty id, and answering the empty
/// id for both is how a truncated reference becomes "this asset names no
/// chain".
pub fn from_slice(b: &[u8]) -> Option<Id> {
    if b.len() != ID_LEN {
        return None;
    }
    let mut out = EMPTY;
    out.copy_from_slice(b);
    Some(out)
}

/// Whether this is the all-zero id.
pub fn is_empty(id: &Id) -> bool {
    *id == EMPTY
}

/// Lowercase hex with no prefix — Go's `hex.EncodeToString`, and `ids.ID.Hex`
/// for an id.
pub fn hex(b: &[u8]) -> String {
    const DIGIT: [char; 16] = [
        '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f',
    ];
    let mut s = String::with_capacity(b.len() * 2);
    for byte in b {
        s.push(DIGIT[(byte >> 4) as usize]);
        s.push(DIGIT[(byte & 0xF) as usize]);
    }
    s
}

/// The bytes back from the hex [`hex`] wrote, or `None`.
pub fn unhex(s: &str) -> Option<Vec<u8>> {
    if !s.len().is_multiple_of(2) {
        return None;
    }
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_one_hash_is_sha256() {
        // The empty string's SHA-256, so the primitive is pinned to a value and
        // not to whatever the crate happens to compute.
        assert_eq!(
            hex(&sha256(b"")),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }

    #[test]
    fn an_empty_id_is_not_an_empty_slice() {
        assert!(is_empty(&EMPTY));
        assert!(!is_empty(&filled(1)));
    }

    #[test]
    fn a_short_read_is_not_the_empty_id() {
        assert_eq!(from_slice(&[0u8; 31]), None);
        assert_eq!(from_slice(&[0u8; 32]), Some(EMPTY));
    }

    #[test]
    fn hex_round_trips_and_refuses_what_is_not_hex() {
        let id = sha256(b"asset");
        assert_eq!(unhex(&hex(&id)).as_deref(), Some(&id[..]));
        assert_eq!(unhex("abc"), None);
        assert_eq!(unhex("zz"), None);
        assert_eq!(unhex(""), Some(Vec::new()));
    }
}
