// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_wire.hpp — arranging this chain's values into the runs the wire holds
// them in, and reading them back out.
//
// There is not an offset in this file. Every offset, every stride and every
// accessor the P-chain has comes out of schema/wire.zap through zapgen and
// lands in gen/wire_zap.hpp, in this same namespace. What is left here is the
// ARRANGEMENT, which is a fact about this chain rather than about the format:
// an output's owner addresses do not live in the output, they live in one run
// shared by the whole transaction, and the output names a slice of it. That is
// what lets a transaction be read without walking it, and it is a decision
// this chain made — so it is stated here, once, rather than repeated at the
// nineteen places a transaction is built.
//
// Each concept has one name and two directions: outs() takes outputs and gives
// the run, or takes the run and gives the outputs.

#pragma once

#include "lux/platformvm/gen/wire_zap.hpp"
#include "lux/platformvm/txs.hpp"

#include <array>
#include <cstdint>
#include <span>
#include <vector>

namespace lux::platformvm::wire {

using Addr = std::array<std::uint8_t, kShortIdLen>;

// An output list and the address run its owners were pooled into.
struct OutRun {
    std::vector<OutInput> list;
    std::vector<Addr> addrs;
};

// An input list and the signature-index run its inputs were pooled into.
struct InRun {
    std::vector<InInput> list;
    std::vector<std::uint32_t> sigs;
};

// A genesis validator list and the two pools it names runs in: one blob of
// node ids, whose lengths differ, and one array of owner addresses.
struct ValidatorRun {
    std::vector<NetworkValidatorInput> list;
    std::vector<std::uint8_t> node_ids;
    std::vector<Addr> addrs;
};

// The four spending fields every envelope opens with.
struct Spend {
    std::vector<OutInput> outs;
    std::vector<Addr> owner_addrs;
    std::vector<InInput> ins;
    std::vector<std::uint32_t> sig_indices;
};

// ── this chain's values, arranged for the wire

std::vector<Addr> addrs(const std::vector<ShortId>& in);
OutRun outs(const std::vector<TransferableOutput>& in);
InRun ins(const std::vector<TransferableInput>& in);
ValidatorRun validators(const std::vector<txs::NetworkValidator>& in);
Spend spend(const BaseTx& base);

// ── the wire's runs, read back as this chain's values
//
// A run a record names is clamped to the run that is actually there: a record
// claiming addresses past the end of the array holds none, which is the same
// answer a reader gives for any other read past the end.

std::vector<TransferableOutput> outs(const zap::List& list, const zap::List& pool);
std::vector<TransferableInput> ins(const zap::List& list, const zap::List& pool);
std::vector<txs::NetworkValidator> validators(const zap::List& list, std::span<const std::uint8_t> node_ids,
                                              const zap::List& pool);
txs::Owner owner(std::uint32_t threshold, std::uint64_t locktime, const zap::List& pool);
txs::Auth auth(const zap::List& pool);
txs::Validator validator(std::span<const std::uint8_t> node_id, std::uint64_t start, std::uint64_t end,
                         std::uint64_t weight);

}  // namespace lux::platformvm::wire
