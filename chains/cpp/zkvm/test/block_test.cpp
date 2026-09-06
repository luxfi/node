// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// block_test.cpp — verify, accept and reject over the node's seam.
//
// The chain under test is a real strict-PQ Z-Chain with its STARK/FRI verifier
// BOUND, which is the profile the Z-Chain ships in. Nothing here is a chain with
// its gate removed.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/vm.hpp"

#include <memory>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

constexpr std::int64_t kNow = 1'700'000'000;

struct Chain {
    store::Memory base;
    std::unique_ptr<Vm> vm;

    explicit Chain(VmConfig cfg = strict_config()) {
        vm = std::make_unique<Vm>(cfg, base);
        vm->set_now(kNow);
        auto r = vm->initialize(Genesis{});
        check_ok(r, "the chain comes up");
    }
};

std::shared_ptr<Block> block_over(Vm& vm, const Id& parent, std::uint64_t height,
                                  std::int64_t ts, std::vector<Transaction> txs) {
    auto blk = std::make_shared<Block>();
    blk->bind_vm(vm);
    blk->parent_id = parent;
    blk->block_height = height;
    blk->block_timestamp = ts;
    blk->txs = std::move(txs);
    const Id root = vm.compute_state_root(blk->txs);
    blk->state_root.assign(root.begin(), root.end());
    blk->set_id(blk->compute_id());
    return blk;
}

void a_block_is_built_verified_and_accepted() {
    std::printf("build → verify → accept, over the node's seam\n");
    bind_accepting_stark_verifier();
    Chain c;

    check_err(c.vm->build_block(), kErrNoTransactions, "an empty pool proposes nothing");

    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x11);
    check_ok(c.vm->issue(tx), "a transaction comes in the door");

    auto built = c.vm->build_block();
    check_ok(built, "a block is built");
    if (!built) return;
    check((*built)->block_height == 1, "at height 1");
    check((*built)->parent() == c.vm->genesis_block()->id(), "on the genesis block");
    check((*built)->txs.size() == 1, "carrying the transaction");

    check((*built)->verify(), "this node's own execution accepts it");
    check_eq(hex_of((*built)->root()), hex_of(c.vm->compute_state_root((*built)->txs)),
             "and the block says what its execution produced");

    (*built)->accept();
    check((*built)->status() == Status::Accepted, "it is decided");
    check_eq(hex_of(c.vm->last_accepted()), hex_of((*built)->id()), "and it is the tip");
    check(c.vm->last_accepted_height() == 1, "at height 1");
    check(c.vm->nullifiers().count() == 1, "the note it spent is spent");
    check(c.vm->utxos().count() == 1, "the note it created exists");
    check(c.vm->mempool().size() == 0, "and the pool has released it");
    unbind_stark_verifier();
}

void verify_refusals() {
    std::printf("\nevery refusal verify can reach\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Id genesis = c.vm->genesis_block()->id();
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x22);

    // A block at height 0 naming a parent is not a genesis block.
    auto rooted = block_over(*c.vm, id_of(1), 0, kNow, {});
    check_err(rooted->check(), kErrInvalidBlock, "height 0 with a parent is refused");

    // A block off the wire is held to the bound a block this node builds is held
    // to, so a proposer cannot produce one its own peers refuse.
    VmConfig tiny = strict_config();
    tiny.z.max_tx_per_block = 1;
    Chain small(tiny);
    auto over_cap = block_over(*small.vm, small.vm->genesis_block()->id(), 1, kNow,
                               {stark_tx(kMinTxFeeFloor, 1), stark_tx(kMinTxFeeFloor, 2)});
    check_err(over_cap->check(), "over the 1 cap", "a block over the per-block cap is refused");

    auto future = block_over(*c.vm, genesis, 1, kNow + kMaxClockSkew + 10, {tx});
    check_err(future->check(), kErrFutureBlock, "a block too far in the future is refused");

    // Two transactions in one block spending the same note — or one transaction
    // listing a nullifier twice — inflate supply, and the accepted-state check
    // cannot see either.
    Transaction twin = tx;
    twin.memo = b("twin");
    twin.id = twin.compute_id();
    auto doubled = block_over(*c.vm, genesis, 1, kNow, {tx, twin});
    check_err(doubled->check(), kErrDuplicateNullifier,
              "one note spent twice in one block is refused");

    Transaction self_double = tx;
    self_double.nullifiers.push_back(tx.nullifiers[0]);
    self_double.id = self_double.compute_id();
    auto self = block_over(*c.vm, genesis, 1, kNow, {self_double});
    check_err(self->check(), kErrDuplicateNullifier,
              "and so is one transaction listing a nullifier twice");

    auto wrong_height = block_over(*c.vm, genesis, 2, kNow, {tx});
    check_err(wrong_height->check(), kErrInvalidHeight, "a height that skips is refused");

    auto missing_parent = block_over(*c.vm, id_of(0xAB), 1, kNow, {tx});
    check_err(missing_parent->check(), kErrNoBlock, "a parent nobody has is refused");

    // The state root is computed by RUNNING the block, never copied from a
    // proposer.
    auto lying = block_over(*c.vm, genesis, 1, kNow, {tx});
    lying->state_root = Bytes(32, 0xAA);
    lying->set_id(lying->compute_id());
    check_err(lying->check(), kErrInvalidStateRoot, "a state root the block did not produce is refused");

    // The one predicate: what verify refuses, assembly refuses too.
    Transaction expired = stark_tx(kMinTxFeeFloor, 0x33, 0);
    auto stale = block_over(*c.vm, genesis, 1, kNow, {expired});
    check_err(stale->check(), kErrNoExpiry, "a transaction naming no expiry takes the block with it");

    unbind_stark_verifier();
}

void a_block_only_extends_something_it_can_build_on() {
    std::printf("\na parent beneath the tip is not a parent\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Id genesis = c.vm->genesis_block()->id();

    auto first = block_over(*c.vm, genesis, 1, kNow, {stark_tx(kMinTxFeeFloor, 0x44)});
    check_ok(first->check(), "the first block verifies");
    check_ok(first->commit(), "and is accepted");

    // Height alone is not a place. A block whose parent is an OLD accepted block
    // satisfies height == parent+1 perfectly well, and accepting it rewinds the
    // tip and leaves the height index naming an orphan as the block at that
    // height to every peer that bootstraps from it.
    auto rewind = block_over(*c.vm, genesis, 1, kNow, {stark_tx(kMinTxFeeFloor, 0x55)});
    check_err(rewind->check(), kErrNotOnTip, "a block on the old tip is refused");

    // And the store refuses it again at the commit, under the lock that commits
    // — the tip moves between verify and accept.
    check_err(rewind->commit(), kErrNotOnTip, "and the store refuses it again at the commit");
    unbind_stark_verifier();
}

void a_verified_block_is_findable_as_a_parent() {
    std::printf("\na block that verifies can be built on, even one from a peer\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Id genesis = c.vm->genesis_block()->id();

    auto first = block_over(*c.vm, genesis, 1, kNow, {stark_tx(kMinTxFeeFloor, 0x66)});
    const Bytes raw(first->bytes().begin(), first->bytes().end());

    // Parsed from a peer, not built here. Tracking only self-built blocks leaves
    // a follower able to verify the first block of a run and unable to verify
    // the second.
    auto peer = c.vm->parse_block(view(raw));
    check_ok(peer, "a peer's block parses");
    if (!peer) return;
    check_eq(hex_of((*peer)->id()), hex_of(first->id()), "to the same identity");
    check_ok((*peer)->check(), "and verifies");

    auto child = block_over(*c.vm, (*peer)->id(), 2, kNow, {stark_tx(kMinTxFeeFloor, 0x77)});
    check_ok(child->check(), "a child of it verifies too");
    unbind_stark_verifier();
}

void a_rejected_block_returns_its_transactions() {
    std::printf("\na rejected block gives its transactions back\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x88);
    check_ok(c.vm->issue(tx), "a transaction is pending");

    auto built = c.vm->build_block();
    check_ok(built, "a block is built around it");
    if (!built) return;
    check((*built)->verify(), "and verifies");
    c.vm->mempool().remove(tx.id);
    check(c.vm->mempool().size() == 0, "the pool is emptied for the test");

    // Consensus chose a sibling. The transactions were never refused — they lost
    // a race — so a chain that dropped them would disagree with every other node
    // about what is still pending.
    (*built)->reject();
    check((*built)->status() == Status::Rejected, "the block is rejected");
    check(c.vm->mempool().size() == 1, "and its transaction is pending again");
    check(c.vm->mempool().has(tx.id), "the same transaction");
    unbind_stark_verifier();
}

void a_block_is_decided_once() {
    std::printf("\na block is decided once, either way\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x8A);
    check_ok(c.vm->issue(tx), "a transaction is pending");

    auto built = c.vm->build_block();
    check_ok(built, "a block is built around it");
    if (!built) return;
    check((*built)->verify(), "and verifies");
    (*built)->accept();
    check((*built)->status() == Status::Accepted, "the block is accepted");
    check(c.vm->nullifiers().count() == 1, "its note is spent");
    check(c.vm->mempool().size() == 0, "and it is out of the pool");

    // The seam says accept and reject are each inert after the other, because
    // more than one path can reach a decision. Here the second one would be
    // destructive: the note is already spent, so handing the transaction back
    // would have this node assemble a double spend its own peers refuse.
    (*built)->reject();
    check((*built)->status() == Status::Accepted, "a reject afterwards decides nothing");
    check(c.vm->mempool().size() == 0, "and hands back no spent transaction");

    // And the other direction, on a block that lost.
    const Transaction other = stark_tx(kMinTxFeeFloor, 0x8B);
    auto loser = block_over(*c.vm, (*built)->id(), 2, kNow, {other});
    check_ok(loser->check(), "a sibling verifies");
    loser->reject();
    check(loser->status() == Status::Rejected, "and is rejected");
    check(c.vm->mempool().size() == 1, "its transaction is pending again");

    loser->accept();
    check(loser->status() == Status::Rejected, "an accept afterwards decides nothing");
    check_eq(hex_of(c.vm->last_accepted()), hex_of((*built)->id()), "and the tip did not move");
    check(c.vm->nullifiers().count() == 1, "no further note was spent");
    unbind_stark_verifier();
}

void a_block_that_cannot_write_spends_nothing() {
    std::printf("\na block whose write fails spends nothing\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Id genesis = c.vm->genesis_block()->id();

    // Two transactions creating the SAME commitment: the first output writes,
    // the second is refused by the UTXO set, and the block must take the whole
    // batch with it rather than leave a shielded pool half applied.
    Transaction a = stark_tx(kMinTxFeeFloor, 0x99);
    Transaction bb = stark_tx(kMinTxFeeFloor, 0xAA);
    bb.outputs[0].commitment = a.outputs[0].commitment;
    bb.id = bb.compute_id();

    auto blk = block_over(*c.vm, genesis, 1, kNow, {a, bb});
    check_ok(blk->check(), "the block verifies — the clash is not visible to verify");
    check_err(blk->commit(), kErrUtxoExists, "and the write refuses it");

    check(c.vm->nullifiers().count() == 0, "no note was spent");
    check(c.vm->utxos().count() == 0, "no output was created");
    check_eq(hex_of(c.vm->last_accepted()), hex_of(genesis), "and the tip did not move");
    check_eq(hex_of(c.vm->state_root().get()), golden::kGenesisStateRoot,
             "nor did the committed root, which is still the genesis fold");
    unbind_stark_verifier();
}

// What syntactic_verify claims is not that it refuses the same blocks — it is
// WHERE it refuses them: before the chain is consulted at all. The conformance
// corpus cannot show that, because every vector in it names the genesis block
// as its parent and the genesis block exists. So the claim is made here, on a
// block whose parent no chain holds.
void the_syntactic_pass_does_not_read_the_ledger() {
    std::printf("\nthe syntactic pass answers about a block no chain holds\n");
    Chain c;
    const Id nowhere = id_of(0xAB);

    // Well formed, and on nothing. syntactic_verify has an answer anyway, which
    // it could not have if it had looked the parent up.
    auto orphan = block_over(*c.vm, nowhere, 1, kNow, {});
    check_ok(orphan->syntactic_verify(), "a well-formed block with a parent nobody has passes");
    check_err(orphan->check(), kErrNoBlock, "and check, which does look, refuses it");

    // The same block, past the clock: the state-free rule fires with no parent
    // to be found, so the refusal cannot have come from the lookup.
    auto future = block_over(*c.vm, nowhere, 1, kNow + kMaxClockSkew + 10, {});
    check_err(future->syntactic_verify(), kErrFutureBlock, "the clock is decided without one");

    // And the rules that DO need the chain stay below the line. A lie about the
    // state root is a fact about the chain's tree, not about these bytes.
    auto lying = block_over(*c.vm, c.vm->genesis_block()->id(), 1, kNow, {});
    lying->state_root = Bytes(32, 0xAA);
    lying->set_id(lying->compute_id());
    check_ok(lying->syntactic_verify(), "a state root the block did not produce is not syntactic");
    check_err(lying->check(), kErrInvalidStateRoot, "check is where that is caught");
}

void the_seam_never_throws() {
    std::printf("\nthe seam answers rather than throwing\n");
    bind_accepting_stark_verifier();
    Chain c;
    // A null block is "no", not a failure: nothing to build, not a block,
    // unknown id. That is the house form the seam declares.
    check(c.vm->build() == nullptr, "nothing to build is a null block");
    check(c.vm->parse(view(Bytes{1, 2, 3})) == nullptr, "not a block is a null block");
    check(c.vm->get(id_of(0xEE)) == nullptr, "an unknown id is a null block");
    check(c.vm->get(c.vm->genesis_block()->id()) != nullptr, "and genesis is findable");
    check(c.vm->alias() == "Z", "the chain answers to Z");
    unbind_stark_verifier();
}

}  // namespace

int main() {
    a_block_is_built_verified_and_accepted();
    verify_refusals();
    a_block_only_extends_something_it_can_build_on();
    a_verified_block_is_findable_as_a_parent();
    a_rejected_block_returns_its_transactions();
    a_block_is_decided_once();
    a_block_that_cannot_write_spends_nothing();
    the_syntactic_pass_does_not_read_the_ledger();
    the_seam_never_throws();
    return report("block");
}
