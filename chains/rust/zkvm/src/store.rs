// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Where the chain is: the tip it has reached, the decisions still in flight
//! above it, and the records under both.
//!
//! A chain package declares what it IS — its transactions, its records, its
//! authorization rules. It does not restate how a decision becomes fact. That
//! is here, once, because the one way to get it wrong is to write it again.
//!
//! THE Z-CHAIN DECIDES IN TWO SHAPES. A linear block has one parent and a
//! timestamp; a DAG vertex has several parents and neither. Both change state
//! the same way, and this is that way — which is why what is tracked, accepted
//! and read back is a [`Decided`] and not a block.
//!
//! WHAT IS NOT HERE is the commit itself. A block's spends, its outputs, its
//! record, its height entry and the tip pointer land in ONE commit or none of
//! them, and the abort that makes that true has to reach the caches those
//! writes also touched. Both live in [`crate::vm::Zvm::accept`], where the
//! caches are — one function that can be read start to finish, rather than a
//! callback into the owner from here.

use std::collections::HashMap;

use crate::block::Block;
use crate::db::Db;
use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::vertex::Vertex;

/// The tip pointer.
pub const TIP: &[u8] = b"chain/tip";
/// A decision, by id.
pub const BLOCK: &[u8] = b"chain/block/";
/// The height index.
pub const HEIGHT: &[u8] = b"chain/height/";

pub fn block_key(id: &Id) -> Vec<u8> {
    let mut k = BLOCK.to_vec();
    k.extend_from_slice(id);
    k
}

pub fn height_key(h: u64) -> Vec<u8> {
    let mut k = HEIGHT.to_vec();
    k.extend_from_slice(&h.to_be_bytes());
    k
}

/// One unit of state change the chain can accept.
#[derive(Clone, Debug)]
pub enum Decided {
    Block(Block),
    Vertex(Vertex),
}

impl Decided {
    pub fn id(&self) -> Id {
        match self {
            Decided::Block(b) => b.id(),
            Decided::Vertex(v) => v.id(),
        }
    }

    pub fn height(&self) -> u64 {
        match self {
            Decided::Block(b) => b.height(),
            Decided::Vertex(v) => v.height(),
        }
    }

    pub fn bytes(&self) -> &[u8] {
        match self {
            Decided::Block(b) => b.bytes(),
            Decided::Vertex(v) => v.bytes(),
        }
    }

    /// The block, or nothing. A vertex is a decision this chain makes but not
    /// a block the engine can take, so asking for one by block id is answered
    /// as a miss rather than with something the caller cannot use.
    pub fn block(&self) -> Option<&Block> {
        match self {
            Decided::Block(b) => Some(b),
            Decided::Vertex(_) => None,
        }
    }
}

/// The chain's place, and what is above it.
#[derive(Debug)]
pub struct Store {
    flight: HashMap<Id, Decided>,
    last: Decided,
    tip: Id,
    height: u64,
    preferred: Id,
}

impl Store {
    /// Open over `db`: the tip recorded in committed state, or `genesis` when
    /// nothing is recorded. Reports which, so a chain that seeds state on its
    /// first run can tell its first run from every later one.
    ///
    /// Only a tip that is ABSENT means a fresh chain. Reading any other failure
    /// that way — a closed database, an unreadable volume, a short read —
    /// starts a live chain over at genesis and lets it build height 1 on top of
    /// state it cannot see, durably. A chain that cannot read its own tip does
    /// not know where it is, and the honest answer is to refuse to open.
    pub fn open(db: &dyn Db, bind: &Id, genesis: Block) -> Result<(Store, bool)> {
        let genesis_id = genesis.id();
        let genesis_height = genesis.height();
        let fresh = |s: Id, h: u64, last: Decided, fresh: bool| {
            Ok((
                Store {
                    flight: HashMap::new(),
                    last,
                    tip: s,
                    height: h,
                    preferred: ids::EMPTY,
                },
                fresh,
            ))
        };

        let raw = match db.get(TIP) {
            Err(Error::NotFound) => {
                return fresh(genesis_id, genesis_height, Decided::Block(genesis), true)
            }
            Err(e) => return Err(Error::Store(e.to_string())),
            Ok(raw) if raw.len() != ids::ID_LEN => {
                return Err(Error::Store(format!(
                    "tip is {} bytes, want {}",
                    raw.len(),
                    ids::ID_LEN
                )))
            }
            Ok(raw) => raw,
        };
        let id = ids::from_slice(&raw);
        if id == genesis_id {
            return fresh(id, genesis_height, Decided::Block(genesis), false);
        }

        let recorded = db.get(&block_key(&id)).map_err(|_| Error::NoBlock(id))?;
        let b = Block::parse(bind, &recorded)?;
        let h = b.height();
        fresh(id, h, Decided::Block(b), false)
    }

    /// The accepted decision's id and height.
    pub fn tip(&self) -> (Id, u64) {
        (self.tip, self.height)
    }

    /// The accepted decision itself, kept so a proposal never re-reads it.
    pub fn last(&self) -> &Decided {
        &self.last
    }

    /// Record what the engine wants the next decision built on.
    ///
    /// A preference the store no longer holds — pruned, or never tracked —
    /// falls back to the tip rather than naming a parent nothing can resolve.
    pub fn prefer(&mut self, id: Id) {
        self.preferred = id;
    }

    /// What to build on.
    pub fn parent(&self) -> &Decided {
        self.flight.get(&self.preferred).unwrap_or(&self.last)
    }

    /// Make a decision findable by id while it is in flight, so a child can
    /// resolve it as a parent — whether this node built it or parsed it from a
    /// peer. Tracking only what a node builds leaves a follower able to verify
    /// the first block of a run and unable to verify the second.
    pub fn track(&mut self, d: Decided) {
        if d.height() <= self.height {
            return;
        }
        self.prune();
        self.flight.insert(d.id(), d);
    }

    /// Forget a decision in flight. A rejected one is one the chain will not
    /// build on, and it never wrote anything, so there is nothing else to undo.
    pub fn forget(&mut self, id: &Id) -> Option<Decided> {
        self.flight.remove(id)
    }

    pub fn in_flight(&self, id: &Id) -> Option<&Decided> {
        self.flight.get(id)
    }

    pub fn in_flight_count(&self) -> usize {
        self.flight.len()
    }

    /// A decision by id: one in flight, the tip itself, or one read back from
    /// committed state.
    pub fn get(&self, db: &dyn Db, bind: &Id, id: &Id) -> Result<Decided> {
        if let Some(d) = self.flight.get(id) {
            return Ok(d.clone());
        }
        if *id == self.tip {
            return Ok(self.last.clone());
        }
        let raw = db.get(&block_key(id)).map_err(|_| Error::NoBlock(*id))?;
        Ok(Decided::Block(Block::parse(bind, &raw)?))
    }

    /// Whether `id` is the accepted tip or a decision committed beneath it. One
    /// in flight is neither.
    pub fn accepted(&self, db: &dyn Db, id: &Id) -> bool {
        *id == self.tip || db.has(&block_key(id)).unwrap_or(false)
    }

    /// Stage the records a decision itself makes: the decision, its height
    /// entry and the tip pointer. The caller stages the decision's EFFECTS
    /// through the same view and commits both together.
    pub fn stage(db: &mut dyn Db, d: &Decided) -> Result<()> {
        let id = d.id();
        let raw = d.bytes();
        if raw.is_empty() {
            return Err(Error::Store(format!(
                "block {} has no encoding",
                ids::hex(&id)
            )));
        }
        db.put(&block_key(&id), raw)?;
        db.put(&height_key(d.height()), &id)?;
        db.put(TIP, &id)?;
        Ok(())
    }

    /// Move the tip. Called only after the commit, so there is no window in
    /// which the chain has advanced past state that is not on disk.
    pub fn advance(&mut self, d: Decided) {
        self.tip = d.id();
        self.height = d.height();
        self.flight.remove(&self.tip);
        self.last = d;
        self.prune();
    }

    /// The decision accepted at `height`, from the index written in the same
    /// commit as the decision itself — so the index can never name something
    /// the chain did not accept.
    pub fn id_at_height(db: &dyn Db, height: u64) -> Result<Id> {
        let raw = db
            .get(&height_key(height))
            .map_err(|_| Error::Store(format!("no such block: height {height}")))?;
        Ok(ids::from_slice(&raw))
    }

    /// Drop everything in flight at or below the accepted height.
    ///
    /// Nothing else will: the engine may abandon a decision without ever
    /// accepting or rejecting it, so a set that only grew would leak. Anything
    /// at or below the tip is already decided or orphaned, which bounds this to
    /// what is actually in flight above it.
    fn prune(&mut self) {
        let height = self.height;
        self.flight.retain(|_, d| d.height() > height);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::block::binding;
    use crate::db::Memory;
    use crate::wire::BlockBody;

    fn bind() -> Id {
        binding(&ids::repeated(40), 1)
    }

    fn block(height: u64, parent: Id) -> Block {
        Block::new(
            &bind(),
            BlockBody {
                parent,
                height,
                timestamp: 1000,
                txs: Vec::new(),
                state_root: vec![height as u8; 32],
                proof: None,
            },
        )
    }

    fn genesis() -> Block {
        Block::new(
            &bind(),
            BlockBody {
                parent: ids::EMPTY,
                height: 0,
                timestamp: 1000,
                txs: Vec::new(),
                state_root: Vec::new(),
                proof: None,
            },
        )
    }

    #[test]
    fn a_store_with_no_tip_recorded_opens_at_genesis_and_says_it_is_fresh() {
        let (s, fresh) = Store::open(&Memory::new(), &bind(), genesis()).unwrap();
        assert!(fresh);
        assert_eq!(s.tip(), (genesis().id(), 0));
    }

    #[test]
    fn a_store_reopened_at_genesis_is_not_fresh_the_second_time() {
        let mut db = Memory::new();
        db.put(TIP, &genesis().id()).unwrap();
        let (s, fresh) = Store::open(&db, &bind(), genesis()).unwrap();
        assert!(!fresh);
        assert_eq!(s.tip(), (genesis().id(), 0));
    }

    #[test]
    fn a_store_opens_at_the_decision_its_tip_pointer_names() {
        let mut db = Memory::new();
        let one = block(1, genesis().id());
        db.put(&block_key(&one.id()), one.bytes()).unwrap();
        db.put(TIP, &one.id()).unwrap();

        let (s, fresh) = Store::open(&db, &bind(), genesis()).unwrap();
        assert!(!fresh);
        assert_eq!(s.tip(), (one.id(), 1));
    }

    /// A tip that cannot be READ is not a chain that has none. Opening at
    /// genesis on any failure is how a live chain starts over and builds height
    /// 1 on state it cannot see.
    #[test]
    fn a_tip_that_is_not_an_id_refuses_to_open() {
        let mut db = Memory::new();
        db.put(TIP, &[1, 2, 3]).unwrap();
        assert!(matches!(
            Store::open(&db, &bind(), genesis()),
            Err(Error::Store(_))
        ));
    }

    #[test]
    fn a_tip_naming_a_decision_the_records_do_not_hold_refuses_to_open() {
        let mut db = Memory::new();
        db.put(TIP, &ids::repeated(7)).unwrap();
        assert!(matches!(
            Store::open(&db, &bind(), genesis()),
            Err(Error::NoBlock(id)) if id == ids::repeated(7)
        ));
    }

    #[test]
    fn a_tracked_decision_is_findable_as_a_parent() {
        let db = Memory::new();
        let (mut s, _) = Store::open(&db, &bind(), genesis()).unwrap();
        let one = block(1, genesis().id());
        s.track(Decided::Block(one.clone()));
        assert_eq!(s.get(&db, &bind(), &one.id()).unwrap().id(), one.id());
    }

    #[test]
    fn a_decision_at_or_below_the_tip_is_never_tracked() {
        let db = Memory::new();
        let (mut s, _) = Store::open(&db, &bind(), genesis()).unwrap();
        s.track(Decided::Block(genesis()));
        assert_eq!(s.in_flight_count(), 0);
    }

    #[test]
    fn advancing_the_tip_prunes_what_it_passed() {
        let db = Memory::new();
        let (mut s, _) = Store::open(&db, &bind(), genesis()).unwrap();
        let one = block(1, genesis().id());
        let sibling = block(1, ids::repeated(3));
        let two = block(2, one.id());
        s.track(Decided::Block(one.clone()));
        s.track(Decided::Block(sibling.clone()));
        s.track(Decided::Block(two.clone()));
        assert_eq!(s.in_flight_count(), 3);

        s.advance(Decided::Block(one.clone()));
        // The accepted one leaves flight, its sibling at the same height is
        // orphaned and dropped, and the child above the tip stays.
        assert_eq!(s.tip(), (one.id(), 1));
        assert!(s.in_flight(&one.id()).is_none());
        assert!(s.in_flight(&sibling.id()).is_none());
        assert!(s.in_flight(&two.id()).is_some());
    }

    #[test]
    fn a_forgotten_decision_is_no_longer_a_parent() {
        let db = Memory::new();
        let (mut s, _) = Store::open(&db, &bind(), genesis()).unwrap();
        let one = block(1, genesis().id());
        s.track(Decided::Block(one.clone()));
        assert!(s.forget(&one.id()).is_some());
        assert!(matches!(
            s.get(&db, &bind(), &one.id()),
            Err(Error::NoBlock(id)) if id == one.id()
        ));
    }

    #[test]
    fn a_preference_the_store_no_longer_holds_falls_back_to_the_tip() {
        let db = Memory::new();
        let (mut s, _) = Store::open(&db, &bind(), genesis()).unwrap();
        s.prefer(ids::repeated(9));
        assert_eq!(s.parent().id(), genesis().id());

        let one = block(1, genesis().id());
        s.track(Decided::Block(one.clone()));
        s.prefer(one.id());
        assert_eq!(s.parent().id(), one.id());
    }

    #[test]
    fn staging_writes_the_decision_its_height_and_the_tip() {
        let mut db = Memory::new();
        let one = block(1, genesis().id());
        Store::stage(&mut db, &Decided::Block(one.clone())).unwrap();
        assert_eq!(db.get(&block_key(&one.id())).unwrap(), one.bytes());
        assert_eq!(Store::id_at_height(&db, 1).unwrap(), one.id());
        assert_eq!(db.get(TIP).unwrap(), one.id().to_vec());
    }

    #[test]
    fn a_height_nothing_was_accepted_at_names_no_decision() {
        assert!(matches!(
            Store::id_at_height(&Memory::new(), 4),
            Err(Error::Store(_))
        ));
    }

    #[test]
    fn only_the_tip_and_what_is_committed_beneath_it_count_as_accepted() {
        let mut db = Memory::new();
        let (mut s, _) = Store::open(&db, &bind(), genesis()).unwrap();
        let one = block(1, genesis().id());
        s.track(Decided::Block(one.clone()));
        assert!(!s.accepted(&db, &one.id()));
        assert!(s.accepted(&db, &genesis().id()));

        Store::stage(&mut db, &Decided::Block(one.clone())).unwrap();
        assert!(s.accepted(&db, &one.id()));
    }
}
