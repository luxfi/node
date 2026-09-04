// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/genesis.hpp"

#include "lux/xvm/zap.hpp"

namespace lux::xvm::genesis {
namespace {

// The genesis object's fixed section — the same offsets the Go definition
// (genesis_wire.go) writes, because the buffer a host hands this VM was written
// by that code.
constexpr int kOffCount = 0;       // u32
constexpr int kOffAliasLens = 4;   // u32 list ptr
constexpr int kOffAliasBlob = 12;  // bytes ptr
constexpr int kOffTxLens = 20;     // u32 list ptr
constexpr int kOffTxBlob = 28;     // bytes ptr
constexpr int kSize = 36;

constexpr std::uint32_t kU32Stride = 4;

int write_u32_list(zap::Builder& b, const std::vector<std::uint32_t>& xs) {
    auto lb = b.start_list(int(kU32Stride));
    for (std::uint32_t x : xs) lb.add_u32(x);
    return lb.finish().first;
}

std::vector<std::uint32_t> read_u32_list(const zap::Object& o, int ptr_off) {
    auto l = o.list_stride(ptr_off, kU32Stride);
    std::vector<std::uint32_t> out(std::size_t(l.len()));
    for (int i = 0; i < l.len(); ++i) out[std::size_t(i)] = l.u32(i);
    return out;
}

// signed_tx wraps a genesis CreateAssetTx into the signed envelope its id is the
// hash of. A genesis asset has no inputs, so it has no credentials — the
// envelope is empty of them, which is what makes the id derivable from the
// genesis bytes alone.
Result<std::shared_ptr<txs::Tx>> signed_tx(const Asset& a) {
    if (a.create == nullptr) return std::unexpected(kErrNotCreateAsset);
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = a.create;
    if (auto r = tx->initialize(); !r) return std::unexpected(r.error());
    return tx;
}

}  // namespace

Result<Id> Asset::id() const {
    auto tx = signed_tx(*this);
    if (!tx) return std::unexpected(tx.error());
    return (*tx)->id();
}

Result<Bytes> bytes(const Genesis& g) {
    std::vector<std::uint32_t> alias_lens, tx_lens;
    Bytes alias_blob, tx_blob;
    alias_lens.reserve(g.assets.size());
    tx_lens.reserve(g.assets.size());

    for (std::size_t i = 0; i < g.assets.size(); ++i) {
        const Asset& a = g.assets[i];
        if (a.create == nullptr)
            return std::unexpected("marshal genesis asset " + std::to_string(i) + " (" + a.alias +
                                   "): " + kErrNotCreateAsset);
        // The SIGNING TARGET is what is stored, not the signed envelope: a
        // genesis asset is never signed, and its id follows from these bytes.
        const Bytes& ub = a.create->bytes();
        alias_lens.push_back(std::uint32_t(a.alias.size()));
        alias_blob.insert(alias_blob.end(), a.alias.begin(), a.alias.end());
        tx_lens.push_back(std::uint32_t(ub.size()));
        tx_blob.insert(tx_blob.end(), ub.begin(), ub.end());
    }

    zap::Builder b(zap::kHeaderSize + kSize + int(alias_blob.size()) + int(tx_blob.size()) +
                   8 * int(g.assets.size()) + 64);
    const int alias_lens_off = write_u32_list(b, alias_lens);
    const int tx_lens_off = write_u32_list(b, tx_lens);

    auto ob = b.start_object(kSize);
    ob.set_u32(kOffCount, std::uint32_t(g.assets.size()));
    ob.set_list(kOffAliasLens, alias_lens_off, int(alias_lens.size()));
    ob.set_bytes(kOffAliasBlob, view(alias_blob));
    ob.set_list(kOffTxLens, tx_lens_off, int(tx_lens.size()));
    ob.set_bytes(kOffTxBlob, view(tx_blob));
    ob.finish_as_root();
    return b.finish();
}

Result<Genesis> parse(ByteView genesis_bytes) {
    zap::Message msg;
    std::string err;
    if (!zap::Message::parse(genesis_bytes, &msg, &err))
        return std::unexpected("parse xvm genesis: " + err);
    // A buffer longer than the message it declares parses the same and hashes
    // differently. That is a malleability surface, so it is refused rather than
    // tolerated.
    if (msg.size() != genesis_bytes.size())
        return std::unexpected(std::string("parse xvm genesis: ") + kErrTrailingBytes);

    const zap::Object root = msg.root();
    const std::size_t n = std::size_t(root.u32(kOffCount));
    const auto alias_lens = read_u32_list(root, kOffAliasLens);
    const auto alias_blob = root.bytes(kOffAliasBlob);
    const auto tx_lens = read_u32_list(root, kOffTxLens);
    const auto tx_blob = root.bytes(kOffTxBlob);

    if (alias_lens.size() != n || tx_lens.size() != n)
        return std::unexpected(std::string("parse xvm genesis: ") + kErrCountMismatch);

    Genesis g;
    g.assets.reserve(n);
    std::size_t a_pos = 0, t_pos = 0;
    for (std::size_t i = 0; i < n; ++i) {
        const std::size_t a_len = alias_lens[i];
        const std::size_t t_len = tx_lens[i];
        if (a_pos + a_len > alias_blob.size() || t_pos + t_len > tx_blob.size())
            return std::unexpected("parse xvm genesis: asset " + std::to_string(i) +
                                   " out of bounds");

        std::string alias(reinterpret_cast<const char*>(alias_blob.data() + a_pos), a_len);
        a_pos += a_len;
        ByteView ub = tx_blob.subspan(t_pos, t_len);
        t_pos += t_len;

        auto unsigned_tx = txs::parse_unsigned(ub);
        if (!unsigned_tx)
            return std::unexpected("hydrate genesis asset " + std::to_string(i) + " (" + alias +
                                   "): " + unsigned_tx.error());
        auto create = std::dynamic_pointer_cast<txs::CreateAssetTx>(*unsigned_tx);
        if (create == nullptr)
            return std::unexpected("genesis asset " + std::to_string(i) + " (" + alias +
                                   "): " + kErrNotCreateAsset);
        g.assets.push_back(Asset{std::move(alias), std::move(create)});
    }
    return g;
}

Result<std::vector<std::shared_ptr<txs::Tx>>> as_txs(const Genesis& g) {
    std::vector<std::shared_ptr<txs::Tx>> out;
    out.reserve(g.assets.size());
    for (std::size_t i = 0; i < g.assets.size(); ++i) {
        auto tx = signed_tx(g.assets[i]);
        if (!tx)
            return std::unexpected("initialize genesis asset " + std::to_string(i) + " (" +
                                   g.assets[i].alias + "): " + tx.error());
        out.push_back(*tx);
    }
    return out;
}

Result<Id> fee_asset_id(ByteView genesis_bytes) {
    auto g = parse(genesis_bytes);
    if (!g) return std::unexpected(g.error());
    if (g->assets.empty()) return std::unexpected(kErrNoAssets);
    return g->assets[0].id();
}

}  // namespace lux::xvm::genesis
