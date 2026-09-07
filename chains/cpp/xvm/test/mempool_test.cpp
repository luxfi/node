// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// mempool_test.cpp — what this node will hold, and what it refuses.
//
// Ported case for case from vms/txs/mempool/mempool_test.go and the admission
// cases in vms/xvm/network/gossip_test.go. The pool's own tests use a stated
// verdict rather than a chain: the pool must not be able to tell a valid
// transaction from an invalid one, so a test that gave it a chain would be
// testing the chain. The chain-backed gate is exercised in vm_test, where it
// belongs.

#include "lux/core/check.hpp"
#include "fixtures.hpp"

#include "lux/xvm/mempool.hpp"

#include <map>
#include <string>
#include <vector>

using namespace lux::xvm;
using namespace lux::core::test;
using namespace lux::xvm::test;

namespace {

std::shared_ptr<fx::secp256k1fx::TransferOutput> tout(std::uint64_t amt) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = fx::OutputOwners{0, 1, {addr(1)}};
    return o;
}

std::shared_ptr<fx::secp256k1fx::TransferInput> tin(std::uint64_t amt) {
    auto i = std::make_shared<fx::secp256k1fx::TransferInput>();
    i->amt = amt;
    i->input.sig_indices = {0};
    return i;
}

// A transaction that spends `spends` and pays `amt`. `spends` is what fixes its
// conflicts; `amt` is what makes two txs spending the same thing distinct.
std::shared_ptr<txs::Tx> tx_spending(std::uint8_t spends, std::uint64_t amt,
                                     std::size_t memo_size = 0) {
    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = 369;
    utx->base.blockchain_id = id(0xC1);
    utx->base.ins.push_back(
        txs::TransferableInput{txs::UTXOID{id(spends), 0, false}, id(2), tin(amt)});
    utx->base.outs.push_back(txs::TransferableOutput{id(2), tout(amt)});
    utx->base.memo.assign(memo_size, std::uint8_t(spends));
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

// Verdict is the chain's opinion, stated. It is a test double for
// mempool::Verifier and nothing else uses it.
struct Verdict final : mempool::Verifier {
    std::map<Id, std::string> refuse;
    int asked = 0;

    mempool::Result<void> verify_tx(txs::Tx& tx) override {
        ++asked;
        auto it = refuse.find(tx.id());
        if (it != refuse.end()) return std::unexpected(it->second);
        return {};
    }
};

// ---- the pool ----

void the_pool_holds_and_orders() {
    mempool::Pool p;
    auto a = tx_spending(1, 10);
    auto b = tx_spending(2, 20);
    check(p.add(a).has_value(), "a transaction is admitted");
    check(p.add(b).has_value(), "and a second one");
    check(p.size() == 2, "the pool holds two");
    check(p.peek() != nullptr && (*p.peek())->id() == a->id(),
          "peek gives the OLDEST — the order the builder takes them in");
    check(p.get(b->id()) != nullptr, "either can be looked up by id");
    check(p.get(id(0xEE)) == nullptr, "and an id nobody issued is simply absent");

    std::vector<Id> walked;
    p.each([&](const std::shared_ptr<txs::Tx>& tx) {
        walked.push_back(tx->id());
        return true;
    });
    check(walked.size() == 2 && walked[0] == a->id(), "the walk is insertion order too");
}

void a_duplicate_is_refused() {
    mempool::Pool p;
    auto a = tx_spending(1, 10);
    check(p.add(a).has_value(), "first");
    auto again = p.add(a);
    check(!again.has_value(), "the same transaction twice is refused");
    if (!again)
        check(again.error() == mempool::Refusal::Duplicate, "and refused as a duplicate");
}

void a_conflict_is_refused() {
    mempool::Pool p;
    // Two transactions spending the SAME output. A block cannot hold both, so
    // the pool does not either.
    auto a = tx_spending(1, 10);
    auto b = tx_spending(1, 11);
    check(a->id() != b->id(), "they are different transactions");
    check(p.add(a).has_value(), "the first is admitted");
    auto second = p.add(b);
    check(!second.has_value(), "the second, which spends the same output, is not");
    if (!second)
        check(second.error() == mempool::Refusal::Conflict, "and is refused as a conflict");
}

void size_and_space_are_bounded() {
    mempool::Pool p;
    check(p.bytes_available() == mempool::kMaxPoolSize, "an empty pool has all its space");
    auto a = tx_spending(1, 10);
    check(p.add(a).has_value(), "admit one");
    check(p.bytes_available() == mempool::kMaxPoolSize - a->size(),
          "the space it takes is exactly its own bytes");
    p.remove({a});
    check(p.bytes_available() == mempool::kMaxPoolSize, "and comes back when it leaves");
    check(p.size() == 0, "the pool is empty again");
}

// The two bounds themselves, and the reason each gives. Byte accounting alone
// says nothing about whether either bound is ENFORCED, and Go asks for both
// through the gate (network_test.go's "tx too big" and "mempool full" cases), so
// they are asked through the gate here too: a caller must not be able to reach
// past the chain's opinion into an unbounded pool.
void the_two_bounds_are_enforced() {
    {
        // A transaction larger than a transaction may be. The memo is the one
        // field a caller can make arbitrarily long, and Pool::add weighs the
        // WIRE bytes rather than trusting a declared length.
        mempool::Pool p;
        Verdict v;
        mempool::Gossip g(p, v);

        auto big = tx_spending(1, 10, mempool::kMaxTxSize + 1024);
        check(big->size() > mempool::kMaxTxSize, "the transaction is over the per-tx bound");
        auto r = g.add(big);
        check(!r, "an oversize transaction is refused");
        check(!r && r.error().find(mempool::kErrTxTooLarge) != std::string::npos,
              "…as too large");
        check(p.size() == 0, "…and never enters the pool");
        check(p.bytes_available() == mempool::kMaxPoolSize, "…so it costs no space");
        check(v.asked == 1,
              "the chain was still asked first — size is the pool's rule, not the chain's");

        // One byte under the bound is admitted, so the refusal above is the bound
        // and not some other thing about a large memo.
        auto fits = tx_spending(2, 20, mempool::kMaxTxSize - 4096);
        check(fits->size() <= mempool::kMaxTxSize, "a transaction just under the bound");
        check(g.add(fits).has_value(), "…is admitted");
    }
    {
        // A pool with no room left. Filling it takes the whole 64 MiB, which is
        // the point: the bound is on the SUM, so nothing short of reaching it
        // proves the sum is what is being checked.
        mempool::Pool p;
        Verdict v;
        mempool::Gossip g(p, v);

        std::size_t admitted = 0;
        std::shared_ptr<txs::Tx> refused;
        std::string reason;
        for (std::uint8_t i = 1; i < 200; ++i) {
            auto tx = tx_spending(i, 1000 + i, mempool::kMaxTxSize - 4096);
            auto r = g.add(tx);
            if (r) {
                ++admitted;
                continue;
            }
            refused = tx;
            reason = r.error();
            break;
        }
        check(refused != nullptr, "the pool eventually fills");
        check(reason.find(mempool::kErrPoolFull) != std::string::npos,
              "…and the transaction that would overflow it is refused as full");
        check(p.bytes_available() < refused->size(),
              "…because what is left is smaller than that transaction");
        check(p.size() == admitted, "…and the pool still holds exactly what it admitted");

        // A full pool says nothing about the transaction, so the refusal is NOT
        // remembered as a drop reason: the same transaction offered after a block
        // drained the pool has to get a fresh look.
        check(g.drop_reason(refused->id()).empty(),
              "…and 'full' is not remembered against the transaction");
    }
}

void removing_takes_the_conflicts_too() {
    mempool::Pool p;
    auto a = tx_spending(1, 10);   // pooled
    auto rival = tx_spending(1, 11);  // never pooled; spends what `a` spends
    auto other = tx_spending(2, 20);
    check(p.add(a).has_value() && p.add(other).has_value(), "two unrelated txs are pooled");

    // A block accepted `rival`. Everything it conflicts with is dead.
    p.remove({rival});
    check(p.get(a->id()) == nullptr, "the pooled tx it conflicts with is gone");
    check(p.get(other->id()) != nullptr, "the unrelated one is untouched");
    check(p.bytes_available() == mempool::kMaxPoolSize - other->size(),
          "and the space it held came back");
}

void a_drop_reason_is_remembered_but_not_always() {
    // The refusal cache belongs to the GATE, not to the pool: "the pool is
    // full" is one of the four things a POOL can say, and every other reason a
    // submitter is told came from an execution the pool knows nothing about.
    {
        mempool::Pool p;
        Verdict v;
        auto a = tx_spending(1, 10);
        v.refuse[a->id()] = "because";
        mempool::Gossip g(p, v);
        check(!g.add(a).has_value(), "the gate refuses what this node's execution refuses");
        check_eq(g.drop_reason(a->id()), "because", "and remembers why");
    }
    {  // A pooled transaction was not dropped.
        mempool::Pool p;
        Verdict v;
        mempool::Gossip g(p, v);
        auto b = tx_spending(2, 20);
        check(g.add(b).has_value(), "pool it");
        g.mark_dropped(b->id(), "because");
        check_eq(g.drop_reason(b->id()), "", "a pooled tx is never marked dropped");
    }
    {  // "The pool is full" says nothing about the transaction, so remembering
       // it would refuse a good tx forever once space freed up.
        mempool::Pool p(10);
        Verdict v;
        mempool::Gossip g(p, v);
        auto c = tx_spending(3, 30);
        check(!g.add(c).has_value(), "a pool with no room refuses it");
        check_eq(g.drop_reason(c->id()), "", "a full pool is not a reason to refuse a tx later");
    }
    {  // A remembered refusal is what step 2 answers with, so `add` never gets
       // past it — that is the whole point of remembering. What DOES clear the
       // record is the transaction actually reaching the pool, which is the
       // path a caller that has already verified it takes.
        mempool::Pool p;
        Verdict v;
        mempool::Gossip g(p, v);
        auto d = tx_spending(4, 40);
        g.mark_dropped(d->id(), "earlier");
        check_eq(g.drop_reason(d->id()), "earlier", "a refusal is remembered");
        check(!g.add(d).has_value(), "and the gate answers with it rather than re-verifying");
        check(g.add_unverified(d).has_value(), "a caller that has verified it admits it");
        check_eq(g.drop_reason(d->id()), "", "and a pooled tx is not a dropped tx");
    }
}

void the_reason_cache_forgets_the_oldest() {
    mempool::Pool p;
    Verdict v;
    mempool::Gossip g(p, v);
    std::vector<Id> ids;
    for (std::size_t i = 0; i < mempool::kDroppedRemembered + 1; ++i) {
        Id key = id(std::uint8_t(i));
        ids.push_back(key);
        g.mark_dropped(key, "reason " + std::to_string(i));
    }
    check_eq(g.drop_reason(ids.front()), "", "the oldest reason has been forgotten");
    check_eq(g.drop_reason(ids.back()), "reason " + std::to_string(mempool::kDroppedRemembered),
             "the newest is still remembered");
}

// ---- the gate ----

void the_gate_admits_what_the_chain_accepts() {
    mempool::Pool p;
    Verdict v;
    mempool::Gossip g(p, v);
    auto a = tx_spending(1, 10);

    check(g.add(a).has_value(), "a transaction the chain accepts is admitted");
    check(v.asked == 1, "and the chain was asked exactly once");
    check(g.has(a->id()), "the gate reports holding it");
    check(g.bloom().has(a->id()), "and the filter a peer samples says so too");
}

void the_gate_refuses_a_duplicate_without_asking_the_chain() {
    mempool::Pool p;
    Verdict v;
    mempool::Gossip g(p, v);
    auto a = tx_spending(1, 10);
    check(g.add(a).has_value(), "admit it");
    const int asked = v.asked;
    auto again = g.add(a);
    check(!again.has_value(), "the same transaction again is refused");
    check(v.asked == asked, "and the chain was not asked a second time");
}

void a_refusal_is_remembered_and_reused() {
    mempool::Pool p;
    Verdict v;
    auto a = tx_spending(1, 10);
    v.refuse[a->id()] = "the chain says no";
    mempool::Gossip g(p, v);

    auto first = g.add(a);
    check(!first.has_value(), "the chain refuses it");
    if (first) return;
    check_eq(first.error(), "the chain says no", "with its own reason");
    check(!g.has(a->id()), "so it is not held");

    const int asked = v.asked;
    auto second = g.add(a);
    check(!second.has_value(), "offered again, it is refused again");
    if (second) return;
    check_eq(second.error(), "the chain says no", "with the ORIGINAL reason");
    check(v.asked == asked, "and without asking the chain a second time");
}

void unverified_admission_skips_only_the_chain() {
    mempool::Pool p;
    Verdict v;
    auto a = tx_spending(1, 10);
    v.refuse[a->id()] = "the chain says no";
    mempool::Gossip g(p, v);

    check(g.add_unverified(a).has_value(), "a caller that already verified may say so");
    check(g.has(a->id()), "and the transaction is held");
    // It still had to pass the structural checks.
    auto rival = tx_spending(1, 11);
    check(!g.add_unverified(rival).has_value(), "but a conflict is still refused");
    check_eq(g.drop_reason(rival->id()).empty() ? "" : "remembered", "remembered",
             "and the refusal is remembered");
}

// ---- the filter ----

void the_filter_never_says_no_to_what_it_holds() {
    // A bloom filter may say "maybe" about something absent. It may never say
    // "no" about something present — that would have a peer skip a transaction
    // this node actually wants.
    mempool::Bloom bloom(64, 0.01, 0.05);
    std::vector<Id> added;
    for (int i = 0; i < 64; ++i) {
        Id key = id(std::uint8_t(i));
        added.push_back(key);
        bloom.add(key);
    }
    int wrong = 0;
    for (const auto& key : added) {
        if (!bloom.has(key)) ++wrong;
    }
    check(wrong == 0, "everything added is reported present");
}

void the_filter_marshals_the_shape_a_peer_reads() {
    // The wire form: one byte of hash count, that many big-endian seeds, then
    // the bits. Anything else and a peer reads a different filter.
    mempool::Bloom bloom(8, 0.01, 0.05, [](std::span<std::uint8_t> out) {
        for (std::size_t i = 0; i < out.size(); ++i) out[i] = std::uint8_t(i + 1);
    });
    const Bytes wire = bloom.marshal();
    check(!wire.empty(), "the filter marshals");
    if (wire.empty()) return;
    check(int(wire[0]) == bloom.num_hashes(), "the first byte is the hash count");
    check(wire.size() == 1 + std::size_t(bloom.num_hashes()) * 8 + bloom.num_entries(),
          "and the rest is the seeds and the bits, exactly");
    // A stated fill makes the salt stated too, which is what a peer needs.
    check(bloom.salt()[0] == 1 && bloom.salt()[1] == 2, "the salt comes from the given source");
}

void the_filter_is_rebuilt_when_it_fills() {
    mempool::Bloom bloom(1, 0.01, 0.05);
    const std::size_t max = bloom.max_count();
    check(max > 0, "a filter knows how much it can hold");
    for (std::size_t i = 0; i <= max; ++i) bloom.add(id(std::uint8_t(i)));
    check(bloom.count() == max + 1, "fill it past that");
    check(bloom.reset_if_needed(16), "it asks to be rebuilt");
    check(bloom.count() == 0, "and comes back empty");
    check(!bloom.reset_if_needed(16), "an empty filter does not ask again");
}

void the_gate_refills_a_rebuilt_filter() {
    // A rebuilt filter that was left empty would tell every peer this node has
    // nothing, and the transactions it holds would never be asked for again.
    mempool::Pool p;
    Verdict v;
    mempool::Gossip g(p, v, mempool::BloomParams{1, 0.5, 0.9});
    std::vector<std::shared_ptr<txs::Tx>> held;
    for (int i = 0; i < 40; ++i) {
        auto tx = tx_spending(std::uint8_t(i), 10);
        if (g.add(tx).has_value()) held.push_back(tx);
    }
    check(held.size() == 40, "forty transactions are held");
    int missing = 0;
    for (const auto& tx : held) {
        if (!g.bloom().has(tx->id())) ++missing;
    }
    check(missing == 0, "and every one of them is in the filter, rebuilds included");
}

void gossip_bytes_are_the_transaction() {
    auto a = tx_spending(1, 10);
    auto back = mempool::unmarshal(mempool::marshal(*a));
    check(back.has_value(), "a gossiped transaction parses");
    if (!back) return;
    check((*back)->id() == a->id(), "into the same transaction — there is no gossip envelope");
}

}  // namespace

int main() {
    std::printf("mempool\n");
    the_pool_holds_and_orders();
    a_duplicate_is_refused();
    a_conflict_is_refused();
    size_and_space_are_bounded();
    the_two_bounds_are_enforced();
    removing_takes_the_conflicts_too();
    a_drop_reason_is_remembered_but_not_always();
    the_reason_cache_forgets_the_oldest();
    the_gate_admits_what_the_chain_accepts();
    the_gate_refuses_a_duplicate_without_asking_the_chain();
    a_refusal_is_remembered_and_reused();
    unverified_admission_skips_only_the_chain();
    the_filter_never_says_no_to_what_it_holds();
    the_filter_marshals_the_shape_a_peer_reads();
    the_filter_is_rebuilt_when_it_fills();
    the_gate_refills_a_rebuilt_filter();
    gossip_bytes_are_the_transaction();
    return report("mempool");
}
