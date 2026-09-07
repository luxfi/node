// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Groth16 over bn254 — the CLASSICAL path, which a strict-PQ chain never
//! reaches.
//!
//! This is here because a non-strict deployment may run it, and because
//! "refuse it" is only a real refusal if the thing being refused exists. The
//! verification equation is
//!
//! ```text
//! e(A, B) = e(α, β) · e(Σ, γ) · e(C, δ)      where Σ = K₀ + Σᵢ wᵢ·Kᵢ₊₁
//! ```
//!
//! and it is the whole of what this module does.
//!
//! THE ENCODING IS GNARK'S, because the peers are Go nodes. A point is
//! big-endian, X before Y, and an extension coordinate is A1 BEFORE A0 — the
//! order gnark writes and the order EIP-197 writes. The top two bits of the
//! first byte are gnark's tag: `00` uncompressed, `10`/`11` compressed with
//! the smaller/larger root, `01` the point at infinity. All four are read
//! here, because gnark reads all four and a proof this node refuses on a tag
//! is a proof a Go node accepts.
//!
//! WHAT IS REFUSED AND WHY. A point must be on the curve, in the
//! prime-order subgroup, and NOT the point at infinity. The last one is
//! load-bearing on its own: a pairing drops any term whose argument is
//! infinity, so an element at infinity would remove itself from the equation
//! and leave a weaker check than the one written down. Infinity also encodes
//! as all-zero bytes, which is what a lazily-built key looks like.
//!
//! THE PUBLIC-INPUT COMBINATION STAYS ON THE CPU. Every node has to reach the
//! same point from the same key and witness; handing that sum to an
//! accelerator whose answer is never compared against the CPU's is how two
//! honest nodes come to disagree about a proof.

use ark_bn254::{Bn254, Fq, Fq2, Fr, G1Affine, G2Affine};
use ark_ec::pairing::Pairing;
use ark_ec::short_weierstrass::SWCurveConfig;
use ark_ec::{AffineRepr, CurveGroup};
use ark_ff::{BigInteger, Field, PrimeField, Zero};

use crate::error::{Error, Result};

/// Bytes in an uncompressed G1 point.
pub const G1_UNCOMPRESSED: usize = 64;
/// Bytes in an uncompressed G2 point.
pub const G2_UNCOMPRESSED: usize = 128;
/// Bytes in a Groth16 proof: `A ‖ B ‖ C`.
pub const PROOF_LEN: usize = G1_UNCOMPRESSED + G2_UNCOMPRESSED + G1_UNCOMPRESSED;

const MASK: u8 = 0b11 << 6;
const UNCOMPRESSED: u8 = 0b00 << 6;
const COMPRESSED_SMALLEST: u8 = 0b10 << 6;
const COMPRESSED_LARGEST: u8 = 0b11 << 6;
const COMPRESSED_INFINITY: u8 = 0b01 << 6;

/// A proof: three group elements and nothing else.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Proof {
    pub a: G1Affine,
    pub b: G2Affine,
    pub c: G1Affine,
}

/// A verifying key. `k` carries one point per public input PLUS the constant
/// term `k[0]`, so a key of n points speaks about exactly n-1 public inputs —
/// a property of the circuit the key was made for, never something a
/// transaction chooses.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct VerifyingKey {
    pub alpha: G1Affine,
    pub beta: G2Affine,
    pub gamma: G2Affine,
    pub delta: G2Affine,
    pub k: Vec<G1Affine>,
}

// ---- reading points --------------------------------------------------------

fn field(bytes: &[u8]) -> Result<Fq> {
    // Canonical: the integer must be BELOW the modulus. gnark refuses a
    // non-canonical encoding rather than reducing it, so two byte strings
    // never name one field element. Both sides are 32 big-endian bytes, so
    // comparing them as bytes IS comparing them as numbers.
    let mut clean = bytes.to_vec();
    clean[0] &= !MASK;
    if clean.as_slice() >= Fq::MODULUS.to_bytes_be().as_slice() {
        return Err(Error::ProofInvalid("a coordinate is not canonical".into()));
    }
    Ok(Fq::from_be_bytes_mod_order(&clean))
}

/// Is `y` the lexicographically larger of `y` and `-y`? gnark's rule, and the
/// one the compressed tag names.
fn larger(y: &Fq) -> bool {
    y.into_bigint() > Fq::MODULUS_MINUS_ONE_DIV_TWO
}

fn larger2(y: &Fq2) -> bool {
    if y.c1.is_zero() {
        larger(&y.c0)
    } else {
        larger(&y.c1)
    }
}

/// A G1 point in gnark's encoding. Answers how many bytes it consumed, the way
/// gnark's `SetBytes` does — a compressed point in a 64-byte buffer reads 32
/// and leaves the rest, which is exactly what a Go peer would do with it.
pub fn read_g1(buf: &[u8]) -> Result<(G1Affine, usize)> {
    if buf.len() < 32 {
        return Err(Error::ProofInvalid(
            "a G1 point needs at least 32 bytes".into(),
        ));
    }
    let tag = buf[0] & MASK;
    if tag == COMPRESSED_INFINITY {
        if buf[0] & !MASK != 0 || buf[1..32].iter().any(|b| *b != 0) {
            return Err(Error::ProofInvalid(
                "infinity is encoded with nothing else".into(),
            ));
        }
        return Err(Error::ProofInvalid("point at infinity".into()));
    }
    if tag == UNCOMPRESSED {
        if buf.len() < G1_UNCOMPRESSED {
            return Err(Error::ProofInvalid("a G1 point needs 64 bytes".into()));
        }
        let x = field(&buf[..32])?;
        let y = field(&buf[32..64])?;
        return Ok((check_g1(G1Affine::new_unchecked(x, y))?, G1_UNCOMPRESSED));
    }
    // Compressed: recover y from x and pick the root the tag names.
    let want_larger = match tag {
        COMPRESSED_LARGEST => true,
        COMPRESSED_SMALLEST => false,
        _ => unreachable!("the two remaining tags are the two compressed ones"),
    };
    let x = field(&buf[..32])?;
    let y2 = x * x * x + Fq::from(3u64);
    let y = y2
        .sqrt()
        .ok_or_else(|| Error::ProofInvalid("x is not on the curve".into()))?;
    let y = if larger(&y) == want_larger { y } else { -y };
    Ok((check_g1(G1Affine::new_unchecked(x, y))?, 32))
}

/// A G2 point, extension coordinate A1 before A0.
pub fn read_g2(buf: &[u8]) -> Result<(G2Affine, usize)> {
    if buf.len() < 64 {
        return Err(Error::ProofInvalid(
            "a G2 point needs at least 64 bytes".into(),
        ));
    }
    let tag = buf[0] & MASK;
    if tag == COMPRESSED_INFINITY {
        if buf[0] & !MASK != 0 || buf[1..64].iter().any(|b| *b != 0) {
            return Err(Error::ProofInvalid(
                "infinity is encoded with nothing else".into(),
            ));
        }
        return Err(Error::ProofInvalid("point at infinity".into()));
    }
    if tag == UNCOMPRESSED {
        if buf.len() < G2_UNCOMPRESSED {
            return Err(Error::ProofInvalid("a G2 point needs 128 bytes".into()));
        }
        let x = Fq2::new(field(&buf[32..64])?, field(&buf[..32])?);
        let y = Fq2::new(field(&buf[96..128])?, field(&buf[64..96])?);
        return Ok((check_g2(G2Affine::new_unchecked(x, y))?, G2_UNCOMPRESSED));
    }
    let want_larger = match tag {
        COMPRESSED_LARGEST => true,
        COMPRESSED_SMALLEST => false,
        _ => unreachable!("the two remaining tags are the two compressed ones"),
    };
    let x = Fq2::new(field(&buf[32..64])?, field(&buf[..32])?);
    let b = <ark_bn254::g2::Config as SWCurveConfig>::COEFF_B;
    let y2 = x * x * x + b;
    let y = y2
        .sqrt()
        .ok_or_else(|| Error::ProofInvalid("x is not on the twist".into()))?;
    let y = if larger2(&y) == want_larger { y } else { -y };
    Ok((check_g2(G2Affine::new_unchecked(x, y))?, 64))
}

/// The one place that decides what a usable G1 point is.
fn check_g1(p: G1Affine) -> Result<G1Affine> {
    // gnark represents infinity as (0, 0), which is also what an unwritten
    // key looks like. Named before the curve test so it reads as the refusal
    // it is rather than as a malformed coordinate.
    if p.x.is_zero() && p.y.is_zero() {
        return Err(Error::ProofInvalid("point at infinity".into()));
    }
    if !p.is_on_curve() {
        return Err(Error::ProofInvalid("point not on the curve".into()));
    }
    if !p.is_in_correct_subgroup_assuming_on_curve() {
        return Err(Error::ProofInvalid(
            "point not in the prime-order subgroup".into(),
        ));
    }
    if p.is_zero() {
        return Err(Error::ProofInvalid("point at infinity".into()));
    }
    Ok(p)
}

/// The same, for G2. Its cofactor is not one, so the subgroup test here
/// refuses points that are on the twist and outside the group.
fn check_g2(p: G2Affine) -> Result<G2Affine> {
    if p.x.is_zero() && p.y.is_zero() {
        return Err(Error::ProofInvalid("point at infinity".into()));
    }
    if !p.is_on_curve() {
        return Err(Error::ProofInvalid("point not on the curve".into()));
    }
    if !p.is_in_correct_subgroup_assuming_on_curve() {
        return Err(Error::ProofInvalid(
            "point not in the prime-order subgroup".into(),
        ));
    }
    if p.is_zero() {
        return Err(Error::ProofInvalid("point at infinity".into()));
    }
    Ok(p)
}

// ---- reading a proof and a key ---------------------------------------------

/// `A(64) ‖ B(128) ‖ C(64)`. A proof this returns has already passed the point
/// checks, so callers never repeat them.
pub fn read_proof(data: &[u8]) -> Result<Proof> {
    if data.len() < PROOF_LEN {
        return Err(Error::ProofInvalid("proof data too short".into()));
    }
    let (a, _) = read_g1(&data[..64])?;
    let (b, _) = read_g2(&data[64..192])?;
    let (c, _) = read_g1(&data[192..256])?;
    Ok(Proof { a, b, c })
}

/// `Alpha(64) ‖ Beta(128) ‖ Gamma(128) ‖ Delta(128) ‖ numK(4) ‖ K[](64·numK)`,
/// the count big-endian.
pub fn read_verifying_key(data: &[u8]) -> Result<VerifyingKey> {
    const MIN: usize = 64 + 128 + 128 + 128 + 4;
    if data.len() < MIN {
        return Err(Error::ProofInvalid("verifying key data too short".into()));
    }
    let (alpha, _) = read_g1(&data[..64])?;
    let (beta, _) = read_g2(&data[64..192])?;
    let (gamma, _) = read_g2(&data[192..320])?;
    let (delta, _) = read_g2(&data[320..448])?;
    let num_k = u32::from_be_bytes(data[448..452].try_into().unwrap()) as usize;
    // Bounded by the bytes that remain BEFORE anything is allocated: a key
    // declaring four billion points must not become four billion points.
    if num_k > (data.len() - 452) / 64 {
        return Err(Error::ProofInvalid("insufficient data for K points".into()));
    }
    let mut k = Vec::with_capacity(num_k);
    for i in 0..num_k {
        let at = 452 + i * 64;
        let (p, _) = read_g1(&data[at..at + 64])?;
        k.push(p);
    }
    Ok(VerifyingKey {
        alpha,
        beta,
        gamma,
        delta,
        k,
    })
}

/// A trusted setup never produces infinity for α, β, γ, δ or K, so a key
/// carrying one is not a setup output and its pairing equation would collapse
/// to something weaker. The point checks in [`read_verifying_key`] already
/// refused it; this states the property where a reader looks for it.
pub fn validate_verifying_key(vk: &VerifyingKey) -> Result<()> {
    if vk.k.is_empty() {
        return Err(Error::ProofInvalid(
            "a verifying key with no K points describes nothing".into(),
        ));
    }
    for p in [&vk.alpha].into_iter().chain(vk.k.iter()) {
        if p.is_zero() {
            return Err(Error::ProofInvalid("point at infinity".into()));
        }
    }
    for p in [&vk.beta, &vk.gamma, &vk.delta] {
        if p.is_zero() {
            return Err(Error::ProofInvalid("point at infinity".into()));
        }
    }
    Ok(())
}

/// One public input, as gnark reads it: a big-endian integer REDUCED modulo
/// the scalar field. Unlike a coordinate, this is not required to be
/// canonical — `fr.Element.SetBytes` reduces — so a 33-byte input and its
/// reduction are the same witness on both sides.
pub fn witness_from_bytes(b: &[u8]) -> Fr {
    Fr::from_be_bytes_mod_order(b)
}

/// The pairing check.
///
/// The witness arrives with the transaction, so a peer picks its length, and
/// the sum below runs over the witness against K. A length either side of what
/// the key states judges the proof against a statement the key does not
/// describe — past the end of K in one direction, and short of the trailing K
/// points in the other — so it has to be exactly what the key says.
pub fn verify(proof: &Proof, vk: &VerifyingKey, witness: &[Fr]) -> Result<()> {
    let want = vk.k.len() - 1;
    if witness.len() != want {
        return Err(Error::ProofInvalid(format!(
            "public inputs: {} supplied, the verifying key's circuit takes {want}",
            witness.len()
        )));
    }

    // Σ = K₀ + Σᵢ wᵢ·Kᵢ₊₁, on the CPU.
    let mut sum = vk.k[0].into_group();
    for (i, w) in witness.iter().enumerate() {
        sum += vk.k[i + 1] * w;
    }
    let sum = sum.into_affine();

    let lhs = Bn254::pairing(proof.a, proof.b);
    let rhs = Bn254::multi_pairing([vk.alpha, sum, proof.c], [vk.beta, vk.gamma, vk.delta]);
    if lhs != rhs {
        return Err(Error::ProofInvalid(
            "pairing check failed: proof is invalid".into(),
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn g1_gen() -> G1Affine {
        G1Affine::generator()
    }
    fn g2_gen() -> G2Affine {
        G2Affine::generator()
    }

    fn write_g1(p: &G1Affine) -> Vec<u8> {
        let mut out = Vec::with_capacity(64);
        out.extend_from_slice(&p.x.into_bigint().to_bytes_be());
        out.extend_from_slice(&p.y.into_bigint().to_bytes_be());
        out
    }

    fn write_g2(p: &G2Affine) -> Vec<u8> {
        let mut out = Vec::with_capacity(128);
        out.extend_from_slice(&p.x.c1.into_bigint().to_bytes_be());
        out.extend_from_slice(&p.x.c0.into_bigint().to_bytes_be());
        out.extend_from_slice(&p.y.c1.into_bigint().to_bytes_be());
        out.extend_from_slice(&p.y.c0.into_bigint().to_bytes_be());
        out
    }

    #[test]
    fn the_generators_round_trip_through_gnarks_encoding() {
        assert_eq!(read_g1(&write_g1(&g1_gen())).unwrap().0, g1_gen());
        assert_eq!(read_g2(&write_g2(&g2_gen())).unwrap().0, g2_gen());
    }

    #[test]
    fn a_compressed_point_recovers_the_root_its_tag_names() {
        for k in 1u64..6 {
            let p = (G1Affine::generator() * Fr::from(k)).into_affine();
            let mut buf = p.x.into_bigint().to_bytes_be();
            buf[0] |= if larger(&p.y) {
                COMPRESSED_LARGEST
            } else {
                COMPRESSED_SMALLEST
            };
            let (got, used) = read_g1(&buf).unwrap();
            assert_eq!(got, p, "k={k}");
            assert_eq!(used, 32, "a compressed point consumes 32 bytes");
        }
    }

    #[test]
    fn a_compressed_g2_point_recovers_the_root_its_tag_names() {
        for k in 1u64..4 {
            let p = (G2Affine::generator() * Fr::from(k)).into_affine();
            let mut buf = Vec::new();
            buf.extend_from_slice(&p.x.c1.into_bigint().to_bytes_be());
            buf.extend_from_slice(&p.x.c0.into_bigint().to_bytes_be());
            buf[0] |= if larger2(&p.y) {
                COMPRESSED_LARGEST
            } else {
                COMPRESSED_SMALLEST
            };
            assert_eq!(read_g2(&buf).unwrap().0, p, "k={k}");
        }
    }

    #[test]
    fn infinity_is_refused_however_it_is_spelled() {
        // All-zero bytes: what a lazily-built key looks like.
        assert!(read_g1(&[0u8; 64]).is_err());
        assert!(read_g2(&[0u8; 128]).is_err());
        // And gnark's explicit infinity tag.
        let mut inf = [0u8; 32];
        inf[0] = COMPRESSED_INFINITY;
        assert!(read_g1(&inf).is_err());
    }

    #[test]
    fn a_non_canonical_coordinate_is_refused() {
        // The modulus itself is not a field element, and neither is anything
        // above it. gnark refuses rather than reducing, so two byte strings
        // never name one point.
        let mut buf = vec![0u8; 64];
        buf[..32].copy_from_slice(&Fq::MODULUS.to_bytes_be());
        assert!(read_g1(&buf).is_err());
    }

    #[test]
    fn a_point_off_the_curve_is_refused() {
        let mut buf = write_g1(&g1_gen());
        buf[63] ^= 1; // y+1 is not on the curve at x=1
        assert!(read_g1(&buf).is_err());
    }

    /// An instance that genuinely satisfies the equation, built the way the
    /// Go emitter builds it: with β = γ = δ = g₂ the check collapses to an
    /// exponent identity, so A = g₁^(a + Σ + c) satisfies it exactly.
    fn instance(pubs: &[Fr], a: u64, c: u64) -> (Proof, VerifyingKey) {
        let ks: Vec<Fr> = (0..=pubs.len())
            .map(|i| Fr::from(i as u64 * 7 + 3))
            .collect();
        let k: Vec<G1Affine> = ks
            .iter()
            .map(|s| (G1Affine::generator() * s).into_affine())
            .collect();
        let mut sum = ks[0];
        for (i, w) in pubs.iter().enumerate() {
            sum += *w * ks[i + 1];
        }
        let a = Fr::from(a);
        let c = Fr::from(c);
        let total = a + sum + c;
        let g1 = G1Affine::generator().into_group();
        (
            Proof {
                a: (g1 * total).into_affine(),
                b: g2_gen(),
                c: (g1 * c).into_affine(),
            },
            VerifyingKey {
                alpha: (g1 * a).into_affine(),
                beta: g2_gen(),
                gamma: g2_gen(),
                delta: g2_gen(),
                k,
            },
        )
    }

    #[test]
    fn a_satisfying_assignment_verifies_and_a_moved_one_does_not() {
        let w = vec![Fr::from(11u64), Fr::from(22u64)];
        let (proof, vk) = instance(&w, 5, 9);
        assert!(verify(&proof, &vk, &w).is_ok());

        let mut moved = proof.clone();
        moved.a = (moved.a.into_group() + g1_gen()).into_affine();
        assert!(verify(&moved, &vk, &w).is_err());

        let mut other_witness = w.clone();
        other_witness[0] += Fr::from(1u64);
        assert!(verify(&proof, &vk, &other_witness).is_err());
    }

    #[test]
    fn the_witness_length_must_be_the_one_the_key_describes() {
        let w = vec![Fr::from(3u64)];
        let (proof, vk) = instance(&w, 1, 2);
        assert!(verify(&proof, &vk, &w).is_ok());
        assert!(verify(&proof, &vk, &[]).is_err());
        assert!(verify(&proof, &vk, &[w[0], Fr::from(4u64)]).is_err());
    }

    #[test]
    fn a_witness_is_reduced_rather_than_refused() {
        // gnark's `SetBytes` reduces; a coordinate's does not. The difference
        // matters: a 32-byte hash above the scalar modulus is a legitimate
        // public input, and it must mean the same thing on both sides.
        let big = [0xFFu8; 32];
        assert_eq!(witness_from_bytes(&big), Fr::from_be_bytes_mod_order(&big));
    }
}
