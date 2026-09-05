// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! A block, and the rules that decide whether it may be built on.
//!
//! A block here is DATA. Whether it verifies, whether it is accepted and
//! whether it is dropped are the chain's business, not the block's, and they
//! live on [`crate::vm::Qvm`] keyed by id — which is also the shape the host
//! seam and the wire already have, because a block handle cannot cross a
//! process boundary.
//!
//! What IS on the block is what can be decided from the block alone plus the
//! one thing it names: its parent.

use std::sync::{Arc, OnceLock};

use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::quantum::{Quantum, Stamp};
use crate::tx::Tx;
use crate::wire::{self, MAX_BLOCK_SIZE, MAX_FUTURE_SKEW};

/// One Q-Chain block.
#[derive(Debug, Clone)]
pub struct Block {
    /// Unix SECONDS. Q-Chain's block-time resolution, and what the wire holds.
    pub timestamp: i64,
    pub height: u64,
    pub parent: Id,
    /// The chain this block belongs to.
    pub chain: Id,
    /// The network that chain belongs to.
    pub network: u32,
    pub txs: Vec<Arc<Tx>>,

    /// The canonical wire and the id over it, computed once.
    ///
    /// The id is NOT a field of the wire: it is `sha256(bytes)`. That is what
    /// keeps a signature off the block — a stamp on the wire would make the id
    /// depend on WHO signed, and two honest nodes would compute two ids for one
    /// block.
    wire: OnceLock<Vec<u8>>,
    id: OnceLock<Id>,
}

impl Block {
    pub fn new(
        timestamp: i64,
        height: u64,
        parent: Id,
        chain: Id,
        network: u32,
        txs: Vec<Arc<Tx>>,
    ) -> Block {
        Block {
            timestamp,
            height,
            parent,
            chain,
            network,
            txs,
            wire: OnceLock::new(),
            id: OnceLock::new(),
        }
    }

    /// The block's canonical ZAP wire.
    pub fn bytes(&self) -> &[u8] {
        self.wire.get_or_init(|| wire::block_bytes(self))
    }

    /// The content id: `sha256(bytes)`.
    pub fn id(&self) -> Id {
        *self.id.get_or_init(|| ids::hash(self.bytes()))
    }

    /// Refuse a block that names another chain or another network.
    ///
    /// Nothing else in the wire is chain-specific and genesis is a constant, so
    /// without this every Q-Chain in existence would share a genesis id and a
    /// block built on one would be a well-formed block on all of them.
    pub fn on_chain(&self, chain: Id, network: u32) -> Result<()> {
        if self.chain != chain || self.network != network {
            return Err(Error::ForeignChain {
                chain: self.chain,
                network: self.network,
                this_chain: chain,
                this_network: network,
            });
        }
        Ok(())
    }

    /// What can be decided without the parent: a proposed block is not genesis,
    /// carries work, and fits on the wire.
    ///
    /// Refusing an empty block is also what keeps the signature check from
    /// being satisfiable by removing its subject: a parser that dropped the
    /// transaction set would produce a block that verifies nothing, and a block
    /// that verifies nothing must not verify.
    pub fn well_formed(&self) -> Result<()> {
        // Genesis is written by the VM, never proposed.
        if self.height == 0 {
            return Err(Error::Genesis);
        }
        if self.txs.is_empty() {
            return Err(Error::EmptyBlock);
        }
        if self.bytes().len() > MAX_BLOCK_SIZE {
            return Err(Error::TooLarge {
                bytes: self.bytes().len(),
                limit: MAX_BLOCK_SIZE,
            });
        }
        Ok(())
    }

    /// The block sits on its parent, in both height and time.
    ///
    /// Checking only that the parent EXISTS lets a proposer pick height and
    /// timestamp freely: it can name genesis as the parent of a height-500
    /// block, rewind chain time to revive expired quantum stamps, or jump
    /// forward and expire every stamp in flight at once.
    pub fn follows(&self, parent: &Block, now: i64) -> Result<()> {
        if self.height != parent.height + 1 {
            return Err(Error::InvalidHeight {
                height: self.height,
                parent: parent.height,
            });
        }
        if self.timestamp < parent.timestamp {
            return Err(Error::TimeBeforeParent {
                block: self.timestamp,
                parent: parent.timestamp,
            });
        }
        if self.timestamp > now + MAX_FUTURE_SKEW {
            return Err(Error::TimeTooFarAhead {
                block: self.timestamp,
                limit: now + MAX_FUTURE_SKEW,
            });
        }
        Ok(())
    }

    /// Every transaction's ML-DSA signature, over the transaction it covers.
    ///
    /// One batch, so a block is one verdict: any bad signature is the block's
    /// answer, not one transaction's.
    pub fn stamps_verify(&self, quantum: &Quantum) -> Result<()> {
        let batch: Vec<(&[u8], Option<&Stamp>)> = self
            .txs
            .iter()
            .map(|tx| (tx.bytes(), tx.stamp.as_ref()))
            .collect();
        quantum
            .verify_all(&batch)
            .map_err(|e| Error::BlockSignature(e.to_string()))
    }

    /// Run the block's transactions.
    ///
    /// They do NOT run at build time. A builder that executed them would apply
    /// the effects of a block the network may never accept, apply them a second
    /// time when it rebuilt, and apply nothing at all on every node that
    /// received the block instead of building it — which is every node but one,
    /// for every block. They run where the block becomes the tip, on every
    /// node, exactly once.
    ///
    /// A transaction that cannot be applied stops the block: a node that cannot
    /// apply an agreed block stops rather than committing a chain its state no
    /// longer matches.
    pub fn apply(&self) -> Result<()> {
        for tx in &self.txs {
            tx.execute().map_err(|e| Error::Execute {
                tx: tx.id(),
                why: e.to_string(),
            })?;
        }
        Ok(())
    }
}

impl PartialEq for Block {
    fn eq(&self, other: &Self) -> bool {
        self.id() == other.id()
    }
}
impl Eq for Block {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::quantum::{Quantum, ALGORITHM_MLDSA65};
    use std::time::Duration;

    fn quantum() -> Quantum {
        Quantum::new(ALGORITHM_MLDSA65, Duration::from_secs(60)).unwrap()
    }

    fn stamped(q: &Quantum, nonce: u64) -> Arc<Tx> {
        let key = q.generate().unwrap();
        let mut tx = Tx::new(1000, nonce, b"payload".to_vec());
        tx.stamp = Some(q.sign(tx.bytes(), &key).unwrap());
        Arc::new(tx)
    }

    fn chain() -> Id {
        ids::filled(30)
    }

    fn parent() -> Block {
        Block::new(1000, 4, ids::filled(9), chain(), 1, Vec::new())
    }

    fn child(q: &Quantum, timestamp: i64, height: u64) -> Block {
        Block::new(
            timestamp,
            height,
            parent().id(),
            chain(),
            1,
            vec![stamped(q, 1)],
        )
    }

    #[test]
    fn a_block_id_is_the_hash_of_its_own_bytes() {
        let q = quantum();
        let b = child(&q, 1000, 5);
        assert_eq!(b.id(), ids::hash(b.bytes()));
    }

    #[test]
    fn a_block_of_another_chain_or_another_network_is_refused() {
        let q = quantum();
        let b = child(&q, 1000, 5);
        b.on_chain(chain(), 1).unwrap();
        assert!(matches!(
            b.on_chain(ids::filled(31), 1),
            Err(Error::ForeignChain { .. })
        ));
        assert!(matches!(
            b.on_chain(chain(), 2),
            Err(Error::ForeignChain { .. })
        ));
    }

    #[test]
    fn a_proposed_block_is_never_genesis_and_never_empty() {
        let q = quantum();
        assert!(matches!(
            child(&q, 1000, 0).well_formed(),
            Err(Error::Genesis)
        ));
        let empty = Block::new(1000, 5, parent().id(), chain(), 1, Vec::new());
        assert!(matches!(empty.well_formed(), Err(Error::EmptyBlock)));
        child(&q, 1000, 5).well_formed().unwrap();
    }

    #[test]
    fn a_block_sits_on_its_parent_in_height_and_in_time() {
        let q = quantum();
        let p = parent();
        child(&q, 1000, 5).follows(&p, 1000).unwrap();

        assert!(matches!(
            child(&q, 1000, 500).follows(&p, 1000),
            Err(Error::InvalidHeight { .. })
        ));
        assert!(matches!(
            child(&q, 999, 5).follows(&p, 1000),
            Err(Error::TimeBeforeParent { .. })
        ));
        assert!(matches!(
            child(&q, 1000 + MAX_FUTURE_SKEW + 1, 5).follows(&p, 1000),
            Err(Error::TimeTooFarAhead { .. })
        ));
        // Exactly at the allowance is inside it.
        child(&q, 1000 + MAX_FUTURE_SKEW, 5)
            .follows(&p, 1000)
            .unwrap();
    }

    #[test]
    fn every_transaction_in_a_block_is_checked_not_just_the_first() {
        let q = quantum();
        let good = stamped(&q, 1);
        let mut forged = (*stamped(&q, 2)).clone();
        forged.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        let b = Block::new(
            1000,
            5,
            parent().id(),
            chain(),
            1,
            vec![good, Arc::new(forged)],
        );
        assert!(matches!(b.stamps_verify(&q), Err(Error::BlockSignature(_))));
    }

    #[test]
    fn a_block_that_came_off_the_wire_is_checked_the_same_way() {
        // The check runs over the transactions the PARSER produced, so a block
        // received is checked exactly as a block built.
        let q = quantum();
        let b = child(&q, 1000, 5);
        let back = wire::parse_block(b.bytes()).unwrap();
        back.stamps_verify(&q).unwrap();

        let mut forged = (*b.txs[0]).clone();
        forged.stamp.as_mut().unwrap().signature[0] ^= 0xFF;
        let bad = Block::new(1000, 5, parent().id(), chain(), 1, vec![Arc::new(forged)]);
        let back = wire::parse_block(bad.bytes()).unwrap();
        assert!(back.stamps_verify(&q).is_err());
    }

    #[test]
    fn applying_a_block_runs_every_transaction() {
        let q = quantum();
        child(&q, 1000, 5).apply().unwrap();
    }
}
