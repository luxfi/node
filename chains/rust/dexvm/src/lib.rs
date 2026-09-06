// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Lux D-Chain: the DEX's real-assets-only admission layer.
//!
//! D is not a block chain. `chains/dexvm` is a REGISTRY, and what it decides is
//! identity and admission — what an asset IS, what a market IS, which kinds may
//! be registered, and whether native value may activate at all. Those are
//! consensus decisions with no wire of their own: two implementations deriving
//! different bytes for one asset have forked the value plane without ever
//! disagreeing about a transaction.
//!
//! One property is made structural rather than incidental: **every asset the DEX
//! can credit or debit corresponds to a real object on-chain**. There is no
//! synthetic class, no ASCII-ticker identity, no declared-but-unbacked credit.
//! An asset's identity is a hash of where it lives, so two parties given the
//! same chain derive the same 32-byte AssetID, a fabricated asset has no
//! preimage, and a market over one has no id to resolve.
//!
//! ## What is where
//!
//! - [`ids`] — the 32-byte name, and the one hash that derives it.
//! - [`error`] — what the chain refuses for, in two halves: the sentinel a
//!   caller branches on, and the sentence a human reads.
//! - [`asset`] — `derive_asset_id`, `market_id`, the length-prefixed fold. The
//!   consensus bytes.
//! - [`registry`] — the admitted set, and the one door an asset enters through.
//! - [`market`] — an admitted pair, pinned to two real assets by construction.
//! - [`mode`] — under which consensus posture value may activate, and what it
//!   must then say about itself.
//! - [`network`] — which networks bear value, and which are a developer's.
//!
//! ## The bytes are cross-language and they are checked
//!
//! `tests/golden.rs` holds identities the GO reference printed, and
//! `tests/corpus.rs` runs this crate's differential evaluator over the shared
//! corpus and compares every field against Go's recorded answers. A change that
//! moves an AssetID is a fork, and it fails here before it reaches a chain.

pub mod asset;
pub mod error;
pub mod ids;
pub mod market;
pub mod mode;
pub mod network;
pub mod registry;

pub use asset::{derive_asset_id, market_id, AssetKind};
pub use error::{Error, Result};
pub use ids::Id;
pub use market::Market;
pub use mode::{guard_value_activation, ConsensusMode, LaunchAssertions, ValueModeStatus};
pub use network::{network_class_for, NetworkClass};
pub use registry::{Asset, ChainVerifier, Registry, RiskTier};
