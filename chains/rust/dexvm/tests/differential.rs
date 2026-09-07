// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The D-Chain differential: this port against the Go reference, vector for
//! vector.
//!
//! `conformance/dex_differential.json` is not a set of expectations someone
//! wrote down. It is what `github.com/luxfi/chains/dexvm/registry` ANSWERED,
//! emitted by `conformance/gen`, which imports the reference itself. Every case
//! below replays one of those inputs through this crate and asserts the same
//! answer — the same 32-byte id, the same verdict, the same error IDENTITY.
//!
//! A failure here is not a style disagreement. An AssetID is what the settlement
//! path names an asset by, so two languages that derive different ids for one
//! token have forked the ledger; and two that disagree about what registers have
//! forked what can trade.
//!
//! Each case also prints a `RESULT` line, in the shape the repo's differential
//! harness reads, so this suite can be run under it without being rewritten.

use lux_dexvm::asset::{derive_asset_id, market_id, AssetKind};
use lux_dexvm::consensus_mode::{
    guard_value_activation, ConsensusMode, LaunchAssertions, NO_BYZANTINE_FINALITY_CLAIM,
};
use lux_dexvm::embedded::embedded_manifest_for;
use lux_dexvm::error::{Code, Error, Result};
use lux_dexvm::forbidden::{
    assert_no_forbidden_asset_refs, is_forbidden_universe_label, is_mock_liquidity_ref,
};
use lux_dexvm::gate::{refuse_under_synthetic_config, DexAssetPolicy, NetworkClass};
use lux_dexvm::ids::{self, Id};
use lux_dexvm::market::Market;
use lux_dexvm::registry::{Asset, ChainVerifier, Registry, RiskTier};
use serde_json::Value;
use std::collections::BTreeMap;

// ---- reading the corpus ----------------------------------------------------

fn corpus() -> Value {
    let path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/conformance/dex_differential.json"
    );
    let raw = std::fs::read(path).unwrap_or_else(|e| {
        panic!("the differential corpus must be present at {path}: {e}");
    });
    serde_json::from_slice(&raw).expect("the corpus must be valid JSON")
}

fn s(v: &Value, k: &str) -> String {
    v.get(k)
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}

fn u(v: &Value, k: &str) -> u64 {
    v.get(k).and_then(Value::as_u64).unwrap_or(0)
}

fn b(v: &Value, k: &str) -> bool {
    v.get(k).and_then(Value::as_bool).unwrap_or(false)
}

fn arr<'a>(v: &'a Value, k: &str) -> &'a [Value] {
    v.get(k)
        .and_then(Value::as_array)
        .map(|a| a.as_slice())
        .unwrap_or(&[])
}

fn unhex(h: &str) -> Vec<u8> {
    hex::decode(h).unwrap_or_else(|e| panic!("corpus hex {h:?}: {e}"))
}

fn id_of(h: &str) -> Id {
    ids::from_slice(&unhex(h)).unwrap_or_else(|| panic!("corpus id {h:?} is not 32 bytes"))
}

fn kind_of(name: &str) -> AssetKind {
    match name {
        "EVM_NATIVE" => AssetKind::EvmNative,
        "ERC20" => AssetKind::Erc20,
        "UTXO" => AssetKind::Utxo,
        _ => AssetKind::Invalid,
    }
}

/// The Go sentinel a refusal must carry. `OTHER` is a refusal with no sentinel:
/// the corpus asserts that a refusal happened, not which words it used.
fn code_name(e: &Error) -> &'static str {
    match e.code {
        Code::InvalidKind => "InvalidKind",
        Code::BadRef => "BadRef",
        Code::EmptyChainId => "EmptyChainID",
        Code::SameAsset => "SameAsset",
        Code::NetworkMismatch => "NetworkMismatch",
        Code::DuplicateMarket => "DuplicateMarket",
        Code::UnknownAsset => "UnknownAsset",
        Code::AssetDisabled => "AssetDisabled",
        Code::KindNotAllowed => "KindNotAllowed",
        Code::DuplicateAsset => "DuplicateAsset",
        Code::SyntheticOnValueNet => "SyntheticOnValueNet",
        Code::EnabledMarketUnknownAsset => "EnabledMarketUnknownAsset",
        Code::BadAllowedKind => "BadAllowedKind",
        Code::ValueModeUnset => "ValueModeUnset",
        Code::ValueModeIllegal => "ValueModeIllegal",
        Code::LaunchAssertionsUnmet => "LaunchAssertionsUnmet",
        Code::ManifestHashMismatch => "ManifestHashMismatch",
        Code::NoEmbeddedManifest => "NoEmbeddedManifest",
        Code::Other => "OTHER",
    }
}

/// Assert one outcome against the reference's, and say so in the harness's
/// shape. `want_id` is checked only where the reference produced one.
fn agree<T>(id: &str, got: &Result<T>, want_status: &str, want_code: &str) {
    let (status, code) = match got {
        Ok(_) => ("OK", String::new()),
        Err(e) => ("REFUSED", code_name(e).to_string()),
    };
    println!("RESULT id={id} status={status} detail={code}");
    assert_eq!(
        status,
        want_status,
        "{id}: the reference said {want_status}, this port said {status}{}",
        match got {
            Err(e) => format!(" ({e})"),
            Ok(_) => String::new(),
        }
    );
    if status == "REFUSED" && !want_code.is_empty() {
        assert_eq!(
            code, want_code,
            "{id}: the reference refused with {want_code}, this port with {code}"
        );
    }
}

// ---- the chain a scenario declares -----------------------------------------

/// The snapshot the corpus describes: it answers "real" for exactly what the
/// scenario seeded, which is what makes every refusal below a genuine one.
#[derive(Default)]
struct SnapshotChain {
    seen: BTreeMap<(u32, Id, String, Vec<u8>), u8>,
}

impl SnapshotChain {
    fn seed(&mut self, network_id: u32, chain: Id, kind: &str, r: Vec<u8>, dec: u8) {
        self.seen
            .insert((network_id, chain, kind.to_string(), r), dec);
    }

    fn get(&self, network_id: u32, chain: Id, kind: &str, r: &[u8]) -> Result<u8> {
        self.seen
            .get(&(network_id, chain, kind.to_string(), r.to_vec()))
            .copied()
            .ok_or_else(|| Error::other("snapshot: no such object on this network/chain"))
    }
}

impl ChainVerifier for SnapshotChain {
    fn verify_erc20(&self, n: u32, ch: Id, addr: &[u8]) -> Result<u8> {
        self.get(n, ch, "ERC20", addr)
    }
    fn verify_evm_native(&self, n: u32, ch: Id) -> Result<u8> {
        self.get(n, ch, "EVM_NATIVE", &[0u8; 20])
    }
    fn verify_utxo_asset(&self, n: u32, ch: Id, a: Id) -> Result<u8> {
        self.get(n, ch, "UTXO", &a)
    }
}

// ---- the vectors -----------------------------------------------------------

#[test]
fn asset_ids_agree_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "assetIds");
    assert!(!cases.is_empty(), "the corpus must carry asset-id vectors");
    for v in cases {
        let name = s(v, "name");
        let got = derive_asset_id(
            u(v, "networkID") as u32,
            id_of(&s(v, "chain")),
            kind_of(&s(v, "kind")),
            &unhex(&s(v, "ref")),
        );
        agree(&name, &got, &s(v, "status"), &s(v, "code"));
        if let Ok(id) = got {
            assert_eq!(
                ids::hex(&id),
                s(v, "id"),
                "{name}: the AssetID diverged from the reference — this is a fork, not a preference"
            );
        }
    }
}

#[test]
fn market_ids_agree_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "marketIds");
    assert!(!cases.is_empty(), "the corpus must carry market-id vectors");
    for v in cases {
        let name = s(v, "name");
        let got = market_id(
            u(v, "networkID") as u32,
            id_of(&s(v, "base")),
            id_of(&s(v, "quote")),
            &unhex(&s(v, "venue")),
        );
        println!(
            "RESULT id=market/{name} status=OK detail={}",
            ids::hex(&got)
        );
        assert_eq!(ids::hex(&got), s(v, "id"), "{name}: MarketID diverged");
    }
}

#[test]
fn cb58_agrees_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "cb58");
    assert!(!cases.is_empty(), "the corpus must carry cb58 vectors");
    for v in cases {
        let h = s(v, "hex");
        let id = id_of(&h);
        let want = s(v, "cb58");
        println!("RESULT id=cb58/{h} status=OK detail={want}");
        assert_eq!(ids::cb58(&id), want, "{h}: cb58 diverged");
        assert_eq!(
            ids::from_string(&want).expect("parse"),
            id,
            "{h}: cb58 does not round-trip"
        );
    }
}

#[test]
fn the_embedded_manifests_agree_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "embedded");
    assert!(!cases.is_empty(), "the corpus must carry embedded vectors");
    for v in cases {
        let chain_id = u(v, "evmChainID");
        let name = format!("embedded/{chain_id}");
        let got = embedded_manifest_for(chain_id);
        agree(&name, &got, &s(v, "status"), &s(v, "code"));
        if let Ok((m, sum)) = got {
            // The content hash is over the bytes THIS binary carries. It
            // matching the reference's is what says the two binaries hold the
            // same artifact, not merely a manifest that parses the same.
            assert_eq!(
                sum,
                s(v, "sha256"),
                "{name}: manifest content hash diverged"
            );
            assert_eq!(m.network, s(v, "network"), "{name}: network");
            assert_eq!(m.network_id, u(v, "networkID") as u32, "{name}: networkID");
            assert_eq!(
                ids::hex(&m.c_chain_id),
                s(v, "cChainHex"),
                "{name}: cChainID"
            );
            assert_eq!(m.assets.len() as u64, u(v, "assets"), "{name}: asset count");
            assert_eq!(
                m.markets.len() as u64,
                u(v, "markets"),
                "{name}: market count"
            );
        }
    }
}

#[test]
fn the_label_predicates_agree_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "labels");
    assert!(!cases.is_empty(), "the corpus must carry label vectors");
    for v in cases {
        let l = s(v, "s");
        assert_eq!(
            is_forbidden_universe_label(&l),
            b(v, "universe"),
            "{l:?}: the white-label universe predicate diverged"
        );
        assert_eq!(
            is_mock_liquidity_ref(&l),
            b(v, "mock"),
            "{l:?}: the mock-liquidity predicate diverged"
        );
        println!(
            "RESULT id=label/{l:?} status=OK detail=universe={},mock={}",
            b(v, "universe"),
            b(v, "mock")
        );
    }
}

#[test]
fn the_deny_gate_agrees_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "deny");
    assert!(!cases.is_empty(), "the corpus must carry deny vectors");
    for v in cases {
        let name = s(v, "name");
        let got = assert_no_forbidden_asset_refs(
            &s(v, "symbol"),
            &s(v, "assetName"),
            &s(v, "chainLabel"),
        );
        agree(&format!("deny/{name}"), &got, &s(v, "status"), "");
    }
}

#[test]
fn the_value_guard_agrees_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "guards");
    assert!(!cases.is_empty(), "the corpus must carry guard vectors");
    for v in cases {
        let name = s(v, "name");
        let mode = match u(v, "mode") {
            0 => ConsensusMode::Unset,
            1 => ConsensusMode::QuorumFinality,
            2 => ConsensusMode::HonestValidatorLabeled,
            other => ConsensusMode::Other(other as u8),
        };
        let got = guard_value_activation(
            b(v, "valueEnabled"),
            mode,
            LaunchAssertions {
                caps_on: b(v, "capsOn"),
                real_assets_only: b(v, "realAssetsOnly"),
                halt_ready: b(v, "haltReady"),
            },
        );
        agree(
            &format!("guard/{name}"),
            &got,
            &s(v, "status"),
            &s(v, "code"),
        );
        if let Ok(st) = &got {
            // The disclaimer is matched BYTE for byte: an audit surface compares
            // it as a constant, so a reworded one is a different claim.
            assert_eq!(
                st.status,
                s(v, "statusString"),
                "{name}: the surfaced status string diverged"
            );
            if mode == ConsensusMode::HonestValidatorLabeled && b(v, "valueEnabled") {
                assert_eq!(st.status, NO_BYZANTINE_FINALITY_CLAIM);
            }
        }
    }
}

#[test]
fn whole_registry_scenarios_agree_with_the_reference() {
    let c = corpus();
    let cases = arr(&c, "gates");
    assert!(!cases.is_empty(), "the corpus must carry gate scenarios");
    for v in cases {
        let name = s(v, "name");

        // The chain snapshot this scenario declares.
        let mut chain = SnapshotChain::default();
        for sd in arr(v, "seeds") {
            chain.seed(
                u(sd, "networkID") as u32,
                id_of(&s(sd, "chain")),
                &s(sd, "kind"),
                unhex(&s(sd, "ref")),
                u(sd, "decimals") as u8,
            );
        }

        let allowed: Vec<AssetKind> = arr(v, "registryAllowedKinds")
            .iter()
            .map(|k| kind_of(k.as_str().unwrap_or_default()))
            .collect();
        let reg = Registry::new(&allowed);

        // Registration, one asset at a time, in the corpus's order — because
        // order is what makes a duplicate a duplicate.
        for (i, a) in arr(v, "assets").iter().enumerate() {
            let asset = Asset {
                network_id: u(a, "networkID") as u32,
                chain_id: id_of(&s(a, "chain")),
                kind: kind_of(&s(a, "kind")),
                canonical_ref: unhex(&s(a, "ref")).into(),
                decimals: u(a, "decimals") as u8,
                symbol: s(a, "symbol"),
                name: s(a, "name"),
                enabled: b(a, "enabled"),
                risk_tier: RiskTier(u(a, "riskTier") as u8),
            };
            let got = reg.register(&asset, &chain);
            agree(
                &format!("{name}/asset[{i}]"),
                &got,
                &s(a, "status"),
                &s(a, "code"),
            );
            if let Ok(id) = got {
                assert_eq!(
                    ids::hex(&id),
                    s(a, "id"),
                    "{name}/asset[{i}]: the registered AssetID diverged"
                );
            }
        }

        for (i, m) in arr(v, "markets").iter().enumerate() {
            let market = Market {
                network_id: u(m, "networkID") as u32,
                base_asset_id: id_of(&s(m, "base")),
                quote_asset_id: id_of(&s(m, "quote")),
                venue_config: unhex(&s(m, "venue")).into(),
                enabled: b(m, "enabled"),
            };
            let got = reg.create_market(&market);
            agree(
                &format!("{name}/market[{i}]"),
                &got,
                &s(m, "status"),
                &s(m, "code"),
            );
            if let Ok(id) = got {
                assert_eq!(
                    ids::hex(&id),
                    s(m, "id"),
                    "{name}/market[{i}]: the created MarketID diverged"
                );
            }
        }

        // The policy, including the raw bytes a scenario may smuggle a non-real
        // kind through.
        let pol = v.get("policy").cloned().unwrap_or(Value::Null);
        let policy = DexAssetPolicy {
            allow_synthetic_assets: b(&pol, "allowSyntheticAssets"),
            allow_synthetic_markets: b(&pol, "allowSyntheticMarkets"),
            allow_mock_liquidity: b(&pol, "allowMockLiquidity"),
            allowed_asset_kinds: pol
                .get("allowedKindBytes")
                .and_then(Value::as_array)
                .map(|a| {
                    a.iter()
                        .map(|k| AssetKind::from_byte(k.as_u64().unwrap_or(0) as u8))
                        .collect()
                })
                .unwrap_or_default(),
        };

        let labels: BTreeMap<String, String> = v
            .get("labels")
            .and_then(Value::as_object)
            .map(|o| {
                o.iter()
                    .map(|(k, val)| (k.clone(), val.as_str().unwrap_or_default().to_string()))
                    .collect()
            })
            .unwrap_or_default();
        let label_for = |id: Id| labels.get(&ids::hex(&id)).cloned().unwrap_or_default();

        let class = match u(v, "class") {
            2 => NetworkClass::Mainnet,
            1 => NetworkClass::Testnet,
            _ => NetworkClass::Dev,
        };
        let got = refuse_under_synthetic_config(class, &policy, &reg, Some(&label_for));
        agree(
            &format!("{name}/gate"),
            &got,
            &s(v, "status"),
            &s(v, "code"),
        );
    }
}

#[test]
fn the_corpus_covers_every_family() {
    // A corpus that silently lost a family would let this suite pass while
    // testing less than it says. The counts are asserted as floors, so adding
    // vectors is free and removing one is loud.
    let c = corpus();
    for (family, floor) in [
        ("assetIds", 20usize),
        ("marketIds", 6),
        ("cb58", 8),
        ("embedded", 5),
        ("labels", 23),
        ("deny", 13),
        ("guards", 13),
        ("gates", 21),
    ] {
        let n = arr(&c, family).len();
        assert!(
            n >= floor,
            "the corpus lost vectors: {family} has {n}, expected at least {floor}"
        );
    }
}
