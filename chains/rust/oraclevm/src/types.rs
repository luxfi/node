// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the O-chain holds, and how each of them travels.
//!
//! Every struct here is the Go struct of the same name, member for member and
//! in the same order, because the ORDER is the wire: a block id is the SHA-256
//! of the block marshalled again, so a field written out of place names a
//! different block.
//!
//! An OracleAttestation is NOT here. A block may carry a list of them and this
//! port does not model one; no vector in the corpus carries one, and a block
//! that did would be refused rather than silently re-marshalled without them.
//! That is the port's known boundary and it is stated here rather than left to
//! be discovered.

use crate::gojson::{self as gj, Writer};
use crate::gotime::Time;
use crate::ids::{self, Id, NodeId};
use serde_json::Value;

pub type Error = String;

#[derive(Clone, Debug)]
pub struct Observation {
    pub feed_id: Id,
    pub value: Option<Vec<u8>>,
    pub timestamp: Time,
    pub source_meta: [u8; 32],
    pub operator_id: NodeId,
    pub scheme: u8,
    pub signature: Option<Vec<u8>>,
}

impl Observation {
    pub fn read(v: &Value) -> Result<Observation, Error> {
        let o = gj::object(v)?;
        Ok(Observation {
            feed_id: gj::id_of(o, "feedId")?,
            value: gj::bytes_of(o, "value")?,
            timestamp: gj::time_of(o, "timestamp")?,
            source_meta: gj::byte_array_of(o, "sourceMetaHash")?,
            operator_id: gj::node_id_of(o, "operatorId")?,
            scheme: gj::u8_of(o, "scheme")?,
            signature: gj::bytes_of(o, "signature")?,
        })
    }

    pub fn write(&self) -> String {
        let mut w = Writer::object();
        w.id("feedId", &self.feed_id);
        w.bytes("value", &self.value);
        w.time("timestamp", &self.timestamp);
        w.byte_array("sourceMetaHash", &self.source_meta);
        w.node_id("operatorId", &self.operator_id);
        w.unum("scheme", self.scheme as u64);
        w.bytes("signature", &self.signature);
        w.finish()
    }
}

#[derive(Clone, Debug)]
pub struct AggregatedValue {
    pub feed_id: Id,
    pub epoch: u64,
    pub value: Option<Vec<u8>>,
    pub timestamp: Time,
    pub observations: i64,
    pub agg_proof: Option<Vec<u8>>,
    pub quorum_cert: Option<Vec<u8>>,
}

impl AggregatedValue {
    pub fn read(v: &Value) -> Result<AggregatedValue, Error> {
        let o = gj::object(v)?;
        Ok(AggregatedValue {
            feed_id: gj::id_of(o, "feedId")?,
            epoch: gj::u64_of(o, "epoch")?,
            value: gj::bytes_of(o, "value")?,
            timestamp: gj::time_of(o, "timestamp")?,
            observations: gj::i64_of(o, "observationCount")?,
            agg_proof: gj::bytes_of(o, "aggProof")?,
            quorum_cert: gj::bytes_of(o, "quorumCert")?,
        })
    }

    pub fn write(&self) -> String {
        let mut w = Writer::object();
        w.id("feedId", &self.feed_id);
        w.unum("epoch", self.epoch);
        w.bytes("value", &self.value);
        w.time("timestamp", &self.timestamp);
        w.num("observationCount", self.observations);
        // omitempty: a nil OR empty slice is not written at all.
        if let Some(b) = &self.agg_proof {
            if !b.is_empty() {
                w.bytes("aggProof", &self.agg_proof);
            }
        }
        if let Some(b) = &self.quorum_cert {
            if !b.is_empty() {
                w.bytes("quorumCert", &self.quorum_cert);
            }
        }
        w.finish()
    }
}

#[derive(Clone, Debug)]
pub struct Feed {
    pub id: Id,
    pub name: String,
    pub description: String,
    pub sources: Option<Vec<String>>,
    /// A `time.Duration`, which is an int64 count of nanoseconds on the wire.
    pub update_freq: i64,
    pub policy_hash: [u8; 32],
    pub operators: Option<Vec<NodeId>>,
    pub created_at: Time,
    pub status: String,
    pub metadata: Option<Vec<(String, String)>>,
}

impl Feed {
    pub fn read(v: &Value) -> Result<Feed, Error> {
        let o = gj::object(v)?;
        Ok(Feed {
            id: gj::id_of(o, "id")?,
            name: gj::string_of(o, "name")?,
            description: gj::string_of(o, "description")?,
            sources: gj::strings_of(o, "sources")?,
            update_freq: gj::i64_of(o, "updateFreq")?,
            policy_hash: gj::byte_array_of(o, "policyHash")?,
            operators: gj::node_ids_of(o, "operators")?,
            created_at: gj::time_of(o, "createdAt")?,
            status: gj::string_of(o, "status")?,
            metadata: gj::map_of(o, "metadata")?,
        })
    }

    pub fn write(&self) -> String {
        let mut w = Writer::object();
        w.id("id", &self.id);
        w.text("name", &self.name);
        w.text("description", &self.description);
        w.strings("sources", &self.sources);
        w.num("updateFreq", self.update_freq);
        w.byte_array("policyHash", &self.policy_hash);
        w.node_ids("operators", &self.operators);
        w.time("createdAt", &self.created_at);
        w.text("status", &self.status);
        w.map("metadata", &self.metadata);
        w.finish()
    }

    pub fn admits(&self, op: &NodeId) -> bool {
        match &self.operators {
            None => false,
            Some(ops) => ops.iter().any(|o| o == op),
        }
    }
}

#[derive(Clone, Debug)]
pub struct Block {
    pub id: Id,
    pub parent_id: Id,
    pub height: u64,
    pub timestamp: Time,
    pub observations: Option<Vec<Observation>>,
    pub aggregations: Option<Vec<AggregatedValue>>,
    pub feed_updates: Option<Vec<Feed>>,
}

impl Block {
    pub fn read(v: &Value) -> Result<Block, Error> {
        let o = gj::object(v)?;
        if gj::member(o, "attestations")
            .map(|m| !m.is_null())
            .unwrap_or(false)
        {
            return Err("oraclevm: this port does not model an attestation".into());
        }
        let mut obs = None;
        if let Some(a) = gj::array_of(o, "observations")? {
            let mut items = Vec::with_capacity(a.len());
            for e in a {
                items.push(Observation::read(e)?);
            }
            obs = Some(items);
        }
        let mut agg = None;
        if let Some(a) = gj::array_of(o, "aggregations")? {
            let mut items = Vec::with_capacity(a.len());
            for e in a {
                items.push(AggregatedValue::read(e)?);
            }
            agg = Some(items);
        }
        let mut feeds = None;
        if let Some(a) = gj::array_of(o, "feedUpdates")? {
            let mut items = Vec::with_capacity(a.len());
            for e in a {
                items.push(Feed::read(e)?);
            }
            feeds = Some(items);
        }
        Ok(Block {
            id: gj::id_of(o, "id")?,
            parent_id: gj::id_of(o, "parentID")?,
            height: gj::u64_of(o, "height")?,
            timestamp: gj::time_of(o, "timestamp")?,
            observations: obs,
            aggregations: agg,
            feed_updates: feeds,
        })
    }

    /// The bytes `Marshal` writes for this block. The id is the SHA-256 of
    /// exactly these, which is why `omitempty` is spelled out rather than
    /// approximated: an empty list is not written, so a block that arrived
    /// carrying one is re-marshalled without it and takes the id of the block
    /// that never had it.
    pub fn write(&self) -> String {
        let mut w = Writer::object();
        w.id("id", &self.id);
        w.id("parentID", &self.parent_id);
        w.unum("height", self.height);
        w.time("timestamp", &self.timestamp);
        if let Some(items) = &self.observations {
            if !items.is_empty() {
                let bodies: Vec<String> = items.iter().map(Observation::write).collect();
                w.list("observations", &bodies);
            }
        }
        if let Some(items) = &self.aggregations {
            if !items.is_empty() {
                let bodies: Vec<String> = items.iter().map(AggregatedValue::write).collect();
                w.list("aggregations", &bodies);
            }
        }
        if let Some(items) = &self.feed_updates {
            if !items.is_empty() {
                let bodies: Vec<String> = items.iter().map(Feed::write).collect();
                w.list("feedUpdates", &bodies);
            }
        }
        w.finish()
    }

    /// The id the chain derives: parse, marshal again, hash THAT. Not the hash
    /// of the bytes it was handed.
    pub fn compute_id(&self) -> Id {
        ids::sha256(self.write().as_bytes())
    }

    pub fn count(&self) -> (usize, usize, usize) {
        (
            self.observations.as_ref().map_or(0, Vec::len),
            self.aggregations.as_ref().map_or(0, Vec::len),
            self.feed_updates.as_ref().map_or(0, Vec::len),
        )
    }
}

#[derive(Clone, Debug)]
pub struct Genesis {
    pub version: i64,
    pub message: String,
    pub timestamp: i64,
    pub initial_feeds: Option<Vec<Feed>>,
}

impl Genesis {
    pub fn read(v: &Value) -> Result<Genesis, Error> {
        let o = gj::object(v)?;
        let mut feeds = None;
        if let Some(a) = gj::array_of(o, "initialFeeds")? {
            let mut items = Vec::with_capacity(a.len());
            for e in a {
                items.push(Feed::read(e)?);
            }
            feeds = Some(items);
        }
        Ok(Genesis {
            version: gj::i64_of(o, "version")?,
            message: gj::string_of(o, "message")?,
            timestamp: gj::i64_of(o, "timestamp")?,
            initial_feeds: feeds,
        })
    }

    pub fn write(&self) -> String {
        let mut w = Writer::object();
        w.num("version", self.version);
        w.text("message", &self.message);
        w.num("timestamp", self.timestamp);
        if let Some(items) = &self.initial_feeds {
            if !items.is_empty() {
                let bodies: Vec<String> = items.iter().map(Feed::write).collect();
                w.list("initialFeeds", &bodies);
            }
        }
        w.finish()
    }
}

pub const KIND_WRITE: u8 = 0;
pub const KIND_READ: u8 = 1;

#[derive(Clone, Debug)]
pub struct OracleRequest {
    pub request_id: [u8; 32],
    pub service_id: Id,
    pub session_id: Id,
    pub step: u32,
    pub retry: u32,
    pub tx_id: Id,
    pub kind: u8,
    pub target: Option<Vec<u8>>,
    pub payload_hash: [u8; 32],
    pub schema_hash: [u8; 32],
    pub deadline_height: u64,
    pub executors: Option<Vec<NodeId>>,
    pub created_at: Time,
    pub status: u8,
}

impl OracleRequest {
    pub fn read(v: &Value) -> Result<OracleRequest, Error> {
        let o = gj::object(v)?;
        Ok(OracleRequest {
            request_id: gj::byte_array_of(o, "requestId")?,
            service_id: gj::id_of(o, "serviceId")?,
            session_id: gj::id_of(o, "sessionId")?,
            step: gj::u32_of(o, "step")?,
            retry: gj::u32_of(o, "retry")?,
            tx_id: gj::id_of(o, "txId")?,
            kind: gj::u8_of(o, "kind")?,
            target: gj::bytes_of(o, "target")?,
            payload_hash: gj::byte_array_of(o, "payloadHash")?,
            schema_hash: gj::byte_array_of(o, "schemaHash")?,
            deadline_height: gj::u64_of(o, "deadlineHeight")?,
            executors: gj::node_ids_of(o, "executors")?,
            created_at: gj::time_of(o, "createdAt")?,
            status: gj::u8_of(o, "status")?,
        })
    }

    pub fn admits(&self, executor: &NodeId) -> bool {
        match &self.executors {
            None => false,
            Some(ex) => ex.iter().any(|e| e == executor),
        }
    }
}

#[derive(Clone, Debug)]
pub struct OracleRecord {
    pub request_id: [u8; 32],
    pub executor: NodeId,
    pub timestamp: u64,
    pub endpoint: String,
    pub body_hash: [u8; 32],
    pub result_code: u32,
    pub external_ref: Option<Vec<u8>>,
    pub scheme: u8,
    pub signature: Option<Vec<u8>>,
}

impl OracleRecord {
    pub fn read(v: &Value) -> Result<OracleRecord, Error> {
        let o = gj::object(v)?;
        Ok(OracleRecord {
            request_id: gj::byte_array_of(o, "requestId")?,
            executor: gj::node_id_of(o, "executor")?,
            timestamp: gj::u64_of(o, "timestamp")?,
            endpoint: gj::string_of(o, "endpoint")?,
            body_hash: gj::byte_array_of(o, "bodyHash")?,
            result_code: gj::u32_of(o, "resultCode")?,
            external_ref: gj::bytes_of(o, "externalRef")?,
            scheme: gj::u8_of(o, "scheme")?,
            signature: gj::bytes_of(o, "signature")?,
        })
    }
}
