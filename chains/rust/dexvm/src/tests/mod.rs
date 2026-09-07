// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The reference's own suite, ported case for case.
//!
//! These are the 27 tests of `chains/dexvm/registry`, in the same files and in
//! the same order, asserting the same things with the same expected values.
//! They are IN the crate rather than in `tests/` because the Go tests are in
//! `package registry`: one of them reaches past the door to insert a market the
//! way a corrupted config would, and a suite that could not do that could not
//! prove the startup gate is a second line of defence rather than an echo of the
//! first.
//!
//! Where Go writes `ids.GenerateTestID()` these write [`tid`], which is
//! deterministic. Go's ids are random, so its tests are distinct-by-luck and its
//! failures are unreproducible; naming each id makes both fixed, and the
//! property under test — that two different chains are two different chains — is
//! unchanged.

mod asset_golden;
mod fakechain;
mod gate;
mod manifest;
mod manifest_pin;
mod registry;
mod runtime_verifier;

pub(crate) use fakechain::{addr20, FakeChain};

use crate::ids::{self, Id};

/// A distinct, named id. Go writes `ids.GenerateTestID()`; naming the id makes a
/// failure say WHICH chain disagreed.
pub(crate) fn tid(label: &str) -> Id {
    ids::sha256(format!("lux:dexvm:test:{label}").as_bytes())
}

/// The network the Go suite writes as `mainnetID`.
pub(crate) const MAINNET_ID: u32 = 1;
