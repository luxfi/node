// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis.hpp — the state the network starts in.
//
// Rendered from Go vms/platformvm/genesis (genesis.go, genesiswire.go).
//
// A Lux network has exactly one P-chain, and the P-chain's genesis IS the
// network's genesis: who is staking, what money exists, which chains exist.
// There is nothing else to agree on before the first block.
//
// The blob is one ZAP object carrying a scalar header and three lists — money,
// validators, chains — each as a length list plus a concatenated blob. The
// validators and chains are stored as their own SIGNED bytes and re-parsed, so
// every transaction id (= sha256 of those bytes) is what it was when the
// genesis was written. Nothing is re-encoded, so nothing can be re-encoded
// differently.

#pragma once

#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstdint>
#include <span>
#include <string>
#include <vector>

namespace lux::platformvm::genesis {

// A UTXO the network starts holding, plus whatever the allocation said about
// itself. The message is data the chain carries and never reads.
struct Allocation {
    UTXO utxo{};
    std::vector<std::uint8_t> message;

    friend bool operator==(const Allocation&, const Allocation&) = default;
};

struct Genesis {
    std::uint64_t timestamp = 0;
    std::uint64_t initial_supply = 0;
    std::string message;
    std::vector<Allocation> utxos;
    // The transactions that put the first validators in the set, and the ones
    // that created the first chains. Signed, so they carry their own ids.
    std::vector<txs::Tx> validators;
    std::vector<txs::Tx> chains;

    std::vector<std::uint8_t> encode() const;
    static Result<Genesis> parse(std::span<const std::uint8_t> b);

    // Go: genesis validation. Every refusal here is about a genesis that would
    // start a chain in a state no transaction could have produced.
    Status verify() const;
};

}  // namespace lux::platformvm::genesis
