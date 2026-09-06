// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A block: what it says, what it is called, and what can be decided about it
//! without asking the chain anything.
//!
//! ## The id is a fold over CONTENT, and it opens with the chain
//!
//! [`compute_id`] begins with `sha256(ChainID ‖ NetworkID)`, which is NOT on
//! the wire. Two chains with different ids and an identical genesis would
//! otherwise derive the same genesis id, and one chain's blocks would then
//! chain onto the other's verbatim.
//!
//! Each transaction contributes its own content fold rather than an id read off
//! the wire — there is no such field to read — because a block whose identity
//! depended on an id a peer supplied would have as many identities as the peer
//! cared to send.
//!
//! ## The shape half of verification
//!
//! [`Block::shape`] is everything the reference decides before it reads the
//! chain, in the order it decides it: a height-0 block that names a parent, the
//! transaction cap, the clock, a nullifier repeated inside one block, and then
//! each transaction's own well-formedness and expiry. Three of those are
//! BLOCK-level and could not live in a per-transaction pass at all. What
//! remains — the proofs, the spent set, the parent, the tip, the state root —
//! needs the chain and belongs to [`crate::vm`].

use std::collections::BTreeSet;

use crate::error::{Error, Result};
use crate::hash::Fold;
use crate::host;
use crate::ids::{self, Id};
use crate::txs::Tx;
use crate::wire::{self, BlockBody};

/// How far ahead of this node's clock a block may be stamped.
pub const MAX_CLOCK_SKEW: i64 = 60;

/// The chain binding: `sha256(ChainID ‖ NetworkID)`.
///
/// It is hashed into every block and vertex id and into the public inputs every
/// shielded proof is checked against, and it is NOT on the wire — so a block or
/// a proof made for another chain does not name a block of this one and does
/// not verify here, rather than passing a check someone could forget to write.
pub fn binding(chain: &Id, network: u32) -> Id {
    let mut h = Fold::new();
    h.raw(chain).num32(network);
    h.id()
}

/// A block's identity.
pub fn compute_id(bind: &Id, b: &BlockBody) -> Id {
    let mut h = Fold::new();
    h.raw(bind).raw(&b.parent).num(b.height).signed(b.timestamp);
    for tx in &b.txs {
        h.raw(&tx.id());
    }
    h.raw(&b.state_root);
    if let Some(p) = &b.proof {
        h.raw(&p.system).raw(&p.data);
    }
    h.id()
}

/// A block of the Z-chain.
#[derive(Clone, Debug)]
pub struct Block {
    body: BlockBody,
    id: Id,
    bytes: Vec<u8>,
    status: host::Status,
}

impl Block {
    /// A block this node built, encoded and named.
    pub fn new(bind: &Id, body: BlockBody) -> Block {
        let bytes = wire::write_block(&body);
        let id = compute_id(bind, &body);
        Block {
            body,
            id,
            bytes,
            status: host::Status::Processing,
        }
    }

    /// A block off the wire.
    ///
    /// The bytes are kept AS GIVEN rather than re-encoded, which costs nothing
    /// here: the frame rule already refuses a buffer that does not account for
    /// every byte, so what came in is the one encoding of what came out.
    pub fn parse(bind: &Id, raw: &[u8]) -> Result<Block> {
        let body = wire::read_block(raw)?;
        let id = compute_id(bind, &body);
        Ok(Block {
            body,
            id,
            bytes: raw.to_vec(),
            status: host::Status::Unknown,
        })
    }

    pub fn id(&self) -> Id {
        self.id
    }

    pub fn parent(&self) -> Id {
        self.body.parent
    }

    pub fn height(&self) -> u64 {
        self.body.height
    }

    pub fn timestamp(&self) -> i64 {
        self.body.timestamp
    }

    pub fn txs(&self) -> &[Tx] {
        &self.body.txs
    }

    pub fn state_root(&self) -> &[u8] {
        &self.body.state_root
    }

    pub fn body(&self) -> &BlockBody {
        &self.body
    }

    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    pub fn status(&self) -> host::Status {
        self.status
    }

    pub fn set_status(&mut self, s: host::Status) {
        self.status = s;
    }

    /// What a Z block IS, is what it CARRIES.
    ///
    /// The chain has one block type, so naming the block names nothing — the
    /// word would be the same on every row. A block of one kind repeated is
    /// named once with its count; a mixed block spells every one out, because
    /// which kinds a block mixes is the thing worth comparing.
    pub fn carries(&self) -> String {
        let Some(first) = self.body.txs.first() else {
            return "Empty".into();
        };
        let first = first.kind.to_string();
        let mut uniform = true;
        let mut joined = first.clone();
        for tx in &self.body.txs[1..] {
            let n = tx.kind.to_string();
            if n != first {
                uniform = false;
            }
            joined.push('+');
            joined.push_str(&n);
        }
        if !uniform {
            return joined;
        }
        if self.body.txs.len() == 1 {
            return first;
        }
        format!("{first}x{}", self.body.txs.len())
    }

    /// Everything this block can be refused for without reading the chain, in
    /// the order the reference refuses it.
    ///
    /// `cap` is what a block may carry and `now` is this node's clock. A block
    /// off the wire is held to the bound a block this node builds is held to,
    /// so a proposer cannot produce one its own peers refuse.
    pub fn shape(&self, cap: u32, now: i64) -> Result<()> {
        if self.body.height == 0 && !ids::is_empty(&self.body.parent) {
            // Height 0 is genesis, and genesis has no parent. A height-0 block
            // that names one is two claims about which block it is.
            return Err(Error::InvalidBlock);
        }

        if self.body.txs.len() as u64 > u64::from(cap) {
            return Err(Error::TxCap {
                txs: self.body.txs.len(),
                cap,
            });
        }

        if self.body.timestamp > now.saturating_add(MAX_CLOCK_SKEW) {
            return Err(Error::FutureBlock);
        }

        // Every nullifier in the block must be distinct. Per-transaction
        // verification only sees nullifiers already spent in ACCEPTED state, so
        // without this two transactions in one block — or one transaction
        // listing a nullifier twice — spend the same shielded note and inflate
        // supply. Checked before the proofs because it is the cheaper gate.
        let mut here: BTreeSet<&[u8]> = BTreeSet::new();
        for tx in &self.body.txs {
            for n in &tx.nullifiers {
                if !here.insert(n.as_slice()) {
                    return Err(Error::DuplicateNullifier);
                }
            }
        }
        Ok(())
    }
}

impl host::Block for Block {
    fn id(&self) -> host::Id {
        self.id
    }

    fn parent(&self) -> host::Id {
        self.body.parent
    }

    fn height(&self) -> u64 {
        self.body.height
    }

    fn timestamp(&self) -> u64 {
        self.body.timestamp.max(0) as u64
    }

    fn bytes(&self) -> Vec<u8> {
        self.bytes.clone()
    }

    /// The state this block leaves behind: the chain's own committed root,
    /// which is a field of the block rather than something derived here. A
    /// certificate over a root nobody computed certifies nothing.
    fn state_root(&self) -> host::Id {
        ids::from_slice(&self.body.state_root)
    }

    /// What this block CARRIES: its transactions, folded in order. Separate
    /// from the state root, because a certificate that named a state but not
    /// the payload that produced it would be one two different blocks could
    /// satisfy.
    fn payload_root(&self) -> host::Id {
        let mut folded = Vec::with_capacity(self.body.txs.len() * ids::ID_LEN);
        for tx in &self.body.txs {
            folded.extend_from_slice(&tx.id());
        }
        crate::hash::sha256(&folded)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::txs::{Kind, Proof, Shielded, Tx};

    fn bind() -> Id {
        binding(&ids::repeated(40), 1)
    }

    fn transfer(nullifier: u8) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            version: 1,
            nullifiers: vec![vec![nullifier; 32]],
            outputs: vec![Shielded {
                commitment: vec![0x50; 32],
                ..Shielded::default()
            }],
            proof: Some(Proof {
                system: b"stark".to_vec(),
                data: vec![0x60; 192],
                public: vec![vec![0x61; 32]],
            }),
            fee: 1,
            expiry: 1000,
            ..Tx::default()
        }
    }

    fn body(height: u64, parent: Id, txs: Vec<Tx>) -> BlockBody {
        BlockBody {
            parent,
            height,
            timestamp: 1000,
            txs,
            state_root: vec![0xAB; 32],
            proof: None,
        }
    }

    /// The corpus's contract, computed here and asserted against the number the
    /// Go generator recorded. Both numbers are off the wire in neither case:
    /// they are the chain the evaluator was built for.
    #[test]
    fn the_binding_is_the_chain_and_the_network_and_nothing_else() {
        assert_eq!(
            ids::hex(&bind()),
            "c360d9f00bd7a0f2624fc5897800c2e24811f553b0df0e88d682d99e112acd78"
        );
    }

    /// The genesis of the chain the Z vectors are built for: height 0, no
    /// parent, the timestamp the genesis file names, no transactions and no
    /// state root. Every vector names this block as its parent, so this id is
    /// the corpus's contract and not an implementation detail.
    #[test]
    fn the_corpus_genesis_takes_the_id_the_reference_recorded() {
        let genesis = BlockBody {
            parent: ids::EMPTY,
            height: 0,
            timestamp: 1000,
            txs: Vec::new(),
            state_root: Vec::new(),
            proof: None,
        };
        assert_eq!(
            ids::hex(&compute_id(&bind(), &genesis)),
            "9c227870184f9269e1c720c2df8256dea664debf56f8eec76acab03c4bea755d"
        );
    }

    /// The same block on another chain is another block. This is the whole
    /// reason the binding is hashed in.
    #[test]
    fn the_same_bytes_name_a_different_block_on_a_different_chain() {
        let b = body(1, ids::repeated(9), vec![]);
        assert_ne!(
            compute_id(&bind(), &b),
            compute_id(&binding(&ids::repeated(41), 1), &b)
        );
        assert_ne!(
            compute_id(&bind(), &b),
            compute_id(&binding(&ids::repeated(40), 2), &b)
        );
    }

    #[test]
    fn every_field_of_a_block_is_in_its_identity() {
        let base = body(1, ids::repeated(9), vec![transfer(0x10)]);
        let id = compute_id(&bind(), &base);

        let mut moved = Vec::new();
        for f in [
            (|b: &mut BlockBody| b.parent = ids::repeated(8)) as fn(&mut BlockBody),
            |b: &mut BlockBody| b.height = 2,
            |b: &mut BlockBody| b.timestamp = 1001,
            |b: &mut BlockBody| b.state_root = vec![0xAC; 32],
            |b: &mut BlockBody| b.txs[0].nullifiers[0][0] ^= 1,
            |b: &mut BlockBody| {
                b.proof = Some(Proof {
                    system: b"stark".to_vec(),
                    data: vec![1],
                    public: vec![],
                })
            },
        ] {
            let mut b = base.clone();
            f(&mut b);
            moved.push(compute_id(&bind(), &b));
        }
        for m in moved {
            assert_ne!(id, m);
        }
    }

    #[test]
    fn a_parsed_block_keeps_the_bytes_it_was_given() {
        let raw = wire::write_block(&body(1, ids::repeated(9), vec![transfer(0x10)]));
        let b = Block::parse(&bind(), &raw).unwrap();
        assert_eq!(b.bytes(), raw.as_slice());
        assert_eq!(b.id(), compute_id(&bind(), b.body()));
    }

    #[test]
    fn a_built_block_and_the_same_block_parsed_back_are_one_block() {
        let built = Block::new(&bind(), body(1, ids::repeated(9), vec![transfer(0x10)]));
        let parsed = Block::parse(&bind(), built.bytes()).unwrap();
        assert_eq!(built.id(), parsed.id());
        assert_eq!(built.bytes(), parsed.bytes());
    }

    #[test]
    fn what_a_block_carries_is_what_it_is_called() {
        let b = |txs: Vec<Tx>| Block::new(&bind(), body(1, ids::repeated(9), txs)).carries();
        assert_eq!(b(vec![]), "Empty");
        assert_eq!(b(vec![transfer(1)]), "Transfer");
        assert_eq!(b(vec![transfer(1), transfer(2)]), "Transferx2");

        let mut shield = transfer(3);
        shield.kind = Kind::SHIELD;
        assert_eq!(b(vec![transfer(1), shield.clone()]), "Transfer+Shield");

        let mut unknown = transfer(4);
        unknown.kind = Kind(11);
        assert_eq!(b(vec![unknown]), "unknown");
    }

    #[test]
    fn a_height_zero_block_that_names_a_parent_is_two_claims() {
        let b = Block::new(&bind(), body(0, ids::repeated(9), vec![]));
        assert_eq!(b.shape(100, 1000), Err(Error::InvalidBlock));

        let genesis = Block::new(&bind(), body(0, ids::EMPTY, vec![]));
        assert_eq!(genesis.shape(100, 1000), Ok(()));
    }

    #[test]
    fn a_block_over_the_cap_is_refused_by_the_bound_a_proposer_builds_to() {
        let txs: Vec<Tx> = (0..4u8).map(transfer).collect();
        let b = Block::new(&bind(), body(1, ids::repeated(9), txs));
        assert_eq!(b.shape(4, 1000), Ok(()));
        assert_eq!(b.shape(3, 1000), Err(Error::TxCap { txs: 4, cap: 3 }));
    }

    #[test]
    fn a_block_stamped_past_the_skew_allowance_is_refused() {
        let mut body = body(1, ids::repeated(9), vec![]);
        body.timestamp = 1 << 40;
        let b = Block::new(&bind(), body);
        assert_eq!(b.shape(100, 1000), Err(Error::FutureBlock));
        // Inside the allowance it is not the clock that refuses it.
        assert_eq!(b.shape(100, (1 << 40) - MAX_CLOCK_SKEW), Ok(()));
    }

    /// One note spent twice, across two transactions that are each well formed
    /// on their own. Nothing else in the chain sees this: per-transaction
    /// verification reads only what is already spent in accepted state.
    #[test]
    fn one_note_spent_twice_inside_one_block_is_refused() {
        let mut twin = transfer(0x10);
        twin.fee = 2;
        let b = Block::new(&bind(), body(1, ids::repeated(9), vec![transfer(0x10), twin]));
        assert_eq!(b.shape(100, 1000), Err(Error::DuplicateNullifier));
    }

    #[test]
    fn one_transaction_listing_a_nullifier_twice_is_refused_the_same_way() {
        let mut tx = transfer(0x10);
        tx.nullifiers.push(vec![0x10; 32]);
        let b = Block::new(&bind(), body(1, ids::repeated(9), vec![tx]));
        assert_eq!(b.shape(100, 1000), Err(Error::DuplicateNullifier));
    }

    /// The order the reference refuses in. A block that breaks two rules is
    /// refused for the first one, and a port that reordered them would report a
    /// different reason for the same block.
    #[test]
    fn the_shape_rules_are_checked_in_the_reference_order() {
        let mut twin = transfer(0x10);
        twin.fee = 2;
        let mut b = body(0, ids::repeated(9), vec![transfer(0x10), twin]);
        b.timestamp = 1 << 40;
        let b = Block::new(&bind(), b);
        // Genesis-with-a-parent, over the cap, past the clock, and a repeated
        // nullifier — all four at once.
        assert_eq!(b.shape(1, 1000), Err(Error::InvalidBlock));

        let mut ok_genesis = b.body().clone();
        ok_genesis.height = 1;
        let b = Block::new(&bind(), ok_genesis);
        assert_eq!(b.shape(1, 1000), Err(Error::TxCap { txs: 2, cap: 1 }));
        assert_eq!(b.shape(100, 1000), Err(Error::FutureBlock));
        assert_eq!(b.shape(100, 1 << 40), Err(Error::DuplicateNullifier));
    }

    #[test]
    fn the_host_reads_the_block_the_chain_wrote() {
        let b = Block::new(&bind(), body(1, ids::repeated(9), vec![transfer(0x10)]));
        assert_eq!(host::Block::id(&b), b.id);
        assert_eq!(host::Block::height(&b), 1);
        assert_eq!(host::Block::timestamp(&b), 1000);
        assert_eq!(host::Block::state_root(&b), ids::repeated(0xAB));
        assert_eq!(
            host::Block::payload_root(&b),
            crate::hash::sha256(&transfer(0x10).id())
        );
        assert_eq!(b.bytes(), <Block as host::Block>::bytes(&b).as_slice());
    }
}
