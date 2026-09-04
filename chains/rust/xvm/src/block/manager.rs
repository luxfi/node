// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The state machine over blocks: what has been committed, what is being
//! decided, and what each of those would do.
//!
//! A verified block is not applied. It gets a [`Diff`] — everything it WOULD
//! change — and that diff is remembered under the block's id. Children verify
//! on top of their parent's diff, so a fork is several stacks of diffs over one
//! committed store and none of them can see each other. Acceptance is the only
//! thing that writes: the winning diff is pushed down onto the store and the
//! losers are dropped.
//!
//! The two rules that are easy to miss and expensive to get wrong:
//!
//! - **Imported inputs are unique across a whole ancestry.** Two blocks in one
//!   line may not both claim the same UTXO from another chain, and neither may
//!   two transactions in one block. The other chain's UTXO is not in this
//!   chain's state yet, so nothing else would notice.
//! - **The root is recomputed, never trusted.** The block carries a root; the
//!   verifier derives what the root must be from the state the block actually
//!   produced, through the same function the builder used, and refuses any
//!   difference.

use std::collections::{BTreeSet, HashMap};
use std::sync::{Arc, Mutex};

use crate::block::root::block_execution_root;
use crate::block::Block;
use crate::error::{Error, Result};
use crate::ids::{Id, EMPTY};
use crate::state::{Chain, ChainRef, Diff, ReadOnlyChain, Versions};
use crate::txs::executor::{execute, verify_semantic, verify_syntactic, AtomicRequests, Backend};
use crate::txs::Tx;

/// How far into the future a block's timestamp may be, in seconds.
///
/// A clock that is a little ahead is a fact of a distributed system; a
/// timestamp far ahead is either a broken clock or an attempt to move the chain
/// forward, and both are refused the same way.
pub const SYNC_BOUND: u64 = 10;

/// What a verified, undecided block would do.
struct Processing {
    block: Block,
    on_accept: Arc<Mutex<Diff>>,
    imported_inputs: BTreeSet<Id>,
    atomic_requests: Vec<(Id, AtomicRequests)>,
}

/// The blocks: one committed store, and whatever is still being decided.
pub struct Manager {
    store: ChainRef,
    processing: HashMap<Id, Processing>,
    last_accepted: Id,
    preferred: Id,
}

impl Manager {
    /// A manager over a committed store, starting where that store left off.
    pub fn new(store: ChainRef) -> Manager {
        let last_accepted = store
            .lock()
            .expect("chain layer poisoned")
            .get_last_accepted();
        Manager {
            store,
            processing: HashMap::new(),
            last_accepted,
            preferred: last_accepted,
        }
    }

    pub fn last_accepted(&self) -> Id {
        self.last_accepted
    }

    pub fn preferred(&self) -> Id {
        self.preferred
    }

    pub fn set_preference(&mut self, blk_id: Id) {
        self.preferred = blk_id;
    }

    /// The committed store, for a caller that wants to read what is settled.
    pub fn store(&self) -> ChainRef {
        self.store.clone()
    }

    /// Whether a block is still being decided.
    pub fn is_processing(&self, blk_id: &Id) -> bool {
        self.processing.contains_key(blk_id)
    }

    /// A block, whether it is being decided or already committed.
    pub fn get_block(&self, blk_id: &Id) -> Result<Block> {
        if let Some(p) = self.processing.get(blk_id) {
            return Ok(p.block.clone());
        }
        self.store
            .lock()
            .expect("chain layer poisoned")
            .get_block(blk_id)
    }

    /// The id of the block at a height, from whichever layer holds it.
    pub fn block_id_at_height(&self, height: u64) -> Result<Id> {
        self.store
            .lock()
            .expect("chain layer poisoned")
            .get_block_id_at_height(height)
    }

    /// The state as of a block: its own diff if it is being decided, the
    /// committed store if it is the last accepted, and nothing otherwise.
    ///
    /// "Nothing" is the honest answer for a block that is neither: a block
    /// whose state was already thrown away cannot be built on.
    pub fn state_after(&self, blk_id: &Id) -> Option<ChainRef> {
        if let Some(p) = self.processing.get(blk_id) {
            return Some(p.on_accept.clone());
        }
        if *blk_id == self.last_accepted {
            return Some(self.store.clone());
        }
        None
    }

    /// Verify a block: everything about it, and everything it does.
    ///
    /// Idempotent — a block that has already been verified is already in the
    /// map, and consensus asks more than once.
    pub fn verify(&mut self, backend: &Backend<'_>, blk: &Block) -> Result<()> {
        let blk_id = blk.id();
        if self.processing.contains_key(&blk_id) {
            return Ok(());
        }

        // A timestamp far in the future is refused before anything is read.
        if blk.timestamp() > backend.now.saturating_add(SYNC_BOUND) {
            return Err(Error::TimestampBeyondSyncBound);
        }

        let txs = blk.txs();
        if txs.is_empty() {
            return Err(Error::EmptyBlock);
        }

        // Syntactic verification reads no state and does no cryptography, so
        // it goes first: most nonsense is refused before a database is touched.
        for tx in txs {
            verify_syntactic(backend, tx)?;
        }

        let parent_id = blk.parent();
        let parent = self.get_block(&parent_id)?;

        let expected_height = parent.height() + 1;
        if expected_height != blk.height() {
            return Err(Error::IncorrectHeight(expected_height, blk.height()));
        }

        let parent_state = self
            .state_after(&parent_id)
            .ok_or(Error::MissingParentState)?;
        let mut diff = Diff::on(parent_state);

        // Time moves one way.
        if blk.timestamp() < diff.get_timestamp() {
            return Err(Error::ChildBlockEarlierThanParent);
        }
        diff.set_timestamp(blk.timestamp());

        let mut imported_inputs: BTreeSet<Id> = BTreeSet::new();
        let mut atomic_requests: Vec<(Id, AtomicRequests)> = Vec::new();

        for tx in txs {
            // Semantic verification and execution are interleaved on purpose:
            // a transaction later in the block must see what the ones before it
            // did, which is what makes a block a sequence and not a set.
            verify_semantic(backend, &diff, tx)?;
            let effects = execute(&mut diff, tx)?;

            if effects.inputs.iter().any(|i| imported_inputs.contains(i)) {
                return Err(Error::ConflictingBlockTxs);
            }
            imported_inputs.extend(effects.inputs.iter().copied());

            diff.add_tx(tx.clone());
            merge_atomic(&mut atomic_requests, effects.atomic_requests);
        }

        // Nothing anywhere in this line may have claimed the same foreign UTXO.
        self.verify_unique_inputs(&parent_id, &imported_inputs)?;

        // The root is derived, then compared. Both sides of the comparison come
        // from one function, so there is no second rule to disagree with.
        let expected_root = block_execution_root(parent.merkle_root(), txs, &diff, blk.height())?;
        if blk.merkle_root() != expected_root {
            return Err(Error::UnexpectedMerkleRoot);
        }

        diff.set_last_accepted(blk_id);
        diff.add_block(blk.clone());

        self.processing.insert(
            blk_id,
            Processing {
                block: blk.clone(),
                on_accept: Arc::new(Mutex::new(diff)),
                imported_inputs,
                atomic_requests,
            },
        );
        Ok(())
    }

    /// Commit a verified block, and say what it asked of the other chains.
    ///
    /// The caller applies those requests; this returns them rather than
    /// reaching for a shared area itself, because whether a chain has one is
    /// the host's business.
    pub fn accept(&mut self, blk_id: &Id) -> Result<Vec<(Id, AtomicRequests)>> {
        let p = self.processing.remove(blk_id).ok_or(Error::BlockNotFound)?;
        {
            let diff = p.on_accept.lock().expect("chain layer poisoned");
            let mut store = self.store.lock().expect("chain layer poisoned");
            diff.apply(&mut *store);
            store.add_block(p.block.clone());
        }
        self.last_accepted = *blk_id;
        Ok(p.atomic_requests)
    }

    /// Drop a block that lost, and hand back what it carried.
    ///
    /// The transactions are not gone — they were never applied — so the caller
    /// may re-offer the ones that are still valid.
    pub fn reject(&mut self, blk_id: &Id) -> Result<Vec<Tx>> {
        let p = self.processing.remove(blk_id).ok_or(Error::BlockNotFound)?;
        Ok(p.block.txs().to_vec())
    }

    /// Whether a transaction could be issued against the preferred state.
    ///
    /// This is the mempool's question, not a block's: it runs the same three
    /// passes on a throwaway diff and keeps nothing.
    pub fn verify_tx(&self, backend: &Backend<'_>, tx: &Tx) -> Result<()> {
        if !backend.bootstrapped {
            return Err(Error::ChainNotSynced);
        }
        verify_syntactic(backend, tx)?;
        let parent = self
            .state_after(&self.last_accepted)
            .ok_or(Error::MissingParentState)?;
        let mut diff = Diff::on(parent);
        verify_semantic(backend, &diff, tx)?;
        execute(&mut diff, tx)?;
        Ok(())
    }

    /// No block in the inclusive ancestry of `blk_id` claims any of `inputs`.
    ///
    /// The walk stops at the first ancestor that is not being decided: that
    /// ancestor is accepted, so its imports are already spent state and a
    /// double claim would have been caught reading the UTXO.
    pub fn verify_unique_inputs(&self, blk_id: &Id, inputs: &BTreeSet<Id>) -> Result<()> {
        if inputs.is_empty() {
            return Ok(());
        }
        let mut cur = *blk_id;
        loop {
            let Some(p) = self.processing.get(&cur) else {
                return Ok(());
            };
            if p.imported_inputs.iter().any(|i| inputs.contains(i)) {
                return Err(Error::ConflictingParentTxs);
            }
            cur = p.block.parent();
        }
    }

    /// Install the first block. The chain has to start somewhere, and it is not
    /// a block anybody verified — there is no parent to verify it against.
    pub fn set_genesis(&mut self, genesis: Block) {
        let id = genesis.id();
        let time = genesis.timestamp();
        {
            let mut store = self.store.lock().expect("chain layer poisoned");
            store.add_block(genesis);
            store.set_last_accepted(id);
            store.set_timestamp(time);
        }
        self.last_accepted = id;
        self.preferred = id;
    }
}

impl Versions for Manager {
    fn get_state(&self, blk_id: &Id) -> Option<ChainRef> {
        self.state_after(blk_id)
    }
}

/// Merge one transaction's cross-chain asks into the block's, keyed by chain.
fn merge_atomic(into: &mut Vec<(Id, AtomicRequests)>, from: Vec<(Id, AtomicRequests)>) {
    for (chain_id, reqs) in from {
        match into.iter_mut().find(|(c, _)| *c == chain_id) {
            Some((_, existing)) => {
                existing.puts.extend(reqs.puts);
                existing.removes.extend(reqs.removes);
            }
            None => into.push((chain_id, reqs)),
        }
    }
}

/// The empty root a chain starts from, as an id.
pub fn genesis_parent_root() -> Id {
    EMPTY
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::block::root::block_execution_root;
    use crate::fx::secp256k1::{address_of, TransferInput, TransferOutput};
    use crate::fx::{self, Input, Owners, State};
    use crate::ids::ShortId;
    use crate::state::Store;
    use crate::txs::executor::{Config, Net, SharedMemory};
    use crate::txs::{BaseTx, Unsigned};
    use crate::utxo::{
        Asset, BaseTxFields, Runtime, TransferableInput, TransferableOutput, Utxo, UtxoId,
    };

    const NETWORK_ID: u32 = 10;

    fn chain_id() -> Id {
        Id::prefixed_bytes(&[5])
    }
    fn asset() -> Id {
        Id::prefixed_bytes(&[1])
    }
    fn key(n: u8) -> [u8; 32] {
        let mut k = [0u8; 32];
        k[31] = n;
        k
    }
    fn addr(n: u8) -> ShortId {
        address_of(&key(n)).unwrap()
    }

    struct OneNet(Id);
    impl Net for OneNet {
        fn network_of(&self, _: &Id) -> Result<Id> {
            Ok(self.0)
        }
    }
    struct NoMemory;
    impl SharedMemory for NoMemory {
        fn get(&self, _: &Id, _: &[Vec<u8>]) -> Result<Vec<Vec<u8>>> {
            Ok(Vec::new())
        }
        fn apply(&self, _: &[(Id, AtomicRequests)]) -> Result<()> {
            Ok(())
        }
    }

    fn backend<'a>(net: &'a OneNet, sm: &'a NoMemory, now: u64) -> Backend<'a> {
        Backend {
            runtime: Runtime {
                network_id: NETWORK_ID,
                chain_id: chain_id(),
            },
            net_id: Id::prefixed_bytes(&[0xAB]),
            config: Config {
                tx_fee: 0,
                create_asset_tx_fee: 0,
            },
            fee_asset_id: asset(),
            fx_index: fx::FxIndex::standard(),
            num_fxs: 3,
            bootstrapped: true,
            now,
            net: Some(net),
            shared_memory: Some(sm),
        }
    }

    /// A UTXO worth `amt`, owned by key `n`, produced by transaction `src`.
    fn funded(src: u8, amt: u64, n: u8) -> Utxo {
        Utxo {
            utxo_id: UtxoId::new(Id::prefixed_bytes(&[src]), 0),
            asset: Asset { id: asset() },
            out: State::Transfer(TransferOutput {
                amt,
                owners: Owners::new(1, vec![addr(n)]),
            }),
        }
    }

    /// Spend `funded(src, amt, n)` into one output worth the same.
    fn spend(src: u8, amt: u64, n: u8) -> Tx {
        let mut tx = Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: NETWORK_ID,
                blockchain_id: chain_id(),
                outs: vec![TransferableOutput {
                    asset: Asset { id: asset() },
                    out: State::Transfer(TransferOutput {
                        amt,
                        owners: Owners::new(1, vec![addr(n)]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(Id::prefixed_bytes(&[src]), 0),
                    asset: Asset { id: asset() },
                    input: fx::FxIn::Transfer(TransferInput {
                        amt,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: vec![],
            },
        }));
        tx.sign(fx::Family::Secp256k1, &[vec![key(n)]]).unwrap();
        tx
    }

    fn started() -> Manager {
        Manager::new(Store::new().shared())
    }

    /// Build the standing state: the asset's creating transaction has to be
    /// findable, because the semantic pass asks the asset which fxs it allows.
    fn with_asset_and_funds(funds: &[(u8, u64, u8)]) -> ChainRef {
        let mut store = Store::new();
        // An asset's id is the id of the transaction that created it. The
        // fixture wants a KNOWN asset id, so the creating transaction is
        // recorded under it directly.
        let create = Tx::new(Unsigned::CreateAsset(crate::txs::CreateAssetTx {
            base: BaseTxFields {
                network_id: NETWORK_ID,
                blockchain_id: chain_id(),
                outs: vec![],
                ins: vec![],
                memo: vec![],
            },
            name: "Asset".into(),
            symbol: "AST".into(),
            denomination: 0,
            states: vec![crate::txs::InitialState {
                fx_index: 0,
                outs: vec![State::Mint(crate::fx::secp256k1::MintOutput {
                    owners: Owners::new(1, vec![addr(1)]),
                })],
            }],
        }));
        store.add_tx_at(asset(), create);
        for (src, amt, n) in funds {
            store.add_utxo(funded(*src, *amt, *n));
        }
        store.shared()
    }

    /// Seal a block over `parent`, computing the root the verifier will expect.
    fn seal(mgr: &Manager, parent: &Block, txs: Vec<Tx>, time: u64) -> Block {
        let parent_state = mgr.state_after(&parent.id()).unwrap();
        let mut diff = Diff::on(parent_state);
        diff.set_timestamp(time);
        for tx in &txs {
            execute(&mut diff, tx).unwrap();
            diff.add_tx(tx.clone());
        }
        let height = parent.height() + 1;
        let root = block_execution_root(parent.merkle_root(), &txs, &diff, height).unwrap();
        Block::new(parent.id(), height, time, root, txs).unwrap()
    }

    fn genesis(mgr: &mut Manager) -> Block {
        let g = Block::new(EMPTY, 0, 0, EMPTY, vec![]).unwrap();
        mgr.set_genesis(g.clone());
        g
    }

    #[test]
    fn a_manager_starts_where_its_store_left_off() {
        let mgr = started();
        assert_eq!(mgr.last_accepted(), EMPTY);
        assert_eq!(mgr.preferred(), EMPTY);
    }

    #[test]
    fn a_good_block_verifies_accepts_and_lands_in_the_store() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store.clone());
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);

        let tx = spend(9, 100, 1);
        let blk = seal(&mgr, &g, vec![tx.clone()], 100);

        mgr.verify(&b, &blk).unwrap();
        assert!(mgr.is_processing(&blk.id()));
        // Nothing has moved in the store yet.
        assert!(store
            .lock()
            .unwrap()
            .get_utxo(&tx.id().prefix(&[0]))
            .is_err());

        mgr.accept(&blk.id()).unwrap();
        assert_eq!(mgr.last_accepted(), blk.id());
        assert!(!mgr.is_processing(&blk.id()));
        let s = store.lock().unwrap();
        // The input is gone and the output is there.
        assert!(s.get_utxo(&funded(9, 100, 1).input_id()).is_err());
        assert!(s.get_utxo(&tx.id().prefix(&[0])).is_ok());
        assert_eq!(s.get_last_accepted(), blk.id());
    }

    #[test]
    fn verifying_twice_is_the_same_as_verifying_once() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let blk = seal(&mgr, &g, vec![spend(9, 100, 1)], 100);
        mgr.verify(&b, &blk).unwrap();
        mgr.verify(&b, &blk).unwrap();
    }

    #[test]
    fn an_empty_block_is_refused() {
        let store = with_asset_and_funds(&[]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let blk = Block::new(g.id(), 1, 10, EMPTY, vec![]).unwrap();
        assert_eq!(mgr.verify(&b, &blk), Err(Error::EmptyBlock));
    }

    #[test]
    fn a_block_from_the_future_is_refused() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let blk = seal(&mgr, &g, vec![spend(9, 100, 1)], 1000 + SYNC_BOUND + 1);
        assert_eq!(mgr.verify(&b, &blk), Err(Error::TimestampBeyondSyncBound));
    }

    #[test]
    fn a_block_exactly_at_the_bound_is_allowed() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let blk = seal(&mgr, &g, vec![spend(9, 100, 1)], 1000 + SYNC_BOUND);
        mgr.verify(&b, &blk).unwrap();
    }

    #[test]
    fn a_block_at_the_wrong_height_is_refused() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let good = seal(&mgr, &g, vec![spend(9, 100, 1)], 100);
        let wrong = Block::new(g.id(), 7, 100, good.merkle_root(), good.txs().to_vec()).unwrap();
        assert_eq!(mgr.verify(&b, &wrong), Err(Error::IncorrectHeight(1, 7)));
    }

    #[test]
    fn a_block_whose_parent_is_unknown_is_refused() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let blk = Block::new(
            Id::prefixed_bytes(&[0xDE, 0xAD]),
            1,
            100,
            EMPTY,
            vec![spend(9, 100, 1)],
        )
        .unwrap();
        assert_eq!(mgr.verify(&b, &blk), Err(Error::NotFound));
    }

    #[test]
    fn a_block_older_than_its_parent_is_refused() {
        let store = with_asset_and_funds(&[(9, 100, 1), (8, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = Block::new(EMPTY, 0, 500, EMPTY, vec![]).unwrap();
        mgr.set_genesis(g.clone());
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        // Sealed at a time before the genesis time.
        let blk = seal(&mgr, &g, vec![spend(9, 100, 1)], 100);
        assert_eq!(
            mgr.verify(&b, &blk),
            Err(Error::ChildBlockEarlierThanParent)
        );
    }

    #[test]
    fn a_block_carrying_the_wrong_root_is_refused() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let good = seal(&mgr, &g, vec![spend(9, 100, 1)], 100);
        let bad = Block::new(
            g.id(),
            1,
            100,
            Id::prefixed_bytes(&[0xBA, 0xD0]),
            good.txs().to_vec(),
        )
        .unwrap();
        assert_eq!(mgr.verify(&b, &bad), Err(Error::UnexpectedMerkleRoot));
    }

    #[test]
    fn a_block_that_spends_the_same_output_twice_is_refused() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        // The second transaction spends a UTXO the first one already removed,
        // so semantic verification cannot find it.
        let tx = spend(9, 100, 1);
        let blk = seal(&mgr, &g, vec![tx.clone(), tx.clone()], 100);
        assert!(mgr.verify(&b, &blk).is_err());
    }

    #[test]
    fn a_rejected_block_changes_nothing_and_gives_its_transactions_back() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store.clone());
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let tx = spend(9, 100, 1);
        let blk = seal(&mgr, &g, vec![tx.clone()], 100);
        mgr.verify(&b, &blk).unwrap();

        let back = mgr.reject(&blk.id()).unwrap();
        assert_eq!(back.len(), 1);
        assert_eq!(back[0].id(), tx.id());
        assert!(!mgr.is_processing(&blk.id()));
        assert_eq!(mgr.last_accepted(), g.id());
        // The funding UTXO is untouched.
        assert!(store
            .lock()
            .unwrap()
            .get_utxo(&funded(9, 100, 1).input_id())
            .is_ok());
    }

    #[test]
    fn accepting_a_block_nobody_verified_is_refused() {
        let mut mgr = started();
        assert_eq!(
            mgr.accept(&Id::prefixed_bytes(&[7])),
            Err(Error::BlockNotFound)
        );
    }

    #[test]
    fn two_children_of_one_parent_cannot_see_each_other() {
        let store = with_asset_and_funds(&[(9, 100, 1), (8, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);

        let left = seal(&mgr, &g, vec![spend(9, 100, 1)], 100);
        let right = seal(&mgr, &g, vec![spend(8, 100, 1)], 100);
        mgr.verify(&b, &left).unwrap();
        mgr.verify(&b, &right).unwrap();

        // Each fork's state has its own spend and not the other's.
        let l = mgr.state_after(&left.id()).unwrap();
        assert!(l
            .lock()
            .unwrap()
            .get_utxo(&funded(9, 100, 1).input_id())
            .is_err());
        assert!(l
            .lock()
            .unwrap()
            .get_utxo(&funded(8, 100, 1).input_id())
            .is_ok());
    }

    #[test]
    fn a_state_that_was_thrown_away_is_not_offered() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        let g = genesis(&mut mgr);
        assert!(mgr.state_after(&g.id()).is_some());
        assert!(mgr.state_after(&Id::prefixed_bytes(&[0xFF])).is_none());
    }

    #[test]
    fn unique_inputs_are_only_checked_against_blocks_still_being_decided() {
        let mgr = started();
        let mut inputs = BTreeSet::new();
        inputs.insert(Id::prefixed_bytes(&[1]));
        // Nothing is processing, so nothing conflicts.
        mgr.verify_unique_inputs(&EMPTY, &inputs).unwrap();
        // An empty claim is always fine.
        mgr.verify_unique_inputs(&EMPTY, &BTreeSet::new()).unwrap();
    }

    #[test]
    fn a_transaction_is_not_admitted_before_the_chain_has_synced() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store);
        genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let mut b = backend(&net, &sm, 1000);
        b.bootstrapped = false;
        assert_eq!(
            mgr.verify_tx(&b, &spend(9, 100, 1)),
            Err(Error::ChainNotSynced)
        );
    }

    #[test]
    fn a_transaction_admitted_to_the_mempool_leaves_no_trace() {
        let store = with_asset_and_funds(&[(9, 100, 1)]);
        let mut mgr = Manager::new(store.clone());
        genesis(&mut mgr);
        let net = OneNet(Id::prefixed_bytes(&[0xAB]));
        let sm = NoMemory;
        let b = backend(&net, &sm, 1000);
        let tx = spend(9, 100, 1);
        mgr.verify_tx(&b, &tx).unwrap();
        // The throwaway diff kept nothing.
        assert!(store
            .lock()
            .unwrap()
            .get_utxo(&funded(9, 100, 1).input_id())
            .is_ok());
    }

    #[test]
    fn cross_chain_asks_for_one_chain_merge_into_one_entry() {
        let mut into = vec![(
            Id::prefixed_bytes(&[1]),
            AtomicRequests {
                puts: vec![],
                removes: vec![vec![1]],
            },
        )];
        merge_atomic(
            &mut into,
            vec![
                (
                    Id::prefixed_bytes(&[1]),
                    AtomicRequests {
                        puts: vec![],
                        removes: vec![vec![2]],
                    },
                ),
                (
                    Id::prefixed_bytes(&[2]),
                    AtomicRequests {
                        puts: vec![],
                        removes: vec![vec![3]],
                    },
                ),
            ],
        );
        assert_eq!(into.len(), 2);
        assert_eq!(into[0].1.removes, vec![vec![1], vec![2]]);
        assert_eq!(into[1].1.removes, vec![vec![3]]);
    }
}
