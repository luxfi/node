// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Where the chain writes itself down.
//!
//! A node that cannot restart is a process, not a node. Everything above this
//! module holds the ledger in memory, which is correct and is not enough: a
//! chain that forgets on restart has to be handed its whole history again, and
//! a signature made at a past height cannot be checked against a validator set
//! nobody kept.
//!
//! ## The shape
//!
//! An ordered map from bytes to bytes, written in batches that either all
//! happen or none do. That is the whole seam ([`Db`]), and it is small because
//! everything above it was already written against ordered bytes: the UTXO set
//! enumerates in ascending id order because the state root folds in that order,
//! so the store's own ordering and the disk's are the same ordering.
//!
//! Go states this as `luxfi/database.Database` and stacks `prefixdb` and
//! `versiondb` on it. The prefixes below are those, byte for byte, so the same
//! key names the same thing in both languages: `utxo`, `tx`, `blockID`,
//! `block`, `singleton`, and the three singleton keys `0x00`, `0x01`, `0x02`.
//! What is under a key is the object's canonical ZAP encoding — the same bytes
//! that go on a wire and that the id is taken over. Nothing is re-encoded on
//! the way to disk, so a stored block hashes to the id it is filed under.
//!
//! WHAT IS NOT CLAIMED. This is not Go's on-disk FILE format. That format is
//! LevelDB's, and it is not a consensus artefact — no rule in this chain is
//! stated over it, and two nodes never exchange it. The two things that ARE
//! consensus — the key that names a thing and the bytes under it — are shared.
//! The container around them is [`Log`], below, and it is this crate's own.
//!
//! ## Two implementations, and why the second one exists
//!
//! [`Memory`] is a map. It is what a test uses and what a node with nowhere to
//! write uses, and it is honest about being neither durable nor a lie about
//! durability.
//!
//! [`Log`] is a file. Each batch is appended as one framed record and the file
//! is flushed to the device before the write returns, so a batch that returned
//! is on disk and a batch that did not return either happened or did not. On
//! open the frames are replayed in order, which reconstructs the map, and a
//! torn tail — a frame whose length or digest does not check out — is a batch
//! that was interrupted mid-append. Under append-then-return ordering that
//! batch never returned to a caller, so nothing above ever saw its effects: it
//! is dropped and the file is cut back to the last whole frame, so the next
//! append lands on a boundary.
//!
//! WHY A LOG AND NOT A TREE. The chain writes at one moment — a block is
//! accepted — and reads its whole state at another — the node starts. An
//! append-only log makes the write one sequential flush and the read one
//! sequential pass, and it has no interior structure to corrupt. It grows with
//! the writes rather than with the state, which is why [`Log::compact`] exists:
//! it rewrites the file as one frame holding exactly what is live, through a
//! temporary file and a rename, so a crash during compaction leaves the old
//! file whole.

use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

use crate::error::{Error, Result};
use crate::hash::sha256;

// ------------------------------------------------------------ the prefixes --

/// Go's `utxoPrefix`, and the record sub-prefix `UTXOState` nests inside it.
/// Two levels, because Go's UTXO state is itself a prefixed database inside the
/// chain's UTXO database, and the key on disk is the concatenation.
pub const UTXO: &[u8] = b"utxoutxo";
/// Go's `txPrefix`.
pub const TX: &[u8] = b"tx";
/// Go's `blockIDPrefix`: height to block id.
pub const BLOCK_ID: &[u8] = b"blockID";
/// Go's `blockPrefix`: block id to block.
pub const BLOCK: &[u8] = b"block";
/// Go's `singletonPrefix`: the three facts that are not a set.
pub const SINGLETON: &[u8] = b"singleton";

/// `isInitializedKey`. Its presence is the fact; the value is one byte.
pub const IS_INITIALIZED: u8 = 0x00;
/// `timestampKey`.
pub const TIMESTAMP: u8 = 0x01;
/// `lastAcceptedKey`.
pub const LAST_ACCEPTED: u8 = 0x02;

/// A key under a prefix: the prefix, then the key, concatenated. Go's
/// `prefixdb.prefixKey`.
pub fn key(prefix: &[u8], rest: &[u8]) -> Vec<u8> {
    let mut k = Vec::with_capacity(prefix.len() + rest.len());
    k.extend_from_slice(prefix);
    k.extend_from_slice(rest);
    k
}

/// A height as a key: eight bytes, big-endian, so the byte order and the
/// numeric order are one order. Go's `database.PackUInt64`.
pub fn height_key(height: u64) -> Vec<u8> {
    key(BLOCK_ID, &height.to_be_bytes())
}

/// A height read back out of such a key.
pub fn height_of(k: &[u8]) -> Option<u64> {
    let rest = k.strip_prefix(BLOCK_ID)?;
    let eight: [u8; 8] = rest.try_into().ok()?;
    Some(u64::from_be_bytes(eight))
}

// ----------------------------------------------------------------- a batch --

/// A set of changes that happen together or not at all.
///
/// Ordered: the entries are applied in the order they were added, so a put and
/// a later delete of one key mean the key is gone, in memory and on disk alike.
#[derive(Clone, Debug, Default)]
pub struct Batch {
    entries: Vec<(Vec<u8>, Option<Vec<u8>>)>,
}

impl Batch {
    pub fn new() -> Batch {
        Batch::default()
    }

    pub fn put(&mut self, k: Vec<u8>, v: Vec<u8>) {
        self.entries.push((k, Some(v)));
    }

    pub fn delete(&mut self, k: Vec<u8>) {
        self.entries.push((k, None));
    }

    pub fn len(&self) -> usize {
        self.entries.len()
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    pub fn entries(&self) -> &[(Vec<u8>, Option<Vec<u8>>)] {
        &self.entries
    }

    /// One frame: the entry count, then each entry as key length, key, value
    /// length, value — with `u32::MAX` as the value length meaning a deletion,
    /// which is why an empty value and an absent value are different things
    /// here rather than the same thing.
    fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        out.extend_from_slice(&(self.entries.len() as u32).to_be_bytes());
        for (k, v) in &self.entries {
            out.extend_from_slice(&(k.len() as u32).to_be_bytes());
            out.extend_from_slice(k);
            match v {
                Some(v) => {
                    out.extend_from_slice(&(v.len() as u32).to_be_bytes());
                    out.extend_from_slice(v);
                }
                None => out.extend_from_slice(&u32::MAX.to_be_bytes()),
            }
        }
        out
    }

    fn decode(mut b: &[u8]) -> Option<Batch> {
        fn take<'a>(b: &mut &'a [u8], n: usize) -> Option<&'a [u8]> {
            if b.len() < n {
                return None;
            }
            let (head, tail) = b.split_at(n);
            *b = tail;
            Some(head)
        }
        fn u32_of(b: &mut &[u8]) -> Option<u32> {
            let four: [u8; 4] = take(b, 4)?.try_into().ok()?;
            Some(u32::from_be_bytes(four))
        }

        let count = u32_of(&mut b)?;
        let mut batch = Batch::new();
        for _ in 0..count {
            let klen = u32_of(&mut b)? as usize;
            let k = take(&mut b, klen)?.to_vec();
            let vlen = u32_of(&mut b)?;
            if vlen == u32::MAX {
                batch.delete(k);
            } else {
                let v = take(&mut b, vlen as usize)?.to_vec();
                batch.put(k, v);
            }
        }
        b.is_empty().then_some(batch)
    }
}

// ------------------------------------------------------------------ the db --

/// An ordered map from bytes to bytes, written atomically.
pub trait Db: Send + Sync {
    fn get(&self, k: &[u8]) -> Option<Vec<u8>>;

    /// Every pair whose key begins with `prefix`, in ascending key order.
    fn range(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)>;

    /// Apply the batch. It is on the device when this returns, or it did not
    /// happen and the error says why.
    fn write(&self, batch: &Batch) -> Result<()>;

    /// How many pairs are held. A way to see the store, not a consensus value.
    fn len(&self) -> usize;

    fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

/// A map. Not durable, and does not pretend to be.
#[derive(Default)]
pub struct Memory {
    map: Mutex<BTreeMap<Vec<u8>, Vec<u8>>>,
}

impl Memory {
    pub fn new() -> Memory {
        Memory::default()
    }
}

impl Db for Memory {
    fn get(&self, k: &[u8]) -> Option<Vec<u8>> {
        self.map.lock().expect("db poisoned").get(k).cloned()
    }

    fn range(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        self.map
            .lock()
            .expect("db poisoned")
            .iter()
            .filter(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect()
    }

    fn write(&self, batch: &Batch) -> Result<()> {
        let mut map = self.map.lock().expect("db poisoned");
        apply(&mut map, batch);
        Ok(())
    }

    fn len(&self) -> usize {
        self.map.lock().expect("db poisoned").len()
    }
}

fn apply(map: &mut BTreeMap<Vec<u8>, Vec<u8>>, batch: &Batch) {
    for (k, v) in batch.entries() {
        match v {
            Some(v) => {
                map.insert(k.clone(), v.clone());
            }
            None => {
                map.remove(k);
            }
        }
    }
}

// ----------------------------------------------------------------- the log --

/// Eight bytes of the frame's digest, which is enough to tell a whole frame
/// from a torn one. This is a torn-write check, not a signature: nothing here
/// defends against an attacker who can write the file, because such an attacker
/// owns the node's disk and could write a valid frame saying anything.
const DIGEST: usize = 8;
/// The frame header: the payload length.
const HEADER: usize = 4;

/// A file. Each batch is one appended frame, flushed to the device before the
/// write returns.
pub struct Log {
    path: PathBuf,
    inner: Mutex<Open>,
}

struct Open {
    file: File,
    map: BTreeMap<Vec<u8>, Vec<u8>>,
}

impl Log {
    /// Open the file at `path`, replaying what is in it, creating it if it is
    /// not there.
    ///
    /// A torn tail is dropped and the file is cut back to the last whole frame.
    pub fn open(path: impl AsRef<Path>) -> Result<Log> {
        let path = path.as_ref().to_path_buf();
        if let Some(dir) = path.parent() {
            if !dir.as_os_str().is_empty() {
                std::fs::create_dir_all(dir).map_err(|e| Error::Storage(e.to_string()))?;
            }
        }
        let mut file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(&path)
            .map_err(|e| Error::Storage(e.to_string()))?;

        let mut raw = Vec::new();
        file.read_to_end(&mut raw)
            .map_err(|e| Error::Storage(e.to_string()))?;

        let mut map = BTreeMap::new();
        let mut whole = 0usize;
        let mut at = 0usize;
        while at + HEADER + DIGEST <= raw.len() {
            let len = u32::from_be_bytes(
                raw[at..at + HEADER]
                    .try_into()
                    .expect("four bytes are four bytes"),
            ) as usize;
            let end = at + HEADER + len + DIGEST;
            if end > raw.len() {
                break; // a frame that was still being written
            }
            let payload = &raw[at + HEADER..at + HEADER + len];
            let want = &raw[at + HEADER + len..end];
            if &sha256(payload)[..DIGEST] != want {
                break; // the bytes arrived, the digest says not all of them
            }
            let Some(batch) = Batch::decode(payload) else {
                break; // a frame whose digest checks out but whose shape does not
            };
            apply(&mut map, &batch);
            at = end;
            whole = end;
        }

        if whole != raw.len() {
            file.set_len(whole as u64)
                .map_err(|e| Error::Storage(e.to_string()))?;
        }
        file.seek(SeekFrom::End(0))
            .map_err(|e| Error::Storage(e.to_string()))?;

        Ok(Log {
            path,
            inner: Mutex::new(Open { file, map }),
        })
    }

    /// Where this log is.
    pub fn path(&self) -> &Path {
        &self.path
    }

    /// How many bytes the file holds. Not a consensus value — the number
    /// [`Log::compact`] exists to bring down.
    pub fn size(&self) -> Result<u64> {
        std::fs::metadata(&self.path)
            .map(|m| m.len())
            .map_err(|e| Error::Storage(e.to_string()))
    }

    /// Rewrite the file as one frame holding exactly what is live.
    ///
    /// Through a temporary file and a rename, so the old file stays whole until
    /// the new one is on the device: a crash at any point leaves one of the two
    /// complete, and both say the same thing.
    pub fn compact(&self) -> Result<()> {
        let mut inner = self.inner.lock().expect("db poisoned");
        let mut batch = Batch::new();
        for (k, v) in inner.map.iter() {
            batch.put(k.clone(), v.clone());
        }

        let temp = self.path.with_extension("compacting");
        {
            let mut f = OpenOptions::new()
                .write(true)
                .create(true)
                .truncate(true)
                .open(&temp)
                .map_err(|e| Error::Storage(e.to_string()))?;
            if !batch.is_empty() {
                f.write_all(&frame(&batch))
                    .map_err(|e| Error::Storage(e.to_string()))?;
            }
            f.sync_all().map_err(|e| Error::Storage(e.to_string()))?;
        }
        std::fs::rename(&temp, &self.path).map_err(|e| Error::Storage(e.to_string()))?;

        let mut f = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&self.path)
            .map_err(|e| Error::Storage(e.to_string()))?;
        f.seek(SeekFrom::End(0))
            .map_err(|e| Error::Storage(e.to_string()))?;
        inner.file = f;
        Ok(())
    }
}

/// One frame: the payload length, the payload, and eight bytes of its digest.
fn frame(batch: &Batch) -> Vec<u8> {
    let payload = batch.encode();
    let mut out = Vec::with_capacity(HEADER + payload.len() + DIGEST);
    out.extend_from_slice(&(payload.len() as u32).to_be_bytes());
    out.extend_from_slice(&payload);
    out.extend_from_slice(&sha256(&payload)[..DIGEST]);
    out
}

impl Db for Log {
    fn get(&self, k: &[u8]) -> Option<Vec<u8>> {
        self.inner.lock().expect("db poisoned").map.get(k).cloned()
    }

    fn range(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        self.inner
            .lock()
            .expect("db poisoned")
            .map
            .iter()
            .filter(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect()
    }

    fn write(&self, batch: &Batch) -> Result<()> {
        if batch.is_empty() {
            return Ok(());
        }
        let mut inner = self.inner.lock().expect("db poisoned");
        // Device first, memory second. A crash between them loses a write that
        // no caller was told about; the other order would hand back a success
        // for something a restart will not remember.
        inner
            .file
            .write_all(&frame(batch))
            .map_err(|e| Error::Storage(e.to_string()))?;
        inner
            .file
            .sync_data()
            .map_err(|e| Error::Storage(e.to_string()))?;
        apply(&mut inner.map, batch);
        Ok(())
    }

    fn len(&self) -> usize {
        self.inner.lock().expect("db poisoned").map.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp(name: &str) -> PathBuf {
        let mut p = std::env::temp_dir();
        p.push(format!(
            "lux-xvm-db-{}-{}-{:?}",
            name,
            std::process::id(),
            std::thread::current().id()
        ));
        let _ = std::fs::remove_file(&p);
        p
    }

    #[test]
    fn a_key_is_its_prefix_then_the_rest_concatenated() {
        // Go's prefixdb appends; nothing is hashed, so the key on disk is
        // readable and the same in both languages.
        assert_eq!(key(TX, &[1, 2]), b"tx\x01\x02".to_vec());
        assert_eq!(UTXO, b"utxoutxo", "the nested record prefix, both levels");
    }

    #[test]
    fn a_height_key_orders_the_way_the_number_does() {
        assert!(height_key(9) < height_key(10), "big-endian, not decimal");
        assert_eq!(height_of(&height_key(1 << 40)), Some(1 << 40));
        assert_eq!(height_of(b"blockID"), None, "eight bytes or nothing");
        assert_eq!(height_of(b"tx\x00"), None);
    }

    #[test]
    fn a_batch_round_trips_including_the_difference_between_empty_and_absent() {
        let mut b = Batch::new();
        b.put(b"a".to_vec(), b"one".to_vec());
        b.put(b"b".to_vec(), Vec::new());
        b.delete(b"c".to_vec());
        let back = Batch::decode(&b.encode()).expect("a batch decodes");
        assert_eq!(back.entries(), b.entries());
        assert_eq!(back.entries()[1].1, Some(Vec::new()), "empty is a value");
        assert_eq!(back.entries()[2].1, None, "absent is a deletion");
    }

    #[test]
    fn a_truncated_batch_does_not_decode_into_something_shorter() {
        let mut b = Batch::new();
        b.put(b"key".to_vec(), b"value".to_vec());
        let raw = b.encode();
        for cut in 0..raw.len() {
            assert!(Batch::decode(&raw[..cut]).is_none(), "cut at {cut}");
        }
        // Nor into something longer: a trailing byte is not a second entry.
        let mut over = raw.clone();
        over.push(0);
        assert!(Batch::decode(&over).is_none());
    }

    #[test]
    fn what_was_written_reads_back_in_key_order() {
        for db in [
            Box::new(Memory::new()) as Box<dyn Db>,
            Box::new(Log::open(temp("order")).unwrap()),
        ] {
            let mut b = Batch::new();
            b.put(key(TX, &[2]), b"two".to_vec());
            b.put(key(TX, &[1]), b"one".to_vec());
            b.put(key(BLOCK, &[1]), b"block".to_vec());
            db.write(&b).unwrap();

            assert_eq!(db.get(&key(TX, &[1])), Some(b"one".to_vec()));
            assert_eq!(db.get(&key(TX, &[9])), None);
            let under: Vec<Vec<u8>> = db.range(TX).into_iter().map(|(k, _)| k).collect();
            assert_eq!(
                under,
                vec![key(TX, &[1]), key(TX, &[2])],
                "ascending, and only this prefix"
            );
            assert_eq!(db.len(), 3);
        }
    }

    #[test]
    fn a_later_delete_in_one_batch_beats_an_earlier_put() {
        let db = Memory::new();
        let mut b = Batch::new();
        b.put(b"k".to_vec(), b"v".to_vec());
        b.delete(b"k".to_vec());
        db.write(&b).unwrap();
        assert_eq!(db.get(b"k"), None);
    }

    #[test]
    fn a_log_reopens_as_what_was_written_to_it() {
        let path = temp("reopen");
        {
            let db = Log::open(&path).unwrap();
            let mut b = Batch::new();
            b.put(b"a".to_vec(), b"1".to_vec());
            db.write(&b).unwrap();
            let mut b = Batch::new();
            b.put(b"b".to_vec(), b"2".to_vec());
            b.delete(b"a".to_vec());
            db.write(&b).unwrap();
        }
        let again = Log::open(&path).unwrap();
        assert_eq!(again.get(b"a"), None, "the deletion survived too");
        assert_eq!(again.get(b"b"), Some(b"2".to_vec()));
        assert_eq!(again.len(), 1);
        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn a_torn_tail_is_dropped_and_the_file_is_cut_back_to_it() {
        let path = temp("torn");
        {
            let db = Log::open(&path).unwrap();
            let mut b = Batch::new();
            b.put(b"whole".to_vec(), b"1".to_vec());
            db.write(&b).unwrap();
        }
        let good = std::fs::metadata(&path).unwrap().len();

        // A second batch that stopped halfway through being appended: the
        // caller was never told it succeeded, so nothing above ever saw it.
        let mut b = Batch::new();
        b.put(b"torn".to_vec(), b"2".to_vec());
        let half = frame(&b);
        let mut f = OpenOptions::new().append(true).open(&path).unwrap();
        f.write_all(&half[..half.len() - 3]).unwrap();
        drop(f);
        assert!(std::fs::metadata(&path).unwrap().len() > good);

        let again = Log::open(&path).unwrap();
        assert_eq!(again.get(b"whole"), Some(b"1".to_vec()));
        assert_eq!(again.get(b"torn"), None);
        assert_eq!(
            std::fs::metadata(&path).unwrap().len(),
            good,
            "cut back, so the next append lands on a boundary"
        );

        // And the next write lands correctly on that boundary.
        let mut b = Batch::new();
        b.put(b"after".to_vec(), b"3".to_vec());
        again.write(&b).unwrap();
        drop(again);
        let third = Log::open(&path).unwrap();
        assert_eq!(third.get(b"after"), Some(b"3".to_vec()));
        assert_eq!(third.get(b"torn"), None);
        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn a_frame_whose_bytes_were_changed_is_not_replayed() {
        let path = temp("bitrot");
        {
            let db = Log::open(&path).unwrap();
            let mut b = Batch::new();
            b.put(b"a".to_vec(), b"1".to_vec());
            db.write(&b).unwrap();
            let mut b = Batch::new();
            b.put(b"b".to_vec(), b"2".to_vec());
            db.write(&b).unwrap();
        }
        // Flip a bit inside the SECOND frame's payload. Its digest no longer
        // checks out, so replay stops there — the first frame still stands.
        let mut raw = std::fs::read(&path).unwrap();
        let last = raw.len() - DIGEST - 1;
        raw[last] ^= 0x01;
        std::fs::write(&path, &raw).unwrap();

        let again = Log::open(&path).unwrap();
        assert_eq!(again.get(b"a"), Some(b"1".to_vec()));
        assert_eq!(again.get(b"b"), None);
        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn compacting_keeps_what_is_live_and_makes_the_file_smaller() {
        let path = temp("compact");
        let db = Log::open(&path).unwrap();
        for n in 0..64u8 {
            let mut b = Batch::new();
            b.put(b"one-key".to_vec(), vec![n; 512]);
            db.write(&b).unwrap();
        }
        let before = db.size().unwrap();
        db.compact().unwrap();
        let after = db.size().unwrap();
        assert!(after < before, "{after} is not smaller than {before}");
        assert_eq!(db.get(b"one-key"), Some(vec![63u8; 512]));

        // Still a log: it takes another write, and reopens as both.
        let mut b = Batch::new();
        b.put(b"later".to_vec(), b"x".to_vec());
        db.write(&b).unwrap();
        drop(db);
        let again = Log::open(&path).unwrap();
        assert_eq!(again.get(b"one-key"), Some(vec![63u8; 512]));
        assert_eq!(again.get(b"later"), Some(b"x".to_vec()));
        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn an_empty_batch_writes_nothing_at_all() {
        let path = temp("empty");
        let db = Log::open(&path).unwrap();
        db.write(&Batch::new()).unwrap();
        assert_eq!(db.size().unwrap(), 0, "not even a frame header");
        std::fs::remove_file(&path).ok();
    }
}
