// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! ML-DSA-65, and the one place this chain reaches it.
//!
//! F authenticates a payer and recognises a committee member by the same
//! algorithm — the platform's service-identity scheme, FIPS 204 ML-DSA-65.
//! Authentication here is a PUBLIC operation: F parses a public key and
//! verifies a signature over a preimage. It never possesses a secret, so there
//! is deliberately no signing function in this file.
//!
//! WHERE THE IMPLEMENTATION COMES FROM. `lux-pq` reaches `libluxcrypto`, which
//! is the C ABI of `luxfi/crypto` — the same package the Go reference verifies
//! with, and the same one the C++ chain links. So the three chains are not
//! three lattice implementations that have to agree; they are one, reached
//! three ways. A second implementation here would be a second opinion about
//! which signatures are valid, and the day it disagreed the differential would
//! report a chain fork where there was a library bug.
//!
//! NOTHING IN THIS CHAIN IS HOMOMORPHIC. F is the coordination plane: it
//! records handles, digests, permits and attestations, and never evaluates a
//! ciphertext, so it needs no FHE library at all. The encryption happens
//! off-chain; the committee holds the shares; F holds the public coordinates.

use lux_pq::sign::PublicKey;
use lux_pq::{Signature, Verifier};

/// How wide a public key is, from the algorithm rather than from a constant
/// somebody has to keep in step with it.
pub fn public_key_size() -> usize {
    lux_pq::sign::public_key_size()
}

/// How wide a signature is.
pub fn signature_size() -> usize {
    lux_pq::sign::signature_size()
}

/// Whether these bytes are a public key.
///
/// The reference's `PublicKeyFromBytes` validates by unmarshalling, and the
/// unmarshal for this parameter set checks the length and nothing else — every
/// 1952-byte string unpacks. So the width IS the check, and saying that here is
/// more honest than routing it through a parse that cannot fail.
pub fn is_public_key(bytes: &[u8]) -> bool {
    bytes.len() == public_key_size()
}

/// Whether `sig` is `key`'s signature over `message`.
///
/// The empty FIPS 204 context, which is what the reference's `VerifySignature`
/// uses. A different context is a different signature scheme, so this is not a
/// parameter here — the one F speaks is the one it speaks.
pub fn verify(key: &[u8], message: &[u8], sig: &[u8]) -> bool {
    if !is_public_key(key) || sig.len() != signature_size() {
        return false;
    }
    PublicKey::from_bytes(key.to_vec()).verify(message, &Signature::from_bytes(sig.to_vec()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_parameter_set_is_the_one_the_reference_signs_under() {
        // ML-DSA-65: NIST level 3. Pinned because these two widths are what
        // syntactic verification refuses an auth or a signature against, and a
        // library that answered a different pair would change that rule.
        assert_eq!(public_key_size(), 1952);
        assert_eq!(signature_size(), 3309);
    }

    #[test]
    fn a_key_of_the_wrong_width_is_not_a_key() {
        assert!(!is_public_key(&[]));
        assert!(!is_public_key(&[0u8; 8]));
        assert!(is_public_key(&[0u8; 1952]));
    }

    #[test]
    fn nothing_verifies_under_a_key_or_signature_of_the_wrong_width() {
        assert!(!verify(&[0u8; 8], b"m", &[0u8; 3309]));
        assert!(!verify(&[0u8; 1952], b"m", &[0u8; 8]));
    }

    #[test]
    fn a_signature_that_is_not_one_does_not_verify() {
        // A well-formed-width key and signature of zeros. The library has to
        // answer, and the answer has to be no.
        assert!(!verify(&[0u8; 1952], b"m", &[0u8; 3309]));
    }
}
