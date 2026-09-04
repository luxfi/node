// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain behind the host's seam.
//!
//! Everything above this is the chain thinking for itself: a transaction, a
//! block, the state they change. This is the one place that speaks the host's
//! language — ten methods, keyed by block id, with the lock inside.
//!
//! The mempool lives here rather than in the manager, because holding pending
//! transactions is a node's job, not a ledger's: the manager decides what is
//! true and this decides what to try next. A transaction that a block rejected
//! comes back here to be re-offered, and one that a block accepted is gone.

use std::sync::{Arc, Mutex};

use crate::block::builder;
use crate::block::manager::Manager;
use crate::block::Block;
use crate::error::Error as ChainError;
use crate::host;
use crate::ids::Id;
use crate::state::{Chain as _, Store};
use crate::txs::executor::{Backend, Config, Net, SharedMemory};
use crate::txs::Tx;
use crate::utxo::Runtime;
use crate::{fx, ids};

/// What time it is, so a test can say.
///
/// A chain reads the clock in exactly two places — the timestamp it stamps into
/// a block it builds, and the bound it holds an incoming block's timestamp to.
/// Both of them being one call means a test can move time and see the rule.
pub trait Clock: Send + Sync {
    /// Seconds since the epoch.
    fn now(&self) -> u64;
}

/// The system clock.
pub struct SystemClock;

impl Clock for SystemClock {
    fn now(&self) -> u64 {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0)
    }
}

/// A clock stopped at a value, for the tests.
pub struct FixedClock(pub Mutex<u64>);

impl FixedClock {
    pub fn new(t: u64) -> FixedClock {
        FixedClock(Mutex::new(t))
    }
    pub fn set(&self, t: u64) {
        *self.0.lock().expect("clock poisoned") = t;
    }
}

impl Clock for FixedClock {
    fn now(&self) -> u64 {
        *self.0.lock().expect("clock poisoned")
    }
}

/// What the chain is, before it is running.
pub struct Genesis {
    /// The network this chain is on.
    pub network_id: u32,
    /// This chain's id — what a transaction must name to be spent here.
    pub chain_id: Id,
    /// The network id, as an id: what a peer chain must be on.
    pub net_id: Id,
    /// The asset fees are paid in.
    pub fee_asset_id: Id,
    pub config: Config,
    /// The transactions that create the chain's first assets and hand out the
    /// first outputs. Their effects are applied before the genesis block is
    /// sealed, so the chain starts with them already true.
    pub txs: Vec<Tx>,
    /// Seconds since the epoch.
    pub timestamp: u64,
}

/// The transactions waiting to go into a block.
///
/// Insertion-ordered and free of duplicates: a transaction offered twice is
/// held once, and the order they were offered in is the order they are tried.
#[derive(Default)]
struct Mempool {
    order: Vec<Id>,
    txs: std::collections::HashMap<Id, Tx>,
    /// Why a transaction was dropped, kept so a caller can be told.
    dropped: std::collections::HashMap<Id, ChainError>,
}

impl Mempool {
    fn add(&mut self, tx: Tx) -> bool {
        let id = tx.id();
        if self.txs.contains_key(&id) {
            return false;
        }
        self.dropped.remove(&id);
        self.order.push(id);
        self.txs.insert(id, tx);
        true
    }

    fn remove(&mut self, id: &Id) {
        if self.txs.remove(id).is_some() {
            self.order.retain(|i| i != id);
        }
    }

    fn drop_with(&mut self, id: Id, why: ChainError) {
        self.remove(&id);
        self.dropped.insert(id, why);
    }

    fn candidates(&self) -> Vec<Tx> {
        self.order
            .iter()
            .filter_map(|id| self.txs.get(id).cloned())
            .collect()
    }

    fn len(&self) -> usize {
        self.txs.len()
    }
}

/// Everything the chain holds, behind one lock.
struct Inner {
    manager: Manager,
    mempool: Mempool,
    bootstrapped: bool,
    /// Blocks the chain has seen but not decided: what [`Vm::build`] produced
    /// and what [`Vm::parse`] read off the wire.
    ///
    /// The seam is keyed by id, so a block has to be findable by id between
    /// being read and being verified. A block leaves here when it is decided —
    /// accepted blocks live in the store, rejected ones are gone.
    known: std::collections::HashMap<Id, Block>,
}

impl Inner {
    /// A block from wherever it currently is.
    fn block(&self, blk_id: &Id) -> crate::Result<Block> {
        match self.manager.get_block(blk_id) {
            Ok(b) => Ok(b),
            Err(e) => self.known.get(blk_id).cloned().ok_or(e),
        }
    }
}

/// The X-Chain.
pub struct Xvm {
    inner: Mutex<Inner>,
    genesis: Genesis,
    clock: Arc<dyn Clock>,
    net: Option<Arc<dyn Net>>,
    shared_memory: Option<Arc<dyn SharedMemory>>,
    fx_index: fx::FxIndex,
}

/// How many feature extensions the chain runs: secp256k1, nft, property.
const NUM_FXS: usize = 3;

impl Xvm {
    /// Start a chain from its genesis.
    ///
    /// The genesis transactions are applied to the store and then sealed into
    /// block zero, so the chain's first state and its first block agree by
    /// construction rather than by a second rule.
    pub fn new(
        genesis: Genesis,
        clock: Arc<dyn Clock>,
        net: Option<Arc<dyn Net>>,
        shared_memory: Option<Arc<dyn SharedMemory>>,
    ) -> crate::Result<Xvm> {
        let mut store = Store::new();
        for tx in &genesis.txs {
            crate::txs::executor::execute(&mut store, tx)?;
            store.add_tx(tx.clone());
        }
        let store = store.shared();
        let mut manager = Manager::new(store);
        let genesis_block = Block::new(
            ids::EMPTY,
            0,
            genesis.timestamp,
            ids::EMPTY,
            genesis.txs.clone(),
        )?;
        manager.set_genesis(genesis_block);
        Ok(Xvm {
            inner: Mutex::new(Inner {
                manager,
                mempool: Mempool::default(),
                bootstrapped: false,
                known: std::collections::HashMap::new(),
            }),
            genesis,
            clock,
            net,
            shared_memory,
            fx_index: fx::FxIndex::standard(),
        })
    }

    /// Say the chain has caught up.
    ///
    /// Before this, signatures are not checked — a bootstrapping node is
    /// replaying history that was already decided, and re-checking every
    /// signature in it would only be a way to take longer to reach the same
    /// answer. After it, everything is checked.
    pub fn set_bootstrapped(&self, yes: bool) {
        self.inner.lock().expect("chain poisoned").bootstrapped = yes;
    }

    pub fn is_bootstrapped(&self) -> bool {
        self.inner.lock().expect("chain poisoned").bootstrapped
    }

    /// Offer a transaction. It is verified against the preferred state before
    /// it is held, so the mempool never carries something that cannot go in a
    /// block on this branch.
    pub fn issue(&self, tx: Tx) -> crate::Result<()> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let backend = self.backend(inner.bootstrapped);
        inner.manager.verify_tx(&backend, &tx)?;
        inner.mempool.add(tx);
        Ok(())
    }

    /// How many transactions are waiting.
    pub fn pending(&self) -> usize {
        self.inner.lock().expect("chain poisoned").mempool.len()
    }

    /// The committed state, for a caller that wants to read what is settled.
    pub fn committed_utxo(&self, utxo_id: &Id) -> crate::Result<crate::utxo::Utxo> {
        let inner = self.inner.lock().expect("chain poisoned");
        let store = inner.manager.store();
        let s = store.lock().expect("chain layer poisoned");
        s.get_utxo(utxo_id)
    }

    /// A transaction the chain has accepted.
    pub fn committed_tx(&self, tx_id: &Id) -> crate::Result<Tx> {
        let inner = self.inner.lock().expect("chain poisoned");
        let store = inner.manager.store();
        let s = store.lock().expect("chain layer poisoned");
        s.get_tx(tx_id)
    }

    fn backend(&self, bootstrapped: bool) -> Backend<'_> {
        Backend {
            runtime: Runtime {
                network_id: self.genesis.network_id,
                chain_id: self.genesis.chain_id,
            },
            net_id: self.genesis.net_id,
            config: self.genesis.config,
            fee_asset_id: self.genesis.fee_asset_id,
            fx_index: self.fx_index.clone(),
            num_fxs: NUM_FXS,
            bootstrapped,
            now: self.clock.now(),
            net: self.net.as_deref(),
            shared_memory: self.shared_memory.as_deref(),
        }
    }
}

/// A refusal in the chain's words, said in the host's.
///
/// The mapping is by what the host would DO about it, which is the only thing
/// the distinction is for: a block it has never seen is `NotFound`, bytes that
/// are not a block are `Malformed`, and everything else is a block that is
/// wrong.
fn to_host(e: ChainError) -> host::Error {
    match e {
        ChainError::NotFound | ChainError::BlockNotFound => host::Error::NotFound,
        ChainError::Wire(_) | ChainError::UnknownTxKind(_) | ChainError::UnknownFxPrimitive(..) => {
            host::Error::Malformed(e.to_string())
        }
        ChainError::NoTransactions => host::Error::Empty,
        other => host::Error::Invalid(other.to_string()),
    }
}

/// A block, as the host sees one.
struct HostBlock(Block);

impl host::Block for HostBlock {
    fn id(&self) -> host::Id {
        self.0.id().0
    }
    fn parent(&self) -> host::Id {
        self.0.parent().0
    }
    fn height(&self) -> u64 {
        self.0.height()
    }
    fn timestamp(&self) -> u64 {
        self.0.timestamp()
    }
    fn bytes(&self) -> Vec<u8> {
        self.0.bytes().to_vec()
    }
    fn state_root(&self) -> host::Id {
        self.0.merkle_root().0
    }
    fn payload_root(&self) -> host::Id {
        crate::block::root::payload_root(self.0.txs()).0
    }
}

impl host::Vm for Xvm {
    fn name(&self) -> &'static str {
        "X"
    }

    fn version(&self) -> String {
        env!("CARGO_PKG_VERSION").to_string()
    }

    fn build(&self) -> Result<Box<dyn host::Block>, host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let backend = self.backend(inner.bootstrapped);
        let now = self.clock.now();
        let candidates = inner.mempool.candidates();
        let built = builder::build(&inner.manager, &backend, now, &candidates).map_err(to_host)?;
        // What went in is no longer pending; what could not go in is not coming
        // back on this branch.
        for tx in built.block.txs() {
            inner.mempool.remove(&tx.id());
        }
        for (id, why) in built.dropped {
            inner.mempool.drop_with(id, why);
        }
        inner.known.insert(built.block.id(), built.block.clone());
        Ok(Box::new(HostBlock(built.block)))
    }

    fn parse(&self, raw: &[u8]) -> Result<Box<dyn host::Block>, host::Error> {
        // No state is read: a bootstrapping node parses blocks whose parents it
        // does not have. It is remembered so the id it answers with can be
        // handed back to `verify`.
        let blk = Block::parse(raw).map_err(|e| host::Error::Malformed(e.to_string()))?;
        self.inner
            .lock()
            .expect("chain poisoned")
            .known
            .insert(blk.id(), blk.clone());
        Ok(Box::new(HostBlock(blk)))
    }

    fn get(&self, id: &host::Id) -> Result<Box<dyn host::Block>, host::Error> {
        let inner = self.inner.lock().expect("chain poisoned");
        inner
            .block(&Id(*id))
            .map(|b| Box::new(HostBlock(b)) as Box<dyn host::Block>)
            .map_err(to_host)
    }

    fn verify(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let blk = inner.block(&Id(*id)).map_err(to_host)?;
        let backend = self.backend(inner.bootstrapped);
        inner.manager.verify(&backend, &blk).map_err(to_host)?;
        // A transaction that is in a verified block is not a candidate for
        // another one on this branch.
        for tx in blk.txs() {
            inner.mempool.remove(&tx.id());
        }
        Ok(())
    }

    fn accept(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let requests = inner.manager.accept(&Id(*id)).map_err(to_host)?;
        inner.known.remove(&Id(*id));
        if !requests.is_empty() {
            let sm = self
                .shared_memory
                .as_ref()
                .ok_or_else(|| host::Error::Invalid("no shared memory to apply to".into()))?;
            sm.apply(&requests).map_err(to_host)?;
        }
        Ok(())
    }

    fn reject(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let txs = inner.manager.reject(&Id(*id)).map_err(to_host)?;
        inner.known.remove(&Id(*id));
        // The block did nothing, so its transactions may still be good. Each is
        // re-checked against the preferred state before it is offered again.
        let backend = self.backend(inner.bootstrapped);
        let mut good = Vec::new();
        for tx in txs {
            if inner.manager.verify_tx(&backend, &tx).is_ok() {
                good.push(tx);
            }
        }
        for tx in good {
            inner.mempool.add(tx);
        }
        Ok(())
    }

    fn set_preference(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let blk_id = Id(*id);
        // Preferring a block nobody has is how a chain builds on nothing.
        inner.block(&blk_id).map_err(to_host)?;
        inner.manager.set_preference(blk_id);
        Ok(())
    }

    fn last_accepted(&self) -> host::Id {
        self.inner
            .lock()
            .expect("chain poisoned")
            .manager
            .last_accepted()
            .0
    }

    fn block_id_at(&self, height: u64) -> Result<host::Id, host::Error> {
        let inner = self.inner.lock().expect("chain poisoned");
        inner
            .manager
            .block_id_at_height(height)
            .map(|i| i.0)
            .map_err(to_host)
    }

    fn call(
        &self,
        method: &str,
        params: &serde_json::Value,
    ) -> Result<serde_json::Value, host::Error> {
        match method {
            // The chain's own name for itself.
            "xvm.getBlockchainID" => Ok(serde_json::json!(hex(&self.genesis.chain_id.0))),

            // Where the chain has got to.
            "xvm.getHeight" => {
                let inner = self.inner.lock().expect("chain poisoned");
                let last = inner.manager.last_accepted();
                let blk = inner.block(&last).map_err(to_host)?;
                Ok(serde_json::json!(blk.height()))
            }

            "xvm.getLastAccepted" => Ok(serde_json::json!(hex(&host::Vm::last_accepted(self)))),

            // One unspent output, by its id.
            "xvm.getUTXO" => {
                let id = id_param(params, "utxoID")?;
                let utxo = self.committed_utxo(&id).map_err(to_host)?;
                Ok(serde_json::json!({
                    "utxoID": hex(&utxo.input_id().0),
                    "txID": hex(&utxo.utxo_id.tx_id.0),
                    "outputIndex": utxo.utxo_id.output_index,
                    "assetID": hex(&utxo.asset_id().0),
                    "amount": utxo.out.amount(),
                    "locktime": utxo.out.owners().locktime,
                    "threshold": utxo.out.owners().threshold,
                }))
            }

            // One accepted transaction, as the bytes it was accepted as.
            "xvm.getTx" => {
                let id = id_param(params, "txID")?;
                let tx = self.committed_tx(&id).map_err(to_host)?;
                Ok(serde_json::json!({ "tx": hex(tx.bytes()) }))
            }

            // Offer a transaction, as the bytes a wallet signed.
            "xvm.issueTx" => {
                let raw = bytes_param(params, "tx")?;
                let tx = Tx::parse(&raw).map_err(|e| host::Error::Malformed(e.to_string()))?;
                let id = tx.id();
                self.issue(tx).map_err(to_host)?;
                Ok(serde_json::json!({ "txID": hex(&id.0) }))
            }

            "xvm.getPendingCount" => Ok(serde_json::json!(self.pending())),

            other => Err(host::Error::NoMethod(other.to_string())),
        }
    }
}

fn hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2);
    for x in b {
        s.push_str(&format!("{x:02x}"));
    }
    s
}

fn unhex(s: &str) -> Option<Vec<u8>> {
    if !s.len().is_multiple_of(2) {
        return None;
    }
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).ok())
        .collect()
}

fn bytes_param(params: &serde_json::Value, name: &str) -> Result<Vec<u8>, host::Error> {
    let s = params
        .get(name)
        .and_then(|v| v.as_str())
        .ok_or_else(|| host::Error::BadRequest(format!("{name} is required")))?;
    unhex(s).ok_or_else(|| host::Error::BadRequest(format!("{name} is not hex")))
}

fn id_param(params: &serde_json::Value, name: &str) -> Result<Id, host::Error> {
    let raw = bytes_param(params, name)?;
    Id::from_slice(&raw).ok_or_else(|| host::Error::BadRequest(format!("{name} is not 32 bytes")))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::{address_of, MintOutput, TransferInput, TransferOutput};
    use crate::fx::{Input, Owners, State};
    use crate::host::Vm as _;
    use crate::ids::ShortId;
    use crate::txs::executor::AtomicRequests;
    use crate::txs::{BaseTx, CreateAssetTx, InitialState, Unsigned};
    use crate::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, UtxoId};

    const NETWORK_ID: u32 = 10;

    fn chain_id() -> Id {
        Id::prefixed_bytes(&[5])
    }
    fn key(n: u8) -> [u8; 32] {
        let mut k = [0u8; 32];
        k[31] = n;
        k
    }
    fn addr(n: u8) -> ShortId {
        address_of(&key(n)).unwrap()
    }

    struct OneNet;
    impl Net for OneNet {
        fn network_of(&self, _: &Id) -> crate::Result<Id> {
            Ok(Id::prefixed_bytes(&[0xAB]))
        }
    }
    struct NoMemory;
    impl SharedMemory for NoMemory {
        fn get(&self, _: &Id, _: &[Vec<u8>]) -> crate::Result<Vec<Vec<u8>>> {
            Ok(Vec::new())
        }
        fn apply(&self, _: &[(Id, AtomicRequests)]) -> crate::Result<()> {
            Ok(())
        }
    }

    /// The one genesis transaction: it creates the asset, and its own id
    /// becomes the asset's id. Everything afterwards spends what it made.
    fn genesis_tx() -> Tx {
        Tx::new(Unsigned::CreateAsset(CreateAssetTx {
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
            states: vec![InitialState {
                fx_index: 0,
                outs: vec![
                    State::Mint(MintOutput {
                        owners: Owners::new(1, vec![addr(1)]),
                    }),
                    State::Transfer(TransferOutput {
                        amt: 1_000,
                        owners: Owners::new(1, vec![addr(1)]),
                    }),
                ],
            }],
        }))
    }

    fn a_chain(now: u64) -> (Xvm, Tx, Arc<FixedClock>) {
        let g = genesis_tx();
        let clock = Arc::new(FixedClock::new(now));
        let vm = Xvm::new(
            Genesis {
                network_id: NETWORK_ID,
                chain_id: chain_id(),
                net_id: Id::prefixed_bytes(&[0xAB]),
                fee_asset_id: g.id(),
                config: Config {
                    tx_fee: 0,
                    create_asset_tx_fee: 0,
                },
                txs: vec![g.clone()],
                timestamp: now,
            },
            clock.clone(),
            Some(Arc::new(OneNet)),
            Some(Arc::new(NoMemory)),
        )
        .unwrap();
        vm.set_bootstrapped(true);
        (vm, g, clock)
    }

    /// Spend the genesis transfer output (index 1) into one output.
    fn spend_genesis(g: &Tx, amt: u64, n: u8) -> Tx {
        let mut tx = Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: NETWORK_ID,
                blockchain_id: chain_id(),
                outs: vec![TransferableOutput {
                    asset: Asset { id: g.id() },
                    out: State::Transfer(TransferOutput {
                        amt,
                        owners: Owners::new(1, vec![addr(n)]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(g.id(), 1),
                    asset: Asset { id: g.id() },
                    input: crate::fx::FxIn::Transfer(TransferInput {
                        amt: 1_000,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: vec![],
            },
        }));
        tx.sign(fx::Family::Secp256k1, &[vec![key(1)]]).unwrap();
        tx
    }

    #[test]
    fn the_chain_names_itself_and_its_version() {
        let (vm, _, _) = a_chain(1000);
        assert_eq!(vm.name(), "X");
        assert_eq!(vm.version(), env!("CARGO_PKG_VERSION"));
    }

    #[test]
    fn a_chain_starts_at_its_genesis_block() {
        let (vm, _, _) = a_chain(1000);
        let last = vm.last_accepted();
        let blk = vm.get(&last).unwrap();
        assert_eq!(blk.height(), 0);
        assert_eq!(blk.id(), last);
        assert_eq!(blk.parent(), ids::EMPTY.0);
    }

    #[test]
    fn the_genesis_state_is_true_before_any_block_is_built() {
        let (vm, g, _) = a_chain(1000);
        // The transfer output the genesis transaction made is spendable.
        let utxo = vm.committed_utxo(&g.id().prefix(&[1])).unwrap();
        assert_eq!(utxo.out.amount(), 1_000);
    }

    #[test]
    fn nothing_to_build_is_said_rather_than_an_empty_block() {
        let (vm, _, _) = a_chain(1000);
        assert!(matches!(vm.build(), Err(host::Error::Empty)));
    }

    #[test]
    fn a_transaction_goes_in_a_block_and_the_block_is_accepted() {
        let (vm, g, _) = a_chain(1000);
        let tx = spend_genesis(&g, 1_000, 2);
        vm.issue(tx.clone()).unwrap();
        assert_eq!(vm.pending(), 1);

        let blk = vm.build().unwrap();
        assert_eq!(blk.height(), 1);
        assert_eq!(vm.pending(), 0);

        let id = blk.id();
        vm.verify(&id).unwrap();
        vm.accept(&id).unwrap();
        assert_eq!(vm.last_accepted(), id);

        // The spend happened.
        assert!(vm.committed_utxo(&g.id().prefix(&[1])).is_err());
        assert!(vm.committed_utxo(&tx.id().prefix(&[0])).is_ok());
    }

    #[test]
    fn a_built_block_survives_a_round_trip_through_its_bytes() {
        let (vm, g, _) = a_chain(1000);
        vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
        let blk = vm.build().unwrap();
        let again = vm.parse(&blk.bytes()).unwrap();
        assert_eq!(again.id(), blk.id());
        assert_eq!(again.height(), blk.height());
        assert_eq!(again.state_root(), blk.state_root());
        assert_eq!(again.payload_root(), blk.payload_root());
    }

    #[test]
    fn bytes_that_are_not_a_block_are_malformed_not_invalid() {
        let (vm, _, _) = a_chain(1000);
        assert!(matches!(
            vm.parse(b"not a block at all"),
            Err(host::Error::Malformed(_))
        ));
    }

    #[test]
    fn a_block_nobody_has_is_not_found() {
        let (vm, _, _) = a_chain(1000);
        assert!(matches!(vm.get(&[7u8; 32]), Err(host::Error::NotFound)));
        assert_eq!(vm.verify(&[7u8; 32]).unwrap_err(), host::Error::NotFound);
        assert_eq!(vm.accept(&[7u8; 32]).unwrap_err(), host::Error::NotFound);
    }

    #[test]
    fn verifying_the_same_block_twice_is_allowed() {
        let (vm, g, _) = a_chain(1000);
        vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.verify(&blk.id()).unwrap();
    }

    #[test]
    fn a_rejected_blocks_transactions_come_back() {
        let (vm, g, _) = a_chain(1000);
        let tx = spend_genesis(&g, 1_000, 2);
        vm.issue(tx.clone()).unwrap();
        let blk = vm.build().unwrap();
        assert_eq!(vm.pending(), 0);
        vm.verify(&blk.id()).unwrap();
        vm.reject(&blk.id()).unwrap();
        // Still valid against the unchanged state, so still offered.
        assert_eq!(vm.pending(), 1);
        assert_eq!(
            vm.last_accepted(),
            vm.get(&vm.last_accepted()).unwrap().id()
        );
    }

    #[test]
    fn preference_can_only_name_a_block_the_chain_has() {
        let (vm, g, _) = a_chain(1000);
        assert_eq!(
            vm.set_preference(&[9u8; 32]).unwrap_err(),
            host::Error::NotFound
        );
        vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.set_preference(&blk.id()).unwrap();
    }

    #[test]
    fn a_chain_builds_a_second_block_on_the_first() {
        let (vm, g, _) = a_chain(1000);
        let first = spend_genesis(&g, 1_000, 2);
        vm.issue(first.clone()).unwrap();
        let b1 = vm.build().unwrap();
        vm.verify(&b1.id()).unwrap();
        vm.accept(&b1.id()).unwrap();
        vm.set_preference(&b1.id()).unwrap();

        // Spend what the first block produced.
        let mut second = Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: NETWORK_ID,
                blockchain_id: chain_id(),
                outs: vec![TransferableOutput {
                    asset: Asset { id: g.id() },
                    out: State::Transfer(TransferOutput {
                        amt: 1_000,
                        owners: Owners::new(1, vec![addr(3)]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(first.id(), 0),
                    asset: Asset { id: g.id() },
                    input: crate::fx::FxIn::Transfer(TransferInput {
                        amt: 1_000,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: vec![],
            },
        }));
        second.sign(fx::Family::Secp256k1, &[vec![key(2)]]).unwrap();
        vm.issue(second).unwrap();

        let b2 = vm.build().unwrap();
        assert_eq!(b2.height(), 2);
        assert_eq!(b2.parent(), b1.id());
        vm.verify(&b2.id()).unwrap();
        vm.accept(&b2.id()).unwrap();
        assert_eq!(vm.last_accepted(), b2.id());
    }

    #[test]
    fn a_transaction_that_cannot_be_verified_is_not_held() {
        let (vm, g, _) = a_chain(1000);
        // Spends an output nothing produced.
        let mut tx = spend_genesis(&g, 1_000, 2);
        if let Unsigned::Base(t) = &mut tx.unsigned {
            t.base.ins[0].utxo_id = UtxoId::new(Id::prefixed_bytes(&[0xDD]), 0);
        }
        let tx = {
            let mut t = Tx::new(tx.unsigned.clone());
            t.sign(fx::Family::Secp256k1, &[vec![key(1)]]).unwrap();
            t
        };
        assert!(vm.issue(tx).is_err());
        assert_eq!(vm.pending(), 0);
    }

    #[test]
    fn the_same_transaction_offered_twice_is_held_once() {
        let (vm, g, _) = a_chain(1000);
        let tx = spend_genesis(&g, 1_000, 2);
        vm.issue(tx.clone()).unwrap();
        vm.issue(tx).unwrap();
        assert_eq!(vm.pending(), 1);
    }

    #[test]
    fn the_height_index_answers_for_the_blocks_the_chain_has_accepted() {
        let (vm, g, _) = a_chain(1000);
        vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
        let b1 = vm.build().unwrap();
        vm.verify(&b1.id()).unwrap();
        vm.accept(&b1.id()).unwrap();
        assert_eq!(vm.block_id_at(1).unwrap(), b1.id());
        assert_eq!(vm.block_id_at(9).unwrap_err(), host::Error::NotFound);
    }

    #[test]
    fn a_method_the_chain_does_not_answer_says_so() {
        let (vm, _, _) = a_chain(1000);
        assert_eq!(
            vm.call("xvm.nonsense", &serde_json::json!({})).unwrap_err(),
            host::Error::NoMethod("xvm.nonsense".into())
        );
    }

    #[test]
    fn the_chain_answers_for_its_own_identity_and_height() {
        let (vm, _, _) = a_chain(1000);
        let params = serde_json::json!({});
        assert_eq!(
            vm.call("xvm.getBlockchainID", &params).unwrap(),
            serde_json::json!(hex(&chain_id().0))
        );
        assert_eq!(
            vm.call("xvm.getHeight", &params).unwrap(),
            serde_json::json!(0)
        );
        assert_eq!(
            vm.call("xvm.getLastAccepted", &params).unwrap(),
            serde_json::json!(hex(&vm.last_accepted()))
        );
    }

    #[test]
    fn a_transaction_can_be_offered_and_read_back_over_the_call_surface() {
        let (vm, g, _) = a_chain(1000);
        let tx = spend_genesis(&g, 1_000, 2);
        let issued = vm
            .call("xvm.issueTx", &serde_json::json!({ "tx": hex(tx.bytes()) }))
            .unwrap();
        assert_eq!(issued["txID"], serde_json::json!(hex(&tx.id().0)));
        assert_eq!(
            vm.call("xvm.getPendingCount", &serde_json::json!({}))
                .unwrap(),
            serde_json::json!(1)
        );

        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();

        let got = vm
            .call("xvm.getTx", &serde_json::json!({ "txID": hex(&tx.id().0) }))
            .unwrap();
        assert_eq!(got["tx"], serde_json::json!(hex(tx.bytes())));
    }

    #[test]
    fn a_call_with_a_parameter_the_chain_cannot_use_says_so() {
        let (vm, _, _) = a_chain(1000);
        match vm.call("xvm.getTx", &serde_json::json!({})) {
            Err(host::Error::BadRequest(_)) => {}
            other => panic!("expected bad request, got {other:?}"),
        }
        match vm.call("xvm.getTx", &serde_json::json!({ "txID": "zz" })) {
            Err(host::Error::BadRequest(_)) => {}
            other => panic!("expected bad request, got {other:?}"),
        }
        match vm.call("xvm.getTx", &serde_json::json!({ "txID": "00ff" })) {
            Err(host::Error::BadRequest(_)) => {}
            other => panic!("expected bad request, got {other:?}"),
        }
    }

    #[test]
    fn an_unspent_output_can_be_read_over_the_call_surface() {
        let (vm, g, _) = a_chain(1000);
        let utxo_id = g.id().prefix(&[1]);
        let got = vm
            .call(
                "xvm.getUTXO",
                &serde_json::json!({ "utxoID": hex(&utxo_id.0) }),
            )
            .unwrap();
        assert_eq!(got["amount"], serde_json::json!(1_000));
        assert_eq!(got["assetID"], serde_json::json!(hex(&g.id().0)));
        assert_eq!(got["outputIndex"], serde_json::json!(1));
    }

    #[test]
    fn health_is_reported_and_the_seam_is_object_safe() {
        let (vm, _, _) = a_chain(1000);
        vm.health().unwrap();
        let as_dyn: &dyn host::Vm = &vm;
        assert_eq!(as_dyn.name(), "X");
    }

    #[test]
    fn a_block_from_too_far_in_the_future_is_refused() {
        let (vm, g, clock) = a_chain(1000);
        vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
        let blk = vm.build().unwrap();
        // The clock goes backwards under the built block's timestamp.
        clock.set(1000 - crate::block::manager::SYNC_BOUND - 1);
        match vm.verify(&blk.id()) {
            Err(host::Error::Invalid(why)) => assert!(why.contains("too far in the future")),
            other => panic!("expected invalid, got {other:?}"),
        }
    }
}
