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

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"
#include "keys.hpp"

#include "lux/xvm/genesis.hpp"
#include "lux/xvm/vm.hpp"

#include <algorithm>
#include <map>
#include <string>
#include <vector>

using namespace lux::xvm;
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

// ================= a genesis STATED rather than encoded =================
//
// Ported from genesis/lux_test.go, whose BuildBytes(networkID, descriptor,
// holders, memo) is exactly the composition below: one asset definition keyed by
// its own symbol, through NewGenesis and then Bytes. AssetIDFromBytes is this
// port's genesis::fee_asset_id.
//
// The bech32 strings are GO's, printed by luxfi/address.FormatBech32 for the same
// twenty-byte payloads Go's `addr(t, [20]byte{…})` helper uses. Hard-coding what
// Go printed rather than re-deriving it is the point: a bech32 bug on this side
// would otherwise agree with itself.

constexpr const char* kLocal_1_2_3 = "local1qypqxqqqqqqqqqqqqqqqqqqqqqqqqqqqtrx2mw";  // {1,2,3}
constexpr const char* kLocal_4_5_6 = "local1qszsvqqqqqqqqqqqqqqqqqqqqqqqqqqq6hr9jg";  // {4,5,6}
constexpr const char* kLocal_9_9_9 = "local1pyysjqqqqqqqqqqqqqqqqqqqqqqqqqqqv0nnj3";  // {9,9,9}

// build_bytes is genesis.BuildBytes: the asset is registered under its own
// symbol, holders become fixed-cap outputs, and the memo rides on the tx.
genesis::Result<Bytes> build_bytes(std::uint32_t network_id, const std::string& name,
                                   const std::string& symbol, std::uint8_t denomination,
                                   const std::vector<genesis::Holder>& holders,
                                   const Bytes& memo) {
    genesis::AssetDefinition def;
    def.name = name;
    def.symbol = symbol;
    def.denomination = denomination;
    def.memo = memo;
    def.initial_state.fixed_cap = holders;

    auto g = genesis::from_definitions(network_id, {{symbol, def}});
    if (!g) return std::unexpected(g.error());
    return genesis::bytes(*g);
}

void a_genesis_built_from_a_description() {
    std::printf("\n  -- a genesis stated as holdings --\n");

    const std::vector<genesis::Holder> holders = {
        genesis::Holder{1000000, kLocal_1_2_3},
        genesis::Holder{2000000, kLocal_4_5_6},
    };
    const char* memo_text = "deadbeef";
    const Bytes memo(memo_text, memo_text + 8);

    // TestBuildBytes_Deterministic — a pure function of its inputs. The genesis
    // hash check depends on it: nothing about map iteration order or the clock may
    // reach the bytes.
    auto b1 = build_bytes(1, "Lux", "LUX", 9, holders, memo);
    check(b1.has_value(), b1 ? "a stated genesis serializes" : "build: " + b1.error());
    if (!b1) return;
    check(!b1->empty(), "…to a non-empty buffer");
    auto b2 = build_bytes(1, "Lux", "LUX", 9, holders, memo);
    check(b2 && *b1 == *b2, "building it twice gives byte-identical bytes");

    // TestBuildBytes_NetworkScoped — the networkID reaches the buffer. This is
    // what keeps mainnet and testnet genesis hashes distinct.
    auto mainnet = build_bytes(1, "Lux", "LUX", 9, {holders[0]}, {});
    auto testnet = build_bytes(2, "Lux", "LUX", 9, {holders[0]}, {});
    check(mainnet && testnet, "the same asset builds on two networks");
    if (mainnet && testnet)
        check(*mainnet != *testnet, "a different networkID produces different bytes");

    // TestBuildBytes_EmptyHolders — an asset with no initial holders is valid; the
    // genesis simply has no fixed-cap outputs.
    auto none = build_bytes(1, "Lux", "LUX", 9, {}, {});
    check(none.has_value(), none ? "an asset with no holders is a valid genesis"
                                 : "no holders: " + none.error());
    if (none) {
        check(!none->empty(), "…and still serializes to something");
        auto parsed = genesis::parse(view(*none));
        check(parsed && parsed->assets.size() == 1, "…carrying the one asset");
        check(parsed && parsed->assets[0].create != nullptr &&
                  parsed->assets[0].create->states.empty(),
              "…with no initial state at all");
    }

    // TestBuildBytes_BadAddress — a malformed holder address surfaces an error
    // rather than silently producing garbage bytes.
    auto bad = build_bytes(1, "Lux", "LUX", 9,
                           {genesis::Holder{1, "not-a-bech32-address"}}, {});
    check(!bad, "a malformed holder address is refused");
    check(!bad && bad.error().find("holder address") != std::string::npos,
          "…and the reason names the holder's address");
}

void the_asset_id_follows_the_bytes() {
    std::printf("\n  -- the asset id is a function of the buffer --\n");

    const std::vector<genesis::Holder> holders = {genesis::Holder{1000000, kLocal_1_2_3}};

    // TestAssetIDFromBytes_Stable, BOTH halves. Same blob → same id, because every
    // node must agree on the asset id after parsing genesis; different blob →
    // different id, because sovereign L1s sharing a primary-network id must get
    // distinct X-Chain assets. The second half is the one that makes the first
    // mean something.
    auto bytes1 = build_bytes(1, "Lux", "LUX", 9, holders, {});
    check(bytes1.has_value(), bytes1 ? "network 1 builds" : "build: " + bytes1.error());
    if (!bytes1) return;
    auto id1 = genesis::fee_asset_id(view(*bytes1));
    check(id1.has_value(), "…and yields an asset id");
    if (!id1) return;
    check(*id1 != kEmptyId, "…which is not the empty id");

    auto id1_again = genesis::fee_asset_id(view(*bytes1));
    check(id1_again && *id1_again == *id1, "the asset id is a pure function of the buffer");

    auto bytes2 = build_bytes(2, "Lux", "LUX", 9, holders, {});
    check(bytes2.has_value(), "network 2 builds");
    if (!bytes2) return;
    auto id2 = genesis::fee_asset_id(view(*bytes2));
    check(id2.has_value(), "…and yields its own asset id");
    if (id2)
        check(*id2 != *id1, "a different networkID produces a different X-Chain asset id");

    // TestAssetIDFromBytes_HolderSensitivity — two buffers identical but for their
    // initial holders must also produce different ids. The holder set is inside
    // the CreateAssetTx, so the id the chain runs on has to reflect it.
    auto a = build_bytes(1, "Lux", "LUX", 9, {genesis::Holder{1000000, kLocal_1_2_3}}, {});
    auto b = build_bytes(1, "Lux", "LUX", 9, {genesis::Holder{1000000, kLocal_9_9_9}}, {});
    check(a && b, "two genesis buffers differing only in their holder build");
    if (!a || !b) return;
    auto id_a = genesis::fee_asset_id(view(*a));
    auto id_b = genesis::fee_asset_id(view(*b));
    check(id_a && id_b, "…and both yield asset ids");
    if (id_a && id_b)
        check(*id_a != *id_b, "a different holder set produces a different X-Chain asset id");

    // …and the amount is bound in too: the same address holding a different
    // balance is a different genesis.
    auto c = build_bytes(1, "Lux", "LUX", 9, {genesis::Holder{1000001, kLocal_1_2_3}}, {});
    auto id_c = c ? genesis::fee_asset_id(view(*c)) : genesis::Result<Id>{std::unexpected("")};
    check(id_c && *id_c != *id_a, "…and so is the amount a holder starts with");
}

// TestBuildGenesis — the three-asset request the static service has always
// accepted: one fixed-cap asset with four holders, and two variable-cap assets
// with their minter groups. Go asserts only that BuildGenesis returns no error;
// the port can be asked what it built, so it is.
//
// The four addresses are Go's own addrStrArray, formatted under the unit-test HRP
// exactly as Go's test formats them.
void the_three_asset_genesis() {
    std::printf("\n  -- the three-asset genesis --\n");

    // constants.UnitTestHRP is "testing"; these are luxfi/address.FormatBech32's
    // output for Go's four addrStrArray short ids.
    const std::string a1 = "testing1v3vn3wm6ugvnyu8xllhsp83kvng3up7pl88ry3";
    const std::string a2 = "testing18av0mghfa2xeujccrqe2q7exmt3gdukt0m8g74";
    const std::string a3 = "testing18auw2yxlv27y3vyznmqx66ntnqrz662np4ph9k";
    const std::string a4 = "testing1c4ys8hj3w7sk77q3wu009ar9nk0gv3n3z0cr6q";
    constexpr std::uint64_t kStartBalance = 10ULL * 1000000000ULL;  // Go's startBalance

    std::map<std::string, genesis::AssetDefinition> defs;
    {
        genesis::AssetDefinition d;
        d.name = "myFixedCapAsset";
        d.symbol = "MFCA";
        d.denomination = 8;
        d.initial_state.fixed_cap = {
            genesis::Holder{100000, a1},
            genesis::Holder{100000, a2},
            genesis::Holder{kStartBalance, a3},
            genesis::Holder{kStartBalance, a4},
        };
        defs["asset1"] = d;
    }
    {
        genesis::AssetDefinition d;
        d.name = "myVarCapAsset";
        d.symbol = "MVCA";
        d.initial_state.variable_cap = {
            genesis::Owners{1, {a1, a2}},
            genesis::Owners{2, {a3, a4}},
        };
        defs["asset2"] = d;
    }
    {
        genesis::AssetDefinition d;
        d.name = "myOtherVarCapAsset";
        d.initial_state.variable_cap = {genesis::Owners{1, {a1}}};
        defs["asset3"] = d;
    }

    auto g = genesis::from_definitions(12345, defs);
    check(g.has_value(), g ? "the three-asset genesis is built" : "build: " + g.error());
    if (!g) return;
    auto b = genesis::bytes(*g);
    check(b.has_value(), b ? "…and serializes" : "serialize: " + b.error());
    if (!b) return;

    // What it built. The aliases are the request's keys, in the key order the
    // buffer fixes.
    check(g->assets.size() == 3, "all three assets are there");
    check_eq(g->assets[0].alias, "asset1", "the first alias");
    check_eq(g->assets[1].alias, "asset2", "the second");
    check_eq(g->assets[2].alias, "asset3", "the third");

    // The fixed-cap asset's four holdings became four transfer outputs, each a
    // one-of-one owner, carrying exactly the amounts asked for.
    const auto& mfca = g->assets[0].create;
    check(mfca != nullptr && mfca->name == "myFixedCapAsset", "the first asset's name");
    check(mfca != nullptr && mfca->denomination == 8, "…and its denomination");
    check(mfca != nullptr && mfca->states.size() == 1, "…with one initial state");
    if (mfca != nullptr && mfca->states.size() == 1) {
        const auto& outs = mfca->states[0].outs;
        check(outs.size() == 4, "…holding four outputs");
        std::uint64_t total = 0;
        bool all_transfer = true;
        for (const auto& o : outs) {
            const auto* t = dynamic_cast<const fx::secp256k1fx::TransferOutput*>(o.get());
            if (t == nullptr) {
                all_transfer = false;
                continue;
            }
            total += t->amt;
            if (t->out_owners.threshold != 1 || t->out_owners.addrs.size() != 1)
                all_transfer = false;
        }
        check(all_transfer, "…every one a one-of-one transfer output");
        check(total == 100000 + 100000 + kStartBalance + kStartBalance,
              "…and the whole stated supply is in them");
    }

    // The variable-cap asset's two minter groups became two mint outputs, with the
    // thresholds the request named.
    const auto& mvca = g->assets[1].create;
    check(mvca != nullptr && mvca->states.size() == 1, "the second asset has one initial state");
    if (mvca != nullptr && mvca->states.size() == 1) {
        const auto& outs = mvca->states[0].outs;
        check(outs.size() == 2, "…holding its two minter groups");
        std::vector<std::uint32_t> thresholds;
        for (const auto& o : outs) {
            const auto* m = dynamic_cast<const fx::secp256k1fx::MintOutput*>(o.get());
            if (m != nullptr) thresholds.push_back(m->out_owners.threshold);
        }
        std::sort(thresholds.begin(), thresholds.end());
        check(thresholds == std::vector<std::uint32_t>({1, 2}),
              "…as mint outputs, at the thresholds asked for");
    }

    // An asset defined with a name and no symbol is still an asset: the genesis
    // says what was stated, and the chain refuses it later if the chain's rules
    // refuse it. (Go's asset3 has no symbol either.)
    check(g->assets[2].create != nullptr && g->assets[2].create->symbol.empty(),
          "the third asset carries the empty symbol it was given");

    // Round trip: the buffer parses back to the same three assets, and re-emits
    // the very same bytes.
    auto parsed = genesis::parse(view(*b));
    check(parsed && parsed->assets.size() == 3, "the buffer parses back to three assets");
    if (parsed) {
        auto again = genesis::bytes(*parsed);
        check(again && *again == *b, "…and re-serializes to the same bytes");
    }

    // Order is by alias, not by insertion: a genesis whose bytes depended on the
    // order the definitions were handed over would give two nodes two different
    // fee assets.
    std::map<std::string, genesis::AssetDefinition> reordered;
    reordered["asset3"] = defs["asset3"];
    reordered["asset1"] = defs["asset1"];
    reordered["asset2"] = defs["asset2"];
    auto g2 = genesis::from_definitions(12345, reordered);
    check(g2.has_value(), "the same three definitions, given in another order, build");
    if (g2) {
        auto b2 = genesis::bytes(*g2);
        check(b2 && *b2 == *b, "…to byte-identical genesis bytes");
    }
}

}  // namespace

int main() {
    std::printf("xvm — the genesis buffer, against the Go X-Chain's own bytes\n");
    bytes_match_go();
    go_bytes_parse_here();
    refusals();
    a_genesis_built_from_a_description();
    the_asset_id_follows_the_bytes();
    the_three_asset_genesis();
    vm_boots_from_genesis();
    return report("genesis");
}
