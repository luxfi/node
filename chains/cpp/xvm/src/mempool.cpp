// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/mempool.hpp"

#include <bit>
#include <cmath>
#include <cstring>
#include <limits>
#include <random>

namespace lux::xvm::mempool {
namespace {

std::string with_id(const char* err, const Id& tx_id) {
    return std::string(err) + ": " + hex(tx_id);
}

constexpr double kLn2 = 0.69314718055994530942;
constexpr double kLn2Squared = kLn2 * kLn2;

}  // namespace

// ================= the pool's four refusals, in this chain's words =========

std::string refused(Refusal r, const Id& tx_id) {
    switch (r) {
        case Refusal::Duplicate: return with_id(kErrDuplicateTx, tx_id);
        case Refusal::TooLarge: return with_id(kErrTxTooLarge, tx_id);
        case Refusal::Full: return with_id(kErrPoolFull, tx_id);
        case Refusal::Conflict: return with_id(kErrConflictsWithOtherTx, tx_id);
    }
    return with_id("refused", tx_id);
}

// ================= Bloom =================

void random_fill(std::span<std::uint8_t> out) {
    static thread_local std::random_device rd;
    std::size_t i = 0;
    while (i < out.size()) {
        const std::uint32_t r = rd();
        for (int b = 0; b < 4 && i < out.size(); ++b, ++i)
            out[i] = std::uint8_t(r >> (8 * b));
    }
}

namespace {

// The filter's hash: the first eight bytes of sha256(key ‖ salt), big-endian.
std::uint64_t bloom_hash(const Id& key, const Id& salt) {
    Bytes buf;
    buf.reserve(key.size() + salt.size());
    buf.insert(buf.end(), key.begin(), key.end());
    buf.insert(buf.end(), salt.begin(), salt.end());
    const Id h = sha256(view(buf));
    std::uint64_t v = 0;
    for (int i = 0; i < 8; ++i) v = (v << 8) | h[std::size_t(i)];
    return v;
}

}  // namespace

Bloom::Bloom(int min_target_elements, double target_false_positive,
             double reset_false_positive, Fill fill)
    : min_target_elements_(min_target_elements),
      target_false_positive_(target_false_positive),
      reset_false_positive_(reset_false_positive),
      fill_(fill ? std::move(fill) : Fill(&random_fill)) {
    reset(std::size_t(min_target_elements < 0 ? 0 : min_target_elements));
}

std::size_t Bloom::optimal_entries(std::size_t count, double false_positive) {
    if (count == 0) return std::size_t(kMinEntries);
    if (false_positive >= 1) return std::size_t(kMinEntries);
    if (false_positive <= 0) return std::size_t(std::numeric_limits<int>::max());
    const double bits = -double(count) * std::log(false_positive) / kLn2Squared;
    const double entries = (bits + 8 - 1) / 8;
    if (entries >= double(std::numeric_limits<int>::max()))
        return std::size_t(std::numeric_limits<int>::max());
    const auto e = std::size_t(entries);
    return e < std::size_t(kMinEntries) ? std::size_t(kMinEntries) : e;
}

int Bloom::optimal_hashes(std::size_t num_entries, std::size_t count) {
    if (num_entries < std::size_t(kMinEntries)) return kMinHashes;
    if (count == 0) return kMaxHashes;
    const double n = std::ceil(double(num_entries) * 8 * kLn2 / double(count));
    if (n >= double(kMaxHashes)) return kMaxHashes;
    const int h = int(n);
    return h < kMinHashes ? kMinHashes : h;
}

std::size_t Bloom::estimate_count(int num_hashes, std::size_t num_entries,
                                  double false_positive) {
    if (num_hashes < kMinHashes) return 0;
    if (num_entries < std::size_t(kMinEntries)) return 0;
    if (false_positive <= 0) return 0;
    if (false_positive >= 1) return std::size_t(std::numeric_limits<int>::max());
    const double inv = 1.0 / double(num_hashes);
    const double bits = double(num_entries * 8);
    const double exp = 1 - std::pow(false_positive, inv);
    const double count = std::ceil(-std::log(exp) * bits * inv);
    if (count >= double(std::numeric_limits<int>::max()))
        return std::size_t(std::numeric_limits<int>::max());
    if (count <= 0) return 0;
    return std::size_t(count);
}

void Bloom::reset(std::size_t target_elements) {
    const std::size_t entries = optimal_entries(target_elements, target_false_positive_);
    const int hashes = optimal_hashes(entries, target_elements);

    seeds_.assign(std::size_t(hashes), 0);
    Bytes seed_bytes(std::size_t(hashes) * 8, 0);
    fill_(std::span<std::uint8_t>(seed_bytes.data(), seed_bytes.size()));
    for (std::size_t i = 0; i < seeds_.size(); ++i) {
        std::uint64_t v = 0;
        for (int b = 0; b < 8; ++b) v = (v << 8) | seed_bytes[i * 8 + std::size_t(b)];
        seeds_[i] = v;
    }
    entries_.assign(entries, 0);
    fill_(std::span<std::uint8_t>(salt_.data(), salt_.size()));
    max_count_ = estimate_count(hashes, entries, reset_false_positive_);
    count_ = 0;
}

void Bloom::add(const Id& gossip_id) {
    std::uint64_t h = bloom_hash(gossip_id, salt_);
    const std::uint64_t num_bits = std::uint64_t(entries_.size()) * 8;
    for (std::uint64_t seed : seeds_) {
        h = std::rotl(h, kHashRotation) ^ seed;
        const std::uint64_t index = h % num_bits;
        entries_[std::size_t(index / 8)] |= std::uint8_t(1u << (index % 8));
    }
    ++count_;
}

bool Bloom::has(const Id& gossip_id) const {
    std::uint64_t h = bloom_hash(gossip_id, salt_);
    const std::uint64_t num_bits = std::uint64_t(entries_.size()) * 8;
    std::uint8_t acc = 1;
    for (std::size_t i = 0; i < seeds_.size() && acc != 0; ++i) {
        h = std::rotl(h, kHashRotation) ^ seeds_[i];
        const std::uint64_t index = h % num_bits;
        acc = std::uint8_t(acc & (entries_[std::size_t(index / 8)] >> (index % 8)));
    }
    return acc != 0;
}

bool Bloom::reset_if_needed(std::size_t target_elements) {
    if (count_ <= max_count_) return false;
    const auto floor = std::size_t(min_target_elements_ < 0 ? 0 : min_target_elements_);
    reset(target_elements < floor ? floor : target_elements);
    return true;
}

Bytes Bloom::marshal() const {
    const std::size_t n = seeds_.size();
    Bytes out(1 + n * 8 + entries_.size(), 0);
    out[0] = std::uint8_t(n);
    for (std::size_t i = 0; i < n; ++i) {
        for (int b = 0; b < 8; ++b)
            out[1 + i * 8 + std::size_t(b)] = std::uint8_t(seeds_[i] >> (8 * (7 - b)));
    }
    std::memcpy(out.data() + 1 + n * 8, entries_.data(), entries_.size());
    return out;
}

// ================= Gossip =================

Gossip::Gossip(Pool& pool, Verifier& verifier, BloomParams params, Bloom::Fill fill)
    : pool_(&pool),
      verifier_(&verifier),
      bloom_(params.min_target_elements, params.target_false_positive,
             params.reset_false_positive, std::move(fill)) {}

Result<void> Gossip::add(TxPtr tx) {
    if (tx == nullptr) return std::unexpected(txs::kErrNilTx);
    const Id tx_id = tx->id();

    // 1. Already held.
    if (pool_->get(tx_id) != nullptr)
        return std::unexpected("attempted to issue " + with_id(kErrDuplicateTx, tx_id));

    // 2. Already refused. The ORIGINAL reason is returned: a second opinion
    //    would cost a second verification and could not be a better one.
    if (auto reason = dropped_.why(tx_id); reason.has_value())
        return std::unexpected(*reason);

    // 3. This node's own execution, against its last accepted state.
    if (auto r = verifier_->verify_tx(*tx); !r) {
        remember(tx_id, r.error());
        return std::unexpected(r.error());
    }

    return add_unverified(std::move(tx));
}

Result<void> Gossip::add_unverified(TxPtr tx) {
    if (tx == nullptr) return std::unexpected(txs::kErrNilTx);
    const Id tx_id = tx->id();

    // 4. Structural admission.
    if (auto r = pool_->add(tx); !r) {
        const std::string why = refused(r.error(), tx_id);
        // "The pool is full" is a fact about the pool, not about the
        // transaction, so remembering it would refuse a perfectly good one once
        // space freed up.
        if (r.error() != Refusal::Full) remember(tx_id, why);
        return std::unexpected(why);
    }
    dropped_.forget(tx_id);

    bloom_.add(tx_id);
    if (bloom_.reset_if_needed(pool_->size() * std::size_t(kBloomChurnMultiplier))) {
        // A rebuilt filter is empty; what the pool still holds goes back in, or
        // a peer would be told this node has nothing.
        pool_->each([&](const TxPtr& held) {
            bloom_.add(held->id());
            return true;
        });
    }
    return {};
}

}  // namespace lux::xvm::mempool
