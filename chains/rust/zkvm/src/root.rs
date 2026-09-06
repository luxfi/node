// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The committed state root, and the fold that produces the next one.
//!
//! There is exactly ONE root function and it is [`Root::after`]. A
//! hardware-conditional digest — a GPU path with a CPU fallback — would make
//! the consensus-committed root depend on whether a node has an accelerator, so
//! validators with and without one would reject each other's blocks.
//!
//! [`Root::after`] is PURE. Nothing is mutated, so computing a root is safe
//! inside a block's verification: verifying the same block twice, or verifying
//! a block that is later rejected and then verifying its competitor, all yield
//! the root that block's proposer computed. Only [`Root::finalize`] advances
//! the committed root, and only acceptance calls it.

use crate::db::Db;
use crate::error::{Error, Result};
use crate::hash::Fold;
use crate::ids::ID_LEN;
use crate::txs::Tx;

/// Where the committed root is kept.
pub const KEY: &[u8] = b"state_root";

/// The committed state root.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Root {
    committed: Vec<u8>,
}

impl Root {
    /// Open the committed root from `db`.
    pub fn open(db: &dyn Db) -> Result<Root> {
        let mut r = Root {
            committed: vec![0u8; ID_LEN],
        };
        r.reload(db)?;
        Ok(r)
    }

    /// The state root that results from applying `txs` on top of the committed
    /// one:
    ///
    /// ```text
    /// sha256(committed ‖ every output commitment ‖ every nullifier)
    /// ```
    ///
    /// Both runs are in transaction order, and the commitments all precede the
    /// nullifiers — one pass over the block for each — because that is the
    /// order the fold is defined in and a different order is a different chain.
    pub fn after(&self, txs: &[Tx]) -> Vec<u8> {
        let mut h = Fold::new();
        h.raw(&self.committed);
        for tx in txs {
            for out in &tx.outputs {
                h.raw(&out.commitment);
            }
        }
        for tx in txs {
            for n in &tx.nullifiers {
                h.raw(n);
            }
        }
        h.id().to_vec()
    }

    /// Advance the committed root. The only mutation, and acceptance is its
    /// only caller.
    ///
    /// The record goes first: a root held in memory that is not in the database
    /// is a root this node alone believes.
    pub fn finalize(&mut self, db: &mut dyn Db, next: &[u8]) -> Result<()> {
        db.put(KEY, next)?;
        self.committed = next.to_vec();
        Ok(())
    }

    /// The committed state root.
    pub fn get(&self) -> &[u8] {
        &self.committed
    }

    /// Put the committed root back to what the database now says, discarding an
    /// advance that belonged to a block whose writes were discarded.
    ///
    /// A read that FAILED is not an absent root. Answering any error with the
    /// empty root and reporting success is how an unreadable database boots a
    /// node believing the shielded state is empty — after which it disagrees
    /// with the network on every block it sees, permanently. Only an absent key
    /// means a chain with no root yet.
    pub fn reload(&mut self, db: &dyn Db) -> Result<()> {
        match db.get(KEY) {
            Err(Error::NotFound) => {
                self.committed = vec![0u8; ID_LEN];
                Ok(())
            }
            Err(e) => Err(Error::StateRootRead(Box::new(e))),
            Ok(raw) if raw.len() != ID_LEN => Err(Error::StateRootSize(raw.len())),
            Ok(raw) => {
                self.committed = raw;
                Ok(())
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::Memory;
    use crate::hash::sha256;
    use crate::ids;
    use crate::txs::{Kind, Shielded, Tx};

    fn tx(nullifier: u8, commitment: u8) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            nullifiers: vec![vec![nullifier; 32]],
            outputs: vec![Shielded {
                commitment: vec![commitment; 32],
                ..Shielded::default()
            }],
            ..Tx::default()
        }
    }

    #[test]
    fn a_chain_with_no_root_starts_at_the_zero_root() {
        let r = Root::open(&Memory::new()).unwrap();
        assert_eq!(r.get(), &[0u8; 32]);
    }

    #[test]
    fn the_fold_over_no_transactions_is_the_hash_of_the_committed_root() {
        let r = Root::open(&Memory::new()).unwrap();
        assert_eq!(r.after(&[]), sha256(&[0u8; 32]).to_vec());
    }

    #[test]
    fn the_fold_writes_every_commitment_then_every_nullifier() {
        let mut r = Root::open(&Memory::new()).unwrap();
        r.committed = vec![7u8; 32];
        let txs = vec![tx(1, 0xA1), tx(2, 0xA2)];

        let mut want = Vec::new();
        want.extend_from_slice(&[7u8; 32]);
        want.extend_from_slice(&[0xA1u8; 32]);
        want.extend_from_slice(&[0xA2u8; 32]);
        want.extend_from_slice(&[1u8; 32]);
        want.extend_from_slice(&[2u8; 32]);
        assert_eq!(r.after(&txs), sha256(&want).to_vec());
    }

    /// The commitments-then-nullifiers order is load-bearing: interleaving the
    /// two would be a different chain, and this says so rather than leaving it
    /// to be discovered.
    #[test]
    fn interleaving_the_two_runs_would_be_a_different_root() {
        let r = Root::open(&Memory::new()).unwrap();
        let txs = vec![tx(1, 0xA1), tx(2, 0xA2)];
        let mut interleaved = Vec::new();
        interleaved.extend_from_slice(&[0u8; 32]);
        for t in &txs {
            interleaved.extend_from_slice(&t.outputs[0].commitment);
            interleaved.extend_from_slice(&t.nullifiers[0]);
        }
        assert_ne!(r.after(&txs), sha256(&interleaved).to_vec());
    }

    #[test]
    fn the_fold_mutates_nothing_so_it_can_run_inside_verification() {
        let r = Root::open(&Memory::new()).unwrap();
        let txs = vec![tx(1, 0xA1)];
        let once = r.after(&txs);
        let twice = r.after(&txs);
        assert_eq!(once, twice);
        assert_eq!(r.get(), &[0u8; 32]);
    }

    #[test]
    fn finalize_records_before_it_believes() {
        let mut db = Memory::new();
        let mut r = Root::open(&db).unwrap();
        let next = r.after(&[]);
        r.finalize(&mut db, &next).unwrap();
        assert_eq!(r.get(), next.as_slice());
        assert_eq!(db.get(KEY).unwrap(), next);

        // A second Root over the same database opens at the same place.
        assert_eq!(Root::open(&db).unwrap().get(), next.as_slice());
    }

    #[test]
    fn a_reload_discards_an_advance_the_database_never_took() {
        let mut db = Memory::new();
        let mut r = Root::open(&db).unwrap();
        let genesis = r.after(&[]);
        r.finalize(&mut db, &genesis).unwrap();

        // A block advanced the root in memory and its writes were discarded.
        r.committed = vec![0xFF; 32];
        r.reload(&db).unwrap();
        assert_eq!(r.get(), genesis.as_slice());
    }

    #[test]
    fn a_root_that_is_not_thirty_two_bytes_is_refused_rather_than_used() {
        let mut db = Memory::new();
        db.put(KEY, &[1, 2, 3]).unwrap();
        assert_eq!(Root::open(&db), Err(Error::StateRootSize(3)));
    }

    /// The genesis fold. A chain born from a genesis with no transactions still
    /// starts ONE fold in, not at the zero root, because acceptance commits
    /// `after(genesis transactions)` before any block. Every block-1 state root
    /// in the corpus is computed against that.
    #[test]
    fn a_chain_starts_one_fold_in_even_with_an_empty_genesis() {
        let mut db = Memory::new();
        let mut r = Root::open(&db).unwrap();
        r.finalize(&mut db, &r.clone().after(&[])).unwrap();
        assert_eq!(r.get(), sha256(&[0u8; 32]).to_vec().as_slice());
        assert_eq!(
            ids::hex_of(&r.after(&[])),
            "2b32db6c2c0a6235fb1397e8225ea85e0f0e6e8c7b126d0016ccbde0e667151e"
        );
    }
}
