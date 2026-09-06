// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block.hpp — the Z-Chain's linear block, and what accepting one means.
//
// There is NO aggregate block proof. What stood in that place was carried on the
// wire, hashed into the id, and "verified" by re-verifying the very transactions
// verify had already verified one line above — a second implementation of a
// check, standing where a reader would take an aggregate guarantee to be.
//
// A block implements BOTH seams at once: the node's Block (what consensus reads:
// where it sits, what its execution produced, this node's own verdict) and the
// chain store's Decision (how it becomes fact). They are the same object because
// they describe the same thing, and a wrapper between them would be a second
// place for the two to disagree about a height.

#pragma once

#include "lux/node/vm.hpp"

#include "lux/zkvm/chainstore.hpp"
#include "lux/zkvm/id.hpp"
#include "lux/zkvm/txs.hpp"
#include "lux/zkvm/wire.hpp"

#include <memory>
#include <string>
#include <vector>

namespace lux::zkvm {

class Vm;

inline constexpr std::int64_t kMaxClockSkew = 60;  // seconds

inline constexpr const char* kErrInvalidBlock = "invalid block";
inline constexpr const char* kErrFutureBlock = "block timestamp too far in future";
inline constexpr const char* kErrInvalidHeight = "invalid block height";
inline constexpr const char* kErrInvalidTimestamp = "invalid block timestamp";
inline constexpr const char* kErrInvalidStateRoot = "invalid state root";
// The same nullifier appears twice inside one block or vertex, i.e. one shielded
// note spent twice. Fail closed.
inline constexpr const char* kErrDuplicateNullifier = "nullifier spent twice in one block";
inline constexpr const char* kErrNoTransactions = "zkvm: nothing to propose";

class Block final : public lux::node::Block,
                    public Decision,
                    public std::enable_shared_from_this<Block> {
public:
    Block() = default;

    Id parent_id{};
    std::uint64_t block_height = 0;
    std::int64_t block_timestamp = 0;
    std::vector<Transaction> txs;
    Bytes state_root;

    // ---- the node's seam ----
    lux::node::Id id() const override;
    lux::node::Id parent() const override { return parent_id; }
    std::uint64_t height() const override { return block_height; }
    std::span<const std::uint8_t> bytes() const override;
    lux::node::Id root() const override;

    // verify is this node's OWN verdict. False is a refusal to vote, never a
    // crash; the reason is kept for the caller that wants it.
    bool verify() override;
    void accept() override;

    // reject is the OTHER half of being decided, and it is not optional.
    // Consensus chose a sibling; this block will never be accepted. Its
    // transactions were never refused — they lost a race — so they go back into
    // the pool. A chain that dropped them would disagree with every other node
    // about what is still pending.
    //
    // DECIDED ONCE, either way: accept and reject are each inert after the
    // other, which is the seam's own contract. More than one path can reach a
    // decision, and here the second one would be destructive rather than
    // redundant — rejecting a block this chain already accepted returns
    // transactions whose notes are spent to the pool, so every later block this
    // node assembles carries a double spend its own peers refuse. Go guards the
    // same double-decide one layer up, in the engine's pendingBlocks.Decided.
    void reject() override;

    // ---- the chain store's seam ----
    wire::Result<void> write(store::Store& view) override;
    void publish() override;

    // ---- the same two, with their reasons ----
    //
    // check is what verify asks. accept's durable half is the store's, so commit
    // is what accept asks — a caller that wants the reason for a failed accept
    // uses commit, and a caller that only votes uses the seam's two.
    wire::Result<void> check();
    wire::Result<void> commit();

    // syntactic_verify is check's first half: the rules a node settles from the
    // block in hand — the genesis/parent pairing, the transaction cap, the
    // clock, a nullifier repeated inside the block, and each transaction's own
    // shape and expiry against the height this block claims. It reads no key,
    // looks up no parent and asks the spent set nothing, so a block that cannot
    // be true of ANY chain is refused before this one is touched.
    //
    // The boundary is check's own: every line below the call to this asks the
    // ledger something. It has a name because it was already there without one,
    // as an ordering property — and three ports of this chain each had to work
    // out where it fell, and worked it out differently.
    wire::Result<void> syntactic_verify() const;

    Status status() const { return status_; }
    const std::string& error() const { return error_; }

    // compute_id opens with the chain's binding — sha256(ChainID ‖ NetworkID),
    // which is NOT on the wire — so the same bytes name a different block on a
    // different chain. Two chains with different ids and an identical genesis
    // config would otherwise derive the same genesis id, and one chain's blocks
    // would then chain onto the other's verbatim.
    //
    // Each transaction contributes compute_id() rather than its carried id: an
    // id is a function of content, and a block whose identity depended on an id
    // a peer supplied would have as many identities as the peer cared to send.
    Id compute_id() const;

    Bytes marshal() const;

    void bind_vm(Vm& vm) { vm_ = &vm; }
    void set_id(const Id& id) { id_ = id; }
    void set_bytes(Bytes b) { bytes_ = std::move(b); }

private:
    Vm* vm_ = nullptr;
    mutable Id id_{};
    mutable Bytes bytes_;
    Status status_ = Status::Processing;
    std::string error_;
};

// parse_block_bytes decodes a block frame. The identity is NOT read from it.
wire::Result<void> parse_block_bytes(ByteView data, Block& blk);

inline constexpr int kBlkSize = 72;

// Genesis is what the chain starts from.
//
// The Go host hands this in as JSON, which is a HOST CONFIGURATION encoding, not
// a consensus one: what the genesis block's identity binds is the timestamp and
// the initial transactions, never the buffer they arrived in. So the port reads
// the same values from the chain's own serialization, and a Go node and this one
// configured with the same values reach the same genesis id.
//
// A genesis that names no timestamp is stamped 0, not "now". The timestamp is
// hashed into the genesis block id, so reading the wall clock here would give
// every node a different genesis id — a different chain — for the same genesis
// file, and a different one again after each restart.
struct Genesis {
    std::int64_t timestamp = 0;
    std::vector<Transaction> initial_txs;

    Bytes marshal() const;
};

wire::Result<Genesis> parse_genesis(ByteView data);

struct BlockSummary {
    Id id{};
    std::uint64_t height = 0;
    std::int64_t timestamp = 0;
    std::size_t tx_count = 0;
    Bytes state_root;
};

BlockSummary summarize(const Block& b);

}  // namespace lux::zkvm
