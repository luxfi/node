// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// groth16_test.cpp — the classical proof system's encoding, its field, and the
// point discipline that keeps its arithmetic honest.
//
// Encodings are checked against the GO reference's own bytes: the same points
// marshalled compressed and uncompressed must decode HERE to the same point, or
// a proof a Go node accepts is one this node refuses.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/zkvm/groth16.hpp"

#include "bn254_fp.hpp"

#include <string>
#include <vector>

using namespace lux::zkvm;
using namespace lux::zkvm::test;
using lux::crypto::bn254::U256;

namespace {

bool same_g1(const groth16::G1& a, const groth16::G1& c) {
    return a.infinity == c.infinity && (a.infinity || (a.x == c.x && a.y == c.y));
}

bool same_g2(const groth16::G2& a, const groth16::G2& c) {
    return a.infinity == c.infinity && (a.infinity || (a.x == c.x && a.y == c.y));
}

std::string be_hex(const U256& v) {
    std::uint8_t out[32];
    v.to_be32(out);
    return hex(ByteView(out, 32));
}

void the_two_encodings_name_the_same_point() {
    std::printf("compressed and uncompressed decode to the same point\n");

    const char* g1_un[] = {golden::kG1Uncompressed0, golden::kG1Uncompressed1,
                           golden::kG1Uncompressed2, golden::kG1Uncompressed3,
                           golden::kG1Uncompressed4};
    const char* g1_co[] = {golden::kG1Compressed0, golden::kG1Compressed1,
                           golden::kG1Compressed2, golden::kG1Compressed3,
                           golden::kG1Compressed4};
    const char* g2_un[] = {golden::kG2Uncompressed0, golden::kG2Uncompressed1,
                           golden::kG2Uncompressed2, golden::kG2Uncompressed3,
                           golden::kG2Uncompressed4};
    const char* g2_co[] = {golden::kG2Compressed0, golden::kG2Compressed1,
                           golden::kG2Compressed2, golden::kG2Compressed3,
                           golden::kG2Compressed4};

    for (int i = 0; i < golden::kPointPairs; ++i) {
        const std::string tag = " (point " + std::to_string(i) + ")";

        auto un = groth16::set_bytes_g1(view(from_hex(g1_un[i])));
        auto co = groth16::set_bytes_g1(view(from_hex(g1_co[i])));
        check_ok(un, "the uncompressed G1 decodes" + tag);
        check_ok(co, "the compressed G1 decodes" + tag);
        if (un && co) check(same_g1(*un, *co), "and they are the same G1 point" + tag);

        auto un2 = groth16::set_bytes_g2(view(from_hex(g2_un[i])));
        auto co2 = groth16::set_bytes_g2(view(from_hex(g2_co[i])));
        check_ok(un2, "the uncompressed G2 decodes" + tag);
        check_ok(co2, "the compressed G2 decodes" + tag);
        if (un2 && co2) check(same_g2(*un2, *co2), "and they are the same G2 point" + tag);
    }
}

void the_scalar_field_reads_bytes_as_the_reference_does() {
    std::printf("\nbig-endian bytes become the field element the reference reads\n");

    const char* ins[] = {golden::kFrIn0, golden::kFrIn1, golden::kFrIn2, golden::kFrIn3,
                         golden::kFrIn4, golden::kFrIn5, golden::kFrIn6};
    const char* outs[] = {golden::kFrOut0, golden::kFrOut1, golden::kFrOut2, golden::kFrOut3,
                          golden::kFrOut4, golden::kFrOut5, golden::kFrOut6};
    for (int i = 0; i < golden::kFrCases; ++i) {
        const U256 got = groth16::fr_from_bytes(view(from_hex(ins[i])));
        check_eq(be_hex(got), outs[i],
                 "case " + std::to_string(i) + ": bytes reduce to the reference's element");
    }
}

void infinity_is_not_a_usable_point() {
    std::printf("\nthe point at infinity is never handed on\n");

    // The encoding renders infinity as all-zero bytes and reports it in-subgroup,
    // and a pairing skips every term whose argument is infinity. A proof or key
    // element at infinity therefore deletes itself from the equation, so no
    // decoder may hand one on.
    const Bytes zeros(256, 0);
    check_err(groth16::deserialize_proof(view(zeros)), "infinity",
              "a proof of infinity points does not decode");

    const Bytes key_zeros(1024, 0);
    auto vk = groth16::deserialize_verifying_key(view(key_zeros));
    if (vk) {
        check_err(groth16::validate_verifying_key(*vk), "infinity",
                  "a key of infinity points does not validate");
    } else {
        check(true, "a key of infinity points does not even decode");
    }
}

void a_verifying_key_must_decode() {
    std::printf("\na verifying key that does not decode is not a key\n");
    const Bytes full = from_hex(golden::kGroth16Key2);

    check_err(groth16::deserialize_verifying_key(ByteView(full.data(), 100)), "too short",
              "a truncated key is refused");

    struct Spot {
        const char* want;
        std::size_t at;
    };
    for (const Spot& s : std::vector<Spot>{
             {"Alpha", 0}, {"Beta", 64}, {"Gamma", 64 + 128}, {"Delta", 64 + 256},
             {"K[0]", 64 + 384 + 4}}) {
        Bytes broken = full;
        for (int i = 0; i < 8; ++i) broken[s.at + std::size_t(i)] = 0xFF;
        check_err(groth16::deserialize_verifying_key(view(broken)), s.want,
                  std::string("a broken ") + s.want + " is named in the refusal");
    }

    // A K count the bytes cannot back.
    Bytes short_key = full;
    const std::size_t count_at = 64 + 384;
    short_key[count_at + 0] = 0x00;
    short_key[count_at + 1] = 0x10;
    short_key[count_at + 2] = 0x00;
    short_key[count_at + 3] = 0x00;
    check_err(groth16::deserialize_verifying_key(view(short_key)), "insufficient data for K",
              "a K count the bytes cannot back is refused");
}

void a_proof_must_decode() {
    std::printf("\na proof that does not decode is not a proof\n");
    const Bytes full = from_hex(golden::kGroth16Frame);
    check_err(groth16::deserialize_proof(ByteView(full.data(), 100)), "too short",
              "a truncated proof is refused");

    for (const auto& [name, at] :
         std::vector<std::pair<const char*, std::size_t>>{{"Ar", 0}, {"Bs", 64}, {"Krs", 192}}) {
        Bytes broken = full;
        for (int i = 0; i < 8; ++i) broken[at + std::size_t(i)] = 0xFF;
        check_err(groth16::deserialize_proof(view(broken)), name,
                  std::string("a broken ") + name + " is named in the refusal");
    }
}

void the_witness_must_fit_the_key() {
    std::printf("\nthe witness must be exactly what the key's circuit takes\n");

    // Four K points is a circuit taking three public inputs.
    auto vk = groth16::deserialize_verifying_key(view(from_hex(golden::kGroth16Key4)));
    auto proof = groth16::deserialize_proof(view(from_hex(golden::kGroth16Frame)));
    check_ok(vk, "the key decodes");
    check_ok(proof, "the proof decodes");
    if (!vk || !proof) return;

    auto witness = [](std::size_t n) {
        std::vector<U256> out;
        for (std::size_t i = 0; i < n; ++i) out.push_back(U256{i + 1, 0, 0, 0});
        return out;
    };

    for (std::size_t n : {0u, 1u, 2u, 4u, 9u}) {
        check_err(groth16::verify_pairing(*proof, *vk, witness(n)), "public inputs",
                  std::to_string(n) + " public inputs against a circuit taking 3 is refused");
    }

    // The count the key states reaches the arithmetic, which is the control:
    // without it the refusals above would prove only that this rejects
    // everything.
    check_err(groth16::verify_pairing(*proof, *vk, witness(3)), "pairing check failed",
              "the count the key states reaches the pairing and is judged there");
}

void a_keyless_circuit_has_no_points() {
    std::printf("\na key of no K points speaks about no circuit\n");
    // A key of n points speaks about exactly n-1 public inputs, so a key of zero
    // points describes nothing and every witness is the wrong length.
    groth16::VerifyingKey empty;
    auto proof = groth16::deserialize_proof(view(from_hex(golden::kGroth16Frame)));
    if (!proof) {
        check(false, "the proof decodes");
        return;
    }
    check_err(groth16::verify_pairing(*proof, empty, {}), "public inputs",
              "an empty witness against an empty key is refused, not accepted");
}

void a_satisfying_proof_verifies() {
    std::printf("\nthe arithmetic reaches BOTH verdicts\n");

    // The proof the Go reference built to satisfy the pairing equation must
    // verify here, or this port refuses what the reference accepts.
    auto vk = groth16::deserialize_verifying_key(view(from_hex(golden::kSatisfyingKey)));
    auto proof = groth16::deserialize_proof(view(from_hex(golden::kSatisfyingProof)));
    check_ok(vk, "the satisfying key decodes");
    check_ok(proof, "the satisfying proof decodes");
    if (!vk || !proof) return;
    check_ok(groth16::validate_verifying_key(*vk), "the key's every point is usable");

    std::vector<U256> witness = {
        groth16::fr_from_bytes(view(from_hex(golden::kTestBind))),
        groth16::fr_from_bytes(view(from_hex(golden::kSatisfyingNullifier))),
        groth16::fr_from_bytes(view(from_hex(golden::kSatisfyingCommitment)))};
    check_ok(groth16::verify_pairing(*proof, *vk, witness),
             "a proof that satisfies the equation is ACCEPTED");

    // And one field element different is not the same statement.
    witness[1] = groth16::fr_from_bytes(view(from_hex(golden::kSatisfyingCommitment)));
    check_err(groth16::verify_pairing(*proof, *vk, witness), "pairing check failed",
              "the same proof against a different statement is refused");
}

}  // namespace

int main() {
    the_two_encodings_name_the_same_point();
    the_scalar_field_reads_bytes_as_the_reference_does();
    infinity_is_not_a_usable_point();
    a_verifying_key_must_decode();
    a_proof_must_decode();
    the_witness_must_fit_the_key();
    a_keyless_circuit_has_no_points();
    a_satisfying_proof_verifies();
    return report("groth16");
}
