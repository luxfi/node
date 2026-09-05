// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// manifest_test — load, validate, admit, and every way that must fail,
// ported from chains/dexvm/registry/manifest_test.go.
//
// The path exercised here is the one CI uses: write a manifest, load it through
// the real loader, and validate it against a verifier — so the file, the shape
// check, the identity confirm, the admission and the boot gate are all in the
// same line of fire.

#include "lux/dexvm/manifest.hpp"

#include "check.hpp"
#include "fixtures.hpp"

#include <cstdio>
#include <string>
#include <unistd.h>

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

// A temp file that removes itself, so a failing test leaves no litter behind.
class TempFile {
public:
    explicit TempFile(const std::string& contents) {
        char tmpl[] = "/tmp/dexvm-manifest-XXXXXX";
        const int fd = ::mkstemp(tmpl);
        path_ = tmpl;
        if (fd >= 0) {
            const ssize_t n = ::write(fd, contents.data(), contents.size());
            (void)n;
            ::close(fd);
        }
    }
    ~TempFile() { ::unlink(path_.c_str()); }
    const std::string& path() const { return path_; }

private:
    std::string path_;
};

Manifest two_asset_manifest(const Id& c_chain, const Bytes& wlux, const Bytes& lusd) {
    Manifest m;
    m.network = "mainnet";
    m.network_id = kMainnetID;
    m.evm_chain_id = 96369;
    m.c_chain_id = c_chain;
    m.chain_labels[hex(c_chain)] = "Lux C-Chain";
    m.assets_is_null = false;
    for (const auto& [ref, sym, name] :
         {std::tuple{wlux, std::string("WLUX"), std::string("Wrapped LUX")},
          std::tuple{lusd, std::string("LUSD"), std::string("Lux Dollar")}}) {
        Asset a;
        a.network_id = kMainnetID;
        a.chain_id = c_chain;
        a.kind = AssetKind::ERC20;
        a.canonical_ref = ref;
        a.decimals = 18;
        a.symbol = sym;
        a.name = name;
        a.enabled = true;
        a.risk_tier = RiskTier::Tier0;
        m.assets.push_back(std::move(a));
    }
    return m;
}

void load_and_validate_real_entries() {
    std::printf("a real manifest loads, validates and admits\n");
    const Id c_chain = test_id(100);
    const Bytes wlux = addr20(0x4a);
    const Bytes lusd = addr20(0x84);

    FakeChain fc;
    fc.seed_erc20(kMainnetID, c_chain, view(wlux), 18);
    fc.seed_erc20(kMainnetID, c_chain, view(lusd), 18);
    fc.confirm(kMainnetID, 96369, c_chain);

    Manifest m = two_asset_manifest(c_chain, wlux, lusd);
    auto wlux_id = derive_asset_id(kMainnetID, c_chain, AssetKind::ERC20, view(wlux));
    auto lusd_id = derive_asset_id(kMainnetID, c_chain, AssetKind::ERC20, view(lusd));
    const std::string venue = "tick=1;lot=1;fee=30";
    Market mk;
    mk.network_id = kMainnetID;
    mk.base_asset_id = *wlux_id;
    mk.quote_asset_id = *lusd_id;
    mk.venue_config = Bytes(venue.begin(), venue.end());
    mk.enabled = true;
    m.markets.push_back(mk);
    m.markets_is_null = false;

    TempFile file(m.encode());
    auto loaded = load_manifest(file.path());
    admitted(loaded, "the manifest loads from disk");
    if (!loaded) return;

    auto reg = loaded->validate(fc);
    admitted(reg, "and validates against the chain");
    if (!reg) return;
    check((*reg)->len() == 2, "two assets admitted");
    check((*reg)->resolve(*wlux_id).has_value(), "WLUX resolves by its derived id");
    check((*reg)->resolve_market(mk.id()).has_value(), "and the market by its derived id");
}

void rejects_the_wrong_chain() {
    std::printf("a manifest cannot be validated against the wrong chain\n");
    const Id c_chain = test_id(110);
    const Bytes wlux = addr20(0x4a);
    FakeChain fc;
    fc.seed_erc20(kMainnetID, c_chain, view(wlux), 18);
    // The verifier confirms a DIFFERENT C-Chain, so the identity confirm fails
    // before a single asset is looked at.
    fc.confirm(kMainnetID, 96369, test_id(111));

    Manifest m = two_asset_manifest(c_chain, wlux, addr20(0x84));
    m.assets.pop_back();
    refused_any(m.validate(fc), "the C-Chain identity confirm refuses it");

    // An ERC-20 rooted off the manifest's own C-Chain is a shape error: the
    // manifest cannot point an "ERC20" at a chain that is not the C-Chain.
    Manifest bad = m;
    bad.assets[0].chain_id = test_id(112);
    refused_any(bad.validate_shape(), "an ERC-20 rooted off the C-Chain");

    // And the other shape rules.
    Manifest empty_name = m;
    empty_name.network.clear();
    refused_any(empty_name.validate_shape(), "a manifest with no network name");

    Manifest zero_net = m;
    zero_net.network_id = 0;
    refused_any(zero_net.validate_shape(), "…with a zero networkID");

    Manifest zero_evm = m;
    zero_evm.evm_chain_id = 0;
    refused_any(zero_evm.validate_shape(), "…with no EVM chainID");

    Manifest no_cchain = m;
    no_cchain.c_chain_id = kEmptyId;
    refused_any(no_cchain.validate_shape(), "…and with no C-Chain id");

    Manifest wrong_asset_net = m;
    wrong_asset_net.assets[0].network_id = 2;
    refused_any(wrong_asset_net.validate_shape(), "an asset on another network");

    Manifest wrong_market_net = m;
    wrong_market_net.markets.push_back(Market{2, test_id(1), test_id(2), {}, true});
    wrong_market_net.markets_is_null = false;
    refused_any(wrong_market_net.validate_shape(), "and a market on another network");
}

void a_forbidden_label_travels_through_the_file() {
    std::printf("a forbidden universe label travels file → label lookup → deny-scan\n");
    const Id c_chain = test_id(120);
    const Bytes wlux = addr20(0x4a);
    FakeChain fc;
    fc.seed_erc20(kMainnetID, c_chain, view(wlux), 18);
    fc.confirm(kMainnetID, 96369, c_chain);

    Manifest m = two_asset_manifest(c_chain, wlux, addr20(0x84));
    m.assets.pop_back();
    m.chain_labels[hex(c_chain)] = "Liquidity primary network";

    TempFile file(m.encode());
    auto loaded = load_manifest(file.path());
    admitted(loaded, "the manifest loads (the token really is on-chain)");
    if (loaded) refused_any(loaded->validate(fc), "but the label refuses it at the gate");
}

void a_missing_file_is_an_error_not_an_empty_manifest() {
    std::printf("a manifest that is not there\n");
    refused_any(load_manifest("/tmp/dexvm-does-not-exist-38f1a2"),
                "a missing file is an error, not an empty registry");
}

void admit_into_leaves_the_gate_to_its_owner() {
    std::printf("admission and the gate are separable, so the node runs the gate once\n");
    const Id c_chain = test_id(130);
    const Bytes addr = addr20(0x55);
    FakeChain fc;
    fc.seed_erc20(kMainnetID, c_chain, view(addr), 6);

    Manifest m;
    m.network = "mainnet";
    m.network_id = kMainnetID;
    m.evm_chain_id = 96369;
    m.c_chain_id = c_chain;
    m.assets_is_null = false;
    Asset a{kMainnetID, c_chain, AssetKind::ERC20, addr, 6, "MOCKUSD", "mock liquidity", true,
            RiskTier::Tier0};
    m.assets.push_back(a);

    Registry reg({AssetKind::ERC20});
    admitted(m.admit_into(reg, fc), "admit_into admits a real token whatever it is called");
    check(reg.len() == 1, "…and it is in the registry");

    Registry reg2({AssetKind::ERC20});
    refused_any(m.apply_to(reg2, fc, default_dex_asset_policy()),
                "apply_to runs the gate too, and the gate refuses the mock label");
}

}  // namespace

int main() {
    load_and_validate_real_entries();
    rejects_the_wrong_chain();
    a_forbidden_label_travels_through_the_file();
    a_missing_file_is_an_error_not_an_empty_manifest();
    admit_into_leaves_the_gate_to_its_owner();
    return report("manifest");
}
