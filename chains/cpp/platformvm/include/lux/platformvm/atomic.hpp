// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// atomic.hpp — money arriving from another chain, and money leaving for one.
//
// Rendered from the atomic.SharedMemory surface the P-chain reads
// (github.com/luxfi/vm/chains/atomic, as vms/platformvm/txs/executor uses it).
//
// An import is the one transaction that spends something this chain has never
// seen. Everything else it verifies against its own state; an import has to ask
// the OTHER chain what it produced, and that answer is not the P-chain's to
// invent. So it enters through this interface and no other, and a node that
// cannot ask refuses the transaction rather than believing it.
//
// The elements an export produces are exchanged as VALUES rather than as bytes.
// Their byte encoding is the receiving chain's wire, not this one's, and a port
// that guessed it would be guessing about money.

#pragma once

#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"

#include <map>
#include <vector>

namespace lux::platformvm::atomic {

// One UTXO handed to another chain, with the addresses that chain will index it
// under.
struct Element {
    Id key{};
    UTXO utxo{};
    std::vector<ShortId> traits;

    friend bool operator==(const Element&, const Element&) = default;
};

// What one accepted block asks the shared memory to do, per peer chain.
struct Requests {
    std::vector<Id> remove;   // consumed by an import
    std::vector<Element> put; // produced by an export

    friend bool operator==(const Requests&, const Requests&) = default;
};

// The other chains' side of the ledger, as this chain is allowed to read it.
class SharedMemory {
  public:
    virtual ~SharedMemory() = default;

    // The UTXOs `peer_chain` produced under these keys, in the order asked.
    // A key the peer chain did not produce is a refusal, never a zero.
    virtual Result<std::vector<UTXO>> get(const Id& peer_chain, const std::vector<Id>& keys) const = 0;
};

}  // namespace lux::platformvm::atomic
