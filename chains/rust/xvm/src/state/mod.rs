// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the chain knows: the unspent outputs, the transactions that made them,
//! the blocks, and where the chain has got to.
//!
//! Two shapes implement the same thing. [`Store`] is what has been committed.
//! [`Diff`] is what a block WOULD do, layered over a parent — a block is
//! executed against a diff, and the diff is applied only once the block is
//! accepted, so a block that loses leaves nothing behind.
//!
//! The one method that is not a plain lookup is [`Chain::utxos`]: it hands back
//! the occupied set in ascending id order, which is the order the state root
//! folds. Both shapes answer it by the same merge — a base stream against an
//! overlay of adds and removals — so a diff over a diff over the store all
//! resolve to one ordered stream.

pub mod root;

use std::collections::{BTreeMap, HashMap};
use std::sync::{Arc, Mutex};

use crate::block::Block;
use crate::db::{self, Batch, Db};
use crate::error::{Error, Result};
use crate::ids::{self, Id, EMPTY};
use crate::txs::Tx;
use crate::utxo::Utxo;

/// A chain layer, shared. Layers form a stack — a diff over a diff over the
/// store — and each holds the one below it.
pub type ChainRef = Arc<Mutex<dyn Chain>>;

/// What can be read from a chain layer.
pub trait ReadOnlyChain {
    fn get_utxo(&self, utxo_id: &Id) -> Result<Utxo>;

    /// The occupied UTXO set in ascending id order, beginning strictly after
    /// `start` and bounded by `limit` (zero means no bound).
    ///
    /// This is the deterministic full-set ordering the state root commits to.
    /// A spent output is never returned — it is not part of the occupied set.
    fn utxos(&self, start: &Id, limit: usize) -> Result<Vec<Utxo>>;

    fn get_tx(&self, tx_id: &Id) -> Result<Tx>;
    fn get_block_id_at_height(&self, height: u64) -> Result<Id>;
    fn get_block(&self, blk_id: &Id) -> Result<Block>;
    fn get_last_accepted(&self) -> Id;
    /// Seconds since the epoch.
    fn get_timestamp(&self) -> u64;
}

/// What can be written to one.
pub trait Chain: ReadOnlyChain + Send {
    fn add_utxo(&mut self, utxo: Utxo);
    fn delete_utxo(&mut self, utxo_id: &Id);
    fn add_tx(&mut self, tx: Tx);
    fn add_block(&mut self, block: Block);
    fn set_last_accepted(&mut self, blk_id: Id);
    fn set_timestamp(&mut self, t: u64);

    /// Put what has changed where it belongs.
    ///
    /// A [`Store`] with a device writes its pending batch to it. A [`Diff`] has
    /// no device — its changes reach the layer below when it is applied — so
    /// there is nothing for it to do, which is why the default is to succeed
    /// rather than to refuse.
    fn commit(&mut self) -> Result<()> {
        Ok(())
    }
}

/// Where a block's state layer can be found, by block id.
pub trait Versions {
    fn get_state(&self, blk_id: &Id) -> Option<ChainRef>;
}

/// The committed state.
///
/// Backed by ordered maps: the UTXO map is ordered because the enumeration
/// order is part of the consensus rule, not a convenience.
///
/// ## What is written down
///
/// A store may be given a [`crate::db::Db`], and then every change it takes is
/// also recorded into a pending batch. Nothing reaches the device until
/// [`Store::commit`], which is called when a block is accepted — so what is on
/// disk is exactly what was decided, and a block that was verified and then
/// lost leaves no trace. That is Go's arrangement: `versiondb` buffers, the
/// accept path calls `Commit`, and the two shapes above this — store and
/// diff — are the same two Go has.
///
/// A store without a `Db` is the same store with nowhere to write. Its
/// `commit` succeeds having done nothing, which is what a node running on a
/// memory database does, rather than a silent failure or a second code path.
#[derive(Default)]
pub struct Store {
    utxos: BTreeMap<Id, Utxo>,
    txs: HashMap<Id, Tx>,
    block_ids: HashMap<u64, Id>,
    blocks: HashMap<Id, Block>,
    last_accepted: Id,
    timestamp: u64,
    /// Where this store is written down, if anywhere.
    db: Option<Arc<dyn Db>>,
    /// What has changed since the last [`Store::commit`].
    pending: Batch,
}

impl Store {
    pub fn new() -> Store {
        Store::default()
    }

    /// A store backed by `db`, holding whatever `db` already holds.
    ///
    /// An empty database gives an empty store, which the caller then fills from
    /// genesis. A database that has been written to gives the chain back as it
    /// was left, and nothing is re-executed: what was accepted was accepted,
    /// and re-deriving it from the blocks would be a second implementation of
    /// the ledger that has to agree with the first.
    pub fn on(db: Arc<dyn Db>) -> Result<Store> {
        let mut s = Store::new();

        for (k, v) in db.range(db::UTXO) {
            let utxo = Utxo::from_wire(&v)?;
            let named = k[db::UTXO.len()..].to_vec();
            // The key is the id, and the id is derived from the value. A stored
            // pair where they disagree is a record this chain did not write.
            if ids::from_slice(&named) != Some(utxo.input_id()) {
                return Err(Error::Storage(format!(
                    "utxo filed under {} is {}",
                    ids::hex(&named),
                    ids::hex(&utxo.input_id())
                )));
            }
            s.utxos.insert(utxo.input_id(), utxo);
        }

        for (k, v) in db.range(db::TX) {
            let tx = Tx::parse(&v)?;
            let named = k[db::TX.len()..].to_vec();
            if ids::from_slice(&named) != Some(tx.id()) {
                return Err(Error::Storage(format!(
                    "tx filed under {} is {}",
                    ids::hex(&named),
                    ids::hex(&tx.id())
                )));
            }
            s.txs.insert(tx.id(), tx);
        }

        for (k, v) in db.range(db::BLOCK) {
            // `blockID` also begins with `block`, so the height index would be
            // read here as well were it not skipped by its own prefix.
            if k.starts_with(db::BLOCK_ID) {
                continue;
            }
            let blk = Block::parse(&v)?;
            let named = k[db::BLOCK.len()..].to_vec();
            if ids::from_slice(&named) != Some(blk.id()) {
                return Err(Error::Storage(format!(
                    "block filed under {} is {}",
                    ids::hex(&named),
                    ids::hex(&blk.id())
                )));
            }
            s.blocks.insert(blk.id(), blk);
        }

        for (k, v) in db.range(db::BLOCK_ID) {
            let height = db::height_of(&k)
                .ok_or_else(|| Error::Storage(format!("not a height key: {}", ids::hex(&k))))?;
            let id = ids::from_slice(&v)
                .ok_or_else(|| Error::Storage(format!("height {height} names no block")))?;
            s.block_ids.insert(height, id);
        }

        if let Some(v) = db.get(&db::key(db::SINGLETON, &[db::LAST_ACCEPTED])) {
            s.last_accepted = ids::from_slice(&v)
                .ok_or_else(|| Error::Storage("last accepted is not an id".into()))?;
        }
        if let Some(v) = db.get(&db::key(db::SINGLETON, &[db::TIMESTAMP])) {
            let eight: [u8; 8] = v
                .as_slice()
                .try_into()
                .map_err(|_| Error::Storage("the timestamp is not eight bytes".into()))?;
            s.timestamp = u64::from_be_bytes(eight);
        }

        s.db = Some(db);
        Ok(s)
    }

    /// Whether this store is written down anywhere.
    pub fn is_persistent(&self) -> bool {
        self.db.is_some()
    }

    /// What is waiting to be written. A way to see the store, not a rule.
    pub fn uncommitted(&self) -> usize {
        self.pending.len()
    }

    /// Record a change, if there is anywhere to record it.
    fn note(&mut self, f: impl FnOnce(&mut Batch)) {
        if self.db.is_some() {
            f(&mut self.pending);
        }
    }

    /// Wrap it as a shareable chain layer.
    pub fn shared(self) -> ChainRef {
        Arc::new(Mutex::new(self))
    }

    /// Whether the chain has been given its first block.
    pub fn is_initialized(&self) -> bool {
        !ids::is_empty(&self.last_accepted)
    }

    /// Install the genesis block's identity and time. The block itself is
    /// recorded by the caller that built it.
    pub fn initialize_chain_state(&mut self, genesis_id: Id, genesis_timestamp: u64) {
        if self.is_initialized() {
            return;
        }
        self.last_accepted = genesis_id;
        self.timestamp = genesis_timestamp;
    }

    /// How many UTXOs are held. Not a consensus value — a way to see the store.
    pub fn utxo_count(&self) -> usize {
        self.utxos.len()
    }

    /// Record a transaction under an id chosen by the caller.
    ///
    /// [`Chain::add_tx`] indexes by the transaction's own id, which is the only
    /// thing the chain ever does. This exists for the tests, which need a store
    /// where an asset with a CHOSEN id exists: an asset's id is the id of the
    /// transaction that created it, so standing that up otherwise would mean
    /// mining for a transaction that hashes to the id the test wants.
    #[cfg(test)]
    pub fn add_tx_at(&mut self, tx_id: Id, tx: Tx) {
        self.txs.insert(tx_id, tx);
    }
}

impl ReadOnlyChain for Store {
    fn get_utxo(&self, utxo_id: &Id) -> Result<Utxo> {
        self.utxos.get(utxo_id).cloned().ok_or(Error::NotFound)
    }

    fn utxos(&self, start: &Id, limit: usize) -> Result<Vec<Utxo>> {
        let mut out = Vec::new();
        for (id, u) in self.utxos.iter() {
            if id <= start {
                continue;
            }
            out.push(u.clone());
            if limit > 0 && out.len() >= limit {
                break;
            }
        }
        Ok(out)
    }

    fn get_tx(&self, tx_id: &Id) -> Result<Tx> {
        self.txs.get(tx_id).cloned().ok_or(Error::NotFound)
    }

    fn get_block_id_at_height(&self, height: u64) -> Result<Id> {
        self.block_ids.get(&height).copied().ok_or(Error::NotFound)
    }

    fn get_block(&self, blk_id: &Id) -> Result<Block> {
        self.blocks.get(blk_id).cloned().ok_or(Error::NotFound)
    }

    fn get_last_accepted(&self) -> Id {
        self.last_accepted
    }

    fn get_timestamp(&self) -> u64 {
        self.timestamp
    }
}

impl Chain for Store {
    fn add_utxo(&mut self, utxo: Utxo) {
        let id = utxo.input_id();
        let raw = utxo.wire_bytes();
        self.note(|b| b.put(db::key(db::UTXO, &id), raw));
        self.utxos.insert(id, utxo);
    }

    fn delete_utxo(&mut self, utxo_id: &Id) {
        let id = *utxo_id;
        self.note(|b| b.delete(db::key(db::UTXO, &id)));
        self.utxos.remove(utxo_id);
    }

    fn add_tx(&mut self, tx: Tx) {
        let id = tx.id();
        let raw = tx.bytes().to_vec();
        self.note(|b| b.put(db::key(db::TX, &id), raw));
        self.txs.insert(id, tx);
    }

    fn add_block(&mut self, block: Block) {
        let id = block.id();
        let height = block.height();
        let raw = block.bytes().to_vec();
        self.note(|b| {
            b.put(db::key(db::BLOCK, &id), raw);
            b.put(db::height_key(height), id.to_vec());
        });
        self.block_ids.insert(height, id);
        self.blocks.insert(id, block);
    }

    fn set_last_accepted(&mut self, blk_id: Id) {
        // The initialised marker is written with the first block the chain
        // accepts, which is what makes an empty database and a chain at genesis
        // two different things on disk.
        self.note(|b| {
            b.put(db::key(db::SINGLETON, &[db::IS_INITIALIZED]), vec![1]);
            b.put(
                db::key(db::SINGLETON, &[db::LAST_ACCEPTED]),
                blk_id.to_vec(),
            );
        });
        self.last_accepted = blk_id;
    }

    fn set_timestamp(&mut self, t: u64) {
        // Seconds since the epoch, big-endian. Go writes a `time.Time`'s own
        // binary encoding here; that is a language's internal representation of
        // a clock reading, and the chain's timestamp is seconds everywhere else
        // it appears — in a block header, on the wire, in the rule that bounds
        // it. One representation, and it is the one the rules are stated in.
        self.note(|b| {
            b.put(
                db::key(db::SINGLETON, &[db::TIMESTAMP]),
                t.to_be_bytes().to_vec(),
            )
        });
        self.timestamp = t;
    }

    /// Put what has changed on the device.
    ///
    /// Called when a block is accepted, and only then. Until it returns, the
    /// changes exist in this process and nowhere else; after it returns they
    /// survive the process. A store with no database has nothing to do and
    /// says so by succeeding.
    fn commit(&mut self) -> Result<()> {
        let Some(db) = self.db.clone() else {
            self.pending = Batch::new();
            return Ok(());
        };
        // The batch is cleared only once the device has it: a write that failed
        // leaves the changes pending, so the next commit tries them again
        // rather than dropping them on the floor.
        db.write(&self.pending)?;
        self.pending = Batch::new();
        Ok(())
    }
}

/// What a block would do to the chain.
///
/// Nothing here has happened. A diff answers every read by its own changes
/// first and the layer below second, so a transaction later in a block sees the
/// effects of the ones before it — which is exactly what makes a block a
/// sequence rather than a set.
pub struct Diff {
    parent: ChainRef,
    /// An entry with `None` is a removal.
    modified_utxos: BTreeMap<Id, Option<Utxo>>,
    added_txs: HashMap<Id, Tx>,
    added_block_ids: HashMap<u64, Id>,
    added_blocks: HashMap<Id, Block>,
    last_accepted: Id,
    timestamp: u64,
}

impl Diff {
    /// A diff over the layer a block id names.
    pub fn new(parent_id: &Id, versions: &dyn Versions) -> Result<Diff> {
        let parent = versions
            .get_state(parent_id)
            .ok_or(Error::MissingParentState)?;
        Ok(Diff::on(parent))
    }

    /// A diff directly over a layer.
    pub fn on(parent: ChainRef) -> Diff {
        let (last_accepted, timestamp) = {
            let p = parent.lock().expect("chain layer poisoned");
            (p.get_last_accepted(), p.get_timestamp())
        };
        Diff {
            parent,
            modified_utxos: BTreeMap::new(),
            added_txs: HashMap::new(),
            added_block_ids: HashMap::new(),
            added_blocks: HashMap::new(),
            last_accepted,
            timestamp,
        }
    }

    pub fn shared(self) -> ChainRef {
        Arc::new(Mutex::new(self))
    }

    /// Push these changes down onto another layer.
    pub fn apply(&self, chain: &mut dyn Chain) {
        for (utxo_id, utxo) in &self.modified_utxos {
            match utxo {
                Some(u) => chain.add_utxo(u.clone()),
                None => chain.delete_utxo(utxo_id),
            }
        }
        for tx in self.added_txs.values() {
            chain.add_tx(tx.clone());
        }
        for blk in self.added_blocks.values() {
            chain.add_block(blk.clone());
        }
        chain.set_last_accepted(self.last_accepted);
        chain.set_timestamp(self.timestamp);
    }
}

/// How much of a parent is drawn at a time when enumerating through it.
const PARENT_PAGE_SIZE: usize = 1024;

impl ReadOnlyChain for Diff {
    fn get_utxo(&self, utxo_id: &Id) -> Result<Utxo> {
        if let Some(entry) = self.modified_utxos.get(utxo_id) {
            return match entry {
                Some(u) => Ok(u.clone()),
                None => Err(Error::NotFound),
            };
        }
        self.parent
            .lock()
            .expect("chain layer poisoned")
            .get_utxo(utxo_id)
    }

    fn utxos(&self, start: &Id, limit: usize) -> Result<Vec<Utxo>> {
        merge_utxo_overlay(
            ParentStream::new(self.parent.clone()),
            &self.modified_utxos,
            start,
            limit,
        )
    }

    fn get_tx(&self, tx_id: &Id) -> Result<Tx> {
        if let Some(tx) = self.added_txs.get(tx_id) {
            return Ok(tx.clone());
        }
        self.parent
            .lock()
            .expect("chain layer poisoned")
            .get_tx(tx_id)
    }

    fn get_block_id_at_height(&self, height: u64) -> Result<Id> {
        if let Some(id) = self.added_block_ids.get(&height) {
            return Ok(*id);
        }
        self.parent
            .lock()
            .expect("chain layer poisoned")
            .get_block_id_at_height(height)
    }

    fn get_block(&self, blk_id: &Id) -> Result<Block> {
        if let Some(b) = self.added_blocks.get(blk_id) {
            return Ok(b.clone());
        }
        self.parent
            .lock()
            .expect("chain layer poisoned")
            .get_block(blk_id)
    }

    fn get_last_accepted(&self) -> Id {
        self.last_accepted
    }

    fn get_timestamp(&self) -> u64 {
        self.timestamp
    }
}

impl Chain for Diff {
    fn add_utxo(&mut self, utxo: Utxo) {
        self.modified_utxos.insert(utxo.input_id(), Some(utxo));
    }

    fn delete_utxo(&mut self, utxo_id: &Id) {
        self.modified_utxos.insert(*utxo_id, None);
    }

    fn add_tx(&mut self, tx: Tx) {
        self.added_txs.insert(tx.id(), tx);
    }

    fn add_block(&mut self, block: Block) {
        self.added_block_ids.insert(block.height(), block.id());
        self.added_blocks.insert(block.id(), block);
    }

    fn set_last_accepted(&mut self, blk_id: Id) {
        self.last_accepted = blk_id;
    }

    fn set_timestamp(&mut self, t: u64) {
        self.timestamp = t;
    }
}

/// A cursor over an ascending-id UTXO source.
trait UtxoStream {
    fn next(&mut self) -> Result<Option<(Id, Utxo)>>;
}

/// Pages a parent layer through its own `utxos` contract, so an arbitrarily
/// large parent streams instead of arriving whole.
struct ParentStream {
    parent: ChainRef,
    page: Vec<Utxo>,
    idx: usize,
    last: Id,
    done: bool,
}

impl ParentStream {
    fn new(parent: ChainRef) -> ParentStream {
        ParentStream {
            parent,
            page: Vec::new(),
            idx: 0,
            last: EMPTY,
            done: false,
        }
    }
}

impl UtxoStream for ParentStream {
    fn next(&mut self) -> Result<Option<(Id, Utxo)>> {
        loop {
            if self.idx < self.page.len() {
                let u = self.page[self.idx].clone();
                self.idx += 1;
                let id = u.input_id();
                self.last = id;
                return Ok(Some((id, u)));
            }
            if self.done {
                return Ok(None);
            }
            let page = self
                .parent
                .lock()
                .expect("chain layer poisoned")
                .utxos(&self.last, PARENT_PAGE_SIZE)?;
            if page.len() < PARENT_PAGE_SIZE {
                // A short page means there is nothing after it.
                self.done = true;
            }
            if page.is_empty() {
                return Ok(None);
            }
            self.page = page;
            self.idx = 0;
        }
    }
}

/// Fold an ascending base stream against an overlay into the occupied set.
///
/// The overlay wins: a base record whose id is also in the overlay is replaced
/// by the overlay's value, or dropped when the overlay says it was removed. An
/// overlay entry with no base record is an insert. Both sides are already
/// ordered, so this is one pass.
fn merge_utxo_overlay(
    mut base: impl UtxoStream,
    overlay: &BTreeMap<Id, Option<Utxo>>,
    start: &Id,
    limit: usize,
) -> Result<Vec<Utxo>> {
    let overlay_ids: Vec<Id> = overlay.keys().filter(|id| *id > start).copied().collect();
    let mut oi = 0usize;
    let mut out: Vec<Utxo> = Vec::new();

    macro_rules! push {
        ($u:expr) => {{
            out.push($u);
            if limit > 0 && out.len() >= limit {
                return Ok(out);
            }
        }};
    }

    let mut cur = base.next()?;
    while let Some((base_id, base_utxo)) = cur {
        if base_id <= *start {
            cur = base.next()?;
            continue;
        }

        // Overlay entries that sort before this record are pure inserts.
        while oi < overlay_ids.len() && overlay_ids[oi] < base_id {
            let id = overlay_ids[oi];
            oi += 1;
            if let Some(Some(u)) = overlay.get(&id) {
                push!(u.clone());
            }
        }

        if oi < overlay_ids.len() && overlay_ids[oi] == base_id {
            let id = overlay_ids[oi];
            oi += 1;
            if let Some(Some(u)) = overlay.get(&id) {
                push!(u.clone());
            }
            cur = base.next()?;
            continue;
        }

        push!(base_utxo);
        cur = base.next()?;
    }

    while oi < overlay_ids.len() {
        let id = overlay_ids[oi];
        oi += 1;
        if let Some(Some(u)) = overlay.get(&id) {
            push!(u.clone());
        }
    }
    Ok(out)
}

/// A [`Versions`] that answers with one layer for every id — what a single
/// standing state looks like to a diff.
pub struct OneVersion(pub ChainRef);

impl Versions for OneVersion {
    fn get_state(&self, _blk_id: &Id) -> Option<ChainRef> {
        Some(self.0.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::TransferOutput;
    use crate::fx::{Owners, State as FxState};
    use crate::ids::ShortId;
    use crate::utxo::{Asset, UtxoId};

    fn utxo(n: u8, idx: u32) -> Utxo {
        Utxo {
            utxo_id: UtxoId::new(ids::prefixed(&[n]), idx),
            asset: Asset {
                id: ids::prefixed(&[1]),
            },
            out: FxState::Transfer(TransferOutput {
                amt: 1 + n as u64,
                owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]),
            }),
        }
    }

    fn ids(us: &[Utxo]) -> Vec<Id> {
        us.iter().map(|u| u.input_id()).collect()
    }

    #[test]
    fn the_store_enumerates_in_ascending_id_order() {
        let mut s = Store::new();
        let us: Vec<Utxo> = (0u8..8).map(|n| utxo(n, 0)).collect();
        for u in &us {
            s.add_utxo(u.clone());
        }
        let got = s.utxos(&EMPTY, 0).unwrap();
        let mut want = ids(&us);
        want.sort();
        assert_eq!(ids(&got), want);
    }

    #[test]
    fn enumeration_is_the_same_every_time() {
        let mut s = Store::new();
        for n in 0u8..16 {
            s.add_utxo(utxo(n, 0));
        }
        assert_eq!(
            ids(&s.utxos(&EMPTY, 0).unwrap()),
            ids(&s.utxos(&EMPTY, 0).unwrap())
        );
    }

    #[test]
    fn an_empty_store_enumerates_to_nothing() {
        assert!(Store::new().utxos(&EMPTY, 0).unwrap().is_empty());
    }

    #[test]
    fn start_is_exclusive_and_limit_bounds_the_page() {
        let mut s = Store::new();
        for n in 0u8..8 {
            s.add_utxo(utxo(n, 0));
        }
        let all = s.utxos(&EMPTY, 0).unwrap();
        assert_eq!(all.len(), 8);

        let first_three = s.utxos(&EMPTY, 3).unwrap();
        assert_eq!(ids(&first_three), ids(&all[..3]));

        // Paging from the last of a page picks up strictly after it.
        let next = s.utxos(&first_three[2].input_id(), 3).unwrap();
        assert_eq!(ids(&next), ids(&all[3..6]));
    }

    #[test]
    fn a_spent_output_is_not_in_the_occupied_set() {
        let mut s = Store::new();
        let u = utxo(1, 0);
        s.add_utxo(u.clone());
        assert_eq!(s.utxos(&EMPTY, 0).unwrap().len(), 1);
        s.delete_utxo(&u.input_id());
        assert!(s.utxos(&EMPTY, 0).unwrap().is_empty());
        assert_eq!(s.get_utxo(&u.input_id()).unwrap_err(), Error::NotFound);
    }

    #[test]
    fn a_diff_overlays_its_parent_without_touching_it() {
        let mut store = Store::new();
        for n in 0u8..4 {
            store.add_utxo(utxo(n, 0));
        }
        let parent = store.shared();
        let mut d = Diff::on(parent.clone());

        let fresh = utxo(9, 0);
        d.add_utxo(fresh.clone());
        let removed = utxo(0, 0);
        d.delete_utxo(&removed.input_id());

        let seen = ids(&d.utxos(&EMPTY, 0).unwrap());
        assert!(seen.contains(&fresh.input_id()));
        assert!(!seen.contains(&removed.input_id()));
        assert_eq!(seen.len(), 4);

        // The parent has not moved.
        let parent_seen = ids(&parent.lock().unwrap().utxos(&EMPTY, 0).unwrap());
        assert_eq!(parent_seen.len(), 4);
        assert!(parent_seen.contains(&removed.input_id()));
    }

    #[test]
    fn a_diffs_replacement_shadows_the_parents_record() {
        let mut store = Store::new();
        let original = utxo(1, 0);
        store.add_utxo(original.clone());
        let parent = store.shared();

        let mut replacement = original.clone();
        replacement.out = FxState::Transfer(TransferOutput {
            amt: 999,
            owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]),
        });

        let mut d = Diff::on(parent);
        d.add_utxo(replacement.clone());
        let got = d.utxos(&EMPTY, 0).unwrap();
        assert_eq!(got.len(), 1);
        assert_eq!(got[0], replacement);
        assert_eq!(d.get_utxo(&original.input_id()).unwrap(), replacement);
    }

    #[test]
    fn a_removal_of_something_the_parent_never_had_emits_nothing() {
        let store = Store::new();
        let mut d = Diff::on(store.shared());
        d.delete_utxo(&utxo(1, 0).input_id());
        assert!(d.utxos(&EMPTY, 0).unwrap().is_empty());
    }

    #[test]
    fn a_diff_over_a_diff_still_enumerates_in_one_order() {
        let mut store = Store::new();
        for n in 0u8..4 {
            store.add_utxo(utxo(n, 0));
        }
        let base = store.shared();
        let mut mid = Diff::on(base);
        mid.add_utxo(utxo(10, 0));
        mid.delete_utxo(&utxo(1, 0).input_id());
        let mid_ref = mid.shared();

        let mut top = Diff::on(mid_ref);
        top.add_utxo(utxo(11, 0));
        top.delete_utxo(&utxo(2, 0).input_id());

        let got = ids(&top.utxos(&EMPTY, 0).unwrap());
        let mut want = got.clone();
        want.sort();
        assert_eq!(got, want, "the merged stream is ascending");
        assert_eq!(got.len(), 4);
        assert!(got.contains(&utxo(10, 0).input_id()));
        assert!(got.contains(&utxo(11, 0).input_id()));
        assert!(!got.contains(&utxo(1, 0).input_id()));
        assert!(!got.contains(&utxo(2, 0).input_id()));
    }

    #[test]
    fn a_diff_pages_a_parent_larger_than_one_page() {
        let mut store = Store::new();
        // Two pages and a bit, so the refill path is exercised.
        for n in 0u32..(PARENT_PAGE_SIZE as u32 * 2 + 5) {
            let mut u = utxo(1, n);
            u.utxo_id.output_index = n;
            store.add_utxo(u);
        }
        let want = store.utxo_count();
        let d = Diff::on(store.shared());
        let got = d.utxos(&EMPTY, 0).unwrap();
        assert_eq!(got.len(), want);
        let seen = ids(&got);
        let mut sorted = seen.clone();
        sorted.sort();
        assert_eq!(seen, sorted);
    }

    #[test]
    fn applying_a_diff_moves_exactly_its_changes_down() {
        let mut store = Store::new();
        store.add_utxo(utxo(1, 0));
        store.add_utxo(utxo(2, 0));

        let mut d = Diff::on(Store::new().shared());
        d.add_utxo(utxo(3, 0));
        d.delete_utxo(&utxo(1, 0).input_id());
        d.set_timestamp(1234);
        d.set_last_accepted(ids::prefixed(&[7]));

        d.apply(&mut store);
        assert!(store.get_utxo(&utxo(3, 0).input_id()).is_ok());
        assert_eq!(
            store.get_utxo(&utxo(1, 0).input_id()).unwrap_err(),
            Error::NotFound
        );
        assert!(store.get_utxo(&utxo(2, 0).input_id()).is_ok());
        assert_eq!(store.get_timestamp(), 1234);
        assert_eq!(store.get_last_accepted(), ids::prefixed(&[7]));
    }

    #[test]
    fn a_diff_starts_where_its_parent_stands() {
        let mut store = Store::new();
        store.set_timestamp(500);
        store.set_last_accepted(ids::prefixed(&[3]));
        let d = Diff::on(store.shared());
        assert_eq!(d.get_timestamp(), 500);
        assert_eq!(d.get_last_accepted(), ids::prefixed(&[3]));
    }

    #[test]
    fn a_diff_over_a_parent_that_is_not_there_is_refused() {
        struct NoVersions;
        impl Versions for NoVersions {
            fn get_state(&self, _: &Id) -> Option<ChainRef> {
                None
            }
        }
        assert!(matches!(
            Diff::new(&EMPTY, &NoVersions),
            Err(Error::MissingParentState)
        ));
    }

    #[test]
    fn transactions_and_blocks_are_read_back_from_whichever_layer_has_them() {
        let store = Store::new();
        let parent = store.shared();
        let mut d = Diff::on(parent.clone());
        assert_eq!(d.get_tx(&ids::prefixed(&[1])).unwrap_err(), Error::NotFound);
        assert_eq!(
            d.get_block(&ids::prefixed(&[1])).unwrap_err(),
            Error::NotFound
        );
        assert_eq!(d.get_block_id_at_height(0).unwrap_err(), Error::NotFound);
        d.set_timestamp(9);
        assert_eq!(d.get_timestamp(), 9);
        // The parent is untouched by the diff's timestamp.
        assert_eq!(parent.lock().unwrap().get_timestamp(), 0);
    }
}
