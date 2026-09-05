// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the chain keeps, and how a block's writes reach the device as ONE
//! thing.
//!
//! A block, its height index entry and the tip pointer move together or not at
//! all. Without that a node restarts holding a tip pointer to a block it did
//! not store, or a height index naming a block that is not there — and both are
//! states no rule can recover from, because the chain's own record of where it
//! is disagrees with what it holds.
//!
//! So the interface is not "put a key": it is [`Store::commit`], which takes
//! the whole batch. There is no way to write half of one.
//!
//! Two implementations, one meaning. [`Memory`] is for tests. [`Log`] is a
//! node's: an append-only file of committed batches, each one length-prefixed
//! and digested, replayed into an index at open. A torn tail — a batch that was
//! being written when the power went — fails its digest and is dropped, which
//! is the same all-or-nothing rule the commit gives, extended across a crash.

use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::Path;
use std::sync::{Mutex, RwLock};

use sha2::{Digest, Sha256};

use crate::error::{Error, Result};

/// One key and the value it takes.
pub type Entry = (Vec<u8>, Vec<u8>);

/// What the chain reads from and commits to.
pub trait Store: Send + Sync {
    /// The value under `key`, or [`Error::NotFound`].
    ///
    /// A missing key and a store that could not answer are DIFFERENT facts, and
    /// this reports them differently. Answering "missing" for both is how one
    /// transient failure looks like a chain that never started.
    fn get(&self, key: &[u8]) -> Result<Vec<u8>>;

    /// Whether a key is there.
    fn has(&self, key: &[u8]) -> Result<bool> {
        match self.get(key) {
            Ok(_) => Ok(true),
            Err(Error::NotFound) => Ok(false),
            Err(e) => Err(e),
        }
    }

    /// Apply every write, or none of them, and be durable before returning.
    fn commit(&self, writes: &[Entry]) -> Result<()>;
}

/// A store in memory. Durable across nothing, which is why a node does not use
/// one.
#[derive(Default)]
pub struct Memory {
    map: Mutex<BTreeMap<Vec<u8>, Vec<u8>>>,
}

impl Memory {
    pub fn new() -> Memory {
        Memory::default()
    }

    /// How many keys are held. For tests that count what a commit wrote.
    pub fn len(&self) -> usize {
        self.map.lock().expect("store").len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

impl Store for Memory {
    fn get(&self, key: &[u8]) -> Result<Vec<u8>> {
        self.map
            .lock()
            .expect("store")
            .get(key)
            .cloned()
            .ok_or(Error::NotFound)
    }

    fn commit(&self, writes: &[Entry]) -> Result<()> {
        let mut map = self.map.lock().expect("store");
        for (k, v) in writes {
            map.insert(k.clone(), v.clone());
        }
        Ok(())
    }
}

/// One committed batch on disk:
///
/// ```text
/// [8-byte BE payload length][32-byte SHA-256 of the payload][payload]
/// ```
///
/// and the payload is `[4-byte BE key length][key][4-byte BE value length][value]`
/// repeated. Big-endian because that is the byte order this estate writes
/// lengths in, and a digest rather than a checksum because the same hash names
/// every block already.
const LEN: usize = 8;
const DIGEST: usize = 32;
const FRAME: usize = LEN + DIGEST;

/// A node's store: an append-only log of committed batches, indexed in memory.
pub struct Log {
    file: Mutex<File>,
    map: RwLock<BTreeMap<Vec<u8>, Vec<u8>>>,
}

impl Log {
    /// Open the log at `path`, replaying what is already there.
    ///
    /// A torn final batch is dropped and the file cut back to the last whole
    /// one, so the next append lands on a boundary. Under the write-then-apply
    /// order a torn batch is a batch no reader was ever told about, which is
    /// why dropping it is the correct repair rather than a loss.
    pub fn open(path: impl AsRef<Path>) -> Result<Log> {
        let path = path.as_ref();
        if let Some(dir) = path.parent() {
            if !dir.as_os_str().is_empty() {
                std::fs::create_dir_all(dir).map_err(|e| Error::Store(e.to_string()))?;
            }
        }
        let mut file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(path)
            .map_err(|e| Error::Store(e.to_string()))?;

        let mut raw = Vec::new();
        file.read_to_end(&mut raw)
            .map_err(|e| Error::Store(e.to_string()))?;

        let mut map = BTreeMap::new();
        let mut good = 0usize;
        let mut at = 0usize;
        while at + FRAME <= raw.len() {
            let len = u64::from_be_bytes(raw[at..at + LEN].try_into().unwrap()) as usize;
            let body = at + FRAME;
            if body + len > raw.len() {
                break; // the batch was being written when the power went
            }
            let want = &raw[at + LEN..at + FRAME];
            let mut h = Sha256::new();
            h.update(&raw[body..body + len]);
            if h.finalize().as_slice() != want {
                break; // the bytes reached the device and the batch did not
            }
            for (k, v) in unpack(&raw[body..body + len])? {
                map.insert(k, v);
            }
            at = body + len;
            good = at;
        }
        if good != raw.len() {
            file.set_len(good as u64)
                .map_err(|e| Error::Store(e.to_string()))?;
        }
        file.seek(SeekFrom::End(0))
            .map_err(|e| Error::Store(e.to_string()))?;

        Ok(Log {
            file: Mutex::new(file),
            map: RwLock::new(map),
        })
    }

    /// How many committed batches the file holds.
    pub fn batches(&self) -> Result<usize> {
        let mut file = self.file.lock().expect("store");
        let at = file
            .stream_position()
            .map_err(|e| Error::Store(e.to_string()))?;
        file.seek(SeekFrom::Start(0))
            .map_err(|e| Error::Store(e.to_string()))?;
        let mut raw = Vec::new();
        file.read_to_end(&mut raw)
            .map_err(|e| Error::Store(e.to_string()))?;
        file.seek(SeekFrom::Start(at))
            .map_err(|e| Error::Store(e.to_string()))?;

        let mut n = 0;
        let mut cursor = 0usize;
        while cursor + FRAME <= raw.len() {
            let len = u64::from_be_bytes(raw[cursor..cursor + LEN].try_into().unwrap()) as usize;
            if cursor + FRAME + len > raw.len() {
                break;
            }
            cursor += FRAME + len;
            n += 1;
        }
        Ok(n)
    }
}

impl Store for Log {
    fn get(&self, key: &[u8]) -> Result<Vec<u8>> {
        self.map
            .read()
            .expect("store")
            .get(key)
            .cloned()
            .ok_or(Error::NotFound)
    }

    fn commit(&self, writes: &[Entry]) -> Result<()> {
        if writes.is_empty() {
            return Ok(());
        }
        let payload = pack(writes);
        let mut record = Vec::with_capacity(FRAME + payload.len());
        record.extend_from_slice(&(payload.len() as u64).to_be_bytes());
        let mut h = Sha256::new();
        h.update(&payload);
        record.extend_from_slice(&h.finalize());
        record.extend_from_slice(&payload);

        // Durable FIRST, visible second. A batch that is readable and not on
        // the device is a batch a restart forgets, and the chain would come
        // back naming a tip it cannot produce.
        {
            let mut file = self.file.lock().expect("store");
            let at = file
                .stream_position()
                .map_err(|e| Error::Store(e.to_string()))?;
            if let Err(e) = file.write_all(&record).and_then(|()| file.sync_all()) {
                // Cut the partial record back off, so the log stays a run of
                // whole batches even before the replay that would drop it.
                let _ = file.set_len(at);
                let _ = file.seek(SeekFrom::End(0));
                return Err(Error::Store(e.to_string()));
            }
        }

        let mut map = self.map.write().expect("store");
        for (k, v) in writes {
            map.insert(k.clone(), v.clone());
        }
        Ok(())
    }
}

fn pack(writes: &[Entry]) -> Vec<u8> {
    let mut out = Vec::new();
    for (k, v) in writes {
        out.extend_from_slice(&(k.len() as u32).to_be_bytes());
        out.extend_from_slice(k);
        out.extend_from_slice(&(v.len() as u32).to_be_bytes());
        out.extend_from_slice(v);
    }
    out
}

fn unpack(raw: &[u8]) -> Result<Vec<Entry>> {
    let mut out = Vec::new();
    let mut at = 0usize;
    while at < raw.len() {
        if at + 4 > raw.len() {
            return Err(Error::Store("a batch ends inside a key length".into()));
        }
        let klen = u32::from_be_bytes(raw[at..at + 4].try_into().unwrap()) as usize;
        at += 4;
        if at + klen + 4 > raw.len() {
            return Err(Error::Store("a batch ends inside a key".into()));
        }
        let key = raw[at..at + klen].to_vec();
        at += klen;
        let vlen = u32::from_be_bytes(raw[at..at + 4].try_into().unwrap()) as usize;
        at += 4;
        if at + vlen > raw.len() {
            return Err(Error::Store("a batch ends inside a value".into()));
        }
        let value = raw[at..at + vlen].to_vec();
        at += vlen;
        out.push((key, value));
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp(name: &str) -> std::path::PathBuf {
        let mut p = std::env::temp_dir();
        p.push(format!(
            "quantumvm-store-{}-{}-{:?}",
            name,
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        p
    }

    fn store_round_trip(s: &dyn Store) {
        assert!(matches!(s.get(b"nothing"), Err(Error::NotFound)));
        assert!(!s.has(b"nothing").unwrap());
        s.commit(&[
            (b"a".to_vec(), b"1".to_vec()),
            (b"b".to_vec(), b"2".to_vec()),
        ])
        .unwrap();
        assert_eq!(s.get(b"a").unwrap(), b"1");
        assert_eq!(s.get(b"b").unwrap(), b"2");
        assert!(s.has(b"a").unwrap());
        s.commit(&[(b"a".to_vec(), b"3".to_vec())]).unwrap();
        assert_eq!(s.get(b"a").unwrap(), b"3");
    }

    #[test]
    fn both_stores_mean_the_same_thing() {
        store_round_trip(&Memory::new());
        let path = temp("round-trip");
        store_round_trip(&Log::open(&path).unwrap());
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn what_was_committed_is_there_after_a_restart() {
        let path = temp("restart");
        {
            let log = Log::open(&path).unwrap();
            log.commit(&[
                (b"lastAccepted".to_vec(), vec![7; 32]),
                (b"height".to_vec(), 42u64.to_be_bytes().to_vec()),
            ])
            .unwrap();
        }
        let reopened = Log::open(&path).unwrap();
        assert_eq!(reopened.get(b"lastAccepted").unwrap(), vec![7; 32]);
        assert_eq!(
            reopened.get(b"height").unwrap(),
            42u64.to_be_bytes().to_vec()
        );
        assert_eq!(reopened.batches().unwrap(), 1);
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn a_batch_that_was_being_written_when_the_power_went_is_dropped_whole() {
        let path = temp("torn");
        {
            let log = Log::open(&path).unwrap();
            log.commit(&[(b"first".to_vec(), b"one".to_vec())]).unwrap();
            log.commit(&[
                (b"second".to_vec(), b"two".to_vec()),
                (b"tip".to_vec(), b"moved".to_vec()),
            ])
            .unwrap();
        }
        // Tear the last batch in half — the state a crash mid-write leaves.
        let mut raw = std::fs::read(&path).unwrap();
        let cut = raw.len() - 6;
        raw.truncate(cut);
        std::fs::write(&path, &raw).unwrap();

        let reopened = Log::open(&path).unwrap();
        assert_eq!(reopened.get(b"first").unwrap(), b"one");
        // Neither half of the torn batch survived: all or nothing holds across
        // the crash, not just across the call.
        assert!(matches!(reopened.get(b"second"), Err(Error::NotFound)));
        assert!(matches!(reopened.get(b"tip"), Err(Error::NotFound)));
        assert_eq!(reopened.batches().unwrap(), 1);

        // And the file was cut back, so the next append lands on a boundary.
        reopened
            .commit(&[(b"third".to_vec(), b"three".to_vec())])
            .unwrap();
        let again = Log::open(&path).unwrap();
        assert_eq!(again.get(b"third").unwrap(), b"three");
        assert_eq!(again.batches().unwrap(), 2);
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn a_batch_whose_bytes_were_altered_is_dropped() {
        let path = temp("altered");
        {
            let log = Log::open(&path).unwrap();
            log.commit(&[(b"key".to_vec(), b"honest".to_vec())])
                .unwrap();
        }
        let mut raw = std::fs::read(&path).unwrap();
        let last = raw.len() - 1;
        raw[last] ^= 0xFF;
        std::fs::write(&path, &raw).unwrap();

        let reopened = Log::open(&path).unwrap();
        assert!(matches!(reopened.get(b"key"), Err(Error::NotFound)));
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn an_empty_commit_writes_nothing() {
        let path = temp("empty");
        let log = Log::open(&path).unwrap();
        log.commit(&[]).unwrap();
        assert_eq!(log.batches().unwrap(), 0);
        let _ = std::fs::remove_file(path);
    }
}
