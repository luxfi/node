// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the chain names things with.
//!
//! [`Id`] is 32 bytes — a block, a transaction, a chain. It is not this crate's
//! type: it is `lux_consensus::finality::Id`, the one the host seam and the
//! finality rule are already written in. A chain that minted its own 32-byte
//! name would have to be translated at every call the node makes, and a node
//! holding two chains that each did so could not put them in one map.
//!
//! `Id` being an array rather than a newtype is why the operations on one are
//! free functions here rather than methods. In particular there is no
//! `is_empty` METHOD on an id — `[u8; 32]` derefs to a slice, whose inherent
//! `is_empty` answers "does this array have zero elements", which is always
//! false and would be a silently wrong answer to "is this the empty id".

use sha2::{Digest, Sha256};

pub use lux_consensus::finality::Id;

/// Bytes in an [`Id`].
pub const ID_LEN: usize = 32;

/// The all-zero id. Genesis names it as its parent, and nothing else
/// legitimately holds it.
pub const EMPTY: Id = [0u8; ID_LEN];

/// The content id of a byte string: SHA-256 over it.
///
/// One derivation, used by both a block and a transaction, because a second one
/// would be a second answer to "which block is this".
pub fn hash(bytes: &[u8]) -> Id {
    let mut h = Sha256::new();
    h.update(bytes);
    let out = h.finalize();
    let mut id = EMPTY;
    id.copy_from_slice(&out);
    id
}

/// An id from however many leading bytes are given, zero-filled — the shape the
/// Go tests write as `ids.ID{5, 4, 3}`.
pub fn prefixed(b: &[u8]) -> Id {
    let mut out = EMPTY;
    let n = b.len().min(ID_LEN);
    out[..n].copy_from_slice(&b[..n]);
    out
}

/// An id whose every byte is `b` — the shape the corpus generator writes as
/// `id(30)`.
pub fn filled(b: u8) -> Id {
    [b; ID_LEN]
}

/// The id of a slice, or `None` if it is not 32 bytes.
///
/// A short read is a different fact from the empty id, and answering the empty
/// id for both is how one failed read looks like a chain that never started.
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

/// Lowercase hex, for a log line or an RPC answer.
pub fn hex(id: &Id) -> String {
    let mut s = String::with_capacity(ID_LEN * 2);
    for b in id {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

/// An id back from the hex [`hex`] wrote, or `None`.
pub fn from_hex(s: &str) -> Option<Id> {
    if s.len() != ID_LEN * 2 {
        return None;
    }
    let mut out = EMPTY;
    for i in 0..ID_LEN {
        out[i] = u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok()?;
    }
    Some(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_id_is_the_host_seam_s_id() {
        // Not "the same shape as" — the same type. A block this chain builds is
        // named the way the node that certifies it names it.
        fn host_id(x: lux_node::vm::Id) -> Id {
            x
        }
        assert_eq!(host_id(EMPTY), EMPTY);
    }

    #[test]
    fn an_empty_id_is_not_an_empty_slice() {
        assert!(is_empty(&EMPTY));
        assert!(!is_empty(&filled(1)));
    }

    #[test]
    fn hex_round_trips() {
        let id = hash(b"quantum");
        assert_eq!(from_hex(&hex(&id)), Some(id));
        assert_eq!(from_hex("beef"), None);
    }

    #[test]
    fn a_short_read_is_not_the_empty_id() {
        assert_eq!(from_slice(&[0u8; 31]), None);
        assert_eq!(from_slice(&[0u8; 32]), Some(EMPTY));
    }
}
