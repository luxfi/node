// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the O-chain decides.
//!
//! Three planes, and they refuse for different reasons:
//!
//!   - an OBSERVATION is offered against a feed. The chain looks the feed up,
//!     then reads its own clock, then asks whether the operator is one the
//!     feed names.
//!   - a REQUEST names itself, and the name has to be the one its own
//!     arguments derive. That check runs BEFORE the chain is read, which is
//!     what puts it on the syntactic side of the corpus's boundary.
//!   - a RECORD is executed against a registered request by an executor the
//!     request names, and the COMMITMENT over a set of records is a Merkle
//!     root — the one number a light client checks an oracle answer against.
//!
//! `Block::verify` is not here because the chain does not have one: the Go
//! reference's `Verify` is `return nil` with no condition in it, and a port
//! that invented a rule would fail the differential for being right.

use sha2::{Digest, Sha256};
use std::collections::HashMap;

use crate::ids::{self, Id, NodeId};
use crate::types::{Feed, OracleRecord, OracleRequest, Observation};

/// The window an observation must be inside, in seconds. It is the chain's
/// default configuration — `ObservationWindow: "1m"` — and it is read against
/// the wall clock, so an observation with a fixed past timestamp is stale
/// forever and one dated far ahead is fresh.
pub const OBSERVATION_WINDOW_SECONDS: i64 = 60;

/// `request_id = sha256("LUX:OracleRequest:v1" ‖ service ‖ session ‖ be32(step)
/// ‖ be32(retry) ‖ tx)`.
///
/// Nothing separates the three ids, so their ORDER is the whole of what keeps
/// two requests apart: an implementation that wrote session before service
/// would derive one chain's names for another chain's requests. The two
/// counters are big-endian and they are NOT interchangeable, which is why the
/// preimage puts step first and retry second and both are written out here
/// rather than looped.
pub fn compute_request_id(service: &Id, session: &Id, tx: &Id, step: u32, retry: u32) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"LUX:OracleRequest:v1");
    h.update(service);
    h.update(session);
    h.update(step.to_be_bytes());
    h.update(retry.to_be_bytes());
    h.update(tx);
    let mut out = [0u8; 32];
    out.copy_from_slice(&h.finalize());
    out
}

/// The leaf a record hashes to. The signature is NOT in it: two records that
/// differ only there commit to one root, which is the chain's answer and has
/// to be every implementation's.
fn record_leaf(r: &OracleRecord) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(r.request_id);
    h.update(r.executor);
    h.update(r.timestamp.to_be_bytes());
    h.update(r.endpoint.as_bytes());
    h.update(r.body_hash);
    h.update(r.result_code.to_be_bytes());
    if let Some(b) = &r.external_ref {
        h.update(b);
    }
    let mut out = [0u8; 32];
    out.copy_from_slice(&h.finalize());
    out
}

/// The root over a request's records.
///
/// A lone last leaf is paired WITH ITSELF, so a set of records and that set
/// with its last member repeated commit to the same root. That is the chain's
/// tree and this port reproduces it rather than correcting it: a port that
/// fixed the malleability here would derive a different root for every odd
/// record count and fork the chain in the act of improving it.
pub fn records_merkle_root(records: &[OracleRecord]) -> [u8; 32] {
    if records.is_empty() {
        return [0u8; 32];
    }
    let mut level: Vec<[u8; 32]> = records.iter().map(record_leaf).collect();
    while level.len() > 1 {
        let mut next = Vec::with_capacity(level.len().div_ceil(2));
        let mut i = 0;
        while i < level.len() {
            let mut h = Sha256::new();
            h.update(level[i]);
            if i + 1 < level.len() {
                h.update(level[i + 1]);
            } else {
                h.update(level[i]);
            }
            let mut out = [0u8; 32];
            out.copy_from_slice(&h.finalize());
            next.push(out);
            i += 2;
        }
        level = next;
    }
    level[0]
}

pub struct Commit {
    pub root: [u8; 32],
    pub count: u32,
    pub window_start: u64,
    pub window_end: u64,
}

/// An O-chain holding the feeds its genesis named and nothing else.
pub struct Vm {
    feeds: HashMap<Id, Feed>,
    requests: HashMap<[u8; 32], OracleRequest>,
    records: HashMap<[u8; 32], Vec<OracleRecord>>,
    /// The height of the last accepted block. A chain seeded from genesis and
    /// nothing else is at zero, which is what the deadline rule reads.
    pub last_height: u64,
}

impl Vm {
    pub fn seeded(feeds: &[Feed]) -> Vm {
        let mut m = HashMap::new();
        for f in feeds {
            m.insert(f.id, f.clone());
        }
        Vm { feeds: m, requests: HashMap::new(), records: HashMap::new(), last_height: 0 }
    }

    /// The clock the staleness rule reads, in seconds since the epoch.
    fn now(&self) -> i64 {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0)
    }

    /// The feed lookup is the chain's FIRST read, so everything this method
    /// can refuse is past the corpus's syntactic boundary.
    pub fn submit_observation(&mut self, obs: &Observation) -> Result<(), String> {
        let feed = match self.feeds.get(&obs.feed_id) {
            None => return Err("feed not found".into()),
            Some(f) => f,
        };
        if self.now() - obs.timestamp.unix() > OBSERVATION_WINDOW_SECONDS {
            return Err("stale observation".into());
        }
        if !feed.admits(&obs.operator_id) {
            return Err(format!(
                "operator {} not authorized for feed {}",
                ids::node_id_string(&obs.operator_id),
                feed.name
            ));
        }
        Ok(())
    }

    /// The deterministic-id check runs before the request map is read, which
    /// is why it is the one refusal here that answers the syntactic field too.
    pub fn register_request(&mut self, req: &OracleRequest) -> Result<(), String> {
        let expected = compute_request_id(
            &req.service_id,
            &req.session_id,
            &req.tx_id,
            req.step,
            req.retry,
        );
        if expected != req.request_id {
            return Err(format!(
                "invalid request_id: expected {}, got {}",
                hex::encode(expected),
                hex::encode(req.request_id)
            ));
        }
        if self.requests.contains_key(&req.request_id) {
            return Err(format!("request {} already exists", hex::encode(req.request_id)));
        }
        self.requests.insert(req.request_id, req.clone());
        self.records.insert(req.request_id, Vec::new());
        Ok(())
    }

    pub fn submit_record(&mut self, rec: &OracleRecord) -> Result<(), String> {
        let req = match self.requests.get(&rec.request_id) {
            None => {
                return Err(format!("request {} not found", hex::encode(rec.request_id)))
            }
            Some(r) => r,
        };
        if !req.admits(&rec.executor) {
            return Err(format!(
                "executor {} not authorized for request {}",
                ids::node_id_string(&rec.executor),
                hex::encode(rec.request_id)
            ));
        }
        if self.last_height > req.deadline_height {
            return Err(format!("request {} has expired", hex::encode(rec.request_id)));
        }
        self.records.entry(rec.request_id).or_default().push(rec.clone());
        Ok(())
    }

    pub fn commit_records(&mut self, request_id: &[u8; 32]) -> Result<Commit, String> {
        if !self.requests.contains_key(request_id) {
            return Err(format!("request {} not found", hex::encode(request_id)));
        }
        let records = self.records.get(request_id).cloned().unwrap_or_default();
        if records.is_empty() {
            return Err(format!("no records for request {}", hex::encode(request_id)));
        }
        let root = records_merkle_root(&records);
        // The window walks the records the way the chain walks them, zero
        // included: a record timestamped zero does not open the window,
        // because the start is only replaced while it is still zero.
        let mut start: u64 = 0;
        let mut end: u64 = 0;
        for r in &records {
            if start == 0 || r.timestamp < start {
                start = r.timestamp;
            }
            if r.timestamp > end {
                end = r.timestamp;
            }
        }
        Ok(Commit { root, count: records.len() as u32, window_start: start, window_end: end })
    }
}

/// The operator a feed names, rendered — used only in refusal messages, which
/// the differential carries beside the verdict rather than comparing.
pub fn operator_word(n: &NodeId) -> String {
    ids::node_id_string(n)
}
