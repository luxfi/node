// SPDX-License-Identifier: BSD-3-Clause-Eco

//! ZAP, once, for every chain in this directory.
//!
//! [`zap`] is emitted by `zapgen` from the same toolchain that emits each
//! chain's accessors, so the reader a chain calls and the reader the Go and
//! C++ chains call come out of one source. It is not edited here: `make wire`
//! writes it, and a hand fix to a generated file is a fork of the wire that
//! compiles.
//!
//! The schemas the accessors come from are in `chains/schema`.

pub mod zap;
