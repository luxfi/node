// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain, behind the host's seam.
//!
//! One place decides what the tip is, what may follow it and what reaches the
//! device. Everything that advances this chain — accepting a block, and the
//! genesis seed — goes through [`Qvm::commit`], so a block cannot be applied
//! without being admitted or stored without being applied.

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use crate::block::Block;
use crate::config::Config;
use crate::error::{Error, Result};
use crate::host;
use crate::ids::{self, Id};
use crate::quantum::{Quantum, Stamp};
use crate::quasar::{Aggregate, Quasar, Setup, Sig};
use crate::store::{Entry, Memory, Store};
use crate::tx::{self, Pool, Tx};
use crate::wire::{self, MAX_BLOCK_SIZE, MAX_FUTURE_SKEW};
use crate::VERSION;

/// Where the tip pointer is kept.
///
/// Every key is a distinct length, so no two can collide: a block id is 32
/// bytes, a height index entry is 9, `lastAccepted` is 12 and `height` is 6.
const LAST_ACCEPTED: &[u8] = b"lastAccepted";
const TIP_HEIGHT: &[u8] = b"height";

/// The accepted block at a height, so a peer catching up asks by height and
/// gets an answer in one read rather than a walk back from the tip.
fn height_key(h: u64) -> Vec<u8> {
    let mut k = Vec::with_capacity(9);
    k.push(b'h');
    k.extend_from_slice(&h.to_be_bytes());
    k
}

/// What the VM is told at start-up.
pub struct Init {
    /// This node's identity — what a validator signature is attributed to.
    pub node: String,
    /// The chain this VM serves.
    pub chain: Id,
    /// The network that chain belongs to.
    pub network: u32,
    /// The genesis bytes the node was configured with. Q-Chain's genesis block
    /// is a constant of the chain, so these are recorded, not decoded into
    /// state.
    pub genesis: Vec<u8>,
    /// Where the chain is kept. A node hands a [`crate::store::Log`]; a test
    /// hands a [`Memory`].
    pub store: Arc<dyn Store>,
}

impl Init {
    /// A VM in memory, for a test.
    pub fn memory(node: &str, chain: Id, network: u32) -> Init {
        Init {
            node: node.to_string(),
            chain,
            network,
            genesis: Vec::new(),
            store: Arc::new(Memory::new()),
        }
    }
}

/// The node's clock, which a test can hold still.
///
/// Block time is decided here and nowhere else, so a test that needs a
/// particular "now" sets one rather than sleeping.
#[derive(Debug, Default)]
pub struct Clock {
    fixed: Mutex<Option<i64>>,
}

impl Clock {
    /// Seconds since the epoch.
    pub fn now(&self) -> i64 {
        if let Some(t) = *self.fixed.lock().expect("clock") {
            return t;
        }
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0)
    }

    /// Hold the clock at `t`.
    pub fn set(&self, t: i64) {
        *self.fixed.lock().expect("clock") = Some(t);
    }

    /// Let it run again.
    pub fn release(&self) {
        *self.fixed.lock().expect("clock") = None;
    }
}

/// The Q-Chain.
pub struct Qvm {
    pub config: Config,
    chain: Id,
    network: u32,
    node: String,

    store: Arc<dyn Store>,
    quantum: Quantum,
    quasar: Quasar,
    pool: Pool,
    pub clock: Clock,

    /// Blocks this node knows and has not accepted: what it built and what it
    /// parsed off the wire. The seam names a block by id, so there has to be
    /// somewhere to look one up before it is stored.
    proposed: Mutex<HashMap<Id, Arc<Block>>>,

    /// Serializes the tip: one writer, so two accepts cannot both extend it.
    tip_lock: RwLock<()>,
    down: AtomicBool,
}

impl Qvm {
    /// Start the chain: check the config, mint the signer, open the committee,
    /// and make sure there is a block to answer the frontier query with.
    pub fn new(mut config: Config, init: Init) -> Result<Qvm> {
        config.validate()?;

        // The node's own identity, which is what a validator signature is
        // attributed to, and the chain it serves, which is what its blocks
        // belong to. A VM with neither runs, signs as nobody, and produces
        // blocks every other Q-Chain will also accept — so it does not run.
        if init.node.is_empty() || ids::is_empty(&init.chain) {
            return Err(Error::NoIdentity);
        }

        let quantum = Quantum::new(
            config.quantum_algorithm_version,
            config.quantum_stamp_window,
        )?;

        // The committee counts DISTINCT validator ids, so this is the NODE's
        // id, not the chain's. Naming the chain here gave every node on Q-Chain
        // the same signer identity: each peer's signature arrived as a
        // duplicate of the first, the count never passed one, and quantum
        // finality was unreachable on any threshold above 1.
        let quasar = Quasar::new(Setup {
            validator: init.node.clone(),
            committee: config.committee,
        })?;

        let vm = Qvm {
            pool: Pool::new(config.max_parallel_txs),
            config,
            chain: init.chain,
            network: init.network,
            node: init.node,
            store: init.store,
            quantum,
            quasar,
            clock: Clock::default(),
            proposed: Mutex::new(HashMap::new()),
            tip_lock: RwLock::new(()),
            down: AtomicBool::new(false),
        };
        vm.seed_genesis()?;
        Ok(vm)
    }

    /// The committee this node signs in.
    pub fn quasar(&self) -> &Quasar {
        &self.quasar
    }

    /// The signer this chain stamps and checks under.
    pub fn quantum(&self) -> &Quantum {
        &self.quantum
    }

    /// What is pending.
    pub fn pool(&self) -> &Pool {
        &self.pool
    }

    /// This node's identity.
    pub fn node(&self) -> &str {
        &self.node
    }

    /// The chain this VM serves.
    pub fn chain(&self) -> Id {
        self.chain
    }

    pub fn network(&self) -> u32 {
        self.network
    }

    // ---- fees -------------------------------------------------------------

    /// The user-transaction admission point, which admits nothing.
    ///
    /// Q-Chain has NO user-payable blockspace: finality-cert inclusion is a
    /// validator obligation paid through P-Chain reward distribution, never a
    /// user fee (LP-0130 §6). A fee market on Q would make finality hostage to
    /// blockspace pricing — the exact failure that rule eliminates — so every
    /// amount is refused and there is no path from here into the pool.
    /// Consensus-internal aggregation reaches [`Qvm::admit`] directly and never
    /// passes this way.
    pub fn issue_tx(&self, tx: &Tx) -> Result<()> {
        Err(Error::FeeRefused(tx.fee))
    }

    /// The consensus-internal admission path: a certificate aggregation putting
    /// its attestation into the pool.
    pub fn admit(&self, tx: Arc<Tx>) -> Result<()> {
        self.pool.add(tx)
    }

    // ---- the chain --------------------------------------------------------

    /// The id of the last accepted block.
    ///
    /// Answers the empty id with no error for exactly one reason — the chain
    /// holds no block at all — and an error for every other reason it could not
    /// read one. Collapsing those is how a single failed read destroys a chain:
    /// the genesis seed reads the empty id as "fresh chain" and writes genesis,
    /// so one transient error at boot commits genesis over a live tip and
    /// start-up returns success.
    pub fn tip(&self) -> Result<Id> {
        match self.store.get(LAST_ACCEPTED) {
            Ok(raw) => ids::from_slice(&raw).ok_or_else(|| {
                Error::TipUnreadable(format!("last accepted holds {} bytes", raw.len()))
            }),
            Err(Error::NotFound) => Ok(ids::EMPTY),
            Err(e) => Err(Error::TipUnreadable(e.to_string())),
        }
    }

    /// The height of the last accepted block, or 0 on a chain that holds only
    /// genesis.
    ///
    /// A read that FAILED and a height of zero are different facts, and this
    /// reports them differently. Answering 0 for both is what let one transient
    /// failure look like a chain that had not started: the next block was built
    /// at height 1 on a chain a thousand blocks long.
    pub fn tip_height(&self) -> Result<u64> {
        match self.store.get(TIP_HEIGHT) {
            Ok(raw) => {
                if raw.len() != 8 {
                    return Err(Error::TipUnreadable(format!(
                        "height holds {} bytes",
                        raw.len()
                    )));
                }
                Ok(u64::from_be_bytes(raw[..8].try_into().unwrap()))
            }
            Err(Error::NotFound) => Ok(0),
            Err(e) => Err(Error::TipUnreadable(e.to_string())),
        }
    }

    /// A block by id: what this node has proposed or what it has stored.
    pub fn block(&self, id: &Id) -> Result<Arc<Block>> {
        if let Some(b) = self.proposed.lock().expect("proposed").get(id) {
            return Ok(Arc::clone(b));
        }
        self.stored(id)
    }

    /// A block from the store only.
    fn stored(&self, id: &Id) -> Result<Arc<Block>> {
        match self.store.get(id) {
            Ok(raw) => Ok(Arc::new(wire::parse_block(&raw)?)),
            Err(Error::NotFound) => Err(Error::NotFound),
            Err(e) => Err(e),
        }
    }

    /// What this node accepted at a height, from the index written in the same
    /// commit as the block itself.
    pub fn block_id_at(&self, height: u64) -> Result<Id> {
        match self.store.get(&height_key(height)) {
            Ok(raw) => ids::from_slice(&raw).ok_or(Error::NoBlockAtHeight(height)),
            Err(_) => Err(Error::NoBlockAtHeight(height)),
        }
    }

    /// Build a block from what is pending.
    pub fn build(&self) -> Result<Arc<Block>> {
        let built = self.build_inner();
        // Whatever this call leaves behind has to wake a builder again. The
        // latch holds ONE signal: two transactions arriving together wake one
        // build, and work the batch limit left over would otherwise sit there
        // until some unrelated transaction happened to arrive.
        self.pool.signal_if_work();

        let block = built?;
        // Signing reaches the committee and waits on it, so it happens with no
        // tip lock held — a verify arriving meanwhile must not queue behind it.
        self.sign_with_quasar(&block);
        Ok(block)
    }

    fn build_inner(&self) -> Result<Arc<Block>> {
        if self.down.load(Ordering::SeqCst) {
            return Err(Error::ShuttingDown);
        }
        let _tip = self.tip_lock.write().expect("tip");

        let pending = self.pool.pending(self.config.parallel_batch_size);
        if pending.is_empty() {
            return Err(Error::NoPendingTxs);
        }

        let (valid, rejected) = tx::triage(&self.quantum, &self.config, &pending);

        // A transaction that cannot verify now will not verify later — a stamp
        // only gets staler. Left in place it holds a pool slot for good, and
        // enough of them fill the pool and stop the chain accepting anything.
        for bad in &rejected {
            // It came out of this pool a moment ago, so the removal is the
            // reverse of a step that just happened.
            let _ = self.pool.remove(&bad.id());
        }
        if valid.is_empty() {
            return Err(Error::NoneSurvived);
        }

        let parent_id = self.tip()?;
        let parent = self.stored(&parent_id)?;

        // Never stamp behind the parent, and never stamp where verify will
        // refuse it. A clock that trails the tip by less than the skew
        // allowance is a peer that ran fast, and clamping forward covers it. A
        // clock that trails by MORE is this node's clock being wrong: every
        // block it could build now carries a timestamp its own verify rejects
        // for exceeding now+skew, so it says so rather than producing blocks
        // nobody — itself included — accepts.
        let now = self.clock.now();
        if parent.timestamp > now + MAX_FUTURE_SKEW {
            return Err(Error::ClockBehindTip {
                tip: parent.timestamp,
                now,
            });
        }
        let timestamp = now.max(parent.timestamp);

        let block = Arc::new(Block::new(
            timestamp,
            parent.height + 1,
            parent_id,
            self.chain,
            self.network,
            valid,
        ));
        if block.bytes().len() > MAX_BLOCK_SIZE {
            return Err(Error::TooLarge {
                bytes: block.bytes().len(),
                limit: MAX_BLOCK_SIZE,
            });
        }

        self.proposed
            .lock()
            .expect("proposed")
            .insert(block.id(), Arc::clone(&block));
        Ok(block)
    }

    /// Read a block off the wire and remember it by id.
    ///
    /// It does not need the parent: a bootstrapping node parses blocks whose
    /// parents it does not have yet.
    pub fn parse(&self, raw: &[u8]) -> Result<Arc<Block>> {
        let block = Arc::new(wire::parse_block(raw)?);
        self.proposed
            .lock()
            .expect("proposed")
            .insert(block.id(), Arc::clone(&block));
        Ok(block)
    }

    /// Decide whether a block may be built on, changing nothing.
    pub fn verify(&self, id: &Id) -> Result<()> {
        let _tip = self.tip_lock.read().expect("tip");
        let block = self.block(id)?;

        block.on_chain(self.chain, self.network)?;
        block.well_formed()?;

        let parent = self
            .block(&block.parent)
            .map_err(|_| Error::ParentNotFound(block.parent))?;
        block.follows(&parent, self.clock.now())?;

        if self.config.quantum_stamp_enabled {
            block.stamps_verify(&self.quantum)?;
        }
        Ok(())
    }

    /// Make the block the tip.
    ///
    /// Nothing is dropped from the pool until that succeeds: evicting first
    /// would lose the transactions of a block that then failed to persist.
    pub fn accept(&self, id: &Id) -> Result<()> {
        let _tip = self.tip_lock.write().expect("tip");
        let block = self.block(id)?;
        self.commit(&block)?;

        for tx in &block.txs {
            let _ = self.pool.remove(&tx.id());
        }
        self.proposed.lock().expect("proposed").remove(id);
        Ok(())
    }

    /// Where a block stands.
    ///
    /// Stored is accepted: the commit is the only writer, and it moves the
    /// block and the tip pointer together. A block this node holds and has not
    /// committed is still being decided; one it has never seen is unknown.
    pub fn status(&self, id: &Id) -> host::Status {
        if self.store.has(id).unwrap_or(false) {
            return host::Status::Accepted;
        }
        if self.proposed.lock().expect("proposed").contains_key(id) {
            return host::Status::Processing;
        }
        host::Status::Unknown
    }

    /// Drop a block that lost.
    ///
    /// Nothing ran — execution belongs to accept — and its transactions are
    /// still in the pool, because building COPIES from the queue rather than
    /// draining it and only accepting removes anything. So there is nothing to
    /// undo and nothing to give back; what there is to do is stop holding the
    /// block, which is what this does.
    pub fn reject(&self, id: &Id) -> Result<()> {
        self.proposed.lock().expect("proposed").remove(id);
        Ok(())
    }

    /// Admit the block, apply it, store it, index it by height and move the
    /// tip — in that order, in one place, as ONE commit.
    ///
    /// A commit EXTENDS the tip: the block names the last accepted block as its
    /// parent and sits one height above it. Without that, any block that merely
    /// verified could be committed — a verified sibling of an old block rewound
    /// the chain to its height, left every height above indexed to an abandoned
    /// branch, and served those to bootstrapping peers as canonical.
    /// Re-accepting a block already accepted did the same, and two siblings
    /// both verified, both committed, the second silently replacing the first.
    ///
    /// The writes then land as ONE batch: block, height index and tip pointer
    /// move together or not at all, so a node never restarts holding a tip
    /// pointer to a block it did not store.
    fn commit(&self, b: &Block) -> Result<()> {
        b.on_chain(self.chain, self.network)?;
        self.extends_tip(b)?;
        b.apply()?;

        let writes: Vec<Entry> = vec![
            (b.id().to_vec(), b.bytes().to_vec()),
            (height_key(b.height), b.id().to_vec()),
            (LAST_ACCEPTED.to_vec(), b.id().to_vec()),
            (TIP_HEIGHT.to_vec(), b.height.to_be_bytes().to_vec()),
        ];
        self.store.commit(&writes)
    }

    /// Exactly one block may follow the one this node last accepted. An empty
    /// chain's tip is no block at no height, and the only block that follows it
    /// is genesis.
    fn extends_tip(&self, b: &Block) -> Result<()> {
        let tip = self.tip()?;
        if b.parent != tip {
            return Err(Error::NotTheTip(format!(
                "parent {}, tip {}",
                ids::hex(&b.parent),
                ids::hex(&tip)
            )));
        }
        let next = if ids::is_empty(&tip) {
            0
        } else {
            self.tip_height()? + 1
        };
        if b.height != next {
            return Err(Error::NotTheTip(format!(
                "height {}, tip expects {next}",
                b.height
            )));
        }
        Ok(())
    }

    /// Write the height-0 block on a chain that has none, so the VM can name a
    /// tip the moment it starts.
    ///
    /// A VM whose last-accepted is empty answers the bootstrap frontier query
    /// with no block, and an answer naming no block is not a responder: it
    /// neither backs a tip nor counts toward the response floor. Every node
    /// then reads every other node as silent, the beacon floor is unreachable,
    /// and each waits on the others for as long as the chain runs.
    ///
    /// The block is a CONSTANT of the chain it belongs to, so every node on
    /// that chain computes one id alone: fixed timestamp, height 0, empty
    /// parent, no transactions, this chain and this network. Wall-clock time
    /// here would give each node a different id for the same block and make the
    /// repair a fork.
    fn seed_genesis(&self) -> Result<()> {
        let tip = self.tip()?;
        if !ids::is_empty(&tip) {
            return Ok(());
        }
        let genesis = self.genesis_block();
        self.commit(&genesis)
    }

    /// The height-0 block of this chain. A pure function of the chain and the
    /// network, which is what makes it the same block on every node.
    pub fn genesis_block(&self) -> Block {
        Block::new(0, 0, ids::EMPTY, self.chain, self.network, Vec::new())
    }

    /// Sign the block for the CONSENSUS layer, which is where a block-level
    /// signature belongs.
    ///
    /// A failure here does not fail the build: the block is well-formed and the
    /// certificate is a separate object gathered over the following rounds.
    fn sign_with_quasar(&self, block: &Block) -> Option<Sig> {
        self.quasar
            .sign_block(block.id(), block.bytes(), block.height)
            .ok()
    }

    /// Attest to a message for the finality bridge: a committee signature when
    /// the block is one this node is tracking, an ML-DSA identity signature
    /// otherwise.
    pub fn stamp(&self, block: Id, height: u64, message: &[u8]) -> Result<Attestation> {
        if !ids::is_empty(&block) {
            if let Ok(sig) = self.quasar.sign_block(block, message, height) {
                return Ok(Attestation::Committee(sig));
            }
        }
        let key = self.quantum.generate()?;
        Ok(Attestation::Identity(self.quantum.sign(message, &key)?))
    }

    /// Check an attestation against the message it claims to cover.
    ///
    /// The MESSAGE is the argument that makes this a verification. Without it
    /// there is nothing to check a signature against, so each arm could only
    /// look at the attestation's own shape — and shape is what the sender
    /// chose: a two-byte aggregate declaring three signers passed, and so did a
    /// one-byte BLS signature. A self-declared signer count is not evidence of
    /// anything.
    pub fn verify_stamp(&self, message: &[u8], stamp: &Attestation) -> Result<()> {
        match stamp {
            Attestation::Committee(sig) => {
                if self.quasar.verify(message, sig) {
                    Ok(())
                } else {
                    Err(Error::UnverifiedSigner(sig.validator.clone()))
                }
            }
            Attestation::Quorum(agg) => {
                if self.quasar.verify_aggregate(message, agg) {
                    Ok(())
                } else {
                    Err(Error::AggregateRefused(ids::EMPTY))
                }
            }
            Attestation::Identity(sig) => self.quantum.verify(message, Some(sig)),
        }
    }

    /// Stop. Idempotent: a second call is a no-op, not a second teardown.
    pub fn shutdown(&self) {
        if self.down.swap(true, Ordering::SeqCst) {
            return;
        }
        self.pool.close();
    }

    /// Whether the chain is up.
    pub fn healthy(&self) -> bool {
        !self.down.load(Ordering::SeqCst)
    }

    /// Block until there is something to build, or `timeout` passes.
    ///
    /// Waiting on a timer alone would mean a build is never called and the
    /// chain never leaves genesis, however many transactions the pool has
    /// accepted.
    pub fn wait_for_work(&self, timeout: Duration) -> bool {
        self.pool.wait(timeout)
    }
}

/// What a node can say about a message.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Attestation {
    /// One committee member's signature.
    Committee(Sig),
    /// A quorum's.
    Quorum(Aggregate),
    /// This node's ML-DSA identity signature, when there is no committee
    /// statement to make.
    Identity(Stamp),
}

// ---- the host seam --------------------------------------------------------

impl host::Block for Block {
    fn id(&self) -> host::Id {
        Block::id(self)
    }

    fn parent(&self) -> host::Id {
        self.parent
    }

    fn height(&self) -> u64 {
        self.height
    }

    fn timestamp(&self) -> u64 {
        self.timestamp.max(0) as u64
    }

    fn bytes(&self) -> Vec<u8> {
        Block::bytes(self).to_vec()
    }

    /// What the chain looks like after this block.
    ///
    /// Q-Chain's state IS its chain of attestations, so the root that names it
    /// is the block's own id — the hash of everything the block holds, over a
    /// parent that is named inside those bytes. A certificate over this names
    /// exactly one history.
    fn state_root(&self) -> host::Id {
        Block::id(self)
    }

    /// What this block CARRIES: the transaction set, folded in order.
    ///
    /// Separate from the state root, because a certificate that named a state
    /// but not the payload that produced it would be a certificate two
    /// different blocks could satisfy.
    fn payload_root(&self) -> host::Id {
        let mut folded = Vec::with_capacity(self.txs.len() * 32);
        for tx in &self.txs {
            folded.extend_from_slice(&tx.id());
        }
        ids::hash(&folded)
    }
}

impl host::Vm for Qvm {
    fn name(&self) -> &'static str {
        "Q"
    }

    fn version(&self) -> String {
        VERSION.to_string()
    }

    fn build(&self) -> std::result::Result<Box<dyn host::Block>, host::Error> {
        let block = Qvm::build(self)?;
        Ok(Box::new((*block).clone()))
    }

    fn parse(&self, raw: &[u8]) -> std::result::Result<Box<dyn host::Block>, host::Error> {
        let block = Qvm::parse(self, raw)?;
        Ok(Box::new((*block).clone()))
    }

    fn get(&self, id: &host::Id) -> std::result::Result<Box<dyn host::Block>, host::Error> {
        let block = self.block(id)?;
        Ok(Box::new((*block).clone()))
    }

    fn verify(&self, id: &host::Id) -> std::result::Result<(), host::Error> {
        Ok(Qvm::verify(self, id)?)
    }

    fn accept(&self, id: &host::Id) -> std::result::Result<(), host::Error> {
        Ok(Qvm::accept(self, id)?)
    }

    fn reject(&self, id: &host::Id) -> std::result::Result<(), host::Error> {
        Ok(Qvm::reject(self, id)?)
    }

    /// Q-Chain finalizes on a verified threshold signature rather than on a
    /// preference, so there is no preferred branch to record: the only block
    /// that may follow the tip is the one that extends it.
    fn set_preference(&self, _id: &host::Id) -> std::result::Result<(), host::Error> {
        Ok(())
    }

    fn last_accepted(&self) -> host::Id {
        // The seam has nowhere to put a read failure, and a wrong id is worse
        // than none: the empty id is what a chain with no block answers, and a
        // node that cannot read its own tip is in exactly that position as far
        // as any caller can act on it.
        self.tip().unwrap_or(ids::EMPTY)
    }

    fn block_id_at(&self, height: u64) -> std::result::Result<host::Id, host::Error> {
        Ok(Qvm::block_id_at(self, height)?)
    }

    fn health(&self) -> std::result::Result<(), host::Error> {
        if self.healthy() {
            Ok(())
        } else {
            Err(host::Error::Invalid(Error::ShuttingDown.to_string()))
        }
    }

    fn call(
        &self,
        method: &str,
        params: &serde_json::Value,
    ) -> std::result::Result<serde_json::Value, host::Error> {
        match method {
            // One block, by id.
            "quantumvm.getBlock" => {
                let id = id_param(params, "blockID")?;
                let block = self.block(&id).map_err(host::Error::from)?;
                Ok(serde_json::json!({
                    "block": {
                        "id": ids::hex(&block.id()),
                        "parentID": ids::hex(&block.parent),
                        "height": block.height,
                    },
                    "height": block.height,
                    "timestamp": block.timestamp,
                    "txCount": block.txs.len(),
                    // Blocks carry no stamp: a signature inside a block would
                    // make its id depend on who signed it.
                    "quantumSig": false,
                }))
            }

            // A fresh validator identity key. The method name is the one on the
            // wire; what it mints is an ML-DSA identity, not a Corona share.
            "quantumvm.generateCoronaKey" => {
                if !self.config.corona_enabled {
                    return Err(host::Error::BadRequest(
                        "corona keys are not enabled".into(),
                    ));
                }
                let key = self.quantum.generate().map_err(host::Error::from)?;
                Ok(serde_json::json!({
                    "publicKey": to_hex(&key.public),
                    "version": key.version,
                    "keySize": key.public.len(),
                }))
            }

            // Whether a signature checks out over a message.
            "quantumvm.verifyQuantumSignature" => {
                if !self.config.quantum_stamp_enabled {
                    return Err(host::Error::BadRequest(
                        "quantum signatures are not enabled".into(),
                    ));
                }
                let message = params
                    .get("message")
                    .and_then(|v| v.as_str())
                    .ok_or_else(|| host::Error::BadRequest("message is required".into()))?;
                let sig = stamp_param(params)?;
                let algorithm = sig.algorithm;
                let valid = self.quantum.verify(message.as_bytes(), Some(&sig)).is_ok();
                Ok(serde_json::json!({ "valid": valid, "algorithm": algorithm }))
            }

            "quantumvm.getPendingTransactions" => {
                let limit = params
                    .get("limit")
                    .and_then(|v| v.as_u64())
                    .unwrap_or(0)
                    .min(100) as usize;
                let limit = if limit == 0 { 100 } else { limit };
                let txs = self.pool.pending(limit);
                let listed: Vec<serde_json::Value> = txs
                    .iter()
                    .map(|tx| {
                        serde_json::json!({
                            "id": ids::hex(&tx.id()),
                            "timestamp": tx.timestamp,
                        })
                    })
                    .collect();
                Ok(serde_json::json!({ "count": listed.len(), "transactions": listed }))
            }

            "quantumvm.getHealth" => Ok(serde_json::json!({
                "healthy": self.healthy(),
                "version": VERSION,
                "quantumEnabled": self.config.quantum_stamp_enabled,
                "coronaEnabled": self.config.corona_enabled,
                "pendingTxCount": self.pool.len(),
                "parallelWorkers": self.config.max_parallel_txs,
            })),

            // What actually governs the chain. No fee schedule, because
            // Q-Chain charges none (LP-0130 §6) — a number nothing reads is a
            // price the chain does not charge, and reporting one tells
            // operators otherwise.
            "quantumvm.getConfig" => Ok(serde_json::json!({
                "maxParallelTxs": self.config.max_parallel_txs,
                "quantumAlgorithmVersion": self.config.quantum_algorithm_version,
                "quantumStampEnabled": self.config.quantum_stamp_enabled,
                "coronaEnabled": self.config.corona_enabled,
                "parallelBatchSize": self.config.parallel_batch_size,
            })),

            // The committee, and how many of it a block needs.
            "quantumvm.getCommittee" => Ok(serde_json::json!({
                "validator": self.node,
                "committee": self.quasar.committee(),
                "registered": self.quasar.registered(),
                "threshold": self.quasar.threshold(),
            })),

            other => Err(host::Error::NoMethod(other.to_string())),
        }
    }
}

fn to_hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

fn from_hex(s: &str) -> Option<Vec<u8>> {
    if !s.len().is_multiple_of(2) {
        return None;
    }
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok())
        .collect()
}

fn id_param(params: &serde_json::Value, name: &str) -> std::result::Result<Id, host::Error> {
    let raw = params
        .get(name)
        .and_then(|v| v.as_str())
        .ok_or_else(|| host::Error::BadRequest(format!("{name} is required")))?;
    ids::from_hex(raw).ok_or_else(|| host::Error::BadRequest(format!("{name} is not an id")))
}

fn stamp_param(params: &serde_json::Value) -> std::result::Result<Stamp, host::Error> {
    let sig = params
        .get("signature")
        .ok_or_else(|| host::Error::BadRequest("signature is required".into()))?;
    let field = |name: &str| -> std::result::Result<Vec<u8>, host::Error> {
        let raw = sig
            .get(name)
            .and_then(|v| v.as_str())
            .ok_or_else(|| host::Error::BadRequest(format!("signature.{name} is required")))?;
        from_hex(raw).ok_or_else(|| host::Error::BadRequest(format!("signature.{name} is not hex")))
    };
    Ok(Stamp {
        algorithm: sig
            .get("algorithm")
            .and_then(|v| v.as_u64())
            .unwrap_or_default() as u32,
        stamped: sig
            .get("timestamp")
            .and_then(|v| v.as_i64())
            .ok_or_else(|| host::Error::BadRequest("signature.timestamp is required".into()))?,
        public_key: field("publicKey")?,
        signature: field("signature")?,
        quantum_stamp: field("quantumStamp")?,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::store::Log;

    fn chain() -> Id {
        ids::filled(30)
    }

    fn vm() -> Qvm {
        Qvm::new(Config::default(), Init::memory("node-0", chain(), 1)).unwrap()
    }

    fn stamped(vm: &Qvm, nonce: u64) -> Arc<Tx> {
        let key = vm.quantum.generate().unwrap();
        let mut tx = Tx::new(vm.clock.now(), nonce, b"attestation".to_vec());
        tx.stamp = Some(vm.quantum.sign(tx.bytes(), &key).unwrap());
        Arc::new(tx)
    }

    #[test]
    fn a_chain_answers_the_frontier_query_the_moment_it_starts() {
        let vm = vm();
        let tip = vm.tip().unwrap();
        assert!(!ids::is_empty(&tip));
        assert_eq!(vm.tip_height().unwrap(), 0);
        let genesis = vm.block(&tip).unwrap();
        assert_eq!(genesis.height, 0);
        assert_eq!(genesis.timestamp, 0);
        assert!(genesis.txs.is_empty());
        assert_eq!(vm.block_id_at(0).unwrap(), tip);
    }

    #[test]
    fn genesis_is_a_constant_of_the_chain_and_not_of_the_moment() {
        let a = vm();
        let b = vm();
        assert_eq!(a.tip().unwrap(), b.tip().unwrap());

        // A different chain, or a different network, is a different genesis.
        let other_chain = Qvm::new(
            Config::default(),
            Init::memory("node-0", ids::filled(31), 1),
        )
        .unwrap();
        assert_ne!(a.tip().unwrap(), other_chain.tip().unwrap());
        let other_net = Qvm::new(Config::default(), Init::memory("node-0", chain(), 2)).unwrap();
        assert_ne!(a.tip().unwrap(), other_net.tip().unwrap());
    }

    #[test]
    fn a_vm_with_no_identity_or_no_chain_does_not_run() {
        assert!(matches!(
            Qvm::new(Config::default(), Init::memory("", chain(), 1)),
            Err(Error::NoIdentity)
        ));
        assert!(matches!(
            Qvm::new(Config::default(), Init::memory("node-0", ids::EMPTY, 1)),
            Err(Error::NoIdentity)
        ));
    }

    #[test]
    fn a_user_transaction_is_refused_whatever_it_offers() {
        let vm = vm();
        for fee in [0u64, 1, u64::MAX] {
            let tx = Tx::new(1000, 1, b"x".to_vec()).offering(fee);
            assert!(matches!(vm.issue_tx(&tx), Err(Error::FeeRefused(_))));
        }
        assert!(vm.pool.is_empty(), "nothing reached the pool");
    }

    #[test]
    fn building_takes_the_batch_and_leaves_the_rest_for_the_next_block() {
        let config = Config {
            parallel_batch_size: 2,
            ..Config::default()
        };
        let vm = Qvm::new(config, Init::memory("node-0", chain(), 1)).unwrap();
        for n in 0..3 {
            vm.admit(stamped(&vm, n)).unwrap();
        }
        let block = vm.build().unwrap();
        assert_eq!(block.txs.len(), 2);
        assert_eq!(block.height, 1);
        assert_eq!(block.parent, vm.tip().unwrap());
        // The leftover re-armed the builder rather than waiting for an
        // unrelated arrival.
        assert!(vm.wait_for_work(Duration::from_millis(200)));
    }

    #[test]
    fn a_built_block_verifies_accepts_and_becomes_the_tip() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let block = vm.build().unwrap();
        vm.verify(&block.id()).unwrap();
        vm.accept(&block.id()).unwrap();

        assert_eq!(vm.tip().unwrap(), block.id());
        assert_eq!(vm.tip_height().unwrap(), 1);
        assert_eq!(vm.block_id_at(1).unwrap(), block.id());
        assert!(vm.pool.is_empty(), "an accepted block leaves the pool");
        // And it is readable from the store, not just from memory.
        assert_eq!(vm.stored(&block.id()).unwrap().id(), block.id());
    }

    #[test]
    fn nothing_to_build_is_not_a_failure_it_is_an_empty_answer() {
        let vm = vm();
        assert!(matches!(vm.build(), Err(Error::NoPendingTxs)));
        let err: host::Error = Error::NoPendingTxs.into();
        assert_eq!(err, host::Error::Empty);
    }

    #[test]
    fn a_block_whose_transactions_all_failed_is_never_built() {
        let vm = vm();
        let mut bad = (*stamped(&vm, 1)).clone();
        vm.admit(Arc::new(bad.clone())).unwrap();
        bad.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        // Replace the pool's copy with the forged one.
        let id = bad.id();
        vm.pool.remove(&id).unwrap();
        vm.pool.add(Arc::new(bad)).unwrap();

        assert!(matches!(vm.build(), Err(Error::NoneSurvived)));
        // …and the transaction that will never verify is gone rather than
        // holding its slot for good.
        assert!(vm.pool.is_empty());
    }

    #[test]
    fn a_block_that_lost_leaves_its_transactions_in_the_pool() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let block = vm.build().unwrap();
        vm.reject(&block.id()).unwrap();
        assert_eq!(vm.pool.len(), 1, "rejecting gives nothing back to undo");
        // The block is no longer held, so the id no longer names anything.
        assert!(vm.block(&block.id()).is_err());
        // And the transaction can go into the next block.
        let again = vm.build().unwrap();
        assert_eq!(again.txs.len(), 1);
    }

    #[test]
    fn only_the_block_that_extends_the_tip_is_committed() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let first = vm.build().unwrap();
        vm.accept(&first.id()).unwrap();

        // Re-accepting what is already the tip does not rewind anything.
        vm.proposed
            .lock()
            .unwrap()
            .insert(first.id(), Arc::clone(&first));
        assert!(matches!(vm.accept(&first.id()), Err(Error::NotTheTip(_))));
        assert_eq!(vm.tip().unwrap(), first.id());

        // A sibling of an old block is not a continuation of the chain.
        let sibling = Arc::new(Block::new(
            first.timestamp,
            1,
            first.parent,
            chain(),
            1,
            first.txs.clone(),
        ));
        vm.proposed
            .lock()
            .unwrap()
            .insert(sibling.id(), Arc::clone(&sibling));
        assert!(matches!(vm.accept(&sibling.id()), Err(Error::NotTheTip(_))));
        assert_eq!(vm.tip().unwrap(), first.id());
        assert_eq!(vm.block_id_at(1).unwrap(), first.id());
    }

    #[test]
    fn a_block_of_another_chain_is_refused_even_if_it_is_otherwise_perfect() {
        let vm = vm();
        let stranger = Arc::new(Block::new(
            vm.clock.now(),
            1,
            vm.tip().unwrap(),
            ids::filled(31),
            1,
            vec![stamped(&vm, 1)],
        ));
        vm.proposed
            .lock()
            .unwrap()
            .insert(stranger.id(), Arc::clone(&stranger));
        assert!(matches!(
            vm.verify(&stranger.id()),
            Err(Error::ForeignChain { .. })
        ));
        assert!(matches!(
            vm.accept(&stranger.id()),
            Err(Error::ForeignChain { .. })
        ));
    }

    #[test]
    fn a_node_whose_clock_trails_its_own_tip_says_so_rather_than_building() {
        let vm = vm();
        vm.clock.set(10_000);
        vm.admit(stamped(&vm, 1)).unwrap();
        let block = vm.build().unwrap();
        vm.accept(&block.id()).unwrap();

        // A clock inside the allowance clamps forward to the parent's time.
        vm.clock.set(10_000 - MAX_FUTURE_SKEW + 1);
        vm.admit(stamped(&vm, 2)).unwrap();
        let clamped = vm.build().unwrap();
        assert_eq!(clamped.timestamp, block.timestamp);
        vm.reject(&clamped.id()).unwrap();

        // A clock further behind than that is this node's clock being wrong.
        vm.clock.set(1_000);
        assert!(matches!(vm.build(), Err(Error::ClockBehindTip { .. })));
    }

    /// The signature check runs on a block this node did NOT build, over the
    /// transactions the PARSER produced.
    ///
    /// This is the failure that has bitten this estate before: a check bound to
    /// a field the parser skipped, or handed a verifier nothing supplied, runs
    /// on locally built blocks and never on received ones — which is every
    /// block but one, for every block. Here the forged block is serialized,
    /// parsed back by a second node, and refused by that node's verify.
    #[test]
    fn a_received_block_with_a_forged_stamp_is_refused_by_the_node_that_received_it() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let honest = vm.build().unwrap();

        let mut forged = (*honest.txs[0]).clone();
        forged.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        let bad = Block::new(
            honest.timestamp,
            honest.height,
            honest.parent,
            chain(),
            1,
            vec![Arc::new(forged)],
        );

        let peer = Qvm::new(Config::default(), Init::memory("node-1", chain(), 1)).unwrap();
        peer.clock.set(vm.clock.now());
        let parsed = peer.parse(bad.bytes()).unwrap();
        assert!(
            matches!(peer.verify(&parsed.id()), Err(Error::BlockSignature(_))),
            "a block whose stamps do not check out was verified"
        );

        // The honest one, over the same path, does verify — so the refusal
        // above is the signature and not the path.
        let good = peer.parse(honest.bytes()).unwrap();
        peer.verify(&good.id()).unwrap();
    }

    /// And the check is a CONFIGURED switch, not an accident of the code path:
    /// with stamps off it does not run, and the config is the only thing that
    /// says so.
    #[test]
    fn the_stamp_check_runs_unless_the_config_says_otherwise() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let honest = vm.build().unwrap();
        let mut forged = (*honest.txs[0]).clone();
        forged.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        let bad = Block::new(
            honest.timestamp,
            honest.height,
            honest.parent,
            chain(),
            1,
            vec![Arc::new(forged)],
        );

        let off = Qvm::new(
            Config {
                quantum_stamp_enabled: false,
                ..Config::default()
            },
            Init::memory("node-2", chain(), 1),
        )
        .unwrap();
        off.clock.set(vm.clock.now());
        let parsed = off.parse(bad.bytes()).unwrap();
        off.verify(&parsed.id()).unwrap();
        assert!(!off.config.quantum_stamp_enabled);
    }

    #[test]
    fn a_block_off_the_wire_is_verified_the_same_way_one_built_here_is() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let built = vm.build().unwrap();
        let wire = built.bytes().to_vec();

        // A second node, which never saw the build.
        let peer = Qvm::new(Config::default(), Init::memory("node-1", chain(), 1)).unwrap();
        peer.clock.set(vm.clock.now());
        let parsed = peer.parse(&wire).unwrap();
        assert_eq!(parsed.id(), built.id());
        peer.verify(&parsed.id()).unwrap();
        peer.accept(&parsed.id()).unwrap();
        // Two nodes, one chain: the peer's tip is the block this node built.
        assert_eq!(peer.tip().unwrap(), built.id());
        assert_eq!(peer.tip_height().unwrap(), 1);
    }

    #[test]
    fn a_block_whose_parent_is_not_held_is_refused_rather_than_guessed_at() {
        let vm = vm();
        let orphan = Arc::new(Block::new(
            vm.clock.now(),
            1,
            ids::filled(77),
            chain(),
            1,
            vec![stamped(&vm, 1)],
        ));
        vm.proposed
            .lock()
            .unwrap()
            .insert(orphan.id(), Arc::clone(&orphan));
        assert!(matches!(
            vm.verify(&orphan.id()),
            Err(Error::ParentNotFound(_))
        ));
    }

    #[test]
    fn the_chain_comes_back_where_it_left_off() {
        let mut path = std::env::temp_dir();
        path.push(format!(
            "quantumvm-vm-{}-{:?}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));

        let (tip, height, block_wire) = {
            let vm = Qvm::new(
                Config::default(),
                Init {
                    node: "node-0".into(),
                    chain: chain(),
                    network: 1,
                    genesis: Vec::new(),
                    store: Arc::new(Log::open(&path).unwrap()),
                },
            )
            .unwrap();
            vm.admit(stamped(&vm, 1)).unwrap();
            let block = vm.build().unwrap();
            vm.accept(&block.id()).unwrap();
            (
                vm.tip().unwrap(),
                vm.tip_height().unwrap(),
                block.bytes().to_vec(),
            )
        };

        // A new process, the same disk.
        let again = Qvm::new(
            Config::default(),
            Init {
                node: "node-0".into(),
                chain: chain(),
                network: 1,
                genesis: Vec::new(),
                store: Arc::new(Log::open(&path).unwrap()),
            },
        )
        .unwrap();
        assert_eq!(again.tip().unwrap(), tip, "the tip survived the restart");
        assert_eq!(again.tip_height().unwrap(), height);
        assert_eq!(again.block(&tip).unwrap().bytes(), block_wire.as_slice());
        assert_eq!(again.block_id_at(1).unwrap(), tip);
        // And genesis was not written a second time over the live tip.
        assert_eq!(
            again.block_id_at(0).unwrap(),
            again.block(&tip).unwrap().parent
        );

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn the_seam_is_the_node_s_own() {
        let vm = vm();
        let seam: &dyn host::Vm = &vm;
        assert_eq!(seam.name(), "Q");
        assert_eq!(seam.version(), VERSION);
        assert_eq!(seam.last_accepted(), vm.tip().unwrap());
        seam.health().unwrap();
        assert!(matches!(seam.build(), Err(host::Error::Empty)));

        vm.admit(stamped(&vm, 1)).unwrap();
        let block = seam.build().unwrap();
        let id = block.id();
        seam.verify(&id).unwrap();
        seam.accept(&id).unwrap();
        assert_eq!(seam.last_accepted(), id);
        assert_eq!(seam.block_id_at(1).unwrap(), id);

        let again = seam.parse(&block.bytes()).unwrap();
        assert_eq!(again.id(), id);
        assert_eq!(again.state_root(), id);
        assert_ne!(again.payload_root(), ids::EMPTY);
    }

    /// A store that answers a read with a failure, or refuses to commit. Both
    /// are the states a real device gets into, and both are what the chain has
    /// to tell apart from "there is nothing here".
    struct Broken {
        inner: Memory,
        read_fails: AtomicBool,
        write_fails: AtomicBool,
        bad_height: AtomicBool,
    }

    impl Broken {
        fn new() -> Broken {
            Broken {
                inner: Memory::new(),
                read_fails: AtomicBool::new(false),
                write_fails: AtomicBool::new(false),
                bad_height: AtomicBool::new(false),
            }
        }
    }

    impl Store for Broken {
        fn get(&self, key: &[u8]) -> Result<Vec<u8>> {
            if self.read_fails.load(Ordering::SeqCst) {
                return Err(Error::Store("the device did not answer".into()));
            }
            if self.bad_height.load(Ordering::SeqCst) && key == TIP_HEIGHT {
                return Ok(vec![0, 0]);
            }
            self.inner.get(key)
        }

        fn commit(&self, writes: &[Entry]) -> Result<()> {
            if self.write_fails.load(Ordering::SeqCst) {
                return Err(Error::Store("the device did not take the write".into()));
            }
            self.inner.commit(writes)
        }
    }

    // Go: TestAReadThatFailedIsNotAChainThatIsEmpty. A failed read reported as
    // "no block" is how one transient error commits genesis over a live chain.
    #[test]
    fn a_read_that_failed_is_not_a_chain_that_is_empty() {
        let store = Arc::new(Broken::new());
        let vm = Qvm::new(
            Config::default(),
            Init {
                node: "node-0".into(),
                chain: chain(),
                network: 1,
                genesis: Vec::new(),
                store: Arc::clone(&store) as Arc<dyn Store>,
            },
        )
        .unwrap();
        let live = vm.tip().unwrap();

        store.read_fails.store(true, Ordering::SeqCst);
        assert!(matches!(vm.tip(), Err(Error::TipUnreadable(_))));
        assert!(matches!(vm.tip_height(), Err(Error::TipUnreadable(_))));

        // …and a VM booting against it refuses rather than seeding a second
        // genesis over the chain that is there.
        assert!(Qvm::new(
            Config::default(),
            Init {
                node: "node-0".into(),
                chain: chain(),
                network: 1,
                genesis: Vec::new(),
                store: Arc::clone(&store) as Arc<dyn Store>,
            },
        )
        .is_err());

        store.read_fails.store(false, Ordering::SeqCst);
        assert_eq!(vm.tip().unwrap(), live, "the chain was left alone");
    }

    // Go: TestAMalformedHeightIsNotAHeightOfZero.
    #[test]
    fn a_malformed_height_is_not_a_height_of_zero() {
        let store = Arc::new(Broken::new());
        let vm = Qvm::new(
            Config::default(),
            Init {
                node: "node-0".into(),
                chain: chain(),
                network: 1,
                genesis: Vec::new(),
                store: Arc::clone(&store) as Arc<dyn Store>,
            },
        )
        .unwrap();
        store.bad_height.store(true, Ordering::SeqCst);
        assert!(matches!(vm.tip_height(), Err(Error::TipUnreadable(_))));
    }

    // Go: TestInitializeRefusesAStoreItCannotCommitTo.
    #[test]
    fn a_store_that_cannot_take_a_write_stops_the_boot() {
        let store = Arc::new(Broken::new());
        store.write_fails.store(true, Ordering::SeqCst);
        assert!(matches!(
            Qvm::new(
                Config::default(),
                Init {
                    node: "node-0".into(),
                    chain: chain(),
                    network: 1,
                    genesis: Vec::new(),
                    store: store as Arc<dyn Store>,
                },
            ),
            Err(Error::Store(_))
        ));
    }

    // Go: TestAcceptLeavesNothingBehindWhenAWriteFails +
    // TestAcceptKeepsTheMempoolWhenTheWriteFails. A block that could not be
    // persisted did not happen: the tip does not move, the height index gains
    // nothing, and the transactions are still there to go into the next one.
    #[test]
    fn a_block_that_could_not_be_persisted_did_not_happen() {
        let store = Arc::new(Broken::new());
        let vm = Qvm::new(
            Config::default(),
            Init {
                node: "node-0".into(),
                chain: chain(),
                network: 1,
                genesis: Vec::new(),
                store: Arc::clone(&store) as Arc<dyn Store>,
            },
        )
        .unwrap();
        let genesis = vm.tip().unwrap();

        vm.admit(stamped(&vm, 1)).unwrap();
        let block = vm.build().unwrap();

        store.write_fails.store(true, Ordering::SeqCst);
        assert!(matches!(vm.accept(&block.id()), Err(Error::Store(_))));
        assert_eq!(vm.tip().unwrap(), genesis, "the tip did not move");
        assert_eq!(vm.tip_height().unwrap(), 0);
        assert!(vm.block_id_at(1).is_err(), "nothing was indexed");
        assert_eq!(vm.pool.len(), 1, "the transactions are still there");
        assert_eq!(vm.status(&block.id()), host::Status::Processing);

        // And the write that failed staged nothing for the next one: when the
        // device comes back, accepting is a clean single commit.
        store.write_fails.store(false, Ordering::SeqCst);
        vm.accept(&block.id()).unwrap();
        assert_eq!(vm.tip().unwrap(), block.id());
        assert_eq!(vm.status(&block.id()), host::Status::Accepted);
        assert!(vm.pool.is_empty());
    }

    // Go: TestStoreKeysCannotCollide. Every key is a distinct length, so no
    // block id can be read as the tip pointer or as a height index entry.
    #[test]
    fn no_two_store_keys_can_collide() {
        let mut lengths = vec![
            32,                  // a block id
            height_key(0).len(), // a height index entry
            LAST_ACCEPTED.len(), // the tip pointer
            TIP_HEIGHT.len(),    // the tip's height
        ];
        lengths.sort_unstable();
        lengths.dedup();
        assert_eq!(lengths.len(), 4, "two keys share a length");
        assert_eq!(height_key(0).len(), height_key(u64::MAX).len());
    }

    // Go: TestTheTransactionBlobIsNotMutable. What the parser produced does not
    // change when the buffer it was read from does — otherwise a peer could
    // rewrite a block this node had already verified.
    #[test]
    fn a_parsed_block_does_not_share_the_buffer_it_came_from() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        let built = vm.build().unwrap();
        let mut wire = built.bytes().to_vec();

        let parsed = wire::parse_block(&wire).unwrap();
        let before = parsed.txs[0].bytes().to_vec();
        for b in wire.iter_mut() {
            *b ^= 0xFF;
        }
        assert_eq!(parsed.txs[0].bytes(), before.as_slice());
        assert_eq!(parsed.id(), built.id());
    }

    // Go: TestVerifyDoesNotDeadlockAgainstABuilder. Verify takes the tip lock
    // for reading and a builder takes it for writing; a verify that re-entered
    // it would wait behind a writer that is waiting for it.
    #[test]
    fn verifying_does_not_deadlock_against_a_builder() {
        let vm = Arc::new(vm());
        for n in 0..8 {
            vm.admit(stamped(&vm, n)).unwrap();
        }
        let block = vm.build().unwrap();

        let verifier = {
            let vm = Arc::clone(&vm);
            let id = block.id();
            std::thread::spawn(move || {
                for _ in 0..50 {
                    let _ = vm.verify(&id);
                }
            })
        };
        let builder = {
            let vm = Arc::clone(&vm);
            std::thread::spawn(move || {
                for _ in 0..50 {
                    let _ = vm.build();
                }
            })
        };
        verifier.join().unwrap();
        builder.join().unwrap();
        vm.verify(&block.id()).unwrap();
    }

    // Go: TestServiceRepliesCarryNoSecrets. A key mint answers with the public
    // half; nothing an RPC returns may carry the private one.
    #[test]
    fn no_rpc_reply_carries_a_secret() {
        let vm = vm();
        let seam: &dyn host::Vm = &vm;
        let reply = seam
            .call("quantumvm.generateCoronaKey", &serde_json::json!({}))
            .unwrap();
        let text = reply.to_string();
        for field in ["privateKey", "secret", "secretKey", "nonce"] {
            assert!(!text.contains(field), "the reply carries {field}: {text}");
        }
        // The public key is 1952 bytes; a secret would be longer, and the reply
        // says how long what it returned is.
        assert_eq!(reply["keySize"], 1952);
    }

    #[test]
    fn a_shut_down_chain_builds_nothing_and_says_it_is_unhealthy() {
        let vm = vm();
        vm.admit(stamped(&vm, 1)).unwrap();
        vm.shutdown();
        vm.shutdown(); // idempotent
        assert!(!vm.healthy());
        assert!(matches!(vm.build(), Err(Error::ShuttingDown)));
        let seam: &dyn host::Vm = &vm;
        assert!(seam.health().is_err());
    }

    #[test]
    fn the_rpc_answers_what_the_chain_actually_holds() {
        let vm = vm();
        let seam: &dyn host::Vm = &vm;
        vm.admit(stamped(&vm, 1)).unwrap();
        let block = vm.build().unwrap();
        vm.accept(&block.id()).unwrap();

        let got = seam
            .call(
                "quantumvm.getBlock",
                &serde_json::json!({ "blockID": ids::hex(&block.id()) }),
            )
            .unwrap();
        assert_eq!(got["height"], 1);
        assert_eq!(got["txCount"], 1);
        assert_eq!(got["quantumSig"], false);
        assert_eq!(got["block"]["parentID"], ids::hex(&block.parent));

        let health = seam
            .call("quantumvm.getHealth", &serde_json::json!({}))
            .unwrap();
        assert_eq!(health["healthy"], true);
        assert_eq!(health["version"], VERSION);

        let config = seam
            .call("quantumvm.getConfig", &serde_json::json!({}))
            .unwrap();
        assert_eq!(config["quantumAlgorithmVersion"], 2);
        assert!(
            config.get("fee").is_none() && config.get("txFee").is_none(),
            "Q-Chain charges no fee, so it reports no fee schedule"
        );

        let committee = seam
            .call("quantumvm.getCommittee", &serde_json::json!({}))
            .unwrap();
        assert_eq!(committee["threshold"], 3);
        assert_eq!(committee["committee"], 4);

        let key = seam
            .call("quantumvm.generateCoronaKey", &serde_json::json!({}))
            .unwrap();
        assert_eq!(key["version"], 2);
        assert_eq!(key["keySize"], 1952);

        assert!(matches!(
            seam.call("quantumvm.nothingLikeThis", &serde_json::json!({})),
            Err(host::Error::NoMethod(_))
        ));
    }

    #[test]
    fn the_rpc_checks_a_signature_against_the_message_it_names() {
        let vm = vm();
        let seam: &dyn host::Vm = &vm;
        let key = vm.quantum.generate().unwrap();
        let sig = vm.quantum.sign(b"a round digest", &key).unwrap();
        let as_json = serde_json::json!({
            "message": "a round digest",
            "signature": {
                "algorithm": sig.algorithm,
                "timestamp": sig.stamped,
                "publicKey": to_hex(&sig.public_key),
                "signature": to_hex(&sig.signature),
                "quantumStamp": to_hex(&sig.quantum_stamp),
            }
        });
        let got = seam
            .call("quantumvm.verifyQuantumSignature", &as_json)
            .unwrap();
        assert_eq!(got["valid"], true);

        let mut other = as_json.clone();
        other["message"] = serde_json::json!("a different digest");
        let got = seam
            .call("quantumvm.verifyQuantumSignature", &other)
            .unwrap();
        assert_eq!(got["valid"], false, "a signature over other bytes");
    }

    #[test]
    fn a_stamp_is_checked_against_the_message_and_not_against_its_own_shape() {
        let vm = vm();
        let attestation = vm.stamp(ids::filled(3), 1, b"round digest").unwrap();
        vm.verify_stamp(b"round digest", &attestation).unwrap();
        assert!(vm.verify_stamp(b"another digest", &attestation).is_err());

        // A hand-made "aggregate" declaring a quorum it does not have.
        let claimed = Attestation::Quorum(Aggregate {
            bls: vec![0; 96],
            validators: vec!["a".into(), "b".into(), "c".into()],
        });
        assert!(vm.verify_stamp(b"round digest", &claimed).is_err());
    }
}
