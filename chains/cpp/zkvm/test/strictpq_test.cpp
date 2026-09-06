// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// strictpq_test.cpp — the profile gate.
//
// The Z-Chain is DEFINITIVELY strict-PQ. On a strict-PQ chain the shielded-tx
// verifier MUST:
//   - refuse groth16 / plonk / bulletproofs — a CRQC that breaks bn254 must not
//     be able to forge a shield or unshield proof and mint shielded value;
//   - accept ONLY the post-quantum STARK/FRI system, failing closed until the
//     prover binds;
//   - ERROR when a real (non-dummy) bn254 verifying key is loaded.
//
// A permissive chain is unchanged, and must say so explicitly.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/zkvm/starkfri.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/verifier.hpp"
#include "lux/zkvm/vm.hpp"

#include <memory>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

std::unique_ptr<ProofVerifier> strict() {
    ZConfig cfg;
    cfg.strict_pq = true;
    cfg.proof_cache_size = 100;
    auto pv = ProofVerifier::open(cfg, test_bind());
    if (!pv) {
        check(false, "the strict-PQ verifier opens: " + pv.error());
        return nullptr;
    }
    return std::move(*pv);
}

void classical_shielded_proofs_are_refused() {
    std::printf("a strict-PQ chain refuses every classical system\n");
    auto pv = strict();
    if (!pv) return;

    for (const char* kind : {"groth16", "plonk", "bulletproofs"}) {
        // 544 bytes is enough to reach any classical body had the gate not
        // fired; the gate must fire FIRST.
        const Transaction tx = shielded_tx(kind, Bytes(544, 0));
        check_err(pv->verify(tx), kErrStrictPQClassicalForbidden,
                  std::string(kind) + " is refused by the profile gate, before anything else");
    }
}

void a_real_bn254_key_is_refused_at_construction() {
    std::printf("\na real bn254 key on a strict-PQ chain is an error, not a warning\n");

    Bytes real_vk(1024, 0);
    real_vk[0] = 0x01;  // non-zero ⇒ a real (non-dummy) key

    ZConfig cfg;
    cfg.strict_pq = true;
    cfg.proof_cache_size = 100;
    cfg.verifying_keys[circuit_key(TxType::Transfer)] = real_vk;
    check_err(ProofVerifier::open(cfg, test_bind()), kErrStrictPQRealVKForbidden,
              "loading one is refused where it is loaded, not somewhere downstream");

    // The converse: no supplied keys constructs cleanly, and verifies nothing.
    auto pv = strict();
    if (pv) check(!pv->verifying_keys_loaded(), "a strict-PQ chain starts holding no real key");

    // And a PERMISSIVE chain accepts the same key: the guard is strict-PQ only.
    cfg.strict_pq = false;
    auto permissive = ProofVerifier::open(cfg, test_bind());
    check_ok(permissive, "a permissive chain accepts a real bn254 key");
    if (permissive) check((*permissive)->verifying_keys_loaded(), "and reports it loaded");
}

void stark_is_the_only_accepted_system() {
    std::printf("\nSTARK/FRI is the only system that reaches a verifier\n");
    auto pv = strict();
    if (!pv) return;

    Bytes proof = b(starkfri::kMagicHeader);
    const Bytes payload = b("strict-pq-fri-payload");
    proof.insert(proof.end(), payload.begin(), payload.end());
    const Transaction tx = shielded_tx("stark", proof);

    // Unbound: fail closed.
    starkfri::register_verifier(nullptr);
    check_err(pv->verify(tx), starkfri::kErrVerifierNotRegistered,
              "an unbound STARK verifier refuses, and names why");

    // Bound and accepting: the shielded path verifies, and the public inputs are
    // the chain binding followed by the transaction's nullifiers and output
    // commitments.
    Bytes saw_pub;
    starkfri::register_verifier(
        [&saw_pub](std::uint8_t, ByteView, ByteView pub) -> wire::Result<bool> {
            saw_pub.assign(pub.begin(), pub.end());
            return true;
        });
    check_ok(pv->verify(tx), "a bound verifier's acceptance is the chain's");
    check(saw_pub.size() == 96,
          "the public inputs bind chain + nullifiers + commitments (96 bytes)");
    const Id bind = test_bind();
    check(saw_pub.size() >= 32 && std::equal(bind.begin(), bind.end(), saw_pub.begin()),
          "and the CHAIN goes first, so a proof made elsewhere does not verify here");

    // Bound and rejecting.
    starkfri::register_verifier(
        [](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> { return false; });
    check_err(pv->verify(tx), "rejected", "a bound verifier's refusal is the chain's too");
    starkfri::register_verifier(nullptr);
}

void the_permissive_path_is_untouched() {
    std::printf("\na permissive chain is unaffected by the gate\n");

    ZConfig cfg;
    cfg.strict_pq = false;
    cfg.proof_cache_size = 100;
    auto pv = ProofVerifier::open(cfg, test_bind());
    check_ok(pv, "a permissive chain opens");
    if (!pv) return;
    (*pv)->force_real_keys();  // reach the classical switch body

    const Transaction tx = shielded_tx("groth16", Bytes(256, 0));
    auto r = (*pv)->verify(tx);
    check(!r.has_value(), "a zero-byte groth16 proof is still rejected on its own merits");
    if (!r) {
        check(r.error().find("strict-PQ") == std::string::npos,
              "and the refusal does not mention strict-PQ");
    }
}

void the_chain_default_is_strict() {
    std::printf("\nthe Z-Chain's DEFAULT profile is the strict one\n");

    store::Memory base;
    VmConfig cfg;
    cfg.chain_id = id_from_hex(golden::kChainId);
    cfg.network_id = golden::kNetworkId;
    Vm vm(cfg, base);
    check_ok(vm.initialize(Genesis{}), "a chain configured with nothing comes up");
    check(vm.strict_pq(), "and it is strict-PQ");
    check_err(vm.proofs().refuse_classical_under_strict_pq("groth16"),
              kErrStrictPQClassicalForbidden, "so it refuses a classical system");
    check_ok(vm.proofs().refuse_classical_under_strict_pq("stark"), "and accepts STARK");

    // A permissive deployment is possible but MUST be explicit.
    store::Memory base2;
    VmConfig loose = cfg;
    loose.z.strict_pq = false;
    Vm vm2(loose, base2);
    check_ok(vm2.initialize(Genesis{}), "an explicitly permissive chain comes up");
    check(!vm2.strict_pq(), "and it is not strict-PQ");
    check_ok(vm2.proofs().refuse_classical_under_strict_pq("groth16"),
             "so the gate is a no-op there");
}

}  // namespace

int main() {
    classical_shielded_proofs_are_refused();
    a_real_bn254_key_is_refused_at_construction();
    stark_is_the_only_accepted_system();
    the_permissive_path_is_untouched();
    the_chain_default_is_strict();
    return report("strictpq");
}
