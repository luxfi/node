// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// utxo.hpp — the shielded outputs this chain has created, and the set that says
// which commitments it already holds.
//
// The set is rebuilt from the records at boot, so a restarted node knows the
// same commitments a running one does. That is not a cache detail: the set is
// what refuses a duplicate commitment, and a verdict that depended on how
// recently the node was started would put a restarted validator on a different
// chain from a running one.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/wire.hpp"

#include <map>
#include <string>

namespace lux::zkvm {

// Database prefix for UTXO records: utxoPrefix || commitment -> marshalled UTXO.
inline constexpr std::uint8_t kUtxoPrefix = 0x10;

// The commitment names no unspent output. Distinct from a read that failed,
// which is reported as the failure it is.
inline constexpr const char* kErrNoUtxo = "zkvm: no such utxo";
inline constexpr const char* kErrUtxoExists = "UTXO already exists";

struct Utxo {
    Id tx_id{};
    std::uint32_t output_index = 0;
    Bytes commitment;
    Bytes ciphertext;
    Bytes ephemeral_pk;
    std::uint64_t height = 0;

    Bytes marshal() const;

    bool operator==(const Utxo&) const = default;
};

wire::Result<Utxo> parse_utxo(ByteView data);

Bytes make_utxo_key(ByteView commitment);

// UtxoDb manages the UTXO set. There is NO removal path: a spend is recorded by
// its nullifier, and deleting the output the note names would destroy the record
// of it.
class UtxoDb {
public:
    static wire::Result<std::unique_ptr<UtxoDb>> open(store::Store& db);

    wire::Result<void> add(const Utxo& u);
    wire::Result<Utxo> get(ByteView commitment) const;

    // count is read off the set rather than from a running total kept beside it.
    // A total is a second write, and a node that dies between the two comes back
    // with a number that disagrees with its own records — from which one removal
    // drives an unsigned counter below zero and reports 1.8e19 unspent notes
    // forever. Counting the set cannot disagree with the set.
    std::uint64_t count() const { return std::uint64_t(set_.size()); }

    // reload rebuilds the set from what the store now says, discarding whatever
    // a block that did not commit had already added.
    wire::Result<void> reload();

    void close() { set_.clear(); }

private:
    explicit UtxoDb(store::Store& db) : db_(&db) {}
    wire::Result<void> load();

    store::Store* db_;
    // commitment -> height when created.
    ByteMap<std::uint64_t> set_;
};

inline constexpr int kUtxoSize = 68;

}  // namespace lux::zkvm
