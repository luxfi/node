// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/asset.hpp"

#include <algorithm>

namespace lux::dexvm {
namespace {

// Domain-separation tags, folded as the FIRST field of every preimage so the
// two identity spaces never overlap. Versioned so a future migration can
// re-domain without ambiguity.
constexpr std::string_view kDomAssetV1 = "lux:dex:asset:v1";
constexpr std::string_view kDomMarketV1 = "lux:dex:market:v1";

bool all_zero(ByteView b) {
    return std::all_of(b.begin(), b.end(), [](std::uint8_t x) { return x == 0; });
}

std::string_view trim(std::string_view s) {
    auto space = [](char c) { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'; };
    while (!s.empty() && space(s.front())) s.remove_prefix(1);
    while (!s.empty() && space(s.back())) s.remove_suffix(1);
    return s;
}

}  // namespace

std::string_view to_string(AssetKind k) {
    switch (k) {
        case AssetKind::EVMNative: return "EVM_NATIVE";
        case AssetKind::ERC20:     return "ERC20";
        case AssetKind::UTXO:      return "UTXO";
        default:                   return "INVALID";
    }
}

bool valid(AssetKind k) {
    return k == AssetKind::EVMNative || k == AssetKind::ERC20 || k == AssetKind::UTXO;
}

Result<std::string> marshal_kind(AssetKind k) {
    if (!valid(k))
        return fail("registry: refuse to marshal invalid asset kind " +
                    std::to_string(std::uint32_t(std::uint8_t(k))));
    return std::string(to_string(k));
}

Result<AssetKind> parse_kind(std::string_view s) {
    const std::string_view t = trim(s);
    if (t == "EVM_NATIVE") return AssetKind::EVMNative;
    if (t == "ERC20") return AssetKind::ERC20;
    if (t == "UTXO") return AssetKind::UTXO;
    return fail("registry: unknown asset kind \"" + std::string(s) +
                "\" (only EVM_NATIVE, ERC20, UTXO)");
}

Result<Bytes> canonical_ref_for(AssetKind kind, ByteView ref) {
    switch (kind) {
        case AssetKind::EVMNative:
            if (ref.size() != 20)
                return fail(Err::BadRef, "EVM_NATIVE marker must be 20 bytes, got " +
                                             std::to_string(ref.size()));
            if (!all_zero(ref))
                return fail(Err::BadRef,
                            "EVM_NATIVE reference must be the native marker (address zero)");
            // Normalised to the canonical marker so a caller cannot pass a
            // distinct all-zero-but-differently-typed buffer.
            return to_bytes(view(kEVMNativeMarker));
        case AssetKind::ERC20:
            if (ref.size() != 20)
                return fail(Err::BadRef, "ERC20 token address must be 20 bytes, got " +
                                             std::to_string(ref.size()));
            if (all_zero(ref))
                return fail(Err::BadRef, "ERC20 token address must not be the zero address");
            return to_bytes(ref);
        case AssetKind::UTXO:
            if (ref.size() != 32)
                return fail(Err::BadRef,
                            "UTXO assetID must be 32 bytes, got " + std::to_string(ref.size()));
            if (all_zero(ref)) return fail(Err::BadRef, "UTXO assetID must not be empty");
            return to_bytes(ref);
        default:
            return fail(Err::InvalidKind);
    }
}

void Folder::raw(ByteView b) {
    std::uint64_t n = b.size();
    for (int i = 7; i >= 0; --i) buf_.push_back(std::uint8_t((n >> (8 * i)) & 0xff));
    buf_.insert(buf_.end(), b.begin(), b.end());
}

void Folder::tag(std::string_view s) {
    raw(ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size()));
}

void Folder::bytes(ByteView b) { raw(b); }

void Folder::u8(std::uint8_t v) { raw(ByteView(&v, 1)); }

void Folder::u32(std::uint32_t v) {
    const std::uint8_t b[4] = {std::uint8_t(v >> 24), std::uint8_t(v >> 16), std::uint8_t(v >> 8),
                               std::uint8_t(v)};
    raw(ByteView(b, 4));
}

void Folder::u64(std::uint64_t v) {
    std::uint8_t b[8];
    for (int i = 0; i < 8; ++i) b[i] = std::uint8_t((v >> (8 * (7 - i))) & 0xff);
    raw(ByteView(b, 8));
}

Id Folder::sum() const { return sha256(view(buf_)); }

Result<Id> derive_asset_id(std::uint32_t network_id, const Id& source_chain_id, AssetKind kind,
                           ByteView ref) {
    if (source_chain_id == kEmptyId) return fail(Err::EmptyChainID);
    auto cref = canonical_ref_for(kind, ref);
    if (!cref) return std::unexpected(cref.error());

    Folder f;
    f.tag(kDomAssetV1);
    f.u32(network_id);
    f.bytes(view(source_chain_id));
    f.u8(std::uint8_t(kind));
    f.bytes(view(*cref));
    return f.sum();
}

Id market_id(std::uint32_t network_id, const Id& base_asset_id, const Id& quote_asset_id,
             ByteView venue_config) {
    Folder f;
    f.tag(kDomMarketV1);
    f.u32(network_id);
    f.bytes(view(base_asset_id));
    f.bytes(view(quote_asset_id));
    f.bytes(venue_config);
    return f.sum();
}

}  // namespace lux::dexvm
