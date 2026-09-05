// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/signer.hpp"

#include "lux/quantumvm/sha512.hpp"

#include "mldsa.hpp"

#include <algorithm>
#include <cstring>
#include <random>
#include <string>
#include <thread>

namespace lux::quantumvm::quantum {
namespace {

struct Widths {
    std::size_t pk = 0;
    std::size_t sk = 0;
    std::size_t sig = 0;
};

Widths widths_of(std::uint32_t version) {
    namespace m = lux::crypto::mldsa;
    switch (version) {
        case kMldsa44: return {m::PK44, m::SK44, m::SIG44};
        case kMldsa65: return {m::PK65, m::SK65, m::SIG65};
        case kMldsa87: return {m::PK87, m::SK87, m::SIG87};
        default: return {};
    }
}

bool keypair(std::uint32_t version, std::uint8_t* pk, std::uint8_t* sk) {
    namespace m = lux::crypto::mldsa;
    switch (version) {
        case kMldsa44: return m::keypair_44(pk, sk);
        case kMldsa65: return m::keypair_65(pk, sk);
        case kMldsa87: return m::keypair_87(pk, sk);
        default: return false;
    }
}

bool sign_raw(std::uint32_t version, std::uint8_t* sig, std::size_t* siglen, ByteView msg,
              const std::uint8_t* sk) {
    namespace m = lux::crypto::mldsa;
    switch (version) {
        case kMldsa44: return m::sign_44(sig, siglen, msg.data(), msg.size(), sk);
        case kMldsa65: return m::sign_65(sig, siglen, msg.data(), msg.size(), sk);
        case kMldsa87: return m::sign_87(sig, siglen, msg.data(), msg.size(), sk);
        default: return false;
    }
}

bool verify_raw(std::uint32_t version, ByteView sig, ByteView msg, const std::uint8_t* pk) {
    namespace m = lux::crypto::mldsa;
    switch (version) {
        case kMldsa44: return m::verify_44(sig.data(), sig.size(), msg.data(), msg.size(), pk);
        case kMldsa65: return m::verify_65(sig.data(), sig.size(), msg.data(), msg.size(), pk);
        case kMldsa87: return m::verify_87(sig.data(), sig.size(), msg.data(), msg.size(), pk);
        default: return false;
    }
}

void put_be64(std::uint8_t* p, std::uint64_t v) {
    for (int i = 0; i < 8; ++i) p[i] = static_cast<std::uint8_t>(v >> (56 - 8 * i));
}

// Entropy for the nonce and the stamp's noise. The signature itself draws its
// randomness inside PQClean, from the OS.
void fill_random(std::uint8_t* p, std::size_t n) {
    static thread_local std::random_device rd;
    for (std::size_t i = 0; i < n; ++i) p[i] = static_cast<std::uint8_t>(rd() & 0xff);
}

// The quantum stamp: sha512(message ‖ key nonce ‖ stamp time) with 32 bytes of
// fresh noise after it, so two stamps over one message are never equal.
Bytes generate_stamp(ByteView message, const MldsaValidatorKey& key, Nanos stamped) {
    Bytes data(message.size() + key.nonce.size() + 8);
    if (!message.empty()) std::memcpy(data.data(), message.data(), message.size());
    if (!key.nonce.empty())
        std::memcpy(data.data() + message.size(), key.nonce.data(), key.nonce.size());
    put_be64(data.data() + message.size() + key.nonce.size(), static_cast<std::uint64_t>(stamped));

    const Hash512 h = sha512(view(data));
    Bytes stamp(h.size() + 32);
    std::memcpy(stamp.data(), h.data(), h.size());
    fill_random(stamp.data() + h.size(), 32);
    return stamp;
}

}  // namespace

Bytes signed_data(ByteView message, ByteView stamp, Nanos stamped) {
    Bytes data(message.size() + stamp.size() + 8);
    if (!message.empty()) std::memcpy(data.data(), message.data(), message.size());
    if (!stamp.empty()) std::memcpy(data.data() + message.size(), stamp.data(), stamp.size());
    put_be64(data.data() + message.size() + stamp.size(), static_cast<std::uint64_t>(stamped));
    return data;
}

Result<QuantumSigner> QuantumSigner::make(std::uint32_t algorithm_version, Duration stamp_window) {
    if (widths_of(algorithm_version).pk == 0)
        return fail(Err::UnsupportedAlgorithm,
                    std::to_string(algorithm_version) +
                        " (1=ML-DSA-44, 2=ML-DSA-65, 3=ML-DSA-87)");
    return QuantumSigner(algorithm_version, stamp_window);
}

std::size_t QuantumSigner::signature_size() const { return widths_of(version_).sig; }
std::size_t QuantumSigner::public_key_size() const { return widths_of(version_).pk; }

Result<MldsaValidatorKey> QuantumSigner::generate_key() const {
    const Widths w = widths_of(version_);
    MldsaValidatorKey key;
    key.version = version_;
    key.public_key.assign(w.pk, 0);
    key.private_key.assign(w.sk, 0);
    if (!keypair(version_, key.public_key.data(), key.private_key.data()))
        return fail(Err::InvalidCoronaKey, "ML-DSA key generation failed");
    key.nonce.assign(32, 0);
    fill_random(key.nonce.data(), key.nonce.size());
    return key;
}

Result<QuantumSignature> QuantumSigner::sign(ByteView message, const MldsaValidatorKey* key) const {
    if (key == nullptr) return fail(Err::InvalidCoronaKey);

    const Widths w = widths_of(version_);
    // A secret that is not this mode's width is not a key. Signing from it
    // would mean signing with whatever is in the buffer beside it.
    if (key->private_key.size() != w.sk)
        return fail(Err::InvalidCoronaKey,
                    "secret is " + std::to_string(key->private_key.size()) + " bytes, ML-DSA takes " +
                        std::to_string(w.sk));

    // One reading of the clock: the stamp is derived from it, the signature
    // covers it, and it is what the signature reports. Two readings would be
    // two different times for one signature.
    const Nanos stamped = wall_nanos();

    QuantumSignature sig;
    sig.algorithm = version_;
    sig.timestamp = stamped;
    sig.quantum_stamp = generate_stamp(message, *key, stamped);

    const Bytes covered = signed_data(message, view(sig.quantum_stamp), stamped);
    sig.signature.assign(w.sig, 0);
    std::size_t written = w.sig;
    if (!sign_raw(version_, sig.signature.data(), &written, view(covered), key->private_key.data()))
        return fail(Err::InvalidCoronaKey, "ML-DSA signing failed");
    sig.signature.resize(written);

    sig.public_key = key->public_key;
    sig.corona_key = key->public_key;
    return sig;
}

Status QuantumSigner::verify(ByteView message, const QuantumSignature* sig) const {
    if (sig == nullptr) return fail(Err::InvalidQuantumSignature);
    if (sig->algorithm != version_) return fail(Err::UnsupportedAlgorithm);

    // Fresh in BOTH directions. Only one side was checked once, and a
    // difference goes negative for a future date, so any timestamp ahead of now
    // compared as arbitrarily fresh and never expired at all.
    const Nanos age = wall_nanos() - sig->timestamp;
    const Nanos window = window_.count();
    if (age > window || age < -window) return fail(Err::QuantumStampExpired);

    if (sig->public_key.size() != widths_of(version_).pk)
        return fail(Err::InvalidQuantumSignature, "public key is not the mode's width");

    const Bytes covered = signed_data(message, view(sig->quantum_stamp), sig->timestamp);
    if (!verify_raw(version_, view(sig->signature), view(covered), sig->public_key.data()))
        return fail(Err::QuantumVerificationFailed);
    return ok();
}

Result<QuantumSigner::Batch> QuantumSigner::pack_batch(
    const std::vector<Bytes>& messages,
    const std::vector<const QuantumSignature*>& signatures) const {
    const Widths w = widths_of(version_);
    const std::size_t n = messages.size();

    std::vector<Bytes> full(n);
    std::size_t max_len = 0;
    for (std::size_t i = 0; i < n; ++i) {
        const QuantumSignature* sig = signatures[i];
        if (sig == nullptr)
            return fail(Err::OffWidth, "signature " + std::to_string(i) + ": nil");
        if (sig->signature.size() != w.sig)
            return fail(Err::OffWidth, "signature " + std::to_string(i) + ": " +
                                           std::to_string(sig->signature.size()) +
                                           " bytes, ML-DSA takes " + std::to_string(w.sig));
        if (sig->public_key.size() != w.pk)
            return fail(Err::OffWidth, "public key " + std::to_string(i) + ": " +
                                           std::to_string(sig->public_key.size()) +
                                           " bytes, ML-DSA takes " + std::to_string(w.pk));
        full[i] = signed_data(view(messages[i]), view(sig->quantum_stamp), sig->timestamp);
        max_len = std::max(max_len, full[i].size());
    }

    Batch b;
    b.max_message_len = max_len;
    b.messages.assign(n * max_len, 0);
    b.signatures.assign(n * w.sig, 0);
    b.public_keys.assign(n * w.pk, 0);
    for (std::size_t i = 0; i < n; ++i) {
        // Every copy is bounded at BOTH ends, and every input was measured
        // above. Slicing only the low end let row i run into row i+1.
        std::memcpy(b.messages.data() + i * max_len, full[i].data(), full[i].size());
        std::memcpy(b.signatures.data() + i * w.sig, signatures[i]->signature.data(), w.sig);
        std::memcpy(b.public_keys.data() + i * w.pk, signatures[i]->public_key.data(), w.pk);
    }
    return b;
}

bool QuantumSigner::accelerator_available() { return false; }

Status QuantumSigner::gpu_batch_verify(const std::vector<Bytes>& messages,
                                       const std::vector<const QuantumSignature*>& signatures) const {
    // The batch is packed first, so an off-width input is refused here exactly
    // as it would be with an accelerator present.
    auto packed = pack_batch(messages, signatures);
    if (!packed) return std::unexpected(packed.error());
    return fail(Err::NoAccelerator, "no accelerator is bound to this build");
}

Status QuantumSigner::cpu_parallel_verify(
    const std::vector<Bytes>& messages,
    const std::vector<const QuantumSignature*>& signatures) const {
    const std::size_t n = messages.size();
    std::vector<Status> verdicts(n);

    unsigned lanes = std::thread::hardware_concurrency();
    if (lanes == 0) lanes = 1;
    if (lanes > n) lanes = static_cast<unsigned>(n);

    std::vector<std::thread> pool;
    pool.reserve(lanes);
    for (unsigned lane = 0; lane < lanes; ++lane) {
        pool.emplace_back([&, lane] {
            for (std::size_t i = lane; i < n; i += lanes)
                verdicts[i] = verify(view(messages[i]), signatures[i]);
        });
    }
    for (auto& t : pool) t.join();

    // The batch verdict is the AND of its members, and the reason reported is
    // the FIRST failure by index — a deterministic answer, so two nodes running
    // the same batch report the same thing.
    for (std::size_t i = 0; i < n; ++i)
        if (!verdicts[i])
            return fail(verdicts[i].error().code,
                        "signature " + std::to_string(i) + ": " + verdicts[i].error().message());
    return ok();
}

Status QuantumSigner::parallel_verify_with_threshold(
    const std::vector<Bytes>& messages, const std::vector<const QuantumSignature*>& signatures,
    int gpu_threshold) const {
    if (messages.size() != signatures.size()) return fail(Err::BatchMismatch);
    if (messages.empty()) return ok();

    if (accelerator_available() && messages.size() >= static_cast<std::size_t>(gpu_threshold)) {
        if (gpu_batch_verify(messages, signatures)) return ok();
        // The accelerator refused (out of memory, unsupported, absent). That is
        // an error, never a verdict, so the CPU below still does the work.
    }
    return cpu_parallel_verify(messages, signatures);
}

Status QuantumSigner::parallel_verify(
    const std::vector<Bytes>& messages,
    const std::vector<const QuantumSignature*>& signatures) const {
    // The threshold Go takes from accel.DilithiumBatchThreshold. With no
    // accelerator bound it selects nothing, and the CPU path runs either way.
    return parallel_verify_with_threshold(messages, signatures, 8);
}

}  // namespace lux::quantumvm::quantum
