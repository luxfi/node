// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// root.hpp — the committed state root, and the fold that produces the next one.
//
// There is exactly ONE root function. A hardware-conditional digest — a GPU
// Poseidon path with a SHA-256 fallback — would make the consensus-committed
// root depend on whether a node has an accelerator, so validators with and
// without one would reject each other's blocks. The CPU fold below is the root,
// on every node, with or without a kernel present.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/wire.hpp"

#include <memory>
#include <vector>

namespace lux::zkvm {

// rootKey holds the committed state root.
inline constexpr const char* kRootKey = "state_root";

class Root {
public:
    static wire::Result<std::unique_ptr<Root>> open(store::Store& db);

    // after returns the state root that results from applying txs on top of the
    // committed root, as SHA-256 over
    //
    //   committed ‖ every output commitment (tx order) ‖ every nullifier (tx order)
    //
    // It is PURE: nothing is mutated, so computing a root is safe inside a
    // block's verify. Verifying the same block twice, or verifying a block that
    // is later rejected and then verifying its competitor, all yield the root
    // that block's proposer computed. Only finalize advances the committed root,
    // and only accept calls finalize.
    Id after(const std::vector<Transaction>& txs) const;

    // finalize advances the committed root. It is the only mutation.
    wire::Result<void> finalize(const Id& next);

    Id get() const { return committed_; }

    // reload puts the committed root back, discarding an advance that belonged
    // to a block whose writes were discarded.
    //
    // A read that FAILED is not an absent root: answering any error with the
    // empty root boots a node believing the shielded state is empty, after which
    // it disagrees with the network on every block it sees, permanently. Only an
    // absent row means a chain with no root yet.
    wire::Result<void> reload();

    void close() { committed_ = kEmptyId; }

private:
    explicit Root(store::Store& db) : db_(&db) {}

    store::Store* db_;
    Id committed_{};
};

}  // namespace lux::zkvm
