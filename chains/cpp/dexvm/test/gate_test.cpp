// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gate_test — the two guards, ported from chains/dexvm/registry/gate_test.go.
//
//   the CONSENSUS-MODE guard: under which posture value may activate, and what
//   an activation must then say out loud
//   the BOOT gate: every way a configuration or a registry can fail closed
//
// The consensus guard's decisive case is the out-of-enum mode: a closed enum
// whose default arm refuses is the difference between "two legal modes" and
// "two legal modes plus whatever an integer happens to hold".

#include "lux/dexvm/consensus_mode.hpp"
#include "lux/dexvm/forbidden.hpp"
#include "lux/dexvm/gate.hpp"

#include "check.hpp"
#include "fixtures.hpp"

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

void quorum_finality_allows_value() {
    std::printf("quorum finality carries value, and disclaims nothing\n");
    auto st = guard_value_activation(true, ConsensusMode::QuorumFinality, {});
    admitted(st, "value activates under QUORUM_FINALITY");
    if (st) {
        check(st->mode == ConsensusMode::QuorumFinality, "and reports the mode that authorised it");
        check_eq(st->status, "", "with no disclaimer, because the finality is genuine");
    }
}

void labeled_cft_needs_the_bundle_and_says_so() {
    std::printf("labeled CFT parity carries value only with the bundle, and must say so\n");
    const LaunchAssertions missing[] = {
        {false, true, true},
        {true, false, true},
        {true, true, false},
    };
    const char* which[] = {"without caps-on", "without real-assets-only", "without halt-ready"};
    for (int i = 0; i < 3; ++i) {
        refused(guard_value_activation(true, ConsensusMode::HonestValidatorLabeled, missing[i]),
                Err::LaunchAssertionsUnmet, std::string("refused ") + which[i]);
    }

    auto st = guard_value_activation(true, ConsensusMode::HonestValidatorLabeled,
                                     LaunchAssertions{true, true, true});
    admitted(st, "the full bundle activates value");
    if (st)
        check_eq(st->status, std::string(kNoByzantineFinalityClaim),
                 "and surfaces the exact no-Byzantine-finality claim");
}

void never_a_silent_third_state() {
    std::printf("there is never a silent third state\n");
    refused(guard_value_activation(true, ConsensusMode::Unset, {}), Err::ValueModeUnset,
            "UNSET with value requested");
    refused(guard_value_activation(true, ConsensusMode(99), LaunchAssertions{true, true, true}),
            Err::ValueModeIllegal, "an out-of-enum mode, bundle and all");
    admitted(guard_value_activation(false, ConsensusMode::Unset, {}),
             "value disabled authorises nothing and is not an error");
}

void mode_tokens() {
    std::printf("the mode tokens\n");
    refused_any(parse_consensus_mode("PARTIAL_FINALITY"), "an unknown token is not a mode");
    struct Case {
        const char* token;
        ConsensusMode want;
    };
    for (const Case& c : {Case{"QUORUM_FINALITY", ConsensusMode::QuorumFinality},
                          Case{"HONEST_VALIDATOR_LABELED", ConsensusMode::HonestValidatorLabeled},
                          Case{"", ConsensusMode::Unset},
                          Case{"UNSET", ConsensusMode::Unset}}) {
        auto got = parse_consensus_mode(c.token);
        check(got.has_value() && *got == c.want,
              std::string("\"") + c.token + "\" parses to " + std::string(to_string(c.want)));
    }
}

void synthetic_flags_on_value_nets() {
    std::printf("no synthetic flag on a value-bearing network\n");
    Registry reg({AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO});

    DexAssetPolicy synth_assets;
    synth_assets.allow_synthetic_assets = true;
    DexAssetPolicy synth_markets;
    synth_markets.allow_synthetic_markets = true;
    DexAssetPolicy mock_liq;
    mock_liq.allow_mock_liquidity = true;

    for (NetworkClass cls : {NetworkClass::Mainnet, NetworkClass::Testnet}) {
        for (const DexAssetPolicy& p : {synth_assets, synth_markets, mock_liq}) {
            refused(refuse_under_synthetic_config(cls, p, reg, nullptr), Err::SyntheticOnValueNet,
                    std::string("refused on ") + std::string(to_string(cls)));
        }
    }
    admitted(refuse_under_synthetic_config(NetworkClass::Dev, synth_assets, reg, nullptr),
             "a dev network may opt in");

    // The class mapping is convention-fixed, and every sovereign id is dev-class
    // for the purpose of these flags.
    check(network_class_for(1) == NetworkClass::Mainnet, "networkID 1 is mainnet");
    check(network_class_for(2) == NetworkClass::Testnet, "networkID 2 is testnet");
    check(network_class_for(3) == NetworkClass::Dev, "networkID 3 is dev");
    check(network_class_for(1337) == NetworkClass::Dev, "networkID 1337 is dev");
    check(network_class_for(8675309) == NetworkClass::Dev, "a sovereign L1 id is dev");
}

void forbidden_universe_is_refused_by_name() {
    std::printf("a forbidden off-network universe is refused by name, real token or not\n");
    FakeChain fc;
    const Id c_chain = test_id(70);
    Registry reg({AssetKind::ERC20});
    admitted(reg.register_asset(real_erc20(fc, c_chain, view(addr20(0x33)), "USDC"), fc),
             "the token is genuinely on-chain");

    auto branded = [&](const Id& id) { return id == c_chain ? "Liquidity L1 universe" : ""; };
    refused_any(refuse_under_synthetic_config(NetworkClass::Mainnet, default_dex_asset_policy(),
                                              reg, branded),
                "and is STILL refused, because reality does not police provenance");

    auto clean = [](const Id&) { return "Lux C-Chain"; };
    admitted(refuse_under_synthetic_config(NetworkClass::Mainnet, default_dex_asset_policy(), reg,
                                           clean),
             "the same registry under a clean label starts");

    check(is_forbidden_universe_label("Liquid EVM"), "\"Liquid EVM\" is a forbidden universe");
    check(is_forbidden_universe_label("liquidity primary network"), "…and so is its lowercase");
    check(!is_forbidden_universe_label("Lux C-Chain (mainnet)"), "a Lux label is not");
    check(!is_forbidden_universe_label(""), "and an unlabeled chain has no brand to match");
}

void mock_liquidity_and_ticker_ids() {
    std::printf("mock liquidity and ticker-shaped ids\n");
    FakeChain fc;
    const Id c_chain = test_id(80);
    Registry reg({AssetKind::ERC20});

    const Bytes addr = addr20(0x55);
    fc.seed_erc20(kMainnetID, c_chain, view(addr), 6);
    Asset mock{kMainnetID, c_chain, AssetKind::ERC20, addr, 6, "MOCKUSD", "mock liquidity token",
               true, RiskTier::Tier0};
    admitted(reg.register_asset(mock, fc),
             "the token is real, so registration (which proves reality) admits it");
    refused_any(refuse_under_synthetic_config(NetworkClass::Mainnet, default_dex_asset_policy(),
                                             reg, nullptr),
                "and the gate's deny-scan is what catches the mock label");

    check(is_mock_liquidity_ref("MOCKUSD"), "\"MOCKUSD\" names mock liquidity");
    check(is_mock_liquidity_ref("D-Native credit"), "so does the forbidden D-native class");
    check(is_mock_liquidity_ref("dnative"), "…spelled either way");
    check(!is_mock_liquidity_ref("USD Coin"), "a real name does not");

    // A symbol shaped like a pair id is an identity masquerading as a label, and
    // is refused at the shape check — before any chain lookup.
    Asset pair_id{kMainnetID, c_chain, AssetKind::ERC20, addr20(0x56), 6, "LUX/USDC", "", true,
                  RiskTier::Tier0};
    refused_any(pair_id.validate_shape(), "a pair-shaped symbol is an id, not a label");
    check(looks_like_ascii_ticker_id("LUX-USDC@VENUE"), "…and so is a venue-qualified pair");
    check(looks_like_ascii_ticker_id("A-B"), "the separator plus all-caps is the whole rule");
    check(!looks_like_ascii_ticker_id("LUX"), "but a plain ticker MAY be a symbol");
    check(!looks_like_ascii_ticker_id("USD Coin"), "and so may a human name");
    // The reference's own doc comment offers "LUX-USDC@venue" as an example of
    // what this catches, and its code does NOT catch it: one lowercase letter
    // takes the string out of the ticker alphabet. Verified against Go, which
    // answers false for exactly this string. The COMMENT is what is wrong there,
    // so the behaviour is ported as written rather than as described — a port
    // that "fixed" it would be the divergence.
    check(!looks_like_ascii_ticker_id("LUX-USDC@venue"),
          "a lowercase tail leaves the ticker alphabet, as in Go");
}

void allowed_kinds_must_be_real() {
    std::printf("the allowed-kinds list may only ever be a subset of the three real kinds\n");
    Registry reg({AssetKind::ERC20});
    DexAssetPolicy smuggled;
    smuggled.allowed_asset_kinds = {AssetKind::ERC20, AssetKind::Invalid};
    refused(refuse_under_synthetic_config(NetworkClass::Mainnet, smuggled, reg, nullptr),
            Err::BadAllowedKind, "a non-real kind in the policy");

    DexAssetPolicy unset;
    check(unset.kinds().size() == 3, "an unset list means the canonical three");
    check(!unset.any_synthetic_flag(), "and a zero-valued policy is the locked-down one");
    const DexAssetPolicy dflt = default_dex_asset_policy();
    check(dflt.kinds().size() == 3 && !dflt.any_synthetic_flag(),
          "which is what the default policy states explicitly");
}

void kind_text_round_trip() {
    std::printf("an asset kind travels as its token, never as an integer\n");
    for (AssetKind k : {AssetKind::EVMNative, AssetKind::ERC20, AssetKind::UTXO}) {
        auto txt = marshal_kind(k);
        admitted(txt, std::string("marshals ") + std::string(to_string(k)));
        if (txt) {
            auto back = parse_kind(*txt);
            check(back.has_value() && *back == k, "…and parses back to the same kind");
        }
    }
    refused_any(marshal_kind(AssetKind::Invalid), "the invalid kind refuses to marshal");
    refused_any(parse_kind("LUX"), "an ASCII ticker is not a kind");
    refused_any(parse_kind("erc20"), "and the token is case-sensitive");
    auto padded = parse_kind("  ERC20\n");
    check(padded.has_value() && *padded == AssetKind::ERC20, "surrounding whitespace is trimmed");
}

}  // namespace

int main() {
    quorum_finality_allows_value();
    labeled_cft_needs_the_bundle_and_says_so();
    never_a_silent_third_state();
    mode_tokens();
    synthetic_flags_on_value_nets();
    forbidden_universe_is_refused_by_name();
    mock_liquidity_and_ticker_ids();
    allowed_kinds_must_be_real();
    kind_text_round_trip();
    return report("gate");
}
