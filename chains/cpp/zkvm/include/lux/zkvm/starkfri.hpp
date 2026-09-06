// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// starkfri.hpp — the strict-PQ STARK/FRI verification seam (Go:
// luxfi/precompile/starkfri).
//
// The Z-Chain's shielded proofs are STARK/FRI: cSHAKE256 Merkle over Goldilocks
// and a FRI low-degree test — no pairings, no bn254, no trusted setup. The
// verifier itself is an out-of-band component (the Plonky3-derived prover and
// its C ABI), so what lives here is the BINDING, exactly as it does in Go:
//
//   register        this IS the verifier (forces, overriding any earlier one)
//   register_default be the verifier iff nobody better volunteered
//   verify           the entry point every caller uses
//
// UNBOUND MEANS REFUSE. With no verifier registered, verify returns
// kErrVerifierNotRegistered — never "ok". A structurally well-formed proof is
// NEVER accepted without the real verifier, so there is no forgery oracle. That
// is not a placeholder: it is the shipped posture of the Go reference in every
// build that does not carry the prover binding, and it is the correct one.
//
// PROVER STATUS (tracked, not faked). The full post-quantum shielded path also
// needs the prover side: a p3q_prove ABI and a shielded AIR that arithmetises
// the spend/output circuit — note commitments, nullifier derivation, value
// balance, range proofs — over the Goldilocks field. Until that AIR and prover
// land, this verifier accepts no shielded proof on a strict-PQ chain, and
// shielded value transfer is effectively disabled there. That is the correct
// posture: no classical fallback, no forgeable path.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/wire.hpp"

#include <functional>
#include <string>

namespace lux::zkvm::starkfri {

// MagicHeader is the 4-byte prefix every STARK-FRI proof MUST begin with.
inline constexpr const char* kMagicHeader = "P3Q1";

// VersionV1 is the first wire-format version.
inline constexpr std::uint8_t kVersionV1 = 0x01;

inline constexpr const char* kErrVerifierNotRegistered = "starkfri: verifier not registered";
inline constexpr const char* kErrInvalidProof = "starkfri: proof verification failed";

// Verifier is the callback that bridges to the out-of-band STARK/FRI verifier.
// It returns true for a verified proof, false for a well-formed proof that did
// not verify, and an error for an internal failure.
using Verifier = std::function<wire::Result<bool>(std::uint8_t version, ByteView proof,
                                                  ByteView public_inputs)>;

// register_verifier is the authoritative seam: it FORCES the given verifier,
// overriding any previously-registered one. Passing an empty function clears the
// registration, so verify refuses again.
void register_verifier(Verifier fn);

// register_default_verifier installs fn ONLY IF none is registered. It is the
// safe-refuse seam a host wires so the path never silently no-ops, without
// clobbering a real verifier something else already installed. Returns true iff
// it installed fn.
bool register_default_verifier(Verifier fn);

bool registered();

// verify checks the structural prefix and then the registered verifier.
//
//   ok            a verified proof
//   false         a well-formed proof that did not verify
//   error         no verifier registered, a bad magic header, or a verifier
//                 failure — each named, so an operator can tell them apart.
wire::Result<bool> verify(ByteView proof, ByteView public_inputs);

}  // namespace lux::zkvm::starkfri
