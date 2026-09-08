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

use lux_quantumvm::config::Config;
use lux_quantumvm::ids::{self, Id};
use lux_quantumvm::vm::{Init, Qvm};
use lux_quantumvm::wire;

const NONE: &str = "-";

// The verdict vocabulary, shared with the Go and C++ evaluators. A word here
// means the same thing in all three, which is what lets the runner compare
// strings instead of understanding rules.
const OK: &str = "OK";
const MALFORMED: &str = "MALFORMED";
const SYNTACTIC: &str = "SYNTACTIC";
const OVERFLOW: &str = "OVERFLOW";
const LEDGER: &str = "LEDGER";
const AUTH: &str = "AUTH";
const WARP: &str = "WARP";
const UNSUPPORTED: &str = "UNSUPPORTED";
const SKIPPED: &str = "SKIPPED";
const INTERNAL: &str = "INTERNAL";

// The chain this evaluator answers for.
//
// Q carries the pair ON the wire — a block names its chain and its network —
// and it also REFUSES a block whose pair is not the one the node serves. So the
// node's own identity decides the verdict of every well-formed vector, and it
// is corpus contract rather than this file's private choice. It is asked back
// as Q_CHAIN_IDENTITY, and the same two constants feed both that row and the
// binding check below: an evaluator pointed at the wrong chain says so in one
// row instead of reporting "belongs to another chain" eighty times.
const CHAIN_BYTE: u8 = 30;
const NETWORK: u32 = 1;

fn chain() -> Id {
    ids::filled(CHAIN_BYTE)
}

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
        "identity" => eval_identity(id),
        _ => {
            let mut r = row(id);
            r.parse = INTERNAL.into();
            r.note = format!("unknown op {op}");
            r
        }
    }
}

/// The identity THIS evaluator derives ids under, never the one the corpus
/// asked about. A row that echoed the question would agree with an evaluator
/// built for another chain, which is the disagreement it exists to surface.
fn eval_identity(id: &str) -> Row {
    let mut r = row(id);
    r.parse = "ok".into();
    r.kind = "ChainIdentity".into();
    r.hash = ids::hex(&chain());
    r.syntactic = format!("network={NETWORK}");
    r.exec = OK.into();
    r.note = "the identity this evaluator derives ids under".into();
    r
}

/// The same word table the Go and C++ evaluators carry, in the same order, so
/// that one refusal means one class in all three. The order is load-bearing: a
/// refusal that names a kind the chain does not run usually lists the ones it
/// does, and those lists carry ledger words.
fn classify(s: &str) -> &'static str {
    let s = s.to_lowercase();
    let has = |w: &str| s.contains(w);
    if has("overflow") || has("underflow") {
        OVERFLOW
    } else if has("wrong transaction type")
        || has("wrong tx type")
        || has("not permitted")
        || has("not held")
        || has("unsupported")
        || has("forbidden")
        || has("unknown")
        || has("parameter set")
    {
        UNSUPPORTED
    } else if has("credential")
        || has("signature")
        || has("unauthorized")
        || has("not authorised")
        || has("not authorized")
        || has("proof verification failed")
        || has("does not match auth")
    {
        AUTH
    } else if has("warp") {
        WARP
    } else if has("utxo")
        || has("funds")
        || has("insufficient")
        || has("burn")
        || has("consumed")
        || has("produced")
        || has("flow")
        || has("fee")
        || has("not found")
        || has("doesn't exist")
        || has("does not exist")
        || has("isn't a current")
        || has("not validator")
        || has("no such")
        || has("could not load")
        || has("shared memory")
        || has("state root")
        || has("precedes its parent")
        || has("skew allowance")
    {
        LEDGER
    } else {
        SYNTACTIC
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
            let why = e.to_string();
            r.syntactic = classify(&why).into();
            r.note = why;
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

    // What can be decided from the block alone, in the order the chain's own
    // `verify` decides it: the block belongs to this chain and this network,
    // then it is not genesis, carries work, and fits on the wire. Whether it
    // sits correctly on its parent needs a chain, which `accept` stands up.
    //
    // The binding comes first because it comes first in `Qvm::verify`, and
    // because the two questions are not the same one: a block of another chain
    // is well formed, and a chain that only asked the second question would
    // accept every peer's chain as its own.
    //
    // These two are also exactly what `verify` settles before it looks for the
    // parent, which is this chain's first read of the store — so they are the
    // whole of `syntactic` here. That question is the corpus's, not this file's,
    // and it is defined once in conformance/README.md under "`syntactic` where
    // verify is one pass".
    match block
        .on_chain(chain(), NETWORK)
        .and_then(|()| block.well_formed())
    {
        Err(e) => {
            // A refusal reached before the first read of the store answers BOTH
            // fields — the block never got far enough for them to differ. That
            // is the corpus's rule, in conformance/README.md under "`syntactic`
            // where verify is one pass", and not a second opinion about the
            // block: there is one verdict here and it is written down twice.
            //
            // Declining `exec` here was wrong twice over. It reported a gap
            // where the answer was already in hand, and the note it left behind
            // was the syntactic refusal, so the decline came with a reason that
            // was about a different field.
            let why = e.to_string();
            let verdict: String = classify(&why).into();
            r.syntactic = verdict.clone();
            r.exec = verdict;
            r.note = why;
        }
        Ok(()) => {
            r.syntactic = OK.into();
            // Past that boundary is the parent, so the chain that holds one is
            // stood up and asked. It costs a store with a genesis in it, which
            // is a thing this crate hands out — the tip the answer needs is not
            // a fixture the corpus has to carry.
            match accept(&bytes) {
                Ok(()) => {
                    r.exec = OK.into();
                    r.note = "verified against the seeded chain".into();
                }
                Err(why) => {
                    r.exec = classify(&why).into();
                    r.note = why;
                }
            }
        }
    }
    r
}

/// Whether the chain would build on this block.
///
/// A chain stood up FRESH for the block, over its own in-memory store, seeded
/// with the genesis `Qvm::new` writes — so what refuses is the rule that
/// refuses rather than a chain that happened to hold something another
/// evaluator's did not. `Qvm::verify` is the whole of the question and reads
/// the store without writing to it; the same arrangement the Go and C++
/// evaluators answer this field from.
fn accept(bytes: &[u8]) -> std::result::Result<(), String> {
    let init = Init::memory("conformance", chain(), NETWORK);
    // The chain's OWN defaults, asked for rather than assembled here. The one
    // that matters is that stamps are checked at all: a chain configured with
    // that off accepts an expired stamp, an unsupported parameter set and a
    // duplicate transaction alike, and nothing downstream would catch it.
    let vm = Qvm::new(Config::default(), init).map_err(|e| e.to_string())?;
    let block = vm.parse(bytes).map_err(|e| e.to_string())?;
    vm.verify(&block.id()).map_err(|e| e.to_string())
}
