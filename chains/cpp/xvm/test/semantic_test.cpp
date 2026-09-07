// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// semantic_test.cpp — the chain's own agreement, ported case for case from
// txs/executor/semantic_verifier_test.go.
//
// Syntactic verification asks whether a transaction is well-formed on its own.
// Semantic verification asks the four questions only the CHAIN can answer:
//
//   does the UTXO this input names exist?
//   does it hold the asset the input claims?
//   does the credential actually authorize spending it?
//   is the fx being used one the asset itself declared?
//
// Go answers them against a mock Chain that returns exactly what each case
// wants. Here the chain is real and seeded to the same shape — the same
// question, asked of a real store, which is strictly harder to fake.

#include "lux/core/check.hpp"
#include "fixtures.hpp"
#include "keys.hpp"

#include "lux/xvm/executor.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/txs.hpp"

using namespace lux::xvm;
using namespace lux::core::test;
using namespace lux::xvm::test;

namespace {

Id asset_id() { return id(0x51); }
Id chain_id() { return id(0xC1); }
Id peer_chain_id() { return id(0xCC); }
Id local_net() { return id(0x0A); }
Id peer_net() { return id(0x0B); }
Id spent_tx_id() { return id(0x22); }

txs::UTXOID spent_utxo_id() { return txs::UTXOID{spent_tx_id(), 2, false}; }

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

// the_asset is the CreateAssetTx the chain holds under the asset's id: what
// "this asset declares fx 0" is actually stored as.
std::shared_ptr<txs::Tx> the_asset(bool declares_fx) {
    auto utx = std::make_shared<txs::CreateAssetTx>();
    utx->name = "asset";
    utx->symbol = "AST";
    if (declares_fx) {
        txs::InitialState s;
        s.fx_index = 0;
        utx->states = {s};
    }
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

// not_an_asset is a plain BaseTx stored where the asset should be — the chain
// knows the id, but what it names is not an asset definition.
std::shared_ptr<txs::Tx> not_an_asset() {
    auto utx = std::make_shared<txs::BaseTx>();
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = utx;
    (void)tx->initialize();
    return tx;
}

// A net lookup that says which network each chain belongs to.
struct Nets final : executor::NetLookup {
    std::map<Id, Id> of;
    wire::Result<Id> network_of(const Id& chain) const override {
        auto it = of.find(chain);
        if (it == of.end()) return std::unexpected("unknown chain");
        return it->second;
    }
};

// Shared memory holding exactly the UTXO bytes an import will read.
struct Memory final : executor::SharedMemory {
    std::map<Bytes, Bytes> rows;
    bool fail = false;
    wire::Result<std::vector<Bytes>> get(const Id&,
                                         const std::vector<Bytes>& keys) const override {
        if (fail) return std::unexpected("shared memory is unavailable");
        std::vector<Bytes> out;
        for (const auto& k : keys) {
            auto it = rows.find(k);
            if (it == rows.end()) return std::unexpected("not found in shared memory");
            out.push_back(it->second);
        }
        return out;
    }
};

struct Rig {
    state::State chain;
    Nets nets;
    Memory memory;
    executor::Backend backend;

    Rig() {
        backend.config.tx_fee = 0;
        backend.config.create_asset_tx_fee = 0;
        backend.network_id = 369;
        backend.chain_id = chain_id();
        backend.net_id = local_net();
        backend.fee_asset_id = id(0xFE);
        backend.bootstrapped = true;
        auto secp = std::make_shared<fx::Secp256k1Fx>();
        secp->bootstrapping();
        secp->bootstrapped();
        secp->clock = &clock;
        backend.fxs.push_back(executor::ParsedFx{id(1), secp});
        backend.fx_index.set(wire::TypeKind::Secp256k1, 0);
        backend.net_lookup = &nets;
        backend.shared_memory = &memory;
        nets.of[chain_id()] = local_net();
        nets.of[peer_chain_id()] = local_net();
    }

    fx::Clock clock{1749000000};

    // seed_utxo writes the UTXO an input will name.
    void seed_utxo(const Id& asset, std::uint64_t amt, int key = 0) {
        txs::UTXO u;
        u.utxo_id = spent_utxo_id();
        u.asset_id = asset;
        u.out = tout(amt, key);
        chain.add_utxo(u);
    }
    void seed_asset(std::shared_ptr<txs::Tx> tx) {
        // The asset is stored under the ASSET's id, which is what
        // verify_fx_usage looks up — not under the tx's own id.
        assets[asset_id()] = std::move(tx);
    }
    std::map<Id, std::shared_ptr<txs::Tx>> assets;
};

// AssetChain is a chain whose GetTx answers from an explicit table, so a case
// can say "this id is not an asset" or "this id is unknown" without having to
// forge a transaction whose hash lands on the asset's id.
struct AssetChain final : state::ReadOnlyChain {
    state::Chain* under;
    const std::map<Id, std::shared_ptr<txs::Tx>>* assets;

    AssetChain(state::Chain* u, const std::map<Id, std::shared_ptr<txs::Tx>>* a)
        : under(u), assets(a) {}

    wire::Result<txs::UTXO> get_utxo(const Id& utxo_id) const override {
        return under->get_utxo(utxo_id);
    }
    std::vector<txs::UTXO> utxos(const Id& start, int limit) const override {
        return under->utxos(start, limit);
    }
    wire::Result<std::shared_ptr<txs::Tx>> get_tx(const Id& tx_id) const override {
        auto it = assets->find(tx_id);
        if (it == assets->end()) return std::unexpected(state::kErrNotFound);
        return it->second;
    }
    wire::Result<Id> get_block_id_at_height(std::uint64_t h) const override {
        return under->get_block_id_at_height(h);
    }
    wire::Result<std::shared_ptr<block::StandardBlock>> get_block(
        const Id& blk_id) const override {
        return under->get_block(blk_id);
    }
    Id get_last_accepted() const override { return under->get_last_accepted(); }
    std::uint64_t get_timestamp() const override { return under->get_timestamp(); }
};

// sign attaches one credential per signer set, over the tx's unsigned bytes.
std::shared_ptr<txs::Tx> sign(std::shared_ptr<txs::UnsignedTx> u, const std::vector<int>& keys) {
    auto tx = std::make_shared<txs::Tx>();
    tx->unsigned_tx = std::move(u);
    const Bytes& unsigned_bytes = tx->unsigned_tx->bytes();
    for (int k : keys) {
        auto c = std::make_shared<fx::secp256k1fx::Credential>();
        c->signatures = {sign_unsigned_tx(test_key(k), view(unsigned_bytes))};
        tx->creds.push_back(c);
    }
    (void)tx->initialize();
    return tx;
}

void expect(Rig& rig, const std::string& name, txs::Tx& tx, const std::string& want) {
    AssetChain chain(&rig.chain, &rig.assets);
    executor::SemanticVerifier v(rig.backend, chain, tx);
    auto r = tx.unsigned_tx->visit(v);
    bool ok = want.empty() ? r.has_value() : (!r && r.error().find(want) != std::string::npos);
    check(ok, name);
    if (!ok) {
        std::printf("        got  \"%s\"\n        want \"%s\"\n", r ? "" : r.error().c_str(),
                    want.c_str());
    }
}

// ================= BaseTx =================

std::shared_ptr<txs::BaseTx> spending_base(const Id& asset, std::uint64_t amt) {
    auto utx = std::make_shared<txs::BaseTx>();
    utx->base.network_id = 369;
    utx->base.blockchain_id = chain_id();
    utx->base.ins.push_back(txs::TransferableInput{spent_utxo_id(), asset, tin(amt)});
    return utx;
}

void base_tx_cases() {
    std::printf("\n  -- BaseTx --\n");

    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "valid", *tx, "");
    }
    {
        // The UTXO holds a different asset than the input claims.
        Rig rig;
        rig.seed_utxo(id(0x52), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "assetID mismatch", *tx, executor::kErrAssetIDMismatch);
    }
    {
        // The asset exists but declares no fx, so the fx this input uses is not
        // one the asset ever allowed.
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(false));
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "not allowed input feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // Signed by a key that is not the owner.
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(spending_base(asset_id(), 12345), {1});
        expect(rig, "invalid signature", *tx, fx::kErrWrongSig);
    }
    {
        Rig rig;  // nothing seeded
        rig.seed_asset(the_asset(true));
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "missing UTXO", *tx, state::kErrNotFound);
    }
    {
        // The UTXO is worth one less than the input claims to spend.
        Rig rig;
        rig.seed_utxo(asset_id(), 12344);
        rig.seed_asset(the_asset(true));
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "invalid UTXO amount", *tx, fx::kErrMismatchedAmounts);
    }
    {
        // An OUTPUT of an asset that declares no fx is refused too — the gate is
        // on both sides of the transaction.
        Rig rig;
        rig.seed_asset(the_asset(false));
        auto utx = std::make_shared<txs::BaseTx>();
        utx->base.network_id = 369;
        utx->base.blockchain_id = chain_id();
        utx->base.outs.push_back(txs::TransferableOutput{asset_id(), tout(12345)});
        auto tx = sign(utx, {});
        expect(rig, "not allowed output feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // The chain has never heard of the asset.
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "unknown asset", *tx, state::kErrNotFound);
    }
    {
        // The id resolves, but to something that is not an asset definition.
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(not_an_asset());
        auto tx = sign(spending_base(asset_id(), 12345), {0});
        expect(rig, "not an asset", *tx, executor::kErrNotAnAsset);
    }
    {
        // Bootstrapping replays history, so signatures are not re-checked — but
        // everything that is not a signature still is.
        Rig rig;
        rig.backend.bootstrapped = false;
        for (auto& p : rig.backend.fxs) p.fx->bootstrapping();
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(spending_base(asset_id(), 12345), {1});
        expect(rig, "a wrong signature is not checked while bootstrapping", *tx, "");
    }
}

// ================= ExportTx =================

void export_tx_cases() {
    std::printf("\n  -- ExportTx --\n");

    auto build = [](const Id& destination) {
        auto utx = std::make_shared<txs::ExportTx>();
        utx->base.network_id = 369;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(txs::TransferableInput{spent_utxo_id(), asset_id(), tin(12345)});
        utx->destination_chain = destination;
        utx->exported_outs = {txs::TransferableOutput{asset_id(), tout(12345)}};
        return utx;
    };

    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "valid", *tx, "");
    }
    {
        Rig rig;
        rig.seed_utxo(id(0x52), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "assetID mismatch", *tx, executor::kErrAssetIDMismatch);
    }
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(false));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "not allowed input feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // The other half of the same gate: what an export SENDS is checked
        // against the asset's declared fxs too. Without inputs there is nothing
        // to spend and nothing to sign, so the exported output is the only thing
        // the verifier can be refusing — which is what makes this case distinct
        // from the input one above rather than a second spelling of it.
        Rig rig;
        rig.seed_asset(the_asset(false));
        auto utx = build(peer_chain_id());
        utx->base.ins.clear();
        auto tx = sign(utx, {});
        expect(rig, "not allowed output feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // …and it passes when the asset DOES declare the fx, so the refusal above
        // is the declaration and not the empty input list.
        Rig rig;
        rig.seed_asset(the_asset(true));
        auto utx = build(peer_chain_id());
        utx->base.ins.clear();
        auto tx = sign(utx, {});
        expect(rig, "an exported output of an asset that declares the fx", *tx, "");
    }
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {1});
        expect(rig, "invalid signature", *tx, fx::kErrWrongSig);
    }
    {
        Rig rig;
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "missing UTXO", *tx, state::kErrNotFound);
    }
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12344);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "invalid UTXO amount", *tx, fx::kErrMismatchedAmounts);
    }
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "unknown asset", *tx, state::kErrNotFound);
    }
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(not_an_asset());
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "not an asset", *tx, executor::kErrNotAnAsset);
    }
    {
        // Go's TestSemanticVerifierExportTxDifferentNet: exporting to a chain on
        // ANOTHER network would move value across a boundary no validator set
        // spans, so it is refused.
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        rig.nets.of[peer_chain_id()] = peer_net();
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "a destination on a different network", *tx, executor::kErrMismatchedNetIDs);
    }
    {
        // Exporting to THIS chain is not an export.
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(chain_id()), {0});
        expect(rig, "a destination that is this chain", *tx, executor::kErrSameChainID);
    }
    {
        // Before bootstrapping finishes there is no validator set to ask, so the
        // network check is skipped — exactly as Go's `if v.Bootstrapped` does.
        Rig rig;
        rig.backend.bootstrapped = false;
        for (auto& p : rig.backend.fxs) p.fx->bootstrapping();
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        rig.nets.of[peer_chain_id()] = peer_net();
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "…and is not asked while bootstrapping", *tx, "");
    }
}

// ================= ImportTx =================

void import_tx_cases() {
    std::printf("\n  -- ImportTx --\n");

    auto build = [](const Id& source) {
        auto utx = std::make_shared<txs::ImportTx>();
        utx->base.network_id = 369;
        utx->base.blockchain_id = chain_id();
        utx->source_chain = source;
        utx->imported_ins = {
            txs::TransferableInput{spent_utxo_id(), asset_id(), tin(12345)}};
        return utx;
    };

    // put writes the exported UTXO into shared memory the way an ExportTx on the
    // peer chain would have: the same ZAP envelope, keyed by the UTXO's id.
    auto put = [](Rig& rig, std::uint64_t amt, int key) {
        txs::UTXO u;
        u.utxo_id = spent_utxo_id();
        u.asset_id = asset_id();
        u.out = tout(amt, key);
        auto bytes = u.wire_bytes();
        check(bytes.has_value(), "the exported UTXO serializes");
        Id k = u.utxo_id.input_id();
        rig.memory.rows[Bytes(k.begin(), k.end())] = bytes ? *bytes : Bytes{};
    };

    {
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "valid", *tx, "");
    }
    {
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(false));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "not allowed input feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // An import may also PAY OUT on this chain, and what it pays out is
        // checked against the asset's declared fxs like any other output. The
        // imported input is left in place — an ImportTx with none is refused
        // syntactically — so the only thing failing here is the output.
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(false));
        auto utx = build(peer_chain_id());
        utx->base.outs.push_back(txs::TransferableOutput{asset_id(), tout(12345)});
        auto tx = sign(utx, {0});
        expect(rig, "not allowed output feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // The same import, against an asset that declares the fx, is fine.
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(true));
        auto utx = build(peer_chain_id());
        utx->base.outs.push_back(txs::TransferableOutput{asset_id(), tout(12345)});
        auto tx = sign(utx, {0});
        expect(rig, "an imported payout of an asset that declares the fx", *tx, "");
    }
    {
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {1});
        expect(rig, "invalid signature", *tx, fx::kErrWrongSig);
    }
    {
        Rig rig;
        put(rig, 12345, 0);
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "unknown asset", *tx, state::kErrNotFound);
    }
    {
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(not_an_asset());
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "not an asset", *tx, executor::kErrNotAnAsset);
    }
    {
        // The peer chain never put the UTXO there.
        Rig rig;
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "the UTXO is not in shared memory", *tx, "not found");
    }
    {
        // The imported UTXO is worth less than the input claims.
        Rig rig;
        put(rig, 12344, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "invalid UTXO amount", *tx, fx::kErrMismatchedAmounts);
    }
    {
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(true));
        rig.nets.of[peer_chain_id()] = peer_net();
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "a source on a different network", *tx, executor::kErrMismatchedNetIDs);
    }
    {
        Rig rig;
        put(rig, 12345, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(chain_id()), {0});
        expect(rig, "a source that is this chain", *tx, executor::kErrSameChainID);
    }
    {
        // While bootstrapping, an import touches neither shared memory nor the
        // network check.
        Rig rig;
        rig.backend.bootstrapped = false;
        for (auto& p : rig.backend.fxs) p.fx->bootstrapping();
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(peer_chain_id()), {0});
        expect(rig, "…none of which is asked while bootstrapping", *tx, "");
    }
}

// ================= OperationTx =================

void operation_tx_cases() {
    std::printf("\n  -- OperationTx --\n");

    // The mint authority the operation RE-CREATES must equal the one it
    // consumed, so `mint_key` names both — a case that wants a wrong signature
    // has to keep them equal and sign with someone else, or it would trip the
    // mint-output check first and never reach the signature at all.
    auto build = [](const Id& op_asset, int mint_key = 0) {
        auto utx = std::make_shared<txs::OperationTx>();
        utx->base.network_id = 369;
        utx->base.blockchain_id = chain_id();
        txs::Operation op;
        op.asset_id = op_asset;
        op.utxo_ids = {spent_utxo_id()};
        auto mint = std::make_shared<fx::secp256k1fx::MintOperation>();
        mint->mint_input.sig_indices = {0};
        mint->mint_output.out_owners = owner(mint_key);
        mint->transfer_output.amt = 1;
        mint->transfer_output.out_owners = owner(mint_key);
        op.op = mint;
        utx->ops = {op};
        return utx;
    };

    auto seed_mint = [](Rig& rig, int key) {
        txs::UTXO u;
        u.utxo_id = spent_utxo_id();
        u.asset_id = asset_id();
        auto m = std::make_shared<fx::secp256k1fx::MintOutput>();
        m->out_owners = owner(key);
        u.out = m;
        rig.chain.add_utxo(u);
    };

    {
        Rig rig;
        seed_mint(rig, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(asset_id()), {0});
        expect(rig, "valid", *tx, "");
    }
    {
        // The operation names an asset the consumed UTXO does not hold.
        Rig rig;
        seed_mint(rig, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(id(0x52)), {0});
        expect(rig, "assetID mismatch", *tx, executor::kErrAssetIDMismatch);
    }
    {
        Rig rig;
        seed_mint(rig, 1);  // owned by someone else
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(asset_id(), 1), {0});
        expect(rig, "invalid signature", *tx, fx::kErrWrongSig);
    }
    {
        // Re-creating a DIFFERENT mint authority is refused before any
        // signature is looked at: minting must not let a holder rewrite who may
        // mint next.
        Rig rig;
        seed_mint(rig, 0);
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(asset_id(), 1), {0});
        expect(rig, "the mint authority must be re-created unchanged", *tx,
               fx::kErrWrongMintCreated);
    }
    {
        Rig rig;
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(asset_id()), {0});
        expect(rig, "missing UTXO", *tx, state::kErrNotFound);
    }
    {
        Rig rig;
        seed_mint(rig, 0);
        rig.seed_asset(the_asset(false));
        auto tx = sign(build(asset_id()), {0});
        expect(rig, "not allowed feature extension", *tx, executor::kErrIncompatibleFx);
    }
    {
        // Operations are only verified once bootstrapped, for the same reason
        // signatures are: history is being replayed, not judged.
        Rig rig;
        rig.backend.bootstrapped = false;
        for (auto& p : rig.backend.fxs) p.fx->bootstrapping();
        rig.seed_asset(the_asset(true));
        auto tx = sign(build(asset_id()), {0});
        expect(rig, "…and are not verified while bootstrapping", *tx, "");
    }
}

// ================= CreateAssetTx =================

void create_asset_tx_cases() {
    std::printf("\n  -- CreateAssetTx --\n");

    // A CreateAssetTx is semantically a BaseTx: the asset it creates does not
    // exist yet, so there is nothing about it to look up.
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto utx = std::make_shared<txs::CreateAssetTx>();
        utx->base.network_id = 369;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(txs::TransferableInput{spent_utxo_id(), asset_id(), tin(12345)});
        utx->name = "new";
        utx->symbol = "NEW";
        txs::InitialState s;
        s.fx_index = 0;
        s.outs = {tout(1)};
        utx->states = {s};
        auto tx = sign(utx, {0});
        expect(rig, "valid", *tx, "");
    }
    {
        Rig rig;
        rig.seed_utxo(asset_id(), 12345);
        rig.seed_asset(the_asset(true));
        auto utx = std::make_shared<txs::CreateAssetTx>();
        utx->base.network_id = 369;
        utx->base.blockchain_id = chain_id();
        utx->base.ins.push_back(txs::TransferableInput{spent_utxo_id(), asset_id(), tin(12345)});
        utx->name = "new";
        utx->symbol = "NEW";
        auto tx = sign(utx, {1});
        expect(rig, "invalid signature", *tx, fx::kErrWrongSig);
    }
}

}  // namespace

int main() {
    std::printf("xvm — the chain's agreement, ported from semantic_verifier_test.go\n");
    base_tx_cases();
    export_tx_cases();
    import_tx_cases();
    operation_tx_cases();
    create_asset_tx_cases();
    return report("semantic");
}
