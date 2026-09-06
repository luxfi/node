// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What a block's transactions must satisfy AS A SEQUENCE: nonces strictly in
//! order per payer, fees affordable against a running per-payer debit, and no
//! two transactions claiming one effect.
//!
//! ONE RULE, TWO POLICIES. Verification runs it over a received block and
//! refuses the whole block on the first transaction that does not fit; building
//! runs it over the mempool and simply leaves out what does not fit. A proposer
//! therefore cannot build a block its own verification would reject, and one
//! unfit transaction can no longer take the rest of the mempool down with it.
//!
//! WHAT IS DELIBERATELY NOT HERE is authorization. That verdict reads state
//! earlier transactions in the same block may change, so a block-time answer
//! can differ from the application-time one — and a block that passes
//! verification on every validator and then fails to apply on every validator
//! halts the chain. Authorization is decided once, at application time, and a
//! transaction that fails it REVERTS: it pays its fee and has no effect. This
//! pass's job is that the block is well formed, authentic, ordered and paid for.

use std::collections::{BTreeMap, BTreeSet};

use crate::error::{Code, Error, Result};
use crate::fee;
use crate::gas;
use crate::id::Account;
use crate::tx::Transaction;
use crate::vm::Vm;

/// The running state one block's transactions are admitted against.
#[derive(Debug, Default)]
pub struct Batch {
    /// The next nonce owed by each payer.
    nonce: BTreeMap<Account, u64>,
    /// The running debit per payer.
    spent: BTreeMap<Account, u64>,
    /// The effects already taken in this block.
    claimed: BTreeSet<[u8; 32]>,
}

impl Batch {
    pub fn new() -> Batch {
        Batch::default()
    }

    /// Test one transaction against the running state and, if it fits, record
    /// its consumption of that state.
    pub fn admit(&mut self, vm: &Vm, tx: &Transaction) -> Result<()> {
        tx.syntactic_verify()?;
        tx.authenticate(vm.chain_id())?;

        // Replay and ordering: a payer's nonces are consecutive from its
        // committed one, counting what has already been taken from it in this
        // block.
        let want = match self.nonce.get(&tx.payer) {
            Some(n) => *n,
            None => vm.nonce_of(&tx.payer)? + 1,
        };
        if tx.nonce != want {
            return Err(Error::new(Code::BadNonce));
        }

        let effect = tx.effect();
        if self.claimed.contains(&effect) {
            return Err(Error::new(Code::DuplicateEffect));
        }

        let gas_used = gas::gas_for(tx)?;
        if gas_used > tx.gas_limit {
            return Err(Error::detail(
                Code::OutOfGas,
                format!("gas {} > limit {}", gas_used, tx.gas_limit),
            ));
        }
        let amount = fee::cost(gas_used, gas::GAS_PRICE)?;
        let balance = fee::balance(vm.store(), &tx.payer)?;
        let already = self.spent.get(&tx.payer).copied().unwrap_or(0);
        let Some(next) = already.checked_add(amount) else {
            return Err(Error::new(Code::InsufficientFunds));
        };
        if balance < next {
            return Err(Error::new(Code::InsufficientFunds));
        }

        self.nonce.insert(tx.payer, want + 1);
        self.spent.insert(tx.payer, next);
        self.claimed.insert(effect);
        Ok(())
    }
}
