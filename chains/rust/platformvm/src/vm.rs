// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The P-Chain, as the node sees it.
//!
//! The node holds several chains at once and does the same five things to each
//! of them: build, parse, verify, accept, reject. That is the seam, and it is
//! **not declared here**: [`Vm`], [`Block`], [`Status`] and [`Error`] are
//! `lux-rs/node`'s own `src/vm.rs`, named through it. A port that restated the
//! trait would compile against a trait of the same shape and satisfy nothing —
//! the host's chain map takes the host's `dyn Vm`, and two declarations can
//! drift while both still build.
//!
//! Verify and accept are separate on purpose, and the gap between them is
//! where a chain keeps the world it would leave behind if a block won. A
//! verified block holds a whole state; accepting one is choosing which held
//! state becomes the state. Nothing a block did is visible until then, which
//! is what makes a rejected block cost nothing.
//!
//! A proposal block is the reason `verify` cannot be `accept`: it leaves two
//! states behind rather than one, and which of them is real is decided by the
//! block after it.

use std::collections::HashMap;
use std::sync::Mutex;

// The seam, from the host that owns it. `Id` is `lux_consensus::finality::Id`
// either way — the node re-exports the same type this crate's `ids` names —
// so a block built here is named the way the node that certifies it names it.
pub use lux_node::vm::{Block, Error, Id, Status, Vm};

use crate::block;
use crate::executor::{self, Config, Fees};
use crate::ids::{hash256, EMPTY, PRIMARY_NETWORK_ID};
use crate::state::State;
use crate::txs::{Tx, Unsigned};
use crate::persist;
use crate::store;
use crate::validators;

/// A block plus the roots consensus signs over.
#[derive(Clone, Debug)]
pub struct PChainBlock {
    inner: block::Block,
    state_root: Id,
    payload_root: Id,
}

impl Block for PChainBlock {
    fn id(&self) -> Id {
        self.inner.id()
    }
    fn parent(&self) -> Id {
        self.inner.parent()
    }
    fn height(&self) -> u64 {
        self.inner.height()
    }
    fn timestamp(&self) -> u64 {
        self.inner.timestamp()
    }
    fn bytes(&self) -> Vec<u8> {
        self.inner.bytes().to_vec()
    }
    fn state_root(&self) -> Id {
        self.state_root
    }
    fn payload_root(&self) -> Id {
        self.payload_root
    }
}

impl PChainBlock {
    pub fn block(&self) -> &block::Block {
        &self.inner
    }
}

/// What a block left behind, held until it is accepted or dropped.
#[derive(Clone, Debug)]
struct Verified {
    block: block::Block,
    /// The state if this block wins. A proposal block leaves two; this is the
    /// one a commit would take.
    on_commit: State,
    /// The state a proposal's abort would take. `None` for every other kind.
    on_abort: Option<State>,
    state_root: Id,
}

/// The P-Chain.
///
/// One lock around everything, because the seam is `&self` and a chain has one
/// consistent answer at a time. Go arrives at the same arrangement from the
/// other side: its VMs are interfaces with pointer receivers and their own
/// internal mutexes.
pub struct PlatformVm {
    inner: Mutex<Inner>,
}

struct Inner {
    config: Config,
    fees: Box<dyn Fees + Send + Sync>,
    /// How reachable each validator has been. The chain cannot measure it —
    /// see [`executor::Uptime`] — so the node that runs the chain answers.
    uptime: Box<dyn executor::Uptime>,
    /// What another chain has handed to this one. The other half of an import,
    /// which this chain cannot see by itself — see [`executor::Atomic`].
    atomic: Box<dyn executor::Atomic>,
    /// The state as of the last accepted block.
    state: State,
    /// Every block this chain knows, accepted or not.
    blocks: HashMap<Id, block::Block>,
    /// What each verified block would leave behind.
    verified: HashMap<Id, Verified>,
    accepted_by_height: HashMap<u64, Id>,
    /// The root each accepted block left behind. Kept because it cannot be
    /// recomputed once the state has moved past it, and a block that answered
    /// a zero root would be claiming to have committed to nothing.
    accepted_roots: HashMap<Id, Id>,
    /// What every accepted height changed about the validator sets. This is
    /// what lets a signature made at a past height be checked at all: the set
    /// then is the set now with everything since undone.
    history: validators::History,
    /// Where the accepted state is written down. A chain given nowhere to
    /// write still runs, and says so by holding nothing here rather than by
    /// writing into something that forgets.
    store: Option<Box<dyn store::Store>>,
    last_accepted: Id,
    last_accepted_height: u64,
    preference: Id,
    /// Submitted transactions waiting for a block.
    mempool: Vec<Tx>,
    /// What the node's clock says. Injected so a test is not at the mercy of
    /// the wall clock, and so a node with a wrong clock is a configuration
    /// problem rather than a consensus one.
    now: u64,
}

/// The instant the first block is stamped with.
///
/// Go names it `upgrade.InitiallyActiveTime` — 2020-12-05T05:00:00Z. It is a
/// constant rather than the genesis timestamp because the first block is not
/// a block anyone produced: it is the fixed point both implementations start
/// from, and a chain whose first block was stamped differently is a different
/// chain from its first byte.
pub const GENESIS_TIME: u64 = 1_607_144_400;

impl PlatformVm {
    /// Start a chain from a genesis state.
    ///
    /// The genesis block is the one block nobody verified: it is where the
    /// chain's first answer comes from, so it is given rather than derived.
    pub fn new(
        config: Config,
        fees: Box<dyn Fees + Send + Sync>,
        uptime: Box<dyn executor::Uptime>,
        atomic: Box<dyn executor::Atomic>,
        genesis_state: State,
    ) -> PlatformVm {
        let timestamp = genesis_state.timestamp();
        let genesis = block::Block::standard(EMPTY, 0, timestamp, Vec::new());
        let id = genesis.id();
        let state_root = root_of(&genesis_state);
        let mut blocks = HashMap::new();
        blocks.insert(id, genesis);
        let mut accepted_by_height = HashMap::new();
        accepted_by_height.insert(0, id);
        PlatformVm {
            inner: Mutex::new(Inner {
                config,
                fees,
                uptime,
                atomic,
                state: genesis_state,
                blocks,
                verified: HashMap::new(),
                accepted_by_height,
                accepted_roots: HashMap::from([(id, state_root)]),
                history: validators::History::new(),
                store: None,
                last_accepted: id,
                last_accepted_height: 0,
                preference: id,
                mempool: Vec::new(),
                now: timestamp,
            }),
        }
    }

    /// Start a chain from the bytes a network is published as.
    ///
    /// This is the whole birth of a network in one call: the genesis blob says
    /// who validates, what exists, and how much of the asset there is, and the
    /// state that follows from it is a function of those bytes alone. Two
    /// nodes handed the same publication start on the same chain, at the same
    /// first block, with the same validator set, having trusted nobody.
    ///
    /// The first block is a commit block naming the genesis bytes as its
    /// parent, at [`GENESIS_TIME`] — the same shape Go's `state.init` builds,
    /// so both implementations name the first block identically.
    pub fn from_genesis(
        config: Config,
        fees: Box<dyn Fees + Send + Sync>,
        uptime: Box<dyn executor::Uptime>,
        atomic: Box<dyn executor::Atomic>,
        genesis_bytes: &[u8],
    ) -> Result<PlatformVm, crate::genesis::Error> {
        let published = crate::genesis::Genesis::parse(genesis_bytes)?;
        let rewards = crate::reward::Calculator::new(config.reward);
        let state = published.state(&rewards)?;
        let state_root = root_of(&state);
        let genesis = block::Block::commit(published.id(), 0, GENESIS_TIME);
        let id = genesis.id();
        let mut blocks = HashMap::new();
        blocks.insert(id, genesis);
        let mut accepted_by_height = HashMap::new();
        accepted_by_height.insert(0, id);
        let now = state.timestamp();
        Ok(PlatformVm {
            inner: Mutex::new(Inner {
                config,
                fees,
                uptime,
                atomic,
                state,
                blocks,
                verified: HashMap::new(),
                accepted_by_height,
                accepted_roots: HashMap::from([(id, state_root)]),
                history: validators::History::new(),
                store: None,
                last_accepted: id,
                last_accepted_height: 0,
                preference: id,
                mempool: Vec::new(),
                now,
            }),
        })
    }

    /// Start a chain that writes itself down — resuming what is written if
    /// anything is.
    ///
    /// One call rather than two, because "start" and "resume" are the same
    /// question asked of the store: a chain handed somewhere to write either
    /// comes back as what is there or writes its own birth. Two entry points
    /// would be two answers to which chain this is, and the wrong one is a
    /// node that silently rejoins at height zero.
    ///
    /// What comes back is checked, not trusted: a block filed under a name
    /// that is not the hash of its bytes, a staker whose priority names
    /// nothing, an L1 validator whose fixed fields moved — each is refused,
    /// because a node that starts from a state it cannot justify votes on one.
    pub fn open(
        config: Config,
        fees: Box<dyn Fees + Send + Sync>,
        uptime: Box<dyn executor::Uptime>,
        atomic: Box<dyn executor::Atomic>,
        genesis_bytes: &[u8],
        mut store: Box<dyn store::Store>,
    ) -> Result<PlatformVm, Error> {
        let (stored, by_height, roots, tip) =
            persist::restore_blocks(store.as_ref()).map_err(|e| Error::Malformed(e.to_string()))?;

        let Some((last_accepted, last_accepted_height)) = tip else {
            // Nothing written: this is the chain's birth, and the birth is
            // written down before anything is built on it.
            let vm = PlatformVm::from_genesis(config, fees, uptime, atomic, genesis_bytes)
                .map_err(|e| Error::Malformed(e.to_string()))?;
            {
                let mut inner = vm.inner.lock().unwrap();
                let genesis: Vec<(Id, u64, block::Block, Id)> = inner
                    .blocks
                    .iter()
                    .map(|(id, b)| {
                        (*id, b.height(), b.clone(), inner.accepted_roots[id])
                    })
                    .collect();
                persist::flush(
                    store.as_mut(),
                    &State::new(),
                    &inner.state,
                    &validators::History::new(),
                    &inner.history,
                    &genesis,
                    (inner.last_accepted, inner.last_accepted_height),
                )
                .map_err(|e| Error::Invalid(e.to_string()))?;
                inner.store = Some(store);
            }
            return Ok(vm);
        };

        let state = persist::restore(store.as_ref()).map_err(|e| Error::Malformed(e.to_string()))?;
        let history =
            persist::restore_history(store.as_ref()).map_err(|e| Error::Malformed(e.to_string()))?;
        if !stored.contains_key(&last_accepted) {
            return Err(Error::Malformed(
                "the stored tip names a block that is not stored".into(),
            ));
        }
        let now = state.timestamp();
        Ok(PlatformVm {
            inner: Mutex::new(Inner {
                config,
                fees,
                uptime,
                atomic,
                state,
                blocks: stored.into_iter().collect(),
                verified: HashMap::new(),
                accepted_by_height: by_height.into_iter().collect(),
                accepted_roots: roots.into_iter().collect(),
                history,
                store: Some(store),
                last_accepted,
                last_accepted_height,
                preference: last_accepted,
                mempool: Vec::new(),
                now,
            }),
        })
    }

    /// Hand the chain a transaction to include.
    pub fn submit(&self, tx: Tx) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        tx.syntactic_verify(inner.config.chain())
            .map_err(|e| Error::Invalid(e.to_string()))?;
        inner.mempool.push(tx);
        Ok(())
    }

    /// Tell the chain what time it is.
    pub fn set_clock(&self, now: u64) {
        self.inner.lock().unwrap().now = now;
    }

    /// The state as of the last accepted block.
    pub fn state(&self) -> State {
        self.inner.lock().unwrap().state.clone()
    }

    /// Who validated `chain` at `height`, keyed by node.
    ///
    /// This is what a node needs to check a signature made in the past: the
    /// answer is the set now with every recorded change since `height` undone.
    /// A height this chain has not accepted is refused rather than
    /// extrapolated — Go's `makeValidatorSet` refuses it too, with
    /// `errUnfinalizedHeight`.
    pub fn validator_set_at(
        &self,
        chain: &Id,
        height: u64,
    ) -> Result<std::collections::BTreeMap<crate::ids::NodeId, validators::Validator>, Error> {
        let inner = self.inner.lock().unwrap();
        let mut set = validators::current_set(&inner.state, chain)
            .map_err(|e| Error::Invalid(e.to_string()))?;
        inner
            .history
            .rewind(&mut set, chain, inner.last_accepted_height, height)
            .map_err(|e| Error::BadRequest(e.to_string()))?;
        Ok(set)
    }

    /// The set validating `chain` as of the last accepted block.
    pub fn validator_set(
        &self,
        chain: &Id,
    ) -> Result<std::collections::BTreeMap<crate::ids::NodeId, validators::Validator>, Error> {
        let inner = self.inner.lock().unwrap();
        validators::current_set(&inner.state, chain).map_err(|e| Error::Invalid(e.to_string()))
    }
}

/// A digest of everything the chain believes.
///
/// This is what a certificate is over, so it has to be a function of the state
/// alone and of nothing about how the state was reached. Everything that goes
/// in is in a sorted order the state itself defines, so two nodes that agree
/// about the world compute the same bytes.
fn root_of(state: &State) -> Id {
    let mut buf: Vec<u8> = Vec::new();
    buf.extend_from_slice(&state.timestamp().to_le_bytes());

    // The staker sets, in the order they will change — the order that decides
    // which staker a reward transaction is about.
    for s in state.current_stakers() {
        buf.extend_from_slice(&s.tx_id);
        buf.extend_from_slice(&s.node_id.0);
        buf.extend_from_slice(&s.chain);
        buf.extend_from_slice(&s.weight.to_le_bytes());
        buf.extend_from_slice(&s.start_time.to_le_bytes());
        buf.extend_from_slice(&s.end_time.to_le_bytes());
        buf.extend_from_slice(&s.potential_reward.to_le_bytes());
        buf.push(s.priority as u8);
    }
    buf.push(0xff);
    for s in state.pending_stakers() {
        buf.extend_from_slice(&s.tx_id);
        buf.extend_from_slice(&s.node_id.0);
        buf.extend_from_slice(&s.chain);
        buf.extend_from_slice(&s.weight.to_le_bytes());
        buf.extend_from_slice(&s.start_time.to_le_bytes());
        buf.extend_from_slice(&s.end_time.to_le_bytes());
        buf.push(s.priority as u8);
    }
    buf.push(0xff);

    // The networks that exist and what each has minted.
    for chain in state.chains() {
        buf.extend_from_slice(chain);
        buf.extend_from_slice(&state.current_supply(chain).unwrap_or(0).to_le_bytes());
    }
    buf.extend_from_slice(
        &state
            .current_supply(&PRIMARY_NETWORK_ID)
            .unwrap_or(0)
            .to_le_bytes(),
    );
    hash256(&buf)
}

/// A digest of what a block carries.
fn payload_root_of(blk: &block::Block) -> Id {
    let mut buf: Vec<u8> = Vec::new();
    for tx in blk.decision_txs() {
        buf.extend_from_slice(&tx.id());
    }
    if let Some(tx) = blk.proposal_tx() {
        buf.push(0xff);
        buf.extend_from_slice(&tx.id());
    }
    hash256(&buf)
}

impl Inner {
    /// The state a block would be verified against.
    fn parent_state(&self, parent: &Id) -> Option<State> {
        if *parent == self.last_accepted {
            return Some(self.state.clone());
        }
        let v = self.verified.get(parent)?;
        // A proposal block leaves two states, so a block built on one must say
        // which — commit and abort blocks do, and nothing else may sit there.
        if v.on_abort.is_some() {
            return None;
        }
        Some(v.on_commit.clone())
    }

    fn wrap(&self, blk: block::Block) -> PChainBlock {
        let id = blk.id();
        let state_root = self
            .verified
            .get(&id)
            .map(|v| v.state_root)
            // A block that was accepted before this process started is not in
            // the verified set, and its root is the one that was written down
            // when it was accepted.
            .or_else(|| self.accepted_roots.get(&id).copied())
            .unwrap_or(EMPTY);
        PChainBlock {
            payload_root: payload_root_of(&blk),
            state_root,
            inner: blk,
        }
    }
}

impl Vm for PlatformVm {
    fn name(&self) -> &'static str {
        "P"
    }

    fn version(&self) -> String {
        env!("CARGO_PKG_VERSION").to_string()
    }

    /// Build the next block.
    ///
    /// The order is the chain's own priorities, and it is not arbitrary. An
    /// outstanding question is answered before anything else is asked; a
    /// staker whose term has ended is retired before new work is taken on; and
    /// only then are submitted transactions included.
    fn build(&self) -> Result<Box<dyn Block>, Error> {
        let mut inner = self.inner.lock().unwrap();
        let preference = inner.preference;
        let parent = inner
            .blocks
            .get(&preference)
            .cloned()
            .ok_or(Error::NotFound)?;
        let state = inner.parent_state(&preference).or_else(|| {
            // Building on a proposal block means answering it.
            inner.verified.get(&preference).map(|v| v.on_commit.clone())
        });
        let state = match state {
            Some(s) => s,
            None => return Err(Error::NotFound),
        };
        let height = parent.height() + 1;

        // An outstanding question is answered first, and the answer is this
        // node's own: whether the staker being retired was reachable enough,
        // for long enough, on the terms it agreed to when it bonded. Every
        // node answers from what it saw and the stake-weighted vote settles
        // it, which is why a reward is a proposal and not a decision.
        if inner
            .verified
            .get(&preference)
            .is_some_and(|v| v.on_abort.is_some())
        {
            let pays = match parent.proposal_tx() {
                Some(tx) => executor::prefers_reward(&state, tx, &inner.config, &*inner.uptime)
                    // Not "do not pay" — "cannot say". Answering that with a
                    // refusal would let an unusual case or a hostile proposer
                    // cost an honest validator its reward, so this errs the
                    // way Go errs: toward paying.
                    .unwrap_or(true),
                None => true,
            };
            let blk = if pays {
                block::Block::commit(preference, height, state.timestamp())
            } else {
                block::Block::abort(preference, height, state.timestamp())
            };
            inner.blocks.insert(blk.id(), blk.clone());
            return Ok(Box::new(inner.wrap(blk)));
        }

        let now = inner.now.max(state.timestamp());
        let next_change = state.next_staker_change_time(now);

        // A staker whose term has ended is retired, and paid or not paid.
        if let Some(staker) = state.next_current_staker() {
            if staker.end_time <= state.timestamp() && !staker.priority.is_permissioned_validator()
            {
                let tx = Tx::new(
                    Unsigned::RewardValidator {
                        staker_tx_id: staker.tx_id,
                    },
                    Vec::new(),
                );
                let blk = block::Block::proposal(preference, height, state.timestamp(), tx);
                inner.blocks.insert(blk.id(), blk.clone());
                return Ok(Box::new(inner.wrap(blk)));
            }
        }

        let txs = std::mem::take(&mut inner.mempool);
        if txs.is_empty() && next_change <= state.timestamp() {
            inner.mempool = txs;
            return Err(Error::Empty);
        }
        let blk = block::Block::standard(preference, height, next_change, txs);
        inner.blocks.insert(blk.id(), blk.clone());
        Ok(Box::new(inner.wrap(blk)))
    }

    fn parse(&self, raw: &[u8]) -> Result<Box<dyn Block>, Error> {
        let blk = block::Block::parse(raw).map_err(|e| Error::Malformed(e.to_string()))?;
        let mut inner = self.inner.lock().unwrap();
        inner.blocks.insert(blk.id(), blk.clone());
        Ok(Box::new(inner.wrap(blk)))
    }

    fn get(&self, id: &Id) -> Result<Box<dyn Block>, Error> {
        let inner = self.inner.lock().unwrap();
        let blk = inner.blocks.get(id).cloned().ok_or(Error::NotFound)?;
        Ok(Box::new(inner.wrap(blk)))
    }

    /// Execute the block and check what it claims against what happened.
    ///
    /// Idempotent: a block already verified answers yes without doing the work
    /// again, because the engine calls this more than once for one block.
    fn verify(&self, id: &Id) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        if inner.verified.contains_key(id) {
            return Ok(());
        }
        let blk = inner.blocks.get(id).cloned().ok_or(Error::NotFound)?;
        let parent = blk.parent();

        match blk.kind() {
            block::Kind::Commit | block::Kind::Abort => {
                let v = inner
                    .verified
                    .get(&parent)
                    .cloned()
                    .ok_or_else(|| Error::Invalid("no proposal to answer".into()))?;
                let on_abort = v
                    .on_abort
                    .ok_or_else(|| Error::Invalid("parent is not a proposal".into()))?;
                let state = if blk.kind() == block::Kind::Commit {
                    v.on_commit
                } else {
                    on_abort
                };
                let state_root = root_of(&state);
                inner.verified.insert(
                    *id,
                    Verified {
                        block: blk,
                        on_commit: state,
                        on_abort: None,
                        state_root,
                    },
                );
                Ok(())
            }

            block::Kind::Proposal => {
                inner.verify_warp(&blk)?;
                let mut state = inner
                    .parent_state(&parent)
                    .ok_or_else(|| Error::Invalid("parent has not been verified".into()))?;
                if blk.height() != inner.height_of(&parent)? + 1 {
                    return Err(Error::Invalid("height does not follow its parent".into()));
                }
                let tx = blk
                    .proposal_tx()
                    .cloned()
                    .ok_or_else(|| Error::Invalid("no proposal".into()))?;

                executor::advance_time_to(&mut state, blk.timestamp(), &inner.config)
                    .map_err(|e| Error::Invalid(e.to_string()))?;

                let mut on_commit = state.clone();
                let mut on_abort = state;
                executor::execute_proposal(&tx, &mut on_commit, &mut on_abort)
                    .map_err(|e| Error::Invalid(e.to_string()))?;

                let state_root = root_of(&on_commit);
                inner.verified.insert(
                    *id,
                    Verified {
                        block: blk,
                        on_commit,
                        on_abort: Some(on_abort),
                        state_root,
                    },
                );
                Ok(())
            }

            block::Kind::Standard => {
                inner.verify_warp(&blk)?;
                let mut state = inner
                    .parent_state(&parent)
                    .ok_or_else(|| Error::Invalid("parent has not been verified".into()))?;
                if blk.height() != inner.height_of(&parent)? + 1 {
                    return Err(Error::Invalid("height does not follow its parent".into()));
                }
                executor::verify_new_chain_time(&state, blk.timestamp(), inner.now)
                    .map_err(|e| Error::Invalid(e.to_string()))?;
                executor::advance_time_to(&mut state, blk.timestamp(), &inner.config)
                    .map_err(|e| Error::Invalid(e.to_string()))?;

                for tx in blk.decision_txs() {
                    executor::execute_standard(
                        &mut state,
                        tx,
                        &inner.config,
                        inner.fees.as_ref(),
                        inner.atomic.as_ref(),
                    )
                        .map_err(|e| Error::Invalid(e.to_string()))?;
                }

                let state_root = root_of(&state);
                inner.verified.insert(
                    *id,
                    Verified {
                        block: blk,
                        on_commit: state,
                        on_abort: None,
                        state_root,
                    },
                );
                Ok(())
            }
        }
    }

    /// Commit it.
    ///
    /// A proposal block cannot be accepted into a state, because it has two.
    /// Accepting one records the block and leaves the state where it was; the
    /// commit or abort that follows chooses.
    fn accept(&self, id: &Id) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        let v = inner.verified.get(id).cloned().ok_or(Error::NotFound)?;
        // What is about to be replaced. The difference between these and what
        // follows is both what the validator-set record holds and what is
        // written to the store, so it is taken once.
        let was = inner.state.clone();
        let history_was = inner.history.clone();
        if v.on_abort.is_none() {
            // Record what this height did to the validator sets BEFORE the new
            // state replaces the old one, because the record is the difference
            // between the two and one of them is about to be gone.
            //
            // A proposal block leaves two states behind and commits neither;
            // its height changes nothing, and the option block that follows is
            // the height that does.
            let changed = validators::changes(&inner.state, &v.on_commit)
                .map_err(|e| Error::Invalid(e.to_string()))?;
            inner
                .history
                .record(v.block.height(), &changed)
                .map_err(|e| Error::Invalid(e.to_string()))?;
            inner.state = v.on_commit;
        }
        inner.last_accepted = *id;
        inner.last_accepted_height = v.block.height();
        inner.accepted_by_height.insert(v.block.height(), *id);
        inner.accepted_roots.insert(*id, v.state_root);
        inner.preference = *id;

        // Written down as one thing, so a machine that stops here comes back
        // at this height or at the one before it, and never between them.
        if let Some(mut store) = inner.store.take() {
            let written = persist::flush(
                store.as_mut(),
                &was,
                &inner.state,
                &history_was,
                &inner.history,
                &[(*id, v.block.height(), v.block.clone(), v.state_root)],
                (*id, v.block.height()),
            );
            inner.store = Some(store);
            written.map_err(|e| Error::Invalid(e.to_string()))?;
        }

        // Every sibling of the accepted block is now unreachable.
        let parent = v.block.parent();
        let losers: Vec<Id> = inner
            .verified
            .iter()
            .filter(|(other, w)| **other != *id && w.block.parent() == parent)
            .map(|(other, _)| *other)
            .collect();
        for loser in losers {
            inner.verified.remove(&loser);
        }
        Ok(())
    }

    fn reject(&self, id: &Id) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        inner.verified.remove(id);
        inner.blocks.remove(id);
        Ok(())
    }

    fn set_preference(&self, id: &Id) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        if !inner.blocks.contains_key(id) {
            return Err(Error::NotFound);
        }
        inner.preference = *id;
        Ok(())
    }

    fn last_accepted(&self) -> Id {
        self.inner.lock().unwrap().last_accepted
    }

    fn block_id_at(&self, height: u64) -> Result<Id, Error> {
        let inner = self.inner.lock().unwrap();
        inner
            .accepted_by_height
            .get(&height)
            .copied()
            .ok_or(Error::NotFound)
    }

    /// Answer one call.
    ///
    /// The reads a P-Chain is asked for are about the validator set, the
    /// clock, and the supply. Each is answered from the accepted state, never
    /// from a verified-but-undecided one — a caller asking what is true must
    /// not be told what might become true.
    fn call(&self, method: &str, params: &serde_json::Value) -> Result<serde_json::Value, Error> {
        let inner = self.inner.lock().unwrap();
        match method {
            "platform.getHeight" => Ok(serde_json::json!({
                "height": inner.last_accepted_height,
            })),
            "platform.getTimestamp" => Ok(serde_json::json!({
                "timestamp": inner.state.timestamp(),
            })),
            "platform.getCurrentSupply" => {
                let chain = chain_param(params)?;
                let supply = inner
                    .state
                    .current_supply(&chain)
                    .map_err(|_| Error::NotFound)?;
                Ok(serde_json::json!({ "supply": supply }))
            }
            "platform.getCurrentValidators" => {
                let chain = chain_param(params)?;
                let set = validators::current_set(&inner.state, &chain)
                    .map_err(|e| Error::Invalid(e.to_string()))?;
                Ok(serde_json::json!({ "validators": as_json(&set) }))
            }
            // Who validated then, so a signature made then can be checked now.
            "platform.getValidatorsAt" => {
                let chain = chain_param(params)?;
                let height = params
                    .get("height")
                    .and_then(|h| h.as_u64())
                    .ok_or_else(|| Error::BadRequest("height must be a number".into()))?;
                let mut set = validators::current_set(&inner.state, &chain)
                    .map_err(|e| Error::Invalid(e.to_string()))?;
                inner
                    .history
                    .rewind(&mut set, &chain, inner.last_accepted_height, height)
                    .map_err(|e| Error::BadRequest(e.to_string()))?;
                Ok(serde_json::json!({
                    "height": height,
                    "validators": as_json(&set),
                    // The commitment the set at that height is named by, so a
                    // caller can check it against what a certificate claimed
                    // rather than comparing lists by eye.
                    "setRoot": hex(&validators::set_root(&set)),
                }))
            }
            other => Err(Error::NoMethod(other.to_string())),
        }
    }
}

impl Inner {
    /// Check the aggregate proof on every warp message a block carries.
    ///
    /// This is the only thing binding a message to the chain it claims to come
    /// from. The source chain and the sender's address are read out of the
    /// message itself, so anyone can write any pair there; what they cannot
    /// write is a quorum of that chain's validators over the bytes. Without
    /// this, `verify_l1_conversion` would be checking a claim against itself
    /// and anybody could re-weight or de-register any L1's validators.
    ///
    /// The set that signed has to be the set as it stood when the message was
    /// made, which is the set at the height this block is verified against —
    /// the last accepted one. Go arranges the same thing: it runs this as its
    /// own pass, over the block's decision transactions and, on a proposal
    /// block, the transaction the chain emitted about itself, at the P-chain
    /// height carried in the block's context.
    ///
    /// Every transaction means every transaction: a warp message is an
    /// assertion about another chain no matter who put it in the block, so
    /// which of the two sets it arrived in cannot decide whether its signature
    /// is checked.
    fn verify_warp(&self, blk: &block::Block) -> Result<(), Error> {
        let carried = blk
            .decision_txs()
            .iter()
            .chain(blk.proposal_tx())
            .map(|tx| &tx.unsigned);
        for unsigned in carried {
            let Some(raw) = warp_message_of(unsigned) else {
                continue;
            };
            let message = crate::warp::Message::parse(raw)
                .map_err(|e| Error::Invalid(e.to_string()))?;
            let source = message.unsigned.source_chain_id;
            let set = validators::current_set(&self.state, &source)
                .map_err(|e| Error::Invalid(e.to_string()))?;
            let canonical =
                validators::canonical(&set).map_err(|e| Error::Invalid(e.to_string()))?;
            executor::verify_warp_messages(unsigned, self.config.network_id, &canonical)
                .map_err(|e| Error::Invalid(e.to_string()))?;
        }
        Ok(())
    }

    fn height_of(&self, id: &Id) -> Result<u64, Error> {
        self.blocks
            .get(id)
            .map(|b| b.height())
            .ok_or(Error::NotFound)
    }
}

/// The network a read is about. Absent means the primary network, which is
/// what every P-Chain read defaults to.
fn chain_param(params: &serde_json::Value) -> Result<Id, Error> {
    let raw = match params.get("chainID").or_else(|| params.get("netID")) {
        None | Some(serde_json::Value::Null) => return Ok(PRIMARY_NETWORK_ID),
        Some(v) => v
            .as_str()
            .ok_or_else(|| Error::BadRequest("chainID must be a string".into()))?,
    };
    let bytes = unhex(raw).ok_or_else(|| Error::BadRequest("chainID is not hex".into()))?;
    if bytes.len() != 32 {
        return Err(Error::BadRequest("chainID must be 32 bytes".into()));
    }
    let mut id = [0u8; 32];
    id.copy_from_slice(&bytes);
    Ok(id)
}

/// The warp message a transaction carries, if it carries one. Named here so a
/// kind that starts carrying one has to be added in exactly one place — the
/// executor's own reader is the other half of the same question.
fn warp_message_of(unsigned: &Unsigned) -> Option<&[u8]> {
    match unsigned {
        Unsigned::RegisterL1Validator { message, .. }
        | Unsigned::SetL1ValidatorWeight { message, .. } => Some(message),
        _ => None,
    }
}

/// A set as the RPC hands it back. The key is the uncompressed one the set
/// commitment hashes, because that is the key a caller has to check a
/// signature against; handing back the compressed form would be handing back
/// something no aggregate verifies under.
fn as_json(
    set: &std::collections::BTreeMap<crate::ids::NodeId, validators::Validator>,
) -> Vec<serde_json::Value> {
    set.values()
        .map(|v| {
            serde_json::json!({
                "nodeID": hex(&v.node_id.0),
                "weight": v.weight,
                "publicKey": v.public_key.as_deref().map(hex),
                "txID": hex(&v.tx_id),
            })
        })
        .collect()
}

fn hex(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

fn unhex(s: &str) -> Option<Vec<u8>> {
    let s = s.strip_prefix("0x").unwrap_or(s);
    if !s.len().is_multiple_of(2) {
        return None;
    }
    (0..s.len() / 2)
        .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).ok())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Credential, Input, Output, Owners, Utxo, UtxoId};
    use crate::executor::{FlatFees, StakingPolicy};
    use crate::ids::{NodeId, ShortId};
    use crate::reward;
    use crate::signer::Signer;
    use crate::store::Store;
    use crate::txs::{Envelope, Validator};

    const ASSET: Id = [9u8; 32];
    const DAY: u64 = 24 * 60 * 60;
    const YEAR: u64 = 365 * DAY;
    const MEGA: u64 = 1_000_000_000_000;

    fn config() -> Config {
        Config {
            network_id: 1,
            blockchain_id: [3; 32],
            native_asset: ASSET,
            validator_fee: crate::l1::FeeConfig {
                capacity: 20_000,
                target: 10_000,
                min_price: 512,
                excess_conversion_constant: 1_246_488,
            },
            staking: StakingPolicy {
                min_validator_stake: 2 * MEGA,
                max_validator_stake: 3_000 * MEGA,
                min_delegator_stake: MEGA / 40,
                min_stake_duration: 2 * 7 * DAY,
                max_stake_duration: YEAR,
                min_delegation_fee: 20_000,
                uptime_requirement: 800_000,
            },
            staking_history: None,
            reward: reward::Config {
                max_consumption_rate: 120_000,
                min_consumption_rate: 100_000,
                minting_period: std::time::Duration::from_secs(YEAR),
                supply_cap: 2_000_000_000_000_000_000,
            },
            bootstrapped: true,
        }
    }

    /// The key the test genesis funds, and the one every spend here is signed
    /// with. Real signatures: a chain that accepted an unsigned spend would
    /// pass every other test in this file.
    fn spender() -> k256::ecdsa::SigningKey {
        k256::ecdsa::SigningKey::from_bytes(&[0x11u8; 32].into()).expect("a key")
    }

    fn spend_owner() -> Owners {
        Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![crate::sign::address(
                spender().verifying_key().to_encoded_point(true).as_bytes(),
            )],
        }
    }

    /// A node that saw the validator the whole time. Every existing test in
    /// this file is about something other than reachability, and this is what
    /// "nothing was wrong" looks like.
    struct AlwaysUp;
    impl executor::Uptime for AlwaysUp {
        fn fraction_since(&self, _: &NodeId, _: &Id, _: u64) -> Option<f64> {
            Some(1.0)
        }
    }

    /// A node that saw nothing of the validator at all.
    struct NeverUp;
    impl executor::Uptime for NeverUp {
        fn fraction_since(&self, _: &NodeId, _: &Id, _: u64) -> Option<f64> {
            Some(0.0)
        }
    }

    /// A node that cannot say — a validator it only started watching today.
    struct Unknown;
    impl executor::Uptime for Unknown {
        fn fraction_since(&self, _: &NodeId, _: &Id, _: u64) -> Option<f64> {
            None
        }
    }

    fn genesis(now: u64) -> State {
        let mut state = State::new();
        state.set_timestamp(now);
        state.set_current_supply(PRIMARY_NETWORK_ID, 400 * 1_000_000 * 1_000_000);
        state.add_utxo(Utxo {
            id: UtxoId {
                tx_id: [1; 32],
                output_index: 0,
            },
            output: Output {
                asset: ASSET,
                stake_lock: 0,
                amount: 10 * MEGA,
                owners: spend_owner(),
            },
        });
        state
    }

    fn vm(now: u64) -> PlatformVm {
        PlatformVm::new(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            genesis(now),
        )
    }

    fn a_validator_tx(now: u64) -> Tx {
        let sk = blst::min_pk::SecretKey::key_gen(&[7u8; 32], &[]).unwrap();
        let unsigned = Unsigned::AddPermissionlessValidator {
            base: Envelope {
                network_id: 1,
                blockchain_id: [3; 32],
                outs: vec![],
                ins: vec![Input {
                    utxo: UtxoId {
                        tx_id: [1; 32],
                        output_index: 0,
                    },
                    asset: ASSET,
                    stake_lock: 0,
                    amount: 10 * MEGA,
                    sig_indices: vec![0],
                }],
                memo: Vec::new(),
            },
            validator: Validator {
                node_id: NodeId([5; 20]),
                start: 0,
                end: now + YEAR,
                weight: 10 * MEGA,
            },
            chain: PRIMARY_NETWORK_ID,
            signer: Signer::prove(&sk),
            stake: vec![Output {
                asset: ASSET,
                stake_lock: 0,
                amount: 10 * MEGA,
                owners: Owners {
                    locktime: 0,
                    threshold: 1,
                    addrs: vec![ShortId([2; 20])],
                },
            }],
            validator_rewards_owner: Owners {
                locktime: 0,
                threshold: 1,
                addrs: vec![ShortId([3; 20])],
            },
            delegator_rewards_owner: Owners {
                locktime: 0,
                threshold: 1,
                addrs: vec![ShortId([4; 20])],
            },
            delegation_shares: 20_000,
        };
        let sighash = crate::ids::hash256(&unsigned.to_bytes());
        Tx::new(
            unsigned,
            vec![Credential {
                sigs: vec![crate::sign::sign(&spender(), &sighash)],
            }],
        )
    }

    #[test]
    fn the_chain_is_named_p() {
        assert_eq!(vm(1000).name(), "P");
    }

    #[test]
    fn a_chain_starts_at_its_genesis() {
        let vm = vm(1000);
        let id = vm.last_accepted();
        assert_eq!(vm.block_id_at(0).unwrap(), id);
        assert_eq!(vm.get(&id).unwrap().height(), 0);
        assert_eq!(vm.block_id_at(1), Err(Error::NotFound));
    }

    #[test]
    fn a_chain_with_nothing_to_say_builds_nothing() {
        let vm = vm(1000);
        assert_eq!(vm.build().err(), Some(Error::Empty));
    }

    #[test]
    fn a_submitted_transaction_becomes_a_block_that_changes_the_set() {
        // The whole path, end to end: submit, build, verify, accept, and the
        // validator set has one more member than it did.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();

        let blk = vm.build().unwrap();
        assert_eq!(blk.height(), 1);
        assert_eq!(blk.parent(), vm.last_accepted());

        vm.verify(&blk.id()).unwrap();
        // Nothing is visible until it is accepted.
        assert!(vm.state().validator_set(&PRIMARY_NETWORK_ID).is_empty());

        vm.accept(&blk.id()).unwrap();
        let set = vm.state().validator_set(&PRIMARY_NETWORK_ID);
        assert_eq!(set.len(), 1);
        assert_eq!(set[0].0, NodeId([5; 20]));
        assert_eq!(set[0].1, 10 * MEGA);
        assert!(set[0].2.is_some(), "and it registered a key to sign with");

        assert_eq!(vm.last_accepted(), blk.id());
        assert_eq!(vm.block_id_at(1).unwrap(), blk.id());
    }

    /// The bytes a network is published as, holding exactly the one output
    /// `a_validator_tx` spends. Enough for a chain to be born from and then to
    /// change.
    fn a_published_network(now: u64) -> Vec<u8> {
        use crate::genesis::{Allocation, Genesis};
        Genesis {
            utxos: vec![Allocation {
                utxo: Utxo {
                    id: UtxoId {
                        tx_id: [1; 32],
                        output_index: 0,
                    },
                    output: Output {
                        asset: ASSET,
                        stake_lock: 0,
                        amount: 10 * MEGA,
                        owners: spend_owner(),
                    },
                },
                message: Vec::new(),
            }],
            validators: Vec::new(),
            chains: Vec::new(),
            timestamp: now,
            initial_supply: 400 * 1_000_000 * 1_000_000,
            message: "a network".to_string(),
        }
        .to_bytes()
    }

    #[test]
    fn a_chain_comes_back_as_the_chain_it_was() {
        // The whole reason the state is written down. A node that restarts and
        // remembers nothing has to be TOLD what it decided by the peers whose
        // claims it exists to check.
        let now = 1000;
        let path = {
            let mut p = std::env::temp_dir();
            p.push(format!("lux-pvm-restart-{}", std::process::id()));
            let _ = std::fs::remove_file(&p);
            p
        };
        let published = a_published_network(now);

        let (tip, height, root, set_at_zero) = {
            let vm = PlatformVm::open(
                config(),
                Box::new(FlatFees::default()),
                Box::new(AlwaysUp),
                Box::new(executor::NoImports),
                &published,
                Box::new(store::File::open(&path).unwrap()),
            )
            .expect("a chain writes its own birth");
            vm.set_clock(now);
            vm.submit(a_validator_tx(now)).unwrap();
            let blk = vm.build().unwrap();
            vm.verify(&blk.id()).unwrap();
            vm.accept(&blk.id()).unwrap();
            (
                vm.last_accepted(),
                blk.height(),
                vm.get(&blk.id()).unwrap().state_root(),
                vm.validator_set_at(&PRIMARY_NETWORK_ID, 0).unwrap(),
            )
        };

        let back = PlatformVm::open(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            &published,
            Box::new(store::File::open(&path).unwrap()),
        )
        .expect("what was written is a chain");

        assert_eq!(back.last_accepted(), tip, "the same tip");
        assert_eq!(back.block_id_at(height), Ok(tip));
        assert_eq!(back.block_id_at(0), Ok(back.get(&tip).unwrap().parent()));
        // The same state, checked by the digest a certificate is over rather
        // than by a list of fields somebody remembered.
        assert_eq!(back.get(&tip).unwrap().state_root(), root);
        // The set now, and the set at a height decided before the restart.
        assert_eq!(
            back.validator_set(&PRIMARY_NETWORK_ID).unwrap()[&NodeId([5; 20])].weight,
            10 * MEGA
        );
        assert_eq!(back.validator_set_at(&PRIMARY_NETWORK_ID, 0).unwrap(), set_at_zero);
        assert!(set_at_zero.is_empty());

        // And it goes on from there rather than starting again.
        assert!(matches!(
            back.validator_set_at(&PRIMARY_NETWORK_ID, height + 1),
            Err(Error::BadRequest(_))
        ));
        let _ = std::fs::remove_file(&path);
    }

    #[test]
    fn a_chain_given_nowhere_to_write_still_runs() {
        // A memory store has no durability to offer and does not claim any.
        let now = 1000;
        let published = a_published_network(now);
        let vm = PlatformVm::open(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            &published,
            Box::new(store::Memory::new()),
        )
        .unwrap();
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();
        assert_eq!(vm.validator_set(&PRIMARY_NETWORK_ID).unwrap().len(), 1);
    }

    #[test]
    fn a_stored_tip_naming_a_block_that_is_not_there_does_not_start_a_chain() {
        let mut s = store::Memory::new();
        let mut tip = [7u8; 32].to_vec();
        tip.extend_from_slice(&3u64.to_le_bytes());
        s.put(b"P", &tip);
        s.commit().unwrap();
        assert!(matches!(
            PlatformVm::open(
                config(),
                Box::new(FlatFees::default()),
                Box::new(AlwaysUp),
                Box::new(executor::NoImports),
                &a_published_network(1000),
                Box::new(s),
            ),
            Err(Error::Malformed(_))
        ));
    }

    /// A block carrying one warp-bearing transaction, built by hand so the
    /// message can be anything.
    fn a_block_carrying(vm: &PlatformVm, message: Vec<u8>) -> block::Block {
        let sk = blst::min_pk::SecretKey::key_gen(&[3u8; 32], &[]).unwrap();
        let proof = match Signer::prove(&sk) {
            Signer::ProofOfPossession { proof, .. } => proof,
            Signer::Empty => unreachable!("a proven signer carries a proof"),
        };
        let unsigned = Unsigned::RegisterL1Validator {
            base: Envelope {
                network_id: 1,
                blockchain_id: [3; 32],
                outs: Vec::new(),
                ins: Vec::new(),
                memo: Vec::new(),
            },
            balance: 0,
            proof_of_possession: proof,
            message,
        };
        let tx = Tx::new(unsigned, Vec::new());
        block::Block::standard(vm.last_accepted(), 1, 1000, vec![tx])
    }

    /// The chain a warp message in these tests claims to come from.
    const SOURCE: Id = [0x5cu8; 32];

    /// A chain whose state already holds two validators of [`SOURCE`], each
    /// with a real BLS key. That set is what a warp proof from there is
    /// measured against.
    fn vm_with_a_source_set(now: u64) -> (PlatformVm, Vec<blst::min_pk::SecretKey>) {
        let keys: Vec<blst::min_pk::SecretKey> = [11u8, 12]
            .iter()
            .map(|seed| blst::min_pk::SecretKey::key_gen(&[*seed; 32], &[]).unwrap())
            .collect();
        let mut state = genesis(now);
        for (i, sk) in keys.iter().enumerate() {
            state
                .put_l1_validator(crate::l1::Validator {
                    validation_id: [(0x40 + i) as u8; 32],
                    chain_id: SOURCE,
                    node_id: NodeId([(0x50 + i) as u8; 20]),
                    public_key: crate::signer::uncompress(&sk.sk_to_pk().compress()).unwrap(),
                    remaining_balance_owner: Vec::new(),
                    deactivation_owner: Vec::new(),
                    start_time: now,
                    weight: 100,
                    min_nonce: 0,
                    end_accumulated_fee: 1_000_000,
                })
                .unwrap();
        }
        (
            PlatformVm::new(
                config(),
                Box::new(FlatFees::default()),
                Box::new(AlwaysUp),
                Box::new(executor::NoImports),
                state,
            ),
            keys,
        )
    }

    /// A warp envelope from [`SOURCE`], signed by whichever of the source set's
    /// validators `signing` names, in the canonical order the bit vector
    /// indexes — ascending by uncompressed key, which is the order
    /// `warp::flatten` puts them in.
    fn a_message_from_the_source(
        vm: &PlatformVm,
        keys: &[blst::min_pk::SecretKey],
        signing: &[usize],
    ) -> Vec<u8> {
        // A registration naming the same key `a_block_carrying` proves
        // possession of, so the transaction is coherent all the way down and
        // the only thing left to refuse it is the ledger.
        let registering = blst::min_pk::SecretKey::key_gen(&[3u8; 32], &[]).unwrap();
        let owner = crate::txs::PChainOwner {
            threshold: 1,
            addresses: vec![ShortId([6; 20])],
        };
        let payload = crate::warpmsg::Register::build(
            [0x77; 32],
            &NodeId([9; 20]),
            &registering.sk_to_pk().compress(),
            2000,
            &owner,
            &owner,
            42,
        )
        .bytes;
        let call = crate::warpmsg::Call::build(&[0x5au8; 20], &payload);
        let unsigned = crate::warp::Unsigned::build(1, SOURCE, &call.bytes);

        // The canonical order, taken from the chain's own flattening rather
        // than assumed — the bit vector indexes THAT order, and guessing it
        // would make this test agree with itself instead of with the chain.
        let set = validators::current_set(&vm.state(), &SOURCE).unwrap();
        let canonical = validators::canonical(&set).unwrap();
        let position = |sk: &blst::min_pk::SecretKey| {
            let compressed = sk.sk_to_pk().compress();
            canonical
                .validators
                .iter()
                .position(|v| v.public_key == compressed)
                .expect("the key is in the source set")
        };

        let mut bits = vec![0u8; canonical.validators.len().div_ceil(8).max(1)];
        let mut sigs = Vec::new();
        for i in signing {
            let at = position(&keys[*i]);
            let byte = bits.len() - 1 - at / 8;
            bits[byte] |= 1 << (at % 8);
            sigs.push(crate::signer::sign(&keys[*i], &unsigned.bytes));
        }
        // Go refuses a bit vector with unnecessary leading zero bytes.
        while bits.first() == Some(&0) {
            bits.remove(0);
        }
        let signature = if sigs.is_empty() {
            [0u8; crate::signer::SIGNATURE_LEN]
        } else {
            crate::signer::aggregate_signatures(&sigs).unwrap()
        };

        crate::warp::Message::build(
            &unsigned,
            &crate::warp::BitSet {
                signers: bits,
                signature,
            },
        )
        .bytes
    }

    #[test]
    fn a_warp_message_too_few_of_the_source_set_signed_is_refused() {
        // The quorum rule itself, against a real set: one of two equal
        // validators is half the weight, and the bar is 67%.
        let now = 1000;
        let (vm, keys) = vm_with_a_source_set(now);
        vm.set_clock(now);

        let raw = a_message_from_the_source(&vm, &keys, &[0]);
        let blk = a_block_carrying(&vm, raw);
        let parsed = vm.parse(blk.bytes()).expect("the block reads back");
        let refused = vm.verify(&parsed.id()).expect_err("half is not a quorum");
        assert!(
            format!("{refused}").contains("weight"),
            "refused for the wrong reason: {refused}"
        );
    }

    #[test]
    fn a_warp_message_the_source_set_really_signed_gets_through_the_check() {
        // The other half: the door opens for a real quorum. What stops the
        // transaction after that is the ledger — this chain holds no
        // registration by that name — which is the point: the proof was
        // accepted and execution was reached.
        let now = 1000;
        let (vm, keys) = vm_with_a_source_set(now);
        vm.set_clock(now);

        let raw = a_message_from_the_source(&vm, &keys, &[0, 1]);
        let blk = a_block_carrying(&vm, raw);
        let parsed = vm.parse(blk.bytes()).expect("the block reads back");
        let refused = vm.verify(&parsed.id()).expect_err("no such registration here");
        let said = format!("{refused}");
        assert!(
            !said.contains("weight") && !said.contains("signature") && !said.contains("warp"),
            "the proof should have been accepted, but: {said}"
        );
    }

    #[test]
    fn a_warp_message_signed_over_other_bytes_is_refused() {
        // An aggregate lifted off one message onto another. The signature is
        // real and the signers are the whole set; it just is not over these
        // bytes.
        let now = 1000;
        let (vm, keys) = vm_with_a_source_set(now);
        vm.set_clock(now);

        let mut raw = a_message_from_the_source(&vm, &keys, &[0, 1]);
        // Move the message's payload without touching the proof: the last byte
        // of the buffer is inside the addressed call.
        let last = raw.len() - 1;
        raw[last] ^= 0xff;
        let blk = a_block_carrying(&vm, raw);
        let parsed = vm.parse(blk.bytes()).expect("the block reads back");
        assert!(
            vm.verify(&parsed.id()).is_err(),
            "a proof over other bytes proves nothing about these"
        );
    }

    #[test]
    fn a_warp_message_no_quorum_signed_does_not_reach_execution() {
        // The only thing binding a warp message to the chain it claims to come
        // from. The source chain and the sender's address are written INSIDE
        // the message, so anyone can put any pair there; what nobody can write
        // is a quorum of that chain's validators over the bytes. Without this
        // check the conversion check would be comparing a claim with itself,
        // and anyone could re-weight or de-register any L1's validators.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);

        let payload = crate::warpmsg::Weight::build([7; 32], 0, 99).bytes;
        let call = crate::warpmsg::Call::build(&[0x5au8; 20], &payload);
        let unsigned = crate::warp::Unsigned::build(1, [0x5cu8; 32], &call.bytes);
        let nobody = crate::warp::BitSet {
            signers: Vec::new(),
            signature: [0u8; crate::signer::SIGNATURE_LEN],
        };
        let raw = crate::warp::Message::build(&unsigned, &nobody).bytes;

        let blk = a_block_carrying(&vm, raw);
        let parsed = vm.parse(blk.bytes()).expect("the block reads back");
        let refused = vm.verify(&parsed.id()).expect_err("nobody signed it");
        // Named, so this cannot pass because the block was refused for some
        // other reason it happens to also deserve.
        // With a source chain this node knows nothing about, the set is empty
        // and the weight rule passes vacuously — 0 of 0 — so the refusal lands
        // on the aggregate, which cannot be built from no keys. That is Go's
        // shape too: `VerifyWeight(0, 0)` returns nil there and
        // `AggregatePublicKeys` of nothing is the error. Either way the
        // message never reaches execution, which is the claim.
        assert!(
            format!("{refused}").contains("warp"),
            "refused for the wrong reason: {refused}"
        );
    }

    #[test]
    fn a_warp_message_addressed_to_another_network_is_refused() {
        // The network id is inside the signed bytes, so a message made for the
        // test network cannot be replayed here even if its signers overlap.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);

        let payload = crate::warpmsg::Weight::build([7; 32], 0, 99).bytes;
        let call = crate::warpmsg::Call::build(&[0x5au8; 20], &payload);
        let elsewhere = crate::warp::Unsigned::build(2, [0x5cu8; 32], &call.bytes);
        let nobody = crate::warp::BitSet {
            signers: Vec::new(),
            signature: [0u8; crate::signer::SIGNATURE_LEN],
        };
        let raw = crate::warp::Message::build(&elsewhere, &nobody).bytes;

        let blk = a_block_carrying(&vm, raw);
        let parsed = vm.parse(blk.bytes()).expect("the block reads back");
        let refused = vm.verify(&parsed.id()).expect_err("it is for another network");
        assert!(
            format!("{refused}").contains("network"),
            "refused for the wrong reason: {refused}"
        );
    }

    #[test]
    fn a_block_carrying_no_warp_message_is_not_held_up_by_the_check() {
        // The check is over the messages a block carries, and a block that
        // carries none passes it without an opinion.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        assert_eq!(vm.verify(&blk.id()), Ok(()));
    }

    #[test]
    fn a_past_height_still_names_the_set_that_validated_then() {
        // The point of the record. A chain that has admitted a validator must
        // still be able to say who was in the set BEFORE it did, or a
        // signature made then can never be checked.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);

        assert!(vm.validator_set(&PRIMARY_NETWORK_ID).unwrap().is_empty());

        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();

        let now_set = vm.validator_set_at(&PRIMARY_NETWORK_ID, 1).unwrap();
        assert_eq!(now_set.len(), 1);
        let held = &now_set[&NodeId([5; 20])];
        assert_eq!(held.weight, 10 * MEGA);
        // The uncompressed key — the one the set commitment hashes.
        assert_eq!(held.public_key.as_ref().map(|k| k.len()), Some(96));

        // And at the height before it, nobody.
        assert!(vm.validator_set_at(&PRIMARY_NETWORK_ID, 0).unwrap().is_empty());

        // A height this chain has not reached has no set, rather than the
        // current one under a false name.
        assert!(matches!(
            vm.validator_set_at(&PRIMARY_NETWORK_ID, 2),
            Err(Error::BadRequest(_))
        ));
    }

    #[test]
    fn the_set_at_a_height_is_answered_over_the_wire_with_its_root() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();

        let at_one = vm
            .call(
                "platform.getValidatorsAt",
                &serde_json::json!({ "height": 1 }),
            )
            .unwrap();
        assert_eq!(at_one["validators"].as_array().unwrap().len(), 1);

        let at_zero = vm
            .call(
                "platform.getValidatorsAt",
                &serde_json::json!({ "height": 0 }),
            )
            .unwrap();
        assert!(at_zero["validators"].as_array().unwrap().is_empty());
        // An empty set commits to the zero id rather than to sha256("").
        assert_eq!(at_zero["setRoot"], hex(&crate::ids::EMPTY));

        // The root is over the set, so the two heights do not share one.
        assert_ne!(at_one["setRoot"], at_zero["setRoot"]);
    }

    #[test]
    fn verifying_twice_is_the_same_as_verifying_once() {
        // The engine calls verify more than once for one block.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        assert_eq!(vm.verify(&blk.id()), Ok(()));
        assert_eq!(vm.verify(&blk.id()), Ok(()));
        vm.accept(&blk.id()).unwrap();
        assert_eq!(vm.state().validator_set(&PRIMARY_NETWORK_ID).len(), 1);
    }

    #[test]
    fn a_block_that_was_never_verified_is_not_accepted() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        assert_eq!(vm.accept(&blk.id()), Err(Error::NotFound));
    }

    #[test]
    fn a_rejected_block_costs_nothing() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.reject(&blk.id()).unwrap();
        assert!(vm.state().validator_set(&PRIMARY_NETWORK_ID).is_empty());
        assert_eq!(vm.get(&blk.id()).err(), Some(Error::NotFound));
    }

    #[test]
    fn a_block_travels_as_bytes_and_comes_back_the_same_block() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        let bytes = blk.bytes();

        // A second chain, which has never seen this block, reads it.
        let other = vm2(now);
        let parsed = other.parse(&bytes).unwrap();
        assert_eq!(parsed.id(), blk.id());
        assert_eq!(parsed.height(), blk.height());
        assert_eq!(parsed.parent(), blk.parent());
        assert_eq!(parsed.payload_root(), blk.payload_root());
    }

    fn vm2(now: u64) -> PlatformVm {
        PlatformVm::new(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            genesis(now),
        )
    }

    #[test]
    fn bytes_that_are_not_a_block_are_refused() {
        let vm = vm(1000);
        assert!(matches!(vm.parse(b"not a block"), Err(Error::Malformed(_))));
    }

    #[test]
    fn a_block_whose_height_does_not_follow_its_parent_is_refused() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        let parent = vm.last_accepted();
        let blk = block::Block::standard(parent, 7, now, Vec::new());
        let wrapped = vm.parse(blk.bytes()).unwrap();
        assert!(matches!(vm.verify(&wrapped.id()), Err(Error::Invalid(_))));
    }

    #[test]
    fn a_block_whose_time_runs_backwards_is_refused() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        let parent = vm.last_accepted();
        let blk = block::Block::standard(parent, 1, now - 1, Vec::new());
        vm.parse(blk.bytes()).unwrap();
        assert!(matches!(vm.verify(&blk.id()), Err(Error::Invalid(_))));
    }

    #[test]
    fn the_state_root_changes_when_the_state_changes() {
        // A certificate is over this, so two different worlds must not share
        // a root.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let before = root_of(&vm.state());
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();
        assert_ne!(root_of(&vm.state()), before);
        assert_eq!(
            vm.get(&blk.id()).unwrap().state_root(),
            root_of(&vm.state())
        );
    }

    #[test]
    fn two_states_that_agree_have_the_same_root() {
        let a = genesis(1000);
        let b = genesis(1000);
        assert_eq!(root_of(&a), root_of(&b));
        let mut c = genesis(1001);
        assert_ne!(root_of(&a), root_of(&c));
        c.set_timestamp(1000);
        assert_eq!(root_of(&a), root_of(&c));
    }

    #[test]
    fn a_finished_validator_is_retired_by_a_proposal_and_paid_by_a_commit() {
        // The two-block shape: the question, then the answer.
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();
        let staker_tx_id = vm
            .state()
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .unwrap()
            .tx_id;

        // Move the chain to the end of the validator's term.
        let end = now + YEAR;
        vm.set_clock(end);
        {
            let mut inner = vm.inner.lock().unwrap();
            inner.state.set_timestamp(end);
        }

        let proposal = vm.build().unwrap();
        vm.verify(&proposal.id()).unwrap();
        let proposal_block = vm.get(&proposal.id()).unwrap();
        assert_eq!(proposal_block.height(), 2);
        vm.accept(&proposal.id()).unwrap();
        // A proposal changes nothing by itself.
        assert_eq!(vm.state().validator_set(&PRIMARY_NETWORK_ID).len(), 1);

        let commit = vm.build().unwrap();
        vm.verify(&commit.id()).unwrap();
        vm.accept(&commit.id()).unwrap();

        // Retired, and its stake and reward paid out.
        assert!(vm.state().validator_set(&PRIMARY_NETWORK_ID).is_empty());
        assert_eq!(vm.state().reward_utxos(&staker_tx_id).len(), 1);
    }

    #[test]
    fn the_reads_answer_from_the_accepted_state() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);

        assert_eq!(
            vm.call("platform.getHeight", &serde_json::json!({}))
                .unwrap(),
            serde_json::json!({ "height": 0 })
        );
        assert_eq!(
            vm.call("platform.getTimestamp", &serde_json::json!({}))
                .unwrap(),
            serde_json::json!({ "timestamp": now })
        );

        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();

        // Verified is not accepted: the read still says nobody validates.
        let answer = vm
            .call("platform.getCurrentValidators", &serde_json::json!({}))
            .unwrap();
        assert_eq!(answer["validators"].as_array().unwrap().len(), 0);

        vm.accept(&blk.id()).unwrap();
        let answer = vm
            .call("platform.getCurrentValidators", &serde_json::json!({}))
            .unwrap();
        let validators = answer["validators"].as_array().unwrap();
        assert_eq!(validators.len(), 1);
        assert_eq!(validators[0]["weight"], serde_json::json!(10 * MEGA));
    }

    #[test]
    fn a_method_the_chain_does_not_answer_says_so() {
        let vm = vm(1000);
        assert_eq!(
            vm.call("platform.somethingElse", &serde_json::json!({})),
            Err(Error::NoMethod("platform.somethingElse".into()))
        );
    }

    #[test]
    fn a_read_about_a_network_that_does_not_exist_is_not_found() {
        let vm = vm(1000);
        let params = serde_json::json!({ "chainID": hex(&[7u8; 32]) });
        assert_eq!(
            vm.call("platform.getCurrentSupply", &params),
            Err(Error::NotFound)
        );
    }

    #[test]
    fn a_malformed_read_is_a_bad_request_and_not_a_crash() {
        let vm = vm(1000);
        for params in [
            serde_json::json!({ "chainID": "zz" }),
            serde_json::json!({ "chainID": "00" }),
            serde_json::json!({ "chainID": 7 }),
        ] {
            assert!(matches!(
                vm.call("platform.getCurrentSupply", &params),
                Err(Error::BadRequest(_))
            ));
        }
    }

    #[test]
    fn the_seam_is_object_safe() {
        // The reason for the shape: a node holds several chains at once.
        fn takes(_: &[&dyn Vm]) {}
        let vm = vm(1000);
        takes(&[&vm]);
        fn holds(_: Box<dyn Block>) {}
        let _ = holds as fn(Box<dyn Block>);
    }

    #[test]
    fn the_seam_is_the_hosts_own_declaration() {
        // Named through its full path rather than through this module's
        // re-export, so a restated trait of the same shape would not satisfy
        // it. This is what "the node registers this chain" means: the host's
        // map holds `Box<dyn lux_node::vm::Vm>`, and only the host's trait
        // coerces into it.
        let held: Box<dyn lux_node::vm::Vm> = Box::new(vm(1000));
        assert_eq!(held.name(), "P");

        let block: Box<dyn lux_node::vm::Block> = held.get(&held.last_accepted()).unwrap();
        assert_eq!(block.height(), 0);

        // And the id is one type, not two that happen to be the same bytes.
        let _: lux_node::vm::Id = block.id();
        let _: lux_consensus::finality::Id = block.id();
        let _: crate::ids::Id = block.id();
    }

    #[test]
    fn a_status_is_the_number_the_wire_carries() {
        assert_eq!(Status::Unknown as u8, 0);
        assert_eq!(Status::Processing as u8, 1);
        assert_eq!(Status::Rejected as u8, 2);
        assert_eq!(Status::Accepted as u8, 3);
    }

    /// A whole network, from bytes.
    ///
    /// This is the case the port exists for: hand a node the bytes a network
    /// was published as and it holds the first block, the first validator set
    /// and the first balance, with nobody having told it any of them.
    #[test]
    fn a_network_starts_from_the_bytes_it_was_published_as() {
        use crate::genesis::{Allocation, Genesis};
        use crate::signer::Signer;
        use crate::txs::Validator;

        let sk = blst::min_pk::SecretKey::key_gen(&[9u8; 32], &[]).unwrap();
        let owner = Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![ShortId([1; 20])],
        };
        let stake = vec![Output {
            asset: ASSET,
            stake_lock: 0,
            amount: 10 * MEGA,
            owners: owner.clone(),
        }];
        let vdr = Tx::new(
            Unsigned::AddPermissionlessValidator {
                base: Envelope {
                    network_id: 1,
                    blockchain_id: [0; 32],
                    outs: Vec::new(),
                    ins: Vec::new(),
                    memo: Vec::new(),
                },
                validator: Validator {
                    node_id: NodeId([5; 20]),
                    start: 0,
                    end: 1_000 + YEAR,
                    weight: 10 * MEGA,
                },
                chain: PRIMARY_NETWORK_ID,
                signer: Signer::prove(&sk),
                stake,
                validator_rewards_owner: owner.clone(),
                delegator_rewards_owner: owner.clone(),
                delegation_shares: 20_000,
            },
            Vec::new(),
        );
        let published = Genesis {
            utxos: vec![Allocation {
                utxo: Utxo {
                    id: UtxoId {
                        tx_id: [0; 32],
                        output_index: 0,
                    },
                    output: Output {
                        asset: ASSET,
                        stake_lock: 0,
                        amount: 5 * MEGA,
                        owners: owner,
                    },
                },
                message: Vec::new(),
            }],
            validators: vec![vdr.clone()],
            chains: Vec::new(),
            timestamp: 1_000,
            initial_supply: 400 * 1_000_000 * 1_000_000,
            message: "a network".to_string(),
        };
        let bytes = published.to_bytes();

        let vm = PlatformVm::from_genesis(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            &bytes,
        )
        .expect("the bytes are a genesis");

        // Two nodes reading the same publication agree about the first block
        // before they have agreed about anything else.
        let again = PlatformVm::from_genesis(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            &bytes,
        )
        .expect("the bytes are a genesis");
        assert_eq!(vm.last_accepted(), again.last_accepted());
        assert_eq!(vm.block_id_at(0), Ok(vm.last_accepted()));

        let first = vm.get(&vm.last_accepted()).expect("the first block");
        assert_eq!(first.height(), 0);
        assert_eq!(first.timestamp(), GENESIS_TIME);
        assert_eq!(first.parent(), published.id());

        // And the validator the publication admits is validating from the
        // start, with the reward it is owed already counted in the supply.
        let state = vm.state();
        let v = state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .expect("the genesis validator");
        assert_eq!(v.weight, 10 * MEGA);
        assert_eq!(v.tx_id, vdr.id());
        assert!(v.potential_reward > 0);
        assert_eq!(
            state.current_supply(&PRIMARY_NETWORK_ID).unwrap(),
            published.initial_supply + v.potential_reward
        );
        assert_eq!(state.timestamp(), 1_000);
    }

    #[test]
    fn bytes_that_are_not_a_genesis_do_not_start_a_chain() {
        assert!(PlatformVm::from_genesis(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            b"not a genesis"
        )
        .is_err());
    }

    /// A chain built with a given uptime source, so a test can say what this
    /// node saw.
    fn vm_seeing(now: u64, uptime: Box<dyn executor::Uptime>) -> PlatformVm {
        PlatformVm::new(
            config(),
            Box::new(FlatFees::default()),
            uptime,
            Box::new(executor::NoImports),
            genesis(now),
        )
    }

    /// Run a validator to the end of its term and return the option block this
    /// node would build to answer the reward proposal.
    fn answer_to_the_reward(vm: &PlatformVm, now: u64) -> Box<dyn Block> {
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();
        vm.accept(&blk.id()).unwrap();

        let end = now + YEAR;
        vm.set_clock(end);
        {
            let mut inner = vm.inner.lock().unwrap();
            inner.state.set_timestamp(end);
        }
        let proposal = vm.build().unwrap();
        vm.verify(&proposal.id()).unwrap();
        vm.accept(&proposal.id()).unwrap();
        vm.build().unwrap()
    }

    /// A validator that was there is paid.
    #[test]
    fn a_validator_that_was_reachable_is_paid() {
        let now = 1000;
        let vm = vm_seeing(now, Box::new(AlwaysUp));
        let answer = answer_to_the_reward(&vm, now);
        vm.verify(&answer.id()).unwrap();
        vm.accept(&answer.id()).unwrap();

        let staker_tx_id = vm.state().reward_utxos_by_tx().next().map(|(id, _)| *id);
        assert!(
            staker_tx_id.is_some(),
            "a validator that was there earns its reward"
        );
        assert!(vm.state().validator_set(&PRIMARY_NETWORK_ID).is_empty());
    }

    /// A validator that was not there gets its stake back and nothing else.
    ///
    /// This is the reward gate, and it is why a reward is a proposal: this
    /// node answers from what it saw, another node answers from what it saw,
    /// and the stake-weighted vote settles which answer the chain takes.
    ///
    /// Go: `TestMainnetStakeIsRefundedOnAbort`. "Nothing was minted" is only
    /// half the story, and on its own it is the half that reads as a
    /// catastrophe: the other half is that the PRINCIPAL comes back. Reward
    /// forfeiture is the only economic penalty the P-Chain has — there is no
    /// stake slashing anywhere in it — so the amount an uptime failure puts at
    /// risk is one year's emission, not the bond.
    #[test]
    fn a_validator_that_was_not_reachable_is_not_paid() {
        let now = 1000;
        let vm = vm_seeing(now, Box::new(NeverUp));
        let staker_tx = a_validator_tx(now);
        let answer = answer_to_the_reward(&vm, now);
        vm.verify(&answer.id()).unwrap();
        vm.accept(&answer.id()).unwrap();

        // Retired, and nothing was minted.
        assert!(vm.state().validator_set(&PRIMARY_NETWORK_ID).is_empty());
        assert_eq!(vm.state().reward_utxos_by_tx().count(), 0);

        // And the stake itself came back, in full, to the owner the staking
        // transaction named. This is the bound on the blast radius.
        let returned = vm
            .state()
            .utxo(
                &UtxoId {
                    tx_id: staker_tx.id(),
                    output_index: 0,
                }
                .input_id(),
            )
            .expect("the stake is returned on an abort as well as on a commit")
            .clone();
        assert_eq!(returned.output.amount, 10 * MEGA);
        assert_eq!(returned.output.owners.addrs, vec![ShortId([2; 20])]);
    }

    /// The size of that blast radius, in the numbers mainnet is carrying.
    ///
    /// Go states these in `uptime_forfeiture_mainnet_test.go`, read off
    /// mainnet (96369): five validators, each bonded with a weight of
    /// 500,000,000,000,000,000 base units, carrying 165,583,347,962,466,973 of
    /// potential reward between them. What an uptime abort forfeits is the
    /// second number; the first is refunded either way.
    #[test]
    fn what_an_uptime_abort_forfeits_is_a_years_emission_not_the_bond() {
        const STAKE_PER_VALIDATOR: u64 = 500_000_000_000_000_000;
        const VALIDATORS: u64 = 5;
        const POTENTIAL_REWARD_TOTAL: u64 = 165_583_347_962_466_973;

        let total_stake = STAKE_PER_VALIDATOR * VALIDATORS;
        assert_eq!(total_stake, 2_500_000_000_000_000_000);

        // ~6.6% of the bonded stake — a year's emission at the mainnet rates,
        // not the principal.
        let ratio = POTENTIAL_REWARD_TOTAL as f64 / total_stake as f64;
        assert!(
            (ratio - 0.0662).abs() < 0.001,
            "rewards are {ratio} of the bonded stake, expected ~0.0662"
        );
    }

    /// Go: `config.TestUngovernedNodeIsUnchanged`, driven through the live
    /// reward gate.
    ///
    /// A node that carries no staking history must resolve exactly the policy
    /// it was compiled with, at every instant — so shipping the governed
    /// lookup changes nothing on any live network until stake votes. The
    /// compiled requirement here is 80%; a validator that was 85% reachable is
    /// paid, and the answer must not depend on when it bonded.
    #[test]
    fn an_ungoverned_node_is_judged_on_its_compiled_policy() {
        struct Reachable(f64);
        impl executor::Uptime for Reachable {
            fn fraction_since(&self, _: &NodeId, _: &Id, _: u64) -> Option<f64> {
                Some(self.0)
            }
        }

        let ungoverned = config();
        assert!(
            ungoverned.staking_history.is_none(),
            "this is what an ungoverned node looks like"
        );
        assert_eq!(ungoverned.staking.uptime_requirement, 800_000);

        // Two nodes bonding at instants a governed chain would treat
        // differently. An ungoverned one must treat them the same.
        for bonded_at in [1_000u64, 1_785_000_000] {
            for (reachable, paid) in [(0.85, 1usize), (0.75, 0usize)] {
                let vm = PlatformVm::new(
                    ungoverned.clone(),
                    Box::new(FlatFees::default()),
                    Box::new(Reachable(reachable)),
                    Box::new(executor::NoImports),
                    genesis(bonded_at),
                );
                let answer = answer_to_the_reward(&vm, bonded_at);
                vm.verify(&answer.id()).unwrap();
                vm.accept(&answer.id()).unwrap();
                assert_eq!(
                    vm.state().reward_utxos_by_tx().count(),
                    paid,
                    "bonded at {bonded_at}, {reachable} reachable, compiled bar 80%"
                );
            }
        }
    }

    /// Go: `block/builder.TestPermissionedValidatorIsNeverRewarded`.
    ///
    /// A permissioned chain validator put up no stake, so it has no reward to
    /// collect and never leaves the set through one. Naming it as the staker
    /// to pay would mint a reward nobody staked for.
    #[test]
    fn a_permissioned_chain_validator_is_never_rewarded() {
        use crate::state::Staker;
        use crate::txs::Priority;

        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);

        // A permissioned chain validator whose term has already ended: the one
        // the packing loop would reach for first if it did not check.
        {
            let mut inner = vm.inner.lock().unwrap();
            inner
                .state
                .put_current_validator(Staker {
                    tx_id: [0xEE; 32],
                    node_id: NodeId([8; 20]),
                    public_key: None,
                    chain: [0xAA; 32],
                    weight: 1,
                    start_time: 0,
                    end_time: now,
                    potential_reward: 0,
                    next_time: now,
                    priority: Priority::ChainPermissionedValidatorCurrent,
                })
                .unwrap();
            inner.state.set_timestamp(now);
        }

        // It is the next staker to leave, and it is still not paid: there is
        // nothing to build, rather than a reward proposal naming it.
        assert!(vm
            .state()
            .next_current_staker()
            .is_some_and(|s| s.priority.is_permissioned_validator()));
        assert_eq!(
            vm.build().err(),
            Some(Error::Empty),
            "a reward proposal was built for a validator that staked nothing"
        );
    }

    /// Go: `block/executor.TestGetState` — which state a block is verified
    /// against, in all four cases the map can be in.
    #[test]
    fn the_state_a_block_is_verified_against() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let standard = vm.build().unwrap();
        vm.verify(&standard.id()).unwrap();

        let inner = vm.inner.lock().unwrap();

        // The last accepted block is answered from the chain's own state.
        let last = inner.last_accepted;
        assert_eq!(
            inner.parent_state(&last).map(|s| s.timestamp()),
            Some(inner.state.timestamp())
        );

        // A verified block that leaves ONE state is answered with it.
        assert!(inner.parent_state(&standard.id()).is_some());

        // A block nobody has verified and that is not the last accepted has no
        // state to be built on — the map is not consulted for a guess.
        assert!(inner.parent_state(&[0x5A; 32]).is_none());
        drop(inner);

        // A verified PROPOSAL block leaves two states, and neither is "the"
        // state: only a commit or an abort may sit on it, and they say which.
        let end = now + YEAR;
        vm.accept(&standard.id()).unwrap();
        vm.set_clock(end);
        {
            let mut inner = vm.inner.lock().unwrap();
            inner.state.set_timestamp(end);
        }
        let proposal = vm.build().unwrap();
        vm.verify(&proposal.id()).unwrap();
        let inner = vm.inner.lock().unwrap();
        assert!(
            inner.verified[&proposal.id()].on_abort.is_some(),
            "a reward proposal leaves both futures"
        );
        assert!(
            inner.parent_state(&proposal.id()).is_none(),
            "a proposal block was handed out as though it left one state"
        );
    }

    /// Go: `block/executor.TestBackendGetBlock`.
    #[test]
    fn a_block_is_found_by_the_name_it_was_given() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let built = vm.build().unwrap();

        // Built here: found, and it is the same block.
        let got = vm.get(&built.id()).expect("a block this node built");
        assert_eq!(got.id(), built.id());
        assert_eq!(got.height(), built.height());
        assert_eq!(got.bytes(), built.bytes());

        // Never seen: not found, rather than an empty block.
        assert_eq!(vm.get(&[0x5A; 32]).err(), Some(Error::NotFound));

        // Arrived off the wire: found from that point on, under the name the
        // bytes give it.
        let elsewhere = PlatformVm::new(
            config(),
            Box::new(FlatFees::default()),
            Box::new(AlwaysUp),
            Box::new(executor::NoImports),
            genesis(now),
        );
        assert_eq!(elsewhere.get(&built.id()).err(), Some(Error::NotFound));
        let parsed = elsewhere.parse(&built.bytes()).expect("it reads");
        assert_eq!(parsed.id(), built.id());
        assert_eq!(elsewhere.get(&built.id()).unwrap().id(), built.id());
    }

    /// Go: `block/executor.TestGetTimestamp`.
    ///
    /// The timestamp a block is verified against is the one its parent left,
    /// not the wall clock — otherwise two nodes verifying the same block at
    /// different moments would reach different states.
    #[test]
    fn the_timestamp_a_block_is_verified_against() {
        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        vm.verify(&blk.id()).unwrap();

        let inner = vm.inner.lock().unwrap();
        let last = inner.last_accepted;
        assert_eq!(
            inner.parent_state(&last).unwrap().timestamp(),
            now,
            "the last accepted block is at the chain's own time"
        );
        assert_eq!(
            inner.parent_state(&blk.id()).unwrap().timestamp(),
            blk.timestamp(),
            "a verified block is at the time it states"
        );
        drop(inner);

        // The wall clock moving does not move it.
        vm.set_clock(now + 10 * DAY);
        let inner = vm.inner.lock().unwrap();
        assert_eq!(
            inner.parent_state(&blk.id()).unwrap().timestamp(),
            blk.timestamp()
        );
    }

    /// Go: `block/executor.TestVerifiedHeightsConcurrentAccessIsSafe`.
    ///
    /// The node starts a goroutine per inbound message, so two blocks can be in
    /// verification at once. In Go the state a block leaves lived behind a lock
    /// that guarded the MAP and not the struct it handed out, and two threads
    /// reading and writing that bare map abort the process outright — a hard
    /// abort, not a panic, so nothing can recover and a restart lands back in
    /// the same race.
    ///
    /// Here the whole chain is behind one lock and the seam takes `&self`, so
    /// the arrangement that made that possible cannot be built: the compile-time
    /// half of this test is that `PlatformVm` is `Sync` at all.
    #[test]
    fn verifying_from_many_threads_at_once_is_safe() {
        fn assert_shareable<T: Send + Sync>() {}
        assert_shareable::<PlatformVm>();

        let now = 1000;
        let vm = vm(now);
        vm.set_clock(now);
        vm.submit(a_validator_tx(now)).unwrap();
        let blk = vm.build().unwrap();
        let id = blk.id();

        std::thread::scope(|s| {
            for _ in 0..64 {
                s.spawn(|| assert_eq!(vm.verify(&id), Ok(())));
                s.spawn(|| assert_eq!(vm.get(&id).map(|b| b.id()), Ok(id)));
                s.spawn(|| {
                    let _ = vm.last_accepted();
                });
            }
        });

        // Sixty-four verifications of one block leave exactly one held state,
        // and the chain still accepts it.
        {
            let inner = vm.inner.lock().unwrap();
            assert_eq!(inner.verified.len(), 1);
        }
        vm.accept(&id).unwrap();
        assert_eq!(vm.last_accepted(), id);
        assert_eq!(vm.state().validator_set(&PRIMARY_NETWORK_ID).len(), 1);
    }

    /// "Cannot say" is answered by paying.
    ///
    /// A node that has only just started watching has no measurement, and Go
    /// says why it pays anyway: erring toward over-rewarding costs the network
    /// a little emission, and erring the other way lets an unusual case or a
    /// hostile proposer take an honest validator's reward.
    #[test]
    fn a_node_that_cannot_say_pays() {
        let now = 1000;
        let vm = vm_seeing(now, Box::new(Unknown));
        let answer = answer_to_the_reward(&vm, now);
        vm.verify(&answer.id()).unwrap();
        vm.accept(&answer.id()).unwrap();
        assert_eq!(vm.state().reward_utxos_by_tx().count(), 1);
    }

    /// The requirement a validator is judged against is the one that was in
    /// force when it BONDED.
    ///
    /// This is the property the whole governance argument rests on. The
    /// validator below bonds under an 80% rule and is 85% reachable. A later
    /// vote raises the rule to 95%. It is still paid, because governance binds
    /// the future — raising the bar the day before a rival's stake matures and
    /// taking its reward is expropriation, not policy.
    #[test]
    fn a_validator_is_judged_on_the_terms_it_bonded_under() {
        use crate::stakingparams::{Entry, History, Params, MAINNET_GENESIS};

        let now = 1000;
        let bonded_at = now;
        let voted_at = (now + 10) as i64;

        let mut governed = config();
        governed.staking_history = Some(History(vec![
            Entry {
                activation: 0,
                params: Params {
                    uptime_requirement: 800_000,
                    ..MAINNET_GENESIS
                },
            },
            Entry {
                activation: voted_at,
                params: Params {
                    uptime_requirement: 950_000,
                    ..MAINNET_GENESIS
                },
            },
        ]));

        struct Reachable(f64);
        impl executor::Uptime for Reachable {
            fn fraction_since(&self, _: &NodeId, _: &Id, _: u64) -> Option<f64> {
                Some(self.0)
            }
        }

        let vm = PlatformVm::new(
            governed,
            Box::new(FlatFees::default()),
            Box::new(Reachable(0.85)),
            Box::new(executor::NoImports),
            genesis(now),
        );
        let answer = answer_to_the_reward(&vm, bonded_at);
        vm.verify(&answer.id()).unwrap();
        vm.accept(&answer.id()).unwrap();
        assert_eq!(
            vm.state().reward_utxos_by_tx().count(),
            1,
            "a validator bonded under an 80% rule is judged at 80%"
        );
    }
}
