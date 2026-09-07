// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Z-Chain's ZK verifier precompiles.
//!
//! ONE PROFILE BIT DRIVES TWO SWITCHES. `strict_pq` gates the shielded-proof
//! verifier in [`crate::verifier`] AND the registration here. A precompile's
//! `run` takes bytes and nothing else — there is no state to consult at call
//! time — so on a strict-PQ chain the decision is made at REGISTRATION: the
//! classical Groth16 (0x80) and PLONK (0x81) verifiers are simply not
//! registered, and a call to those addresses finds no precompile. Fail-closed
//! by absence.
//!
//! On a non-strict chain every verifier is registered, because Groth16 and
//! PLONK remain a useful building block for verifying another chain's
//! classical proofs. Kept optional, never deleted.
//!
//! WHAT IS ABSENT IS ABSENT. Halo2 (0x83) and Nova (0x84) have no verifier.
//! They are registered and REFUSE, on every chain — a caller gets "this
//! verifier does not exist yet" rather than silence at an address, which is
//! what tells an integrator the difference between a chain that will never
//! answer and one that is misconfigured.
//!
//! THE ONE PLACE THIS DOES NOT FOLLOW THE REFERENCE, and why. Both classical
//! verifiers there read a caller-supplied `vk_len` and bound it with 32-BIT
//! arithmetic: `uint32(len(input)) < off+vkLen`. A length near 2^32 makes that
//! sum wrap below the offset, the bound passes, and the slice that follows is
//! taken with its end before its start. Twelve bytes of calldata — a length of
//! `0xFFFFFFFC` and nothing else — panic the verifier, at 0x80 and at 0x81
//! alike (`slice bounds out of range [4:0]`, reproduced against that code).
//! Every read here is bounded by the bytes that are actually present, so those
//! twelve bytes are refused. A crash is not a verdict, and matching one would
//! only mean two implementations that fall over on the same input rather than
//! one that stands up. Reported upstream; every OTHER answer of these two
//! verifiers is the reference's, checked against it in `tests/golden.rs`.

use std::collections::BTreeMap;

use crate::error::{Error, Result};
use crate::groth16;
use crate::starkfri;

/// Where each verifier lives in the Z-Chain precompile space.
pub const GROTH16: u8 = 0x80;
pub const PLONK: u8 = 0x81;
pub const STARK: u8 = 0x82;
pub const HALO2: u8 = 0x83;
pub const NOVA: u8 = 0x84;

/// Gas, calibrated to verification cost. Groth16: three pairings and one
/// multi-scalar multiplication. PLONK: two pairings and a polynomial
/// evaluation. STARK: a hash chain. Halo2: IPA.
pub const GROTH16_GAS: u64 = 50_000;
pub const PLONK_GAS: u64 = 80_000;
pub const STARK_GAS: u64 = 200_000;
pub const HALO2_GAS: u64 = 100_000;
pub const NOVA_GAS: u64 = 100_000;

/// What a precompile answers: one byte, 1 for a verified proof and 0 for
/// anything else.
pub const VALID: &[u8] = &[0x01];
pub const INVALID: &[u8] = &[0x00];

/// A precompiled contract.
///
/// `run` answers `(bytes, error)`. An INVALID PROOF IS NOT AN ERROR: it
/// answers `(INVALID, Ok)`, because a proof that does not verify is a fact the
/// caller asked for. An error means the call could not be judged at all — the
/// input is malformed, or the verifier is not available — and the two must not
/// be collapsed, or a contract cannot tell "your proof is wrong" from "this
/// node cannot check proofs".
pub trait Precompile: Send + Sync {
    fn required_gas(&self, input: &[u8]) -> u64;
    fn run(&self, input: &[u8]) -> (Vec<u8>, Result<()>);
}

/// The registry a chain hands its EVM.
#[derive(Default)]
pub struct Registry {
    contracts: BTreeMap<u8, Box<dyn Precompile>>,
}

impl Registry {
    pub fn new() -> Registry {
        Registry {
            contracts: BTreeMap::new(),
        }
    }

    pub fn register(&mut self, addr: u8, c: Box<dyn Precompile>) {
        self.contracts.insert(addr, c);
    }

    pub fn get(&self, addr: u8) -> Result<&dyn Precompile> {
        self.contracts
            .get(&addr)
            .map(|c| c.as_ref())
            .ok_or_else(|| Error::NoMethod(format!("no precompile at address 0x{addr:02x}")))
    }

    pub fn addresses(&self) -> Vec<u8> {
        self.contracts.keys().copied().collect()
    }
}

/// Register the Z-Chain verifiers. `stark` is the binding the STARK verifier
/// will use — unbound on a node built without it, which is what makes that
/// path fail closed.
pub fn register(registry: &mut Registry, strict_pq: bool, stark: starkfri::Verifier) {
    registry.register(STARK, Box::new(StarkVerifier { stark }));
    registry.register(HALO2, Box::new(Absent(HALO2_GAS)));
    registry.register(NOVA, Box::new(Absent(NOVA_GAS)));

    if strict_pq {
        // Classical pairing-based verifiers are forbidden here: omit them, so
        // 0x80 and 0x81 resolve to "no precompile".
        return;
    }
    registry.register(GROTH16, Box::new(Groth16Verifier));
    registry.register(PLONK, Box::new(PlonkVerifier));
}

// ---- Groth16 ---------------------------------------------------------------

/// ```text
/// vk_len(4) ‖ vk ‖ proof(256) ‖ num_inputs(4) ‖ inputs(32·num_inputs)
/// ```
pub struct Groth16Verifier;

impl Precompile for Groth16Verifier {
    fn required_gas(&self, _: &[u8]) -> u64 {
        GROTH16_GAS
    }

    fn run(&self, input: &[u8]) -> (Vec<u8>, Result<()>) {
        let parsed = (|| -> Result<bool> {
            let mut r = Cursor::new(input);
            let vk_len = r.u32()? as usize;
            let vk = groth16::read_verifying_key(r.take(vk_len)?)?;
            let proof = groth16::read_proof(r.take(groth16::PROOF_LEN)?)?;
            let n = r.u32()? as usize;

            // K holds one point per public input plus the constant term, so
            // the KEY states how many inputs its circuit takes. The caller
            // supplies both the key and the count, and they have to agree:
            // too many reads past the end of K, too few leaves the trailing K
            // points out of the combination — judging the proof against a
            // smaller statement than the key describes.
            if n != vk.k.len() - 1 {
                return Err(Error::ProofInvalid(format!(
                    "public inputs: {n} supplied, the verifying key's circuit takes {}",
                    vk.k.len() - 1
                )));
            }
            let raw = r.take(n * 32)?;
            let witness: Vec<_> = (0..n)
                .map(|i| groth16::witness_from_bytes(&raw[i * 32..i * 32 + 32]))
                .collect();
            Ok(groth16::verify(&proof, &vk, &witness).is_ok())
        })();
        answer(parsed)
    }
}

// ---- PLONK -----------------------------------------------------------------

/// ```text
/// vk_len(4) ‖ vk ‖ proof_len(4) ‖ proof ‖ num_inputs(4) ‖ inputs(32·n)
/// ```
///
/// The structure is parsed and then REFUSED. What stood in this place decoded
/// the proof and the key — exact sizes, subgroup checks, points at infinity,
/// some two hundred lines of it — and handed the result to a pairing that
/// refused every proof unconditionally, because the verification equation
///
/// ```text
/// e(Wz + u·Wzw, [α]₂) = e(z·Wz + u·zω·Wzw, [1]₂)
/// ```
///
/// was never written.
///
/// THE FRAME IS AN ERROR AND THE PROOF IS A VERDICT, and which of the two an
/// input gets is not a matter of taste. A caller reading `INVALID` with no
/// error was told "that does not verify" and carries on; an error is a call
/// that could not be judged at all — in an EVM, the difference between a
/// `false` and a revert. The reference draws the line after the frame: a
/// length that does not fit the bytes sent is an error, and EVERYTHING past
/// it — a short proof, a commitment off the curve, a key too small to hold its
/// own G2, the missing equation — is `(INVALID, no error)`.
///
/// So the structural parse is GONE from here, because it decided nothing.
/// Seven point decodings whose every outcome is the same byte and the same
/// absent error are not a check; they are work an untrusted caller can ask for.
/// What the parse used to distinguish, it distinguished only in a log line the
/// reference never returns. `testdata/golden.json` carries eight of these
/// inputs with the answer the reference gave to each, including the five that
/// reach this refusal by five different routes.
pub struct PlonkVerifier;

impl Precompile for PlonkVerifier {
    fn required_gas(&self, _: &[u8]) -> u64 {
        PLONK_GAS
    }

    fn run(&self, input: &[u8]) -> (Vec<u8>, Result<()>) {
        let framed = (|| -> Result<()> {
            let mut r = Cursor::new(input);
            let vk_len = r.u32()? as usize;
            r.take(vk_len)?;
            let proof_len = r.u32()? as usize;
            r.take(proof_len)?;
            let n = r.u32()?;
            // The reference computes this last bound in 32-bit arithmetic, so
            // a count of 2^27 wraps to zero and passes it. What that admits is
            // an input whose public inputs are not there — and they are never
            // read, by either implementation, because the equation that would
            // read them is not written. Refusing it here instead would make
            // one call revert on this node and return false on a Go one.
            let need = (r.pos as u32).wrapping_add(n.wrapping_mul(32));
            if (input.len() as u32) < need {
                return Err(Error::ProofInvalid("input too short".into()));
            }
            Ok(())
        })();
        match framed {
            // A well-framed call, and no verifier behind it.
            Ok(()) => (INVALID.to_vec(), Ok(())),
            Err(e) => (INVALID.to_vec(), Err(e)),
        }
    }
}

// ---- STARK / FRI -----------------------------------------------------------

/// ```text
/// proof_len(4) ‖ proof ‖ pub_len(4) ‖ inputs
/// ```
///
/// The proof must begin with [`starkfri::MAGIC`]. When no verifier is bound
/// this answers [`Error::VerifierNotRegistered`] and NEVER `VALID`: there is
/// no forgery oracle in the unbound configuration, and an operator can tell a
/// missing binding from a bad proof.
pub struct StarkVerifier {
    stark: starkfri::Verifier,
}

impl Precompile for StarkVerifier {
    fn required_gas(&self, _: &[u8]) -> u64 {
        STARK_GAS
    }

    fn run(&self, input: &[u8]) -> (Vec<u8>, Result<()>) {
        let parsed = (|| -> Result<bool> {
            let mut r = Cursor::new(input);
            let proof_len = r.u32()? as usize;
            let proof = r.take(proof_len)?.to_vec();
            let pub_len = r.u32()? as usize;
            let public = r.take(pub_len)?.to_vec();
            match self.stark.verify(&proof, &public) {
                Ok(()) => Ok(true),
                // A binding that is not there is an ERROR, not a verdict.
                Err(Error::VerifierNotRegistered) => Err(Error::VerifierNotRegistered),
                // A malformed or non-verifying proof is a verdict.
                Err(_) => Ok(false),
            }
        })();
        answer(parsed)
    }
}

// ---- what has no verifier --------------------------------------------------

/// A verifier that does not exist yet, and says so.
pub struct Absent(u64);

impl Precompile for Absent {
    fn required_gas(&self, _: &[u8]) -> u64 {
        self.0
    }
    fn run(&self, _: &[u8]) -> (Vec<u8>, Result<()>) {
        (
            INVALID.to_vec(),
            Err(Error::UnsupportedProofType(
                "verifier not yet available".into(),
            )),
        )
    }
}

// ---- input reading ---------------------------------------------------------

/// Reads a precompile's argument. Every length comes from the caller, so
/// every read is bounded by what is actually there before it is used.
struct Cursor<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> Cursor<'a> {
    fn new(data: &'a [u8]) -> Cursor<'a> {
        Cursor { data, pos: 0 }
    }

    fn u32(&mut self) -> Result<u32> {
        let b = self.take(4)?;
        Ok(u32::from_be_bytes(b.try_into().unwrap()))
    }

    fn take(&mut self, n: usize) -> Result<&'a [u8]> {
        if self.data.len() - self.pos < n {
            return Err(Error::ProofInvalid("input too short".into()));
        }
        let at = self.pos;
        self.pos += n;
        Ok(&self.data[at..at + n])
    }
}

fn answer(r: Result<bool>) -> (Vec<u8>, Result<()>) {
    match r {
        Ok(true) => (VALID.to_vec(), Ok(())),
        Ok(false) => (INVALID.to_vec(), Ok(())),
        Err(e) => (INVALID.to_vec(), Err(e)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    #[test]
    fn a_strict_chain_has_no_classical_verifier_at_all() {
        let mut r = Registry::new();
        register(&mut r, true, starkfri::Verifier::unbound());
        assert_eq!(r.addresses(), vec![STARK, HALO2, NOVA]);
        assert!(r.get(GROTH16).is_err(), "0x80 must resolve to nothing");
        assert!(r.get(PLONK).is_err(), "0x81 must resolve to nothing");
    }

    #[test]
    fn a_permissive_chain_keeps_them_as_a_building_block() {
        let mut r = Registry::new();
        register(&mut r, false, starkfri::Verifier::unbound());
        assert_eq!(r.addresses(), vec![GROTH16, PLONK, STARK, HALO2, NOVA]);
    }

    #[test]
    fn the_stark_precompile_fails_closed_when_nothing_is_bound() {
        let mut r = Registry::new();
        register(&mut r, true, starkfri::Verifier::unbound());
        let mut input = Vec::new();
        let proof = b"P3Q1 a structurally perfect proof";
        input.extend_from_slice(&(proof.len() as u32).to_be_bytes());
        input.extend_from_slice(proof);
        input.extend_from_slice(&0u32.to_be_bytes());
        let (out, err) = r.get(STARK).unwrap().run(&input);
        assert_eq!(out, INVALID);
        assert_eq!(err, Err(Error::VerifierNotRegistered));
    }

    #[test]
    fn the_stark_precompile_answers_a_bound_verifier() {
        struct Yes;
        impl starkfri::Backend for Yes {
            fn verify(&self, _: u8, _: &[u8], _: &[u8]) -> Result<bool> {
                Ok(true)
            }
        }
        let mut r = Registry::new();
        register(&mut r, true, starkfri::Verifier::bound(Arc::new(Yes)));
        let mut input = Vec::new();
        input.extend_from_slice(&4u32.to_be_bytes());
        input.extend_from_slice(b"P3Q1");
        input.extend_from_slice(&3u32.to_be_bytes());
        input.extend_from_slice(b"pub");
        let (out, err) = r.get(STARK).unwrap().run(&input);
        assert_eq!((out.as_slice(), err), (VALID, Ok(())));

        // And a proof without the envelope is a VERDICT, not an error.
        let mut bad = Vec::new();
        bad.extend_from_slice(&4u32.to_be_bytes());
        bad.extend_from_slice(b"XXXX");
        bad.extend_from_slice(&0u32.to_be_bytes());
        let (out, err) = r.get(STARK).unwrap().run(&bad);
        assert_eq!((out.as_slice(), err), (INVALID, Ok(())));
    }

    #[test]
    fn a_truncated_argument_is_refused_rather_than_read_past() {
        let mut r = Registry::new();
        register(&mut r, false, starkfri::Verifier::unbound());
        for addr in [GROTH16, PLONK, STARK] {
            for len in [0usize, 1, 3, 7] {
                let (out, err) = r.get(addr).unwrap().run(&vec![0u8; len]);
                assert_eq!(out, INVALID);
                assert!(err.is_err(), "0x{addr:02x} with {len} bytes");
            }
        }
    }

    #[test]
    fn a_declared_length_never_sizes_an_allocation() {
        let mut r = Registry::new();
        register(&mut r, false, starkfri::Verifier::unbound());
        // vk_len = 2^32-1 with nothing behind it.
        let mut input = u32::MAX.to_be_bytes().to_vec();
        input.extend_from_slice(&[0u8; 8]);
        let (out, err) = r.get(GROTH16).unwrap().run(&input);
        assert_eq!(out, INVALID);
        assert!(err.is_err());
    }

    #[test]
    fn what_has_no_verifier_says_so_on_every_chain() {
        for strict in [true, false] {
            let mut r = Registry::new();
            register(&mut r, strict, starkfri::Verifier::unbound());
            for addr in [HALO2, NOVA] {
                let (out, err) = r.get(addr).unwrap().run(b"anything");
                assert_eq!(out, INVALID);
                assert!(matches!(err, Err(Error::UnsupportedProofType(_))));
            }
        }
    }

    #[test]
    fn plonk_refuses_a_well_formed_proof_because_the_equation_is_not_written() {
        use ark_ec::AffineRepr;
        use ark_ff::{BigInteger, PrimeField};
        let g1 = ark_bn254::G1Affine::generator();
        let g2 = ark_bn254::G2Affine::generator();
        let mut g1b = g1.x.into_bigint().to_bytes_be();
        g1b.extend_from_slice(&g1.y.into_bigint().to_bytes_be());
        let mut g2b = g2.x.c1.into_bigint().to_bytes_be();
        g2b.extend_from_slice(&g2.x.c0.into_bigint().to_bytes_be());
        g2b.extend_from_slice(&g2.y.c1.into_bigint().to_bytes_be());
        g2b.extend_from_slice(&g2.y.c0.into_bigint().to_bytes_be());

        let mut proof = Vec::new();
        for _ in 0..7 {
            proof.extend_from_slice(&g1b);
        }
        proof.extend_from_slice(&[0u8; 96]);

        let mut input = Vec::new();
        input.extend_from_slice(&(g2b.len() as u32).to_be_bytes());
        input.extend_from_slice(&g2b);
        input.extend_from_slice(&(proof.len() as u32).to_be_bytes());
        input.extend_from_slice(&proof);
        input.extend_from_slice(&0u32.to_be_bytes());

        let mut r = Registry::new();
        register(&mut r, false, starkfri::Verifier::unbound());
        let (out, err) = r.get(PLONK).unwrap().run(&input);
        // Never valid — and a verdict, not an error: the frame was correct,
        // and it is the PROOF this chain will not judge.
        assert_eq!(out, INVALID);
        assert_eq!(err, Ok(()));
    }

    #[test]
    fn a_plonk_frame_that_does_not_hold_its_own_lengths_is_an_error() {
        let mut r = Registry::new();
        register(&mut r, false, starkfri::Verifier::unbound());
        let plonk = r.get(PLONK).unwrap();
        for input in [
            vec![0u8, 0, 0],                       // not even a length
            vec![0u8, 0, 39, 15, 1, 2, 3],         // a key that is not there
            vec![0xFF, 0xFF, 0xFF, 0xFC, 1, 2, 3], // a length that would wrap
        ] {
            let (out, err) = plonk.run(&input);
            assert_eq!(out, INVALID);
            assert!(err.is_err(), "a frame that does not hold up is an error");
        }
    }

    #[test]
    fn a_plonk_input_count_that_overruns_the_bytes_is_still_a_verdict() {
        // 2^27 inputs: the reference's 32-bit bound wraps to zero and admits
        // this, and the inputs are never read. An error here would revert a
        // call that returns false over there.
        let mut input = 0u32.to_be_bytes().to_vec(); // no key
        input.extend_from_slice(&0u32.to_be_bytes()); // no proof
        input.extend_from_slice(&(1u32 << 27).to_be_bytes());
        let mut r = Registry::new();
        register(&mut r, false, starkfri::Verifier::unbound());
        let (out, err) = r.get(PLONK).unwrap().run(&input);
        assert_eq!(out, INVALID);
        assert_eq!(err, Ok(()));
    }
}
