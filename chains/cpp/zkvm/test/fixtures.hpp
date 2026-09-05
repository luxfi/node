// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fixtures.hpp — the transactions the Go Z-Chain's own tests are written
// around, built here field for field so a C++ assertion and a Go assertion are
// about the same value.

#pragma once

#include "check.hpp"
#include "golden.hpp"

#include "lux/zkvm/starkfri.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/vm.hpp"

#include <string>

namespace lux::zkvm::test {

inline Bytes b(const std::string& s) {
    return Bytes(reinterpret_cast<const std::uint8_t*>(s.data()),
                 reinterpret_cast<const std::uint8_t*>(s.data()) + s.size());
}

// id_of makes the ids.ID{n} the Go fixtures write: the first byte set, the rest
// zero.
inline Id id_of(std::uint8_t n) {
    Id id{};
    id[0] = n;
    return id;
}

inline Id id_from_hex(const char* h) {
    Id id{};
    const Bytes raw = from_hex(h);
    if (raw.size() == 32) std::copy(raw.begin(), raw.end(), id.begin());
    return id;
}

inline Id test_bind() { return id_from_hex(golden::kTestBind); }

// shield_tx is Go's shieldTx(): something in every field, for the identity
// tests.
inline Transaction shield_tx() {
    Transaction tx;
    tx.type = TxType::Shield;
    tx.version = 1;
    tx.transparent_inputs = {{id_of(1), 0, 100, b("payer")}};
    tx.transparent_outputs = {{90, id_of(2), b("payee")}};
    tx.nullifiers = {b("n")};
    tx.outputs = {{b("c"), {}, {}, {}}};
    tx.proof = ZkProof{"groth16", b("p"), {}};
    tx.fee = 7;
    return tx;
}

// block_tx is Go's blockTx(fee): something in every list, so a block built
// around it exercises the packed sub-frames rather than a trivial one.
inline Transaction block_tx(std::uint64_t fee) {
    Transaction tx;
    tx.type = TxType::Shield;
    tx.version = 1;
    tx.fee = fee;
    tx.transparent_inputs = {{id_of(1), 2, 100, b("payer")}};
    tx.transparent_outputs = {{90, id_of(2), b("payee")}};
    tx.nullifiers = {b("n1"), b("n2")};
    tx.outputs = {{b("c"), b("n"), b("e"), b("p")}};
    tx.proof = ZkProof{"groth16", b("pd"), {b("a"), b("bb")}};
    tx.expiry = 1u << 20;
    tx.memo = b("memo");
    tx.id = tx.compute_id();
    return tx;
}

// wire_tx is the transaction Go's wire round-trip is written around.
inline Transaction wire_tx() {
    Transaction tx;
    tx.type = TxType::Shield;
    tx.version = 1;
    tx.fee = 100;
    tx.expiry = 999;
    tx.transparent_inputs = {{id_of(4), 2, 50, b("addr-in")}};
    tx.transparent_outputs = {{30, id_of(5), b("addr-out")}};
    tx.nullifiers = {b("null1"), b("null2")};
    tx.outputs = {{b("c"), b("n"), b("e"), b("p")}};
    tx.proof = ZkProof{"groth16", b("pd"), {b("pi1"), b("pi2")}};
    tx.memo = b("memo");
    return tx;
}

// shielded_transfer is Go's smallest transaction the mempool will accept.
inline Transaction shielded_transfer(std::uint64_t fee, std::uint8_t n) {
    Transaction tx;
    tx.type = TxType::Transfer;
    tx.version = 1;
    tx.fee = fee;
    tx.nullifiers = {Bytes{n}};
    tx.outputs = {{Bytes{n}, {}, {}, {}}};
    tx.proof = ZkProof{"groth16", b("p"), {}};
    tx.expiry = 1u << 20;
    tx.id = tx.compute_id();
    return tx;
}

// shielded_tx is Go's shieldedTx(kind, data): a minimal shielded transfer whose
// public inputs are already the ones a verifier expects.
inline Transaction shielded_tx(const std::string& proof_type, Bytes proof_data) {
    const Bytes nullifier(32, 0);
    const Bytes commitment(32, 0);
    const Id bind = test_bind();
    Transaction tx;
    tx.type = TxType::Transfer;
    tx.version = 1;
    tx.nullifiers = {nullifier};
    tx.outputs = {{commitment, {}, {}, {}}};
    tx.proof = ZkProof{proof_type, std::move(proof_data),
                       {Bytes(bind.begin(), bind.end()), nullifier, commitment}};
    tx.id = tx.compute_id();
    return tx;
}

// bind_accepting_stark_verifier binds a STARK/FRI verifier that accepts. It is
// the SAME seam the real P3Q binding uses — the reference's own tests bind
// through it too — so a test chain is a strict-PQ chain with its verifier
// present, not a chain with its gate removed.
inline void bind_accepting_stark_verifier() {
    starkfri::register_verifier(
        [](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> { return true; });
}

inline void unbind_stark_verifier() { starkfri::register_verifier(nullptr); }

// stark_tx is a shielded transfer a strict-PQ chain will admit once a verifier
// is bound: one note in, one note out, and a proof carrying the wire format's
// magic header.
inline Transaction stark_tx(std::uint64_t fee, std::uint8_t n, std::uint64_t expiry = 1u << 20) {
    Bytes proof = b(starkfri::kMagicHeader);
    proof.push_back(n);

    Transaction tx;
    tx.type = TxType::Transfer;
    tx.version = 1;
    tx.fee = fee;
    tx.expiry = expiry;
    tx.nullifiers = {Bytes(32, n)};
    tx.outputs = {{Bytes(32, std::uint8_t(n ^ 0xFF)), b("note"), b("epk"), {}}};
    tx.proof = ZkProof{"stark", std::move(proof), {}};
    tx.id = tx.compute_id();
    return tx;
}

// strict_config is the Z-Chain's own profile: strict-PQ, over the golden
// corpus's chain identity.
inline VmConfig strict_config() {
    VmConfig c;
    c.chain_id = id_from_hex(golden::kChainId);
    c.network_id = golden::kNetworkId;
    c.alias = "Z";
    return c;
}

// The chain the golden corpus was generated against.
inline VmConfig golden_config() {
    VmConfig c;
    c.chain_id = id_from_hex(golden::kChainId);
    c.network_id = golden::kNetworkId;
    c.alias = "Z";
    c.z.strict_pq = false;  // the corpus exercises the classical path
    c.z.verifying_keys[circuit_key(TxType::Transfer)] =
        from_hex(golden::kSpendingVerifyingKey);
    return c;
}

}  // namespace lux::zkvm::test
