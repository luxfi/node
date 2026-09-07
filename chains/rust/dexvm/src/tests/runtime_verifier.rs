// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The node-side identity bind at boot: a manifest built for another network,
//! or rooted at a chain this node does not run, is refused before any work.

use super::{addr20, tid};
use crate::asset::{AssetKind, EVM_NATIVE_MARKER};
use crate::gate::DexAssetPolicy;
use crate::ids::{self, Id};
use crate::manifest::Manifest;
use crate::registry::{Asset, Registry};
use crate::runtime_verifier::RuntimeVerifier;

/// A minimal manifest with one native, one ERC-20 and one UTXO asset.
fn native_manifest(
    network_id: u32,
    c_chain: Id,
    x_chain: Id,
    utxo_asset: Id,
    erc20: &[u8],
) -> Manifest {
    Manifest {
        network: "testnet".to_string(),
        network_id,
        evm_chain_id: 96368,
        c_chain_id: c_chain,
        chain_labels: None,
        assets: vec![
            Asset {
                network_id,
                chain_id: c_chain,
                kind: AssetKind::EvmNative,
                canonical_ref: EVM_NATIVE_MARKER.as_slice().into(),
                decimals: 18,
                symbol: "LUX".to_string(),
                name: "Lux".to_string(),
                enabled: true,
                ..Asset::default()
            },
            Asset {
                network_id,
                chain_id: c_chain,
                kind: AssetKind::Erc20,
                canonical_ref: erc20.into(),
                decimals: 6,
                symbol: "USDC".to_string(),
                name: "USD Coin".to_string(),
                enabled: true,
                ..Asset::default()
            },
            Asset {
                network_id,
                chain_id: x_chain,
                kind: AssetKind::Utxo,
                canonical_ref: utxo_asset.as_slice().into(),
                decimals: 9,
                symbol: "XAV".to_string(),
                name: "X asset".to_string(),
                enabled: true,
                ..Asset::default()
            },
        ],
        markets: Vec::new(),
    }
}

#[test]
fn the_verifier_binds_to_the_running_chain_and_admits_real_entries() {
    let c_chain = tid("rv/ok/c-chain");
    let x_chain = tid("rv/ok/x-chain");
    let utxo_asset = tid("rv/ok/utxo-asset");
    let erc20 = addr20(0x21);
    const NET: u32 = 2;

    let m = native_manifest(NET, c_chain, x_chain, utxo_asset, &erc20);
    m.validate_shape().expect("manifest shape");
    let rv = RuntimeVerifier::new(NET, c_chain, x_chain, &m).expect("build runtime verifier");

    let reg = Registry::new(&[AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]);
    m.apply_to(&reg, &rv, &DexAssetPolicy::default_policy())
        .expect("the manifest must admit through the runtime verifier");
    assert_eq!(reg.len(), 3, "expected 3 admitted assets");
}

#[test]
fn the_verifier_refuses_the_wrong_c_chain() {
    let manifest_c = tid("rv/wrongc/manifest-c");
    let running_c = tid("rv/wrongc/running-c"); // the node runs a different C-Chain
    let m = native_manifest(
        2,
        manifest_c,
        tid("rv/wrongc/x"),
        tid("rv/wrongc/utxo"),
        &addr20(0x21),
    );
    let e = RuntimeVerifier::new(2, running_c, ids::EMPTY, &m)
        .expect_err("must refuse a manifest rooted at a C-Chain the node does not run");
    assert!(e.text.contains("wrong-chain"), "expected wrong-chain: {e}");
}

#[test]
fn the_verifier_refuses_the_wrong_network() {
    let c_chain = tid("rv/wrongnet/c-chain");
    let m = native_manifest(
        2,
        c_chain,
        tid("rv/wrongnet/x"),
        tid("rv/wrongnet/utxo"),
        &addr20(0x21),
    );
    assert!(
        RuntimeVerifier::new(1 /* running mainnet */, c_chain, ids::EMPTY, &m).is_err(),
        "must refuse a manifest whose networkID != the running network"
    );
}

#[test]
fn the_verifier_refuses_a_utxo_off_the_running_x_chain() {
    let c_chain = tid("rv/wrongx/c-chain");
    let manifest_x = tid("rv/wrongx/manifest-x");
    let running_x = tid("rv/wrongx/running-x"); // the node runs a different X-Chain
    let utxo_asset = tid("rv/wrongx/utxo");
    let m = native_manifest(2, c_chain, manifest_x, utxo_asset, &addr20(0x21));

    // The C-Chain entries bind fine; the UTXO entry on the wrong X-Chain is
    // refused at admission.
    let rv = RuntimeVerifier::new(2, c_chain, running_x, &m)
        .expect("verifier builds: the C-Chain matches, only X differs");
    let reg = Registry::new(&[AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]);
    assert!(
        m.apply_to(&reg, &rv, &DexAssetPolicy::default_policy())
            .is_err(),
        "a UTXO asset rooted off the node's X-Chain must be refused"
    );
}

#[test]
fn the_verifier_refuses_an_asset_absent_from_the_manifest() {
    let c_chain = tid("rv/absent/c-chain");
    let m = native_manifest(
        2,
        c_chain,
        tid("rv/absent/x"),
        tid("rv/absent/utxo"),
        &addr20(0x21),
    );
    let rv = RuntimeVerifier::new(2, c_chain, ids::EMPTY, &m).expect("verifier build");

    // An ERC-20 the verifier was NOT built from — a different address — is
    // refused: its reality was never CI-proven for this manifest.
    let other = Asset {
        network_id: 2,
        chain_id: c_chain,
        kind: AssetKind::Erc20,
        canonical_ref: addr20(0x99).as_slice().into(),
        decimals: 6,
        symbol: "OTH".to_string(),
        enabled: true,
        ..Asset::default()
    };
    let reg = Registry::new(&[AssetKind::Erc20]);
    assert!(
        reg.register(&other, &rv).is_err(),
        "an asset absent from the runtime manifest must be refused at the node"
    );
}
