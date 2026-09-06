// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// chainstore.hpp — how a decision becomes fact, written once.
//
// A chain declares what it IS — its transactions, its records, its rules. It
// does not restate how a block becomes fact: every chain does that identically
// and the one way to get it wrong is to write it again. So it is here, once,
// and both shapes the Z-Chain decides in — a linear block and a DAG vertex —
// go through it.
//
// SPLITTING write FROM publish IS WHAT MAKES A HALF-APPLIED DECISION
// UNWRITABLE. write stages every durable change through the view and can fail,
// in which case it is discarded whole; publish makes the effects visible in
// memory, runs only once the writes are durable, and cannot fail. A chain that
// wrote and published in one pass has no such boundary, and its first failed
// write leaves the chain believing something that is not on disk.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/wire.hpp"

#include <functional>
#include <map>
#include <memory>
#include <mutex>

namespace lux::zkvm {

inline constexpr const char* kErrNoBlock = "chain: no such block";
inline constexpr const char* kErrNotOnTip = "chain: block does not extend the accepted tip";
inline constexpr const char* kErrNotOpen = "chain: store has not opened";

// Status is where a decision stands. It belongs beside Decision because both
// shapes — a block and a vertex — reach the same three.
enum class Status : std::uint8_t { Processing = 0, Accepted = 1, Rejected = 2 };

// Decision is a unit of state change the store can accept: an identity, a place
// in the sequence, an encoding, and the two halves of applying it.
//
// It is deliberately NOT the node's block interface. A linear chain's blocks
// satisfy that as well; a DAG's vertices have several parents and no timestamp
// and do not. Both change state the same way, and this is that way.
struct Decision {
    virtual ~Decision() = default;

    virtual Id id() const = 0;
    virtual ByteView bytes() const = 0;

    // parent and height are the decision's place in the sequence, and it takes
    // both. Height alone is not a place: a block whose parent is an OLD accepted
    // block satisfies height == parent+1 perfectly well, and a chain that admits
    // it rewinds.
    virtual Id parent() const = 0;
    virtual std::uint64_t height() const = 0;

    virtual wire::Result<void> write(store::Store& view) = 0;
    virtual void publish() = 0;
};

class ChainStore {
public:
    // reload rebuilds whatever the chain caches in memory from committed state.
    // It runs after a failed apply, so the caches say what the store says rather
    // than what the abandoned decision said.
    ChainStore(store::Store& base, std::function<wire::Result<void>()> reload)
        : base_(&base), view_(base), reload_(std::move(reload)) {}

    // state is what a decision writes through, and what every read sees:
    // committed state plus whatever the decision in progress has staged.
    store::Store& state() { return view_; }
    store::Store& base() { return *base_; }

    // open sets the chain's starting point: the tip recorded in committed state,
    // or genesis if nothing is recorded. It reports WHICH — `fresh` — so a chain
    // that seeds state on its first run can tell its first run from every later
    // one.
    //
    // Only a tip that is ABSENT means a fresh chain. Reading any other failure
    // that way — a closed store, an unreadable volume, a short read — starts a
    // live chain over at genesis and lets it build height 1 on top of state it
    // cannot see, durably.
    wire::Result<bool> open(std::shared_ptr<Decision> genesis,
                            const std::function<wire::Result<std::shared_ptr<Decision>>(
                                ByteView)>& parse);

    // accept applies d and commits it.
    //
    // A decision extends the tip or it is not accepted, and that is decided
    // HERE, under the lock that commits. Verify reached the same verdict earlier
    // against the tip AT THAT TIME; the tip moves between the two, so a caller
    // that asks before calling this asks in a window the store then reopens.
    //
    // Every write d makes goes through the view and lands in ONE commit,
    // together with the decision itself, its height entry and the tip pointer.
    // Any failure rolls the view back, rebuilds the caches from committed state,
    // and leaves the tip where it was.
    wire::Result<void> accept(const std::shared_ptr<Decision>& d);

    // seed applies the one mutation a chain makes outside consensus: what its
    // genesis allocates, written through the view and committed at once, before
    // any decision exists.
    //
    // The tip goes in that SAME commit, because seeding is the act that makes a
    // chain no longer fresh. A seed that recorded no tip left the two
    // disagreeing: a chain that had allocated its genesis and not yet accepted a
    // block reported itself fresh on every boot and allocated again — which its
    // own state then refuses ("already exists"), so the node cannot restart
    // until it produces a block it cannot produce.
    wire::Result<void> seed(const std::function<wire::Result<void>(store::Store&)>& write);

    // propose hands the caller the decision to build on and tracks whatever it
    // builds, in one step. Reading the parent and registering the child as two
    // steps leaves a window in which something is accepted between them.
    wire::Result<std::shared_ptr<Decision>> propose(
        const std::function<wire::Result<std::shared_ptr<Decision>>(const Decision&)>& build);

    // track makes a decision findable by id while it is in flight, so a child
    // can resolve it as a parent — whether this node built it or parsed it from
    // a peer. Tracking only what a node builds leaves a follower able to verify
    // the first block of a run and unable to verify the second.
    void track(const std::shared_ptr<Decision>& d);
    void drop(const Id& id);
    void prefer(const Id& id);

    Id tip() const;
    std::uint64_t height() const;

    std::shared_ptr<Decision> last() const;

    wire::Result<std::shared_ptr<Decision>> block(
        const Id& id,
        const std::function<wire::Result<std::shared_ptr<Decision>>(ByteView)>& parse) const;

    wire::Result<Id> id_at_height(std::uint64_t height) const;

    bool accepted(const Id& id) const;

    std::size_t in_flight() const;

private:
    void track_locked(const std::shared_ptr<Decision>& d);
    void prune_locked();
    wire::Result<void> undo(std::string cause);

    static Bytes block_key(const Id& id);
    static Bytes height_key(std::uint64_t h);

    mutable std::mutex mu_;
    store::Store* base_;
    store::View view_;
    std::function<wire::Result<void>()> reload_;

    std::map<Id, std::shared_ptr<Decision>> flight_;
    std::shared_ptr<Decision> last_;
    Id tip_{};
    std::uint64_t height_ = 0;
    Id preferred_{};
    bool opened_ = false;
};

}  // namespace lux::zkvm
