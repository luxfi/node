// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Fee settlement: the mechanism, not the schedule.
//!
//! This is `github.com/luxfi/chains/fee` — a debitable balance, a gas meter and
//! the three settlement calls over them. Which operation costs how much gas is
//! the chain's business and lives in [`crate::gas`]; nothing here knows what an
//! operation is.
//!
//! Fees are BURNED, not paid to a proposer: a burn debits the payer and reduces
//! circulating supply, and there is deliberately no account-to-account transfer
//! here for it to be confused with. Every write goes through the block's
//! [`crate::store::Store`] buffer, so the debit commits with the operation it
//! pays for or neither does.

use crate::error::{Code, Error, Result};
use crate::id::Account;
use crate::store::Store;

/// A unit of metered work.
pub type Gas = u64;

/// Where a balance lives: `fee/bal/` ‖ account.
const BAL_PREFIX: &[u8] = b"fee/bal/";
/// Where the cumulative burn lives.
const BURNED_KEY: &[u8] = b"fee/burned";

fn bal_key(acct: &Account) -> Vec<u8> {
    let mut k = Vec::with_capacity(BAL_PREFIX.len() + acct.len());
    k.extend_from_slice(BAL_PREFIX);
    k.extend_from_slice(acct);
    k
}

/// Read a u64, or zero if the key is absent. A stored value of the wrong width
/// is a refusal rather than a zero: a zero here says "this account has spent
/// nothing", which is the reading that lets everything through.
fn read_u64(s: &Store, key: &[u8]) -> Result<u64> {
    match s.get(key) {
        None => Ok(0),
        Some(b) if b.len() == 8 => Ok(u64::from_be_bytes(b.try_into().unwrap())),
        Some(b) => Err(Error::detail(
            Code::InvalidPayload,
            format!("fee ledger: corrupt u64: len {}", b.len()),
        )),
    }
}

fn write_u64(s: &mut Store, key: &[u8], v: u64) {
    s.put(key, &v.to_be_bytes());
}

/// An account's spendable nLUX.
pub fn balance(s: &Store, acct: &Account) -> Result<u64> {
    read_u64(s, &bal_key(acct))
}

/// Add nLUX. Overflow is refused — minting must never wrap to a smaller
/// balance.
pub fn credit(s: &mut Store, acct: &Account, amount: u64) -> Result<()> {
    if amount == 0 {
        return Ok(());
    }
    let key = bal_key(acct);
    let cur = read_u64(s, &key)?;
    let next = cur.checked_add(amount).ok_or_else(|| Error::new(Code::BalanceOverflow))?;
    write_u64(s, &key, next);
    Ok(())
}

/// Debit the payer and reduce circulating supply by the same amount. Leaves
/// state untouched when the payer cannot cover it.
pub fn burn(s: &mut Store, acct: &Account, amount: u64) -> Result<()> {
    if amount == 0 {
        return Ok(());
    }
    let key = bal_key(acct);
    let cur = read_u64(s, &key)?;
    if cur < amount {
        return Err(Error::new(Code::InsufficientFunds));
    }
    let burned = read_u64(s, BURNED_KEY)?;
    // The audit total must never wrap either. Unreachable while burned stays
    // under the genesis supply, and refused rather than trusted to.
    let next = burned.checked_add(amount).ok_or_else(|| Error::new(Code::BalanceOverflow))?;
    write_u64(s, &key, cur - amount);
    write_u64(s, BURNED_KEY, next);
    Ok(())
}

/// Cumulative burned supply.
pub fn burned(s: &Store) -> Result<u64> {
    read_u64(s, BURNED_KEY)
}

/// The read-only affordability check a block runs before it can be accepted.
/// It moves nothing, so verifying a block cannot move funds.
pub fn can_pay(s: &Store, acct: &Account, fee: u64) -> Result<()> {
    if balance(s, acct)? < fee {
        return Err(Error::new(Code::InsufficientFunds));
    }
    Ok(())
}

/// The authoritative settlement a block runs in accept: debit and burn.
pub fn charge(s: &mut Store, acct: &Account, fee: u64) -> Result<()> {
    burn(s, acct, fee)
}

/// Metered gas to nLUX at a per-unit price. Overflow is refused: a fee must
/// never wrap to a smaller number.
pub fn cost(gas_used: Gas, price: Gas) -> Result<u64> {
    if gas_used == 0 || price == 0 {
        return Ok(0);
    }
    gas_used.checked_mul(price).ok_or_else(|| Error::new(Code::BalanceOverflow))
}

/// Gas against a hard limit, exactly like the EVM gas pool. Consuming past the
/// limit denies the operation rather than overdrawing it.
#[derive(Clone, Copy, Debug)]
pub struct Meter {
    limit: Gas,
    remaining: Gas,
}

impl Meter {
    pub fn new(limit: Gas) -> Meter {
        Meter { limit, remaining: limit }
    }

    pub fn consume(&mut self, amount: Gas) -> Result<()> {
        if amount > self.remaining {
            return Err(Error::new(Code::OutOfGas));
        }
        self.remaining -= amount;
        Ok(())
    }

    pub fn remaining(&self) -> Gas {
        self.remaining
    }

    pub fn used(&self) -> Gas {
        self.limit - self.remaining
    }

    pub fn limit(&self) -> Gas {
        self.limit
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn acct(b: u8) -> Account {
        let mut a = [0u8; 20];
        a[0] = b;
        a
    }

    #[test]
    fn a_burn_debits_the_payer_and_raises_the_burned_total() {
        let mut s = Store::new();
        credit(&mut s, &acct(1), 1000).unwrap();
        assert_eq!(balance(&s, &acct(1)).unwrap(), 1000);
        burn(&mut s, &acct(1), 300).unwrap();
        assert_eq!(balance(&s, &acct(1)).unwrap(), 700);
        assert_eq!(burned(&s).unwrap(), 300, "a burn reduces circulating supply");
    }

    #[test]
    fn a_burn_the_payer_cannot_cover_moves_nothing() {
        let mut s = Store::new();
        credit(&mut s, &acct(2), 100).unwrap();
        assert_eq!(burn(&mut s, &acct(2), 101).unwrap_err().code, Code::InsufficientFunds);
        assert_eq!(balance(&s, &acct(2)).unwrap(), 100);
        assert_eq!(burned(&s).unwrap(), 0);
    }

    #[test]
    fn minting_past_the_top_of_the_range_is_refused() {
        let mut s = Store::new();
        credit(&mut s, &acct(3), u64::MAX).unwrap();
        assert_eq!(credit(&mut s, &acct(3), 1).unwrap_err().code, Code::BalanceOverflow);
    }

    #[test]
    fn a_meter_denies_rather_than_overdraws() {
        let mut m = Meter::new(100);
        m.consume(40).unwrap();
        assert_eq!(m.remaining(), 60);
        assert_eq!(m.used(), 40);
        assert_eq!(m.consume(61).unwrap_err().code, Code::OutOfGas);
        assert_eq!(m.remaining(), 60, "an out-of-gas consume must not consume");
    }

    #[test]
    fn a_cost_that_would_wrap_is_refused() {
        assert_eq!(cost(81_000, 1_000).unwrap(), 81_000_000);
        assert_eq!(cost(u64::MAX, 2).unwrap_err().code, Code::BalanceOverflow);
        assert_eq!(cost(0, 1_000).unwrap(), 0);
    }

    #[test]
    fn affordability_is_read_only() {
        let mut s = Store::new();
        credit(&mut s, &acct(4), 1000).unwrap();
        can_pay(&s, &acct(4), 1000).unwrap();
        assert_eq!(can_pay(&s, &acct(4), 1001).unwrap_err().code, Code::InsufficientFunds);
        assert_eq!(balance(&s, &acct(4)).unwrap(), 1000);
    }

    #[test]
    fn a_settlement_rolls_back_with_the_block_that_did_not_commit() {
        let mut s = Store::new();
        credit(&mut s, &acct(5), 1000).unwrap();
        s.commit();
        charge(&mut s, &acct(5), 250).unwrap();
        assert_eq!(balance(&s, &acct(5)).unwrap(), 750);
        s.abort();
        assert_eq!(balance(&s, &acct(5)).unwrap(), 1000);
        assert_eq!(burned(&s).unwrap(), 0);
    }
}
