// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vertex_test.cpp — the DAG shape: its conflict rule, what it is held to, and a
// wire that a peer's counts cannot turn into memory.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/store.hpp"
#include "lux/zkvm/vertex.hpp"
#include "lux/zkvm/vm.hpp"

#include <memory>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

constexpr std::int64_t kNow = 1'700'000'000;

struct Chain {
    store::Memory base;
    std::unique_ptr<Vm> vm;

    Chain() {
        vm = std::make_unique<Vm>(strict_config(), base);
        vm->set_now(kNow);
        check_ok(vm->initialize(Genesis{}), "the chain comes up");
    }
};

std::shared_ptr<Vertex> vertex_over(Vm& vm, std::vector<Id> parents, std::uint64_t height,
                                    std::vector<Transaction> txs) {
    auto v = std::make_shared<Vertex>();
    v->bind_vm(vm);
    v->parents = std::move(parents);
    v->vertex_height = height;
    v->txs = std::move(txs);
    v->finish();
    return v;
}

void conflicts_are_decided_by_the_notes_spent() {
    std::printf("two vertices conflict iff their nullifier sets intersect\n");
    Chain c;

    const Transaction a = stark_tx(kMinTxFeeFloor, 1);
    const Transaction bb = stark_tx(kMinTxFeeFloor, 2);
    const Transaction cc = stark_tx(kMinTxFeeFloor, 3);

    auto one = vertex_over(*c.vm, {id_of(9)}, 1, {a, bb});
    auto shares = vertex_over(*c.vm, {id_of(9)}, 1, {bb, cc});
    auto disjoint = vertex_over(*c.vm, {id_of(9)}, 1, {cc});

    check(one->conflicts(*shares), "overlapping nullifiers conflict");
    check(shares->conflicts(*one), "and the relation is symmetric");
    check(!one->conflicts(*disjoint), "disjoint nullifiers do not");
    check(one->nullifier_set().size() == 2, "a vertex's set is the notes it spends");
}

void a_vertex_is_held_to_what_a_block_is_held_to() {
    std::printf("\na vertex extends the frontier, or it is refused\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Id tip = c.vm->last_accepted();
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x10);

    auto good = vertex_over(*c.vm, {tip}, 1, {tx});
    check_ok(good->check(), "a vertex on the tip at the next height verifies");

    // It used to check the transactions and NOTHING ELSE — not the parents, not
    // the height — so a vertex naming no parent at height 1<<40 verified, and
    // accepting it set the store's height to 1<<40, pruned every block in flight
    // and left the linear chain unable to propose a child ever again.
    auto no_parent = vertex_over(*c.vm, {}, std::uint64_t(1) << 40, {tx});
    check_err(no_parent->check(), kErrNotOnTip, "a vertex naming no parent is refused");

    auto two_parents = vertex_over(*c.vm, {tip, id_of(3)}, 1, {tx});
    check_err(two_parents->check(), kErrNotOnTip,
              "a vertex naming two parents extends no single block");
    check(two_parents->parent() == kEmptyId, "and names none to the store");

    auto elsewhere = vertex_over(*c.vm, {id_of(7)}, 1, {tx});
    check_err(elsewhere->check(), kErrNotOnTip, "a vertex on something else is refused");

    auto leaping = vertex_over(*c.vm, {tip}, 99, {tx});
    check_err(leaping->check(), kErrInvalidHeight, "a height that does not follow is refused");

    Transaction twin = tx;
    twin.memo = b("twin");
    twin.id = twin.compute_id();
    auto doubled = vertex_over(*c.vm, {tip}, 1, {tx, twin});
    check_err(doubled->check(), kErrDuplicateNullifier,
              "one note spent twice in one vertex is refused");
    unbind_stark_verifier();
}

void a_vertex_changes_state_the_way_a_block_does() {
    std::printf("\na vertex is not a block, and changes state the same way\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x20);
    check_ok(c.vm->issue(tx), "a transaction is pending");

    auto built = c.vm->build_vertex();
    check_ok(built, "a vertex is built");
    if (!built) return;
    check((*built)->vertex_height == 1, "at the next height");
    check((*built)->parents.size() == 1, "on one parent");
    check((*built)->tx_ids.size() == 1, "naming the transaction it carries");

    check_ok((*built)->check(), "it verifies");
    check_ok((*built)->commit(), "and is accepted");
    check_eq(hex_of(c.vm->last_accepted()), hex_of((*built)->id()), "it is the tip");
    check(c.vm->last_accepted_height() == 1, "at height 1");
    check(c.vm->nullifiers().count() == 1, "the note it spent is spent");
    check(c.vm->utxos().count() == 1, "the note it created exists");
    check(c.vm->mempool().size() == 0, "and the pool released it");

    // Asking for a vertex by BLOCK id is a miss rather than something the caller
    // cannot use.
    check_err(c.vm->block((*built)->id()), "is not a block",
              "a vertex is not answered as a block");
    unbind_stark_verifier();
}

void assembly_batches_only_disjoint_spends() {
    std::printf("\nassembly batches only what does not conflict\n");
    bind_accepting_stark_verifier();
    Chain c;

    const Transaction a = stark_tx(kMinTxFeeFloor * 3, 0x30);
    Transaction clash = a;
    clash.memo = b("clash");
    clash.fee = kMinTxFeeFloor * 2;
    clash.id = clash.compute_id();
    const Transaction other = stark_tx(kMinTxFeeFloor, 0x31);

    check_ok(c.vm->issue(a), "the first is pending");
    // The pool itself refuses the second claim on the same note.
    check_err(c.vm->issue(clash), kErrNullifierInPool, "a second claim on the note is refused");
    check_ok(c.vm->issue(other), "a disjoint transaction is pending");

    auto v = c.vm->build_vertex();
    check_ok(v, "a vertex is built");
    if (v) check((*v)->txs.size() == 2, "batching both disjoint transactions");
    unbind_stark_verifier();
}

void a_rejected_vertex_returns_its_transactions() {
    std::printf("\na rejected vertex gives its transactions back\n");
    bind_accepting_stark_verifier();
    Chain c;
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x40);
    check_ok(c.vm->issue(tx), "a transaction is pending");
    auto v = c.vm->build_vertex();
    check_ok(v, "a vertex is built");
    if (!v) return;
    c.vm->mempool().remove(tx.id);
    check(c.vm->mempool().size() == 0, "the pool is emptied for the test");
    (*v)->reject();
    check((*v)->status() == Status::Rejected, "the vertex is rejected");
    check(c.vm->mempool().has(tx.id), "and its transaction is pending again");

    // Decided once. A second reject would hand the same transaction back a
    // second time, and on a vertex that had been accepted it would hand back
    // one whose note is already spent.
    c.vm->mempool().remove(tx.id);
    (*v)->reject();
    check(c.vm->mempool().size() == 0, "a second reject hands nothing back");
    unbind_stark_verifier();
}

void the_vertex_wire_is_canonical_and_bounded() {
    std::printf("\nthe vertex wire: canonical, and a peer's counts are not memory\n");
    Chain c;
    const Transaction tx = stark_tx(kMinTxFeeFloor, 0x50);
    auto v = vertex_over(*c.vm, {id_of(2)}, 5, {tx});
    const Bytes raw(v->bytes().begin(), v->bytes().end());

    auto back = deserialize_vertex(view(raw), *c.vm);
    check_ok(back, "a vertex round-trips");
    if (back) {
        check((*back)->vertex_height == 5, "at its height");
        check((*back)->parents.size() == 1 && (*back)->parents[0] == id_of(2), "on its parent");
        check((*back)->txs.size() == 1, "with its transaction");
        check_eq(hex_of((*back)->id()), hex_of(v->id()), "and derives the same identity");
    }

    // Without the canonical rule, arbitrary trailing bytes ride along in the
    // bytes the store writes to disk, so one logical vertex has unboundedly many
    // encodings all mapping to the same id, and a peer can park megabytes under a
    // legitimate one.
    Bytes padded = raw;
    padded.push_back(0);
    check_err(deserialize_vertex(view(padded), *c.vm), wire::kErrTrailingBytes,
              "trailing bytes are refused");

    for (std::size_t cut = 1; cut < 40 && cut < raw.size(); cut += 7) {
        const Bytes truncated(raw.begin(), raw.end() - std::ptrdiff_t(cut));
        auto r = deserialize_vertex(view(truncated), *c.vm);
        check(!r.has_value(), "a truncated vertex is refused, never half-decoded");
    }

    // A 16-byte vertex claiming 2^32-1 parents asks for 128 GiB; the count is
    // bounded by the bytes that remain BEFORE anything is reserved.
    Bytes hostile(16, 0);
    hostile[8] = 0xFF;
    hostile[9] = 0xFF;
    hostile[10] = 0xFF;
    hostile[11] = 0xFF;  // parentCount = 0xFFFFFFFF
    check_err(deserialize_vertex(view(hostile), *c.vm), kErrInvalidBlock,
              "an impossible parent count is refused, not allocated");

    Bytes hostile_txs(20, 0);
    hostile_txs[15] = 0;  // parentCount = 0
    hostile_txs[16] = 0xFF;
    hostile_txs[17] = 0xFF;
    hostile_txs[18] = 0xFF;
    hostile_txs[19] = 0xFF;  // txCount = 0xFFFFFFFF
    check_err(deserialize_vertex(view(hostile_txs), *c.vm), kErrInvalidBlock,
              "an impossible transaction count is refused, not allocated");

    check_err(deserialize_vertex(view(Bytes(8, 0)), *c.vm), kErrInvalidBlock,
              "a buffer too small to be a vertex is refused");
}

}  // namespace

int main() {
    conflicts_are_decided_by_the_notes_spent();
    a_vertex_is_held_to_what_a_block_is_held_to();
    a_vertex_changes_state_the_way_a_block_does();
    assembly_batches_only_disjoint_spends();
    a_rejected_vertex_returns_its_transactions();
    the_vertex_wire_is_canonical_and_bounded();
    return report("vertex");
}
