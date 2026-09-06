// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Rust D-chain's answers to the shared corpus.
//!
//! One of the evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per vector.
//! The runner compares their lines; this program never sees another
//! implementation's answer and has nothing to agree with.
//!
//! Usage: `conformance <vectors.tsv>`
//!
//! It answers the `D` rows and stays silent on every other chain's, because a
//! row is a claim and this program has no claim to make about a P-chain
//! transaction.
//!
//! D IS NOT A BLOCK CHAIN HERE, so a D vector's wire column carries the
//! ARGUMENTS to a derivation rather than a serialization: pipe-separated ASCII,
//! hex-encoded because it travels the column a block's bytes travel. The op says
//! which function is being asked.
//!
//! ```text
//! assetid   networkID | sourceChainHex | kindToken | refHex
//! marketid  networkID | baseHex | quoteHex | venueHex
//! kind      token
//! mode      token
//! class     networkID
//! guard     valueEnabled | modeToken | capsOn | realAssetsOnly | haltReady
//! ```

use lux_dexvm::asset::{derive_asset_id, market_id, AssetKind};
use lux_dexvm::error::Error;
use lux_dexvm::ids::{self, Id};
use lux_dexvm::mode::{guard_value_activation, ConsensusMode, LaunchAssertions};
use lux_dexvm::network::network_class_for;

const NONE: &str = "-";

// The verdict vocabulary, shared with the Go and C++ evaluators. A word here
// means the same thing in all three, which is what lets the runner compare
// strings instead of understanding rules.
const OK: &str = "OK";
const SYNTACTIC: &str = "SYNTACTIC";
const OVERFLOW: &str = "OVERFLOW";
const LEDGER: &str = "LEDGER";
const AUTH: &str = "AUTH";
const WARP: &str = "WARP";
const UNSUPPORTED: &str = "UNSUPPORTED";
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
        if f[2] != "D" {
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
        let mut note = self.note.replace(['\t', '\n'], " ");
        note.truncate(floor_char_boundary(&note, 160));
        let note = if note.is_empty() { NONE } else { &note };
        write!(
            f,
            "R\t{}\t{}\t{}\t{}\t{}\t{}\t{}",
            self.id, self.parse, self.kind, self.hash, self.syntactic, self.exec, note
        )
    }
}

/// The largest index at or below `at` that a `String` may be cut at. The
/// reference trims a note to 160 BYTES; cutting a multi-byte character in half
/// would panic here, and the refusal that carries an em dash is one of the
/// notes this trims.
fn floor_char_boundary(s: &str, at: usize) -> usize {
    if at >= s.len() {
        return s.len();
    }
    let mut i = at;
    while !s.is_char_boundary(i) {
        i -= 1;
    }
    i
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
    let mut r = row(id);
    // The arguments are ASCII inside the hex, so the column is decoded once and
    // then split. Splitting the column itself would find one field whose text is
    // "4552433230".
    let Some(raw) = ids::unhex(wire_or_empty(wire)) else {
        return bad_input(r, "corpus wire is not hex");
    };
    let Ok(text) = String::from_utf8(raw) else {
        return bad_input(r, "corpus wire is not an argument list");
    };
    let args: Vec<&str> = text.split('|').collect();
    r.parse = "ok".into();

    match op {
        "assetid" => eval_assetid(r, &args),
        "marketid" => eval_marketid(r, &args),
        "kind" => eval_kind(r, &args),
        "mode" => eval_mode(r, &args),
        "class" => eval_class(r, &args),
        "guard" => eval_guard(r, &args),
        _ => {
            r.parse = INTERNAL.into();
            r.syntactic = INTERNAL.into();
            r.exec = INTERNAL.into();
            r.note = format!("unknown op {op}");
            r
        }
    }
}

/// A vector with no bytes writes `-`, which is an empty argument list rather
/// than an unreadable one.
fn wire_or_empty(wire: &str) -> &str {
    if wire == NONE {
        ""
    } else {
        wire
    }
}

/// A vector whose ARGUMENTS do not read. That is this evaluator's failure, not
/// the chain's, and it is reported as such rather than as a refusal the chain
/// never made.
fn bad_input(mut r: Row, why: &str) -> Row {
    r.parse = INTERNAL.into();
    r.syntactic = INTERNAL.into();
    r.exec = INTERNAL.into();
    r.note = why.into();
    r
}

fn eval_assetid(mut r: Row, a: &[&str]) -> Row {
    r.kind = "AssetID".into();
    if a.len() != 4 {
        return bad_input(r, &format!("assetid wants 4 arguments, got {}", a.len()));
    }
    let Some(network) = u32_of(a[0]) else {
        return bad_input(r, &format!("network id: {}", a[0]));
    };
    let Some(chain) = id_of(a[1]) else {
        return bad_input(r, &format!("source chain: {}", a[1]));
    };
    let Some(reference) = ids::unhex(a[3]) else {
        return bad_input(r, &format!("reference: {}", a[3]));
    };

    // The kind token goes through the registry's own parser, so a token it
    // refuses is refused at the layer a manifest would meet it.
    let kind = match a[2].parse::<AssetKind>() {
        Ok(k) => k,
        Err(e) => return refused_at_syntax(r, &e),
    };
    r.syntactic = OK.into();

    match derive_asset_id(network, chain, kind, &reference) {
        Ok(asset) => {
            r.hash = ids::hex(&asset);
            r.exec = OK.into();
            r.note = "derived".into();
        }
        Err(e) => {
            r.exec = classify(&e.sentinel()).into();
            r.note = e.to_string();
        }
    }
    r
}

fn eval_marketid(mut r: Row, a: &[&str]) -> Row {
    r.kind = "MarketID".into();
    if a.len() != 4 {
        return bad_input(r, &format!("marketid wants 4 arguments, got {}", a.len()));
    }
    let Some(network) = u32_of(a[0]) else {
        return bad_input(r, &format!("network id: {}", a[0]));
    };
    let Some(base) = id_of(a[1]) else {
        return bad_input(r, &format!("base asset: {}", a[1]));
    };
    let Some(quote) = id_of(a[2]) else {
        return bad_input(r, &format!("quote asset: {}", a[2]));
    };
    let Some(venue) = ids::unhex(a[3]) else {
        return bad_input(r, &format!("venue: {}", a[3]));
    };

    r.syntactic = OK.into();
    r.hash = ids::hex(&market_id(network, base, quote, &venue));
    r.exec = OK.into();
    r.note = "derived".into();
    r
}

fn eval_kind(mut r: Row, a: &[&str]) -> Row {
    r.kind = "AssetKind".into();
    if a.len() != 1 {
        return bad_input(r, &format!("kind wants 1 argument, got {}", a.len()));
    }
    match a[0].parse::<AssetKind>() {
        Ok(k) => {
            r.syntactic = OK.into();
            r.exec = OK.into();
            r.note = k.to_string();
            r
        }
        Err(e) => refused_at_syntax(r, &e),
    }
}

fn eval_mode(mut r: Row, a: &[&str]) -> Row {
    r.kind = "ConsensusMode".into();
    if a.len() != 1 {
        return bad_input(r, &format!("mode wants 1 argument, got {}", a.len()));
    }
    match a[0].parse::<ConsensusMode>() {
        Ok(m) => {
            r.syntactic = OK.into();
            r.exec = OK.into();
            r.note = m.to_string();
            r
        }
        Err(e) => refused_at_syntax(r, &e),
    }
}

fn eval_class(mut r: Row, a: &[&str]) -> Row {
    r.kind = "NetworkClass".into();
    if a.len() != 1 {
        return bad_input(r, &format!("class wants 1 argument, got {}", a.len()));
    }
    let Some(network) = u32_of(a[0]) else {
        return bad_input(r, &format!("network id: {}", a[0]));
    };
    r.syntactic = OK.into();
    r.exec = OK.into();
    r.note = network_class_for(network).to_string();
    r
}

fn eval_guard(mut r: Row, a: &[&str]) -> Row {
    r.kind = "ValueActivation".into();
    if a.len() != 5 {
        return bad_input(r, &format!("guard wants 5 arguments, got {}", a.len()));
    }
    let Some(enabled) = bool_of(a[0]) else {
        return bad_input(r, &format!("value enabled: {}", a[0]));
    };
    let Some(caps_on) = bool_of(a[2]) else {
        return bad_input(r, &format!("caps: {}", a[2]));
    };
    let Some(real_assets_only) = bool_of(a[3]) else {
        return bad_input(r, &format!("real assets: {}", a[3]));
    };
    let Some(halt_ready) = bool_of(a[4]) else {
        return bad_input(r, &format!("halt ready: {}", a[4]));
    };

    // An unparseable mode token is UNSET, because that is what a caller who
    // declared nothing legible has declared — and UNSET is refused. Reading it
    // as an error here would report the parse rather than the guard.
    let mode = a[1].parse::<ConsensusMode>().unwrap_or_default();
    r.syntactic = OK.into();

    match guard_value_activation(
        enabled,
        mode,
        LaunchAssertions {
            caps_on,
            real_assets_only,
            halt_ready,
        },
    ) {
        Ok(status) => {
            r.exec = OK.into();
            r.note = if status.status.is_empty() {
                format!("{} (no disclaimer)", status.mode)
            } else {
                format!("{} {}", status.mode, status.status)
            };
        }
        Err(e) => {
            r.exec = classify(&e.sentinel()).into();
            r.note = e.to_string();
        }
    }
    r
}

/// A refusal the chain reached on the arguments' own shape, before it derived
/// anything. Both layers carry it, exactly as the reference does: a token that
/// is not a kind is refused at the layer a manifest would meet it, and there is
/// nothing left for the derivation to answer.
fn refused_at_syntax(mut r: Row, e: &Error) -> Row {
    r.syntactic = classify(&e.sentinel()).into();
    r.exec = r.syntactic.clone();
    r.note = e.to_string();
    r
}

// Reading one argument. These are the HARNESS's readers, not the chain's: what
// they refuse becomes INTERNAL, a loud non-answer the runner reports, never a
// verdict. So they accept exactly what the generator writes — decimal digits,
// `true`/`false`, an even run of hex — and nothing else. Go's readers are
// looser here (`ParseBool` also takes `1` and `t`, `ParseUint` also takes a
// leading `+`), and the looser direction is the wrong one for a reader: it
// would answer a verdict for an argument list nobody wrote.

fn u32_of(s: &str) -> Option<u32> {
    if s.is_empty() || !s.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    s.parse::<u32>().ok()
}

fn id_of(s: &str) -> Option<Id> {
    ids::from_slice(&ids::unhex(s)?)
}

fn bool_of(s: &str) -> Option<bool> {
    match s {
        "true" => Some(true),
        "false" => Some(false),
        _ => None,
    }
}

/// The same word table the Go and C++ evaluators carry, in the same order, so
/// that one refusal means one class in all three.
///
/// It is handed the refusal's SENTINEL, never its sentence. The two differ on
/// purpose here: `canonical reference does not match asset kind: UTXO assetID
/// must be 32 bytes` is a refusal about a reference's shape whose detail
/// contains the word `utxo`, and the ledger arm would claim it. Go reads the
/// same half — `classify` unwraps to the root error before it reads any words.
///
/// The order is load-bearing for the same kind of reason: a refusal that names a
/// kind the chain does not run usually lists the ones it does, and those lists
/// carry ledger words.
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
