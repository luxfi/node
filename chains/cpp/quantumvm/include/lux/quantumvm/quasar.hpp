// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// quasar.hpp — the Q-chain finality bridge.
//
// Rendered from Go chains/quantumvm/quasar.go over
// consensus/protocol/quasar (core.go, quasar.go — the legacy per-validator
// BLS path this chain drives).
//
// A block is final when a quorum of the committee has SIGNED it and the
// aggregate of those signatures verifies against the committee's keys. Every
// word of that is load-bearing: signed, not claimed; verified, not counted;
// aggregate, not a tally of names a sender chose for itself.
//
// The quorum was once a count of STRINGS: a caller supplied a validator id and
// it was counted, unverified, against the threshold. So three fabricated ids
// finalized a block; five spellings of one name — case, a trailing space, an
// embedded NUL, the fullwidth forms — counted as five distinct signers of one
// signature; and a signature made for block A was accepted onto block B,
// because nothing bound a signature to its message. Verification fixes all
// three at once, because the name is looked up in the committee and the
// signature is checked against THAT key over THIS block.
//
// Each validator signature is TWO signatures over the same message: BLS12-381
// (the classical fast path, which is what aggregates) and ML-DSA-65 (FIPS 204,
// the post-quantum identity attestation). verify checks both, or rejects.

#pragma once

#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/signer.hpp"

#include <cstdint>
#include <map>
#include <mutex>
#include <set>
#include <string>
#include <vector>

namespace lux::quantumvm::quasar {

inline constexpr std::size_t kBlsPublicKeyLen = 48;
inline constexpr std::size_t kBlsSignatureLen = 96;

// The ordinary-signature ciphersuite, byte-identical to Go bls.dstSignature.
inline constexpr char kSigDst[] = "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_NUL_";
inline constexpr std::size_t kSigDstLen = sizeof(kSigDst) - 1;

// One validator's statement about one message.
struct QuasarSig {
    Bytes bls;                  // BLS12-381 G2 signature, 96 compressed bytes
    Bytes mldsa;                // per-validator ML-DSA-65 identity attestation
    std::string validator_id;   // whose statement it CLAIMS to be
    bool is_threshold = false;
    int signer_index = 0;

    friend bool operator==(const QuasarSig&, const QuasarSig&) = default;
};

// A quorum's statement, as one signature.
struct AggregatedSignature {
    Bytes bls_aggregated;
    std::vector<std::string> validator_ids;
    int signer_count = 0;
    bool is_threshold = false;
};

// The consensus signing core: it holds the committee's keys and is the only
// thing that can say whether a signature is real.
class Core {
  public:
    // Below two the "quorum" is one validator, which is not a quorum.
    static Result<std::shared_ptr<Core>> make(int threshold);

    Status add_validator(const std::string& validator_id, std::uint64_t weight);
    Result<QuasarSig> sign(const std::string& validator_id, ByteView message) const;

    // Verifies every path present: BLS always, ML-DSA when the signature
    // carries one. The claimed id is looked up in the committee and the
    // signature checked against THAT key.
    bool verify(ByteView message, const QuasarSig* sig) const;

    Result<AggregatedSignature> aggregate(ByteView message,
                                          const std::vector<QuasarSig>& signatures) const;
    bool verify_aggregate(ByteView message, const AggregatedSignature* agg) const;

    int threshold() const { return threshold_; }
    int active_validators() const;

  private:
    explicit Core(int t) : threshold_(t) {}

    struct Validator {
        Bytes bls_secret;
        Bytes bls_public;
        quantum::MldsaValidatorKey mldsa;
        std::uint64_t weight = 0;
        bool active = true;
    };

    const Validator* find(const std::string& id) const;

    mutable std::mutex mu_;
    std::map<std::string, Validator> validators_;
    int threshold_ = 0;
};

// A block gathering signatures. Every signature in the vector has been verified
// against block_hash and against the registered key of the validator it names,
// and no two name the same validator.
struct PendingBlock {
    Id block_id{};
    Bytes block_hash;
    std::uint64_t height = 0;
    std::vector<QuasarSig> signatures;
    bool finalized = false;

    const QuasarSig* by(const std::string& validator_id) const;
};

struct Config {
    std::string validator_id;
    // How many validators the committee holds. The threshold FOLLOWS from it,
    // so the two cannot be set to disagree.
    int committee = 0;
};

class Quasar {
  public:
    // Registration with the core is not optional and not deferred: a bridge
    // that cannot sign for its own identity does nothing at all — no signature
    // is ever recorded, so no peer signature finds a block to attach to, so the
    // quorum is never reached and finality is unreachable, silently.
    static Result<std::shared_ptr<Quasar>> make(Config cfg);

    // Signs a block with this node's key and records the signature. Signing the
    // same block again returns the signature already recorded: one validator
    // makes one statement about one block.
    Result<QuasarSig> sign_block(const Id& block_id, ByteView block_hash, std::uint64_t height);

    Status add_signature(const Id& block_id, const QuasarSig* sig);
    bool verify_signature(ByteView message, const QuasarSig* sig) const;
    bool verify_aggregate(ByteView message, const AggregatedSignature* agg) const;

    // Finalizes a block once the quorum's signatures aggregate into a signature
    // that verifies over it. Reaching the count is necessary and not
    // sufficient: the aggregate is built and checked.
    struct Finality {
        AggregatedSignature aggregate;
        bool finalized = false;
    };
    Result<Finality> try_finalize(const Id& block_id);

    bool is_finalized(const Id& block_id) const;
    Status add_validator(const std::string& validator_id, std::uint64_t weight);

    // Drops every block tracked below min_height. The height is the caller's
    // finalized frontier, so a block beneath it will never gather another
    // signature whether it finalized or not — keeping only the finalized ones
    // meant the entries that could not be cleaned up were exactly the ones that
    // accumulated.
    void cleanup(std::uint64_t min_height);

    int threshold() const { return core_->threshold(); }
    int committee() const { return committee_; }
    int active_validators() const;
    const std::string& validator_id() const { return validator_id_; }
    std::shared_ptr<Core> core() const { return core_; }

    // What the bridge is tracking. Exposed because "the node built a block and
    // recorded no signature for it" is a property worth asserting.
    bool tracking(const Id& block_id) const;
    std::size_t tracked() const;
    std::vector<QuasarSig> signatures_of(const Id& block_id) const;

  private:
    Quasar(std::shared_ptr<Core> core, std::string validator_id, int committee)
        : core_(std::move(core)), validator_id_(std::move(validator_id)), committee_(committee) {}

    // Admits one signature into a block's set. Caller holds mu_. It VERIFIES
    // before it counts, and files the signature under the identity that
    // verification authenticated.
    Status record(PendingBlock& p, const QuasarSig* sig);

    mutable std::mutex mu_;
    std::shared_ptr<Core> core_;
    std::string validator_id_;
    int committee_ = 0;

    // The committee roster this bridge registered. The core holds their keys;
    // this holds the membership, which is what makes the committee a set of a
    // declared size rather than whatever accumulated.
    std::set<std::string> members_;
    std::map<Id, PendingBlock> pending_;
    std::set<Id> finalized_;
};

}  // namespace lux::quantumvm::quasar
