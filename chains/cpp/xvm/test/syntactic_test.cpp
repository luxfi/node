// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// syntactic_test.cpp — every syntactic rejection, ported case for case from
// txs/executor/syntactic_verifier_test.go.
//
// Syntactic verification is the pass that reads NO state, so each case here is a
// statement about a transaction on its own: its shape, its fee arithmetic, its
// ordering, its credential count. The fixture is the Go one — the same fee
// config (2 / 3), the same amounts (12345 out, 54321 in), the same test key —
// so a case that passes here and fails there is a real divergence, not a
// different setup.

#include "check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/executor.hpp"
#include "lux/xvm/txs.hpp"

#include <limits>

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

constexpr std::uint32_t kUnitTestNetworkID = 369;
constexpr std::uint64_t kTxFee = 2;
constexpr std::uint64_t kCreateAssetTxFee = 3;
constexpr std::uint64_t kOutAmount = 12345;
constexpr std::uint64_t kInAmount = 54321;

Id chain_id() { return id(0xC1); }
Id fee_asset_id() { return id(0xFE); }
Id input_tx_id() { return id(0x11); }

fx::OutputOwners owners() { return fx::OutputOwners{0, 1, {test_address(0)}}; }

std::shared_ptr<fx::secp256k1fx::TransferOutput> out_amt(std::uint64_t amt) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = owners();
    return o;
}

std::shared_ptr<fx::secp256k1fx::TransferInput> in_amt(std::uint64_t amt) {
    auto i = std::make_shared<fx::secp256k1fx::TransferInput>();
    i->amt = amt;
    i->input.sig_indices = {2};
    return i;
}

txs::TransferableOutput output() { return txs::TransferableOutput{fee_asset_id(), out_amt(kOutAmount)}; }

txs::TransferableInput input(std::uint32_t index = 0) {
    return txs::TransferableInput{txs::UTXOID{input_tx_id(), index, false}, fee_asset_id(),
                                  in_amt(kInAmount)};
}

txs::BaseTxFields base() {
    txs::BaseTxFields b;
    b.network_id = kUnitTestNetworkID;
    b.blockchain_id = chain_id();
    b.outs.push_back(output());
    b.ins.push_back(input());
    return b;
}

// The three flow-arithmetic fixtures Go repeats verbatim in every section,
// stated once here: an out-of-order output list, a pair of inputs whose sum
// passes 2^64, and a pair of outputs that does the same.
txs::BaseTxFields base_unsorted_outs() {
    auto b = base();
    b.outs = {txs::TransferableOutput{fee_asset_id(), out_amt(1)},
              txs::TransferableOutput{fee_asset_id(), out_amt(2)}};
    txs::sort_transferable_outputs(b.outs);
    std::swap(b.outs[0], b.outs[1]);
    return b;
}

txs::BaseTxFields base_input_overflow() {
    auto b = base();
    auto i0 = input(0);
    i0.in = in_amt(1);
    auto i1 = input(1);
    i1.in = in_amt(std::numeric_limits<std::uint64_t>::max());
    b.ins = {i0, i1};
    return b;
}

txs::BaseTxFields base_output_overflow() {
    auto b = base();
    b.outs = {txs::TransferableOutput{fee_asset_id(), out_amt(1)},
              txs::TransferableOutput{fee_asset_id(),
                                      out_amt(std::numeric_limits<std::uint64_t>::max())}};
    txs::sort_transferable_outputs(b.outs);
    return b;
}

std::shared_ptr<fx::secp256k1fx::Credential> empty_cred() {
    return std::make_shared<fx::secp256k1fx::Credential>();
}

executor::Backend make_backend() {
    executor::Backend b;
    b.config.tx_fee = kTxFee;
    b.config.create_asset_tx_fee = kCreateAssetTxFee;
    b.network_id = kUnitTestNetworkID;
    b.chain_id = chain_id();
    b.fee_asset_id = fee_asset_id();
    b.fxs.push_back(executor::ParsedFx{id(1), std::make_shared<fx::Secp256k1Fx>()});
    b.fx_index.set(wire::TypeKind::Secp256k1, 0);
    return b;
}

// run verifies a tx and reports the error it produced (empty on success).
std::string run(const executor::Backend& backend, txs::Tx& tx) {
    executor::SyntacticVerifier v(backend, tx);
    auto r = tx.unsigned_tx->visit(v);
    return r ? std::string{} : r.error();
}

// expect names the case and the error it must produce. An empty `want` means the
// tx must be accepted; otherwise the produced error must CONTAIN want, which is
// how a Go sentinel wrapped in context is matched.
void expect(const executor::Backend& backend, const std::string& name, txs::Tx& tx,
            const std::string& want) {
    std::string got = run(backend, tx);
    bool ok = want.empty() ? got.empty() : got.find(want) != std::string::npos;
    check(ok, name);
    if (!ok) std::printf("        got  \"%s\"\n        want \"%s\"\n", got.c_str(), want.c_str());
}

txs::Tx tx_of(std::shared_ptr<txs::UnsignedTx> u,
              std::vector<std::shared_ptr<fx::FxCredential>> creds) {
    txs::Tx t;
    t.unsigned_tx = std::move(u);
    t.creds = std::move(creds);
    return t;
}

std::shared_ptr<txs::BaseTx> base_tx(txs::BaseTxFields b) {
    auto t = std::make_shared<txs::BaseTx>();
    t->base = std::move(b);
    return t;
}

// ================= BaseTx =================

void base_tx_cases(const executor::Backend& backend) {
    std::printf("\n  -- BaseTx --\n");
    auto creds = std::vector<std::shared_ptr<fx::FxCredential>>{empty_cred()};

    {
        auto tx = tx_of(base_tx(base()), creds);
        expect(backend, "valid", tx, "");
    }
    {
        auto b = base();
        b.network_id++;
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "wrong networkID", tx, txs::kErrWrongNetworkID);
    }
    {
        auto b = base();
        b.blockchain_id = id(0xAB);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "wrong chainID", tx, txs::kErrWrongChainID);
    }
    {
        auto b = base();
        b.memo.assign(txs::kMaxMemoSize + 1, 0);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "memo too large", tx, txs::kErrMemoTooLarge);
    }
    {
        auto b = base();
        b.outs[0].out = out_amt(0);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "invalid output", tx, fx::kErrNoValueOutput);
    }
    {
        auto b = base();
        b.outs = {txs::TransferableOutput{fee_asset_id(), out_amt(1)},
                  txs::TransferableOutput{fee_asset_id(), out_amt(2)}};
        txs::sort_transferable_outputs(b.outs);
        std::swap(b.outs[0], b.outs[1]);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "unsorted outputs", tx, txs::kErrOutputsNotSorted);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(0);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "invalid input", tx, fx::kErrNoValueInput);
    }
    {
        auto b = base();
        b.ins = {input(), input()};
        auto tx = tx_of(base_tx(b), {empty_cred(), empty_cred()});
        expect(backend, "duplicate inputs", tx, txs::kErrInputsNotSortedUnique);
    }
    {
        // Two inputs whose amounts sum past 2^64 — the flow checker must refuse
        // rather than wrap, which would make an unfunded tx look funded.
        auto b = base();
        auto i0 = input(0);
        i0.in = in_amt(1);
        auto i1 = input(1);
        i1.in = in_amt(std::numeric_limits<std::uint64_t>::max());
        b.ins = {i0, i1};
        auto tx = tx_of(base_tx(b), {empty_cred(), empty_cred()});
        expect(backend, "input overflow", tx, "overflow");
    }
    {
        auto b = base();
        b.outs = {txs::TransferableOutput{fee_asset_id(), out_amt(1)},
                  txs::TransferableOutput{fee_asset_id(),
                                          out_amt(std::numeric_limits<std::uint64_t>::max())}};
        txs::sort_transferable_outputs(b.outs);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "output overflow", tx, "overflow");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(1);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    {
        auto tx = tx_of(base_tx(base()), {nullptr});
        expect(backend, "invalid credential", tx, fx::kErrNilCredential);
    }
    {
        auto tx = tx_of(base_tx(base()), {});
        expect(backend, "wrong number of credentials", tx,
               executor::kErrWrongNumberOfCredentials);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(kOutAmount + kTxFee);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "barely sufficient funds", tx, "");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(kOutAmount + kTxFee - 1);
        auto tx = tx_of(base_tx(b), creds);
        expect(backend, "barely insufficient funds", tx, txs::kErrInsufficientFunds);
    }
}

// ================= CreateAssetTx =================

txs::InitialState initial_state() {
    txs::InitialState s;
    s.fx_index = 0;
    s.outs = {out_amt(kOutAmount)};
    return s;
}

std::shared_ptr<txs::CreateAssetTx> create_asset_tx(txs::BaseTxFields b) {
    auto t = std::make_shared<txs::CreateAssetTx>();
    t->base = std::move(b);
    t->name = "NormalName";
    t->symbol = "TICK";
    t->denomination = 2;
    t->states = {initial_state()};
    return t;
}

void create_asset_tx_cases(const executor::Backend& backend) {
    std::printf("\n  -- CreateAssetTx --\n");
    auto creds = std::vector<std::shared_ptr<fx::FxCredential>>{empty_cred()};

    {
        auto tx = tx_of(create_asset_tx(base()), creds);
        expect(backend, "valid", tx, "");
    }
    {
        auto t = create_asset_tx(base());
        t->name = "";
        auto tx = tx_of(t, creds);
        expect(backend, "name too short", tx, executor::kErrNameTooShort);
    }
    {
        auto t = create_asset_tx(base());
        t->name = std::string(executor::kMaxNameLen + 1, 'a');
        auto tx = tx_of(t, creds);
        expect(backend, "name too long", tx, executor::kErrNameTooLong);
    }
    {
        auto t = create_asset_tx(base());
        t->symbol = "";
        auto tx = tx_of(t, creds);
        expect(backend, "symbol too short", tx, executor::kErrSymbolTooShort);
    }
    {
        auto t = create_asset_tx(base());
        t->symbol = "TICKER";
        auto tx = tx_of(t, creds);
        expect(backend, "symbol too long", tx, executor::kErrSymbolTooLong);
    }
    {
        auto t = create_asset_tx(base());
        t->states.clear();
        auto tx = tx_of(t, creds);
        expect(backend, "no feature extensions", tx, executor::kErrNoFxs);
    }
    {
        auto t = create_asset_tx(base());
        t->denomination = executor::kMaxDenomination + 1;
        auto tx = tx_of(t, creds);
        expect(backend, "denomination too large", tx, executor::kErrDenominationTooLarge);
    }
    {
        auto t = create_asset_tx(base());
        t->name = " NormalName";
        auto tx = tx_of(t, creds);
        expect(backend, "bounding whitespace in name", tx, executor::kErrUnexpectedWhitespace);
    }
    {
        auto t = create_asset_tx(base());
        t->name = "Bad!Name";
        auto tx = tx_of(t, creds);
        expect(backend, "illegal character in name", tx, executor::kErrIllegalNameCharacter);
    }
    {
        auto t = create_asset_tx(base());
        t->symbol = "bad";
        auto tx = tx_of(t, creds);
        expect(backend, "illegal character in ticker", tx, executor::kErrIllegalSymbolCharacter);
    }
    {
        // Non-ASCII decodes to a rune above U+007F, which the name rule refuses
        // — the reason the check is on runes and not on bytes.
        auto t = create_asset_tx(base());
        t->name = "Náme";
        auto tx = tx_of(t, creds);
        expect(backend, "non-ASCII name rune", tx, executor::kErrIllegalNameCharacter);
    }
    {
        auto b = base();
        b.network_id++;
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "wrong networkID", tx, txs::kErrWrongNetworkID);
    }
    {
        auto b = base();
        b.blockchain_id = id(0xAB);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "wrong chainID", tx, txs::kErrWrongChainID);
    }
    {
        auto b = base();
        b.memo.assign(txs::kMaxMemoSize + 1, 0);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "memo too large", tx, txs::kErrMemoTooLarge);
    }
    {
        auto b = base();
        b.outs[0].out = out_amt(0);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "invalid output", tx, fx::kErrNoValueOutput);
    }
    {
        auto b = base();
        b.outs = {txs::TransferableOutput{fee_asset_id(), out_amt(1)},
                  txs::TransferableOutput{fee_asset_id(), out_amt(2)}};
        txs::sort_transferable_outputs(b.outs);
        std::swap(b.outs[0], b.outs[1]);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "unsorted outputs", tx, txs::kErrOutputsNotSorted);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(0);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "invalid input", tx, fx::kErrNoValueInput);
    }
    {
        auto b = base();
        b.ins = {input(), input()};
        auto tx = tx_of(create_asset_tx(b), {empty_cred(), empty_cred()});
        expect(backend, "duplicate inputs", tx, txs::kErrInputsNotSortedUnique);
    }
    {
        auto tx = tx_of(create_asset_tx(base_input_overflow()), {empty_cred(), empty_cred()});
        expect(backend, "input overflow", tx, "overflow");
    }
    {
        auto tx = tx_of(create_asset_tx(base_output_overflow()), creds);
        expect(backend, "output overflow", tx, "overflow");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(1);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    {
        auto t = create_asset_tx(base());
        auto s = initial_state();
        s.fx_index = 1;  // there is only one fx registered
        t->states = {s};
        auto tx = tx_of(t, creds);
        expect(backend, "invalid fx", tx, txs::kErrUnknownFx);
    }
    {
        auto t = create_asset_tx(base());
        auto s = initial_state();
        s.outs = {nullptr};
        t->states = {s};
        auto tx = tx_of(t, creds);
        expect(backend, "invalid nil state output", tx, txs::kErrNilFxOutput);
    }
    {
        auto t = create_asset_tx(base());
        auto s = initial_state();
        s.outs = {out_amt(0)};
        t->states = {s};
        auto tx = tx_of(t, creds);
        expect(backend, "invalid state output", tx, fx::kErrNoValueOutput);
    }
    {
        auto t = create_asset_tx(base());
        auto s = initial_state();
        s.outs = {out_amt(kOutAmount), out_amt(kOutAmount + 1)};
        s.sort();
        std::swap(s.outs[0], s.outs[1]);
        t->states = {s};
        auto tx = tx_of(t, creds);
        expect(backend, "unsorted initial state", tx, txs::kErrInitialOutputsNotSorted);
    }
    {
        auto t = create_asset_tx(base());
        t->states = {initial_state(), initial_state()};
        auto tx = tx_of(t, creds);
        expect(backend, "non-unique initial states", tx,
               executor::kErrInitialStatesNotSortedUnique);
    }
    {
        auto tx = tx_of(create_asset_tx(base()), {nullptr});
        expect(backend, "invalid credential", tx, fx::kErrNilCredential);
    }
    {
        auto tx = tx_of(create_asset_tx(base()), {});
        expect(backend, "wrong number of credentials", tx,
               executor::kErrWrongNumberOfCredentials);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(kOutAmount + kCreateAssetTxFee);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "barely sufficient funds", tx, "");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(kOutAmount + kCreateAssetTxFee - 1);
        auto tx = tx_of(create_asset_tx(b), creds);
        expect(backend, "barely insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    // The Go table also has an "invalid nil state" case, where a *InitialState
    // in the slice is nil. That is UNREPRESENTABLE here — states is a vector of
    // values, not of pointers — so the rule holds by construction rather than by
    // a check, and there is nothing to assert.
    check(true, "a nil InitialState is unrepresentable (states are values)");
}

// ================= OperationTx =================

std::shared_ptr<fx::secp256k1fx::MintOperation> mint_op() {
    auto op = std::make_shared<fx::secp256k1fx::MintOperation>();
    op->mint_input.sig_indices = {2};
    op->mint_output.out_owners = owners();
    op->transfer_output = *out_amt(kOutAmount);
    return op;
}

txs::Operation operation() {
    txs::Operation op;
    op.asset_id = fee_asset_id();
    op.utxo_ids = {txs::UTXOID{input_tx_id(), 1, false}};
    op.op = mint_op();
    return op;
}

std::shared_ptr<txs::OperationTx> operation_tx(txs::BaseTxFields b) {
    auto t = std::make_shared<txs::OperationTx>();
    t->base = std::move(b);
    t->ops = {operation()};
    return t;
}

void operation_tx_cases(const executor::Backend& backend) {
    std::printf("\n  -- OperationTx --\n");
    auto creds = std::vector<std::shared_ptr<fx::FxCredential>>{empty_cred(), empty_cred()};

    {
        auto tx = tx_of(operation_tx(base()), creds);
        expect(backend, "valid", tx, "");
    }
    {
        auto t = operation_tx(base());
        t->ops.clear();
        auto tx = tx_of(t, creds);
        expect(backend, "no operation", tx, executor::kErrNoOperations);
    }
    {
        auto b = base();
        b.network_id++;
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "wrong networkID", tx, txs::kErrWrongNetworkID);
    }
    {
        auto b = base();
        b.blockchain_id = id(0xAB);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "wrong chainID", tx, txs::kErrWrongChainID);
    }
    {
        auto b = base();
        b.memo.assign(txs::kMaxMemoSize + 1, 0);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "memo too large", tx, txs::kErrMemoTooLarge);
    }
    {
        auto b = base();
        b.outs[0].out = out_amt(0);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "invalid output", tx, fx::kErrNoValueOutput);
    }
    {
        auto tx = tx_of(operation_tx(base_unsorted_outs()), creds);
        expect(backend, "unsorted outputs", tx, txs::kErrOutputsNotSorted);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(0);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "invalid input", tx, fx::kErrNoValueInput);
    }
    {
        auto b = base();
        b.ins = {input(), input()};
        auto tx = tx_of(operation_tx(b), {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "duplicate inputs", tx, txs::kErrInputsNotSortedUnique);
    }
    {
        auto tx = tx_of(operation_tx(base_input_overflow()),
                        {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "input overflow", tx, "overflow");
    }
    {
        auto tx = tx_of(operation_tx(base_output_overflow()), creds);
        expect(backend, "output overflow", tx, "overflow");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(1);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    {
        auto t = operation_tx(base());
        auto op = operation();
        op.op = nullptr;
        t->ops = {op};
        auto tx = tx_of(t, creds);
        expect(backend, "invalid nil fx op", tx, txs::kErrNilFxOperation);
    }
    {
        auto t = operation_tx(base());
        auto op = operation();
        op.utxo_ids = {txs::UTXOID{input_tx_id(), 1, false}, txs::UTXOID{input_tx_id(), 1, false}};
        t->ops = {op};
        auto tx = tx_of(t, creds);
        expect(backend, "invalid duplicated op UTXOs", tx, txs::kErrNotSortedAndUniqueUTXOIDs);
    }
    {
        // Two operations naming the SAME UTXO: legal on its own, a double spend
        // together — which is why the double-spend set spans the whole tx.
        auto t = operation_tx(base());
        auto op0 = operation();
        auto op1 = operation();
        op1.asset_id = id(0x99);
        t->ops = {op0, op1};
        txs::sort_operations(t->ops);
        auto tx = tx_of(t, {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "invalid duplicated UTXOs across ops", tx, executor::kErrDoubleSpend);
    }
    {
        auto t = operation_tx(base());
        auto op = operation();
        op.utxo_ids.clear();
        t->ops = {op, op};
        txs::sort_operations(t->ops);
        auto tx = tx_of(t, {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "invalid duplicated op", tx, executor::kErrOperationsNotSortedUnique);
    }
    {
        auto tx = tx_of(operation_tx(base()), {nullptr, nullptr});
        expect(backend, "invalid credential", tx, fx::kErrNilCredential);
    }
    {
        auto tx = tx_of(operation_tx(base()), {});
        expect(backend, "wrong number of credentials", tx,
               executor::kErrWrongNumberOfCredentials);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(kOutAmount + kTxFee);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "barely sufficient funds", tx, "");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(kOutAmount + kTxFee - 1);
        auto tx = tx_of(operation_tx(b), creds);
        expect(backend, "barely insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    check(true, "a nil Operation is unrepresentable (ops are values)");
}

// ================= ImportTx =================

std::shared_ptr<txs::ImportTx> import_tx(txs::BaseTxFields b) {
    auto t = std::make_shared<txs::ImportTx>();
    t->base = std::move(b);
    t->source_chain = id(0x77);
    t->imported_ins = {txs::TransferableInput{txs::UTXOID{input_tx_id(), 1, false},
                                              fee_asset_id(), in_amt(kInAmount)}};
    return t;
}

void import_tx_cases(const executor::Backend& backend) {
    std::printf("\n  -- ImportTx --\n");
    auto creds = std::vector<std::shared_ptr<fx::FxCredential>>{empty_cred(), empty_cred()};

    {
        auto tx = tx_of(import_tx(base()), creds);
        expect(backend, "valid", tx, "");
    }
    {
        auto t = import_tx(base());
        t->imported_ins.clear();
        auto tx = tx_of(t, creds);
        expect(backend, "no imported inputs", tx, executor::kErrNoImportInputs);
    }
    {
        auto b = base();
        b.network_id++;
        auto tx = tx_of(import_tx(b), creds);
        expect(backend, "wrong networkID", tx, txs::kErrWrongNetworkID);
    }
    {
        auto b = base();
        b.blockchain_id = id(0xAB);
        auto tx = tx_of(import_tx(b), creds);
        expect(backend, "wrong chainID", tx, txs::kErrWrongChainID);
    }
    {
        auto b = base();
        b.memo.assign(txs::kMaxMemoSize + 1, 0);
        auto tx = tx_of(import_tx(b), creds);
        expect(backend, "memo too large", tx, txs::kErrMemoTooLarge);
    }
    {
        auto b = base();
        b.outs[0].out = out_amt(0);
        auto tx = tx_of(import_tx(b), creds);
        expect(backend, "invalid output", tx, fx::kErrNoValueOutput);
    }
    {
        auto tx = tx_of(import_tx(base_unsorted_outs()), creds);
        expect(backend, "unsorted outputs", tx, txs::kErrOutputsNotSorted);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(0);
        auto tx = tx_of(import_tx(b), creds);
        expect(backend, "invalid input", tx, fx::kErrNoValueInput);
    }
    {
        auto b = base();
        b.ins = {input(), input()};
        auto tx = tx_of(import_tx(b), {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "duplicate inputs", tx, txs::kErrInputsNotSortedUnique);
    }
    {
        auto t = import_tx(base());
        t->imported_ins = {t->imported_ins[0], t->imported_ins[0]};
        auto tx = tx_of(t, {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "duplicate imported inputs", tx, txs::kErrInputsNotSortedUnique);
    }
    {
        auto tx = tx_of(import_tx(base_input_overflow()),
                        {empty_cred(), empty_cred(), empty_cred()});
        expect(backend, "input overflow", tx, "overflow");
    }
    {
        auto tx = tx_of(import_tx(base_output_overflow()), creds);
        expect(backend, "output overflow", tx, "overflow");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(1);
        auto t = import_tx(b);
        t->imported_ins[0].in = in_amt(1);
        auto tx = tx_of(t, creds);
        expect(backend, "insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    {
        auto tx = tx_of(import_tx(base()), {nullptr, nullptr});
        expect(backend, "invalid credential", tx, fx::kErrNilCredential);
    }
    {
        auto tx = tx_of(import_tx(base()), {});
        expect(backend, "wrong number of credentials", tx,
               executor::kErrWrongNumberOfCredentials);
    }
    {
        // The imported input carries the whole cost, so the local input can be
        // exactly nothing but the fee.
        auto b = base();
        b.ins.clear();
        auto t = import_tx(b);
        t->imported_ins[0].in = in_amt(kOutAmount + kTxFee);
        auto tx = tx_of(t, {empty_cred()});
        expect(backend, "barely sufficient funds", tx, "");
    }
    {
        auto b = base();
        b.ins.clear();
        auto t = import_tx(b);
        t->imported_ins[0].in = in_amt(kOutAmount + kTxFee - 1);
        auto tx = tx_of(t, {empty_cred()});
        expect(backend, "barely insufficient funds", tx, txs::kErrInsufficientFunds);
    }
}

// ================= ExportTx =================

std::shared_ptr<txs::ExportTx> export_tx(txs::BaseTxFields b) {
    auto t = std::make_shared<txs::ExportTx>();
    t->base = std::move(b);
    t->destination_chain = id(0x88);
    t->exported_outs = {txs::TransferableOutput{fee_asset_id(), out_amt(kOutAmount)}};
    return t;
}

void export_tx_cases(const executor::Backend& backend) {
    std::printf("\n  -- ExportTx --\n");
    auto creds = std::vector<std::shared_ptr<fx::FxCredential>>{empty_cred()};

    {
        // Two outputs of 12345 plus a fee of 2 needs an input of 24692.
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "valid", tx, "");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee);
        auto t = export_tx(b);
        t->exported_outs.clear();
        auto tx = tx_of(t, creds);
        expect(backend, "no exported outputs", tx, executor::kErrNoExportOutputs);
    }
    {
        auto b = base();
        b.network_id++;
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "wrong networkID", tx, txs::kErrWrongNetworkID);
    }
    {
        auto b = base();
        b.blockchain_id = id(0xAB);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "wrong chainID", tx, txs::kErrWrongChainID);
    }
    {
        auto b = base();
        b.memo.assign(txs::kMaxMemoSize + 1, 0);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "memo too large", tx, txs::kErrMemoTooLarge);
    }
    {
        auto b = base();
        b.outs[0].out = out_amt(0);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "invalid output", tx, fx::kErrNoValueOutput);
    }
    {
        auto tx = tx_of(export_tx(base_unsorted_outs()), creds);
        expect(backend, "unsorted outputs", tx, txs::kErrOutputsNotSorted);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee);
        auto t = export_tx(b);
        // The EXPORTED outputs are their own sorted list, checked separately.
        t->exported_outs = {txs::TransferableOutput{fee_asset_id(), out_amt(1)},
                            txs::TransferableOutput{fee_asset_id(), out_amt(2)}};
        txs::sort_transferable_outputs(t->exported_outs);
        std::swap(t->exported_outs[0], t->exported_outs[1]);
        auto tx = tx_of(t, creds);
        expect(backend, "unsorted exported outputs", tx, txs::kErrOutputsNotSorted);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(0);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "invalid input", tx, fx::kErrNoValueInput);
    }
    {
        auto b = base();
        b.ins = {input(), input()};
        auto tx = tx_of(export_tx(b), {empty_cred(), empty_cred()});
        expect(backend, "duplicate inputs", tx, txs::kErrInputsNotSortedUnique);
    }
    {
        auto tx = tx_of(export_tx(base_input_overflow()), {empty_cred(), empty_cred()});
        expect(backend, "input overflow", tx, "overflow");
    }
    {
        auto tx = tx_of(export_tx(base_output_overflow()), creds);
        expect(backend, "output overflow", tx, "overflow");
    }
    {
        // Go sets the single input to 1 here; the default input funds the tx.
        auto b = base();
        b.ins[0].in = in_amt(1);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "insufficient funds", tx, txs::kErrInsufficientFunds);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee);
        auto tx = tx_of(export_tx(b), {nullptr});
        expect(backend, "invalid credential", tx, fx::kErrNilCredential);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee);
        auto tx = tx_of(export_tx(b), {});
        expect(backend, "wrong number of credentials", tx,
               executor::kErrWrongNumberOfCredentials);
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "barely sufficient funds", tx, "");
    }
    {
        auto b = base();
        b.ins[0].in = in_amt(2 * kOutAmount + kTxFee - 1);
        auto tx = tx_of(export_tx(b), creds);
        expect(backend, "barely insufficient funds", tx, txs::kErrInsufficientFunds);
    }
}

}  // namespace

int main() {
    std::printf("xvm — syntactic verification, ported from the Go case table\n");
    const auto backend = make_backend();
    base_tx_cases(backend);
    create_asset_tx_cases(backend);
    operation_tx_cases(backend);
    import_tx_cases(backend);
    export_tx_cases(backend);
    return report("syntactic");
}
