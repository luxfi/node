// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/manifest.hpp"

#include "lux/dexvm/json.hpp"

#include <algorithm>
#include <array>
#include <cstdio>
#include <memory>
#include <set>

namespace lux::dexvm {
namespace {

// reject_unknown_fields is Go's DisallowUnknownFields, applied at every object a
// struct is decoded from. A typo'd field must be visible: a manifest that quietly
// dropped "asssets" would admit nothing and claim success.
Result<void> reject_unknown_fields(const json::Object& o, std::string_view what,
                                   std::span<const std::string_view> known) {
    for (const auto& [k, _] : o) {
        if (std::find(known.begin(), known.end(), k) == known.end())
            return fail("json: unknown field \"" + k + "\" in " + std::string(what));
    }
    return {};
}

const json::Value* member(const json::Object& o, std::string_view k) {
    auto it = o.find(std::string(k));
    if (it == o.end() || it->second.is_null()) return nullptr;
    return &it->second;
}

Result<std::uint64_t> u64_member(const json::Object& o, std::string_view k, std::uint64_t max) {
    const json::Value* v = member(o, k);
    if (!v) return std::uint64_t(0);
    auto n = v->as_u64(max);
    if (!n) return std::unexpected(n.error().wrap("field \"" + std::string(k) + "\""));
    return *n;
}

Result<std::string> string_member(const json::Object& o, std::string_view k) {
    const json::Value* v = member(o, k);
    if (!v) return std::string();
    auto s = v->as_string();
    if (!s) return std::unexpected(s.error().wrap("field \"" + std::string(k) + "\""));
    return *s;
}

Result<bool> bool_member(const json::Object& o, std::string_view k) {
    const json::Value* v = member(o, k);
    if (!v) return false;
    auto b = v->as_bool();
    if (!b) return std::unexpected(b.error().wrap("field \"" + std::string(k) + "\""));
    return *b;
}

Result<Id> id_member(const json::Object& o, std::string_view k) {
    const json::Value* v = member(o, k);
    if (!v) return kEmptyId;
    auto s = v->as_string();
    if (!s) return std::unexpected(s.error().wrap("field \"" + std::string(k) + "\""));
    auto id = id_from_string(*s);
    if (!id) return std::unexpected(id.error().wrap("field \"" + std::string(k) + "\""));
    return *id;
}

Result<Bytes> hex_member(const json::Object& o, std::string_view k) {
    const json::Value* v = member(o, k);
    if (!v) return Bytes{};
    auto s = v->as_string();
    if (!s) return std::unexpected(s.error().wrap("field \"" + std::string(k) + "\""));
    auto b = from_hex(*s);
    if (!b) return std::unexpected(b.error().wrap("field \"" + std::string(k) + "\""));
    return *b;
}

constexpr std::array<std::string_view, 9> kAssetFields{
    "networkID", "chainID", "assetKind", "canonicalRef", "decimals",
    "symbol",    "name",    "enabled",   "riskTier"};
constexpr std::array<std::string_view, 5> kMarketFields{"networkID", "baseAssetID", "quoteAssetID",
                                                        "venueConfig", "enabled"};
constexpr std::array<std::string_view, 7> kManifestFields{
    "network", "networkID", "evmChainID", "cChainID", "chainLabels", "assets", "markets"};

Result<Asset> decode_asset(const json::Value& v) {
    auto obj = v.as_object();
    if (!obj) return std::unexpected(obj.error());
    const json::Object& o = **obj;
    if (auto r = reject_unknown_fields(o, "registry.Asset", kAssetFields); !r)
        return std::unexpected(r.error());

    Asset a;
    auto net = u64_member(o, "networkID", 0xFFFFFFFFull);
    if (!net) return std::unexpected(net.error());
    a.network_id = std::uint32_t(*net);

    auto chain = id_member(o, "chainID");
    if (!chain) return std::unexpected(chain.error());
    a.chain_id = *chain;

    // assetKind decodes through the same closed-token parser the wire uses, so
    // an ASCII ticker in the kind slot fails here rather than downstream.
    if (const json::Value* kv = member(o, "assetKind")) {
        auto s = kv->as_string();
        if (!s) return std::unexpected(s.error().wrap("field \"assetKind\""));
        auto k = parse_kind(*s);
        if (!k) return std::unexpected(k.error());
        a.kind = *k;
    }

    auto ref = hex_member(o, "canonicalRef");
    if (!ref) return std::unexpected(ref.error());
    a.canonical_ref = *ref;

    auto dec = u64_member(o, "decimals", 0xFFull);
    if (!dec) return std::unexpected(dec.error());
    a.decimals = std::uint8_t(*dec);

    auto sym = string_member(o, "symbol");
    if (!sym) return std::unexpected(sym.error());
    a.symbol = *sym;

    auto nm = string_member(o, "name");
    if (!nm) return std::unexpected(nm.error());
    a.name = *nm;

    auto en = bool_member(o, "enabled");
    if (!en) return std::unexpected(en.error());
    a.enabled = *en;

    auto tier = u64_member(o, "riskTier", 0xFFull);
    if (!tier) return std::unexpected(tier.error());
    a.risk_tier = RiskTier(std::uint8_t(*tier));

    return a;
}

Result<Market> decode_market(const json::Value& v) {
    auto obj = v.as_object();
    if (!obj) return std::unexpected(obj.error());
    const json::Object& o = **obj;
    if (auto r = reject_unknown_fields(o, "registry.Market", kMarketFields); !r)
        return std::unexpected(r.error());

    Market m;
    auto net = u64_member(o, "networkID", 0xFFFFFFFFull);
    if (!net) return std::unexpected(net.error());
    m.network_id = std::uint32_t(*net);

    auto base = id_member(o, "baseAssetID");
    if (!base) return std::unexpected(base.error());
    m.base_asset_id = *base;

    auto quote = id_member(o, "quoteAssetID");
    if (!quote) return std::unexpected(quote.error());
    m.quote_asset_id = *quote;

    auto venue = hex_member(o, "venueConfig");
    if (!venue) return std::unexpected(venue.error());
    m.venue_config = *venue;

    auto en = bool_member(o, "enabled");
    if (!en) return std::unexpected(en.error());
    m.enabled = *en;

    return m;
}

void encode_asset(json::Writer& w, const Asset& a) {
    w.begin_object();
    w.key("networkID");
    w.number(a.network_id);
    w.key("chainID");
    w.string(cb58(a.chain_id));
    w.key("assetKind");
    w.string(to_string(a.kind));
    w.key("canonicalRef");
    w.string(hex0x(view(a.canonical_ref)));
    w.key("decimals");
    w.number(a.decimals);
    w.key("symbol");
    w.string(a.symbol);
    w.key("name");
    w.string(a.name);
    w.key("enabled");
    w.boolean(a.enabled);
    w.key("riskTier");
    w.number(std::uint8_t(a.risk_tier));
    w.end_object();
}

void encode_market(json::Writer& w, const Market& m) {
    w.begin_object();
    w.key("networkID");
    w.number(m.network_id);
    w.key("baseAssetID");
    w.string(cb58(m.base_asset_id));
    w.key("quoteAssetID");
    w.string(cb58(m.quote_asset_id));
    w.key("venueConfig");
    w.string(hex0x(view(m.venue_config)));
    w.key("enabled");
    w.boolean(m.enabled);
    w.end_object();
}

Result<Bytes> read_file(const std::string& path) {
    std::unique_ptr<std::FILE, int (*)(std::FILE*)> f(std::fopen(path.c_str(), "rb"), std::fclose);
    if (!f) return fail("manifest: read " + path + ": no such file or directory");
    Bytes out;
    std::array<std::uint8_t, 4096> buf{};
    while (true) {
        const std::size_t n = std::fread(buf.data(), 1, buf.size(), f.get());
        out.insert(out.end(), buf.begin(), buf.begin() + std::ptrdiff_t(n));
        if (n < buf.size()) break;
    }
    if (std::ferror(f.get())) return fail("manifest: read " + path + ": I/O error");
    return out;
}

}  // namespace

Result<void> Manifest::validate_shape() const {
    if (network.empty()) return fail("manifest: empty network name");
    if (network_id == 0) return fail("manifest: networkID must be non-zero");
    if (evm_chain_id == 0)
        return fail("manifest: evmChainID must be non-zero (the RPC-checkable C-Chain identity)");
    if (c_chain_id == kEmptyId)
        return fail("manifest: cChainID must be set (the C-Chain consensus id)");

    for (std::size_t i = 0; i < assets.size(); ++i) {
        const Asset& a = assets[i];
        const std::string at = "manifest: asset[" + std::to_string(i) + "]";
        if (a.network_id != network_id)
            return fail(at + " networkID " + std::to_string(a.network_id) +
                        " != manifest networkID " + std::to_string(network_id));
        switch (a.kind) {
            case AssetKind::EVMNative:
            case AssetKind::ERC20:
                if (a.chain_id != c_chain_id)
                    return fail(at + " (" + std::string(to_string(a.kind)) +
                                ") chainID must be the C-Chain " + cb58(c_chain_id) + ", got " +
                                cb58(a.chain_id));
                break;
            case AssetKind::UTXO:
                // Rooted at a UTXO source chain, not the C-Chain.
                break;
            default:
                return fail(at + " invalid kind");
        }
        if (auto r = a.validate_shape(); !r) return std::unexpected(r.error().wrap(at));
    }
    for (std::size_t i = 0; i < markets.size(); ++i) {
        if (markets[i].network_id != network_id)
            return fail("manifest: market[" + std::to_string(i) + "] networkID " +
                        std::to_string(markets[i].network_id) + " != manifest networkID " +
                        std::to_string(network_id));
    }
    return {};
}

std::function<std::string(const Id&)> Manifest::chain_label_for() const {
    // The labels are captured by value: the lookup outlives no manifest it did
    // not copy, so a gate run cannot read a freed map.
    return [labels = chain_labels](const Id& id) -> std::string {
        auto it = labels.find(hex(id));
        return it == labels.end() ? std::string() : it->second;
    };
}

Result<void> Manifest::admit_into(Registry& reg, ChainVerifier& v) const {
    if (auto r = validate_shape(); !r) return r;
    if (v.confirms_c_chain()) {
        if (auto r = v.confirm_c_chain(network_id, evm_chain_id, c_chain_id); !r)
            return std::unexpected(
                r.error().wrap("manifest " + network + ": C-Chain identity confirm"));
    }
    for (std::size_t i = 0; i < assets.size(); ++i) {
        if (auto r = reg.register_asset(assets[i], v); !r)
            return std::unexpected(r.error().wrap("manifest " + network + ": asset[" +
                                                  std::to_string(i) + "] (" +
                                                  std::string(to_string(assets[i].kind)) + ")"));
    }
    for (std::size_t i = 0; i < markets.size(); ++i) {
        if (auto r = reg.create_market(markets[i]); !r)
            return std::unexpected(
                r.error().wrap("manifest " + network + ": market[" + std::to_string(i) + "]"));
    }
    return {};
}

Result<void> Manifest::apply_to(Registry& reg, ChainVerifier& v,
                                const DexAssetPolicy& policy) const {
    if (auto r = admit_into(reg, v); !r) return r;
    const NetworkClass cls = network_class_for(network_id);
    if (auto r = refuse_under_synthetic_config(cls, policy, reg, chain_label_for()); !r)
        return std::unexpected(r.error().wrap("manifest " + network + ": startup gate"));
    return {};
}

Result<std::unique_ptr<Registry>> Manifest::validate(ChainVerifier& v) const {
    auto reg = std::make_unique<Registry>(
        std::vector<AssetKind>{AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});
    if (auto r = apply_to(*reg, v, default_dex_asset_policy()); !r)
        return std::unexpected(r.error());
    return reg;
}

std::string Manifest::encode() const {
    json::Writer w;
    w.begin_object();
    w.key("network");
    w.string(network);
    w.key("networkID");
    w.number(network_id);
    w.key("evmChainID");
    w.number(evm_chain_id);
    w.key("cChainID");
    w.string(cb58(c_chain_id));
    // chainLabels carries omitempty, so an absent or empty map is not written at
    // all. Its keys are written sorted, which is what Go does for every map.
    if (!chain_labels.empty()) {
        w.key("chainLabels");
        w.begin_object();
        for (const auto& [k, v] : chain_labels) {
            w.key(k);
            w.string(v);
        }
        w.end_object();
    }
    w.key("assets");
    if (assets_is_null) {
        w.null();
    } else {
        w.begin_array();
        for (const Asset& a : assets) encode_asset(w, a);
        w.end_array();
    }
    w.key("markets");
    if (markets_is_null) {
        w.null();
    } else {
        w.begin_array();
        for (const Market& m : markets) encode_market(w, m);
        w.end_array();
    }
    w.end_object();
    return w.str();
}

Result<DecodedManifest> decode_manifest_bytes(ByteView raw, std::string_view label) {
    DecodedManifest out;
    out.sha256_hex = hex(view(sha256(raw)));

    auto doc = json::parse(std::string_view(reinterpret_cast<const char*>(raw.data()), raw.size()));
    if (!doc)
        return std::unexpected(doc.error().wrap("manifest: decode " + std::string(label)));
    auto obj = doc->as_object();
    if (!obj)
        return std::unexpected(obj.error().wrap("manifest: decode " + std::string(label)));
    const json::Object& o = **obj;
    if (auto r = reject_unknown_fields(o, "registry.Manifest", kManifestFields); !r)
        return std::unexpected(r.error().wrap("manifest: decode " + std::string(label)));

    Manifest& m = out.manifest;
    auto ctx = [&](Error e) { return e.wrap("manifest: decode " + std::string(label)); };

    auto network = string_member(o, "network");
    if (!network) return std::unexpected(ctx(network.error()));
    m.network = *network;

    auto net = u64_member(o, "networkID", 0xFFFFFFFFull);
    if (!net) return std::unexpected(ctx(net.error()));
    m.network_id = std::uint32_t(*net);

    auto evm = u64_member(o, "evmChainID", 0xFFFFFFFFFFFFFFFFull);
    if (!evm) return std::unexpected(ctx(evm.error()));
    m.evm_chain_id = *evm;

    auto cchain = id_member(o, "cChainID");
    if (!cchain) return std::unexpected(ctx(cchain.error()));
    m.c_chain_id = *cchain;

    if (const json::Value* lv = member(o, "chainLabels")) {
        auto lo = lv->as_object();
        if (!lo) return std::unexpected(ctx(lo.error().wrap("field \"chainLabels\"")));
        for (const auto& [k, v] : **lo) {
            auto s = v.as_string();
            if (!s) return std::unexpected(ctx(s.error().wrap("chainLabels[\"" + k + "\"]")));
            m.chain_labels[k] = *s;
        }
    }

    if (const json::Value* av = member(o, "assets")) {
        auto arr = av->as_array();
        if (!arr) return std::unexpected(ctx(arr.error().wrap("field \"assets\"")));
        m.assets_is_null = false;
        for (std::size_t i = 0; i < (*arr)->size(); ++i) {
            auto a = decode_asset((**arr)[i]);
            if (!a)
                return std::unexpected(
                    ctx(a.error().wrap("assets[" + std::to_string(i) + "]")));
            m.assets.push_back(std::move(*a));
        }
    }

    if (const json::Value* mv = member(o, "markets")) {
        auto arr = mv->as_array();
        if (!arr) return std::unexpected(ctx(arr.error().wrap("field \"markets\"")));
        m.markets_is_null = false;
        for (std::size_t i = 0; i < (*arr)->size(); ++i) {
            auto mk = decode_market((**arr)[i]);
            if (!mk)
                return std::unexpected(
                    ctx(mk.error().wrap("markets[" + std::to_string(i) + "]")));
            m.markets.push_back(std::move(*mk));
        }
    }

    if (auto r = m.validate_shape(); !r)
        return std::unexpected(r.error().wrap("manifest " + std::string(label)));
    return out;
}

Result<Manifest> load_manifest(const std::string& path) {
    auto raw = read_file(path);
    if (!raw) return std::unexpected(raw.error());
    auto d = decode_manifest_bytes(view(*raw), path);
    if (!d) return std::unexpected(d.error());
    return std::move(d->manifest);
}

Result<std::string> normalize_sha256(std::string_view h) {
    std::string s;
    for (char c : h) {
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r') continue;
        s.push_back(char(std::tolower(static_cast<unsigned char>(c))));
    }
    if (s.starts_with("sha256:")) s.erase(0, 7);
    if (s.starts_with("0x")) s.erase(0, 2);
    if (s.size() != 64)
        return fail("registry: pinned manifest hash must be a 32-byte SHA-256 (64 hex chars), got " +
                    std::to_string(s.size()) + " chars");
    for (char c : s) {
        if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')))
            return fail("registry: pinned manifest hash is not valid hex");
    }
    return s;
}

Result<Manifest> load_manifest_pinned(const std::string& path, std::string_view expected_sha256) {
    if (expected_sha256.empty()) return load_manifest(path);
    auto want = normalize_sha256(expected_sha256);
    if (!want) return std::unexpected(want.error());
    auto raw = read_file(path);
    if (!raw) return std::unexpected(raw.error());
    auto d = decode_manifest_bytes(view(*raw), path);
    if (!d) return std::unexpected(d.error());
    if (d->sha256_hex != *want)
        return fail_note(Err::ManifestHashMismatch,
                         "path=" + path + " want=" + *want + " got=" + d->sha256_hex);
    return std::move(d->manifest);
}

}  // namespace lux::dexvm
