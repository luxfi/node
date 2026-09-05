// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/utxo.hpp"

#include "lux/zkvm/zap.hpp"

namespace lux::zkvm {
namespace {

// UTXO: TxID 32B@0, OutputIndex u32@32, Height u64@36, Commitment bytes@44,
//       Ciphertext bytes@52, EphemeralPK bytes@60
constexpr int kTxId = 0, kIndex = 32, kHeight = 36, kCommit = 44, kCipher = 52, kEpk = 60;

}  // namespace

Bytes Utxo::marshal() const {
    zap::Builder b(zap::kHeaderSize + kUtxoSize + int(commitment.size()) +
                   int(ciphertext.size()) + int(ephemeral_pk.size()) + 64);
    auto ob = b.start_object(kUtxoSize);
    ob.set_bytes_fixed(kTxId, view(tx_id));
    ob.set_u32(kIndex, output_index);
    ob.set_u64(kHeight, height);
    ob.set_bytes(kCommit, view(commitment));
    ob.set_bytes(kCipher, view(ciphertext));
    ob.set_bytes(kEpk, view(ephemeral_pk));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<Utxo> parse_utxo(ByteView data) {
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());
    Utxo u;
    u.tx_id = wire::read_id(*o, kTxId);
    u.output_index = o->u32(kIndex);
    u.height = o->u64(kHeight);
    u.commitment = wire::cp(o->bytes(kCommit));
    u.ciphertext = wire::cp(o->bytes(kCipher));
    u.ephemeral_pk = wire::cp(o->bytes(kEpk));
    return u;
}

Bytes make_utxo_key(ByteView commitment) {
    Bytes key;
    key.reserve(1 + commitment.size());
    key.push_back(kUtxoPrefix);
    key.insert(key.end(), commitment.begin(), commitment.end());
    return key;
}

wire::Result<std::unique_ptr<UtxoDb>> UtxoDb::open(store::Store& db) {
    std::unique_ptr<UtxoDb> u(new UtxoDb(db));
    if (auto r = u->load(); !r) return std::unexpected(r.error());
    return u;
}

// load rebuilds the set from the records.
//
// The records share a keyspace with other writers, so a key carrying the UTXO
// prefix counts as a UTXO only if the record under it names the commitment its
// own key is made of. Anything else belongs to someone else and is left alone.
wire::Result<void> UtxoDb::load() {
    const Bytes prefix{kUtxoPrefix};
    return db_->each(view(prefix), [&](ByteView key, ByteView val) {
        if (key.size() < 2) return true;
        const ByteView commitment = key.subspan(1);
        auto u = parse_utxo(val);
        if (!u) return true;
        if (u->commitment.size() != commitment.size() ||
            !std::equal(u->commitment.begin(), u->commitment.end(), commitment.begin()))
            return true;
        set_[bytes_of(commitment)] = u->height;
        return true;
    });
}

wire::Result<void> UtxoDb::reload() {
    set_.clear();
    return load();
}

wire::Result<void> UtxoDb::add(const Utxo& u) {
    if (set_.count(u.commitment)) return std::unexpected(kErrUtxoExists);

    const Bytes key = make_utxo_key(view(u.commitment));
    const Bytes record = u.marshal();
    if (auto r = db_->put(view(key), view(record)); !r) return r;

    set_[u.commitment] = u.height;
    return {};
}

// get reads the body from the records every time. Memoising it here would make
// a read a write, and the set is what every other reader is promised is not
// changing under them.
wire::Result<Utxo> UtxoDb::get(ByteView commitment) const {
    const Bytes key = make_utxo_key(commitment);
    auto raw = db_->get(view(key));
    // A read that FAILED is not a UTXO that is absent. Reported as absent it
    // says a note was never created, rather than that the disk is gone.
    if (!raw) return std::unexpected("zkvm: read utxo: " + raw.error());
    if (!raw->has_value()) return std::unexpected(kErrNoUtxo);
    return parse_utxo(view(**raw));
}

}  // namespace lux::zkvm
