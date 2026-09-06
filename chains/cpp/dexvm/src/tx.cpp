// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/tx.hpp"

#include <zap/zap.hpp>

#include <string>

namespace lux::dexvm {
namespace {

// The three objects' layouts. Every variable field is an 8-byte (relative
// pointer, length) pair; every scalar sits in the fixed head. The offsets are
// constants rather than a struct so the format is stated once and read the same
// way it is written.
constexpr int kAssetNetworkID = 0;   // u32
constexpr int kAssetKindOff = 4;     // u8
constexpr int kAssetDecimals = 5;    // u8
constexpr int kAssetEnabled = 6;     // u8
constexpr int kAssetRiskTier = 7;    // u8
constexpr int kAssetChainID = 8;     // bytes
constexpr int kAssetRef = 16;        // bytes
constexpr int kAssetSymbol = 24;     // bytes
constexpr int kAssetName = 32;       // bytes
constexpr int kAssetSize = 40;

constexpr int kMarketNetworkID = 0;  // u32
constexpr int kMarketEnabled = 4;    // u8
constexpr int kMarketBase = 8;       // bytes
constexpr int kMarketQuote = 16;     // bytes
constexpr int kMarketVenue = 24;     // bytes
constexpr int kMarketSize = 32;

constexpr int kTxKind = 0;           // u8
constexpr int kTxPayload = 8;        // object pointer
constexpr int kTxSize = 16;

constexpr int kBlockHeight = 0;      // u64
constexpr int kBlockParent = 8;      // bytes
constexpr int kBlockTxs = 16;        // list of object pointers
constexpr int kBlockSize = 24;

constexpr const char* kErrTx = "dexvm: transaction does not parse";
constexpr const char* kErrBlock = "dexvm: block does not parse";

int write_asset(zap::Builder& b, const Asset& a) {
    auto ob = b.start_object(kAssetSize);
    ob.set_u32(kAssetNetworkID, a.network_id);
    ob.set_u8(kAssetKindOff, std::uint8_t(a.kind));
    ob.set_u8(kAssetDecimals, a.decimals);
    ob.set_u8(kAssetEnabled, a.enabled ? 1 : 0);
    ob.set_u8(kAssetRiskTier, std::uint8_t(a.risk_tier));
    ob.set_bytes(kAssetChainID, view(a.chain_id));
    ob.set_bytes(kAssetRef, view(a.canonical_ref));
    ob.set_text(kAssetSymbol, a.symbol);
    ob.set_text(kAssetName, a.name);
    return ob.finish();
}

int write_market(zap::Builder& b, const Market& m) {
    auto ob = b.start_object(kMarketSize);
    ob.set_u32(kMarketNetworkID, m.network_id);
    ob.set_u8(kMarketEnabled, m.enabled ? 1 : 0);
    ob.set_bytes(kMarketBase, view(m.base_asset_id));
    ob.set_bytes(kMarketQuote, view(m.quote_asset_id));
    ob.set_bytes(kMarketVenue, view(m.venue_config));
    return ob.finish();
}

Result<Id> read_id(std::span<const std::uint8_t> b, const char* what) {
    if (b.size() != 32)
        return fail(std::string(what) + ": expected a 32-byte id, got " +
                    std::to_string(b.size()));
    Id id{};
    std::copy(b.begin(), b.end(), id.begin());
    return id;
}

std::string read_text(std::span<const std::uint8_t> b) {
    return std::string(reinterpret_cast<const char*>(b.data()), b.size());
}

Result<Asset> read_asset(const zap::Object& o) {
    Asset a;
    a.network_id = o.u32(kAssetNetworkID);
    a.kind = AssetKind(o.u8(kAssetKindOff));
    a.decimals = o.u8(kAssetDecimals);
    a.enabled = o.u8(kAssetEnabled) != 0;
    a.risk_tier = RiskTier(o.u8(kAssetRiskTier));
    auto chain = read_id(o.bytes(kAssetChainID), "asset chainID");
    if (!chain) return std::unexpected(chain.error());
    a.chain_id = *chain;
    const auto ref = o.bytes(kAssetRef);
    a.canonical_ref.assign(ref.begin(), ref.end());
    a.symbol = read_text(o.bytes(kAssetSymbol));
    a.name = read_text(o.bytes(kAssetName));
    return a;
}

Result<Market> read_market(const zap::Object& o) {
    Market m;
    m.network_id = o.u32(kMarketNetworkID);
    m.enabled = o.u8(kMarketEnabled) != 0;
    auto base = read_id(o.bytes(kMarketBase), "market baseAssetID");
    if (!base) return std::unexpected(base.error());
    m.base_asset_id = *base;
    auto quote = read_id(o.bytes(kMarketQuote), "market quoteAssetID");
    if (!quote) return std::unexpected(quote.error());
    m.quote_asset_id = *quote;
    const auto venue = o.bytes(kMarketVenue);
    m.venue_config.assign(venue.begin(), venue.end());
    return m;
}

Result<Tx> read_tx(const zap::Object& o) {
    Tx tx;
    tx.kind = TxKind(o.u8(kTxKind));
    const zap::Object payload = o.object(kTxPayload);
    if (payload.is_null()) return fail(std::string(kErrTx) + ": missing payload");
    switch (tx.kind) {
        case TxKind::RegisterAsset: {
            auto a = read_asset(payload);
            if (!a) return std::unexpected(a.error());
            tx.asset = std::move(*a);
            return tx;
        }
        case TxKind::CreateMarket: {
            auto m = read_market(payload);
            if (!m) return std::unexpected(m.error());
            tx.market = std::move(*m);
            return tx;
        }
        default:
            return fail(std::string(kErrTx) + ": unknown kind " +
                        std::to_string(std::uint32_t(o.u8(kTxKind))));
    }
}

}  // namespace

Tx Tx::register_asset(Asset a) {
    Tx tx;
    tx.kind = TxKind::RegisterAsset;
    tx.asset = std::move(a);
    return tx;
}

Tx Tx::create_market(Market m) {
    Tx tx;
    tx.kind = TxKind::CreateMarket;
    tx.market = std::move(m);
    return tx;
}

Bytes Tx::encode() const {
    zap::Builder b(256);
    int payload = 0;
    switch (kind) {
        case TxKind::RegisterAsset: payload = write_asset(b, asset); break;
        case TxKind::CreateMarket:  payload = write_market(b, market); break;
        default: break;
    }
    auto ob = b.start_object(kTxSize);
    ob.set_u8(kTxKind, std::uint8_t(kind));
    ob.set_object(kTxPayload, payload);
    ob.finish_as_root();
    return b.finish();
}

Id Tx::id() const {
    const Bytes b = encode();
    return sha256(view(b));
}

Result<Tx> decode_tx(ByteView b) {
    auto msg = zap::Message::parse(b);
    if (!msg) return fail(std::string(kErrTx) + ": " + std::string(zap::describe(msg.error())));
    return read_tx(msg->root());
}

Bytes BlockBody::encode() const {
    zap::Builder b(512 + int(txs.size()) * 256);
    std::vector<Bytes> tx_bytes;
    tx_bytes.reserve(txs.size());
    std::vector<int> positions;
    positions.reserve(txs.size());
    for (const Tx& t : txs) {
        int payload = 0;
        switch (t.kind) {
            case TxKind::RegisterAsset: payload = write_asset(b, t.asset); break;
            case TxKind::CreateMarket:  payload = write_market(b, t.market); break;
            default: break;
        }
        auto ob = b.start_object(kTxSize);
        ob.set_u8(kTxKind, std::uint8_t(t.kind));
        ob.set_object(kTxPayload, payload);
        positions.push_back(ob.finish());
    }
    auto lb = b.start_list(4);
    for (int p : positions) lb.add_object_ptr(p);
    const auto list = lb.finish();

    auto ob = b.start_object(kBlockSize);
    ob.set_u64(kBlockHeight, height);
    ob.set_bytes(kBlockParent, view(parent));
    ob.set_list(kBlockTxs, list.first, int(positions.size()));
    ob.finish_as_root();
    return b.finish();
}

Id BlockBody::id() const {
    const Bytes b = encode();
    return sha256(view(b));
}

Result<BlockBody> decode_block(ByteView b) {
    auto msg = zap::Message::parse(b);
    if (!msg) return fail(std::string(kErrBlock) + ": " + std::string(zap::describe(msg.error())));
    const zap::Object root = msg->root();

    BlockBody body;
    body.height = root.u64(kBlockHeight);
    auto parent = read_id(root.bytes(kBlockParent), "block parent");
    if (!parent) return std::unexpected(parent.error());
    body.parent = *parent;

    // The stride clamp rejects an attacker-set element count up front rather
    // than at every accessor: each element is a 4-byte pointer.
    const zap::List txs = root.list_stride(kBlockTxs, 4);
    for (int i = 0; i < txs.size(); ++i) {
        auto tx = read_tx(txs.object_ptr(i));
        if (!tx) return std::unexpected(tx.error().wrap("block tx[" + std::to_string(i) + "]"));
        body.txs.push_back(std::move(*tx));
    }
    return body;
}

}  // namespace lux::dexvm
