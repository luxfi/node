// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.hpp — a Q-chain block, and the rules that decide whether it may be
// built on.
//
// Rendered from Go chains/quantumvm/block.go.
//
// The shape of the thing:
//
//   verify   decides whether the block may be built on, and changes NOTHING.
//            It sits on its parent in both height and time, it belongs to this
//            chain, and every transaction's ML-DSA signature checks out.
//   accept   admits the block, applies it and persists it as ONE step
//            (vm.commit_block), and only then are its transactions settled.
//   reject   discards it. Nothing ran — execution belongs to accept — and its
//            transactions are still in the mempool, so there is nothing to undo
//            and nothing to give back.
//
// A proposed block carries work. Refusing an empty one is also what keeps the
// signature check from being satisfiable by removing its subject: a parser that
// dropped the transaction set produces a block that verifies nothing, and a
// block that verifies nothing must not verify.

#pragma once

#include "lux/quantumvm/clock.hpp"
#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/wire.hpp"

#include <chrono>
#include <cstdint>
#include <memory>
#include <string>

namespace lux::quantumvm {

class QuantumVM;

// MaxFutureSkew is how far ahead of the verifying node's clock a proposer may
// stamp a block. Peers' clocks differ, and this is the allowance for that; an
// uncapped timestamp is a proposer writing chain time, which is what decides
// whether the NEXT block may be stamped at all.
inline constexpr Seconds kMaxFutureSkewSeconds = 60;

class Block {
  public:
    Block(QuantumVM* vm, wire::BlockFields fields) : vm_(vm), f_(std::move(fields)) {}

    // The id is the content hash of the canonical wire and is NOT stored in it,
    // so `id() == sha256(bytes())` holds unconditionally.
    const Id& id() const;
    const Id& parent_id() const { return f_.parent_id; }
    const Id& chain_id() const { return f_.chain_id; }
    std::uint32_t network_id() const { return f_.network_id; }
    std::uint64_t height() const { return f_.height; }
    Seconds timestamp() const { return f_.timestamp; }
    const std::vector<TxPtr>& transactions() const { return f_.transactions; }
    ByteView bytes() const;

    // The state this block's execution PRODUCES: the commitment to the rows
    // accepting it writes — the block itself, its height index entry and the
    // tip the chain moves to. It is computed by running the arithmetic of the
    // commit, never copied from a proposer, which is what makes it something a
    // validator can sign.
    Id execution_root() const;

    Status verify() const;
    Status accept();
    Status reject() const;

    // 0 while the block is only proposed and 1 once it is stored. A stored
    // block is an accepted one: accept is the only writer, and it commits the
    // block and the tip pointer together.
    std::uint8_t status() const;

    // Refuses a block that names another chain or another network. Nothing else
    // in the wire is chain-specific and genesis is a constant, so without this
    // every Q-Chain in existence shares a genesis id and a block built on one is
    // a well-formed block on all of them.
    Status on_this_chain() const;

    // Runs the block's transactions. They run where the block becomes the tip,
    // on every node, exactly once. A transaction that cannot be applied stops
    // the block: a node that cannot apply an agreed block stops rather than
    // committing a chain its state no longer matches.
    Status apply() const;

    const wire::BlockFields& fields() const { return f_; }
    QuantumVM* vm() const { return vm_; }

    // For a block assembled field by field (a test, or the genesis seed): the
    // cached wire and id are dropped so the next read derives them again.
    void restate(wire::BlockFields fields) {
        f_ = std::move(fields);
        wire_built_ = false;
        id_.reset();
    }

  private:
    QuantumVM* vm_ = nullptr;
    wire::BlockFields f_;

    mutable Bytes wire_;
    mutable bool wire_built_ = false;
    mutable std::optional<Id> id_;
};

using BlockPtr = std::shared_ptr<Block>;

}  // namespace lux::quantumvm
