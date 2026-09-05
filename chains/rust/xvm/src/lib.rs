// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Lux X-Chain.
//!
//! A UTXO ledger: value exists as unspent outputs, a transaction consumes some
//! and produces others, and a block is an ordered run of transactions plus the
//! root of the state they leave behind. There is one asset type only in the
//! sense that every output names the asset it moves — the chain is multi-asset
//! from the bottom, and an asset is created by a transaction whose id becomes
//! the asset's id.
//!
//! ## What is where
//!
//! - [`zap`] — the serialization. Every byte this chain puts on a wire or a
//!   disk is a ZAP object: a fixed section of known offsets plus a tail. There
//!   is no codec, no version negotiation and no reflection.
//! - [`wire`] — the envelopes. A polymorphic value names itself on the wire
//!   with two bytes, `(family, shape)`, and is reconstructed by matching on
//!   that pair. Adding a primitive is a new arm, checked by the compiler.
//! - [`fx`] — the feature extensions: the three families of output, input,
//!   operation and credential this chain understands, and the rules by which a
//!   credential spends an output.
//! - [`utxo`] — what an output, an input and an unspent output ARE, plus the
//!   flow check that makes a transaction balance.
//! - [`txs`] — the five transactions, their encoding, their signing, and the
//!   three passes ([`txs::executor`]) that verify and apply one.
//! - [`state`] — what the chain knows, in two shapes: what is committed, and
//!   what a block would do.
//! - [`block`] — a block, how one is built, and the state machine that
//!   verifies, accepts and rejects them.
//! - [`vm`] — the chain behind the host's seam ([`host`]).
//!
//! ## The rule the whole thing turns on
//!
//! A transaction's id is the SHA-256 of its signed bytes and a block's id is
//! the SHA-256 of its block bytes. Nothing is ever re-encoded on the way in:
//! what was parsed is what is stored and what is hashed. That is why a
//! sortedness rule is a consensus rule here — two encodings of one spend would
//! be two ids for one thing.

pub mod block;
pub mod db;
pub mod error;
pub mod fx;
pub mod gossip;
pub mod hash;
pub mod ids;
pub mod mempool;
pub mod security;
pub mod state;
pub mod txs;
pub mod utxo;
pub mod vm;
pub mod wire;
pub mod zap;

/// The node's seam, from the node.
///
/// `lux-rs/node` defines `Vm` and `Block` in its own `src/vm.rs` and holds
/// every chain it runs as `Arc<dyn Vm>`. This crate names THAT trait — it does
/// not restate it. A restatement would compile against a shape the node never
/// sees, and would keep compiling on the day the node changed a method.
pub use lux_node::vm as host;

pub use error::{Error, Result};
pub use ids::{Id, ShortId};
