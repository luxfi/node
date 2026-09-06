// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// verifier_test.cpp — the classical proof path on a permissive chain: what it
// refuses, the one thing it accepts, and the cache that remembers both.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/zkvm/verifier.hpp"

#include <memory>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

std::unique_ptr<ProofVerifier> keyed(const std::map<std::string, Bytes>& keys) {
    ZConfig cfg;
    cfg.strict_pq = false;
    cfg.proof_cache_size = 16;
    cfg.verifying_keys = keys;
    auto pv = ProofVerifier::open(cfg, test_bind());
    if (!pv) {
        check(false, "the verifier opens: " + pv.error());
        return nullptr;
    }
    return std::move(*pv);
}

Transaction proof_tx(const std::string& kind, Bytes data) {
    const Id bind = test_bind();
    Transaction tx;
    tx.type = TxType::Transfer;
    tx.proof = ZkProof{kind, std::move(data), {Bytes(bind.begin(), bind.end())}};
    tx.id = tx.compute_id();
    return tx;
}

void a_satisfying_proof_is_accepted_and_remembered() {
    std::printf("a satisfying proof is accepted, and the second look is the cache's\n");

    auto pv = keyed({{circuit_key(TxType::Transfer), from_hex(golden::kSatisfyingKey)}});
    if (!pv) return;

    const Bytes nullifier = from_hex(golden::kSatisfyingNullifier);
    const Bytes commitment = from_hex(golden::kSatisfyingCommitment);
    const Id bind = test_bind();

    Transaction tx;
    tx.type = TxType::Transfer;
    tx.version = 1;
    tx.nullifiers = {nullifier};
    tx.outputs = {{commitment, {}, {}, {}}};
    tx.proof = ZkProof{"groth16",
                       from_hex(golden::kSatisfyingProof),
                       {Bytes(bind.begin(), bind.end()), nullifier, commitment}};
    tx.id = tx.compute_id();

    check_ok(pv->verify(tx), "the proof verifies on the arithmetic");

    std::uint64_t count = 0, hits = 0, misses = 0;
    pv->stats(&count, &hits, &misses);
    check(hits == 0 && misses == 1, "the first look was a miss");

    check_ok(pv->verify(tx), "the same transaction, from the cache");
    pv->stats(&count, &hits, &misses);
    check(hits == 1, "and that look was a hit");

    // A transaction spending DIFFERENT notes with the same proof bytes and the
    // same public inputs is a different transaction, so it does not reach this
    // answer. Before the key was the content, its id was the sender's to copy
    // and this returned "ok".
    Transaction forged = tx;
    forged.nullifiers = {Bytes(32, 0)};
    forged.id = tx.id;  // the sender's claim, and it buys nothing
    check_err(pv->verify(forged), "mismatch",
              "a forged transaction wearing the accepted one's id is refused");

    // A refusal is remembered too, and reported as a refusal.
    const std::uint64_t before = hits;
    check_err(pv->verify(forged), "(cached)", "and the refusal is remembered as a refusal");
    pv->stats(&count, &hits, &misses);
    check(hits > before, "which is another cache hit");
}

void proof_refusals() {
    std::printf("\nthe refusals in front of the arithmetic\n");

    auto pv = keyed({{circuit_key(TxType::Transfer), from_hex(golden::kGroth16Key4)}});
    if (!pv) return;

    Transaction bare;
    bare.type = TxType::Transfer;
    check_err(pv->verify(bare), "missing proof", "a transaction with no proof is refused");

    check_err(pv->verify(proof_tx("bulletproofs", {})), "not yet implemented",
              "bulletproofs says why it is refused");

    // PLONK's verification equation was never written. Decoding a proof
    // carefully in order to refuse it is not a verifier, so the refusal is all
    // this path does and all it says.
    check_err(pv->verify(proof_tx("plonk", Bytes(736, 0))), kErrPlonkIncomplete,
              "a well-shaped PLONK proof is refused by name");
    check_err(pv->verify(proof_tx("plonk", {})), kErrPlonkIncomplete,
              "and so is an empty one");
    check_err(pv->verify(proof_tx("nonsense", {})), "unsupported proof type",
              "an unknown system is refused");
    check_err(pv->verify(proof_tx("groth16", Bytes(10, 0))), "invalid proof data length",
              "a groth16 proof too short to be one is refused");

    // The right LENGTH and the wrong points. Zero bytes are not a bn254 point,
    // so a proof made of them has to be refused by the decode and the subgroup
    // check rather than carried into the pairing, where a point off the
    // prime-order subgroup is what a forgery is built out of. This is the
    // reference's own H-01 regression, and it is asked of a chain that HOLDS a
    // key — asking it of one that holds none only proves the missing-key
    // refusal fires first.
    check_err(pv->verify(proof_tx("groth16", Bytes(256, 0))), "point at infinity",
              "a groth16 proof of zero bytes is refused at the points");

    // A chain holding no real key verifies nothing, and says so.
    ZConfig none;
    none.strict_pq = false;
    none.proof_cache_size = 4;
    auto dummy = ProofVerifier::open(none, test_bind());
    check_ok(dummy, "a chain with no keys still opens");
    if (dummy) {
        check(!(*dummy)->verifying_keys_loaded(), "and reports that it holds none");
        check_err((*dummy)->verify(proof_tx("groth16", Bytes(256, 0))),
                  "proof verification disabled", "so it verifies nothing");
    }

    // A cache too small to hold anything is not a verifier.
    ZConfig no_cache;
    no_cache.strict_pq = false;
    no_cache.proof_cache_size = 0;
    check_err(ProofVerifier::open(no_cache, test_bind()), "cache size",
              "a zero-sized proof cache is refused at construction");
}

void public_inputs_bind_the_transaction() {
    std::printf("\nthe public inputs are what tie a proof to the transaction\n");

    auto pv = keyed({{circuit_key(TxType::Transfer), from_hex(golden::kGroth16Key4)}});
    if (!pv) return;

    const Bytes nullifier = from_hex(golden::kSatisfyingNullifier);
    const Bytes commitment = from_hex(golden::kSatisfyingCommitment);
    const Id bind = test_bind();
    const Bytes bind_bytes(bind.begin(), bind.end());

    auto with = [&](std::vector<Bytes> inputs) {
        Transaction tx;
        tx.type = TxType::Transfer;
        tx.nullifiers = {nullifier};
        tx.outputs = {{commitment, {}, {}, {}}};
        tx.proof = ZkProof{"groth16", from_hex(golden::kGroth16Frame), std::move(inputs)};
        tx.id = tx.compute_id();
        return tx;
    };

    check_err(pv->verify(with({})), "no public inputs", "none at all is refused");
    check_err(pv->verify(with({Bytes(32, 0), nullifier, commitment})), "chain binding",
              "a proof made for another chain is refused");
    check_err(pv->verify(with({bind_bytes})), "missing public input for nullifier",
              "no room for the nullifier is refused");
    check_err(pv->verify(with({bind_bytes, commitment, commitment})), "mismatch for nullifier",
              "a different nullifier is refused");
    check_err(pv->verify(with({bind_bytes, nullifier})),
              "missing public input for output commitment",
              "no room for the commitment is refused");
    check_err(pv->verify(with({bind_bytes, nullifier, nullifier})),
              "mismatch for output commitment", "a different commitment is refused");

    // Same length, different bytes: the comparison is by VALUE, not by length.
    Bytes near = nullifier;
    near[0] = std::uint8_t(near[0] ^ 0xFF);
    check_err(pv->verify(with({bind_bytes, near, commitment})), "mismatch",
              "same length and different bytes is still a mismatch");
}

void a_circuit_without_a_key_is_refused_by_name() {
    std::printf("\neach circuit is judged on its OWN key\n");

    auto pv = keyed({{circuit_key(TxType::Transfer), from_hex(golden::kGroth16Key4)}});
    if (!pv) return;

    check(!pv->has_key(circuit_key(TxType::Shield)),
          "a circuit the operator did not key was not given one anyway");
    check(pv->verifying_keys_loaded(), "and the verifier still reports a real key loaded");

    const Bytes commitment = from_hex(golden::kSatisfyingCommitment);
    const Id bind = test_bind();
    Transaction tx;
    tx.type = TxType::Shield;
    tx.version = 1;
    tx.outputs = {{commitment, {}, {}, {}}};
    tx.proof = ZkProof{
        "groth16", from_hex(golden::kGroth16Frame), {Bytes(bind.begin(), bind.end()), commitment}};
    tx.id = tx.compute_id();

    // Zeros decode to points at infinity, and a pairing drops any term whose
    // argument is infinity — which empties both sides and makes every proof
    // valid. So an unkeyed circuit must be refused BY NAME, never judged against
    // whatever stands in for a key.
    check_err(pv->verify(tx), "no verifying key for circuit",
              "a proof against an unkeyed circuit is refused by name");
}

void groth16_reaches_its_pairing() {
    std::printf("\nany proof that decodes produces a VERDICT, not a refusal in front of it\n");

    auto pv = keyed({{circuit_key(TxType::Transfer), from_hex(golden::kGroth16Key4)}});
    if (!pv) return;

    const Bytes nullifier = from_hex(golden::kSatisfyingNullifier);
    const Bytes commitment = from_hex(golden::kSatisfyingCommitment);
    const Id bind = test_bind();

    Transaction tx;
    tx.type = TxType::Transfer;
    tx.version = 1;
    tx.nullifiers = {nullifier};
    tx.outputs = {{commitment, {}, {}, {}}};
    tx.proof = ZkProof{"groth16",
                       from_hex(golden::kGroth16Frame),
                       {Bytes(bind.begin(), bind.end()), nullifier, commitment}};
    tx.id = tx.compute_id();

    // The proof satisfies no statement, so the verdict is "rejected" — but a
    // verdict FROM THE ARITHMETIC is what has to come back, not a refusal from
    // the checks in front of it.
    check_err(pv->verify(tx), "pairing check failed",
              "a well-formed proof of nothing is refused BY THE PAIRING");
}

}  // namespace

int main() {
    a_satisfying_proof_is_accepted_and_remembered();
    proof_refusals();
    public_inputs_bind_the_transaction();
    a_circuit_without_a_key_is_refused_by_name();
    groth16_reaches_its_pairing();
    return report("verifier");
}
