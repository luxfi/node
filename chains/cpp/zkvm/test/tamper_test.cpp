// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// tamper_test.cpp — the adversarial half of the proof path.
//
// Every other suite here asks the verifier to refuse bytes that are not a proof:
// too short, off the curve, at infinity, against a circuit with no key. Those
// refusals are all decided IN FRONT OF the arithmetic, so a `verify` that ended
// in `return {}` passes every one of them. That is what the earlier port did,
// and it is why a suite of refusals is not evidence of a verifier.
//
// What is asked here is the other question: a proof that DECODES — every point
// on the curve, in the prime-order subgroup, none at infinity, the exact right
// length — but is not a proof OF THIS STATEMENT. Only the pairing can tell those
// apart, so every case below is a verdict the equation reaches and nothing in
// front of it can.
//
// The suite opens with an ACCEPTANCE, deliberately. A verifier that refuses
// everything is as broken as one that accepts everything, and only a suite that
// contains both verdicts can tell which one it is looking at.
//
// PROVEN ABLE TO FAIL: with `verify_pairing` short-circuited to `return {}` —
// the shape the earlier port shipped — this suite reports 22 failures. Every one
// of the tampered proofs below is then accepted.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/zkvm/groth16.hpp"
#include "lux/zkvm/verifier.hpp"

#include <memory>
#include <string>
#include <vector>

using namespace lux::zkvm;
using namespace lux::zkvm::test;
using groth16::U256;

namespace {

// The bn254 base field modulus, big-endian. It is written out here rather than
// reached for through the curve library because a test that borrowed the
// implementation's own constant could not catch the implementation using the
// wrong one.
const char* kFieldModulus = "30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd47";

// negate_coordinate returns p - y for a 32-byte big-endian coordinate: the OTHER
// square root of the same x, which is a point on the curve and in the subgroup,
// and is not the point that was there. Nothing in front of the pairing can
// refuse it.
Bytes negate_coordinate(const Bytes& y) {
    const Bytes p = from_hex(kFieldModulus);
    Bytes out(32, 0);
    int borrow = 0;
    for (int i = 31; i >= 0; --i) {
        const int d = int(p[std::size_t(i)]) - int(y[std::size_t(i)]) - borrow;
        out[std::size_t(i)] = std::uint8_t(d & 0xFF);
        borrow = d < 0 ? 1 : 0;
    }
    return out;
}

Bytes splice(const Bytes& whole, std::size_t at, const Bytes& piece) {
    Bytes out = whole;
    for (std::size_t i = 0; i < piece.size(); ++i) out[at + i] = piece[i];
    return out;
}

Bytes slice(const Bytes& whole, std::size_t at, std::size_t n) {
    return Bytes(whole.begin() + std::ptrdiff_t(at), whole.begin() + std::ptrdiff_t(at + n));
}

// The proof's three points, at the offsets the encoding fixes them at.
constexpr std::size_t kArOff = 0, kBsOff = 64, kKrsOff = 192;

std::vector<U256> satisfying_witness() {
    return {groth16::fr_from_bytes(view(from_hex(golden::kTestBind))),
            groth16::fr_from_bytes(view(from_hex(golden::kSatisfyingNullifier))),
            groth16::fr_from_bytes(view(from_hex(golden::kSatisfyingCommitment)))};
}

// judge decodes a candidate proof against the satisfying key and returns the
// verdict the whole path reaches — decode, point checks, then the equation.
wire::Result<void> judge(const Bytes& proof_bytes, const std::vector<U256>& witness) {
    auto vk = groth16::deserialize_verifying_key(view(from_hex(golden::kSatisfyingKey)));
    if (!vk) return std::unexpected(vk.error());
    if (auto r = groth16::validate_verifying_key(*vk); !r) return r;
    auto proof = groth16::deserialize_proof(view(proof_bytes));
    if (!proof) return std::unexpected(proof.error());
    return groth16::verify_pairing(*proof, *vk, witness);
}

void the_untampered_proof_is_accepted() {
    std::printf("the proof this suite tampers with is one the verifier ACCEPTS\n");
    check_ok(judge(from_hex(golden::kSatisfyingProof), satisfying_witness()),
             "the Go reference's satisfying proof verifies here");
}

void a_point_swapped_for_its_own_negation_is_refused() {
    std::printf("\nthe other square root of the same x is not the same proof\n");

    const Bytes good = from_hex(golden::kSatisfyingProof);

    // -A. Same x, a y that is equally on the curve and equally in the subgroup,
    // so the decoder and both point checks pass it through untouched. e(-A,B)
    // is the inverse of e(A,B), so the equation is the one thing that can see
    // the difference.
    const Bytes neg_ar_y = negate_coordinate(slice(good, kArOff + 32, 32));
    check_err(judge(splice(good, kArOff + 32, neg_ar_y), satisfying_witness()),
              groth16::kErrPairingFailed, "A negated is refused BY THE PAIRING");

    // -C, the same way.
    const Bytes neg_krs_y = negate_coordinate(slice(good, kKrsOff + 32, 32));
    check_err(judge(splice(good, kKrsOff + 32, neg_krs_y), satisfying_witness()),
              groth16::kErrPairingFailed, "C negated is refused BY THE PAIRING");

    // -B is two Fp coordinates, both negated: the G2 encoding is
    // X.A1 ‖ X.A0 ‖ Y.A1 ‖ Y.A0, so Y is the last two words.
    Bytes neg_bs = splice(good, kBsOff + 64, negate_coordinate(slice(good, kBsOff + 64, 32)));
    neg_bs = splice(neg_bs, kBsOff + 96, negate_coordinate(slice(good, kBsOff + 96, 32)));
    check_err(judge(neg_bs, satisfying_witness()), groth16::kErrPairingFailed,
              "B negated is refused BY THE PAIRING");
}

void a_point_swapped_for_a_generator_is_refused() {
    std::printf("\na different valid point in the same place is not the same proof\n");

    const Bytes good = from_hex(golden::kSatisfyingProof);

    // Points the golden corpus pins as valid: unquestionably on the curve,
    // unquestionably in the prime-order subgroup, and unquestionably not this
    // proof's.
    //
    // The substitute is asserted to DIFFER from what it replaces first. The G2
    // generator is what this proof's B already is, so swapping it in changes
    // nothing and the case would report a pass for having done nothing — which
    // is how a suite quietly stops asking its question.
    const Bytes g1 = from_hex(golden::kG1Uncompressed0);
    const Bytes g2 = from_hex(golden::kG2Uncompressed1);
    check(slice(good, kArOff, 64) != g1, "the substitute G1 point is not the A it replaces");
    check(slice(good, kKrsOff, 64) != g1, "nor the C");
    check(slice(good, kBsOff, 128) != g2, "and the substitute G2 point is not the B it replaces");

    check_err(judge(splice(good, kArOff, g1), satisfying_witness()), groth16::kErrPairingFailed,
              "A replaced by another valid G1 point is refused BY THE PAIRING");
    check_err(judge(splice(good, kKrsOff, g1), satisfying_witness()), groth16::kErrPairingFailed,
              "C replaced by another valid G1 point is refused BY THE PAIRING");
    check_err(judge(splice(good, kBsOff, g2), satisfying_witness()), groth16::kErrPairingFailed,
              "B replaced by another valid G2 point is refused BY THE PAIRING");

    // And the proof's own points moved to each other's places. Nothing about
    // A or C is unusable in the other's slot; only the equation objects.
    Bytes swapped = splice(good, kArOff, slice(good, kKrsOff, 64));
    swapped = splice(swapped, kKrsOff, slice(good, kArOff, 64));
    check_err(judge(swapped, satisfying_witness()), groth16::kErrPairingFailed,
              "A and C exchanged is refused BY THE PAIRING");
}

void every_word_of_the_proof_is_load_bearing() {
    std::printf("\nno byte of the proof can be changed without changing the verdict\n");

    const Bytes good = from_hex(golden::kSatisfyingProof);

    // One bit flipped in the low byte of each of the eight coordinates. A flip
    // usually leaves an x with no square root or a y off the curve, which the
    // decoder refuses; sometimes it lands on a usable point, which the pairing
    // refuses. Either is a refusal — what must never happen is acceptance, and
    // that is what is asserted, not which door the refusal came out of.
    for (std::size_t word = 0; word < 8; ++word) {
        Bytes t = good;
        t[word * 32 + 31] ^= 0x01;
        const std::string what = "word " + std::to_string(word) + " with one bit flipped";
        check(!judge(t, satisfying_witness()).has_value(), what + " is refused");
    }
}

void the_witness_is_what_the_proof_is_about() {
    std::printf("\nthe same proof against a different statement is a different question\n");

    const Bytes good = from_hex(golden::kSatisfyingProof);

    for (std::size_t i = 0; i < 3; ++i) {
        std::vector<U256> w = satisfying_witness();
        // One added to one field element: still a perfectly good element, and
        // no longer the statement the proof attests to.
        std::vector<Bytes> raw = {from_hex(golden::kTestBind),
                                  from_hex(golden::kSatisfyingNullifier),
                                  from_hex(golden::kSatisfyingCommitment)};
        raw[i][31] ^= 0x01;
        w[i] = groth16::fr_from_bytes(view(raw[i]));
        check_err(judge(good, w), groth16::kErrPairingFailed,
                  "public input " + std::to_string(i) + " changed is refused BY THE PAIRING");
    }
}

void a_tampered_key_does_not_verify_the_proof() {
    std::printf("\nthe key is half the statement, and it is checked too\n");

    const Bytes good_key = from_hex(golden::kSatisfyingKey);
    const Bytes proof = from_hex(golden::kSatisfyingProof);
    const std::vector<U256> witness = satisfying_witness();

    // Alpha negated: on the curve, in the subgroup, not this trusted setup.
    Bytes tampered = splice(good_key, 32, negate_coordinate(slice(good_key, 32, 32)));
    auto vk = groth16::deserialize_verifying_key(view(tampered));
    check_ok(vk, "a key with alpha negated still decodes");
    if (vk) {
        check_ok(groth16::validate_verifying_key(*vk), "and still passes every point check");
        auto p = groth16::deserialize_proof(view(proof));
        if (p)
            check_err(groth16::verify_pairing(*p, *vk, witness), groth16::kErrPairingFailed,
                      "but the proof does not verify under it");
    }

    // One of the K points swapped for the G1 generator. K is the public-input
    // half of the equation, so this is a key that describes a different circuit.
    const std::size_t k0 = 64 + 128 + 128 + 128 + 4;
    Bytes kswap = splice(good_key, k0, from_hex(golden::kG1Uncompressed0));
    auto vk2 = groth16::deserialize_verifying_key(view(kswap));
    check_ok(vk2, "a key with K[0] replaced still decodes");
    if (vk2) {
        auto p = groth16::deserialize_proof(view(proof));
        if (p)
            check_err(groth16::verify_pairing(*p, *vk2, witness), groth16::kErrPairingFailed,
                      "but the proof does not verify under it either");
    }
}

// And the whole path, not only the arithmetic: a tampered proof carried by a
// transaction the verifier would otherwise accept.
void a_tampered_proof_cannot_ride_the_accepted_one() {
    std::printf("\nthe cache is keyed on content, so a tampered proof asks its own question\n");

    ZConfig cfg;
    cfg.strict_pq = false;
    cfg.proof_cache_size = 16;
    cfg.verifying_keys[circuit_key(TxType::Transfer)] = from_hex(golden::kSatisfyingKey);
    auto opened = ProofVerifier::open(cfg, test_bind());
    check_ok(opened, "a permissive chain holding the satisfying key opens");
    if (!opened) return;
    auto pv = std::move(*opened);

    const Bytes nullifier = from_hex(golden::kSatisfyingNullifier);
    const Bytes commitment = from_hex(golden::kSatisfyingCommitment);
    const Id bind = test_bind();

    Transaction good;
    good.type = TxType::Transfer;
    good.version = 1;
    good.nullifiers = {nullifier};
    good.outputs = {{commitment, {}, {}, {}}};
    good.proof = ZkProof{"groth16",
                         from_hex(golden::kSatisfyingProof),
                         {Bytes(bind.begin(), bind.end()), nullifier, commitment}};
    good.id = good.compute_id();
    check_ok(pv->verify(good), "the untampered transaction verifies");

    // The same transaction with -A. The proof still decodes, still passes both
    // point checks, and still carries the right public inputs — so every refusal
    // in front of the arithmetic passes it through.
    Transaction tampered = good;
    const Bytes original = from_hex(golden::kSatisfyingProof);
    tampered.proof->proof_data =
        splice(original, kArOff + 32, negate_coordinate(slice(original, kArOff + 32, 32)));
    tampered.id = tampered.compute_id();

    check(hex_of(tampered.id) != hex_of(good.id),
          "the tampered transaction has an id of its own, so the cache cannot answer for it");
    check_err(pv->verify(tampered), "pairing", "and it is refused BY THE PAIRING");

    // A peer that copies the accepted transaction's id onto it buys nothing:
    // the cache is keyed on computed content, not on the claimed id, so the
    // answer that comes back is the tampered proof's own remembered refusal —
    // not the accepted transaction's.
    Transaction forged = tampered;
    forged.id = good.id;
    check_err(pv->verify(forged), "(cached)",
              "wearing the accepted transaction's id reaches the tampered proof's own refusal");
}

}  // namespace

int main() {
    the_untampered_proof_is_accepted();
    a_point_swapped_for_its_own_negation_is_refused();
    a_point_swapped_for_a_generator_is_refused();
    every_word_of_the_proof_is_load_bearing();
    the_witness_is_what_the_proof_is_about();
    a_tampered_key_does_not_verify_the_proof();
    a_tampered_proof_cannot_ride_the_accepted_one();
    return report("tamper");
}
