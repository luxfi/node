// SPDX-License-Identifier: BSD-3-Clause-Eco

//! Somewhere to put bytes that are still there after a restart.
//!
//! A chain that only remembers what is in memory cannot restart as a node: it
//! comes back knowing nothing, and a node that knows nothing has to be *told*
//! what it decided by the peers whose claims it exists to check. So the
//! accepted state is written down.
//!
//! This is deliberately not a port of the reference's on-disk layout. That
//! layout — its key encodings, its prefix databases, its batched commits — is
//! about two thousand lines and none of it is consensus: what a block commits
//! to is the root its execution produced, and two nodes that agree on every
//! root agree completely no matter what shape either wrote its own copy in. So
//! the obligation here is narrower and total: everything the accepted state
//! holds goes in, comes back identical, and a machine that loses power part way
//! through a write comes back at the last height it finished rather than half
//! way into the next one.
//!
//! The interface is a byte map with one property that matters: **a batch is all
//! there or none of it is**. That is what makes a height atomic — a block's
//! whole effect is one commit, so a chain never comes back having applied part
//! of a block.

use std::collections::BTreeMap;
use std::fs::{File as OsFile, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};

/// Why a store could not do what was asked.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// The underlying file said no. Carried as text because the chain cannot
    /// do anything with an `io::Error` except report it.
    Io(String),
    /// A record whose bytes are not what its kind says they are.
    Corrupt(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Io(why) => write!(f, "store: {why}"),
            Error::Corrupt(why) => write!(f, "store: corrupt: {why}"),
        }
    }
}

impl std::error::Error for Error {}

impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Error {
        Error::Io(e.to_string())
    }
}

/// A byte map whose writes become durable together or not at all.
///
/// Writes are buffered until [`Store::commit`], and reads see the buffer over
/// what has been committed — a block being applied reads its own writes back,
/// which is what lets one commit hold a whole height.
pub trait Store: Send {
    fn put(&mut self, key: &[u8], value: &[u8]);
    fn delete(&mut self, key: &[u8]);
    fn get(&self, key: &[u8]) -> Option<Vec<u8>>;
    /// Everything under `prefix`, in ascending key order.
    fn scan(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)>;
    /// Everything written since the last commit becomes durable together. A
    /// store that cannot promise that refuses rather than pretending.
    fn commit(&mut self) -> Result<(), Error>;
}

/// What both stores hold: what is live, and what is waiting to become live.
#[derive(Debug, Default)]
struct Pages {
    live: BTreeMap<Vec<u8>, Vec<u8>>,
    /// `None` means deleted. A pending entry shadows the live one.
    pending: BTreeMap<Vec<u8>, Option<Vec<u8>>>,
}

impl Pages {
    fn get(&self, key: &[u8]) -> Option<Vec<u8>> {
        match self.pending.get(key) {
            Some(pending) => pending.clone(),
            None => self.live.get(key).cloned(),
        }
    }

    fn scan(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        let mut merged: BTreeMap<&[u8], Option<&Vec<u8>>> = BTreeMap::new();
        for (k, v) in &self.live {
            if k.starts_with(prefix) {
                merged.insert(k, Some(v));
            }
        }
        for (k, v) in &self.pending {
            if k.starts_with(prefix) {
                merged.insert(k, v.as_ref());
            }
        }
        merged
            .into_iter()
            .filter_map(|(k, v)| v.map(|v| (k.to_vec(), v.clone())))
            .collect()
    }

    /// The pending writes, drained, in key order — the order they are written
    /// to the log in, so a replay produces the same map.
    fn drain(&mut self) -> Vec<Row> {
        std::mem::take(&mut self.pending).into_iter().collect()
    }

    fn apply(&mut self, batch: &[Row]) {
        for (k, v) in batch {
            match v {
                Some(v) => {
                    self.live.insert(k.clone(), v.clone());
                }
                None => {
                    self.live.remove(k);
                }
            }
        }
    }
}

/// A store in memory.
///
/// It has no durability to offer and does not claim any; it exists so the
/// write-through state can be exercised without a filesystem, and so a chain
/// that has not been given anywhere to write still runs.
#[derive(Debug, Default)]
pub struct Memory {
    pages: Pages,
}

impl Memory {
    pub fn new() -> Memory {
        Memory::default()
    }

    pub fn len(&self) -> usize {
        self.pages.live.len()
    }

    pub fn is_empty(&self) -> bool {
        self.pages.live.is_empty()
    }
}

impl Store for Memory {
    fn put(&mut self, key: &[u8], value: &[u8]) {
        self.pages.pending.insert(key.to_vec(), Some(value.to_vec()));
    }

    fn delete(&mut self, key: &[u8]) {
        self.pages.pending.insert(key.to_vec(), None);
    }

    fn get(&self, key: &[u8]) -> Option<Vec<u8>> {
        self.pages.get(key)
    }

    fn scan(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        self.pages.scan(prefix)
    }

    fn commit(&mut self) -> Result<(), Error> {
        let batch = self.pages.drain();
        self.pages.apply(&batch);
        Ok(())
    }
}

/// One row of a batch: a key, and either the bytes it takes or nothing, which
/// means it was deleted.
type Row = (Vec<u8>, Option<Vec<u8>>);

/// What a batch's closing record begins with, so a partial write cannot be
/// mistaken for a whole one just because its length happened to line up.
const SEAL: &[u8; 4] = b"ZBAT";

/// A store on disk: one append-only file of batches, each ending in a record
/// that says how many bytes preceded it and what they hash to.
///
/// A batch is applied on the way back in only when its closing record is there
/// **and** its checksum matches, so a machine that lost power part way through
/// a write comes back without that batch and with every batch before it. That
/// is the whole crash story: the last height either finished or did not happen.
///
/// The file is rewritten on open, holding exactly what is live. A log that only
/// ever grew would be a chain that gets slower to start the longer it has run.
#[derive(Debug)]
pub struct File {
    path: PathBuf,
    file: OsFile,
    pages: Pages,
}

impl File {
    /// Opens `path`, replaying what is there. A file that does not exist is an
    /// empty store, which is how a chain starts.
    pub fn open(path: impl AsRef<Path>) -> Result<File, Error> {
        let path = path.as_ref().to_path_buf();
        let mut raw = Vec::new();
        if path.exists() {
            OsFile::open(&path)?.read_to_end(&mut raw)?;
        }
        let mut pages = Pages::default();
        for batch in Self::replay(&raw) {
            pages.apply(&batch);
        }
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(true)
            .open(&path)?;
        let mut store = File { path, file, pages };
        store.compact()?;
        Ok(store)
    }

    /// Every batch in the log whose seal is present and whose checksum agrees.
    /// The first one that does not ends the replay: everything after a torn
    /// write is unreachable, because a batch's meaning depends on the ones
    /// before it.
    fn replay(raw: &[u8]) -> Vec<Vec<Row>> {
        let mut out = Vec::new();
        let mut at = 0usize;
        loop {
            let start = at;
            let mut batch = Vec::new();
            let sealed = loop {
                // Each record: kind(1) ‖ klen(4) ‖ key ‖ [vlen(4) ‖ value].
                let Some(&kind) = raw.get(at) else {
                    break false;
                };
                at += 1;
                if kind == SEAL[0] {
                    // A closing record: the rest of the seal, the length of the
                    // batch's body, and its digest. The body is everything
                    // between the batch's start and this record.
                    let body = at - 1 - start;
                    if raw.len() < at + 3 + 8 + 32 || &raw[at - 1..at + 3] != SEAL {
                        break false;
                    }
                    at += 3;
                    let len = u64::from_le_bytes(raw[at..at + 8].try_into().unwrap()) as usize;
                    at += 8;
                    let digest: [u8; 32] = raw[at..at + 32].try_into().unwrap();
                    at += 32;
                    if len != body {
                        break false;
                    }
                    break crate::ids::hash256(&raw[start..start + len]) == digest;
                }
                if kind != 0 && kind != 1 {
                    break false;
                }
                if raw.len() < at + 4 {
                    break false;
                }
                let klen = u32::from_le_bytes(raw[at..at + 4].try_into().unwrap()) as usize;
                at += 4;
                if raw.len() < at + klen {
                    break false;
                }
                let key = raw[at..at + klen].to_vec();
                at += klen;
                if kind == 0 {
                    batch.push((key, None));
                    continue;
                }
                if raw.len() < at + 4 {
                    break false;
                }
                let vlen = u32::from_le_bytes(raw[at..at + 4].try_into().unwrap()) as usize;
                at += 4;
                if raw.len() < at + vlen {
                    break false;
                }
                batch.push((key, Some(raw[at..at + vlen].to_vec())));
                at += vlen;
            };
            if !sealed {
                return out;
            }
            out.push(batch);
            if at >= raw.len() {
                return out;
            }
        }
    }

    fn encode(batch: &[Row]) -> Vec<u8> {
        let mut body = Vec::new();
        for (key, value) in batch {
            match value {
                None => body.push(0u8),
                Some(_) => body.push(1u8),
            }
            body.extend_from_slice(&(key.len() as u32).to_le_bytes());
            body.extend_from_slice(key);
            if let Some(v) = value {
                body.extend_from_slice(&(v.len() as u32).to_le_bytes());
                body.extend_from_slice(v);
            }
        }
        let digest = crate::ids::hash256(&body);
        let mut out = body;
        let len = out.len() as u64;
        out.extend_from_slice(SEAL);
        out.extend_from_slice(&len.to_le_bytes());
        out.extend_from_slice(&digest);
        out
    }

    /// One batch holding exactly what is live, and nothing that has since been
    /// overwritten.
    fn compact(&mut self) -> Result<(), Error> {
        let batch: Vec<Row> = self
            .pages
            .live
            .iter()
            .map(|(k, v)| (k.clone(), Some(v.clone())))
            .collect();
        self.file.set_len(0)?;
        self.file.seek(SeekFrom::Start(0))?;
        if !batch.is_empty() {
            self.file.write_all(&Self::encode(&batch))?;
        }
        self.file.sync_data()?;
        Ok(())
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn len(&self) -> usize {
        self.pages.live.len()
    }

    pub fn is_empty(&self) -> bool {
        self.pages.live.is_empty()
    }
}

impl Store for File {
    fn put(&mut self, key: &[u8], value: &[u8]) {
        self.pages.pending.insert(key.to_vec(), Some(value.to_vec()));
    }

    fn delete(&mut self, key: &[u8]) {
        self.pages.pending.insert(key.to_vec(), None);
    }

    fn get(&self, key: &[u8]) -> Option<Vec<u8>> {
        self.pages.get(key)
    }

    fn scan(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        self.pages.scan(prefix)
    }

    fn commit(&mut self) -> Result<(), Error> {
        let batch = self.pages.drain();
        if batch.is_empty() {
            return Ok(());
        }
        // Appended and flushed BEFORE it is visible in memory: a store that
        // said yes and then failed to write would have the chain believing a
        // height it cannot come back to.
        self.file.seek(SeekFrom::End(0))?;
        self.file.write_all(&Self::encode(&batch))?;
        self.file.sync_data()?;
        self.pages.apply(&batch);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tmp(name: &str) -> PathBuf {
        let mut p = std::env::temp_dir();
        p.push(format!(
            "lux-pvm-store-{}-{}-{:?}",
            name,
            std::process::id(),
            std::thread::current().id()
        ));
        let _ = std::fs::remove_file(&p);
        p
    }

    fn exercise(store: &mut dyn Store) {
        store.put(b"a", b"1");
        store.put(b"b", b"2");
        // Uncommitted writes read back — a block applying reads its own.
        assert_eq!(store.get(b"a"), Some(b"1".to_vec()));
        store.commit().unwrap();

        store.delete(b"a");
        assert_eq!(store.get(b"a"), None, "a pending delete shadows the live row");
        assert_eq!(store.scan(b"").len(), 1);
        store.commit().unwrap();
        assert_eq!(store.get(b"a"), None);
        assert_eq!(store.get(b"b"), Some(b"2".to_vec()));
    }

    #[test]
    fn a_memory_store_holds_what_it_is_given() {
        exercise(&mut Memory::new());
    }

    #[test]
    fn a_file_store_holds_what_it_is_given() {
        let path = tmp("basic");
        exercise(&mut File::open(&path).unwrap());
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn a_scan_is_in_key_order_and_bounded_by_its_prefix() {
        let mut s = Memory::new();
        s.put(b"p/2", b"b");
        s.put(b"p/1", b"a");
        s.put(b"q/1", b"c");
        s.commit().unwrap();
        let got: Vec<Vec<u8>> = s.scan(b"p/").into_iter().map(|(k, _)| k).collect();
        assert_eq!(got, vec![b"p/1".to_vec(), b"p/2".to_vec()]);
    }

    #[test]
    fn what_was_committed_is_there_after_a_reopen() {
        let path = tmp("reopen");
        {
            let mut s = File::open(&path).unwrap();
            s.put(b"height", b"7");
            s.put(b"gone", b"x");
            s.commit().unwrap();
            s.delete(b"gone");
            s.commit().unwrap();
        }
        let s = File::open(&path).unwrap();
        assert_eq!(s.get(b"height"), Some(b"7".to_vec()));
        assert_eq!(s.get(b"gone"), None);
        assert_eq!(s.len(), 1);
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn what_was_never_committed_is_not_there() {
        let path = tmp("uncommitted");
        {
            let mut s = File::open(&path).unwrap();
            s.put(b"kept", b"1");
            s.commit().unwrap();
            s.put(b"dropped", b"2");
            // No commit. The height did not happen.
        }
        let s = File::open(&path).unwrap();
        assert_eq!(s.get(b"kept"), Some(b"1".to_vec()));
        assert_eq!(s.get(b"dropped"), None);
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn a_torn_write_comes_back_at_the_last_height_that_finished() {
        // The crash story, made to happen: a batch is appended and the machine
        // dies part way through it. Every prefix of that trailing batch must
        // read as absent, and everything before it must be intact.
        let path = tmp("torn");
        {
            let mut s = File::open(&path).unwrap();
            s.put(b"one", b"1");
            s.commit().unwrap();
            s.put(b"two", b"2");
            s.commit().unwrap();
        }
        let whole = std::fs::read(&path).unwrap();
        // Where the second batch begins: rewrite the file with the first batch
        // whole and any prefix of the second.
        let mut first = File::open(&path).unwrap();
        first.pages.live.remove(b"two".as_slice());
        first.compact().unwrap();
        let head = std::fs::read(&path).unwrap().len();
        drop(first);

        for cut in head..whole.len() {
            std::fs::write(&path, &whole[..cut]).unwrap();
            let s = File::open(&path).unwrap();
            assert_eq!(
                s.get(b"one"),
                Some(b"1".to_vec()),
                "the height that finished survives a tear at {cut}"
            );
            assert_eq!(
                s.get(b"two"),
                None,
                "the height that did not finish is absent at {cut}"
            );
        }
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn a_batch_whose_bytes_were_altered_is_refused() {
        // Not corruption-proofing for its own sake: a half-written record whose
        // lengths happen to line up would otherwise replay as a DIFFERENT
        // height, which is worse than losing it.
        let path = tmp("altered");
        {
            let mut s = File::open(&path).unwrap();
            s.put(b"k", b"value");
            s.commit().unwrap();
        }
        let mut raw = std::fs::read(&path).unwrap();
        let last = raw.len() - 40;
        raw[last] ^= 0xff;
        std::fs::write(&path, &raw).unwrap();
        let s = File::open(&path).unwrap();
        assert!(s.is_empty());
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn the_log_does_not_grow_without_bound() {
        // A store that only ever appended would take longer to open the longer
        // it had run. Reopening rewrites it as exactly what is live.
        let path = tmp("compact");
        {
            let mut s = File::open(&path).unwrap();
            for i in 0..200u32 {
                s.put(b"same", &i.to_le_bytes());
                s.commit().unwrap();
            }
        }
        let grown = std::fs::metadata(&path).unwrap().len();
        let s = File::open(&path).unwrap();
        assert_eq!(s.get(b"same"), Some(199u32.to_le_bytes().to_vec()));
        drop(s);
        let compacted = std::fs::metadata(&path).unwrap().len();
        assert!(compacted < grown, "{compacted} < {grown}");
        let _ = std::fs::remove_file(&path);
    }
}
