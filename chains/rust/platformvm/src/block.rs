// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The four things a P-Chain block can be.
//!
//! A **standard** block carries transactions people submitted and applies them
//! all. A **proposal** block carries one transaction the chain emitted about
//! itself and applies nothing — it states a question. A **commit** and an
//! **abort** block each answer the proposal above them, and carry nothing but
//! the answer.
//!
//! That is why a reward is two blocks and not one field: whether a validator
//! is paid is not something the transaction's author gets to say, and it is
//! not something a block producer gets to say either. The proposal is agreed
//! on first, and the answer is agreed on separately.
//!
//! One ZAP object per block, a kind byte at offset zero, and the bytes are
//! authoritative: a block is named by the hash of exactly the bytes it
//! arrived as. Trailing bytes are refused rather than ignored, because a
//! buffer with a tail wraps the same message and hashes to a different name —
//! two names for one block is two blocks.

use crate::ids::{hash256, Id};
use crate::txs::{self, Tx};
use crate::pchain_zap as w;
use lux_zap::zap;

/// Which block this is.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Kind {
    Abort = 1,
    Commit = 2,
    Proposal = 3,
    Standard = 4,
}

impl Kind {
    fn from_u8(v: u8) -> Option<Kind> {
        Some(match v {
            1 => Kind::Abort,
            2 => Kind::Commit,
            3 => Kind::Proposal,
            4 => Kind::Standard,
            _ => return None,
        })
    }
}



/// Why a buffer is not a block.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    Wire(zap::Error),
    UnknownKind(u8),
    /// Bytes beyond the self-delimiting message. A block must be exactly its
    /// own bytes or its name is not its name.
    ExtraSpace,
    /// A declared transaction length that runs past the blob.
    TxOverrunsBlob {
        index: usize,
        length: usize,
    },
    Tx(txs::Error),
    /// A proposal block with nothing to propose.
    NoProposalTx,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Wire(e) => write!(f, "{e}"),
            Error::UnknownKind(k) => write!(f, "zap: unknown block kind {k}"),
            Error::ExtraSpace => write!(f, "block: trailing bytes after zap message"),
            Error::TxOverrunsBlob { index, length } => {
                write!(f, "block: tx {index} length {length} overruns blob")
            }
            Error::Tx(e) => write!(f, "{e}"),
            Error::NoProposalTx => write!(f, "proposal block has no proposal tx"),
        }
    }
}

impl std::error::Error for Error {}

impl From<zap::Error> for Error {
    fn from(e: zap::Error) -> Self {
        Error::Wire(e)
    }
}

/// A block, with the bytes it was made from or arrived as.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Block {
    kind: Kind,
    parent: Id,
    height: u64,
    timestamp: u64,
    /// The transactions a standard block applies. Empty for the others.
    decision_txs: Vec<Tx>,
    /// The one transaction a proposal block states.
    proposal_tx: Option<Tx>,
    id: Id,
    bytes: Vec<u8>,
}

impl Block {
    /// A block of transactions people submitted.
    pub fn standard(parent: Id, height: u64, timestamp: u64, txs: Vec<Tx>) -> Block {
        let bytes = build(Kind::Standard, parent, height, timestamp, &txs, None);
        Block {
            kind: Kind::Standard,
            parent,
            height,
            timestamp,
            decision_txs: txs,
            proposal_tx: None,
            id: hash256(&bytes),
            bytes,
        }
    }

    /// A block stating one question the chain is asking about itself.
    pub fn proposal(parent: Id, height: u64, timestamp: u64, tx: Tx) -> Block {
        let bytes = build(Kind::Proposal, parent, height, timestamp, &[], Some(&tx));
        Block {
            kind: Kind::Proposal,
            parent,
            height,
            timestamp,
            decision_txs: Vec::new(),
            proposal_tx: Some(tx),
            id: hash256(&bytes),
            bytes,
        }
    }

    /// Yes to the proposal above.
    pub fn commit(parent: Id, height: u64, timestamp: u64) -> Block {
        Block::decided(Kind::Commit, parent, height, timestamp)
    }

    /// No to the proposal above.
    pub fn abort(parent: Id, height: u64, timestamp: u64) -> Block {
        Block::decided(Kind::Abort, parent, height, timestamp)
    }

    fn decided(kind: Kind, parent: Id, height: u64, timestamp: u64) -> Block {
        let bytes = build(kind, parent, height, timestamp, &[], None);
        Block {
            kind,
            parent,
            height,
            timestamp,
            decision_txs: Vec::new(),
            proposal_tx: None,
            id: hash256(&bytes),
            bytes,
        }
    }

    /// Read a block. The bytes are kept exactly and the name comes from them.
    pub fn parse(bytes: &[u8]) -> Result<Block, Error> {
        let msg = zap::Message::parse(bytes)?;
        if msg.size() != bytes.len() {
            return Err(Error::ExtraSpace);
        }
        let o = msg.root();
        let raw = o.u8(w::DECIDED_KIND);
        let kind = Kind::from_u8(raw).ok_or(Error::UnknownKind(raw))?;

        // Three shapes, one prefix: a decided block stops at its timestamp, a
        // standard block adds the transactions it applies, and a proposal
        // block adds one of its own after those. Reading the prefix through
        // Decided whatever the kind is what makes the shorter shape a genuine
        // prefix rather than a special case.
        let head = w::Decided::new(o);
        let decision_txs = match kind {
            Kind::Standard | Kind::Proposal => read_tx_list(w::Standard::new(o))?,
            _ => Vec::new(),
        };
        let proposal_tx = if kind == Kind::Proposal {
            let raw = w::Proposal::new(o).proposal_tx();
            if raw.is_empty() {
                return Err(Error::NoProposalTx);
            }
            Some(Tx::parse(raw).map_err(Error::Tx)?)
        } else {
            None
        };

        Ok(Block {
            kind,
            parent: *head.parent(),
            height: head.height(),
            timestamp: head.time(),
            decision_txs,
            proposal_tx,
            id: hash256(bytes),
            bytes: bytes.to_vec(),
        })
    }

    pub fn kind(&self) -> Kind {
        self.kind
    }

    pub fn id(&self) -> Id {
        self.id
    }

    pub fn parent(&self) -> Id {
        self.parent
    }

    pub fn height(&self) -> u64 {
        self.height
    }

    pub fn timestamp(&self) -> u64 {
        self.timestamp
    }

    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    /// The transactions this block charges for — the ones somebody else
    /// submitted.
    ///
    /// A proposal block's own transaction is deliberately not among them: it
    /// is reachable only through [`Block::proposal_tx`], typed and singular,
    /// so it cannot be handed to anything that prices or re-issues a list of
    /// other people's transactions.
    pub fn decision_txs(&self) -> &[Tx] {
        &self.decision_txs
    }

    pub fn proposal_tx(&self) -> Option<&Tx> {
        self.proposal_tx.as_ref()
    }
}

fn build(
    kind: Kind,
    parent: Id,
    height: u64,
    timestamp: u64,
    decision_txs: &[Tx],
    proposal_tx: Option<&Tx>,
) -> Vec<u8> {
    // Writing the shorter shape is what makes the shorter bytes: a decided
    // block has no transaction list to say anything about, so it does not
    // reserve the two fields a standard block does.
    let lengths: Vec<u32> = decision_txs.iter().map(|t| t.bytes().len() as u32).collect();
    let blob: Vec<u8> = decision_txs.iter().flat_map(|t| t.bytes().to_vec()).collect();

    match (kind, proposal_tx) {
        (Kind::Proposal, Some(tx)) => w::new_proposal(&w::ProposalInput {
            kind: kind as u8,
            parent: &parent,
            height,
            time: timestamp,
            tx_lengths: &lengths,
            tx_blob: &blob,
            proposal_tx: tx.bytes(),
        }),
        (Kind::Standard, _) | (Kind::Proposal, None) => w::new_standard(&w::StandardInput {
            kind: kind as u8,
            parent: &parent,
            height,
            time: timestamp,
            tx_lengths: &lengths,
            tx_blob: &blob,
        }),
        _ => w::new_decided(&w::DecidedInput {
            kind: kind as u8,
            parent: &parent,
            height,
            time: timestamp,
        }),
    }
}

fn read_tx_list(v: w::Standard<'_>) -> Result<Vec<Tx>, Error> {
    let lengths = v.tx_lengths();
    if lengths.is_empty() {
        return Ok(Vec::new());
    }
    let blob = v.tx_blob();
    let mut out = Vec::with_capacity(lengths.len());
    let mut cursor = 0usize;
    for i in 0..lengths.len() {
        let size = lengths.u32(i) as usize;
        if cursor + size > blob.len() {
            return Err(Error::TxOverrunsBlob {
                index: i,
                length: size,
            });
        }
        out.push(Tx::parse(&blob[cursor..cursor + size]).map_err(Error::Tx)?);
        cursor += size;
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Input, Output, Owners, UtxoId};
    use crate::ids::ShortId;
    use crate::txs::{Envelope, Unsigned};

    fn a_tx(n: u8) -> Tx {
        Tx::new(
            Unsigned::Base(Envelope {
                network_id: 1,
                blockchain_id: [3; 32],
                outs: vec![Output {
                    asset: [9; 32],
                    stake_lock: 0,
                    amount: 100 + n as u64,
                    owners: Owners {
                        locktime: 0,
                        threshold: 1,
                        addrs: vec![ShortId([n; 20])],
                    },
                }],
                ins: vec![Input {
                    utxo: UtxoId {
                        tx_id: [n; 32],
                        output_index: 0,
                    },
                    asset: [9; 32],
                    stake_lock: 0,
                    amount: 200,
                    sig_indices: vec![0],
                }],
                memo: Vec::new(),
            }),
            vec![crate::components::Credential {
                sigs: vec![[n; 65]],
            }],
        )
    }

    fn a_proposal() -> Tx {
        Tx::new(
            Unsigned::RewardValidator {
                staker_tx_id: [7; 32],
            },
            Vec::new(),
        )
    }

    #[test]
    fn every_block_kind_round_trips() {
        let parent = [1u8; 32];
        let blocks = vec![
            Block::standard(parent, 5, 1000, vec![a_tx(1), a_tx(2)]),
            Block::standard(parent, 5, 1000, vec![]),
            Block::proposal(parent, 5, 1000, a_proposal()),
            Block::commit(parent, 5, 1000),
            Block::abort(parent, 5, 1000),
        ];
        for blk in blocks {
            let back = Block::parse(blk.bytes()).expect("parse");
            assert_eq!(back, blk, "round trip for {:?}", blk.kind());
            assert_eq!(back.id(), blk.id());
            assert_eq!(back.parent(), parent);
            assert_eq!(back.height(), 5);
            assert_eq!(back.timestamp(), 1000);
        }
    }

    /// Go's `TestBlockReachesItsOwnVisitorArm`.
    ///
    /// A block is executed by whichever arm it reaches, so the arm it reaches
    /// IS its meaning, and nothing downstream re-checks the kind. A standard
    /// block that arrived at the commit arm would be accepted as an empty
    /// decision and every transaction it carries dropped from state while the
    /// block itself stayed final. Go dispatches through a visitor; here the
    /// dispatch is a match on [`Kind`], and the question is the same one: as
    /// built, and again after a trip through the wire, does exactly the right
    /// arm fire?
    #[test]
    fn every_block_is_executed_as_the_kind_it_is() {
        // The dispatch every executor writes, with the arms named so that two
        // firing, or none, is as visible as the wrong one firing.
        fn arms(blk: &Block) -> Vec<&'static str> {
            let mut reached = Vec::new();
            match blk.kind() {
                Kind::Abort => reached.push("abort"),
                Kind::Commit => reached.push("commit"),
                Kind::Proposal => reached.push("proposal"),
                Kind::Standard => reached.push("standard"),
            }
            reached
        }

        let parent = [1u8; 32];
        let cases: [(&str, Block); 4] = [
            ("abort", Block::abort(parent, 1, 0)),
            ("commit", Block::commit(parent, 1, 0)),
            ("standard", Block::standard(parent, 1, 0, vec![a_tx(1)])),
            ("proposal", Block::proposal(parent, 1, 0, a_proposal())),
        ];

        for (kind, blk) in cases {
            assert_eq!(
                arms(&blk),
                vec![kind],
                "the block was executed as the wrong kind of block"
            );
            let parsed = Block::parse(blk.bytes()).expect("parse");
            assert_eq!(
                arms(&parsed),
                vec![kind],
                "the block changed meaning on its way through the wire"
            );
        }
    }

    #[test]
    fn a_block_is_named_by_the_hash_of_its_bytes() {
        let blk = Block::standard([1; 32], 5, 1000, vec![a_tx(1)]);
        assert_eq!(blk.id(), hash256(blk.bytes()));
    }

    #[test]
    fn two_blocks_that_differ_anywhere_have_different_names() {
        let a = Block::standard([1; 32], 5, 1000, vec![a_tx(1)]);
        for b in [
            Block::standard([2; 32], 5, 1000, vec![a_tx(1)]),
            Block::standard([1; 32], 6, 1000, vec![a_tx(1)]),
            Block::standard([1; 32], 5, 1001, vec![a_tx(1)]),
            Block::standard([1; 32], 5, 1000, vec![a_tx(2)]),
            Block::commit([1; 32], 5, 1000),
        ] {
            assert_ne!(a.id(), b.id());
        }
    }

    #[test]
    fn trailing_bytes_are_refused() {
        // A tail would wrap the same message and hash to a different name.
        let blk = Block::commit([1; 32], 5, 1000);
        let mut with_tail = blk.bytes().to_vec();
        with_tail.push(0);
        assert_eq!(Block::parse(&with_tail), Err(Error::ExtraSpace));
    }

    #[test]
    fn an_unknown_kind_is_refused() {
        let blk = Block::commit([1; 32], 5, 1000);
        let mut bytes = blk.bytes().to_vec();
        let root = u32::from_le_bytes(bytes[8..12].try_into().unwrap()) as usize;
        bytes[root] = 9;
        assert_eq!(Block::parse(&bytes), Err(Error::UnknownKind(9)));
        bytes[root] = 0;
        assert_eq!(Block::parse(&bytes), Err(Error::UnknownKind(0)));
    }

    #[test]
    fn a_proposal_block_must_carry_its_proposal() {
        // Go refuses this at construction and again at parse; an empty slot
        // reads back as nothing, so the question is asked where the block is
        // made rather than where it is dereferenced.
        let blk = Block::proposal([1; 32], 5, 1000, a_proposal());
        let mut bytes = blk.bytes().to_vec();
        let root = u32::from_le_bytes(bytes[8..12].try_into().unwrap()) as usize;
        let at = root + w::PROPOSAL_PROPOSAL_TX;
        bytes[at..at + 8].copy_from_slice(&[0u8; 8]);
        assert_eq!(Block::parse(&bytes), Err(Error::NoProposalTx));
    }

    #[test]
    fn a_declared_length_that_runs_past_the_blob_is_refused() {
        let blk = Block::standard([1; 32], 5, 1000, vec![a_tx(1)]);
        let mut bytes = blk.bytes().to_vec();
        let root = u32::from_le_bytes(bytes[8..12].try_into().unwrap()) as usize;
        // The length list is one u32; enlarge it past the blob.
        let at = root + w::STANDARD_TX_LENGTHS;
        let rel = i32::from_le_bytes(bytes[at..at + 4].try_into().unwrap());
        let list = (at as i64 + rel as i64) as usize;
        bytes[list..list + 4].copy_from_slice(&99_999u32.to_le_bytes());
        assert!(matches!(
            Block::parse(&bytes),
            Err(Error::TxOverrunsBlob { .. })
        ));
    }

    #[test]
    fn a_proposals_own_transaction_is_not_among_the_ones_it_charges_for() {
        // The typed-and-singular rule: a proposal transaction can never be
        // handed to something that treats it as an assertion someone made.
        let blk = Block::proposal([1; 32], 5, 1000, a_proposal());
        assert!(blk.decision_txs().is_empty());
        assert!(blk.proposal_tx().is_some());
    }

    #[test]
    fn a_decided_block_carries_nothing_but_its_answer() {
        for blk in [
            Block::commit([1; 32], 5, 1000),
            Block::abort([1; 32], 5, 1000),
        ] {
            assert!(blk.decision_txs().is_empty());
            assert!(blk.proposal_tx().is_none());
            // The whole fixed section is the 49 bytes a decided block needs.
            let msg = zap::Message::parse(blk.bytes()).unwrap();
            assert_eq!(msg.size(), zap::HEADER_SIZE + w::DECIDED_SIZE);
        }
    }

    #[test]
    fn a_standard_blocks_transactions_come_back_in_order() {
        let txs = vec![a_tx(1), a_tx(2), a_tx(3)];
        let ids: Vec<Id> = txs.iter().map(|t| t.id()).collect();
        let blk = Block::standard([1; 32], 5, 1000, txs);
        let back = Block::parse(blk.bytes()).unwrap();
        let got: Vec<Id> = back.decision_txs().iter().map(|t| t.id()).collect();
        assert_eq!(got, ids);
    }
}
