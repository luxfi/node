// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The spent set: the whole of what stops a shielded note being spent twice.
//!
//! A record is the height of the block that spent the nullifier. The set is
//! loaded at startup and never pruned, which is why the count is read off it
//! rather than kept beside it and reconciled — a total is a second write, and a
//! node that dies between the two comes back with a number that disagrees with
//! its own records.
//!
//! NULLIFIERS ARE PERMANENT. There is no removal path here — not for reorg, not
//! for compaction — because deleting a spent nullifier lets a previously spent
//! note be spent again. If storage becomes a concern, a Merkle accumulator is
//! the compaction. [`Nullifiers::reload`] is not a removal: it rebuilds the set
//! from what the database says, which is how a block whose writes were
//! discarded stops being believed.

use std::collections::BTreeMap;

use crate::db::Db;
use crate::error::{Error, Result};

/// The prefix a spend is recorded under.
pub const PREFIX: u8 = 0x20;

/// Every nullifier this chain has seen spent, and when.
#[derive(Clone, Debug, Default)]
pub struct Nullifiers {
    spent: BTreeMap<Vec<u8>, u64>,
}

/// The key a nullifier is recorded under.
pub fn key(nullifier: &[u8]) -> Vec<u8> {
    let mut k = Vec::with_capacity(1 + nullifier.len());
    k.push(PREFIX);
    k.extend_from_slice(nullifier);
    k
}

impl Nullifiers {
    /// Read the whole set out of `db`.
    pub fn open(db: &dyn Db) -> Result<Nullifiers> {
        let mut n = Nullifiers::default();
        n.reload(db)?;
        Ok(n)
    }

    /// Record a spend.
    pub fn mark(&mut self, db: &mut dyn Db, nullifier: &[u8], height: u64) -> Result<()> {
        if self.spent.contains_key(nullifier) {
            return Err(Error::NullifierSpent);
        }
        db.put(&key(nullifier), &height.to_be_bytes())?;
        self.spent.insert(nullifier.to_vec(), height);
        Ok(())
    }

    /// The height this nullifier was spent at, or `None` if it has not been.
    ///
    /// ONE question with one answer. Two calls — a bool and a height — cannot
    /// report a failed read at all, and `Get(key); return err == nil` answers
    /// "not spent" for a set that could not be read. That answer is what lets
    /// an already-spent note be spent again, so a read that failed is an error
    /// here and the caller refuses the transaction rather than admitting it.
    ///
    /// A miss falls through to the records and returns what it finds WITHOUT
    /// memoising it: the set is the whole set already, so a memo would only be
    /// a write on a read path.
    pub fn spent(&self, db: &dyn Db, nullifier: &[u8]) -> Result<Option<u64>> {
        if let Some(h) = self.spent.get(nullifier) {
            return Ok(Some(*h));
        }
        match db.get(&key(nullifier)) {
            Err(Error::NotFound) => Ok(None),
            Err(e) => Err(e),
            Ok(raw) if raw.len() != 8 => Err(Error::NullifierRecord),
            Ok(raw) => Ok(Some(u64::from_be_bytes(raw[..8].try_into().unwrap()))),
        }
    }

    /// How many notes have been spent, counted off the set itself.
    pub fn count(&self) -> u64 {
        self.spent.len() as u64
    }

    /// Rebuild the set from what the database now says.
    ///
    /// It runs after a block's writes have been discarded: a set that had
    /// already recorded that block's spends must stop claiming those notes are
    /// spent, or the block can never be applied again.
    pub fn reload(&mut self, db: &dyn Db) -> Result<()> {
        self.spent.clear();
        for (k, v) in db.prefixed(&[PREFIX]) {
            if k.len() < 2 || v.len() != 8 {
                continue;
            }
            self.spent
                .insert(k[1..].to_vec(), u64::from_be_bytes(v[..8].try_into().unwrap()));
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::{Memory, View};

    #[test]
    fn a_nullifier_nobody_spent_is_not_spent() {
        let db = Memory::new();
        let n = Nullifiers::open(&db).unwrap();
        assert_eq!(n.spent(&db, &[1u8; 32]), Ok(None));
        assert_eq!(n.count(), 0);
    }

    #[test]
    fn a_marked_nullifier_reports_the_height_that_spent_it() {
        let mut db = Memory::new();
        let mut n = Nullifiers::open(&db).unwrap();
        n.mark(&mut db, &[1u8; 32], 7).unwrap();
        assert_eq!(n.spent(&db, &[1u8; 32]), Ok(Some(7)));
        assert_eq!(n.count(), 1);
    }

    #[test]
    fn marking_a_note_twice_is_refused() {
        let mut db = Memory::new();
        let mut n = Nullifiers::open(&db).unwrap();
        n.mark(&mut db, &[1u8; 32], 7).unwrap();
        assert_eq!(n.mark(&mut db, &[1u8; 32], 8), Err(Error::NullifierSpent));
    }

    #[test]
    fn the_set_comes_back_from_the_records_on_a_restart() {
        let mut db = Memory::new();
        let mut n = Nullifiers::open(&db).unwrap();
        n.mark(&mut db, &[1u8; 32], 7).unwrap();
        n.mark(&mut db, &[2u8; 32], 9).unwrap();

        let reopened = Nullifiers::open(&db).unwrap();
        assert_eq!(reopened.count(), 2);
        assert_eq!(reopened.spent(&db, &[1u8; 32]), Ok(Some(7)));
        assert_eq!(reopened.spent(&db, &[2u8; 32]), Ok(Some(9)));
    }

    /// The reason `reload` exists. A block staged its spends and then failed to
    /// commit; the set has to stop claiming those notes are spent, or the block
    /// can never be applied again.
    #[test]
    fn a_reload_after_a_discarded_block_stops_claiming_its_spends() {
        let mut view = View::new(Memory::new());
        let mut n = Nullifiers::open(&view).unwrap();
        n.mark(&mut view, &[1u8; 32], 7).unwrap();
        assert_eq!(n.spent(&view, &[1u8; 32]), Ok(Some(7)));

        view.abort();
        n.reload(&view).unwrap();
        assert_eq!(n.spent(&view, &[1u8; 32]), Ok(None));
        assert_eq!(n.count(), 0);
    }

    #[test]
    fn a_record_that_is_not_a_height_is_refused_rather_than_read() {
        let mut db = Memory::new();
        db.put(&key(&[1u8; 32]), &[1, 2, 3]).unwrap();
        // A malformed record is skipped when the set is loaded, so the direct
        // read is what has to notice it.
        let n = Nullifiers::default();
        assert_eq!(n.spent(&db, &[1u8; 32]), Err(Error::NullifierRecord));
    }

    /// The records share a keyspace with other writers. A key under another
    /// prefix is not a spend.
    #[test]
    fn the_set_takes_only_what_is_under_its_own_prefix() {
        let mut db = Memory::new();
        db.put(&[0x10, 1], &7u64.to_be_bytes()).unwrap();
        db.put(&key(&[1u8; 32]), &7u64.to_be_bytes()).unwrap();
        assert_eq!(Nullifiers::open(&db).unwrap().count(), 1);
    }
}
