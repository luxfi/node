// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool_test.cpp — admission, eviction, ordering and expiry.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/mempool.hpp"

#include <chrono>
#include <thread>
#include <vector>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

void it_tells_consensus_there_is_work() {
    std::printf("a transaction the pool accepted is worth nothing until consensus is told\n");
    Mempool mp(10);

    // Nothing has arrived, so nothing should be claimed.
    check(!mp.wait_for_event(std::chrono::milliseconds(50)),
          "an empty pool reports no work to build");

    bool woke = false;
    std::thread waiter([&] { woke = mp.wait_for_event(std::chrono::seconds(5)); });
    std::this_thread::sleep_for(std::chrono::milliseconds(20));
    check_ok(mp.add(shielded_transfer(1000, 1)), "a transaction is accepted");
    waiter.join();
    // Consensus builds only when the wait returns, so accepting work and
    // reporting it are one step; without this the chain sits at genesis with a
    // full pool.
    check(woke, "and consensus was told, in the same step");
}

void a_full_pool_gives_up_the_cheapest() {
    std::printf("\na full pool gives up whatever pays least\n");
    Mempool mp(2);

    const Transaction best = shielded_transfer(1000, 1);
    const Transaction dust = shielded_transfer(1, 2);
    const Transaction middle = shielded_transfer(100, 3);

    check_ok(mp.add(best), "the best payer is held");
    check_ok(mp.add(dust), "and the dust");
    check_ok(mp.add(middle), "the pool is full but the arrival outbids the dust");

    check(mp.has(best.id), "the best payer survives a full pool");
    check(mp.has(middle.id), "so does the arrival");
    check(!mp.has(dust.id), "the cheapest is the one to give up");
    check(mp.size() == 2, "and the pool is still the size it declared");
}

void a_full_pool_refuses_an_arrival_worse_than_it_holds() {
    std::printf("\nand refuses an arrival worse than anything it holds\n");
    Mempool mp(2);
    check_ok(mp.add(shielded_transfer(1000, 1)), "a rich transaction");
    check_ok(mp.add(shielded_transfer(900, 2)), "and another");

    const Transaction pauper = shielded_transfer(1, 3);
    check_err(mp.add(pauper), kErrPoolFull, "the pauper is refused");
    check(!mp.has(pauper.id), "and is not held");
    check(mp.size() == 2, "a refused arrival does not change what the pool holds");
}

void a_nullifier_is_claimed_once() {
    std::printf("\ntwo transactions cannot both claim one note\n");
    Mempool mp(10);
    Transaction first = shielded_transfer(100, 7);
    Transaction second = shielded_transfer(200, 7);  // the same nullifier
    second.memo = b("different");
    second.id = second.compute_id();

    check_ok(mp.add(first), "the first is held");
    check(first.id != second.id, "the two are different transactions");
    check_err(mp.add(second), kErrNullifierInPool, "the second is refused on the conflict");

    mp.remove(first.id);
    check_ok(mp.add(second), "and once the first leaves, the note is free again");
}

void the_id_is_derived_not_taken() {
    std::printf("\nthe pool derives the id from the content\n");
    Mempool mp(10);
    Transaction tx = shielded_transfer(100, 9);
    const Id real = tx.compute_id();
    tx.id = id_of(0xFF);  // whatever the caller claims
    check_ok(mp.add(tx), "the transaction is accepted");
    check(mp.has(real), "and is held under the id its CONTENT names");
    check(!mp.has(id_of(0xFF)), "not the one the caller claimed");
}

void the_best_paying_come_first_and_ties_are_stable() {
    std::printf("\nthe proposer takes the best-paying first, reproducibly\n");
    Mempool mp(64);
    for (std::uint8_t i = 0; i < 16; ++i)
        check_ok(mp.add(shielded_transfer(std::uint64_t(i) + 1, i)), "a transaction is held");

    const auto got = mp.pending(8);
    check(got.size() == 8, "asking for eight gives eight");
    bool descending = true;
    for (std::size_t i = 1; i < got.size(); ++i)
        if (got[i - 1].fee < got[i].fee) descending = false;
    check(descending, "best-paying first");
    check(got.size() > 0 && got[0].fee == 16, "and the best payer leads");

    // Equal fees break on the id, so two nodes with the same pool assemble the
    // same block — a proposer that cannot reproduce its own choice is one that
    // disagrees with itself.
    Mempool tied(8);
    for (std::uint8_t i = 0; i < 4; ++i) check_ok(tied.add(shielded_transfer(5, i)), "a tie");
    const auto a = tied.pending(4);
    const auto c = tied.pending(4);
    bool same = a.size() == c.size();
    for (std::size_t i = 0; same && i < a.size(); ++i) same = a[i].id == c[i].id;
    check(same, "and ties come back in the same order every time");
}

void reading_the_pool_does_not_write_to_it() {
    std::printf("\nreading what to build does not change what is held\n");
    Mempool mp(64);
    for (std::uint8_t i = 0; i < 16; ++i)
        check_ok(mp.add(shielded_transfer(std::uint64_t(i) + 1, i)), "a transaction is held");

    std::vector<std::thread> readers;
    std::atomic<int> wrong{0};
    for (int i = 0; i < 8; ++i)
        readers.emplace_back([&] {
            if (mp.pending(8).size() != 8) ++wrong;
        });
    for (auto& t : readers) t.join();
    check(wrong == 0, "eight readers at once all see eight transactions");
    check(mp.size() == 16, "and the pool still holds every one of them");
}

void what_the_chain_has_passed_is_dropped() {
    std::printf("\nthe pool drops what the chain has passed\n");
    Mempool mp(10);
    Transaction soon = shielded_transfer(100, 1);
    soon.expiry = 5;
    soon.id = soon.compute_id();
    Transaction later = shielded_transfer(100, 2);
    later.expiry = 500;
    later.id = later.compute_id();

    check_ok(mp.add(soon), "a transaction expiring soon");
    check_ok(mp.add(later), "and one that does not");

    check(mp.prune_expired(3) == 0, "nothing is dropped before its expiry");
    check(mp.size() == 2, "so the pool is unchanged");

    // Nothing else does this: a transaction that can never enter a block occupies
    // a slot forever, and a pool full of those refuses every honest arrival
    // paying the same floor.
    check(mp.prune_expired(6) == 1, "past its expiry it is dropped");
    check(!mp.has(soon.id), "and is gone");
    check(mp.has(later.id), "while the live one stays");
}

}  // namespace

int main() {
    it_tells_consensus_there_is_work();
    a_full_pool_gives_up_the_cheapest();
    a_full_pool_refuses_an_arrival_worse_than_it_holds();
    a_nullifier_is_claimed_once();
    the_id_is_derived_not_taken();
    the_best_paying_come_first_and_ties_are_stable();
    reading_the_pool_does_not_write_to_it();
    what_the_chain_has_passed_is_dropped();
    return report("mempool");
}
