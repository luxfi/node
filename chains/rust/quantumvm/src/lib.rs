// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Lux Q-Chain.
//!
//! Q is the post-quantum lane of the parallel-witness finality model (LP-020).
//! Two things live here and they are not the same thing:
//!
//! - A **transaction stamp**: an ML-DSA-65 (FIPS 204) signature by one
//!   validator over one transaction, bound to the moment it was made. It rides
//!   on the wire beside the transaction it covers.
//! - A **round witness**: a quorum of the committee signing one block, whose
//!   signatures aggregate into a single certificate. That lives in [`quasar`]
//!   and is deliberately NOT a field of a block — the block id is the hash of
//!   the block's own bytes, so a signature inside it would give two honest
//!   nodes two ids for one block.
//!
//! Q-Chain sells no blockspace. LP-0130 §6 makes finality-cert inclusion a
//! validator obligation paid through P-Chain rewards, so [`vm::Qvm::issue_tx`]
//! refuses every user transaction whatever it offers, and the chain advances
//! only through consensus-internal aggregation.
//!
//! ## What is where
//!
//! - [`qchain_zap`] — the block and transaction shapes, emitted by `zapgen`
//!   from `chains/schema/qchain.zap`. Every byte on a wire or a disk is a ZAP
//!   object: a fixed section at known offsets plus a tail, read through
//!   `lux_zap`, the one runtime all three chains call.
//! - [`wire`] — what a Q block MEANS: the transaction set a length list
//!   partitions, and the canonical-or-nothing parse that admits exactly one
//!   byte string per block.
//! - [`quantum`] — the ML-DSA signer: keys, the stamp, what the signature
//!   covers, and batch verification.
//! - [`tx`] — a transaction, the pool that holds pending ones, and the worker
//!   that verifies a batch.
//! - [`block`] — a block and the rules that decide whether it may be built on.
//! - [`quasar`] — the finality bridge: the committee, the threshold, and the
//!   aggregate that finalizes a block.
//! - [`store`] — what the chain keeps, on disk, one commit at a time.
//! - [`vm`] — the chain behind the host's seam ([`host`]).

pub mod block;
pub mod config;
pub mod error;
pub mod ids;
pub mod quantum;
pub mod quasar;
pub mod store;
pub mod tx;
pub mod vm;
pub mod qchain_zap;
pub mod wire;

/// The node's seam, from the node.
///
/// `lux-rs/node` defines `Vm` and `Block` in its own `src/vm.rs` and holds
/// every chain it runs as a `dyn Vm`. This crate names THAT trait — it does not
/// restate it. A restatement would compile against a shape the node never sees,
/// and would keep compiling on the day the node changed a method.
pub use lux_node::vm as host;

pub use block::Block;
pub use error::{Error, Result};
pub use ids::Id;
pub use tx::Tx;
pub use vm::Qvm;

/// What this chain reports as its version.
pub const VERSION: &str = "1.0.0";
