// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_test.cpp — the transaction vocabulary, ported from txs/base_tx_test.go,
// create_asset_tx_test.go, import_tx_test.go, initial_state_test.go and
// operation_test.go.
//
// The shape of every case is the Go one: build a transaction from the SAME
// fixture values, serialize it, parse the bytes back, and require that what
// comes out is what went in — field by field, and byte for byte where the Go
// test compares wire bytes. A round trip that agrees on every field but not on
// the bytes is not a round trip: the bytes ARE the transaction, since the id is
// their hash.

#include "lux/core/check.hpp"
#include "keys.hpp"

#include "lux/xvm/txs.hpp"

using namespace lux::xvm;
using namespace lux::core::test;
using namespace lux::xvm::test;

namespace {

// The Go fixture's two constants, verbatim.
constexpr std::uint32_t kUnitTestID = 369;

Id chain_id() { return Id{5, 4, 3, 2, 1}; }
Id asset_id() { return Id{1, 2, 3}; }

Id input_tx_id() {
    return Id{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8, 0xf7, 0xf6, 0xf5,
              0xf4, 0xf3, 0xf2, 0xf1, 0xf0, 0xef, 0xee, 0xed, 0xec, 0xeb, 0xea,
              0xe9, 0xe8, 0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0};
}

fx::OutputOwners owner_of(int key, std::uint64_t locktime = 0) {
    return fx::OutputOwners{locktime, 1, {test_address(key)}};
}

std::shared_ptr<fx::secp256k1fx::TransferOutput> transfer_out(std::uint64_t amt, int key,
                                                              std::uint64_t locktime = 0) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = owner_of(key, locktime);
    return o;
}

std::shared_ptr<fx::secp256k1fx::MintOutput> mint_out(int key) {
    auto o = std::make_shared<fx::secp256k1fx::MintOutput>();
    o->out_owners = owner_of(key);
    return o;
}

// sample_base_tx is Go's sampleBaseTx(), value for value.
txs::BaseTxFields sample_base() {
    txs::BaseTxFields b;
    b.network_id = kUnitTestID;
    b.blockchain_id = chain_id();
    b.outs.push_back(txs::TransferableOutput{asset_id(), transfer_out(12345, 0)});

    auto in = std::make_shared<fx::secp256k1fx::TransferInput>();
    in->amt = 54321;
    in->input.sig_indices = {2};
    b.ins.push_back(txs::TransferableInput{txs::UTXOID{input_tx_id(), 1, false}, asset_id(), in});
    b.memo = Bytes{0x00, 0x01, 0x02, 0x03};
    return b;
}

// sign_with attaches ONE credential carrying a signature per key, exactly as
// Go's Tx.SignSECP256K1Fx does for a single signer set.
void sign_with(txs::Tx& tx, const std::vector<int>& keys) {
    const Bytes& unsigned_bytes = tx.unsigned_tx->bytes();
    auto cred = std::make_shared<fx::secp256k1fx::Credential>();
    for (int k : keys) cred->signatures.push_back(sign_unsigned_tx(test_key(k), view(unsigned_bytes)));
    tx.creds.push_back(cred);
    (void)tx.initialize();
}

// ================= BaseTx =================

void base_tx_round_trip() {
    std::printf("\n  -- BaseTx round trip --\n");

    auto utx = std::make_shared<txs::BaseTx>();
    utx->base = sample_base();
    txs::Tx tx;
    tx.unsigned_tx = utx;
    auto init = tx.initialize();
    check(init.has_value(), "initialize");
    check(tx.id() != kEmptyId, "TxID is not empty");

    auto parsed = txs::parse(view(tx.bytes()));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check_eq(hex_of((*parsed)->bytes()), hex_of(tx.bytes()), "parsed bytes are the same bytes");
    check_eq(hex_of((*parsed)->id()), hex_of(tx.id()), "parsed TxID matches");

    auto* got = dynamic_cast<txs::BaseTx*>((*parsed)->unsigned_tx.get());
    if (got == nullptr) {
        check(false, "parsed as BaseTx");
        return;
    }
    check(got->base.network_id == utx->base.network_id, "networkID round-trips");
    check(got->base.blockchain_id == utx->base.blockchain_id, "blockchainID round-trips");
    check_eq(hex_of(got->base.memo), hex_of(utx->base.memo), "memo round-trips");
    check(got->base.outs.size() == 1, "one output");
    check(got->base.ins.size() == 1, "one input");
    check(got->base.outs[0].asset_id == utx->base.outs[0].asset_id, "output asset round-trips");
    check_eq(hex_of(got->base.outs[0].out->bytes()), hex_of(utx->base.outs[0].out->bytes()),
             "output fx bytes round-trip");
    check_eq(hex_of(got->base.ins[0].in->bytes()), hex_of(utx->base.ins[0].in->bytes()),
             "input fx bytes round-trip");
    check(got->base.ins[0].utxo_id == utx->base.ins[0].utxo_id, "UTXOID round-trips");

    // Signing with two keys under ONE signer set is one credential of two sigs.
    sign_with(tx, {0, 0});
    check(tx.creds.size() == 1, "one credential after signing");
    auto signed_tx = txs::parse(view(tx.bytes()));
    if (!signed_tx) {
        check(false, "parse signed: " + signed_tx.error());
        return;
    }
    check_eq(hex_of((*signed_tx)->id()), hex_of(tx.id()), "signed TxID matches");
    check((*signed_tx)->creds.size() == 1, "one credential parses back");
    check((*signed_tx)->creds[0]->sigs().size() == 2, "two signatures in that credential");
    check((*signed_tx)->creds[0]->family() == wire::TypeKind::Secp256k1,
          "credential is a secp256k1fx one");
}

// ================= CreateAssetTx =================

void create_asset_round_trip() {
    std::printf("\n  -- CreateAssetTx round trip --\n");

    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->base = sample_base();
    utx->name = "Volatility Index";
    utx->symbol = "VIX";
    utx->denomination = 2;
    txs::InitialState s;
    s.fx_index = 0;
    s.outs = {transfer_out(12345, 0, 54321), mint_out(1)};
    s.sort();
    utx->states = {s};

    txs::Tx tx;
    tx.unsigned_tx = utx;
    check(tx.initialize().has_value(), "initialize");

    auto parsed = txs::parse(view(tx.bytes()));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check_eq(hex_of((*parsed)->bytes()), hex_of(tx.bytes()), "parsed bytes are the same bytes");
    check_eq(hex_of((*parsed)->id()), hex_of(tx.id()), "parsed TxID matches");

    auto* got = dynamic_cast<txs::CreateAssetTx*>((*parsed)->unsigned_tx.get());
    if (got == nullptr) {
        check(false, "parsed as CreateAssetTx");
        return;
    }
    check_eq(got->name, "Volatility Index", "name round-trips");
    check_eq(got->symbol, "VIX", "symbol round-trips");
    check(got->denomination == 2, "denomination round-trips");
    check(got->base.network_id == kUnitTestID, "networkID round-trips");
    check(got->states.size() == 1, "one initial state");
    check(got->states[0].fx_index == 0, "fx index round-trips");
    check(got->states[0].outs.size() == 2, "two state outputs");
    check_eq(hex_of(got->states[0].outs[0]->bytes()), hex_of(utx->states[0].outs[0]->bytes()),
             "state output 0 bytes round-trip");
    check_eq(hex_of(got->states[0].outs[1]->bytes()), hex_of(utx->states[0].outs[1]->bytes()),
             "state output 1 bytes round-trip");
}

void create_asset_no_states_round_trip() {
    std::printf("\n  -- CreateAssetTx with no ins/outs --\n");

    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->base.network_id = kUnitTestID;
    utx->base.blockchain_id = chain_id();
    utx->base.memo = Bytes{0x00, 0x01, 0x02, 0x03};
    utx->name = "name";
    utx->symbol = "symb";
    utx->denomination = 0;
    txs::InitialState s;
    s.fx_index = 0;
    s.outs = {mint_out(0)};
    utx->states = {s};

    txs::Tx tx;
    tx.unsigned_tx = utx;
    check(tx.initialize().has_value(), "initialize");

    auto parsed = txs::parse(view(tx.bytes()));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check_eq(hex_of((*parsed)->bytes()), hex_of(tx.bytes()), "parsed bytes are the same bytes");
    auto* got = dynamic_cast<txs::CreateAssetTx*>((*parsed)->unsigned_tx.get());
    if (got == nullptr) {
        check(false, "parsed as CreateAssetTx");
        return;
    }
    check_eq(got->name, "name", "name round-trips");
    check_eq(got->symbol, "symb", "symbol round-trips");
    check(got->base.outs.empty(), "no outputs");
    check(got->base.ins.empty(), "no inputs");
    check(got->states.size() == 1, "one initial state");
    check_eq(hex_of(got->states[0].outs[0]->bytes()), hex_of(utx->states[0].outs[0]->bytes()),
             "state output bytes round-trip");
}

// ================= ImportTx =================

void import_round_trip() {
    std::printf("\n  -- ImportTx round trip --\n");

    const Id source_chain{0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e, 0x2d, 0x7d, 0xad,
                          0xfd, 0x2c, 0x7c, 0xac, 0xfc, 0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a,
                          0xba, 0xea, 0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8};
    const Id imported_tx_id{0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee, 0x0d, 0x2d, 0x4d,
                            0x6d, 0x8c, 0xac, 0xcc, 0xec, 0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa,
                            0xca, 0xea, 0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8};

    auto in = std::make_shared<fx::secp256k1fx::TransferInput>();
    in->amt = 1000;
    in->input.sig_indices = {0};

    auto utx = std::make_shared<txs::ImportTx>();
    utx->base.network_id = kUnitTestID;
    utx->base.blockchain_id = chain_id();
    utx->base.memo = Bytes{0x00, 0x01, 0x02, 0x03};
    utx->source_chain = source_chain;
    utx->imported_ins = {
        txs::TransferableInput{txs::UTXOID{imported_tx_id, 5, false}, asset_id(), in}};

    txs::Tx tx;
    tx.unsigned_tx = utx;
    check(tx.initialize().has_value(), "initialize");

    auto parsed = txs::parse(view(tx.bytes()));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check_eq(hex_of((*parsed)->bytes()), hex_of(tx.bytes()), "parsed bytes are the same bytes");
    check_eq(hex_of((*parsed)->id()), hex_of(tx.id()), "parsed TxID matches");

    auto* got = dynamic_cast<txs::ImportTx*>((*parsed)->unsigned_tx.get());
    if (got == nullptr) {
        check(false, "parsed as ImportTx");
        return;
    }
    check(got->source_chain == source_chain, "source chain round-trips");
    check(got->imported_ins.size() == 1, "one imported input");
    check(got->imported_ins[0].utxo_id == utx->imported_ins[0].utxo_id, "imported UTXOID");
    check(got->imported_ins[0].asset_id == asset_id(), "imported asset");
    check_eq(hex_of(got->imported_ins[0].in->bytes()), hex_of(utx->imported_ins[0].in->bytes()),
             "imported input fx bytes");

    sign_with(tx, {0, 0});
    auto signed_tx = txs::parse(view(tx.bytes()));
    if (!signed_tx) {
        check(false, "parse signed: " + signed_tx.error());
        return;
    }
    check_eq(hex_of((*signed_tx)->id()), hex_of(tx.id()), "signed TxID matches");
    check((*signed_tx)->creds.size() == 1, "one credential");
}

// ================= ExportTx =================

void export_round_trip() {
    std::printf("\n  -- ExportTx round trip --\n");

    const Id destination_chain = Id{0x11, 0x22, 0x33};
    auto utx = std::make_shared<txs::ExportTx>();
    utx->base = sample_base();
    utx->destination_chain = destination_chain;
    utx->exported_outs = {txs::TransferableOutput{asset_id(), transfer_out(999, 1)}};

    txs::Tx tx;
    tx.unsigned_tx = utx;
    check(tx.initialize().has_value(), "initialize");

    auto parsed = txs::parse(view(tx.bytes()));
    if (!parsed) {
        check(false, "parse: " + parsed.error());
        return;
    }
    check_eq(hex_of((*parsed)->bytes()), hex_of(tx.bytes()), "parsed bytes are the same bytes");
    auto* got = dynamic_cast<txs::ExportTx*>((*parsed)->unsigned_tx.get());
    if (got == nullptr) {
        check(false, "parsed as ExportTx");
        return;
    }
    check(got->destination_chain == destination_chain, "destination chain round-trips");
    check(got->exported_outs.size() == 1, "one exported output");
    check_eq(hex_of(got->exported_outs[0].out->bytes()),
             hex_of(utx->exported_outs[0].out->bytes()), "exported output fx bytes");
}

// ================= InitialState =================

void initial_state_cases() {
    std::printf("\n  -- InitialState --\n");

    {
        txs::InitialState is;
        is.fx_index = 0;
        is.outs = {transfer_out(12345, 0, 54321)};
        auto b = is.bytes();
        auto got = txs::parse_initial_state(view(b));
        if (!got) {
            check(false, "round trip: " + got.error());
        } else {
            check(got->fx_index == 0, "fx index round-trips");
            check(got->outs.size() == 1, "one output");
            check_eq(hex_of(got->outs[0]->bytes()), hex_of(is.outs[0]->bytes()),
                     "output bytes round-trip");
        }
    }
    {
        // Go's TestInitialStateVerifyUnknownFxID.
        txs::InitialState is;
        is.fx_index = 1;
        auto r = is.verify(1);
        check(!r && r.error().find(txs::kErrUnknownFx) != std::string::npos, "unknown fx index");
    }
    {
        // Go's TestInitialStateVerifyNilOutput.
        txs::InitialState is;
        is.fx_index = 0;
        is.outs = {nullptr};
        auto r = is.verify(1);
        check(!r && r.error().find(txs::kErrNilFxOutput) != std::string::npos, "nil fx output");
    }
    {
        // Go's TestInitialStateVerifyInvalidOutput: an output that fails its own
        // Verify (amount zero) fails the state's.
        txs::InitialState is;
        is.fx_index = 0;
        is.outs = {transfer_out(0, 0)};
        auto r = is.verify(1);
        check(!r && r.error().find(fx::kErrNoValueOutput) != std::string::npos, "invalid output");
    }
    {
        // Go's TestInitialStateVerifyUnsortedOutputs.
        txs::InitialState is;
        is.fx_index = 0;
        is.outs = {transfer_out(1, 0), transfer_out(2, 0)};
        is.sort();
        std::swap(is.outs[0], is.outs[1]);
        auto bad = is.verify(1);
        check(!bad && bad.error().find(txs::kErrInitialOutputsNotSorted) != std::string::npos,
              "unsorted outputs");
        is.sort();
        check(is.verify(1).has_value(), "sorting fixes it");
    }
    {
        // Go's TestInitialStateCompare.
        txs::InitialState a, b;
        check(a.compare(b) == 0 && b.compare(a) == 0, "equal fx indices compare 0");
        a.fx_index = 1;
        check(a.compare(b) > 0, "higher fx index compares greater");
        check(b.compare(a) < 0, "…and the reverse compares less");
    }
}

// ================= Operation =================

// A property-fx burn operation is the smallest real FxOperation: it names its
// signers and produces nothing. Go's operation_test.go uses a bare test double
// for the same purpose; using a real one instead means the sort key is the real
// wire encoding.
std::shared_ptr<fx::propertyfx::BurnOperation> burn_op() {
    auto op = std::make_shared<fx::propertyfx::BurnOperation>();
    op->input.sig_indices = {0};
    return op;
}

void operation_cases() {
    std::printf("\n  -- Operation --\n");

    {
        txs::Operation op;
        op.asset_id = kEmptyId;
        auto r = op.verify();
        check(!r && r.error().find(txs::kErrNilFxOperation) != std::string::npos,
              "an operation with no fx op is invalid");
    }
    {
        // Go's TestOperationVerifyUTXOIDsNotSorted.
        txs::Operation op;
        op.asset_id = kEmptyId;
        op.utxo_ids = {txs::UTXOID{kEmptyId, 1, false}, txs::UTXOID{kEmptyId, 0, false}};
        op.op = burn_op();
        auto r = op.verify();
        check(!r && r.error().find(txs::kErrNotSortedAndUniqueUTXOIDs) != std::string::npos,
              "unsorted UTXO ids");
    }
    {
        // Go's TestOperationVerify: an empty asset id is still an invalid asset.
        txs::Operation op;
        op.asset_id = kEmptyId;
        op.utxo_ids = {txs::UTXOID{kEmptyId, 1, false}};
        op.op = burn_op();
        auto r = op.verify();
        check(!r && r.error().find(txs::kErrEmptyAssetID) != std::string::npos,
              "the empty asset id is refused");
    }
    {
        Id a{};
        a[0] = 0x42;
        txs::Operation op;
        op.asset_id = a;
        op.utxo_ids = {txs::UTXOID{a, 1, false}};
        op.op = burn_op();
        check(op.verify().has_value(), "a well-formed operation verifies");
    }
    {
        // Go's TestOperationSorting.
        auto make = [&](std::uint32_t index) {
            txs::Operation op;
            op.asset_id = kEmptyId;
            op.utxo_ids = {txs::UTXOID{kEmptyId, index, false}};
            op.op = burn_op();
            return op;
        };
        std::vector<txs::Operation> ops = {make(1), make(0)};
        check(!txs::is_sorted_and_unique_operations(ops), "starts unsorted");
        txs::sort_operations(ops);
        check(txs::is_sorted_and_unique_operations(ops), "sorts");
        ops.push_back(make(1));
        check(!txs::is_sorted_and_unique_operations(ops),
              "a duplicate after sorting is still refused");
    }
    {
        txs::Operation op;
        Id a{};
        a[0] = 0x42;
        op.asset_id = a;
        op.utxo_ids = {txs::UTXOID{a, 1, false}, txs::UTXOID{a, 7, false}};
        op.op = burn_op();
        auto b = op.bytes();
        auto got = txs::parse_operation(view(b));
        if (!got) {
            check(false, "operation round trip: " + got.error());
        } else {
            check(got->asset_id == a, "asset round-trips");
            check(got->utxo_ids.size() == 2, "two UTXO ids");
            check(got->utxo_ids[0] == op.utxo_ids[0] && got->utxo_ids[1] == op.utxo_ids[1],
                  "UTXO ids round-trip");
            check_eq(hex_of(got->op->bytes()), hex_of(op.op->bytes()), "fx op bytes round-trip");
        }
    }
}

// ================= what a tx produces =================

void produced_utxos() {
    std::printf("\n  -- produced UTXOs --\n");

    {
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base = sample_base();
        txs::Tx tx;
        tx.unsigned_tx = utx;
        (void)tx.initialize();
        auto produced = tx.utxos();
        check(produced.size() == 1, "a BaseTx produces one UTXO per output");
        check(produced[0].utxo_id.tx_id == tx.id(), "…owned by the tx");
        check(produced[0].utxo_id.output_index == 0, "…at index 0");
        check(produced[0].asset_id == asset_id(), "…of the output's asset");
    }
    {
        // A CreateAssetTx's initial states produce UTXOs of the NEW asset — the
        // tx id — indexed after the base outputs.
        auto utx = std::make_shared<txs::CreateAssetTx>();
        utx->base = sample_base();
        utx->name = "Asset";
        utx->symbol = "AST";
        txs::InitialState s;
        s.fx_index = 0;
        s.outs = {mint_out(0)};
        utx->states = {s};
        txs::Tx tx;
        tx.unsigned_tx = utx;
        (void)tx.initialize();
        auto produced = tx.utxos();
        check(produced.size() == 2, "one base output plus one state output");
        check(produced[1].utxo_id.output_index == 1, "the state output follows the base outputs");
        check(produced[1].asset_id == tx.id(), "the new asset IS the tx id");
    }
    {
        // Input ids are the sha256 prefix of (index, txID), and a tx reports one
        // per input it consumes.
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base = sample_base();
        auto ids_ = utx->input_ids();
        check(ids_.size() == 1, "one input id");
        check(ids_.count(utx->base.ins[0].utxo_id.input_id()) == 1,
              "…and it is the input's own id");
    }
}

}  // namespace

int main() {
    std::printf("xvm — the transaction vocabulary, ported from the Go txs tests\n");
    base_tx_round_trip();
    create_asset_round_trip();
    create_asset_no_states_round_trip();
    import_round_trip();
    export_round_trip();
    initial_state_cases();
    operation_cases();
    produced_utxos();
    return report("txs");
}
