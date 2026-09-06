// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The strict-PQ STARK/FRI verification entry point.
//!
//! cSHAKE256 Merkle hashes over the Goldilocks 64-bit prime field, a FRI
//! low-degree test, no pairings, no bn254, no trusted setup. The prover and the
//! verifier themselves run out of band; what lives here is the seam a chain
//! calls through, the structural check that runs before it, and the registry
//! that says whether a verifier is wired at all.
//!
//! TWO REFUSALS, AND THEY ARE DIFFERENT FACTS.
//!
//! A proof that does not begin with [`MAGIC`] never reaches a verifier: it is
//! not a proof of this system, and saying so costs nothing an honest prover
//! needs. That is [`Error::StarkInvalidProof`].
//!
//! A proof that IS one, on a build with no verifier registered, is refused as
//! [`Error::StarkVerifierNotRegistered`] — not accepted, and not silently
//! passed. A structurally well-formed proof is NEVER accepted without a real
//! verifier, so there is no forgery oracle. The two are kept apart because an
//! operator has to be able to tell "your proof is wrong" from "this node cannot
//! judge proofs", and a caller has to be able to say which in its own words.
//!
//! PROVER STATUS, tracked rather than faked. The full post-quantum shielded
//! path also needs the prover side and a shielded AIR that arithmetises the
//! spend/output circuit — note commitments, nullifier derivation, value
//! balance, range proofs — over the Goldilocks field. Until those land, this
//! accepts no shielded proof on a strict-PQ chain, and shielded value transfer
//! is effectively disabled there. That is the correct posture: no classical
//! fallback, no forgeable path.

use std::sync::RwLock;

use crate::error::{Error, Result};

/// The four bytes every STARK/FRI proof must begin with.
///
/// Preserved verbatim from before the dispatch was renamed, so the EasyCrypt,
/// Lean and Jasmin artifacts keep referring to a byte-identical wire tag. It is
/// a wire tag, not a name claim.
pub const MAGIC: &[u8; 4] = b"P3Q1";

/// The first wire-format version, and the only one.
pub const VERSION1: u8 = 0x01;

/// What bridges to the out-of-band verifier.
///
/// `Ok(true)` is a verified proof, `Ok(false)` a well-formed proof that did not
/// verify, and `Err` a failure inside the verifier itself. The three are
/// separate because they are three different things to tell an operator.
pub type Verifier = fn(version: u8, proof: &[u8], public: &[u8]) -> Result<bool>;

/// The registered verifier, or none.
///
/// A lock rather than a one-shot cell because a test registers one, checks what
/// the chain does with it, and puts the previous one back — and a registry that
/// could only be written once would make the second test in a process depend on
/// the first.
static VERIFIER: RwLock<Option<Verifier>> = RwLock::new(None);

/// Wire the verifier, or clear it with `None`.
///
/// This is the authoritative seam: it FORCES the given verifier, overriding
/// whatever was there.
pub fn register(v: Option<Verifier>) {
    *VERIFIER.write().expect("the starkfri registry") = v;
}

/// Wire `v` only if nothing is registered. The safe-refuse seam: a node's
/// startup can guarantee the chain never silently no-ops without clobbering a
/// real verifier something else already installed. Reports whether it installed
/// one.
pub fn register_default(v: Verifier) -> bool {
    let mut held = VERIFIER.write().expect("the starkfri registry");
    if held.is_some() {
        return false;
    }
    *held = Some(v);
    true
}

/// The currently registered verifier.
pub fn registered() -> Option<Verifier> {
    *VERIFIER.read().expect("the starkfri registry")
}

/// Verify a proof against its public inputs.
///
/// The structural check is here rather than inside a verifier because it is the
/// same check on every path into this package, and because it is
/// constant-time-friendly: a STARK proof and its public inputs are non-secret
/// by construction, so byte equality on the tag is fine.
pub fn verify(proof: &[u8], public: &[u8]) -> Result<bool> {
    if proof.len() < MAGIC.len() || &proof[..MAGIC.len()] != MAGIC {
        return Err(Error::StarkInvalidProof);
    }
    match registered() {
        None => Err(Error::StarkVerifierNotRegistered),
        Some(fn_) => fn_(VERSION1, proof, public),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The registry is process-wide, so the tests that touch it take one lock
    /// between them. Without it, one test's verifier answers another's proof.
    static SERIAL: RwLock<()> = RwLock::new(());

    fn tagged(rest: &[u8]) -> Vec<u8> {
        let mut p = MAGIC.to_vec();
        p.extend_from_slice(rest);
        p
    }

    fn accepts_everything(_: u8, _: &[u8], _: &[u8]) -> Result<bool> {
        Ok(true)
    }

    fn refuses_everything(_: u8, _: &[u8], _: &[u8]) -> Result<bool> {
        Ok(false)
    }

    fn breaks(_: u8, _: &[u8], _: &[u8]) -> Result<bool> {
        Err(Error::Store("the verifier could not be reached".into()))
    }

    /// A proof that is not one of this system's is refused before any verifier
    /// is consulted — which is why a build with none still gives this answer.
    #[test]
    fn a_proof_without_the_wire_tag_never_reaches_a_verifier() {
        let _held = SERIAL.write().unwrap();
        register(Some(accepts_everything));
        assert_eq!(verify(&[0x60; 192], b""), Err(Error::StarkInvalidProof));
        assert_eq!(verify(b"", b""), Err(Error::StarkInvalidProof));
        assert_eq!(verify(b"P3Q", b""), Err(Error::StarkInvalidProof));
        register(None);
    }

    /// The corpus's proofs are 192 bytes of one repeated value. This is the
    /// answer every well-formed Z vector gets, on every implementation, and it
    /// is reached without a verifier being registered at all.
    #[test]
    fn a_repeated_byte_is_not_a_proof_of_this_system() {
        let _held = SERIAL.write().unwrap();
        register(None);
        for seed in [0x60u8, 0x61, 0x62, 0x63] {
            assert_eq!(verify(&[seed; 192], b""), Err(Error::StarkInvalidProof));
        }
        // And one byte of it flipped is still not one.
        let mut tampered = [0x60u8; 192];
        tampered[0] ^= 0xFF;
        assert_eq!(verify(&tampered, b""), Err(Error::StarkInvalidProof));
    }

    /// The whole point of the registry: a structurally well-formed proof is
    /// never accepted just because nothing judged it.
    #[test]
    fn a_well_formed_proof_with_no_verifier_is_refused_not_accepted() {
        let _held = SERIAL.write().unwrap();
        register(None);
        assert_eq!(
            verify(&tagged(&[7; 64]), b"public"),
            Err(Error::StarkVerifierNotRegistered)
        );
    }

    #[test]
    fn a_registered_verifier_decides_a_well_formed_proof() {
        let _held = SERIAL.write().unwrap();
        register(Some(accepts_everything));
        assert_eq!(verify(&tagged(&[7; 64]), b"public"), Ok(true));
        register(Some(refuses_everything));
        assert_eq!(verify(&tagged(&[7; 64]), b"public"), Ok(false));
        register(Some(breaks));
        assert!(matches!(verify(&tagged(&[7; 64]), b"public"), Err(Error::Store(_))));
        register(None);
    }

    #[test]
    fn a_default_yields_to_one_already_registered_and_fills_an_empty_seam() {
        let _held = SERIAL.write().unwrap();
        register(None);
        assert!(register_default(refuses_everything));
        assert!(!register_default(accepts_everything));
        assert_eq!(verify(&tagged(&[7; 64]), b""), Ok(false));
        register(None);
    }

    #[test]
    fn the_version_the_seam_passes_is_the_one_wire_version() {
        let _held = SERIAL.write().unwrap();
        fn checks_version(v: u8, _: &[u8], _: &[u8]) -> Result<bool> {
            Ok(v == VERSION1)
        }
        register(Some(checks_version));
        assert_eq!(verify(&tagged(&[7; 64]), b""), Ok(true));
        register(None);
    }
}
