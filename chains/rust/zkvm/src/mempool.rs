// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What is waiting for a block.
//!
//! Three rules make this bounded and honest:
//!
//! ONE, the id is DERIVED here, not taken from the caller. A client decoding
//! straight into a transaction supplies whatever id it likes, and a proposer
//! carrying that id would compute a block id its own peers do not — the block
//! it built would not be the block they see.
//!
//! TWO, a full pool gives up whatever pays LEAST, so what it holds is always
//! the best it has been offered; an arrival that is itself the cheapest is
//! refused, because taking it would mean dropping a better transaction for a
//! worse one.
//!
//! THREE, a transaction the chain has passed is DROPPED. Nothing else drops
//! it: one that can never enter a block would occupy a slot forever, and a
//! pool full of those refuses every honest arrival paying the same floor.

use std::collections::HashMap;
use std::sync::{Condvar, Mutex};

use crate::error::{Error, Result};
use crate::ids::Id;
use crate::tx::Transaction;

pub struct Mempool {
    max_size: usize,
    state: Mutex<State>,
    /// Consensus builds nothing until it is told there is something to build.
    /// Waiting only on a timer means the chain never leaves genesis, however
    /// many transactions the pool has accepted.
    work: Condvar,
}

#[derive(Default)]
struct State {
    txs: HashMap<Id, Transaction>,
    /// nullifier → the transaction that claims it. Two transactions spending
    /// one note cannot both wait: one of them can never be built.
    nullifiers: HashMap<Vec<u8>, Id>,
    /// Raised when something arrives, lowered when a waiter takes it.
    pending: bool,
}

impl Mempool {
    pub fn new(max_size: usize) -> Mempool {
        Mempool {
            max_size,
            state: Mutex::new(State::default()),
            work: Condvar::new(),
        }
    }

    /// Admit a transaction. Answers `Ok(id)` with the id the CHAIN will carry.
    pub fn add(&self, tx: &Transaction) -> Result<Id> {
        let tx = tx.clone().with_id();
        let id = tx.id;
        let mut st = self.state.lock().map_err(lock)?;

        if st.txs.contains_key(&id) {
            return Ok(id); // already waiting
        }
        for n in &tx.nullifiers {
            if st.nullifiers.contains_key(n) {
                return Err(Error::BadRequest("nullifier already in mempool".into()));
            }
        }

        if st.txs.len() >= self.max_size {
            match st
                .txs
                .values()
                .min_by_key(|t| (t.fee, t.id))
                .map(|t| (t.id, t.fee))
            {
                Some((victim, fee)) if fee < tx.fee => remove(&mut st, &victim),
                _ => {
                    return Err(Error::BadRequest(
                        "mempool is full and the transaction pays less than what it would displace"
                            .into(),
                    ))
                }
            }
        }

        for n in &tx.nullifiers {
            st.nullifiers.insert(n.clone(), id);
        }
        st.txs.insert(id, tx);
        st.pending = true;
        self.work.notify_one();
        Ok(id)
    }

    pub fn remove(&self, id: &Id) {
        if let Ok(mut st) = self.state.lock() {
            remove(&mut st, id);
        }
    }

    pub fn has(&self, id: &Id) -> bool {
        self.state
            .lock()
            .map(|s| s.txs.contains_key(id))
            .unwrap_or(false)
    }

    pub fn len(&self) -> usize {
        self.state.lock().map(|s| s.txs.len()).unwrap_or(0)
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// The best `limit` transactions, highest fee first.
    ///
    /// Ties break on the id, so two nodes holding the same pool assemble the
    /// same block. A hash-map iteration order would make the batch — and
    /// therefore the block — depend on which node built it.
    pub fn pending(&self, limit: usize) -> Vec<Transaction> {
        let Ok(st) = self.state.lock() else {
            return Vec::new();
        };
        let mut all: Vec<&Transaction> = st.txs.values().collect();
        all.sort_by(|a, b| b.fee.cmp(&a.fee).then_with(|| a.id.cmp(&b.id)));
        all.into_iter().take(limit).cloned().collect()
    }

    /// Drop everything the chain has passed.
    pub fn prune_expired(&self, height: u64) -> usize {
        let Ok(mut st) = self.state.lock() else {
            return 0;
        };
        let doomed: Vec<Id> = st
            .txs
            .values()
            .filter(|t| t.expiry > 0 && t.expiry < height)
            .map(|t| t.id)
            .collect();
        for id in &doomed {
            remove(&mut st, id);
        }
        doomed.len()
    }

    /// Block until there is something to build from.
    ///
    /// Answers `true` when work is waiting. `timeout` bounds the wait so a
    /// caller can be shut down; the Go reference passes a context for the same
    /// reason.
    pub fn wait_for_work(&self, timeout: std::time::Duration) -> bool {
        let Ok(mut st) = self.state.lock() else {
            return false;
        };
        if st.pending {
            st.pending = false;
            return true;
        }
        let (mut st, _) = match self.work.wait_timeout(st, timeout) {
            Ok(v) => v,
            Err(_) => return false,
        };
        let ready = st.pending;
        st.pending = false;
        ready
    }
}

fn remove(st: &mut State, id: &Id) {
    if let Some(tx) = st.txs.remove(id) {
        for n in &tx.nullifiers {
            // Only if it is still ours: a later transaction never took the
            // slot, because a conflicting arrival is refused above.
            if st.nullifiers.get(n) == Some(id) {
                st.nullifiers.remove(n);
            }
        }
    }
}

fn lock<T>(_: T) -> Error {
    Error::Storage("the mempool's lock was poisoned by a panic".into())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tx::{ShieldedOutput, TransactionType};

    fn tx(fee: u64, expiry: u64, nulls: &[&[u8]]) -> Transaction {
        Transaction {
            kind: TransactionType::Transfer as u8,
            fee,
            expiry,
            nullifiers: nulls.iter().map(|n| n.to_vec()).collect(),
            outputs: vec![ShieldedOutput::default()],
            ..Default::default()
        }
    }

    #[test]
    fn the_id_is_derived_here_whatever_the_caller_said() {
        let mp = Mempool::new(4);
        let mut forged = tx(1, 10, &[b"a"]);
        forged.id = [0xFF; 32];
        let id = mp.add(&forged).unwrap();
        assert_eq!(id, forged.compute_id());
        assert!(mp.has(&id));
        assert!(!mp.has(&[0xFF; 32]));
    }

    #[test]
    fn two_transactions_cannot_wait_on_one_note() {
        let mp = Mempool::new(4);
        mp.add(&tx(1, 10, &[b"n"])).unwrap();
        assert!(mp.add(&tx(2, 10, &[b"n"])).is_err());
        // Once the first leaves, the note is free again.
        let id = tx(1, 10, &[b"n"]).compute_id();
        mp.remove(&id);
        assert!(mp.add(&tx(2, 10, &[b"n"])).is_ok());
    }

    #[test]
    fn a_full_pool_gives_up_the_cheapest_and_refuses_a_cheaper_arrival() {
        let mp = Mempool::new(2);
        mp.add(&tx(10, 10, &[b"a"])).unwrap();
        mp.add(&tx(20, 10, &[b"b"])).unwrap();

        // Cheaper than everything held: refused, because taking it would drop
        // a better transaction for a worse one.
        assert!(mp.add(&tx(5, 10, &[b"c"])).is_err());
        assert_eq!(mp.len(), 2);

        // Better than the cheapest: the cheapest goes.
        mp.add(&tx(30, 10, &[b"d"])).unwrap();
        assert_eq!(mp.len(), 2);
        assert!(!mp.has(&tx(10, 10, &[b"a"]).compute_id()));
        assert!(mp.has(&tx(20, 10, &[b"b"]).compute_id()));
        assert!(mp.has(&tx(30, 10, &[b"d"]).compute_id()));
        // And the displaced transaction's note is free again.
        assert!(mp.add(&tx(40, 10, &[b"a"])).is_ok());
    }

    #[test]
    fn what_is_pending_is_ordered_by_fee_and_then_by_identity() {
        let mp = Mempool::new(8);
        for (fee, n) in [(5u64, &b"a"[..]), (9, b"b"), (9, b"c"), (1, b"d")] {
            mp.add(&tx(fee, 10, &[n])).unwrap();
        }
        let got = mp.pending(3);
        assert_eq!(got.len(), 3);
        assert_eq!(got[0].fee, 9);
        assert_eq!(got[1].fee, 9);
        assert_eq!(got[2].fee, 5);
        assert!(
            got[0].id < got[1].id,
            "a tie breaks on the id, not on a map's order"
        );
        // Twice in a row, the same batch — two nodes must assemble alike.
        assert_eq!(mp.pending(3), got);
    }

    #[test]
    fn the_chain_passing_a_height_drops_what_can_never_be_built() {
        let mp = Mempool::new(8);
        mp.add(&tx(1, 5, &[b"old"])).unwrap();
        mp.add(&tx(1, 50, &[b"live"])).unwrap();
        assert_eq!(mp.prune_expired(6), 1);
        assert_eq!(mp.len(), 1);
        assert!(mp.has(&tx(1, 50, &[b"live"]).compute_id()));
        // And the pruned transaction's note is free.
        assert!(mp.add(&tx(1, 60, &[b"old"])).is_ok());
    }

    #[test]
    fn a_waiter_is_woken_by_an_arrival() {
        use std::sync::Arc;
        let mp = Arc::new(Mempool::new(4));
        let waiter = {
            let mp = mp.clone();
            std::thread::spawn(move || mp.wait_for_work(std::time::Duration::from_secs(5)))
        };
        std::thread::sleep(std::time::Duration::from_millis(50));
        mp.add(&tx(1, 10, &[b"n"])).unwrap();
        assert!(waiter.join().unwrap(), "an arrival must wake the builder");
    }

    #[test]
    fn a_waiter_gives_up_when_nothing_arrives() {
        let mp = Mempool::new(4);
        assert!(!mp.wait_for_work(std::time::Duration::from_millis(20)));
    }
}
