// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/chainstore.hpp"

#include <cstring>

namespace lux::zkvm {
namespace {

// Key namespaces. The decisions, the height index and the tip pointer are the
// store's own; a chain's records live under prefixes of its choosing and cannot
// collide with these.
constexpr const char* kTipKey = "chain/tip";
constexpr const char* kBlockPrefix = "chain/block/";
constexpr const char* kHeightPrefix = "chain/height/";

// as_view names the span helper explicitly here, because `view_` is the member
// this file spends its time in and the two must not be read as one thing.
inline ByteView as_view(const Bytes& b) { return ByteView(b.data(), b.size()); }
inline ByteView as_view(const Id& id) { return ByteView(id.data(), id.size()); }

Bytes str_bytes(const char* s) {
    const std::size_t n = std::strlen(s);
    return Bytes(reinterpret_cast<const std::uint8_t*>(s),
                 reinterpret_cast<const std::uint8_t*>(s) + n);
}

}  // namespace

Bytes ChainStore::block_key(const Id& id) {
    Bytes k = str_bytes(kBlockPrefix);
    k.insert(k.end(), id.begin(), id.end());
    return k;
}

Bytes ChainStore::height_key(std::uint64_t h) {
    Bytes k = str_bytes(kHeightPrefix);
    for (int i = 7; i >= 0; --i) k.push_back(std::uint8_t(h >> (8 * i)));
    return k;
}

wire::Result<bool> ChainStore::open(
    std::shared_ptr<Decision> genesis,
    const std::function<wire::Result<std::shared_ptr<Decision>>(ByteView)>& parse) {
    std::lock_guard<std::mutex> g(mu_);

    const Bytes tip_key = str_bytes(kTipKey);
    auto raw = view_.get(as_view(tip_key));
    if (!raw) return std::unexpected("chain: read tip: " + raw.error());

    if (!raw->has_value()) {
        tip_ = genesis->id();
        height_ = genesis->height();
        last_ = std::move(genesis);
        opened_ = true;
        return true;  // fresh
    }
    if ((*raw)->size() != 32)
        return std::unexpected("chain: tip is " + std::to_string((*raw)->size()) +
                               " bytes, want 32");

    Id id{};
    std::copy((*raw)->begin(), (*raw)->end(), id.begin());
    if (id == genesis->id()) {
        tip_ = id;
        height_ = genesis->height();
        last_ = std::move(genesis);
        opened_ = true;
        return false;
    }

    auto stored = view_.get(as_view(block_key(id)));
    if (!stored) return std::unexpected("chain: read tip block: " + stored.error());
    if (!stored->has_value())
        return std::unexpected(std::string(kErrNoBlock) + ": tip " + hex(id));
    auto d = parse(view(**stored));
    if (!d) return std::unexpected(d.error());
    tip_ = id;
    height_ = (*d)->height();
    last_ = *d;
    opened_ = true;
    return false;
}

wire::Result<void> ChainStore::undo(std::string cause) {
    view_.abort();
    if (reload_) {
        if (auto r = reload_(); !r) return std::unexpected(cause + "; " + r.error());
    }
    return std::unexpected(std::move(cause));
}

wire::Result<void> ChainStore::accept(const std::shared_ptr<Decision>& d) {
    std::lock_guard<std::mutex> g(mu_);

    const Id id = d->id();
    if (!opened_)
        return std::unexpected(std::string(kErrNotOpen) + ", so " + hex(id) + " extends nothing");
    if (d->parent() != tip_)
        return std::unexpected(std::string(kErrNotOnTip) + ": " + hex(id) + " extends " +
                               hex(d->parent()) + ", and the tip is " + hex(tip_));

    const ByteView raw = d->bytes();
    if (raw.empty()) return undo("chain: block " + hex(id) + " has no encoding");

    if (auto r = d->write(view_); !r) return undo(r.error());
    if (auto r = view_.put(as_view(block_key(id)), raw); !r) return undo(r.error());
    if (auto r = view_.put(as_view(height_key(d->height())), view(id)); !r) return undo(r.error());
    if (auto r = view_.put(as_view(str_bytes(kTipKey)), view(id)); !r) return undo(r.error());
    if (auto r = view_.commit(); !r)
        return undo("chain: commit block " + hex(id) + ": " + r.error());

    tip_ = id;
    height_ = d->height();
    last_ = d;
    flight_.erase(id);
    prune_locked();
    // The decision's own effects become visible LAST, after the commit, so there
    // is no window in which the chain has advanced past state that is not on
    // disk.
    d->publish();
    return {};
}

wire::Result<void> ChainStore::seed(
    const std::function<wire::Result<void>(store::Store&)>& write) {
    std::lock_guard<std::mutex> g(mu_);
    if (!opened_)
        return std::unexpected(std::string(kErrNotOpen) + ", so there is no genesis to seed");

    if (auto r = write(view_); !r) return undo(r.error());
    if (auto r = view_.put(as_view(str_bytes(kTipKey)), view(tip_)); !r) return undo(r.error());
    if (auto r = view_.commit(); !r) return undo("chain: commit genesis: " + r.error());
    return {};
}

void ChainStore::prefer(const Id& id) {
    std::lock_guard<std::mutex> g(mu_);
    preferred_ = id;
}

wire::Result<std::shared_ptr<Decision>> ChainStore::propose(
    const std::function<wire::Result<std::shared_ptr<Decision>>(const Decision&)>& build) {
    std::lock_guard<std::mutex> g(mu_);
    if (!opened_) return std::unexpected(kErrNotOpen);

    const Decision* parent = last_.get();
    auto it = flight_.find(preferred_);
    if (it != flight_.end()) parent = it->second.get();

    auto d = build(*parent);
    if (!d) return std::unexpected(d.error());
    track_locked(*d);
    return *d;
}

void ChainStore::track(const std::shared_ptr<Decision>& d) {
    std::lock_guard<std::mutex> g(mu_);
    track_locked(d);
}

void ChainStore::track_locked(const std::shared_ptr<Decision>& d) {
    if (d->height() <= height_) return;
    prune_locked();
    flight_[d->id()] = d;
}

// prune drops every decision in flight at or below the accepted height. Nothing
// else will: the engine may abandon one without ever accepting or rejecting it,
// so a set that only grew would leak.
void ChainStore::prune_locked() {
    for (auto it = flight_.begin(); it != flight_.end();) {
        if (it->second->height() <= height_) {
            it = flight_.erase(it);
        } else {
            ++it;
        }
    }
}

void ChainStore::drop(const Id& id) {
    std::lock_guard<std::mutex> g(mu_);
    flight_.erase(id);
}

Id ChainStore::tip() const {
    std::lock_guard<std::mutex> g(mu_);
    return tip_;
}

std::uint64_t ChainStore::height() const {
    std::lock_guard<std::mutex> g(mu_);
    return height_;
}

std::shared_ptr<Decision> ChainStore::last() const {
    std::lock_guard<std::mutex> g(mu_);
    return last_;
}

std::size_t ChainStore::in_flight() const {
    std::lock_guard<std::mutex> g(mu_);
    return flight_.size();
}

wire::Result<std::shared_ptr<Decision>> ChainStore::block(
    const Id& id,
    const std::function<wire::Result<std::shared_ptr<Decision>>(ByteView)>& parse) const {
    std::lock_guard<std::mutex> g(mu_);
    auto it = flight_.find(id);
    if (it != flight_.end()) return it->second;
    if (id == tip_ && last_) return last_;

    auto raw = view_.get(as_view(block_key(id)));
    if (!raw || !raw->has_value())
        return std::unexpected(std::string(kErrNoBlock) + ": " + hex(id));
    return parse(view(**raw));
}

wire::Result<Id> ChainStore::id_at_height(std::uint64_t h) const {
    std::lock_guard<std::mutex> g(mu_);
    auto raw = view_.get(as_view(height_key(h)));
    if (!raw || !raw->has_value())
        return std::unexpected(std::string(kErrNoBlock) + ": height " + std::to_string(h));
    if ((*raw)->size() != 32) return std::unexpected("chain: height index is not an id");
    Id id{};
    std::copy((*raw)->begin(), (*raw)->end(), id.begin());
    return id;
}

bool ChainStore::accepted(const Id& id) const {
    std::lock_guard<std::mutex> g(mu_);
    if (id == tip_) return true;
    auto raw = view_.get(as_view(block_key(id)));
    return raw.has_value() && raw->has_value();
}

}  // namespace lux::zkvm
