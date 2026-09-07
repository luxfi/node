// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The content pin: a manifest is bound to its CI-approved artifact by a
//! SHA-256 over its exact bytes.
//!
//! An edited local manifest — a fabricated token address, an added asset — no
//! longer hashes to the pin and is REFUSED. The node holds no EVM state of its
//! own, so it cannot look the token up itself; the content hash is what stops a
//! tampered file from loading.

use super::{addr20, tid};
use crate::asset::AssetKind;
use crate::error::Code;
use crate::ids::{self, Id};
use crate::manifest::{load_manifest, load_manifest_pinned, Manifest};
use crate::registry::{Asset, RISK_TIER0};

/// A minimal real manifest on disk, and its content hash.
fn write_pin_manifest(
    tag: &str,
    network_id: u32,
    c_chain: Id,
    erc20: &[u8],
) -> (std::path::PathBuf, String) {
    let m = Manifest {
        network: "mainnet".to_string(),
        network_id,
        evm_chain_id: 96369,
        c_chain_id: c_chain,
        assets: vec![Asset {
            network_id,
            chain_id: c_chain,
            kind: AssetKind::Erc20,
            canonical_ref: erc20.into(),
            decimals: 18,
            symbol: "WLUX".to_string(),
            name: "Wrapped LUX".to_string(),
            enabled: true,
            risk_tier: RISK_TIER0,
        }],
        ..Manifest::default()
    };
    let b = serde_json::to_vec_pretty(&m).expect("marshal");
    let dir = std::env::temp_dir().join(format!("lux-dexvm-pin-{}-{tag}", std::process::id()));
    std::fs::create_dir_all(&dir).expect("temp dir");
    let path = dir.join("assets.mainnet.json");
    std::fs::write(&path, &b).expect("write");
    (path, ids::hex(&ids::sha256(&b)))
}

#[test]
fn a_manifest_matching_its_pinned_hash_loads() {
    let c_chain = tid("pin/match/c-chain");
    let (path, sum) = write_pin_manifest("match", 1, c_chain, &addr20(0x4a));

    let m =
        load_manifest_pinned(&path, &sum).expect("a manifest matching its pinned hash must load");
    assert_eq!(m.assets.len(), 1);

    // The pin accepts the common prefixes and is case-insensitive.
    load_manifest_pinned(&path, &format!("0x{sum}")).expect("0x-prefixed pin must load");
    load_manifest_pinned(&path, &format!("sha256:{}", sum.to_uppercase()))
        .expect("sha256:-prefixed uppercase pin must load");
}

#[test]
fn an_edited_manifest_with_a_fabricated_address_is_rejected() {
    let c_chain = tid("pin/edit/c-chain");
    let (path, ci_approved_hash) = write_pin_manifest("edit", 1, c_chain, &addr20(0x4a));

    // The file is rewritten in place, swapping the real token for a fabricated
    // address — and it is still structurally valid, so shape validation alone
    // would pass it.
    let tampered = Manifest {
        network: "mainnet".to_string(),
        network_id: 1,
        evm_chain_id: 96369,
        c_chain_id: c_chain,
        assets: vec![Asset {
            network_id: 1,
            chain_id: c_chain,
            kind: AssetKind::Erc20,
            canonical_ref: addr20(0xEE).as_slice().into(), // fabricated
            decimals: 18,
            symbol: "WLUX".to_string(),
            name: "Wrapped LUX".to_string(),
            enabled: true,
            risk_tier: RISK_TIER0,
        }],
        ..Manifest::default()
    };
    std::fs::write(&path, serde_json::to_vec_pretty(&tampered).unwrap()).expect("rewrite");

    // Loading WITHOUT a pin accepts the tampered file — which is the proof that
    // the shape check alone is insufficient.
    load_manifest(&path).expect("sanity: the tampered file is still structurally valid");

    // Loading WITH the CI-approved pin refuses it.
    let e = load_manifest_pinned(&path, &ci_approved_hash).unwrap_err();
    assert!(e.is(Code::ManifestHashMismatch), "got: {e}");
}

#[test]
fn a_malformed_pin_fails_closed() {
    let c_chain = tid("pin/malformed/c-chain");
    let (path, _) = write_pin_manifest("malformed", 1, c_chain, &addr20(0x4a));

    for bad in [
        "deadbeef",       /* too short */
        &"zz".repeat(32), /* non-hex */
    ] {
        assert!(
            load_manifest_pinned(&path, bad).is_err(),
            "a malformed pin {bad:?} must fail closed"
        );
    }
}

#[test]
fn an_empty_pin_falls_back_to_shape_only() {
    let c_chain = tid("pin/empty/c-chain");
    let (path, _) = write_pin_manifest("empty", 1, c_chain, &addr20(0x4a));
    load_manifest_pinned(&path, "").expect("an empty pin must fall back to a shape-only load");
}
