// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The Rust P-chain's answers to the shared corpus.
//!
//! One of three evaluators — Go, Rust, C++ — that read the same
//! `conformance/corpus/vectors.tsv` and print the same seven fields per
//! vector. The runner compares the three sets of lines; this program never
//! sees another implementation's answer and has nothing to agree with.
//!
//! Usage: `conformance <vectors.tsv>`

use lux_platformvm::block::{self, Block};
use lux_platformvm::executor::{self, Config, Fees, FlatFees, StakingPolicy};
use lux_platformvm::ids::Id;
use lux_platformvm::reward;
use lux_platformvm::state::State;
use lux_platformvm::txs::{Kind, Tx, Unsigned};
use lux_platformvm::vm::{self, PlatformVm};

const NONE: &str = "-";

// The verdict vocabulary, shared with the Go and C++ evaluators. Three
// spellings of one answer — "failed to fetch UTXO", `MissingUtxo`,
// `kUtxoNotFound` — would otherwise read as a disagreement that is not one.
const OK: &str = "OK";
const MALFORMED: &str = "MALFORMED";
const SYNTACTIC: &str = "SYNTACTIC";
const OVERFLOW: &str = "OVERFLOW";
const LEDGER: &str = "LEDGER";
const AUTH: &str = "AUTH";
const WARP: &str = "WARP";
const UNSUPPORTED: &str = "UNSUPPORTED";

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
        let (id, chain, op, wire) = (f[1], f[2], f[3], f[4]);
        // This evaluator is the P-chain. The X-chain vectors belong to the
        // X-chain evaluator; printing nothing for them is what lets the runner
        // see that this implementation did not answer, rather than inventing
        // an answer it has no standing to give.
        if chain != "P" {
            continue;
        }
        println!("{}", evaluate(id, op, wire));
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

/// The chain the corpus is built for: network 1, chain id the id whose every
/// byte is 3, the fee and stake asset the id whose every byte is 9. The Go and
/// C++ evaluators are given the same three numbers, so a rule that reads them
/// reads one set of values.
///
/// The network and the chain are part of that identity and not decoration: a
/// transaction names the network and the chain it was signed for, and one
/// naming another of either is refused before it is executed. An evaluator that
/// did not know which chain it was would have nothing to compare them against.
fn config() -> Config {
    Config {
        network_id: 1,
        blockchain_id: [3u8; 32],
        native_asset: [9u8; 32],
        // Go's `builder.LocalValidatorFeeConfig`, field for field — what the
        // reference charges an L1 validator for the P-Chain's trouble in
        // tracking it, and how many it has room for at once.
        validator_fee: lux_platformvm::l1::FeeConfig {
            capacity: 20_000,
            target: 10_000,
            min_price: 512,
            excess_conversion_constant: 1_587,
        },
        staking: StakingPolicy {
            min_validator_stake: 1,
            max_validator_stake: 1 << 60,
            min_delegator_stake: 1,
            min_stake_duration: 24 * 60 * 60,
            max_stake_duration: 365 * 24 * 60 * 60,
            min_delegation_fee: 20_000,
            uptime_requirement: 800_000,
        },
        staking_history: None,
        reward: reward::Config {
            max_consumption_rate: 120_000,
            min_consumption_rate: 100_000,
            minting_period: std::time::Duration::from_secs(365 * 24 * 60 * 60),
            supply_cap: 720 * 1_000_000 * 1_000_000,
        },
        bootstrapped: true,
    }
}

/// A chain that holds nothing, at the corpus's genesis time. Every vector meets
/// the same empty ledger, which is the one starting state all three
/// implementations can stand up without three state builders to compare.
fn empty_chain() -> State {
    let mut s = State::new();
    s.set_timestamp(1000);
    s
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
            r.note = format!("{e}");
            return r;
        }
    };
    r.parse = "ok".into();
    r.kind = kind_name(tx.unsigned.kind()).into();
    r.hash = hex(&tx.id());

    if let Err(e) = tx.syntactic_verify(config().chain()) {
        let class = classify_txs(&e);
        r.note = format!("{e}");
        r.syntactic = class.clone();
        r.exec = class;
        return r;
    }
    r.syntactic = OK.into();

    let mut state = empty_chain();
    let fees = FlatFees::default();
    // A node with no shared half. Every import finds nothing rather than being
    // refused outright — which is the answer Go gives from an empty shared
    // memory, and is what the corpus's empty ledger means for the other two
    // evaluators as well.
    let atomic = executor::NoImports;
    match executor::execute_standard(&mut state, &tx, &config(), &fees as &dyn Fees, &atomic) {
        Ok(()) => {
            r.exec = OK.into();
            r.note = "executed on the empty chain".into();
        }
        Err(e) => {
            r.exec = classify_exec(&e);
            r.note = format!("{e}");
        }
    }
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
            r.kind = block_kind_name(blk.kind()).into();
            r.hash = hex(&blk.id());
            r.syntactic = OK.into();
            r.exec = LEDGER.into();
            r.note = format!(
                "height={} parent={} txs={}",
                blk.height(),
                hex(&blk.parent()[..4]),
                blk.decision_txs().len()
            );
        }
        Err(e) => {
            r.parse = MALFORMED.into();
            r.syntactic = MALFORMED.into();
            r.exec = MALFORMED.into();
            r.note = format!("{e}");
        }
    }
    r
}

/// The block-decision seam. The answer comes from the compiler: the function
/// item below only names something if `Manager::reject` is there with that
/// shape. Nothing about it is typed by hand.
fn eval_seam(id: &str) -> Row {
    let mut r = row(id);
    r.parse = "ok".into();
    r.kind = "Block.Reject".into();
    // A P-chain block decision is `vm::Vm::reject`; naming it here is the
    // check, and the compiler is the one that answers.
    let _: fn(&PlatformVm, &Id) -> Result<(), vm::Error> = <PlatformVm as vm::Vm>::reject;
    r.exec = "PRESENT".into();
    r.note = "platformvm vm::Vm::reject drops the block and its pending state".into();
    r
}

fn kind_name(k: Kind) -> &'static str {
    match k {
        Kind::RewardValidator => "RewardValidator",
        Kind::Base => "Base",
        Kind::Import => "Import",
        Kind::Export => "Export",
        Kind::CreateNetwork => "CreateNetwork",
        Kind::CreateChain => "CreateChain",
        Kind::TransferChainOwnership => "TransferChainOwnership",
        Kind::RemoveChainValidator => "RemoveChainValidator",
        Kind::TransformChain => "TransformChain",
        Kind::AddValidator => "AddValidator",
        Kind::AddChainValidator => "AddChainValidator",
        Kind::AddDelegator => "AddDelegator",
        Kind::AddPermissionlessValidator => "AddPermissionlessValidator",
        Kind::AddPermissionlessDelegator => "AddPermissionlessDelegator",
        Kind::RegisterL1Validator => "RegisterL1Validator",
        Kind::SetL1ValidatorWeight => "SetL1ValidatorWeight",
        Kind::IncreaseL1ValidatorBalance => "IncreaseL1ValidatorBalance",
        Kind::DisableL1Validator => "DisableL1Validator",
        Kind::ConvertNetwork => "ConvertNetwork",
    }
}

fn block_kind_name(k: block::Kind) -> &'static str {
    match k {
        block::Kind::Abort => "AbortBlock",
        block::Kind::Commit => "CommitBlock",
        block::Kind::Proposal => "ProposalBlock",
        block::Kind::Standard => "StandardBlock",
    }
}

/// A refusal that never looked at the chain — this implementation does not run
/// this kind of transaction at all — is UNSUPPORTED, and is matched on the
/// variant rather than on words, because it is the single most important thing
/// this harness has to be able to see. Everything else is classified from the
/// message, the same way the Go and C++ evaluators classify theirs.
fn classify_exec(e: &executor::Error) -> String {
    match e {
        executor::Error::WrongTxType(_)
        | executor::Error::TransformChainNotPermitted
        | executor::Error::AddValidatorNotPermitted
        | executor::Error::AddDelegatorNotPermitted => UNSUPPORTED.to_string(),
        executor::Error::Syntactic(inner) => classify_txs(inner),
        executor::Error::Overflow => OVERFLOW.to_string(),
        executor::Error::Credential(_) => AUTH.to_string(),
        // The chain does not hold what the transaction names. On the empty
        // chain every vector meets, that is the ordinary answer.
        //
        // The four named absences belong here for the same reason `State` does,
        // and are matched the same way. They read as sentences rather than as
        // "not found" — "this network never stated staking terms of its own",
        // "no L1 validator is registered as …" — and the shared word table,
        // which looks for the reference's words, therefore does not see them.
        // It called them SYNTACTIC: a transaction refused for its shape, where
        // Go and C++ both said the chain simply does not hold the thing. That
        // is the wrong class, not a wrong wording, so it is fixed at the
        // variant. Rewording the chain's errors to suit a word table would put
        // the harness in charge of what the chain says.
        executor::Error::State(_)
        | executor::Error::Flow(_)
        | executor::Error::NoNetworkTerms
        | executor::Error::NoSuchNetwork
        | executor::Error::NoConversion(_)
        | executor::Error::NoSuchL1Validator(_) => LEDGER.to_string(),
        _ => classify_words(&format!("{e}")),
    }
}

fn classify_txs(e: &lux_platformvm::txs::Error) -> String {
    match e {
        lux_platformvm::txs::Error::Wire(_) => MALFORMED.to_string(),
        lux_platformvm::txs::Error::Overflow => OVERFLOW.to_string(),
        _ => classify_words(&format!("{e}")),
    }
}

/// The same word table the Go evaluator uses, in the same order, so that a
/// message meaning "no UTXO backs this" lands on FUNDS in both.
fn classify_words(s: &str) -> String {
    let s = s.to_lowercase();
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

// A vector the corpus carries but this chain has no opinion about would be
// silently absent above; `Unsigned` is named here so a kind added to the wire
// without a name in `kind_name` fails to compile rather than printing
// "unknown".
const _: fn(&Unsigned) -> Kind = |u| u.kind();
