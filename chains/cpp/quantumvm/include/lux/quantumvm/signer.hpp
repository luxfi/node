// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// signer.hpp — the ML-DSA (FIPS 204) signature a Q-chain transaction carries.
//
// Rendered from Go chains/quantumvm/quantum/signer.go over
// github.com/luxfi/crypto/mldsa. The body underneath is PQClean's reference
// ML-DSA, the same specification circl implements for the Go side, so a
// signature made here verifies there and the reverse.
//
// Three things in this file carry the weight:
//
//   signed_data   what the signature COVERS: the message, the stamp, and the
//                 TIME the stamp was made. The time was once a plain field
//                 nothing signed, so an expired stamp was revived by writing
//                 the current time into it, and the same edit forward produced
//                 one that never expired. A field a verifier trusts has to be a
//                 field the signature covers.
//
//   verify        checks freshness in BOTH directions. Go's time.Since goes
//                 negative for a future date, so a stamp dated ahead compared
//                 as arbitrarily fresh and never expired at all.
//
//   pack_batch    lays a batch out as fixed-width rows, and MEASURES every
//                 input before copying it. Bounding only the low end let an
//                 over-long signature or key at row i overwrite row i+1 — so a
//                 caller supplying a matching over-long pair chose the key the
//                 next entry would be verified under.
//
// THE ACCELERATOR. Go asks an accelerator first and falls back to the CPU on
// any error. That fallback is only safe because a missing accelerator is an
// ERROR: a GPU path that answered "fine" having verified nothing would report
// every signature in the batch as good and the CPU check would never run. This
// build has no accelerator bound, so gpu_batch_verify always refuses — which is
// the same contract, honestly reported, and the CPU path does the work.

#pragma once

#include "lux/quantumvm/clock.hpp"
#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"

#include <cstdint>
#include <span>
#include <vector>

namespace lux::quantumvm::quantum {

// Algorithm versions. The parameter set fixes every key and signature width, so
// this number is the only thing that has to be agreed.
inline constexpr std::uint32_t kMldsa44 = 1;  // NIST Level 2 (128-bit security)
inline constexpr std::uint32_t kMldsa65 = 2;  // NIST Level 3 (192-bit security)
inline constexpr std::uint32_t kMldsa87 = 3;  // NIST Level 5 (256-bit security)

// A quantum-resistant signature, as it rides on the wire.
struct QuantumSignature {
    std::uint32_t algorithm = 0;
    // When the stamp was made, in Unix nanoseconds. It is part of what the
    // signature covers, which is what makes the freshness check meaningful.
    Nanos timestamp = 0;
    Bytes public_key;
    Bytes signature;
    // The public key again, by construction: sign() sets both from one key.
    // A second copy on the wire would be a second thing to disagree, so the
    // parser derives it rather than reading it.
    Bytes corona_key;
    Bytes quantum_stamp;

    bool present() const { return !signature.empty(); }
};

// The per-validator ML-DSA identity key the Q-Chain attests round digests with.
// It is NOT the Corona threshold share — that lives in the threshold protocols
// and feeds the Q-witness aggregation in consensus/protocol/quasar. The "Corona"
// name survives on the RPC method (qvm.generateCoronaKey) for compatibility.
struct MldsaValidatorKey {
    std::uint32_t version = 0;
    Bytes public_key;
    Bytes private_key;
    Bytes nonce;
};

// What the signature covers: message ‖ stamp ‖ big-endian Unix-nanosecond
// stamp time.
Bytes signed_data(ByteView message, ByteView stamp, Nanos stamped);

class QuantumSigner {
  public:
    // A version that does not exist is refused. Falling through to ML-DSA-65
    // meant an operator who asked for something else got a chain signing under a
    // parameter set nobody chose, and never heard about it — and it made the
    // signer disagree with the config about what "unset" means.
    static Result<QuantumSigner> make(std::uint32_t algorithm_version, Duration stamp_window);

    Result<MldsaValidatorKey> generate_key() const;

    Result<QuantumSignature> sign(ByteView message, const MldsaValidatorKey* key) const;
    Status verify(ByteView message, const QuantumSignature* sig) const;

    // The batch the VM actually calls, and the threshold at which it would hand
    // the work to an accelerator.
    Status parallel_verify(const std::vector<Bytes>& messages,
                           const std::vector<const QuantumSignature*>& signatures) const;
    Status parallel_verify_with_threshold(const std::vector<Bytes>& messages,
                                          const std::vector<const QuantumSignature*>& signatures,
                                          int gpu_threshold) const;

    // The three flat row-major buffers a batched verifier takes, one row per
    // signature. Public because its bounds are the property under test.
    struct Batch {
        Bytes messages;  // n rows of max_message_len
        Bytes signatures;
        Bytes public_keys;
        std::size_t max_message_len = 0;
    };
    Result<Batch> pack_batch(const std::vector<Bytes>& messages,
                             const std::vector<const QuantumSignature*>& signatures) const;

    // Refuses, always, on a build with no accelerator bound. See the header
    // comment: this is the contract, not a gap.
    Status gpu_batch_verify(const std::vector<Bytes>& messages,
                            const std::vector<const QuantumSignature*>& signatures) const;
    static bool accelerator_available();

    std::size_t signature_size() const;
    std::size_t public_key_size() const;
    std::uint32_t mode() const { return version_; }
    Duration stamp_window() const { return window_; }

  private:
    QuantumSigner(std::uint32_t v, Duration w) : version_(v), window_(w) {}

    Status cpu_parallel_verify(const std::vector<Bytes>& messages,
                               const std::vector<const QuantumSignature*>& signatures) const;

    std::uint32_t version_ = 0;
    Duration window_{};
};

}  // namespace lux::quantumvm::quantum
