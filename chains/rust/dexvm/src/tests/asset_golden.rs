// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The cross-home golden KAT.
//!
//! These are the EXACT 32-byte AssetIDs asserted in BOTH Go homes — the chains
//! registry (`asset_golden_test.go`) and `luxfi/dex` (`assetid_test.go`,
//! `AssetIDGoldenVectors`). Pinning the identical bytes in a THIRD home locks
//! the resolve-to-register equivalence the value path depends on: a registered
//! AssetID and a swap-derived AssetID name the SAME asset by the SAME id, in
//! every language that computes one.
//!
//! Every vector uses networkID=2 and the all-0x11 source chain id. Do NOT edit a
//! vector to make a test pass — a changed id is a fork.

use crate::asset::{derive_asset_id, AssetKind, EVM_NATIVE_MARKER};
use crate::ids::{self, Id};

/// The fixed 32-byte source chain id — every byte 0x11 — the vectors are
/// generated against, identical to both Go homes' `chainAllOnes`.
fn golden_chain_all_ones() -> Id {
    [0x11u8; 32]
}

#[test]
fn asset_id_golden_kat_matches_both_go_homes() {
    let chain = golden_chain_all_ones();

    let mut erc20 = [0u8; 20];
    erc20[19] = 0x01;
    let mut utxo = [0u8; 32];
    utxo[31] = 0x07;

    let vectors: &[(&str, AssetKind, &[u8], &str)] = &[
        (
            "ERC20/addr..01",
            AssetKind::Erc20,
            &erc20,
            "dc392784b1b0764f885a2b24786850dae0a221fe7eaa218065ac1497473fa868",
        ),
        (
            "EVM_NATIVE/marker",
            AssetKind::EvmNative,
            &EVM_NATIVE_MARKER,
            "5941ecf871f909bac11b9b3d34fff1d05c7a0182f3a1c5b905ee6059dbb6dc72",
        ),
        (
            "UTXO/asset..07",
            AssetKind::Utxo,
            &utxo,
            "5cd895b8a577437bdf39e921902cbf06c29a11ae4f1776b369584c46fc0d647d",
        ),
    ];

    for (name, kind, r, want) in vectors {
        let got = derive_asset_id(2, chain, *kind, r).expect(name);
        assert_eq!(
            &ids::hex(&got),
            want,
            "{name}: the Rust AssetID diverged from the cross-home golden KAT"
        );
    }
}
