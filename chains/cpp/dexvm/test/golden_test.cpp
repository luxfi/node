// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// golden_test — the cross-language identity KAT.
//
// This is the one suite whose failure is a FORK, not a bug: it asserts that
// derive_asset_id here reproduces the exact 32-byte ids Go's registry and
// luxfi/dex both assert, and that an id renders on the wire the way Go renders
// it. If either drifts, a registered asset and a swap-derived asset stop naming
// the same thing.

#include "lux/dexvm/asset.hpp"

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

Id chain_all_ones() {
    Id c{};
    for (auto& b : c) b = 0x11;
    return c;
}

void asset_id_kat() {
    std::printf("AssetID golden KAT (networkID 2, chain 0x11*32)\n");
    const Id chain = chain_all_ones();

    Bytes erc20(20, 0);
    erc20[19] = 0x01;
    Bytes utxo(32, 0);
    utxo[31] = 0x07;

    struct Vector {
        const char* name;
        AssetKind kind;
        ByteView ref;
        const char* want;
    };
    const Vector vectors[] = {
        {"ERC20/addr..01", AssetKind::ERC20, view(erc20), golden::kAssetERC20},
        {"EVM_NATIVE/marker", AssetKind::EVMNative, view(kEVMNativeMarker), golden::kAssetEVMNative},
        {"UTXO/asset..07", AssetKind::UTXO, view(utxo), golden::kAssetUTXO},
    };
    for (const Vector& v : vectors) {
        auto id = derive_asset_id(2, chain, v.kind, v.ref);
        if (!id) {
            check(false, std::string(v.name) + ": " + id.error().text);
            continue;
        }
        check_eq(hex(*id), v.want, v.name);
    }
}

void kind_tokens() {
    std::printf("the wire tokens, which are contract and not cosmetics\n");
    check_eq(std::string(to_string(AssetKind::EVMNative)), "EVM_NATIVE", "EVM_NATIVE token");
    check_eq(std::string(to_string(AssetKind::ERC20)), "ERC20", "ERC20 token");
    check_eq(std::string(to_string(AssetKind::UTXO)), "UTXO", "UTXO token");
    check_eq(std::string(to_string(AssetKind::Invalid)), "INVALID", "the zero value is INVALID");

    // The kind byte is folded into the preimage, so its numeric value is part of
    // the identity and not an implementation detail.
    check(std::uint8_t(AssetKind::EVMNative) == 1, "EVM_NATIVE folds as 1");
    check(std::uint8_t(AssetKind::ERC20) == 2, "ERC20 folds as 2");
    check(std::uint8_t(AssetKind::UTXO) == 3, "UTXO folds as 3");
}

void cb58_vectors() {
    std::printf("cb58, which is how a manifest spells an id\n");
    struct Vector {
        std::uint8_t fill;
        const char* want;
    };
    const Vector vectors[] = {
        {0x00, golden::kCb58AllZero},
        {0x01, golden::kCb58AllOnes},
        {0x11, golden::kCb58All11},
        {0xff, golden::kCb58AllFF},
    };
    for (const Vector& v : vectors) {
        Id id{};
        for (auto& b : id) b = v.fill;
        check_eq(cb58(id), v.want, "cb58 of an all-0x" + hex(ByteView(&v.fill, 1)) + " id");
        auto back = id_from_string(v.want);
        check(back.has_value() && *back == id, "and it decodes back to the same id");
    }

    Id counting{};
    for (std::size_t i = 0; i < counting.size(); ++i) counting[i] = std::uint8_t(i);
    check_eq(cb58(counting), golden::kCb58Counting, "cb58 of 0x00..0x1f");

    // A native chain id renders as its alias, not as base58 — Go's own fast
    // path, which a manifest can therefore carry.
    Id d_chain{};
    d_chain[31] = 'D';
    check_eq(cb58(d_chain), "11111111111111111111111111111111D", "the D-Chain's alias");
    auto back = id_from_string("11111111111111111111111111111111D");
    check(back.has_value() && *back == d_chain, "and the alias decodes back");

    // A corrupted cb58 string fails its checksum rather than decoding to
    // something else.
    std::string bad = golden::kCb58All11;
    bad[bad.size() - 1] = (bad.back() == 'M') ? 'N' : 'M';
    refused_any(id_from_string(bad), "a corrupted cb58 fails its checksum");
}

void identity_is_the_reference_not_the_ticker() {
    std::printf("the id is bound to where the asset lives, and to nothing else\n");
    const Id chain = chain_all_ones();
    const Bytes a = addr20(0x11);
    const Bytes b = addr20(0x22);

    auto id_a = derive_asset_id(1, chain, AssetKind::ERC20, view(a));
    auto id_b = derive_asset_id(1, chain, AssetKind::ERC20, view(b));
    check(id_a && id_b && *id_a != *id_b, "two addresses are two assets");

    auto other_chain = derive_asset_id(1, test_id(7), AssetKind::ERC20, view(a));
    check(other_chain && *other_chain != *id_a, "the same address on another chain is another asset");

    auto other_net = derive_asset_id(2, chain, AssetKind::ERC20, view(a));
    check(other_net && *other_net != *id_a, "and on another network, another asset again");

    // Domain separation: a UTXO assetID whose first 20 bytes ARE an ERC-20
    // address must not collide with it.
    Bytes utxo(32, 0);
    std::copy(a.begin(), a.end(), utxo.begin());
    auto id_utxo = derive_asset_id(1, chain, AssetKind::UTXO, view(utxo));
    check(id_utxo && *id_utxo != *id_a, "overlapping bytes across kinds do not collide");
}

void market_identity() {
    std::printf("a market's id is its two assets and its venue\n");
    const Id base = test_id(1);
    const Id quote = test_id(2);
    const std::string venue_a = "tick=1,lot=1,fee=30";
    const std::string venue_b = "tick=1,lot=1,fee=5";
    auto bytes_of = [](const std::string& s) {
        return ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size());
    };

    const Id id1 = market_id(1, base, quote, bytes_of(venue_a));
    const Id id2 = market_id(1, base, quote, bytes_of(venue_b));
    check(id1 != id2, "two venue configs are two markets");
    check(market_id(1, base, quote, bytes_of(venue_a)) != market_id(1, quote, base, bytes_of(venue_a)),
          "base/quote order is part of the identity");
    check(market_id(1, base, quote, bytes_of(venue_a)) == id1, "and the id is reproducible");
}

void reference_shapes() {
    std::printf("what a canonical reference is allowed to be\n");
    const Id chain = chain_all_ones();

    refused(derive_asset_id(1, chain, AssetKind::ERC20, view(Bytes(19, 0x01))), Err::BadRef,
            "a 19-byte ERC-20 address");
    refused(derive_asset_id(1, chain, AssetKind::ERC20, view(Bytes(20, 0x00))), Err::BadRef,
            "the zero address as an ERC-20 (that is the native marker)");
    refused(derive_asset_id(1, chain, AssetKind::EVMNative, view(addr20(0x01))), Err::BadRef,
            "a non-zero reference smuggled into EVM_NATIVE");
    refused(derive_asset_id(1, chain, AssetKind::UTXO, view(Bytes(31, 0x01))), Err::BadRef,
            "a 31-byte UTXO assetID");
    refused(derive_asset_id(1, chain, AssetKind::UTXO, view(Bytes(32, 0x00))), Err::BadRef,
            "an all-zero UTXO assetID");
    refused(derive_asset_id(1, chain, AssetKind::Invalid, view(addr20(0x01))), Err::InvalidKind,
            "the zero kind, which is the closest thing to a synthetic class");
    refused(derive_asset_id(1, kEmptyId, AssetKind::ERC20, view(addr20(0x01))), Err::EmptyChainID,
            "an empty source chain");
}

}  // namespace

int main() {
    asset_id_kat();
    kind_tokens();
    cb58_vectors();
    identity_is_the_reference_not_the_ticker();
    market_identity();
    reference_shapes();
    return report("golden");
}
