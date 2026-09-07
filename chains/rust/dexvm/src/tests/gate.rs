// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The consensus-mode value guard, and the fail-closed startup gate's edges.

use super::{addr20, tid, FakeChain, MAINNET_ID};
use crate::asset::{market_id, parse_asset_kind, AssetKind};
use crate::consensus_mode::{
    guard_value_activation, parse_consensus_mode, ConsensusMode, LaunchAssertions,
    NO_BYZANTINE_FINALITY_CLAIM,
};
use crate::error::Code;
use crate::gate::{refuse_under_synthetic_config, DexAssetPolicy, NetworkClass};
use crate::registry::{Asset, Registry, RISK_TIER1};

/// The T-suite's `realERC20`, again — 6 decimals, seeded, enabled.
fn real_erc20(fc: &mut FakeChain, c_chain: crate::ids::Id, addr: &[u8], sym: &str) -> Asset {
    fc.seed_erc20(MAINNET_ID, c_chain, addr, 6);
    Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr.into(),
        decimals: 6,
        symbol: sym.to_string(),
        name: format!("{sym} token"),
        enabled: true,
        risk_tier: RISK_TIER1,
    }
}

// --- the consensus-mode value guard -----------------------------------------

#[test]
fn quorum_finality_allows_value() {
    let st = guard_value_activation(
        true,
        ConsensusMode::QuorumFinality,
        LaunchAssertions::default(),
    )
    .expect("QUORUM_FINALITY must permit value activation");
    assert_eq!(st.mode, ConsensusMode::QuorumFinality);
    assert_eq!(
        st.status, "",
        "QUORUM_FINALITY must not surface a disclaimer (genuine BFT)"
    );
}

#[test]
fn honest_validator_labeled_requires_the_bundle_and_the_disclaimer() {
    // Missing any leg of the bundle => refuse.
    for a in [
        LaunchAssertions {
            caps_on: false,
            real_assets_only: true,
            halt_ready: true,
        },
        LaunchAssertions {
            caps_on: true,
            real_assets_only: false,
            halt_ready: true,
        },
        LaunchAssertions {
            caps_on: true,
            real_assets_only: true,
            halt_ready: false,
        },
    ] {
        let e = guard_value_activation(true, ConsensusMode::HonestValidatorLabeled, a).unwrap_err();
        assert!(
            e.is(Code::LaunchAssertionsUnmet),
            "must refuse without the full bundle (a={a:?}), got: {e}"
        );
    }

    // The full bundle permits value AND surfaces the exact disclaimer.
    let st = guard_value_activation(
        true,
        ConsensusMode::HonestValidatorLabeled,
        LaunchAssertions {
            caps_on: true,
            real_assets_only: true,
            halt_ready: true,
        },
    )
    .expect("full bundle must permit value");
    assert_eq!(st.status, NO_BYZANTINE_FINALITY_CLAIM);
}

#[test]
fn unset_and_unknown_modes_are_refused_never_a_silent_third_state() {
    // UNSET with value requested => refuse.
    let e = guard_value_activation(true, ConsensusMode::Unset, LaunchAssertions::default())
        .unwrap_err();
    assert!(e.is(Code::ValueModeUnset), "got: {e}");

    // Any out-of-enum mode => refuse. 99 is not a legal mode; it must never
    // silently authorise value, even carrying a full bundle.
    let e = guard_value_activation(
        true,
        ConsensusMode::Other(99),
        LaunchAssertions {
            caps_on: true,
            real_assets_only: true,
            halt_ready: true,
        },
    )
    .unwrap_err();
    assert!(e.is(Code::ValueModeIllegal), "got: {e}");

    // Value DISABLED => no error regardless of mode; nothing to authorise.
    guard_value_activation(false, ConsensusMode::Unset, LaunchAssertions::default())
        .expect("value-disabled must not error");
}

#[test]
fn parse_consensus_mode_rejects_unknown() {
    assert!(
        parse_consensus_mode("PARTIAL_FINALITY").is_err(),
        "unknown consensus mode token must be rejected"
    );
    for (tok, want) in [
        ("QUORUM_FINALITY", ConsensusMode::QuorumFinality),
        (
            "HONEST_VALIDATOR_LABELED",
            ConsensusMode::HonestValidatorLabeled,
        ),
        ("", ConsensusMode::Unset),
    ] {
        assert_eq!(parse_consensus_mode(tok).expect(tok), want, "{tok}");
    }
}

// --- the fail-closed startup gate -------------------------------------------

#[test]
fn the_gate_refuses_synthetic_flags_on_value_nets() {
    let reg = Registry::new(&[AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]);

    let synth_policies = [
        DexAssetPolicy {
            allow_synthetic_assets: true,
            ..DexAssetPolicy::default()
        },
        DexAssetPolicy {
            allow_synthetic_markets: true,
            ..DexAssetPolicy::default()
        },
        DexAssetPolicy {
            allow_mock_liquidity: true,
            ..DexAssetPolicy::default()
        },
    ];
    for class in [NetworkClass::Mainnet, NetworkClass::Testnet] {
        for p in &synth_policies {
            let e = refuse_under_synthetic_config(class, p, &reg, None).unwrap_err();
            assert!(
                e.is(Code::SyntheticOnValueNet),
                "class={class} policy={p:?} must refuse, got: {e}"
            );
        }
    }

    // A dev network MAY set synthetic flags — developer opt-in.
    refuse_under_synthetic_config(
        NetworkClass::Dev,
        &DexAssetPolicy {
            allow_synthetic_assets: true,
            ..DexAssetPolicy::default()
        },
        &reg,
        None,
    )
    .expect("dev network should permit synthetic flags");
}

#[test]
fn the_gate_refuses_a_forbidden_liquidity_universe() {
    let mut fc = FakeChain::new();
    let c_chain = tid("gate/liquidity-c-chain");
    let reg = Registry::new(&[AssetKind::Erc20]);

    // A REAL asset — but its source chain carries a white-label universe label.
    let a = real_erc20(&mut fc, c_chain, &addr20(0x33), "USDC");
    reg.register(&a, &fc).expect("register");

    let label_liquidity = |id: crate::ids::Id| {
        if id == c_chain {
            "Liquidity L1 universe".to_string()
        } else {
            String::new()
        }
    };
    assert!(
        refuse_under_synthetic_config(
            NetworkClass::Mainnet,
            &DexAssetPolicy::default_policy(),
            &reg,
            Some(&label_liquidity),
        )
        .is_err(),
        "must refuse an asset on a white-label universe chain"
    );

    // With a clean label the same registry starts fine — so the refusal above
    // is the LABEL, not the asset.
    let clean = |_: crate::ids::Id| "Lux C-Chain".to_string();
    refuse_under_synthetic_config(
        NetworkClass::Mainnet,
        &DexAssetPolicy::default_policy(),
        &reg,
        Some(&clean),
    )
    .expect("clean-label registry should start");
}

#[test]
fn the_gate_refuses_a_bad_allowed_kind() {
    let reg = Registry::new(&[AssetKind::Erc20]);
    // An allowed-kinds list that smuggles the invalid kind is refused: the list
    // may only ever be a subset of the three real kinds.
    let p = DexAssetPolicy {
        allowed_asset_kinds: vec![AssetKind::Erc20, AssetKind::Invalid],
        ..DexAssetPolicy::default()
    };
    let e = refuse_under_synthetic_config(NetworkClass::Mainnet, &p, &reg, None).unwrap_err();
    assert!(e.is(Code::BadAllowedKind), "got: {e}");
}

#[test]
fn registration_refuses_a_decimals_mismatch_and_the_gate_refuses_a_forbidden_symbol() {
    let mut fc = FakeChain::new();
    let c_chain = tid("gate/mismatch-c-chain");
    let reg = Registry::new(&[AssetKind::Erc20]);

    // Seeded with 6 decimals on-chain but declared 18 => refused.
    let addr = addr20(0x44);
    fc.seed_erc20(MAINNET_ID, c_chain, &addr, 6);
    let mismatch = Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr.as_slice().into(),
        decimals: 18,
        symbol: "USDC".to_string(),
        enabled: true,
        ..Asset::default()
    };
    assert!(
        reg.register(&mismatch, &fc).is_err(),
        "declared decimals != on-chain decimals must be refused"
    );

    // A mock/synthetic name is caught by the deny-scan AT THE GATE even though
    // the token itself is real on-chain.
    let addr2 = addr20(0x55);
    fc.seed_erc20(MAINNET_ID, c_chain, &addr2, 6);
    let mock = Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr2.as_slice().into(),
        decimals: 6,
        symbol: "MOCKUSD".to_string(),
        name: "mock liquidity token".to_string(),
        enabled: true,
        ..Asset::default()
    };
    reg.register(&mock, &fc)
        .expect("a real token registers; the symbol scan is at the gate");
    assert!(
        refuse_under_synthetic_config(
            NetworkClass::Mainnet,
            &DexAssetPolicy::default_policy(),
            &reg,
            None
        )
        .is_err(),
        "the gate must refuse an asset whose name/symbol names mock liquidity"
    );
}

#[test]
fn asset_kind_round_trips_as_json_and_a_ticker_is_not_a_kind() {
    for k in [AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo] {
        let txt = serde_json::to_string(&k).expect("marshal");
        let back: AssetKind = serde_json::from_str(&txt).expect("unmarshal");
        assert_eq!(back, k, "round-trip {k}");
    }
    // The invalid kind refuses to marshal — fail-closed.
    assert!(
        serde_json::to_string(&AssetKind::Invalid).is_err(),
        "invalid kind must refuse to marshal"
    );
    // An ASCII ticker is not a kind.
    assert!(
        serde_json::from_str::<AssetKind>("\"LUX\"").is_err(),
        "ASCII ticker must not parse as an asset kind"
    );
    assert!(parse_asset_kind("LUX").is_err());
}

#[test]
fn a_market_id_is_bound_to_its_assets_and_its_venue() {
    let base = tid("gate/market-base");
    let quote = tid("gate/market-quote");
    let venue_a = b"tick=1,lot=1,fee=30";
    let venue_b = b"tick=1,lot=1,fee=5";

    let id1 = market_id(MAINNET_ID, base, quote, venue_a);
    let id2 = market_id(MAINNET_ID, base, quote, venue_b);
    assert_ne!(id1, id2, "different venue configs must yield different ids");

    // Swapping base and quote is a different market: direction is part of the id.
    assert_ne!(
        market_id(MAINNET_ID, base, quote, venue_a),
        market_id(MAINNET_ID, quote, base, venue_a),
        "base/quote order must affect the market id"
    );

    assert_eq!(market_id(MAINNET_ID, base, quote, venue_a), id1);
}
