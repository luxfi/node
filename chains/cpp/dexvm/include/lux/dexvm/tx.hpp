// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// tx.hpp — what a D-Chain block carries.
//
// The registry's admission surface has exactly two verbs: admit a real asset,
// and open a market over two admitted ones. So the chain has exactly two
// transactions, and neither adds a decision the registry does not already make —
// applying one IS calling register_asset or create_market, with the same
// verifier and the same refusals.
//
// SCOPE, stated plainly: the registry semantics below are ported from Go
// (chains/dexvm/registry) and are identical to it. The BLOCK and TRANSACTION
// layer around them is NOT — Go's dexvm is a library the C-Chain settlement
// precompile calls in-process, and has no block of its own. So these bytes have
// no Go counterpart to agree with yet, and a Go or Rust D-Chain must be rendered
// FROM this definition rather than beside it, or the three will disagree the
// first time they are asked the same question.

#pragma once

#include "lux/dexvm/registry.hpp"

#include <variant>

namespace lux::dexvm {

enum class TxKind : std::uint8_t {
    Invalid = 0,
    RegisterAsset = 1,
    CreateMarket = 2,
};

struct Tx {
    TxKind kind = TxKind::Invalid;
    // Exactly one of these is meaningful, chosen by kind. A default-built Tx is
    // Invalid, which no execution accepts.
    Asset asset;
    Market market;

    static Tx register_asset(Asset a);
    static Tx create_market(Market m);

    // encode is the transaction's bytes, and id is their hash. The bytes are the
    // only thing a peer is trusted to send, so the id is derived from them
    // rather than carried beside them.
    Bytes encode() const;
    Id id() const;
};

Result<Tx> decode_tx(ByteView b);

// A block: a parent, a height, and the transactions in order. The order is
// consensus — the same transactions applied in another order can admit a
// different set, because a market's two sides must already be registered — so it
// is preserved exactly as encoded and never sorted.
struct BlockBody {
    Id parent{};
    std::uint64_t height = 0;
    std::vector<Tx> txs;

    Bytes encode() const;
    Id id() const;
};

Result<BlockBody> decode_block(ByteView b);

}  // namespace lux::dexvm
