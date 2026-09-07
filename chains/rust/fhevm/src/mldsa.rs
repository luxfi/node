// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! ML-DSA-65, the PUBLIC half.
//!
//! F authenticates a payer and recognises a committee member by one algorithm —
//! the platform service-identity scheme, ML-DSA-65 (FIPS 204, NIST level 3). It
//! parses a public key and it verifies a signature. It never holds a secret, so
//! there is no signing here and no key generation: those live in whatever built
//! the transaction, off this chain entirely.
//!
//! WHAT "PARSEABLE" MEANS, AND WHY IT MEANS EXACTLY THAT. Go reaches this
//! algorithm through `luxfi/crypto/mldsa`, which reaches Cloudflare's circl, and
//! circl's `PublicKey.UnmarshalBinary` checks ONE thing: the length. A public key
//! is a 32-byte seed ρ followed by t₁ packed ten bits to a coefficient, and every
//! 1952-byte string unpacks to some t₁ — there is nothing further to reject. So
//! [`parse_public_key`] checks the length and nothing else, because a committee
//! member Go seats and this port refuses is a fork, and the rule that decides it
//! is `ValidateCommittee`. A key that is well-formed and wrong still verifies
//! nothing, which is the answer both languages give.
//!
//! The verifier under it is `fips204`, a different implementation of the same
//! standard. That is the point: two implementations agreeing is evidence, one
//! implementation agreeing with itself is not. `tests/golden.rs` verifies
//! signatures the Go chain actually produced.

use fips204::ml_dsa_65;
use fips204::traits::{SerDes, Verifier};

/// The width of an ML-DSA-65 public key.
pub const PUBLIC_KEY_SIZE: usize = ml_dsa_65::PK_LEN;

/// The width of an ML-DSA-65 signature.
pub const SIGNATURE_SIZE: usize = ml_dsa_65::SIG_LEN;

/// Why a public key was refused.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    /// The bytes are not the width of a key.
    InvalidKeySize,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::InvalidKeySize => write!(f, "invalid key size"),
        }
    }
}

impl std::error::Error for Error {}

/// Accepts a public key, on exactly the terms the Go chain accepts one: the
/// width, and nothing else. See the module note.
pub fn parse_public_key(data: &[u8]) -> Result<(), Error> {
    if data.len() != PUBLIC_KEY_SIZE {
        return Err(Error::InvalidKeySize);
    }
    Ok(())
}

/// Whether `sig` is `pk`'s signature over `message`.
///
/// The context is empty, which is what the Go chain verifies under
/// (`VerifySignature` passes a nil context to circl, and a nil context is a
/// zero-length one). A key or a signature of the wrong width, or one the
/// verifier cannot unpack, is not an error here: it is a signature that does not
/// verify, which is the same answer Go's `VerifySignature` gives by returning
/// false.
pub fn verify(pk: &[u8], message: &[u8], sig: &[u8]) -> bool {
    let pk: [u8; PUBLIC_KEY_SIZE] = match pk.try_into() {
        Ok(k) => k,
        Err(_) => return false,
    };
    let sig: [u8; SIGNATURE_SIZE] = match sig.try_into() {
        Ok(s) => s,
        Err(_) => return false,
    };
    match ml_dsa_65::PublicKey::try_from_bytes(pk) {
        Ok(key) => key.verify(message, &sig, &[]),
        Err(_) => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_widths_are_the_ones_go_declares() {
        assert_eq!(PUBLIC_KEY_SIZE, 1952);
        assert_eq!(SIGNATURE_SIZE, 3309);
    }

    #[test]
    fn a_key_is_accepted_on_its_width_alone() {
        assert!(parse_public_key(&[0u8; PUBLIC_KEY_SIZE]).is_ok());
        assert!(parse_public_key(&[0xff; PUBLIC_KEY_SIZE]).is_ok());
        assert_eq!(parse_public_key(b"not-an-mldsa-key"), Err(Error::InvalidKeySize));
        assert_eq!(parse_public_key(&[0u8; PUBLIC_KEY_SIZE - 1]), Err(Error::InvalidKeySize));
        assert_eq!(parse_public_key(&[0u8; PUBLIC_KEY_SIZE + 1]), Err(Error::InvalidKeySize));
    }

    #[test]
    fn nothing_verifies_under_a_key_of_the_wrong_shape() {
        assert!(!verify(b"short", b"message", &[0u8; SIGNATURE_SIZE]));
        assert!(!verify(&[0u8; PUBLIC_KEY_SIZE], b"message", b"short"));
        assert!(!verify(&[0u8; PUBLIC_KEY_SIZE], b"message", &[0u8; SIGNATURE_SIZE]));
    }
}
