// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The three passes a transaction goes through, in the order they cost.
//!
//! [`verify_syntactic`] asks whether the transaction is well-formed at all:
//! the right network, sorted lists, a fee that is covered, the right number of
//! credentials, a legible name and symbol. It reads no state and does no
//! cryptography, so it runs first and rejects most nonsense before anything
//! touches a database.
//!
//! [`verify_semantic`] asks whether it is allowed: does each input name a UTXO
//! that exists, does the asset match, does the fx that owns the credential
//! agree the signature spends it, and is that fx one the asset supports. This
//! reads state and recovers public keys.
//!
//! [`execute`] applies it: the inputs stop existing and the outputs start.
//! Nothing here re-checks anything — by the time it runs, both questions have
//! been answered.

use std::collections::BTreeSet;

use crate::error::{Error, Result};
use crate::fx::{self, Family, FxContext};
use crate::ids::Id;
use crate::state::{Chain, ReadOnlyChain};
use crate::txs::{Tx, Unsigned};
use crate::utxo::{verify_tx, Asset, Runtime, Utxo, UtxoId};

const MIN_NAME_LEN: usize = 1;
const MAX_NAME_LEN: usize = 128;
const MIN_SYMBOL_LEN: usize = 1;
const MAX_SYMBOL_LEN: usize = 4;
const MAX_DENOMINATION: u8 = 32;

/// The one operation transaction the semantic pass waives.
///
/// It is on the chain, it was accepted before the rule that would refuse it,
/// and history does not get to be re-decided. The value is the id itself, so
/// this exempts exactly one transaction and nothing that resembles it.
pub const EXEMPT_OPERATION_TX: Id = [
    0x2f, 0x21, 0xd5, 0x74, 0x88, 0x89, 0x2c, 0x35, 0xa3, 0x39, 0xd1, 0xbf, 0x09, 0x6f, 0x8f, 0x33,
    0xe0, 0xe6, 0x01, 0x51, 0xc3, 0xf4, 0x2a, 0x99, 0x23, 0x73, 0x5b, 0x79, 0xbf, 0x4b, 0x2e, 0x68,
];

/// What the chain charges.
#[derive(Clone, Copy, Debug)]
pub struct Config {
    pub tx_fee: u64,
    pub create_asset_tx_fee: u64,
}

impl Default for Config {
    fn default() -> Self {
        Config {
            tx_fee: 1000,
            create_asset_tx_fee: 10000,
        }
    }
}

/// Which network a chain belongs to. A cross-chain transaction has to name a
/// chain on THIS network, and something has to be able to say which.
pub trait Net: Send + Sync {
    fn network_of(&self, chain_id: &Id) -> Result<Id>;
}

/// Where the UTXOs of an import wait: the shared area between two chains.
pub trait SharedMemory: Send + Sync {
    /// The encoded UTXOs the peer chain put there under these keys.
    fn get(&self, peer_chain: &Id, keys: &[Vec<u8>]) -> Result<Vec<Vec<u8>>>;

    /// Hand over what an accepted block asked of the other chains, all of it or
    /// none. A block is accepted once, so this runs once — and it runs with the
    /// same batch that commits the block's own state, which is what makes an
    /// import on one chain and the export on the other one event.
    fn apply(&self, requests: &[(Id, AtomicRequests)]) -> Result<()>;
}

/// Everything the two verification passes need that is not the transaction.
pub struct Backend<'a> {
    pub runtime: Runtime,
    /// The network this chain is on.
    pub net_id: Id,
    pub config: Config,
    pub fee_asset_id: Id,
    pub fx_index: fx::FxIndex,
    /// How many feature extensions the chain runs.
    pub num_fxs: usize,
    pub bootstrapped: bool,
    /// Seconds since the epoch.
    pub now: u64,
    pub net: Option<&'a dyn Net>,
    pub shared_memory: Option<&'a dyn SharedMemory>,
}

impl Backend<'_> {
    pub fn fx_context(&self) -> FxContext {
        FxContext {
            now: self.now,
            bootstrapped: self.bootstrapped,
        }
    }
}

/// A cross-chain transaction must name a chain that is not this one and is on
/// this network.
pub fn verify_same_net(backend: &Backend<'_>, peer_chain: &Id) -> Result<()> {
    if *peer_chain == backend.runtime.chain_id {
        return Err(Error::SameChainId);
    }
    let net = backend.net.ok_or(Error::UnknownChain)?;
    let peer_net = net.network_of(peer_chain)?;
    if peer_net != backend.net_id {
        return Err(Error::MismatchedNetIds);
    }
    Ok(())
}

// ------------------------------------------------------------- syntactic --

/// Is this a well-formed transaction?
pub fn verify_syntactic(backend: &Backend<'_>, tx: &Tx) -> Result<()> {
    match &tx.unsigned {
        Unsigned::Base(t) => {
            t.base.verify(&backend.runtime)?;
            verify_tx(
                backend.config.tx_fee,
                backend.fee_asset_id,
                &[&t.base.ins],
                &[&t.base.outs],
            )?;
            verify_creds(tx, t.base.ins.len())
        }

        Unsigned::CreateAsset(t) => {
            // The name and symbol are read by people, so they are held to what
            // a person can read: printable ASCII, no leading or trailing
            // space, upper-case for the symbol.
            if t.name.len() < MIN_NAME_LEN {
                return Err(Error::NameTooShort);
            }
            if t.name.len() > MAX_NAME_LEN {
                return Err(Error::NameTooLong);
            }
            if t.symbol.len() < MIN_SYMBOL_LEN {
                return Err(Error::SymbolTooShort);
            }
            if t.symbol.len() > MAX_SYMBOL_LEN {
                return Err(Error::SymbolTooLong);
            }
            if t.states.is_empty() {
                return Err(Error::NoFxs);
            }
            if t.denomination > MAX_DENOMINATION {
                return Err(Error::DenominationTooLarge);
            }
            if t.name.trim() != t.name {
                return Err(Error::UnexpectedWhitespace);
            }
            for r in t.name.chars() {
                if !r.is_ascii() || !(r.is_alphanumeric() || r == ' ') {
                    return Err(Error::IllegalNameCharacter);
                }
            }
            for r in t.symbol.chars() {
                if !r.is_ascii() || !r.is_uppercase() {
                    return Err(Error::IllegalSymbolCharacter);
                }
            }

            t.base.verify(&backend.runtime)?;
            verify_tx(
                backend.config.create_asset_tx_fee,
                backend.fee_asset_id,
                &[&t.base.ins],
                &[&t.base.outs],
            )?;

            for state in &t.states {
                state.verify(backend.num_fxs)?;
            }
            if !t.states.windows(2).all(|w| w[0].fx_index < w[1].fx_index) {
                return Err(Error::InitialStatesNotSortedUnique);
            }
            verify_creds(tx, t.base.ins.len())
        }

        Unsigned::Operation(t) => {
            if t.ops.is_empty() {
                return Err(Error::NoOperations);
            }
            t.base.verify(&backend.runtime)?;
            verify_tx(
                backend.config.tx_fee,
                backend.fee_asset_id,
                &[&t.base.ins],
                &[&t.base.outs],
            )?;

            // An operation may not reach for a UTXO the envelope already
            // spends, or for one another operation already took.
            let mut inputs: BTreeSet<Id> = t.base.ins.iter().map(|i| i.input_id()).collect();
            for op in &t.ops {
                op.verify()?;
                for utxo_id in &op.utxo_ids {
                    if !inputs.insert(utxo_id.input_id()) {
                        return Err(Error::DoubleSpend);
                    }
                }
            }
            if !super::is_sorted_and_unique_operations(&t.ops) {
                return Err(Error::OperationsNotSortedUnique);
            }
            verify_creds(tx, t.base.ins.len() + t.ops.len())
        }

        Unsigned::Import(t) => {
            if t.imported_ins.is_empty() {
                return Err(Error::NoImportInputs);
            }
            t.base.verify(&backend.runtime)?;
            verify_tx(
                backend.config.tx_fee,
                backend.fee_asset_id,
                &[&t.base.ins, &t.imported_ins],
                &[&t.base.outs],
            )?;
            verify_creds(tx, t.base.ins.len() + t.imported_ins.len())
        }

        Unsigned::Export(t) => {
            if t.exported_outs.is_empty() {
                return Err(Error::NoExportOutputs);
            }
            t.base.verify(&backend.runtime)?;
            verify_tx(
                backend.config.tx_fee,
                backend.fee_asset_id,
                &[&t.base.ins],
                &[&t.base.outs, &t.exported_outs],
            )?;
            verify_creds(tx, t.base.ins.len())
        }
    }
}

fn verify_creds(tx: &Tx, want: usize) -> Result<()> {
    for cred in &tx.creds {
        cred.verify()?;
    }
    if tx.creds.len() != want {
        return Err(Error::WrongNumberOfCredentials(tx.creds.len(), want));
    }
    Ok(())
}

// -------------------------------------------------------------- semantic --

/// Is this transaction allowed against the state it would run on?
pub fn verify_semantic(backend: &Backend<'_>, state: &dyn ReadOnlyChain, tx: &Tx) -> Result<()> {
    verify_base_semantic(backend, state, tx)?;

    match &tx.unsigned {
        Unsigned::Base(_) | Unsigned::CreateAsset(_) => Ok(()),

        Unsigned::Operation(t) => {
            // History is not re-decided: one transaction predates the rule
            // that would refuse it.
            if !backend.bootstrapped || tx.id() == EXEMPT_OPERATION_TX {
                return Ok(());
            }
            let offset = t.base.ins.len();
            for (i, op) in t.ops.iter().enumerate() {
                let cred = &tx.creds[i + offset];
                verify_operation(backend, state, tx, op, cred)?;
            }
            Ok(())
        }

        Unsigned::Import(t) => {
            if !backend.bootstrapped {
                return Ok(());
            }
            verify_same_net(backend, &t.source_chain)?;

            let keys: Vec<Vec<u8>> = t
                .imported_ins
                .iter()
                .map(|i| i.input_id().to_vec())
                .collect();
            let sm = backend.shared_memory.ok_or(Error::NotFound)?;
            let raw = sm.get(&t.source_chain, &keys)?;

            let offset = t.base.ins.len();
            for (i, in_) in t.imported_ins.iter().enumerate() {
                let bytes = raw.get(i).ok_or(Error::NotFound)?;
                let utxo = Utxo::from_wire(bytes)?;
                let cred = &tx.creds[i + offset];
                verify_transfer_of_utxo(backend, state, tx, in_, cred, &utxo)?;
            }
            Ok(())
        }

        Unsigned::Export(t) => {
            if backend.bootstrapped {
                verify_same_net(backend, &t.destination_chain)?;
            }
            for out in &t.exported_outs {
                let fx_index = get_fx_index(backend, out.out.family())?;
                verify_fx_usage(state, fx_index, &out.asset_id())?;
            }
            Ok(())
        }
    }
}

/// The part every transaction shares: every input is a real, matching,
/// properly-signed spend, and every output uses an fx its asset supports.
fn verify_base_semantic(backend: &Backend<'_>, state: &dyn ReadOnlyChain, tx: &Tx) -> Result<()> {
    let base = tx.unsigned.base();
    for (i, in_) in base.ins.iter().enumerate() {
        // The credential count was settled by the syntactic pass.
        let cred = &tx.creds[i];
        let utxo = state.get_utxo(&in_.input_id())?;
        verify_transfer_of_utxo(backend, state, tx, in_, cred, &utxo)?;
    }
    for out in &base.outs {
        let fx_index = get_fx_index(backend, out.out.family())?;
        verify_fx_usage(state, fx_index, &out.asset_id())?;
    }
    Ok(())
}

fn verify_transfer_of_utxo(
    backend: &Backend<'_>,
    state: &dyn ReadOnlyChain,
    tx: &Tx,
    in_: &crate::utxo::TransferableInput,
    cred: &fx::Cred,
    utxo: &Utxo,
) -> Result<()> {
    if utxo.asset_id() != in_.asset_id() {
        return Err(Error::AssetIdMismatch);
    }
    // The fx that answers is the one the CREDENTIAL names — not the one the
    // output happens to be, so a credential cannot borrow another fx's rule.
    let fx_index = get_fx_index(backend, cred.family)?;
    verify_fx_usage(state, fx_index, &in_.asset_id())?;
    fx::verify_transfer(
        cred.family,
        &backend.fx_context(),
        tx.unsigned_bytes(),
        &in_.input,
        cred,
        &utxo.out,
    )
}

fn verify_operation(
    backend: &Backend<'_>,
    state: &dyn ReadOnlyChain,
    tx: &Tx,
    op: &super::Operation,
    cred: &fx::Cred,
) -> Result<()> {
    let op_asset_id = op.asset_id();
    let mut utxos = Vec::with_capacity(op.utxo_ids.len());
    for utxo_id in &op.utxo_ids {
        let utxo = state.get_utxo(&utxo_id.input_id())?;
        if utxo.asset_id() != op_asset_id {
            return Err(Error::AssetIdMismatch);
        }
        utxos.push(utxo.out.clone());
    }

    let fx_index = get_fx_index(backend, op.op.family())?;
    verify_fx_usage(state, fx_index, &op_asset_id)?;
    fx::verify_operation(
        op.op.family(),
        &backend.fx_context(),
        tx.unsigned_bytes(),
        &op.op,
        cred,
        &utxos,
    )
}

fn get_fx_index(backend: &Backend<'_>, family: Family) -> Result<usize> {
    backend.fx_index.get_family(family).ok_or(Error::UnknownFx)
}

/// An asset says which fx it supports; using one it does not is refused.
fn verify_fx_usage(state: &dyn ReadOnlyChain, fx_index: usize, asset_id: &Id) -> Result<()> {
    let tx = state.get_tx(asset_id)?;
    let create = match &tx.unsigned {
        Unsigned::CreateAsset(t) => t,
        _ => return Err(Error::NotAnAsset),
    };
    for state in &create.states {
        if state.fx_index as usize == fx_index {
            return Ok(());
        }
    }
    Err(Error::IncompatibleFx)
}

// --------------------------------------------------------------- executing --

/// One UTXO handed to another chain.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AtomicElement {
    /// What the other chain will look it up by: the UTXO's own id.
    pub key: Vec<u8>,
    /// The UTXO, in the encoding both chains read — the same bytes that go on
    /// disk here, so there is nothing for the two sides to disagree about.
    pub value: Vec<u8>,
    /// The addresses it is indexed by, so a wallet on the other chain can find
    /// what is waiting for it.
    pub traits: Vec<Vec<u8>>,
}

/// What a transaction asks another chain to do.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct AtomicRequests {
    /// UTXOs to hand the other chain.
    pub puts: Vec<AtomicElement>,
    /// UTXOs to take from the other chain, by key.
    pub removes: Vec<Vec<u8>>,
}

/// What executing a transaction produced beyond the state changes.
#[derive(Clone, Debug, Default)]
pub struct Effects {
    /// The UTXOs an import claimed, so a block can refuse to claim one twice.
    pub inputs: BTreeSet<Id>,
    /// Keyed by the other chain.
    pub atomic_requests: Vec<(Id, AtomicRequests)>,
}

/// Apply the transaction: the inputs stop existing, the outputs start.
pub fn execute(state: &mut dyn Chain, tx: &Tx) -> Result<Effects> {
    let tx_id = tx.id();
    let base = tx.unsigned.base();

    for in_ in &base.ins {
        state.delete_utxo(&in_.input_id());
    }
    for (index, out) in base.outs.iter().enumerate() {
        state.add_utxo(Utxo {
            utxo_id: UtxoId::new(tx_id, index as u32),
            asset: Asset { id: out.asset_id() },
            out: out.out.clone(),
        });
    }

    let mut effects = Effects::default();
    let mut index = base.outs.len();

    match &tx.unsigned {
        Unsigned::Base(_) => {}

        Unsigned::CreateAsset(t) => {
            // The asset is named after the transaction that created it.
            for state_group in &t.states {
                for out in &state_group.outs {
                    state.add_utxo(Utxo {
                        utxo_id: UtxoId::new(tx_id, index as u32),
                        asset: Asset { id: tx_id },
                        out: out.clone(),
                    });
                    index += 1;
                }
            }
        }

        Unsigned::Operation(t) => {
            for op in &t.ops {
                for utxo_id in &op.utxo_ids {
                    state.delete_utxo(&utxo_id.input_id());
                }
                let asset = op.asset_id();
                for out in op.op.outs() {
                    state.add_utxo(Utxo {
                        utxo_id: UtxoId::new(tx_id, index as u32),
                        asset: Asset { id: asset },
                        out,
                    });
                    index += 1;
                }
            }
        }

        Unsigned::Import(t) => {
            let mut removes = Vec::with_capacity(t.imported_ins.len());
            for in_ in &t.imported_ins {
                let utxo_id = in_.input_id();
                effects.inputs.insert(utxo_id);
                removes.push(utxo_id.to_vec());
            }
            effects.atomic_requests.push((
                t.source_chain,
                AtomicRequests {
                    puts: Vec::new(),
                    removes,
                },
            ));
        }

        Unsigned::Export(t) => {
            let mut puts = Vec::with_capacity(t.exported_outs.len());
            for out in &t.exported_outs {
                let utxo = Utxo {
                    utxo_id: UtxoId::new(tx_id, index as u32),
                    asset: Asset { id: out.asset_id() },
                    out: out.out.clone(),
                };
                index += 1;
                // The same encoding on shared memory and on disk.
                let bytes = utxo.wire_bytes();
                let key = utxo.input_id().to_vec();
                puts.push(AtomicElement {
                    key,
                    value: bytes,
                    traits: out.out.addresses(),
                });
            }
            effects.atomic_requests.push((
                t.destination_chain,
                AtomicRequests {
                    puts,
                    removes: Vec::new(),
                },
            ));
        }
    }

    Ok(effects)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::{
        address_of, MintOperation, MintOutput, TransferInput, TransferOutput,
    };
    use crate::fx::{property, Cred, Credential, Input, Owners, State};
    use crate::hash::sha256;
    use crate::ids;
    use crate::ids::ShortId;
    use crate::state::Store;
    use crate::txs::{
        BaseTx, CreateAssetTx, ExportTx, ImportTx, InitialState, Operation, OperationTx,
    };
    use crate::utxo::{BaseTxFields, TransferableInput, TransferableOutput};

    const KEY: [u8; 32] = [11u8; 32];
    const OTHER_KEY: [u8; 32] = [12u8; 32];

    fn asset(n: u8) -> Id {
        ids::prefixed(&[n])
    }

    fn chain() -> Id {
        asset(5)
    }

    fn me() -> ShortId {
        address_of(&KEY).unwrap()
    }

    fn owners() -> Owners {
        Owners::new(1, vec![me()])
    }

    struct OneNet(Id);
    impl Net for OneNet {
        fn network_of(&self, _chain_id: &Id) -> Result<Id> {
            Ok(self.0)
        }
    }

    struct NoMemory;
    impl SharedMemory for NoMemory {
        fn get(&self, _: &Id, _: &[Vec<u8>]) -> Result<Vec<Vec<u8>>> {
            Ok(Vec::new())
        }
        fn apply(&self, _: &[(Id, AtomicRequests)]) -> Result<()> {
            Ok(())
        }
    }

    fn backend<'a>(net: &'a OneNet, sm: &'a dyn SharedMemory) -> Backend<'a> {
        Backend {
            runtime: Runtime {
                network_id: 10,
                chain_id: chain(),
            },
            net_id: asset(0xAB),
            config: Config {
                tx_fee: 0,
                create_asset_tx_fee: 0,
            },
            fee_asset_id: asset(1),
            fx_index: fx::FxIndex::standard(),
            num_fxs: 3,
            bootstrapped: true,
            now: 1000,
            net: Some(net),
            shared_memory: Some(sm),
        }
    }

    fn base_fields(amt_in: u64, amt_out: u64) -> BaseTxFields {
        BaseTxFields {
            network_id: 10,
            blockchain_id: chain(),
            outs: vec![TransferableOutput {
                asset: Asset { id: asset(1) },
                out: State::Transfer(TransferOutput {
                    amt: amt_out,
                    owners: owners(),
                }),
            }],
            ins: vec![TransferableInput {
                utxo_id: UtxoId::new(asset(2), 0),
                asset: Asset { id: asset(1) },
                input: fx::FxIn::Transfer(TransferInput {
                    amt: amt_in,
                    input: Input {
                        sig_indices: vec![0],
                    },
                }),
            }],
            memo: vec![],
        }
    }

    fn signed(u: Unsigned, n: usize) -> Tx {
        let mut tx = Tx::new(u);
        let signers: Vec<Vec<[u8; 32]>> = (0..n).map(|_| vec![KEY]).collect();
        tx.sign(Family::Secp256k1, &signers).unwrap();
        tx
    }

    // ---- syntactic ----

    #[test]
    fn a_well_formed_transfer_passes_the_syntactic_pass() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let tx = signed(
            Unsigned::Base(BaseTx {
                base: base_fields(10, 10),
            }),
            1,
        );
        assert!(verify_syntactic(&b, &tx).is_ok());
    }

    #[test]
    fn the_syntactic_pass_refuses_the_wrong_network_and_the_wrong_chain() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let mut f = base_fields(10, 10);
        f.network_id = 11;
        assert_eq!(
            verify_syntactic(&b, &signed(Unsigned::Base(BaseTx { base: f }), 1)).unwrap_err(),
            Error::WrongNetworkId
        );
        let mut f = base_fields(10, 10);
        f.blockchain_id = asset(6);
        assert_eq!(
            verify_syntactic(&b, &signed(Unsigned::Base(BaseTx { base: f }), 1)).unwrap_err(),
            Error::WrongChainId
        );
    }

    #[test]
    fn a_transfer_that_produces_more_than_it_consumes_is_refused() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let tx = signed(
            Unsigned::Base(BaseTx {
                base: base_fields(10, 11),
            }),
            1,
        );
        assert_eq!(
            verify_syntactic(&b, &tx).unwrap_err(),
            Error::InsufficientFunds
        );
    }

    #[test]
    fn the_fee_is_charged_by_the_syntactic_pass() {
        let net = OneNet(asset(0xAB));
        let mut b = backend(&net, &NoMemory);
        b.config.tx_fee = 5;
        // 10 in, 5 out, 5 fee: exact.
        assert!(verify_syntactic(
            &b,
            &signed(
                Unsigned::Base(BaseTx {
                    base: base_fields(10, 5)
                }),
                1
            )
        )
        .is_ok());
        // 10 in, 6 out, 5 fee: short.
        assert_eq!(
            verify_syntactic(
                &b,
                &signed(
                    Unsigned::Base(BaseTx {
                        base: base_fields(10, 6)
                    }),
                    1
                )
            )
            .unwrap_err(),
            Error::InsufficientFunds
        );
    }

    #[test]
    fn a_transaction_must_carry_one_credential_per_thing_it_authorises() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let tx = signed(
            Unsigned::Base(BaseTx {
                base: base_fields(10, 10),
            }),
            2,
        );
        assert_eq!(
            verify_syntactic(&b, &tx).unwrap_err(),
            Error::WrongNumberOfCredentials(2, 1)
        );
        let tx = signed(
            Unsigned::Base(BaseTx {
                base: base_fields(10, 10),
            }),
            0,
        );
        assert_eq!(
            verify_syntactic(&b, &tx).unwrap_err(),
            Error::WrongNumberOfCredentials(0, 1)
        );
    }

    fn create_asset(name: &str, symbol: &str, denom: u8, states: Vec<InitialState>) -> Unsigned {
        Unsigned::CreateAsset(CreateAssetTx {
            base: base_fields(10, 10),
            name: name.into(),
            symbol: symbol.into(),
            denomination: denom,
            states,
        })
    }

    fn one_state() -> Vec<InitialState> {
        vec![InitialState {
            fx_index: 0,
            outs: vec![State::Mint(MintOutput { owners: owners() })],
        }]
    }

    #[test]
    fn a_new_asset_is_held_to_a_name_a_person_can_read() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        for (u, want) in [
            (create_asset("", "TST", 0, one_state()), Error::NameTooShort),
            (
                create_asset(&"a".repeat(129), "TST", 0, one_state()),
                Error::NameTooLong,
            ),
            (
                create_asset("Name", "", 0, one_state()),
                Error::SymbolTooShort,
            ),
            (
                create_asset("Name", "TOOLONG", 0, one_state()),
                Error::SymbolTooLong,
            ),
            (create_asset("Name", "TST", 0, vec![]), Error::NoFxs),
            (
                create_asset("Name", "TST", 33, one_state()),
                Error::DenominationTooLarge,
            ),
            (
                create_asset(" Name", "TST", 0, one_state()),
                Error::UnexpectedWhitespace,
            ),
            (
                create_asset("Name ", "TST", 0, one_state()),
                Error::UnexpectedWhitespace,
            ),
            (
                create_asset("Na\u{00e9}me", "TST", 0, one_state()),
                Error::IllegalNameCharacter,
            ),
            (
                create_asset("Na\tme", "TST", 0, one_state()),
                Error::IllegalNameCharacter,
            ),
            (
                create_asset("Name", "tst", 0, one_state()),
                Error::IllegalSymbolCharacter,
            ),
        ] {
            assert_eq!(verify_syntactic(&b, &signed(u, 1)).unwrap_err(), want);
        }
        // A name with an inner space, digits, and a 32 denomination is fine.
        assert!(verify_syntactic(
            &b,
            &signed(create_asset("My Asset 2", "MYA", 32, one_state()), 1)
        )
        .is_ok());
    }

    #[test]
    fn an_assets_initial_states_are_listed_once_each_in_order() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let s = |i: u32| InitialState {
            fx_index: i,
            outs: vec![],
        };
        assert_eq!(
            verify_syntactic(&b, &signed(create_asset("N", "N", 0, vec![s(1), s(0)]), 1))
                .unwrap_err(),
            Error::InitialStatesNotSortedUnique
        );
        assert_eq!(
            verify_syntactic(&b, &signed(create_asset("N", "N", 0, vec![s(0), s(0)]), 1))
                .unwrap_err(),
            Error::InitialStatesNotSortedUnique
        );
        assert!(
            verify_syntactic(&b, &signed(create_asset("N", "N", 0, vec![s(0), s(1)]), 1)).is_ok()
        );
    }

    #[test]
    fn an_asset_may_not_name_an_fx_the_chain_does_not_run() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let u = create_asset(
            "N",
            "N",
            0,
            vec![InitialState {
                fx_index: 3,
                outs: vec![],
            }],
        );
        assert_eq!(
            verify_syntactic(&b, &signed(u, 1)).unwrap_err(),
            Error::UnknownFx
        );
    }

    fn burn_op(idx: u32) -> Operation {
        Operation {
            asset: Asset { id: asset(1) },
            utxo_ids: vec![UtxoId::new(asset(3), idx)],
            op: fx::Op::PropertyBurn(property::BurnOperation {
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        }
    }

    #[test]
    fn an_operation_transaction_must_have_an_operation() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let u = Unsigned::Operation(OperationTx {
            base: base_fields(10, 10),
            ops: vec![],
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 1)).unwrap_err(),
            Error::NoOperations
        );
    }

    #[test]
    fn two_operations_may_not_reach_for_the_same_utxo() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let mut ops = vec![burn_op(0), burn_op(0)];
        super::super::sort_operations(&mut ops);
        let u = Unsigned::Operation(OperationTx {
            base: base_fields(10, 10),
            ops,
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 3)).unwrap_err(),
            Error::DoubleSpend
        );
    }

    #[test]
    fn an_operation_may_not_reach_for_a_utxo_the_envelope_already_spends() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let fields = base_fields(10, 10);
        // Point the operation at the very UTXO the envelope's input spends.
        let spent = fields.ins[0].utxo_id;
        let op = Operation {
            asset: Asset { id: asset(1) },
            utxo_ids: vec![spent],
            op: fx::Op::PropertyBurn(property::BurnOperation {
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        };
        let u = Unsigned::Operation(OperationTx {
            base: fields,
            ops: vec![op],
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 2)).unwrap_err(),
            Error::DoubleSpend
        );
    }

    #[test]
    fn operations_are_listed_in_canonical_order() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let mut ops = vec![burn_op(1), burn_op(2)];
        super::super::sort_operations(&mut ops);
        ops.reverse();
        let u = Unsigned::Operation(OperationTx {
            base: base_fields(10, 10),
            ops,
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 3)).unwrap_err(),
            Error::OperationsNotSortedUnique
        );
    }

    #[test]
    fn an_import_must_import_something_and_an_export_must_export_something() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let u = Unsigned::Import(ImportTx {
            base: base_fields(10, 10),
            source_chain: asset(9),
            imported_ins: vec![],
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 1)).unwrap_err(),
            Error::NoImportInputs
        );

        let u = Unsigned::Export(ExportTx {
            base: base_fields(10, 10),
            destination_chain: asset(9),
            exported_outs: vec![],
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 1)).unwrap_err(),
            Error::NoExportOutputs
        );
    }

    #[test]
    fn an_import_counts_its_imported_inputs_in_the_flow_and_in_the_credentials() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        // 10 in the envelope, 5 imported, 15 out: exactly balanced.
        let mut fields = base_fields(10, 15);
        fields.memo = vec![];
        let u = Unsigned::Import(ImportTx {
            base: fields,
            source_chain: asset(9),
            imported_ins: vec![TransferableInput {
                utxo_id: UtxoId::new(asset(4), 0),
                asset: Asset { id: asset(1) },
                input: fx::FxIn::Transfer(TransferInput {
                    amt: 5,
                    input: Input {
                        sig_indices: vec![0],
                    },
                }),
            }],
        });
        assert!(verify_syntactic(&b, &signed(u.clone(), 2)).is_ok());
        assert_eq!(
            verify_syntactic(&b, &signed(u, 1)).unwrap_err(),
            Error::WrongNumberOfCredentials(1, 2)
        );
    }

    #[test]
    fn an_export_counts_what_leaves_as_produced_but_needs_no_extra_credential() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        // 10 in, 4 out here and 6 out there.
        let u = Unsigned::Export(ExportTx {
            base: base_fields(10, 4),
            destination_chain: asset(9),
            exported_outs: vec![TransferableOutput {
                asset: Asset { id: asset(1) },
                out: State::Transfer(TransferOutput {
                    amt: 6,
                    owners: owners(),
                }),
            }],
        });
        assert!(verify_syntactic(&b, &signed(u, 1)).is_ok());

        // One more than was consumed is refused.
        let u = Unsigned::Export(ExportTx {
            base: base_fields(10, 5),
            destination_chain: asset(9),
            exported_outs: vec![TransferableOutput {
                asset: Asset { id: asset(1) },
                out: State::Transfer(TransferOutput {
                    amt: 6,
                    owners: owners(),
                }),
            }],
        });
        assert_eq!(
            verify_syntactic(&b, &signed(u, 1)).unwrap_err(),
            Error::InsufficientFunds
        );
    }

    // ---- semantic ----

    /// A store holding one asset (a create-asset transaction under its own id)
    /// and one spendable UTXO of it.
    fn store_with_asset_and_utxo(out: State, utxo_id: UtxoId, asset_id: Id) -> Store {
        let mut s = Store::new();
        let create = Tx::new(create_asset("Asset", "AST", 0, one_state()));
        // The asset's id is the id of the transaction that made it, so index
        // the store by the id we want the asset to have.
        let mut store_tx = create;
        store_tx = Tx::parse(store_tx.bytes()).unwrap();
        s.add_tx_at(asset_id, store_tx);
        s.add_utxo(Utxo {
            utxo_id,
            asset: Asset { id: asset_id },
            out,
        });
        s
    }

    #[test]
    fn a_spend_of_a_utxo_that_is_not_there_is_refused() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let s = Store::new();
        let tx = signed(
            Unsigned::Base(BaseTx {
                base: base_fields(10, 10),
            }),
            1,
        );
        assert_eq!(verify_semantic(&b, &s, &tx).unwrap_err(), Error::NotFound);
    }

    #[test]
    fn an_input_whose_asset_differs_from_the_utxo_is_refused() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let fields = base_fields(10, 10);
        let utxo_id = fields.ins[0].utxo_id;
        let s = store_with_asset_and_utxo(
            State::Transfer(TransferOutput {
                amt: 10,
                owners: owners(),
            }),
            utxo_id,
            asset(7), // a different asset than the input names
        );
        let tx = signed(Unsigned::Base(BaseTx { base: fields }), 1);
        assert_eq!(
            verify_semantic(&b, &s, &tx).unwrap_err(),
            Error::AssetIdMismatch
        );
    }

    #[test]
    fn a_spend_signed_by_the_owner_passes_and_by_a_stranger_does_not() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let fields = base_fields(10, 10);
        let utxo_id = fields.ins[0].utxo_id;
        let s = store_with_asset_and_utxo(
            State::Transfer(TransferOutput {
                amt: 10,
                owners: owners(),
            }),
            utxo_id,
            asset(1),
        );

        let tx = signed(
            Unsigned::Base(BaseTx {
                base: fields.clone(),
            }),
            1,
        );
        assert!(verify_semantic(&b, &s, &tx).is_ok());

        let mut stranger = Tx::new(Unsigned::Base(BaseTx { base: fields }));
        stranger
            .sign(Family::Secp256k1, &[vec![OTHER_KEY]])
            .unwrap();
        assert_eq!(
            verify_semantic(&b, &s, &stranger).unwrap_err(),
            Error::WrongSig
        );
    }

    #[test]
    fn an_asset_that_is_not_an_asset_is_refused() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let fields = base_fields(10, 10);
        let utxo_id = fields.ins[0].utxo_id;
        let mut s = Store::new();
        // Index a plain transfer under the asset id: it is not a create-asset.
        s.add_tx_at(
            asset(1),
            signed(
                Unsigned::Base(BaseTx {
                    base: base_fields(1, 1),
                }),
                1,
            ),
        );
        s.add_utxo(Utxo {
            utxo_id,
            asset: Asset { id: asset(1) },
            out: State::Transfer(TransferOutput {
                amt: 10,
                owners: owners(),
            }),
        });
        let tx = signed(Unsigned::Base(BaseTx { base: fields }), 1);
        assert_eq!(verify_semantic(&b, &s, &tx).unwrap_err(), Error::NotAnAsset);
    }

    #[test]
    fn an_fx_the_asset_does_not_support_is_refused() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        let fields = base_fields(10, 10);
        let utxo_id = fields.ins[0].utxo_id;
        let mut s = Store::new();
        // An asset that supports only fx 2 (property).
        s.add_tx_at(
            asset(1),
            Tx::new(create_asset(
                "A",
                "A",
                0,
                vec![InitialState {
                    fx_index: 2,
                    outs: vec![],
                }],
            )),
        );
        s.add_utxo(Utxo {
            utxo_id,
            asset: Asset { id: asset(1) },
            out: State::Transfer(TransferOutput {
                amt: 10,
                owners: owners(),
            }),
        });
        let tx = signed(Unsigned::Base(BaseTx { base: fields }), 1);
        assert_eq!(
            verify_semantic(&b, &s, &tx).unwrap_err(),
            Error::IncompatibleFx
        );
    }

    #[test]
    fn a_cross_chain_transaction_may_not_name_this_chain() {
        let net = OneNet(asset(0xAB));
        let b = backend(&net, &NoMemory);
        assert_eq!(
            verify_same_net(&b, &chain()).unwrap_err(),
            Error::SameChainId
        );
        assert!(verify_same_net(&b, &asset(9)).is_ok());
    }

    #[test]
    fn a_chain_on_another_network_is_refused() {
        let net = OneNet(asset(0xCD));
        let b = backend(&net, &NoMemory);
        assert_eq!(
            verify_same_net(&b, &asset(9)).unwrap_err(),
            Error::MismatchedNetIds
        );
    }

    #[test]
    fn an_operation_is_not_verified_while_the_node_is_still_catching_up() {
        let net = OneNet(asset(0xAB));
        let mut b = backend(&net, &NoMemory);
        b.bootstrapped = false;
        let s = Store::new();
        let u = Unsigned::Operation(OperationTx {
            base: BaseTxFields {
                network_id: 10,
                blockchain_id: chain(),
                outs: vec![],
                ins: vec![],
                memo: vec![],
            },
            ops: vec![burn_op(0)],
        });
        // No state at all, and it still passes: those blocks were decided.
        assert!(verify_semantic(&b, &s, &signed(u, 1)).is_ok());
    }

    // ---- executing ----

    #[test]
    fn executing_a_transfer_spends_the_inputs_and_creates_the_outputs() {
        let mut s = Store::new();
        let fields = base_fields(10, 10);
        let spent = fields.ins[0].utxo_id;
        s.add_utxo(Utxo {
            utxo_id: spent,
            asset: Asset { id: asset(1) },
            out: State::Transfer(TransferOutput {
                amt: 10,
                owners: owners(),
            }),
        });
        let tx = signed(Unsigned::Base(BaseTx { base: fields }), 1);

        execute(&mut s, &tx).unwrap();
        assert_eq!(s.get_utxo(&spent.input_id()).unwrap_err(), Error::NotFound);
        let made = s.get_utxo(&UtxoId::new(tx.id(), 0).input_id()).unwrap();
        assert_eq!(made.asset.id, asset(1));
        assert_eq!(made.out.amount(), 10);
    }

    #[test]
    fn executing_a_create_asset_mints_an_asset_named_after_the_transaction() {
        let mut s = Store::new();
        let tx = signed(create_asset("A", "A", 0, one_state()), 1);
        execute(&mut s, &tx).unwrap();
        // Index 0 is the envelope's own output; index 1 is the initial state's.
        let opened = s.get_utxo(&UtxoId::new(tx.id(), 1).input_id()).unwrap();
        assert_eq!(opened.asset.id, tx.id());
        assert!(matches!(opened.out, State::Mint(_)));
    }

    #[test]
    fn executing_an_operation_spends_what_it_names_and_creates_what_it_makes() {
        let mut s = Store::new();
        let op_utxo = UtxoId::new(asset(3), 0);
        s.add_utxo(Utxo {
            utxo_id: op_utxo,
            asset: Asset { id: asset(9) },
            out: State::Mint(MintOutput { owners: owners() }),
        });
        let tx = signed(
            Unsigned::Operation(OperationTx {
                base: base_fields(10, 10),
                ops: vec![Operation {
                    asset: Asset { id: asset(9) },
                    utxo_ids: vec![op_utxo],
                    op: fx::Op::Mint(MintOperation {
                        mint_input: Input {
                            sig_indices: vec![0],
                        },
                        mint_output: MintOutput { owners: owners() },
                        transfer_output: TransferOutput {
                            amt: 42,
                            owners: owners(),
                        },
                    }),
                }],
            }),
            2,
        );
        execute(&mut s, &tx).unwrap();
        assert_eq!(
            s.get_utxo(&op_utxo.input_id()).unwrap_err(),
            Error::NotFound
        );
        // Index 0 is the envelope output; 1 and 2 are the mint's two outputs.
        let authority = s.get_utxo(&UtxoId::new(tx.id(), 1).input_id()).unwrap();
        let minted = s.get_utxo(&UtxoId::new(tx.id(), 2).input_id()).unwrap();
        assert!(matches!(authority.out, State::Mint(_)));
        assert_eq!(minted.out.amount(), 42);
        assert_eq!(minted.asset.id, asset(9));
    }

    #[test]
    fn executing_an_import_asks_the_other_chain_to_remove_what_it_claimed() {
        let mut s = Store::new();
        let imported = UtxoId::new(asset(4), 0);
        let tx = signed(
            Unsigned::Import(ImportTx {
                base: base_fields(10, 15),
                source_chain: asset(9),
                imported_ins: vec![TransferableInput {
                    utxo_id: imported,
                    asset: Asset { id: asset(1) },
                    input: fx::FxIn::Transfer(TransferInput {
                        amt: 5,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
            }),
            2,
        );
        let effects = execute(&mut s, &tx).unwrap();
        assert!(effects.inputs.contains(&imported.input_id()));
        assert_eq!(effects.atomic_requests.len(), 1);
        let (peer, reqs) = &effects.atomic_requests[0];
        assert_eq!(*peer, asset(9));
        assert_eq!(reqs.removes, vec![imported.input_id().to_vec()]);
        assert!(reqs.puts.is_empty());
    }

    #[test]
    fn executing_an_export_hands_the_other_chain_a_utxo_it_can_read() {
        let mut s = Store::new();
        let tx = signed(
            Unsigned::Export(ExportTx {
                base: base_fields(10, 4),
                destination_chain: asset(9),
                exported_outs: vec![TransferableOutput {
                    asset: Asset { id: asset(1) },
                    out: State::Transfer(TransferOutput {
                        amt: 6,
                        owners: owners(),
                    }),
                }],
            }),
            1,
        );
        let effects = execute(&mut s, &tx).unwrap();
        let (peer, reqs) = &effects.atomic_requests[0];
        assert_eq!(*peer, asset(9));
        assert_eq!(reqs.puts.len(), 1);
        let put = &reqs.puts[0];

        // The exported UTXO takes the index after the envelope's own outputs.
        let want = Utxo {
            utxo_id: UtxoId::new(tx.id(), 1),
            asset: Asset { id: asset(1) },
            out: State::Transfer(TransferOutput {
                amt: 6,
                owners: owners(),
            }),
        };
        assert_eq!(put.key, want.input_id().to_vec());
        // The other chain reads exactly these bytes back.
        assert_eq!(Utxo::from_wire(&put.value).unwrap(), want);
        assert_eq!(put.traits, vec![me().0.to_vec()]);
        // The exported output does NOT become a local UTXO.
        assert_eq!(s.get_utxo(&want.input_id()).unwrap_err(), Error::NotFound);
    }

    #[test]
    fn the_exempt_operation_transaction_is_the_one_id_and_no_other() {
        // The value is the id, so nothing that merely resembles it is waived.
        assert_eq!(
            hex::encode(EXEMPT_OPERATION_TX),
            "2f21d57488892c35a339d1bf096f8f33e0e60151c3f42a9923735b79bf4b2e68"
        );
        let mut near = EXEMPT_OPERATION_TX;
        near[31] ^= 1;
        assert_ne!(near, EXEMPT_OPERATION_TX);
    }

    #[test]
    fn a_signature_covers_the_unsigned_bytes_the_verifier_reads() {
        let tx = signed(
            Unsigned::Base(BaseTx {
                base: base_fields(10, 10),
            }),
            1,
        );
        let want = crate::fx::secp256k1::sign_hash(&KEY, &sha256(tx.unsigned_bytes())).unwrap();
        assert_eq!(tx.creds[0].cred.sigs[0], want);
        assert_eq!(
            tx.creds[0],
            Cred::new(Family::Secp256k1, Credential { sigs: vec![want] })
        );
    }
}
