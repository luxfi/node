// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The two identifiers the chain names things with.
//!
//! [`Id`] is 32 bytes — a transaction, an asset, a chain, a block. It is not
//! this crate's type: it is `lux_consensus::finality::Id`, the one the host
//! seam and the finality rule are already written in. A chain that minted its
//! own 32-byte name would have to be translated at every call the node makes,
//! and a node holding two chains that each did so could not put them in one
//! map.
//!
//! [`ShortId`] is 20 — an address. That one IS this crate's, because it is not
//! a name consensus ever sees: a certificate names blocks, never owners.
//!
//! Both order lexicographically, because every sortedness rule in this chain is
//! stated over their bytes. For `Id` that ordering is the array's own.
//!
//! `Id` being an array rather than a newtype is why the operations on one are
//! free functions here rather than methods: [`prefixed`], [`from_slice`],
//! [`prefix`], [`is_empty`], [`hex`]. In particular there is no `is_empty`
//! METHOD on an id — `[u8; 32]` derefs to a slice, whose inherent `is_empty`
//! answers "does this array have zero elements", which is always false and
//! would be a silently wrong answer to "is this the empty id".

use crate::hash::sha256;

pub use lux_consensus::finality::Id;

/// Bytes in an [`Id`].
pub const ID_LEN: usize = 32;
/// Bytes in a [`ShortId`].
pub const SHORT_ID_LEN: usize = 20;

/// The all-zero id. An asset, a chain or a transaction never legitimately has
/// it, which is why several checks are written against it.
pub const EMPTY: Id = [0u8; ID_LEN];

/// An id from however many leading bytes are given, zero-filled. The shape the
/// Go tests write as `ids.ID{5, 4, 3, 2, 1}`.
pub fn prefixed(b: &[u8]) -> Id {
    let mut out = EMPTY;
    let n = b.len().min(ID_LEN);
    out[..n].copy_from_slice(&b[..n]);
    out
}

/// The id of a slice, or `None` if it is not 32 bytes.
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

/// Hash an id under a list of big-endian u64 prefixes:
/// `sha256(p0 ‖ p1 ‖ … ‖ id)`.
///
/// This is how a UTXO gets an identity of its own — the id of the transaction
/// that produced it, prefixed by the output index — so two outputs of one
/// transaction are two different things to spend.
pub fn prefix(id: &Id, prefixes: &[u64]) -> Id {
    let mut buf = Vec::with_capacity(prefixes.len() * 8 + ID_LEN);
    for p in prefixes {
        buf.extend_from_slice(&p.to_be_bytes());
    }
    buf.extend_from_slice(id);
    sha256(&buf)
}

/// An id written out, which is how every id in this crate is shown to a caller.
pub fn hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2);
    for x in b {
        s.push_str(&format!("{x:02x}"));
    }
    s
}

/// A 20-byte address.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ShortId(pub [u8; SHORT_ID_LEN]);

/// The all-zero address — a phantom signer, refused wherever an owner set is
/// checked. It is also the address the strict-post-quantum gate is asked about
/// on this chain; see [`crate::security`].
pub const SHORT_EMPTY: ShortId = ShortId([0u8; SHORT_ID_LEN]);

impl ShortId {
    pub const fn from_bytes(b: [u8; SHORT_ID_LEN]) -> Self {
        ShortId(b)
    }

    pub fn from_slice(b: &[u8]) -> Option<Self> {
        if b.len() != SHORT_ID_LEN {
            return None;
        }
        let mut out = [0u8; SHORT_ID_LEN];
        out.copy_from_slice(b);
        Some(ShortId(out))
    }

    pub fn prefixed_bytes(b: &[u8]) -> Self {
        let mut out = [0u8; SHORT_ID_LEN];
        let n = b.len().min(SHORT_ID_LEN);
        out[..n].copy_from_slice(&b[..n]);
        ShortId(out)
    }

    pub fn as_bytes(&self) -> &[u8; SHORT_ID_LEN] {
        &self.0
    }

    pub fn is_empty(&self) -> bool {
        self.0 == SHORT_EMPTY.0
    }
}

impl std::fmt::Debug for ShortId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&hex(&self.0))
    }
}

impl std::fmt::Display for ShortId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Debug::fmt(self, f)
    }
}

/// Whether a slice is strictly increasing — sorted AND free of duplicates.
///
/// The chain asks this of inputs, of signature indices and of owner addresses.
/// Sorted-and-unique is not a tidiness rule: it is what makes one transaction
/// have one encoding, so a second encoding of the same spend cannot exist.
pub fn is_sorted_and_unique<T: Ord>(items: &[T]) -> bool {
    items.windows(2).all(|w| w[0] < w[1])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_id_is_the_hosts_own_id_type() {
        // Not "the same shape as": the same type. This is what lets `impl
        // host::Vm for Xvm` name a block id with nothing in between.
        fn host_id(x: lux_node::vm::Id) -> Id {
            x
        }
        assert_eq!(host_id(prefixed(&[1])), prefixed(&[1]));
        assert_eq!(std::mem::size_of::<Id>(), ID_LEN);
    }

    #[test]
    fn prefix_is_the_sha256_of_the_big_endian_prefixes_then_the_id() {
        let id = prefixed(&[1, 2, 3]);
        let mut want_buf = Vec::new();
        want_buf.extend_from_slice(&7u64.to_be_bytes());
        want_buf.extend_from_slice(&id);
        assert_eq!(prefix(&id, &[7]), sha256(&want_buf));
    }

    #[test]
    fn two_outputs_of_one_transaction_are_two_different_things_to_spend() {
        let tx = prefixed(&[9]);
        assert_ne!(prefix(&tx, &[0]), prefix(&tx, &[1]));
    }

    #[test]
    fn ids_order_lexicographically() {
        assert!(prefixed(&[1]) < prefixed(&[2]));
        assert!(ShortId::prefixed_bytes(&[1]) < ShortId::prefixed_bytes(&[2]));
    }

    #[test]
    fn the_empty_id_is_the_all_zero_one_and_nothing_else() {
        assert!(is_empty(&EMPTY));
        assert!(!is_empty(&prefixed(&[1])));
        // The trap this module exists to keep out of the crate: the slice
        // method answers about the LENGTH, which is never zero.
        #[allow(clippy::const_is_empty)]
        {
            assert!(!EMPTY.is_empty(), "the slice method is not the id check");
        }
    }

    #[test]
    fn an_id_reads_back_from_exactly_thirty_two_bytes() {
        let id = prefixed(&[7, 7]);
        assert_eq!(from_slice(&id), Some(id));
        assert_eq!(from_slice(&id[..31]), None);
        assert_eq!(from_slice(&[0u8; 33]), None);
    }

    #[test]
    fn sorted_and_unique_rejects_a_repeat() {
        assert!(is_sorted_and_unique(&[1u32, 2, 3]));
        assert!(!is_sorted_and_unique(&[1u32, 1]));
        assert!(!is_sorted_and_unique(&[2u32, 1]));
        assert!(is_sorted_and_unique::<u32>(&[]));
    }
}
