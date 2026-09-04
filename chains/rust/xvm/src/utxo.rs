// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The UTXO model: what a transaction spends, what it makes, and the arithmetic
//! that says the two add up.
//!
//! A UTXO is an output of some past transaction that nobody has spent yet. It
//! is named by the transaction that made it and its position in that
//! transaction's output list — hashed together, so two outputs of one
//! transaction are two different things and neither can be spent twice.
//!
//! A transaction is valid when, for every asset, what it consumes is at least
//! what it produces, and the fee is produced out of thin air so it must be
//! consumed too. That is the whole flow rule; the signatures are a separate
//! question, asked by the fx.

use std::cmp::Ordering;
use std::collections::HashMap;

use crate::error::{Error, Result};
use crate::fx::{self, State};
use crate::ids::Id;
use crate::wire::containers;

/// The largest memo a transaction may carry.
pub const MAX_MEMO_SIZE: usize = 256;

/// What a chain is: which network it is on and which chain it is.
///
/// A transaction states both, and both are checked, because a transaction that
/// did not name its chain could be replayed onto another one.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Runtime {
    pub network_id: u32,
    pub chain_id: Id,
}

/// Which past output is being spent.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct UtxoId {
    pub tx_id: Id,
    pub output_index: u32,
    /// False when the UTXO is a record in the database. An imported input is
    /// symbolic: it names a UTXO that lives on the other chain.
    pub symbol: bool,
}

impl UtxoId {
    pub fn new(tx_id: Id, output_index: u32) -> Self {
        UtxoId {
            tx_id,
            output_index,
            symbol: false,
        }
    }

    /// The unique identity of the UTXO this names.
    ///
    /// Not the pair, but a hash of it: one 32-byte key for the store, and no
    /// way to construct two different (tx, index) pairs that collide without
    /// breaking SHA-256.
    pub fn input_id(&self) -> Id {
        self.tx_id.prefix(&[self.output_index as u64])
    }

    pub fn input_source(&self) -> (Id, u32) {
        (self.tx_id, self.output_index)
    }
}

impl PartialOrd for UtxoId {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for UtxoId {
    /// By transaction id, then by output index. The order inputs must be
    /// listed in.
    fn cmp(&self, other: &Self) -> Ordering {
        match self.tx_id.0.cmp(&other.tx_id.0) {
            Ordering::Equal => self.output_index.cmp(&other.output_index),
            o => o,
        }
    }
}

/// Which asset a value refers to.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Asset {
    pub id: Id,
}

impl Asset {
    pub fn verify(&self) -> Result<()> {
        if self.id.is_empty() {
            return Err(Error::EmptyAssetId);
        }
        Ok(())
    }
}

/// An output, bound to the asset it moves.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferableOutput {
    pub asset: Asset,
    pub out: State,
}

impl TransferableOutput {
    pub fn asset_id(&self) -> Id {
        self.asset.id
    }

    /// What this output is worth of its asset.
    pub fn amount(&self) -> u64 {
        self.out.amount()
    }

    pub fn verify(&self) -> Result<()> {
        self.asset.verify()?;
        self.out.verify()
    }

    /// The sort key of the inner fx primitive — its wire envelope, the same
    /// bytes that reach disk and the network. One source of truth for the
    /// ordering, so it cannot drift from what is encoded.
    pub fn inner_bytes(&self) -> Vec<u8> {
        self.out.bytes()
    }
}

/// The order outputs must be listed in: by asset, then by the inner output's
/// wire bytes.
pub fn sort_transferable_outputs(outs: &mut [TransferableOutput]) {
    outs.sort_by(|a, b| match a.asset_id().0.cmp(&b.asset_id().0) {
        Ordering::Equal => a.inner_bytes().cmp(&b.inner_bytes()),
        o => o,
    });
}

pub fn is_sorted_transferable_outputs(outs: &[TransferableOutput]) -> bool {
    outs.windows(2)
        .all(|w| match w[0].asset_id().0.cmp(&w[1].asset_id().0) {
            Ordering::Less => true,
            Ordering::Greater => false,
            Ordering::Equal => w[0].inner_bytes() <= w[1].inner_bytes(),
        })
}

/// An input, bound to the UTXO it spends and the asset that UTXO holds.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferableInput {
    pub utxo_id: UtxoId,
    pub asset: Asset,
    pub input: fx::FxIn,
}

impl TransferableInput {
    pub fn asset_id(&self) -> Id {
        self.asset.id
    }

    pub fn amount(&self) -> u64 {
        self.input.amount()
    }

    pub fn input_id(&self) -> Id {
        self.utxo_id.input_id()
    }

    pub fn verify(&self) -> Result<()> {
        self.asset.verify()?;
        self.input.verify()
    }
}

/// Inputs are ordered by the UTXO they spend, and no two may be the same — a
/// transaction that listed one UTXO twice would be spending it twice.
pub fn is_sorted_and_unique_inputs(ins: &[TransferableInput]) -> bool {
    ins.windows(2).all(|w| w[0].utxo_id < w[1].utxo_id)
}

/// An unspent output, as it is stored and as it crosses between chains.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Utxo {
    pub utxo_id: UtxoId,
    pub asset: Asset,
    pub out: State,
}

impl Utxo {
    pub fn input_id(&self) -> Id {
        self.utxo_id.input_id()
    }

    pub fn asset_id(&self) -> Id {
        self.asset.id
    }

    /// The canonical encoding. The same bytes on shared memory and on disk: a
    /// second encoding would be a second thing for two chains to disagree
    /// about.
    pub fn wire_bytes(&self) -> Vec<u8> {
        containers::new_utxo(
            &self.utxo_id.tx_id,
            self.utxo_id.output_index,
            &self.asset.id,
            &self.out.bytes(),
        )
    }

    pub fn from_wire(b: &[u8]) -> Result<Utxo> {
        let v = containers::wrap_utxo(b)?;
        Ok(Utxo {
            utxo_id: UtxoId::new(v.tx_id(), v.output_index()),
            asset: Asset { id: v.asset_id() },
            out: State::from_envelope(v.output_bytes())?,
        })
    }
}

/// The arithmetic of a transaction: what goes in against what comes out, per
/// asset.
#[derive(Debug, Default)]
pub struct FlowChecker {
    consumed: HashMap<Id, u64>,
    produced: HashMap<Id, u64>,
    overflowed: bool,
}

impl FlowChecker {
    pub fn new() -> Self {
        FlowChecker::default()
    }

    pub fn consume(&mut self, asset: Id, amount: u64) {
        Self::add(&mut self.consumed, &mut self.overflowed, asset, amount);
    }

    pub fn produce(&mut self, asset: Id, amount: u64) {
        Self::add(&mut self.produced, &mut self.overflowed, asset, amount);
    }

    fn add(map: &mut HashMap<Id, u64>, overflowed: &mut bool, asset: Id, amount: u64) {
        let slot = map.entry(asset).or_insert(0);
        match slot.checked_add(amount) {
            Some(v) => *slot = v,
            None => *overflowed = true,
        }
    }

    /// Nothing may be produced that was not consumed.
    pub fn verify(&self) -> Result<()> {
        if self.overflowed {
            return Err(Error::Overflow);
        }
        for (asset, produced) in &self.produced {
            let consumed = self.consumed.get(asset).copied().unwrap_or(0);
            if *produced > consumed {
                return Err(Error::InsufficientFunds);
            }
        }
        Ok(())
    }
}

/// The whole flow rule for a transaction, plus the sortedness of what it lists.
///
/// The fee is PRODUCED: it has to come from somewhere, so it is charged the
/// same way an output is, and a transaction that does not consume enough to
/// cover it fails the same check that catches printing money.
pub fn verify_tx(
    fee_amount: u64,
    fee_asset_id: Id,
    all_ins: &[&[TransferableInput]],
    all_outs: &[&[TransferableOutput]],
) -> Result<()> {
    let mut fc = FlowChecker::new();
    fc.produce(fee_asset_id, fee_amount);

    for outs in all_outs {
        for out in outs.iter() {
            out.verify()?;
            fc.produce(out.asset_id(), out.amount());
        }
        if !is_sorted_transferable_outputs(outs) {
            return Err(Error::OutputsNotSorted);
        }
    }

    for ins in all_ins {
        for in_ in ins.iter() {
            in_.verify()?;
            fc.consume(in_.asset_id(), in_.amount());
        }
        if !is_sorted_and_unique_inputs(ins) {
            return Err(Error::InputsNotSortedUnique);
        }
    }

    fc.verify()
}

/// The spending envelope every X-Chain transaction carries.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct BaseTxFields {
    pub network_id: u32,
    pub blockchain_id: Id,
    pub outs: Vec<TransferableOutput>,
    pub ins: Vec<TransferableInput>,
    pub memo: Vec<u8>,
}

impl BaseTxFields {
    /// The metadata gate: right network, right chain, memo within bounds.
    pub fn verify(&self, rt: &Runtime) -> Result<()> {
        if self.network_id != rt.network_id {
            return Err(Error::WrongNetworkId);
        }
        if self.blockchain_id != rt.chain_id {
            return Err(Error::WrongChainId);
        }
        if self.memo.len() > MAX_MEMO_SIZE {
            return Err(Error::MemoTooLarge(self.memo.len(), MAX_MEMO_SIZE));
        }
        Ok(())
    }

    /// The UTXOs this transaction consumes.
    pub fn input_utxos(&self) -> Vec<UtxoId> {
        self.ins.iter().map(|i| i.utxo_id).collect()
    }

    /// How many credentials it must carry.
    pub fn num_credentials(&self) -> usize {
        self.ins.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::{TransferInput, TransferOutput};
    use crate::fx::{Input, Owners};
    use crate::ids::ShortId;

    fn asset(n: u8) -> Id {
        Id::prefixed_bytes(&[n])
    }

    fn owners() -> Owners {
        Owners::new(1, vec![ShortId::prefixed_bytes(&[1])])
    }

    fn out(asset_n: u8, amt: u64) -> TransferableOutput {
        TransferableOutput {
            asset: Asset { id: asset(asset_n) },
            out: State::Transfer(TransferOutput {
                amt,
                owners: owners(),
            }),
        }
    }

    fn inp(tx: u8, idx: u32, asset_n: u8, amt: u64) -> TransferableInput {
        TransferableInput {
            utxo_id: UtxoId::new(asset(tx), idx),
            asset: Asset { id: asset(asset_n) },
            input: fx::FxIn::Transfer(TransferInput {
                amt,
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        }
    }

    #[test]
    fn a_utxo_id_is_the_hash_of_the_transaction_and_the_index() {
        let u = UtxoId::new(asset(9), 3);
        assert_eq!(u.input_id(), asset(9).prefix(&[3]));
        assert_ne!(u.input_id(), UtxoId::new(asset(9), 4).input_id());
    }

    #[test]
    fn utxo_ids_order_by_transaction_then_index() {
        let a = UtxoId::new(asset(1), 5);
        let b = UtxoId::new(asset(1), 6);
        let c = UtxoId::new(asset(2), 0);
        assert!(a < b);
        assert!(b < c);
    }

    #[test]
    fn an_empty_asset_id_is_not_an_asset() {
        assert_eq!(
            Asset { id: Id::default() }.verify().unwrap_err(),
            Error::EmptyAssetId
        );
        assert!(Asset { id: asset(1) }.verify().is_ok());
    }

    #[test]
    fn outputs_sort_by_asset_then_by_the_inner_bytes() {
        let mut outs = vec![out(2, 1), out(1, 9), out(1, 2)];
        sort_transferable_outputs(&mut outs);
        assert!(is_sorted_transferable_outputs(&outs));
        assert_eq!(outs[0].asset_id(), asset(1));
        assert_eq!(outs[1].asset_id(), asset(1));
        assert_eq!(outs[2].asset_id(), asset(2));
        // The two same-asset outputs are in wire-byte order.
        assert!(outs[0].inner_bytes() <= outs[1].inner_bytes());
    }

    #[test]
    fn inputs_must_be_sorted_and_may_not_repeat_a_utxo() {
        assert!(is_sorted_and_unique_inputs(&[
            inp(1, 0, 1, 1),
            inp(1, 1, 1, 1)
        ]));
        assert!(!is_sorted_and_unique_inputs(&[
            inp(1, 1, 1, 1),
            inp(1, 0, 1, 1)
        ]));
        // The same UTXO twice is a double spend, caught by the ordering rule.
        assert!(!is_sorted_and_unique_inputs(&[
            inp(1, 0, 1, 1),
            inp(1, 0, 1, 1)
        ]));
    }

    #[test]
    fn nothing_may_be_produced_that_was_not_consumed() {
        let mut fc = FlowChecker::new();
        fc.consume(asset(1), 10);
        fc.produce(asset(1), 10);
        assert!(fc.verify().is_ok());

        let mut fc = FlowChecker::new();
        fc.consume(asset(1), 10);
        fc.produce(asset(1), 11);
        assert_eq!(fc.verify().unwrap_err(), Error::InsufficientFunds);

        // Consuming more than is produced is fine — the excess is a fee.
        let mut fc = FlowChecker::new();
        fc.consume(asset(1), 10);
        fc.produce(asset(1), 1);
        assert!(fc.verify().is_ok());
    }

    #[test]
    fn an_asset_produced_from_nowhere_is_caught_even_when_another_balances() {
        let mut fc = FlowChecker::new();
        fc.consume(asset(1), 10);
        fc.produce(asset(1), 10);
        fc.produce(asset(2), 1);
        assert_eq!(fc.verify().unwrap_err(), Error::InsufficientFunds);
    }

    #[test]
    fn a_sum_that_does_not_fit_in_64_bits_is_a_refusal_not_a_wrap() {
        let mut fc = FlowChecker::new();
        fc.consume(asset(1), u64::MAX);
        fc.consume(asset(1), 1);
        assert_eq!(fc.verify().unwrap_err(), Error::Overflow);
    }

    #[test]
    fn the_fee_has_to_be_paid_for_like_any_other_output() {
        let fee_asset = asset(1);
        // Consumes 10, produces 5 + a fee of 5: exactly balanced.
        assert!(verify_tx(5, fee_asset, &[&[inp(1, 0, 1, 10)]], &[&[out(1, 5)]]).is_ok());
        // The same transaction with a fee of 6 no longer adds up.
        assert_eq!(
            verify_tx(6, fee_asset, &[&[inp(1, 0, 1, 10)]], &[&[out(1, 5)]]).unwrap_err(),
            Error::InsufficientFunds
        );
    }

    #[test]
    fn verify_tx_refuses_unsorted_outputs_and_unsorted_inputs() {
        let outs = vec![out(2, 1), out(1, 1)];
        assert_eq!(
            verify_tx(0, asset(1), &[&[inp(1, 0, 1, 10)]], &[&outs]).unwrap_err(),
            Error::OutputsNotSorted
        );
        let ins = vec![inp(1, 1, 1, 5), inp(1, 0, 1, 5)];
        assert_eq!(
            verify_tx(0, asset(1), &[&ins], &[&[out(1, 1)]]).unwrap_err(),
            Error::InputsNotSortedUnique
        );
    }

    #[test]
    fn a_utxo_round_trips_through_its_wire_bytes() {
        let u = Utxo {
            utxo_id: UtxoId::new(asset(3), 2),
            asset: Asset { id: asset(4) },
            out: State::Transfer(TransferOutput {
                amt: 7,
                owners: owners(),
            }),
        };
        assert_eq!(Utxo::from_wire(&u.wire_bytes()).unwrap(), u);
    }

    #[test]
    fn a_transaction_names_its_network_and_its_chain_and_both_are_checked() {
        let rt = Runtime {
            network_id: 10,
            chain_id: asset(5),
        };
        let good = BaseTxFields {
            network_id: 10,
            blockchain_id: asset(5),
            ..Default::default()
        };
        assert!(good.verify(&rt).is_ok());

        let wrong_net = BaseTxFields {
            network_id: 11,
            ..good.clone()
        };
        assert_eq!(wrong_net.verify(&rt).unwrap_err(), Error::WrongNetworkId);

        let wrong_chain = BaseTxFields {
            blockchain_id: asset(6),
            ..good.clone()
        };
        assert_eq!(wrong_chain.verify(&rt).unwrap_err(), Error::WrongChainId);

        let big_memo = BaseTxFields {
            memo: vec![0; MAX_MEMO_SIZE + 1],
            ..good.clone()
        };
        assert_eq!(
            big_memo.verify(&rt).unwrap_err(),
            Error::MemoTooLarge(MAX_MEMO_SIZE + 1, MAX_MEMO_SIZE)
        );

        let at_limit = BaseTxFields {
            memo: vec![0; MAX_MEMO_SIZE],
            ..good
        };
        assert!(at_limit.verify(&rt).is_ok());
    }
}
