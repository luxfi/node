// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// golden.hpp — the bytes this port is not allowed to move.
//
// Every value here was produced by the GO reference (chains/dexvm/registry) or,
// for the AssetID vectors, is byte-identical in TWO Go homes — the registry's
// own KAT and luxfi/dex's AssetIDGoldenVectors, which assert the same strings so
// a registered AssetID and a swap-derived one name the same asset.
//
// Do NOT edit a vector to make a test pass. A changed id is a fork.

#pragma once

#include <cstdint>

namespace lux::dexvm::test::golden {

// ---- AssetID: networkID 2, source chain id all 0x11, the three kinds -------
inline constexpr const char* kAssetERC20 =
    "dc392784b1b0764f885a2b24786850dae0a221fe7eaa218065ac1497473fa868";
inline constexpr const char* kAssetEVMNative =
    "5941ecf871f909bac11b9b3d34fff1d05c7a0182f3a1c5b905ee6059dbb6dc72";
inline constexpr const char* kAssetUTXO =
    "5cd895b8a577437bdf39e921902cbf06c29a11ae4f1776b369584c46fc0d647d";

// ---- cb58: what Go's ids.ID.String() writes, which is what a manifest holds -
inline constexpr const char* kCb58AllZero = "11111111111111111111111111111111LpoYY";
inline constexpr const char* kCb58AllOnes = "SeLqn3UAUoRymWmwW7axrzJK7JfNaBR2cHCryA6cFscgkny8";
inline constexpr const char* kCb58All11 = "8WwpJCixn9cKe3jAyXvxNeo5JrBFKj43ULkUeTfeLMqQJgouM";
inline constexpr const char* kCb58AllFF = "2wkBET2rRgE8pahuaczxKbmv7ciehqsne57F9gtzf1PVcUJEQG";
inline constexpr const char* kCb58Counting = "16qJFWMMHFy3xDdLmvUeyc2S6FrWRhJP51HsvDYdz9cWcm5W";

// ---- the three committed manifests ----------------------------------------
// The SHA-256 of each file's bytes, the C-Chain each declares, and the AssetID
// each one's single asset derives to — all as Go computes them.
// `sha256` is the hash of the FILE's bytes, which is what a pin binds.
// `reencode_sha256` is the hash of Go's MarshalIndent of the manifest AFTER
// loading it, which is a different value on purpose: the committed files were
// written by hand, so they carry a trailing newline and their chainLabels are
// not in sorted order, and no marshaller reproduces either. Go's own re-encode
// does not equal its file, so a port that made it equal would be the divergent
// one. Asserting BOTH is what separates "reads the same bytes" from "writes the
// same bytes".
struct EmbeddedManifest {
    std::uint64_t evm_chain_id;
    std::uint32_t network_id;
    const char* name;
    const char* sha256;
    const char* reencode_sha256;
    const char* c_chain_cb58;
    const char* asset_id;
};

inline constexpr EmbeddedManifest kMainnet{
    96369, 1, "mainnet",
    "aeed1b37914dfff3f8706a320816a1fa267bed2749b57fe9024ad6b6d362f6fa",
    "8ff0f839355c029fd778c8a40893a4e08017aad1c131e90aed74d31baf530a74",
    "2wRdZGeca1qkxzNCq88NWDF5nJ5A9o623vRJKd3FsjRYvuVvvt",
    "157ce221694b6d354ded2c412b4a8af78737aaea45fbc2d673ba9a0297b16d79"};

inline constexpr EmbeddedManifest kTestnet{
    96368, 2, "testnet",
    "87ad44a13fc679ea63268842272dd755ea947ee1198bfddb826110ced2ca27f0",
    "38fe1fa233d58e7029b75d20bfd940b837bf2ace78e429565ea48b62f0d4335b",
    "uzPtAE7PHd2TFHaxsvyVqmVbhMuWR4jiEh4XV8uF9uMQEFyUB",
    "b28b8ae23dffbcf83c77b7e1f217ef29e0b47fc08c62e0ef0c828f199a80f379"};

inline constexpr EmbeddedManifest kDevnet{
    96370, 3, "devnet",
    "e9e312d6e0fb617a84311d573fbfb54368fb3c0a71259f51a9f98dd6f67f996d",
    "a2a65e7f94bb2a42f6b4b1c9da31d66c9adefe271009a55e50ff2b6c83c4d328",
    "Bm8X7THQtS2txLrQWTDXN8a4JDuhP4KGybUE754LLSiVFLQ7v",
    "b3e7122cf728552ca6e7336e95ade83c2f48711b7ac963cebe549afeaf6a73f4"};

// ---- Go's MarshalIndent, byte for byte ------------------------------------
// A manifest with a chainLabels map (two keys, so the sort order shows), one
// ERC-20 asset, and a nil market list. This is what Go WROTE; the C++ writer
// must produce the same bytes, because the pin is over exactly these.
inline constexpr const char* kFixedManifestJSON =
    "{\n"
    "  \"network\": \"mainnet\",\n"
    "  \"networkID\": 1,\n"
    "  \"evmChainID\": 96369,\n"
    "  \"cChainID\": \"8WwpJCixn9cKe3jAyXvxNeo5JrBFKj43ULkUeTfeLMqQJgouM\",\n"
    "  \"chainLabels\": {\n"
    "    \"00\": \"z\",\n"
    "    \"1111111111111111111111111111111111111111111111111111111111111111\": \"Lux C-Chain\"\n"
    "  },\n"
    "  \"assets\": [\n"
    "    {\n"
    "      \"networkID\": 1,\n"
    "      \"chainID\": \"8WwpJCixn9cKe3jAyXvxNeo5JrBFKj43ULkUeTfeLMqQJgouM\",\n"
    "      \"assetKind\": \"ERC20\",\n"
    "      \"canonicalRef\": \"0x4b4c4d4e4f505152535455565758595a5b5c5d5e\",\n"
    "      \"decimals\": 18,\n"
    "      \"symbol\": \"WLUX\",\n"
    "      \"name\": \"Wrapped LUX\",\n"
    "      \"enabled\": true,\n"
    "      \"riskTier\": 0\n"
    "    }\n"
    "  ],\n"
    "  \"markets\": null\n"
    "}";
inline constexpr const char* kFixedManifestSHA256 =
    "6987bf9f17d2983925414e348359738db58d74d8fb37cfecd8aa714fe9269b40";

// The same shape with no labels at all (omitempty drops the member) — the
// localnet manifest's exact form.
inline constexpr const char* kLocalnetManifestJSON =
    "{\n"
    "  \"network\": \"localnet\",\n"
    "  \"networkID\": 1337,\n"
    "  \"evmChainID\": 1337,\n"
    "  \"cChainID\": \"8WwpJCixn9cKe3jAyXvxNeo5JrBFKj43ULkUeTfeLMqQJgouM\",\n"
    "  \"assets\": [\n"
    "    {\n"
    "      \"networkID\": 1337,\n"
    "      \"chainID\": \"8WwpJCixn9cKe3jAyXvxNeo5JrBFKj43ULkUeTfeLMqQJgouM\",\n"
    "      \"assetKind\": \"EVM_NATIVE\",\n"
    "      \"canonicalRef\": \"0x0000000000000000000000000000000000000000\",\n"
    "      \"decimals\": 18,\n"
    "      \"symbol\": \"LUX\",\n"
    "      \"name\": \"Lux\",\n"
    "      \"enabled\": true,\n"
    "      \"riskTier\": 0\n"
    "    }\n"
    "  ],\n"
    "  \"markets\": null\n"
    "}";
inline constexpr const char* kLocalnetManifestSHA256 =
    "3efea02fe5adf868c352a3bb75f21264faa06c08b277afa5ea72d58c5cba41c9";

}  // namespace lux::dexvm::test::golden
