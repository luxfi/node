// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The P-Chain, as the node sees it.
//!
//! The node holds several chains at once and does the same five things to each
//! of them: build, parse, verify, accept, reject. That is the seam, and it is
//! stated here exactly as `lux-rs/node`'s `src/vm.rs` states it — same
//! methods, same `Id` (`lux_consensus::finality::Id`), same errors — so the
//! host's trait is satisfied by naming this type, with nothing in between to
//! translate.
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
use std::fmt;
use std::sync::Mutex;

pub use lux_consensus::finality::Id;

use crate::block;
use crate::executor::{self, Config, Fees};
use crate::ids::{hash256, EMPTY, PRIMARY_NETWORK_ID};
use crate::state::State;
use crate::txs::{Tx, Unsigned};

/// Where a block stands. The numbers are the ones that cross the wire.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Status {
    Unknown = 0,
    Processing = 1,
    Rejected = 2,
    Accepted = 3,
}

/// What a chain can refuse for.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    NotFound,
    Malformed(String),
    Invalid(String),
    /// Nothing to build.
    Empty,
    NoMethod(String),
    BadRequest(String),
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::NotFound => write!(f, "not found"),
            Error::Malformed(why) => write!(f, "malformed: {why}"),
            Error::Invalid(why) => write!(f, "invalid: {why}"),
            Error::Empty => write!(f, "nothing to build"),
            Error::NoMethod(m) => write!(f, "the method {m} does not exist"),
            Error::BadRequest(why) => write!(f, "{why}"),
        }
    }
}

impl std::error::Error for Error {}

/// One block, as consensus sees it.
pub trait Block: Send + Sync {
    fn id(&self) -> Id;
    fn parent(&self) -> Id;
    fn height(&self) -> u64;
    fn timestamp(&self) -> u64;
    fn bytes(&self) -> Vec<u8>;
    /// The root of the state this block leaves behind.
    fn state_root(&self) -> Id;
    /// The root of what this block carries.
    fn payload_root(&self) -> Id;
}

/// A chain.
pub trait Vm: Send + Sync {
    fn name(&self) -> &'static str;
    fn version(&self) -> String;
    fn build(&self) -> Result<Box<dyn Block>, Error>;
    fn parse(&self, raw: &[u8]) -> Result<Box<dyn Block>, Error>;
    fn get(&self, id: &Id) -> Result<Box<dyn Block>, Error>;
    fn verify(&self, id: &Id) -> Result<(), Error>;
    fn accept(&self, id: &Id) -> Result<(), Error>;
    fn reject(&self, id: &Id) -> Result<(), Error>;
    fn set_preference(&self, id: &Id) -> Result<(), Error>;
    fn last_accepted(&self) -> Id;
    fn block_id_at(&self, height: u64) -> Result<Id, Error>;
    fn health(&self) -> Result<(), Error> {
        Ok(())
    }
    fn call(&self, method: &str, params: &serde_json::Value) -> Result<serde_json::Value, Error>;
}

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
    /// The state as of the last accepted block.
    state: State,
    /// Every block this chain knows, accepted or not.
    blocks: HashMap<Id, block::Block>,
    /// What each verified block would leave behind.
    verified: HashMap<Id, Verified>,
    accepted_by_height: HashMap<u64, Id>,
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
        let _ = state_root;
        PlatformVm {
            inner: Mutex::new(Inner {
                config,
                fees,
                state: genesis_state,
                blocks,
                verified: HashMap::new(),
                accepted_by_height,
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
        genesis_bytes: &[u8],
    ) -> Result<PlatformVm, crate::genesis::Error> {
        let published = crate::genesis::Genesis::parse(genesis_bytes)?;
        let rewards = crate::reward::Calculator::new(config.reward);
        let state = published.state(&rewards)?;
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
                state,
                blocks,
                verified: HashMap::new(),
                accepted_by_height,
                last_accepted: id,
                last_accepted_height: 0,
                preference: id,
                mempool: Vec::new(),
                now,
            }),
        })
    }

    /// Hand the chain a transaction to include.
    pub fn submit(&self, tx: Tx) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        tx.syntactic_verify(inner.config.native_asset)
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
        let state_root = self
            .verified
            .get(&blk.id())
            .map(|v| v.state_root)
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

        // An outstanding question is answered first.
        if let Some(v) = inner.verified.get(&preference) {
            if v.on_abort.is_some() {
                let blk = block::Block::commit(preference, height, state.timestamp());
                inner.blocks.insert(blk.id(), blk.clone());
                return Ok(Box::new(inner.wrap(blk)));
            }
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
                    executor::execute_standard(&mut state, tx, &inner.config, inner.fees.as_ref())
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
        if v.on_abort.is_none() {
            inner.state = v.on_commit;
        }
        inner.last_accepted = *id;
        inner.last_accepted_height = v.block.height();
        inner.accepted_by_height.insert(v.block.height(), *id);
        inner.preference = *id;

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
                let validators: Vec<serde_json::Value> = inner
                    .state
                    .validator_set(&chain)
                    .into_iter()
                    .map(|(node, weight, key)| {
                        serde_json::json!({
                            "nodeID": hex(&node.0),
                            "weight": weight,
                            "publicKey": key.map(|k| hex(&k)),
                        })
                    })
                    .collect();
                Ok(serde_json::json!({ "validators": validators }))
            }
            other => Err(Error::NoMethod(other.to_string())),
        }
    }
}

impl Inner {
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
    use crate::txs::{Envelope, Validator};

    const ASSET: Id = [9u8; 32];
    const DAY: u64 = 24 * 60 * 60;
    const YEAR: u64 = 365 * DAY;
    const MEGA: u64 = 1_000_000_000_000;

    fn config() -> Config {
        Config {
            native_asset: ASSET,
            staking: StakingPolicy {
                min_validator_stake: 2 * MEGA,
                max_validator_stake: 3_000 * MEGA,
                min_delegator_stake: MEGA / 40,
                min_stake_duration: 2 * 7 * DAY,
                max_stake_duration: YEAR,
                min_delegation_fee: 20_000,
            },
            reward: reward::Config {
                max_consumption_rate: 120_000,
                min_consumption_rate: 100_000,
                minting_period: std::time::Duration::from_secs(YEAR),
                supply_cap: 720 * 1_000_000 * 1_000_000,
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
        PlatformVm::new(config(), Box::new(FlatFees::default()), genesis(now))
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
        PlatformVm::new(config(), Box::new(FlatFees::default()), genesis(now))
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

        let vm = PlatformVm::from_genesis(config(), Box::new(FlatFees::default()), &bytes)
            .expect("the bytes are a genesis");

        // Two nodes reading the same publication agree about the first block
        // before they have agreed about anything else.
        let again = PlatformVm::from_genesis(config(), Box::new(FlatFees::default()), &bytes)
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
            b"not a genesis"
        )
        .is_err());
    }
}
