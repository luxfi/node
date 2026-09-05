// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What the chain writes down, and how it reads itself back.
//!
//! One function says what the accepted state *is*, as records
//! ([`records`]); one says how to build a state out of records
//! ([`restore`]); and one writes the difference between two states
//! ([`flush`]). Nothing else knows the record layout, so the shape written and
//! the shape read cannot drift — a round trip is checked by comparing the
//! records a restored state produces against the ones it was built from, which
//! is a comparison of the whole state rather than of the fields somebody
//! remembered to check.
//!
//! **The difference, not a snapshot.** A height writes only the records it
//! changed, found by comparing the state it replaced with the state it
//! installed. That keeps the I/O proportional to what the block did, which is
//! what a chain needs; the comparison itself walks the state, which is the
//! price of not threading a dirty mark through every writer in [`crate::state`]
//! and letting the two fall out of step.
//!
//! Values are ZAP, because ZAP is the only thing chain values are written in
//! here. Where a value already has a wire form — a transaction, an unspent
//! output — that form is used rather than a second one: two encodings of a
//! transaction are two answers to what its id is.

use std::collections::BTreeMap;

use crate::block;
use crate::components::{Owners, Utxo};
use crate::ids::{Id, NodeId, ShortId};
use crate::l1::{Expiry, Validator as L1Validator};
use crate::state::{Conversion, Staker, State};
use crate::store::{self, Store};
use crate::txs::{Priority, Tx};
use crate::validators::{Change, History, WeightDiff, Where};
use crate::zap;

/// A record's kind. One byte, first, so a scan by kind is a prefix scan.
mod kind {
    pub const TIMESTAMP: u8 = b'T';
    pub const ACCRUED_FEES: u8 = b'A';
    pub const L1_EXCESS: u8 = b'X';
    pub const CHAIN_OWNER: u8 = b'C';
    pub const SUPPLY: u8 = b'S';
    pub const UTXO: u8 = b'U';
    pub const REWARD_UTXOS: u8 = b'W';
    pub const TX: u8 = b'K';
    pub const BLOCKCHAIN: u8 = b'G';
    pub const CHAIN_NAME: u8 = b'N';
    pub const STAKER: u8 = b'V';
    pub const L1_VALIDATOR: u8 = b'L';
    pub const EXPIRY: u8 = b'E';
    pub const CONVERSION: u8 = b'O';
    pub const TRANSFORMATION: u8 = b'F';
    pub const DELEGATEE_REWARD: u8 = b'R';
    pub const SET_CHANGE: u8 = b'H';
    pub const BLOCK: u8 = b'B';
    pub const BLOCK_ROOT: u8 = b'D';
    pub const ACCEPTED_AT: u8 = b'I';
    pub const LAST_ACCEPTED: u8 = b'P';
}

/// Why a store's contents are not a chain.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// A record whose bytes do not read back as the thing its key says it is.
    Unreadable(&'static str),
    /// The store said no.
    Store(store::Error),
    /// A restored state the chain refuses — two stakers claiming one seat, an
    /// L1 validator changing something fixed for the life of its name.
    Rejected(&'static str),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Unreadable(what) => write!(f, "a stored {what} does not read back"),
            Error::Store(e) => write!(f, "{e}"),
            Error::Rejected(why) => write!(f, "what was stored is not a state: {why}"),
        }
    }
}

impl std::error::Error for Error {}

impl From<store::Error> for Error {
    fn from(e: store::Error) -> Error {
        Error::Store(e)
    }
}

// ---- keys -----------------------------------------------------------------

fn key(kind: u8, rest: &[&[u8]]) -> Vec<u8> {
    let mut k = vec![kind];
    for part in rest {
        k.extend_from_slice(part);
    }
    k
}

/// A staker's seat: which set it is in, then network, node and name.
///
/// The network comes before the node so one network's set is a prefix scan,
/// and the name is last so two delegators on one node are two rows rather than
/// one that overwrites the other.
fn staker_key(pending: bool, s: &Staker) -> Vec<u8> {
    key(
        kind::STAKER,
        &[
            &[pending as u8],
            &s.chain,
            &s.node_id.0,
            &s.tx_id,
        ],
    )
}

fn change_key(height: u64, at: &Where) -> Vec<u8> {
    key(
        kind::SET_CHANGE,
        &[&height.to_be_bytes(), &at.chain, &at.node.0],
    )
}

// ---- values ---------------------------------------------------------------

fn u64_value(v: u64) -> Vec<u8> {
    v.to_le_bytes().to_vec()
}

fn read_u64(raw: &[u8], what: &'static str) -> Result<u64, Error> {
    raw.try_into()
        .map(u64::from_le_bytes)
        .map_err(|_| Error::Unreadable(what))
}

/// Owners: a locktime, a threshold, and the addresses, packed end to end.
///
/// Go writes this same triple in `secp256k1fx.OutputOwners`; the layout here is
/// this store's own, because what is written down is not what crosses the wire.
fn owners_value(o: &Owners) -> Vec<u8> {
    let mut b = zap::Builder::new(64 + o.addrs.len() * 20);
    let ob = b.start_object(24);
    b.set_u64(&ob, 0, o.locktime);
    b.set_u32(&ob, 8, o.threshold);
    let flat: Vec<u8> = o.addrs.iter().flat_map(|a| a.0).collect();
    b.set_bytes(&ob, 16, &flat);
    b.finish_as_root(&ob);
    b.finish()
}

fn read_owners(raw: &[u8]) -> Result<Owners, Error> {
    let m = zap::Message::parse(raw).map_err(|_| Error::Unreadable("owner"))?;
    let o = m.root();
    let flat = o.bytes(16);
    if flat.len() % 20 != 0 {
        return Err(Error::Unreadable("owner"));
    }
    Ok(Owners {
        locktime: o.u64(0),
        threshold: o.u32(8),
        addrs: flat.as_chunks::<20>().0.iter().copied().map(ShortId).collect(),
    })
}

fn staker_value(s: &Staker) -> Vec<u8> {
    let mut b = zap::Builder::new(192);
    let ob = b.start_object(72);
    b.set_u64(&ob, 0, s.weight);
    b.set_u64(&ob, 8, s.start_time);
    b.set_u64(&ob, 16, s.end_time);
    b.set_u64(&ob, 24, s.potential_reward);
    b.set_u64(&ob, 32, s.next_time);
    b.set_u8(&ob, 40, s.priority as u8);
    // A key is present or it is not; a validator registered before keys existed
    // has none, and an all-zero key would be a key that verifies nothing.
    b.set_u8(&ob, 41, s.public_key.is_some() as u8);
    b.set_bytes(&ob, 48, s.public_key.as_ref().map(|k| &k[..]).unwrap_or(&[]));
    b.finish_as_root(&ob);
    b.finish()
}

fn read_staker(k: &[u8], raw: &[u8]) -> Result<(bool, Staker), Error> {
    // key = kind ‖ pending ‖ chain(32) ‖ node(20) ‖ tx(32)
    if k.len() != 1 + 1 + 32 + 20 + 32 {
        return Err(Error::Unreadable("staker key"));
    }
    let m = zap::Message::parse(raw).map_err(|_| Error::Unreadable("staker"))?;
    let o = m.root();
    let priority = priority_from(o.u8(40)).ok_or(Error::Unreadable("staker priority"))?;
    let public_key = if o.u8(41) == 0 {
        None
    } else {
        let bytes = o.bytes(48);
        Some(<[u8; 48]>::try_from(bytes).map_err(|_| Error::Unreadable("staker key"))?)
    };
    Ok((
        k[1] != 0,
        Staker {
            chain: k[2..34].try_into().unwrap(),
            node_id: NodeId(k[34..54].try_into().unwrap()),
            tx_id: k[54..86].try_into().unwrap(),
            public_key,
            weight: o.u64(0),
            start_time: o.u64(8),
            end_time: o.u64(16),
            potential_reward: o.u64(24),
            next_time: o.u64(32),
            priority,
        },
    ))
}

/// The inverse of `Priority as u8`. Written here rather than derived, because a
/// number that names no priority has to be refused rather than rounded to one.
fn priority_from(v: u8) -> Option<Priority> {
    Some(match v {
        1 => Priority::PrimaryNetworkDelegatorLegacyPending,
        2 => Priority::PrimaryNetworkValidatorPending,
        3 => Priority::PrimaryNetworkDelegatorPermissionlessPending,
        4 => Priority::ChainPermissionlessValidatorPending,
        5 => Priority::ChainPermissionlessDelegatorPending,
        6 => Priority::ChainPermissionedValidatorPending,
        7 => Priority::ChainPermissionedValidatorCurrent,
        8 => Priority::ChainPermissionlessDelegatorCurrent,
        9 => Priority::ChainPermissionlessValidatorCurrent,
        10 => Priority::PrimaryNetworkDelegatorCurrent,
        11 => Priority::PrimaryNetworkValidatorCurrent,
        _ => return None,
    })
}

fn l1_value(v: &L1Validator) -> Vec<u8> {
    let mut b = zap::Builder::new(384);
    let ob = b.start_object(112);
    b.set_bytes_fixed(&ob, 0, &v.chain_id);
    b.set_bytes_fixed(&ob, 32, &v.node_id.0);
    b.set_u64(&ob, 56, v.start_time);
    b.set_u64(&ob, 64, v.weight);
    b.set_u64(&ob, 72, v.min_nonce);
    b.set_u64(&ob, 80, v.end_accumulated_fee);
    b.set_bytes(&ob, 88, &v.public_key);
    b.set_bytes(&ob, 96, &v.remaining_balance_owner);
    b.set_bytes(&ob, 104, &v.deactivation_owner);
    b.finish_as_root(&ob);
    b.finish()
}

fn read_l1(id: Id, raw: &[u8]) -> Result<L1Validator, Error> {
    let m = zap::Message::parse(raw).map_err(|_| Error::Unreadable("l1 validator"))?;
    let o = m.root();
    Ok(L1Validator {
        validation_id: id,
        chain_id: o.id(0),
        node_id: NodeId(o.short_id(32)),
        start_time: o.u64(56),
        weight: o.u64(64),
        min_nonce: o.u64(72),
        end_accumulated_fee: o.u64(80),
        public_key: o.bytes(88).to_vec(),
        remaining_balance_owner: o.bytes(96).to_vec(),
        deactivation_owner: o.bytes(104).to_vec(),
    })
}

fn conversion_value(c: &Conversion) -> Vec<u8> {
    let mut b = zap::Builder::new(160);
    let ob = b.start_object(72);
    b.set_bytes_fixed(&ob, 0, &c.conversion_id);
    b.set_bytes_fixed(&ob, 32, &c.chain_id);
    b.set_bytes(&ob, 64, &c.address);
    b.finish_as_root(&ob);
    b.finish()
}

fn read_conversion(raw: &[u8]) -> Result<Conversion, Error> {
    let m = zap::Message::parse(raw).map_err(|_| Error::Unreadable("conversion"))?;
    let o = m.root();
    Ok(Conversion {
        conversion_id: o.id(0),
        chain_id: o.id(32),
        address: o.bytes(64).to_vec(),
    })
}

fn change_value(c: &Change) -> Vec<u8> {
    let mut b = zap::Builder::new(320);
    let ob = b.start_object(64);
    b.set_u64(&ob, 0, c.weight.amount);
    b.set_u8(&ob, 8, c.weight.decrease as u8);
    b.set_u8(&ob, 9, c.renamed as u8);
    b.set_bytes_fixed(&ob, 16, &c.validation);
    b.set_bytes(&ob, 48, &c.key_before);
    b.set_bytes(&ob, 56, &c.key_after);
    b.finish_as_root(&ob);
    b.finish()
}

fn read_change(raw: &[u8]) -> Result<Change, Error> {
    let m = zap::Message::parse(raw).map_err(|_| Error::Unreadable("set change"))?;
    let o = m.root();
    Ok(Change {
        weight: WeightDiff {
            amount: o.u64(0),
            decrease: o.u8(8) != 0,
        },
        renamed: o.u8(9) != 0,
        validation: o.id(16),
        key_before: o.bytes(48).to_vec(),
        key_after: o.bytes(56).to_vec(),
    })
}

/// A transaction's reward outputs, each as the wire form it already has, laid
/// end to end behind its own length.
fn utxos_value(utxos: &[Utxo]) -> Vec<u8> {
    let mut out = Vec::new();
    for u in utxos {
        let wire = u.wire_bytes();
        out.extend_from_slice(&(wire.len() as u32).to_le_bytes());
        out.extend_from_slice(&wire);
    }
    out
}

fn read_utxos(mut raw: &[u8]) -> Result<Vec<Utxo>, Error> {
    let mut out = Vec::new();
    while !raw.is_empty() {
        if raw.len() < 4 {
            return Err(Error::Unreadable("reward outputs"));
        }
        let n = u32::from_le_bytes(raw[..4].try_into().unwrap()) as usize;
        if raw.len() < 4 + n {
            return Err(Error::Unreadable("reward outputs"));
        }
        out.push(Utxo::parse_wire(&raw[4..4 + n]).map_err(|_| Error::Unreadable("reward output"))?);
        raw = &raw[4 + n..];
    }
    Ok(out)
}

// ---- the state, as records ------------------------------------------------

/// Everything the accepted state holds, as the rows that hold it.
///
/// This is the whole definition of what is durable. A field of [`State`] that
/// is not here is a field a restarted chain does not have.
pub fn records(state: &State) -> BTreeMap<Vec<u8>, Vec<u8>> {
    let mut out = BTreeMap::new();

    out.insert(key(kind::TIMESTAMP, &[]), u64_value(state.timestamp()));
    out.insert(
        key(kind::ACCRUED_FEES, &[]),
        u64_value(state.accrued_fees()),
    );
    out.insert(key(kind::L1_EXCESS, &[]), u64_value(state.l1_excess()));

    for chain in state.chains() {
        out.insert(
            key(kind::CHAIN_OWNER, &[chain]),
            owners_value(state.chain_owner(chain).expect("a network has an owner")),
        );
    }
    // The primary network's supply is not a network row: it has no creating
    // transaction and so is not in `chains`.
    for chain in state
        .chains()
        .copied()
        .chain(std::iter::once(crate::ids::PRIMARY_NETWORK_ID))
    {
        if let Ok(supply) = state.current_supply(&chain) {
            out.insert(key(kind::SUPPLY, &[&chain]), u64_value(supply));
        }
    }

    for (id, utxo) in state.utxos() {
        out.insert(key(kind::UTXO, &[id]), utxo.wire_bytes());
    }
    for (tx_id, utxos) in state.reward_utxos_by_tx() {
        out.insert(key(kind::REWARD_UTXOS, &[tx_id]), utxos_value(utxos));
    }
    for (id, tx) in state.txs() {
        out.insert(key(kind::TX, &[id]), tx.bytes().to_vec());
    }
    for (i, id) in state.blockchains().iter().enumerate() {
        out.insert(
            key(kind::BLOCKCHAIN, &[&(i as u32).to_be_bytes()]),
            id.to_vec(),
        );
    }
    for name in state.chain_names() {
        out.insert(key(kind::CHAIN_NAME, &[name.as_bytes()]), Vec::new());
    }

    for s in state.current_stakers() {
        out.insert(staker_key(false, s), staker_value(s));
    }
    for s in state.pending_stakers() {
        out.insert(staker_key(true, s), staker_value(s));
    }

    for v in state.all_l1_validators() {
        out.insert(key(kind::L1_VALIDATOR, &[&v.validation_id]), l1_value(v));
    }
    for e in state.expiries() {
        out.insert(key(kind::EXPIRY, &[&e.marshal()]), Vec::new());
    }
    for (chain, c) in state.conversions() {
        out.insert(key(kind::CONVERSION, &[chain]), conversion_value(c));
    }
    for (chain, tx) in state.transformations() {
        out.insert(key(kind::TRANSFORMATION, &[chain]), tx.bytes().to_vec());
    }
    for ((chain, node), amount) in state.delegatee_rewards() {
        out.insert(
            key(kind::DELEGATEE_REWARD, &[chain, &node.0]),
            u64_value(*amount),
        );
    }
    out
}

/// Everything a validator-set history holds, as rows.
pub fn history_records(history: &History) -> BTreeMap<Vec<u8>, Vec<u8>> {
    let mut out = BTreeMap::new();
    for (height, at) in history.heights() {
        for (place, change) in at {
            out.insert(change_key(*height, place), change_value(change));
        }
    }
    out
}

/// The state those records describe.
pub fn restore(store: &dyn Store) -> Result<State, Error> {
    let mut state = State::new();

    if let Some(raw) = store.get(&key(kind::TIMESTAMP, &[])) {
        state.set_timestamp(read_u64(&raw, "timestamp")?);
    }
    if let Some(raw) = store.get(&key(kind::ACCRUED_FEES, &[])) {
        state.set_accrued_fees(read_u64(&raw, "accrued fees")?);
    }
    if let Some(raw) = store.get(&key(kind::L1_EXCESS, &[])) {
        state.set_l1_excess(read_u64(&raw, "excess")?);
    }

    for (k, v) in store.scan(&[kind::CHAIN_OWNER]) {
        let chain = id_from(&k, 1)?;
        state.add_chain(chain, read_owners(&v)?);
    }
    for (k, v) in store.scan(&[kind::SUPPLY]) {
        state.set_current_supply(id_from(&k, 1)?, read_u64(&v, "supply")?);
    }
    for (_, v) in store.scan(&[kind::UTXO]) {
        state.add_utxo(Utxo::parse_wire(&v).map_err(|_| Error::Unreadable("unspent output"))?);
    }
    for (k, v) in store.scan(&[kind::REWARD_UTXOS]) {
        let tx_id = id_from(&k, 1)?;
        for utxo in read_utxos(&v)? {
            state.add_reward_utxo(tx_id, utxo);
        }
    }
    for (_, v) in store.scan(&[kind::TX]) {
        state.add_tx(Tx::parse(&v).map_err(|_| Error::Unreadable("transaction"))?);
    }
    // In index order, which is the order the rows are keyed in, which is the
    // order the blockchains were created in.
    for (_, v) in store.scan(&[kind::BLOCKCHAIN]) {
        state.add_blockchain(id_from(&v, 0)?, "");
    }
    for (k, _) in store.scan(&[kind::CHAIN_NAME]) {
        let name = std::str::from_utf8(&k[1..]).map_err(|_| Error::Unreadable("network name"))?;
        state.take_chain_name(name);
    }

    for (k, v) in store.scan(&[kind::STAKER]) {
        let (pending, staker) = read_staker(&k, &v)?;
        match (pending, staker.priority.is_validator()) {
            (false, true) => state
                .put_current_validator(staker)
                .map_err(|_| Error::Rejected("two validators in one seat"))?,
            (true, true) => state
                .put_pending_validator(staker)
                .map_err(|_| Error::Rejected("two validators in one seat"))?,
            (false, false) => state.put_current_delegator(staker),
            (true, false) => state.put_pending_delegator(staker),
        }
    }

    for (k, v) in store.scan(&[kind::L1_VALIDATOR]) {
        state
            .put_l1_validator(read_l1(id_from(&k, 1)?, &v)?)
            .map_err(|_| Error::Rejected("an l1 validator's fixed fields changed"))?;
    }
    for (k, _) in store.scan(&[kind::EXPIRY]) {
        state.put_expiry(Expiry::unmarshal(&k[1..]).ok_or(Error::Unreadable("expiry"))?);
    }
    for (k, v) in store.scan(&[kind::CONVERSION]) {
        state.set_conversion(id_from(&k, 1)?, read_conversion(&v)?);
    }
    for (k, v) in store.scan(&[kind::TRANSFORMATION]) {
        state.add_transformation(
            id_from(&k, 1)?,
            Tx::parse(&v).map_err(|_| Error::Unreadable("transformation"))?,
        );
    }
    for (k, v) in store.scan(&[kind::DELEGATEE_REWARD]) {
        if k.len() != 1 + 32 + 20 {
            return Err(Error::Unreadable("delegatee reward key"));
        }
        state.set_delegatee_reward(
            k[1..33].try_into().unwrap(),
            NodeId(k[33..53].try_into().unwrap()),
            read_u64(&v, "delegatee reward")?,
        );
    }
    Ok(state)
}

/// The validator-set history those records describe.
pub fn restore_history(store: &dyn Store) -> Result<History, Error> {
    let mut history = History::new();
    let mut by_height: BTreeMap<u64, BTreeMap<Where, Change>> = BTreeMap::new();
    for (k, v) in store.scan(&[kind::SET_CHANGE]) {
        if k.len() != 1 + 8 + 32 + 20 {
            return Err(Error::Unreadable("set change key"));
        }
        let height = u64::from_be_bytes(k[1..9].try_into().unwrap());
        let at = Where {
            chain: k[9..41].try_into().unwrap(),
            node: NodeId(k[41..61].try_into().unwrap()),
        };
        by_height.entry(height).or_default().insert(at, read_change(&v)?);
    }
    for (height, at) in by_height {
        history
            .record(height, &at)
            .map_err(|_| Error::Rejected("a recorded change does not compose"))?;
    }
    Ok(history)
}

fn id_from(bytes: &[u8], at: usize) -> Result<Id, Error> {
    bytes
        .get(at..at + 32)
        .and_then(|s| s.try_into().ok())
        .ok_or(Error::Unreadable("name"))
}

/// Write what changed between two states, and commit it as one thing.
///
/// The whole effect of a height is one commit, so a chain never comes back
/// having applied part of a block.
pub fn flush(
    store: &mut dyn Store,
    before: &State,
    after: &State,
    history_before: &History,
    history_after: &History,
    blocks: &[(Id, u64, block::Block, Id)],
    last_accepted: (Id, u64),
) -> Result<(), Error> {
    write_difference(store, &records(before), &records(after));
    write_difference(
        store,
        &history_records(history_before),
        &history_records(history_after),
    );
    for (id, height, blk, state_root) in blocks {
        store.put(&key(kind::BLOCK, &[id]), blk.bytes());
        store.put(&key(kind::ACCEPTED_AT, &[&height.to_be_bytes()]), id);
        // The root the block left behind, kept because it cannot be recomputed
        // once the state has moved past it — and a block that answers a zero
        // root after a restart is a block claiming to have committed to
        // nothing.
        store.put(&key(kind::BLOCK_ROOT, &[id]), state_root);
    }
    let mut tip = last_accepted.0.to_vec();
    tip.extend_from_slice(&last_accepted.1.to_le_bytes());
    store.put(&key(kind::LAST_ACCEPTED, &[]), &tip);
    store.commit()?;
    Ok(())
}

fn write_difference(
    store: &mut dyn Store,
    before: &BTreeMap<Vec<u8>, Vec<u8>>,
    after: &BTreeMap<Vec<u8>, Vec<u8>>,
) {
    for (k, v) in after {
        if before.get(k) != Some(v) {
            store.put(k, v);
        }
    }
    for k in before.keys() {
        if !after.contains_key(k) {
            store.delete(k);
        }
    }
}

/// The accepted blocks, by height, and which one is the tip.
type Restored = (
    BTreeMap<Id, block::Block>,
    BTreeMap<u64, Id>,
    BTreeMap<Id, Id>,
    Option<(Id, u64)>,
);

pub fn restore_blocks(store: &dyn Store) -> Result<Restored, Error> {
    let mut blocks = BTreeMap::new();
    for (k, v) in store.scan(&[kind::BLOCK]) {
        let id = id_from(&k, 1)?;
        let blk = block::Block::parse(&v).map_err(|_| Error::Unreadable("block"))?;
        if blk.id() != id {
            // A block filed under a name that is not the hash of its bytes is
            // a block this chain would answer for and could not have built.
            return Err(Error::Rejected("a block is stored under the wrong name"));
        }
        blocks.insert(id, blk);
    }
    let mut by_height = BTreeMap::new();
    for (k, v) in store.scan(&[kind::ACCEPTED_AT]) {
        if k.len() != 1 + 8 {
            return Err(Error::Unreadable("accepted height key"));
        }
        by_height.insert(u64::from_be_bytes(k[1..9].try_into().unwrap()), id_from(&v, 0)?);
    }
    let mut roots = BTreeMap::new();
    for (k, v) in store.scan(&[kind::BLOCK_ROOT]) {
        roots.insert(id_from(&k, 1)?, id_from(&v, 0)?);
    }
    let tip = match store.get(&key(kind::LAST_ACCEPTED, &[])) {
        None => None,
        Some(raw) => {
            if raw.len() != 40 {
                return Err(Error::Unreadable("tip"));
            }
            Some((
                raw[..32].try_into().unwrap(),
                u64::from_le_bytes(raw[32..].try_into().unwrap()),
            ))
        }
    };
    Ok((blocks, by_height, roots, tip))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Output, UtxoId};
    use crate::ids::PRIMARY_NETWORK_ID;
    use crate::store::Memory;

    fn owners(n: u8) -> Owners {
        Owners {
            locktime: 7,
            threshold: 2,
            addrs: vec![ShortId([n; 20]), ShortId([n + 1; 20])],
        }
    }

    fn utxo(n: u8) -> Utxo {
        Utxo {
            id: UtxoId {
                tx_id: [n; 32],
                output_index: n as u32,
            },
            output: Output {
                asset: [9; 32],
                stake_lock: 0,
                amount: 1234 + n as u64,
                owners: owners(n),
            },
        }
    }

    fn staker(tx: u8, node: u8, chain: Id, priority: Priority, key: Option<[u8; 48]>) -> Staker {
        Staker {
            tx_id: [tx; 32],
            node_id: NodeId([node; 20]),
            public_key: key,
            chain,
            weight: 100 + tx as u64,
            start_time: 10,
            end_time: 200,
            potential_reward: 5,
            next_time: 200,
            priority,
        }
    }

    /// A state with something of every kind in it, so the round trip is over
    /// the whole record layout rather than over the easy half of it.
    fn a_full_state() -> State {
        let mut s = State::new();
        s.set_timestamp(1_700_000_000);
        s.set_accrued_fees(4242);
        s.set_l1_excess(99);
        s.set_current_supply(PRIMARY_NETWORK_ID, 720_000_000);

        let chain = [3u8; 32];
        s.add_chain(chain, owners(1));
        s.set_current_supply(chain, 1_000);
        s.add_blockchain([21; 32], "First");
        s.add_blockchain([22; 32], "Second");

        s.add_utxo(utxo(1));
        s.add_utxo(utxo(2));
        s.add_reward_utxo([30; 32], utxo(3));
        s.add_reward_utxo([30; 32], utxo(4));

        let mut pk = [0u8; 48];
        pk[0] = 0xab;
        s.put_current_validator(staker(
            1,
            1,
            PRIMARY_NETWORK_ID,
            Priority::PrimaryNetworkValidatorCurrent,
            Some(pk),
        ))
        .unwrap();
        s.put_current_delegator(staker(
            2,
            1,
            PRIMARY_NETWORK_ID,
            Priority::PrimaryNetworkDelegatorCurrent,
            None,
        ));
        s.put_pending_validator(staker(
            3,
            2,
            chain,
            Priority::ChainPermissionlessValidatorPending,
            None,
        ))
        .unwrap();
        s.set_delegatee_reward(PRIMARY_NETWORK_ID, NodeId([1; 20]), 77);

        s.put_l1_validator(L1Validator {
            validation_id: [40; 32],
            chain_id: chain,
            node_id: NodeId([9; 20]),
            public_key: vec![7u8; 96],
            remaining_balance_owner: vec![1, 2, 3],
            deactivation_owner: vec![4, 5],
            start_time: 11,
            weight: 500,
            min_nonce: 3,
            end_accumulated_fee: 9_000,
        })
        .unwrap();
        s.put_expiry(Expiry {
            timestamp: 5_000,
            validation_id: [41; 32],
        });
        s.set_conversion(
            chain,
            Conversion {
                conversion_id: [50; 32],
                chain_id: [51; 32],
                address: vec![6, 6, 6],
            },
        );
        s
    }

    #[test]
    fn a_state_comes_back_from_its_records_identical() {
        // Compared as records rather than field by field: a field somebody
        // forgot to persist is a field this comparison sees missing, whereas a
        // hand-written list of assertions is exactly as complete as its author
        // remembered to be.
        let state = a_full_state();
        let mut store = Memory::new();
        flush(
            &mut store,
            &State::new(),
            &state,
            &History::new(),
            &History::new(),
            &[],
            ([0; 32], 0),
        )
        .unwrap();

        let back = restore(&store).unwrap();
        assert_eq!(records(&back), records(&state));
    }

    #[test]
    fn every_field_of_the_state_is_in_the_records() {
        // The one thing the round trip above cannot catch: a field that is in
        // neither the records nor the restored state agrees with itself while
        // being lost. So each is also read back through the state's own
        // accessors.
        let state = a_full_state();
        let mut store = Memory::new();
        flush(
            &mut store,
            &State::new(),
            &state,
            &History::new(),
            &History::new(),
            &[],
            ([0; 32], 0),
        )
        .unwrap();
        let back = restore(&store).unwrap();

        assert_eq!(back.timestamp(), 1_700_000_000);
        assert_eq!(back.accrued_fees(), 4242);
        assert_eq!(back.l1_excess(), 99);
        assert_eq!(back.current_supply(&PRIMARY_NETWORK_ID), Ok(720_000_000));
        let chain = [3u8; 32];
        assert_eq!(back.current_supply(&chain), Ok(1_000));
        assert_eq!(back.chain_owner(&chain), Ok(&owners(1)));
        assert_eq!(back.blockchains(), &[[21u8; 32], [22u8; 32]]);
        assert!(back.is_chain_name_taken("first"));
        assert!(back.is_chain_name_taken("SECOND"));
        assert_eq!(back.utxo(&utxo(1).id.input_id()), Ok(&utxo(1)));
        assert_eq!(back.reward_utxos(&[30; 32]).len(), 2);
        assert_eq!(back.current_stakers().count(), 2);
        assert_eq!(back.pending_stakers().count(), 1);
        let held = back.current_validator(&PRIMARY_NETWORK_ID, &NodeId([1; 20])).unwrap();
        assert_eq!(held.public_key.map(|k| k[0]), Some(0xab));
        assert_eq!(held.weight, 101);
        assert_eq!(back.delegatee_reward(&PRIMARY_NETWORK_ID, &NodeId([1; 20])), 77);
        let v = back.l1_validator(&[40; 32]).unwrap();
        assert_eq!(v.public_key, vec![7u8; 96]);
        assert_eq!(v.remaining_balance_owner, vec![1, 2, 3]);
        assert_eq!(v.deactivation_owner, vec![4, 5]);
        assert_eq!(v.min_nonce, 3);
        assert_eq!(v.end_accumulated_fee, 9_000);
        assert!(back.has_expiry(&Expiry {
            timestamp: 5_000,
            validation_id: [41; 32],
        }));
        assert_eq!(back.conversion(&chain).unwrap().address, vec![6, 6, 6]);
    }

    #[test]
    fn a_height_writes_only_what_it_changed() {
        // The reason the difference is taken at all. A chain whose every block
        // rewrote the whole state would slow down as it grew.
        let before = a_full_state();
        let mut store = Memory::new();
        flush(
            &mut store,
            &State::new(),
            &before,
            &History::new(),
            &History::new(),
            &[],
            ([0; 32], 0),
        )
        .unwrap();

        let mut after = before.clone();
        after.set_timestamp(1_700_000_100);

        // Count the writes by watching what the difference contains, rather
        // than by instrumenting the store.
        let diff_keys: Vec<Vec<u8>> = {
            let a = records(&before);
            let b = records(&after);
            b.iter()
                .filter(|(k, v)| a.get(*k) != Some(*v))
                .map(|(k, _)| k.clone())
                .collect()
        };
        assert_eq!(diff_keys, vec![vec![kind::TIMESTAMP]]);
    }

    #[test]
    fn what_a_height_removed_is_removed() {
        let before = a_full_state();
        let mut store = Memory::new();
        flush(
            &mut store,
            &State::new(),
            &before,
            &History::new(),
            &History::new(),
            &[],
            ([0; 32], 0),
        )
        .unwrap();

        let mut after = before.clone();
        after.delete_utxo(&utxo(1).id.input_id());
        after.delete_current_validator(&staker(
            1,
            1,
            PRIMARY_NETWORK_ID,
            Priority::PrimaryNetworkValidatorCurrent,
            None,
        ));
        flush(
            &mut store,
            &before,
            &after,
            &History::new(),
            &History::new(),
            &[],
            ([0; 32], 0),
        )
        .unwrap();

        let back = restore(&store).unwrap();
        assert_eq!(records(&back), records(&after));
        assert!(back.utxo(&utxo(1).id.input_id()).is_err());
        assert!(back
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([1; 20]))
            .is_err());
    }

    #[test]
    fn a_history_comes_back_from_its_records() {
        let mut history = History::new();
        let at = Where {
            chain: PRIMARY_NETWORK_ID,
            node: NodeId([1; 20]),
        };
        let mut c = Change {
            validation: [8; 32],
            renamed: true,
            key_before: vec![1u8; 96],
            key_after: vec![2u8; 96],
            ..Change::default()
        };
        c.weight.sub(500).unwrap();
        history.record(12, &BTreeMap::from([(at, c)])).unwrap();

        let mut store = Memory::new();
        flush(
            &mut store,
            &State::new(),
            &State::new(),
            &History::new(),
            &history,
            &[],
            ([0; 32], 0),
        )
        .unwrap();
        assert_eq!(restore_history(&store).unwrap(), history);
    }

    #[test]
    fn a_priority_that_names_nothing_is_refused() {
        // Rather than rounded to a neighbouring one, which would silently move
        // a delegator into a validator's seat.
        let s = staker(
            1,
            1,
            PRIMARY_NETWORK_ID,
            Priority::PrimaryNetworkValidatorCurrent,
            None,
        );
        let k = staker_key(false, &s);
        let mut v = staker_value(&s);
        // Field 40 of the root object holds the priority.
        let root = u32::from_le_bytes(v[8..12].try_into().unwrap()) as usize;
        v[root + 40] = 0;
        assert_eq!(read_staker(&k, &v), Err(Error::Unreadable("staker priority")));
    }

    #[test]
    fn a_block_filed_under_the_wrong_name_is_refused() {
        let mut store = Memory::new();
        let blk = block::Block::commit([1; 32], 4, 900);
        store.put(&key(kind::BLOCK, &[&[7u8; 32]]), blk.bytes());
        store.commit().unwrap();
        assert_eq!(
            restore_blocks(&store),
            Err(Error::Rejected("a block is stored under the wrong name"))
        );
    }
}
