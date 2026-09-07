// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fixtures.hpp — the deterministic values the golden vectors were generated
// from. They are defined ONCE here and in test/golden_gen.go, and the two must
// agree; that is the point of making them trivially derivable from one byte.

#pragma once

#include "lux/xvm/fx.hpp"
#include "lux/xvm/state.hpp"
#include "lux/core/store.hpp"
#include "keys.hpp"

#include "lux/xvm/executor.hpp"
#include "lux/xvm/txs.hpp"

namespace lux::xvm::test {

// The harness — check, check_eq, hex_of, from_hex — is the node's, in core.
using namespace lux::core::test;


namespace detail {
// A base only so the store is constructed before the state that references it;
// members would be too late.
struct OwnStore {
    store::Memory store;
};
}  // namespace detail

// MemoryState is a State with its own store beside it — what a test uses when
// what it is testing is the state rather than where the state rests. Durability
// has its own suite (store_test), and the VM's restart has its own case.
struct MemoryState : private detail::OwnStore, public state::State {
    MemoryState() : detail::OwnStore(), state::State(store) {}
    store::Memory& backing() { return store; }
};

inline ShortId addr(std::uint8_t n) {
    ShortId a{};
    for (std::size_t i = 0; i < a.size(); ++i) a[i] = std::uint8_t(n + i);
    return a;
}

inline Id id(std::uint8_t n) {
    Id a{};
    for (std::size_t i = 0; i < a.size(); ++i) a[i] = std::uint8_t(n + i);
    return a;
}

inline fx::Signature sig(std::uint8_t n) {
    fx::Signature s{};
    for (std::size_t i = 0; i < s.size(); ++i) s[i] = std::uint8_t(n + i);
    return s;
}

inline fx::OutputOwners owners_one() { return fx::OutputOwners{12345, 1, {addr(1)}}; }
inline fx::OutputOwners owners_two() { return fx::OutputOwners{0, 2, {addr(1), addr(40)}}; }

inline std::shared_ptr<fx::secp256k1fx::TransferOutput> transfer_output() {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = 54321;
    o->out_owners = owners_one();
    return o;
}

inline std::shared_ptr<fx::secp256k1fx::TransferInput> transfer_input() {
    auto i = std::make_shared<fx::secp256k1fx::TransferInput>();
    i->amt = 54321;
    i->input.sig_indices = {0, 3};
    return i;
}

inline std::shared_ptr<fx::secp256k1fx::MintOutput> mint_output() {
    auto o = std::make_shared<fx::secp256k1fx::MintOutput>();
    o->out_owners = owners_one();
    return o;
}

inline std::shared_ptr<fx::secp256k1fx::MintOperation> mint_operation() {
    auto op = std::make_shared<fx::secp256k1fx::MintOperation>();
    op->mint_input.sig_indices = {0};
    op->mint_output = *mint_output();
    op->transfer_output = *transfer_output();
    return op;
}

inline std::shared_ptr<fx::secp256k1fx::Credential> credential() {
    auto c = std::make_shared<fx::secp256k1fx::Credential>();
    c->signatures = {sig(7), sig(9)};
    return c;
}

// The multi-asset spending envelope every golden transaction is built on.
inline txs::BaseTxFields base_fields() {
    txs::BaseTxFields base;
    base.network_id = 10;
    base.blockchain_id = id(200);
    base.outs.push_back(txs::TransferableOutput{id(50), transfer_output()});
    base.ins.push_back(
        txs::TransferableInput{txs::UTXOID{id(100), 3, false}, id(50), transfer_input()});
    const char* memo = "memo bytes";
    base.memo.assign(memo, memo + 10);
    return base;
}


// ── the chain a VM test boots
//
// The one genesis this suite starts from, and the values it is made of. They
// live here rather than in one test file because more than one test needs to
// boot the SAME chain — a durability test that booted a different one would be
// proving something about a chain nobody else runs.

constexpr std::uint32_t kNetworkID = 369;
constexpr std::uint64_t kGenesisTime = 1749000000;
constexpr std::uint64_t kStartingBalance = 1000000;

Id chain_id() { return id(0xC1); }

fx::OutputOwners owner(int key = 0) { return fx::OutputOwners{0, 1, {test_address(key)}}; }

std::shared_ptr<fx::secp256k1fx::TransferOutput> tout(std::uint64_t amt, int key = 0) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = owner(key);
    return o;
}

std::shared_ptr<fx::secp256k1fx::TransferInput> tin(std::uint64_t amt) {
    auto i = std::make_shared<fx::secp256k1fx::TransferInput>();
    i->amt = amt;
    i->input.sig_indices = {0};
    return i;
}

// genesis_asset is the one transaction the chain starts with: it defines the
// asset AND, through its initial state, the outputs that hold its whole supply.
std::shared_ptr<txs::Tx> genesis_asset(int num_outputs) {
    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->base.network_id = kNetworkID;
    utx->base.blockchain_id = chain_id();
    utx->name = "Lux";
    utx->symbol = "LUX";
    utx->denomination = 0;
    txs::InitialState s;
    s.fx_index = 0;
    for (int i = 0; i < num_outputs; ++i) s.outs.push_back(tout(kStartingBalance, 0));
    s.sort();
    utx->states = {s};

    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

std::vector<executor::ParsedFx> the_fxs() {
    return {executor::ParsedFx{id(1), std::make_shared<fx::Secp256k1Fx>()},
            executor::ParsedFx{id(2), std::make_shared<fx::NFTFx>()},
            executor::ParsedFx{id(3), std::make_shared<fx::PropertyFx>()}};
}

}  // namespace lux::xvm::test
