// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What this chain refuses for, and which class a refusal falls in.
//!
//! Every refusal carries a [`Code`] — one of the sentinels Go declares in
//! `fhevm/vm.go` and `chains/fee` — and a detail that says which instance of it
//! this was. The two are kept apart for one reason, and it is the reason the
//! differential exists: Go classifies a refusal by unwrapping to the DEEPEST
//! error and reading that, so `fmt.Errorf("fhevm: %w: unknown permit operation
//! bits", ErrInvalidPayload)` classifies on `fhevm: invalid transaction
//! payload` and NOT on the sentence around it. Classifying the sentence instead
//! would read the word "unknown" and answer UNSUPPORTED where Go answers
//! SYNTACTIC — a disagreement invented by the evaluator rather than found in
//! the chain.
//!
//! So [`Code::text`] is the Go sentinel VERBATIM, [`classify`] reads only that,
//! and the detail rides into the conformance note beside the class where the
//! runner does not compare it.

/// A refusal, named by the sentinel Go names it by.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Code {
    // ---- the chain ----
    Shutdown,
    NoPendingTxs,
    NoParentBlock,
    ClockBehind,
    NotOnTip,
    InvalidBlock,
    MempoolFull,
    InvalidTxType,
    InvalidPayload,
    UnknownScheme,
    InvalidThreshold,
    InvalidCommittee,
    HandleMismatch,

    // ---- authorization of the payer ----
    UnsignedTx,
    PayerMismatch,
    BadPublicKey,
    BadSignature,

    // ---- what the chain holds ----
    CiphertextExists,
    CiphertextNotFound,
    PermitNotFound,
    PermitRevoked,
    PermitExpired,
    PermitInvalid,
    RequestNotFound,
    RequestClosed,
    RequestExpired,
    EpochNotFound,
    EpochMismatch,
    NotCommittee,
    Unauthorized,
    BadNonce,
    DuplicateEffect,

    // ---- settlement (chains/fee) ----
    InsufficientFunds,
    BalanceOverflow,
    OutOfGas,

    // ---- the wire (luxfi/zap) ----
    Zap,

    // ---- an unpriceable operation: gas.go returns these unwrapped ----
    UnknownTxTypeGas,
}

impl Code {
    /// The Go sentinel's own words. This is what [`classify`] reads and what a
    /// C++ `name(code)` returns for the same refusal, so the three evaluators
    /// classify the same string.
    pub fn text(self) -> &'static str {
        match self {
            Code::Shutdown => "fhevm: shutting down",
            Code::NoPendingTxs => "fhevm: no pending transactions",
            Code::NoParentBlock => "fhevm: no parent block",
            Code::ClockBehind => {
                "fhevm: local clock trails the chain tip by more than the skew allowance"
            }
            Code::NotOnTip => "fhevm: block does not extend the accepted tip",
            Code::InvalidBlock => "fhevm: malformed block",
            Code::MempoolFull => "fhevm: mempool is full",
            Code::InvalidTxType => "fhevm: invalid transaction type",
            Code::InvalidPayload => "fhevm: invalid transaction payload",
            Code::UnknownScheme => "fhevm: unsupported FHE scheme",
            Code::InvalidThreshold => "fhevm: invalid threshold (need 0 < t <= n)",
            Code::InvalidCommittee => "fhevm: invalid committee",
            Code::HandleMismatch => "fhevm: subject does not match its payload",
            Code::UnsignedTx => "fhevm: transaction missing payer auth/signature",
            Code::PayerMismatch => "fhevm: payer does not match auth public key",
            // The reference wraps the crypto package's own error here rather
            // than a sentinel of its own, so what a classifier reads is that
            // package's words and not F's.
            Code::BadPublicKey => "invalid key size",
            Code::BadSignature => "fhevm: invalid payer signature",
            Code::CiphertextExists => "fhevm: ciphertext already registered",
            Code::CiphertextNotFound => "fhevm: ciphertext not found",
            Code::PermitNotFound => "fhevm: permit not found",
            Code::PermitRevoked => "fhevm: permit is revoked",
            Code::PermitExpired => "fhevm: permit expired",
            Code::PermitInvalid => "fhevm: permit does not authorize this operation",
            Code::RequestNotFound => "fhevm: decrypt request not found",
            Code::RequestClosed => "fhevm: decrypt request already answered",
            Code::RequestExpired => "fhevm: decrypt request expired",
            Code::EpochNotFound => "fhevm: epoch not found",
            Code::EpochMismatch => "fhevm: epoch is not the next one",
            Code::NotCommittee => "fhevm: payer is not a committee member",
            Code::Unauthorized => "fhevm: payer not authorized for operation",
            Code::BadNonce => "fhevm: bad or replayed nonce",
            Code::DuplicateEffect => "fhevm: effect already claimed by a pending transaction",
            Code::InsufficientFunds => "fee: insufficient funds",
            Code::BalanceOverflow => "fee: balance overflow",
            Code::OutOfGas => "fee: out of gas",
            Code::Zap => "zap",
            // gas.go builds this one with the type number in it and does NOT
            // wrap a sentinel, so the whole sentence is what Go classifies —
            // and the word "unknown" in it is load-bearing.
            Code::UnknownTxTypeGas => "fhevm gas: unknown tx type",
        }
    }
}

/// A refusal: which rule, and which instance of it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Error {
    pub code: Code,
    pub detail: String,
}

impl Error {
    /// A bare sentinel — the refusal Go returns without wrapping.
    pub fn new(code: Code) -> Error {
        Error { code, detail: String::new() }
    }

    /// A sentinel wrapped in the sentence that says which instance it is. Go
    /// writes `fmt.Errorf("fhevm: %w: <detail>", Sentinel)`, and the note below
    /// reproduces that shape so a row is readable beside a Go one.
    pub fn detail(code: Code, detail: impl Into<String>) -> Error {
        Error { code, detail: detail.into() }
    }

    /// The whole sentence, for the note.
    pub fn message(&self) -> String {
        if self.detail.is_empty() {
            self.code.text().to_string()
        } else {
            format!("fhevm: {}: {}", self.code.text(), self.detail)
        }
    }

    /// The class this refusal falls in.
    pub fn class(&self) -> &'static str {
        classify(self.code.text())
    }
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message())
    }
}

impl std::error::Error for Error {}

impl From<lux_zap::zap::Error> for Error {
    fn from(e: lux_zap::zap::Error) -> Error {
        Error { code: Code::Zap, detail: e.to_string() }
    }
}

/// A refusal, or a value.
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
/// The order is not decoration. A kind the chain does not recognise is refused
/// by NAME, and the sentence that refuses it usually lists the ones it would
/// accept — so "unknown" has to be tested before the ledger words, or a refusal
/// that never looked at the chain would be reported as one that did.
pub fn classify(sentinel: &str) -> &'static str {
    let s = sentinel.to_lowercase();
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_wrapped_sentinel_classifies_on_the_sentinel_and_not_the_sentence() {
        // The whole reason code and detail are apart. "unknown permit operation
        // bits" wraps ErrInvalidPayload; Go unwraps to the sentinel and answers
        // SYNTACTIC. Reading the sentence would find "unknown" and answer
        // UNSUPPORTED, which is a disagreement nobody's chain has.
        let e = Error::detail(Code::InvalidPayload, "unknown permit operation bits");
        assert_eq!(e.class(), SYNTACTIC);
        assert_eq!(classify("unknown permit operation bits"), UNSUPPORTED);
    }

    #[test]
    fn every_class_the_f_chain_can_reach() {
        assert_eq!(Error::new(Code::InvalidTxType).class(), SYNTACTIC);
        assert_eq!(Error::new(Code::HandleMismatch).class(), SYNTACTIC);
        assert_eq!(Error::new(Code::InvalidCommittee).class(), SYNTACTIC);
        assert_eq!(Error::new(Code::BadNonce).class(), SYNTACTIC);
        assert_eq!(Error::new(Code::UnknownScheme).class(), UNSUPPORTED);
        assert_eq!(Error::new(Code::UnsignedTx).class(), AUTH);
        assert_eq!(Error::new(Code::BadSignature).class(), AUTH);
        assert_eq!(Error::new(Code::PayerMismatch).class(), AUTH);
        assert_eq!(Error::new(Code::Unauthorized).class(), AUTH);
        assert_eq!(Error::new(Code::CiphertextNotFound).class(), LEDGER);
        assert_eq!(Error::new(Code::PermitNotFound).class(), LEDGER);
        assert_eq!(Error::new(Code::RequestNotFound).class(), LEDGER);
        assert_eq!(Error::new(Code::EpochNotFound).class(), LEDGER);
        assert_eq!(Error::new(Code::InsufficientFunds).class(), LEDGER);
        assert_eq!(Error::new(Code::OutOfGas).class(), LEDGER);
        assert_eq!(Error::new(Code::BalanceOverflow).class(), OVERFLOW);
    }

    #[test]
    fn payer_mismatch_is_an_authorization_refusal_and_not_a_lookup() {
        // "payer does not match auth public key" reaches AUTH through the
        // `does not match auth` clause alone — nothing else in the table sees
        // it, and without that clause it would fall through to SYNTACTIC.
        assert_eq!(Code::PayerMismatch.text(), "fhevm: payer does not match auth public key");
        assert_eq!(Error::new(Code::PayerMismatch).class(), AUTH);
    }

    #[test]
    fn a_note_reads_like_the_go_one() {
        let e = Error::detail(Code::InvalidPayload, "register: empty ciphertext digest");
        assert_eq!(
            e.message(),
            "fhevm: fhevm: invalid transaction payload: register: empty ciphertext digest"
        );
        assert_eq!(Error::new(Code::InvalidTxType).message(), "fhevm: invalid transaction type");
    }
}
