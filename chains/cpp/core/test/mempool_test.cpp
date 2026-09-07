// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool_test.cpp — the pool's four refusals, its order, and the two rules
// that are easy to state and easy to get wrong: that removing a transaction
// also removes its rivals, and that the space it took comes back.
//
// The transaction here is a struct with three fields, because that is all the
// pool is allowed to know about one. If a test needed a real transaction to
// exercise the pool, the pool would know too much.

#include "lux/core/check.hpp"
#include "lux/core/mempool.hpp"

#include <cstdio>
#include <string>
#include <vector>

using namespace lux::core;
using namespace lux::core::test;
using namespace lux::core::mempool;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    v[0] = b;
    return v;
}

// Everything a pool may know about a transaction.
struct Tx {
    Id id{};
    std::size_t size = 100;
    std::vector<Id> inputs;
};

Id pool_id(const Tx& t) { return t.id; }
std::size_t pool_size(const Tx& t) { return t.size; }
const std::vector<Id>& pool_inputs(const Tx& t) { return t.inputs; }

Tx tx(std::uint8_t name, std::vector<std::uint8_t> spends = {}, std::size_t size = 100) {
    Tx t;
    t.id = id_of(name);
    t.size = size;
    for (auto s : spends) t.inputs.push_back(id_of(s));
    return t;
}

void the_oldest_goes_first() {
    Pool<Tx> p;
    check(p.add(tx(1)).has_value(), "one is admitted");
    check(p.add(tx(2)).has_value(), "and another");
    check(p.add(tx(3)).has_value(), "and a third");
    check(p.size() == 3, "the pool holds three");

    const Tx* first = p.peek();
    check(first != nullptr && first->id == id_of(1), "the oldest is the one that arrived first");

    std::vector<std::uint8_t> seen;
    p.each([&](const Tx& t) {
        seen.push_back(t.id[0]);
        return true;
    });
    check(seen.size() == 3 && seen[0] == 1 && seen[1] == 2 && seen[2] == 3,
          "and the walk is in arrival order — the order two nodes must agree on");

    int count = 0;
    p.each([&](const Tx&) {
        ++count;
        return false;
    });
    check(count == 1, "returning false stops the walk");
}

void the_four_refusals() {
    Pool<Tx> p(1000);
    check(p.add(tx(1, {0x10})).has_value(), "the first is admitted");

    auto dup = p.add(tx(1, {0x11}));
    check(!dup && dup.error() == Refusal::Duplicate, "the same name twice is a duplicate");

    auto big = p.add(tx(2, {}, kMaxTxSize + 1));
    check(!big && big.error() == Refusal::TooLarge, "one over the transaction bound is too large");

    auto conflict = p.add(tx(3, {0x10}));
    check(!conflict && conflict.error() == Refusal::Conflict,
          "one spending an input already spoken for conflicts");

    auto full = p.add(tx(4, {}, 5000));
    check(!full && full.error() == Refusal::Full, "one bigger than the room left does not fit");

    check(p.size() == 1, "and none of the four is holding space");
    check(p.bytes_available() == 900, "the space is exactly what the one admitted left");
}

void space_comes_back() {
    Pool<Tx> p(1000);
    check(p.add(tx(1, {}, 400)).has_value() && p.add(tx(2, {}, 400)).has_value(),
          "two large transactions fill most of it");
    check(p.bytes_available() == 200, "leaving what they did not take");
    p.erase(id_of(1));
    check(p.bytes_available() == 600, "removing one gives its bytes back");
    check(p.add(tx(3, {}, 500)).has_value(), "so something that would not have fit now does");
}

void removing_a_transaction_removes_its_rivals() {
    Pool<Tx> p;
    check(p.add(tx(1, {0x10})).has_value(), "one spending an output is pooled");
    check(p.add(tx(2, {0x20})).has_value(), "and one spending another");

    // A transaction that was never pooled is accepted in a block. It spends the
    // same output as tx 1, so tx 1 can NEVER be accepted now.
    p.remove({tx(9, {0x10})});
    check(p.get(id_of(1)) == nullptr, "the rival of an accepted transaction is dropped");
    check(p.get(id_of(2)) != nullptr, "and the one that rivals nothing stays");
    check(p.size() == 1, "the pool is one smaller");

    // Its input is free again, so a new spender of it is admissible.
    check(p.add(tx(3, {0x10})).has_value(), "and the input it held is free again");
}

void a_removed_transaction_frees_its_own_inputs() {
    Pool<Tx> p;
    check(p.add(tx(1, {0x10, 0x11})).has_value(), "one spending two outputs");
    p.remove({tx(1, {0x10, 0x11})});
    check(p.empty(), "removing it by name empties the pool");
    check(p.add(tx(2, {0x10})).has_value(), "and both of its inputs are free");
    check(p.add(tx(3, {0x11})).has_value(), "both of them");
}

void refusals_are_remembered_and_forgotten() {
    Dropped<std::string> d(3);
    d.mark(id_of(1), "because");
    d.mark(id_of(2), "because");
    d.mark(id_of(3), "because");
    check(d.why(id_of(1)).value_or("") == "because", "a refusal is remembered");

    d.mark(id_of(4), "because");
    check(d.size() == 3, "only so many are remembered");
    check(!d.why(id_of(1)).has_value(), "and the oldest is the one forgotten");
    check(d.why(id_of(4)).has_value(), "the newest is kept");

    d.mark(id_of(2), "a better reason");
    check(d.why(id_of(2)).value_or("") == "a better reason",
          "marking one twice replaces the reason rather than the slot");
    check(d.size() == 3, "and does not cost a second slot");

    d.forget(id_of(2));
    check(!d.why(id_of(2)).has_value(),
          "a transaction that arrives is no longer a transaction that was refused");
    check(d.size() == 2, "and its slot is free");
}

}  // namespace

int main() {
    std::printf("mempool\n");
    the_oldest_goes_first();
    the_four_refusals();
    space_comes_back();
    removing_a_transaction_removes_its_rivals();
    a_removed_transaction_frees_its_own_inputs();
    refusals_are_remembered_and_forgotten();
    return report("mempool");
}
