// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain behind the host's seam.
//!
//! Everything above this is the chain thinking for itself: a transaction, a
//! block, the state they change. This is the one place that speaks the host's
//! language — ten methods, keyed by block id, with the lock inside.
//!
//! The mempool lives beside the manager rather than inside it, because holding
//! pending transactions is a node's job, not a ledger's: the manager decides
//! what is true and the pool decides what to try next. A transaction that a
//! block rejected comes back to it to be re-offered, and one that a block
//! accepted is gone.
//!
//! There is ONE pool, and the chain reaches it through [`crate::gossip::Gossip`]
//! — the same set, with the filter this node advertises and the queue of what
//! it has yet to pass on. A wallet's transaction and a peer's arrive at the
//! same door, in the same order, judged by the same rules: already held,
//! already refused, does it verify, does it fit, does it conflict, is it
//! allowed. Two doors would be two answers, and the second one would be the
//! one nobody tested.

use std::sync::{Arc, Mutex};

use crate::block::builder;
use crate::block::manager::Manager;
use crate::block::Block;
use crate::error::Error as ChainError;
use crate::gossip::{self, Gossip};
use crate::host;
use crate::ids::Id;
use crate::mempool::Mempool;
use crate::security::{Exempt, Profile};
use crate::state::{Chain as _, ReadOnlyChain as _, Store};
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

/// Everything the chain holds, behind one lock.
struct Inner {
    manager: Manager,
    /// The one pool, and what this node tells peers about it.
    gossip: Gossip,
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
    /// Start a chain from its genesis, holding it in memory only.
    ///
    /// The genesis transactions are applied to the store and then sealed into
    /// block zero, so the chain's first state and its first block agree by
    /// construction rather than by a second rule.
    pub fn new(
        genesis: Genesis,
        clock: Arc<dyn Clock>,
        entropy: Arc<dyn gossip::Entropy>,
        net: Option<Arc<dyn Net>>,
        shared_memory: Option<Arc<dyn SharedMemory>>,
    ) -> crate::Result<Xvm> {
        Xvm::start(genesis, clock, entropy, net, shared_memory, Store::new())
    }

    /// Start a chain on a database, from its genesis or from where it was left.
    ///
    /// An empty database is a chain that has never run: genesis is applied and
    /// sealed, exactly as [`Xvm::new`] does, and then written down. A database
    /// that has been written to is a chain that HAS run, and it comes back as
    /// it was — genesis is not re-applied, because doing so would execute
    /// transactions that were executed once already.
    ///
    /// The genesis it is handed is still checked against the one on disk. A
    /// node pointed at another chain's database has to be told so: silently
    /// running one chain's genesis over another chain's state is how two
    /// networks end up sharing a directory and disagreeing about history.
    pub fn open(
        genesis: Genesis,
        clock: Arc<dyn Clock>,
        entropy: Arc<dyn gossip::Entropy>,
        net: Option<Arc<dyn Net>>,
        shared_memory: Option<Arc<dyn SharedMemory>>,
        db: Arc<dyn crate::db::Db>,
    ) -> crate::Result<Xvm> {
        let store = Store::on(db)?;
        if !store.is_initialized() {
            return Xvm::start(genesis, clock, entropy, net, shared_memory, store);
        }

        // The chain has run. The genesis block it ran under is block zero, and
        // it is on disk: the caller's genesis has to seal to the same block.
        let want = Block::new(
            ids::EMPTY,
            0,
            genesis.timestamp,
            ids::EMPTY,
            genesis.txs.clone(),
        )?;
        let held = store
            .get_block_id_at_height(0)
            .and_then(|id| store.get_block(&id));
        match held {
            Ok(blk) if blk.id() == want.id() => {}
            Ok(blk) => {
                return Err(ChainError::Storage(format!(
                    "this database is chain {}, not {}",
                    ids::hex(&blk.id()),
                    ids::hex(&want.id())
                )))
            }
            Err(_) => {
                return Err(ChainError::Storage(
                    "an initialised database with no genesis block".into(),
                ))
            }
        }

        let manager = Manager::new(store.shared());
        Ok(Xvm {
            inner: Mutex::new(Inner {
                manager,
                gossip: Gossip::new(Mempool::new(), &gossip::Config::default(), entropy)?,
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

    /// Apply genesis to a fresh store and seal block zero.
    fn start(
        genesis: Genesis,
        clock: Arc<dyn Clock>,
        entropy: Arc<dyn gossip::Entropy>,
        net: Option<Arc<dyn Net>>,
        shared_memory: Option<Arc<dyn SharedMemory>>,
        mut store: Store,
    ) -> crate::Result<Xvm> {
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
        manager.set_genesis(genesis_block)?;
        Ok(Xvm {
            inner: Mutex::new(Inner {
                manager,
                gossip: Gossip::new(Mempool::new(), &gossip::Config::default(), entropy)?,
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

    /// Hold this chain's transactions to a security profile.
    ///
    /// Go's `SetAuthPolicy`, called once at start-up from the chain's
    /// configuration. Until it is called the pool admits on structure alone;
    /// after it, [`crate::security::admits`] runs on every offer. Both the
    /// direct [`Xvm::issue`] path and the gossip path go through the same pool,
    /// so there is one place a transaction can get in and one rule it passes.
    pub fn hold_to(&self, profile: Profile, exempt: Option<Arc<dyn Exempt>>) {
        self.inner
            .lock()
            .expect("chain poisoned")
            .gossip
            .pool_mut()
            .hold_to(profile, exempt);
    }

    /// The terms this chain admits on, if it has been told any.
    pub fn profile(&self) -> Option<Profile> {
        self.inner
            .lock()
            .expect("chain poisoned")
            .gossip
            .pool_mut()
            .profile()
            .cloned()
    }

    /// Offer a transaction — from a wallet, from a peer's push, or as a pull's
    /// answer. One door, and these are not three kinds of offer.
    ///
    /// The order is Go's, cheapest first: already held, already refused for a
    /// reason this node still remembers, then verification against the state
    /// this node prefers, then the pool's own admission — size, room,
    /// conflicts, and the chain's security terms. Every refusal before the
    /// verification costs a lookup, which is what keeps a flood of junk from
    /// being a way to make this node work for free.
    ///
    /// Returning `Ok(())` also means it is worth passing on, so it joins what
    /// this node has to gossip and what it advertises holding.
    pub fn issue(&self, tx: Tx) -> crate::Result<()> {
        let inner = &mut *self.inner.lock().expect("chain poisoned");
        let backend = self.backend(inner.bootstrapped);
        // The chain's own verification, passed to the pool rather than held by
        // it: the manager decides whether a transaction is good, here as it
        // does for a block, and there is no second copy of that judgement.
        let manager = &inner.manager;
        inner.gossip.add(tx, |t| manager.verify_tx(&backend, t))
    }

    /// Whether this node holds it — exactly, not probabilistically. What a
    /// peer's `Has` asks.
    pub fn holds(&self, id: &Id) -> bool {
        self.inner.lock().expect("chain poisoned").gossip.has(id)
    }

    /// What to tell peers this node already knows: the filter, and the salt it
    /// was built under.
    pub fn advertise(&self) -> (Vec<u8>, Id) {
        self.inner
            .lock()
            .expect("chain poisoned")
            .gossip
            .marshal_filter()
    }

    /// Transactions to push, oldest first, up to `target` bytes — each as the
    /// canonical bytes a peer parses back.
    pub fn take_outbound(&self, target: usize) -> Vec<Vec<u8>> {
        self.inner
            .lock()
            .expect("chain poisoned")
            .gossip
            .take_outbound(target)
    }

    /// How many are waiting to be pushed.
    pub fn outbound(&self) -> usize {
        self.inner.lock().expect("chain poisoned").gossip.outbound()
    }

    /// Why a transaction was refused, if this node still remembers.
    pub fn refusal(&self, id: &Id) -> Option<ChainError> {
        self.inner
            .lock()
            .expect("chain poisoned")
            .gossip
            .pool()
            .drop_reason(id)
            .cloned()
    }

    /// How many transactions are waiting.
    pub fn pending(&self) -> usize {
        self.inner
            .lock()
            .expect("chain poisoned")
            .gossip
            .pool()
            .len()
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

/// A block, as consensus sees one.
///
/// The same block, not a wrapper around it. A chain's block and the thing the
/// node certifies are one value seen two ways: the chain reads its
/// transactions, the node reads its height and the two roots it commits to.
/// Wrapping would have made a second type that has to be unwrapped at every
/// boundary and kept in step with the first.
impl host::Block for Block {
    fn id(&self) -> host::Id {
        Block::id(self)
    }
    fn parent(&self) -> host::Id {
        Block::parent(self)
    }
    fn height(&self) -> u64 {
        Block::height(self)
    }
    fn timestamp(&self) -> u64 {
        Block::timestamp(self)
    }
    fn bytes(&self) -> Vec<u8> {
        Block::bytes(self).to_vec()
    }
    fn state_root(&self) -> host::Id {
        self.merkle_root()
    }
    fn payload_root(&self) -> host::Id {
        crate::block::root::payload_root(self.txs())
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
        let candidates = inner.gossip.pool_mut().candidates();
        let built = builder::build(&inner.manager, &backend, now, &candidates).map_err(to_host)?;
        // What went in is no longer pending, and neither is anything that
        // wanted the same outputs. What could not go in is not coming back on
        // this branch, and the reason is remembered.
        let taken = built.block.txs().to_vec();
        inner.gossip.pool_mut().remove(&taken);
        for (id, why) in built.dropped {
            inner.gossip.pool_mut().remove_id(&id);
            inner.gossip.pool_mut().mark_dropped(id, why);
        }
        inner.known.insert(built.block.id(), built.block.clone());
        Ok(Box::new(built.block))
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
        Ok(Box::new(blk))
    }

    fn get(&self, id: &host::Id) -> Result<Box<dyn host::Block>, host::Error> {
        let inner = self.inner.lock().expect("chain poisoned");
        inner
            .block(id)
            .map(|b| Box::new(b) as Box<dyn host::Block>)
            .map_err(to_host)
    }

    fn verify(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let blk = inner.block(id).map_err(to_host)?;
        let backend = self.backend(inner.bootstrapped);
        inner.manager.verify(&backend, &blk).map_err(to_host)?;
        // A transaction that is in a verified block is not a candidate for
        // another one on this branch — and neither is one that wanted the same
        // outputs, whether or not this node was holding it.
        let taken = blk.txs().to_vec();
        inner.gossip.pool_mut().remove(&taken);
        Ok(())
    }

    fn accept(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        // The shared area goes IN, rather than the block's asks coming out: the
        // block's own state and what it moves across a chain boundary are one
        // write, and only the accept path holds both halves at once. A caller
        // handed the asks after the state had been written would be making the
        // second of two writes, which is the arrangement an interrupted process
        // turns into value created or lost.
        inner
            .manager
            .accept(id, self.shared_memory.as_deref())
            .map_err(to_host)?;
        inner.known.remove(id);
        Ok(())
    }

    fn reject(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let txs = inner.manager.reject(id).map_err(to_host)?;
        inner.known.remove(id);
        // The block did nothing, so its transactions may still be good. Each is
        // re-checked against the preferred state before it is offered again.
        let backend = self.backend(inner.bootstrapped);
        let mut good = Vec::new();
        for tx in txs {
            if inner.manager.verify_tx(&backend, &tx).is_ok() {
                good.push(tx);
            }
        }
        // Re-offered through the pool's own admission, so a transaction that a
        // rejected block carried does not re-enter on weaker terms than one a
        // peer sends: the security gate runs on it again.
        for tx in good {
            // Through the one door, so what comes back is admitted on the same
            // terms as what a peer sends — and lands in what this node
            // advertises and passes on, which a direct pool insert would skip.
            let _ = inner.gossip.add_unverified(tx);
        }
        Ok(())
    }

    fn set_preference(&self, id: &host::Id) -> Result<(), host::Error> {
        let mut inner = self.inner.lock().expect("chain poisoned");
        let blk_id = *id;
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
    }

    fn block_id_at(&self, height: u64) -> Result<host::Id, host::Error> {
        let inner = self.inner.lock().expect("chain poisoned");
        inner.manager.block_id_at_height(height).map_err(to_host)
    }

    fn call(
        &self,
        method: &str,
        params: &serde_json::Value,
    ) -> Result<serde_json::Value, host::Error> {
        match method {
            // The chain's own name for itself.
            "xvm.getBlockchainID" => Ok(serde_json::json!(hex(&self.genesis.chain_id))),

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
                    "utxoID": hex(&utxo.input_id()),
                    "txID": hex(&utxo.utxo_id.tx_id),
                    "outputIndex": utxo.utxo_id.output_index,
                    "assetID": hex(&utxo.asset_id()),
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
                Ok(serde_json::json!({ "txID": hex(&id) }))
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
    ids::from_slice(&raw).ok_or_else(|| host::Error::BadRequest(format!("{name} is not 32 bytes")))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::Batch;
    use crate::fx::secp256k1::{address_of, MintOutput, TransferInput, TransferOutput};
    use crate::fx::{FxIn, Input, Owners, State};
    use crate::host::Vm as _;
    use crate::ids::ShortId;
    use crate::txs::executor::AtomicRequests;
    use crate::txs::{BaseTx, CreateAssetTx, InitialState, Unsigned};
    use crate::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, UtxoId};

    const NETWORK_ID: u32 = 10;

    /// A fixed entropy source. The salt only has to be unpredictable to a
    /// stranger; a test that could not say what it is could not check the
    /// filter against Go's bytes at all.
    fn a_salt() -> Arc<dyn crate::gossip::Entropy> {
        Arc::new(crate::gossip::Fixed(vec![0x5A, 0xA5, 0x11, 0x22]))
    }

    fn chain_id() -> Id {
        ids::prefixed(&[5])
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
            Ok(ids::prefixed(&[0xAB]))
        }
    }
    struct NoMemory;
    impl SharedMemory for NoMemory {
        fn get(&self, _: &Id, _: &[Vec<u8>]) -> crate::Result<Vec<Vec<u8>>> {
            Ok(Vec::new())
        }
        fn apply(&self, _: &[(Id, AtomicRequests)], batch: &Batch) -> crate::Result<()> {
            // These chains never cross a boundary, so nothing arrives here
            // holding a block. If something did, returning without writing
            // `batch` would lose it.
            assert!(
                batch.is_empty(),
                "a block reached a shared area that does not write"
            );
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
                net_id: ids::prefixed(&[0xAB]),
                fee_asset_id: g.id(),
                config: Config {
                    tx_fee: 0,
                    create_asset_tx_fee: 0,
                },
                txs: vec![g.clone()],
                timestamp: now,
            },
            clock.clone(),
            a_salt(),
            Some(Arc::new(OneNet)),
            Some(Arc::new(NoMemory)),
        )
        .unwrap();
        vm.set_bootstrapped(true);
        (vm, g, clock)
    }

    /// The same chain, on a database, opened rather than created — so calling
    /// it twice over one database is a restart.
    fn a_chain_on(db: Arc<dyn crate::db::Db>, now: u64) -> (Xvm, Tx) {
        let g = genesis_tx();
        let vm = Xvm::open(
            Genesis {
                network_id: NETWORK_ID,
                chain_id: chain_id(),
                net_id: ids::prefixed(&[0xAB]),
                fee_asset_id: g.id(),
                config: Config {
                    tx_fee: 0,
                    create_asset_tx_fee: 0,
                },
                txs: vec![g.clone()],
                timestamp: 1000,
            },
            Arc::new(FixedClock::new(now)),
            a_salt(),
            Some(Arc::new(OneNet)),
            Some(Arc::new(NoMemory)),
            db,
        )
        .unwrap();
        vm.set_bootstrapped(true);
        (vm, g)
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

    // ------------------------------------------------------- across a restart --

    #[test]
    fn a_chain_comes_back_where_it_left_off() {
        let db: Arc<dyn crate::db::Db> = Arc::new(crate::db::Memory::new());

        // A chain runs, accepts a block, and the process ends.
        let (state_root, block_id, spent, made, genesis_id) = {
            let (vm, g) = a_chain_on(db.clone(), 1000);
            let genesis_id = vm.last_accepted();
            let tx = spend_genesis(&g, 1_000, 2);
            vm.issue(tx.clone()).unwrap();
            let blk = vm.build().unwrap();
            vm.verify(&blk.id()).unwrap();
            vm.accept(&blk.id()).unwrap();
            (
                blk.state_root(),
                blk.id(),
                ids::prefix(&g.id(), &[1]),
                ids::prefix(&tx.id(), &[0]),
                genesis_id,
            )
        };

        // A new chain, over the same database. Nothing is replayed.
        let (again, _) = a_chain_on(db.clone(), 2000);
        assert_eq!(again.last_accepted(), block_id, "at the block it accepted");
        assert_eq!(again.block_id_at(0).unwrap(), genesis_id);
        assert_eq!(again.block_id_at(1).unwrap(), block_id);

        // The ledger is the ledger it had: the output that was spent is gone
        // and the output that was made is there.
        assert_eq!(
            again.committed_utxo(&spent).unwrap_err(),
            ChainError::NotFound
        );
        assert_eq!(again.committed_utxo(&made).unwrap().out.amount(), 1_000);

        // And the block reads back as the same bytes, which means it hashes to
        // the id it is filed under and commits to the same state root.
        let blk = again.get(&block_id).unwrap();
        assert_eq!(blk.id(), block_id);
        assert_eq!(blk.state_root(), state_root);
        assert_eq!(blk.height(), 1);

        // The chain keeps going from there.
        let (vm3, g3) = (again, genesis_tx());
        assert_eq!(g3.id(), vm3.committed_tx(&g3.id()).unwrap().id());
        let next = spend_second(&vm3, block_id);
        assert_eq!(next, 2, "the next block is height two");
    }

    /// Build an empty-mempool chain's next block by re-offering the only output
    /// left, and return the height it reached.
    fn spend_second(vm: &Xvm, _parent: Id) -> u64 {
        // The one spendable output after the first block is the one it made,
        // owned by key 2. Spend it back to key 1.
        let last = vm.last_accepted();
        let blk = vm.get(&last).unwrap();
        let prev = Block::parse(&blk.bytes()).unwrap();
        let source = prev.txs()[0].id();
        let mut tx = Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: NETWORK_ID,
                blockchain_id: chain_id(),
                outs: vec![TransferableOutput {
                    asset: Asset {
                        id: genesis_tx().id(),
                    },
                    out: State::Transfer(TransferOutput {
                        amt: 1_000,
                        owners: Owners::new(1, vec![addr(1)]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(source, 0),
                    asset: Asset {
                        id: genesis_tx().id(),
                    },
                    input: FxIn::Transfer(TransferInput {
                        amt: 1_000,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: vec![],
            },
        }));
        tx.sign(fx::Family::Secp256k1, &[vec![key(2)]]).unwrap();
        vm.issue(tx).unwrap();
        let built = vm.build().unwrap();
        vm.verify(&built.id()).unwrap();
        vm.accept(&built.id()).unwrap();
        built.height()
    }

    #[test]
    fn a_block_that_was_verified_and_never_accepted_leaves_nothing_behind() {
        let db: Arc<dyn crate::db::Db> = Arc::new(crate::db::Memory::new());
        let at_genesis = {
            let (vm, g) = a_chain_on(db.clone(), 1000);
            vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
            let blk = vm.build().unwrap();
            vm.verify(&blk.id()).unwrap();
            // Verified — the whole block executed — and then the process ends
            // without it being accepted.
            vm.last_accepted()
        };
        let (again, g) = a_chain_on(db, 2000);
        assert_eq!(again.last_accepted(), at_genesis, "still at genesis");
        assert_eq!(again.block_id_at(1).unwrap_err(), host::Error::NotFound);
        // The output the unaccepted block would have spent is still spendable.
        assert_eq!(
            again
                .committed_utxo(&ids::prefix(&g.id(), &[1]))
                .unwrap()
                .out
                .amount(),
            1_000
        );
    }

    #[test]
    fn a_database_written_by_another_chain_is_refused_by_name() {
        let db: Arc<dyn crate::db::Db> = Arc::new(crate::db::Memory::new());
        let _ = a_chain_on(db.clone(), 1000);

        // The same database, handed a genesis that seals to a different block.
        let mut other = genesis_tx();
        if let Unsigned::CreateAsset(ref mut ca) = other.unsigned {
            ca.symbol = "OTH".into();
        }
        let other = Tx::new(other.unsigned.clone());
        let refused = Xvm::open(
            Genesis {
                network_id: NETWORK_ID,
                chain_id: chain_id(),
                net_id: ids::prefixed(&[0xAB]),
                fee_asset_id: other.id(),
                config: Config {
                    tx_fee: 0,
                    create_asset_tx_fee: 0,
                },
                txs: vec![other],
                timestamp: 1000,
            },
            Arc::new(FixedClock::new(1000)),
            a_salt(),
            Some(Arc::new(OneNet)),
            Some(Arc::new(NoMemory)),
            db,
        )
        .err()
        .expect("another chain's database must not open");
        assert!(
            matches!(&refused, ChainError::Storage(why) if why.contains("this database is chain")),
            "{refused:?}"
        );
    }

    #[test]
    fn a_chain_on_a_file_comes_back_from_the_file() {
        // The same restart, through a real file rather than a map: the frames
        // are written, flushed, and replayed by a second process's worth of
        // `Log::open`. Nothing above the database changes.
        let mut path = std::env::temp_dir();
        path.push(format!("lux-xvm-chain-{}.log", std::process::id()));
        std::fs::remove_file(&path).ok();

        let want = {
            let db: Arc<dyn crate::db::Db> = Arc::new(crate::db::Log::open(&path).unwrap());
            let (vm, g) = a_chain_on(db, 1000);
            vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
            let blk = vm.build().unwrap();
            vm.verify(&blk.id()).unwrap();
            vm.accept(&blk.id()).unwrap();
            blk.id()
        };
        assert!(std::fs::metadata(&path).unwrap().len() > 0, "it wrote");

        let db: Arc<dyn crate::db::Db> = Arc::new(crate::db::Log::open(&path).unwrap());
        let (again, _) = a_chain_on(db, 2000);
        assert_eq!(again.last_accepted(), want);
        assert_eq!(again.get(&want).unwrap().height(), 1);
        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn what_is_held_is_advertised_and_queued_to_be_passed_on() {
        let (vm, g, _) = a_chain(1000);
        let tx = spend_genesis(&g, 1_000, 2);
        let id = tx.id();
        assert!(!vm.holds(&id));
        vm.issue(tx.clone()).unwrap();

        // Held, exactly.
        assert!(vm.holds(&id));
        assert!(!vm.holds(&ids::prefixed(&[0xEE])));

        // Advertised: the filter a peer is sent has it, and the filter is
        // reconstructible from the bytes that travel.
        let (raw, salt) = vm.advertise();
        let peers = crate::gossip::Filter::parse(&raw, salt)
            .expect("a peer rebuilds the filter from what travelled");
        assert!(peers.has(&id), "the peer's copy holds what this node holds");

        // And queued to be pushed, as the same bytes a peer parses back.
        assert_eq!(vm.outbound(), 1);
        let out = vm.take_outbound(1 << 20);
        assert_eq!(out.len(), 1);
        assert_eq!(crate::gossip::unmarshal(&out[0]).unwrap().id(), id);
        assert_eq!(vm.outbound(), 0, "pushed once, not forever");

        // A block takes it, and then this node no longer holds it.
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();
        assert!(!vm.holds(&id));
    }

    #[test]
    fn a_peers_transaction_and_a_wallets_reach_one_pool_by_one_rule() {
        // The claim the single door is for: whichever way a transaction
        // arrives, the same refusals apply in the same order.
        let (vm, g, _) = a_chain(1000);
        let first = spend_genesis(&g, 1_000, 2);
        let conflicting = spend_genesis(&g, 1_000, 3);

        vm.issue(first.clone()).unwrap();
        // Offered again — held already.
        assert_eq!(vm.issue(first).unwrap_err(), ChainError::DuplicateTx);
        // A different transaction spending the same output — refused, and
        // remembered.
        assert_eq!(
            vm.issue(conflicting.clone()).unwrap_err(),
            ChainError::ConflictsWithOtherTx
        );
        // Offered a second time, the answer comes from memory: the chain is
        // not asked to verify it again.
        assert_eq!(
            vm.issue(conflicting.clone()).unwrap_err(),
            ChainError::ConflictsWithOtherTx
        );
        assert_eq!(
            vm.refusal(&conflicting.id()),
            Some(ChainError::ConflictsWithOtherTx)
        );
        assert_eq!(vm.pending(), 1);
    }

    #[test]
    fn a_chain_with_no_database_still_runs_and_writes_nothing() {
        // The memory-only path is the same path: `new` is `open` without a
        // device, and a commit it cannot make is not an error it reports.
        let (vm, g, _) = a_chain(1000);
        vm.issue(spend_genesis(&g, 1_000, 2)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();
        assert_eq!(vm.last_accepted(), blk.id());
    }

    #[test]
    fn a_chain_starts_at_its_genesis_block() {
        let (vm, _, _) = a_chain(1000);
        let last = vm.last_accepted();
        let blk = vm.get(&last).unwrap();
        assert_eq!(blk.height(), 0);
        assert_eq!(blk.id(), last);
        assert_eq!(blk.parent(), ids::EMPTY);
    }

    #[test]
    fn the_genesis_state_is_true_before_any_block_is_built() {
        let (vm, g, _) = a_chain(1000);
        // The transfer output the genesis transaction made is spendable.
        let utxo = vm.committed_utxo(&ids::prefix(&g.id(), &[1])).unwrap();
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
        assert!(vm.committed_utxo(&ids::prefix(&g.id(), &[1])).is_err());
        assert!(vm.committed_utxo(&ids::prefix(&tx.id(), &[0])).is_ok());
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
            t.base.ins[0].utxo_id = UtxoId::new(ids::prefixed(&[0xDD]), 0);
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
    fn the_same_transaction_offered_twice_is_held_once_and_the_second_is_told_why() {
        let (vm, g, _) = a_chain(1000);
        let tx = spend_genesis(&g, 1_000, 2);
        let id = tx.id();
        vm.issue(tx.clone()).unwrap();
        // Go answers `ErrDuplicateTx` rather than quietly succeeding: a wallet
        // that re-sent needs to know this node already has it, and a peer that
        // re-gossiped needs the same answer without the node executing it
        // twice.
        assert_eq!(vm.issue(tx).unwrap_err(), ChainError::DuplicateTx);
        assert_eq!(vm.pending(), 1);
        // Being held is not being dropped: the duplicate is refused, and the
        // one that is held keeps no refusal against its name.
        assert_eq!(vm.refusal(&id), None);
    }

    #[test]
    fn a_second_spend_of_one_output_is_refused_before_it_is_executed() {
        // The pool's conflict set, which the chain's own verification cannot
        // see: both of these verify against the SAME preferred state — nothing
        // has been accepted — and only one can ever go in a block.
        let (vm, g, _) = a_chain(1000);
        let first = spend_genesis(&g, 1_000, 2);
        let second = spend_genesis(&g, 1_000, 3);
        assert_ne!(first.id(), second.id(), "two different transactions");
        vm.issue(first).unwrap();
        assert_eq!(
            vm.issue(second.clone()).unwrap_err(),
            ChainError::ConflictsWithOtherTx
        );
        assert_eq!(vm.pending(), 1);
        // And the refusal is remembered, so the next peer to offer it is
        // answered from memory rather than by executing it again.
        assert_eq!(
            vm.refusal(&second.id()),
            Some(ChainError::ConflictsWithOtherTx)
        );
    }

    #[test]
    fn a_chain_told_to_hold_to_strict_post_quantum_admits_no_classical_spend() {
        // The X-Chain's three fx families all spend with a secp256k1 signature,
        // so under the strict profile with no exemption there is nothing this
        // chain can admit. That refusal is the point: a chain that took the
        // spend anyway would be post-quantum in its block format and classical
        // in what it accepts.
        let (vm, g, _) = a_chain(1000);
        vm.hold_to(crate::security::strict_pq(), None);
        assert_eq!(
            vm.profile().map(|p| p.which),
            Some(crate::security::Which::StrictPq)
        );
        let tx = spend_genesis(&g, 1_000, 2);
        let id = tx.id();
        assert_eq!(
            vm.issue(tx).unwrap_err(),
            ChainError::ClassicalCredentialRefused
        );
        assert_eq!(vm.pending(), 0);
        assert!(vm.refusal(&id).is_some(), "and this node remembers why");

        // The same transaction, on a chain told the permissive terms, goes in.
        let (other, g2, _) = a_chain(1000);
        other.hold_to(crate::security::permissive(), None);
        other.issue(spend_genesis(&g2, 1_000, 2)).unwrap();
        assert_eq!(other.pending(), 1);
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
            serde_json::json!(hex(&chain_id()))
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
        assert_eq!(issued["txID"], serde_json::json!(hex(&tx.id())));
        assert_eq!(
            vm.call("xvm.getPendingCount", &serde_json::json!({}))
                .unwrap(),
            serde_json::json!(1)
        );

        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();

        let got = vm
            .call("xvm.getTx", &serde_json::json!({ "txID": hex(&tx.id()) }))
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
        let utxo_id = ids::prefix(&g.id(), &[1]);
        let got = vm
            .call(
                "xvm.getUTXO",
                &serde_json::json!({ "utxoID": hex(&utxo_id) }),
            )
            .unwrap();
        assert_eq!(got["amount"], serde_json::json!(1_000));
        assert_eq!(got["assetID"], serde_json::json!(hex(&g.id())));
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
