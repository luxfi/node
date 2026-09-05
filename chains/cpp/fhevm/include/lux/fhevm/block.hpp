// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.hpp — an F-Chain block: an ordered batch of fee-settled confidential-
// compute operations, and the three things that can happen to it.
//
// verify decides nothing about authorization. That verdict depends on state
// earlier transactions in the same block may change, so a block-time verdict
// can differ from the application-time one — and a block every validator
// certifies and no validator can apply halts the chain. Authorization is
// decided once, in accept, where failing it reverts the one transaction instead
// of the block.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/id.hpp"
#include "lux/fhevm/transaction.hpp"
#include "lux/node/vm.hpp"

#include <cstdint>
#include <memory>
#include <vector>

namespace lux::fhevm {

class VM;

// MaxBlockTxs bounds how many transactions one block carries. Without it a
// proposer's block size is whatever the mempool happens to hold.
inline constexpr std::size_t kMaxBlockTxs = 1024;

// MaxBlockSize bounds a block on the wire. Without it a peer decides how much
// this node parses, hashes and allocates before anything about the block has
// been checked: an 8 MB message carrying 1,524 transactions parsed to
// completion, because the transaction bound was applied by verify and verify
// runs after the parse. Both bounds belong at the first byte, and neither
// implies the other — this one bounds the bytes read, kMaxBlockTxs bounds the
// signatures verified, and a small block can still declare a great many tiny
// transactions.
inline constexpr std::size_t kMaxBlockSize = 2 << 20;  // 2 MiB

// kTxEntry is what one transaction costs a block BEYOND its own bytes: four for
// its entry in the length list, and up to four more of ZAP's eight-byte
// alignment. It is an upper bound by construction, which is the direction that
// matters — the builder adds it per selected transaction, so its running total
// can never come in under the block it describes and a proposer cannot select
// its way past a size its own verify refuses.
inline constexpr std::size_t kTxEntry = 8;

// kMaxFutureSkew is how far ahead of the verifying node's clock a block's
// timestamp may be. Chain time drives every expiry F enforces, so without a cap
// a proposer stamping year 36812 would expire every permit and every pending
// request at once.
inline constexpr std::int64_t kMaxFutureSkew = 60;

// BlockHeader is a block as it comes off the wire, before a VM is attached.
struct BlockHeader {
    Id parent{};
    std::uint64_t height = 0;
    std::int64_t timestamp = 0;
    std::vector<Transaction> transactions;
};

Bytes block_bytes(const Id& parent, std::uint64_t height, std::int64_t timestamp,
                  const std::vector<Transaction>& txs);
Result<BlockHeader> parse_block_bytes(ByteView data);

// empty_block_size is the wire cost of a block carrying no transactions: the
// ZAP header, the root object, and an empty length list.
std::size_t empty_block_size();

class Block final : public lux::node::Block, public std::enable_shared_from_this<Block> {
public:
    Block(VM* vm, Id parent, std::uint64_t height, std::int64_t timestamp,
          std::vector<Transaction> txs);

    // ---- the node's seam ----
    lux::node::Id id() const override;
    lux::node::Id parent() const override { return parent_; }
    std::uint64_t height() const override { return height_; }
    std::span<const std::uint8_t> bytes() const override;

    // F COMMITS NO STATE ROOT, and this is the one place that is visible. The
    // Go F-Chain implements no execution-root surface either: putting a root in
    // consensus needs a state layer per in-flight block, which this VM does not
    // have. Reporting a root the Go chain does not report would be the worse
    // answer — two implementations of one chain would hand consensus different
    // values for the same block. What stands in its place is the replay test,
    // which drives a chain onto independently-built nodes running different
    // clocks and requires their databases to be byte-identical; records_root()
    // on the VM is the digest it compares.
    lux::node::Id root() const override { return kEmptyId; }

    // verify runs this node's own structural and sequence checks. False is a
    // refusal to vote, never a crash; the reason is kept for the caller.
    bool verify() override;
    void accept() override;
    void reject() override;

    // ---- the same three, with their reasons ----
    Result<void> check() const;
    Result<void> accept_block();

    const std::string& error() const { return error_; }
    std::int64_t timestamp() const { return timestamp_; }
    const std::vector<Transaction>& transactions() const { return transactions_; }
    // status is 0 = processing, 1 = accepted.
    std::uint8_t status() const;

    Id compute_id() const;

private:
    Result<void> settle_and_apply(std::int64_t now);
    void abort();

    VM* vm_;
    Id parent_{};
    std::uint64_t height_ = 0;
    std::int64_t timestamp_ = 0;
    std::vector<Transaction> transactions_;
    mutable Id id_{};
    mutable bool id_cached_ = false;
    mutable Bytes bytes_;
    mutable bool bytes_cached_ = false;
    std::string error_;
};

}  // namespace lux::fhevm
