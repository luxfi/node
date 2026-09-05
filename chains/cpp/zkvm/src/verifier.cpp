// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/verifier.hpp"

#include "lux/zkvm/starkfri.hpp"

namespace lux::zkvm {

bool Cache::get(const Id& key, bool* value) {
    auto it = map_.find(key);
    if (it == map_.end()) return false;
    order_.splice(order_.begin(), order_, it->second);
    *value = it->second->second;
    return true;
}

void Cache::add(const Id& key, bool value) {
    auto it = map_.find(key);
    if (it != map_.end()) {
        it->second->second = value;
        order_.splice(order_.begin(), order_, it->second);
        return;
    }
    order_.emplace_front(key, value);
    map_[key] = order_.begin();
    while (map_.size() > capacity_) {
        map_.erase(order_.back().first);
        order_.pop_back();
    }
}

wire::Result<std::unique_ptr<ProofVerifier>> ProofVerifier::open(const ZConfig& config,
                                                                const Id& bind) {
    // A cache too small to hold anything is not a verifier.
    if (config.proof_cache_size == 0)
        return std::unexpected("zkvm: proof cache size must be positive");

    std::unique_ptr<ProofVerifier> pv(new ProofVerifier(config, bind));
    if (auto r = pv->load_verifying_keys(); !r) return std::unexpected(r.error());
    return pv;
}

// load_verifying_keys takes the verifying key for each circuit the config keys
// and installs nothing for the others.
//
// A circuit the operator did not key has no key here, so the lookup in front of
// each verify refuses it BY NAME. That is the one place the question is asked,
// and it is asked per circuit: a key is judged on its own, never on what the
// verifier holds for some other circuit. An absent key cannot be mistaken for a
// present one.
wire::Result<void> ProofVerifier::load_verifying_keys() {
    for (TxType t : {TxType::Transfer, TxType::Shield, TxType::Unshield}) {
        const std::string k = circuit_key(t);
        auto it = config_.verifying_keys.find(k);
        if (it != config_.verifying_keys.end() && !it->second.empty()) keys_[k] = it->second;
    }

    // A verifier holding no key at all judges nothing, which is what the
    // classical path checks before it starts.
    dummy_keys_ = keys_.empty();

    // Strict-PQ hard gate. Loading a REAL (non-dummy) bn254 verifying key on a
    // strict-PQ chain is forbidden: such keys would re-enable the forgeable
    // classical pairing path for shielded value. Refuse explicitly at
    // construction rather than relying on the implicit dummy-key detector.
    if (config_.strict_pq && !dummy_keys_) return std::unexpected(kErrStrictPQRealVKForbidden);
    return {};
}

wire::Result<void> ProofVerifier::refuse_classical_under_strict_pq(
    const std::string& proof_type) const {
    if (!config_.strict_pq) return {};
    if (proof_type == "stark") return {};
    // groth16, plonk, bulletproofs, anything else: forbidden.
    return std::unexpected(kErrStrictPQClassicalForbidden);
}

wire::Result<void> ProofVerifier::verify(const Transaction& tx) {
    if (!tx.proof) return std::unexpected(kErrMissingProof);

    // The strict-PQ gate fires FIRST, before anything else can reach a classical
    // body.
    if (auto r = refuse_classical_under_strict_pq(tx.proof->proof_type); !r) return r;

    // STARK/FRI: the ONLY accepted system on a strict-PQ chain and the
    // quantum-safe shielded path everywhere. It fails closed when no FRI
    // verifier is bound.
    if (tx.proof->proof_type == "stark") return verify_stark(tx);

    // Classical path — permissive chains only; the gate above already rejected
    // these under strict-PQ. It requires real (non-dummy) keys.
    if (dummy_keys_) return std::unexpected(kErrProofDisabled);

    const Id key = tx.compute_id();
    {
        std::lock_guard<std::mutex> g(mu_);
        ++verify_count_;
        bool cached = false;
        if (cache_.get(key, &cached)) {
            ++cache_hits_;
            if (cached) return {};
            return std::unexpected(kErrCachedFailure);
        }
        ++cache_misses_;
    }

    wire::Result<void> verdict = {};
    if (tx.proof->proof_type == "groth16") {
        verdict = verify_groth16(tx);
    } else if (tx.proof->proof_type == "plonk") {
        // What stood in this place decoded the proof and the key — exact sizes,
        // subgroup checks, points at infinity — and then handed the result to a
        // pairing that refused every proof unconditionally, because the
        // verification equation was never written. Decoding a proof carefully in
        // order to refuse it is not a verifier; it is a shape that reads like
        // one. The refusal is the whole of what this path does, so it is the
        // whole of what this path says.
        verdict = std::unexpected(kErrPlonkIncomplete);
    } else if (tx.proof->proof_type == "bulletproofs") {
        verdict = std::unexpected(kErrBulletproofsIncomplete);
    } else {
        verdict = std::unexpected(kErrUnsupportedProofType);
    }

    {
        std::lock_guard<std::mutex> g(mu_);
        cache_.add(key, verdict.has_value());
    }
    return verdict;
}

// verify_stark delegates to the strict-PQ FRI seam. The public inputs are the
// chain binding followed by this transaction's nullifiers and output
// commitments, which is what binds the proof to this transaction's shielded
// value flow — and the chain goes first, so a proof made for another chain does
// not verify here even when the notes it names are unspent on both.
wire::Result<void> ProofVerifier::verify_stark(const Transaction& tx) const {
    Bytes pub;
    pub.reserve(96);
    pub.insert(pub.end(), bind_.begin(), bind_.end());
    for (const auto& n : tx.nullifiers) pub.insert(pub.end(), n.begin(), n.end());
    for (const auto& c : tx.output_commitments()) pub.insert(pub.end(), c.begin(), c.end());

    auto ok = starkfri::verify(view(tx.proof->proof_data), view(pub));
    if (!ok) {
        if (ok.error() == starkfri::kErrVerifierNotRegistered)
            return std::unexpected(
                "zkvm: strict-PQ STARK shielded verifier unbound (build with the P3Q "
                "binding): " +
                ok.error());
        return std::unexpected("zkvm: STARK shielded proof verification failed: " + ok.error());
    }
    if (!*ok) return std::unexpected("zkvm: STARK shielded proof rejected");
    return {};
}

wire::Result<void> ProofVerifier::verify_groth16(const Transaction& tx) const {
    const std::string circuit = circuit_key(tx.type);
    auto it = keys_.find(circuit);
    if (it == keys_.end())
        return std::unexpected("zkvm: no verifying key for circuit " +
                               std::to_string(static_cast<unsigned>(tx.type)));

    if (auto r = verify_public_inputs(tx); !r) return r;

    // Groth16 on this curve is 2 G1 points and 1 G2 point: 64 + 128 + 64.
    if (tx.proof->proof_data.size() < 256)
        return std::unexpected("invalid proof data length for Groth16");

    auto vk = groth16::deserialize_verifying_key(view(it->second));
    if (!vk)
        return std::unexpected("groth16 verification failed: failed to deserialize verifying "
                               "key: " +
                               vk.error());
    if (auto r = groth16::validate_verifying_key(*vk); !r)
        return std::unexpected("groth16 verification failed: verifying key validation failed: " +
                               r.error());

    auto proof = groth16::deserialize_proof(view(tx.proof->proof_data));
    if (!proof)
        return std::unexpected("groth16 verification failed: failed to deserialize proof: " +
                               proof.error());

    std::vector<groth16::U256> witness;
    witness.reserve(tx.proof->public_inputs.size());
    for (const auto& in : tx.proof->public_inputs)
        witness.push_back(groth16::fr_from_bytes(view(in)));

    if (auto r = groth16::verify_pairing(*proof, *vk, witness); !r)
        return std::unexpected("groth16 verification failed: pairing verification failed: " +
                               r.error());
    return {};
}

// verify_public_inputs verifies that the public inputs match the transaction.
//
// The first input is the chain binding, so a proof made for another chain does
// not verify here even when the notes it names happen to be unspent on both.
// Every other input is compared BY VALUE — a length match is not a match.
wire::Result<void> ProofVerifier::verify_public_inputs(const Transaction& tx) const {
    const auto& pub = tx.proof->public_inputs;
    if (pub.empty()) return std::unexpected("no public inputs provided");
    if (pub[0].size() != bind_.size() ||
        !std::equal(pub[0].begin(), pub[0].end(), bind_.begin()))
        return std::unexpected("public input mismatch for chain binding");

    for (std::size_t i = 0; i < tx.nullifiers.size(); ++i) {
        if (i + 1 >= pub.size()) return std::unexpected("missing public input for nullifier");
        if (pub[i + 1] != tx.nullifiers[i])
            return std::unexpected("public input mismatch for nullifier");
    }

    const auto commitments = tx.output_commitments();
    const std::size_t offset = tx.nullifiers.size() + 1;
    for (std::size_t i = 0; i < commitments.size(); ++i) {
        const std::size_t idx = offset + i;
        if (idx >= pub.size())
            return std::unexpected("missing public input for output commitment");
        if (pub[idx] != commitments[i])
            return std::unexpected("public input mismatch for output commitment");
    }
    return {};
}

std::size_t ProofVerifier::cache_size() const {
    std::lock_guard<std::mutex> g(mu_);
    return cache_.size();
}

void ProofVerifier::stats(std::uint64_t* verify_count, std::uint64_t* hits,
                          std::uint64_t* misses) const {
    std::lock_guard<std::mutex> g(mu_);
    if (verify_count) *verify_count = verify_count_;
    if (hits) *hits = cache_hits_;
    if (misses) *misses = cache_misses_;
}

}  // namespace lux::zkvm
