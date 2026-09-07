// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// scheme.hpp — the threshold signature schemes this chain can be given.
//
// Rendered from the Go reference's crypto/threshold registry (HasScheme,
// GetScheme, and the init() that registers one). The chain knows what a scheme
// must ANSWER and knows nothing about how it answers: a scheme is registered
// from outside, and the wire kinds that need it stop refusing the moment one
// is.
//
// WHY A REGISTRY RATHER THAN A VERIFIER.
//
// A signature routine written from a paper and checked against nothing verifies
// whatever it is given, and it fails OPEN — every forgery it does not
// understand looks like a signature it does not understand. The lattice scheme
// the post-quantum warp signature names has no implementation in this estate to
// render from and no vectors to check against, so writing one here would be
// inventing an admission rule for the validator set of every sovereign network.
//
// It has none in the REFERENCE either, which is the fact that settles the
// shape. The Go P-chain reaches its Corona verifier through this same registry,
// and the only scheme registered anywhere in its dependency graph is BLS
// (luxfi/threshold ships scheme/bls and nothing else), so its own
// VerifyCoronaSignature returns false for want of a scheme. Refusing is
// therefore not this port falling short of the reference; it is the reference's
// behaviour, reached the reference's way. What this buys is that the missing
// piece is a REGISTRATION rather than a rewrite: the day a Corona
// implementation exists, it registers itself and nothing in this chain changes.

#pragma once

#include "lux/platformvm/error.hpp"

#include <cstdint>
#include <memory>
#include <span>
#include <vector>

namespace lux::platformvm::scheme {

// Which scheme. The values are the Go reference's own scheme ids.
enum class Id : std::uint8_t {
    Bls = 0,
    Corona = 1,
};

std::string_view name(Id id);

// What a threshold scheme must answer.
//
// Both questions are about a GROUP: a threshold signature is one signature by a
// key that no single signer holds, so the only key it can be checked against is
// the group's.
class Threshold {
  public:
    virtual ~Threshold() = default;

    // The one key a set of shares verifies against. Every share of a threshold
    // key verifies against the same group key, so keys that differ have no
    // aggregate — returning the first would check one signer's key while the
    // caller's quorum tally counted all of them.
    virtual Result<std::vector<std::uint8_t>> aggregate(
        const std::vector<std::vector<std::uint8_t>>& public_keys) const = 0;

    virtual bool verify(std::span<const std::uint8_t> public_key, std::span<const std::uint8_t> message,
                        std::span<const std::uint8_t> signature) const = 0;
};

// Registers a scheme. Registering an id twice is refused rather than allowed to
// win: which of two implementations checked a signature is not a thing a node
// should be deciding by link order.
Status hold(Id id, std::shared_ptr<const Threshold> impl);

// Whether a scheme is held. This is the question the wire kinds ask, and the
// honest answer today for Corona is no.
bool held(Id id);

// The scheme, or a refusal that says which one is missing.
Result<std::shared_ptr<const Threshold>> of(Id id);

// Forgets a scheme. Only a test has any business calling this — a node that
// dropped a scheme mid-run would start refusing signatures it had accepted.
void release(Id id);

}  // namespace lux::platformvm::scheme
