// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/starkfri.hpp"

#include <cstring>
#include <mutex>

namespace lux::zkvm::starkfri {
namespace {

std::mutex& mu() {
    static std::mutex m;
    return m;
}

Verifier& slot() {
    static Verifier v;
    return v;
}

}  // namespace

void register_verifier(Verifier fn) {
    std::lock_guard<std::mutex> g(mu());
    slot() = std::move(fn);
}

bool register_default_verifier(Verifier fn) {
    if (!fn) return false;
    std::lock_guard<std::mutex> g(mu());
    if (slot()) return false;
    slot() = std::move(fn);
    return true;
}

bool registered() {
    std::lock_guard<std::mutex> g(mu());
    return static_cast<bool>(slot());
}

wire::Result<bool> verify(ByteView proof, ByteView public_inputs) {
    const std::size_t magic = std::strlen(kMagicHeader);
    if (proof.size() < magic || std::memcmp(proof.data(), kMagicHeader, magic) != 0)
        return std::unexpected(kErrInvalidProof);

    Verifier fn;
    {
        std::lock_guard<std::mutex> g(mu());
        fn = slot();
    }
    if (!fn) return std::unexpected(kErrVerifierNotRegistered);
    return fn(kVersionV1, proof, public_inputs);
}

}  // namespace lux::zkvm::starkfri
