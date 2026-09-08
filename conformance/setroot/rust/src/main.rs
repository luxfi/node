// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The Rust node's answer to the validator-set-root corpus.
//!
//! It calls `lux_node::validators::root` — the function the Rust node uses on
//! the set it reads out of luxd's own `/validators/at` — and nothing else. A
//! second implementation of the encoding here would make this program agree
//! with itself and report that as agreement.

use std::io::{BufWriter, Write};

use lux_node::validators::{parse, root, Member};

fn main() {
    let mut args = std::env::args().skip(1);
    let first = args.next();

    // The document mode: read what luxd serves at
    // /v1/chain/<P>/ops/validators/at, through the reader this node uses on it,
    // and print the root that set commits to. It is the join the corpus does
    // not reach — hashing a set alike is worth nothing to a node that cannot
    // read the set the network published.
    if first.as_deref() == Some("-document") {
        let mut body = Vec::new();
        if let Err(e) = std::io::Read::read_to_end(&mut std::io::stdin(), &mut body) {
            eprintln!("setroot: read the document: {e}");
            std::process::exit(1);
        }
        match parse(&body) {
            Ok(set) => println!("{}", hex(&root(&set))),
            Err(e) => {
                eprintln!("setroot: the document luxd published did not read: {e}");
                std::process::exit(1);
            }
        }
        return;
    }

    let path = match first {
        Some(p) => p,
        None => {
            eprintln!("setroot: name the corpus to answer");
            std::process::exit(2);
        }
    };
    let text = match std::fs::read_to_string(&path) {
        Ok(t) => t,
        Err(e) => {
            eprintln!("setroot: {path}: {e}");
            std::process::exit(1);
        }
    };

    let out = std::io::stdout();
    let mut w = BufWriter::new(out.lock());
    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let cols: Vec<&str> = line.split('\t').collect();
        if cols.len() < 3 || cols[0] != "V" {
            eprintln!("setroot: not a vector line: {line:?}");
            std::process::exit(1);
        }
        let id = cols[1];
        let mut set: Vec<Member> = Vec::new();
        for c in &cols[3..] {
            match member(c) {
                Ok(m) => set.push(m),
                Err(e) => {
                    eprintln!("setroot: {id}: {e}");
                    std::process::exit(1);
                }
            }
        }
        writeln!(w, "R\t{}\t{}\t", id, hex(&root(&set))).expect("stdout");
    }
}

fn member(s: &str) -> Result<Member, String> {
    let f: Vec<&str> = s.split(':').collect();
    let [node, weight, key] = f.as_slice() else {
        return Err(format!("a member is nodeID:weight:key, got {s:?}"));
    };
    let node = unhex(node)?;
    let node: [u8; 20] = node.try_into().map_err(|_| "a node id is 20 bytes".to_string())?;
    let weight: u64 = weight.parse().map_err(|e| format!("{weight:?} is not a weight: {e}"))?;
    Ok(Member { node, weight, key: unhex(key)? })
}

fn unhex(s: &str) -> Result<Vec<u8>, String> {
    (0..s.len())
        .step_by(2)
        .map(|i| {
            s.get(i..i + 2)
                .ok_or_else(|| format!("{s:?} is not hex"))
                .and_then(|b| u8::from_str_radix(b, 16).map_err(|e| format!("{s:?} is not hex: {e}")))
        })
        .collect()
}

fn hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}
