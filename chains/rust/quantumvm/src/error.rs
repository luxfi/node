// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What this chain refuses for.
//!
//! One enum for the whole chain, because a refusal crosses module boundaries on
//! its way out: a wire that does not decode is refused by the parser, reported
//! by the VM and rendered by the host seam, and three spellings of one fact
//! would be three things to keep in step.
//!
//! Every variant names a rule. There is deliberately no `Other(String)`: a
//! catch-all is where a rule goes to stop being checked.

use std::fmt;

use crate::ids::{self, Id};
use lux_zap::zap;

/// Why the chain said no.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    // ---- wire ----
    /// The bytes are not a ZAP message at all.
    Zap(zap::Error),
    /// A message declares a size its buffer does not match.
    Trailing(&'static str),
    /// A wire ends inside the fixed section it declares.
    Truncated {
        what: &'static str,
        have: usize,
        need: usize,
    },
    /// The block exceeds the wire bound.
    TooLarge {
        bytes: usize,
        limit: usize,
    },
    /// The bytes decode, and re-encoding what came out does not reproduce them.
    /// One block, one byte string: anything else is two ids for one block.
    NonCanonical,
    /// The transaction count is larger than the blob could hold.
    TxCountAbsurd {
        count: usize,
        bytes: usize,
    },
    /// The lengths do not partition the transaction blob exactly.
    TxBlobMismatch(String),
    /// Transaction `index` of a block did not decode.
    TxMalformed {
        index: usize,
        why: Box<Error>,
    },

    // ---- pool ----
    PoolClosed,
    PoolFull,
    DuplicateTx(Id),
    TxNotInPool(Id),
    /// A transaction with no quantum signature. Q-Chain's transactions are
    /// attestations; one without a stamp attests nothing.
    MissingStamp,

    // ---- block rules ----
    /// The block names another chain or another network.
    ForeignChain {
        chain: Id,
        network: u32,
        this_chain: Id,
        this_network: u32,
    },
    /// A block proposed at height 0. Genesis is written by the VM, never
    /// proposed.
    Genesis,
    /// The block does not sit one height above the parent it names.
    InvalidHeight {
        height: u64,
        parent: u64,
    },
    /// A proposed block carries work, and refusing an empty one is what keeps
    /// the signature check from being satisfiable by removing its subject.
    EmptyBlock,
    ParentNotFound(Id),
    TimeBeforeParent {
        block: i64,
        parent: i64,
    },
    TimeTooFarAhead {
        block: i64,
        limit: i64,
    },
    /// A transaction in the block failed its ML-DSA check.
    BlockSignature(String),
    /// A transaction could not be applied. A node that cannot apply an agreed
    /// block stops rather than committing a chain its state no longer matches.
    Execute {
        tx: Id,
        why: String,
    },

    // ---- the chain ----
    /// The node has no identity to sign under, or no chain to serve.
    NoIdentity,
    /// The tip could not be READ. Distinct from a chain that holds no block:
    /// collapsing the two is how one transient failure committed genesis over a
    /// live chain.
    TipUnreadable(String),
    /// The block does not extend the last accepted one.
    NotTheTip(String),
    /// The node's clock trails its own tip beyond the skew allowance, so every
    /// block it could build now carries a timestamp its own verify refuses.
    ClockBehindTip {
        tip: i64,
        now: i64,
    },
    NoBlockAtHeight(u64),
    NoPendingTxs,
    /// Every pending transaction failed verification, so there is nothing to
    /// put in a block.
    NoneSurvived,
    ShuttingDown,
    /// Q-Chain has no user-payable blockspace (LP-0130 §6).
    FeeRefused(u64),
    /// A configuration that cannot be run.
    Config(String),

    // ---- signing ----
    /// A parameter set that does not exist, or one this build cannot sign under.
    Algorithm(String),
    /// The stamp is outside its validity window — in either direction.
    StampExpired {
        age_nanos: i64,
        window_nanos: i64,
    },
    /// The ML-DSA signature does not check out.
    SignatureRefused,
    /// There is no stamp to check.
    NoStamp,
    /// Signing failed inside the crypto library.
    Signing(String),

    // ---- the committee ----
    NoValidatorId,
    AlreadyRegistered(String),
    CommitteeFull {
        have: usize,
        committee: usize,
    },
    /// A validator that already contributed a signature for this block sent
    /// another. The quorum counts signers, so a second signature from one
    /// validator would let a single peer reach the threshold by resending.
    DuplicateSigner(String),
    /// The signature does not verify against the registered key of the
    /// validator it names, over the block it names.
    UnverifiedSigner(String),
    UnknownBlock(Id),
    /// The count was reached and the aggregate did not verify. Reaching the
    /// count is necessary and not sufficient.
    AggregateRefused(Id),

    // ---- storage ----
    /// The store could not answer or could not commit.
    Store(String),
    /// No value under that key.
    NotFound,
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use Error::*;
        match self {
            Zap(e) => write!(f, "quantumvm: {e}"),
            Trailing(what) => write!(f, "quantumvm: {what} trailing bytes"),
            Truncated { what, have, need } => write!(
                f,
                "quantumvm: {what} wire ends {have} bytes into a {need}-byte header"
            ),
            TooLarge { bytes, limit } => {
                write!(f, "quantumvm: block exceeds the wire bound: {bytes} over {limit}")
            }
            NonCanonical => write!(
                f,
                "quantumvm: block wire is not the canonical encoding of its content"
            ),
            TxCountAbsurd { count, bytes } => write!(
                f,
                "quantumvm: block declares more transactions than its bytes can hold: {count} in {bytes} bytes"
            ),
            TxBlobMismatch(why) => write!(
                f,
                "quantumvm: transaction lengths do not partition the transaction blob: {why}"
            ),
            TxMalformed { index, why } => write!(f, "quantumvm: transaction {index}: {why}"),
            PoolClosed => write!(f, "quantumvm: transaction pool is closed"),
            PoolFull => write!(f, "quantumvm: transaction pool is full"),
            DuplicateTx(id) => write!(f, "quantumvm: transaction {} already in the pool", ids::hex(id)),
            TxNotInPool(id) => write!(f, "quantumvm: transaction {} not in the pool", ids::hex(id)),
            MissingStamp => write!(f, "quantumvm: missing quantum signature"),
            ForeignChain {
                chain,
                network,
                this_chain,
                this_network,
            } => write!(
                f,
                "quantumvm: block belongs to another chain: chain {} network {}, this node serves chain {} network {}",
                ids::hex(chain),
                network,
                ids::hex(this_chain),
                this_network
            ),
            Genesis => write!(
                f,
                "quantumvm: height 0 is genesis, which the VM writes and no peer proposes"
            ),
            InvalidHeight { height, parent } => write!(
                f,
                "quantumvm: invalid block height: {height} does not follow parent {parent}"
            ),
            EmptyBlock => write!(f, "quantumvm: block carries no transactions"),
            ParentNotFound(id) => write!(f, "quantumvm: parent block not found: {}", ids::hex(id)),
            TimeBeforeParent { block, parent } => write!(
                f,
                "quantumvm: block timestamp precedes its parent: {block} precedes {parent}"
            ),
            TimeTooFarAhead { block, limit } => write!(
                f,
                "quantumvm: block timestamp is beyond the skew allowance: {block} exceeds {limit}"
            ),
            BlockSignature(why) => write!(
                f,
                "quantumvm: block transaction signatures failed verification: {why}"
            ),
            Execute { tx, why } => write!(
                f,
                "quantumvm: block transaction could not be applied: {}: {why}",
                ids::hex(tx)
            ),
            NoIdentity => write!(f, "quantumvm: the node has no identity to sign under"),
            TipUnreadable(why) => write!(f, "quantumvm: the chain tip cannot be read: {why}"),
            NotTheTip(why) => write!(f, "quantumvm: block does not extend the tip: {why}"),
            ClockBehindTip { tip, now } => write!(
                f,
                "quantumvm: the node's clock trails its own tip beyond the skew allowance: tip is stamped {tip}, this node reads {now}"
            ),
            NoBlockAtHeight(h) => write!(f, "quantumvm: no block at height {h}"),
            NoPendingTxs => write!(f, "quantumvm: no pending transactions"),
            NoneSurvived => write!(
                f,
                "quantumvm: no pending transaction survived verification"
            ),
            ShuttingDown => write!(f, "quantumvm: VM is shutting down"),
            FeeRefused(fee) => write!(
                f,
                "quantumvm: Q-Chain sells no blockspace (LP-0130 §6); a fee of {fee} nLUX buys nothing"
            ),
            Config(why) => write!(f, "quantumvm: config: {why}"),
            Algorithm(why) => write!(f, "quantumvm: {why}"),
            StampExpired { age_nanos, window_nanos } => write!(
                f,
                "quantumvm: quantum stamp expired: {age_nanos}ns from now, window is ±{window_nanos}ns"
            ),
            SignatureRefused => write!(f, "quantumvm: quantum verification failed"),
            NoStamp => write!(f, "quantumvm: no stamp to check"),
            Signing(why) => write!(f, "quantumvm: signing failed: {why}"),
            NoValidatorId => write!(
                f,
                "quantumvm: a signer with no identity cannot be a member of a quorum"
            ),
            AlreadyRegistered(v) => write!(f, "quantumvm: validator is already in the committee: {v}"),
            CommitteeFull { have, committee } => {
                write!(f, "quantumvm: the committee is full: {have} of {committee}")
            }
            DuplicateSigner(v) => write!(f, "quantumvm: validator already signed this block: {v}"),
            UnverifiedSigner(why) => write!(
                f,
                "quantumvm: signature does not verify for the validator it claims: {why}"
            ),
            UnknownBlock(id) => write!(
                f,
                "quantumvm: no block awaiting signatures: {}",
                ids::hex(id)
            ),
            AggregateRefused(id) => write!(
                f,
                "quantumvm: the aggregate of the collected signatures does not verify: {}",
                ids::hex(id)
            ),
            Store(why) => write!(f, "quantumvm: store: {why}"),
            NotFound => write!(f, "quantumvm: not found"),
        }
    }
}

impl std::error::Error for Error {}

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
            NotFound | ParentNotFound(_) | NoBlockAtHeight(_) | UnknownBlock(_) => {
                crate::host::Error::NotFound
            }
            Zap(_)
            | Trailing(_)
            | Truncated { .. }
            | NonCanonical
            | TxCountAbsurd { .. }
            | TxBlobMismatch(_)
            | TxMalformed { .. } => crate::host::Error::Malformed(e.to_string()),
            NoPendingTxs | NoneSurvived | EmptyBlock => crate::host::Error::Empty,
            FeeRefused(_) | Config(_) => crate::host::Error::BadRequest(e.to_string()),
            _ => crate::host::Error::Invalid(e.to_string()),
        }
    }
}

pub type Result<T> = std::result::Result<T, Error>;
