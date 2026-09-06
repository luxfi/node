// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What this chain names things with.
//!
//! [`Id`] is 32 bytes — a block, a vertex, a transaction, an asset, a chain. It
//! is not this crate's type: it is `lux_consensus::finality::Id`, the one the
//! host seam and the finality rule are already written in. A chain that minted
//! its own 32-byte name would have to be translated at every call the node
//! makes, and a node holding two chains that each did so could not put them in
//! one map.
//!
//! `Id` being an array rather than a newtype is why the operations on one are
//! free functions here rather than methods. In particular there is no
//! `is_empty` METHOD on an id — `[u8; 32]` derefs to a slice, whose inherent
//! `is_empty` answers "does this array have zero elements", which is always
//! false and would be a silently wrong answer to "is this the empty id".

pub use lux_consensus::finality::Id;

/// Bytes in an [`Id`].
pub const ID_LEN: usize = 32;

/// The all-zero id. Genesis names it as its parent, and nothing else
/// legitimately carries it — which is what makes "height 0 with a parent" a
/// rule that can be stated at all.
pub const EMPTY: Id = [0u8; ID_LEN];

/// Whether this is the all-zero id.
pub fn is_empty(id: &Id) -> bool {
    *id == EMPTY
}

/// An id of one byte repeated. The shape a corpus writes as `id(40)`.
pub fn repeated(b: u8) -> Id {
    [b; ID_LEN]
}

/// The id of a slice, zero-filling a short one and taking the first 32 bytes of
/// a long one. This is `copy(id[:], b)`, which is how the reference reads an id
/// out of a fixed slot.
pub fn from_slice(b: &[u8]) -> Id {
    let mut out = EMPTY;
    let n = b.len().min(ID_LEN);
    out[..n].copy_from_slice(&b[..n]);
    out
}

/// Lowercase hex, all 32 bytes. What a verdict line carries.
pub fn hex(id: &Id) -> String {
    hex_of(&id[..])
}

/// Lowercase hex of any bytes.
pub fn hex_of(b: &[u8]) -> String {
    let mut out = String::with_capacity(b.len() * 2);
    for x in b {
        out.push(char::from_digit((x >> 4) as u32, 16).unwrap());
        out.push(char::from_digit((x & 0x0f) as u32, 16).unwrap());
    }
    out
}

/// The first four bytes, for a log line that names a block without spelling it.
pub fn short(id: &Id) -> String {
    hex_of(&id[..4])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_id_of_a_repeated_byte_is_that_byte_thirty_two_times() {
        let id = repeated(0x28);
        assert_eq!(id.len(), ID_LEN);
        assert!(id.iter().all(|b| *b == 0x28));
        assert_eq!(
            hex(&id),
            "2828282828282828282828282828282828282828282828282828282828282828"
        );
    }

    #[test]
    fn only_the_all_zero_id_is_empty() {
        assert!(is_empty(&EMPTY));
        assert!(!is_empty(&repeated(1)));
    }

    #[test]
    fn a_short_slice_zero_fills_and_a_long_one_is_cut() {
        assert_eq!(from_slice(&[1, 2, 3])[..3], [1, 2, 3]);
        assert!(from_slice(&[1, 2, 3])[3..].iter().all(|b| *b == 0));
        assert_eq!(from_slice(&[7u8; 40]), repeated(7));
    }

    #[test]
    fn short_names_the_first_four_bytes() {
        assert_eq!(short(&from_slice(&[0xde, 0xad, 0xbe, 0xef, 0x99])), "deadbeef");
    }
}
