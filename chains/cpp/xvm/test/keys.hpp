// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// keys.hpp — the five deterministic test keys, and signing with them.
//
// These are the SAME private keys Go's secp256k1.TestKeys() returns, so an
// address derived here must equal the one Go derives — which is itself a test
// (see keys_derive_the_go_addresses in fx_test.cpp). Deriving rather than
// hard-coding the address is deliberate: hard-coding both sides would let a
// broken derivation agree with itself.

#pragma once

#include "check.hpp"

#include "lux/crypto/secp256k1.h"
#include "lux/xvm/fx.hpp"

#include <cstring>

// The C++ ECDSA signer lives beside the recovery path in luxcpp/crypto, and is
// reused rather than re-declared — a second declaration is a second definition
// of the contract.
#include "ecdsa.hpp"

namespace lux::xvm::test {

using PrivateKey = std::array<std::uint8_t, 32>;

inline PrivateKey test_key(int i) {
    static const char* hexes[5] = {
        "8c2bae69b0e1f6f3a5e784504ee93279226f997c5a6771b9bd6b881a8fee1e9d",
        "b1ed77ad48555d49f03a7465f0685a7d86bfd5f3a3ccf1be01971ea8dec5471c",
        "51a5e21237263396a5dfce60496d0ca3829d23fd33c38e6d13ae53b4810df9ca",
        "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027",
        "bb56fc254e71a6a5fd2655b337b7cb7a42543aa5ffefa3efd839bdcd0ca680e5",
    };
    PrivateKey k{};
    Bytes b = from_hex(hexes[i % 5]);
    std::memcpy(k.data(), b.data(), 32);
    return k;
}

// compressed_pubkey is the 33-byte form the Lux address commits to.
inline std::array<std::uint8_t, 33> compressed_pubkey(const PrivateKey& sk) {
    std::uint8_t uncompressed[64]{};
    (void)lux::crypto::secp256k1::secret_to_public(sk.data(), uncompressed);
    std::array<std::uint8_t, 33> out{};
    out[0] = std::uint8_t(0x02 | (uncompressed[63] & 1));
    std::memcpy(out.data() + 1, uncompressed, 32);
    return out;
}

inline ShortId test_address(int i) { return pubkey_to_address(view(compressed_pubkey(test_key(i)))); }

// sign_hash produces the 65-byte recoverable signature (r ‖ s ‖ v) the fx
// credentials carry.
inline fx::Signature sign_hash(const PrivateKey& sk, const Id& hash) {
    fx::Signature sig{};
    std::uint8_t recid = 0;
    (void)lux::crypto::secp256k1::sign(sk.data(), hash.data(), sig.data(), &recid);
    sig[64] = recid;
    return sig;
}

// sign_unsigned_tx signs the tx's SIGNING TARGET: sha256 of its unsigned wire
// bytes. This is the one place a test decides what a signature is over, so a
// wrong answer here shows up as a failed spend rather than as silence.
inline fx::Signature sign_unsigned_tx(const PrivateKey& sk, ByteView unsigned_bytes) {
    return sign_hash(sk, sha256(unsigned_bytes));
}

}  // namespace lux::xvm::test
