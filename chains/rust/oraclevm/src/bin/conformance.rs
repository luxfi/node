// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Rust O-chain's answers to the shared corpus.
//!
//! One of three evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per
//! vector. The runner compares the three sets of lines; this program never
//! sees another implementation's answer and has nothing to agree with.
//!
//! WHAT IS ANSWERED. Six ops, and each is a different plane of the chain:
//! `block` is the JSON wire and the id taken over its re-marshal, `genesis`
//! is the configuration the observation vectors are judged under, `requestid`
//! is the derivation that gives a request its name, and `request`, `commit`
//! and `observation` are the three places this chain refuses anything.
//!
//! WHERE THE TWO COMPARED HALVES DIVIDE. The corpus's rule, from
//! conformance/README.md, is that `syntactic` is the verdict a single
//! verification pass reached BEFORE it read the chain. On O there is exactly
//! one such refusal — a request whose id its own arguments do not derive,
//! checked ahead of the request map — and everything else this chain refuses
//! is past a lookup and leaves `syntactic` OK. `Block::Verify` is
//! unconditional in the reference, so a block that parses answers OK to both.
//!
//! Usage: `conformance <vectors.tsv> [repeats]`
//!
//! A repeat count asks for the same work that many times and reports the
//! elapsed time of THAT WORK on stderr, where the runner does not read.

use lux_oraclevm::gojson;
use lux_oraclevm::ids;
use lux_oraclevm::types::{Block, Genesis, Observation, OracleRecord, OracleRequest, KIND_READ, KIND_WRITE};
use lux_oraclevm::vm::{compute_request_id, Vm};

const NONE: &str = "-";
const INTERNAL: &str = "INTERNAL";
const OK: &str = "OK";
const MALFORMED: &str = "MALFORMED";
const SYNTACTIC: &str = "SYNTACTIC";
const OVERFLOW: &str = "OVERFLOW";
const LEDGER: &str = "LEDGER";
const AUTH: &str = "AUTH";
const WARP: &str = "WARP";
const UNSUPPORTED: &str = "UNSUPPORTED";

/// The chain this evaluator was built for, and the network it belongs to.
/// Both are corpus contract; `O_CHAIN_IDENTITY` asks them back and this
/// answers with the ones it HOLDS, never the ones the corpus asked about.
const CHAIN_BYTE: u8 = 79; // 'O'
const NETWORK_ID: u32 = 1;

/// The feeds every observation vector is judged against. These bytes are the
/// corpus's `O_GENESIS` vector, and they are here so that an implementation
/// seeded differently is named by that row rather than by the four rows
/// underneath it.
const GENESIS: &str = concat!(
    r#"{"version":1,"message":"conformance","timestamp":1000,"#,
    r#""initialFeeds":[{"id":"2Kw2XL8QVSQHwJYKpWLktaxtwKuz7iYF5pqcauUHpmcrSjRPp","#,
    r#""name":"lux-usd","description":"","sources":null,"updateFreq":0,"#,
    r#""policyHash":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"#,
    r#""operators":["NodeID-6Jswqk47s9PUcyCc88MMVwzgvHQxfLnA"],"#,
    r#""createdAt":"1970-01-01T00:00:05Z","status":"active","metadata":null}]}"#
);

fn usage() -> ! {
    eprintln!("usage: conformance <vectors.tsv> [repeats]");
    std::process::exit(2);
}

fn fail(why: &str) -> ! {
    eprintln!("conformance: {why}");
    std::process::exit(1);
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

impl Row {
    fn new(id: &str) -> Row {
        Row {
            id: id.to_owned(),
            parse: String::new(),
            kind: NONE.to_owned(),
            hash: NONE.to_owned(),
            syntactic: NONE.to_owned(),
            exec: NONE.to_owned(),
            note: String::new(),
        }
    }

    fn malformed(mut self, why: &str) -> Row {
        self.parse = MALFORMED.to_owned();
        self.syntactic = MALFORMED.to_owned();
        self.exec = MALFORMED.to_owned();
        self.note = why.to_owned();
        self
    }

    fn internal(mut self, why: &str) -> Row {
        self.parse = INTERNAL.to_owned();
        self.syntactic = INTERNAL.to_owned();
        self.exec = INTERNAL.to_owned();
        self.note = why.to_owned();
        self
    }
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

/// The shared verdict vocabulary, reached from this implementation's own
/// refusal words. The table is the same table in the same ORDER as the Go and
/// C++ evaluators carry, because the order is the rule: a message that refuses
/// an unknown kind usually lists the kinds it would accept, so the words that
/// name a refusal-by-name are tested before the words that name a ledger miss.
fn classify(msg: &str) -> &'static str {
    let s = msg.to_lowercase();
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

fn trim(s: &str) -> String {
    let s = s.replace('\n', " ");
    if s.len() > 160 {
        s[..160].to_owned()
    } else {
        s
    }
}

fn seeded_vm() -> Result<Vm, String> {
    let v = gojson::parse(GENESIS.as_bytes())?;
    let g = Genesis::read(&v)?;
    Ok(Vm::seeded(g.initial_feeds.as_deref().unwrap_or(&[])))
}

fn main() {
    let mut args = std::env::args().skip(1);
    let path = match args.next() {
        Some(p) => p,
        None => usage(),
    };
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

    let mut vectors: Vec<(&str, &str, &str)> = Vec::new();
    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split('\t').collect();
        if f.len() != 5 || f[0] != "V" {
            fail(&format!("not a vector line: {line}"));
        }
        // This evaluator is the O-chain; the other chains' vectors are theirs
        // to answer.
        if f[2] != "O" {
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
        eprintln!("B\trust/oraclevm\t{}\t{}\t{:.6}", vectors.len(), repeats, elapsed);
    }
}

fn wire_of(id: &str, wire: &str) -> Result<Vec<u8>, Row> {
    if wire == NONE {
        return Ok(Vec::new());
    }
    hex::decode(wire).map_err(|_| Row::new(id).internal("corpus wire is not hex"))
}

fn evaluate(id: &str, op: &str, wire: &str) -> Row {
    let bytes = match wire_of(id, wire) {
        Ok(b) => b,
        Err(r) => return r,
    };
    match op {
        "identity" => identity(id),
        "block" => block(id, &bytes),
        "genesis" => genesis(id, &bytes),
        "requestid" => request_id(id, &bytes),
        "request" => request(id, &bytes),
        "commit" => commit(id, &bytes),
        "observation" => observation(id, &bytes),
        _ => Row::new(id).internal(&format!("unknown O op {op}")),
    }
}

/// The identity THIS evaluator holds, never the one the corpus asked about: a
/// row that echoed the question would agree with an evaluator built for
/// another chain entirely.
fn identity(id: &str) -> Row {
    let mut chain = ids::EMPTY;
    for b in chain.iter_mut() {
        *b = CHAIN_BYTE;
    }
    let mut r = Row::new(id);
    r.parse = "ok".into();
    r.kind = "ChainIdentity".into();
    r.hash = hex::encode(chain);
    r.syntactic = format!("network={NETWORK_ID}");
    r.exec = OK.into();
    r.note = "the identity this evaluator derives ids under".into();
    r
}

fn block(id: &str, bytes: &[u8]) -> Row {
    let mut r = Row::new(id);
    let v = match gojson::parse(bytes) {
        Ok(v) => v,
        Err(e) => return r.malformed(&trim(&e)),
    };
    let blk = match Block::read(&v) {
        Ok(b) => b,
        Err(e) => return r.malformed(&trim(&e)),
    };
    r.parse = "ok".into();
    r.kind = kind_of(&blk);
    r.hash = hex::encode(blk.compute_id());
    // The reference's Verify is unconditional, so a block that parses is a
    // block this chain accepts. Anything else printed here would be this
    // port's opinion rather than the chain's.
    r.syntactic = OK.into();
    r.exec = OK.into();
    let (o, a, f, t) = blk.count();
    r.note = format!(
        "height={} parent={} obs={} agg={} feeds={} att={}",
        blk.height,
        hex::encode(&blk.parent_id[..4]),
        o,
        a,
        f,
        t
    );
    r
}

/// What a block CARRIES. The chain has one block type, so naming the type
/// would compare a constant against itself.
fn kind_of(b: &Block) -> String {
    let (o, a, f, t) = b.count();
    let mut parts: Vec<String> = Vec::new();
    let mut add = |name: &str, n: usize| match n {
        0 => {}
        1 => parts.push(name.to_owned()),
        n => parts.push(format!("{name}x{n}")),
    };
    add("Observation", o);
    add("Aggregation", a);
    add("FeedUpdate", f);
    add("Attestation", t);
    if parts.is_empty() {
        "Empty".to_owned()
    } else {
        parts.join("+")
    }
}

fn genesis(id: &str, bytes: &[u8]) -> Row {
    let mut r = Row::new(id);
    let v = match gojson::parse(bytes) {
        Ok(v) => v,
        Err(e) => return r.malformed(&trim(&e)),
    };
    let g = match Genesis::read(&v) {
        Ok(g) => g,
        Err(e) => return r.malformed(&trim(&e)),
    };
    r.parse = "ok".into();
    r.kind = "Genesis".into();
    r.hash = hex::encode(ids::sha256(g.write().as_bytes()));
    r.syntactic = format!("feeds={}", g.initial_feeds.as_ref().map_or(0, Vec::len));
    r.exec = OK.into();
    r.note = format!("timestamp={} message={:?}", g.timestamp, g.message);
    r
}

fn request_id(id: &str, bytes: &[u8]) -> Row {
    let mut r = Row::new(id);
    let text = match std::str::from_utf8(bytes) {
        Ok(t) => t,
        Err(_) => return r.malformed("arguments are not text"),
    };
    let f: Vec<&str> = text.split('|').collect();
    if f.len() != 5 {
        return r.malformed(&format!("a request id takes five arguments, got {}", f.len()));
    }
    let arg = |s: &str| -> Result<ids::Id, String> {
        let raw = hex::decode(s).map_err(|e| e.to_string())?;
        if raw.len() != 32 {
            return Err(format!("an id is 32 bytes, got {}", raw.len()));
        }
        let mut out = ids::EMPTY;
        out.copy_from_slice(&raw);
        Ok(out)
    };
    let (service, session, tx) = match (arg(f[0]), arg(f[1]), arg(f[2])) {
        (Ok(a), Ok(b), Ok(c)) => (a, b, c),
        (Err(e), _, _) | (_, Err(e), _) | (_, _, Err(e)) => return r.malformed(&trim(&e)),
    };
    let step: u32 = match f[3].parse() {
        Ok(n) => n,
        Err(e) => return r.malformed(&trim(&e.to_string())),
    };
    let retry: u32 = match f[4].parse() {
        Ok(n) => n,
        Err(e) => return r.malformed(&trim(&e.to_string())),
    };
    r.parse = "ok".into();
    r.kind = "RequestID".into();
    r.hash = hex::encode(compute_request_id(&service, &session, &tx, step, retry));
    r.syntactic = OK.into();
    r.exec = OK.into();
    r.note = format!("step={step} retry={retry}");
    r
}

fn request(id: &str, bytes: &[u8]) -> Row {
    let mut r = Row::new(id);
    let v = match gojson::parse(bytes) {
        Ok(v) => v,
        Err(e) => return r.malformed(&trim(&e)),
    };
    let req = match OracleRequest::read(&v) {
        Ok(q) => q,
        Err(e) => return r.malformed(&trim(&e)),
    };
    r.parse = "ok".into();
    r.kind = match req.kind {
        KIND_WRITE => "Write".to_owned(),
        KIND_READ => "Read".to_owned(),
        _ => "unknown".to_owned(),
    };
    r.hash = hex::encode(req.request_id);
    let mut vm = match seeded_vm() {
        Ok(vm) => vm,
        Err(e) => return r.internal(&trim(&e)),
    };
    match vm.register_request(&req) {
        Err(e) => {
            let class = classify(&e);
            // The one refusal on this chain that is reached before the chain
            // is read answers both halves; everything else leaves the
            // syntactic verdict OK.
            r.syntactic = if e.starts_with("invalid request_id") {
                class.to_owned()
            } else {
                OK.to_owned()
            };
            r.exec = class.to_owned();
            r.note = trim(&e);
        }
        Ok(()) => {
            r.syntactic = OK.into();
            r.exec = OK.into();
            r.note = format!(
                "executors={} deadline={}",
                req.executors.as_ref().map_or(0, Vec::len),
                req.deadline_height
            );
        }
    }
    r
}

fn commit(id: &str, bytes: &[u8]) -> Row {
    let mut r = Row::new(id);
    let v = match gojson::parse(bytes) {
        Ok(v) => v,
        Err(e) => return r.malformed(&trim(&e)),
    };
    let obj = match gojson::object(&v) {
        Ok(o) => o,
        Err(e) => return r.malformed(&trim(&e)),
    };
    let req = match gojson::member(obj, "request") {
        None => return r.malformed("a commit run carries a request"),
        Some(m) if m.is_null() => return r.malformed("a commit run carries a request"),
        Some(m) => match OracleRequest::read(m) {
            Ok(q) => q,
            Err(e) => return r.malformed(&trim(&e)),
        },
    };
    let mut records: Vec<OracleRecord> = Vec::new();
    match gojson::array_of(obj, "records") {
        Err(e) => return r.malformed(&trim(&e)),
        Ok(None) => {}
        Ok(Some(items)) => {
            for e in items {
                match OracleRecord::read(e) {
                    Ok(rec) => records.push(rec),
                    Err(why) => return r.malformed(&trim(&why)),
                }
            }
        }
    }
    r.parse = "ok".into();
    r.kind = "OracleCommit".into();
    r.syntactic = format!("records={}", records.len());

    let mut vm = match seeded_vm() {
        Ok(vm) => vm,
        Err(e) => return r.internal(&trim(&e)),
    };
    if let Err(e) = vm.register_request(&req) {
        r.exec = classify(&e).to_owned();
        r.note = trim(&e);
        return r;
    }
    for rec in &records {
        if let Err(e) = vm.submit_record(rec) {
            r.exec = classify(&e).to_owned();
            r.note = trim(&e);
            return r;
        }
    }
    match vm.commit_records(&req.request_id) {
        Err(e) => {
            r.exec = classify(&e).to_owned();
            r.note = trim(&e);
        }
        Ok(c) => {
            r.hash = hex::encode(c.root);
            r.exec = OK.into();
            r.note = format!("count={} window=[{},{}]", c.count, c.window_start, c.window_end);
        }
    }
    r
}

fn observation(id: &str, bytes: &[u8]) -> Row {
    let mut r = Row::new(id);
    let v = match gojson::parse(bytes) {
        Ok(v) => v,
        Err(e) => return r.malformed(&trim(&e)),
    };
    let obs = match Observation::read(&v) {
        Ok(o) => o,
        Err(e) => return r.malformed(&trim(&e)),
    };
    r.parse = "ok".into();
    r.kind = "Observation".into();
    r.hash = hex::encode(obs.feed_id);
    r.syntactic = OK.into();
    let mut vm = match seeded_vm() {
        Ok(vm) => vm,
        Err(e) => return r.internal(&trim(&e)),
    };
    match vm.submit_observation(&obs) {
        Err(e) => {
            r.exec = classify(&e).to_owned();
            r.note = trim(&e);
        }
        Ok(()) => {
            r.exec = OK.into();
            r.note = "accepted into the pending set".into();
        }
    }
    r
}
