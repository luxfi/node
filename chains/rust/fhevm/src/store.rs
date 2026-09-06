// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain's key/value state, and the buffer a block commits through.
//!
//! Go stands this up as `versiondb.New(memdb.New())`: reads see the buffered
//! writes first and the committed base behind them, [`Store::commit`] folds the
//! buffer into the base, and [`Store::abort`] throws it away. That arrangement
//! is the whole of F's atomicity — a fee burn and the operation it pays for are
//! two writes into one buffer, and the block's single commit is what makes them
//! land together or not at all.
//!
//! The keys are ordered, so a prefix scan reads a namespace in one pass and in
//! the same order on every node. Go gets that from its iterator; here it is a
//! `BTreeMap`, for the same reason: a chain that rebuilt its caches in map
//! order would rebuild them differently on two nodes.

use std::collections::BTreeMap;

/// A key/value store with one uncommitted layer over it.
#[derive(Debug, Default)]
pub struct Store {
    base: BTreeMap<Vec<u8>, Vec<u8>>,
    /// The uncommitted layer. `None` is a deletion, which is not the same as
    /// an absence: a key deleted in the buffer must read as gone even while the
    /// base still holds it.
    buf: BTreeMap<Vec<u8>, Option<Vec<u8>>>,
}

impl Store {
    pub fn new() -> Store {
        Store::default()
    }

    pub fn get(&self, key: &[u8]) -> Option<&[u8]> {
        match self.buf.get(key) {
            Some(Some(v)) => Some(v),
            Some(None) => None,
            None => self.base.get(key).map(|v| v.as_slice()),
        }
    }

    pub fn has(&self, key: &[u8]) -> bool {
        self.get(key).is_some()
    }

    pub fn put(&mut self, key: &[u8], value: &[u8]) {
        self.buf.insert(key.to_vec(), Some(value.to_vec()));
    }

    pub fn delete(&mut self, key: &[u8]) {
        self.buf.insert(key.to_vec(), None);
    }

    /// Every committed-or-buffered pair whose key starts with `prefix`, in key
    /// order.
    pub fn scan(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        let mut keys: Vec<&Vec<u8>> = self
            .base
            .keys()
            .chain(self.buf.keys())
            .filter(|k| k.starts_with(prefix))
            .collect();
        keys.sort_unstable();
        keys.dedup();
        keys.into_iter()
            .filter_map(|k| self.get(k).map(|v| (k.clone(), v.to_vec())))
            .collect()
    }

    /// Fold the buffer into the base.
    pub fn commit(&mut self) {
        for (k, v) in std::mem::take(&mut self.buf) {
            match v {
                Some(v) => {
                    self.base.insert(k, v);
                }
                None => {
                    self.base.remove(&k);
                }
            }
        }
    }

    /// Throw the buffer away. What is left is what was committed.
    pub fn abort(&mut self) {
        self.buf.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_buffered_write_reads_back_and_a_commit_keeps_it() {
        let mut s = Store::new();
        s.put(b"a", b"1");
        assert_eq!(s.get(b"a"), Some(&b"1"[..]));
        s.commit();
        assert_eq!(s.get(b"a"), Some(&b"1"[..]));
    }

    #[test]
    fn an_abort_leaves_exactly_what_was_committed() {
        let mut s = Store::new();
        s.put(b"a", b"1");
        s.commit();
        s.put(b"a", b"2");
        s.put(b"b", b"3");
        s.abort();
        assert_eq!(s.get(b"a"), Some(&b"1"[..]));
        assert_eq!(s.get(b"b"), None);
    }

    #[test]
    fn a_buffered_deletion_hides_a_committed_key() {
        let mut s = Store::new();
        s.put(b"a", b"1");
        s.commit();
        s.delete(b"a");
        assert!(!s.has(b"a"));
        s.abort();
        assert!(s.has(b"a"));
    }

    #[test]
    fn a_scan_reads_one_namespace_in_key_order() {
        let mut s = Store::new();
        s.put(b"ct:b", b"2");
        s.put(b"ct:a", b"1");
        s.put(b"pm:a", b"x");
        s.commit();
        s.put(b"ct:c", b"3");
        let got: Vec<(String, String)> = s
            .scan(b"ct:")
            .into_iter()
            .map(|(k, v)| {
                (String::from_utf8(k).unwrap(), String::from_utf8(v).unwrap())
            })
            .collect();
        assert_eq!(
            got,
            vec![
                ("ct:a".into(), "1".into()),
                ("ct:b".into(), "2".into()),
                ("ct:c".into(), "3".into())
            ]
        );
    }
}
