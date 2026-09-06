// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The seam this chain plugs into.
//!
//! `lux_node::vm::{Vm, Block}` are the NODE's own trait definitions, not a copy
//! of them: a chain that restated the seam would compile against a shape the
//! node never sees, and the day the node changed a method the chain would still
//! build.
//!
//! The node holds several chains at once, so the seam is `dyn Vm` — `&self`
//! with the lock inside. The chain itself ([`crate::vm::Vm`]) takes `&mut self`
//! and knows nothing about locking; this file is the one place the two meet.
//! Go arrives at the same arrangement from the other side: its VMs are
//! interfaces with pointer receivers and their own internal mutexes.
//!
//! WHAT F ANSWERS FOR THE TWO ROOTS. It answers neither, and that is the
//! reference's answer rather than an omission here. The F-Chain commits no
//! state root — `fhevm/vm.go` says so and says what stands in its place — and
//! its block id already binds the chain, the parent, the height, the time and
//! every transaction id, so a payload root would be a second name for what the
//! id is. Computing one here would be a rule this chain does not have, and a
//! certificate over an invented root certifies nothing.

use std::sync::Mutex;

use lux_node::vm::{Block as HostBlock, Error as HostError, Id, Vm as HostVm};

use crate::block::Block;
use crate::error::Error;
use crate::id::EMPTY;
use crate::vm::{Config, Vm, NAME, VERSION};

/// A chain behind the lock the node needs.
pub struct Host {
    chain: Mutex<Vm>,
}

impl Host {
    pub fn new(config: Config, genesis: &[u8]) -> crate::error::Result<Host> {
        Ok(Host { chain: Mutex::new(Vm::new(config, genesis)?) })
    }

    /// The chain itself, for a caller that holds this host directly.
    pub fn chain(&self) -> std::sync::MutexGuard<'_, Vm> {
        self.chain.lock().expect("the F-chain lock")
    }
}

/// A refusal, in the node's vocabulary.
///
/// The mapping keeps the two apart on purpose: `Malformed` is "these bytes are
/// not a block of this chain", `Invalid` is "it is a block and it is wrong",
/// and a chain that reported one as the other would have the engine retrying a
/// peer that can never satisfy it.
fn refused(e: Error) -> HostError {
    use crate::error::Code::*;
    match e.code {
        InvalidPayload | InvalidBlock | Zap => HostError::Malformed(e.message()),
        NoPendingTxs => HostError::Empty,
        NotOnTip | NoParentBlock => HostError::NotFound,
        _ => HostError::Invalid(e.message()),
    }
}

impl HostBlock for Block {
    fn id(&self) -> Id {
        Block::id(self)
    }
    fn parent(&self) -> Id {
        Block::parent(self)
    }
    fn height(&self) -> u64 {
        Block::height(self)
    }
    fn timestamp(&self) -> u64 {
        Block::timestamp(self).max(0) as u64
    }
    fn bytes(&self) -> Vec<u8> {
        Block::bytes(self)
    }
    fn state_root(&self) -> Id {
        // F commits none. See this file's header.
        EMPTY
    }
    fn payload_root(&self) -> Id {
        // The block id already names every transaction in it. See the header.
        EMPTY
    }
}

impl HostVm for Host {
    fn name(&self) -> &'static str {
        "F"
    }

    fn version(&self) -> String {
        format!("{NAME}/{VERSION}")
    }

    fn build(&self) -> Result<Box<dyn HostBlock>, HostError> {
        let b = self.chain().build().map_err(refused)?;
        Ok(Box::new(b))
    }

    fn parse(&self, raw: &[u8]) -> Result<Box<dyn HostBlock>, HostError> {
        let b = self.chain().parse(raw).map_err(refused)?;
        Ok(Box::new(b))
    }

    fn get(&self, id: &Id) -> Result<Box<dyn HostBlock>, HostError> {
        match self.chain().block(id).map_err(refused)? {
            Some(b) => Ok(Box::new(b)),
            None => Err(HostError::NotFound),
        }
    }

    fn verify(&self, id: &Id) -> Result<(), HostError> {
        let mut chain = self.chain();
        let b = chain.block(id).map_err(refused)?.ok_or(HostError::NotFound)?;
        chain.verify(&b).map_err(refused)
    }

    fn accept(&self, id: &Id) -> Result<(), HostError> {
        let mut chain = self.chain();
        let b = chain.block(id).map_err(refused)?.ok_or(HostError::NotFound)?;
        chain.accept(&b).map_err(refused)
    }

    fn reject(&self, id: &Id) -> Result<(), HostError> {
        self.chain().reject(id);
        Ok(())
    }

    fn set_preference(&self, _id: &Id) -> Result<(), HostError> {
        // F builds on the accepted tip and admits nothing else, so a preference
        // among in-flight blocks is a statement it has no use for.
        Ok(())
    }

    fn last_accepted(&self) -> Id {
        self.chain().last_accepted()
    }

    fn block_id_at(&self, height: u64) -> Result<Id, HostError> {
        self.chain().block_id_at(height).map_err(|_| HostError::NotFound)
    }

    fn health(&self) -> Result<(), HostError> {
        // A committee is what makes F answerable. With none seated, decryptions
        // can be requested and never fulfilled.
        if self.chain().healthy() {
            Ok(())
        } else {
            Err(HostError::Invalid("no committee is seated".into()))
        }
    }

    fn call(&self, method: &str, params: &serde_json::Value) -> Result<serde_json::Value, HostError> {
        let chain = self.chain();
        let hex32 = |v: &serde_json::Value, k: &str| -> Result<[u8; 32], HostError> {
            let s = v.get(k).and_then(|x| x.as_str()).unwrap_or("");
            let b = crate::id::unhex(s.strip_prefix("0x").unwrap_or(s))
                .ok_or_else(|| HostError::BadRequest(format!("{k} is not hex")))?;
            b.as_slice()
                .try_into()
                .map_err(|_| HostError::BadRequest(format!("{k} is not 32 bytes")))
        };
        match method {
            "fchain.height" => Ok(serde_json::json!(chain.height())),
            "fchain.epoch" => Ok(serde_json::json!(chain.current_epoch().epoch)),
            "fchain.burned" => {
                Ok(serde_json::json!(chain.burned().map_err(refused)?))
            }
            "fchain.balance" => {
                let s = params.get("account").and_then(|x| x.as_str()).unwrap_or("");
                let acct = crate::id::account_from_str(s)
                    .map_err(|e| HostError::BadRequest(e.to_string()))?;
                Ok(serde_json::json!(chain.balance(&acct).map_err(refused)?))
            }
            "fchain.ciphertext" => {
                let h = hex32(params, "handle")?;
                Ok(match chain.ciphertext(&h) {
                    None => serde_json::Value::Null,
                    Some(c) => serde_json::json!({
                        "handle": crate::id::hex(&c.handle),
                        "owner": crate::id::cb58_encode(&c.owner),
                        "scheme": String::from_utf8_lossy(&c.scheme),
                        "digest": crate::id::hex(&c.digest),
                        "size": c.size,
                        "level": c.level,
                        "epoch": c.epoch,
                        "registeredAt": c.registered_at,
                    }),
                })
            }
            other => Err(HostError::NoMethod(other.to_string())),
        }
    }
}
