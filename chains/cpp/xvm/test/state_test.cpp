// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state_test.cpp — what the chain remembers, ported from state/state_test.go
// and state/iterator_test.go.
//
// Two things are under test and they are not the same thing. The first is the
// store: put a UTXO, a tx, a block; get them back; delete and stop getting them
// — and the SAME case list run against a Diff, because a Diff must be
// indistinguishable from the chain it covers (Go runs one ChainUTXOTest against
// both for exactly that reason).
//
// The second is ENUMERATION ORDER, and it is load-bearing rather than cosmetic:
// the execution root is a fold over the occupied set, so two nodes that
// enumerate the same set in different orders compute different roots and never
// agree. Ascending UTXOID, deletions applied, overlay shadowing the parent —
// every one of those is a consensus rule wearing the clothes of an iterator.

#include "check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/block.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/txs.hpp"

#include <algorithm>

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

fx::OutputOwners owner() { return fx::OutputOwners{0, 1, {test_address(0)}}; }

std::shared_ptr<fx::secp256k1fx::TransferOutput> tout(std::uint64_t amt) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = owner();
    return o;
}

// utxo_with is Go's utxoWith: one UTXO of a given tx, index and amount.
txs::UTXO utxo_with(const Id& tx_id, std::uint32_t index, std::uint64_t amt) {
    txs::UTXO u;
    u.utxo_id = txs::UTXOID{tx_id, index, false};
    u.asset_id = id(0x50);
    u.out = tout(amt);
    return u;
}

std::shared_ptr<txs::Tx> a_tx(std::uint8_t seed) {
    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = 369;
    utx->base.blockchain_id = id(seed);
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

std::shared_ptr<block::StandardBlock> a_block(const Id& parent, std::uint64_t height,
                                              std::uint8_t seed) {
    auto blk = block::build(parent, height, 1600000000, kEmptyId, {a_tx(seed)});
    return blk ? *blk : nullptr;
}

std::vector<Id> ids_of(const std::vector<txs::UTXO>& us) {
    std::vector<Id> out;
    out.reserve(us.size());
    for (const auto& u : us) out.push_back(u.utxo_id.input_id());
    return out;
}

std::vector<Id> sorted_ids_of(const std::vector<txs::UTXO>& us) {
    auto out = ids_of(us);
    std::sort(out.begin(), out.end());
    return out;
}

bool not_found(const wire::Result<txs::UTXO>& r) {
    return !r && r.error().find(state::kErrNotFound) != std::string::npos;
}

// ================= the store, run against BOTH a State and a Diff =================

// chain_cases is Go's ChainUTXOTest + ChainTxTest + ChainBlockTest, run against
// whatever Chain it is handed. A Diff that answered differently from the State
// under it would make a block verify against one and fail against the other.
void chain_cases(state::Chain& c, const txs::UTXO& seeded, const std::shared_ptr<txs::Tx>& seed_tx,
                 const std::shared_ptr<block::StandardBlock>& seed_blk, const char* which) {
    std::printf("\n  -- the store, via %s --\n", which);

    {
        auto got = c.get_utxo(seeded.utxo_id.input_id());
        check(got && got->utxo_id.input_id() == seeded.utxo_id.input_id(),
              "the seeded UTXO is there");
    }
    {
        auto u = utxo_with(id(0x9a), 3, 1);
        const Id uid = u.utxo_id.input_id();
        check(not_found(c.get_utxo(uid)), "an unwritten UTXO is not found");
        c.add_utxo(u);
        auto got = c.get_utxo(uid);
        check(got && got->utxo_id.input_id() == uid, "…then it is");
        check(got && got->out->bytes() == u.out->bytes(), "…with the output it was given");
        c.delete_utxo(uid);
        check(not_found(c.get_utxo(uid)), "…and deleting it removes it again");
    }
    {
        auto got = c.get_tx(seed_tx->id());
        check(got && (*got)->id() == seed_tx->id(), "the seeded tx is there");
        auto tx = a_tx(0x40);
        auto missing = c.get_tx(tx->id());
        check(!missing, "an unwritten tx is not found");
        c.add_tx(tx);
        auto now = c.get_tx(tx->id());
        check(now && (*now)->id() == tx->id(), "…then it is");
    }
    {
        auto by_height = c.get_block_id_at_height(seed_blk->height);
        check(by_height && *by_height == seed_blk->block_id, "the seeded block is at its height");
        auto by_id = c.get_block(seed_blk->block_id);
        check(by_id && (*by_id)->block_id == seed_blk->block_id, "…and by its id");

        auto blk = a_block(id(0x60), 10, 0x61);
        check(blk != nullptr, "a second block builds");
        check(!c.get_block_id_at_height(blk->height), "an unwritten height is not found");
        check(!c.get_block(blk->block_id), "an unwritten block is not found");
        c.add_block(blk);
        auto h = c.get_block_id_at_height(blk->height);
        check(h && *h == blk->block_id, "…then the height resolves");
        auto b = c.get_block(blk->block_id);
        check(b && (*b)->block_id == blk->block_id, "…and the block is there");
    }
    {
        c.set_last_accepted(seed_blk->block_id);
        check(c.get_last_accepted() == seed_blk->block_id, "last accepted round-trips");
        c.set_timestamp(1234567);
        check(c.get_timestamp() == 1234567, "timestamp round-trips");
    }
}

// versions_of hands every lookup the one chain under test — Go's stateGetter.
struct OneChain final : state::Versions {
    state::Chain* chain;
    explicit OneChain(state::Chain* c) : chain(c) {}
    state::Chain* get_state(const Id&) override { return chain; }
};

void store_cases() {
    const auto seeded = utxo_with(id(0x11), 1, 100);
    auto seed_tx = a_tx(0x20);
    auto seed_blk = a_block(id(0x30), 7, 0x31);

    {
        test::MemoryState s;
        s.add_utxo(seeded);
        s.add_tx(seed_tx);
        s.add_block(seed_blk);
        chain_cases(s, seeded, seed_tx, seed_blk, "State");
    }
    {
        test::MemoryState s;
        s.add_utxo(seeded);
        s.add_tx(seed_tx);
        s.add_block(seed_blk);
        OneChain versions(&s);
        auto d = state::Diff::create(kEmptyId, versions);
        if (!d) {
            check(false, "diff over the state: " + d.error());
            return;
        }
        chain_cases(**d, seeded, seed_tx, seed_blk, "Diff");
    }
}

// ================= a diff is not the chain until it is applied =================

void diff_isolation() {
    std::printf("\n  -- a diff records, it does not write --\n");

    test::MemoryState s;
    const auto kept = utxo_with(id(0x11), 0, 10);
    const auto doomed = utxo_with(id(0x11), 1, 11);
    s.add_utxo(kept);
    s.add_utxo(doomed);
    s.set_timestamp(1000);

    OneChain versions(&s);
    auto d = state::Diff::create(kEmptyId, versions);
    if (!d) {
        check(false, "diff: " + d.error());
        return;
    }
    check((*d)->get_timestamp() == 1000, "a fresh diff inherits the parent's timestamp");

    const auto added = utxo_with(id(0x11), 2, 12);
    (*d)->add_utxo(added);
    (*d)->delete_utxo(doomed.utxo_id.input_id());
    (*d)->set_timestamp(2000);

    // Nothing the diff did is visible in the chain it covers.
    check(not_found(s.get_utxo(added.utxo_id.input_id())), "the added UTXO is not in the state");
    check(s.get_utxo(doomed.utxo_id.input_id()).has_value(),
          "the deleted UTXO is still in the state");
    check(s.get_timestamp() == 1000, "the state's timestamp is untouched");

    // …but all of it is visible through the diff.
    check((*d)->get_utxo(added.utxo_id.input_id()).has_value(), "the diff sees its own addition");
    check(not_found((*d)->get_utxo(doomed.utxo_id.input_id())), "…and its own deletion");
    check((*d)->get_utxo(kept.utxo_id.input_id()).has_value(),
          "…and the parent's untouched UTXOs");

    // Applying is acceptance, and it is the only moment the two agree.
    (*d)->apply(s);
    check(s.get_utxo(added.utxo_id.input_id()).has_value(), "apply writes the addition");
    check(not_found(s.get_utxo(doomed.utxo_id.input_id())), "apply writes the deletion");
    check(s.get_timestamp() == 2000, "apply writes the timestamp");
    check(s.utxo_count() == 2, "the set is the parent's, plus one, minus one");
}

void missing_parent() {
    std::printf("\n  -- a diff with no parent --\n");
    struct NoChain final : state::Versions {
        state::Chain* get_state(const Id&) override { return nullptr; }
    } none;
    auto d = state::Diff::create(id(0x77), none);
    check(!d && d.error().find(state::kErrMissingParentState) != std::string::npos,
          "a diff over a parent that is not there fails to open");
}

// ================= enumeration order =================

void ascending_order() {
    std::printf("\n  -- enumeration --\n");

    test::MemoryState s;
    const Id tx_id = id(0x81);
    std::vector<txs::UTXO> want;
    for (std::uint32_t i = 0; i < 16; ++i) {
        auto u = utxo_with(tx_id, i, 100 + i);
        want.push_back(u);
        s.add_utxo(u);
    }
    auto got = s.utxos(kEmptyId, 0);
    const auto got_ids = ids_of(got);
    check(got.size() == want.size(), "every occupied UTXO is enumerated");
    check(got_ids == sorted_ids_of(want), "…in ascending UTXOID order");
    check(std::is_sorted(got_ids.begin(), got_ids.end()), "…which is a total order");
}

void overlay_and_removal() {
    test::MemoryState s;
    const Id tx_id = id(0x82);
    std::vector<txs::UTXO> committed;
    for (std::uint32_t i = 0; i < 4; ++i) {
        committed.push_back(utxo_with(tx_id, i, 10 + i));
        s.add_utxo(committed.back());
    }
    s.delete_utxo(committed[1].utxo_id.input_id());
    s.add_utxo(utxo_with(tx_id, 100, 999));
    s.add_utxo(utxo_with(tx_id, 101, 1000));

    auto got = s.utxos(kEmptyId, 0);
    auto got_ids = ids_of(got);
    check(got.size() == 5, "a deletion removes exactly one entry");
    check(std::find(got_ids.begin(), got_ids.end(), committed[1].utxo_id.input_id()) ==
              got_ids.end(),
          "a deleted UTXO does not appear");
    check(std::is_sorted(got_ids.begin(), got_ids.end()), "the merged stream stays ascending");

    // Writing the same UTXOID twice replaces, never duplicates: the id is the
    // identity, so two rows under one id would be two answers to one question.
    test::MemoryState r;
    auto orig = utxo_with(tx_id, 7, 500);
    r.add_utxo(orig);
    auto replacement = utxo_with(tx_id, 7, 777);
    check(replacement.utxo_id.input_id() == orig.utxo_id.input_id(), "the same id");
    r.add_utxo(replacement);
    auto rgot = r.utxos(kEmptyId, 0);
    check(rgot.size() == 1, "the same UTXOID appears exactly once");
    if (rgot.size() == 1) {
        auto* v = dynamic_cast<fx::secp256k1fx::TransferOutput*>(rgot[0].out.get());
        check(v != nullptr && v->amt == 777, "…and it is the later value");
    }
}

void start_and_limit() {
    test::MemoryState s;
    const Id tx_id = id(0x83);
    std::vector<txs::UTXO> all;
    for (std::uint32_t i = 0; i < 20; ++i) {
        all.push_back(utxo_with(tx_id, i, i));
        s.add_utxo(all.back());
    }
    const auto order = sorted_ids_of(all);

    auto page = s.utxos(kEmptyId, 5);
    check(ids_of(page) == std::vector<Id>(order.begin(), order.begin() + 5),
          "a limit returns the first page");

    auto page2 = s.utxos(order[4], 5);
    check(ids_of(page2) == std::vector<Id>(order.begin() + 5, order.begin() + 10),
          "…and a start resumes strictly after it");

    std::vector<Id> walked;
    Id start = kEmptyId;
    while (true) {
        auto p = s.utxos(start, 7);
        if (p.empty()) break;
        auto pid = ids_of(p);
        walked.insert(walked.end(), pid.begin(), pid.end());
        start = pid.back();
    }
    check(walked == order, "paging the whole set walks it exactly once, in order");
}

void empty_and_phantom() {
    {
        test::MemoryState s;
        check(s.utxos(kEmptyId, 0).empty(), "an empty set enumerates to nothing");
    }
    {
        // Deleting something that was never there changes nothing. A phantom
        // removal that shortened the set would silently change the root.
        test::MemoryState s;
        const Id tx_id = id(0x84);
        std::vector<txs::UTXO> committed;
        for (std::uint32_t i = 0; i < 3; ++i) {
            committed.push_back(utxo_with(tx_id, i, i));
            s.add_utxo(committed.back());
        }
        s.delete_utxo(utxo_with(tx_id, 9999, 0).utxo_id.input_id());
        s.delete_utxo(utxo_with(id(0x85), 0, 0).utxo_id.input_id());
        check(ids_of(s.utxos(kEmptyId, 0)) == sorted_ids_of(committed),
              "phantom removals leave the set intact and ordered");
    }
    {
        // Repeating the enumeration gives the same answer. It has to: the root
        // is a fold over it.
        test::MemoryState s;
        const Id tx_id = id(0x86);
        for (std::uint32_t i = 0; i < 50; ++i) s.add_utxo(utxo_with(tx_id, i, i));
        const auto first = ids_of(s.utxos(kEmptyId, 0));
        bool same = true;
        for (int i = 0; i < 8; ++i) same = same && (ids_of(s.utxos(kEmptyId, 0)) == first);
        check(same, "enumeration is deterministic across runs");
    }
}

void diff_enumeration() {
    std::printf("\n  -- enumeration through a diff --\n");

    test::MemoryState s;
    const Id tx_id = id(0x87);
    std::vector<txs::UTXO> parent;
    for (std::uint32_t i = 0; i < 6; ++i) {
        parent.push_back(utxo_with(tx_id, i, i));
        s.add_utxo(parent.back());
    }
    OneChain versions(&s);
    auto d = state::Diff::create(kEmptyId, versions);
    if (!d) {
        check(false, "diff: " + d.error());
        return;
    }
    auto fresh = utxo_with(tx_id, 200, 12345);
    (*d)->add_utxo(fresh);
    (*d)->delete_utxo(parent[0].utxo_id.input_id());
    (*d)->delete_utxo(parent[3].utxo_id.input_id());

    auto got = ids_of((*d)->utxos(kEmptyId, 0));
    check(got.size() == 5, "the parent's set, minus two, plus one");
    check(std::find(got.begin(), got.end(), parent[0].utxo_id.input_id()) == got.end(),
          "a deletion in the diff hides the parent's row");
    check(std::find(got.begin(), got.end(), parent[3].utxo_id.input_id()) == got.end(),
          "…for every deletion");
    check(std::find(got.begin(), got.end(), fresh.utxo_id.input_id()) != got.end(),
          "…and the diff's own addition is there");
    check(std::is_sorted(got.begin(), got.end()), "the merged stream stays ascending");

    // A parent larger than any one page: the merge must not stop at a boundary.
    test::MemoryState big;
    std::vector<txs::UTXO> many;
    for (std::uint32_t i = 0; i < 537; ++i) {
        many.push_back(utxo_with(tx_id, i, i));
        big.add_utxo(many.back());
    }
    OneChain big_versions(&big);
    auto bd = state::Diff::create(kEmptyId, big_versions);
    if (!bd) {
        check(false, "diff over a large parent: " + bd.error());
        return;
    }
    auto all = ids_of((*bd)->utxos(kEmptyId, 0));
    check(all.size() == many.size(), "a large parent enumerates completely through a diff");
    check(all == sorted_ids_of(many), "…in ascending order across page boundaries");
}

}  // namespace

int main() {
    std::printf("xvm — the chain's memory, ported from the Go state tests\n");
    store_cases();
    diff_isolation();
    missing_parent();
    ascending_order();
    overlay_and_removal();
    start_and_limit();
    empty_and_phantom();
    diff_enumeration();
    return report("state");
}
