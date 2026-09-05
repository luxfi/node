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

// ================= Pool =================

void Pool::erase_at(const Id& tx_id, Order::iterator it) {
    bytes_available_ += (*it)->size();
    order_.erase(it);
    index_.erase(tx_id);
    auto c = consumed_.find(tx_id);
    if (c != consumed_.end()) {
        for (const auto& in : c->second) consumer_.erase(in);
        consumed_.erase(c);
    }
}

Result<void> Pool::add(TxPtr tx) {
    if (tx == nullptr) return std::unexpected(txs::kErrNilTx);
    const Id tx_id = tx->id();

    if (index_.count(tx_id) != 0) return std::unexpected(with_id(kErrDuplicateTx, tx_id));

    const std::size_t size = tx->size();
    if (size > kMaxTxSize) return std::unexpected(with_id(kErrTxTooLarge, tx_id));
    if (size > bytes_available_) return std::unexpected(with_id(kErrPoolFull, tx_id));

    // An input already spoken for by a pooled tx makes this one unplaceable: a
    // block cannot hold both, so holding both would only make the builder
    // discover the conflict later, once per round.
    const std::set<Id> inputs = tx->input_ids();
    for (const auto& in : inputs) {
        if (consumer_.count(in) != 0)
            return std::unexpected(with_id(kErrConflictsWithOtherTx, tx_id));
    }

    bytes_available_ -= size;
    order_.push_back(std::move(tx));
    index_[tx_id] = std::prev(order_.end());
    for (const auto& in : inputs) consumer_[in] = tx_id;
    consumed_[tx_id] = inputs;

    // A tx that is IN the pool is not a dropped tx.
    auto d = dropped_.find(tx_id);
    if (d != dropped_.end()) {
        dropped_order_.erase(d->second.second);
        dropped_.erase(d);
    }
    return {};
}

Pool::TxPtr Pool::get(const Id& tx_id) const {
    auto it = index_.find(tx_id);
    if (it == index_.end()) return nullptr;
    return *it->second;
}

void Pool::remove(const std::vector<TxPtr>& list) {
    for (const auto& tx : list) {
        if (tx == nullptr) continue;
        const Id tx_id = tx->id();
        auto it = index_.find(tx_id);
        if (it != index_.end()) {
            erase_at(tx_id, it->second);
            continue;
        }
        // Not pooled: drop whatever IS pooled that spends one of its inputs.
        for (const auto& in : tx->input_ids()) {
            auto c = consumer_.find(in);
            if (c == consumer_.end()) continue;
            const Id other = c->second;
            auto o = index_.find(other);
            if (o != index_.end()) {
                erase_at(other, o->second);
            } else {
                // A consumer with no pooled tx cannot happen; if it somehow
                // does, the index is what would leak, so it is cleared here.
                consumed_.erase(other);
                consumer_.erase(in);
            }
        }
    }
}

Pool::TxPtr Pool::peek() const {
    if (order_.empty()) return nullptr;
    return order_.front();
}

void Pool::each(const std::function<bool(const TxPtr&)>& f) const {
    for (const auto& tx : order_) {
        if (!f(tx)) return;
    }
}

void Pool::mark_dropped(const Id& tx_id, const std::string& reason) {
    // "The pool is full" is a fact about the pool, not about the transaction,
    // so remembering it would refuse a perfectly good tx once space freed up.
    if (reason.find(kErrPoolFull) != std::string::npos) return;
    if (index_.count(tx_id) != 0) return;

    auto it = dropped_.find(tx_id);
    if (it != dropped_.end()) {
        dropped_order_.erase(it->second.second);
        dropped_.erase(it);
    }
    dropped_order_.push_back(tx_id);
    dropped_[tx_id] = {reason, std::prev(dropped_order_.end())};
    while (dropped_order_.size() > kDroppedCacheSize) {
        dropped_.erase(dropped_order_.front());
        dropped_order_.pop_front();
    }
}

std::string Pool::drop_reason(const Id& tx_id) const {
    auto it = dropped_.find(tx_id);
    if (it == dropped_.end()) return {};
    return it->second.first;
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
    if (auto reason = pool_->drop_reason(tx_id); !reason.empty())
        return std::unexpected(reason);

    // 3. This node's own execution, against its last accepted state.
    if (auto r = verifier_->verify_tx(*tx); !r) {
        pool_->mark_dropped(tx_id, r.error());
        return std::unexpected(r.error());
    }

    return add_unverified(std::move(tx));
}

Result<void> Gossip::add_unverified(TxPtr tx) {
    if (tx == nullptr) return std::unexpected(txs::kErrNilTx);
    const Id tx_id = tx->id();

    // 4. Structural admission.
    if (auto r = pool_->add(tx); !r) {
        pool_->mark_dropped(tx_id, r.error());
        return std::unexpected(r.error());
    }

    bloom_.add(tx_id);
    if (bloom_.reset_if_needed(pool_->len() * std::size_t(kBloomChurnMultiplier))) {
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
