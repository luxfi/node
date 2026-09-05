// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm_test.cpp — the whole chain: genesis, the door a transaction comes in by,
// the fee floor, the height index, a run of blocks, and a RESTART.
//
// The restart is the point of persistence. A chain that forgets what it accepted
// when the process exits would re-sign a height it already signed, and a
// signature from a past height could not be checked at all.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/store.hpp"
#include "lux/zkvm/vm.hpp"

#include <cstdio>
#include <memory>
#include <string>
#include <unistd.h>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

constexpr std::int64_t kNow = 1'700'000'000;

std::string temp_path(const char* tag) {
    return std::string("/tmp/zkvm-vm-") + tag + "-" + std::to_string(::getpid());
}

std::unique_ptr<Vm> open_chain(store::Store& base, const Genesis& g = Genesis{}) {
    auto vm = std::make_unique<Vm>(strict_config(), base);
    vm->set_now(kNow);
    auto r = vm->initialize(g);
    check_ok(r, "the chain comes up");
    return vm;
}

void the_chain_binding_separates_two_chains() {
    std::printf("two chains with the same genesis are still two chains\n");
    store::Memory a_store, b_store;

    VmConfig a = strict_config();
    VmConfig bcfg = strict_config();
    bcfg.chain_id = id_of(0xAB);

    Vm a_vm(a, a_store);
    Vm b_vm(bcfg, b_store);
    check_ok(a_vm.initialize(Genesis{}), "the first chain comes up");
    check_ok(b_vm.initialize(Genesis{}), "the second comes up on the same genesis");

    // The binding is sha256(ChainID ‖ NetworkID) and is NOT on the wire, so the
    // same bytes name a different block on a different chain. Without it the two
    // would derive the same genesis id, and one chain's blocks would chain onto
    // the other's verbatim.
    check(a_vm.bind() != b_vm.bind(), "their bindings differ");
    check(a_vm.last_accepted() != b_vm.last_accepted(), "so their genesis blocks differ");

    // A different network on the same chain id is a different chain too.
    store::Memory c_store;
    VmConfig ccfg = strict_config();
    ccfg.network_id = 1;
    Vm c_vm(ccfg, c_store);
    check_ok(c_vm.initialize(Genesis{}), "a chain on another network comes up");
    check(c_vm.bind() != a_vm.bind(), "and it too is a different chain");
}

void genesis_is_deterministic() {
    std::printf("\nthe same genesis gives the same chain, every time\n");
    store::Memory one, two;
    Vm a(strict_config(), one);
    Vm bvm(strict_config(), two);
    Genesis g;
    g.timestamp = 1'500'000'000;
    check_ok(a.initialize(g), "the first comes up");
    check_ok(bvm.initialize(g), "the second comes up");
    check_eq(hex_of(a.last_accepted()), hex_of(bvm.last_accepted()),
             "and they agree on the genesis id");

    // The timestamp is hashed into the genesis id, so reading a wall clock there
    // would give every node a different chain for the same genesis file, and a
    // different one again after each restart.
    store::Memory three;
    Vm c(strict_config(), three);
    Genesis later = g;
    later.timestamp = g.timestamp + 1;
    check_ok(c.initialize(later), "a chain one second later comes up");
    check(c.last_accepted() != a.last_accepted(), "and it is a different chain");
}

void the_genesis_allocation_is_durable_and_happens_once() {
    std::printf("\nthe genesis allocation is committed on its own, and only once\n");
    const std::string path = temp_path("genesis");
    ::unlink(path.c_str());

    Genesis g;
    Transaction alloc;
    alloc.type = TxType::Transfer;
    alloc.outputs = {{Bytes(32, 0xC0), b("note"), b("epk"), {}}};
    alloc.id = alloc.compute_id();
    g.initial_txs = {alloc};

    Id genesis_id{};
    {
        auto f = store::File::open(path);
        check_ok(f, "a fresh store opens");
        if (!f) return;
        auto vm = open_chain(**f, g);
        genesis_id = vm->last_accepted();
        check(vm->utxos().count() == 1, "genesis allocated its output");
        // Genesis is folded into the root ONCE, before block 1, rather than
        // being re-folded into every later block's root.
        store::Memory empty;
        auto fresh = Root::open(empty);
        check_ok(fresh, "a fresh root opens for the comparison");
        if (fresh)
            check_eq(hex_of(vm->state_root().get()), hex_of((*fresh)->after(g.initial_txs)),
                     "and the committed root is the genesis fold over the empty root");
    }

    // Left staged, a genesis allocation would ride on whichever block committed
    // first and vanish from a chain that never accepted one. And a seed that
    // recorded no tip would leave the chain calling itself fresh on every boot —
    // allocating again, which its own state then refuses ("already exists"), so
    // the node cannot restart until it produces a block it cannot produce.
    {
        auto f = store::File::open(path);
        check_ok(f, "the store reopens");
        if (!f) return;
        auto vm = open_chain(**f, g);
        check_eq(hex_of(vm->last_accepted()), hex_of(genesis_id), "on the same genesis");
        check(vm->utxos().count() == 1, "with the allocation still there, allocated once");
    }
    ::unlink(path.c_str());
}

void the_door_charges_before_the_pool_fills() {
    std::printf("\nthe fee gate fires at the door, before pool pressure changes\n");
    bind_accepting_stark_verifier();
    store::Memory base;
    auto vm = open_chain(base);

    check(vm->fee().accepts_user_txs(), "the chain accepts user transactions");
    check(vm->fee().minimum() == kMinTxFeeFloor, "at the declared floor");
    check_ok(vm->fee().validate(), "which the boot-time gate accepts");

    check_err(vm->issue(stark_tx(0, 1)), kErrFeeTooLow, "a zero-fee transaction is refused");
    check_err(vm->issue(stark_tx(kMinTxFeeFloor - 1, 1)), kErrFeeTooLow,
              "and so is one paying under the floor");
    check(vm->mempool().size() == 0, "neither took a slot in the pool");

    // The shape too: a transaction that cannot be built into any block must not
    // occupy a slot in a bounded pool.
    Transaction shapeless = stark_tx(kMinTxFeeFloor, 2);
    shapeless.outputs.clear();
    shapeless.id = shapeless.compute_id();
    check_err(vm->issue(shapeless), kErrNoOutputs, "a transaction creating nothing is refused");
    check(vm->mempool().size() == 0, "and it took no slot either");

    auto id = vm->issue(stark_tx(kMinTxFeeFloor, 3));
    check_ok(id, "a transaction paying the floor is admitted");
    check(vm->mempool().size() == 1, "and is pending");
    if (id) check(vm->mempool().has(*id), "under the id its content names");

    // A chain that accepts no user transactions refuses every caller explicitly
    // rather than by omission.
    check_err(Fee::closed().admit(kMinTxFeeFloor), kErrChainAcceptsNoUserTxs,
              "a closed chain refuses every caller");
    unbind_stark_verifier();
}

void a_run_of_blocks_and_the_height_index() {
    std::printf("\na run of blocks, and a height index that names only accepted ones\n");
    bind_accepting_stark_verifier();
    store::Memory base;
    auto vm = open_chain(base);

    std::vector<Id> ids;
    for (std::uint8_t i = 1; i <= 3; ++i) {
        check_ok(vm->issue(stark_tx(kMinTxFeeFloor, std::uint8_t(0x60 + i))),
                 "a transaction comes in");
        auto blk = vm->build_block();
        check_ok(blk, "a block is built");
        if (!blk) break;
        check((*blk)->verify(), "and verifies");
        (*blk)->accept();
        check((*blk)->status() == Status::Accepted, "and is accepted");
        ids.push_back((*blk)->id());
    }
    check(vm->last_accepted_height() == 3, "the chain reached height 3");
    check(vm->nullifiers().count() == 3, "with three notes spent");
    check(vm->utxos().count() == 3, "and three created");

    for (std::size_t i = 0; i < ids.size(); ++i) {
        auto at = vm->chain().id_at_height(std::uint64_t(i) + 1);
        check_ok(at, "the height index answers");
        if (at) check_eq(hex_of(*at), hex_of(ids[i]), "with the block accepted at that height");
    }
    auto genesis_at = vm->chain().id_at_height(0);
    check(!genesis_at.has_value(),
          "and names nothing at genesis, which no block was accepted at");
    check_err(vm->chain().id_at_height(99), kErrNoBlock, "nor at a height nobody reached");

    // Every accepted block reads back by id, from committed state.
    for (const Id& id : ids) {
        auto blk = vm->block(id);
        check_ok(blk, "an accepted block reads back by id");
        if (blk) check_eq(hex_of((*blk)->id()), hex_of(id), "as the block it was");
    }
    unbind_stark_verifier();
}

void the_chain_survives_a_restart() {
    std::printf("\nthe chain survives the process\n");
    bind_accepting_stark_verifier();
    const std::string path = temp_path("restart");
    ::unlink(path.c_str());

    Id tip{};
    Bytes spent_nullifier;
    {
        auto f = store::File::open(path);
        check_ok(f, "a fresh store opens");
        if (!f) return;
        auto vm = open_chain(**f);
        const Transaction tx = stark_tx(kMinTxFeeFloor, 0x70);
        spent_nullifier = tx.nullifiers[0];
        check_ok(vm->issue(tx), "a transaction is issued");
        auto blk = vm->build_block();
        check_ok(blk, "a block is built");
        if (!blk) return;
        check((*blk)->verify(), "and verifies");
        (*blk)->accept();
        tip = vm->last_accepted();
        check(vm->last_accepted_height() == 1, "the chain is at height 1");
    }

    {
        auto f = store::File::open(path);
        check_ok(f, "the store reopens");
        if (!f) return;
        auto vm = open_chain(**f);

        // A node that restarted must know where it is, or it re-signs a height it
        // already signed.
        check_eq(hex_of(vm->last_accepted()), hex_of(tip), "the tip is where it was left");
        check(vm->last_accepted_height() == 1, "at the height it reached");

        // And the spent set is the whole of what stops a note being spent twice.
        auto spend = vm->nullifiers().spent_at(view(spent_nullifier));
        check(spend && spend->spent, "the note spent before the restart is still spent");
        check(spend && spend->height == 1, "at the height it was spent");
        check(vm->utxos().count() == 1, "the output it created is still there");

        // A signature from a PAST height can be checked, because the block at
        // that height is still nameable and still readable.
        auto at = vm->chain().id_at_height(1);
        check_ok(at, "the height index survived");
        if (at) {
            check_eq(hex_of(*at), hex_of(tip), "naming the block accepted there");
            auto blk = vm->block(*at);
            check_ok(blk, "and that block reads back whole");
            if (blk) check((*blk)->txs.size() == 1, "with the transaction it carried");
        }

        // Spending it again is refused, which is the whole point.
        Transaction again = stark_tx(kMinTxFeeFloor, 0x70);
        check_err(vm->admit(again, 2), kErrNullifierSpent,
                  "and spending that note again is refused after the restart");
    }
    ::unlink(path.c_str());
    unbind_stark_verifier();
}

void assembly_drops_what_it_cannot_build() {
    std::printf("\nassembly runs the same predicate verify runs\n");
    bind_accepting_stark_verifier();
    store::Memory base;
    auto vm = open_chain(base);

    // A transaction with an out-of-range type is read straight off the wire, so
    // it can reach the pool. It used to be assembled into every block and then
    // refused by every node's verify, including the proposer's — and nothing
    // evicted it, so that proposer never produced another block.
    Transaction rotten = stark_tx(kMinTxFeeFloor, 0x80);
    check_ok(vm->issue(rotten), "a good transaction is pending");
    Transaction good = stark_tx(kMinTxFeeFloor, 0x81);
    check_ok(vm->issue(good), "and another");

    // Make the first unbuildable AFTER it is in the pool, the way a chain that
    // has moved on makes an expiry unreachable.
    vm->mempool().remove(rotten.id);
    Transaction expired = stark_tx(kMinTxFeeFloor, 0x80, 1);
    check_ok(vm->mempool().add(expired), "an expired transaction reaches the pool");
    check(vm->mempool().size() == 2, "the pool holds both");

    // Nothing has passed height 1 yet, so nothing is expired at height 1.
    auto blk = vm->build_block();
    check_ok(blk, "a block is built");
    if (blk) {
        check((*blk)->verify(), "and it verifies — assembly built nothing verify refuses");
        (*blk)->accept();
    }

    // Now the chain has passed the expiry, and the pool drops it rather than
    // holding a slot forever.
    check(vm->mempool().size() == 0, "the pool is empty after the block");
    unbind_stark_verifier();
}

void a_preference_is_what_the_next_block_is_built_on() {
    std::printf("\nthe engine's preference decides what the next block extends\n");
    bind_accepting_stark_verifier();
    store::Memory base;
    auto vm = open_chain(base);

    check_ok(vm->issue(stark_tx(kMinTxFeeFloor, 0x90)), "a transaction is pending");
    auto first = vm->build_block();
    check_ok(first, "a block is built on genesis");
    if (!first) return;
    check((*first)->verify(), "and verifies");

    // Dropping the preference meant a proposal always built on the ACCEPTED tip,
    // so a node with two blocks in flight re-proposed a height it had already
    // proposed.
    vm->prefer((*first)->id());
    check_ok(vm->issue(stark_tx(kMinTxFeeFloor, 0x91)), "another transaction arrives");
    auto second = vm->build_block();
    check_ok(second, "a second block is built");
    if (second) {
        check((*second)->block_height == 2, "at the NEXT height, not the same one again");
        check_eq(hex_of((*second)->parent()), hex_of((*first)->id()),
                 "on the block the engine preferred");
    }
    unbind_stark_verifier();
}

void health_reports_the_chain() {
    std::printf("\nthe chain says what it holds\n");
    bind_accepting_stark_verifier();
    store::Memory base;
    auto vm = open_chain(base);
    check_ok(vm->issue(stark_tx(kMinTxFeeFloor, 0xA0)), "a transaction is pending");
    auto blk = vm->build_block();
    if (blk) {
        check((*blk)->verify(), "a block verifies");
        (*blk)->accept();
    }
    const Health h = vm->health();
    check(h.healthy, "the chain is healthy");
    check(h.last_block_height == 1, "at height 1");
    check(h.nullifier_count == 1, "with one note spent");
    check(h.utxo_count == 1, "one output created");
    check(h.mempool_size == 0, "an empty pool");
    unbind_stark_verifier();
}

}  // namespace

int main() {
    the_chain_binding_separates_two_chains();
    genesis_is_deterministic();
    the_genesis_allocation_is_durable_and_happens_once();
    the_door_charges_before_the_pool_fills();
    a_run_of_blocks_and_the_height_index();
    the_chain_survives_a_restart();
    assembly_drops_what_it_cannot_build();
    a_preference_is_what_the_next_block_is_built_on();
    health_reports_the_chain();
    return report("vm");
}
