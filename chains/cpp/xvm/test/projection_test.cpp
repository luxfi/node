// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// projection_test.cpp — the projection from chain state to the execution root's
// leaves, and the fold over them.
//
// Ported from block/executor/projection_test.go and
// block/executor/executionroot_test.go.
//
// root_test.cpp pins the fold against the frozen cross-language KAT: one fixture,
// one answer, every backend. This file pins the OTHER half — that the CHAIN's own
// state reaches those leaves with the right field in every slot, and that the fold
// holds over shapes the KAT does not visit (0, 1, odd, even, and the counts where
// lone-right promotion lives).
//
// PORTING NOTE, stated because it changes what the size sweep proves.
// Go's TestParallelFamilyRootsMatchSerial / TestParallelExecutionRootMatchesSerial
// are a differential between TWO implementations of one fold: the frozen serial
// xvmroot oracle and the concurrent worker-pool fold the block executor runs. This
// port has ONE fold (src/root.cpp) and no parallel one, so "parallel == serial"
// cannot be asked here and asking it of a single implementation would be a test
// that passes because nothing was compared.
//
// What is asked instead is the same QUESTION against an independent oracle written
// in this file: RFC-6962's own recursive definition,
//
//     MTH(D[n]) = NodeHash( MTH(D[0:k]), MTH(D[k:n]) ),  k the largest power of
//                                                        two strictly below n
//
// which is a different algorithm from the iterative bottom-up promotion in
// src/root.cpp (and in Go's merkle.Root) and agrees with it only if the promotion
// rule is right. The sweep covers Go's exact leaf counts, and the leaves carry
// Go's exact occupancy mix, so a promotion or compaction bug moves the answer here
// just as it would move it there.

#include "lux/core/check.hpp"
#include "fixtures.hpp"

#include "lux/xvm/root.hpp"
#include "lux/xvm/state.hpp"

#include <algorithm>
#include <cstring>
#include <random>
#include <vector>

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

// ================= Go's helpers, ported =================

// shortIDs builds n distinct 20-byte addresses from a seed byte — Go's
// shortIDs(seed, n): byte 0 is the seed and byte 19 is the ordinal, so the set is
// deliberately NOT in ascending byte order for n > 1 once it is reversed.
std::vector<ShortId> short_ids(std::uint8_t seed, int n) {
    std::vector<ShortId> out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        ShortId a{};
        a[0] = seed;
        a[19] = std::uint8_t(i);
        out[std::size_t(i)] = a;
    }
    return out;
}

// transferOut builds a secp256k1fx transfer output with a SORTED owner set —
// Go's transferOut, which calls o.Sort().
std::shared_ptr<fx::secp256k1fx::TransferOutput> transfer_out(std::uint64_t amount,
                                                              std::uint32_t threshold,
                                                              std::uint64_t locktime,
                                                              std::vector<ShortId> addrs) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amount;
    o->out_owners.locktime = locktime;
    o->out_owners.threshold = threshold;
    o->out_owners.addrs = std::move(addrs);
    o->out_owners.sort();
    return o;
}

// ================= the independent fold =================

// ref_merkle is RFC-6962's own recursive definition, written top-down. It shares
// nothing with src/root.cpp but leaf_hash and node_hash, which root_test.cpp pins
// against the KAT.
root::Digest ref_merkle(const std::vector<root::Digest>& leaves, std::size_t lo, std::size_t hi) {
    if (hi - lo == 1) return root::leaf_hash(leaves[lo]);
    std::size_t k = 1;
    while (k * 2 < hi - lo) k *= 2;
    return root::node_hash(ref_merkle(leaves, lo, lo + k), ref_merkle(leaves, lo + k, hi));
}
root::Digest ref_merkle(const std::vector<root::Digest>& leaves) {
    if (leaves.empty()) return root::empty_root();
    return ref_merkle(leaves, 0, leaves.size());
}

// The three family folds, over the reference tree. The compaction rule (skip an
// unoccupied slot, but bind the SLOT index into the leaf) is restated here rather
// than reused, because it is half of what is being checked.
root::Digest ref_utxo_root(const std::vector<root::UTXOLeaf>& us) {
    std::vector<root::Digest> leaves;
    for (std::size_t i = 0; i < us.size(); ++i) {
        if ((us[i].status & root::kUTXOOccupied) == 0) continue;
        leaves.push_back(root::utxo_leaf_digest(us[i], std::uint32_t(i)));
    }
    return ref_merkle(leaves);
}
root::Digest ref_asset_root(const std::vector<root::AssetLeaf>& as) {
    std::vector<root::Digest> leaves;
    for (std::size_t i = 0; i < as.size(); ++i) {
        if (as[i].occupied == 0) continue;
        leaves.push_back(root::asset_leaf_digest(as[i], std::uint32_t(i)));
    }
    return ref_merkle(leaves);
}
root::Digest ref_tx_root(const std::vector<root::TxLeaf>& ts) {
    std::vector<root::Digest> leaves;
    for (std::size_t i = 0; i < ts.size(); ++i)
        leaves.push_back(root::tx_leaf_digest(ts[i], std::uint32_t(i)));
    return ref_merkle(leaves);
}

// ================= random leaves, Go's shapes =================
//
// The BYTES differ from Go's (the two languages have different generators); the
// SHAPES are Go's — random fields, and roughly half the UTXO/asset slots left
// unoccupied so the compaction is exercised over a non-trivial subset.

using Rng = std::mt19937_64;

void fill_id(Rng& rng, Id& id) {
    for (auto& b : id) b = std::uint8_t(rng());
}

std::vector<root::UTXOLeaf> rand_utxo_leaves(Rng& rng, int n) {
    std::vector<root::UTXOLeaf> us(static_cast<std::size_t>(n));
    for (auto& u : us) {
        fill_id(rng, u.utxo_id);
        fill_id(rng, u.asset_id);
        fill_id(rng, u.owner_root);
        u.amount_lo = rng();
        u.amount_hi = rng();
        u.locktime = rng();
        u.threshold = std::uint32_t(rng());
        if (rng() % 2 == 0) u.status = root::kUTXOOccupied;
    }
    return us;
}

std::vector<root::AssetLeaf> rand_asset_leaves(Rng& rng, int n) {
    std::vector<root::AssetLeaf> as(static_cast<std::size_t>(n));
    for (auto& a : as) {
        fill_id(rng, a.asset_id);
        fill_id(rng, a.mint_authority_root);
        a.total_supply_lo = rng();
        a.total_supply_hi = rng();
        a.freeze_flag = std::uint32_t(rng());
        a.denomination = std::uint32_t(rng());
        if (rng() % 2 == 0) a.occupied = 1;
    }
    return as;
}

std::vector<root::TxLeaf> rand_tx_leaves(Rng& rng, int n) {
    std::vector<root::TxLeaf> ts(static_cast<std::size_t>(n));
    for (auto& t : ts) {
        fill_id(rng, t.tx_id);
        fill_id(rng, t.proof_digest);
        t.kind = std::uint32_t(rng());
        t.status = std::uint32_t(rng());
        t.reject_reason = std::uint32_t(rng());
    }
    return ts;
}

// ================= owner_root =================

// TestOwnerRootMatchesGPULayoutIntent — the canonical owner_root preimage, built
// here by hand from the documented definition rather than read out of root.cpp:
//
//     keccak256( threshold_le ‖ key_count_le ‖ sorted_key[0] ‖ … )
void owner_root_matches_the_documented_preimage() {
    std::printf("\n  -- owner_root is the documented preimage --\n");

    auto addrs = short_ids(0xAB, 3);
    auto o = transfer_out(1000, 2, 42, addrs);
    auto owners = root::extract_owners(*o);
    check(owners.has_value(), "a transfer output has a canonical owner model");
    if (!owners) return;

    // The independent reference preimage: threshold_le ‖ count_le ‖ sorted addrs.
    std::vector<Bytes> sorted;
    for (const auto& a : addrs) sorted.emplace_back(a.begin(), a.end());
    std::sort(sorted.begin(), sorted.end());

    Bytes pre;
    pre.push_back(2);  // threshold = 2, LE u32
    pre.push_back(0);
    pre.push_back(0);
    pre.push_back(0);
    pre.push_back(std::uint8_t(sorted.size()));  // count = 3, LE u32
    pre.push_back(0);
    pre.push_back(0);
    pre.push_back(0);
    for (const auto& a : sorted) pre.insert(pre.end(), a.begin(), a.end());
    const auto want = root::keccak({view(pre)});

    check_eq(hex_of(root::owner_root(*owners)), hex_of(want),
             "owner_root equals the documented canonical preimage");

    // The locktime is carried on Owners but is NOT in the preimage: it reaches the
    // root through the UTXO leaf's own locktime field instead, and hashing it twice
    // would let two different states share a leaf.
    auto other_locktime = transfer_out(1000, 2, 43, addrs);
    auto ol = root::extract_owners(*other_locktime);
    check(ol && root::owner_root(*ol) == root::owner_root(*owners),
          "…and the locktime is not in it — the leaf carries that");
}

// TestOwnerRootDeterministicAcrossOrder — owner_root is a function of the address
// SET and the threshold, never of the order the addresses were listed in.
void owner_root_does_not_depend_on_listing_order() {
    std::printf("\n  -- owner_root over a set, not a list --\n");

    auto addrs = short_ids(0x11, 4);
    auto forward = transfer_out(5, 2, 0, addrs);

    // Reversed, and built WITHOUT sort() — so the derivation itself has to sort.
    auto reversed = addrs;
    std::reverse(reversed.begin(), reversed.end());
    auto rev = std::make_shared<fx::secp256k1fx::TransferOutput>();
    rev->amt = 5;
    rev->out_owners.threshold = 2;
    rev->out_owners.addrs = reversed;

    auto of = root::extract_owners(*forward);
    auto orr = root::extract_owners(*rev);
    check(of.has_value(), "the sorted output has an owner model");
    check(orr.has_value(), "…and so does the unsorted one");
    if (!of || !orr) return;
    check(root::owner_root(*of) == root::owner_root(*orr),
          "owner_root does not depend on address listing order");
}

// TestOwnerRootThresholdBinds — two owner sets identical but for the threshold
// must not share a root.
void owner_root_binds_the_threshold() {
    std::printf("\n  -- the threshold is bound in --\n");
    auto addrs = short_ids(0x22, 3);
    auto a = transfer_out(1, 1, 0, addrs);
    auto b = transfer_out(1, 2, 0, addrs);
    auto oa = root::extract_owners(*a);
    auto ob = root::extract_owners(*b);
    check(oa && ob, "both outputs have an owner model");
    if (!oa || !ob) return;
    check(root::owner_root(*oa) != root::owner_root(*ob), "the threshold changes owner_root");
}

// ================= the UTXO leaf =================

// utxo_leaf_of is the port's own projection of ONE UTXO, reconstructed here from
// the public surface: block_execution_root over a state holding exactly that UTXO
// and no transactions must equal a compose over the leaf built by hand. The port
// keeps the per-UTXO projection inside block_execution_root (Go exposes it as
// utxoLeaf), so this is how the field-by-field mapping is reached from outside.
//
// TestUTXOLeafProjection.
void utxo_leaf_projection() {
    std::printf("\n  -- the UTXO leaf, field by field --\n");

    const Id tx_id = id(0x5A);
    const Id asset_id = id(0x7B);
    auto addrs = short_ids(0x33, 2);
    auto out = transfer_out(7777, 1, 99, addrs);

    MemoryState st;
    st.add_utxo(txs::UTXO{txs::UTXOID{tx_id, 5, false}, asset_id, out});

    const Id parent = id(0x02);
    const std::uint64_t height = 41;
    auto got = root::block_execution_root(parent, {}, st, height);
    check(got.has_value(), got ? "the state projects to a root" : "project: " + got.error());
    if (!got) return;

    // The leaf, stated field by field the way Go states it.
    auto owners = root::extract_owners(*out);
    check(owners.has_value(), "the output has an owner model");
    if (!owners) return;

    root::UTXOLeaf leaf;
    leaf.utxo_id = txs::UTXOID{tx_id, 5, false}.input_id();  // the INPUT id, not the tx id
    leaf.asset_id = asset_id;
    leaf.amount_lo = 7777;
    leaf.amount_hi = 0;
    leaf.owner_root = root::owner_root(*owners);
    leaf.locktime = 99;
    leaf.threshold = 1;
    leaf.status = root::kUTXOOccupied;

    const auto want = root::compose(parent, root::utxo_root({leaf}), root::asset_root({}),
                                    root::tx_root({}), height);
    check_eq(hex_of(*got), hex_of(want), "the wired projection equals the hand-built leaf");

    // Each field is load-bearing: change one and the root moves. A projection that
    // dropped a field would still agree with the line above if the hand-built leaf
    // were derived from it, which is why every one is perturbed separately.
    auto moved = [&](root::UTXOLeaf l, const char* what) {
        check(root::compose(parent, root::utxo_root({l}), root::asset_root({}), root::tx_root({}),
                            height) != want,
              std::string("…and ") + what + " is part of it");
    };
    { auto l = leaf; l.amount_lo = 7778; moved(l, "the amount"); }
    { auto l = leaf; l.locktime = 100; moved(l, "the locktime"); }
    { auto l = leaf; l.threshold = 2; moved(l, "the threshold"); }
    { auto l = leaf; l.asset_id = id(0x7C); moved(l, "the asset id"); }
    { auto l = leaf; l.utxo_id = txs::UTXOID{tx_id, 6, false}.input_id(); moved(l, "the output index"); }
    { auto l = leaf; l.owner_root = root::owner_root(root::Owners{1, 0, {}}); moved(l, "the owner root"); }

    // …and the leaf's utxo_id is the INPUT id — the hash of (tx id, index) — not
    // the tx id, which two outputs of one transaction would share.
    check(leaf.utxo_id != tx_id, "the leaf is keyed on the input id, not on the tx id");
}

// ================= the whole block root =================

// TestBlockExecutionRootMatchesHandProjection — a known post-block state's wired
// root equals a compose over the hand-projected leaves.
void block_root_matches_the_hand_projection() {
    std::printf("\n  -- the block root over a known state --\n");

    MemoryState st;
    const Id tx_id = id(0x11);
    struct Spec {
        std::uint32_t index;
        std::uint64_t amount;
        std::uint64_t locktime;
        std::uint32_t threshold;
        std::uint8_t addr_seed;
        int addr_n;
    };
    // Go's four specs, verbatim.
    const Spec specs[] = {
        {0, 1000, 0, 1, 0x01, 1},
        {1, 250, 50, 2, 0x02, 3},
        {2, 9999, 0, 1, 0x03, 2},
        {3, 1, 7, 1, 0x04, 1},
    };
    std::vector<txs::UTXO> written;
    for (const auto& sp : specs) {
        auto out = transfer_out(sp.amount, sp.threshold, sp.locktime,
                                short_ids(sp.addr_seed, sp.addr_n));
        txs::UTXO u{txs::UTXOID{tx_id, sp.index, false}, id(std::uint8_t(0x80 + sp.index)), out};
        st.add_utxo(u);
        written.push_back(u);
    }

    // The block's transactions. Only their ids reach the tx family.
    std::vector<std::shared_ptr<txs::Tx>> blk_txs;
    for (int i = 0; i < 3; ++i) {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = 10;
        utx->base.blockchain_id = id(std::uint8_t(0xB0 + i));
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        auto r = tx->initialize();
        check(r.has_value(), r ? "a block tx initializes" : "tx init: " + r.error());
        blk_txs.push_back(tx);
    }

    const Id parent = id(0xEE);
    const std::uint64_t height = 88;

    auto got = root::block_execution_root(parent, blk_txs, st, height);
    check(got.has_value(), got ? "the wired root computes" : "root: " + got.error());
    if (!got) return;

    // The hand projection: enumerate the SAME occupied set in the SAME canonical
    // order the fold walks, and project each UTXO.
    std::vector<root::UTXOLeaf> hand;
    for (const auto& u : st.utxos(kEmptyId, 0)) {
        auto owners = root::extract_owners(*u.out);
        check(owners.has_value(), "each enumerated UTXO has an owner model");
        if (!owners) return;
        const auto* amt = dynamic_cast<const fx::FxTransferOut*>(u.out.get());
        hand.push_back(root::UTXOLeaf{u.utxo_id.input_id(), u.asset_id,
                                      amt == nullptr ? 0 : amt->amount(), 0,
                                      root::owner_root(*owners), owners->locktime,
                                      owners->threshold, root::kUTXOOccupied});
    }
    check(hand.size() == 4, "all four UTXOs are enumerated");

    std::vector<root::TxLeaf> hand_txs;
    for (const auto& tx : blk_txs) hand_txs.push_back(root::TxLeaf{tx->id(), 0, 1, 0, {}});

    const auto want_utxo = root::utxo_root(hand);
    const auto want_asset = root::asset_root({});
    const auto want_tx = root::tx_root(hand_txs);
    const auto want = root::compose(parent, want_utxo, want_asset, want_tx, height);

    check_eq(hex_of(*got), hex_of(want),
             "the wired execution_root equals the fold over the hand-projected leaves");

    // The UTXO family is non-trivial (real leaves), not the empty root; the asset
    // family IS the empty root, because the executor state has no asset arena; and
    // the tx family is keccak-based, so it is never zero.
    check(want_utxo != root::utxo_root({}), "the UTXO root is not the empty root");
    check(want_asset == root::asset_root({}), "the asset root is the empty root");
    check(want_tx != root::Digest{}, "the tx root is never zero");
}

// TestBlockExecutionRootDeterministic — Go repeats the wired root 16 times over a
// 40-UTXO state of mixed thresholds, locktimes and owner-set sizes. The port's
// existing case (vm_test.cpp, root_is_recomputed) shows two VMs agree on a
// one-transaction block; this is the property over a state large enough for
// enumeration order to matter.
void block_root_is_deterministic() {
    std::printf("\n  -- the same state, sixteen more times --\n");

    MemoryState st;
    const Id tx_id = id(0x21);
    for (std::uint32_t i = 0; i < 40; ++i) {
        auto out = transfer_out(std::uint64_t(i + 1), 1, std::uint64_t(i),
                                short_ids(std::uint8_t(i % 7), 1 + int(i % 3)));
        st.add_utxo(txs::UTXO{txs::UTXOID{tx_id, i, false}, id(std::uint8_t(i)), out});
    }
    check(st.utxo_count() == 40, "forty UTXOs are occupied");

    std::vector<std::shared_ptr<txs::Tx>> blk_txs;
    for (int i = 0; i < 5; ++i) {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = 10;
        utx->base.blockchain_id = id(std::uint8_t(0xC0 + i));
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        (void)tx->initialize();
        blk_txs.push_back(tx);
    }

    const Id parent = id(0x31);
    auto first = root::block_execution_root(parent, blk_txs, st, 7);
    check(first.has_value(), first ? "the first computation succeeds" : "root: " + first.error());
    if (!first) return;

    bool all_same = true;
    for (int i = 0; i < 16; ++i) {
        auto again = root::block_execution_root(parent, blk_txs, st, 7);
        if (!again || *again != *first) all_same = false;
    }
    check(all_same, "the execution root is the same across sixteen more runs");

    // …and it is a function of the state, not of the object: a second state built
    // by writing the SAME UTXOs in reverse order must reach the same root, because
    // the fold walks ascending UTXOID order rather than insertion order.
    MemoryState other;
    for (int i = 39; i >= 0; --i) {
        auto u = std::uint32_t(i);
        auto out = transfer_out(std::uint64_t(u + 1), 1, std::uint64_t(u),
                                short_ids(std::uint8_t(u % 7), 1 + int(u % 3)));
        other.add_utxo(txs::UTXO{txs::UTXOID{tx_id, u, false}, id(std::uint8_t(u)), out});
    }
    auto mirrored = root::block_execution_root(parent, blk_txs, other, 7);
    check(mirrored && *mirrored == *first,
          "…and a state written in the opposite order reaches the same root");

    // Height and parent are bound in, so two blocks over one state differ.
    auto higher = root::block_execution_root(parent, blk_txs, st, 8);
    check(higher && *higher != *first, "the height is bound into the root");
    auto other_parent = root::block_execution_root(id(0x32), blk_txs, st, 7);
    check(other_parent && *other_parent != *first, "…and so is the parent's root");
}

// TestBlockExecutionRootEmptyState — an empty occupied set must compose the EMPTY
// UTXO root (keccak256("")), not a zero and not a wrong value. root_test.cpp pins
// the empty family root at the fold; this pins that block_execution_root wires it.
void block_root_over_an_empty_state() {
    std::printf("\n  -- an empty occupied set --\n");

    MemoryState st;
    check(st.utxo_count() == 0, "the state holds nothing");

    std::vector<std::shared_ptr<txs::Tx>> blk_txs;
    for (int i = 0; i < 2; ++i) {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = 10;
        utx->base.blockchain_id = id(std::uint8_t(0xD0 + i));
        auto tx = std::make_shared<txs::Tx>();
        tx->unsigned_tx = utx;
        (void)tx->initialize();
        blk_txs.push_back(tx);
    }

    const Id parent = id(0x41);
    const std::uint64_t height = 3;
    auto got = root::block_execution_root(parent, blk_txs, st, height);
    check(got.has_value(), got ? "an empty state still composes a root" : "root: " + got.error());
    if (!got) return;

    std::vector<root::TxLeaf> hand_txs;
    for (const auto& tx : blk_txs) hand_txs.push_back(root::TxLeaf{tx->id(), 0, 1, 0, {}});
    const auto want = root::compose(parent, root::utxo_root({}), root::asset_root({}),
                                    root::tx_root(hand_txs), height);
    check_eq(hex_of(*got), hex_of(want), "…over the EMPTY UTXO root, not over a zero");

    // And that empty root is keccak256(""), the same constant root_test.cpp pins.
    check_eq(hex_of(root::utxo_root({})),
             "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
             "the empty UTXO root is keccak256(\"\")");
    check(root::utxo_root({}) != root::Digest{}, "…and is not the zero digest");
}

// ================= the fold, against the independent oracle =================

// TestParallelFamilyRootsMatchSerial — Go's leaf counts, verbatim: 0, 1, and every
// size around the odd/even and sharding edges where lone-right promotion lives.
void family_roots_match_the_reference_fold() {
    std::printf("\n  -- every family fold, at every edge --\n");

    Rng rng(0xC0FFEEULL);
    const int sizes[] = {0,  1,   2,   3,   4,   5,    7,    8,    9,    15,  16,  17,  31,
                         33, 63,  64,  65,  127, 255,  256,  257,  511,  1000, 1023, 1024, 4097};

    bool utxo_ok = true, asset_ok = true, tx_ok = true;
    int worst = -1;
    for (int n : sizes) {
        auto us = rand_utxo_leaves(rng, n);
        if (root::utxo_root(us) != ref_utxo_root(us)) {
            utxo_ok = false;
            if (worst < 0) worst = n;
        }
        auto as = rand_asset_leaves(rng, n);
        if (root::asset_root(as) != ref_asset_root(as)) {
            asset_ok = false;
            if (worst < 0) worst = n;
        }
        auto ts = rand_tx_leaves(rng, n);
        if (root::tx_root(ts) != ref_tx_root(ts)) {
            tx_ok = false;
            if (worst < 0) worst = n;
        }
    }
    const std::string at = worst < 0 ? "" : " (first disagreement at n=" + std::to_string(worst) + ")";
    check(utxo_ok, "utxo_root agrees with RFC-6962's recursive fold at every size" + at);
    check(asset_ok, "asset_root agrees at every size" + at);
    check(tx_ok, "tx_root agrees at every size" + at);
}

// TestParallelExecutionRootMatchesSerial — the whole compose, over random state of
// independent, frequently-odd per-family sizes.
void execution_root_matches_the_reference_fold() {
    std::printf("\n  -- the whole compose, two hundred times --\n");

    Rng rng(0x5EEDULL);
    bool ok = true;
    int failed_iter = -1;
    for (int iter = 0; iter < 200; ++iter) {
        Id parent{};
        fill_id(rng, parent);
        const std::uint64_t height = rng();

        const int nu = int(rng() % 600), na = int(rng() % 600), nt = int(rng() % 600);
        auto us = rand_utxo_leaves(rng, nu);
        auto as = rand_asset_leaves(rng, na);
        auto ts = rand_tx_leaves(rng, nt);

        const auto want = root::compose(parent, ref_utxo_root(us), ref_asset_root(as),
                                        ref_tx_root(ts), height);
        const auto got = root::compose(parent, root::utxo_root(us), root::asset_root(as),
                                       root::tx_root(ts), height);
        if (want != got) {
            ok = false;
            if (failed_iter < 0) failed_iter = iter;
        }
    }
    check(ok, ok ? "the composed root agrees over two hundred random states"
                 : "the composed root disagrees at iteration " + std::to_string(failed_iter));
}

// TestParallelExecutionRootEmpty — the all-empty state reduces to the same
// composed root, with empty family roots that are keccak256("") rather than zero.
void execution_root_over_nothing_at_all() {
    std::printf("\n  -- nothing at all --\n");
    const Id parent{};
    const auto want = root::compose(parent, ref_utxo_root({}), ref_asset_root({}),
                                    ref_tx_root({}), 0);
    const auto got = root::compose(parent, root::utxo_root({}), root::asset_root({}),
                                   root::tx_root({}), 0);
    check_eq(hex_of(got), hex_of(want), "the all-empty compose agrees with the reference");
    check(got != root::Digest{}, "…and the composed root of nothing is still not zero");
}

// ================= a UTXO the projection cannot read =================

// extract_owners refuses an output with no owner model rather than answering zero,
// and block_execution_root carries that refusal out. A zero would silently make
// two different outputs commit to the same thing.
void an_output_with_no_owners_is_refused() {
    std::printf("\n  -- an output the root cannot commit to --\n");

    // A UTXO with no output at all: the projection must say so, not skip it.
    MemoryState st;
    st.add_utxo(txs::UTXO{txs::UTXOID{id(0x51), 0, false}, id(0x52), nullptr});
    auto r = root::block_execution_root(id(0x53), {}, st, 1);
    check(!r, "a UTXO with no output is refused, not folded as a zero");
    check(!r && r.error().find("utxo has no output") != std::string::npos,
          "…and says which half is missing");
}

}  // namespace

int main() {
    std::printf("xvm — the projection from chain state to the execution root\n");
    owner_root_matches_the_documented_preimage();
    owner_root_does_not_depend_on_listing_order();
    owner_root_binds_the_threshold();
    utxo_leaf_projection();
    block_root_matches_the_hand_projection();
    block_root_is_deterministic();
    block_root_over_an_empty_state();
    family_roots_match_the_reference_fold();
    execution_root_matches_the_reference_fold();
    execution_root_over_nothing_at_all();
    an_output_with_no_owners_is_refused();
    return report("projection");
}
