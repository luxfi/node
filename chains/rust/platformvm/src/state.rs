// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What the chain believes.
//!
//! Four things, and they are separate on purpose: the clock, the unspent
//! outputs, the staker sets, and the supply. A validator is not a row in a
//! table of accounts — it is an entry in an ordered set whose order is the
//! order it will leave in, so "who changes next" is the first element rather
//! than a scan.
//!
//! There are two staker sets. A *pending* staker has been admitted and its
//! start time has not arrived; a *current* staker is in the set consensus
//! samples. Both are ordered by the time of the staker's next move, then by
//! priority, then by transaction id — and every part of that ordering is
//! consensus, because it decides which staker a reward transaction is about.
//!
//! ## Where this departs from Go, and why
//!
//! Go layers a `Diff` over its parent state and reads through the layer. Here
//! a block's changes are made on a copy of the state, and the copy replaces
//! the parent when the block is accepted. The state transitions are identical
//! — the same puts, deletes, and supply changes in the same order — and what
//! differs is only how much memory a pending block holds. The layered version
//! is what to build when a P-Chain here holds a set large enough to care;
//! saying that plainly is better than a layer that is subtly not the same
//! read.

use std::collections::{BTreeSet, HashMap};

use crate::components::{Owners, Utxo, UtxoId};
use crate::ids::{Id, NodeId};
use crate::txs::{Priority, Tx};

/// A validator or delegator, as the set holds it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Staker {
    pub tx_id: Id,
    pub node_id: NodeId,
    /// The BLS key consensus aggregates under, when the staker registered one.
    pub public_key: Option<[u8; 48]>,
    /// The network this staker validates.
    pub chain: Id,
    pub weight: u64,
    pub start_time: u64,
    pub end_time: u64,
    /// What will be minted if the staker is rewarded.
    pub potential_reward: u64,
    /// When this staker next moves: its start time while pending, its end time
    /// while current.
    pub next_time: u64,
    pub priority: Priority,
}

/// Stakers order by when they move, then by priority, then by name.
///
/// Go states this as `(*Staker).Less`. The priority tie-break is what keeps
/// stakers created by the same kind of transaction together, and the id
/// tie-break is what makes the order total — without it two stakers changing
/// at the same second in the same class would have no agreed successor, and
/// the reward transaction would be about whichever one a node happened to
/// visit first.
impl Ord for Staker {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.next_time
            .cmp(&other.next_time)
            .then_with(|| self.priority.cmp(&other.priority))
            .then_with(|| self.tx_id.cmp(&other.tx_id))
    }
}

impl PartialOrd for Staker {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

/// Every change the current staker set will undergo, in the order time will
/// apply them.
///
/// There are two kinds of change: a current staker leaving at its end time,
/// and a pending staker arriving at its start time. The order is the one Go
/// states in `StakerDiffIterator`:
///
/// - by the time the change happens;
/// - at the same time, an arrival before a departure;
/// - and further ties by the staker order — when, then priority, then name.
///
/// The middle rule is the one that matters and the easy one to get backwards.
/// A reader of this sequence is usually asking "how much weight was on this
/// validator at its heaviest", and it answers by looking at the weight
/// *before* each change. Putting arrivals first is what makes that reading see
/// the peak; putting departures first would report a maximum that never
/// happened, and admit a delegation the rest of the network refuses.
///
/// An arrival also schedules its own departure: when a pending staker joins,
/// the copy that will leave at its end time is put into the departures, so a
/// staker that arrives and leaves inside the window is seen doing both.
pub struct StakerDiff {
    /// Departures, least first.
    leaving: std::collections::BinaryHeap<std::cmp::Reverse<Staker>>,
    /// Arrivals, least first, in the order the pending set is kept.
    arriving: std::collections::VecDeque<Staker>,
}

impl StakerDiff {
    /// The changes to a set that currently holds `current`, given `pending`.
    ///
    /// Both are taken in the order they are given, and the caller gives them
    /// in set order — which is staker order, so the arrivals are already
    /// least-first. Sorting them again here would be a second opinion about
    /// an order the set already holds, and the two could differ.
    pub fn new(current: Vec<Staker>, pending: Vec<Staker>) -> StakerDiff {
        StakerDiff {
            leaving: current.into_iter().map(std::cmp::Reverse).collect(),
            arriving: pending.into(),
        }
    }

    /// The next change, and whether it is an arrival.
    #[allow(clippy::should_implement_trait)]
    pub fn next(&mut self) -> Option<(Staker, bool)> {
        let take_arrival = match (self.leaving.peek(), self.arriving.front()) {
            (None, None) => return None,
            (None, Some(_)) => true,
            (Some(_), None) => false,
            // At the same instant, the arrival goes first.
            (Some(std::cmp::Reverse(out)), Some(into)) => out.end_time >= into.start_time,
        };
        if take_arrival {
            let staker = self.arriving.pop_front().expect("an arrival");
            // What arrives will leave; schedule that now so the two are seen
            // in the right order relative to everything else.
            let mut departure = staker.clone();
            departure.next_time = departure.end_time;
            if let Some(p) = departure.priority.to_current() {
                departure.priority = p;
            }
            self.leaving.push(std::cmp::Reverse(departure));
            Some((staker, true))
        } else {
            let std::cmp::Reverse(staker) = self.leaving.pop().expect("a departure");
            Some((staker, false))
        }
    }
}

/// Why the state could not answer.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// Go's `database.ErrNotFound`. The executor branches on it, so it is one
    /// value and not a family of near-misses.
    NotFound,
    /// A staker was put where one already stands.
    AlreadyExists,
    /// Arithmetic that would leave the supply wrong.
    Overflow,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::NotFound => write!(f, "not found"),
            Error::AlreadyExists => write!(f, "already exists"),
            Error::Overflow => write!(f, "overflow"),
        }
    }
}

impl std::error::Error for Error {}

/// The whole of what the chain believes.
#[derive(Clone, Debug, Default)]
pub struct State {
    timestamp: u64,
    /// Ordered by when each staker next moves.
    current: BTreeSet<Staker>,
    pending: BTreeSet<Staker>,
    /// The one validator per network per node, for the lookups the executor
    /// makes constantly. Delegators are not here: many may share a node.
    current_validators: HashMap<(Id, NodeId), Staker>,
    pending_validators: HashMap<(Id, NodeId), Staker>,
    utxos: HashMap<Id, Utxo>,
    txs: HashMap<Id, Tx>,
    /// Minted so far, per network.
    supply: HashMap<Id, u64>,
    /// Who may admit validators to a network, and rename or hand it on.
    chain_owners: HashMap<Id, Owners>,
    /// Networks that exist.
    chains: BTreeSet<Id>,
    /// Blockchains created, by the transaction that created them.
    blockchains: Vec<Id>,
    /// Names already taken, lowercased.
    chain_names: BTreeSet<String>,
    /// Fees a validator has earned from its delegators but not yet been paid.
    delegatee_rewards: HashMap<(Id, NodeId), u64>,
    /// The outputs a reward transaction made, so they can be shown later.
    reward_utxos: HashMap<Id, Vec<Utxo>>,
}

impl State {
    pub fn new() -> State {
        State::default()
    }

    // ---- the clock ----

    pub fn timestamp(&self) -> u64 {
        self.timestamp
    }

    pub fn set_timestamp(&mut self, t: u64) {
        self.timestamp = t;
    }

    // ---- the supply ----

    pub fn current_supply(&self, chain: &Id) -> Result<u64, Error> {
        self.supply.get(chain).copied().ok_or(Error::NotFound)
    }

    pub fn set_current_supply(&mut self, chain: Id, supply: u64) {
        self.supply.insert(chain, supply);
    }

    // ---- unspent outputs ----

    pub fn utxo(&self, id: &Id) -> Result<&Utxo, Error> {
        self.utxos.get(id).ok_or(Error::NotFound)
    }

    pub fn add_utxo(&mut self, utxo: Utxo) {
        self.utxos.insert(utxo.id.input_id(), utxo);
    }

    pub fn delete_utxo(&mut self, id: &Id) {
        self.utxos.remove(id);
    }

    /// Spend what a transaction names and make what it declares.
    ///
    /// One function, because consuming without producing and producing without
    /// consuming are each a way to lose or invent value, and nothing should be
    /// able to do one without the other.
    pub fn consume_and_produce(
        &mut self,
        tx_id: Id,
        ins: &[crate::components::Input],
        outs: &[crate::components::Output],
    ) {
        for i in ins {
            self.delete_utxo(&i.utxo.input_id());
        }
        for (index, out) in outs.iter().enumerate() {
            self.add_utxo(Utxo {
                id: UtxoId {
                    tx_id,
                    output_index: index as u32,
                },
                output: out.clone(),
            });
        }
    }

    pub fn add_reward_utxo(&mut self, tx_id: Id, utxo: Utxo) {
        self.reward_utxos.entry(tx_id).or_default().push(utxo);
    }

    pub fn reward_utxos(&self, tx_id: &Id) -> &[Utxo] {
        self.reward_utxos
            .get(tx_id)
            .map(|v| v.as_slice())
            .unwrap_or(&[])
    }

    // ---- transactions ----

    pub fn tx(&self, id: &Id) -> Result<&Tx, Error> {
        self.txs.get(id).ok_or(Error::NotFound)
    }

    pub fn add_tx(&mut self, tx: Tx) {
        self.txs.insert(tx.id(), tx);
    }

    // ---- networks and chains ----

    pub fn add_chain(&mut self, chain: Id, owner: Owners) {
        self.chains.insert(chain);
        self.chain_owners.insert(chain, owner);
    }

    pub fn chain_owner(&self, chain: &Id) -> Result<&Owners, Error> {
        self.chain_owners.get(chain).ok_or(Error::NotFound)
    }

    pub fn set_chain_owner(&mut self, chain: Id, owner: Owners) {
        self.chain_owners.insert(chain, owner);
    }

    pub fn chains(&self) -> impl Iterator<Item = &Id> {
        self.chains.iter()
    }

    pub fn add_blockchain(&mut self, tx_id: Id, name: &str) {
        self.blockchains.push(tx_id);
        if !name.is_empty() {
            self.chain_names.insert(name.to_lowercase());
        }
    }

    pub fn blockchains(&self) -> &[Id] {
        &self.blockchains
    }

    /// Names are compared without case, so two chains cannot differ only in
    /// capitalisation.
    pub fn is_chain_name_taken(&self, name: &str) -> bool {
        self.chain_names.contains(&name.to_lowercase())
    }

    // ---- the staker sets ----

    /// The staker that moves next, if any.
    pub fn next_current_staker(&self) -> Option<&Staker> {
        self.current.first()
    }

    pub fn next_pending_staker(&self) -> Option<&Staker> {
        self.pending.first()
    }

    /// Every current staker, in the order they will leave.
    pub fn current_stakers(&self) -> impl Iterator<Item = &Staker> {
        self.current.iter()
    }

    pub fn pending_stakers(&self) -> impl Iterator<Item = &Staker> {
        self.pending.iter()
    }

    /// The next second at which the set changes, capped at `cap`.
    pub fn next_staker_change_time(&self, cap: u64) -> u64 {
        let mut next = cap;
        if let Some(s) = self.current.first() {
            next = next.min(s.next_time);
        }
        if let Some(s) = self.pending.first() {
            next = next.min(s.next_time);
        }
        next
    }

    pub fn current_validator(&self, chain: &Id, node: &NodeId) -> Result<&Staker, Error> {
        self.current_validators
            .get(&(*chain, *node))
            .ok_or(Error::NotFound)
    }

    pub fn pending_validator(&self, chain: &Id, node: &NodeId) -> Result<&Staker, Error> {
        self.pending_validators
            .get(&(*chain, *node))
            .ok_or(Error::NotFound)
    }

    /// The validator for a node on a network, current or pending.
    ///
    /// Go's `GetValidator` reads current first and falls back to pending; the
    /// order matters because a node may be both while its replacement waits.
    pub fn validator(&self, chain: &Id, node: &NodeId) -> Result<&Staker, Error> {
        match self.current_validator(chain, node) {
            Ok(v) => Ok(v),
            Err(Error::NotFound) => self.pending_validator(chain, node),
            Err(e) => Err(e),
        }
    }

    pub fn put_current_validator(&mut self, staker: Staker) -> Result<(), Error> {
        let key = (staker.chain, staker.node_id);
        if self.current_validators.contains_key(&key) {
            return Err(Error::AlreadyExists);
        }
        self.current_validators.insert(key, staker.clone());
        self.current.insert(staker);
        Ok(())
    }

    pub fn delete_current_validator(&mut self, staker: &Staker) {
        self.current_validators
            .remove(&(staker.chain, staker.node_id));
        self.current.remove(staker);
    }

    pub fn put_pending_validator(&mut self, staker: Staker) -> Result<(), Error> {
        let key = (staker.chain, staker.node_id);
        if self.pending_validators.contains_key(&key) {
            return Err(Error::AlreadyExists);
        }
        self.pending_validators.insert(key, staker.clone());
        self.pending.insert(staker);
        Ok(())
    }

    pub fn delete_pending_validator(&mut self, staker: &Staker) {
        self.pending_validators
            .remove(&(staker.chain, staker.node_id));
        self.pending.remove(staker);
    }

    /// Delegators are not keyed by node: many may back one validator.
    pub fn put_current_delegator(&mut self, staker: Staker) {
        self.current.insert(staker);
    }

    pub fn delete_current_delegator(&mut self, staker: &Staker) {
        self.current.remove(staker);
    }

    pub fn put_pending_delegator(&mut self, staker: Staker) {
        self.pending.insert(staker);
    }

    pub fn delete_pending_delegator(&mut self, staker: &Staker) {
        self.pending.remove(staker);
    }

    /// Every current delegator backing one validator on one network.
    pub fn current_delegators<'a>(
        &'a self,
        chain: &'a Id,
        node: &'a NodeId,
    ) -> impl Iterator<Item = &'a Staker> {
        self.current.iter().filter(move |s| {
            s.chain == *chain && s.node_id == *node && s.priority.is_current_delegator()
        })
    }

    pub fn pending_delegators<'a>(
        &'a self,
        chain: &'a Id,
        node: &'a NodeId,
    ) -> impl Iterator<Item = &'a Staker> {
        self.pending.iter().filter(move |s| {
            s.chain == *chain && s.node_id == *node && s.priority.is_pending_delegator()
        })
    }

    // ---- what a validator has earned from its delegators ----

    pub fn delegatee_reward(&self, chain: &Id, node: &NodeId) -> u64 {
        self.delegatee_rewards
            .get(&(*chain, *node))
            .copied()
            .unwrap_or(0)
    }

    pub fn set_delegatee_reward(&mut self, chain: Id, node: NodeId, amount: u64) {
        self.delegatee_rewards.insert((chain, node), amount);
    }

    /// The validator set consensus samples: node, weight, key.
    ///
    /// A delegator's weight counts toward the validator it backs, because a
    /// delegation is stake behind that validator's votes and nothing else.
    pub fn validator_set(&self, chain: &Id) -> Vec<(NodeId, u64, Option<[u8; 48]>)> {
        let mut by_node: HashMap<NodeId, (u64, Option<[u8; 48]>)> = HashMap::new();
        for s in self.current.iter().filter(|s| s.chain == *chain) {
            let e = by_node.entry(s.node_id).or_insert((0, None));
            e.0 = e.0.saturating_add(s.weight);
            if s.priority.is_current_validator() {
                e.1 = s.public_key;
            }
        }
        let mut out: Vec<_> = by_node
            .into_iter()
            .map(|(node, (weight, key))| (node, weight, key))
            .collect();
        out.sort_by_key(|(node, _, _)| *node);
        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Output, Owners};

    fn staker(tx: u8, next_time: u64, priority: Priority) -> Staker {
        Staker {
            tx_id: [tx; 32],
            node_id: NodeId([tx; 20]),
            public_key: None,
            chain: crate::ids::PRIMARY_NETWORK_ID,
            weight: 10,
            start_time: 0,
            end_time: next_time,
            potential_reward: 0,
            next_time,
            priority,
        }
    }

    fn output(amount: u64) -> Output {
        Output {
            asset: [9; 32],
            stake_lock: 0,
            amount,
            owners: Owners::default(),
        }
    }

    #[test]
    fn stakers_order_by_time_then_priority_then_name() {
        // Go's (*Staker).Less, in all three of its clauses.
        let early = staker(1, 100, Priority::PrimaryNetworkValidatorCurrent);
        let late = staker(1, 200, Priority::PrimaryNetworkValidatorCurrent);
        assert!(early < late);

        // Same second: the lower priority is first, which is what makes a
        // permissioned validator leave before anything else.
        let permissioned = staker(1, 100, Priority::ChainPermissionedValidatorCurrent);
        let permissionless = staker(1, 100, Priority::ChainPermissionlessValidatorCurrent);
        assert!(permissioned < permissionless);

        // Same second, same priority: the lesser id.
        let a = staker(1, 100, Priority::PrimaryNetworkValidatorCurrent);
        let b = staker(2, 100, Priority::PrimaryNetworkValidatorCurrent);
        assert!(a < b);
    }

    #[test]
    fn the_next_staker_to_move_is_the_first_of_the_set() {
        let mut s = State::new();
        s.put_current_validator(staker(3, 300, Priority::PrimaryNetworkValidatorCurrent))
            .unwrap();
        s.put_current_validator(staker(1, 100, Priority::PrimaryNetworkValidatorCurrent))
            .unwrap();
        s.put_current_validator(staker(2, 200, Priority::PrimaryNetworkValidatorCurrent))
            .unwrap();
        assert_eq!(s.next_current_staker().unwrap().next_time, 100);
    }

    #[test]
    fn one_node_may_validate_one_network_once() {
        // The rule that stops a node from doubling its own weight.
        let mut s = State::new();
        let v = staker(1, 100, Priority::PrimaryNetworkValidatorCurrent);
        assert_eq!(s.put_current_validator(v.clone()), Ok(()));
        assert_eq!(s.put_current_validator(v), Err(Error::AlreadyExists));
    }

    #[test]
    fn the_same_node_may_validate_two_networks() {
        let mut s = State::new();
        let mut a = staker(1, 100, Priority::ChainPermissionlessValidatorCurrent);
        a.chain = [7; 32];
        let mut b = staker(2, 100, Priority::ChainPermissionlessValidatorCurrent);
        b.chain = [8; 32];
        b.node_id = a.node_id;
        assert_eq!(s.put_current_validator(a), Ok(()));
        assert_eq!(s.put_current_validator(b), Ok(()));
    }

    #[test]
    fn many_delegators_may_back_one_validator() {
        // Delegators are not keyed by node — that is the difference between
        // the two puts.
        let mut s = State::new();
        let node = NodeId([5; 20]);
        for i in 0..3u8 {
            let mut d = staker(
                i + 1,
                100 + i as u64,
                Priority::PrimaryNetworkDelegatorCurrent,
            );
            d.node_id = node;
            s.put_current_delegator(d);
        }
        assert_eq!(
            s.current_delegators(&crate::ids::PRIMARY_NETWORK_ID, &node)
                .count(),
            3
        );
    }

    #[test]
    fn a_validator_is_found_current_before_pending() {
        let mut s = State::new();
        let node = NodeId([5; 20]);
        let mut pending = staker(1, 100, Priority::PrimaryNetworkValidatorPending);
        pending.node_id = node;
        s.put_pending_validator(pending).unwrap();
        assert_eq!(
            s.validator(&crate::ids::PRIMARY_NETWORK_ID, &node)
                .unwrap()
                .priority,
            Priority::PrimaryNetworkValidatorPending
        );

        let mut current = staker(2, 200, Priority::PrimaryNetworkValidatorCurrent);
        current.node_id = node;
        s.put_current_validator(current).unwrap();
        assert_eq!(
            s.validator(&crate::ids::PRIMARY_NETWORK_ID, &node)
                .unwrap()
                .priority,
            Priority::PrimaryNetworkValidatorCurrent
        );
    }

    #[test]
    fn a_missing_thing_is_not_found_rather_than_a_zero() {
        // The executor branches on this exact answer.
        let s = State::new();
        assert_eq!(s.current_supply(&[1; 32]), Err(Error::NotFound));
        assert_eq!(s.utxo(&[1; 32]).err(), Some(Error::NotFound));
        assert_eq!(s.tx(&[1; 32]).err(), Some(Error::NotFound));
        assert_eq!(
            s.validator(&crate::ids::PRIMARY_NETWORK_ID, &NodeId([1; 20]))
                .err(),
            Some(Error::NotFound)
        );
    }

    #[test]
    fn spending_removes_what_was_named_and_makes_what_was_declared() {
        let mut s = State::new();
        let spent = UtxoId {
            tx_id: [1; 32],
            output_index: 0,
        };
        s.add_utxo(Utxo {
            id: spent,
            output: output(100),
        });
        assert!(s.utxo(&spent.input_id()).is_ok());

        let tx_id = [2u8; 32];
        s.consume_and_produce(
            tx_id,
            &[crate::components::Input {
                utxo: spent,
                asset: [9; 32],
                stake_lock: 0,
                amount: 100,
                sig_indices: vec![0],
            }],
            &[output(40), output(60)],
        );

        assert_eq!(s.utxo(&spent.input_id()).err(), Some(Error::NotFound));
        for (i, amount) in [(0u32, 40u64), (1, 60)] {
            let made = UtxoId {
                tx_id,
                output_index: i,
            };
            assert_eq!(s.utxo(&made.input_id()).unwrap().output.amount, amount);
        }
    }

    #[test]
    fn the_next_change_is_the_earlier_of_the_two_sets_capped() {
        let mut s = State::new();
        assert_eq!(s.next_staker_change_time(999), 999);

        s.put_current_validator(staker(1, 500, Priority::PrimaryNetworkValidatorCurrent))
            .unwrap();
        assert_eq!(s.next_staker_change_time(999), 500);

        s.put_pending_validator(staker(2, 300, Priority::PrimaryNetworkValidatorPending))
            .unwrap();
        assert_eq!(s.next_staker_change_time(999), 300);

        // A cap earlier than either is still the answer.
        assert_eq!(s.next_staker_change_time(100), 100);
    }

    #[test]
    fn a_delegators_weight_counts_toward_the_validator_it_backs() {
        let mut s = State::new();
        let node = NodeId([5; 20]);
        let mut v = staker(1, 100, Priority::PrimaryNetworkValidatorCurrent);
        v.node_id = node;
        v.weight = 100;
        v.public_key = Some([3; 48]);
        s.put_current_validator(v).unwrap();

        let mut d = staker(2, 90, Priority::PrimaryNetworkDelegatorCurrent);
        d.node_id = node;
        d.weight = 25;
        s.put_current_delegator(d);

        let set = s.validator_set(&crate::ids::PRIMARY_NETWORK_ID);
        assert_eq!(set.len(), 1);
        assert_eq!(set[0].0, node);
        assert_eq!(set[0].1, 125);
        assert_eq!(set[0].2, Some([3; 48]));
    }

    #[test]
    fn a_chain_name_is_taken_regardless_of_case() {
        let mut s = State::new();
        s.add_blockchain([1; 32], "MyChain");
        assert!(s.is_chain_name_taken("mychain"));
        assert!(s.is_chain_name_taken("MYCHAIN"));
        assert!(!s.is_chain_name_taken("other"));
    }

    #[test]
    fn a_validator_accrues_the_fees_its_delegators_paid_it() {
        let mut s = State::new();
        let node = NodeId([5; 20]);
        let chain = crate::ids::PRIMARY_NETWORK_ID;
        assert_eq!(s.delegatee_reward(&chain, &node), 0);
        s.set_delegatee_reward(chain, node, 42);
        assert_eq!(s.delegatee_reward(&chain, &node), 42);
    }

    /// Go: `TestStakerDiffIterator`, with the same set and the same expected
    /// sequence.
    #[test]
    fn the_changes_come_in_the_order_time_applies_them() {
        let vdr = |tx: u8, start: u64, end: u64, next: u64, priority: Priority| Staker {
            tx_id: [tx; 32],
            node_id: NodeId([1; 20]),
            public_key: None,
            chain: crate::ids::PRIMARY_NETWORK_ID,
            weight: 1,
            start_time: start,
            end_time: end,
            potential_reward: 0,
            next_time: next,
            priority,
        };

        let current = vec![vdr(0, 0, 10, 10, Priority::PrimaryNetworkValidatorCurrent)];
        let pending = vec![
            vdr(1, 0, 5, 0, Priority::PrimaryNetworkDelegatorLegacyPending),
            vdr(2, 5, 10, 5, Priority::PrimaryNetworkDelegatorLegacyPending),
            vdr(3, 11, 20, 11, Priority::PrimaryNetworkValidatorPending),
            vdr(
                4,
                11,
                20,
                11,
                Priority::PrimaryNetworkDelegatorLegacyPending,
            ),
        ];

        let want: Vec<(u8, bool)> = vec![
            (1, true),
            (2, true),
            (1, false),
            (2, false),
            (0, false),
            (3, true),
            (4, true),
            (4, false),
            (3, false),
        ];

        let mut diff = StakerDiff::new(current, pending);
        for (tx, arriving) in want {
            let (staker, is_arriving) = diff.next().expect("a change");
            assert_eq!(staker.tx_id[0], tx, "the staker that changes");
            assert_eq!(is_arriving, arriving, "arriving or leaving");
        }
        assert!(diff.next().is_none());
    }

    /// Go: `TestMutableStakerIterator` — departures added while the walk is in
    /// progress take their place in time, not at the end.
    #[test]
    fn a_departure_added_mid_walk_lands_in_its_place() {
        let leaving = |tx: u8, end: u64| Staker {
            tx_id: [tx; 32],
            node_id: NodeId([1; 20]),
            public_key: None,
            chain: crate::ids::PRIMARY_NETWORK_ID,
            weight: 1,
            start_time: 0,
            end_time: end,
            potential_reward: 0,
            next_time: end,
            priority: Priority::PrimaryNetworkValidatorCurrent,
        };
        // Go seeds three, then adds three more that fall between them. Here
        // the arrivals do the adding: each one schedules its own departure.
        let arriving = |tx: u8, start: u64, end: u64| Staker {
            tx_id: [tx; 32],
            node_id: NodeId([1; 20]),
            public_key: None,
            chain: crate::ids::PRIMARY_NETWORK_ID,
            weight: 1,
            start_time: start,
            end_time: end,
            potential_reward: 0,
            next_time: start,
            priority: Priority::PrimaryNetworkValidatorPending,
        };

        let mut diff = StakerDiff::new(
            vec![leaving(10, 10), leaving(20, 20), leaving(30, 30)],
            vec![arriving(15, 1, 15), arriving(25, 2, 25)],
        );

        let mut seen = Vec::new();
        while let Some((staker, is_arriving)) = diff.next() {
            seen.push((staker.tx_id[0], staker.next_time, is_arriving));
        }
        assert_eq!(
            seen,
            vec![
                (15, 1, true),
                (25, 2, true),
                (10, 10, false),
                (15, 15, false),
                (20, 20, false),
                (25, 25, false),
                (30, 30, false),
            ]
        );
    }

    /// The rule that is easy to get backwards, on its own.
    ///
    /// A departure and an arrival at the same instant: the arrival is seen
    /// first, so a reader taking the weight before each change sees the moment
    /// both were on the validator. Reversing this reports a maximum that never
    /// happened.
    #[test]
    fn an_arrival_and_a_departure_at_one_instant_arrive_first() {
        let at = |tx: u8, next: u64, start: u64, end: u64, priority: Priority| Staker {
            tx_id: [tx; 32],
            node_id: NodeId([1; 20]),
            public_key: None,
            chain: crate::ids::PRIMARY_NETWORK_ID,
            weight: 1,
            start_time: start,
            end_time: end,
            potential_reward: 0,
            next_time: next,
            priority,
        };
        let mut diff = StakerDiff::new(
            vec![at(1, 10, 0, 10, Priority::PrimaryNetworkDelegatorCurrent)],
            vec![at(
                2,
                10,
                10,
                20,
                Priority::PrimaryNetworkDelegatorLegacyPending,
            )],
        );
        assert_eq!(diff.next().map(|(s, a)| (s.tx_id[0], a)), Some((2, true)));
        assert_eq!(diff.next().map(|(s, a)| (s.tx_id[0], a)), Some((1, false)));
        assert_eq!(diff.next().map(|(s, a)| (s.tx_id[0], a)), Some((2, false)));
        assert!(diff.next().is_none());
    }
}
