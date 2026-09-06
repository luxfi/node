// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The seam this chain plugs into.
//!
//! [`Block`], [`Vm`], [`Error`], [`Status`] and [`Id`] are the NODE's own
//! definitions, re-exported rather than restated: a chain that wrote the seam
//! down again would compile against a shape the node never sees, and the day
//! the node changed a method the chain would still build.
//!
//! The node holds several chains at once, so the seam is `dyn Vm` — `&self`
//! with the lock inside. The chain itself ([`crate::vm::Zvm`]) takes `&mut
//! self` and knows nothing about locking; [`Host`] is the one place the two
//! meet. Go arrives at the same arrangement from the other side: its VMs are
//! interfaces with pointer receivers and their own internal mutexes.
//!
//! `impl Block for crate::block::Block` lives beside the block itself, in
//! [`crate::block`], because what a Z block answers about its own roots is a
//! fact about the block and not about the seam.

use std::sync::Mutex;

pub use lux_node::vm::{Block, Error, Id, Status, Vm};

use crate::config::Config;
use crate::ids;
use crate::vm::{Zvm, NAME, VERSION};

/// A chain behind the lock the node needs.
pub struct Host {
    chain: Mutex<Zvm>,
}

impl Host {
    pub fn new(cfg: Config, chain_id: ids::Id, network_id: u32, genesis: &[u8]) -> crate::Result<Host> {
        Ok(Host {
            chain: Mutex::new(Zvm::new(cfg, chain_id, network_id, genesis)?),
        })
    }

    /// The chain itself, for a caller that holds this host directly.
    pub fn chain(&self) -> std::sync::MutexGuard<'_, Zvm> {
        self.chain.lock().expect("the Z-chain lock")
    }
}

impl Vm for Host {
    fn name(&self) -> &'static str {
        "Z"
    }

    fn version(&self) -> String {
        format!("{NAME}/{VERSION}")
    }

    fn build(&self) -> Result<Box<dyn Block>, Error> {
        let b = self.chain().build()?;
        Ok(Box::new(b))
    }

    fn parse(&self, raw: &[u8]) -> Result<Box<dyn Block>, Error> {
        let b = self.chain().parse_block(raw)?;
        Ok(Box::new(b))
    }

    fn get(&self, id: &Id) -> Result<Box<dyn Block>, Error> {
        let b = self.chain().block(id)?;
        Ok(Box::new(b))
    }

    fn verify(&self, id: &Id) -> Result<(), Error> {
        let mut chain = self.chain();
        let b = chain.block(id)?;
        Ok(chain.verify(&b)?)
    }

    fn accept(&self, id: &Id) -> Result<(), Error> {
        let mut chain = self.chain();
        let b = chain.block(id)?;
        Ok(chain.accept(&b)?)
    }

    fn reject(&self, id: &Id) -> Result<(), Error> {
        self.chain().reject(id);
        Ok(())
    }

    fn set_preference(&self, id: &Id) -> Result<(), Error> {
        self.chain().prefer(*id);
        Ok(())
    }

    fn last_accepted(&self) -> Id {
        self.chain().last_accepted()
    }

    fn block_id_at(&self, height: u64) -> Result<Id, Error> {
        self.chain().block_id_at(height).map_err(|_| Error::NotFound)
    }

    fn call(&self, method: &str, params: &serde_json::Value) -> Result<serde_json::Value, Error> {
        let chain = self.chain();
        match method {
            "zchain.height" => Ok(serde_json::json!(chain.height())),
            "zchain.stateRoot" => Ok(serde_json::json!(ids::hex_of(chain.state_root()))),
            "zchain.nullifiers" => Ok(serde_json::json!(chain.nullifier_count())),
            "zchain.outputs" => Ok(serde_json::json!(chain.output_count())),
            "zchain.spent" => {
                let s = params.get("nullifier").and_then(|x| x.as_str()).unwrap_or("");
                let n = unhex(s.strip_prefix("0x").unwrap_or(s))
                    .ok_or_else(|| Error::BadRequest("nullifier is not hex".into()))?;
                Ok(serde_json::json!(chain.spent_at(&n)?))
            }
            other => Err(Error::NoMethod(other.to_string())),
        }
    }
}

fn unhex(s: &str) -> Option<Vec<u8>> {
    if s.len() % 2 != 0 {
        return None;
    }
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(s.len() / 2);
    for pair in b.chunks(2) {
        let hi = (pair[0] as char).to_digit(16)?;
        let lo = (pair[1] as char).to_digit(16)?;
        out.push((hi * 16 + lo) as u8);
    }
    Some(out)
}
