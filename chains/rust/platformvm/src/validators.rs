// SPDX-License-Identifier: BSD-3-Clause-Eco

//! Who validates a network, with what weight and under what key — now, and at
//! any height this chain has already passed.
//!
//! Ported from Go `vms/platformvm/validators/manager.go` (`makeValidatorSet`),
//! `state/state_validators.go` (`GetCurrentValidators`,
//! `getInheritedPublicKey`), `state/state_diffs.go` (`calculateValidatorDiffs`,
//! `writeValidatorDiffs`, `applyWeightDiff`, `ApplyValidatorPublicKeyDiffs`) and
//! `luxfi/node chains/quorum.go` (`hashValidatorSet`).
//!
//! ## Why the past matters
//!
//! A signature made at height H is checked long after H. To check it a node has
//! to say who validated then and with what key — so a chain that can only
//! answer "now" cannot verify anything a peer said a minute ago, and a node
//! that restarts and answers only from its current set silently accepts or
//! rejects the wrong signatures.
//!
//! Keeping every past set costs the whole history. Keeping what each height
//! *changed* costs the changes, and the set at H is the set now with every
//! change since H undone. That is why [`History`] faces backwards: a weight
//! change is inverted on the way down, and the key it stores is the one the
//! node signed with **before** the change, not after.
//!
//! ## Two things that are silent when wrong
//!
//! The key here is **uncompressed**. A proof of possession signs the 48-byte
//! compressed form; the set commitment hashes the 96-byte uncompressed one.
//! Using one where the other belongs produces a root that verifies against
//! nothing — and a validator whose signed message differs from its peers' has
//! its votes dropped rather than disputed.
//!
//! A validator of a network that is not the primary one holds **no key of its
//! own** — the transaction that registered it carries no signer — and signs
//! with the key of its primary-network entry. Surfacing it keyless would leave
//! its weight in the denominator with no way for anyone to vote toward it,
//! which is a quorum that cannot be reached.

use std::collections::BTreeMap;

use crate::ids::{Id, NodeId, EMPTY, PRIMARY_NETWORK_ID};
use crate::signer;
use crate::state::State;

/// Why a set cannot be answered.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// A staker of another network whose node holds no primary-network entry.
    /// Its key can only come from there, so there is no set to give.
    NotPrimaryValidator(NodeId),
    /// A registered key that does not decompress. Nothing signed under it can
    /// ever verify, so it is refused rather than carried.
    MalformedPublicKey,
    /// A node carrying delegated weight with no validator of its own. Weight
    /// with nothing to sample is not a set entry.
    DelegatedToNobody(NodeId),
    /// The set's weight does not fit.
    Overflow,
    /// A height this chain has not reached. It has no set, and answering one
    /// would be inventing it.
    NotReached { asked: u64, reached: u64 },
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::NotPrimaryValidator(n) => {
                write!(f, "{n:?} validates a network without validating the primary one")
            }
            Error::MalformedPublicKey => write!(f, "a registered key does not decompress"),
            Error::DelegatedToNobody(n) => write!(f, "{n:?} holds delegated weight and no entry"),
            Error::Overflow => write!(f, "the validator set's weight overflows"),
            Error::NotReached { asked, reached } => {
                write!(f, "height {asked} has not been reached; the chain is at {reached}")
            }
        }
    }
}

impl std::error::Error for Error {}

/// One validator, as consensus reads it.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Validator {
    pub node_id: NodeId,
    /// The key this validator signs with, **uncompressed** (96 bytes). Absent
    /// for one registered before keys existed, and for the pooled entry every
    /// inactive L1 validator's weight gathers under.
    pub public_key: Option<Vec<u8>>,
    pub weight: u64,
    /// The name the entry is known by: the transaction that added a staker, or
    /// the registration that created an L1 validator.
    pub tx_id: Id,
}

/// One node's entry in one network's set. The key the whole record is ordered
/// by, so a walk over it is deterministic.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord)]
pub struct Where {
    pub chain: Id,
    pub node: NodeId,
}

/// A signed weight, as a sign and a magnitude. Go: `state.ValidatorWeightDiff`.
///
/// Kept this way rather than as an `i64` because a validator's weight is a
/// `u64` and the difference of two of them does not fit one.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct WeightDiff {
    pub decrease: bool,
    pub amount: u64,
}

impl WeightDiff {
    pub fn add(&mut self, amount: u64) -> Result<(), Error> {
        self.add_or_sub(false, amount)
    }

    pub fn sub(&mut self, amount: u64) -> Result<(), Error> {
        self.add_or_sub(true, amount)
    }

    /// Go: `ValidatorWeightDiff.addOrSub`. Same sign accumulates; opposite
    /// signs cancel, and the sign flips when the subtrahend is the larger.
    pub fn add_or_sub(&mut self, sub: bool, amount: u64) -> Result<(), Error> {
        if self.decrease == sub {
            self.amount = self.amount.checked_add(amount).ok_or(Error::Overflow)?;
            return Ok(());
        }
        if self.amount > amount {
            self.amount -= amount;
            return Ok(());
        }
        self.amount = amount - self.amount;
        self.decrease = sub;
        Ok(())
    }
}

/// What one height did to one entry.
///
/// Everything here is stated from the earlier height's point of view, because
/// that is the direction it is read in: `validation` is the name the entry had
/// *before*, and `key_before` is the key it signed with *before*.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Change {
    pub weight: WeightDiff,
    /// The name the entry had at the earlier height. Empty when the entry did
    /// not exist then, in which case undoing the change removes it anyway.
    pub validation: Id,
    /// Whether the name changed. An L1 validator removed and re-registered in
    /// one block can keep its weight and change its name, and the earlier name
    /// still has to come back.
    pub renamed: bool,
    /// The uncompressed key before the change, and after it. Empty means none.
    pub key_before: Vec<u8>,
    pub key_after: Vec<u8>,
}

impl Change {
    /// Whether this change moves weight. Go: `writeValidatorDiffs` stores a
    /// weight only when it moved, or when an entry was removed and re-added
    /// under a new name.
    fn weight_moved(&self) -> bool {
        self.weight.amount != 0 || (self.renamed && self.validation != EMPTY)
    }

    fn key_moved(&self) -> bool {
        self.key_before != self.key_after
    }

    fn is_nothing(&self) -> bool {
        !self.weight_moved() && !self.key_moved()
    }
}

/// One entry of one set, before it is split by network.
#[derive(Clone, Debug, Default)]
struct Entry {
    weight: u64,
    key: Option<Vec<u8>>,
    tx_id: Id,
    /// Whether a validator — as opposed to only delegated weight — stands here.
    seated: bool,
}

/// Every network's set at once, keyed by where each entry sits.
///
/// One function rather than two, because "the set of one network" and "what
/// changed between two states" have to agree about what an entry *is*; two
/// readings of that would be two answers to who validates.
fn entries(state: &State) -> Result<BTreeMap<Where, Entry>, Error> {
    let mut out: BTreeMap<Where, Entry> = BTreeMap::new();

    // Weight first. A delegator's stake sits under its validator's entry rather
    // than being an entry of its own — Go's `diffValidator.WeightDiff` sums the
    // delegators into the node's weight, so the sampled weight of a node is its
    // own stake plus everything delegated to it.
    let mut compressed: BTreeMap<Where, Option<[u8; 48]>> = BTreeMap::new();
    for s in state.current_stakers() {
        let at = Where {
            chain: s.chain,
            node: s.node_id,
        };
        let e = out.entry(at).or_default();
        e.weight = e.weight.checked_add(s.weight).ok_or(Error::Overflow)?;
        if s.priority.is_current_validator() {
            e.seated = true;
            e.tx_id = s.tx_id;
            compressed.insert(at, s.public_key);
        }
    }

    for (at, e) in out.iter_mut() {
        if !e.seated {
            return Err(Error::DelegatedToNobody(at.node));
        }
        // Go: `getInheritedPublicKey`. A validator of another network signs
        // with the key of its primary-network entry and has none of its own.
        let key = match compressed.get(at).copied().flatten() {
            Some(k) => Some(k),
            None if at.chain == PRIMARY_NETWORK_ID => None,
            None => {
                let primary = Where {
                    chain: PRIMARY_NETWORK_ID,
                    node: at.node,
                };
                match compressed.get(&primary) {
                    Some(k) => *k,
                    None => return Err(Error::NotPrimaryValidator(at.node)),
                }
            }
        };
        e.key = match key {
            None => None,
            Some(k) => Some(signer::uncompress(&k).map_err(|_| Error::MalformedPublicKey)?),
        };
    }

    // Then the L1 validators. They hold their own key rather than inheriting
    // one, and they are named by the registration that created them.
    //
    // An INACTIVE one enters under the empty node with no key. It has run out
    // of money: it still weighs on the set, because its stake is part of what a
    // quorum has to beat, but it cannot be sampled and cannot sign. Those two
    // halves are the same statement — surfacing its node would let a quorum
    // wait for a vote that can never come, and surfacing its key would let its
    // weight count toward a quorum it never voted in. So every inactive
    // validator of a network gathers under one keyless entry, which is exactly
    // what a denominator is.
    for v in state.all_l1_validators() {
        let at = Where {
            chain: v.chain_id,
            node: v.effective_node_id(),
        };
        match out.get_mut(&at) {
            Some(e) if e.weight != 0 => {
                e.weight = e.weight.checked_add(v.weight).ok_or(Error::Overflow)?;
            }
            _ => {
                let key = v.effective_public_key();
                out.insert(
                    at,
                    Entry {
                        weight: v.weight,
                        key: (!key.is_empty()).then(|| key.to_vec()),
                        tx_id: v.validation_id,
                        seated: true,
                    },
                );
            }
        }
    }

    Ok(out)
}

/// The set validating `chain` right now, keyed by node.
pub fn current_set(state: &State, chain: &Id) -> Result<BTreeMap<NodeId, Validator>, Error> {
    Ok(entries(state)?
        .into_iter()
        .filter(|(at, _)| at.chain == *chain)
        .map(|(at, e)| {
            (
                at.node,
                Validator {
                    node_id: at.node,
                    public_key: e.key,
                    weight: e.weight,
                    tx_id: e.tx_id,
                },
            )
        })
        .collect())
}

/// The canonical commitment to a set.
///
/// `sha256` over, per validator ascending by raw node id, `node ‖ weight(8, big
/// endian) ‖ len(key)(8, big endian) ‖ key`. An empty set commits to the
/// all-zero id — the explicit "unbound" answer rather than `sha256("")`, which
/// is a perfectly good hash of the wrong thing.
///
/// Go: `hashValidatorSet` (`luxfi/node chains/quorum.go`).
pub fn set_root(set: &BTreeMap<NodeId, Validator>) -> Id {
    if set.is_empty() {
        return EMPTY;
    }
    let mut preimage = Vec::with_capacity(set.len() * (20 + 8 + 8 + 96));
    // A `BTreeMap` over `NodeId` is already ascending by raw bytes, which is
    // the order the commitment is defined in.
    for (node, v) in set {
        preimage.extend_from_slice(&node.0);
        preimage.extend_from_slice(&v.weight.to_be_bytes());
        let key = v.public_key.as_deref().unwrap_or(&[]);
        preimage.extend_from_slice(&(key.len() as u64).to_be_bytes());
        preimage.extend_from_slice(key);
    }
    crate::ids::hash256(&preimage)
}

/// What one height did, as the difference between the state before it and the
/// state after.
///
/// Go reaches the same record by tracking a diff layer as it is written; here
/// the two states are both held, so the difference is read off them directly.
/// The results are the same record and it cannot fall out of step with the
/// state, because it *is* the state.
pub fn changes(before: &State, after: &State) -> Result<BTreeMap<Where, Change>, Error> {
    let old = entries(before)?;
    let new = entries(after)?;

    let mut out: BTreeMap<Where, Change> = BTreeMap::new();
    for at in old.keys().chain(new.keys()) {
        if out.contains_key(at) {
            continue;
        }
        let was = old.get(at);
        let is = new.get(at);
        let mut c = Change::default();

        let old_weight = was.map(|e| e.weight).unwrap_or(0);
        let new_weight = is.map(|e| e.weight).unwrap_or(0);
        if new_weight >= old_weight {
            c.weight.add(new_weight - old_weight)?;
        } else {
            c.weight.sub(old_weight - new_weight)?;
        }

        // The name at the EARLIER height, because that is the one undoing this
        // change has to restore. An entry that did not exist then has none, and
        // undoing removes it anyway.
        c.validation = was.map(|e| e.tx_id).unwrap_or(EMPTY);
        c.renamed = was.map(|e| e.tx_id) != is.map(|e| e.tx_id);

        c.key_before = was.and_then(|e| e.key.clone()).unwrap_or_default();
        c.key_after = is.and_then(|e| e.key.clone()).unwrap_or_default();

        if !c.is_nothing() {
            out.insert(*at, c);
        }
    }
    Ok(out)
}

/// The record of what every height changed.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct History {
    by_height: BTreeMap<u64, BTreeMap<Where, Change>>,
}

impl History {
    pub fn new() -> History {
        History::default()
    }

    pub fn is_empty(&self) -> bool {
        self.by_height.is_empty()
    }

    pub fn heights(&self) -> impl Iterator<Item = (&u64, &BTreeMap<Where, Change>)> {
        self.by_height.iter()
    }

    /// Go: `writeValidatorDiffs`. A height can be written more than once — a
    /// proposal settles its decision and the option that follows chooses the
    /// outcome — so a change at a place already recorded at that height
    /// **composes** with what is there rather than replacing it. Replacing
    /// would lose the first half of what the height did.
    pub fn record(&mut self, height: u64, c: &BTreeMap<Where, Change>) -> Result<(), Error> {
        let at = self.by_height.entry(height).or_default();
        for (place, change) in c {
            match at.get_mut(place) {
                None => {
                    at.insert(*place, change.clone());
                }
                Some(merged) => {
                    merged
                        .weight
                        .add_or_sub(change.weight.decrease, change.weight.amount)?;
                    if merged.validation == EMPTY {
                        merged.validation = change.validation;
                    }
                    merged.renamed = merged.renamed || change.renamed;
                    // The key the height started with, and the one it ended
                    // with.
                    merged.key_after.clone_from(&change.key_after);
                }
            }
        }
        Ok(())
    }

    /// `set` arrives as the set at `from` and leaves as the set at `to`.
    ///
    /// Refuses to look forward: a height this chain has not reached has no set,
    /// and answering one would be inventing it. Go: `makeValidatorSet`, which
    /// applies the diffs in `[to + 1, from]` — the record is inclusive at both
    /// ends, and undoing the change *at* `to` would take the set one height too
    /// far back.
    pub fn rewind(
        &self,
        set: &mut BTreeMap<NodeId, Validator>,
        chain: &Id,
        from: u64,
        to: u64,
    ) -> Result<(), Error> {
        if to > from {
            return Err(Error::NotReached {
                asked: to,
                reached: from,
            });
        }
        if to == from {
            return Ok(());
        }
        let window = (to + 1)..=from;

        // The weights first, newest change to oldest. A record says what a
        // height DID, so undoing it inverts it.
        for (_, at) in self.by_height.range(window.clone()).rev() {
            for (place, e) in at {
                if place.chain != *chain || !e.weight_moved() {
                    continue;
                }
                let v = set.entry(place.node).or_insert_with(|| Validator {
                    node_id: place.node,
                    ..Validator::default()
                });
                if e.validation != EMPTY {
                    v.tx_id = e.validation;
                }
                v.weight = if e.weight.decrease {
                    v.weight.checked_add(e.weight.amount).ok_or(Error::Overflow)?
                } else {
                    v.weight.checked_sub(e.weight.amount).ok_or(Error::Overflow)?
                };
                // Weight zero at the earlier height means the node was not in
                // the set then, and an entry of no weight is not an entry.
                if v.weight == 0 {
                    set.remove(&place.node);
                }
            }
        }

        // Then the keys, in the same direction, so the oldest recorded key —
        // the one the node signed with at the height being asked about — is the
        // one left standing. A key is only restored onto an entry that is
        // there: a node that was not in the set had no key in it either.
        for (_, at) in self.by_height.range(window).rev() {
            for (place, e) in at {
                if place.chain != *chain || !e.key_moved() {
                    continue;
                }
                let Some(v) = set.get_mut(&place.node) else {
                    continue;
                };
                v.public_key = (!e.key_before.is_empty()).then(|| e.key_before.clone());
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::Staker;
    use crate::txs::Priority;

    fn key(seed: u8) -> [u8; 48] {
        let ikm = [seed; 32];
        let sk = blst::min_pk::SecretKey::key_gen(&ikm, &[]).unwrap();
        sk.sk_to_pk().compress()
    }

    fn long(seed: u8) -> Vec<u8> {
        signer::uncompress(&key(seed)).unwrap()
    }

    fn validator(tx: u8, node: u8, chain: Id, weight: u64, pk: Option<[u8; 48]>) -> Staker {
        Staker {
            tx_id: [tx; 32],
            node_id: NodeId([node; 20]),
            public_key: pk,
            chain,
            weight,
            start_time: 0,
            end_time: 100,
            potential_reward: 0,
            next_time: 100,
            priority: if chain == PRIMARY_NETWORK_ID {
                Priority::PrimaryNetworkValidatorCurrent
            } else {
                Priority::ChainPermissionlessValidatorCurrent
            },
        }
    }

    fn delegator(tx: u8, node: u8, weight: u64) -> Staker {
        Staker {
            tx_id: [tx; 32],
            node_id: NodeId([node; 20]),
            public_key: None,
            chain: PRIMARY_NETWORK_ID,
            weight,
            start_time: 0,
            end_time: 100,
            potential_reward: 0,
            next_time: 100,
            priority: Priority::PrimaryNetworkDelegatorCurrent,
        }
    }

    #[test]
    fn a_nodes_weight_is_its_own_stake_plus_what_was_delegated_to_it() {
        // Go: `diffValidator.WeightDiff` sums the delegators into the node's
        // weight, so the sampled weight is the whole entry rather than only the
        // validator's own bond. A set that left the delegations out would
        // sample a node for less than it is standing behind.
        let mut s = State::new();
        s.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(1))))
            .unwrap();
        s.put_current_delegator(delegator(2, 1, 40));
        s.put_current_delegator(delegator(3, 1, 5));

        let set = current_set(&s, &PRIMARY_NETWORK_ID).unwrap();
        assert_eq!(set.len(), 1);
        assert_eq!(set[&NodeId([1; 20])].weight, 145);
        // And it is named by the validator, not by the last delegation.
        assert_eq!(set[&NodeId([1; 20])].tx_id, [1; 32]);
    }

    #[test]
    fn the_key_a_set_holds_is_the_uncompressed_one() {
        // The commitment hashes 96 bytes; a proof of possession signs 48. Using
        // one where the other belongs verifies against nothing.
        let mut s = State::new();
        s.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(7))))
            .unwrap();
        let set = current_set(&s, &PRIMARY_NETWORK_ID).unwrap();
        let held = set[&NodeId([1; 20])].public_key.clone().unwrap();
        assert_eq!(held.len(), 96);
        assert_eq!(held, long(7));
    }

    #[test]
    fn a_validator_of_another_network_signs_with_its_primary_key() {
        // Go: `getInheritedPublicKey`. The registration carries no signer, so
        // the only key it can have is the one its primary-network entry holds.
        let chain = [3u8; 32];
        let mut s = State::new();
        s.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(9))))
            .unwrap();
        s.put_current_validator(validator(2, 1, chain, 1, None)).unwrap();

        let set = current_set(&s, &chain).unwrap();
        assert_eq!(set[&NodeId([1; 20])].public_key, Some(long(9)));
        // Named by the transaction that admitted it to THIS network.
        assert_eq!(set[&NodeId([1; 20])].tx_id, [2; 32]);
    }

    #[test]
    fn a_validator_of_another_network_with_no_primary_entry_has_no_set() {
        // Refused rather than surfaced keyless: weight nobody can vote toward
        // is a quorum that cannot be reached.
        let chain = [3u8; 32];
        let mut s = State::new();
        s.put_current_validator(validator(2, 1, chain, 1, None)).unwrap();
        assert_eq!(
            current_set(&s, &chain),
            Err(Error::NotPrimaryValidator(NodeId([1; 20])))
        );
    }

    #[test]
    fn the_empty_set_commits_to_the_zero_id() {
        assert_eq!(set_root(&BTreeMap::new()), EMPTY);
    }

    #[test]
    fn the_set_root_is_the_hash_go_takes() {
        // Rebuilt here from the definition rather than from this file's own
        // implementation, so the two would have to be wrong the same way.
        let mut s = State::new();
        s.put_current_validator(validator(1, 2, PRIMARY_NETWORK_ID, 5, Some(key(1))))
            .unwrap();
        s.put_current_validator(validator(2, 1, PRIMARY_NETWORK_ID, 7, None))
            .unwrap();
        let set = current_set(&s, &PRIMARY_NETWORK_ID).unwrap();

        let mut want = Vec::new();
        // Ascending by raw node id: 0x01… before 0x02….
        want.extend_from_slice(&[1u8; 20]);
        want.extend_from_slice(&7u64.to_be_bytes());
        want.extend_from_slice(&0u64.to_be_bytes());
        want.extend_from_slice(&[2u8; 20]);
        want.extend_from_slice(&5u64.to_be_bytes());
        want.extend_from_slice(&96u64.to_be_bytes());
        want.extend_from_slice(&long(1));
        assert_eq!(set_root(&set), crate::ids::hash256(&want));
    }

    #[test]
    fn a_weight_diff_is_a_sign_and_a_magnitude() {
        // Go: `ValidatorWeightDiff.addOrSub`.
        let mut d = WeightDiff::default();
        d.add(10).unwrap();
        assert_eq!(d, WeightDiff { decrease: false, amount: 10 });
        d.sub(4).unwrap();
        assert_eq!(d, WeightDiff { decrease: false, amount: 6 });
        // Equal magnitudes flip the sign and leave nothing: Go's `addOrSub`
        // takes the second branch only when the running amount is strictly
        // larger, so a cancelling subtraction lands on the subtrahend's sign.
        d.sub(6).unwrap();
        assert_eq!(d, WeightDiff { decrease: true, amount: 0 });
        d.sub(3).unwrap();
        assert_eq!(d, WeightDiff { decrease: true, amount: 3 });
        d.add(3).unwrap();
        assert_eq!(d, WeightDiff { decrease: false, amount: 0 });

        let mut big = WeightDiff { decrease: false, amount: u64::MAX };
        assert_eq!(big.add(1), Err(Error::Overflow));
    }

    /// The chain grows a validator, then another, then loses the first — and
    /// every intermediate set can still be named afterwards.
    #[test]
    fn the_set_at_a_past_height_is_the_set_now_with_the_changes_undone() {
        let mut history = History::new();
        let s0 = State::new();

        let mut s1 = s0.clone();
        s1.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(1))))
            .unwrap();
        history.record(1, &changes(&s0, &s1).unwrap()).unwrap();

        let mut s2 = s1.clone();
        s2.put_current_validator(validator(2, 2, PRIMARY_NETWORK_ID, 50, Some(key(2))))
            .unwrap();
        history.record(2, &changes(&s1, &s2).unwrap()).unwrap();

        let mut s3 = s2.clone();
        s3.delete_current_validator(&validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(1))));
        history.record(3, &changes(&s2, &s3).unwrap()).unwrap();

        let now = current_set(&s3, &PRIMARY_NETWORK_ID).unwrap();
        assert_eq!(now.len(), 1);

        let at = |h: u64| {
            let mut set = now.clone();
            history.rewind(&mut set, &PRIMARY_NETWORK_ID, 3, h).unwrap();
            set
        };
        assert_eq!(at(3), current_set(&s3, &PRIMARY_NETWORK_ID).unwrap());
        assert_eq!(at(2), current_set(&s2, &PRIMARY_NETWORK_ID).unwrap());
        assert_eq!(at(1), current_set(&s1, &PRIMARY_NETWORK_ID).unwrap());
        assert_eq!(at(0), current_set(&s0, &PRIMARY_NETWORK_ID).unwrap());
        assert!(at(0).is_empty());
    }

    #[test]
    fn the_key_a_past_height_is_read_with_is_the_key_it_signed_with() {
        // The whole reason the record faces backwards. A node that rotated its
        // key at height 2 signed height 1 with the OLD one, and a set that
        // handed back the new key would reject that node's own signature.
        let mut history = History::new();
        let s0 = State::new();

        let mut s1 = s0.clone();
        s1.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(1))))
            .unwrap();
        history.record(1, &changes(&s0, &s1).unwrap()).unwrap();

        let mut s2 = s1.clone();
        s2.delete_current_validator(&validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(1))));
        s2.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(5))))
            .unwrap();
        history.record(2, &changes(&s1, &s2).unwrap()).unwrap();

        let mut set = current_set(&s2, &PRIMARY_NETWORK_ID).unwrap();
        assert_eq!(set[&NodeId([1; 20])].public_key, Some(long(5)));
        history.rewind(&mut set, &PRIMARY_NETWORK_ID, 2, 1).unwrap();
        assert_eq!(set[&NodeId([1; 20])].public_key, Some(long(1)));
    }

    #[test]
    fn a_height_the_chain_has_not_reached_has_no_set() {
        let history = History::new();
        let mut set = BTreeMap::new();
        assert_eq!(
            history.rewind(&mut set, &PRIMARY_NETWORK_ID, 4, 5),
            Err(Error::NotReached { asked: 5, reached: 4 })
        );
    }

    #[test]
    fn one_height_recorded_twice_composes() {
        // Go: `writeValidatorDiffs` is called once per layer and a height can
        // hold more than one. Replacing rather than composing would lose the
        // first half of what the height did.
        let place = Where {
            chain: PRIMARY_NETWORK_ID,
            node: NodeId([1; 20]),
        };
        let mut history = History::new();

        let mut first = Change::default();
        first.weight.add(10).unwrap();
        first.validation = [7; 32];
        history.record(9, &BTreeMap::from([(place, first)])).unwrap();

        let mut second = Change::default();
        second.weight.add(5).unwrap();
        history.record(9, &BTreeMap::from([(place, second)])).unwrap();

        let held = &history.by_height[&9][&place];
        assert_eq!(held.weight, WeightDiff { decrease: false, amount: 15 });
        assert_eq!(held.validation, [7; 32]);
    }

    #[test]
    fn an_inactive_l1_validator_weighs_on_the_set_and_cannot_be_sampled() {
        use crate::l1::Validator as L1Validator;

        let chain = [4u8; 32];
        let mut s = State::new();
        s.put_l1_validator(L1Validator {
            validation_id: [8; 32],
            chain_id: chain,
            node_id: NodeId([9; 20]),
            public_key: long(3),
            weight: 60,
            // Zero accrued-fee mark: it has run out of money.
            end_accumulated_fee: 0,
            ..L1Validator::default()
        })
        .unwrap();

        let set = current_set(&s, &chain).unwrap();
        // Under the empty node, with no key: its weight is in the denominator
        // and nothing it could send would count.
        assert_eq!(set.len(), 1);
        let pooled = &set[&NodeId::EMPTY];
        assert_eq!(pooled.weight, 60);
        assert_eq!(pooled.public_key, None);
    }

    #[test]
    fn an_active_l1_validator_is_sampled_under_its_own_node_and_key() {
        use crate::l1::Validator as L1Validator;

        let chain = [4u8; 32];
        let mut s = State::new();
        s.put_l1_validator(L1Validator {
            validation_id: [8; 32],
            chain_id: chain,
            node_id: NodeId([9; 20]),
            public_key: long(3),
            weight: 60,
            end_accumulated_fee: 1_000,
            ..L1Validator::default()
        })
        .unwrap();

        let set = current_set(&s, &chain).unwrap();
        assert_eq!(set[&NodeId([9; 20])].weight, 60);
        assert_eq!(set[&NodeId([9; 20])].public_key, Some(long(3)));
        assert_eq!(set[&NodeId([9; 20])].tx_id, [8; 32]);
    }

    #[test]
    fn an_l1_validator_renamed_without_changing_weight_still_records_its_old_name() {
        // Removed and re-registered in one block: the weight never moves, so a
        // record that only stored weight would lose the name the entry had, and
        // the past set would answer with a validation id that did not exist
        // then.
        use crate::l1::Validator as L1Validator;

        let chain = [4u8; 32];
        let mut before = State::new();
        before
            .put_l1_validator(L1Validator {
                validation_id: [1; 32],
                chain_id: chain,
                node_id: NodeId([9; 20]),
                public_key: long(3),
                weight: 60,
                end_accumulated_fee: 1_000,
                ..L1Validator::default()
            })
            .unwrap();

        let mut after = State::new();
        after
            .put_l1_validator(L1Validator {
                validation_id: [2; 32],
                chain_id: chain,
                node_id: NodeId([9; 20]),
                public_key: long(3),
                weight: 60,
                end_accumulated_fee: 1_000,
                ..L1Validator::default()
            })
            .unwrap();

        let c = changes(&before, &after).unwrap();
        let place = Where {
            chain,
            node: NodeId([9; 20]),
        };
        assert_eq!(c[&place].weight.amount, 0);
        assert!(c[&place].renamed);
        assert_eq!(c[&place].validation, [1; 32]);

        let mut history = History::new();
        history.record(5, &c).unwrap();
        let mut set = current_set(&after, &chain).unwrap();
        assert_eq!(set[&NodeId([9; 20])].tx_id, [2; 32]);
        history.rewind(&mut set, &chain, 5, 4).unwrap();
        assert_eq!(set[&NodeId([9; 20])].tx_id, [1; 32]);
    }

    #[test]
    fn a_change_to_one_network_does_not_move_another() {
        let chain = [3u8; 32];
        let mut history = History::new();

        let mut s0 = State::new();
        s0.put_current_validator(validator(1, 1, PRIMARY_NETWORK_ID, 100, Some(key(1))))
            .unwrap();
        s0.put_current_validator(validator(2, 1, chain, 7, None)).unwrap();

        let mut s1 = s0.clone();
        s1.put_current_validator(validator(3, 2, PRIMARY_NETWORK_ID, 40, Some(key(2))))
            .unwrap();
        history.record(1, &changes(&s0, &s1).unwrap()).unwrap();

        let mut other = current_set(&s1, &chain).unwrap();
        history.rewind(&mut other, &chain, 1, 0).unwrap();
        assert_eq!(other, current_set(&s0, &chain).unwrap());
    }
}
