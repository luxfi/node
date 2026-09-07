// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// records.hpp — what one thing the chain remembers looks like as bytes, and
// what it is filed under.
//
// One encoder per kind, used by everything that writes and everything that
// reads: the layer writer that puts a height down — genesis is a layer like any
// other — and the loader that brings a chain back. Two encoders for one record
// would be two opinions about what was written, and the second one only ever
// surfaces after a restart.
//
// The encoding is ZAP, like everything else on this chain. The KEY is not: a
// key is compared and ordered by its bytes, so it is a tag byte and then the
// natural key, big-endian where it is a number, which is what makes the walk
// over a prefix the walk in the value's own order.
//
// None of this is consensus. What a block commits to is its execution's root,
// computed by running the block; two nodes that agree on every root agree
// completely whatever shape either wrote its own copy in. So this layout is
// free to be the simple one.

#pragma once

#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/l1.hpp"
#include "lux/platformvm/state.hpp"
#include "lux/platformvm/status.hpp"
#include "lux/platformvm/store.hpp"
#include "lux/platformvm/txs.hpp"
#include "lux/platformvm/validators.hpp"

#include <cstdint>
#include <string>
#include <vector>

namespace lux::platformvm::records {

using Bytes = std::vector<std::uint8_t>;

// What a record is. The tag is the first byte of every key, so a scan over one
// tag is a scan over one kind.
enum class Tag : std::uint8_t {
    Meta = 0x01,             // the scalars there is one of
    LastAccepted = 0x02,     // the block this chain last decided, and its height
    Supply = 0x03,           // chain(32)
    Utxo = 0x04,             // id(32)
    RewardUtxos = 0x05,      // tx(32)
    CurrentValidator = 0x06, // chain(32) node(20)
    CurrentDelegator = 0x07, // chain(32) node(20) tx(32)
    PendingValidator = 0x08, // chain(32) node(20)
    PendingDelegator = 0x09, // chain(32) node(20) tx(32)
    DelegateeReward = 0x0A,  // chain(32) node(20)
    Network = 0x0B,          // id(32)
    NetworkOwner = 0x0C,     // id(32)
    NetworkConversion = 0x0D,// id(32)
    Transformation = 0x0E,   // network(32)
    Chain = 0x0F,            // network(32) tx(32)
    Tx = 0x10,               // id(32)
    L1Validator = 0x11,      // validation(32)
    Expiry = 0x12,           // the entry's own marshalling, which is already ordered
    History = 0x13,          // height(8, big-endian) chain(32) node(20)
    Block = 0x14,            // height(8, big-endian) id(32)
};

// ── keys

Bytes key(Tag t);
Bytes key(Tag t, const Id& a);
Bytes key(Tag t, const Id& a, const NodeId& n);
Bytes key(Tag t, const Id& a, const NodeId& n, const Id& b);
Bytes key(Tag t, const Id& a, const Id& b);
Bytes key(Tag t, std::span<const std::uint8_t> raw);
// Both of these lead with a big-endian height, so the walk over the tag is the
// walk in height order: the record of what a height changed, and the block that
// changed it.
Bytes history_key(std::uint64_t height, const Id& chain, const NodeId& node);
Bytes block_key(std::uint64_t height, const Id& id);

// The staker keys, which differ only in which set they name.
inline Bytes staker_key(bool current, bool validator, const state::Staker& s) {
    const Tag t = current ? (validator ? Tag::CurrentValidator : Tag::CurrentDelegator)
                          : (validator ? Tag::PendingValidator : Tag::PendingDelegator);
    return validator ? key(t, s.chain_id, s.node_id) : key(t, s.chain_id, s.node_id, s.tx_id);
}

// ── values
//
// Every encode/decode pair round-trips: what a chain writes is what it reads
// back, which is the one promise a store owes.

Bytes encode_meta(const state::Chain& s);
Status decode_meta(std::span<const std::uint8_t> b, state::MemState& into);

Bytes encode_last_accepted(const Id& id, std::uint64_t height);
Result<std::pair<Id, std::uint64_t>> decode_last_accepted(std::span<const std::uint8_t> b);

Bytes encode_u64(std::uint64_t v);
Result<std::uint64_t> decode_u64(std::span<const std::uint8_t> b);

Bytes encode_staker(const state::Staker& s);
Result<state::Staker> decode_staker(std::span<const std::uint8_t> b);

Bytes encode_utxos(const std::vector<UTXO>& us);
Result<std::vector<UTXO>> decode_utxos(std::span<const std::uint8_t> b);

Bytes encode_owner(const txs::Owner& o);
Result<txs::Owner> decode_owner(std::span<const std::uint8_t> b);

Bytes encode_conversion(const state::NetToL1Conversion& c);
Result<state::NetToL1Conversion> decode_conversion(std::span<const std::uint8_t> b);

Bytes encode_l1_validator(const l1::Validator& v);
Result<l1::Validator> decode_l1_validator(std::span<const std::uint8_t> b);

// A transaction is already its own bytes; only the status beside it is new.
Bytes encode_tx(const txs::Tx& tx, status::Status st);
Result<std::pair<txs::Tx, status::Status>> decode_tx(std::span<const std::uint8_t> b);

Bytes encode_change(const validators::Change& c);
Result<validators::Change> decode_change(std::span<const std::uint8_t> b);

// ── what a height leaves behind, and what a boot reads back
//
// What a height CHANGED about the validator sets, filed under that height, so
// the set at a height that has passed is the set now with everything since
// undone.
Status write_history(store::Store& to, std::uint64_t height,
                     const std::map<validators::Where, validators::Change>& c);

// A chain, as it was when it last committed. Fails rather than guessing when
// what comes back is not what this chain writes.
struct Restored {
    Id last_accepted{};
    std::uint64_t height = 0;
    std::vector<Bytes> blocks;  // every block this chain accepted, oldest first
};
Result<Restored> load(const store::Store& from, state::MemState& into, validators::History& history);

}  // namespace lux::platformvm::records
