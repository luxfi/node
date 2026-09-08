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

use lux_quantumvm::ids::{self, Id};
use lux_quantumvm::vm::{Init, Qvm};
use lux_quantumvm::wire;
use lux_quantumvm::config::Config;
use std::sync::OnceLock;

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
    // sits correctly on its parent needs a chain, which the corpus does not
    // stand up.
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
        Ok(()) => r.syntactic = OK.into(),
        Err(e) => {
            let why = e.to_string();
            r.syntactic = classify(&why).into();
            r.note = why;
        }
    }

    // And what the chain decides. `verify` re-runs the two above and then goes
    // on to the parent, the height, the time and the stamps, so a refusal it
    // reaches before the store is the same word `syntactic` already carries,
    // and one it reaches after is the chain's own answer. That is the boundary
    // the corpus asks for, and it falls out of running the real call rather
    // than being restated here.
    match seeded() {
        Some(vm) => match vm.parse(&bytes) {
            Ok(block) => {
                let id = block.id();
                match vm.verify(&id) {
                    Ok(()) => {
                        r.exec = OK.into();
                        if r.note.is_empty() {
                            r.note = "verified against the seeded chain".into();
                        }
                    }
                    Err(e) => {
                        let why = e.to_string();
                        r.exec = classify(&why).into();
                        if r.note.is_empty() {
                            r.note = why;
                        }
                    }
                }
            }
            Err(e) => {
                r.exec = classify(&e.to_string()).into();
            }
        },
        None => {
            r.exec = INTERNAL.into();
            r.note = "the chain did not start".into();
        }
    }
    r
}

/// A Q-chain over an empty store, its own genesis already the tip.
///
/// One per process: starting it opens a committee, and every vector asks it
/// the same question. Without a tip the parent lookup would refuse every
/// vector alike and the exec field would say nothing.
fn seeded() -> Option<&'static Qvm> {
    static VM: OnceLock<Option<Qvm>> = OnceLock::new();
    VM.get_or_init(|| {
        Qvm::new(Config::default(), Init::memory("q-conformance", chain(), NETWORK)).ok()
    })
    .as_ref()
}
