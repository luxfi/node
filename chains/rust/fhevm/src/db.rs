// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The store, and the layer that makes a block atomic.
//!
//! [`Kv`] is the read/write surface a chain needs, stated as a trait so the host
//! supplies the durable store and this crate never picks one. It is the same
//! surface Go's `luxfi/database.Database` presents to a VM, narrowed to what F
//! uses: has, get, put, delete, a prefix walk, and a batch.
//!
//! [`Version`] is the layer a block is written through. Every write a block makes
//! — the fee burn, the record, the nonce, the block itself, the height index, the
//! last-accepted pointer — lands in memory, and [`Version::commit`] writes them to
//! the store in ONE batch. Either the whole block happened or none of it did. A
//! failure anywhere aborts, [`Version::abort`] drops the memory layer, and the
//! store is exactly what it was.
//!
//! ONE RULE ABOUT ABSENCE. [`Kv::get`] answers [`Error::NotFound`] for a key that
//! is not there and an error for a read that FAILED, and the two are never
//! merged. A live chain reading as a fresh one is what merging them looks like
//! from the outside.

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

/// Why a store did not answer.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// The key is not there. Not a failure — an answer.
    NotFound,
    /// The database is closed.
    Closed,
    /// The store said something else.
    Other(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::NotFound => write!(f, "not found"),
            Error::Closed => write!(f, "database closed"),
            Error::Other(why) => write!(f, "{why}"),
        }
    }
}

impl std::error::Error for Error {}

/// What a prefix walk hands back: key and value, in ascending key order.
pub type Rows = Vec<(Vec<u8>, Vec<u8>)>;

/// One write in a batch: a value, or the absence of one.
#[derive(Clone, Debug)]
pub enum Write {
    Put(Vec<u8>, Vec<u8>),
    Delete(Vec<u8>),
}

/// A key/value store.
///
/// `&self` throughout, with whatever lock the implementation needs inside it:
/// the VM holds one store and hands it to a ledger, a record writer and a block
/// at the same time, and a store that demanded `&mut` would make that one
/// borrow instead of one database.
pub trait Kv: Send + Sync {
    fn has(&self, key: &[u8]) -> Result<bool, Error>;
    /// The value, or [`Error::NotFound`]. Never an empty value for a missing key.
    fn get(&self, key: &[u8]) -> Result<Vec<u8>, Error>;
    fn put(&self, key: &[u8], value: &[u8]) -> Result<(), Error>;
    fn delete(&self, key: &[u8]) -> Result<(), Error>;
    /// Every pair whose key starts with `prefix`, in ascending key order.
    ///
    /// The whole walk answers at once, so a store that fails PART WAY through
    /// says so instead of handing back the rows it managed. Go's iterator says
    /// the same thing in two calls (`Next` then `Error`), and the caller there
    /// has to remember to ask; here it cannot forget.
    fn scan(&self, prefix: &[u8]) -> Result<Rows, Error>;
    /// Apply every write, or none of them.
    fn write_batch(&self, batch: &[Write]) -> Result<(), Error>;
}

/// A store in memory. What a test runs on, and what a node runs on before it is
/// given a disk.
#[derive(Debug, Default)]
pub struct Mem {
    data: Mutex<BTreeMap<Vec<u8>, Vec<u8>>>,
    closed: AtomicBool,
}

impl Mem {
    pub fn new() -> Arc<Self> {
        Arc::new(Mem { data: Mutex::new(BTreeMap::new()), closed: AtomicBool::new(false) })
    }

    pub fn close(&self) {
        self.closed.store(true, Ordering::SeqCst);
    }

    fn live(&self) -> Result<(), Error> {
        if self.closed.load(Ordering::SeqCst) {
            return Err(Error::Closed);
        }
        Ok(())
    }
}

impl Kv for Mem {
    fn has(&self, key: &[u8]) -> Result<bool, Error> {
        self.live()?;
        Ok(self.data.lock().unwrap().contains_key(key))
    }

    fn get(&self, key: &[u8]) -> Result<Vec<u8>, Error> {
        self.live()?;
        self.data.lock().unwrap().get(key).cloned().ok_or(Error::NotFound)
    }

    fn put(&self, key: &[u8], value: &[u8]) -> Result<(), Error> {
        self.live()?;
        self.data.lock().unwrap().insert(key.to_vec(), value.to_vec());
        Ok(())
    }

    fn delete(&self, key: &[u8]) -> Result<(), Error> {
        self.live()?;
        self.data.lock().unwrap().remove(key);
        Ok(())
    }

    fn scan(&self, prefix: &[u8]) -> Result<Rows, Error> {
        self.live()?;
        let data = self.data.lock().unwrap();
        Ok(data
            .iter()
            .filter(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect())
    }

    fn write_batch(&self, batch: &[Write]) -> Result<(), Error> {
        self.live()?;
        let mut data = self.data.lock().unwrap();
        for w in batch {
            match w {
                Write::Put(k, v) => {
                    data.insert(k.clone(), v.clone());
                }
                Write::Delete(k) => {
                    data.remove(k);
                }
            }
        }
        Ok(())
    }
}

/// The layer a block is written through.
///
/// Reads see the memory layer first and the store behind it; writes only ever
/// reach memory. [`Version::commit`] is the one moment the store changes, and it
/// changes in one batch.
pub struct Version {
    base: Arc<dyn Kv>,
    /// `None` is a deletion held in memory: the key is gone even though the
    /// store still has it. Without the distinction an uncommitted delete would
    /// read straight through to the value it removed.
    mem: Mutex<BTreeMap<Vec<u8>, Option<Vec<u8>>>>,
    closed: AtomicBool,
}

impl Version {
    pub fn new(base: Arc<dyn Kv>) -> Arc<Self> {
        Arc::new(Version {
            base,
            mem: Mutex::new(BTreeMap::new()),
            closed: AtomicBool::new(false),
        })
    }

    /// Writes everything held in memory, in one batch, and empties the layer.
    pub fn commit(&self) -> Result<(), Error> {
        self.live()?;
        let batch: Vec<Write> = {
            let mem = self.mem.lock().unwrap();
            mem.iter()
                .map(|(k, v)| match v {
                    Some(value) => Write::Put(k.clone(), value.clone()),
                    None => Write::Delete(k.clone()),
                })
                .collect()
        };
        // The store is asked first. A batch that fails leaves the memory layer
        // holding exactly what it held, so the caller can abort it — or, as the
        // fault tests do, try the same block again once the disk answers.
        self.base.write_batch(&batch)?;
        self.mem.lock().unwrap().clear();
        Ok(())
    }

    /// Drops everything held in memory. The store is untouched, because nothing
    /// ever reached it.
    pub fn abort(&self) {
        self.mem.lock().unwrap().clear();
    }

    pub fn close(&self) -> Result<(), Error> {
        self.closed.store(true, Ordering::SeqCst);
        Ok(())
    }

    fn live(&self) -> Result<(), Error> {
        if self.closed.load(Ordering::SeqCst) {
            return Err(Error::Closed);
        }
        Ok(())
    }
}

impl Kv for Version {
    fn has(&self, key: &[u8]) -> Result<bool, Error> {
        self.live()?;
        if let Some(held) = self.mem.lock().unwrap().get(key) {
            return Ok(held.is_some());
        }
        self.base.has(key)
    }

    fn get(&self, key: &[u8]) -> Result<Vec<u8>, Error> {
        self.live()?;
        if let Some(held) = self.mem.lock().unwrap().get(key) {
            return held.clone().ok_or(Error::NotFound);
        }
        self.base.get(key)
    }

    fn put(&self, key: &[u8], value: &[u8]) -> Result<(), Error> {
        self.live()?;
        self.mem.lock().unwrap().insert(key.to_vec(), Some(value.to_vec()));
        Ok(())
    }

    fn delete(&self, key: &[u8]) -> Result<(), Error> {
        self.live()?;
        self.mem.lock().unwrap().insert(key.to_vec(), None);
        Ok(())
    }

    fn scan(&self, prefix: &[u8]) -> Result<Rows, Error> {
        self.live()?;
        let mut merged: BTreeMap<Vec<u8>, Vec<u8>> =
            self.base.scan(prefix)?.into_iter().collect();
        for (k, v) in self.mem.lock().unwrap().iter() {
            if !k.starts_with(prefix) {
                continue;
            }
            match v {
                Some(value) => {
                    merged.insert(k.clone(), value.clone());
                }
                None => {
                    merged.remove(k);
                }
            }
        }
        Ok(merged.into_iter().collect())
    }

    fn write_batch(&self, batch: &[Write]) -> Result<(), Error> {
        self.live()?;
        let mut mem = self.mem.lock().unwrap();
        for w in batch {
            match w {
                Write::Put(k, v) => {
                    mem.insert(k.clone(), Some(v.clone()));
                }
                Write::Delete(k) => {
                    mem.insert(k.clone(), None);
                }
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_missing_key_is_an_answer_not_a_failure() {
        let db = Mem::new();
        assert_eq!(db.get(b"nothing"), Err(Error::NotFound));
        assert_eq!(db.has(b"nothing"), Ok(false));
    }

    #[test]
    fn nothing_written_through_the_layer_reaches_the_store_until_it_commits() {
        let base = Mem::new();
        let v = Version::new(base.clone());
        v.put(b"k", b"v").unwrap();
        assert_eq!(v.get(b"k").unwrap(), b"v".to_vec());
        assert_eq!(base.get(b"k"), Err(Error::NotFound), "the store has not been told");
        v.commit().unwrap();
        assert_eq!(base.get(b"k").unwrap(), b"v".to_vec());
    }

    #[test]
    fn an_abort_leaves_the_store_where_it_was() {
        let base = Mem::new();
        base.put(b"k", b"before").unwrap();
        let v = Version::new(base.clone());
        v.put(b"k", b"after").unwrap();
        v.abort();
        assert_eq!(v.get(b"k").unwrap(), b"before".to_vec());
        assert_eq!(base.get(b"k").unwrap(), b"before".to_vec());
    }

    #[test]
    fn a_deletion_held_in_memory_does_not_read_through_to_the_value_it_removed() {
        let base = Mem::new();
        base.put(b"k", b"v").unwrap();
        let v = Version::new(base.clone());
        v.delete(b"k").unwrap();
        assert_eq!(v.get(b"k"), Err(Error::NotFound));
        assert_eq!(v.has(b"k"), Ok(false));
        assert!(v.scan(b"k").unwrap().is_empty());
        assert_eq!(base.get(b"k").unwrap(), b"v".to_vec(), "and the store still has it");
        v.commit().unwrap();
        assert_eq!(base.get(b"k"), Err(Error::NotFound));
    }

    #[test]
    fn a_scan_merges_both_layers_in_key_order() {
        let base = Mem::new();
        base.put(b"p:b", b"1").unwrap();
        base.put(b"p:d", b"2").unwrap();
        base.put(b"q:z", b"3").unwrap();
        let v = Version::new(base.clone());
        v.put(b"p:a", b"4").unwrap();
        v.put(b"p:d", b"5").unwrap();
        let got: Vec<_> = v
            .scan(b"p:")
            .unwrap()
            .into_iter()
            .map(|(k, val)| (String::from_utf8(k).unwrap(), String::from_utf8(val).unwrap()))
            .collect();
        assert_eq!(
            got,
            vec![
                ("p:a".into(), "4".to_string()),
                ("p:b".into(), "1".to_string()),
                ("p:d".into(), "5".to_string()),
            ]
        );
    }

    #[test]
    fn a_closed_layer_answers_nothing() {
        let v = Version::new(Mem::new());
        v.close().unwrap();
        assert_eq!(v.put(b"k", b"v"), Err(Error::Closed));
        assert_eq!(v.get(b"k"), Err(Error::Closed));
        assert_eq!(v.commit(), Err(Error::Closed));
    }
}
