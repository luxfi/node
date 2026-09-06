// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// verifier.hpp — what stands between a peer's bytes and shielded value.
//
// ONE profile bit, strict_pq, drives the whole posture:
//
//   strict-PQ (the Z-Chain's DEFAULT)  every classical, quantum-breakable
//       system — groth16, plonk, bulletproofs — is refused BEFORE any other
//       check, and STARK/FRI is the only system that reaches a verifier.
//       Loading a real bn254 verifying key is an ERROR at construction, not a
//       warning: such a key would re-enable the forgeable pairing path for
//       shielded value.
//
//   permissive (explicit opt-in only)  the classical path is available and is
//       judged on its own arithmetic.
//
// A CRQC that breaks bn254 must not be able to forge a shield or unshield proof
// and mint shielded value. That is why the default is the strict one and why a
// permissive chain has to say so.

#pragma once

#include "lux/zkvm/groth16.hpp"
#include "lux/zkvm/id.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/wire.hpp"

#include <list>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>

namespace lux::zkvm {

inline constexpr const char* kErrStrictPQClassicalForbidden =
    "zkvm: classical proof system forbidden on strict-PQ chain — only STARK/FRI accepted";
inline constexpr const char* kErrStrictPQRealVKForbidden =
    "zkvm: real bn254 verifying key forbidden on strict-PQ chain — shielded value uses "
    "STARK/FRI (P3Q) only";
inline constexpr const char* kErrProofDisabled =
    "zkvm: proof verification disabled — no real verifying keys loaded";
inline constexpr const char* kErrPlonkIncomplete =
    "plonk: the verification equation is not implemented — failing closed rather than "
    "accepting a proof on its shape";
inline constexpr const char* kErrBulletproofsIncomplete =
    "zkvm: Bulletproof verification not yet implemented, use groth16 or plonk";
inline constexpr const char* kErrUnsupportedProofType = "unsupported proof type";
inline constexpr const char* kErrCachedFailure = "proof verification failed (cached)";

// Defaults for a config that names no bound. Zero would mean a block of
// unbounded size and a cache of unbounded growth, which is not "no limit
// configured" but "no limit".
inline constexpr std::uint32_t kDefaultMaxTxPerBlock = 100;
inline constexpr std::uint32_t kDefaultProofCacheSize = 1000;

struct ZConfig {
    // verifying_keys supplies real (non-dummy) verifying keys per circuit, keyed
    // by circuit_key(TxType). On a strict-PQ chain, supplying one is REFUSED at
    // construction.
    std::map<std::string, Bytes> verifying_keys;

    bool strict_pq = false;
    std::uint32_t max_tx_per_block = kDefaultMaxTxPerBlock;
    std::uint32_t proof_cache_size = kDefaultProofCacheSize;
};

// Cache is the verified-proof memo, bounded and least-recently-used. It is keyed
// on the transaction's CONTENT — compute_id covers the nullifiers, the outputs
// and the proof — so a hit means THIS EXACT transaction verified before.
//
// It used to be keyed on the id as READ OFF THE WIRE, plus the proof and its
// public inputs. All four were the peer's to choose: copy an accepted
// transaction's id, proof type, proof and public inputs onto a transaction
// spending entirely different notes, and the key matched. The hit returned "ok"
// before anything tied the public inputs to what the transaction spends, and the
// shielded pool gained value backed by a proof that attests to nothing about it.
class Cache {
public:
    explicit Cache(std::size_t capacity) : capacity_(capacity) {}

    bool get(const Id& key, bool* value);
    void add(const Id& key, bool value);
    std::size_t size() const { return map_.size(); }

private:
    std::size_t capacity_;
    std::list<std::pair<Id, bool>> order_;
    std::map<Id, std::list<std::pair<Id, bool>>::iterator> map_;
};

class ProofVerifier {
public:
    // open fails when the cache is not a cache — a bound of zero is not a
    // verifier — and when a strict-PQ chain is handed a real bn254 key.
    static wire::Result<std::unique_ptr<ProofVerifier>> open(const ZConfig& config,
                                                             const Id& bind);

    wire::Result<void> verify(const Transaction& tx);

    // refuse_classical_under_strict_pq is the SINGLE strict-PQ enforcement point
    // for the shielded-tx verifier. On a strict-PQ chain it refuses every
    // classical system, leaving only "stark". On a permissive chain it is a
    // no-op.
    wire::Result<void> refuse_classical_under_strict_pq(const std::string& proof_type) const;

    bool verifying_keys_loaded() const { return !dummy_keys_; }
    bool has_key(const std::string& circuit) const { return keys_.count(circuit) != 0; }

    std::size_t cache_size() const;
    void stats(std::uint64_t* verify_count, std::uint64_t* hits, std::uint64_t* misses) const;

    // force_real_keys exists for the ported regression tests that reach the
    // classical switch body on a chain holding no key, exactly as the Go tests
    // set pv.dummyKeys = false. It changes no policy: the per-circuit lookup in
    // front of every verify still refuses a circuit nobody keyed.
    void force_real_keys() { dummy_keys_ = false; }
    void set_key(const std::string& circuit, Bytes vk) { keys_[circuit] = std::move(vk); }

    bool strict_pq() const { return config_.strict_pq; }

private:
    ProofVerifier(const ZConfig& config, const Id& bind)
        : config_(config), bind_(bind), cache_(config.proof_cache_size) {}

    wire::Result<void> load_verifying_keys();
    wire::Result<void> verify_stark(const Transaction& tx) const;
    wire::Result<void> verify_groth16(const Transaction& tx) const;
    wire::Result<void> verify_public_inputs(const Transaction& tx) const;

    ZConfig config_;
    // bind is sha256(ChainID ‖ NetworkID). It is the first public input every
    // proof is checked against, so a proof made for another chain does not
    // verify here even when the notes it names are unspent on both.
    Id bind_;

    std::map<std::string, Bytes> keys_;
    bool dummy_keys_ = true;

    mutable std::mutex mu_;
    mutable Cache cache_;
    mutable std::uint64_t verify_count_ = 0;
    mutable std::uint64_t cache_hits_ = 0;
    mutable std::uint64_t cache_misses_ = 0;
};

}  // namespace lux::zkvm
