// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// signing.hpp — a key, for tests that need a real signature.
//
// Signing belongs to a wallet, not to a VM, so it lives here rather than in the
// chain. It is the first-party ECDSA from luxcpp/crypto, so a transaction signed
// with it and verified by the fx exercises the real recovery — the same one the
// golden vector already proved agrees with the Go reference.

#pragma once

#include "lux/platformvm/fx.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/sha256.hpp"
#include "lux/platformvm/txs.hpp"

#include "ecdsa.hpp"

#include <blst.h>

#include <array>
#include <cstdint>
#include <cstring>

namespace pvmtest {

class Key {
  public:
    // The same secret the golden generator used: bytes 1..32.
    explicit Key(std::uint8_t first = 1) {
        for (std::size_t i = 0; i < sk_.size(); ++i) sk_[i] = static_cast<std::uint8_t>(first + i);
        std::uint8_t pk[64];
        lux::crypto::secp256k1::secret_to_public(sk_.data(), pk);
        std::uint8_t compressed[33];
        compressed[0] = static_cast<std::uint8_t>(0x02 | (pk[63] & 1));
        std::memcpy(compressed + 1, pk, 32);
        addr_ = lux::platformvm::fx::address_of_compressed_key({compressed, sizeof(compressed)});
    }

    const lux::platformvm::ShortId& address() const { return addr_; }

    // A credential over the unsigned bytes of a transaction.
    lux::platformvm::txs::Credential sign(std::span<const std::uint8_t> unsigned_bytes) const {
        const auto h = lux::platformvm::sha256(unsigned_bytes);
        std::uint8_t sig[64];
        std::uint8_t recid = 0;
        lux::crypto::secp256k1::sign(sk_.data(), h.data(), sig, &recid);
        std::array<std::uint8_t, lux::platformvm::txs::kSigLen> out{};
        std::memcpy(out.data(), sig, 64);
        out[64] = recid;
        lux::platformvm::txs::Credential c;
        c.sigs.push_back(out);
        return c;
    }

  private:
    std::array<std::uint8_t, 32> sk_{};
    lux::platformvm::ShortId addr_{};
};

// A BLS key and its proof of possession, for the primary-network validator
// transaction — which REQUIRES one, because an aggregate signed by a key nobody
// holds is an aggregate anyone can forge.
class BlsKey {
  public:
    explicit BlsKey(std::uint8_t seed = 1) {
        std::uint8_t ikm[32];
        for (std::size_t i = 0; i < sizeof(ikm); ++i) ikm[i] = static_cast<std::uint8_t>(seed + i);
        blst_keygen(&sk_, ikm, sizeof(ikm), nullptr, 0);

        blst_p1 pk{};
        blst_sk_to_pk_in_g1(&pk, &sk_);
        blst_p1_compress(pop_.public_key.data(), &pk);

        // The message is the public key itself, under the possession tag.
        blst_p2 hash{};
        blst_hash_to_g2(&hash, pop_.public_key.data(), pop_.public_key.size(),
                        reinterpret_cast<const byte*>(lux::platformvm::signer::kPopDst),
                        lux::platformvm::signer::kPopDstLen, nullptr, 0);
        blst_p2 sig{};
        blst_sign_pk_in_g1(&sig, &hash, &sk_);
        blst_p2_compress(pop_.proof.data(), &sig);
    }

    const lux::platformvm::signer::ProofOfPossession& pop() const { return pop_; }

  private:
    blst_scalar sk_{};
    lux::platformvm::signer::ProofOfPossession pop_{};
};

}  // namespace pvmtest
