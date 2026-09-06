// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The unspent shielded outputs.
//!
//! A record is keyed by its COMMITMENT, and the set beside the records holds
//! commitment → height. Membership and the count are read off the set; the
//! bodies stay in the records, and a body is read fresh every time rather than
//! memoised — a memo on a read path is a write on a read path.
//!
//! The count is counted, not kept. A running total is a second write, and a
//! node that dies between the two comes back with a number that disagrees with
//! its own records — from which one removal drives an unsigned counter below
//! zero and reports 1.8e19 unspent notes forever.

use std::collections::BTreeMap;

use crate::db::Db;
use crate::error::{Error, Result};
use crate::ids::Id;
use crate::wire;

/// The prefix an unspent output is recorded under.
pub const PREFIX: u8 = 0x10;

/// An unspent transaction output.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Utxo {
    pub tx: Id,
    pub output: u32,
    /// The commitment to the note. Also the key it is recorded under.
    pub commitment: Vec<u8>,
    /// The note, encrypted to its recipient.
    pub ciphertext: Vec<u8>,
    /// The key the note was encrypted under.
    pub ephemeral: Vec<u8>,
    /// The height of the block that created it.
    pub height: u64,
}

/// The key an output is recorded under.
pub fn key(commitment: &[u8]) -> Vec<u8> {
    let mut k = Vec::with_capacity(1 + commitment.len());
    k.push(PREFIX);
    k.extend_from_slice(commitment);
    k
}

/// The unspent set.
#[derive(Clone, Debug, Default)]
pub struct Utxos {
    set: BTreeMap<Vec<u8>, u64>,
}

impl Utxos {
    /// Rebuild the set from `db`.
    pub fn open(db: &dyn Db) -> Result<Utxos> {
        let mut u = Utxos::default();
        u.reload(db)?;
        Ok(u)
    }

    /// Add an output. A commitment already in the set is refused: two notes
    /// under one commitment is one note the chain cannot tell apart.
    pub fn add(&mut self, db: &mut dyn Db, utxo: &Utxo) -> Result<()> {
        if self.set.contains_key(&utxo.commitment) {
            return Err(Error::UtxoExists);
        }
        db.put(&key(&utxo.commitment), &wire::write_utxo(utxo))?;
        self.set.insert(utxo.commitment.clone(), utxo.height);
        Ok(())
    }

    /// The output under a commitment.
    ///
    /// A read that FAILED is not an output that is absent: reported as absent
    /// it says a note was never created, rather than that the disk is gone.
    pub fn get(&self, db: &dyn Db, commitment: &[u8]) -> Result<Utxo> {
        match db.get(&key(commitment)) {
            Err(Error::NotFound) => Err(Error::NoUtxo),
            Err(e) => Err(Error::UtxoRead(Box::new(e))),
            Ok(raw) => wire::read_utxo(&raw),
        }
    }

    /// Whether the set holds a commitment, and the height it was created at.
    pub fn height_of(&self, commitment: &[u8]) -> Option<u64> {
        self.set.get(commitment).copied()
    }

    /// How many unspent outputs there are, counted off the set.
    pub fn count(&self) -> u64 {
        self.set.len() as u64
    }

    /// Rebuild the set from what the database now says, discarding whatever a
    /// block that did not commit had already added.
    ///
    /// The records share a keyspace with other writers, so a key carrying this
    /// prefix counts as an output only if the record under it names the
    /// commitment its own key is made of. Anything else belongs to someone else
    /// and is left alone.
    pub fn reload(&mut self, db: &dyn Db) -> Result<()> {
        self.set.clear();
        for (k, v) in db.prefixed(&[PREFIX]) {
            if k.len() < 2 {
                continue;
            }
            let commitment = &k[1..];
            let Ok(utxo) = wire::read_utxo(&v) else {
                continue;
            };
            if utxo.commitment != commitment {
                continue;
            }
            self.set.insert(commitment.to_vec(), utxo.height);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::{Memory, View};
    use crate::ids;

    fn utxo(commitment: u8, height: u64) -> Utxo {
        Utxo {
            tx: ids::repeated(3),
            output: 0,
            commitment: vec![commitment; 32],
            ciphertext: vec![0x51; 64],
            ephemeral: vec![0x52; 32],
            height,
        }
    }

    #[test]
    fn an_added_output_is_readable_and_counted() {
        let mut db = Memory::new();
        let mut u = Utxos::open(&db).unwrap();
        u.add(&mut db, &utxo(0xA1, 5)).unwrap();
        assert_eq!(u.count(), 1);
        assert_eq!(u.height_of(&[0xA1; 32]), Some(5));
        assert_eq!(u.get(&db, &[0xA1; 32]), Ok(utxo(0xA1, 5)));
    }

    #[test]
    fn a_commitment_the_set_does_not_hold_is_no_such_utxo() {
        let db = Memory::new();
        let u = Utxos::open(&db).unwrap();
        assert_eq!(u.get(&db, &[0xA1; 32]), Err(Error::NoUtxo));
        assert_eq!(u.height_of(&[0xA1; 32]), None);
    }

    #[test]
    fn one_commitment_cannot_name_two_notes() {
        let mut db = Memory::new();
        let mut u = Utxos::open(&db).unwrap();
        u.add(&mut db, &utxo(0xA1, 5)).unwrap();
        assert_eq!(u.add(&mut db, &utxo(0xA1, 6)), Err(Error::UtxoExists));
    }

    #[test]
    fn the_set_comes_back_from_the_records_on_a_restart() {
        let mut db = Memory::new();
        let mut u = Utxos::open(&db).unwrap();
        u.add(&mut db, &utxo(0xA1, 5)).unwrap();
        u.add(&mut db, &utxo(0xA2, 6)).unwrap();
        assert_eq!(Utxos::open(&db).unwrap().count(), 2);
    }

    #[test]
    fn a_reload_after_a_discarded_block_drops_what_it_added() {
        let mut view = View::new(Memory::new());
        let mut u = Utxos::open(&view).unwrap();
        u.add(&mut view, &utxo(0xA1, 5)).unwrap();
        view.abort();
        u.reload(&view).unwrap();
        assert_eq!(u.count(), 0);
    }

    /// A record under this prefix that does not name the commitment its own key
    /// is made of belongs to someone else.
    #[test]
    fn a_record_that_does_not_name_its_own_key_is_left_alone() {
        let mut db = Memory::new();
        db.put(&key(&[0xA1; 32]), &wire::write_utxo(&utxo(0xA2, 5)))
            .unwrap();
        db.put(&key(&[0xA3; 32]), b"not a utxo at all").unwrap();
        assert_eq!(Utxos::open(&db).unwrap().count(), 0);
    }
}
