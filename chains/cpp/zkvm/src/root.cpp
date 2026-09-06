// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/root.hpp"

#include <cstring>

namespace lux::zkvm {
namespace {

ByteView key_view() {
    return ByteView(reinterpret_cast<const std::uint8_t*>(kRootKey), std::strlen(kRootKey));
}

}  // namespace

wire::Result<std::unique_ptr<Root>> Root::open(store::Store& db) {
    std::unique_ptr<Root> r(new Root(db));
    if (auto res = r->reload(); !res) return std::unexpected(res.error());
    return r;
}

Id Root::after(const std::vector<Transaction>& txs) const {
    Hasher h;
    h.write(view(committed_));
    for (const auto& tx : txs)
        for (const auto& o : tx.outputs) h.write(view(o.commitment));
    for (const auto& tx : txs)
        for (const auto& n : tx.nullifiers) h.write(view(n));
    return h.sum();
}

wire::Result<void> Root::finalize(const Id& next) {
    // The record first: a root held in memory that is not in the store is a root
    // this node alone believes.
    if (auto r = db_->put(key_view(), view(next)); !r) return r;
    committed_ = next;
    return {};
}

wire::Result<void> Root::reload() {
    auto raw = db_->get(key_view());
    if (!raw) return std::unexpected("zkvm: read state root: " + raw.error());
    if (!raw->has_value()) {
        committed_ = kEmptyId;
        return {};
    }
    if ((*raw)->size() != 32)
        return std::unexpected("zkvm: state root is " + std::to_string((*raw)->size()) +
                               " bytes, want 32");
    std::copy((*raw)->begin(), (*raw)->end(), committed_.begin());
    return {};
}

}  // namespace lux::zkvm
