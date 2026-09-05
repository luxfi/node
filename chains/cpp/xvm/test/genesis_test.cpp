// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis_test.cpp — the buffer a host hands the chain, ported from genesis.go
// and genesis_wire.go, and checked against the bytes the GO implementation
// writes for the same two assets (golden.hpp: kGenesis, kGenesisFeeAssetID).
//
// The vector is the point. A genesis buffer that parses but differs by one byte
// gives every asset a different id, so two nodes booted from "the same" genesis
// would disagree about what every UTXO in the chain is denominated in. Reading
// Go's buffer and re-emitting it byte for byte is the only way to know they do
// not.

#include "lux/core/check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"
#include "keys.hpp"

#include "lux/xvm/genesis.hpp"
#include "lux/xvm/vm.hpp"

using namespace lux::xvm;
using namespace lux::core::test;
using namespace lux::xvm::test;

namespace {

constexpr std::uint32_t kNetworkID = 10;

// The two assets golden_gen.go writes, value for value.
std::shared_ptr<txs::CreateAssetTx> first_asset() {
    auto t = std::make_shared<txs::CreateAssetTx>();
    t->base = base_fields();
    t->name = "Lux Test Asset";
    t->symbol = "LTA";
    t->denomination = 9;
    txs::InitialState s;
    s.fx_index = 0;
    s.outs = {mint_output(), transfer_output()};
    s.sort();
    t->states = {s};
    return t;
}

std::shared_ptr<txs::CreateAssetTx> second_asset() {
    auto t = std::make_shared<txs::CreateAssetTx>();
    t->base.network_id = kNetworkID;
    t->base.blockchain_id = kEmptyId;
    t->name = "Second Asset";
    t->symbol = "SEC";
    t->denomination = 0;
    txs::InitialState s;
    s.fx_index = 0;
    s.outs = {mint_output()};
    s.sort();
    t->states = {s};
    return t;
}

genesis::Genesis two_assets() {
    genesis::Genesis g;
    g.assets.push_back(genesis::Asset{"LTA", first_asset()});
    g.assets.push_back(genesis::Asset{"SEC", second_asset()});
    return g;
}

void bytes_match_go() {
    std::printf("\n  -- the genesis buffer, against Go's own --\n");

    auto b = genesis::bytes(two_assets());
    if (!b) {
        check(false, "serialize: " + b.error());
        return;
    }
    check_eq(hex_of(*b), golden::kGenesis, "the genesis buffer is byte-identical to Go's");

    // The fee asset's id is derived from those bytes, so agreeing on the bytes
    // and disagreeing on the id would be a bug in the derivation rather than in
    // the encoding — worth separating.
    auto fee = genesis::fee_asset_id(view(*b));
    if (!fee) {
        check(false, "fee asset: " + fee.error());
        return;
    }
    check_eq(hex_of(*fee), golden::kGenesisFeeAssetID, "…and the fee asset id Go derives from it");
}

void go_bytes_parse_here() {
    std::printf("\n  -- Go's buffer, read here --\n");

    Bytes go_bytes = from_hex(golden::kGenesis);
    auto g = genesis::parse(view(go_bytes));
    if (!g) {
        check(false, "parse Go's genesis: " + g.error());
        return;
    }
    check(g->assets.size() == 2, "both assets come back");
    check_eq(g->assets[0].alias, "LTA", "the first alias");
    check_eq(g->assets[1].alias, "SEC", "the second alias");
    check(g->assets[0].create != nullptr && g->assets[0].create->name == "Lux Test Asset",
          "the first asset's name");
    check(g->assets[0].create != nullptr && g->assets[0].create->symbol == "LTA",
          "…its symbol");
    check(g->assets[0].create != nullptr && g->assets[0].create->denomination == 9,
          "…and its denomination");
    check(g->assets[1].create != nullptr && g->assets[1].create->name == "Second Asset",
          "the second asset's name");
    check(g->assets[0].create != nullptr && g->assets[0].create->states.size() == 1,
          "the initial state comes back");
    check(g->assets[0].create != nullptr && g->assets[0].create->states[0].outs.size() == 2,
          "…with both of its outputs");

    // Re-emitting what was read gives the same bytes: parsing loses nothing.
    auto again = genesis::bytes(*g);
    check(again.has_value(), "the parsed genesis re-serializes");
    if (again) check_eq(hex_of(*again), golden::kGenesis, "…to the very same buffer");

    auto fee = g->assets[0].id();
    check(fee.has_value(), "the first asset has an id");
    if (fee) check_eq(hex_of(*fee), golden::kGenesisFeeAssetID, "…and it is the fee asset's");
}

void refusals() {
    std::printf("\n  -- what a genesis buffer is refused for --\n");

    check(!genesis::parse(ByteView{}), "empty bytes are not a genesis");
    {
        Bytes junk{0x01, 0x02, 0x03};
        check(!genesis::parse(view(junk)), "three bytes are not a genesis");
    }
    {
        // A buffer with a tail parses the same and hashes differently: two
        // buffers, one genesis, two asset ids. Refused.
        Bytes trailing = from_hex(golden::kGenesis);
        trailing.push_back(0x00);
        auto r = genesis::parse(view(trailing));
        check(!r && r.error().find(genesis::kErrTrailingBytes) != std::string::npos,
              "trailing bytes are refused as non-canonical");
    }
    {
        // A genesis carrying a BaseTx where an asset definition belongs.
        genesis::Genesis g;
        g.assets.push_back(genesis::Asset{"BAD", nullptr});
        check(!genesis::bytes(g), "an asset with no definition cannot be written");
    }
    {
        Bytes empty_genesis;
        auto g = genesis::Genesis{};
        auto b = genesis::bytes(g);
        check(b.has_value(), "an empty genesis serializes");
        if (b) {
            auto parsed = genesis::parse(view(*b));
            check(parsed && parsed->assets.empty(), "…and parses back empty");
            auto fee = genesis::fee_asset_id(view(*b));
            check(!fee && fee.error().find(genesis::kErrNoAssets) != std::string::npos,
                  "…but has no fee asset");
        }
    }
}

void vm_boots_from_genesis() {
    std::printf("\n  -- a VM booted from genesis bytes --\n");

    Bytes go_bytes = from_hex(golden::kGenesis);

    VmConfig cfg;
    cfg.network_id = kNetworkID;
    cfg.chain_id = id(200);
    cfg.net_id = id(0x0A);
    store::Memory store;
    Vm vm(cfg, {executor::ParsedFx{id(1), std::make_shared<fx::Secp256k1Fx>()}}, store);

    auto r = vm.initialize_from_genesis(view(go_bytes), 1749000000);
    check(r.has_value(), r ? "the VM boots from Go's genesis bytes"
                           : "boot: " + r.error());
    if (!r) return;

    // The fee asset is the FIRST asset, adopted from position.
    Bytes want_fee = from_hex(golden::kGenesisFeeAssetID);
    check(Bytes(vm.backend().fee_asset_id.begin(), vm.backend().fee_asset_id.end()) == want_fee,
          "…and charges fees in the first asset");

    auto lta = vm.alias_of("LTA");
    auto sec = vm.alias_of("SEC");
    check(lta && Bytes(lta->begin(), lta->end()) == want_fee, "the LTA alias resolves to it");
    check(sec.has_value() && *sec != *lta, "SEC resolves to a different asset");
    check(!vm.alias_of("NOPE"), "an unknown alias resolves to nothing");

    // Both asset definitions are in the chain, under their own ids, and their
    // initial-state outputs are spendable UTXOs of the asset they created.
    auto asset_tx = vm.chain_state().get_tx(*lta);
    check(asset_tx.has_value(), "the first asset's definition is in the chain");
    // First asset: 1 base output + 2 state outputs. Second: 0 + 1.
    check(vm.chain_state().utxo_count() == 4,
          "every genesis output is a spendable UTXO");
    txs::UTXOID state_out{*lta, 1, false};
    auto u = vm.chain_state().get_utxo(state_out.input_id());
    check(u.has_value(), "a state output of the first asset is there");
    check(u && u->asset_id == *lta, "…denominated in the asset it created");

    check(vm.last_accepted() != kEmptyId, "a genesis block was sealed over it");
    check(vm.last_accepted_height() == 0, "…at height 0");
}

}  // namespace

int main() {
    std::printf("xvm — the genesis buffer, against the Go X-Chain's own bytes\n");
    bytes_match_go();
    go_bytes_parse_here();
    refusals();
    vm_boots_from_genesis();
    return report("genesis");
}
