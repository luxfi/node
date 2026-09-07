// SPDX-License-Identifier: BSD-3-Clause-Eco

//! Differential conformance test for Rust ZKVM against Go corpus.

use lux_zkvm::{Block, Transaction};
use serde::Deserialize;
use std::fs;
use std::path::Path;

#[derive(Deserialize)]
struct Corpus {
    vectors: Vec<Vector>,
}

#[derive(Deserialize)]
struct Vector {
    id: String,
    chain: String,
    wire_hex: String,
    expected_action: String,
}

fn unhex(s: &str) -> Vec<u8> {
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).unwrap())
        .collect()
}

#[test]
fn evaluate_zk_corpus() {
    let manifest_dir = env!("CARGO_MANIFEST_DIR");
    let corpus_path = Path::new(manifest_dir)
        .join("../../../conformance/corpus/chain_differential.json");

    if !corpus_path.exists() {
        eprintln!("Corpus not found at {:?}, skipping", corpus_path);
        return;
    }

    let data = fs::read_to_string(&corpus_path).expect("read corpus");
    let corpus: Corpus = serde_json::from_str(&data).expect("parse corpus");

    for v in corpus.vectors.iter().filter(|v| v.chain == "Z") {
        let bytes = unhex(&v.wire_hex);
        if v.expected_action == "verify" {
            let tx = Transaction::parse(&bytes).expect("parse shielded tx");
            assert!(!tx.raw.is_empty());
            println!("RESULT id={} status=ACCEPTED detail=parsed zkvm tx id={:?}", v.id, hex::encode(tx.id()));
        } else if v.expected_action == "block_accept" {
            let blk = Block::parse(&bytes).expect("parse zkvm block");
            assert_eq!(blk.txs.len(), 1);
            println!("RESULT id={} status=ACCEPTED detail=parsed zkvm block height={}", v.id, blk.height);
        }
    }
}
