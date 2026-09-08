// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Lux O-Chain in Rust.
//!
//! The canonical implementation is `github.com/luxfi/oracle/vm`, 1628 lines of
//! Go; `chains/oraclevm` is a 213-line re-export of it and is not the chain.
//! This crate is the same chain's wire, its identities and its three decision
//! planes, written from the Go source and answering the same corpus.

pub mod gojson;
pub mod gotime;
pub mod ids;
pub mod types;
pub mod vm;
