// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A manifest loaded from a file, validated against a chain, and refused when
//! it names the wrong chain, carries an unknown field, or labels its source
//! chain a forbidden universe.

use super::{addr20, tid, FakeChain, MAINNET_ID};
use crate::asset::{derive_asset_id, AssetKind};
use crate::ids::{self, Id};
use crate::manifest::{load_manifest, Manifest};
use crate::market::Market;
use crate::registry::{Asset, RISK_TIER0};
use std::collections::BTreeMap;

/// Write a manifest to a fresh temp file and return its path. The suite writes
/// its own fixtures, so nothing here can be perturbed by another test.
fn write_manifest(m: &Manifest, tag: &str) -> std::path::PathBuf {
    let dir = std::env::temp_dir().join(format!("lux-dexvm-test-{}-{tag}", std::process::id()));
    std::fs::create_dir_all(&dir).expect("temp dir");
    let path = dir.join(format!("{}.json", m.network));
    let b = serde_json::to_vec_pretty(m).expect("marshal manifest");
    std::fs::write(&path, b).expect("write manifest");
    path
}

fn erc20(network_id: u32, chain: Id, addr: &[u8], sym: &str, name: &str, dec: u8) -> Asset {
    Asset {
        network_id,
        chain_id: chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr.into(),
        decimals: dec,
        symbol: sym.to_string(),
        name: name.to_string(),
        enabled: true,
        risk_tier: RISK_TIER0,
    }
}

#[test]
fn a_manifest_loads_and_validates_its_real_entries() {
    let c_chain = tid("manifest/ok/c-chain");
    let wlux = addr20(0x4a);
    let lusd = addr20(0x84);

    let mut fc = FakeChain::new();
    fc.seed_erc20(MAINNET_ID, c_chain, &wlux, 18);
    fc.seed_erc20(MAINNET_ID, c_chain, &lusd, 18);
    let v = fc.confirming(MAINNET_ID, 96369, c_chain);

    let wlux_id = derive_asset_id(MAINNET_ID, c_chain, AssetKind::Erc20, &wlux).unwrap();
    let lusd_id = derive_asset_id(MAINNET_ID, c_chain, AssetKind::Erc20, &lusd).unwrap();

    let mut labels = BTreeMap::new();
    labels.insert(ids::hex(&c_chain), "Lux C-Chain".to_string());

    let m = Manifest {
        network: "mainnet".to_string(),
        network_id: MAINNET_ID,
        evm_chain_id: 96369,
        c_chain_id: c_chain,
        chain_labels: Some(labels),
        assets: vec![
            erc20(MAINNET_ID, c_chain, &wlux, "WLUX", "Wrapped LUX", 18),
            erc20(MAINNET_ID, c_chain, &lusd, "LUSD", "Lux Dollar", 18),
        ],
        markets: vec![Market {
            network_id: MAINNET_ID,
            base_asset_id: wlux_id,
            quote_asset_id: lusd_id,
            venue_config: b"tick=1;lot=1;fee=30".as_slice().into(),
            enabled: true,
        }],
    };

    // Through the real load path — the same one CI uses.
    let path = write_manifest(&m, "ok");
    let loaded = load_manifest(&path).expect("load manifest");
    let reg = loaded
        .validate(&v)
        .expect("validate manifest against the chain");

    assert_eq!(reg.len(), 2, "expected 2 admitted assets");
    assert!(reg.resolve(&wlux_id).is_some(), "WLUX not admitted");
    let market_id = Market {
        network_id: MAINNET_ID,
        base_asset_id: wlux_id,
        quote_asset_id: lusd_id,
        venue_config: b"tick=1;lot=1;fee=30".as_slice().into(),
        enabled: false,
    }
    .id();
    assert!(
        reg.resolve_market(&market_id).is_some(),
        "WLUX/LUSD market not admitted"
    );
}

#[test]
fn a_manifest_naming_the_wrong_chain_or_an_unknown_field_is_refused() {
    let c_chain = tid("manifest/wrong/c-chain");
    let wlux = addr20(0x4a);
    let mut fc = FakeChain::new();
    fc.seed_erc20(MAINNET_ID, c_chain, &wlux, 18);

    // (a) A verifier bound to a DIFFERENT C-Chain refuses the identity confirm.
    let wrong = FakeChain::new().confirming(MAINNET_ID, 96369, tid("manifest/wrong/other-chain"));
    let m = Manifest {
        network: "mainnet".to_string(),
        network_id: MAINNET_ID,
        evm_chain_id: 96369,
        c_chain_id: c_chain,
        assets: vec![erc20(MAINNET_ID, c_chain, &wlux, "WLUX", "", 18)],
        ..Manifest::default()
    };
    assert!(
        m.validate(&wrong).is_err(),
        "a manifest validated against the wrong C-Chain must be refused"
    );

    // (b) An ERC-20 entry rooted off the manifest's C-Chain is a shape error.
    let bad = Manifest {
        network: "mainnet".to_string(),
        network_id: MAINNET_ID,
        evm_chain_id: 96369,
        c_chain_id: c_chain,
        assets: vec![erc20(
            MAINNET_ID,
            tid("manifest/wrong/rogue-chain"),
            &wlux,
            "WLUX",
            "",
            18,
        )],
        ..Manifest::default()
    };
    assert!(
        bad.validate_shape().is_err(),
        "an ERC-20 rooted off the C-Chain must be rejected by shape validation"
    );

    // (c) A manifest file with an unknown field fails closed at load.
    let dir = std::env::temp_dir().join(format!("lux-dexvm-test-{}-unknown", std::process::id()));
    std::fs::create_dir_all(&dir).expect("temp dir");
    let path = dir.join("bad.json");
    let body = format!(
        r#"{{"network":"mainnet","networkID":1,"evmChainID":96369,"cChainID":"{}","asssets":[]}}"#,
        ids::cb58(&c_chain)
    );
    std::fs::write(&path, body).expect("write");
    assert!(
        load_manifest(&path).is_err(),
        "a manifest with an unknown field must fail to load"
    );
}

#[test]
fn a_forbidden_liquidity_label_refuses_through_the_loaded_file() {
    let c_chain = tid("manifest/label/c-chain");
    let wlux = addr20(0x4a);
    let mut fc = FakeChain::new();
    fc.seed_erc20(MAINNET_ID, c_chain, &wlux, 18);
    let v = fc.confirming(MAINNET_ID, 96369, c_chain);

    // The token is real on-chain, but the file labels its source chain a
    // white-label universe. The label travels file -> chain_label_for ->
    // deny-scan, and the manifest is refused.
    let mut labels = BTreeMap::new();
    labels.insert(ids::hex(&c_chain), "Liquidity primary network".to_string());
    let m = Manifest {
        network: "mainnet".to_string(),
        network_id: MAINNET_ID,
        evm_chain_id: 96369,
        c_chain_id: c_chain,
        chain_labels: Some(labels),
        assets: vec![erc20(MAINNET_ID, c_chain, &wlux, "WLUX", "", 18)],
        ..Manifest::default()
    };
    let path = write_manifest(&m, "label");
    let loaded = load_manifest(&path).expect("load");
    assert!(
        loaded.validate(&v).is_err(),
        "a manifest with a white-label universe label must be refused"
    );
}
