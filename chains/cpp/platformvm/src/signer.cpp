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

}  // namespace lux::platformvm::signer
