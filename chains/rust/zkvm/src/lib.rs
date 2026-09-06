// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Lux Z-Chain in Rust.
//!
//! Z is the SHIELDED settlement plane. It holds a pool of note commitments, the
//! set of nullifiers that have already been spent out of it, and a state root
//! folded over both. What travels is a block of transactions, each carrying a
//! zero-knowledge proof; the proof IS the credential, because nothing else ties
//! a spend to a note nobody can see.
//!
//! THE CHAIN IS STRICT-PQ BY PROFILE. [`config::Config::chain_default`] pins
//! `strict_pq`, and the one bit refuses every classical, pairing-based proof
//! system by name — so a machine that broke bn254 cannot forge a shield or an
//! unshield and mint shielded value. There is one enforcement point,
//! [`verifier::Verifier`], and no path around it.
//!
//! The reference is `github.com/luxfi/chains/zkvm`. Most modules below are the
//! Go file of the same name; the rest are what Go takes from other packages —
//! [`zap`] is `luxfi/zap`'s arena format, [`ids`] is `luxfi/ids`, [`db`] and
//! [`store`] are the `luxfi/vm/chain` staging and tip that Go's `chain.New`
//! supplies, and [`host`] is the seam the node offers a chain.

pub mod block;
pub mod config;
pub mod db;
pub mod error;
pub mod hash;
pub mod host;
pub mod ids;
pub mod nullifier;
pub mod root;
pub mod starkfri;
pub mod store;
pub mod txs;
pub mod utxo;
pub mod verifier;
pub mod vertex;
pub mod vm;
pub mod wire;
pub mod zchain_zap;

pub use block::Block;
pub use config::Config;
pub use error::{Error, Result};
pub use txs::Tx;
pub use vm::{Genesis, Zvm};
