// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// signer.hpp — the half of ML-DSA the F-Chain does not have.
//
// F verifies. It never signs and it never makes a key: a payer signs offline
// with a key F never sees, and the package deliberately offers no way to do
// either (auth.hpp). A test still has to produce a real signature to exercise
// the path that verifies one, so it reaches the FIPS 204 implementation here —
// on the TEST side of the line, where the invariant scan can see that it is.

#pragma once

#include "lux/fhevm/auth.hpp"

#include "mldsa.hpp"

namespace lux::fhevm::test {

// FIPS 204 fixes the private key's width for ML-DSA-65 at 4032 bytes.
inline constexpr std::size_t kSecretKeySize = 4032;

inline bool make_keypair(Bytes* public_key, Bytes* secret_key) {
    Bytes pk(auth::kPublicKeySize);
    Bytes sk(kSecretKeySize);
    if (!lux::crypto::mldsa::keypair_65(pk.data(), sk.data())) return false;
    *public_key = std::move(pk);
    *secret_key = std::move(sk);
    return true;
}

inline bool make_signature(ByteView secret_key, ByteView msg, Bytes* sig) {
    if (secret_key.size() != kSecretKeySize) return false;
    Bytes out(auth::kSignatureSize);
    std::size_t n = out.size();
    if (!lux::crypto::mldsa::sign_65(out.data(), &n, msg.data(), msg.size(), secret_key.data())) {
        return false;
    }
    out.resize(n);
    *sig = std::move(out);
    return true;
}

}  // namespace lux::fhevm::test
