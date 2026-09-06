// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The bytes this port is not allowed to move.
//!
//! Every value here was printed by the GO reference. The first three are
//! stronger than that: they are asserted in TWO Go homes — the registry's own
//! KAT in `asset_golden_test.go` and `luxfi/dex`'s `AssetIDGoldenVectors` —
//! which pin the same strings so that a registered AssetID and a swap-derived
//! one name the same asset by the same id.
//!
//! Do not edit a vector to make a test pass. A changed id is a fork.

use lux_dexvm::asset::{derive_asset_id, market_id, AssetKind, EVM_NATIVE_MARKER};
use lux_dexvm::ids;

/// The cross-home KAT: network 2, source chain id all `0x11`, the three kinds.
#[test]
fn the_three_asset_identities_are_the_ones_two_go_homes_assert() {
    let chain = ids::filled(0x11);
    let mut erc20 = [0u8; 20];
    erc20[19] = 0x01;
    let mut utxo = ids::EMPTY;
    utxo[31] = 0x07;

    let vectors: [(AssetKind, &[u8], &str); 3] = [
        (
            AssetKind::Erc20,
            &erc20,
            "dc392784b1b0764f885a2b24786850dae0a221fe7eaa218065ac1497473fa868",
        ),
        (
            AssetKind::EvmNative,
            &EVM_NATIVE_MARKER,
            "5941ecf871f909bac11b9b3d34fff1d05c7a0182f3a1c5b905ee6059dbb6dc72",
        ),
        (
            AssetKind::Utxo,
            &utxo,
            "5cd895b8a577437bdf39e921902cbf06c29a11ae4f1776b369584c46fc0d647d",
        ),
    ];

    for (kind, reference, want) in vectors {
        let got = derive_asset_id(2, chain, kind, reference).expect("derive");
        assert_eq!(ids::hex(&got), want, "{kind} AssetID diverged from the KAT");
    }
}

/// The corpus's own five assets: the three kinds on their real source chains,
/// then the same token moved to another network and to another chain. Every
/// value is what the Go evaluator printed into `conformance/corpus/expected.tsv`.
#[test]
fn the_corpus_asset_identities_are_the_ones_go_printed() {
    const C_CHAIN: u8 = 3;
    const X_CHAIN: u8 = 2;
    let token = [0xC0u8; 20];

    let vectors: [(u32, u8, AssetKind, &[u8], &str); 5] = [
        (
            1,
            C_CHAIN,
            AssetKind::EvmNative,
            &EVM_NATIVE_MARKER,
            "17b2d55c97c7250dd0e0d88fe65b359ef205ad648760140e5841202c042147ff",
        ),
        (
            1,
            C_CHAIN,
            AssetKind::Erc20,
            &token,
            "f78ce2d083844fa6fdf4125d95989b9ed38efa32086c81d25e7ce3c37b673941",
        ),
        (
            1,
            X_CHAIN,
            AssetKind::Utxo,
            &[0x50u8; 32],
            "6e9415353d6dce8cf0b9dda1c0d2ab35ee28c4325f379232aa45923b90b522ea",
        ),
        (
            2,
            C_CHAIN,
            AssetKind::Erc20,
            &token,
            "95cadbb01f8d7a0959197a273db205231456a2053dba7ba97cd0c8baa8ab6f64",
        ),
        (
            1,
            C_CHAIN + 1,
            AssetKind::Erc20,
            &token,
            "722a92b62fcedff518241db0271d096e5c91d21e4ec9d1a59fd04eb314f35087",
        ),
    ];

    for (network, chain, kind, reference, want) in vectors {
        let got = derive_asset_id(network, ids::filled(chain), kind, reference).expect("derive");
        assert_eq!(ids::hex(&got), want);
    }
}

/// One pair under four descriptions: as written, reversed, at another venue, and
/// on another network. Four ids, all Go's.
#[test]
fn the_corpus_market_identities_are_the_ones_go_printed() {
    let base = ids::filled(0x10);
    let quote = ids::filled(0x11);
    let venue = b"tick=1,lot=1";

    assert_eq!(
        ids::hex(&market_id(1, base, quote, venue)),
        "6d3e427ab2f7354a48cdc7ba48f593589672701c808211e711dce9d48096dbf4"
    );
    assert_eq!(
        ids::hex(&market_id(1, quote, base, venue)),
        "27b57a3ba62728623b0fcbee3d035381642321e2a43b0ff8a1e601f7084b120f"
    );
    assert_eq!(
        ids::hex(&market_id(1, base, quote, b"tick=2,lot=1")),
        "902ca69a7b79443f99ea27892775ed9302187bb92dfa0ee70e5b0fb320a5167f"
    );
    assert_eq!(
        ids::hex(&market_id(2, base, quote, venue)),
        "61d424a71fd49a96743841f7fc0bf3472313e082d1d363d3cb4f350c63b2ed4a"
    );
}

/// The preimage the ids above are the hash of, stated in full for the one case
/// small enough to read. A test that only compared hashes would say WHICH bytes
/// moved but never what they were.
#[test]
fn the_preimage_is_the_tag_the_network_the_chain_the_kind_and_the_reference() {
    use lux_dexvm::asset::Folder;

    let chain = ids::filled(0x11);
    let mut erc20 = [0u8; 20];
    erc20[19] = 0x01;

    let mut f = Folder::default();
    f.tag(b"lux:dex:asset:v1");
    f.u32(2);
    f.bytes(&chain);
    f.u8(AssetKind::Erc20 as u8);
    f.bytes(&erc20);

    let mut want = Vec::new();
    want.extend_from_slice(&16u64.to_be_bytes());
    want.extend_from_slice(b"lux:dex:asset:v1");
    want.extend_from_slice(&4u64.to_be_bytes());
    want.extend_from_slice(&2u32.to_be_bytes());
    want.extend_from_slice(&32u64.to_be_bytes());
    want.extend_from_slice(&chain);
    want.extend_from_slice(&1u64.to_be_bytes());
    want.push(2); // ERC20's wire-pinned kind byte
    want.extend_from_slice(&20u64.to_be_bytes());
    want.extend_from_slice(&erc20);

    assert_eq!(f.preimage(), want.as_slice());
    assert_eq!(
        ids::hex(&f.sum()),
        "dc392784b1b0764f885a2b24786850dae0a221fe7eaa218065ac1497473fa868"
    );
    // And this hand-written preimage is the one the derivation actually folds,
    // so a change to the tag or the field order moves this test too rather than
    // leaving it green over bytes nothing uses.
    assert_eq!(
        f.sum(),
        derive_asset_id(2, chain, AssetKind::Erc20, &erc20).expect("derive")
    );
}

/// The kind bytes are wire-pinned. Renumbering them would move every id of that
/// kind while leaving every test that only spoke in tokens green.
#[test]
fn the_kind_bytes_are_one_two_and_three() {
    assert_eq!(AssetKind::Invalid as u8, 0);
    assert_eq!(AssetKind::EvmNative as u8, 1);
    assert_eq!(AssetKind::Erc20 as u8, 2);
    assert_eq!(AssetKind::Utxo as u8, 3);
}
