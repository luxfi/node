// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/auth.hpp"

#include "mldsa.hpp"

namespace lux::fhevm::auth {

bool public_key_valid(ByteView public_key) { return public_key.size() == kPublicKeySize; }

bool verify(ByteView public_key, ByteView msg, ByteView sig) {
    if (!public_key_valid(public_key)) return false;
    if (sig.size() != kSignatureSize) return false;
    return lux::crypto::mldsa::verify_65(sig.data(), sig.size(), msg.data(), msg.size(),
                                         public_key.data());
}

}  // namespace lux::fhevm::auth
