// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Rust Q-chain's answers to the shared corpus.
//!
//! One of the evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per
//! vector. The runner compares their lines; this program never sees another
//! implementation's answer and has nothing to agree with.
//!
//! Usage: `conformance <vectors.tsv>`
//!
//! It answers the `Q` rows and stays silent on every other chain's, because a
//! row is a claim and this program has no claim to make about a P-chain
//! transaction.

use lux_quantumvm::ids;
use lux_quantumvm::wire;

const NONE: &str = "-";

// The verdict vocabulary, shared with the Go and C++ evaluators. A word here
// means the same thing in all three, which is what lets the runner compare
// strings instead of understanding rules.
const OK: &str = "OK";
const MALFORMED: &str = "MALFORMED";
const SYNTACTIC: &str = "SYNTACTIC";
const SKIPPED: &str = "SKIPPED";
const INTERNAL: &str = "INTERNAL";

fn main() {
    let path = std::env::args().nth(1).unwrap_or_else(|| {
        eprintln!("usage: conformance <vectors.tsv>");
        std::process::exit(2);
    });
    let text = match std::fs::read_to_string(&path) {
        Ok(t) => t,
        Err(e) => {
            eprintln!("conformance: {path}: {e}");
            std::process::exit(1);
        }
    };

    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split('\t').collect();
        if f.len() != 5 || f[0] != "V" {
            eprintln!("conformance: not a vector line: {line}");
            std::process::exit(1);
        }
        if f[2] != "Q" {
            continue;
        }
        println!("{}", evaluate(f[1], f[3], f[4]));
    }
}

struct Row {
    id: String,
    parse: String,
    kind: String,
    hash: String,
    syntactic: String,
    exec: String,
    note: String,
}

impl std::fmt::Display for Row {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let note = self.note.replace(['\t', '\n'], " ");
        let note = if note.is_empty() { NONE } else { &note };
        write!(
            f,
            "R\t{}\t{}\t{}\t{}\t{}\t{}\t{}",
            self.id, self.parse, self.kind, self.hash, self.syntactic, self.exec, note
        )
    }
}

fn row(id: &str) -> Row {
    Row {
        id: id.to_string(),
        parse: NONE.into(),
        kind: NONE.into(),
        hash: NONE.into(),
        syntactic: NONE.into(),
        exec: NONE.into(),
        note: String::new(),
    }
}

fn evaluate(id: &str, op: &str, wire: &str) -> Row {
    match op {
        "tx" => eval_tx(id, wire),
        "block" => eval_block(id, wire),
        _ => {
            let mut r = row(id);
            r.parse = INTERNAL.into();
            r.note = format!("unknown op {op}");
            r
        }
    }
}

fn unhex(s: &str) -> Option<Vec<u8>> {
    if s == NONE {
        return Some(Vec::new());
    }
    if !s.len().is_multiple_of(2) {
        return None;
    }
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok())
        .collect()
}

/// A Q-chain transaction arrives in one of two shapes: the ENVELOPE that rides
/// in a block, and the PREIMAGE the envelope wraps — which is also what the
/// signature covers and what the transaction's id is the hash of. The envelope
/// is tried first, because it is the shape a peer sends.
fn eval_tx(id: &str, wire: &str) -> Row {
    let mut r = row(id);
    let bytes = match unhex(wire) {
        Some(b) => b,
        None => {
            r.parse = INTERNAL.into();
            r.note = "corpus wire is not hex".into();
            return r;
        }
    };

    let (tx, kind) = match wire::parse_tx_envelope(&bytes) {
        Ok(tx) => (tx, "QuantumEnvelope"),
        Err(envelope_err) => match wire::parse_tx_body(&bytes) {
            Ok(tx) => (tx, "QuantumBaseTx"),
            Err(_) => {
                r.parse = MALFORMED.into();
                r.syntactic = MALFORMED.into();
                r.exec = MALFORMED.into();
                r.note = envelope_err.to_string();
                return r;
            }
        },
    };

    r.parse = "ok".into();
    r.kind = kind.into();
    r.hash = ids::hex(&tx.id());

    // A Q-chain transaction is an attestation, so one with no stamp attests
    // nothing. That is the whole of its own well-formedness.
    match tx.verify() {
        Ok(()) => r.syntactic = OK.into(),
        Err(e) => {
            r.syntactic = SYNTACTIC.into();
            r.note = e.to_string();
        }
    }

    // Whether the stamp itself checks out is decided against a CLOCK: a stamp
    // is good only inside its window. A recorded corpus stamp is as old as the
    // corpus, so the answer would change with the calendar rather than with
    // the rules. Reported as not evaluated, which the runner excludes from
    // comparison — never as a pass.
    r.exec = SKIPPED.into();
    if r.note.is_empty() {
        r.note = "the stamp check is not evaluated: a recorded stamp is outside its window".into();
    }
    r
}

fn eval_block(id: &str, wire: &str) -> Row {
    let mut r = row(id);
    let bytes = match unhex(wire) {
        Some(b) => b,
        None => {
            r.parse = INTERNAL.into();
            r.note = "corpus wire is not hex".into();
            return r;
        }
    };

    let block = match wire::parse_block(&bytes) {
        Ok(b) => b,
        Err(e) => {
            r.parse = MALFORMED.into();
            r.syntactic = MALFORMED.into();
            r.exec = MALFORMED.into();
            r.note = e.to_string();
            return r;
        }
    };

    r.parse = "ok".into();
    r.kind = "QuantumBlock".into();
    r.hash = ids::hex(&block.id());

    // What can be decided from the block alone: it is not genesis, it carries
    // work, and it fits on the wire. Whether it sits correctly on its parent
    // needs a chain, which the corpus does not stand up.
    match block.well_formed() {
        Ok(()) => r.syntactic = OK.into(),
        Err(e) => {
            r.syntactic = SYNTACTIC.into();
            r.note = e.to_string();
        }
    }

    r.exec = SKIPPED.into();
    if r.note.is_empty() {
        r.note = "acceptance is not evaluated: it needs a chain with a tip".into();
    }
    r
}
