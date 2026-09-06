// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/nullifier.hpp"

namespace lux::zkvm {
namespace {

Bytes be64(std::uint64_t v) {
    Bytes out(8);
    for (int i = 0; i < 8; ++i) out[std::size_t(i)] = std::uint8_t(v >> (8 * (7 - i)));
    return out;
}

std::uint64_t from_be64(ByteView b) {
    std::uint64_t v = 0;
    for (std::size_t i = 0; i < 8; ++i) v = (v << 8) | b[i];
    return v;
}

}  // namespace

Bytes make_nullifier_key(ByteView nullifier) {
    Bytes key;
    key.reserve(1 + nullifier.size());
    key.push_back(kNullifierPrefix);
    key.insert(key.end(), nullifier.begin(), nullifier.end());
    return key;
}

wire::Result<std::unique_ptr<NullifierDb>> NullifierDb::open(store::Store& db) {
    std::unique_ptr<NullifierDb> n(new NullifierDb(db));
    if (auto r = n->load(); !r) return std::unexpected(r.error());
    return n;
}

// load reads the whole set from the store. A record that is not eight bytes is
// not a height and belongs to some other writer under the same prefix; the read
// path agrees with this, or reading one of those is a crash where a miss was
// meant.
wire::Result<void> NullifierDb::load() {
    const Bytes prefix{kNullifierPrefix};
    return db_->each(view(prefix), [&](ByteView key, ByteView val) {
        if (key.size() < 2 || val.size() != 8) return true;
        spent_[bytes_of(key.subspan(1))] = from_be64(val);
        return true;
    });
}

wire::Result<void> NullifierDb::reload() {
    spent_.clear();
    return load();
}

wire::Result<void> NullifierDb::mark_spent(ByteView nullifier, std::uint64_t height) {
    // The LAST refusal in front of a note being spent twice, so it asks the same
    // question the read path asks — records included, and a failed read reported
    // as a failure rather than as "not spent".
    //
    // It used to consult the in-memory set alone, which is strictly weaker than
    // the read beside it: a set that is not a superset of the records answers
    // "unspent" for a note the records already hold, and this then OVERWRITES
    // that record with a new height and returns success. The note is spent
    // twice and nothing anywhere says so. Two answers to one question is one
    // answer too many; there is one now.
    auto spend = spent_at(nullifier);
    if (!spend) return std::unexpected(spend.error());
    if (spend->spent) return std::unexpected(kErrNullifierSpent);

    const Bytes record = be64(height);
    const Bytes db_key = make_nullifier_key(nullifier);
    if (auto r = db_->put(view(db_key), view(record)); !r) return r;

    spent_[bytes_of(nullifier)] = height;
    return {};
}

wire::Result<Spend> NullifierDb::spent_at(ByteView nullifier) const {
    auto it = spent_.find(nullifier);
    if (it != spent_.end()) return Spend{true, it->second};

    const Bytes db_key = make_nullifier_key(nullifier);
    auto raw = db_->get(view(db_key));
    if (!raw) return std::unexpected(raw.error());
    if (!raw->has_value()) return Spend{false, 0};
    if ((*raw)->size() != 8) return std::unexpected(kErrNotAHeight);
    return Spend{true, from_be64(view(**raw))};
}

}  // namespace lux::zkvm
