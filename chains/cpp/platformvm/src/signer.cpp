// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// signer.cpp — the BLS12-381 leg of the validator's key, over blst.
//
// blst is the same library the Go node reaches through supranational/blst, so a
// proof accepted here is a proof accepted there. Nothing about the scheme is
// invented; only the domain tag distinguishes a possession proof from a vote,
// and that tag is spelled once, in signer.hpp.

#include "lux/platformvm/signer.hpp"

#include <blst.h>

namespace lux::platformvm::signer {

bool public_key_valid(const PublicKeyBytes& pk) {
    blst_p1_affine p{};
    if (blst_p1_uncompress(&p, pk.data()) != BLST_SUCCESS) return false;
    if (blst_p1_affine_is_inf(&p)) return false;
    return blst_p1_affine_in_g1(&p);
}

bool signature_valid(const SignatureBytes& sig) {
    blst_p2_affine s{};
    if (blst_p2_uncompress(&s, sig.data()) != BLST_SUCCESS) return false;
    if (blst_p2_affine_is_inf(&s)) return false;
    return blst_p2_affine_in_g2(&s);
}

bool verify_pop(const PublicKeyBytes& pk, const SignatureBytes& sig, std::span<const std::uint8_t> msg) {
    blst_p1_affine p{};
    if (blst_p1_uncompress(&p, pk.data()) != BLST_SUCCESS) return false;
    blst_p2_affine s{};
    if (blst_p2_uncompress(&s, sig.data()) != BLST_SUCCESS) return false;
    return blst_core_verify_pk_in_g1(&p, &s, /*hash_or_encode=*/true, msg.data(), msg.size(),
                                     reinterpret_cast<const byte*>(kPopDst), kPopDstLen,
                                     /*aug=*/nullptr, /*aug_len=*/0) == BLST_SUCCESS;
}

bool verify_signature(const PublicKeyBytes& pk, const SignatureBytes& sig,
                      std::span<const std::uint8_t> msg) {
    blst_p1_affine p{};
    if (blst_p1_uncompress(&p, pk.data()) != BLST_SUCCESS) return false;
    if (blst_p1_affine_is_inf(&p)) return false;
    blst_p2_affine s{};
    if (blst_p2_uncompress(&s, sig.data()) != BLST_SUCCESS) return false;
    return blst_core_verify_pk_in_g1(&p, &s, /*hash_or_encode=*/true, msg.data(), msg.size(),
                                     reinterpret_cast<const byte*>(kSigDst), kSigDstLen,
                                     /*aug=*/nullptr, /*aug_len=*/0) == BLST_SUCCESS;
}

std::optional<PublicKeyBytes> aggregate_public_keys(const std::vector<PublicKeyBytes>& keys) {
    if (keys.empty()) return std::nullopt;
    blst_p1 acc{};
    bool first = true;
    for (const auto& k : keys) {
        blst_p1_affine a{};
        if (blst_p1_uncompress(&a, k.data()) != BLST_SUCCESS) return std::nullopt;
        if (blst_p1_affine_is_inf(&a)) return std::nullopt;
        if (first) {
            blst_p1_from_affine(&acc, &a);
            first = false;
        } else {
            blst_p1_add_or_double_affine(&acc, &acc, &a);
        }
    }
    PublicKeyBytes out{};
    blst_p1_compress(out.data(), &acc);
    return out;
}

lux::platformvm::Result<std::vector<std::uint8_t>> uncompress_for_set(const PublicKeyBytes& compressed) {
    blst_p1_affine a{};
    if (blst_p1_uncompress(&a, compressed.data()) != BLST_SUCCESS)
        return fail(Err::InvalidPublicKey, "the registered key does not decompress");
    std::vector<std::uint8_t> out(96);
    blst_p1_affine_serialize(out.data(), &a);
    return out;
}

std::optional<PublicKeyBytes> compress_public_key(std::span<const std::uint8_t> uncompressed) {
    if (uncompressed.size() != 96) return std::nullopt;
    blst_p1_affine a{};
    if (blst_p1_deserialize(&a, uncompressed.data()) != BLST_SUCCESS) return std::nullopt;
    if (blst_p1_affine_is_inf(&a)) return std::nullopt;
    if (!blst_p1_affine_in_g1(&a)) return std::nullopt;
    PublicKeyBytes out{};
    blst_p1_affine_compress(out.data(), &a);
    return out;
}

}  // namespace lux::platformvm::signer
