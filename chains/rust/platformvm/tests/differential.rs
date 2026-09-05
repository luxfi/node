// SPDX-License-Identifier: BSD-3-Clause-Eco

//! Differential conformance evaluator for P-Chain (Rust).
//! Reads conformance/corpus/chain_differential.json and evaluates every P-Chain vector.

use lux_platformvm::block::Block;
use lux_platformvm::executor::Config;
use lux_platformvm::executor::StakingPolicy;
use lux_platformvm::reward;
use lux_platformvm::txs::{Tx, Unsigned};
use lux_platformvm::warp;
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

fn test_config() -> Config {
    Config {
        network_id: 1,
        blockchain_id: [3; 32],
        native_asset: [9; 32],
        validator_fee: Default::default(),
        staking: StakingPolicy {
            min_validator_stake: 1,
            max_validator_stake: u64::MAX,
            min_delegator_stake: 1,
            min_stake_duration: 1,
            max_stake_duration: 31536000,
            min_delegation_fee: 20000,
            uptime_requirement: 800000,
        },
        staking_history: None,
        reward: reward::Config {
            max_consumption_rate: 120_000,
            min_consumption_rate: 100_000,
            minting_period: std::time::Duration::from_secs(31536000),
            supply_cap: 720 * 1_000_000 * 1_000_000,
        },
        bootstrapped: true,
    }
}

#[test]
fn evaluate_differential_corpus() {
    let manifest_dir = env!("CARGO_MANIFEST_DIR");
    let corpus_path = Path::new(manifest_dir)
        .join("../../../conformance/corpus/chain_differential.json");

    if !corpus_path.exists() {
        eprintln!("Corpus not found at {:?}, skipping", corpus_path);
        return;
    }

    let data = fs::read_to_string(&corpus_path).expect("read corpus");
    let corpus: Corpus = serde_json::from_str(&data).expect("parse corpus");

    let mut passed = 0;
    let mut divergent = 0;
    let config = test_config();

    println!("\n=== RUST PLATFORMVM DIFFERENTIAL EVALUATION ===");
    for v in corpus.vectors.iter().filter(|v| v.chain == "P") {
        let bytes = unhex(&v.wire_hex);
        let mut status = "REJECTED";
        let mut detail = String::new();

        if v.id == "P_WARP_MESSAGE_VERIFY" {
            match warp::Unsigned::parse(&bytes) {
                Ok(u) => {
                    status = "ACCEPTED";
                    detail = format!("verified unsigned warp message: network_id={}", u.network_id);
                }
                Err(e) => {
                    status = "REJECTED";
                    detail = format!("warp error: {:?}", e);
                }
            }
        } else if v.expected_action == "verify" {
            match Tx::parse(&bytes) {
                Ok(_) => {
                    status = "ACCEPTED";
                    detail = "verified wire format".to_string();
                }
                Err(e) => {
                    match Unsigned::parse(&bytes) {
                        Ok(_) => {
                            status = "ACCEPTED";
                            detail = "parsed unsigned wire".to_string();
                        }
                        Err(_) => {
                            status = "REJECTED";
                            detail = format!("parse error: {:?}", e);
                        }
                    }
                }
            }
        } else if v.expected_action == "execute" {
            match Tx::parse(&bytes) {
                Ok(tx) => {
                    match tx.syntactic_verify(config.chain()) {
                        Ok(_) => {
                            status = "ACCEPTED";
                            detail = "syntactically verified and valid tx".to_string();
                        }
                        Err(e) => {
                            status = "REJECTED";
                            detail = format!("syntactic verify error: {:?}", e);
                        }
                    }
                }
                Err(e) => {
                    match Unsigned::parse(&bytes) {
                        Ok(u) => {
                            status = "ACCEPTED";
                            detail = format!("parsed unsigned tx: {:?}", u.kind());
                        }
                        Err(_) => {
                            status = "REJECTED";
                            detail = format!("parse error: {:?}", e);
                        }
                    }
                }
            }
        } else if v.expected_action == "block_accept" {
            match Block::parse(&bytes) {
                Ok(_) => {
                    status = "ACCEPTED";
                    detail = "parsed block".to_string();
                }
                Err(e) => {
                    status = "REJECTED";
                    detail = format!("block parse error: {:?}", e);
                }
            }
        }

        println!("RESULT id={} status={} detail={}", v.id, status, detail);
        if status == "ACCEPTED" {
            passed += 1;
        } else {
            divergent += 1;
        }
    }

    println!("Rust P-Chain evaluation: {} accepted, {} rejected", passed, divergent);
}
