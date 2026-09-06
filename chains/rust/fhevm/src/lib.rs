// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Lux F-Chain in Rust.
//!
//! F is the COORDINATION plane for confidential compute. It records the public
//! coordinates of encrypted values — a handle, the digest of the off-chain
//! ciphertext body, its owner, the capabilities granted over it, and the
//! threshold decryptions asked for and answered — and nothing else. It holds no
//! ciphertext body, no FHE secret key and no decryption share.
//!
//! THIS CHAIN PERFORMS NO HOMOMORPHIC ARITHMETIC, and needs no library that
//! can. The encryption happens off-chain, the key shares live on the threshold
//! committee, and what reaches F is hashes, addresses, bitmasks and signatures.
//! The only cryptography here is SHA-256 and ML-DSA-65, and the ML-DSA comes
//! from `libluxcrypto` — the same implementation the Go reference verifies with
//! — rather than from a second one written here.
//!
//! The reference is `github.com/luxfi/chains/fhevm`. Most modules below are the
//! Go file of the same name; the rest are what Go takes from other modules —
//! [`zap`] is `luxfi/zap`'s arena format, [`id`] is `luxfi/ids`, [`json`] is
//! `encoding/json`'s rules as this chain relies on them, [`fee`] is
//! `luxfi/chains/fee`, and [`host`] is the seam the node offers a chain.

pub mod batch;
pub mod block;
pub mod error;
pub mod fee;
pub mod gas;
pub mod host;
pub mod id;
pub mod json;
pub mod pq;
pub mod record;
pub mod store;
pub mod tx;
pub mod vm;
pub mod wire;
pub mod zap;

pub use error::{Error, Result};
pub use tx::Transaction;
pub use vm::{Config, Vm};
