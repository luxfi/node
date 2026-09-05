// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// nullifier.hpp — the spent set: the whole of what stops a shielded note being
// spent twice.
//
// Nullifiers are PERMANENT and MUST NOT be pruned or removed. Deleting a spent
// nullifier lets a previously spent note be spent again, so there is no removal
// path here — not for reorg, not for compaction. If storage becomes a concern,
// a Merkle accumulator is the compaction.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/wire.hpp"

#include <map>
#include <memory>

namespace lux::zkvm {

// nullifierPrefix keys the spent set. A nullifier record is the height of the
// block that spent it.
inline constexpr std::uint8_t kNullifierPrefix = 0x20;

inline constexpr const char* kErrNullifierSpent = "zkvm: nullifier already spent";
inline constexpr const char* kErrNotAHeight = "zkvm: nullifier record is not a height";

Bytes make_nullifier_key(ByteView nullifier);

// Spend is the ONE answer to the ONE question "has this note been spent?" —
// because the two it used to be, a bool and a height, could not report a failed
// read at all. `err == nil` answers "not spent" for a set that could not be
// read, and that answer is what lets an already-spent note be spent again.
struct Spend {
    bool spent = false;
    std::uint64_t height = 0;
};

class NullifierDb {
public:
    static wire::Result<std::unique_ptr<NullifierDb>> open(store::Store& db);

    wire::Result<void> mark_spent(ByteView nullifier, std::uint64_t height);

    // spent_at reports whether a nullifier has been spent and at what height. A
    // read that failed is an error here, and the caller refuses the transaction
    // rather than admitting it.
    //
    // A miss falls through to the records and returns what it finds without
    // memoising it: a read that writes is a read that lies to every other
    // reader about the set standing still.
    wire::Result<Spend> spent_at(ByteView nullifier) const;

    // count is read off the set itself. Every record is loaded at startup and
    // nullifiers are never pruned, so the set is the whole of them; a total kept
    // alongside would be a second write that has to agree with the first, and
    // this cannot disagree with what it describes.
    std::uint64_t count() const { return std::uint64_t(spent_.size()); }

    // reload rebuilds the set from what the store now says. A block whose writes
    // were discarded has had its nullifiers discarded with them, so a set that
    // already recorded them must stop claiming those notes are spent —
    // otherwise the block can never be applied again.
    wire::Result<void> reload();

    void close() { spent_.clear(); }

    // forget_cache empties the in-memory set WITHOUT touching the records. It
    // exists for the test that proves a read does not memoise what it loads.
    void forget_cache() { spent_.clear(); }

private:
    explicit NullifierDb(store::Store& db) : db_(&db) {}
    wire::Result<void> load();

    store::Store* db_;
    ByteMap<std::uint64_t> spent_;
};

}  // namespace lux::zkvm
