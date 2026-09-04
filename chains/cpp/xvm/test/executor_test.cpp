// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// executor_test.cpp — what APPLYING a transaction does to the UTXO set, ported
// from txs/executor/executor_test.go.
//
// Each case is the Go one: seed the chain with the UTXOs the transaction will
// spend, apply it, then require that the consumed ones are GONE and that the
// produced ones exist at the exact ids the transaction fixes — (txID, index),
// hashed. Getting the index right is the whole content of the test: an output
// written at the wrong index is a UTXO nobody can spend, and no amount of
// balance arithmetic notices.

#include "check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/executor.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/txs.hpp"

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

constexpr std::uint64_t kKiloLux = 1000ull * 1000ull * 1000ull;  // 1000 LUX, 6 decimals
constexpr std::uint32_t kUnitTestID = 369;

Id chain_id() { return Id{5, 4, 3, 2, 1}; }
Id asset_id() { return Id{1, 2, 3}; }
Id utxo_tx_id() { return id(0x31); }

fx::OutputOwners owner() { return fx::OutputOwners{0, 1, {test_address(0)}}; }

std::shared_ptr<fx::secp256k1fx::TransferOutput> tout(std::uint64_t amt) {
    auto o = std::make_shared<fx::secp256k1fx::TransferOutput>();
    o->amt = amt;
    o->out_owners = owner();
    return o;
}

std::shared_ptr<fx::secp256k1fx::MintOutput> mout() {
    auto o = std::make_shared<fx::secp256k1fx::MintOutput>();
    o->out_owners = owner();
    return o;
}

std::shared_ptr<fx::secp256k1fx::TransferInput> tin(std::uint64_t amt) {
    auto i = std::make_shared<fx::secp256k1fx::TransferInput>();
    i->amt = amt;
    i->input.sig_indices = {0};
    return i;
}

txs::UTXOID utxo_id(std::uint32_t index = 1) { return txs::UTXOID{utxo_tx_id(), index, false}; }

// seed writes the UTXO the transactions below spend, exactly as the Go test's
// state.AddUTXO + Commit does.
txs::UTXO seeded_utxo(std::uint32_t index = 1) {
    txs::UTXO u;
    u.utxo_id = utxo_id(index);
    u.asset_id = asset_id();
    u.out = tout(20 * kKiloLux);
    return u;
}

txs::BaseTxFields spend_base() {
    txs::BaseTxFields b;
    b.network_id = kUnitTestID;
    b.blockchain_id = chain_id();
    b.ins.push_back(txs::TransferableInput{utxo_id(), asset_id(), tin(20 * kKiloLux)});
    b.outs.push_back(txs::TransferableOutput{asset_id(), tout(10 * kKiloLux)});
    return b;
}

std::shared_ptr<txs::Tx> signed_tx(std::shared_ptr<txs::UnsignedTx> u, int num_creds) {
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = std::move(u);
    const Bytes& unsigned_bytes = tx->unsigned_tx->bytes();
    for (int i = 0; i < num_creds; ++i) {
        auto c = std::make_shared<fx::secp256k1fx::Credential>();
        c->signatures = {sign_unsigned_tx(test_key(0), view(unsigned_bytes))};
        tx->creds.push_back(c);
    }
    (void)tx->initialize();
    return tx;
}

// gone reports that a UTXO id is no longer in the set — the shape of Go's
// require.ErrorIs(err, database.ErrNotFound).
bool gone(const state::Chain& chain, const Id& utxo) {
    auto got = chain.get_utxo(utxo);
    return !got && got.error().find(state::kErrNotFound) != std::string::npos;
}

// at requires a UTXO to exist at the id (txID, index) hashes to, and returns it.
bool at(const state::Chain& chain, const Id& tx_id, std::uint32_t index, const Id& asset,
        txs::UTXO* out, const std::string& what) {
    txs::UTXOID want{tx_id, index, false};
    auto got = chain.get_utxo(want.input_id());
    if (!got) {
        check(false, what + " (missing: " + got.error() + ")");
        return false;
    }
    bool ok = got->utxo_id.tx_id == tx_id && got->utxo_id.output_index == index &&
              got->asset_id == asset;
    check(ok, what);
    if (out != nullptr) *out = *got;
    return ok;
}

// ================= BaseTx =================

void base_tx_executor() {
    std::printf("\n  -- BaseTx --\n");

    state::State chain;
    chain.add_utxo(seeded_utxo());

    auto utx = std::make_shared<txs::BaseTx>();
    utx->base = spend_base();
    auto tx = signed_tx(utx, 1);

    executor::Executor exec(chain, *tx);
    auto r = tx->unsigned_tx->visit(exec);
    check(r.has_value(), "applies");

    check(gone(chain, utxo_id().input_id()), "the consumed UTXO is gone");
    txs::UTXO produced;
    if (at(chain, tx->id(), 0, asset_id(), &produced, "the produced UTXO is at index 0")) {
        auto* value = dynamic_cast<fx::secp256k1fx::TransferOutput*>(produced.out.get());
        check(value != nullptr && value->amt == 10 * kKiloLux, "…carrying the output's amount");
        check(value != nullptr && value->out_owners.equals(owner()), "…and the output's owners");
    }
    check(chain.utxo_count() == 1, "the set holds exactly the one produced UTXO");
    check(exec.atomic_requests.empty(), "a BaseTx has no cross-chain effects");
    check(exec.inputs.empty(), "…and imports nothing");
}

// ================= CreateAssetTx =================

void create_asset_tx_executor() {
    std::printf("\n  -- CreateAssetTx --\n");

    state::State chain;
    chain.add_utxo(seeded_utxo());

    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->base = spend_base();
    utx->name = "name";
    utx->symbol = "symb";
    utx->denomination = 0;
    txs::InitialState s;
    s.fx_index = 0;
    s.outs = {mout()};
    utx->states = {s};
    auto tx = signed_tx(utx, 1);

    executor::Executor exec(chain, *tx);
    check(tx->unsigned_tx->visit(exec).has_value(), "applies");

    check(gone(chain, utxo_id().input_id()), "the consumed UTXO is gone");
    at(chain, tx->id(), 0, asset_id(), nullptr, "the base output is at index 0");

    // The state outputs follow the base outputs, and they are outputs of the
    // NEW asset — whose id IS the transaction's, since the asset is what this
    // transaction created.
    txs::UTXO minted;
    if (at(chain, tx->id(), 1, tx->id(), &minted, "the initial state output is at index 1")) {
        check(dynamic_cast<fx::secp256k1fx::MintOutput*>(minted.out.get()) != nullptr,
              "…and it is the mint authority");
    }
    check(chain.utxo_count() == 2, "two UTXOs produced");
}

// ================= OperationTx =================

void operation_tx_executor() {
    std::printf("\n  -- OperationTx --\n");

    state::State chain;
    chain.add_utxo(seeded_utxo(1));

    // The operation consumes a SECOND UTXO — a mint authority — which is not an
    // input of the base tx. That is what makes an operation an operation.
    txs::UTXO op_utxo;
    op_utxo.utxo_id = utxo_id(2);
    op_utxo.asset_id = asset_id();
    op_utxo.out = mout();
    chain.add_utxo(op_utxo);

    auto utx = std::make_shared<txs::OperationTx>();
    utx->base = spend_base();
    txs::Operation op;
    op.asset_id = asset_id();
    op.utxo_ids = {utxo_id(2)};
    auto mint = std::make_shared<fx::secp256k1fx::MintOperation>();
    mint->mint_input.sig_indices = {0};
    mint->mint_output.out_owners = owner();
    mint->transfer_output.amt = 12345;
    mint->transfer_output.out_owners = owner();
    op.op = mint;
    utx->ops = {op};
    auto tx = signed_tx(utx, 2);

    executor::Executor exec(chain, *tx);
    check(tx->unsigned_tx->visit(exec).has_value(), "applies");

    check(gone(chain, utxo_id(1).input_id()), "the spent UTXO is gone");
    check(gone(chain, utxo_id(2).input_id()), "the operated-on UTXO is gone");

    txs::UTXO u0, u1, u2;
    at(chain, tx->id(), 0, asset_id(), &u0, "the base output is at index 0");
    at(chain, tx->id(), 1, asset_id(), &u1, "the operation's mint output is at index 1");
    at(chain, tx->id(), 2, asset_id(), &u2, "the operation's transfer output is at index 2");
    check(dynamic_cast<fx::secp256k1fx::MintOutput*>(u1.out.get()) != nullptr,
          "…index 1 is the re-created mint authority");
    auto* v = dynamic_cast<fx::secp256k1fx::TransferOutput*>(u2.out.get());
    check(v != nullptr && v->amt == 12345, "…index 2 carries the minted amount");
    check(chain.utxo_count() == 3, "three UTXOs produced");
}

// ================= ImportTx =================

void import_tx_executor() {
    std::printf("\n  -- ImportTx --\n");

    state::State chain;
    const Id source = id(0x77);

    auto utx = std::make_shared<txs::ImportTx>();
    utx->base.network_id = kUnitTestID;
    utx->base.blockchain_id = chain_id();
    utx->base.outs.push_back(txs::TransferableOutput{asset_id(), tout(10 * kKiloLux)});
    utx->source_chain = source;
    const txs::UTXOID imported{id(0x55), 4, false};
    utx->imported_ins = {txs::TransferableInput{imported, asset_id(), tin(20 * kKiloLux)}};
    auto tx = signed_tx(utx, 1);

    executor::Executor exec(chain, *tx);
    check(tx->unsigned_tx->visit(exec).has_value(), "applies");

    at(chain, tx->id(), 0, asset_id(), nullptr, "the produced UTXO is at index 0");
    // An imported input is NOT a row in this chain's table, so nothing is
    // deleted locally; what is recorded is the removal request the source chain
    // must honour.
    check(exec.inputs.size() == 1 && exec.inputs.count(imported.input_id()) == 1,
          "the imported input is recorded");
    auto it = exec.atomic_requests.find(source);
    check(it != exec.atomic_requests.end(), "an atomic request is addressed to the source chain");
    if (it != exec.atomic_requests.end()) {
        check(it->second.remove_requests.size() == 1, "…one removal request");
        Id key = imported.input_id();
        check(it->second.remove_requests[0] == Bytes(key.begin(), key.end()),
              "…keyed by the imported UTXO's id");
        check(it->second.put_requests.empty(), "…and nothing is put");
    }
}

// ================= ExportTx =================

void export_tx_executor() {
    std::printf("\n  -- ExportTx --\n");

    state::State chain;
    chain.add_utxo(seeded_utxo());
    const Id destination = id(0x88);

    auto utx = std::make_shared<txs::ExportTx>();
    utx->base = spend_base();
    utx->destination_chain = destination;
    utx->exported_outs = {txs::TransferableOutput{asset_id(), tout(5 * kKiloLux)}};
    auto tx = signed_tx(utx, 1);

    executor::Executor exec(chain, *tx);
    check(tx->unsigned_tx->visit(exec).has_value(), "applies");

    check(gone(chain, utxo_id().input_id()), "the consumed UTXO is gone");
    at(chain, tx->id(), 0, asset_id(), nullptr, "the change output stays on this chain");
    // The exported output is NOT written locally: it left.
    txs::UTXOID exported{tx->id(), 1, false};
    check(gone(chain, exported.input_id()), "the exported output is not in this chain's set");
    check(chain.utxo_count() == 1, "only the change output remains");

    auto it = exec.atomic_requests.find(destination);
    check(it != exec.atomic_requests.end(),
          "an atomic request is addressed to the destination chain");
    if (it != exec.atomic_requests.end()) {
        check(it->second.put_requests.size() == 1, "…one put request");
        const auto& elem = it->second.put_requests[0];
        Id key = exported.input_id();
        check(elem.key == Bytes(key.begin(), key.end()),
              "…keyed by the exported UTXO's id, at index 1");
        check(!elem.traits.empty(), "…carrying the owner addresses as traits");

        // The value is the SAME ZAP envelope this chain would have written to
        // disk, which is why the importing chain needs no second decoder.
        auto parsed = txs::parse_utxo(view(elem.value));
        if (!parsed) {
            check(false, "the exported value parses as a UTXO: " + parsed.error());
        } else {
            check(parsed->utxo_id.tx_id == tx->id() && parsed->utxo_id.output_index == 1,
                  "…and it parses back to the same UTXOID");
            auto* v = dynamic_cast<fx::secp256k1fx::TransferOutput*>(parsed->out.get());
            check(v != nullptr && v->amt == 5 * kKiloLux, "…with the exported amount");
        }
        check(it->second.remove_requests.empty(), "…and nothing is removed");
    }
}

}  // namespace

int main() {
    std::printf("xvm — applying a transaction, ported from executor_test.go\n");
    base_tx_executor();
    create_asset_tx_executor();
    operation_tx_executor();
    import_tx_executor();
    export_tx_executor();
    return report("executor");
}
