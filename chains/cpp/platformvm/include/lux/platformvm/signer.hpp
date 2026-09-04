// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// signer.hpp — the BLS key a validator registers, and the proof it owns it.
//
// Rendered from Go vms/platformvm/signer (signer.go, empty.go,
// proof_of_possession.go) over github.com/luxfi/crypto/bls.
//
// A proof of possession is what stops a rogue-key attack on an aggregate: a
// validator that could publish a public key it does not hold could choose that
// key so the aggregate of the set signs whatever it likes. The proof is a
// signature by the key over its OWN 48 compressed bytes under the
// possession domain tag —
//
//     BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_
//
// distinct from the vote tag so a proof is never a vote and a vote is never a
// proof. The verification here is a real pairing against blst; there is no
// mode in which it answers yes without one.
//
// Two shapes, one interface: Empty registers no key (a validator of a network
// that is not the primary one), ProofOfPossession registers one. The wire
// discriminates on a single byte, so the shape is data, not a subclass.

#pragma once

#include "lux/platformvm/error.hpp"

#include <array>
#include <cstdint>
#include <span>
#include <variant>

namespace lux::platformvm::signer {

inline constexpr std::size_t kPublicKeyLen = 48;
inline constexpr std::size_t kSignatureLen = 96;

using PublicKeyBytes = std::array<std::uint8_t, kPublicKeyLen>;
using SignatureBytes = std::array<std::uint8_t, kSignatureLen>;

// The possession ciphersuite, byte-identical to Go bls.dstPoP.
inline constexpr char kPopDst[] = "BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_";
inline constexpr std::size_t kPopDstLen = sizeof(kPopDst) - 1;  // 43, drops the terminator

// A 48-byte string is a public key iff it decompresses to a non-identity point
// of the prime-order subgroup of G1. Mirrors Go PublicKeyFromCompressedBytes.
bool public_key_valid(const PublicKeyBytes& pk);

// A 96-byte string is a signature iff it decompresses to a non-identity point
// of the prime-order subgroup of G2. Mirrors Go SignatureFromBytes.
bool signature_valid(const SignatureBytes& sig);

// The pairing check e(pk, H_pop(msg)) == e(G1, sig). Real, or not at all.
bool verify_pop(const PublicKeyBytes& pk, const SignatureBytes& sig, std::span<const std::uint8_t> msg);

struct Empty {
    friend bool operator==(const Empty&, const Empty&) = default;
};

struct ProofOfPossession {
    PublicKeyBytes public_key{};
    SignatureBytes proof{};

    friend bool operator==(const ProofOfPossession&, const ProofOfPossession&) = default;

    // The signed message is the public key itself.
    lux::platformvm::Status verify() const {
        if (!public_key_valid(public_key)) return fail(Err::InvalidPublicKey);
        if (!signature_valid(proof)) return fail(Err::InvalidSignature);
        if (!verify_pop(public_key, proof, {public_key.data(), public_key.size()}))
            return fail(Err::InvalidProofOfPossession);
        return ok();
    }
};

// Signer is the sum of the two shapes. A key exists only in the second arm, so
// "does this validator register a key" is a question about the value, not a
// nullable field someone can forget to check.
using Signer = std::variant<Empty, ProofOfPossession>;

inline lux::platformvm::Status verify(const Signer& s) {
    if (const auto* p = std::get_if<ProofOfPossession>(&s)) return p->verify();
    return ok();
}

// The registered key, if there is one. Invariant, inherited from the reference:
// only meaningful after verify() has returned success.
inline const PublicKeyBytes* key(const Signer& s) {
    if (const auto* p = std::get_if<ProofOfPossession>(&s)) return &p->public_key;
    return nullptr;
}

inline bool has_key(const Signer& s) { return std::holds_alternative<ProofOfPossession>(s); }

}  // namespace lux::platformvm::signer
