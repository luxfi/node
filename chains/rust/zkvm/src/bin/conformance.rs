// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Rust Z-chain's answers to the shared corpus.
//!
//! One of three evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per vector.
//! The runner compares the three sets of lines; this program never sees another
//! implementation's answer and has nothing to agree with.
//!
//! THE CHAIN'S NAME IS PART OF EVERY ANSWER. A Z block id opens with
//! `sha256(ChainID ‖ NetworkID)` and NEITHER number is on the wire, so both are
//! corpus contract rather than anything a vector carries — as is the genesis
//! timestamp, which is hashed into the genesis block id that every vector names
//! as its parent. An evaluator that picked its own numbers would derive a
//! different id for every well-formed vector on the chain, which is what a hash
//! fork looks like. `Z_CHAIN_IDENTITY` asks the numbers back, and this answers
//! with the ones it was BUILT with, never the ones the corpus asked about.
//!
//! EVERY VECTOR MEETS A FRESH CHAIN, seeded with that genesis and holding no
//! spent note, no output and no accepted block above it, on the chain's own
//! DEFAULT profile — which is the strict-PQ one. The profile is taken by
//! calling [`Config::chain_default`] rather than by setting the bit here, so a
//! change to the chain's default moves this evaluator with it.
//!
//! WHERE THE TWO COMPARED HALVES DIVIDE. The Z-chain has one verification pass,
//! so the split is stated over the RULES — `Error::before_the_chain` — and not
//! over the code that holds them. A refusal reached from the block and its
//! transactions alone is the syntactic verdict AND the execution one; anything
//! that needed the spent set, the proof verifier, the parent or the tip leaves
//! the syntactic verdict OK.
//!
//! Usage: `conformance <vectors.tsv> [repeats]`
//!
//! A repeat count asks for the same work to be done that many times and for the
//! elapsed time of THAT WORK to be reported: the corpus is read before the
//! clock starts and the verdicts are printed after it stops. The verdicts are
//! kept rather than dropped, so a round cannot be optimised away, and they are
//! printed once however many rounds ran. The timing line goes to stderr, where
//! the runner does not read: `B <impl> <vectors> <repeats> <seconds>`.

use lux_zkvm::config::Config;
use lux_zkvm::error::{MALFORMED, OK};
use lux_zkvm::ids;
use lux_zkvm::vm::Zvm;

const NONE: &str = "-";
const INTERNAL: &str = "INTERNAL";

/// The chain the Z vectors are built for, the network it belongs to, and the
/// genesis it is born with. All three are corpus contract.
const CHAIN_BYTE: u8 = 40;
const NETWORK_ID: u32 = 1;
const GENESIS: &[u8] = br#"{"timestamp":1000}"#;

fn usage() -> ! {
    eprintln!("usage: conformance <vectors.tsv> [repeats]");
    std::process::exit(2);
}

fn fail(why: &str) -> ! {
    eprintln!("conformance: {why}");
    std::process::exit(1);
}

fn main() {
    let mut args = std::env::args().skip(1);
    let path = match args.next() {
        Some(p) => p,
        None => usage(),
    };
    // Absent, the repeat count is 0: evaluate the corpus once and say nothing
    // about how long it took, which is what the differential asks for.
    let repeats: u64 = match args.next() {
        None => 0,
        Some(s) => match s.parse() {
            Ok(n) if n >= 1 => n,
            _ => usage(),
        },
    };

    let text = match std::fs::read_to_string(&path) {
        Ok(t) => t,
        Err(e) => fail(&format!("{path}: {e}")),
    };

    // Reading the corpus, before the clock starts.
    let mut vectors: Vec<(&str, &str, &str)> = Vec::new();
    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split('\t').collect();
        if f.len() != 5 || f[0] != "V" {
            fail(&format!("not a vector line: {line}"));
        }
        // This evaluator is the Z-chain; the other chains' vectors are theirs
        // to answer.
        if f[2] != "Z" {
            continue;
        }
        vectors.push((f[1], f[3], f[4]));
    }

    let mut rows: Vec<Row> = Vec::with_capacity(vectors.len());
    let start = std::time::Instant::now();
    for _ in 0..repeats.max(1) {
        rows.clear();
        for &(id, op, wire) in &vectors {
            rows.push(evaluate(id, op, wire));
        }
    }
    let elapsed = start.elapsed().as_secs_f64();

    for r in &rows {
        println!("{r}");
    }
    if repeats > 0 {
        eprintln!("B\trust/zkvm\t{}\t{}\t{:.6}", vectors.len(), repeats, elapsed);
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

fn unhex(s: &str) -> Option<Vec<u8>> {
    if s == NONE {
        return Some(Vec::new());
    }
    if s.len() % 2 != 0 {
        return None;
    }
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(s.len() / 2);
    for pair in b.chunks(2) {
        let hi = (pair[0] as char).to_digit(16)?;
        let lo = (pair[1] as char).to_digit(16)?;
        out.push((hi * 16 + lo) as u8);
    }
    Some(out)
}

fn chain() -> lux_zkvm::Result<Zvm> {
    Zvm::new(
        Config::chain_default(),
        ids::repeated(CHAIN_BYTE),
        NETWORK_ID,
        GENESIS,
    )
}

fn evaluate(id: &str, op: &str, wire: &str) -> Row {
    match op {
        "identity" => identity(id),
        "block" => eval_block(id, wire),
        _ => {
            let mut r = row(id);
            r.parse = INTERNAL.into();
            r.note = format!("unknown op {op}");
            r
        }
    }
}

/// The identity THIS evaluator derives ids under — never the one the corpus
/// asked about. A row that echoed the question would agree with every
/// implementation, including one configured for another chain entirely.
fn identity(id: &str) -> Row {
    let mut r = row(id);
    r.parse = "ok".into();
    r.kind = "ChainIdentity".into();
    r.hash = ids::hex(&ids::repeated(CHAIN_BYTE));
    r.syntactic = format!("network={NETWORK_ID}");
    r.exec = OK.into();
    r.note = "the identity this evaluator derives ids under".into();
    r
}

fn eval_block(id: &str, wire_hex: &str) -> Row {
    let mut r = row(id);
    let Some(bytes) = unhex(wire_hex) else {
        r.parse = INTERNAL.into();
        r.note = "corpus wire is not hex".into();
        return r;
    };

    // Fresh chain per vector: each block is judged on its own against a chain
    // that has accepted nothing past genesis, so no vector can be answered
    // differently because of one that ran before it.
    let mut vm = match chain() {
        Ok(vm) => vm,
        Err(e) => {
            r.parse = INTERNAL.into();
            r.note = format!("cannot stand up a Z-chain: {e}");
            return r;
        }
    };

    let block = match vm.parse_block(&bytes) {
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
    r.kind = block.carries();
    r.hash = ids::hex(&block.id());

    match vm.verify(&block) {
        Ok(()) => {
            r.syntactic = OK.into();
            r.exec = OK.into();
            r.note = "verified against the seeded chain".into();
        }
        Err(e) => {
            let class = e.class();
            // A refusal reached from the block alone is BOTH verdicts; one that
            // needed the chain leaves the syntactic half standing.
            r.syntactic = if e.before_the_chain() { class.into() } else { OK.into() };
            r.exec = class.into();
            r.note = e.to_string();
        }
    }
    r
}
