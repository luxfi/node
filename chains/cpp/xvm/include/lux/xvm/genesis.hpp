// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis.hpp — the assets the X-Chain starts with, and the bytes a host hands
// it to say so.
//
// Genesis is a list of (alias, CreateAssetTx). The alias is a NAME, local to
// this node — "LUX", "VIX" — and it is what a caller may say instead of the
// 32-byte id. The id itself is not in the genesis bytes and could not be:
// an asset's id IS its creating transaction's id, so it is a HASH of the very
// bytes being written, and writing it down beside them would create a second
// answer that could disagree with the first.
//
// The first asset is the fee asset. That is a convention of position, stated
// here because the alternative — a field naming it — is another second answer.

#pragma once

#include "lux/xvm/address.hpp"
#include "lux/xvm/id.hpp"
#include "lux/xvm/txs.hpp"

#include <map>
#include <memory>
#include <string>
#include <vector>

namespace lux::xvm::genesis {

template <class T>
using Result = wire::Result<T>;

inline constexpr const char* kErrNoAssets = "xvm genesis has zero asset txs";
inline constexpr const char* kErrTrailingBytes = "trailing bytes (non-canonical)";
inline constexpr const char* kErrCountMismatch = "asset count mismatch";
inline constexpr const char* kErrNotCreateAsset = "genesis asset is not a CreateAssetTx";

// A genesis asset: the name this node knows it by, plus the transaction that
// defines it and holds its initial supply.
struct Asset {
    std::string alias;
    std::shared_ptr<txs::CreateAssetTx> create;

    // id is the asset's id: the id of the SIGNED transaction that creates it.
    // A genesis asset carries no credentials — it has no inputs to authorize —
    // so the signed bytes are the unsigned bytes in an empty envelope, and the
    // id follows from them.
    Result<Id> id() const;
};

struct Genesis {
    std::vector<Asset> assets;
};

// ---- a genesis stated rather than encoded ----
//
// A definition is what a person writes: how much of an asset each address
// holds, and who may mint more. `from_definitions` turns that into the
// transactions above, which is the only way a genesis is ever authored — the
// buffer is an output, never an input a human edits. Go: xvm.NewGenesis.

// Holder is an amount and the address that owns it, written as bech32.
struct Holder {
    std::uint64_t amount = 0;
    std::string address;
};

// Owners is who may mint more of an asset, and how many of them must agree.
struct Owners {
    std::uint32_t threshold = 0;
    std::vector<std::string> minters;
};

// What an asset holds the moment the chain starts: fixed holdings, and the
// right to create more. An asset with neither is a definition with no initial
// state at all, which is allowed and means exactly that.
struct InitialState {
    std::vector<Holder> fixed_cap;
    std::vector<Owners> variable_cap;

    bool empty() const { return fixed_cap.empty() && variable_cap.empty(); }
};

struct AssetDefinition {
    std::string name;
    std::string symbol;
    std::uint8_t denomination = 0;
    InitialState initial_state;
    Bytes memo;
};

// from_definitions builds the genesis these definitions describe.
//
// The map is keyed by alias and the result is ordered BY that alias, so the
// buffer does not depend on the order the definitions were given in — a genesis
// whose bytes depended on iteration order would give two nodes two different
// fee assets. Every created transaction names the empty chain id, as Go's does:
// a genesis asset is defined before the chain that carries it has an id.
Result<Genesis> from_definitions(std::uint32_t network_id,
                                 const std::map<std::string, AssetDefinition>& definitions);

// bytes writes the canonical genesis buffer:
//
//   zap object { Count u32 @0, AliasLens list @4, AliasBlob bytes @12,
//                TxLens list @20, TxBlob bytes @28 }
//
// The two blobs are the aliases and the unsigned CreateAssetTx bodies
// concatenated, split by their length lists — the same packed-list shape every
// variable section in this chain uses.
Result<Bytes> bytes(const Genesis& g);

// parse reads that buffer back, re-hydrating each CreateAssetTx from its own
// unsigned bytes. Trailing bytes are refused: a buffer that carries more than
// the message would hash differently while parsing the same, which is a
// malleability surface rather than a tolerance.
Result<Genesis> parse(ByteView genesis_bytes);

// as_txs turns a parsed genesis into the initialized transactions the VM
// installs — the form `Vm::initialize` takes.
Result<std::vector<std::shared_ptr<txs::Tx>>> as_txs(const Genesis& g);

// fee_asset_id is the id of the FIRST asset: the one the chain charges fees in.
Result<Id> fee_asset_id(ByteView genesis_bytes);

}  // namespace lux::xvm::genesis
