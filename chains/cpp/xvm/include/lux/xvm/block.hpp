// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.hpp — the X-Chain block.
//
// A block is a position (parent, height, time), a commitment to what executing
// it produced (root), and the transactions themselves. There is no codec: the
// bytes are authoritative and the id is sha256 of them, so a block that came off
// the wire and a block built locally are the same object when their bytes match.
//
// Transactions are self-describing, so the block stores a u32 list of per-tx
// byte lengths plus the concatenated tx bytes and re-splits them on parse.

#pragma once

#include "lux/xvm/id.hpp"
#include "lux/xvm/txs.hpp"

#include <memory>
#include <vector>

namespace lux::xvm::block {

template <class T>
using Result = wire::Result<T>;

struct StandardBlock {
    Id parent_id{};
    std::uint64_t height = 0;
    std::uint64_t time = 0;
    // The execution root this block commits to. Empty pre-activation: a block
    // that names no root is a block whose validators certify a name rather than
    // a result, which is exactly why the field is in the hashed bytes.
    Id root{};
    std::vector<std::shared_ptr<txs::Tx>> transactions;

    Id block_id{};
    Bytes bytes;
};

// build serializes the block and binds its id. root == kEmptyId is the
// pre-activation shape; above the activation height the builder passes the
// computed execution root so it is part of the hashed bytes.
Result<std::shared_ptr<StandardBlock>> build(const Id& parent_id, std::uint64_t height,
                                             std::uint64_t unix_time, const Id& root,
                                             std::vector<std::shared_ptr<txs::Tx>> txs);

// parse decodes a block, byte-preserving: id = sha256(bytes).
Result<std::shared_ptr<StandardBlock>> parse(ByteView bytes);

}  // namespace lux::xvm::block
