// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// auth.hpp — how the F-Chain knows who asked.
//
// One algorithm, ML-DSA-65 (FIPS 204, NIST level 3), which is the platform
// service-identity scheme. Authentication here is a PUBLIC operation: the chain
// parses a payer's public key and verifies a signature over the transaction's
// preimage. It never possesses a secret, and there is nothing in this VM that
// could hold one.
//
// The verifier underneath is luxcpp/crypto's ML-DSA — the same FIPS 204 pure
// variant with an empty context that the Go chain verifies with, which is a
// claim the cross-language test checks against Go-produced signatures rather
// than asserting.

#pragma once

#include "lux/fhevm/id.hpp"

namespace lux::fhevm::auth {

// FIPS 204 fixes both widths for ML-DSA-65.
inline constexpr std::size_t kPublicKeySize = 1952;
inline constexpr std::size_t kSignatureSize = 3309;

// public_key_valid is Go's mldsa.PublicKeyFromBytes: it accepts exactly a
// well-sized key. ML-DSA's encoding has no further structure to reject — every
// 1952-byte string unpacks — so the width IS the check, and saying so here
// keeps a caller from believing a stronger one happened.
bool public_key_valid(ByteView public_key);

// verify reports whether sig is a valid ML-DSA-65 signature of msg under pk.
//
// VERIFY IS THE WHOLE SURFACE. There is deliberately no signing entry point and
// no key generation here: a payer signs offline with a key F never sees, and a
// library that COULD sign is a library that has to be handed something to sign
// with. A test that needs to sign reaches the FIPS 204 implementation directly
// (test/signer.hpp), which keeps the one direction F actually performs — public
// verification — the only direction this package can perform at all.
bool verify(ByteView public_key, ByteView msg, ByteView sig);

}  // namespace lux::fhevm::auth
