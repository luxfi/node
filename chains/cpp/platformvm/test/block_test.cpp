// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block_test.cpp — the block wire and its id, against the Go reference.
//
// Ported from Go vms/platformvm/block: blockwire_test.go (the four kinds round
// trip), block_determinism_test.go (one block, one id), dispatch_test.go (the
// visitor reaches the right arm) and parse.go's refusals. Every expected buffer
// and every expected id came out of the Go package itself.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/block.hpp"

#include <string>

using namespace lux::platformvm;
namespace blk = lux::platformvm::block;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

// The same envelope goldgen used, so the txs inside these blocks are the very
// buffers the transaction suite already pinned.
BaseTx base_fixture() {
    const Id asset = id_of(0x10);
    BaseTx b;
    b.network_id = 96369;
    b.blockchain_id = id_of(0x20);
    b.outs = {
        TransferableOutput{asset, 0,
                           TransferOutput{1'000'000, OutputOwners{7, 2, {short_of(0x30), short_of(0x40), short_of(0x50)}}}},
        TransferableOutput{asset, 999, TransferOutput{42, OutputOwners{0, 1, {short_of(0x30)}}}},
    };
    b.ins = {TransferableInput{UtxoId{id_of(0x60), 1}, asset, 999, TransferInput{1'000'042, {0, 1}}}};
    const std::string memo = "native-zap";
    b.memo.assign(memo.begin(), memo.end());
    return b;
}

txs::Tx sign_free(std::shared_ptr<txs::UnsignedTx> u) {
    txs::Tx t;
    t.unsigned_tx = std::move(u);
    (void)t.initialize();
    return t;
}

txs::Tx base_tx_fixture() {
    auto u = txs::BaseTxUnsigned::create(base_fixture());
    return sign_free(u.value());
}

txs::Tx import_tx_fixture() {
    std::vector<TransferableInput> imported = {
        TransferableInput{UtxoId{id_of(0x80), 3}, id_of(0x10), 0, TransferInput{500, {0}}}};
    auto u = txs::ImportTx::create(base_fixture(), id_of(0x70), imported);
    return sign_free(u.value());
}

txs::Tx reward_tx_fixture() { return sign_free(txs::RewardValidatorTx::create(id_of(0x7b))); }

constexpr std::uint64_t kTs = 1'600'000'000;

}  // namespace

// Go: block.NewAbortBlock — bytes and id.
TEST(AbortBlockWire) {
    auto b = blk::AbortBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(b);
    REQUIRE_EQ(std::string(pvmgold::abort_block), hex(b.value()->bytes()));
    REQUIRE_EQ(std::string(pvmgold::abort_block_id), hex(b.value()->id()));
    REQUIRE_EQ(id_of(0x20), b.value()->parent());
    REQUIRE_U64(7u, b.value()->height());
    REQUIRE_U64(kTs, b.value()->timestamp());
    REQUIRE(b.value()->decision_txs().empty());
}

// Go: block.NewCommitBlock. Abort and commit differ in ONE byte, and therefore
// in their whole id — which is the point: the two outcomes of one proposal must
// never be confusable.
TEST(CommitBlockWire) {
    auto b = blk::CommitBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(b);
    REQUIRE_EQ(std::string(pvmgold::commit_block), hex(b.value()->bytes()));
    REQUIRE_EQ(std::string(pvmgold::commit_block_id), hex(b.value()->id()));

    auto a = blk::AbortBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(a);
    REQUIRE(!(a.value()->id() == b.value()->id()));
}

// Go: block.NewStandardBlock with two decision txs.
TEST(StandardBlockWire) {
    const std::vector<txs::Tx> decisions = {base_tx_fixture(), import_tx_fixture()};
    auto b = blk::StandardBlock::create(kTs, id_of(0x20), 8, decisions);
    REQUIRE_OK(b);
    REQUIRE_EQ(std::string(pvmgold::standard_block), hex(b.value()->bytes()));
    REQUIRE_EQ(std::string(pvmgold::standard_block_id), hex(b.value()->id()));

    const auto back = b.value()->decision_txs();
    REQUIRE_EQ_NUM(2, back.size());
    REQUIRE_EQ(hex(decisions[0].bytes), hex(back[0].bytes));
    REQUIRE_EQ(hex(decisions[1].bytes), hex(back[1].bytes));
    REQUIRE_EQ(decisions[0].tx_id, back[0].tx_id);
    REQUIRE_EQ(decisions[1].tx_id, back[1].tx_id);
}

TEST(StandardBlockEmptyWire) {
    auto b = blk::StandardBlock::create(kTs, id_of(0x20), 9, {});
    REQUIRE_OK(b);
    REQUIRE_EQ(std::string(pvmgold::standard_block_empty), hex(b.value()->bytes()));
    REQUIRE(b.value()->decision_txs().empty());
}

// Go: block.NewProposalBlock — the proposal tx plus a tail of decision txs.
TEST(ProposalBlockWire) {
    const std::vector<txs::Tx> decisions = {base_tx_fixture()};
    const auto proposal = reward_tx_fixture();
    auto b = blk::ProposalBlock::create(kTs, id_of(0x20), 10, proposal, decisions);
    REQUIRE_OK(b);
    REQUIRE_EQ(std::string(pvmgold::proposal_block), hex(b.value()->bytes()));
    REQUIRE_EQ(std::string(pvmgold::proposal_block_id), hex(b.value()->id()));

    auto tx = b.value()->tx();
    REQUIRE_OK(tx);
    REQUIRE_EQ(proposal.tx_id, tx.value().tx_id);
    REQUIRE(tx.value().unsigned_tx->kind() == txs::Kind::RewardValidator);

    const auto back = b.value()->decision_txs();
    REQUIRE_EQ_NUM(1, back.size());
    REQUIRE_EQ(decisions[0].tx_id, back[0].tx_id);
}

TEST(ProposalBlockNoDecisionsWire) {
    auto b = blk::ProposalBlock::create(kTs, id_of(0x20), 11, reward_tx_fixture(), {});
    REQUIRE_OK(b);
    REQUIRE_EQ(std::string(pvmgold::proposal_block_no_decisions), hex(b.value()->bytes()));
    REQUIRE(b.value()->decision_txs().empty());
    REQUIRE_OK(b.value()->tx());
}

// Go: block.Parse — the dispatch returns the concrete type, and the id survives
// the round trip because the bytes were never re-encoded.
TEST(ParseRoundTripsEveryKind) {
    const std::vector<txs::Tx> decisions = {base_tx_fixture(), import_tx_fixture()};

    auto a = blk::AbortBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(a);
    auto pa = blk::parse(a.value()->bytes());
    REQUIRE_OK(pa);
    REQUIRE(pa.value()->kind() == blk::Kind::Abort);
    REQUIRE_EQ(a.value()->id(), pa.value()->id());

    auto c = blk::CommitBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(c);
    auto pc = blk::parse(c.value()->bytes());
    REQUIRE_OK(pc);
    REQUIRE(pc.value()->kind() == blk::Kind::Commit);
    REQUIRE_EQ(c.value()->id(), pc.value()->id());

    auto s = blk::StandardBlock::create(kTs, id_of(0x20), 8, decisions);
    REQUIRE_OK(s);
    auto ps = blk::parse(s.value()->bytes());
    REQUIRE_OK(ps);
    REQUIRE(ps.value()->kind() == blk::Kind::Standard);
    REQUIRE_EQ(s.value()->id(), ps.value()->id());
    REQUIRE_EQ_NUM(2, ps.value()->decision_txs().size());

    auto p = blk::ProposalBlock::create(kTs, id_of(0x20), 10, reward_tx_fixture(), decisions);
    REQUIRE_OK(p);
    auto pp = blk::parse(p.value()->bytes());
    REQUIRE_OK(pp);
    REQUIRE(pp.value()->kind() == blk::Kind::Proposal);
    REQUIRE_EQ(p.value()->id(), pp.value()->id());
    REQUIRE_EQ_NUM(2, pp.value()->decision_txs().size());
}

// Go: block_determinism_test — the same fields build the same bytes and the
// same id, every time.
TEST(BuildIsDeterministic) {
    const std::vector<txs::Tx> decisions = {base_tx_fixture(), import_tx_fixture()};
    auto a = blk::StandardBlock::create(kTs, id_of(0x20), 8, decisions);
    auto b = blk::StandardBlock::create(kTs, id_of(0x20), 8, decisions);
    REQUIRE_OK(a);
    REQUIRE_OK(b);
    REQUIRE_EQ(hex(a.value()->bytes()), hex(b.value()->bytes()));
    REQUIRE_EQ(a.value()->id(), b.value()->id());
}

// Go: block.ErrExtraSpace — a trailing byte changes the id while wrapping the
// same message, so it is refused rather than silently accepted.
TEST(ParseRejectsTrailingBytes) {
    auto a = blk::AbortBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(a);
    std::vector<std::uint8_t> b(a.value()->bytes().begin(), a.value()->bytes().end());
    b.push_back(0);
    REQUIRE_ERR(blk::parse(b), Err::BlockExtraSpace);
}

// Go: block.Parse default arm — slot 0 and any unnamed slot are refused.
TEST(ParseRejectsUnknownKind) {
    auto a = blk::AbortBlock::create(kTs, id_of(0x20), 7);
    REQUIRE_OK(a);
    std::vector<std::uint8_t> b(a.value()->bytes().begin(), a.value()->bytes().end());
    const std::uint32_t root = static_cast<std::uint32_t>(b[8]) | (static_cast<std::uint32_t>(b[9]) << 8) |
                               (static_cast<std::uint32_t>(b[10]) << 16) |
                               (static_cast<std::uint32_t>(b[11]) << 24);
    b[root] = 0;
    REQUIRE_ERR(blk::parse(b), Err::UnknownBlockKind);
    b[root] = 9;
    REQUIRE_ERR(blk::parse(b), Err::UnknownBlockKind);
}

// Go: block.errNoProposalTx — a proposal block without a proposal is refused
// where it enters, not where it is dereferenced.
TEST(ProposalBlockWithoutProposalIsRefused) {
    // Built: a standard block's buffer relabelled as a proposal leaves the
    // proposal slot outside the object, so it reads back empty.
    auto s = blk::StandardBlock::create(kTs, id_of(0x20), 8, {base_tx_fixture()});
    REQUIRE_OK(s);
    std::vector<std::uint8_t> b(s.value()->bytes().begin(), s.value()->bytes().end());
    const std::uint32_t root = static_cast<std::uint32_t>(b[8]) | (static_cast<std::uint32_t>(b[9]) << 8) |
                               (static_cast<std::uint32_t>(b[10]) << 16) |
                               (static_cast<std::uint32_t>(b[11]) << 24);
    b[root] = static_cast<std::uint8_t>(blk::Kind::Proposal);
    REQUIRE_ERR(blk::parse(b), Err::NoProposalTx);

    // And a block that carries no proposal tx cannot be built at all.
    txs::Tx empty;
    REQUIRE_ERR(blk::ProposalBlock::create(kTs, id_of(0x20), 10, empty, {}), Err::NoProposalTx);
}

// Go: block.readTxList bounds check — a length that overruns the blob is a
// refusal, never a read past the end.
TEST(ParseRejectsTxLengthOverrunningBlob) {
    auto s = blk::StandardBlock::create(kTs, id_of(0x20), 8, {base_tx_fixture()});
    REQUIRE_OK(s);
    std::vector<std::uint8_t> b(s.value()->bytes().begin(), s.value()->bytes().end());
    const std::int64_t root = static_cast<std::int64_t>(
        static_cast<std::uint32_t>(b[8]) | (static_cast<std::uint32_t>(b[9]) << 8) |
        (static_cast<std::uint32_t>(b[10]) << 16) | (static_cast<std::uint32_t>(b[11]) << 24));
    // The length list is a backward pointer from the object's tx_lengths slot.
    const std::int64_t slot = root + wire::kStandardTxLengthsOff;
    const std::int32_t rel = static_cast<std::int32_t>(
        static_cast<std::uint32_t>(b[slot]) | (static_cast<std::uint32_t>(b[slot + 1]) << 8) |
        (static_cast<std::uint32_t>(b[slot + 2]) << 16) | (static_cast<std::uint32_t>(b[slot + 3]) << 24));
    const std::int64_t list = slot + rel;
    b[list] = 0xff;  // first tx length = huge
    b[list + 1] = 0xff;
    REQUIRE_ERR(blk::parse(b), Err::TxOverrunsBlob);
}

// Go: block/dispatch_test — the visitor reaches exactly one arm per kind.
namespace {
struct CountingVisitor final : blk::Visitor {
    int aborts = 0, commits = 0, proposals = 0, standards = 0;
    Status abort_block(const blk::AbortBlock&) override {
        ++aborts;
        return ok();
    }
    Status commit_block(const blk::CommitBlock&) override {
        ++commits;
        return ok();
    }
    Status proposal_block(const blk::ProposalBlock&) override {
        ++proposals;
        return ok();
    }
    Status standard_block(const blk::StandardBlock&) override {
        ++standards;
        return ok();
    }
};
}  // namespace

TEST(VisitorDispatch) {
    CountingVisitor v;
    auto a = blk::AbortBlock::create(kTs, id_of(0x20), 7);
    auto c = blk::CommitBlock::create(kTs, id_of(0x20), 7);
    auto s = blk::StandardBlock::create(kTs, id_of(0x20), 8, {base_tx_fixture()});
    auto p = blk::ProposalBlock::create(kTs, id_of(0x20), 10, reward_tx_fixture(), {});
    REQUIRE_OK(a);
    REQUIRE_OK(c);
    REQUIRE_OK(s);
    REQUIRE_OK(p);
    REQUIRE_OK(a.value()->visit(v));
    REQUIRE_OK(c.value()->visit(v));
    REQUIRE_OK(s.value()->visit(v));
    REQUIRE_OK(p.value()->visit(v));
    REQUIRE_EQ_NUM(1, v.aborts);
    REQUIRE_EQ_NUM(1, v.commits);
    REQUIRE_EQ_NUM(1, v.standards);
    REQUIRE_EQ_NUM(1, v.proposals);

    // And through a parsed block, where the concrete type came off the wire.
    CountingVisitor w;
    auto pp = blk::parse(p.value()->bytes());
    REQUIRE_OK(pp);
    REQUIRE_OK(pp.value()->visit(w));
    REQUIRE_EQ_NUM(1, w.proposals);
    REQUIRE_EQ_NUM(0, w.standards);
}
