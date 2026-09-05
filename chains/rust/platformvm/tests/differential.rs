// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What Go answered, asked again here.
//!
//! `conformance/corpus/chain_differential.json` is written by Go: every P-Chain
//! vector's bytes come out of a Go constructor in `conformance/gen`, and beside
//! the bytes Go records what it made of them — the transaction's name, its type,
//! the numbers it read. This file asks the same bytes of the Rust P-Chain and
//! requires the same answers.
//!
//! It is a differential test, so it may only ever compare. A test that computes
//! a status and prints it has decided nothing: every run is green, including the
//! run where the two implementations disagree. So there is no reporting here and
//! no tolerance — each vector asserts, and a vector the corpus describes and
//! this file does not check is itself a failure, because the way coverage is
//! lost is quietly.
//!
//! ## Why an id is worth asserting
//!
//! Go names a transaction `sha256` of the bytes it signed
//! (`vms/platformvm/txs.Tx.SetBytes`: `TxID = hash.ComputeHash256Array(signedBytes)`),
//! printed in CB58. Rust names it the same way over the bytes it was handed, so
//! comparing ids alone would only prove that both sides can hash — the input
//! decides the answer and the parser is never asked anything.
//!
//! What makes it a real question is re-encoding. Every transaction here is
//! parsed into Rust's own structure and then written back out from that
//! structure, and the bytes that come back must be the bytes that went in.
//! Those bytes were written by Go's encoder from Go's structure; if Rust's
//! encoder reproduces them from Rust's structure, the two agree about the whole
//! shape — every offset, every length, every field it did not think to look at
//! — and only then does the id mean anything.

use lux_platformvm::block::Block;
use lux_platformvm::executor::{Config, StakingPolicy};
use lux_platformvm::ids::{hash256, Id};
use lux_platformvm::reward;
use lux_platformvm::txs::{write_credentials, Kind, Tx, Unsigned};
use lux_platformvm::warp;
use serde::Deserialize;
use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};

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
    go_expectation: Expectation,
}

/// What Go made of the bytes. Every field it recorded is checked; a field it
/// left out is one Go had nothing to say about.
#[derive(Deserialize)]
struct Expectation {
    valid: bool,
    status: String,
    #[serde(default)]
    tx_type: String,
    #[serde(default)]
    tx_id: Option<String>,
    #[serde(default)]
    block_id: Option<String>,
    /// `weight` for a staking transaction. For `RegisterL1ValidatorTx` the
    /// generator files the constructor's second argument under this name, and
    /// that argument is `NewRegisterL1ValidatorTx(base, balance, …)` — the
    /// balance. The number is Go's either way; only the label is loose.
    #[serde(default)]
    weight: Option<u64>,
    #[serde(default)]
    balance: Option<u64>,
}

/// The vectors this file knows how to ask, and what it asks of each.
///
/// Naming them here rather than reacting to whatever the corpus happens to hold
/// is what stops coverage from being lost silently: a vector that disappears
/// from the corpus fails [`every_p_vector_in_the_corpus_is_checked`], and one
/// that appears without being named here fails it too.
const P_VECTORS: &[&str] = &[
    "P_BASE_TX",
    "P_ADD_VALIDATOR_TX",
    "P_CREATE_NETWORK_TX",
    "P_REGISTER_L1_VALIDATOR_TX",
    "P_SET_L1_VALIDATOR_WEIGHT_TX",
    "P_INCREASE_L1_VALIDATOR_BALANCE_TX",
    "P_DISABLE_L1_VALIDATOR_TX",
    "P_WARP_MESSAGE_VERIFY",
    "P_STANDARD_BLOCK",
];

fn unhex(s: &str) -> Vec<u8> {
    assert!(s.len().is_multiple_of(2), "wire_hex has an odd number of digits");
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).expect("wire_hex is hex"))
        .collect()
}

/// An id in the form Go prints it: CB58, which is base58 over the bytes
/// followed by the last four bytes of their `sha256`.
///
/// Comparing in Go's own notation rather than in raw bytes is so that a
/// disagreement reads as the two ids Go and Rust would each name in a log,
/// instead of as two arrays.
fn cb58(id: &Id) -> String {
    const ALPHABET: &[u8] = b"123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
    let mut raw = id.to_vec();
    raw.extend_from_slice(&hash256(id)[28..]);

    // Long division by 58 over the bytes, most significant first.
    let mut digits: Vec<u8> = Vec::new();
    let mut work = raw.clone();
    let mut start = 0;
    while start < work.len() {
        let mut carry = 0u32;
        let mut all_zero = true;
        for byte in work.iter_mut().skip(start) {
            let cur = carry * 256 + *byte as u32;
            *byte = (cur / 58) as u8;
            carry = cur % 58;
            if *byte == 0 && all_zero {
                start += 1;
            } else {
                all_zero = false;
            }
        }
        digits.push(carry as u8);
    }

    let leading_zeros = raw.iter().take_while(|b| **b == 0).count();
    let mut out = String::new();
    for _ in 0..leading_zeros {
        out.push('1');
    }
    for d in digits.iter().rev() {
        out.push(ALPHABET[*d as usize] as char);
    }
    out
}

/// The name Go gives each kind of transaction, which is its Go type's name.
///
/// Exhaustive on purpose: a kind added to the chain has to be given its Go name
/// here, and the compiler is what asks.
fn go_type_name(kind: Kind) -> &'static str {
    match kind {
        Kind::RewardValidator => "RewardValidatorTx",
        Kind::Base => "BaseTx",
        Kind::Import => "ImportTx",
        Kind::Export => "ExportTx",
        Kind::CreateNetwork => "CreateNetworkTx",
        Kind::CreateChain => "CreateChainTx",
        Kind::TransferChainOwnership => "TransferChainOwnershipTx",
        Kind::RemoveChainValidator => "RemoveChainValidatorTx",
        Kind::TransformChain => "TransformChainTx",
        Kind::AddValidator => "AddValidatorTx",
        Kind::AddChainValidator => "AddChainValidatorTx",
        Kind::AddDelegator => "AddDelegatorTx",
        Kind::AddPermissionlessValidator => "AddPermissionlessValidatorTx",
        Kind::AddPermissionlessDelegator => "AddPermissionlessDelegatorTx",
        Kind::RegisterL1Validator => "RegisterL1ValidatorTx",
        Kind::SetL1ValidatorWeight => "SetL1ValidatorWeightTx",
        Kind::IncreaseL1ValidatorBalance => "IncreaseL1ValidatorBalanceTx",
        Kind::DisableL1Validator => "DisableL1ValidatorTx",
        Kind::ConvertNetwork => "ConvertNetworkTx",
    }
}

fn corpus_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../conformance/corpus/chain_differential.json")
}

/// The corpus is not optional. A missing file used to end this test early and
/// count as a pass, which is the same thing as not having the test.
fn corpus() -> Vec<Vector> {
    let path = corpus_path();
    let data = fs::read_to_string(&path).unwrap_or_else(|e| {
        panic!(
            "the differential corpus is what Go answered and there is nothing to \
             compare against without it: {}: {e}",
            path.display()
        )
    });
    let corpus: Corpus = serde_json::from_str(&data).expect("the corpus parses");
    let p: Vec<Vector> = corpus.vectors.into_iter().filter(|v| v.chain == "P").collect();
    assert!(!p.is_empty(), "the corpus holds no P-Chain vectors");
    p
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

/// Every P vector the corpus holds is one this file asks, and every vector this
/// file names is one the corpus holds.
#[test]
fn every_p_vector_in_the_corpus_is_checked() {
    let in_corpus: BTreeSet<String> = corpus().into_iter().map(|v| v.id).collect();
    let checked: BTreeSet<String> = P_VECTORS.iter().map(|s| s.to_string()).collect();
    assert_eq!(
        in_corpus, checked,
        "the corpus and the vectors this file checks have drifted apart"
    );
}

/// Every transaction Go wrote is one Rust reads back into the same bytes.
///
/// This is the assertion the rest lean on: it is the encoder, not the hash,
/// that has to agree.
#[test]
fn every_transaction_re_encodes_to_the_bytes_go_wrote() {
    for v in corpus() {
        if v.go_expectation.tx_id.is_none() {
            continue; // not a transaction — the block and the warp message
        }
        let bytes = unhex(&v.wire_hex);
        let tx = Tx::parse(&bytes)
            .unwrap_or_else(|e| panic!("{}: Go wrote these bytes and we cannot read them: {e:?}", v.id));

        let mut again = tx.unsigned.to_bytes();
        if !tx.creds.is_empty() {
            again.extend_from_slice(&write_credentials(&tx.creds));
        }
        assert_eq!(
            again,
            bytes,
            "{}: re-encoding the parsed transaction did not reproduce Go's bytes",
            v.id
        );
    }
}

/// Every transaction is the type Go says it is, named the way Go names it.
#[test]
fn every_transaction_is_the_type_and_the_id_go_recorded() {
    for v in corpus() {
        let Some(want_id) = v.go_expectation.tx_id.as_deref() else {
            continue;
        };
        let bytes = unhex(&v.wire_hex);
        let tx = Tx::parse(&bytes).unwrap_or_else(|e| panic!("{}: {e:?}", v.id));

        assert_eq!(
            go_type_name(tx.unsigned.kind()),
            v.go_expectation.tx_type,
            "{}: the transaction is not the type Go read",
            v.id
        );
        assert_eq!(
            cb58(&tx.id()),
            want_id,
            "{}: the transaction has a different name here than in Go",
            v.id
        );
        assert!(
            v.go_expectation.valid && v.go_expectation.status == "ACCEPTED",
            "{}: the corpus records a transaction Go refused, and nothing here \
             expects one — teach this file what Go's refusal was before adding it",
            v.id
        );
    }
}

/// The numbers Go read out of each transaction are the numbers Rust reads.
///
/// A field checked nowhere else is a field the two encoders could disagree
/// about at the same offset for a long time.
#[test]
fn the_numbers_go_read_are_the_numbers_here() {
    let mut checked = 0;
    for v in corpus() {
        if v.go_expectation.tx_id.is_none() {
            continue;
        }
        let tx = Tx::parse(&unhex(&v.wire_hex)).unwrap_or_else(|e| panic!("{}: {e:?}", v.id));
        match (&tx.unsigned, v.go_expectation.weight, v.go_expectation.balance) {
            (Unsigned::AddValidator { validator, .. }, Some(weight), _) => {
                assert_eq!(validator.weight, weight, "{}: staked weight", v.id);
                checked += 1;
            }
            // The generator files this one's balance under `weight`; see
            // [`Expectation::weight`].
            (Unsigned::RegisterL1Validator { balance, .. }, Some(recorded), _) => {
                assert_eq!(*balance, recorded, "{}: registration balance", v.id);
                checked += 1;
            }
            (Unsigned::IncreaseL1ValidatorBalance { balance, .. }, _, Some(want)) => {
                assert_eq!(*balance, want, "{}: topped-up balance", v.id);
                checked += 1;
            }
            (_, None, None) => {}
            (u, w, b) => panic!(
                "{}: Go recorded weight={w:?} balance={b:?} for a {} and this file \
                 does not know which field that is",
                v.id,
                go_type_name(u.kind())
            ),
        }
    }
    assert_eq!(checked, 3, "the corpus stopped carrying numbers to check");
}

/// A transaction Go executed is one this chain finds syntactically whole.
///
/// Go's `execute` means it ran; the nearest thing asked of the bytes alone is
/// `syntactic_verify`, which is what a node does before it will consider one.
#[test]
fn every_transaction_go_executed_verifies_here() {
    let config = test_config();
    let mut executed = 0;
    for v in corpus() {
        if v.expected_action != "execute" {
            continue;
        }
        let tx = Tx::parse(&unhex(&v.wire_hex)).unwrap_or_else(|e| panic!("{}: {e:?}", v.id));
        tx.syntactic_verify(config.chain()).unwrap_or_else(|e| {
            panic!(
                "{}: Go executed this transaction and we refuse it as malformed: {e:?}",
                v.id
            )
        });
        executed += 1;
    }
    assert_eq!(executed, 5, "the corpus stopped carrying executable vectors");
}

/// The block Go named is the block named here, and it is exactly its bytes.
#[test]
fn the_block_is_the_one_go_named() {
    let mut seen = 0;
    for v in corpus() {
        let Some(want) = v.go_expectation.block_id.as_deref() else {
            continue;
        };
        let bytes = unhex(&v.wire_hex);
        let blk = Block::parse(&bytes)
            .unwrap_or_else(|e| panic!("{}: Go wrote this block and we refuse it: {e:?}", v.id));
        assert_eq!(cb58(&blk.id()), want, "{}: the block has a different name", v.id);
        assert_eq!(blk.bytes(), &bytes[..], "{}: the block is not its bytes", v.id);
        seen += 1;
    }
    assert_eq!(seen, 1, "the corpus stopped carrying a block");
}

/// The warp message Go wrote reads here, and writing it back gives Go's bytes.
#[test]
fn the_warp_message_re_encodes_to_the_bytes_go_wrote() {
    let mut seen = 0;
    for v in corpus() {
        if v.id != "P_WARP_MESSAGE_VERIFY" {
            continue;
        }
        let bytes = unhex(&v.wire_hex);
        let msg = warp::Unsigned::parse(&bytes)
            .unwrap_or_else(|e| panic!("{}: Go wrote this message and we refuse it: {e:?}", v.id));
        let again = warp::Unsigned::build(msg.network_id, msg.source_chain_id, &msg.payload);
        assert_eq!(
            again.bytes, bytes,
            "{}: re-encoding the warp message did not reproduce Go's bytes",
            v.id
        );
        assert_eq!(msg.bytes, bytes, "{}: the message is not its bytes", v.id);
        seen += 1;
    }
    assert_eq!(seen, 1, "the corpus stopped carrying a warp message");
}

/// The notation this file compares ids in is Go's.
///
/// Without this the id assertions could agree with themselves and with nothing
/// else. The value is Go's own: `ids.Empty.String()`, and the P_STANDARD_BLOCK
/// name the corpus carries beside bytes whose `sha256` is checked here.
#[test]
fn cb58_is_the_notation_go_prints_ids_in() {
    assert_eq!(
        cb58(&[0u8; 32]),
        "11111111111111111111111111111111LpoYY",
        "the empty id is not printed the way Go prints it"
    );
    let block = corpus()
        .into_iter()
        .find(|v| v.id == "P_STANDARD_BLOCK")
        .expect("the block vector");
    let bytes = unhex(&block.wire_hex);
    assert_eq!(
        cb58(&hash256(&bytes)),
        block.go_expectation.block_id.as_deref().expect("a block id"),
        "sha256 of Go's bytes does not print as the name Go gave them"
    );
}
