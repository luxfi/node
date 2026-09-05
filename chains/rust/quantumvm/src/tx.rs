// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A transaction, the pool that holds pending ones, and the triage that decides
//! which of them a block may carry.

use std::collections::HashMap;
use std::sync::{Arc, Condvar, Mutex, OnceLock};
use std::time::Duration;

use crate::config::Config;
use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::quantum::{Quantum, Stamp};

/// One Q-Chain transaction.
///
/// Its id is the SHA-256 of its canonical wire — the SIGNATURE PREIMAGE, which
/// is the same bytes the ML-DSA signature covers and therefore cannot contain
/// the signature. Every other VM in this repo names a transaction the same way,
/// and the invariant `id == sha256(bytes())` holds unconditionally.
#[derive(Debug, Default)]
pub struct Tx {
    /// Unix SECONDS, which is the resolution the wire carries.
    pub timestamp: i64,
    pub nonce: u64,
    pub data: Vec<u8>,
    /// The user-paid burn in nLUX. Q-Chain's policy refuses every amount
    /// (LP-0130 §6), so this exists to be refused.
    pub fee: u64,
    /// The validator's ML-DSA attestation over this transaction.
    pub stamp: Option<Stamp>,

    bytes: OnceLock<Vec<u8>>,
    id: OnceLock<Id>,
}

impl Clone for Tx {
    fn clone(&self) -> Self {
        Tx {
            timestamp: self.timestamp,
            nonce: self.nonce,
            data: self.data.clone(),
            fee: self.fee,
            stamp: self.stamp.clone(),
            bytes: OnceLock::new(),
            id: OnceLock::new(),
        }
    }
}

impl PartialEq for Tx {
    fn eq(&self, other: &Self) -> bool {
        self.timestamp == other.timestamp
            && self.nonce == other.nonce
            && self.data == other.data
            && self.fee == other.fee
            && self.stamp == other.stamp
    }
}
impl Eq for Tx {}

impl Tx {
    /// A transaction with no signature yet.
    pub fn new(timestamp: i64, nonce: u64, data: Vec<u8>) -> Tx {
        Tx {
            timestamp,
            nonce,
            data,
            ..Tx::default()
        }
    }

    /// The same transaction, offering a fee. Q-Chain refuses every amount
    /// (LP-0130 §6), so this exists to be refused — see
    /// [`crate::vm::Qvm::issue_tx`].
    pub fn offering(mut self, fee: u64) -> Tx {
        self.fee = fee;
        self
    }

    /// The canonical wire: what the signature covers.
    pub fn bytes(&self) -> &[u8] {
        self.bytes.get_or_init(|| crate::wire::tx_body(self))
    }

    /// The content id.
    pub fn id(&self) -> Id {
        *self.id.get_or_init(|| ids::hash(self.bytes()))
    }

    /// What the transaction is, structurally.
    ///
    /// A Q-Chain transaction is an attestation, so one with no stamp attests
    /// nothing. Whether the stamp that IS there is good is a separate question,
    /// answered by the signer against this transaction's bytes.
    pub fn verify(&self) -> Result<()> {
        match self.stamp {
            Some(_) => Ok(()),
            None => Err(Error::MissingStamp),
        }
    }

    /// Apply the transaction.
    ///
    /// Q-Chain carries no user-visible ledger: a transaction's effect is that
    /// it is on the chain, attested, at a height. There is nothing further to
    /// move, so this succeeds — and it is still called from exactly one place,
    /// [`crate::block::Block::apply`], on every node, once, at accept time.
    pub fn execute(&self) -> Result<()> {
        Ok(())
    }
}

/// What a pending transaction waits in.
///
/// Bounded, deduplicated, and it is what learns FIRST that a block can be
/// built, whichever path the transaction arrived by.
pub struct Pool {
    state: Mutex<Held>,
    work: Latch,
    max: usize,
}

#[derive(Default)]
struct Held {
    pending: HashMap<Id, Arc<Tx>>,
    queue: Vec<Arc<Tx>>,
    closed: bool,
}

impl Pool {
    pub fn new(max: usize) -> Pool {
        Pool {
            state: Mutex::new(Held::default()),
            work: Latch::default(),
            max,
        }
    }

    /// Admit a transaction, and tell the builder there is something to build.
    ///
    /// Consensus builds nothing until it is told there is something to build,
    /// so the signal is part of admitting, not a thing a caller may forget.
    pub fn add(&self, tx: Arc<Tx>) -> Result<()> {
        {
            let mut held = self.state.lock().expect("pool");
            if held.closed {
                return Err(Error::PoolClosed);
            }
            if held.pending.len() >= self.max {
                return Err(Error::PoolFull);
            }
            let id = tx.id();
            if held.pending.contains_key(&id) {
                return Err(Error::DuplicateTx(id));
            }
            tx.verify()?;
            held.pending.insert(id, Arc::clone(&tx));
            held.queue.push(tx);
        }
        self.work.signal();
        Ok(())
    }

    /// Drop one transaction.
    pub fn remove(&self, id: &Id) -> Result<()> {
        let mut held = self.state.lock().expect("pool");
        if held.pending.remove(id).is_none() {
            return Err(Error::TxNotInPool(*id));
        }
        held.queue.retain(|tx| tx.id() != *id);
        Ok(())
    }

    /// The first `limit` pending transactions, in arrival order. Zero or more
    /// than there are means all of them.
    ///
    /// This COPIES rather than drains: a block that is built and then loses
    /// must leave its transactions where they were, and only acceptance takes
    /// them out.
    pub fn pending(&self, limit: usize) -> Vec<Arc<Tx>> {
        let held = self.state.lock().expect("pool");
        let n = if limit == 0 || limit > held.queue.len() {
            held.queue.len()
        } else {
            limit
        };
        held.queue[..n].iter().map(Arc::clone).collect()
    }

    pub fn len(&self) -> usize {
        self.state.lock().expect("pool").pending.len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    pub fn close(&self) {
        let mut held = self.state.lock().expect("pool");
        held.closed = true;
        held.pending.clear();
        held.queue.clear();
        drop(held);
        // Anything waiting for work is waiting forever otherwise.
        self.work.signal();
    }

    /// Block until there is something to build, or `timeout` passes. `true`
    /// when it was work that ended the wait.
    pub fn wait(&self, timeout: Duration) -> bool {
        self.work.wait(timeout)
    }

    /// Re-arm the builder while the pool still holds anything.
    ///
    /// The latch carries ONE signal, so N arrivals wake one build, and a build
    /// that takes fewer than N leaves the rest with nothing to wake them: they
    /// sit in the pool until some unrelated transaction arrives, which on a
    /// quiet chain is never. Whoever drains the pool says so afterwards.
    pub fn signal_if_work(&self) {
        let has = {
            let held = self.state.lock().expect("pool");
            !held.queue.is_empty()
        };
        if has {
            self.work.signal();
        }
    }
}

/// One bit of "there is something to do", set by whoever notices and cleared by
/// whoever acts on it.
#[derive(Default)]
struct Latch {
    set: Mutex<bool>,
    woke: Condvar,
}

impl Latch {
    fn signal(&self) {
        let mut set = self.set.lock().expect("latch");
        *set = true;
        self.woke.notify_one();
    }

    fn wait(&self, timeout: Duration) -> bool {
        let set = self.set.lock().expect("latch");
        let (mut set, _) = self
            .woke
            .wait_timeout_while(set, timeout, |signalled| !*signalled)
            .expect("latch");
        if *set {
            *set = false;
            return true;
        }
        false
    }
}

/// Sort a batch into what may go in a block and what has to leave the pool.
///
/// Both halves matter. The survivors go in the block; the rest are DROPPED,
/// because a transaction that cannot verify now will not verify later — a stamp
/// only gets staler — and left in place it holds a pool slot for good, until
/// enough of them fill the pool and the chain accepts nothing.
///
/// It does not execute anything. Effects belong to accept, on every node, once:
/// running them here would run them on a block the network may never accept,
/// twice when the builder rebuilds, and not at all on a node that received the
/// block rather than building it — which is every node but one, for every
/// block.
pub fn triage(quantum: &Quantum, config: &Config, txs: &[Arc<Tx>]) -> (Vec<Arc<Tx>>, Vec<Arc<Tx>>) {
    let mut verified: Vec<Arc<Tx>> = Vec::with_capacity(txs.len());
    let mut rejected: Vec<Arc<Tx>> = Vec::new();

    // What the transaction says about itself. A missing stamp is caught here;
    // whether the stamp that IS present is good is the next question.
    for tx in txs {
        match tx.verify() {
            Ok(()) => verified.push(Arc::clone(tx)),
            Err(_) => rejected.push(Arc::clone(tx)),
        }
    }
    if verified.is_empty() || !config.quantum_stamp_enabled {
        return (verified, rejected);
    }

    let batch: Vec<(&[u8], Option<&Stamp>)> = verified
        .iter()
        .map(|tx| (tx.bytes(), tx.stamp.as_ref()))
        .collect();

    if quantum.verify_all(&batch).is_ok() {
        drop(batch);
        return (verified, rejected);
    }
    drop(batch);

    // The batch verdict is one bit, so a failed batch has to be re-run one at a
    // time to find WHICH signatures were bad — the honest ones beside them are
    // still admissible.
    let mut valid = Vec::with_capacity(verified.len());
    for tx in verified {
        if quantum.verify(tx.bytes(), tx.stamp.as_ref()).is_ok() {
            valid.push(tx);
        } else {
            rejected.push(tx);
        }
    }
    (valid, rejected)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::quantum::ALGORITHM_MLDSA65;

    fn signed(q: &Quantum, nonce: u64) -> Arc<Tx> {
        let key = q.generate().unwrap();
        let mut tx = Tx::new(1000, nonce, b"payload".to_vec());
        let stamp = q.sign(tx.bytes(), &key).unwrap();
        tx.stamp = Some(stamp);
        // The stamp is not part of the preimage, so the id does not move when
        // it is attached. Prove it rather than assume it.
        Arc::new(tx)
    }

    fn quantum() -> Quantum {
        Quantum::new(ALGORITHM_MLDSA65, Duration::from_secs(60)).unwrap()
    }

    #[test]
    fn a_transaction_id_is_the_hash_of_its_preimage_and_the_stamp_does_not_move_it() {
        let q = quantum();
        let mut tx = Tx::new(1000, 7, b"payload".to_vec());
        let before = ids::hash(tx.bytes());
        let key = q.generate().unwrap();
        tx.stamp = Some(q.sign(tx.bytes(), &key).unwrap());
        let after = Tx {
            stamp: tx.stamp.clone(),
            ..tx.clone()
        };
        assert_eq!(before, after.id());
    }

    // Go: transaction_test.go — the pool refuses a second copy, refuses an
    // unstamped transaction, and fills up.
    #[test]
    fn the_pool_deduplicates_bounds_and_refuses_what_attests_nothing() {
        let q = quantum();
        let pool = Pool::new(2);
        let a = signed(&q, 1);
        pool.add(Arc::clone(&a)).unwrap();
        assert!(matches!(
            pool.add(Arc::clone(&a)),
            Err(Error::DuplicateTx(_))
        ));

        pool.add(signed(&q, 2)).unwrap();
        assert!(matches!(pool.add(signed(&q, 3)), Err(Error::PoolFull)));

        let bare = Arc::new(Tx::new(1000, 9, b"x".to_vec()));
        let pool = Pool::new(4);
        assert!(matches!(pool.add(bare), Err(Error::MissingStamp)));
    }

    #[test]
    fn removing_what_is_not_there_is_an_error_not_a_silent_success() {
        let pool = Pool::new(4);
        assert!(matches!(
            pool.remove(&ids::EMPTY),
            Err(Error::TxNotInPool(_))
        ));
    }

    #[test]
    fn a_closed_pool_admits_nothing() {
        let q = quantum();
        let pool = Pool::new(4);
        pool.close();
        assert!(matches!(pool.add(signed(&q, 1)), Err(Error::PoolClosed)));
    }

    #[test]
    fn pending_copies_rather_than_drains() {
        let q = quantum();
        let pool = Pool::new(8);
        pool.add(signed(&q, 1)).unwrap();
        pool.add(signed(&q, 2)).unwrap();
        assert_eq!(pool.pending(1).len(), 1);
        assert_eq!(pool.pending(0).len(), 2);
        assert_eq!(pool.pending(99).len(), 2);
        assert_eq!(pool.len(), 2, "reading the queue did not empty it");
    }

    // The latch is what makes a chain leave genesis: an arrival wakes a build.
    #[test]
    fn an_arrival_wakes_a_builder_and_a_leftover_re_arms_it() {
        let q = quantum();
        let pool = Pool::new(8);
        assert!(
            !pool.wait(Duration::from_millis(10)),
            "nothing to build yet"
        );
        pool.add(signed(&q, 1)).unwrap();
        assert!(pool.wait(Duration::from_millis(500)));
        assert!(
            !pool.wait(Duration::from_millis(10)),
            "one signal wakes one build"
        );
        // …which is exactly why whoever leaves work behind says so.
        pool.signal_if_work();
        assert!(pool.wait(Duration::from_millis(500)));
    }

    // Go: TestPoolHoldsUpUnderConcurrentUse. Arrivals, reads and removals all
    // land at once on a live chain, and what must hold is the invariant the
    // pool exists for: what the map holds and what the queue holds are the same
    // set.
    #[test]
    fn the_pool_holds_up_under_concurrent_use() {
        let q = quantum();
        let pool = Arc::new(Pool::new(64));
        let txs: Vec<Arc<Tx>> = (0..32u64).map(|n| signed(&q, n)).collect();

        std::thread::scope(|scope| {
            for tx in &txs {
                let pool = Arc::clone(&pool);
                let tx = Arc::clone(tx);
                scope.spawn(move || {
                    let _ = pool.add(tx);
                });
            }
            for _ in 0..8 {
                let pool = Arc::clone(&pool);
                scope.spawn(move || {
                    for _ in 0..32 {
                        let _ = pool.pending(4);
                    }
                });
            }
        });
        assert_eq!(pool.len(), txs.len());
        assert_eq!(pool.pending(0).len(), txs.len());

        std::thread::scope(|scope| {
            for tx in &txs {
                let pool = Arc::clone(&pool);
                let id = tx.id();
                scope.spawn(move || {
                    let _ = pool.remove(&id);
                });
            }
        });
        assert!(pool.is_empty());
        assert!(pool.pending(0).is_empty(), "the two views agree");
    }

    #[test]
    fn triage_separates_what_verified_from_what_did_not() {
        let q = quantum();
        let config = Config::default();
        let good = signed(&q, 1);
        let mut bad = (*signed(&q, 2)).clone();
        bad.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        let bad = Arc::new(bad);

        let (valid, rejected) = triage(&q, &config, &[Arc::clone(&good), Arc::clone(&bad)]);
        assert_eq!(valid.len(), 1);
        assert_eq!(valid[0].id(), good.id());
        assert_eq!(rejected.len(), 1);
        assert_eq!(rejected[0].id(), bad.id());
    }

    #[test]
    fn triage_with_stamps_off_does_not_check_them() {
        let q = quantum();
        let config = Config {
            quantum_stamp_enabled: false,
            ..Config::default()
        };
        let mut bad = (*signed(&q, 2)).clone();
        bad.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        let (valid, rejected) = triage(&q, &config, &[Arc::new(bad)]);
        assert_eq!(valid.len(), 1);
        assert!(rejected.is_empty());
    }

    #[test]
    fn a_transaction_with_no_stamp_never_reaches_the_signature_check() {
        let q = quantum();
        let config = Config::default();
        let bare = Arc::new(Tx::new(1000, 1, b"x".to_vec()));
        let (valid, rejected) = triage(&q, &config, &[bare]);
        assert!(valid.is_empty());
        assert_eq!(rejected.len(), 1);
    }
}
