// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The F-Chain: the coordination plane for confidential compute.
//!
//! F records the PUBLIC coordinates of encrypted values — a handle, the digest
//! of the off-chain ciphertext body, its owner, the capabilities granted over
//! it, and the threshold decryptions asked for and answered — and it records
//! nothing else. It holds no ciphertext body, no FHE secret key and no
//! decryption share; [`crate::record`] says how that line is held.
//!
//! Mutating operations take effect only through fee-settled consensus blocks,
//! priced by a per-scheme gas schedule and burned from the payer's on-chain
//! balance. Nothing changes state through a synchronous call.
//!
//! EVERY TIMESTAMP F WRITES COMES FROM THE ACCEPTING BLOCK, never from a
//! validator's wall clock. Two validators replaying one block therefore write
//! the same bytes. That is not enforced by a state root — F commits none — so
//! it is a property of where the number comes from, and the only clock read
//! anywhere else is this node's own, for deciding whether a block's timestamp
//! is one it will accept.
//!
//! ONE HOME FOR THE STATE. Everything the chain knows lives in the
//! [`crate::store::Store`], and every read goes to it. The reference keeps
//! in-memory caches beside the database and reloads them after a rollback;
//! caches are an optimisation with a second copy of the truth in them, and the
//! rollback path is exactly where a second copy goes wrong. Here a rollback is
//! [`Store::abort`] and there is nothing else to put back.

use std::collections::BTreeMap;

use crate::batch::Batch;
use crate::block::{Block, MAX_FUTURE_SKEW};
use crate::error::{Code, Error, Result};
use crate::fee;
use crate::gas;
use crate::id::{self, Account, Id, EMPTY};
use crate::json::{self, Fields};
use crate::record::{Ciphertext, Decrypt, Epoch, Member, Permit, EPOCH_ACTIVE};
use crate::store::Store;
use crate::tx::{self, Transaction};
use crate::wire::{self, MAX_BLOCK_SIZE, MAX_BLOCK_TXS, TX_ENTRY};

/// The version this chain reports.
pub const VERSION: &str = "1.0.0";

/// The chain's name.
pub const NAME: &str = "fhevm";

// Namespaces.
const CIPHERTEXT_PREFIX: &[u8] = b"ct:";
const PERMIT_PREFIX: &[u8] = b"pm:";
const DECRYPT_PREFIX: &[u8] = b"dr:";
const EPOCH_PREFIX: &[u8] = b"ep:";
const BLOCK_PREFIX: &[u8] = b"block:";
const NONCE_PREFIX: &[u8] = b"nonce:";
const HEIGHT_PREFIX: &[u8] = b"height:";

const LAST_ACCEPTED_KEY: &[u8] = b"fhevm/last-accepted";
const GENESIS_MARKER: &[u8] = b"fhevm/genesis-applied";
const CURRENT_EPOCH_KEY: &[u8] = b"fhevm/current-epoch";

/// What the queue may hold. Admission is open to anyone who can pay, so without
/// a bound the queue is whatever an adversary chooses to make it.
pub const MAX_MEMPOOL: usize = 4096;

/// Where this node's own time comes from.
///
/// It is read in exactly two places — deciding a proposal's timestamp, and
/// deciding whether a peer's is one this node will verify — and never to stamp
/// a record. A test pins it so a run is reproducible; a node reads the machine.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Clock {
    #[default]
    System,
    At(i64),
}

impl Clock {
    pub fn now(&self) -> i64 {
        match self {
            Clock::System => std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0),
            Clock::At(t) => *t,
        }
    }
}

/// Which network and which chain this is.
///
/// There is no default for the chain id. Every signature F accepts and every
/// block id it computes is bound to it, so a chain with no identity would share
/// both with every other chain that also had none.
#[derive(Clone, Copy, Debug)]
pub struct Config {
    pub network_id: u32,
    pub chain_id: Id,
    pub clock: Clock,
}

impl Default for Config {
    fn default() -> Config {
        Config { network_id: 0, chain_id: EMPTY, clock: Clock::System }
    }
}

/// The chain a chain is born from: a funding allocation and the epoch-0
/// committee. There is no ciphertext in genesis — ciphertexts are registered
/// through consensus transactions — so genesis carries nothing encrypted.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Genesis {
    pub version: i64,
    pub message: String,
    pub timestamp: i64,
    pub alloc: Vec<(Account, u64)>,
    pub committee: Vec<Member>,
    pub threshold: i64,
    pub public_key: Vec<u8>,
}

impl Genesis {
    /// Read a genesis.
    ///
    /// A member the schema does not describe is IGNORED here, unlike in an
    /// operation payload. That is the reference's rule and the difference is
    /// deliberate: a payload is an unauthenticated stranger's bytes that the
    /// chain will persist verbatim, and a genesis is the configuration this
    /// node was started with. Refusing an unknown member there would mean a
    /// node could not read a genesis written by a newer one.
    pub fn parse(b: &[u8]) -> Result<Genesis> {
        if b.is_empty() {
            return Ok(Genesis::default());
        }
        let mut f = Fields::open(json::value(b, "genesis")?, "genesis")?;
        let g = Genesis {
            version: f.int("version")?,
            message: f.string("message")?,
            timestamp: f.i64("timestamp")?,
            alloc: f.hex_amounts("alloc")?,
            committee: f.list("committee", "[]fhe.CommitteeMember", |mut m| {
                let member = Member {
                    node_id: m.node_id("node_id")?,
                    public_key: m.base64("public_key")?,
                    weight: m.u64("weight")?,
                    index: m.int("index")?,
                };
                // A member of a member is still schema, and the reference reads
                // one through the same struct either way.
                m.done()?;
                Ok(member)
            })?,
            threshold: f.int("threshold")?,
            public_key: f.base64("publicKey")?,
        };
        f.ignore_rest();
        Ok(g)
    }
}

/// A running F-Chain.
pub struct Vm {
    config: Config,
    store: Store,

    /// The queue. It holds the transactions and nothing derived from them:
    /// which effects are claimed and which nonce each payer has reached are
    /// READ OFF it, so neither can drift away from the queue it describes.
    mempool: Vec<Transaction>,

    /// Blocks verified above the accepted tip and not yet decided, so a child
    /// can resolve one as its parent.
    pending: BTreeMap<Id, Block>,

    genesis_block: Block,
    last_accepted: Id,
    height: u64,
}

impl Vm {
    /// Stand a chain up on `genesis`.
    pub fn new(config: Config, genesis: &[u8]) -> Result<Vm> {
        if config.chain_id == EMPTY {
            return Err(Error::detail(Code::InvalidBlock, "no chain id"));
        }
        let g = Genesis::parse(genesis)?;
        let genesis_block =
            Block::new(&config.chain_id, EMPTY, 0, g.timestamp, Vec::new());
        let mut vm = Vm {
            config,
            store: Store::new(),
            mempool: Vec::new(),
            pending: BTreeMap::new(),
            last_accepted: genesis_block.id(),
            height: 0,
            genesis_block,
        };
        vm.seed(&g)?;
        vm.reload()?;
        Ok(vm)
    }

    /// Credit the funding allocation and install the epoch-0 committee, once.
    /// It is the only trusted state mutation; every later one goes through a
    /// block.
    fn seed(&mut self, g: &Genesis) -> Result<()> {
        if self.store.has(GENESIS_MARKER) {
            return Ok(());
        }
        for (acct, amount) in &g.alloc {
            fee::credit(&mut self.store, acct, *amount)?;
        }
        // A committee in genesis goes through EXACTLY the check an advance
        // applies, so a chain cannot be born with a committee that consensus
        // would refuse to install later.
        if !g.committee.is_empty() {
            tx::validate_committee(&g.committee, g.threshold, &g.public_key)?;
            let epoch = Epoch {
                epoch: 0,
                start_time: g.timestamp,
                end_time: 0,
                committee: g.committee.clone(),
                threshold: g.threshold,
                public_key: g.public_key.clone(),
                status: EPOCH_ACTIVE,
                attestations: Vec::new(),
            };
            self.put_epoch(&epoch);
            self.store.put(CURRENT_EPOCH_KEY, &0u64.to_be_bytes());
        }
        let genesis_id = self.genesis_block.id();
        self.store.put(&height_key(0), &genesis_id);
        self.store.put(GENESIS_MARKER, &[1]);
        self.store.commit();
        Ok(())
    }

    /// Re-read the tip from what is committed. This is the whole of a rollback:
    /// state lives in one place, so putting it back is putting back one thing.
    fn reload(&mut self) -> Result<()> {
        match self.read_fixed::<32>(LAST_ACCEPTED_KEY)? {
            None => {
                self.last_accepted = self.genesis_block.id();
                self.height = 0;
            }
            Some(tip) => {
                self.last_accepted = tip;
                self.height = self
                    .block(&tip)?
                    .ok_or_else(|| {
                        Error::detail(
                            Code::InvalidBlock,
                            format!("last accepted block {} is missing", id::hex(&tip)),
                        )
                    })?
                    .height();
            }
        }
        Ok(())
    }

    // ---- what the chain is ----

    pub fn chain_id(&self) -> &Id {
        &self.config.chain_id
    }

    pub fn network_id(&self) -> u32 {
        self.config.network_id
    }

    pub fn store(&self) -> &Store {
        &self.store
    }

    pub fn now(&self) -> i64 {
        self.config.clock.now()
    }

    pub fn last_accepted(&self) -> Id {
        self.last_accepted
    }

    pub fn height(&self) -> u64 {
        self.height
    }

    pub fn mempool(&self) -> &[Transaction] {
        &self.mempool
    }

    /// The block at `height`, from the index written in the same commit as the
    /// block itself — so the index cannot name a block the chain did not
    /// accept.
    pub fn block_id_at(&self, height: u64) -> Result<Id> {
        self.read_fixed::<32>(&height_key(height))?.ok_or_else(|| {
            Error::detail(Code::NotOnTip, format!("no block at height {height}"))
        })
    }

    // ---- the records ----

    fn read_fixed<const N: usize>(&self, key: &[u8]) -> Result<Option<[u8; N]>> {
        // A read that FAILED is not a read that found nothing. Conflating the
        // two is how a live chain reads as a fresh one: a bad last-accepted
        // pointer would leave the tip at genesis and the height at zero, and
        // the node would build height 1 over a chain already at height 2.
        match self.store.get(key) {
            None => Ok(None),
            Some(b) if b.len() == N => Ok(Some(b.try_into().unwrap())),
            Some(b) => Err(Error::detail(
                Code::InvalidPayload,
                format!("{:?} is {} bytes, not {}", String::from_utf8_lossy(key), b.len(), N),
            )),
        }
    }

    pub fn ciphertext(&self, handle: &[u8; 32]) -> Option<Ciphertext> {
        self.store.get(&keyed(CIPHERTEXT_PREFIX, handle)).and_then(Ciphertext::decode)
    }

    pub fn put_ciphertext(&mut self, rec: &Ciphertext) {
        self.store.put(&keyed(CIPHERTEXT_PREFIX, &rec.handle), &rec.encode());
    }

    pub fn permit(&self, permit: &[u8; 32]) -> Option<Permit> {
        self.store.get(&keyed(PERMIT_PREFIX, permit)).and_then(Permit::decode)
    }

    pub fn put_permit(&mut self, rec: &Permit) {
        self.store.put(&keyed(PERMIT_PREFIX, &rec.permit_id), &rec.encode());
    }

    pub fn decrypt(&self, request: &[u8; 32]) -> Option<Decrypt> {
        self.store.get(&keyed(DECRYPT_PREFIX, request)).and_then(Decrypt::decode)
    }

    pub fn put_decrypt(&mut self, rec: &Decrypt) {
        self.store.put(&keyed(DECRYPT_PREFIX, &rec.request_id), &rec.encode());
    }

    pub fn epoch(&self, n: u64) -> Option<Epoch> {
        self.store.get(&keyed(EPOCH_PREFIX, &n.to_be_bytes())).and_then(Epoch::decode)
    }

    pub fn put_epoch(&mut self, rec: &Epoch) {
        self.store.put(&keyed(EPOCH_PREFIX, &rec.epoch.to_be_bytes()), &rec.encode());
    }

    pub fn epoch_number(&self) -> u64 {
        self.read_fixed::<8>(CURRENT_EPOCH_KEY)
            .ok()
            .flatten()
            .map(u64::from_be_bytes)
            .unwrap_or(0)
    }

    /// The sitting epoch.
    ///
    /// A chain whose genesis declared no committee has an epoch 0 with no
    /// members, which authorizes NOBODY — so fulfilment and epoch advance are
    /// refused until a committee exists, rather than accepted from anyone.
    pub fn current_epoch(&self) -> Epoch {
        let n = self.epoch_number();
        self.epoch(n).unwrap_or(Epoch { epoch: n, status: EPOCH_ACTIVE, ..Epoch::default() })
    }

    pub fn set_current_epoch(&mut self, n: u64) {
        self.store.put(CURRENT_EPOCH_KEY, &n.to_be_bytes());
    }

    /// The payer's last-used nonce; the next valid one is this plus one.
    ///
    /// An account that has never transacted has nonce 0. A read that FAILED is
    /// an error and not a zero, because a zero here says "this payer has spent
    /// nothing" and lets every transaction it ever signed through again.
    pub fn nonce_of(&self, payer: &Account) -> Result<u64> {
        Ok(self.read_fixed::<8>(&keyed(NONCE_PREFIX, payer))?.map(u64::from_be_bytes).unwrap_or(0))
    }

    pub fn set_nonce(&mut self, payer: &Account, n: u64) {
        self.store.put(&keyed(NONCE_PREFIX, payer), &n.to_be_bytes());
    }

    pub fn balance(&self, acct: &Account) -> Result<u64> {
        fee::balance(&self.store, acct)
    }

    pub fn burned(&self) -> Result<u64> {
        fee::burned(&self.store)
    }

    // ---- admission ----

    /// The highest nonce this payer already has queued.
    fn queued(&self, payer: &Account) -> Option<u64> {
        self.mempool.iter().filter(|t| t.payer == *payer).map(|t| t.nonce).max()
    }

    fn claimed(&self, effect: &[u8; 32]) -> bool {
        self.mempool.iter().any(|t| t.effect() == *effect)
    }

    /// Admit a transaction to this node's queue.
    ///
    /// Admission is a FILTER, not a consensus rule: it refuses what this node
    /// can already tell will not work, so the queue stays useful. The rules
    /// consensus enforces live in [`Batch::admit`], and the fee is SETTLED
    /// later, in accept — never here.
    ///
    /// The nonce rule is the one consensus applies, counted over what is
    /// already queued. The looser "greater than the last committed one" let a
    /// payer queue a gap: admission took it, verification refused the block for
    /// it, and since a failed verification drops a block rather than rejecting
    /// it, the queue behind it went too.
    pub fn submit(&mut self, tx: Transaction) -> Result<Id> {
        tx.syntactic_verify()?;
        tx.authenticate(&self.config.chain_id)?;
        let amount = gas::fee_for(&tx)?;

        if self.mempool.len() >= MAX_MEMPOOL {
            return Err(Error::new(Code::MempoolFull));
        }
        // The replay guard runs BEFORE authorization, so a replayed transaction
        // is reported as replayed whatever else is true of it.
        let committed = self.nonce_of(&tx.payer)?;
        let mut want = committed + 1;
        if let Some(q) = self.queued(&tx.payer) {
            if q >= want {
                want = q + 1;
            }
        }
        if tx.nonce != want {
            return Err(Error::new(Code::BadNonce));
        }
        // Refuse a second claim on an effect already queued. Without it a payer
        // could enqueue two transactions that each pass every check alone and
        // together spend a block on work only one of them can do.
        if self.claimed(&tx.effect()) {
            return Err(Error::new(Code::DuplicateEffect));
        }
        tx.check_auth(self, self.now())?;
        fee::can_pay(&self.store, &tx.payer, amount)?;

        let id = tx.id();
        self.mempool.push(tx);
        Ok(id)
    }

    // ---- building ----

    /// Propose a block from the queue.
    ///
    /// It SELECTS rather than drains: a transaction stays queued until the
    /// block carrying it is accepted, so an engine that discards a proposal —
    /// which it may do without ever rejecting it — cannot take the queue with
    /// it. Selection runs the same admission verification runs and skips what
    /// does not fit, so a proposer cannot build a block its own verification
    /// would reject and one unfit transaction no longer takes the rest down.
    pub fn build(&mut self) -> Result<Block> {
        let parent = self.block_of(&self.last_accepted)?;

        // Chain time never runs backwards, so a proposer whose clock has not
        // moved since its own tip advances by the smallest step instead of
        // proposing a block its own verification would refuse. A clock so far
        // behind that even that step lands outside the skew allowance is a
        // broken clock, and such a node declines to propose rather than produce
        // a block nobody can verify.
        let mut ts = self.now();
        if ts <= parent.timestamp() {
            ts = parent.timestamp() + 1;
        }
        if ts > self.now() + MAX_FUTURE_SKEW {
            return Err(Error::new(Code::ClockBehind));
        }

        if self.mempool.is_empty() {
            return Err(Error::new(Code::NoPendingTxs));
        }

        // Selection stops at whichever bound comes first: the count a block may
        // carry, or the bytes it may occupy. Stopping only on the count would
        // let a thousand ordinary transactions — each carrying an ML-DSA-65 key
        // and signature — build a block this node's own parser refuses.
        let queued = self.mempool.clone();
        let mut batch = Batch::new();
        let mut picked: Vec<Transaction> = Vec::new();
        let mut size = wire::empty_block_size();
        for tx in &queued {
            if picked.len() == MAX_BLOCK_TXS {
                break;
            }
            let grown = size + tx.bytes().len() + TX_ENTRY;
            if grown > MAX_BLOCK_SIZE {
                break;
            }
            if batch.admit(self, tx).is_err() {
                continue;
            }
            picked.push(tx.clone());
            size = grown;
        }
        if picked.is_empty() {
            return Err(Error::new(Code::NoPendingTxs));
        }

        let block = Block::new(
            &self.config.chain_id,
            self.last_accepted,
            parent.height() + 1,
            ts,
            picked,
        );
        self.track(block.clone());
        Ok(block)
    }

    // ---- blocks ----

    /// Read a block off the wire. It must not need the parent: a bootstrapping
    /// node parses blocks whose parents it does not have yet.
    pub fn parse(&self, raw: &[u8]) -> Result<Block> {
        let f = wire::parse_block(raw)?;
        Ok(Block::new(
            &self.config.chain_id,
            f.parent,
            f.height,
            f.timestamp,
            f.transactions,
        ))
    }

    /// A block by id, from what is in flight, the tip, or the store.
    pub fn block(&self, id: &Id) -> Result<Option<Block>> {
        if let Some(b) = self.pending.get(id) {
            return Ok(Some(b.clone()));
        }
        if *id == self.genesis_block.id() {
            return Ok(Some(self.genesis_block.clone()));
        }
        match self.store.get(&keyed(BLOCK_PREFIX, id)) {
            None => Ok(None),
            Some(b) => self.parse(b).map(Some),
        }
    }

    fn block_of(&self, id: &Id) -> Result<Block> {
        self.block(id)?.ok_or_else(|| Error::new(Code::NoParentBlock))
    }

    /// Whether a block whose parent is `parent` extends the chain: the parent
    /// is the accepted tip, or a block that verified above it and is still in
    /// flight.
    ///
    /// Height alone is NOT this check. A block whose parent is an OLD accepted
    /// block satisfies height == parent+1 perfectly well, and accepting it
    /// rewinds the chain and leaves the height index naming an orphan as
    /// canonical.
    fn on_tip(&self, parent: &Id) -> Result<()> {
        if *parent == self.last_accepted || self.pending.contains_key(parent) {
            return Ok(());
        }
        Err(Error::detail(
            Code::NotOnTip,
            format!(
                "parent {} is neither the tip {}",
                id::hex(&parent[..4]),
                id::hex(&self.last_accepted[..4])
            ),
        ))
    }

    /// Make a block findable by id while it is in flight, and prune what is
    /// already decided.
    ///
    /// Nothing else will prune: the engine may drop a block it never accepts
    /// and never rejects, so a tracker that only grew would leak. Anything at
    /// or below the last accepted height is decided or orphaned, which bounds
    /// this to the blocks actually in flight above it.
    fn track(&mut self, b: Block) {
        if b.height() <= self.height {
            return;
        }
        let decided: Vec<Id> = self
            .pending
            .iter()
            .filter(|(_, blk)| blk.height() <= self.height)
            .map(|(id, _)| *id)
            .collect();
        for id in decided {
            self.pending.remove(&id);
        }
        self.pending.insert(b.id(), b);
    }

    /// Check a block can be accepted WITHOUT mutating state: it sits correctly
    /// on its parent in height and time, and every transaction is well formed,
    /// authenticated, correctly ordered and paid for.
    ///
    /// It does NOT decide authorization. That verdict depends on state earlier
    /// transactions in the same block may change, so a block-time verdict can
    /// differ from the application-time one — and a block every validator
    /// certifies and no validator can apply halts the chain. Authorization is
    /// decided once, in accept, where failing it reverts the one transaction
    /// instead of the block.
    pub fn verify(&mut self, b: &Block) -> Result<()> {
        if b.height() == 0 {
            return Err(Error::detail(Code::InvalidBlock, "genesis is not a proposed block"));
        }
        let parent = self.block(&b.parent())?.ok_or_else(|| {
            Error::detail(Code::NotOnTip, format!("verify parent: {}", id::hex(&b.parent()[..4])))
        })?;
        // Chain time and height are the parent's, advanced. Without this a
        // proposer picks both freely: it can rewind time to revive an expired
        // permit, or jump forward to expire everything at once.
        if b.height() != parent.height() + 1 {
            return Err(Error::detail(
                Code::InvalidBlock,
                format!("height {} does not follow parent {}", b.height(), parent.height()),
            ));
        }
        if b.timestamp() < parent.timestamp() {
            return Err(Error::detail(
                Code::InvalidBlock,
                format!("timestamp {} precedes parent {}", b.timestamp(), parent.timestamp()),
            ));
        }
        if b.timestamp() > self.now() + MAX_FUTURE_SKEW {
            return Err(Error::detail(
                Code::InvalidBlock,
                format!("timestamp {} is beyond the skew allowance", b.timestamp()),
            ));
        }
        if b.transactions().is_empty() {
            return Err(Error::detail(Code::InvalidBlock, "empty block"));
        }
        if b.transactions().len() > MAX_BLOCK_TXS {
            return Err(Error::detail(
                Code::InvalidBlock,
                format!("{} transactions exceeds {}", b.transactions().len(), MAX_BLOCK_TXS),
            ));
        }
        // A block this node built in memory is held to the same size a block
        // off the wire is, so a proposer cannot produce one its own peers
        // refuse to parse.
        let n = b.bytes().len();
        if n > MAX_BLOCK_SIZE {
            return Err(Error::detail(
                Code::InvalidBlock,
                format!("{n} bytes exceeds {MAX_BLOCK_SIZE}"),
            ));
        }

        self.on_tip(&b.parent())?;
        let mut batch = Batch::new();
        for tx in b.transactions() {
            batch.admit(self, tx)?;
        }

        // A block that verifies is one the engine may build on, so it has to be
        // findable by id — including one parsed from a peer rather than built
        // here. Tracking only self-built blocks left a follower able to verify
        // one block and not its child.
        self.track(b.clone());
        Ok(())
    }

    /// Settle and apply the block atomically.
    ///
    /// For each transaction it METERS the operation's gas, BURNS the fee from
    /// the payer, then APPLIES the state effect — all through one buffer,
    /// committed exactly once. Any failure aborts the whole block: no partial
    /// application, no unpaid operation.
    pub fn accept(&mut self, b: &Block) -> Result<()> {
        // A block extends the tip or it is not accepted. Verification reached
        // the same verdict earlier, against the tip AT THAT TIME; between the
        // two the chain moves, and this is what decides. Without it the height
        // index, the last-accepted pointer and the height would be written for
        // a block on an abandoned branch — the chain rewinds, and every peer
        // bootstrapping from the index is served an orphan as canonical.
        if b.parent() != self.last_accepted {
            return Err(Error::detail(
                Code::NotOnTip,
                format!(
                    "parent {} is not the accepted tip {}",
                    id::hex(&b.parent()[..4]),
                    id::hex(&self.last_accepted[..4])
                ),
            ));
        }

        if let Err(e) = self.settle(b) {
            self.abort()?;
            return Err(e);
        }

        let id = b.id();
        self.store.put(&keyed(BLOCK_PREFIX, &id), &b.bytes());
        self.store.put(&height_key(b.height()), &id);
        self.store.put(LAST_ACCEPTED_KEY, &id);
        self.store.commit();

        self.last_accepted = id;
        self.height = b.height();
        self.pending.remove(&id);
        self.release(b.transactions());
        Ok(())
    }

    /// Burn each fee and apply each effect.
    ///
    /// A transaction that fails AUTHORIZATION here REVERTS rather than aborting
    /// the block: its fee is burned, its nonce is consumed, and its state
    /// effect does not happen. Every validator reaches that verdict from the
    /// same committed state in the same order, so a reverted transaction is not
    /// a disagreement — and the alternative, aborting, would mean a block every
    /// validator certified and no validator could apply, which halts the chain.
    /// The payer pays for the block space it used either way.
    ///
    /// The error return is therefore reserved for what a validator genuinely
    /// cannot proceed past: a failed burn, or a nonce verification should have
    /// caught. Those abort the block and roll it back whole.
    fn settle(&mut self, b: &Block) -> Result<()> {
        let now = b.timestamp();
        for tx in b.transactions() {
            // Reads the buffer, so earlier transactions in this same block are
            // seen.
            let committed = self.nonce_of(&tx.payer)?;
            if tx.nonce != committed + 1 {
                return Err(Error::new(Code::BadNonce));
            }
            let gas_used = gas::gas_for(tx)?;
            let mut meter = fee::Meter::new(tx.gas_limit);
            meter.consume(gas_used)?;
            let amount = fee::cost(meter.used(), gas::GAS_PRICE)?;
            fee::charge(&mut self.store, &tx.payer, amount)?;
            tx.apply(self, now)?;
            self.set_nonce(&tx.payer, tx.nonce);
        }
        Ok(())
    }

    fn abort(&mut self) -> Result<()> {
        self.store.abort();
        self.reload()
    }

    /// Drop a block that lost. Its transactions were never removed from the
    /// queue — building SELECTS from the queue rather than draining it — so
    /// there is nothing to give back and nothing an engine can lose by dropping
    /// a block without rejecting it.
    pub fn reject(&mut self, id: &Id) {
        self.pending.remove(id);
    }

    /// Drop accepted transactions from the queue.
    fn release(&mut self, accepted: &[Transaction]) {
        if accepted.is_empty() {
            return;
        }
        let ids: Vec<Id> = accepted.iter().map(|t| t.id()).collect();
        self.mempool.retain(|t| !ids.contains(&t.id()));
    }

    /// What this chain is willing to say about itself.
    ///
    /// A committee is what makes F ANSWERABLE: with none seated, decryptions
    /// can be requested and never fulfilled, which is a degraded chain and is
    /// reported as one rather than as healthy.
    pub fn healthy(&self) -> bool {
        !self.current_epoch().committee.is_empty()
    }
}

fn keyed(prefix: &[u8], id: &[u8]) -> Vec<u8> {
    let mut k = Vec::with_capacity(prefix.len() + id.len());
    k.extend_from_slice(prefix);
    k.extend_from_slice(id);
    k
}

fn height_key(h: u64) -> Vec<u8> {
    keyed(HEIGHT_PREFIX, &h.to_be_bytes())
}
