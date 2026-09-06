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
//! `syntactic` and `exec`. The note is not, because it carries each
//! implementation's own words — but the D notes happen to be byte-identical to
//! Go's, and that is asserted separately below, because a port that reproduced
//! every verdict and none of the reasons would be agreeing by coincidence.

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::process::Command;

const FIELDS: [&str; 5] = ["parse", "kind", "hash", "syntactic", "exec"];

/// One implementation's answer to one vector.
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

/// The answers a `R <id> <parse> <kind> <hash> <syntactic> <exec> <note>` stream
/// carries, for the vectors of one chain.
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

/// Run this crate's evaluator over the corpus and read back what it said.
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
        "D_",
    )
}

fn go_answers() -> BTreeMap<String, Answer> {
    let path = corpus_dir().join("expected.tsv");
    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    answers(&text, "D_")
}

/// How many D vectors the corpus holds, so that an evaluator answering FEWER is
/// a failure rather than a shorter green run. Silence is not agreement.
fn corpus_d_vectors() -> Vec<String> {
    let path = corpus_dir().join("vectors.tsv");
    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    text.lines()
        .filter(|l| !l.is_empty() && !l.starts_with('#'))
        .map(|l| l.split('\t').collect::<Vec<_>>())
        .filter(|f| f[0] == "V" && f[2] == "D")
        .map(|f| f[1].to_string())
        .collect()
}

#[test]
fn every_d_vector_is_answered() {
    let vectors = corpus_d_vectors();
    let rust = rust_answers();
    assert!(!vectors.is_empty(), "the corpus holds no D vector");
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
            assert_eq!(
                got.fields[i], want.fields[i],
                "{id}.{field}: go said {}, this chain said {}",
                want.fields[i], got.fields[i]
            );
        }
    }
}

/// The reasons, not only the verdicts. D is the one chain where this holds in
/// full: the port raises the reference's own sentinels, so its sentences are
/// Go's sentences down to the byte — including the ones the 160-byte trim cuts.
#[test]
fn every_reason_reads_the_way_gos_does() {
    let go = go_answers();
    let rust = rust_answers();
    for (id, want) in &go {
        let got = &rust[id];
        assert_eq!(got.note, want.note, "{id}: the reason differs");
    }
}

/// The evaluator answers ITS chain and stays quiet about everyone else's. A row
/// is a claim, and this program has no claim to make about a P-chain
/// transaction.
#[test]
fn no_other_chains_vectors_are_answered() {
    let out = Command::new(env!("CARGO_BIN_EXE_conformance"))
        .arg(corpus_dir().join("vectors.tsv"))
        .output()
        .expect("the evaluator did not run");
    let text = String::from_utf8(out.stdout).expect("valid UTF-8");
    let rows = text.lines().filter(|l| !l.is_empty()).count();
    assert_eq!(rows, corpus_d_vectors().len());
    for line in text.lines() {
        let id = line.split('\t').nth(1).expect("a row names its vector");
        assert!(id.starts_with("D_"), "{id} is not this chain's vector");
    }
}
