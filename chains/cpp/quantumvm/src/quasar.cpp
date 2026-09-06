// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// quasar.cpp — the BLS12-381 leg over blst, and the bridge above it.
//
// blst is the same library the Go node reaches through supranational/blst, and
// the domain tag is spelled once (quasar.hpp), so a signature accepted here is
// a signature accepted there.

#include "lux/quantumvm/quasar.hpp"

#include "lux/quantumvm/config.hpp"

#include "mldsa.hpp"

#include <blst.h>

#include <cstring>
#include <random>

namespace lux::quantumvm::quasar {
namespace {

void fill_random(std::uint8_t* p, std::size_t n) {
    static thread_local std::random_device rd;
    for (std::size_t i = 0; i < n; ++i) p[i] = static_cast<std::uint8_t>(rd() & 0xff);
}

const byte* dst() { return reinterpret_cast<const byte*>(kSigDst); }

// A fresh BLS keypair: a scalar from 32 bytes of entropy, and the G1 point it
// names.
void bls_keygen(Bytes& secret, Bytes& public_key) {
    std::uint8_t ikm[32];
    fill_random(ikm, sizeof(ikm));

    blst_scalar sk{};
    blst_keygen(&sk, ikm, sizeof(ikm), nullptr, 0);
    secret.assign(32, 0);
    blst_bendian_from_scalar(secret.data(), &sk);

    blst_p1 pk{};
    blst_sk_to_pk_in_g1(&pk, &sk);
    public_key.assign(kBlsPublicKeyLen, 0);
    blst_p1_compress(public_key.data(), &pk);
}

Bytes bls_sign(const Bytes& secret, ByteView message) {
    blst_scalar sk{};
    blst_scalar_from_bendian(&sk, secret.data());

    blst_p2 hash{};
    blst_hash_to_g2(&hash, message.data(), message.size(), dst(), kSigDstLen, nullptr, 0);
    blst_p2 sig{};
    blst_sign_pk_in_g1(&sig, &hash, &sk);

    Bytes out(kBlsSignatureLen, 0);
    blst_p2_compress(out.data(), &sig);
    return out;
}

bool bls_verify(const Bytes& public_key, const Bytes& signature, ByteView message) {
    if (public_key.size() != kBlsPublicKeyLen || signature.size() != kBlsSignatureLen) return false;
    blst_p1_affine pk{};
    if (blst_p1_uncompress(&pk, public_key.data()) != BLST_SUCCESS) return false;
    if (blst_p1_affine_is_inf(&pk)) return false;
    blst_p2_affine sig{};
    if (blst_p2_uncompress(&sig, signature.data()) != BLST_SUCCESS) return false;
    return blst_core_verify_pk_in_g1(&pk, &sig, /*hash_or_encode=*/true, message.data(),
                                     message.size(), dst(), kSigDstLen, nullptr, 0) == BLST_SUCCESS;
}

std::optional<Bytes> bls_aggregate_signatures(const std::vector<Bytes>& sigs) {
    if (sigs.empty()) return std::nullopt;
    blst_p2 acc{};
    bool first = true;
    for (const auto& s : sigs) {
        if (s.size() != kBlsSignatureLen) return std::nullopt;
        blst_p2_affine a{};
        if (blst_p2_uncompress(&a, s.data()) != BLST_SUCCESS) return std::nullopt;
        if (first) {
            blst_p2_from_affine(&acc, &a);
            first = false;
        } else {
            blst_p2_add_or_double_affine(&acc, &acc, &a);
        }
    }
    Bytes out(kBlsSignatureLen, 0);
    blst_p2_compress(out.data(), &acc);
    return out;
}

std::optional<Bytes> bls_aggregate_public_keys(const std::vector<Bytes>& keys) {
    if (keys.empty()) return std::nullopt;
    blst_p1 acc{};
    bool first = true;
    for (const auto& k : keys) {
        if (k.size() != kBlsPublicKeyLen) return std::nullopt;
        blst_p1_affine a{};
        if (blst_p1_uncompress(&a, k.data()) != BLST_SUCCESS) return std::nullopt;
        if (blst_p1_affine_is_inf(&a)) return std::nullopt;
        if (first) {
            blst_p1_from_affine(&acc, &a);
            first = false;
        } else {
            blst_p1_add_or_double_affine(&acc, &acc, &a);
        }
    }
    Bytes out(kBlsPublicKeyLen, 0);
    blst_p1_compress(out.data(), &acc);
    return out;
}

// The identity attestation rides beside the BLS half under ML-DSA-65, which is
// what the Go core registers for every validator.
const quantum::QuantumSigner& identity_signer() {
    static const quantum::QuantumSigner s = [] {
        auto made = quantum::QuantumSigner::make(quantum::kMldsa65, std::chrono::hours(24 * 365));
        return *made;
    }();
    return s;
}

}  // namespace

const QuasarSig* PendingBlock::by(const std::string& validator_id) const {
    for (const auto& sig : signatures)
        if (sig.validator_id == validator_id) return &sig;
    return nullptr;
}

// ── Core

Result<std::shared_ptr<Core>> Core::make(int threshold) {
    if (threshold < 2) return fail(Err::CommitteeTooSmall, "threshold must be at least 2");
    return std::shared_ptr<Core>(new Core(threshold));
}

Status Core::add_validator(const std::string& validator_id, std::uint64_t weight) {
    std::lock_guard lock(mu_);
    Validator v;
    bls_keygen(v.bls_secret, v.bls_public);

    auto key = identity_signer().generate_key();
    if (!key) return std::unexpected(key.error());
    v.mldsa = std::move(*key);
    v.weight = weight;
    v.active = true;
    validators_[validator_id] = std::move(v);
    return ok();
}

const Core::Validator* Core::find(const std::string& id) const {
    auto it = validators_.find(id);
    if (it == validators_.end() || !it->second.active) return nullptr;
    return &it->second;
}

Result<QuasarSig> Core::sign(const std::string& validator_id, ByteView message) const {
    std::lock_guard lock(mu_);
    const Validator* v = find(validator_id);
    if (v == nullptr) return fail(Err::SignRefused, "validator not found");

    QuasarSig sig;
    sig.validator_id = validator_id;
    sig.bls = bls_sign(v->bls_secret, message);

    // The post-quantum half rides inside every signature this collects. It is
    // a plain ML-DSA signature over the same message — no stamp and no window,
    // because a consensus signature is bound to the round it names, not to a
    // wall clock.
    const Bytes covered(message.begin(), message.end());
    namespace m = lux::crypto::mldsa;
    sig.mldsa.assign(m::SIG65, 0);
    std::size_t written = m::SIG65;
    if (m::sign_65(sig.mldsa.data(), &written, covered.data(), covered.size(),
                   v->mldsa.private_key.data()))
        sig.mldsa.resize(written);
    else
        sig.mldsa.clear();

    return sig;
}

bool Core::verify(ByteView message, const QuasarSig* sig) const {
    if (sig == nullptr) return false;
    std::lock_guard lock(mu_);

    const Validator* v = find(sig->validator_id);
    if (v == nullptr) return false;
    if (!bls_verify(v->bls_public, sig->bls, message)) return false;

    // The PQ identity path, when present. A signature that carries one and
    // fails it is refused: half a statement is not a statement.
    if (!sig->mldsa.empty()) {
        namespace m = lux::crypto::mldsa;
        if (!m::verify_65(sig->mldsa.data(), sig->mldsa.size(), message.data(), message.size(),
                          v->mldsa.public_key.data()))
            return false;
    }
    return true;
}

Result<AggregatedSignature> Core::aggregate(ByteView message,
                                            const std::vector<QuasarSig>& signatures) const {
    (void)message;
    std::lock_guard lock(mu_);
    if (static_cast<int>(signatures.size()) < threshold_)
        return fail(Err::NotEnoughSignatures, std::to_string(signatures.size()) + " < " +
                                                  std::to_string(threshold_));

    std::vector<Bytes> bls;
    AggregatedSignature agg;
    bls.reserve(signatures.size());
    agg.validator_ids.reserve(signatures.size());
    for (const auto& sig : signatures) {
        bls.push_back(sig.bls);
        agg.validator_ids.push_back(sig.validator_id);
    }

    auto sum = bls_aggregate_signatures(bls);
    if (!sum) return fail(Err::AggregateRefused, "a signature in the set is not a G2 point");
    agg.bls_aggregated = std::move(*sum);
    agg.signer_count = static_cast<int>(signatures.size());
    agg.is_threshold = false;
    return agg;
}

bool Core::verify_aggregate(ByteView message, const AggregatedSignature* agg) const {
    if (agg == nullptr) return false;
    std::lock_guard lock(mu_);

    // signer_count travels inside the message the sender chose, so it is not
    // evidence of anything. It is worth rejecting on early — a sender that does
    // not even CLAIM a quorum is not offering one — but it is never the quantity
    // the threshold is measured against.
    if (agg->signer_count < threshold_) return false;

    // Nor does the sender get to choose HOW its aggregate is checked. This core
    // holds per-validator keys and nothing else, so it can check a sum of those
    // keys and it cannot check a threshold group signature. An aggregate that
    // announces itself as one has nothing here to verify it, and unverified is
    // refused — which is the same answer Go gives, where the threshold verifier
    // on this path is never constructed.
    if (agg->is_threshold) return false;

    if (agg->bls_aggregated.size() != kBlsSignatureLen) return false;

    // The aggregate is checked against the aggregated keys of the DISTINCT
    // registered validators it names. A name nobody holds a key for fails; a
    // name repeated to inflate the count fails the same way.
    std::set<std::string> seen;
    std::vector<Bytes> keys;
    keys.reserve(agg->validator_ids.size());
    for (const auto& id : agg->validator_ids) {
        if (!seen.insert(id).second) return false;
        const Validator* v = find(id);
        if (v == nullptr) return false;
        keys.push_back(v->bls_public);
    }

    // THE QUORUM. Counted over the validators whose keys actually entered the
    // sum — the only number here that had to be earned, because every id in it
    // was looked up in the committee and its key is under the signature about to
    // be checked. Leaving this to the sender's own count let one validator hand
    // over its own signature, name itself once, claim to be a hundred, and
    // finalize a block alone.
    if (static_cast<int>(seen.size()) < threshold_) return false;

    auto sum = bls_aggregate_public_keys(keys);
    if (!sum) return false;
    return bls_verify(*sum, agg->bls_aggregated, message);
}

int Core::active_validators() const {
    std::lock_guard lock(mu_);
    int n = 0;
    for (const auto& [id, v] : validators_)
        if (v.active) ++n;
    return n;
}

// ── the bridge

Result<std::shared_ptr<Quasar>> Quasar::make(Config cfg) {
    if (cfg.validator_id.empty()) return fail(Err::NoValidatorID);
    if (cfg.committee == 0) cfg.committee = config::kCommitteeMin;
    if (cfg.committee < config::kCommitteeMin)
        return fail(Err::CommitteeTooSmall,
                    "a committee of " + std::to_string(cfg.committee) +
                        " tolerates no fault; " + std::to_string(config::kCommitteeMin) +
                        " is the smallest that does");

    auto core = Core::make(config::quorum(cfg.committee));
    if (!core) return std::unexpected(core.error());

    std::shared_ptr<Quasar> q(new Quasar(*core, cfg.validator_id, cfg.committee));
    if (auto r = q->add_validator(cfg.validator_id, 1); !r)
        return fail(r.error().code,
                    "failed to register this node with its own consensus core: " +
                        r.error().message());
    return q;
}

Status Quasar::add_validator(const std::string& validator_id, std::uint64_t weight) {
    std::lock_guard lock(mu_);
    if (validator_id.empty()) return fail(Err::NoValidatorID);
    // The committee is a SET, and it is the set the threshold was derived from.
    // Registering an id twice hands the core a fresh key for it, which silently
    // invalidates every signature that validator has already contributed;
    // registering more members than the committee declares makes the threshold
    // a quorum of a committee that no longer exists.
    if (members_.count(validator_id) != 0) return fail(Err::AlreadyRegistered, validator_id);
    if (static_cast<int>(members_.size()) >= committee_)
        return fail(Err::CommitteeFull,
                    std::to_string(members_.size()) + " of " + std::to_string(committee_));

    if (auto r = core_->add_validator(validator_id, weight); !r) return r;
    members_.insert(validator_id);
    return ok();
}

Status Quasar::record(PendingBlock& p, const QuasarSig* sig) {
    if (sig == nullptr) return fail(Err::UnverifiedSigner);
    if (!core_->verify(view(p.block_hash), sig))
        return fail(Err::UnverifiedSigner, "\"" + sig->validator_id + "\" on block " + text(p.block_id));
    if (p.by(sig->validator_id) != nullptr) return fail(Err::DuplicateSigner, sig->validator_id);
    p.signatures.push_back(*sig);
    return ok();
}

Result<QuasarSig> Quasar::sign_block(const Id& block_id, ByteView block_hash, std::uint64_t height) {
    // What this node would be signing, and whether it already has. Reading it
    // under the lock and signing outside it keeps the core's work off the
    // bridge's mutex.
    Bytes message;
    {
        std::lock_guard lock(mu_);
        auto it = pending_.find(block_id);
        if (it != pending_.end()) {
            if (const QuasarSig* have = it->second.by(validator_id_)) return *have;
            // The hash the block is TRACKED under, not the one this call was
            // handed: peers' signatures already landed against that message.
            message = it->second.block_hash;
        } else {
            message.assign(block_hash.begin(), block_hash.end());
        }
    }

    auto sig = core_->sign(validator_id_, view(message));
    // Nothing has been recorded yet, so a signature that never arrived leaves
    // nothing behind. Creating the tracking entry FIRST is what leaked one map
    // entry per failure for the life of the process: only a block this node
    // signed is ever finalized, and only a finalized block is ever cleaned up.
    if (!sig) return std::unexpected(sig.error());

    std::lock_guard lock(mu_);
    auto it = pending_.find(block_id);
    if (it != pending_.end()) {
        if (auto r = record(it->second, &*sig); !r) {
            if (const QuasarSig* have = it->second.by(validator_id_)) return *have;
            return std::unexpected(r.error());
        }
        return *sig;
    }

    // A block becomes tracked exactly when it holds a verified signature, so
    // there is no state in which it is tracked and empty.
    PendingBlock fresh;
    fresh.block_id = block_id;
    fresh.block_hash = std::move(message);
    fresh.height = height;
    if (auto r = record(fresh, &*sig); !r) return std::unexpected(r.error());
    pending_.emplace(block_id, std::move(fresh));
    return *sig;
}

Status Quasar::add_signature(const Id& block_id, const QuasarSig* sig) {
    std::lock_guard lock(mu_);
    auto it = pending_.find(block_id);
    if (it == pending_.end()) return fail(Err::UnknownBlock, text(block_id));
    return record(it->second, sig);
}

bool Quasar::verify_signature(ByteView message, const QuasarSig* sig) const {
    if (sig == nullptr) return false;
    return core_->verify(message, sig);
}

bool Quasar::verify_aggregate(ByteView message, const AggregatedSignature* agg) const {
    if (agg == nullptr) return false;
    return core_->verify_aggregate(message, agg);
}

Result<Quasar::Finality> Quasar::try_finalize(const Id& block_id) {
    std::lock_guard lock(mu_);
    auto it = pending_.find(block_id);
    if (it == pending_.end()) return fail(Err::UnknownBlock, text(block_id));
    PendingBlock& p = it->second;

    Finality out;
    if (static_cast<int>(p.signatures.size()) < core_->threshold()) return out;

    auto agg = core_->aggregate(view(p.block_hash), p.signatures);
    if (!agg) return std::unexpected(agg.error());
    // Reaching the count is necessary and not sufficient: the aggregate is
    // built AND checked, and only a check that passes finalizes anything.
    if (!core_->verify_aggregate(view(p.block_hash), &*agg))
        return fail(Err::AggregateRefused, text(block_id));

    p.finalized = true;
    finalized_.insert(block_id);
    out.aggregate = std::move(*agg);
    out.finalized = true;
    return out;
}

bool Quasar::is_finalized(const Id& block_id) const {
    std::lock_guard lock(mu_);
    return finalized_.count(block_id) != 0;
}

void Quasar::cleanup(std::uint64_t min_height) {
    std::lock_guard lock(mu_);
    for (auto it = pending_.begin(); it != pending_.end();) {
        if (it->second.height < min_height) {
            finalized_.erase(it->first);
            it = pending_.erase(it);
        } else {
            ++it;
        }
    }
}

int Quasar::active_validators() const {
    std::lock_guard lock(mu_);
    return static_cast<int>(members_.size());
}

bool Quasar::tracking(const Id& block_id) const {
    std::lock_guard lock(mu_);
    return pending_.count(block_id) != 0;
}

std::size_t Quasar::tracked() const {
    std::lock_guard lock(mu_);
    return pending_.size();
}

std::vector<QuasarSig> Quasar::signatures_of(const Id& block_id) const {
    std::lock_guard lock(mu_);
    auto it = pending_.find(block_id);
    if (it == pending_.end()) return {};
    return it->second.signatures;
}

}  // namespace lux::quantumvm::quasar
