// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Rust F-chain's answers to the shared corpus.
//!
//! One of three evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per vector.
//! The runner compares the three sets of lines; this program never sees another
//! implementation's answer and has nothing to agree with.
//!
//! THE CHAIN IS PART OF THE QUESTION. An F transaction is judged against a
//! funded payer, a committee, a threshold and a network key, and none of those
//! is in its bytes — they come from genesis. So the corpus carries the genesis
//! as a vector of its own, and this evaluator stands its chain up on THOSE
//! bytes rather than on a reconstruction of them. A reconstruction would be a
//! second opinion about the configuration, and every transaction would then be
//! refused for reasons that had nothing to do with its bytes.
//!
//! AND SO IS THE CHAIN'S NAME. An F signature's preimage is bound to the chain
//! id, and the chain id is not on the wire — each side supplies its own — so an
//! evaluator configured for another chain would answer "invalid payer
//! signature" to every signed vector, which reads like a chain that had lost
//! its verifier rather than one pointed at the wrong network. `F_CHAIN_IDENTITY`
//! asks for the number back, and this answers with the one it was BUILT with,
//! never the one the corpus asked about.
//!
//! Usage: `conformance <vectors.tsv> [repeats]`
//!
//! A repeat count asks for the same work to be done that many times and for the
//! elapsed time of THAT WORK to be reported: the corpus is read before the
//! clock starts and the verdicts are printed after it stops, so what the clock
//! covers is parsing, verification and execution and nothing else. The verdicts
//! are kept rather than dropped, so a round cannot be optimised away, and they
//! are printed once however many rounds ran. The timing line goes to stderr,
//! where the runner does not read: `B <impl> <vectors> <repeats> <seconds>`.

use lux_fhevm::error::{MALFORMED, OK};
use lux_fhevm::id::{self, filled, Id};
use lux_fhevm::vm::{Clock, Config, Vm};
use lux_fhevm::wire;

const NONE: &str = "-";
const INTERNAL: &str = "INTERNAL";

/// The chain the F vectors are built for, and the network it belongs to. Both
/// are corpus contract rather than anything a vector carries.
const CHAIN_BYTE: u8 = 50;
const NETWORK_ID: u32 = 1;

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
    let mut genesis: Option<Vec<u8>> = None;
    let mut genesis_count = 0;
    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split('\t').collect();
        if f.len() != 5 || f[0] != "V" {
            fail(&format!("not a vector line: {line}"));
        }
        // This evaluator is the F-chain; the other chains' vectors are theirs
        // to answer.
        if f[2] != "F" {
            continue;
        }
        if f[3] == "genesis" {
            genesis_count += 1;
            match unhex(f[4]) {
                Some(b) => genesis = Some(b),
                None => fail("the genesis vector is not hex"),
            }
        }
        vectors.push((f[1], f[3], f[4]));
    }

    // Exactly one, or none of the answers mean anything: a second genesis is a
    // second answer to the question of which chain these transactions are being
    // judged on, and last-one-wins would pick it silently.
    if genesis_count > 1 {
        fail(&format!("the corpus names {genesis_count} F genesis vectors"));
    }
    if !vectors.is_empty() && genesis.is_none() {
        fail("the corpus has F vectors and no F genesis");
    }
    let genesis = genesis.unwrap_or_default();

    let mut rows: Vec<Row> = Vec::with_capacity(vectors.len());
    let start = std::time::Instant::now();
    for _ in 0..repeats.max(1) {
        rows.clear();
        for &(id, op, wire) in &vectors {
            rows.push(evaluate(id, op, wire, &genesis));
        }
    }
    let elapsed = start.elapsed().as_secs_f64();

    for r in &rows {
        println!("{r}");
    }
    if repeats > 0 {
        eprintln!("B\trust/fhevm\t{}\t{}\t{:.6}", vectors.len(), repeats, elapsed);
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
    id::unhex(s)
}

fn config() -> Config {
    Config {
        network_id: NETWORK_ID,
        chain_id: filled(CHAIN_BYTE),
        // The wall clock, which is what the reference reads. Nothing in the
        // corpus depends on it: the one rule that reads a time is a permit's
        // expiry, and every vector that reaches it is refused for a lookup
        // first.
        clock: Clock::System,
    }
}

fn evaluate(id: &str, op: &str, wire: &str, genesis: &[u8]) -> Row {
    match op {
        "identity" => identity(id),
        "genesis" => eval_genesis(id, genesis),
        "tx" => eval_tx(id, wire, genesis),
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
    r.hash = lux_fhevm::id::hex(&filled(CHAIN_BYTE));
    r.syntactic = format!("network={NETWORK_ID}");
    r.exec = OK.into();
    r.note = "the identity this evaluator derives ids under".into();
    r
}

/// Stand the chain up on the corpus's own genesis and answer with the id its
/// genesis block took.
///
/// The genesis is not a transaction, so nothing about it is syntactic; what is
/// compared is whether two implementations handed the same configuration reach
/// the same first block. They can fail to: F's block id is hashed from the
/// chain id, the parent, the height, the timestamp and the transactions, and
/// every one of those but the chain id comes from these bytes.
fn eval_genesis(id: &str, genesis: &[u8]) -> Row {
    let mut r = row(id);
    r.parse = "ok".into();
    r.kind = "Genesis".into();
    match Vm::new(config(), genesis) {
        Err(e) => {
            r.syntactic = INTERNAL.into();
            r.exec = INTERNAL.into();
            r.note = format!("cannot stand up an F-chain: {}", e.message());
        }
        Ok(vm) => {
            let last: Id = vm.last_accepted();
            r.hash = lux_fhevm::id::hex(&last);
            r.syntactic = OK.into();
            r.exec = OK.into();
            r.note = "the chain the F vectors are judged on".into();
        }
    }
    r
}

fn eval_tx(id: &str, wire_hex: &str, genesis: &[u8]) -> Row {
    let mut r = row(id);
    let Some(bytes) = unhex(wire_hex) else {
        r.parse = INTERNAL.into();
        r.note = "corpus wire is not hex".into();
        return r;
    };

    let tx = match wire::parse_transaction(&bytes) {
        Ok(tx) => tx,
        Err(e) => {
            r.parse = MALFORMED.into();
            r.syntactic = MALFORMED.into();
            r.exec = MALFORMED.into();
            r.note = e.message();
            return r;
        }
    };
    r.parse = "ok".into();
    r.kind = tx.kind().into();
    r.hash = lux_fhevm::id::hex(&tx.id());

    // What the transaction says about itself, decided without the chain.
    if let Err(e) = tx.syntactic_verify() {
        let class = e.class().to_string();
        r.syntactic = class.clone();
        r.exec = class;
        r.note = e.message();
        return r;
    }
    r.syntactic = OK.into();

    // And what needed the chain: the payer's balance and nonce, the signature
    // over the preimage this chain id binds, and whatever the operation names.
    // Fresh per vector, because admission REMEMBERS — a nonce it took, an
    // effect it claimed — and a corpus whose answers depended on the order it
    // was read in would not be a corpus.
    let mut vm = match Vm::new(config(), genesis) {
        Ok(vm) => vm,
        Err(e) => {
            r.exec = INTERNAL.into();
            r.note = format!("cannot stand up an F-chain: {}", e.message());
            return r;
        }
    };
    match vm.submit(tx) {
        Err(e) => {
            r.exec = e.class().into();
            r.note = e.message();
        }
        Ok(_) => {
            r.exec = OK.into();
            r.note = "admitted by the funded chain".into();
        }
    }
    r
}
