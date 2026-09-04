// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The seam a chain plugs into.
//!
//! This is the node host's interface, stated here so the chain can be built and
//! tested without the node. It is the same shape, method for method and type
//! for type, as the host's `vm.rs`: `Id` is `[u8; 32]` there and here, `Status`
//! carries the same four numbers, `Error` the same six reasons, and `Block` and
//! `Vm` the same methods in the same order. A host wires this chain in with one
//! `impl` block that forwards each method, the same way it wires its EVM in —
//! the chain crate stays a chain and the host stays the thing that holds
//! several of them.
//!
//! THE DIFFERENCE FROM GO. Go puts `Verify`/`Accept`/`Reject` on the block.
//! Here they are on the VM, keyed by block id. That is not a redesign — it is
//! what the wire already does: those three calls travel as messages carrying an
//! id, because a block handle cannot cross a process boundary.
//!
//! WHY OBJECT-SAFE. A node runs several chains at once and holds them in one
//! place. That means `dyn Vm`, which means no associated types and `&self` with
//! the lock inside.

use std::fmt;

/// What names a block, a transaction, an asset, a chain.
pub type Id = [u8; 32];

/// Where a block stands. The values are Go's `choices` package, so a status
/// crossing the wire means the same thing at both ends.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Status {
    Unknown = 0,
    Processing = 1,
    Rejected = 2,
    Accepted = 3,
}

/// What a chain can refuse for.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// No block by that id.
    NotFound,
    /// The bytes are not a block of this chain.
    Malformed(String),
    /// The block is a block, and it is wrong.
    Invalid(String),
    /// There is nothing to build: no transactions, nothing to say.
    Empty,
    /// The call is not one this chain answers.
    NoMethod(String),
    /// The caller said something this chain cannot use.
    BadRequest(String),
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::NotFound => write!(f, "not found"),
            Error::Malformed(why) => write!(f, "malformed: {why}"),
            Error::Invalid(why) => write!(f, "invalid: {why}"),
            Error::Empty => write!(f, "nothing to build"),
            Error::NoMethod(m) => write!(f, "the method {m} does not exist"),
            Error::BadRequest(why) => write!(f, "{why}"),
        }
    }
}

impl std::error::Error for Error {}

/// One block, as consensus sees it.
///
/// Consensus needs an identity, a parent, a height, a time and the bytes. What
/// a block *means* is the chain's business and is deliberately not here.
pub trait Block: Send + Sync {
    fn id(&self) -> Id;
    fn parent(&self) -> Id;
    fn height(&self) -> u64;
    /// Seconds since the epoch.
    fn timestamp(&self) -> u64;
    /// The block's canonical encoding — what a peer is sent and what
    /// [`Vm::parse`] must accept back.
    fn bytes(&self) -> Vec<u8>;
    /// The root of the state this block leaves behind.
    fn state_root(&self) -> Id;
    /// The root of what this block carries — its transactions.
    fn payload_root(&self) -> Id;
}

/// A chain.
pub trait Vm: Send + Sync {
    /// The chain's letter — `C`, `P`, `X`.
    fn name(&self) -> &'static str;

    /// The version string this chain reports.
    fn version(&self) -> String;

    /// Build a block from whatever is pending. [`Error::Empty`] when there is
    /// nothing.
    fn build(&self) -> Result<Box<dyn Block>, Error>;

    /// Read a block off the wire. Must not need the parent: a bootstrapping
    /// node parses blocks whose parents it does not have yet.
    fn parse(&self, raw: &[u8]) -> Result<Box<dyn Block>, Error>;

    fn get(&self, id: &Id) -> Result<Box<dyn Block>, Error>;

    /// Execute the block and check what it claims against what happened.
    ///
    /// Idempotent: the engine calls this more than once for one block.
    fn verify(&self, id: &Id) -> Result<(), Error>;

    /// Commit it. Called only after a certificate exists, and never before the
    /// parent was accepted.
    fn accept(&self, id: &Id) -> Result<(), Error>;

    /// Drop a block that lost.
    fn reject(&self, id: &Id) -> Result<(), Error>;

    fn set_preference(&self, id: &Id) -> Result<(), Error>;

    fn last_accepted(&self) -> Id;

    fn block_id_at(&self, height: u64) -> Result<Id, Error>;

    fn health(&self) -> Result<(), Error> {
        Ok(())
    }

    /// Answer one JSON-RPC call.
    fn call(&self, method: &str, params: &serde_json::Value) -> Result<serde_json::Value, Error>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_status_is_the_number_go_uses() {
        assert_eq!(Status::Unknown as u8, 0);
        assert_eq!(Status::Processing as u8, 1);
        assert_eq!(Status::Rejected as u8, 2);
        assert_eq!(Status::Accepted as u8, 3);
    }

    #[test]
    fn the_seam_is_object_safe() {
        fn takes(_: &[&dyn Vm]) {}
        takes(&[]);
        fn holds(_: Box<dyn Block>) {}
        let _ = holds as fn(Box<dyn Block>);
    }
}
