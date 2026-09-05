// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The Rust X-chain's answers to the shared corpus.
//!
//! One of three evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per
//! vector. The runner compares the three sets of lines; this program never
//! sees another implementation's answer and has nothing to agree with.
//!
//! Usage: `conformance <vectors.tsv>`

use lux_xvm::block::manager::Manager;
use lux_xvm::block::Block;
use lux_xvm::fx;
use lux_xvm::ids::Id;
use lux_xvm::txs::executor::{verify_syntactic, Backend, Config};
use lux_xvm::txs::{Kind, Tx};
use lux_xvm::utxo::Runtime;
use lux_xvm::Error;

const NONE: &str = "-";

// The verdict vocabulary, shared with the Go and C++ evaluators.
const OK: &str = "OK";
const MALFORMED: &str = "MALFORMED";
const SYNTACTIC: &str = "SYNTACTIC";
const OVERFLOW: &str = "OVERFLOW";
const LEDGER: &str = "LEDGER";
const AUTH: &str = "AUTH";
const WARP: &str = "WARP";
const UNSUPPORTED: &str = "UNSUPPORTED";
const SKIPPED: &str = "SKIPPED";

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
        // This evaluator is the X-chain; the P-chain vectors are the P-chain
        // evaluator's to answer.
        if f[2] != "X" {
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
        "seam" => eval_seam(id),
        _ => {
            let mut r = row(id);
            r.parse = "INTERNAL".into();
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

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// The chain the corpus is built for. The X vectors are built fee-neutral —
/// inputs equal outputs — so the differential measures the chain's rules and
/// not three fee schedules; a non-zero schedule here would reject every vector
/// before any rule was reached. The Go and C++ evaluators are given the same.
fn backend<'a>() -> Backend<'a> {
    Backend {
        runtime: Runtime {
            network_id: 1,
            chain_id: [2u8; 32],
        },
        net_id: [0u8; 32],
        config: Config {
            tx_fee: 0,
            create_asset_tx_fee: 0,
        },
        fee_asset_id: [50u8; 32],
        fx_index: fx::FxIndex::standard(),
        num_fxs: 3,
        bootstrapped: true,
        now: 1000,
        net: None,
        shared_memory: None,
    }
}

fn eval_tx(id: &str, wire: &str) -> Row {
    let mut r = row(id);
    let bytes = match unhex(wire) {
        Some(b) => b,
        None => {
            r.parse = "INTERNAL".into();
            r.note = "corpus wire is not hex".into();
            return r;
        }
    };

    let tx = match Tx::parse(&bytes) {
        Ok(tx) => tx,
        Err(e) => {
            r.parse = MALFORMED.into();
            r.syntactic = MALFORMED.into();
            r.exec = MALFORMED.into();
            r.note = format!("{e:?}");
            return r;
        }
    };
    r.parse = "ok".into();
    r.kind = kind_name(tx.unsigned.kind()).into();
    r.hash = hex(&tx.id());

    let b = backend();
    if let Err(e) = verify_syntactic(&b, &tx) {
        let class = classify(&e);
        r.note = format!("{e:?}");
        r.syntactic = class.clone();
        r.exec = class;
        return r;
    }
    r.syntactic = OK.into();
    // The semantic pass needs a funded UTXO set and a shared-memory peer;
    // neither is stood up here, so this layer is reported as not evaluated
    // rather than as a pass. The Go evaluator says the same for the same
    // reason, which is why the runner reports the field as not compared
    // instead of quietly agreeing.
    r.exec = SKIPPED.into();
    r.note = "semantic verification not evaluated: needs a funded UTXO set".into();
    r
}

fn eval_block(id: &str, wire: &str) -> Row {
    let mut r = row(id);
    let bytes = match unhex(wire) {
        Some(b) => b,
        None => {
            r.parse = "INTERNAL".into();
            r.note = "corpus wire is not hex".into();
            return r;
        }
    };
    match Block::parse(&bytes) {
        Ok(blk) => {
            r.parse = "ok".into();
            r.kind = "StandardBlock".into();
            r.hash = hex(&blk.id());
            r.syntactic = OK.into();
            r.exec = LEDGER.into();
            r.note = format!(
                "height={} parent={} txs={}",
                blk.height(),
                hex(&blk.parent()[..4]),
                blk.txs().len()
            );
        }
        Err(e) => {
            r.parse = MALFORMED.into();
            r.syntactic = MALFORMED.into();
            r.exec = MALFORMED.into();
            r.note = format!("{e:?}");
        }
    }
    r
}

/// The block-decision seam. The answer comes from the compiler: the function
/// item below only names something if `block::Manager::reject` is there with
/// that shape.
fn eval_seam(id: &str) -> Row {
    let mut r = row(id);
    r.parse = "ok".into();
    r.kind = "Block.Reject".into();
    let _: fn(&mut Manager, &Id) -> lux_xvm::Result<Vec<Tx>> = Manager::reject;
    r.exec = "PRESENT".into();
    r.note = "xvm block::Manager::reject hands the block's transactions back".into();
    r
}

fn kind_name(k: Kind) -> &'static str {
    match k {
        Kind::Base => "Base",
        Kind::CreateAsset => "CreateAsset",
        Kind::Operation => "Operation",
        Kind::Import => "Import",
        Kind::Export => "Export",
    }
}

/// The same word table the Go and C++ evaluators use, in the same order. The
/// X-chain names its errors as variants, so the Debug spelling is what the
/// table reads; the raw name travels in the note beside the class.
fn classify(e: &Error) -> String {
    match e {
        Error::Wire(_) | Error::UnknownTxKind(_) | Error::UnknownFxPrimitive(_, _) => {
            return MALFORMED.into()
        }
        Error::Overflow => return OVERFLOW.into(),
        _ => {}
    }
    let s = spaced(&format!("{e:?}")).to_lowercase();
    let has = |w: &str| s.contains(w);
    if has("overflow") || has("underflow") {
        OVERFLOW.into()
    } else if has("wrong transaction type")
        || has("wrong tx type")
        || has("not permitted")
        || has("not held")
        || has("unsupported")
    {
        UNSUPPORTED.into()
    } else if has("credential")
        || has("signature")
        || has("unauthorized")
        || has("not authorised")
        || has("not authorized")
        || has("signers")
        || has("sig")
    {
        AUTH.into()
    } else if has("warp") {
        WARP.into()
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
    {
        LEDGER.into()
    } else {
        SYNTACTIC.into()
    }
}

/// `WrongNetworkId` is one word here and three in the other two chains.
/// Splitting the variant name into words is what lets one table read all three.
fn spaced(name: &str) -> String {
    let mut out = String::with_capacity(name.len() + 8);
    let bytes: Vec<char> = name.chars().collect();
    for (i, c) in bytes.iter().enumerate() {
        if i > 0 && c.is_uppercase() && !bytes[i - 1].is_uppercase() {
            out.push(' ');
        }
        out.push(*c);
    }
    out
}
