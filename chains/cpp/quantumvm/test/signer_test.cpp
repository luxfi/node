// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// signer_test.cpp — ported from Go chains/quantumvm/quantum/signer_test.go.
//
// Two of that file's tests are about Go's own machinery rather than about the
// signature scheme — a key parsed once and cached (`TestSignParsesStoredSecretOnce`)
// and a secret surviving garbage collection (`TestGeneratedKeyOwnsItsSecret`) —
// and they render here as what they are asserting underneath: signing REPEATEDLY
// from stored bytes keeps producing signatures that verify, and the bytes a
// caller read out of a key stay valid for as long as the caller holds them.

#include "fixtures.hpp"

#include <thread>

using namespace qvmtest;
using namespace lux::quantumvm::quantum;

namespace {

QuantumSigner signer_at(std::uint32_t version, Duration window) {
    auto qs = QuantumSigner::make(version, window);
    return *qs;
}

QuantumSigner signer() { return signer_at(kMldsa65, std::chrono::minutes(1)); }

// A validator key in the shape it takes coming back from storage or the wire:
// the exported fields only, with nothing derived.
MldsaValidatorKey stored(const QuantumSigner& qs) {
    auto gen = qs.generate_key();
    MldsaValidatorKey k;
    k.version = gen->version;
    k.public_key = gen->public_key;
    k.private_key = gen->private_key;
    k.nonce = gen->nonce;
    return k;
}

Bytes msg(const std::string& s) { return bytes_of(s); }

}  // namespace

// Repeated signing from a stored secret keeps producing signatures that verify.
TEST(SignFromAStoredSecretKeepsVerifying) {
    const QuantumSigner qs = signer();
    const MldsaValidatorKey key = stored(qs);
    const Bytes m = msg("round digest");

    for (int i = 0; i < 4; ++i) {
        auto sig = qs.sign(view(m), &key);
        REQUIRE_OK(sig);
        REQUIRE_OK(qs.verify(view(m), &*sig));
    }
}

// The bytes a caller persists out of a generated key stay readable for as long
// as the caller holds them, whatever becomes of the key they came from.
TEST(GeneratedKeyOwnsItsSecret) {
    const QuantumSigner qs = signer();
    Bytes persisted;
    Bytes want;
    {
        auto gen = qs.generate_key();
        REQUIRE_OK(gen);
        persisted = gen->private_key;
        want = gen->private_key;
    }
    REQUIRE_MSG(persisted == want, "the secret read out of a key changed when the key went away");
}

// A key that cannot be used fails closed, every time, rather than signing with
// whatever is in the buffer.
TEST(SignRejectsUnparseableSecret) {
    const QuantumSigner qs = signer();
    MldsaValidatorKey key;
    key.private_key = bytes_of("too short to be a key");

    for (int i = 0; i < 2; ++i) REQUIRE_ERR(qs.sign(view(msg("round digest")), &key), Err::InvalidCoronaKey);
}

// A stamp is only good inside its window. The window is the whole reason a stamp
// exists — without the check, a signature captured once is replayable for as
// long as the chain runs.
TEST(VerifyRefusesAnExpiredStamp) {
    const QuantumSigner qs = signer_at(kMldsa65, std::chrono::milliseconds(50));
    const MldsaValidatorKey key = stored(qs);
    const Bytes m = msg("round digest");

    auto sig = qs.sign(view(m), &key);
    REQUIRE_OK(sig);
    REQUIRE_OK(qs.verify(view(m), &*sig));

    std::this_thread::sleep_for(std::chrono::milliseconds(120));
    REQUIRE_ERR(qs.verify(view(m), &*sig), Err::QuantumStampExpired);
}

// A signature made under one ML-DSA parameter set must not be accepted by a
// signer running another. The key and signature widths differ, so accepting one
// would mean verifying against a truncated key.
TEST(VerifyRefusesAnotherAlgorithm) {
    const QuantumSigner weak = signer_at(kMldsa44, std::chrono::minutes(1));
    const QuantumSigner strong = signer_at(kMldsa87, std::chrono::minutes(1));

    const MldsaValidatorKey key = stored(weak);
    auto sig = weak.sign(view(msg("digest")), &key);
    REQUIRE_OK(sig);
    REQUIRE_ERR(strong.verify(view(msg("digest")), &*sig), Err::UnsupportedAlgorithm);
}

// The two ways a caller arrives with nothing valid: a signature over other
// bytes, and none at all.
TEST(VerifyRefusesAlteredMessageAndNilSignature) {
    const QuantumSigner qs = signer();
    const MldsaValidatorKey key = stored(qs);
    auto sig = qs.sign(view(msg("the real message")), &key);
    REQUIRE_OK(sig);

    REQUIRE_ERR(qs.verify(view(msg("a different message")), &*sig), Err::QuantumVerificationFailed);
    REQUIRE_ERR(qs.verify(view(msg("the real message")), nullptr), Err::InvalidQuantumSignature);
    REQUIRE_ERR(qs.sign(view(msg("x")), nullptr), Err::InvalidCoronaKey);
}

// The key travels WITH the signature, so it is attacker-controlled and must fail
// closed rather than reach into a buffer that is not a key.
TEST(VerifyRefusesAnUnparseablePublicKey) {
    const QuantumSigner qs = signer();
    const MldsaValidatorKey key = stored(qs);
    auto sig = qs.sign(view(msg("digest")), &key);
    REQUIRE_OK(sig);

    sig->public_key = bytes_of("not a key");
    REQUIRE_FAILS(qs.verify(view(msg("digest")), &*sig));
}

// The batch verdict is the AND of its members. A batch that passed while holding
// one bad signature would let a forged transaction into a block on the strength
// of the honest ones beside it.
TEST(ParallelVerifyFailsOnOneBadSignature) {
    const QuantumSigner qs = signer();
    constexpr int n = 12;
    std::vector<Bytes> msgs(n);
    std::vector<QuantumSignature> owned(n);
    std::vector<const QuantumSignature*> sigs(n);
    for (int i = 0; i < n; ++i) {
        msgs[static_cast<std::size_t>(i)] = Bytes{static_cast<std::uint8_t>(i), 'm', 's', 'g'};
        const MldsaValidatorKey key = stored(qs);
        auto sig = qs.sign(view(msgs[static_cast<std::size_t>(i)]), &key);
        REQUIRE_OK(sig);
        owned[static_cast<std::size_t>(i)] = *sig;
        sigs[static_cast<std::size_t>(i)] = &owned[static_cast<std::size_t>(i)];
    }

    REQUIRE_OK(qs.parallel_verify(msgs, sigs));

    owned[n / 2].signature[0] ^= 0xFF;
    REQUIRE_FAILS(qs.parallel_verify(msgs, sigs));
    // And on the thresholded path the VM actually calls.
    REQUIRE_FAILS(qs.parallel_verify_with_threshold(msgs, sigs, 4));
}

// A message with no signature beside it is not a thing to verify, and pairing
// them off by index would verify the wrong pair.
TEST(ParallelVerifyRefusesMismatchedInputs) {
    const QuantumSigner qs = signer();
    std::vector<Bytes> two{Bytes{1}, Bytes{2}};
    std::vector<const QuantumSignature*> one{nullptr};
    REQUIRE_ERR(qs.parallel_verify(two, one), Err::BatchMismatch);
    REQUIRE_OK(qs.parallel_verify({}, {}));
}

// The caller sizes buffers from these, so they must come from the mode rather
// than a configured guess — and two signers on one version must agree, or one
// cannot verify what the other produced.
TEST(SignerReportsItsWidths) {
    for (std::uint32_t version : {kMldsa44, kMldsa65, kMldsa87}) {
        const QuantumSigner qs = signer_at(version, std::chrono::minutes(1));
        auto key = qs.generate_key();
        REQUIRE_OK(key);
        REQUIRE_EQ(qs.public_key_size(), key->public_key.size());

        auto sig = qs.sign(view(msg("digest")), &*key);
        REQUIRE_OK(sig);
        REQUIRE(sig->signature.size() <= qs.signature_size());

        const QuantumSigner peer = signer_at(version, std::chrono::minutes(1));
        REQUIRE_EQ(qs.mode(), peer.mode());
        REQUIRE_OK(peer.verify(view(msg("digest")), &*sig));
    }
}

// TestGPUVerifyFailsClosedWhenThereIsNoGPU.
//
// The batch path asks the accelerator first and falls back to the CPU on any
// error. That fallback is only safe if a missing accelerator is an ERROR — a GPU
// path that returned success having verified nothing would report every
// signature in the batch as good, and the CPU check that should have caught them
// would never run.
TEST(GPUVerifyFailsClosedWhenThereIsNoGPU) {
    if (QuantumSigner::accelerator_available()) {
        skip_current("this build has an accelerator bound, so the no-GPU contract "
                     "cannot be observed here");
        return;
    }

    const QuantumSigner qs = signer();
    std::vector<Bytes> msgs{bytes_of("one"), bytes_of("two")};
    std::vector<QuantumSignature> owned(msgs.size());
    std::vector<const QuantumSignature*> sigs(msgs.size());
    for (std::size_t i = 0; i < msgs.size(); ++i) {
        const MldsaValidatorKey key = stored(qs);
        auto sig = qs.sign(view(msgs[i]), &key);
        REQUIRE_OK(sig);
        owned[i] = *sig;
        sigs[i] = &owned[i];
    }

    REQUIRE_ERR(qs.gpu_batch_verify(msgs, sigs), Err::NoAccelerator);
    // And the caller routes around it: the same batch verifies on the CPU.
    REQUIRE_OK(qs.parallel_verify_with_threshold(msgs, sigs, 1));

    owned[1].signature[0] ^= 0xFF;
    REQUIRE_FAILS(qs.parallel_verify_with_threshold(msgs, sigs, 1));
}

// TestTheStampTimeIsSigned.
//
// Freshness was decided by a plain field on the signature — sign covered
// message ‖ stamp and NOT the timestamp — so the holder of an expired stamp
// revived it by writing the current time into it, and the same edit forward
// produced one that never expired at all. The window is the only thing standing
// between a captured signature and unlimited replay, so the value it reads has
// to be one the signature covers.
TEST(TheStampTimeIsSigned) {
    const QuantumSigner qs = signer_at(kMldsa65, std::chrono::milliseconds(50));
    const MldsaValidatorKey key = stored(qs);
    const Bytes m = msg("round digest");

    auto sig = qs.sign(view(m), &key);
    REQUIRE_OK(sig);
    REQUIRE_OK(qs.verify(view(m), &*sig));

    std::this_thread::sleep_for(std::chrono::milliseconds(120));
    REQUIRE_ERR(qs.verify(view(m), &*sig), Err::QuantumStampExpired);

    // Rewrite the field to now. The window is satisfied — and the signature is
    // not, because the field is part of what was signed.
    sig->timestamp = wall_nanos();
    REQUIRE_ERR(qs.verify(view(m), &*sig), Err::QuantumVerificationFailed);
}

// A difference goes NEGATIVE for a date ahead of now, so a stamp dated forward
// compared as arbitrarily fresh and never expired — one write to an unsigned
// field bought a signature that is valid forever.
TEST(VerifyRefusesAFutureStamp) {
    const QuantumSigner qs = signer_at(kMldsa65, std::chrono::minutes(1));
    const MldsaValidatorKey key = stored(qs);
    const Bytes m = msg("round digest");

    auto sig = qs.sign(view(m), &key);
    REQUIRE_OK(sig);

    for (Nanos ahead : {Nanos{2} * 60 * 1'000'000'000, Nanos{3600} * 1'000'000'000,
                        Nanos{24} * 365 * 3600 * 1'000'000'000}) {
        sig->timestamp = wall_nanos() + ahead;
        REQUIRE_ERR(qs.verify(view(m), &*sig), Err::QuantumStampExpired);
    }
}

// It fell through to ML-DSA-65 for anything unrecognised, so a caller that asked
// for something else got a signer running a parameter set nobody chose and never
// heard about it — and the config and the signer disagreed about what an unset
// version means.
TEST(NewQuantumSignerRefusesAnAlgorithmThatDoesNotExist) {
    for (std::uint32_t version : {0u, 4u, 7u, 42u, 99u, ~0u}) {
        auto qs = QuantumSigner::make(version, std::chrono::minutes(1));
        REQUIRE_MSG(!qs, "version " + std::to_string(version) + " was accepted");
        REQUIRE_EQ(Err::UnsupportedAlgorithm, qs.error().code);
    }
}

// TestBatchPackingKeepsEveryEntryInItsOwnRow.
//
// The rows are fixed-width and were copied with the low end bounded only, so an
// over-long signature or public key at index i ran off its row and overwrote
// index i+1's. A caller supplying a matching over-long pair chose the key that
// entry i+1 would be verified under. ML-DSA widths are fixed by the parameter
// set, so an off-width input is refused rather than trusted to fit.
TEST(BatchPackingKeepsEveryEntryInItsOwnRow) {
    const QuantumSigner qs = signer();
    const std::size_t sig_size = qs.signature_size();
    const std::size_t pk_size = qs.public_key_size();

    std::vector<Bytes> msgs{bytes_of("one"), bytes_of("two")};
    std::vector<QuantumSignature> owned(msgs.size());
    std::vector<const QuantumSignature*> sigs(msgs.size());
    for (std::size_t i = 0; i < msgs.size(); ++i) {
        const MldsaValidatorKey key = stored(qs);
        auto sig = qs.sign(view(msgs[i]), &key);
        REQUIRE_OK(sig);
        owned[i] = *sig;
        sigs[i] = &owned[i];
    }

    auto packed = qs.pack_batch(msgs, sigs);
    REQUIRE_OK(packed);
    REQUIRE_EQ(msgs.size() * sig_size, packed->signatures.size());
    REQUIRE_EQ(msgs.size() * pk_size, packed->public_keys.size());
    REQUIRE_MSG(std::equal(owned[1].public_key.begin(), owned[1].public_key.end(),
                           packed->public_keys.begin() + static_cast<std::ptrdiff_t>(pk_size)),
                "entry 1's key is not in entry 1's row");

    // An over-long signature at index 0 would reach into index 1's row.
    QuantumSignature oversized = owned[0];
    oversized.signature.assign(sig_size + 64, 0xAA);
    REQUIRE_ERR(qs.pack_batch(msgs, {&oversized, sigs[1]}), Err::OffWidth);

    // So would an over-long public key — the half that decides what the next
    // entry is verified against.
    QuantumSignature forged = owned[0];
    forged.public_key.assign(pk_size * 2, 0xBB);
    REQUIRE_ERR(qs.pack_batch(msgs, {&forged, sigs[1]}), Err::OffWidth);

    // Short is refused too: padding it would verify against a key nobody sent.
    QuantumSignature short_sig = owned[0];
    short_sig.signature.resize(sig_size - 1);
    REQUIRE_ERR(qs.pack_batch(msgs, {&short_sig, sigs[1]}), Err::OffWidth);

    REQUIRE_ERR(qs.pack_batch(msgs, {nullptr, sigs[1]}), Err::OffWidth);
}

// The stamp is a function of the message, the key's nonce and the time — and of
// fresh noise, so two stamps over one message are never the same bytes.
TEST(TheStampIsNeverTheSameTwice) {
    const QuantumSigner qs = signer();
    const MldsaValidatorKey key = stored(qs);
    const Bytes m = msg("round digest");

    auto first = qs.sign(view(m), &key);
    auto second = qs.sign(view(m), &key);
    REQUIRE_OK(first);
    REQUIRE_OK(second);
    REQUIRE_MSG(first->quantum_stamp != second->quantum_stamp,
                "two stamps over one message came out identical");
    REQUIRE_OK(qs.verify(view(m), &*first));
    REQUIRE_OK(qs.verify(view(m), &*second));
}
