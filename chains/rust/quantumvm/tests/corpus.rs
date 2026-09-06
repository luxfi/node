// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! This chain's evaluator against the shared corpus, held to Go's answers.
//!
//! `make chains` runs the same comparison across three languages. This runs the
//! Rust half of it from `cargo test`, so a change that moves a verdict fails
//! here — in the crate that moved it — rather than in a target that builds nine
//! evaluators first.
//!
//! What is compared is what the differential compares: `parse`, `kind`, `hash`,
//! `syntactic` and `exec`. A field this evaluator declines to answer prints
//! `SKIPPED` and is excluded, exactly as the runner excludes it — and it is
//! never a pass. Today that is `exec` alone: acceptance is decided against a
//! parent and a clock, and this evaluator stands up no chain to hold either.

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::process::Command;

const FIELDS: [&str; 5] = ["parse", "kind", "hash", "syntactic", "exec"];
const NOT_EVALUATED: &str = "SKIPPED";

#[derive(Debug, PartialEq, Eq)]
struct Answer {
    fields: [String; 5],
    note: String,
}

fn corpus_dir() -> PathBuf {
    // The corpus is a sibling in this repository, not an optional checkout: a
    // missing one is a failure rather than a skip.
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../conformance/corpus")
}

fn answers(text: &str, prefix: &str) -> BTreeMap<String, Answer> {
    let mut out = BTreeMap::new();
    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split('\t').collect();
        assert_eq!(f.len(), 8, "not a result line: {line}");
        assert_eq!(f[0], "R", "not a result line: {line}");
        if !f[1].starts_with(prefix) {
            continue;
        }
        let fields = std::array::from_fn(|i| f[2 + i].to_string());
        let previous = out.insert(
            f[1].to_string(),
            Answer {
                fields,
                note: f[7].to_string(),
            },
        );
        assert!(previous.is_none(), "{} was answered twice", f[1]);
    }
    out
}

fn rust_answers() -> BTreeMap<String, Answer> {
    let out = Command::new(env!("CARGO_BIN_EXE_conformance"))
        .arg(corpus_dir().join("vectors.tsv"))
        .output()
        .expect("the evaluator did not run");
    assert!(
        out.status.success(),
        "the evaluator exited {}: {}",
        out.status,
        String::from_utf8_lossy(&out.stderr)
    );
    answers(
        &String::from_utf8(out.stdout).expect("the evaluator wrote no valid UTF-8"),
        "Q_",
    )
}

fn go_answers() -> BTreeMap<String, Answer> {
    let path = corpus_dir().join("expected.tsv");
    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    answers(&text, "Q_")
}

fn corpus_q_vectors() -> Vec<String> {
    let path = corpus_dir().join("vectors.tsv");
    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    text.lines()
        .filter(|l| !l.is_empty() && !l.starts_with('#'))
        .map(|l| l.split('\t').collect::<Vec<_>>())
        .filter(|f| f[0] == "V" && f[2] == "Q")
        .map(|f| f[1].to_string())
        .collect()
}

#[test]
fn every_q_vector_is_answered() {
    let vectors = corpus_q_vectors();
    let rust = rust_answers();
    assert!(!vectors.is_empty(), "the corpus holds no Q vector");
    for id in &vectors {
        assert!(rust.contains_key(id), "{id} was not answered");
    }
    assert_eq!(rust.len(), vectors.len(), "an answer without a vector");
}

#[test]
fn every_compared_field_agrees_with_go() {
    let go = go_answers();
    let rust = rust_answers();
    assert_eq!(go.len(), rust.len());
    for (id, want) in &go {
        let got = rust
            .get(id)
            .unwrap_or_else(|| panic!("{id} was not answered"));
        for (i, field) in FIELDS.iter().enumerate() {
            if got.fields[i] == NOT_EVALUATED {
                continue;
            }
            assert_eq!(
                got.fields[i], want.fields[i],
                "{id}.{field}: go said {}, this chain said {}",
                want.fields[i], got.fields[i]
            );
        }
    }
}

/// The identity is corpus contract, not this evaluator's private choice: a
/// block names its chain and its network on the wire, and the chain refuses one
/// whose pair is not the one this node serves. An evaluator pointed at the wrong
/// chain would answer "belongs to another chain" to every well-formed vector,
/// and the rows would read like a chain that had lost its rules. This one row
/// names the numbers instead.
#[test]
fn the_chain_identity_is_answered_and_is_the_one_go_derives_under() {
    let go = &go_answers()["Q_CHAIN_IDENTITY"];
    let rust = &rust_answers()["Q_CHAIN_IDENTITY"];
    assert_eq!(rust.fields, go.fields);
    assert_eq!(rust.fields[0], "ok");
    assert_eq!(rust.fields[1], "ChainIdentity");
    assert_eq!(rust.fields[3], "network=1");
}

/// A block of another chain, and a block of another network, are two different
/// wires and one refusal. Both are decided from the block alone — the binding
/// runs before the parent is looked for — so both land on the syntactic layer
/// and neither needs a chain with a tip.
#[test]
fn a_block_of_another_chain_or_another_network_is_refused_from_the_block_alone() {
    let rust = rust_answers();
    for id in ["Q_BLOCK_FOREIGN_CHAIN", "Q_BLOCK_FOREIGN_NETWORK"] {
        let got = &rust[id];
        assert_eq!(got.fields[0], "ok", "{id}: it is a well-formed wire");
        assert_eq!(got.fields[3], "SYNTACTIC", "{id}");
        assert!(
            got.note.contains("belongs to another chain"),
            "{id}: {}",
            got.note
        );
    }
    // And the block that DOES name this chain passes the same check, so the two
    // rows above are the rule biting rather than the check refusing everything.
    assert_eq!(rust["Q_BLOCK"].fields[3], "OK");
}

/// The evaluator answers ITS chain and stays quiet about everyone else's.
#[test]
fn no_other_chains_vectors_are_answered() {
    let out = Command::new(env!("CARGO_BIN_EXE_conformance"))
        .arg(corpus_dir().join("vectors.tsv"))
        .output()
        .expect("the evaluator did not run");
    let text = String::from_utf8(out.stdout).expect("valid UTF-8");
    let rows = text.lines().filter(|l| !l.is_empty()).count();
    assert_eq!(rows, corpus_q_vectors().len());
    for line in text.lines() {
        let id = line.split('\t').nth(1).expect("a row names its vector");
        assert!(id.starts_with("Q_"), "{id} is not this chain's vector");
    }
}
