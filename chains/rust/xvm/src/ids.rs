// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The two identifiers the chain names things with.
//!
//! [`Id`] is 32 bytes — a transaction, an asset, a chain, a block. [`ShortId`]
//! is 20 — an address. Both order lexicographically, because every sortedness
//! rule in this chain is stated over their bytes.

use crate::hash::sha256;

/// Bytes in an [`Id`].
pub const ID_LEN: usize = 32;
/// Bytes in a [`ShortId`].
pub const SHORT_ID_LEN: usize = 20;

/// A 32-byte identifier.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct Id(pub [u8; ID_LEN]);

/// The all-zero id. An asset, a chain or a transaction never legitimately has
/// it, which is why several checks are written against it.
pub const EMPTY: Id = Id([0u8; ID_LEN]);

impl Id {
    pub const fn from_bytes(b: [u8; ID_LEN]) -> Self {
        Id(b)
    }

    /// The id of a slice, or `None` if it is not 32 bytes.
    pub fn from_slice(b: &[u8]) -> Option<Self> {
        if b.len() != ID_LEN {
            return None;
        }
        let mut out = [0u8; ID_LEN];
        out.copy_from_slice(b);
        Some(Id(out))
    }

    /// An id from however many leading bytes are given, zero-filled. The shape
    /// the Go tests write as `ids.ID{5, 4, 3, 2, 1}`.
    pub fn prefixed_bytes(b: &[u8]) -> Self {
        let mut out = [0u8; ID_LEN];
        let n = b.len().min(ID_LEN);
        out[..n].copy_from_slice(&b[..n]);
        Id(out)
    }

    pub fn as_bytes(&self) -> &[u8; ID_LEN] {
        &self.0
    }

    pub fn is_empty(&self) -> bool {
        self.0 == EMPTY.0
    }

    /// Hash this id under a list of big-endian u64 prefixes:
    /// `sha256(p0 ‖ p1 ‖ … ‖ id)`.
    ///
    /// This is how a UTXO gets an identity of its own — the id of the
    /// transaction that produced it, prefixed by the output index — so two
    /// outputs of one transaction are two different things to spend.
    pub fn prefix(&self, prefixes: &[u64]) -> Id {
        let mut buf = Vec::with_capacity(prefixes.len() * 8 + ID_LEN);
        for p in prefixes {
            buf.extend_from_slice(&p.to_be_bytes());
        }
        buf.extend_from_slice(&self.0);
        Id(sha256(&buf))
    }
}

impl std::fmt::Debug for Id {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        for b in self.0 {
            write!(f, "{b:02x}")?;
        }
        Ok(())
    }
}

impl std::fmt::Display for Id {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Debug::fmt(self, f)
    }
}

/// A 20-byte address.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ShortId(pub [u8; SHORT_ID_LEN]);

/// The all-zero address — a phantom signer, refused wherever an owner set is
/// checked.
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
        for b in self.0 {
            write!(f, "{b:02x}")?;
        }
        Ok(())
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
    fn prefix_is_the_sha256_of_the_big_endian_prefixes_then_the_id() {
        let id = Id::prefixed_bytes(&[1, 2, 3]);
        let mut want_buf = Vec::new();
        want_buf.extend_from_slice(&7u64.to_be_bytes());
        want_buf.extend_from_slice(&id.0);
        assert_eq!(id.prefix(&[7]).0, sha256(&want_buf));
    }

    #[test]
    fn two_outputs_of_one_transaction_are_two_different_things_to_spend() {
        let tx = Id::prefixed_bytes(&[9]);
        assert_ne!(tx.prefix(&[0]), tx.prefix(&[1]));
    }

    #[test]
    fn ids_order_lexicographically() {
        assert!(Id::prefixed_bytes(&[1]) < Id::prefixed_bytes(&[2]));
        assert!(ShortId::prefixed_bytes(&[1]) < ShortId::prefixed_bytes(&[2]));
    }

    #[test]
    fn sorted_and_unique_rejects_a_repeat() {
        assert!(is_sorted_and_unique(&[1u32, 2, 3]));
        assert!(!is_sorted_and_unique(&[1u32, 1]));
        assert!(!is_sorted_and_unique(&[2u32, 1]));
        assert!(is_sorted_and_unique::<u32>(&[]));
    }
}
