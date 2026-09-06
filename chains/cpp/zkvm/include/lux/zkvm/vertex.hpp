// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vertex.hpp — the Z-Chain's DAG shape.
//
// A vertex is not a block: it names several parents and carries no timestamp.
// It changes state the same way, though, so it goes through the same store, and
// its conflict rule is the one the shielded pool cares about — two vertices
// conflict iff their nullifier sets intersect.

#pragma once

#include "lux/zkvm/chainstore.hpp"
#include "lux/zkvm/id.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/wire.hpp"

#include <memory>
#include <set>
#include <string>
#include <vector>

namespace lux::zkvm {

class Vm;

class Vertex final : public Decision, public std::enable_shared_from_this<Vertex> {
public:
    Vertex() = default;

    std::uint64_t vertex_height = 0;
    std::uint32_t epoch = 0;
    std::vector<Id> parents;
    std::vector<Id> tx_ids;
    std::vector<Transaction> txs;

    Id id() const override { return id_; }
    ByteView bytes() const override { return view(bytes_); }

    // parent is the ONE block this vertex extends, for a store that keeps one
    // tip. A vertex naming several parents extends no single block and names
    // none here, so the store refuses it rather than committing whichever one
    // came first.
    Id parent() const override { return parents.size() == 1 ? parents[0] : kEmptyId; }
    std::uint64_t height() const override { return vertex_height; }

    wire::Result<void> write(store::Store& view) override;
    void publish() override;

    // check holds a vertex to what a block is held to. It used to check the
    // transactions and NOTHING ELSE — not the parents, not the height — so a
    // vertex naming no parent at height 1<<40 verified, and accepting it set the
    // store's height to 1<<40, pruned every block in flight, and left the linear
    // chain unable to propose a child ever again.
    wire::Result<void> check();
    wire::Result<void> commit();
    void reject();

    // conflicts is true iff the two vertices share a nullifier.
    bool conflicts(const Vertex& other) const;
    ByteSet nullifier_set() const;

    Id compute_id() const;
    Bytes serialize() const;

    void bind_vm(Vm& vm) { vm_ = &vm; }
    void finish();  // recompute the id and the bytes from the content
    void set_raw(Bytes b) { bytes_ = std::move(b); }
    void set_computed_id(const Id& id) { id_ = id; }

    Status status() const { return status_; }

private:
    Vm* vm_ = nullptr;
    Id id_{};
    Bytes bytes_;
    Status status_ = Status::Processing;
};

// deserialize_vertex decodes a vertex.
//
// Counts are attacker-controlled: each is bounded by the bytes that remain
// BEFORE anything is reserved, or a 16-byte vertex claiming 2^32-1 parents asks
// for 128 GiB and the node dies on an unrecoverable allocation.
//
// Every byte handed in belongs to the vertex, or the value read is not the value
// sent. Without that, arbitrary trailing bytes ride along in the bytes the store
// writes to disk, so one logical vertex has unboundedly many encodings all
// mapping to the same id, and a peer can park megabytes under a legitimate one.
wire::Result<std::shared_ptr<Vertex>> deserialize_vertex(ByteView data, Vm& vm);

}  // namespace lux::zkvm
