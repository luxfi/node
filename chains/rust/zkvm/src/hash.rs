// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The one digest this chain is defined over, and the one way of feeding it.
//!
//! Everything the Z-chain names — a block, a vertex, a transaction, the state
//! root, the chain binding — is a SHA-256 fold. There is exactly one root
//! function and one identity function, deliberately: a hardware-conditional
//! digest (a GPU Poseidon path with a SHA-256 fallback) would make the
//! consensus-committed root depend on whether a node has an accelerator, and
//! validators with and without one would reject each other's blocks.
//!
//! [`Fold`] is the feeding, and it is here rather than at each call site
//! because the LENGTH-PREFIXING is the part that carries weight. Concatenated
//! raw, a byte could move from the end of one field to the start of the next
//! without the hash noticing — `["ab", "c"]` and `["a", "bc"]` are the same
//! bytes — and two transactions sharing an identity is what consensus decides
//! between blocks with. Every integer is big-endian, which is what Go's
//! `binary.Write(h, binary.BigEndian, …)` writes.

use sha2::{Digest, Sha256};

use crate::ids::Id;

/// SHA-256 of a buffer.
pub fn sha256(b: &[u8]) -> Id {
    let mut h = Sha256::new();
    h.update(b);
    h.finalize().into()
}

/// A digest under construction.
#[derive(Clone, Default)]
pub struct Fold(Sha256);

impl Fold {
    pub fn new() -> Self {
        Fold(Sha256::new())
    }

    /// Raw bytes, with nothing said about how many. For a field whose width is
    /// fixed by the schema — an id, a 32-byte root — where a length prefix
    /// would say only what the reader already knows.
    pub fn raw(&mut self, b: &[u8]) -> &mut Self {
        self.0.update(b);
        self
    }

    /// A number, big-endian in eight bytes.
    pub fn num(&mut self, v: u64) -> &mut Self {
        self.0.update(v.to_be_bytes());
        self
    }

    /// A signed number, big-endian in eight bytes. The two's-complement bytes
    /// Go's `binary.Write` puts on an `int64`.
    pub fn signed(&mut self, v: i64) -> &mut Self {
        self.0.update(v.to_be_bytes());
        self
    }

    /// A number, big-endian in four bytes.
    pub fn num32(&mut self, v: u32) -> &mut Self {
        self.0.update(v.to_be_bytes());
        self
    }

    /// A variable-length field: its length, then the field.
    pub fn blob(&mut self, b: &[u8]) -> &mut Self {
        self.num(b.len() as u64).raw(b)
    }

    /// How many of something follows.
    pub fn count(&mut self, n: usize) -> &mut Self {
        self.num(n as u64)
    }

    /// Whether an optional field is there. One byte, so a transaction with no
    /// proof and one with an empty proof are different transactions.
    pub fn present(&mut self, yes: bool) -> &mut Self {
        self.0.update([u8::from(yes)]);
        self
    }

    /// The digest.
    pub fn id(&self) -> Id {
        self.0.clone().finalize().into()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_digest_is_sha256() {
        // The empty string's SHA-256, which is the one hash everyone knows by
        // sight — a fold that had drifted to another function fails here.
        assert_eq!(
            crate::ids::hex(&sha256(b"")),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
        assert_eq!(crate::ids::hex(&Fold::new().id()), crate::ids::hex(&sha256(b"")));
    }

    #[test]
    fn a_number_is_eight_big_endian_bytes() {
        let mut f = Fold::new();
        f.num(1);
        assert_eq!(f.id(), sha256(&[0, 0, 0, 0, 0, 0, 0, 1]));
    }

    #[test]
    fn a_negative_number_is_its_twos_complement() {
        let mut f = Fold::new();
        f.signed(-1);
        assert_eq!(f.id(), sha256(&[0xff; 8]));
    }

    /// The reason the prefix exists. Two field lists whose concatenations are
    /// identical must not fold to one digest.
    #[test]
    fn a_length_prefix_separates_lists_that_concatenate_alike() {
        let mut a = Fold::new();
        a.blob(b"ab").blob(b"c");
        let mut b = Fold::new();
        b.blob(b"a").blob(b"bc");
        assert_ne!(a.id(), b.id());
    }

    /// And the reason the presence byte exists: absent is not empty.
    #[test]
    fn an_absent_field_is_not_an_empty_one() {
        let mut absent = Fold::new();
        absent.present(false);
        let mut empty = Fold::new();
        empty.present(true).blob(b"");
        assert_ne!(absent.id(), empty.id());
    }
}
