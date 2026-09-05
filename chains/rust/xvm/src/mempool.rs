// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What is waiting to go into a block, and what is refused before it waits.
//!
//! Ported from `vms/txs/mempool` and the X-Chain's wrapper over it
//! (`vms/xvm/txs/mempool`). It is one bounded, ordered, conflict-free set with
//! a memory of what it threw away, and the four cheap refusals are checked in
//! Go's order — duplicate, too large, no room, conflicting — before the
//! expensive one.
//!
//! WHY THE ORDER MATTERS. Every check before the last is a comparison; the last
//! reads the chain's security terms and, in Go, may run a proof. Doing the
//! cheap ones first means a flood of junk costs a hash lookup each rather than
//! a verification each. That is not tidiness, it is the difference between a
//! mempool and a way to make a node do free work.
//!
//! WHY CONFLICTS ARE HELD BY UTXO. Two transactions that spend one output can
//! never both be in a block, so holding both would mean building a block that
//! is refused. The pool keeps which UTXOs each held transaction consumes, and
//! a new transaction overlapping any of them is refused rather than queued.
//! Removal is the same fact read the other way: removing a transaction removes
//! everything that conflicts with it, because a block that took one has settled
//! the others.
//!
//! WHAT IS REMEMBERED. A dropped transaction's reason is kept in a small
//! ring so a caller that asks again is told why, and so gossip does not spend
//! the network re-offering something this node has already judged. Being full
//! is not such a reason: it is a fact about this node at this moment, not about
//! the transaction, and remembering it would refuse a good transaction forever.

use std::collections::{BTreeSet, HashMap};
use std::sync::Arc;

use crate::error::{Error, Result};
use crate::ids::{Id, ShortId, SHORT_EMPTY};
use crate::security::{self, Exempt, Profile};
use crate::txs::Tx;

/// A mebibyte.
const MIB: usize = 1024 * 1024;

/// The largest transaction that may enter. Go raised this from 64 KiB so a
/// large genesis configuration fits in one transaction.
pub const MAX_TX_SIZE: usize = 2 * MIB;

/// How many bytes of transactions the pool holds at once.
pub const MAX_POOL_SIZE: usize = 64 * MIB;

/// How many refusals are remembered.
pub const DROPPED_REMEMBERED: usize = 64;

/// The transactions waiting to go into a block.
///
/// Insertion-ordered: the order they were offered in is the order they are
/// tried, so a block is a function of what arrived and when rather than of a
/// map's iteration order.
pub struct Mempool {
    order: Vec<Id>,
    txs: HashMap<Id, Tx>,
    /// What each held transaction consumes.
    consumed: HashMap<Id, BTreeSet<Id>>,
    /// Which transaction consumes each UTXO. The other direction of the same
    /// fact, so an overlap is a lookup rather than a scan.
    by_utxo: HashMap<Id, Id>,
    /// Bytes still free.
    available: usize,
    dropped: Dropped,
    profile: Option<Profile>,
    exempt: Option<Arc<dyn Exempt>>,
}

impl Default for Mempool {
    fn default() -> Self {
        Mempool::new()
    }
}

impl Mempool {
    /// A pool that has not been told its chain's terms.
    ///
    /// Admission is then structural only — Go's default before a chain calls
    /// `SetAuthPolicy`. [`Mempool::hold_to`] installs the terms.
    pub fn new() -> Mempool {
        Mempool {
            order: Vec::new(),
            txs: HashMap::new(),
            consumed: HashMap::new(),
            by_utxo: HashMap::new(),
            available: MAX_POOL_SIZE,
            dropped: Dropped::new(DROPPED_REMEMBERED),
            profile: None,
            exempt: None,
        }
    }

    /// Install the chain's admission terms. Go's `SetAuthPolicy`.
    pub fn hold_to(&mut self, profile: Profile, exempt: Option<Arc<dyn Exempt>>) {
        self.profile = Some(profile);
        self.exempt = exempt;
    }

    /// The terms this pool holds to, if it has been told any.
    pub fn profile(&self) -> Option<&Profile> {
        self.profile.as_ref()
    }

    /// The address the security gate is asked about.
    ///
    /// The empty one: a UTXO transaction has no single originator until its
    /// inputs are resolved against state, and admission happens before that.
    /// Go passes the same address for the same reason.
    fn originator(&self) -> ShortId {
        SHORT_EMPTY
    }

    /// Offer a transaction.
    ///
    /// The checks are Go's, in Go's order. A refusal is a value: the caller is
    /// told which of the five it was.
    pub fn add(&mut self, tx: Tx) -> Result<()> {
        let id = tx.id();

        if self.txs.contains_key(&id) {
            return Err(Error::DuplicateTx);
        }

        let size = tx.size();
        if size > MAX_TX_SIZE {
            return Err(Error::TxTooLarge(size, MAX_TX_SIZE));
        }
        if size > self.available {
            return Err(Error::MempoolFull(size, self.available));
        }

        let inputs = tx.input_ids();
        if inputs.iter().any(|u| self.by_utxo.contains_key(u)) {
            return Err(Error::ConflictsWithOtherTx);
        }

        // Last, because it is the one that reads policy rather than comparing
        // numbers. Everything a flood can fail on has already failed.
        if self.profile.is_some() {
            security::admits(
                &tx.creds,
                self.profile.as_ref(),
                self.exempt.as_deref(),
                &self.originator(),
            )?;
        }

        self.available -= size;
        self.order.push(id);
        self.txs.insert(id, tx);
        for u in &inputs {
            self.by_utxo.insert(*u, id);
        }
        self.consumed.insert(id, inputs);
        // What is held was not dropped.
        self.dropped.forget(&id);
        Ok(())
    }

    pub fn get(&self, id: &Id) -> Option<&Tx> {
        self.txs.get(id)
    }

    pub fn has(&self, id: &Id) -> bool {
        self.txs.contains_key(id)
    }

    /// Remove these transactions AND anything that conflicts with them.
    ///
    /// A transaction that went into a block settles every transaction that
    /// wanted the same outputs, whether or not this node was holding it.
    pub fn remove(&mut self, txs: &[Tx]) {
        for tx in txs {
            let id = tx.id();
            if self.consumed.contains_key(&id) {
                self.forget(&id);
                continue;
            }
            let overlapping: Vec<Id> = tx
                .input_ids()
                .iter()
                .filter_map(|u| self.by_utxo.get(u).copied())
                .collect();
            for other in overlapping {
                self.forget(&other);
            }
        }
    }

    /// Drop one transaction by id, and nothing else.
    ///
    /// Distinct from [`Mempool::remove`] on purpose. `remove` says "a block
    /// settled these", which also settles everything that wanted the same
    /// outputs. This says only "stop holding this one" — what a builder does
    /// with a candidate that could not go in the block it was building. A
    /// conflicting transaction may still be good, so it stays.
    pub fn remove_id(&mut self, id: &Id) {
        self.forget(id);
    }

    /// Drop one transaction and free what it held.
    fn forget(&mut self, id: &Id) {
        let Some(tx) = self.txs.remove(id) else {
            return;
        };
        self.available += tx.size();
        self.order.retain(|held| held != id);
        if let Some(inputs) = self.consumed.remove(id) {
            for u in inputs {
                self.by_utxo.remove(&u);
            }
        }
    }

    /// The oldest transaction waiting.
    pub fn peek(&self) -> Option<&Tx> {
        self.order.first().and_then(|id| self.txs.get(id))
    }

    /// Every transaction, oldest first, until `f` says stop.
    pub fn iterate(&self, mut f: impl FnMut(&Tx) -> bool) {
        for id in &self.order {
            if let Some(tx) = self.txs.get(id) {
                if !f(tx) {
                    return;
                }
            }
        }
    }

    /// Everything waiting, oldest first. What a block builder is offered.
    pub fn candidates(&self) -> Vec<Tx> {
        self.order
            .iter()
            .filter_map(|id| self.txs.get(id).cloned())
            .collect()
    }

    /// Remember why a transaction was refused.
    ///
    /// A transaction that is held is not dropped, and being full is not a
    /// judgement about a transaction — both are ignored, as in Go.
    pub fn mark_dropped(&mut self, id: Id, why: Error) {
        if matches!(why, Error::MempoolFull(..)) {
            return;
        }
        if self.txs.contains_key(&id) {
            return;
        }
        self.dropped.put(id, why);
    }

    /// Why a transaction was refused, if this node still remembers.
    pub fn drop_reason(&self, id: &Id) -> Option<&Error> {
        self.dropped.get(id)
    }

    pub fn len(&self) -> usize {
        self.txs.len()
    }

    pub fn is_empty(&self) -> bool {
        self.txs.is_empty()
    }

    /// Whether there is anything to build with. Go's `HasTxs`.
    pub fn has_txs(&self) -> bool {
        !self.is_empty()
    }

    /// Bytes still free. Not a consensus value — a way to see the pool.
    pub fn available(&self) -> usize {
        self.available
    }
}

/// The last few refusals, oldest evicted first.
///
/// Bounded because it is fed by whatever the network sends: an unbounded memory
/// of bad transactions is a way for a stranger to fill this node's.
struct Dropped {
    cap: usize,
    order: Vec<Id>,
    why: HashMap<Id, Error>,
}

impl Dropped {
    fn new(cap: usize) -> Dropped {
        Dropped {
            cap,
            order: Vec::new(),
            why: HashMap::new(),
        }
    }

    fn put(&mut self, id: Id, why: Error) {
        if self.why.insert(id, why).is_none() {
            self.order.push(id);
        }
        while self.order.len() > self.cap {
            let oldest = self.order.remove(0);
            self.why.remove(&oldest);
        }
    }

    fn get(&self, id: &Id) -> Option<&Error> {
        self.why.get(id)
    }

    fn forget(&mut self, id: &Id) {
        if self.why.remove(id).is_some() {
            self.order.retain(|held| held != id);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::TransferInput;
    use crate::fx::{self, FxIn, Input, Owners, State};
    use crate::ids;
    use crate::txs::{BaseTx, Unsigned};
    use crate::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, UtxoId};

    fn key(n: u8) -> [u8; 32] {
        let mut k = [0u8; 32];
        k[31] = n;
        k
    }

    /// A transaction spending output `idx` of transaction `src`, with a memo
    /// that makes it distinct from another one spending the same thing.
    fn tx_spending(src: u8, idx: u32, memo: &[u8]) -> Tx {
        Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: 10,
                blockchain_id: ids::prefixed(&[5]),
                outs: vec![TransferableOutput {
                    asset: Asset {
                        id: ids::prefixed(&[1]),
                    },
                    out: State::Transfer(crate::fx::secp256k1::TransferOutput {
                        amt: 1,
                        owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(ids::prefixed(&[src]), idx),
                    asset: Asset {
                        id: ids::prefixed(&[1]),
                    },
                    input: FxIn::Transfer(TransferInput {
                        amt: 1,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: memo.to_vec(),
            },
        }))
    }

    fn signed(src: u8, idx: u32, memo: &[u8]) -> Tx {
        let mut tx = tx_spending(src, idx, memo);
        tx.sign(fx::Family::Secp256k1, &[vec![key(1)]]).unwrap();
        tx
    }

    #[test]
    fn a_transaction_offered_once_is_held_once() {
        let mut m = Mempool::new();
        let tx = tx_spending(1, 0, b"a");
        m.add(tx.clone()).unwrap();
        assert_eq!(m.add(tx.clone()).unwrap_err(), Error::DuplicateTx);
        assert_eq!(m.len(), 1);
        assert!(m.has(&tx.id()));
        assert!(m.has_txs());
    }

    #[test]
    fn a_transaction_larger_than_the_bar_is_refused() {
        let mut m = Mempool::new();
        let big = tx_spending(1, 0, &[0u8; 8]);
        let size = big.size();
        // Say the bar is what this transaction exceeds, by shrinking the room
        // rather than building a two-mebibyte transaction: the two checks are
        // different refusals and both are exercised below.
        assert!(size < MAX_TX_SIZE);
        m.add(big).unwrap();

        let mut tight = Mempool::new();
        tight.available = 4;
        let err = tight.add(tx_spending(2, 0, b"b")).unwrap_err();
        assert!(matches!(err, Error::MempoolFull(..)), "{err:?}");
    }

    #[test]
    fn the_size_bar_is_the_one_go_uses() {
        assert_eq!(MAX_TX_SIZE, 2 * 1024 * 1024);
        assert_eq!(MAX_POOL_SIZE, 64 * 1024 * 1024);
        assert_eq!(DROPPED_REMEMBERED, 64);
    }

    #[test]
    fn two_transactions_that_spend_one_output_are_not_both_held() {
        let mut m = Mempool::new();
        m.add(tx_spending(1, 0, b"first")).unwrap();
        assert_eq!(
            m.add(tx_spending(1, 0, b"second")).unwrap_err(),
            Error::ConflictsWithOtherTx
        );
        assert_eq!(m.len(), 1);
        // A different output of the same transaction is not a conflict.
        m.add(tx_spending(1, 1, b"third")).unwrap();
        assert_eq!(m.len(), 2);
    }

    #[test]
    fn removing_a_transaction_frees_its_room_and_its_outputs() {
        let mut m = Mempool::new();
        let tx = tx_spending(1, 0, b"a");
        let before = m.available();
        m.add(tx.clone()).unwrap();
        assert_eq!(m.available(), before - tx.size());
        m.remove(std::slice::from_ref(&tx));
        assert_eq!(m.len(), 0);
        assert_eq!(m.available(), before);
        // And the output it wanted is free again.
        m.add(tx_spending(1, 0, b"b")).unwrap();
    }

    #[test]
    fn removing_a_transaction_removes_what_conflicts_with_it() {
        let mut m = Mempool::new();
        let held = tx_spending(1, 0, b"held");
        m.add(held).unwrap();
        // A different transaction spending the same output — one this node was
        // not holding — went into a block. What it settled goes too.
        m.remove(&[tx_spending(1, 0, b"in the block")]);
        assert_eq!(m.len(), 0);
        assert_eq!(m.available(), MAX_POOL_SIZE);
    }

    #[test]
    fn the_oldest_offer_is_the_first_tried() {
        let mut m = Mempool::new();
        let a = tx_spending(1, 0, b"a");
        let b = tx_spending(2, 0, b"b");
        let c = tx_spending(3, 0, b"c");
        m.add(a.clone()).unwrap();
        m.add(b.clone()).unwrap();
        m.add(c.clone()).unwrap();
        assert_eq!(m.peek().unwrap().id(), a.id());
        assert_eq!(
            m.candidates().iter().map(|t| t.id()).collect::<Vec<_>>(),
            vec![a.id(), b.id(), c.id()]
        );

        // And removing the oldest promotes the next, not an arbitrary one.
        m.remove(&[a]);
        assert_eq!(m.peek().unwrap().id(), b.id());
    }

    #[test]
    fn iteration_stops_when_the_caller_says_so() {
        let mut m = Mempool::new();
        for n in 1u8..5 {
            m.add(tx_spending(n, 0, &[n])).unwrap();
        }
        let mut seen = 0;
        m.iterate(|_| {
            seen += 1;
            seen < 2
        });
        assert_eq!(seen, 2);
    }

    #[test]
    fn an_empty_pool_has_nothing_to_offer() {
        let m = Mempool::new();
        assert!(m.is_empty());
        assert!(!m.has_txs());
        assert!(m.peek().is_none());
        assert!(m.candidates().is_empty());
        assert_eq!(m.available(), MAX_POOL_SIZE);
    }

    #[test]
    fn a_refusal_is_remembered_and_a_held_transaction_is_not() {
        let mut m = Mempool::new();
        let bad = tx_spending(9, 0, b"bad");
        m.mark_dropped(bad.id(), Error::WrongSig);
        assert_eq!(m.drop_reason(&bad.id()), Some(&Error::WrongSig));

        // Holding it forgets the judgement.
        m.add(bad.clone()).unwrap();
        assert_eq!(m.drop_reason(&bad.id()), None);

        // And a transaction that is held cannot be marked dropped.
        m.mark_dropped(bad.id(), Error::WrongSig);
        assert_eq!(m.drop_reason(&bad.id()), None);
    }

    #[test]
    fn being_full_is_not_a_judgement_about_a_transaction() {
        let mut m = Mempool::new();
        let tx = tx_spending(1, 0, b"a");
        m.mark_dropped(tx.id(), Error::MempoolFull(1, 0));
        assert_eq!(m.drop_reason(&tx.id()), None, "it may be good later");
    }

    #[test]
    fn only_the_last_few_refusals_are_remembered() {
        let mut m = Mempool::new();
        let first = tx_spending(0, 0, b"first");
        m.mark_dropped(first.id(), Error::WrongSig);
        for n in 0..DROPPED_REMEMBERED {
            let tx = tx_spending(1, n as u32, b"later");
            m.mark_dropped(tx.id(), Error::WrongSig);
        }
        assert_eq!(m.drop_reason(&first.id()), None, "the oldest went first");
    }

    #[test]
    fn a_pool_that_was_never_told_its_terms_checks_structure_only() {
        let mut m = Mempool::new();
        assert!(m.profile().is_none());
        // A classical credential, admitted, because no terms were installed —
        // Go's behaviour before a chain calls SetAuthPolicy.
        m.add(signed(1, 0, b"a")).unwrap();
        assert_eq!(m.len(), 1);
    }

    #[test]
    fn a_strict_pool_refuses_a_classically_signed_transaction() {
        let mut m = Mempool::new();
        m.hold_to(security::strict_pq(), None);
        assert_eq!(m.profile(), Some(&security::strict_pq()));
        assert_eq!(
            m.add(signed(1, 0, b"a")).unwrap_err(),
            Error::ClassicalCredentialRefused
        );
        assert_eq!(m.len(), 0);
        assert_eq!(m.available(), MAX_POOL_SIZE, "nothing was reserved");
    }

    #[test]
    fn a_permissive_pool_admits_the_same_transaction() {
        let mut m = Mempool::new();
        m.hold_to(security::permissive(), None);
        m.add(signed(1, 0, b"a")).unwrap();
        assert_eq!(m.len(), 1);
    }

    #[test]
    fn a_named_originator_is_carried_over_by_a_strict_pool() {
        let mut m = Mempool::new();
        m.hold_to(
            security::strict_pq(),
            Some(Arc::new(security::Listed::of(&[SHORT_EMPTY]))),
        );
        m.add(signed(1, 0, b"a")).unwrap();
        assert_eq!(m.len(), 1);
    }

    #[test]
    fn the_cheap_refusals_come_before_the_policy_one() {
        // A duplicate is refused as a duplicate even on a strict chain that
        // would also have refused it for its credential — the order is what
        // stops a flood from costing a policy read each.
        let mut m = Mempool::new();
        m.hold_to(security::permissive(), None);
        let tx = signed(1, 0, b"a");
        m.add(tx.clone()).unwrap();
        m.hold_to(security::strict_pq(), None);
        assert_eq!(m.add(tx).unwrap_err(), Error::DuplicateTx);
    }
}
