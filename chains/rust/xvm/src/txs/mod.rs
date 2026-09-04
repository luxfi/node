// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The five transactions of the X-Chain, and the envelope they are signed in.
//!
//! Every one of them carries the same spending envelope — network, chain,
//! outputs, inputs, memo — and then whatever it adds on top: a new asset, a set
//! of operations, an import from another chain, an export to one. A plain
//! transfer adds nothing, which is why it is called the base.
//!
//! There is no codec and no registry. The first byte of an unsigned
//! transaction says which of the five it is, and that byte is the whole
//! dispatch. A signed transaction is the unsigned bytes plus a packed run of
//! credentials, and its id is the SHA-256 of exactly those bytes — so the id
//! commits to the signatures as well as the contents.

pub mod executor;

use crate::error::{Error, Result};
use crate::fx::{self, secp256k1, Cred, State};
use crate::hash::sha256;
use crate::ids::Id;
use crate::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, Utxo, UtxoId};
use crate::wire::containers::{
    self, read_blob_list, read_utxo_ids, write_blob_list, write_utxo_ids, InSpec, OutSpec,
};
use crate::zap::{self, Builder, Object};

/// Which of the five a transaction is.
///
/// Zero is reserved so it never appears on the wire — a buffer of zeros is not
/// a transaction of any kind.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Kind {
    Base = 1,
    CreateAsset = 2,
    Operation = 3,
    Import = 4,
    Export = 5,
}

impl Kind {
    pub fn from_u8(v: u8) -> Option<Kind> {
        match v {
            1 => Some(Kind::Base),
            2 => Some(Kind::CreateAsset),
            3 => Some(Kind::Operation),
            4 => Some(Kind::Import),
            5 => Some(Kind::Export),
            _ => None,
        }
    }
}

/// Where the kind byte lives, in every one of the five.
pub const OFF_KIND: usize = 0;
/// Where the shared spending envelope lives, in every one of the five.
pub const OFF_BASE_TX: usize = 8;
/// The fixed section of a plain transfer.
pub const SIZE_BASE_OBJ: usize = 16;

// CreateAssetTx
const OFF_CA_NAME: usize = 16;
const OFF_CA_SYMBOL: usize = 24;
const OFF_CA_DENOM: usize = 32;
const OFF_CA_STATES_LEN: usize = 36;
const OFF_CA_STATES_BLOB: usize = 44;
const SIZE_CA: usize = 52;

// OperationTx
const OFF_OPS_LEN: usize = 16;
const OFF_OPS_BLOB: usize = 24;
const SIZE_OP_TX: usize = 32;

// ImportTx
const OFF_IMPORT_SOURCE: usize = 16;
const OFF_IMPORT_INS: usize = 48;
const SIZE_IMPORT: usize = 56;

// ExportTx
const OFF_EXPORT_DEST: usize = 16;
const OFF_EXPORT_OUTS: usize = 48;
const SIZE_EXPORT: usize = 56;

// InitialState
const OFF_IS_FX_INDEX: usize = 0;
const OFF_IS_OUTS_LEN: usize = 4;
const OFF_IS_OUTS_BLOB: usize = 12;
const SIZE_IS: usize = 20;

// Operation
const OFF_OP_ASSET: usize = 0;
const OFF_OP_UTXO_IDS: usize = 32;
const OFF_OP_FX_OP: usize = 40;
const SIZE_OP: usize = 48;

// ------------------------------------------------------------ InitialState --

/// What an asset starts out with, per feature extension.
///
/// An asset says which fx it supports by INDEX into the chain's fx list, and
/// gives that fx its opening outputs. The fx family is recoverable from each
/// output's own envelope, so it is not written twice.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InitialState {
    pub fx_index: u32,
    pub outs: Vec<State>,
}

impl InitialState {
    pub fn bytes(&self) -> Vec<u8> {
        let outs: Vec<Vec<u8>> = self.outs.iter().map(|o| o.bytes()).collect();
        let mut b = Builder::default();
        let (len_off, len_count, blob) = write_blob_list(&mut b, &outs);
        let ob = b.start_object(SIZE_IS);
        ob.set_u32(&mut b, OFF_IS_FX_INDEX, self.fx_index);
        ob.set_list(&mut b, OFF_IS_OUTS_LEN, len_off, len_count);
        ob.set_bytes(&mut b, OFF_IS_OUTS_BLOB, &blob);
        ob.finish_as_root(&mut b);
        b.finish()
    }

    pub fn parse(buf: &[u8]) -> Result<InitialState> {
        let msg = zap::Message::parse(buf)?;
        let obj = msg.root();
        let out_bufs = read_blob_list(&obj, OFF_IS_OUTS_LEN, OFF_IS_OUTS_BLOB)?;
        let mut outs = Vec::with_capacity(out_bufs.len());
        for env in out_bufs {
            outs.push(State::from_envelope(env)?);
        }
        Ok(InitialState {
            fx_index: obj.u32(OFF_IS_FX_INDEX),
            outs,
        })
    }

    /// An initial state is valid when it names an fx the chain actually runs
    /// and every output it holds is a valid output, listed in canonical order.
    pub fn verify(&self, num_fxs: usize) -> Result<()> {
        if self.fx_index as usize >= num_fxs {
            return Err(Error::UnknownFx);
        }
        for out in &self.outs {
            out.verify()?;
        }
        if !self.is_sorted() {
            return Err(Error::OutputsNotSorted);
        }
        Ok(())
    }

    fn is_sorted(&self) -> bool {
        self.outs.windows(2).all(|w| w[0].bytes() <= w[1].bytes())
    }

    pub fn sort(&mut self) {
        self.outs.sort_by_key(|o| o.bytes());
    }
}

// ---------------------------------------------------------------- Operation --

/// One fx operation over a set of UTXOs of one asset.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Operation {
    pub asset: Asset,
    pub utxo_ids: Vec<UtxoId>,
    pub op: fx::Op,
}

impl Operation {
    pub fn asset_id(&self) -> Id {
        self.asset.id
    }

    pub fn verify(&self) -> Result<()> {
        if !crate::ids::is_sorted_and_unique(&self.utxo_ids) {
            return Err(Error::NotSortedAndUniqueUtxoIds);
        }
        self.asset.verify()?;
        self.op.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        let fx_op = self.op.bytes();
        let mut b = Builder::default();
        let ids: Vec<(Id, u32)> = self
            .utxo_ids
            .iter()
            .map(|u| (u.tx_id, u.output_index))
            .collect();
        let (utxo_off, utxo_count) = write_utxo_ids(&mut b, &ids);
        let ob = b.start_object(SIZE_OP);
        ob.set_bytes_fixed(&mut b, OFF_OP_ASSET, &self.asset.id.0);
        ob.set_list(&mut b, OFF_OP_UTXO_IDS, utxo_off, utxo_count);
        ob.set_bytes(&mut b, OFF_OP_FX_OP, &fx_op);
        ob.finish_as_root(&mut b);
        b.finish()
    }

    pub fn parse(buf: &[u8]) -> Result<Operation> {
        let msg = zap::Message::parse(buf)?;
        let obj = msg.root();
        let op = fx::Op::from_envelope(obj.bytes(OFF_OP_FX_OP))?;
        Ok(Operation {
            asset: Asset {
                id: Id::prefixed_bytes(obj.bytes_fixed(OFF_OP_ASSET, 32)),
            },
            utxo_ids: read_utxo_ids(&obj, OFF_OP_UTXO_IDS)
                .into_iter()
                .map(|(tx_id, index)| UtxoId::new(tx_id, index))
                .collect(),
            op,
        })
    }
}

/// Operations are listed in canonical wire order, and no two may be identical.
pub fn is_sorted_and_unique_operations(ops: &[Operation]) -> bool {
    ops.windows(2).all(|w| w[0].bytes() < w[1].bytes())
}

pub fn sort_operations(ops: &mut [Operation]) {
    ops.sort_by_key(|o| o.bytes());
}

// ------------------------------------------------------------- the five txs --

/// A plain transfer: the spending envelope and nothing else.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct BaseTx {
    pub base: BaseTxFields,
}

/// A new asset, with a name, a symbol, a denomination and its opening state.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct CreateAssetTx {
    pub base: BaseTxFields,
    pub name: String,
    pub symbol: String,
    pub denomination: u8,
    pub states: Vec<InitialState>,
}

/// A set of fx operations over existing UTXOs.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct OperationTx {
    pub base: BaseTxFields,
    pub ops: Vec<Operation>,
}

/// Funds arriving from another chain.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct ImportTx {
    pub base: BaseTxFields,
    pub source_chain: Id,
    pub imported_ins: Vec<TransferableInput>,
}

/// Funds leaving for another chain.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct ExportTx {
    pub base: BaseTxFields,
    pub destination_chain: Id,
    pub exported_outs: Vec<TransferableOutput>,
}

/// One of the five. A closed sum: the compiler checks that every place which
/// asks "which transaction is this?" answers for all of them.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Unsigned {
    Base(BaseTx),
    CreateAsset(CreateAssetTx),
    Operation(OperationTx),
    Import(ImportTx),
    Export(ExportTx),
}

impl Unsigned {
    pub fn kind(&self) -> Kind {
        match self {
            Unsigned::Base(_) => Kind::Base,
            Unsigned::CreateAsset(_) => Kind::CreateAsset,
            Unsigned::Operation(_) => Kind::Operation,
            Unsigned::Import(_) => Kind::Import,
            Unsigned::Export(_) => Kind::Export,
        }
    }

    /// The shared spending envelope.
    pub fn base(&self) -> &BaseTxFields {
        match self {
            Unsigned::Base(t) => &t.base,
            Unsigned::CreateAsset(t) => &t.base,
            Unsigned::Operation(t) => &t.base,
            Unsigned::Import(t) => &t.base,
            Unsigned::Export(t) => &t.base,
        }
    }

    pub fn base_mut(&mut self) -> &mut BaseTxFields {
        match self {
            Unsigned::Base(t) => &mut t.base,
            Unsigned::CreateAsset(t) => &mut t.base,
            Unsigned::Operation(t) => &mut t.base,
            Unsigned::Import(t) => &mut t.base,
            Unsigned::Export(t) => &mut t.base,
        }
    }

    /// Every UTXO this transaction consumes — the envelope's inputs, plus
    /// whatever the specific transaction adds.
    pub fn input_utxos(&self) -> Vec<UtxoId> {
        let mut utxos = self.base().input_utxos();
        match self {
            Unsigned::Operation(t) => {
                for op in &t.ops {
                    utxos.extend(op.utxo_ids.iter().copied());
                }
            }
            Unsigned::Import(t) => {
                for in_ in &t.imported_ins {
                    // Symbolic: this UTXO lives on the other chain, not in
                    // this chain's store.
                    let mut id = in_.utxo_id;
                    id.symbol = true;
                    utxos.push(id);
                }
            }
            _ => {}
        }
        utxos
    }

    /// The identities of everything it consumes. A set, because what matters
    /// is whether two transactions reach for the same UTXO.
    pub fn input_ids(&self) -> std::collections::BTreeSet<Id> {
        self.input_utxos().iter().map(|u| u.input_id()).collect()
    }

    /// How many credentials the transaction must carry: one per thing that has
    /// to be authorised.
    pub fn num_credentials(&self) -> usize {
        match self {
            Unsigned::Base(t) => t.base.num_credentials(),
            Unsigned::CreateAsset(t) => t.base.num_credentials(),
            Unsigned::Operation(t) => t.base.num_credentials() + t.ops.len(),
            Unsigned::Import(t) => t.base.num_credentials() + t.imported_ins.len(),
            Unsigned::Export(t) => t.base.num_credentials(),
        }
    }

    /// The canonical unsigned bytes — the signing target.
    pub fn bytes(&self) -> Vec<u8> {
        match self {
            Unsigned::Base(t) => serialize_base(t),
            Unsigned::CreateAsset(t) => serialize_create_asset(t),
            Unsigned::Operation(t) => serialize_operation(t),
            Unsigned::Import(t) => serialize_import(t),
            Unsigned::Export(t) => serialize_export(t),
        }
    }
}

// -------------------------------------------------------------- serializing --

fn base_tx_wire(base: &BaseTxFields) -> Vec<u8> {
    let out_inners: Vec<Vec<u8>> = base.outs.iter().map(|o| o.out.bytes()).collect();
    let in_inners: Vec<Vec<u8>> = base.ins.iter().map(|i| i.input.bytes()).collect();
    let outs: Vec<OutSpec<'_>> = base
        .outs
        .iter()
        .zip(out_inners.iter())
        .map(|(o, inner)| OutSpec {
            asset_id: o.asset.id,
            output: inner,
        })
        .collect();
    let ins: Vec<InSpec<'_>> = base
        .ins
        .iter()
        .zip(in_inners.iter())
        .map(|(i, inner)| InSpec {
            tx_id: i.utxo_id.tx_id,
            output_index: i.utxo_id.output_index,
            asset_id: i.asset.id,
            input: inner,
        })
        .collect();
    containers::new_xvm_base_tx(
        base.network_id,
        &base.blockchain_id,
        &outs,
        &ins,
        &base.memo,
    )
}

fn decode_base_tx_wire(obj: &Object<'_>) -> Result<BaseTxFields> {
    let w = containers::wrap_xvm_base_tx(obj.bytes(OFF_BASE_TX))?;
    let mut base = BaseTxFields {
        network_id: w.network_id(),
        blockchain_id: w.blockchain_id(),
        outs: Vec::new(),
        ins: Vec::new(),
        memo: Vec::new(),
    };
    for i in 0..w.outs_count() {
        let wo = w.out_at(i)?;
        base.outs.push(TransferableOutput {
            asset: Asset { id: wo.asset_id() },
            out: State::from_envelope(wo.output_bytes())?,
        });
    }
    for i in 0..w.ins_count() {
        let wi = w.in_at(i)?;
        base.ins.push(TransferableInput {
            utxo_id: UtxoId::new(wi.tx_id(), wi.output_index()),
            asset: Asset { id: wi.asset_id() },
            input: fx::FxIn::from_envelope(wi.input_bytes())?,
        });
    }
    let memo = w.memo();
    if !memo.is_empty() {
        base.memo = memo.to_vec();
    }
    Ok(base)
}

fn serialize_base(t: &BaseTx) -> Vec<u8> {
    let env = base_tx_wire(&t.base);
    let mut b = Builder::default();
    let ob = b.start_object(SIZE_BASE_OBJ);
    ob.set_u8(&mut b, OFF_KIND, Kind::Base as u8);
    ob.set_bytes(&mut b, OFF_BASE_TX, &env);
    ob.finish_as_root(&mut b);
    b.finish()
}

fn serialize_create_asset(t: &CreateAssetTx) -> Vec<u8> {
    let env = base_tx_wire(&t.base);
    let states: Vec<Vec<u8>> = t.states.iter().map(|s| s.bytes()).collect();
    let mut b = Builder::default();
    let (len_off, len_count, blob) = write_blob_list(&mut b, &states);
    let ob = b.start_object(SIZE_CA);
    ob.set_u8(&mut b, OFF_KIND, Kind::CreateAsset as u8);
    ob.set_bytes(&mut b, OFF_BASE_TX, &env);
    ob.set_bytes(&mut b, OFF_CA_NAME, t.name.as_bytes());
    ob.set_bytes(&mut b, OFF_CA_SYMBOL, t.symbol.as_bytes());
    ob.set_u8(&mut b, OFF_CA_DENOM, t.denomination);
    ob.set_list(&mut b, OFF_CA_STATES_LEN, len_off, len_count);
    ob.set_bytes(&mut b, OFF_CA_STATES_BLOB, &blob);
    ob.finish_as_root(&mut b);
    b.finish()
}

fn serialize_operation(t: &OperationTx) -> Vec<u8> {
    let env = base_tx_wire(&t.base);
    let ops: Vec<Vec<u8>> = t.ops.iter().map(|o| o.bytes()).collect();
    let mut b = Builder::default();
    let (len_off, len_count, blob) = write_blob_list(&mut b, &ops);
    let ob = b.start_object(SIZE_OP_TX);
    ob.set_u8(&mut b, OFF_KIND, Kind::Operation as u8);
    ob.set_bytes(&mut b, OFF_BASE_TX, &env);
    ob.set_list(&mut b, OFF_OPS_LEN, len_off, len_count);
    ob.set_bytes(&mut b, OFF_OPS_BLOB, &blob);
    ob.finish_as_root(&mut b);
    b.finish()
}

fn serialize_import(t: &ImportTx) -> Vec<u8> {
    let env = base_tx_wire(&t.base);
    let inners: Vec<Vec<u8>> = t.imported_ins.iter().map(|i| i.input.bytes()).collect();
    let mut b = Builder::default();
    let offs: Vec<usize> = t
        .imported_ins
        .iter()
        .zip(inners.iter())
        .map(|(i, inner)| {
            containers::append_transferable_in(
                &mut b,
                &i.utxo_id.tx_id,
                i.utxo_id.output_index,
                &i.asset.id,
                inner,
            )
        })
        .collect();
    let mut lb = b.start_list();
    for off in &offs {
        lb.add_object_ptr(&mut b, *off);
    }
    let (ins_off, ins_len) = lb.finish();

    let ob = b.start_object(SIZE_IMPORT);
    ob.set_u8(&mut b, OFF_KIND, Kind::Import as u8);
    ob.set_bytes(&mut b, OFF_BASE_TX, &env);
    ob.set_bytes_fixed(&mut b, OFF_IMPORT_SOURCE, &t.source_chain.0);
    ob.set_list(&mut b, OFF_IMPORT_INS, ins_off, ins_len);
    ob.finish_as_root(&mut b);
    b.finish()
}

fn serialize_export(t: &ExportTx) -> Vec<u8> {
    let env = base_tx_wire(&t.base);
    let inners: Vec<Vec<u8>> = t.exported_outs.iter().map(|o| o.out.bytes()).collect();
    let mut b = Builder::default();
    let offs: Vec<usize> = t
        .exported_outs
        .iter()
        .zip(inners.iter())
        .map(|(o, inner)| containers::append_transferable_out(&mut b, &o.asset.id, inner))
        .collect();
    let mut lb = b.start_list();
    for off in &offs {
        lb.add_object_ptr(&mut b, *off);
    }
    let (outs_off, outs_len) = lb.finish();

    let ob = b.start_object(SIZE_EXPORT);
    ob.set_u8(&mut b, OFF_KIND, Kind::Export as u8);
    ob.set_bytes(&mut b, OFF_BASE_TX, &env);
    ob.set_bytes_fixed(&mut b, OFF_EXPORT_DEST, &t.destination_chain.0);
    ob.set_list(&mut b, OFF_EXPORT_OUTS, outs_off, outs_len);
    ob.finish_as_root(&mut b);
    b.finish()
}

// ------------------------------------------------------------------ parsing --

/// Read an unsigned transaction. The first byte selects which of the five, and
/// a byte that names none of them is a refusal.
pub fn parse_unsigned(unsigned_bytes: &[u8]) -> Result<Unsigned> {
    let msg = zap::Message::parse(unsigned_bytes)?;
    let obj = msg.root();
    let raw = obj.u8(OFF_KIND);
    let kind = Kind::from_u8(raw).ok_or(Error::UnknownTxKind(raw))?;
    let base = decode_base_tx_wire(&obj)?;
    match kind {
        Kind::Base => Ok(Unsigned::Base(BaseTx { base })),
        Kind::CreateAsset => {
            let state_bufs = read_blob_list(&obj, OFF_CA_STATES_LEN, OFF_CA_STATES_BLOB)?;
            let mut states = Vec::with_capacity(state_bufs.len());
            for buf in state_bufs {
                states.push(InitialState::parse(buf)?);
            }
            Ok(Unsigned::CreateAsset(CreateAssetTx {
                base,
                name: String::from_utf8_lossy(obj.bytes(OFF_CA_NAME)).into_owned(),
                symbol: String::from_utf8_lossy(obj.bytes(OFF_CA_SYMBOL)).into_owned(),
                denomination: obj.u8(OFF_CA_DENOM),
                states,
            }))
        }
        Kind::Operation => {
            let op_bufs = read_blob_list(&obj, OFF_OPS_LEN, OFF_OPS_BLOB)?;
            let mut ops = Vec::with_capacity(op_bufs.len());
            for buf in op_bufs {
                ops.push(Operation::parse(buf)?);
            }
            Ok(Unsigned::Operation(OperationTx { base, ops }))
        }
        Kind::Import => {
            let source_chain = Id::prefixed_bytes(obj.bytes_fixed(OFF_IMPORT_SOURCE, 32));
            let l = obj.list_stride(OFF_IMPORT_INS, containers::OBJ_PTR_STRIDE);
            let mut imported_ins = Vec::with_capacity(l.len());
            for i in 0..l.len() {
                let w = containers::TransferableIn::from_object(l.object_ptr(i));
                imported_ins.push(TransferableInput {
                    utxo_id: UtxoId::new(w.tx_id(), w.output_index()),
                    asset: Asset { id: w.asset_id() },
                    input: fx::FxIn::from_envelope(w.input_bytes())?,
                });
            }
            Ok(Unsigned::Import(ImportTx {
                base,
                source_chain,
                imported_ins,
            }))
        }
        Kind::Export => {
            let destination_chain = Id::prefixed_bytes(obj.bytes_fixed(OFF_EXPORT_DEST, 32));
            let l = obj.list_stride(OFF_EXPORT_OUTS, containers::OBJ_PTR_STRIDE);
            let mut exported_outs = Vec::with_capacity(l.len());
            for i in 0..l.len() {
                let w = containers::TransferableOut::from_object(l.object_ptr(i));
                exported_outs.push(TransferableOutput {
                    asset: Asset { id: w.asset_id() },
                    out: State::from_envelope(w.output_bytes())?,
                });
            }
            Ok(Unsigned::Export(ExportTx {
                base,
                destination_chain,
                exported_outs,
            }))
        }
    }
}

// ----------------------------------------------------------------- signed tx --

/// A signed transaction: what it does, who says so, and the bytes both of those
/// were read from.
///
/// The id is the hash of the SIGNED bytes, so two transactions that differ only
/// in their signatures are two different transactions.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Tx {
    pub unsigned: Unsigned,
    pub creds: Vec<Cred>,
    tx_id: Id,
    bytes: Vec<u8>,
    unsigned_bytes: Vec<u8>,
}

impl Tx {
    /// Build a transaction that has not been signed yet. Its id already exists
    /// — over the credential-free envelope — and changes when it is signed.
    pub fn new(unsigned: Unsigned) -> Tx {
        let mut tx = Tx {
            unsigned,
            creds: Vec::new(),
            tx_id: Id::default(),
            bytes: Vec::new(),
            unsigned_bytes: Vec::new(),
        };
        tx.rebind();
        tx
    }

    fn rebind(&mut self) {
        let unsigned_bytes = self.unsigned.bytes();
        let cred_envs: Vec<Vec<u8>> = self.creds.iter().map(|c| c.bytes()).collect();
        let signed = containers::new_signed_tx(&unsigned_bytes, &cred_envs);
        self.tx_id = Id(sha256(&signed));
        self.bytes = signed;
        self.unsigned_bytes = unsigned_bytes;
    }

    pub fn id(&self) -> Id {
        self.tx_id
    }

    /// The canonical encoding — what a peer is sent, and what the id is over.
    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    /// The signing target.
    pub fn unsigned_bytes(&self) -> &[u8] {
        &self.unsigned_bytes
    }

    pub fn size(&self) -> usize {
        self.bytes.len()
    }

    pub fn input_ids(&self) -> std::collections::BTreeSet<Id> {
        self.unsigned.input_ids()
    }

    /// Attach credentials, one signer set at a time, and rebind the bytes and
    /// the id to include them.
    pub fn sign(&mut self, family: fx::Family, signers: &[Vec<[u8; 32]>]) -> Result<()> {
        let hash = sha256(&self.unsigned.bytes());
        for keys in signers {
            let mut sigs = Vec::with_capacity(keys.len());
            for key in keys {
                sigs.push(secp256k1::sign_hash(key, &hash)?);
            }
            self.creds
                .push(Cred::new(family, secp256k1::Credential { sigs }));
        }
        self.rebind();
        Ok(())
    }

    /// Read a signed transaction off the wire, byte-preserving: the id is over
    /// exactly the bytes that arrived.
    pub fn parse(signed_bytes: &[u8]) -> Result<Tx> {
        let st = containers::wrap_signed_tx(signed_bytes)?;
        let unsigned_bytes = st.unsigned_bytes().to_vec();
        let unsigned = parse_unsigned(&unsigned_bytes)?;
        let mut creds = Vec::with_capacity(st.credential_count() as usize);
        for env in st.credential_envelopes()? {
            creds.push(Cred::from_envelope(env)?);
        }
        Ok(Tx {
            unsigned,
            creds,
            tx_id: Id(sha256(signed_bytes)),
            bytes: signed_bytes.to_vec(),
            unsigned_bytes,
        })
    }

    /// The UTXOs this transaction produces, in the order the executor assigns
    /// them.
    ///
    /// The order IS the identity: a UTXO is named by its position, so a
    /// different order would be different money.
    pub fn utxos(&self) -> Vec<Utxo> {
        let tx_id = self.id();
        let base = self.unsigned.base();
        let mut utxos: Vec<Utxo> = base
            .outs
            .iter()
            .enumerate()
            .map(|(i, out)| Utxo {
                utxo_id: UtxoId::new(tx_id, i as u32),
                asset: Asset { id: out.asset_id() },
                out: out.out.clone(),
            })
            .collect();

        match &self.unsigned {
            Unsigned::CreateAsset(t) => {
                // A new asset's id is the id of the transaction that created
                // it, so its opening outputs hold an asset that did not exist
                // a moment ago.
                for state in &t.states {
                    for out in &state.outs {
                        utxos.push(Utxo {
                            utxo_id: UtxoId::new(tx_id, utxos.len() as u32),
                            asset: Asset { id: tx_id },
                            out: out.clone(),
                        });
                    }
                }
            }
            Unsigned::Operation(t) => {
                for op in &t.ops {
                    let asset = op.asset_id();
                    for out in op.op.outs() {
                        utxos.push(Utxo {
                            utxo_id: UtxoId::new(tx_id, utxos.len() as u32),
                            asset: Asset { id: asset },
                            out,
                        });
                    }
                }
            }
            _ => {}
        }
        utxos
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::{TransferInput, TransferOutput};
    use crate::fx::{nft, property, Input, Owners};
    use crate::ids::ShortId;

    fn asset(n: u8) -> Id {
        Id::prefixed_bytes(&[n])
    }

    fn owners() -> Owners {
        Owners::new(1, vec![ShortId::prefixed_bytes(&[1])])
    }

    fn sample_base() -> BaseTxFields {
        BaseTxFields {
            network_id: 10,
            blockchain_id: asset(5),
            outs: vec![TransferableOutput {
                asset: Asset { id: asset(1) },
                out: State::Transfer(TransferOutput {
                    amt: 12345,
                    owners: owners(),
                }),
            }],
            ins: vec![TransferableInput {
                utxo_id: UtxoId::new(asset(0xff), 1),
                asset: Asset { id: asset(1) },
                input: fx::FxIn::Transfer(TransferInput {
                    amt: 54321,
                    input: Input {
                        sig_indices: vec![2],
                    },
                }),
            }],
            memo: vec![0, 1, 2, 3],
        }
    }

    fn round_trip(u: Unsigned) {
        let tx = Tx::new(u.clone());
        let parsed = Tx::parse(tx.bytes()).unwrap();
        assert_eq!(parsed.bytes(), tx.bytes());
        assert_eq!(parsed.id(), tx.id());
        assert_eq!(parsed.unsigned, u);
    }

    #[test]
    fn a_plain_transfer_round_trips() {
        round_trip(Unsigned::Base(BaseTx {
            base: sample_base(),
        }));
    }

    #[test]
    fn a_create_asset_round_trips_including_its_initial_state() {
        round_trip(Unsigned::CreateAsset(CreateAssetTx {
            base: sample_base(),
            name: "Test Asset".into(),
            symbol: "TST".into(),
            denomination: 8,
            states: vec![InitialState {
                fx_index: 0,
                outs: vec![State::Mint(crate::fx::secp256k1::MintOutput {
                    owners: owners(),
                })],
            }],
        }));
    }

    #[test]
    fn a_create_asset_with_no_initial_state_round_trips() {
        round_trip(Unsigned::CreateAsset(CreateAssetTx {
            base: sample_base(),
            name: "A".into(),
            symbol: "A".into(),
            denomination: 0,
            states: vec![],
        }));
    }

    #[test]
    fn an_operation_tx_round_trips_every_fx_operation() {
        let ops = vec![
            fx::Op::Mint(crate::fx::secp256k1::MintOperation {
                mint_input: Input {
                    sig_indices: vec![0],
                },
                mint_output: crate::fx::secp256k1::MintOutput { owners: owners() },
                transfer_output: TransferOutput {
                    amt: 1,
                    owners: owners(),
                },
            }),
            fx::Op::NftMint(nft::MintOperation {
                mint_input: Input {
                    sig_indices: vec![0],
                },
                group_id: 3,
                payload: vec![1, 2],
                outputs: vec![owners()],
            }),
            fx::Op::NftTransfer(nft::TransferOperation {
                input: Input {
                    sig_indices: vec![0],
                },
                output: nft::TransferOutput {
                    group_id: 3,
                    payload: vec![7],
                    owners: owners(),
                },
            }),
            fx::Op::PropertyMint(property::MintOperation {
                mint_input: Input {
                    sig_indices: vec![0],
                },
                mint_output: property::MintOutput { owners: owners() },
                owned_output: property::OwnedOutput { owners: owners() },
            }),
            fx::Op::PropertyBurn(property::BurnOperation {
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        ];
        for op in ops {
            round_trip(Unsigned::Operation(OperationTx {
                base: sample_base(),
                ops: vec![Operation {
                    asset: Asset { id: asset(1) },
                    utxo_ids: vec![UtxoId::new(asset(0x0f), 1)],
                    op,
                }],
            }));
        }
    }

    #[test]
    fn an_import_round_trips_its_source_chain_and_its_imported_inputs() {
        round_trip(Unsigned::Import(ImportTx {
            base: sample_base(),
            source_chain: asset(0x11),
            imported_ins: vec![TransferableInput {
                utxo_id: UtxoId::new(asset(0xaa), 3),
                asset: Asset { id: asset(1) },
                input: fx::FxIn::Transfer(TransferInput {
                    amt: 1000,
                    input: Input {
                        sig_indices: vec![0],
                    },
                }),
            }],
        }));
    }

    #[test]
    fn an_export_round_trips_its_destination_chain_and_its_exported_outputs() {
        round_trip(Unsigned::Export(ExportTx {
            base: sample_base(),
            destination_chain: asset(0x22),
            exported_outs: vec![TransferableOutput {
                asset: Asset { id: asset(1) },
                out: State::Transfer(TransferOutput {
                    amt: 100,
                    owners: owners(),
                }),
            }],
        }));
    }

    #[test]
    fn the_first_byte_is_the_whole_dispatch() {
        for (u, want) in [
            (
                Unsigned::Base(BaseTx {
                    base: sample_base(),
                }),
                Kind::Base,
            ),
            (
                Unsigned::CreateAsset(CreateAssetTx {
                    base: sample_base(),
                    name: "A".into(),
                    symbol: "A".into(),
                    denomination: 0,
                    states: vec![],
                }),
                Kind::CreateAsset,
            ),
            (
                Unsigned::Operation(OperationTx {
                    base: sample_base(),
                    ops: vec![],
                }),
                Kind::Operation,
            ),
            (
                Unsigned::Import(ImportTx {
                    base: sample_base(),
                    source_chain: asset(1),
                    imported_ins: vec![],
                }),
                Kind::Import,
            ),
            (
                Unsigned::Export(ExportTx {
                    base: sample_base(),
                    destination_chain: asset(1),
                    exported_outs: vec![],
                }),
                Kind::Export,
            ),
        ] {
            let raw = u.bytes();
            let root = zap::Message::parse(&raw).unwrap().root();
            assert_eq!(root.u8(OFF_KIND), want as u8);
            assert_eq!(parse_unsigned(&raw).unwrap().kind(), want);
        }
    }

    #[test]
    fn a_kind_byte_that_names_no_transaction_is_refused() {
        let mut raw = Unsigned::Base(BaseTx {
            base: sample_base(),
        })
        .bytes();
        let root_off = u32::from_le_bytes(raw[8..12].try_into().unwrap()) as usize;
        raw[root_off + OFF_KIND] = 9;
        assert_eq!(parse_unsigned(&raw).unwrap_err(), Error::UnknownTxKind(9));
        // Zero is reserved and is refused too.
        raw[root_off + OFF_KIND] = 0;
        assert_eq!(parse_unsigned(&raw).unwrap_err(), Error::UnknownTxKind(0));
    }

    #[test]
    fn signing_changes_the_id_because_the_id_covers_the_signatures() {
        let mut tx = Tx::new(Unsigned::Base(BaseTx {
            base: sample_base(),
        }));
        let before = tx.id();
        tx.sign(fx::Family::Secp256k1, &[vec![[7u8; 32], [7u8; 32]]])
            .unwrap();
        assert_ne!(tx.id(), before);
        assert_eq!(tx.creds.len(), 1);
        assert_eq!(tx.creds[0].cred.sigs.len(), 2);

        let parsed = Tx::parse(tx.bytes()).unwrap();
        assert_eq!(parsed.id(), tx.id());
        assert_eq!(parsed.creds, tx.creds);
    }

    #[test]
    fn a_signature_is_over_the_unsigned_bytes_not_the_signed_ones() {
        let mut tx = Tx::new(Unsigned::Base(BaseTx {
            base: sample_base(),
        }));
        let unsigned = tx.unsigned_bytes().to_vec();
        tx.sign(fx::Family::Secp256k1, &[vec![[7u8; 32]]]).unwrap();
        // Signing does not change the target.
        assert_eq!(tx.unsigned_bytes(), &unsigned[..]);
        let want = secp256k1::sign_hash(&[7u8; 32], &sha256(&unsigned)).unwrap();
        assert_eq!(tx.creds[0].cred.sigs[0], want);
    }

    #[test]
    fn how_many_credentials_a_transaction_needs_depends_on_what_it_authorises() {
        let base = sample_base();
        assert_eq!(
            Unsigned::Base(BaseTx { base: base.clone() }).num_credentials(),
            1
        );
        assert_eq!(
            Unsigned::Operation(OperationTx {
                base: base.clone(),
                ops: vec![Operation {
                    asset: Asset { id: asset(1) },
                    utxo_ids: vec![],
                    op: fx::Op::PropertyBurn(property::BurnOperation {
                        input: Input {
                            sig_indices: vec![0]
                        }
                    }),
                }],
            })
            .num_credentials(),
            2
        );
        assert_eq!(
            Unsigned::Import(ImportTx {
                base: base.clone(),
                source_chain: asset(1),
                imported_ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(asset(2), 0),
                    asset: Asset { id: asset(1) },
                    input: fx::FxIn::Transfer(TransferInput {
                        amt: 1,
                        input: Input {
                            sig_indices: vec![0]
                        }
                    }),
                }],
            })
            .num_credentials(),
            2
        );
        // An export authorises only its own inputs; what leaves needs no
        // signature to arrive.
        assert_eq!(
            Unsigned::Export(ExportTx {
                base,
                destination_chain: asset(1),
                exported_outs: vec![TransferableOutput {
                    asset: Asset { id: asset(1) },
                    out: State::Transfer(TransferOutput {
                        amt: 1,
                        owners: owners()
                    }),
                }],
            })
            .num_credentials(),
            1
        );
    }

    #[test]
    fn an_imported_input_is_symbolic_because_that_utxo_lives_elsewhere() {
        let u = Unsigned::Import(ImportTx {
            base: sample_base(),
            source_chain: asset(1),
            imported_ins: vec![TransferableInput {
                utxo_id: UtxoId::new(asset(2), 0),
                asset: Asset { id: asset(1) },
                input: fx::FxIn::Transfer(TransferInput {
                    amt: 1,
                    input: Input {
                        sig_indices: vec![0],
                    },
                }),
            }],
        });
        let utxos = u.input_utxos();
        assert_eq!(utxos.len(), 2);
        assert!(!utxos[0].symbol);
        assert!(utxos[1].symbol);
    }

    #[test]
    fn a_create_asset_produces_an_asset_named_after_the_transaction_that_made_it() {
        let tx = Tx::new(Unsigned::CreateAsset(CreateAssetTx {
            base: sample_base(),
            name: "A".into(),
            symbol: "A".into(),
            denomination: 0,
            states: vec![InitialState {
                fx_index: 0,
                outs: vec![State::Mint(crate::fx::secp256k1::MintOutput {
                    owners: owners(),
                })],
            }],
        }));
        let utxos = tx.utxos();
        assert_eq!(utxos.len(), 2);
        // The envelope's own output keeps its asset.
        assert_eq!(utxos[0].asset.id, asset(1));
        // The initial state's output holds the brand-new asset.
        assert_eq!(utxos[1].asset.id, tx.id());
        assert_eq!(utxos[1].utxo_id.output_index, 1);
    }

    #[test]
    fn an_operation_tx_produces_what_its_operations_produce() {
        let tx = Tx::new(Unsigned::Operation(OperationTx {
            base: sample_base(),
            ops: vec![Operation {
                asset: Asset { id: asset(9) },
                utxo_ids: vec![UtxoId::new(asset(1), 0)],
                op: fx::Op::Mint(crate::fx::secp256k1::MintOperation {
                    mint_input: Input {
                        sig_indices: vec![0],
                    },
                    mint_output: crate::fx::secp256k1::MintOutput { owners: owners() },
                    transfer_output: TransferOutput {
                        amt: 1,
                        owners: owners(),
                    },
                }),
            }],
        }));
        let utxos = tx.utxos();
        // One envelope output, then the mint authority and the minted value.
        assert_eq!(utxos.len(), 3);
        assert_eq!(utxos[1].asset.id, asset(9));
        assert_eq!(utxos[2].asset.id, asset(9));
        assert_eq!(
            utxos
                .iter()
                .map(|u| u.utxo_id.output_index)
                .collect::<Vec<_>>(),
            vec![0, 1, 2]
        );
    }

    #[test]
    fn an_initial_state_must_name_an_fx_the_chain_runs() {
        let is = InitialState {
            fx_index: 3,
            outs: vec![],
        };
        assert_eq!(is.verify(3).unwrap_err(), Error::UnknownFx);
        assert!(is.verify(4).is_ok());
    }

    #[test]
    fn an_initial_states_outputs_must_be_in_canonical_order() {
        let a = State::Mint(crate::fx::secp256k1::MintOutput { owners: owners() });
        let b = State::Mint(crate::fx::secp256k1::MintOutput {
            owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[9])]),
        });
        let (lo, hi) = if a.bytes() < b.bytes() {
            (a.clone(), b.clone())
        } else {
            (b, a)
        };
        assert!(InitialState {
            fx_index: 0,
            outs: vec![lo.clone(), hi.clone()]
        }
        .verify(1)
        .is_ok());
        assert_eq!(
            InitialState {
                fx_index: 0,
                outs: vec![hi, lo]
            }
            .verify(1)
            .unwrap_err(),
            Error::OutputsNotSorted
        );
    }

    #[test]
    fn an_initial_state_refuses_an_invalid_output() {
        let is = InitialState {
            fx_index: 0,
            outs: vec![State::Transfer(TransferOutput {
                amt: 0,
                owners: owners(),
            })],
        };
        assert_eq!(is.verify(1).unwrap_err(), Error::NoValueOutput);
    }

    #[test]
    fn an_initial_state_round_trips_through_its_own_bytes() {
        let is = InitialState {
            fx_index: 2,
            outs: vec![
                State::PropertyMint(property::MintOutput { owners: owners() }),
                State::PropertyOwned(property::OwnedOutput { owners: owners() }),
            ],
        };
        assert_eq!(InitialState::parse(&is.bytes()).unwrap(), is);
    }

    #[test]
    fn an_operation_must_list_its_utxos_sorted_and_unique() {
        let mk = |ids: Vec<UtxoId>| Operation {
            asset: Asset { id: asset(1) },
            utxo_ids: ids,
            op: fx::Op::PropertyBurn(property::BurnOperation {
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        };
        assert!(mk(vec![UtxoId::new(asset(1), 0), UtxoId::new(asset(1), 1)])
            .verify()
            .is_ok());
        assert_eq!(
            mk(vec![UtxoId::new(asset(1), 1), UtxoId::new(asset(1), 0)])
                .verify()
                .unwrap_err(),
            Error::NotSortedAndUniqueUtxoIds
        );
        assert_eq!(
            mk(vec![UtxoId::new(asset(1), 0), UtxoId::new(asset(1), 0)])
                .verify()
                .unwrap_err(),
            Error::NotSortedAndUniqueUtxoIds
        );
    }

    #[test]
    fn an_operation_with_an_empty_asset_is_refused() {
        let op = Operation {
            asset: Asset { id: Id::default() },
            utxo_ids: vec![],
            op: fx::Op::PropertyBurn(property::BurnOperation {
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        };
        assert_eq!(op.verify().unwrap_err(), Error::EmptyAssetId);
    }

    #[test]
    fn operations_are_ordered_by_their_wire_bytes_and_may_not_repeat() {
        let mk = |idx: u32| Operation {
            asset: Asset { id: asset(1) },
            utxo_ids: vec![UtxoId::new(asset(1), idx)],
            op: fx::Op::PropertyBurn(property::BurnOperation {
                input: Input {
                    sig_indices: vec![0],
                },
            }),
        };
        let mut ops = vec![mk(3), mk(1), mk(2)];
        sort_operations(&mut ops);
        assert!(is_sorted_and_unique_operations(&ops));
        assert!(!is_sorted_and_unique_operations(&[mk(1), mk(1)]));
    }

    #[test]
    fn an_operation_round_trips_through_its_own_bytes() {
        let op = Operation {
            asset: Asset { id: asset(7) },
            utxo_ids: vec![UtxoId::new(asset(1), 0), UtxoId::new(asset(2), 9)],
            op: fx::Op::NftTransfer(nft::TransferOperation {
                input: Input {
                    sig_indices: vec![0, 1],
                },
                output: nft::TransferOutput {
                    group_id: 4,
                    payload: vec![1, 2, 3],
                    owners: owners(),
                },
            }),
        };
        assert_eq!(Operation::parse(&op.bytes()).unwrap(), op);
    }
}
