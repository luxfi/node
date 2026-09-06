// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Where the chain keeps things, and the staging that makes a block's writes
//! one thing.
//!
//! Two shapes and one meaning. [`Memory`] is a keyspace. [`View`] is that
//! keyspace plus whatever the block in progress has staged: every read sees
//! both, every write lands in the staged half, and [`View::commit`] moves the
//! whole staged half across at once. [`View::abort`] throws it away.
//!
//! THAT IS THE WHOLE ALL-OR-NOTHING PROPERTY. A block's spends, its outputs,
//! the block record, its height entry and the tip pointer are staged through
//! one view and committed together, so the chain has the whole block or none of
//! it. A chain that wrote as it went would have a first failed write leave some
//! notes spent and some outputs created, under a tip it had already advanced —
//! a shielded pool half applied, with no way back and no way to apply the block
//! again.
//!
//! A MISSING KEY AND A STORE THAT COULD NOT ANSWER ARE DIFFERENT FACTS, and
//! this reports them differently. Answering "missing" for both is how a chain
//! reads an unreadable disk as an empty one and starts over at genesis.

use std::collections::BTreeMap;

use crate::error::{Error, Result};

/// What the chain reads from and writes through.
pub trait Db {
    /// The value under `key`, or [`Error::NotFound`].
    fn get(&self, key: &[u8]) -> Result<Vec<u8>>;

    fn put(&mut self, key: &[u8], value: &[u8]) -> Result<()>;

    fn delete(&mut self, key: &[u8]) -> Result<()>;

    /// Every key under `prefix`, in key order, with the prefix still on.
    fn prefixed(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)>;

    fn has(&self, key: &[u8]) -> Result<bool> {
        match self.get(key) {
            Ok(_) => Ok(true),
            Err(Error::NotFound) => Ok(false),
            Err(e) => Err(e),
        }
    }
}

/// A keyspace in memory. Durable across nothing, which is why a node does not
/// run on one.
#[derive(Clone, Debug, Default)]
pub struct Memory {
    map: BTreeMap<Vec<u8>, Vec<u8>>,
}

impl Memory {
    pub fn new() -> Memory {
        Memory::default()
    }

    pub fn len(&self) -> usize {
        self.map.len()
    }

    pub fn is_empty(&self) -> bool {
        self.map.is_empty()
    }
}

impl Db for Memory {
    fn get(&self, key: &[u8]) -> Result<Vec<u8>> {
        self.map.get(key).cloned().ok_or(Error::NotFound)
    }

    fn put(&mut self, key: &[u8], value: &[u8]) -> Result<()> {
        self.map.insert(key.to_vec(), value.to_vec());
        Ok(())
    }

    fn delete(&mut self, key: &[u8]) -> Result<()> {
        self.map.remove(key);
        Ok(())
    }

    fn prefixed(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        self.map
            .range(prefix.to_vec()..)
            .take_while(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect()
    }
}

/// Committed state, plus what has been staged on top of it.
///
/// A staged entry is `Some(value)` for a write and `None` for a delete, so a
/// key deleted in the staged half reads as absent even while committed state
/// still holds it — the same answer the commit will make true.
#[derive(Debug, Default)]
pub struct View {
    base: Memory,
    staged: BTreeMap<Vec<u8>, Option<Vec<u8>>>,
}

impl View {
    pub fn new(base: Memory) -> View {
        View {
            base,
            staged: BTreeMap::new(),
        }
    }

    /// Move everything staged into committed state.
    pub fn commit(&mut self) {
        for (k, v) in std::mem::take(&mut self.staged) {
            match v {
                Some(v) => {
                    self.base.map.insert(k, v);
                }
                None => {
                    self.base.map.remove(&k);
                }
            }
        }
    }

    /// Throw away everything staged. What was staged belonged to a block that
    /// was never accepted.
    pub fn abort(&mut self) {
        self.staged.clear();
    }

    /// Whether anything is waiting to be committed.
    pub fn staged(&self) -> usize {
        self.staged.len()
    }

    /// Committed state, for the writes a chain makes outside any block. A write
    /// here is durable at once and belongs to no block, so no rollback takes it
    /// back — which is right for a record no block claims and wrong for one a
    /// block does.
    pub fn base(&mut self) -> &mut Memory {
        &mut self.base
    }
}

impl Db for View {
    fn get(&self, key: &[u8]) -> Result<Vec<u8>> {
        match self.staged.get(key) {
            Some(Some(v)) => Ok(v.clone()),
            Some(None) => Err(Error::NotFound),
            None => self.base.get(key),
        }
    }

    fn put(&mut self, key: &[u8], value: &[u8]) -> Result<()> {
        self.staged.insert(key.to_vec(), Some(value.to_vec()));
        Ok(())
    }

    fn delete(&mut self, key: &[u8]) -> Result<()> {
        self.staged.insert(key.to_vec(), None);
        Ok(())
    }

    fn prefixed(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        let mut out: BTreeMap<Vec<u8>, Vec<u8>> = self.base.prefixed(prefix).into_iter().collect();
        for (k, v) in self
            .staged
            .range(prefix.to_vec()..)
            .take_while(|(k, _)| k.starts_with(prefix))
        {
            match v {
                Some(v) => {
                    out.insert(k.clone(), v.clone());
                }
                None => {
                    out.remove(k);
                }
            }
        }
        out.into_iter().collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_missing_key_is_not_found_rather_than_empty() {
        let m = Memory::new();
        assert_eq!(m.get(b"nothing"), Err(Error::NotFound));
        assert_eq!(m.has(b"nothing"), Ok(false));
    }

    #[test]
    fn a_view_reads_what_it_has_staged_before_what_is_committed() {
        let mut base = Memory::new();
        base.put(b"k", b"committed").unwrap();
        let mut v = View::new(base);
        assert_eq!(v.get(b"k"), Ok(b"committed".to_vec()));
        v.put(b"k", b"staged").unwrap();
        assert_eq!(v.get(b"k"), Ok(b"staged".to_vec()));
    }

    #[test]
    fn an_abort_leaves_committed_state_exactly_as_it_was() {
        let mut base = Memory::new();
        base.put(b"k", b"committed").unwrap();
        let mut v = View::new(base);
        v.put(b"k", b"staged").unwrap();
        v.put(b"new", b"also staged").unwrap();
        v.abort();
        assert_eq!(v.get(b"k"), Ok(b"committed".to_vec()));
        assert_eq!(v.get(b"new"), Err(Error::NotFound));
        assert_eq!(v.staged(), 0);
    }

    #[test]
    fn a_commit_moves_every_staged_write_at_once() {
        let mut v = View::new(Memory::new());
        v.put(b"a", b"1").unwrap();
        v.put(b"b", b"2").unwrap();
        v.commit();
        assert_eq!(v.staged(), 0);
        assert_eq!(v.get(b"a"), Ok(b"1".to_vec()));
        assert_eq!(v.get(b"b"), Ok(b"2".to_vec()));
        // And an abort afterwards cannot take back what was committed.
        v.abort();
        assert_eq!(v.get(b"a"), Ok(b"1".to_vec()));
    }

    #[test]
    fn a_staged_delete_reads_as_absent_and_commits_as_a_removal() {
        let mut base = Memory::new();
        base.put(b"k", b"v").unwrap();
        let mut v = View::new(base);
        v.delete(b"k").unwrap();
        assert_eq!(v.get(b"k"), Err(Error::NotFound));
        v.commit();
        assert_eq!(v.get(b"k"), Err(Error::NotFound));
    }

    #[test]
    fn a_prefix_scan_sees_committed_and_staged_together() {
        let mut base = Memory::new();
        base.put(&[0x10, 1], b"a").unwrap();
        base.put(&[0x10, 2], b"b").unwrap();
        base.put(&[0x20, 1], b"other").unwrap();
        let mut v = View::new(base);
        v.put(&[0x10, 3], b"c").unwrap();
        v.delete(&[0x10, 1]).unwrap();

        let seen = v.prefixed(&[0x10]);
        assert_eq!(
            seen,
            vec![
                (vec![0x10, 2], b"b".to_vec()),
                (vec![0x10, 3], b"c".to_vec()),
            ]
        );
    }

    /// A write to committed state belongs to no block, so a later abort does
    /// not take it back. That is what genesis needs and what a block must not
    /// have.
    #[test]
    fn a_write_to_the_base_survives_an_abort() {
        let mut v = View::new(Memory::new());
        v.base().put(b"genesis", b"allocated").unwrap();
        v.put(b"block", b"staged").unwrap();
        v.abort();
        assert_eq!(v.get(b"genesis"), Ok(b"allocated".to_vec()));
        assert_eq!(v.get(b"block"), Err(Error::NotFound));
    }
}
