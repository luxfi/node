// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.hpp — the four blocks the P-chain has, and their wire.
//
// Rendered from Go vms/platformvm/block (blockwire.go, block.go, parse.go and
// the four *_block.go files). As with a transaction, THE STRUCT IS THE WIRE: a
// block holds its ZAP buffer and reads its fields by offset, and its id is
// sha256 of exactly those bytes.
//
// Object fixed section (object-relative offsets):
//
//     kind        u8  @ 0    abort=1, commit=2, proposal=3, standard=4
//     parent      32B @ 1
//     height      u64 @ 33
//     time        u64 @ 41
//     tx_lengths  8B  @ 49   u32 list — one entry per decision tx
//     tx_blob     8B  @ 57   the decision txs' bytes, concatenated
//     proposal_tx 8B  @ 65   the single proposal tx's bytes
//
// Sizes: abort/commit 49, standard 65, proposal 73.
//
// TRAILING BYTES ARE REFUSED. A ZAP message is self-delimiting, so a buffer
// with junk after it wraps the same message but hashes to a different id. Two
// ids for one block is two blocks, so the extra bytes are rejected where the
// block enters — built or parsed — rather than surviving into a chain that has
// to decide later which id was real.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstdint>
#include <memory>
#include <span>
#include <vector>

namespace lux::platformvm::block {

// The discriminator. Slot 0 names nothing, so a zeroed buffer is not a block.
enum class Kind : std::uint8_t {
    Abort = 1,
    Commit = 2,
    Proposal = 3,
    Standard = 4,
};

inline constexpr std::int64_t kOffKind = 0;
inline constexpr std::int64_t kOffParent = 1;
inline constexpr std::int64_t kOffHeight = 33;
inline constexpr std::int64_t kOffTime = 41;
inline constexpr std::int64_t kOffTxLengths = 49;
inline constexpr std::int64_t kOffTxBlob = 57;
inline constexpr std::int64_t kOffProposalTx = 65;

inline constexpr std::int64_t kSizeDecided = 49;
inline constexpr std::int64_t kSizeStandard = 65;
inline constexpr std::int64_t kSizeProposal = 73;

inline constexpr std::int64_t kTxLenStride = 4;

class Visitor;

// Wire is the single place a block's bytes are bound and its id derived. It is a
// friend rather than a public method because binding bytes is what MAKES a
// block: doing it twice, or to a block that already exists, would give one
// block two ids.
struct Wire;

// A block. Every kind carries a timestamp; the two decided kinds carry no txs,
// the standard kind carries a list, and the proposal kind carries a list plus
// one proposal tx that commits atomically with them.
class Block {
  public:
    virtual ~Block() = default;

    virtual Kind kind() const = 0;
    virtual Status visit(Visitor& v) const = 0;

    Id id() const { return id_; }
    std::span<const std::uint8_t> bytes() const { return {buf_.data(), buf_.size()}; }
    Id parent() const;
    std::uint64_t height() const;
    // Seconds since the epoch, as the wire carries it.
    std::uint64_t timestamp() const;

    // The transactions this block charges for: the ones someone else submitted.
    // A proposal block's OWN tx is never among them — it is reached through
    // ProposalBlock::tx(), typed and singular, so it cannot join a list on its
    // way to something that prices or re-issues transactions.
    virtual std::vector<txs::Tx> decision_txs() const { return {}; }

  protected:
    friend struct Wire;

    Status set_bytes(std::vector<std::uint8_t> b);
    zap::Object root() const;

    std::vector<std::uint8_t> buf_;
    Id id_{};
};

class AbortBlock final : public Block {
  public:
    static Result<std::shared_ptr<AbortBlock>> create(std::uint64_t timestamp, const Id& parent,
                                                      std::uint64_t height);
    Kind kind() const override { return Kind::Abort; }
    Status visit(Visitor& v) const override;
};

class CommitBlock final : public Block {
  public:
    static Result<std::shared_ptr<CommitBlock>> create(std::uint64_t timestamp, const Id& parent,
                                                       std::uint64_t height);
    Kind kind() const override { return Kind::Commit; }
    Status visit(Visitor& v) const override;
};

class StandardBlock final : public Block {
  public:
    static Result<std::shared_ptr<StandardBlock>> create(std::uint64_t timestamp, const Id& parent,
                                                         std::uint64_t height,
                                                         const std::vector<txs::Tx>& decision_txs);
    Kind kind() const override { return Kind::Standard; }
    Status visit(Visitor& v) const override;
    std::vector<txs::Tx> decision_txs() const override;
};

class ProposalBlock final : public Block {
  public:
    static Result<std::shared_ptr<ProposalBlock>> create(std::uint64_t timestamp, const Id& parent,
                                                         std::uint64_t height, const txs::Tx& proposal_tx,
                                                         const std::vector<txs::Tx>& decision_txs);
    Kind kind() const override { return Kind::Proposal; }
    Status visit(Visitor& v) const override;
    std::vector<txs::Tx> decision_txs() const override;

    // The proposal tx. Never absent on a block that exists: a block is only made
    // with a proposal slot that carries bytes, and only parsed if that slot
    // reads back.
    Result<txs::Tx> tx() const;
};

class Visitor {
  public:
    virtual ~Visitor() = default;
    virtual Status abort_block(const AbortBlock&) = 0;
    virtual Status commit_block(const CommitBlock&) = 0;
    virtual Status proposal_block(const ProposalBlock&) = 0;
    virtual Status standard_block(const StandardBlock&) = 0;
};

// Decode a block: read the kind and wrap the matching type over b, byte for
// byte. Nothing is re-marshalled, so the id is sha256 of what arrived.
Result<std::shared_ptr<Block>> parse(std::span<const std::uint8_t> b);

}  // namespace lux::platformvm::block
