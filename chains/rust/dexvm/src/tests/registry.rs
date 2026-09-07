// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! T1–T4 of the reference suite: no synthetic asset registers, no synthetic
//! market starts, and each of the two identity derivations is bound to the real
//! on-chain reference it names.

use super::{addr20, tid, FakeChain, MAINNET_ID};
use crate::asset::{derive_asset_id, AssetKind};
use crate::error::Code;
use crate::ids::Id;
use crate::market::Market;
use crate::registry::{Asset, Registry, RISK_TIER1, RISK_TIER2};

/// A registered+verifiable ERC-20, seeded on the fake chain so registration
/// succeeds. Returns the asset and its derived id.
fn real_erc20(fc: &mut FakeChain, c_chain: Id, addr: &[u8], sym: &str) -> (Asset, Id) {
    fc.seed_erc20(MAINNET_ID, c_chain, addr, 6);
    let a = Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr.into(),
        decimals: 6,
        symbol: sym.to_string(),
        name: format!("{sym} token"),
        enabled: true,
        risk_tier: RISK_TIER1,
    };
    let id = a.id().expect("derive id");
    (a, id)
}

// --- (T1) -------------------------------------------------------------------
//
// A synthetic asset — one with no real on-chain object behind it — MUST be
// refused at registration.
#[test]
fn no_synthetic_asset_can_register() {
    let mut fc = FakeChain::new();
    let c_chain = tid("t1/c-chain");
    let reg = Registry::new(&[AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]);

    // (a) An ERC-20 that does not exist on-chain — never seeded — is refused.
    let ghost = Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr20(0x42).into(),
        decimals: 18,
        symbol: "GHOST".to_string(),
        name: "Ghost token".to_string(),
        enabled: true,
        risk_tier: RISK_TIER2,
    };
    assert!(
        reg.register(&ghost, &fc).is_err(),
        "synthetic ERC-20 (no on-chain code) was registered; must be refused"
    );

    // (b) A UTXO asset that does not exist on the source chain is refused.
    let src = tid("t1/x-chain");
    let phantom = Asset {
        network_id: MAINNET_ID,
        chain_id: src,
        kind: AssetKind::Utxo,
        canonical_ref: tid("t1/phantom-asset").as_slice().into(),
        decimals: 9,
        symbol: "PUTXO".to_string(),
        name: "Phantom UTXO".to_string(),
        enabled: true,
        risk_tier: RISK_TIER2,
    };
    assert!(
        reg.register(&phantom, &fc).is_err(),
        "synthetic UTXO (not on source chain) was registered; must be refused"
    );

    // (c) An asset whose declared kind is the invalid one — the closest a caller
    //     can get to a "D-native" synthetic class — is refused AS an invalid
    //     kind, never admitted.
    let d_native = Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Invalid, // there is no synthetic / D-native kind
        canonical_ref: addr20(0x01).into(),
        decimals: 18,
        symbol: "DNAT".to_string(),
        enabled: true,
        ..Asset::default()
    };
    let e = reg.register(&d_native, &fc).unwrap_err();
    assert!(e.is(Code::InvalidKind), "wrong error: {e}");

    // (d) A REAL ERC-20 registers fine — which proves the verifier is not
    //     refusing everything, so the refusals above are genuine.
    let (real, _) = real_erc20(&mut fc, c_chain, &addr20(0x99), "USDC");
    reg.register(&real, &fc).expect("real seeded ERC-20");
    assert_eq!(reg.len(), 1, "only the real asset should be registered");
}

// --- (T2) -------------------------------------------------------------------
//
// A market must fail creation unless BOTH sides resolve to a registered, real,
// enabled asset, and the startup gate must refuse to start a chain whose enabled
// market references a synthetic asset.
#[test]
fn no_synthetic_market_can_start() {
    use crate::gate::{refuse_under_synthetic_config, DexAssetPolicy, NetworkClass};

    let mut fc = FakeChain::new();
    let c_chain = tid("t2/c-chain");
    let reg = Registry::new(&[AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]);

    // One real asset: the quote. The base will be synthetic.
    let (usdc, usdc_id) = real_erc20(&mut fc, c_chain, &addr20(0x10), "USDC");
    reg.register(&usdc, &fc).expect("register quote");

    // An AssetID for a base that was NEVER registered.
    let synthetic_base_id =
        derive_asset_id(MAINNET_ID, c_chain, AssetKind::Erc20, &addr20(0x7e)).expect("derive");

    // (a) Creation is refused because the base side does not resolve.
    let e = reg
        .create_market(&Market {
            network_id: MAINNET_ID,
            base_asset_id: synthetic_base_id,
            quote_asset_id: usdc_id,
            enabled: true,
            ..Market::default()
        })
        .unwrap_err();
    assert!(e.is(Code::UnknownAsset), "got: {e}");

    // (b) A market over two REAL assets is created fine.
    let (lux, lux_id) = real_erc20(&mut fc, c_chain, &addr20(0x20), "WLUX");
    reg.register(&lux, &fc).expect("register base");
    reg.create_market(&Market {
        network_id: MAINNET_ID,
        base_asset_id: lux_id,
        quote_asset_id: usdc_id,
        enabled: true,
        ..Market::default()
    })
    .expect("real market should be created");

    // (c) The startup gate passes for the all-real registry.
    refuse_under_synthetic_config(
        NetworkClass::Mainnet,
        &DexAssetPolicy::default_policy(),
        &reg,
        None,
    )
    .expect("startup gate should pass for an all-real registry");

    // (d) Now smuggle a synthetic market straight into the registry's market
    //     map — a corrupted or forced config that bypassed the door — and assert
    //     the gate REFUSES to start. This is what proves the gate is a real
    //     second line of defence, not just an echo of the creation check.
    reg.force_market(
        tid("t2/smuggled-market"),
        Market {
            network_id: MAINNET_ID,
            base_asset_id: synthetic_base_id, // unregistered
            quote_asset_id: usdc_id,
            enabled: true,
            ..Market::default()
        },
    );
    let e = refuse_under_synthetic_config(
        NetworkClass::Mainnet,
        &DexAssetPolicy::default_policy(),
        &reg,
        None,
    )
    .unwrap_err();
    assert!(e.is(Code::EnabledMarketUnknownAsset), "got: {e}");
}

// --- (T3) -------------------------------------------------------------------
//
// The ERC-20 AssetID must derive from the REAL token contract address: change
// the address and the id changes; keep it and the id is stable regardless of
// display metadata.
#[test]
fn erc20_asset_id_uses_the_real_token_address() {
    let c_chain = tid("t3/c-chain");
    let addr_a = addr20(0x11);
    let addr_b = addr20(0x22);

    let id_a = derive_asset_id(MAINNET_ID, c_chain, AssetKind::Erc20, &addr_a).expect("derive A");
    let id_b = derive_asset_id(MAINNET_ID, c_chain, AssetKind::Erc20, &addr_b).expect("derive B");
    assert_ne!(
        id_a, id_b,
        "different token addresses produced the same AssetID — id is not bound to the address"
    );

    // Same address, different display metadata => SAME id. Identity is the
    // address, not the ticker: the anti-ASCII-ticker property.
    let display1 = Asset {
        network_id: MAINNET_ID,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr_a.as_slice().into(),
        decimals: 6,
        symbol: "USDC".to_string(),
        name: "USD Coin".to_string(),
        enabled: true,
        ..Asset::default()
    };
    let display2 = Asset {
        decimals: 18,
        symbol: "ZZZ".to_string(),
        name: "Renamed".to_string(),
        ..display1.clone()
    };
    let id1 = display1.id().expect("id1");
    let id2 = display2.id().expect("id2");
    assert_eq!(
        id1, id2,
        "AssetID changed when only display metadata changed"
    );
    assert_eq!(id1, id_a, "Asset::id disagrees with derive_asset_id");

    // The id is genuinely a function of the address bytes.
    assert_eq!(
        derive_asset_id(MAINNET_ID, c_chain, AssetKind::Erc20, &addr_a.clone()).unwrap(),
        id_a,
        "AssetID not reproducible from the same real address bytes"
    );

    // A different network or a different C-Chain also changes the id: the same
    // token address on two chains is two assets.
    let other_chain = tid("t3/other-c-chain");
    assert_ne!(
        derive_asset_id(MAINNET_ID, other_chain, AssetKind::Erc20, &addr_a).unwrap(),
        id_a,
        "chain must be in the preimage"
    );
    assert_ne!(
        derive_asset_id(2, c_chain, AssetKind::Erc20, &addr_a).unwrap(),
        id_a,
        "networkID must be in the preimage"
    );
}

// --- (T4) -------------------------------------------------------------------
//
// The UTXO AssetID must derive from the REAL source-chain assetID, and a UTXO
// asset and an ERC-20 sharing bytes must NOT collide.
#[test]
fn utxo_asset_id_uses_the_real_utxo_asset_id() {
    let src = tid("t4/x-chain");
    let real_asset_id = tid("t4/real-utxo");
    let other_asset_id = tid("t4/other-utxo");

    let id_real =
        derive_asset_id(MAINNET_ID, src, AssetKind::Utxo, &real_asset_id).expect("derive real");
    let id_other =
        derive_asset_id(MAINNET_ID, src, AssetKind::Utxo, &other_asset_id).expect("derive other");
    assert_ne!(
        id_real, id_other,
        "different UTXO assetIDs produced the same DEX AssetID"
    );

    // Reproducible from the real assetID bytes.
    assert_eq!(
        derive_asset_id(MAINNET_ID, src, AssetKind::Utxo, &real_asset_id).unwrap(),
        id_real
    );

    // Domain separation: an ERC-20 whose 20-byte address is the first 20 bytes
    // of the UTXO assetID must NOT collide with the UTXO asset.
    let clash_addr = &real_asset_id[..20];
    let id_erc20 =
        derive_asset_id(MAINNET_ID, src, AssetKind::Erc20, clash_addr).expect("derive clash");
    assert_ne!(
        id_erc20, id_real,
        "ERC-20 and UTXO with overlapping bytes collided; kind must domain-separate the preimage"
    );

    // Through the registry: a real seeded UTXO registers, and the id it is
    // stored under is the derivation from the real assetID.
    let mut fc = FakeChain::new();
    fc.seed_utxo(MAINNET_ID, src, real_asset_id, 9);
    let reg = Registry::new(&[AssetKind::Utxo]);
    let a = Asset {
        network_id: MAINNET_ID,
        chain_id: src,
        kind: AssetKind::Utxo,
        canonical_ref: real_asset_id.as_slice().into(),
        decimals: 9,
        symbol: "XAV".to_string(),
        name: "X-Chain asset".to_string(),
        enabled: true,
        risk_tier: RISK_TIER1,
    };
    let got_id = reg.register(&a, &fc).expect("real UTXO should register");
    assert_eq!(
        got_id, id_real,
        "registry stored a UTXO id that is not the derivation from the real source assetID"
    );
}
