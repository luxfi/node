// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fx.cpp — the signature check, done for real.
//
// Rendered from github.com/luxfi/utxo/secp256k1fx over
// github.com/luxfi/crypto/secp256k1 and github.com/luxfi/crypto/hash. The
// recovery is the first-party secp256k1 in luxcpp/crypto and the address hash
// is its ripemd160; neither is stubbed, and there is no build in which they are.

#include "lux/platformvm/fx.hpp"

#include "lux/crypto/secp256k1.h"
#include "ripemd160.hpp"

#include <cstring>

namespace lux::platformvm::fx {
namespace {

constexpr std::size_t kSigLen = 65;

}  // namespace

ShortId address_of_compressed_key(std::span<const std::uint8_t> compressed) {
    const Id h = sha256(compressed);
    std::byte out[cevm::crypto::RIPEMD160_HASH_SIZE];
    cevm::crypto::ripemd160(out, reinterpret_cast<const std::byte*>(h.data()), h.size());
    ShortId addr{};
    std::memcpy(addr.data(), out, kShortIdLen);
    return addr;
}

Result<ShortId> recover_address(const Id& hash, std::span<const std::uint8_t> sig65) {
    if (sig65.size() != kSigLen) return fail(Err::UnrecoverableSignature, "signature is not 65 bytes");
    const std::uint8_t v = sig65[64];
    if (v > 1) return fail(Err::UnrecoverableSignature, "recovery id is not 0 or 1");

    std::uint8_t pub[64];
    const auto st = secp256k1_ecrecover(hash.data(), sig65.data(), sig65.data() + 32, v, pub);
    if (st != SECP256K1_OK) return fail(Err::UnrecoverableSignature, "public key recovery failed");

    // The address is taken over the COMPRESSED key: 0x02/0x03 by the parity of
    // y, then x. Recovery hands back x ‖ y, so the parity is the last byte of y.
    std::uint8_t compressed[33];
    compressed[0] = static_cast<std::uint8_t>(0x02 | (pub[63] & 1));
    std::memcpy(compressed + 1, pub, 32);
    return address_of_compressed_key({compressed, sizeof(compressed)});
}

Status Fx::verify_credentials(std::span<const std::uint8_t> tx_bytes,
                              const std::vector<std::uint32_t>& sig_indices, const txs::Credential& cred,
                              const OutputOwners& owners, std::uint64_t now) const {
    const std::size_t num_sigs = sig_indices.size();
    if (owners.locktime > now) return fail(Err::Timelocked);
    if (owners.threshold < num_sigs) return fail(Err::TooManySigners);
    if (owners.threshold > num_sigs) return fail(Err::TooFewSigners);
    if (num_sigs != cred.sigs.size()) return fail(Err::InputCredentialSignersMismatch);
    // A node replaying history the network already agreed on does not re-derive
    // every key; it re-checks them all once it is caught up. Go names this the
    // same way and for the same reason.
    if (!bootstrapped_) return ok();

    const Id tx_hash = sha256(tx_bytes);
    for (std::size_t i = 0; i < num_sigs; ++i) {
        const std::uint32_t index = sig_indices[i];
        if (index >= owners.addrs.size()) return fail(Err::InputOutputIndexOutOfBounds);
        auto addr = recover_address(tx_hash, {cred.sigs[i].data(), cred.sigs[i].size()});
        if (!addr) return std::unexpected(addr.error());
        if (!(owners.addrs[index] == addr.value()))
            return fail(Err::WrongSig, "signature is from " + hex(addr.value()) + ", not " +
                                           hex(owners.addrs[index]));
    }
    return ok();
}

Status Fx::verify_transfer(std::span<const std::uint8_t> tx_bytes, const TransferInput& in,
                           const txs::Credential& cred, const TransferOutput& utxo, std::uint64_t now) const {
    if (auto s = utxo.verify(); !s) return s;
    if (auto s = in.verify(); !s) return s;
    if (utxo.amt != in.amt)
        return fail(Err::MismatchedAmounts,
                    std::to_string(utxo.amt) + " != " + std::to_string(in.amt));
    return verify_credentials(tx_bytes, in.sig_indices, cred, utxo.owners, now);
}

Status Fx::verify_permission(std::span<const std::uint8_t> tx_bytes, const txs::Auth& auth,
                             const txs::Credential& cred, const OutputOwners& owners,
                             std::uint64_t now) const {
    if (auto s = owners.verify(); !s) return s;
    if (auto s = txs::verify_auth(auth); !s) return s;
    return verify_credentials(tx_bytes, auth, cred, owners, now);
}

}  // namespace lux::platformvm::fx
