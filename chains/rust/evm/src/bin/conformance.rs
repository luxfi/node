// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The Rust EVM's answers to the shared precompile corpus.
//!
//! One of the evaluators — Go, Rust, C++ — that read
//! `conformance/corpus/precompile_vectors.tsv` and print the same five fields
//! per vector. The runner compares them; this program never sees another
//! implementation's answer and has nothing to agree with. See
//! `conformance/PRECOMPILE.md`.
//!
//! Usage: `conformance <precompile_vectors.tsv>`
//!
//! # The seam
//!
//! Every call goes through [`PrecompileProvider::run`] on
//! [`lux_evm::Precompiles`], which is the object `lux_evm::block::run` installs
//! in the `Evm` it drives. That is the whole point: the answer here is the
//! answer a Rust node would give, including the classical-family gate, and not
//! a second reading of revm's tables. The context the call needs is real but
//! inert — a precompile reads nothing but its own input, so an empty database
//! is the same chain every implementation in this differential is standing on.
//!
//! # Gas
//!
//! Reported as CHARGED, which for a revm `Gas` is `limit - remaining`, i.e.
//! [`Gas::spent`]. revm records that charge inside the precompile: the cost is
//! computed and taken on the way to producing output, so a call that produced
//! output has been charged for it, and a call that refused the input has not.
//! Go's boundary is shaped the other way round — `RequiredGas` is a separate
//! method, deducted before `Run` is reached — so it charges a refusal too. The
//! difference is confined to this boundary: both machines then halt the frame
//! and consume everything the caller offered.
//!
//! So a refused call still has a price here, it is just not in the error. It
//! is recovered by offering less until the precompile objects: see
//! [`price_of_refusal`]. That keeps the gas column comparable on every row
//! rather than going quiet on the rows where an implementation refused, which
//! are the rows a differential most wants to read.

use std::ops::Range;

use lux_evm::{Precompiles, Profile};
use revm::context::{ContextTr, JournalTr, LocalContextTr};
use revm::handler::PrecompileProvider;
use revm::interpreter::{
    CallInput, CallInputs, CallScheme, CallValue, InstructionResult, InterpreterResult,
};
use revm::precompile::{PrecompileSpecId, Precompiles as Stock};
use revm::primitives::hardfork::SpecId;
use revm::primitives::{Address, Bytes, U256};
use revm::{Context, MainContext};

/// The revision the C-chain runs, which is the revision every implementation in
/// this differential answers at. `luxfi/evm` maps its Quasar fork to
/// `CancunTime` and knows no fork after it — there is no `OsakaTime` on a Lux
/// chain config for a network to set, and none sets one — so Cancun is where
/// every Lux chain ends. Asking a port at a later revision makes it serve
/// addresses no validator serves: at Osaka this table answers p256verify at
/// 0x0100, where the chain has an empty account.
const SPEC: SpecId = SpecId::CANCUN;

/// An empty column, spelled so it is not an empty column.
const NONE: &str = "-";

/// The five things an implementation can say about a call. The vocabulary is
/// the corpus's, not this crate's — `PrecompileError` and geth's
/// `ErrExecutionReverted` are two spellings of one answer, and spelling them
/// differently on the wire would be a disagreement invented by the harness.
const OK: &str = "OK";
const FAILED: &str = "FAILED";
/// What the runner reads as "this implementation declined to answer".
const SKIPPED: &str = "SKIPPED";
const OOG: &str = "OOG";
const ABSENT: &str = "ABSENT";

fn main() {
    let path = std::env::args().nth(1).unwrap_or_else(|| {
        eprintln!("usage: conformance <precompile_vectors.tsv>");
        std::process::exit(2);
    });
    let text = match std::fs::read_to_string(&path) {
        Ok(t) => t,
        Err(e) => {
            eprintln!("conformance: {path}: {e}");
            std::process::exit(1);
        }
    };

    // One set and one context for the whole corpus. A precompile touches
    // nothing but its own input, so there is no state for a vector to leave
    // behind for the next one, and standing the machine up 235 times would
    // only be 235 chances for the vectors to stop being independent.
    let mut precompiles = Precompiles::new(SPEC, Profile::default());
    let mut context = Context::mainnet();
    // Depth one: a call whose `to` is the precompile, which is what every
    // vector in this corpus is. revm keeps a refused call's message only at
    // that depth, and the message is what the note carries.
    context.journal_mut().checkpoint();

    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let v = match Vector::parse(line) {
            Ok(v) => v,
            Err(e) => {
                eprintln!("conformance: {e}");
                std::process::exit(1);
            }
        };
        println!("{}", run(&mut precompiles, &mut context, &v));
    }
}

/// One call: an address, what the caller was willing to pay, and the bytes.
struct Vector {
    id: String,
    address: Address,
    gas: u64,
    input: Bytes,
}

impl Vector {
    fn parse(line: &str) -> Result<Self, String> {
        let f: Vec<&str> = line.split('\t').collect();
        if f.len() != 5 || f[0] != "V" {
            return Err(format!("not a vector line: {line:?}"));
        }
        let address = unhex(f[2]).ok_or_else(|| format!("{}: address: {:?}", f[1], f[2]))?;
        if address.len() != 20 {
            return Err(format!("{}: address is {} bytes", f[1], address.len()));
        }
        Ok(Self {
            id: f[1].to_string(),
            address: Address::from_slice(&address),
            gas: f[3]
                .parse()
                .map_err(|e| format!("{}: gas: {e}", f[1]))?,
            input: Bytes::from(unhex(f[4]).ok_or_else(|| format!("{}: input", f[1]))?),
        })
    }
}

/// A Row answers a Vector. Gas is what was charged; on `OOG` and `ABSENT` it is
/// zero and the output is empty, because there is no charge to report and a
/// number nobody agrees to means the column stops comparing.
///
/// `None` prints as SKIPPED, which the runner counts and prints and never
/// scores as agreement. One thing still reaches it: a precompile that could
/// not run at all, revm's `PrecompileError::Fatal`, where no offer produces
/// an answer and so no price can be measured either. A refusal of the INPUT is
/// priced — see [`price_of_refusal`] — because the charge is recoverable there
/// even though revm's error does not carry it.
struct Row {
    id: String,
    status: &'static str,
    gas: Option<u64>,
    output: Bytes,
    note: String,
}

impl std::fmt::Display for Row {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let out = if self.output.is_empty() {
            NONE.to_string()
        } else {
            hex(&self.output)
        };
        let note = self.note.replace(['\t', '\n'], " ");
        let gas = match self.gas {
            Some(g) => g.to_string(),
            None => SKIPPED.to_string(),
        };
        write!(
            f,
            "R\t{}\t{}\t{}\t{}\t{}",
            self.id, self.status, gas, out, note
        )
    }
}

/// Answers one vector the way a Rust node would answer the call.
fn run<CTX: ContextTr>(precompiles: &mut Precompiles, context: &mut CTX, v: &Vector) -> Row {
    let who = name(&v.address);
    let call = inputs(v, v.gas);
    // Whether a refusal here has a charge that can be reported. A Lux module
    // splits price from body the way Go's does, so it knows what a call cost
    // even when it refused; revm computes the price inside the work and an
    // error carries none. Asked before the run, because the run borrows.
    let priced = precompiles.module(&v.address).is_some();

    // Bound, so the borrow the call takes ends here and the refusal path below
    // can ask the same precompiles what the call would have cost.
    let outcome = PrecompileProvider::<CTX>::run(precompiles, context, &call);
    match outcome {
        // Nothing at this address. revm says so by declining to produce a
        // result at all, which is the same sentence as Go's missing map entry.
        Ok(None) => Row {
            id: v.id.clone(),
            status: ABSENT,
            gas: Some(0),
            output: Bytes::new(),
            note: "no precompile at this address".into(),
        },
        Ok(Some(r)) => verdict(v, r, precompiles, context, who, priced),
        // A precompile that could not run at all — revm's `PrecompileError::
        // Fatal`, which is a missing trusted setup rather than a bad input.
        // It refused, so it is reported as a refusal, and the note says which
        // kind so the two are not read as the same thing.
        Err(e) => Row {
            id: v.id.clone(),
            status: FAILED,
            gas: None,
            output: Bytes::new(),
            note: format!("revm:{who}: fatal: {e}"),
        },
    }
}

fn verdict<CTX: ContextTr>(
    v: &Vector,
    r: InterpreterResult,
    precompiles: &mut Precompiles,
    context: &mut CTX,
    who: &str,
    priced: bool,
) -> Row {
    let id = v.id.clone();
    // `limit - remaining`. The conversion happens here, at the edge, and once.
    let charged = r.gas.spent();
    match r.result {
        InstructionResult::Return => Row {
            id,
            status: OK,
            gas: Some(charged),
            output: r.output,
            note: format!("revm:{who}"),
        },
        InstructionResult::PrecompileOOG => Row {
            id,
            status: OOG,
            gas: Some(0),
            output: Bytes::new(),
            note: format!("revm:{who}: out of gas"),
        },
        other => {
            // The message revm parked on the way past. Taking it also clears
            // it, so a refusal cannot be reported twice under two vectors.
            let why = context
                .local_mut()
                .take_precompile_error_context()
                .unwrap_or_else(|| format!("{other:?}"));
            // A Lux module charged for reading the input it then refused and
            // can say how much. Everything else is revm's, where the price
            // lives inside the function that does the work and the error
            // carries none away — so that price is measured rather than read.
            let gas = if priced || charged > 0 {
                charged
            } else {
                price_of_refusal(v, precompiles, context)
            };
            Row {
                id,
                status: FAILED,
                // A refusal that recorded a charge is one the classical gate
                // produced: `Precompiles::run` lets the precompile run and pay
                // before it discards the answer, so the price is already on the
                // meter and is read straight off it. A refusal from inside the
                // precompile recorded nothing — revm hands the `Gas` back
                // untouched — and nothing in the set costs nothing, so that
                // zero is the absence of a number. It is measured instead.
                gas: Some(gas),
                output: Bytes::new(),
                note: format!("revm:{who}: {why}"),
            }
        }
    }
}

/// What a refused call cost, for a precompile that computes its price inside
/// the work and whose error carries none away.
///
/// A price is the smallest offer a precompile accepts: below it the answer is
/// out of gas, and at it the answer is whatever the work arrives at — here, a
/// refusal. So the price is asked for rather than looked up, by bisecting the
/// offer. The vector's own limit already bought a refusal rather than an out
/// of gas, so the answer lies in `[0, v.gas]`, and a few dozen calls find it
/// exactly.
///
/// Measured, not tabulated, and that is the point. revm keeps these rules
/// private — `blake2`'s round cost is a bare `const` — so the alternative is a
/// second table of revm's numbers, which is the thing [`MODULES`] reuses
/// revm's arithmetic to avoid. A table drifts from the code it copies; this
/// cannot drift from the code it measures.
///
/// Zero is a real answer here, not a missing one: revm's blake2 rejects a
/// wrong input length before it reads the round count, so it demands nothing,
/// which is the 0 geth's `RequiredGas` returns for the same call.
fn price_of_refusal<CTX: ContextTr>(
    v: &Vector,
    precompiles: &mut Precompiles,
    context: &mut CTX,
) -> u64 {
    let mut lo = 0u64;
    let mut hi = v.gas;
    while lo < hi {
        let mid = lo + (hi - lo) / 2;
        if refused_for_want_of_gas(v, mid, precompiles, context) {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    // The probing calls parked messages of their own on the way past. Clearing
    // them here keeps a refusal from being reported again under a later vector.
    context.local_mut().take_precompile_error_context();
    lo
}

/// Whether this call answers "out of gas" when offered exactly `gas_limit`.
/// Monotone in the offer, which is what makes the bisection above exact.
fn refused_for_want_of_gas<CTX: ContextTr>(
    v: &Vector,
    gas_limit: u64,
    precompiles: &mut Precompiles,
    context: &mut CTX,
) -> bool {
    let call = inputs(v, gas_limit);
    matches!(
        PrecompileProvider::<CTX>::run(precompiles, context, &call),
        Ok(Some(r)) if r.result == InstructionResult::PrecompileOOG
    )
}

/// A top-level call to the precompile, and nothing else: no value, no static
/// context, no memory to return into. `CallInput::Bytes` rather than a shared
/// buffer, because the corpus hands over the bytes and there is no interpreter
/// here holding them.
fn inputs(v: &Vector, gas_limit: u64) -> CallInputs {
    CallInputs {
        input: CallInput::Bytes(v.input.clone()),
        return_memory_offset: Range::default(),
        gas_limit,
        bytecode_address: v.address,
        known_bytecode: None,
        target_address: v.address,
        caller: Address::ZERO,
        value: CallValue::Transfer(U256::ZERO),
        scheme: CallScheme::Call,
        is_static: false,
    }
}

/// What EIP-7910 calls the precompile at this address.
///
/// The note is never compared — an error string is a fact about a codebase,
/// not about a precompile — but the differential's whole subject is which
/// implementation answers where, and a note that said only the address would
/// leave the reader to look that up. The set read here is the same `'static`
/// table `EthPrecompiles` holds for this spec, so the label cannot name a
/// different precompile than the one that ran.
fn name(address: &Address) -> &'static str {
    Stock::new(PrecompileSpecId::from_spec_id(SPEC))
        .get(address)
        .map_or("none", |p| p.id().name())
}

fn hex(b: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(b.len() * 2);
    for c in b {
        s.push(DIGITS[(c >> 4) as usize] as char);
        s.push(DIGITS[(c & 0x0f) as usize] as char);
    }
    s
}

fn unhex(s: &str) -> Option<Vec<u8>> {
    if s == NONE {
        return Some(Vec::new());
    }
    let s = s.strip_prefix("0x").unwrap_or(s);
    if !s.len().is_multiple_of(2) {
        return None;
    }
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok())
        .collect()
}
