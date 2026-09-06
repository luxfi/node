// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain itself: what it holds, what it admits, and what it commits.
//!
//! ## One predicate
//!
//! [`Zvm::admit`] is the whole of what a transaction must satisfy, and both
//! sides ask it — assembly in [`Zvm::build`] and consensus in [`Zvm::verify`].
//! A proposer that assembled a transaction its own peers refuse produces a
//! block nobody accepts and, since nothing evicts it, never produces another.
//!
//! ## Two halves of one verification
//!
//! [`Zvm::verify`] is a single pass, in the reference's order: the block's own
//! shape, then each transaction's shape and its proof, then the parent and the
//! tip it must sit on, then the state root it commits to. There is no separate
//! syntactic method to call, so where the two halves divide is stated over the
//! RULES, in [`crate::error::Error::before_the_chain`], rather than over the
//! code that happens to hold them — three of those rules are block-level and
//! could not live in a per-transaction pass at all.
//!
//! ## Acceptance is one commit or none
//!
//! [`Zvm::accept`] stages the block record, its height entry, the tip pointer,
//! every nullifier it spends, every output it creates and the advanced state
//! root through ONE view, and commits them together. A chain that wrote as it
//! went would have a first failed write leave some notes spent and some outputs
//! created under a tip it had already moved — a shielded pool half applied,
//! with no way back and no way to apply the block again.

use crate::block::{self, Block};
use crate::config::Config;
use crate::db::{Memory, View};
use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::nullifier::Nullifiers;
use crate::root::Root;
use crate::store::{Decided, Store};
use crate::txs::Tx;
use crate::utxo::{Utxo, Utxos};
use crate::verifier::Verifier;

/// What this chain calls itself.
pub const NAME: &str = "zkvm";
/// The version it reports.
pub const VERSION: &str = "1.0.0";

/// What a Z-chain is born with.
///
/// The timestamp is hashed into the genesis block id, so a genesis that names
/// none is stamped 0 rather than read off the wall clock — otherwise every node
/// derives a different genesis id, which is a different chain, and a different
/// one again after each restart.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Genesis {
    pub timestamp: i64,
}

impl Genesis {
    /// Read a genesis file.
    ///
    /// `initialTransactions` is REFUSED rather than ignored. The genesis
    /// allocation is folded into the committed state root before block 1, so a
    /// chain that skipped it would derive a different root for every later
    /// block — a fork, reported as nothing at all. This port carries no JSON
    /// transaction reader, and says so instead of pretending the file was
    /// empty.
    pub fn parse(raw: &[u8]) -> Result<Genesis> {
        if raw.is_empty() {
            return Ok(Genesis::default());
        }
        let v: serde_json::Value =
            serde_json::from_slice(raw).map_err(|e| Error::Config(e.to_string()))?;
        if let Some(txs) = v.get("initialTransactions").and_then(|x| x.as_array()) {
            if !txs.is_empty() {
                return Err(Error::Config(
                    "genesis names initial transactions, which this port does not read".into(),
                ));
            }
        }
        Ok(Genesis {
            timestamp: v.get("timestamp").and_then(|x| x.as_i64()).unwrap_or(0),
        })
    }
}

/// The Z-chain.
pub struct Zvm {
    cfg: Config,
    /// `sha256(ChainID ‖ NetworkID)`, hashed into every id this chain derives
    /// and into every proof's public inputs.
    bind: Id,
    db: View,
    store: Store,
    root: Root,
    nullifiers: Nullifiers,
    utxos: Utxos,
    verifier: Verifier,
    pending: Vec<Tx>,
    genesis: Id,
}

impl Zvm {
    /// Stand a chain up over an empty keyspace, seeded with `genesis`.
    pub fn new(cfg: Config, chain_id: Id, network_id: u32, genesis: &[u8]) -> Result<Zvm> {
        let g = Genesis::parse(genesis)?;
        let bind = block::binding(&chain_id, network_id);
        let verifier = Verifier::new(&cfg, bind)?;

        let first = Block::new(
            &bind,
            crate::wire::BlockBody {
                parent: ids::EMPTY,
                height: 0,
                timestamp: g.timestamp,
                txs: Vec::new(),
                state_root: Vec::new(),
                proof: None,
            },
        );
        let genesis_id = first.id();

        let mut db = View::new(Memory::new());
        let (store, fresh) = Store::open(&db, &bind, first)?;
        let mut root = Root::open(&db)?;
        let nullifiers = Nullifiers::open(&db)?;
        let utxos = Utxos::open(&db)?;

        if fresh {
            // The genesis fold is the one mutation outside a block, and it is
            // committed on its own: staged, it would ride on whichever block
            // landed first and vanish from a chain that never accepted one.
            let seeded = root.after(&[]);
            root.finalize(&mut db, &seeded)?;
            db.commit();
        }

        Ok(Zvm {
            cfg,
            bind,
            db,
            store,
            root,
            nullifiers,
            utxos,
            verifier,
            pending: Vec::new(),
            genesis: genesis_id,
        })
    }

    /// The chain binding every id and every proof is checked under.
    pub fn binding(&self) -> Id {
        self.bind
    }

    /// The genesis block's id.
    pub fn genesis(&self) -> Id {
        self.genesis
    }

    pub fn last_accepted(&self) -> Id {
        self.store.tip().0
    }

    pub fn height(&self) -> u64 {
        self.store.tip().1
    }

    pub fn state_root(&self) -> &[u8] {
        self.root.get()
    }

    pub fn nullifier_count(&self) -> u64 {
        self.nullifiers.count()
    }

    pub fn output_count(&self) -> u64 {
        self.utxos.count()
    }

    /// The height a nullifier was spent at, or `None`.
    pub fn spent_at(&self, nullifier: &[u8]) -> Result<Option<u64>> {
        self.nullifiers.spent(&self.db, nullifier)
    }

    pub fn block_id_at(&self, height: u64) -> Result<Id> {
        Store::id_at_height(&self.db, height)
    }

    /// A block by id: one in flight, the tip, or one read back from committed
    /// state.
    pub fn block(&self, id: &Id) -> Result<Block> {
        match self.store.get(&self.db, &self.bind, id)? {
            Decided::Block(b) => Ok(b),
            Decided::Vertex(_) => Err(Error::NotABlock(*id)),
        }
    }

    /// Read a block off the wire. It must not need the parent: a bootstrapping
    /// node parses blocks whose parents it does not hold yet.
    pub fn parse_block(&self, raw: &[u8]) -> Result<Block> {
        Block::parse(&self.bind, raw)
    }

    /// Record what the engine wants the next block built on.
    pub fn prefer(&mut self, id: Id) {
        self.store.prefer(id);
    }

    /// Offer a transaction to the pool. The same predicate a block is held to.
    pub fn submit(&mut self, tx: Tx) -> Result<()> {
        let next = self.store.tip().1 + 1;
        self.admit(&tx, next)?;
        self.pending.push(tx);
        Ok(())
    }

    /// Assemble a block from whatever is pending, on the preferred parent.
    ///
    /// Assembly runs the SAME predicate verification runs and drops what it
    /// cannot build, so a proposer never produces a block its own peers refuse.
    pub fn build(&mut self) -> Result<Block> {
        let parent = self.store.parent().clone();
        let height = parent.height() + 1;
        let taken = std::mem::take(&mut self.pending);
        let mut txs = Vec::with_capacity(taken.len());
        for tx in taken {
            if self.admit(&tx, height).is_ok() {
                txs.push(tx);
            }
        }
        if txs.is_empty() {
            return Err(Error::NoTransactions);
        }
        txs.truncate(self.cfg.max_tx_per_block as usize);

        // Chain time only moves forward, and verification refuses a block below
        // its parent. A parent may legally stand up to the skew allowance ahead
        // of this node's clock, so an unclamped clock here builds a block this
        // node's own verification then refuses.
        let mut timestamp = now();
        if let Decided::Block(pb) = &parent {
            timestamp = timestamp.max(pb.timestamp());
        }

        let state_root = self.root.after(&txs);
        Ok(Block::new(
            &self.bind,
            crate::wire::BlockBody {
                parent: parent.id(),
                height,
                timestamp,
                txs,
                state_root,
                proof: None,
            },
        ))
    }

    /// The whole verdict on a block, in the reference's order.
    pub fn verify(&mut self, b: &Block) -> Result<()> {
        b.shape(self.cfg.max_tx_per_block, now())?;

        for tx in b.txs() {
            self.admit(tx, b.height())?;
        }
        if b.body().proof.is_some() {
            self.verifier.verify_block_proof(b.txs())?;
        }

        if b.height() > 0 {
            let parent = self.block(&b.parent())?;
            // The parent must be one this chain can still build on: the
            // accepted tip, or a block verified above it and not yet decided.
            // Height alone is not that check — a block whose parent is an OLD
            // accepted block satisfies height == parent+1 perfectly well, and
            // accepting it rewinds the tip and leaves the height index naming
            // an orphan as the block at that height to every peer that
            // bootstraps from it.
            let (tip, tip_height) = self.store.tip();
            if parent.id() != tip && parent.height() <= tip_height {
                return Err(Error::NotOnTip(format!(
                    "parent {} at height {} is beneath the tip at {}",
                    ids::hex(&parent.id()),
                    parent.height(),
                    tip_height
                )));
            }
            if b.height() != parent.height() + 1 {
                return Err(Error::InvalidHeight);
            }
            if b.timestamp() < parent.timestamp() {
                return Err(Error::InvalidTimestamp);
            }
        }

        if b.state_root() != self.root.after(b.txs()).as_slice() {
            return Err(Error::InvalidStateRoot);
        }

        // A block that verifies is one the engine may build on, so it has to be
        // findable by id — including one parsed from a peer rather than built
        // here. Tracking only self-built blocks leaves a follower able to
        // verify the first block of a run and unable to verify the second.
        self.store.track(Decided::Block(b.clone()));
        Ok(())
    }

    /// Commit a block: its record, its spends, its outputs and the advanced
    /// root, in one commit or none of them.
    pub fn accept(&mut self, b: &Block) -> Result<()> {
        let decided = Decided::Block(b.clone());
        let next = self.root.after(b.txs());

        if let Err(e) = self.stage(b, &decided, &next) {
            // The abort has to reach the caches those writes also touched, or
            // the chain holds in memory what the database does not hold.
            self.db.abort();
            self.reload()?;
            return Err(e);
        }
        self.db.commit();

        let mut accepted = b.clone();
        accepted.set_status(crate::host::Status::Accepted);
        self.store.advance(Decided::Block(accepted));
        self.pending.clear();
        Ok(())
    }

    fn stage(&mut self, b: &Block, decided: &Decided, next: &[u8]) -> Result<()> {
        Store::stage(&mut self.db, decided)?;
        for tx in b.txs() {
            for n in &tx.nullifiers {
                self.nullifiers.mark(&mut self.db, n, b.height())?;
            }
            let id = tx.id();
            for (i, out) in tx.outputs.iter().enumerate() {
                self.utxos.add(
                    &mut self.db,
                    &Utxo {
                        tx: id,
                        output: i as u32,
                        commitment: out.commitment.clone(),
                        ciphertext: out.note.clone(),
                        ephemeral: out.ephemeral.clone(),
                        height: b.height(),
                    },
                )?;
            }
        }
        self.root.finalize(&mut self.db, next)
    }

    /// Put the caches back to what the database now says.
    fn reload(&mut self) -> Result<()> {
        self.root.reload(&self.db)?;
        self.nullifiers.reload(&self.db)?;
        self.utxos.reload(&self.db)
    }

    /// Drop a block that lost. It never wrote anything, so there is nothing
    /// else to undo.
    pub fn reject(&mut self, id: &Id) {
        self.store.forget(id);
    }

    /// The ONE predicate: what a transaction says about itself, whether it is
    /// still live at `height`, and whether its proof is good for it.
    pub fn admit(&mut self, tx: &Tx, height: u64) -> Result<()> {
        tx.validate_basic()?;
        if tx.expiry < height {
            return Err(Error::Expired);
        }
        self.verify_tx(tx)
    }

    fn verify_tx(&mut self, tx: &Tx) -> Result<()> {
        // A read that FAILED refuses the transaction: reporting "not spent" for
        // a set that could not be read is how an already-spent note gets spent
        // again.
        for n in &tx.nullifiers {
            match self.nullifiers.spent(&self.db, n) {
                Err(e) => return Err(Error::SpentSetRead(Box::new(e))),
                Ok(Some(_)) => return Err(Error::NullifierSpent),
                Ok(None) => {}
            }
        }
        self.verifier
            .verify_tx(tx)
            .map_err(|e| Error::ProofVerification(Box::new(e)))
    }
}

/// This node's clock, in seconds. The reference reads the wall clock, so a
/// chain held to a stated time would refuse blocks the reference admits.
fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::txs::{Kind, Proof, Shielded};

    fn chain() -> Zvm {
        Zvm::new(
            Config::chain_default(),
            ids::repeated(40),
            1,
            br#"{"timestamp":1000}"#,
        )
        .expect("a Z-chain")
    }

    fn transfer(nullifier: u8) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            version: 1,
            nullifiers: vec![vec![nullifier; 32]],
            outputs: vec![Shielded {
                commitment: vec![0x50; 32],
                note: vec![0x51; 64],
                ephemeral: vec![0x52; 32],
                range_proof: vec![0x53; 64],
            }],
            proof: Some(Proof {
                system: b"stark".to_vec(),
                data: vec![0x60; 192],
                public: vec![vec![0x61; 32], vec![0x62; 32]],
            }),
            fee: 1,
            expiry: 1000,
            ..Tx::default()
        }
    }

    fn empty_block(vm: &Zvm, parent: Id, height: u64, timestamp: i64) -> Block {
        Block::new(
            &vm.bind,
            crate::wire::BlockBody {
                parent,
                height,
                timestamp,
                txs: Vec::new(),
                state_root: vm.root.after(&[]),
                proof: None,
            },
        )
    }

    /// The genesis fold happens before block 1, so a chain born from a genesis
    /// with no transactions still starts ONE fold in rather than at the zero
    /// root. A block that named the zero root would be refused on a chain that
    /// did the fold and accepted on one that did not.
    #[test]
    fn the_chain_starts_one_fold_in() {
        let vm = chain();
        assert_ne!(vm.state_root(), vec![0u8; 32]);
        assert_eq!(vm.state_root(), Root::open(&Memory::new()).unwrap().after(&[]));
    }

    #[test]
    fn an_empty_block_on_the_seeded_chain_verifies() {
        let mut vm = chain();
        let tip = vm.last_accepted();
        let b = empty_block(&vm, tip, 1, 1000);
        vm.verify(&b).expect("an empty block on the tip");
    }

    /// The same block but for the parent it names. One must verify and the
    /// other must not, and the difference has to be the ledger.
    #[test]
    fn a_parent_no_chain_holds_is_a_ledger_refusal() {
        let mut vm = chain();
        let b = empty_block(&vm, ids::repeated(1), 1, 1000);
        assert!(matches!(vm.verify(&b), Err(Error::NoBlock(_))));
    }

    #[test]
    fn a_height_zero_block_that_names_a_parent_is_two_claims() {
        let mut vm = chain();
        let tip = vm.last_accepted();
        let b = empty_block(&vm, tip, 0, 1000);
        assert_eq!(vm.verify(&b), Err(Error::InvalidBlock));
    }

    #[test]
    fn a_state_root_the_chain_did_not_compute_is_refused() {
        let mut vm = chain();
        let tip = vm.last_accepted();
        let b = Block::new(
            &vm.bind,
            crate::wire::BlockBody {
                parent: tip,
                height: 1,
                timestamp: 1000,
                txs: Vec::new(),
                state_root: vec![0xAB; 32],
                proof: None,
            },
        );
        assert_eq!(vm.verify(&b), Err(Error::InvalidStateRoot));
    }

    /// The corpus's proofs are 192 bytes of one repeated value, which is not a
    /// proof of the one system a strict-PQ chain runs. It is refused for what
    /// the verifier said, under the wrapper the reference puts on it.
    #[test]
    fn a_shielded_transaction_is_refused_by_its_proof_and_not_by_its_shape() {
        let mut vm = chain();
        let tip = vm.last_accepted();
        let txs = vec![transfer(0x10)];
        let b = Block::new(
            &vm.bind,
            crate::wire::BlockBody {
                parent: tip,
                height: 1,
                timestamp: 1000,
                state_root: vm.root.after(&txs),
                txs,
                proof: None,
            },
        );
        let e = vm.verify(&b).unwrap_err();
        assert!(matches!(e, Error::ProofVerification(_)), "{e}");
        assert!(!e.before_the_chain(), "a proof needed the verifier");
    }

    /// A classical system is refused for what it IS, before anything judges it.
    #[test]
    fn a_classical_proof_is_forbidden_by_the_profile() {
        let mut vm = chain();
        let mut tx = transfer(0x1a);
        tx.proof.as_mut().unwrap().system = b"groth16".to_vec();
        let e = vm.admit(&tx, 1).unwrap_err();
        assert_eq!(e.root(), &Error::StrictPqClassicalForbidden);
    }

    #[test]
    fn a_transaction_past_its_expiry_is_refused_at_the_height_that_carries_it() {
        let mut vm = chain();
        let mut tx = transfer(0x19);
        tx.expiry = 1;
        assert_eq!(vm.admit(&tx, 2), Err(Error::Expired));
    }

    /// Acceptance advances the tip, the spent set, the output set and the root
    /// together, and a later block builds on what it left.
    #[test]
    fn acceptance_moves_everything_at_once() {
        let mut vm = chain();
        let tip = vm.last_accepted();
        let one = empty_block(&vm, tip, 1, 1000);
        vm.verify(&one).expect("verify");
        vm.accept(&one).expect("accept");

        assert_eq!(vm.last_accepted(), one.id());
        assert_eq!(vm.height(), 1);
        assert_eq!(vm.block_id_at(1).unwrap(), one.id());
        assert_eq!(vm.block(&one.id()).unwrap().id(), one.id());

        let two = empty_block(&vm, one.id(), 2, 1000);
        vm.verify(&two).expect("a block on the new tip");
    }

    #[test]
    fn a_genesis_naming_transactions_this_port_cannot_read_is_refused() {
        let e = Genesis::parse(br#"{"timestamp":1,"initialTransactions":[{}]}"#).unwrap_err();
        assert!(matches!(e, Error::Config(_)), "{e}");
        // An empty list is the same chain as no list, so it is not a refusal.
        assert_eq!(
            Genesis::parse(br#"{"timestamp":7,"initialTransactions":[]}"#).unwrap(),
            Genesis { timestamp: 7 }
        );
    }
}
