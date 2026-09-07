// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// persist.hpp — the chain's state, laid out as rows in a store.
//
// `state::MemState` is what the chain believes; a `store::Store` is bytes that
// survive the process. This is the one place the two meet, and it is a
// projection, not a second state: every row is derived from the state, and
// `load` rebuilds the same state from the same rows.
//
// Each family sits under a one-byte prefix, so a family can be walked without
// reading the rest — the ordered scan the store promises is what makes that a
// range rather than a filter. The prefixes above 0x10 belong to the validator
// history, which shares this store because a set at a past height and the set
// now are the same fact at two times.
//
// WHAT IS STORED IS THE CANONICAL BYTES where there are any: a transaction goes
// to disk as the bytes it arrived in and an unspent output as its cross-chain
// envelope, so a state reloaded from disk names them by exactly the ids the
// network named them by. The rest — stakers, owners, L1 validators — has no
// wire form of its own, so each is a ZAP object whose layout is written down in
// persist.cpp and nowhere else.
//
// `save` writes the DIFFERENCE between the state and what the store already
// holds. A block changes a handful of rows out of the whole set, and a log that
// recorded the whole set every height would be a log nobody could replay.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/state.hpp"
#include "lux/platformvm/store.hpp"

#include <string>

namespace lux::platformvm::persist {

// Write `from` into `to` and make it durable. Rows the state no longer holds
// are erased; rows that already hold these bytes are left alone.
Status save(const state::MemState& from, store::Store& to);

// Rebuild the state from `from`.
//
// A row that does not parse is a refusal, not a skip: a chain that silently
// dropped a staker it could not read would compute a validator set nobody else
// computes.
Result<state::MemState> load(const store::Store& from);

// Where the chain has got to: which block it last accepted, and at what height.
//
// It is not part of the state — the state is what the blocks PRODUCED — but it
// is just as much a thing a restarted node must know, because a node that
// forgot the last block it accepted would accept a second one at the same
// height.
struct Position {
    Id last_accepted{};
    std::uint64_t height = 0;
    // Every accepted block, by its own name, and the height index over them.
    std::map<Id, std::vector<std::uint8_t>> blocks;
    std::map<std::uint64_t, Id> by_height;

    friend bool operator==(const Position&, const Position&) = default;
};

Status save_position(const Position& p, store::Store& to);
Result<Position> load_position(const store::Store& from);

}  // namespace lux::platformvm::persist
