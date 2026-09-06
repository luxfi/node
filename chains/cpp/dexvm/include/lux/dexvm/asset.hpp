// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// asset.hpp — the D-Chain's identity primitive: what an asset IS, and how its
// 32-byte name is derived from where it really lives.
//
// One property is made structural rather than incidental: every asset the DEX
// can credit or debit corresponds to a REAL object on-chain (an ERC-20 contract
// on the C-Chain, the C-Chain native coin, or a UTXO asset on the X-Chain).
// There is no synthetic class, no ASCII-ticker identity, and no declared-but-
// unbacked credit. An asset's identity is a hash of WHERE IT LIVES, so two
// parties given the same chain derive the same AssetID and a fabricated asset
// has no preimage.
//
// The fold is length-prefixed and domain-separated, and BOTH properties are
// load-bearing. The length prefix makes it injective: (networkID=1, ref=0x02)
// and (networkID=0x0102, ref=) cannot produce the same byte stream. The kind tag
// makes the three classes disjoint: an ERC-20 whose 20-byte address equals the
// low bytes of a UTXO assetID cannot collide.
//
// These bytes are consensus. The golden vectors in test/golden.hpp are the same
// bytes Go and luxfi/dex assert; a change here that moves them is a fork.

#pragma once

#include "lux/dexvm/id.hpp"

#include <cstdint>
#include <string_view>

namespace lux::dexvm {

// AssetKind is the CLOSED set of asset classes the DEX admits. There are exactly
// three. There is no synthetic / D-native / declared class — adding one is a
// breaking change to the wire identity and is intentionally hard.
enum class AssetKind : std::uint8_t {
    // Invalid is the zero value and is never admissible: a zero-initialised
    // record fails closed.
    Invalid = 0,
    // EVMNative is the C-Chain native coin. Its reference is the fixed marker.
    EVMNative = 1,
    // ERC20 is a token deployed on the C-Chain. Its reference is the 20-byte
    // contract address.
    ERC20 = 2,
    // UTXO is an asset native to a UTXO chain. Its reference is the 32-byte
    // source-chain assetID.
    UTXO = 3,
};

// to_string renders the kind as its canonical wire/JSON token. These exact
// strings appear in manifests and in the allowed-kinds policy; they are the
// contract, not cosmetics. An unknown value renders "INVALID".
std::string_view to_string(AssetKind k);

// valid reports whether k is one of the three admissible kinds.
bool valid(AssetKind k);

// marshal_kind / parse_kind make AssetKind round-trip as its canonical token,
// never as a bare integer. An unknown token — including an ASCII ticker
// masquerading as a kind — fails closed, and an invalid kind refuses to marshal.
Result<std::string> marshal_kind(AssetKind k);
Result<AssetKind> parse_kind(std::string_view s);

// kEVMNativeMarker is the fixed reference for the C-Chain native coin: 20 zero
// bytes, the EVM's own sentinel for "the native coin". EVM_NATIVE has no
// contract address, so folding a fixed kind-tagged marker means every party
// derives the same native AssetID for a given (networkID, C-chainID) and nobody
// can invent a second native asset.
inline constexpr std::array<std::uint8_t, 20> kEVMNativeMarker{};

// canonical_ref_for validates and returns the canonical on-chain reference for a
// (kind, ref) pair. This is the SINGLE place that decides what a well-formed
// reference looks like per kind — derivation and registration both call it.
//
//   EVM_NATIVE  ref must equal the marker (20 zero bytes); the native coin has
//               no address, so no caller can smuggle another ref into the kind.
//   ERC20       ref must be a 20-byte address, and not the zero address (that
//               is the native marker, never a token).
//   UTXO        ref must be a 32-byte, non-zero source-chain assetID.
Result<Bytes> canonical_ref_for(AssetKind kind, ByteView ref);

// derive_asset_id computes the canonical 32-byte identity of a real on-chain
// asset: a length-prefixed SHA-256 fold over, in order,
//
//   "lux:dex:asset:v1" | networkID | sourceChainID | kind | canonicalRef
//
// source_chain_id is the C-Chain id for EVM_NATIVE/ERC20 and the UTXO source
// chain id for UTXO. The result lives in the SAME identity space the on-chain
// atomic objects already use, so a registered AssetID is directly comparable to
// the asset a real cross-chain object carries. It is never a string ticker.
Result<Id> derive_asset_id(std::uint32_t network_id, const Id& source_chain_id, AssetKind kind,
                           ByteView ref);

// market_id computes a market's identity from its two asset identities and the
// venue configuration:
//
//   "lux:dex:market:v1" | networkID | baseAssetID | quoteAssetID | venueConfig
//
// Both sides are themselves canonical AssetIDs, so a market is pinned to real
// assets by construction: there is no AssetID for a synthetic asset, so there is
// no market name over one. venue_config is the canonical serialization of the
// parameters (tick, lot, fee tier) that distinguish two venues on one pair.
Id market_id(std::uint32_t network_id, const Id& base_asset_id, const Id& quote_asset_id,
             ByteView venue_config);

// Folder accumulates a length-prefixed, domain-separated preimage and folds it
// with SHA-256. Exposed because a format is only pinned by a test that can state
// it — and because the block layer folds its execution root the same way.
class Folder {
public:
    // Every field is written as a big-endian uint64 length followed by its
    // bytes. That is the whole injectivity argument, so there is one writer.
    void raw(ByteView b);
    void tag(std::string_view s);  // a domain-separation constant
    void bytes(ByteView b);        // a variable-length field
    void u8(std::uint8_t v);       // a single-byte field (the kind tag)
    void u32(std::uint32_t v);     // a 32-bit field (networkID), big-endian
    void u64(std::uint64_t v);

    Id sum() const;
    const Bytes& preimage() const { return buf_; }

private:
    Bytes buf_;
};

}  // namespace lux::dexvm
