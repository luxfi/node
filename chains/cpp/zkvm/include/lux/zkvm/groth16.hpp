// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// groth16.hpp — the classical (bn254 pairing-based) shielded proof system, and
// the point discipline that keeps it honest.
//
// The curve arithmetic is luxcpp/crypto's first-party bn254 — reused in place,
// not vendored, not reimplemented. What is here is the ENCODING and the CHECKS:
// the byte layout is gnark-crypto's (the Go reference decodes with
// G1Affine.Unmarshal / G2Affine.Unmarshal), and a Z-Chain that read those bytes
// differently would accept proofs Go refuses, or refuse proofs Go accepts.
//
// A POINT THIS VERIFIER READS HAS TO BE IN THE PRIME-ORDER SUBGROUP AND MUST NOT
// BE THE POINT AT INFINITY. Both halves are load-bearing: the encoding renders
// infinity as all-zero bytes and reports it in-subgroup, and a pairing drops any
// term whose argument is infinity — so an element at infinity removes itself
// from the equation and leaves a weaker check than the one written down.
// check_g1 and check_g2 are the ONE place that decides what a usable point is,
// and every decoder below routes through them.
//
// This path exists only on a NON-strict chain. The Z-Chain's default profile is
// strict-PQ, where the verifier refuses every classical system before reaching
// any of this.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/wire.hpp"

#include "bn254_g1.hpp"
#include "bn254_g2.hpp"

#include <vector>

namespace lux::zkvm::groth16 {

using G1 = lux::crypto::bn254::G1Affine;
using G2 = lux::crypto::bn254::G2Affine;
using U256 = lux::crypto::bn254::U256;

inline constexpr const char* kErrOffSubgroup = "point not in the prime-order subgroup";
inline constexpr const char* kErrAtInfinity = "point at infinity";
inline constexpr const char* kErrPairingFailed = "pairing check failed: proof is invalid";

struct Proof {
    G1 ar{};   // A
    G2 bs{};   // B
    G1 krs{};  // C
};

struct VerifyingKey {
    G1 alpha{};
    G2 beta{};
    G2 gamma{};
    G2 delta{};
    std::vector<G1> k;  // one point per public input, plus the constant term
};

// set_bytes_g1 / set_bytes_g2 decode ONE point in gnark-crypto's encoding: the
// two most significant bits of byte 0 say uncompressed (00), compressed with the
// smallest (10) or largest (11) square root, or compressed infinity (01). A
// coordinate must be canonical — strictly below the field modulus — and the
// decoded point must be in the prime-order subgroup, both of which the reference
// decoder enforces at this same point.
wire::Result<G1> set_bytes_g1(ByteView buf);
wire::Result<G2> set_bytes_g2(ByteView buf);

wire::Result<void> check_g1(const G1& p);
wire::Result<void> check_g2(const G2& p);

// fr_from_bytes reads big-endian bytes as a scalar field element, reducing
// modulo the group order — which is what the reference's fr.Element.SetBytes
// does for any length, including one that is not 32 bytes and one that is not
// already reduced.
U256 fr_from_bytes(ByteView b);

// deserialize_proof reads Ar (64) ‖ Bs (128) ‖ Krs (64). A proof it returns has
// already passed the point checks, so callers never repeat them.
wire::Result<Proof> deserialize_proof(ByteView data);

// deserialize_verifying_key reads
//   Alpha (64) | Beta (128) | Gamma (128) | Delta (128) | numK (4) | K[..] (64·numK)
wire::Result<VerifyingKey> deserialize_verifying_key(ByteView data);

// validate_verifying_key checks every point of a key. A trusted setup never
// produces infinity for alpha, beta, gamma, delta or K, so a key that carries
// one is not a setup output and its pairing equation would collapse to something
// weaker.
wire::Result<void> validate_verifying_key(const VerifyingKey& vk);

// verify_pairing performs the Groth16 check
//
//   e(A, B) = e(alpha, beta) · e(sum(pubInput_i · K_i), gamma) · e(C, delta)
//
// K carries one point per public input plus the constant term K[0], so a key of
// n points speaks about exactly n-1 public inputs — that count is a property of
// the circuit the key was made for, not something a transaction chooses.
//
// The witness arrives with the transaction, so a peer picks its length, and the
// sum below runs over the witness against K. A length either side of what the
// key states judges the proof against a statement the key does not describe —
// past the end of K in one direction, and short of the trailing K points in the
// other — so it has to be exactly what the key says.
//
// The public-input linear combination stays on the CPU. Every node has to reach
// the same point from the same key and witness, so it is never handed to an
// accelerator whose answer is not compared against this one.
wire::Result<void> verify_pairing(const Proof& proof, const VerifyingKey& vk,
                                  const std::vector<U256>& witness);

}  // namespace lux::zkvm::groth16
