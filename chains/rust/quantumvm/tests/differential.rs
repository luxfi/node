// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Q-Chain evaluator for the differential harness.
//!
//! Two sources, one evaluator.
//!
//! `tests/vectors/q_reference.json` is this chain's own golden file, and every
//! byte of it came out of the GO chain — the wires are what
//! `chains/quantumvm`'s encoder writes and the verdicts are what its parser
//! answers, ids included. It travels with this crate, so the Rust side can be
//! held to the reference with nothing else checked out. Divergence here FAILS.
//!
//! `conformance/corpus/chain_differential.json` is the shared corpus the
//! cross-language harness reads. This evaluator prints a `RESULT` line per Q
//! vector for `conformance/harness_runner.py` to compare against the other
//! runtimes. When the corpus is not checked out, that half prints `skipped` and
//! says why — a corpus that is not there is not a pass.
//!
//! The verdict always comes from the chain. The vector's `expected_action`
//! chooses which OPERATION to run; nothing here ever reads the expected result
//! before deciding, which is the only way a differential test means anything.

use lux_quantumvm::ids;
use lux_quantumvm::wire;

use serde::Deserialize;
use std::path::{Path, PathBuf};

#[derive(Deserialize)]
struct Vector {
    id: String,
    #[serde(default)]
    chain: String,
    wire_hex: String,
    expected_action: String,
    #[serde(default)]
    go_expectation: Expectation,
}

#[derive(Deserialize, Default)]
struct Expectation {
    #[serde(default)]
    status: String,
    #[serde(default)]
    tx_id_hex: String,
    #[serde(default)]
    block_id_hex: String,
}

#[derive(Deserialize)]
struct Corpus {
    vectors: Vec<Vector>,
}

fn unhex(s: &str) -> Vec<u8> {
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).unwrap())
        .collect()
}

/// What this chain answers for one vector: a status, a detail, and the id it
/// named the thing by (empty when it named nothing).
fn evaluate(action: &str, bytes: &[u8]) -> (&'static str, String, String) {
    match action {
        "block_accept" | "block_reject" | "block_wire" => match wire::parse_block(bytes) {
            Ok(block) => (
                "ACCEPTED",
                format!(
                    "block height {} over {} transactions",
                    block.height,
                    block.txs.len()
                ),
                ids::hex(&block.id()),
            ),
            Err(e) => ("REJECTED", e.to_string(), String::new()),
        },
        // A transaction arrives in one of two shapes: the envelope that rides
        // in a block, or the signature preimage the envelope wraps. Try the
        // envelope first, because it is the shape a peer sends.
        _ => match wire::parse_tx_envelope(bytes) {
            Ok(tx) => (
                "ACCEPTED",
                format!("envelope, nonce {}", tx.nonce),
                ids::hex(&tx.id()),
            ),
            Err(envelope_err) => match wire::parse_tx_body(bytes) {
                Ok(tx) => (
                    "ACCEPTED",
                    format!("preimage, nonce {}", tx.nonce),
                    ids::hex(&tx.id()),
                ),
                Err(_) => ("REJECTED", envelope_err.to_string(), String::new()),
            },
        },
    }
}

fn manifest(rel: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join(rel)
}

/// The Go reference, byte for byte and verdict for verdict.
#[test]
fn the_chain_answers_what_the_go_chain_answers() {
    let path = manifest("tests/vectors/q_reference.json");
    let raw = std::fs::read_to_string(&path)
        .unwrap_or_else(|e| panic!("the golden file travels with this crate: {path:?}: {e}"));
    let vectors: Vec<Vector> = serde_json::from_str(&raw).expect("golden file");
    assert!(!vectors.is_empty(), "an empty golden file is not a pass");

    println!("\n=== RUST QUANTUMVM vs THE GO REFERENCE ===");
    for v in &vectors {
        let bytes = unhex(&v.wire_hex);
        let (status, detail, id) = evaluate(&v.expected_action, &bytes);
        println!("{:<24} {status:<9} {detail}", v.id);

        assert_eq!(
            status, v.go_expectation.status,
            "{}: Rust says {status} ({detail}), the Go chain says {}",
            v.id, v.go_expectation.status
        );

        // A verdict that agrees while naming a different id is two chains that
        // both accepted and disagree about WHAT they accepted.
        let want = if v.go_expectation.block_id_hex.is_empty() {
            &v.go_expectation.tx_id_hex
        } else {
            &v.go_expectation.block_id_hex
        };
        if !want.is_empty() {
            assert_eq!(&id, want, "{}: the id differs from the Go chain's", v.id);
        }
    }
}

/// The shared corpus, as the cross-language harness reads it.
#[test]
fn evaluate_the_differential_corpus() {
    let path = manifest("../../../conformance/corpus/chain_differential.json");
    let raw = match std::fs::read_to_string(&path) {
        Ok(raw) => raw,
        Err(e) => {
            // Not a pass, and not a failure of this chain: the corpus is
            // generated into a directory that may not be checked out here.
            println!("skipped: the differential corpus is not at {path:?}: {e}");
            return;
        }
    };
    let corpus: Corpus = serde_json::from_str(&raw).expect("corpus");

    let mut seen = 0;
    let mut disagree = Vec::new();
    println!("\n=== RUST QUANTUMVM DIFFERENTIAL EVALUATION ===");
    for v in corpus.vectors.iter().filter(|v| v.chain == "Q") {
        seen += 1;
        let bytes = unhex(&v.wire_hex);
        let (status, detail, id) = evaluate(&v.expected_action, &bytes);
        let detail = if id.is_empty() {
            detail
        } else {
            format!("{detail} id={id}")
        };
        // The line the harness reads. Spaces would split the field, so the
        // detail is reported with none.
        println!(
            "RESULT id={} status={status} detail={}",
            v.id,
            detail.replace(' ', "_")
        );
        if !v.go_expectation.status.is_empty() && v.go_expectation.status != status {
            disagree.push(format!(
                "{}: Rust {status} ({detail}), recorded Go {}",
                v.id, v.go_expectation.status
            ));
        }
    }
    println!("evaluated {seen} Q vectors");
    assert!(
        disagree.is_empty(),
        "the corpus records a different verdict than this chain reaches:\n  {}",
        disagree.join("\n  ")
    );
}
