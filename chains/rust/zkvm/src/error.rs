// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What this chain refuses for.
//!
//! One enum for the whole chain, because a refusal crosses module boundaries on
//! its way out: a wire that does not decode is refused by the parser, reported
//! by the VM and rendered by the host seam, and three spellings of one fact
//! would be three things to keep in step.
//!
//! THE WORDS ARE THE REFERENCE'S WORDS. Every message here is the string Go's
//! `chains/zkvm` writes for the same refusal, character for character. Two
//! things depend on that and they are different things. The differential's note
//! column carries each implementation's own words beside the verdict class, so
//! a disagreement can be read without opening three debuggers — and identical
//! words make a row that disagrees on a class obviously about the class. And
//! the class itself is decided by a table of words: `SYNTACTIC` versus
//! `LEDGER` versus `AUTH` is read off the message, in all three implementations,
//! from one table in one order. A message that said the same thing differently
//! would be a verdict that differed for no reason a chain could name.
//!
//! [`Error::root`] is the other half of that. Go's classifier reads the
//! DEEPEST error — `errors.Unwrap` to the bottom — because the executor wraps,
//! and reading a wrapper instead of its cause would give every wrapped failure
//! one class. The variants that wrap here hold their cause and unwrap the same
//! way.

use std::fmt;

use crate::ids::{self, Id};
use crate::zap;

/// Why the chain said no.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    // ---- the wire ----
    /// The bytes are not a ZAP message at all.
    Zap(zap::Error),
    /// The frame declares fewer bytes than were handed to the parser. One value
    /// has one byte string, so the remainder belongs to nobody.
    TrailingBytes,
    /// A declared length vector does not exactly cover the blob it indexes:
    /// either a length reaches past the end, or blob bytes are left that no
    /// length claims.
    Length(Option<(&'static str, usize)>),

    // ---- a transaction's own shape ----
    InvalidTransactionType,
    NoInputs,
    NoOutputs,
    MissingProof,
    NoExpiry,
    Expired,
    InvalidTransfer,
    InvalidShield,
    InvalidUnshield,

    // ---- a block's own shape ----
    /// Height 0 with a parent, which is two claims about which block it is.
    InvalidBlock,
    /// More transactions than this chain would ever build into one block.
    TxCap { txs: usize, cap: u32 },
    FutureBlock,
    InvalidHeight,
    InvalidTimestamp,
    InvalidStateRoot,
    /// The same nullifier twice inside one block or vertex: one shielded note
    /// spent twice.
    DuplicateNullifier,
    /// The parent is neither the accepted tip nor a block verified above it.
    NotOnTip(String),
    /// A vertex arrived where a block was asked for.
    NotABlock(Id),
    InvalidParentType,

    // ---- what the chain holds ----
    NoBlock(Id),
    NullifierSpent,
    /// The spent set could not be READ. Reporting "not spent" for a set that
    /// could not be read is how an already-spent note gets spent again.
    SpentSetRead(Box<Error>),
    NoUtxo,
    UtxoExists,
    UtxoRead(Box<Error>),
    StateRootRead(Box<Error>),
    StateRootSize(usize),
    NullifierRecord,

    // ---- the proof ----
    /// Everything the proof verifier refused, under one wrapper, because that
    /// is the one Go's `verifyTransaction` puts on it.
    ProofVerification(Box<Error>),
    /// A classical, pairing-based system on a chain that runs only STARK/FRI.
    StrictPqClassicalForbidden,
    /// A real bn254 key on a strict-PQ chain would re-enable the forgeable
    /// pairing path. Refused where the key is loaded, not where it is used.
    StrictPqRealVkForbidden,
    /// The STARK/FRI verifier judged the proof and said no.
    StarkFailed(Box<Error>),
    /// No FRI binding is registered, so nothing judged it. Distinguishable from
    /// a proof that was judged, and refused either way.
    StarkUnbound(Box<Error>),
    StarkRejected,
    /// A proof that does not begin with the wire tag the verifier reads.
    StarkInvalidProof,
    StarkVerifierNotRegistered,
    ProofVerificationDisabled,
    UnsupportedProofType,
    BulletproofsUnimplemented,
    NoVerifyingKey(u8),
    /// The public inputs do not say what the transaction spends.
    PublicInputs(&'static str),
    Groth16ProofLength,
    /// This port carries no bn254 pairing arithmetic, so a Groth16 proof that
    /// reaches the equation is refused rather than judged. Same posture the
    /// STARK path takes with no FRI binding, and for the same reason: a proof
    /// nothing verified is never accepted.
    Groth16Unbound,
    PlonkProofLength,
    PlonkIncomplete,

    // ---- the pool and the chain ----
    NoTransactions,
    NullifierInMempool,
    MempoolFull,
    /// A store could not answer or could not commit.
    Store(String),
    /// No value under that key. Distinct from a store that could not answer:
    /// collapsing the two is how a chain reads an unreadable disk as an empty
    /// one.
    NotFound,
    /// A configuration that cannot be run.
    Config(String),
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use Error::*;
        match self {
            Zap(e) => write!(f, "{e}"),
            TrailingBytes => write!(f, "zkvm wire: trailing bytes"),
            Length(None) => write!(f, "zkvm wire: declared length does not match blob"),
            Length(Some((what, i))) => {
                write!(f, "zkvm wire: declared length does not match blob: {what} {i}")
            }

            InvalidTransactionType => write!(f, "invalid transaction type"),
            NoInputs => write!(f, "transaction has no inputs"),
            NoOutputs => write!(f, "transaction has no outputs"),
            MissingProof => write!(f, "transaction missing proof"),
            NoExpiry => write!(f, "transaction names no expiry height"),
            Expired => write!(f, "transaction has expired"),
            InvalidTransfer => write!(f, "invalid transfer transaction"),
            InvalidShield => write!(f, "invalid shield transaction"),
            InvalidUnshield => write!(f, "invalid unshield transaction"),

            InvalidBlock => write!(f, "invalid block"),
            TxCap { txs, cap } => {
                write!(f, "invalid block: {txs} transactions over the {cap} cap")
            }
            FutureBlock => write!(f, "block timestamp too far in future"),
            InvalidHeight => write!(f, "invalid block height"),
            InvalidTimestamp => write!(f, "invalid block timestamp"),
            InvalidStateRoot => write!(f, "invalid state root"),
            DuplicateNullifier => write!(f, "nullifier spent twice in one block"),
            NotOnTip(why) => write!(
                f,
                "zkvm: block does not extend the accepted tip: {why}"
            ),
            NotABlock(id) => write!(f, "chain: no such block: {} is not a block", ids::hex(id)),
            InvalidParentType => write!(f, "invalid parent block type"),

            NoBlock(id) => write!(f, "chain: no such block: {}", ids::hex(id)),
            NullifierSpent => write!(f, "zkvm: nullifier already spent"),
            SpentSetRead(e) => write!(f, "zkvm: read spent set: {e}"),
            NoUtxo => write!(f, "zkvm: no such utxo"),
            UtxoExists => write!(f, "UTXO already exists"),
            UtxoRead(e) => write!(f, "zkvm: read utxo: {e}"),
            StateRootRead(e) => write!(f, "zkvm: read state root: {e}"),
            StateRootSize(n) => write!(f, "zkvm: state root is {n} bytes, want 32"),
            NullifierRecord => write!(f, "zkvm: nullifier record is not a height"),

            ProofVerification(e) => write!(f, "proof verification failed: {e}"),
            StrictPqClassicalForbidden => write!(
                f,
                "zkvm: classical proof system forbidden on strict-PQ chain — only STARK/FRI accepted"
            ),
            StrictPqRealVkForbidden => write!(
                f,
                "zkvm: real bn254 verifying key forbidden on strict-PQ chain — shielded value uses STARK/FRI (P3Q) only"
            ),
            StarkFailed(e) => write!(f, "zkvm: STARK shielded proof verification failed: {e}"),
            StarkUnbound(e) => write!(
                f,
                "zkvm: strict-PQ STARK shielded verifier unbound (build with -tags starkfri_p3q): {e}"
            ),
            StarkRejected => write!(f, "zkvm: STARK shielded proof rejected"),
            StarkInvalidProof => write!(f, "starkfri: proof verification failed"),
            StarkVerifierNotRegistered => write!(f, "starkfri: verifier not registered"),
            ProofVerificationDisabled => write!(
                f,
                "zkvm: proof verification disabled — no real verifying keys loaded"
            ),
            UnsupportedProofType => write!(f, "unsupported proof type"),
            BulletproofsUnimplemented => write!(
                f,
                "zkvm: Bulletproof verification not yet implemented, use groth16 or plonk"
            ),
            NoVerifyingKey(circuit) => {
                write!(f, "zkvm: no verifying key for circuit {circuit}")
            }
            PublicInputs(why) => write!(f, "{why}"),
            Groth16ProofLength => write!(f, "invalid proof data length for Groth16"),
            Groth16Unbound => write!(
                f,
                "groth16: the bn254 pairing equation is not carried here — failing closed rather \
                 than accepting a proof nothing checked; use the STARK/FRI verifier"
            ),
            PlonkProofLength => {
                write!(f, "invalid PLONK proof data length: expected 544+ bytes")
            }
            PlonkIncomplete => write!(
                f,
                "plonk: the verification equation is not implemented — failing closed rather than \
                 binding a proof to nothing; use the STARK/FRI verifier on strict-PQ chains"
            ),

            NoTransactions => write!(f, "zkvm: nothing to propose"),
            NullifierInMempool => write!(f, "nullifier already in mempool"),
            MempoolFull => write!(
                f,
                "mempool is full and the transaction pays less than what it would displace"
            ),
            Store(why) => write!(f, "zkvm: store: {why}"),
            NotFound => write!(f, "not found"),
            Config(why) => write!(f, "failed to parse config: {why}"),
        }
    }
}

impl std::error::Error for Error {}

impl Error {
    /// The deepest cause.
    ///
    /// The verdict class is read off a message, and a wrapper is not the thing
    /// that went wrong: "proof verification failed" is the same wrapper over a
    /// forbidden proof system and over one that failed to verify, and the two
    /// are `UNSUPPORTED` and `AUTH`. Reading the wrapper would give both the
    /// same class and the differential would then be comparing one word against
    /// three chains.
    pub fn root(&self) -> &Error {
        use Error::*;
        match self {
            ProofVerification(e) | StarkFailed(e) | StarkUnbound(e) | SpentSetRead(e)
            | UtxoRead(e) | StateRootRead(e) => e.root(),
            other => other,
        }
    }

    /// Whether this refusal was reached WITHOUT the chain — from the block and
    /// its transactions alone.
    ///
    /// The Z-chain's reference has ONE verification pass: `Block.Verify` runs
    /// the shape rules, then the proofs, then the parent lookup, and returns
    /// the first refusal. There is no separate syntactic pass to read, so the
    /// split the differential compares is stated here, over the rules rather
    /// than over the code that happens to hold them — which matters, because
    /// three of these are BLOCK-level (a height-0 block with a parent, the
    /// clock, a nullifier repeated across two transactions) and could not live
    /// in a per-transaction pass at all.
    ///
    /// Everything not named here needed the spent set, the proof verifier, the
    /// parent or the tip.
    pub fn before_the_chain(&self) -> bool {
        use Error::*;
        matches!(
            self,
            InvalidBlock
                | TxCap { .. }
                | FutureBlock
                | DuplicateNullifier
                | InvalidTransactionType
                | NoInputs
                | NoOutputs
                | MissingProof
                | NoExpiry
                | Expired
                | InvalidTransfer
                | InvalidShield
                | InvalidUnshield
        )
    }
}

impl From<zap::Error> for Error {
    fn from(e: zap::Error) -> Self {
        Error::Zap(e)
    }
}

/// The host's shape for the same refusal.
///
/// The mapping is by MEANING, not by module: a wire that does not decode is
/// `Malformed` whoever noticed it, a rule the block broke is `Invalid`, and a
/// build with nothing to build is `Empty` — which is the answer the engine
/// polls on and must not read as a failure.
impl From<Error> for crate::host::Error {
    fn from(e: Error) -> Self {
        use Error::*;
        match &e {
            NotFound | NoBlock(_) | NotABlock(_) | NoUtxo => crate::host::Error::NotFound,
            Zap(_) | TrailingBytes | Length(_) => crate::host::Error::Malformed(e.to_string()),
            NoTransactions => crate::host::Error::Empty,
            Config(_) | MempoolFull | NullifierInMempool => {
                crate::host::Error::BadRequest(e.to_string())
            }
            _ => crate::host::Error::Invalid(e.to_string()),
        }
    }
}

pub type Result<T> = std::result::Result<T, Error>;

// ---- the shared verdict vocabulary ----

pub const OK: &str = "OK";
pub const MALFORMED: &str = "MALFORMED";
pub const SYNTACTIC: &str = "SYNTACTIC";
pub const OVERFLOW: &str = "OVERFLOW";
pub const LEDGER: &str = "LEDGER";
pub const AUTH: &str = "AUTH";
pub const WARP: &str = "WARP";
pub const UNSUPPORTED: &str = "UNSUPPORTED";

/// The same word table the Go and C++ evaluators carry, in the same order.
///
/// The order is not decoration. A system, kind or token the chain does not
/// recognise is refused by NAME, and the sentence that refuses it usually lists
/// the ones it would accept — so "unknown" and "forbidden" have to be tested
/// before the ledger words, or a refusal that never looked at the chain would
/// be reported as one that did.
pub fn classify(sentence: &str) -> &'static str {
    let s = sentence.to_lowercase();
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
        // A zero-knowledge proof IS the credential on a shielded chain.
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

impl Error {
    /// The class this refusal falls in, read off the DEEPEST cause.
    ///
    /// A wrapper is not the thing that went wrong: "proof verification failed"
    /// wraps both a forbidden proof system and one that failed to verify, and
    /// the two are `UNSUPPORTED` and `AUTH`.
    pub fn class(&self) -> &'static str {
        classify(&self.root().to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_wrapper_reports_its_cause_and_classifies_as_it() {
        let e = Error::ProofVerification(Box::new(Error::StrictPqClassicalForbidden));
        assert_eq!(
            e.to_string(),
            "proof verification failed: zkvm: classical proof system forbidden on strict-PQ chain \
             — only STARK/FRI accepted"
        );
        assert_eq!(e.root(), &Error::StrictPqClassicalForbidden);
    }

    #[test]
    fn two_wrappers_deep_still_reaches_the_bottom() {
        let e = Error::ProofVerification(Box::new(Error::StarkFailed(Box::new(
            Error::StarkInvalidProof,
        ))));
        assert_eq!(e.root(), &Error::StarkInvalidProof);
        assert_eq!(
            e.to_string(),
            "proof verification failed: zkvm: STARK shielded proof verification failed: \
             starkfri: proof verification failed"
        );
    }

    #[test]
    fn a_leaf_is_its_own_root() {
        assert_eq!(Error::InvalidStateRoot.root(), &Error::InvalidStateRoot);
    }

    /// The three block-level rules the reference decides before it reads
    /// anything, and the transaction-level ones beside them. This is the split
    /// the differential compares, so it is asserted rather than assumed.
    #[test]
    fn the_split_names_the_rules_decided_without_the_chain() {
        for e in [
            Error::InvalidBlock,
            Error::TxCap { txs: 101, cap: 100 },
            Error::FutureBlock,
            Error::DuplicateNullifier,
            Error::InvalidTransactionType,
            Error::NoInputs,
            Error::NoOutputs,
            Error::MissingProof,
            Error::NoExpiry,
            Error::Expired,
            Error::InvalidTransfer,
            Error::InvalidShield,
            Error::InvalidUnshield,
        ] {
            assert!(e.before_the_chain(), "{e} is decided from the block alone");
        }
    }

    /// Height and timestamp are refused only once the parent has been read, so
    /// they are NOT in the split above — even though both messages open with
    /// the same two words as `invalid block`, which is the trap the Go
    /// evaluator's sentinel list has to write out longhand.
    #[test]
    fn the_split_leaves_out_what_needed_the_parent_or_the_verifier() {
        for e in [
            Error::InvalidHeight,
            Error::InvalidTimestamp,
            Error::InvalidStateRoot,
            Error::NoBlock(ids::EMPTY),
            Error::NullifierSpent,
            Error::ProofVerification(Box::new(Error::StrictPqClassicalForbidden)),
            Error::ProofVerification(Box::new(Error::StarkFailed(Box::new(
                Error::StarkInvalidProof,
            )))),
        ] {
            assert!(!e.before_the_chain(), "{e} needed the chain");
        }
    }

    #[test]
    fn the_host_reads_a_bad_wire_as_malformed_and_a_broken_rule_as_invalid() {
        let malformed: crate::host::Error = Error::TrailingBytes.into();
        assert!(matches!(malformed, crate::host::Error::Malformed(_)));
        let invalid: crate::host::Error = Error::InvalidStateRoot.into();
        assert!(matches!(invalid, crate::host::Error::Invalid(_)));
        let empty: crate::host::Error = Error::NoTransactions.into();
        assert_eq!(empty, crate::host::Error::Empty);
        let missing: crate::host::Error = Error::NoBlock(ids::EMPTY).into();
        assert_eq!(missing, crate::host::Error::NotFound);
    }
}
