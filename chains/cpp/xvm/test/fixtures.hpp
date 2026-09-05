// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fixtures.hpp — the deterministic values the golden vectors were generated
// from. They are defined ONCE here and in test/golden_gen.go, and the two must
// agree; that is the point of making them trivially derivable from one byte.

#pragma once

#include "lux/xvm/fx.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/store.hpp"
#include "lux/xvm/txs.hpp"

namespace lux::xvm::test {

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

}  // namespace lux::xvm::test
