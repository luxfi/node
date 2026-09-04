// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The Lux P-Chain.
//!
//! This is the chain that decides who validates. It holds the validator sets,
//! the stake behind them, the clock those sets change on, and the networks
//! they secure — and it does all of that with no administrator: anyone with
//! enough of the native asset can put it up and be in the set, and nobody can
//! keep them out.
//!
//! That is not a feature among features. A permissionless validator set with
//! no key that admits and no list that excludes is the whole reason a chain
//! like this is a public good rather than a product, and it is why this is the
//! most security-critical thing here. Every check in [`executor`] exists to
//! make the entry honest — enough stake, real stake, for long enough, and only
//! once — and none of them exists to make it selective.
//!
//! ## Where things are
//!
//! - [`zap`] — the wire. A ZAP object is a fixed payload plus pointers, read
//!   by indexing rather than parsing.
//! - [`ids`], [`components`] — names, and the unspent outputs value lives in.
//! - [`txs`] — what someone can ask the chain to do, and what the bytes of
//!   each request are.
//! - [`security`] — on what terms a network's set is admitted.
//! - [`sign`] — who authorised a spend.
//! - [`signer`] — how a validator proves the key it signs with is its own.
//! - [`reward`] — what a staker is paid.
//! - [`stakingparams`] — the terms the set is admitted on, and the keyless
//!   rule that decides whether a change to them is admissible.
//! - [`flow`] — the check that value is not created.
//! - [`genesis`] — how the chain is born.
//! - [`state`] — what the chain believes.
//! - [`executor`] — what a transaction does to that belief.
//! - [`block`] — the four things a block can be.
//! - [`uptime`] — how much of its term a validator was reachable for.
//! - [`vm`] — the chain, as the node holds it.

pub mod block;
pub mod components;
pub mod executor;
pub mod flow;
pub mod genesis;
pub mod ids;
pub mod reward;
pub mod security;
pub mod sign;
pub mod signer;
pub mod stakingparams;
pub mod state;
pub mod txs;
pub mod uptime;
pub mod vm;
pub mod zap;

pub use ids::Id;
pub use vm::PlatformVm;
