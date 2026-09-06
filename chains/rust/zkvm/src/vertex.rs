// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The other shape this chain decides in.
//!
//! A vertex carries transactions like a block, and differs in what it says
//! about its place: several parents, an epoch, and no timestamp. Two vertices
//! CONFLICT exactly when their nullifier sets intersect — one shielded note
//! cannot be spent by both.
//!
//! A VERTEX IS HELD TO WHAT A BLOCK IS HELD TO. It once checked its
//! transactions and nothing else — not the parents, not the height — so a
//! vertex naming no parent at height 2^40 verified, and accepting it set the
//! store's height to 2^40, pruned every block in flight, and left the linear
//! chain unable to propose a child ever again.
//!
//! The encoding here is NOT ZAP. It is the reference's own big-endian framing,
//! reproduced because these bytes are what the store writes to disk and what
//! the id is derived beside. It carries the same rule the ZAP frames do — every
//! byte handed in belongs to the vertex — for the same reason: without it,
//! trailing bytes ride along in what the store writes, one logical vertex has
//! unboundedly many encodings under one id, and a peer can park megabytes under
//! a legitimate one.

use std::collections::BTreeSet;

use crate::error::{Error, Result};
use crate::hash::Fold;
use crate::host;
use crate::ids::{self, Id};
use crate::txs::Tx;
use crate::wire;

/// A vertex's identity: the chain, its place, and what it carries.
pub fn compute_id(bind: &Id, height: u64, epoch: u32, parents: &[Id], txs: &[Tx]) -> Id {
    let mut h = Fold::new();
    h.raw(bind).num(height).num32(epoch);
    for p in parents {
        h.raw(p);
    }
    for tx in txs {
        h.raw(&tx.id());
    }
    h.id()
}

/// A DAG vertex.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Vertex {
    height: u64,
    epoch: u32,
    parents: Vec<Id>,
    txs: Vec<Tx>,
    id: Id,
    bytes: Vec<u8>,
    status: host::Status,
}

impl Vertex {
    /// A vertex this node built.
    pub fn new(bind: &Id, height: u64, epoch: u32, parents: Vec<Id>, txs: Vec<Tx>) -> Vertex {
        let id = compute_id(bind, height, epoch, &parents, &txs);
        let bytes = encode(height, epoch, &parents, &txs);
        Vertex {
            height,
            epoch,
            parents,
            txs,
            id,
            bytes,
            status: host::Status::Processing,
        }
    }

    /// A vertex off the wire.
    pub fn parse(bind: &Id, data: &[u8]) -> Result<Vertex> {
        let (height, epoch, parents, txs) = decode(data)?;
        let id = compute_id(bind, height, epoch, &parents, &txs);
        Ok(Vertex {
            height,
            epoch,
            parents,
            txs,
            id,
            bytes: data.to_vec(),
            status: host::Status::Unknown,
        })
    }

    pub fn id(&self) -> Id {
        self.id
    }

    pub fn height(&self) -> u64 {
        self.height
    }

    pub fn epoch(&self) -> u32 {
        self.epoch
    }

    pub fn parents(&self) -> &[Id] {
        &self.parents
    }

    pub fn txs(&self) -> &[Tx] {
        &self.txs
    }

    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    pub fn status(&self) -> host::Status {
        self.status
    }

    pub fn set_status(&mut self, s: host::Status) {
        self.status = s;
    }

    /// Everything a vertex can be refused for without reading the chain, given
    /// the frontier it claims to extend.
    ///
    /// The store keeps ONE tip for both shapes, so a vertex that names anything
    /// else moves the chain sideways.
    pub fn shape(&self, cap: u32, tip: &Id, tip_height: u64) -> Result<()> {
        if self.txs.len() as u64 > u64::from(cap) {
            return Err(Error::TxCap {
                txs: self.txs.len(),
                cap,
            });
        }
        if self.parents.len() != 1 || self.parents[0] != *tip {
            return Err(Error::NotOnTip(format!(
                "vertex parents {:?} do not name the tip {}",
                self.parents.iter().map(ids::short).collect::<Vec<_>>(),
                ids::hex(tip)
            )));
        }
        if self.height != tip_height + 1 {
            return Err(Error::InvalidHeight);
        }
        let mut here: BTreeSet<&[u8]> = BTreeSet::new();
        for tx in &self.txs {
            for n in &tx.nullifiers {
                if !here.insert(n.as_slice()) {
                    return Err(Error::DuplicateNullifier);
                }
            }
        }
        Ok(())
    }

    /// The nullifiers this vertex spends. Its conflict key.
    pub fn nullifiers(&self) -> BTreeSet<&[u8]> {
        self.txs
            .iter()
            .flat_map(|tx| tx.nullifiers.iter().map(|n| n.as_slice()))
            .collect()
    }

    /// Whether this vertex and `other` spend a note in common.
    pub fn conflicts(&self, other: &Vertex) -> bool {
        let ours = self.nullifiers();
        other.nullifiers().iter().any(|n| ours.contains(n))
    }
}

/// `height ‖ epoch ‖ parent count ‖ parents ‖ tx count ‖ (length ‖ tx)*`, all
/// big-endian.
fn encode(height: u64, epoch: u32, parents: &[Id], txs: &[Tx]) -> Vec<u8> {
    let mut out = Vec::with_capacity(8 + 4 + 4 + parents.len() * ids::ID_LEN + 4 + txs.len() * 64);
    out.extend_from_slice(&height.to_be_bytes());
    out.extend_from_slice(&epoch.to_be_bytes());
    out.extend_from_slice(&(parents.len() as u32).to_be_bytes());
    for p in parents {
        out.extend_from_slice(p);
    }
    out.extend_from_slice(&(txs.len() as u32).to_be_bytes());
    for tx in txs {
        let raw = wire::write_tx(tx);
        out.extend_from_slice(&(raw.len() as u32).to_be_bytes());
        out.extend_from_slice(&raw);
    }
    out
}

/// The counts are attacker-controlled, so each is bounded by the bytes that
/// remain BEFORE anything is allocated: a 16-byte vertex claiming 2^32-1
/// parents would otherwise ask for 128 GiB and take the node with it.
#[allow(clippy::type_complexity)]
fn decode(data: &[u8]) -> Result<(u64, u32, Vec<Id>, Vec<Tx>)> {
    if data.len() < 16 {
        return Err(Error::InvalidBlock);
    }
    let mut pos = 0usize;
    let take = |pos: &mut usize, n: usize| -> &[u8] {
        let s = &data[*pos..*pos + n];
        *pos += n;
        s
    };

    let height = u64::from_be_bytes(take(&mut pos, 8).try_into().unwrap());
    let epoch = u32::from_be_bytes(take(&mut pos, 4).try_into().unwrap());

    let parent_count = u32::from_be_bytes(take(&mut pos, 4).try_into().unwrap()) as usize;
    if parent_count > (data.len() - pos) / ids::ID_LEN {
        return Err(Error::InvalidBlock);
    }
    let mut parents = Vec::with_capacity(parent_count);
    for _ in 0..parent_count {
        parents.push(ids::from_slice(take(&mut pos, ids::ID_LEN)));
    }

    if pos + 4 > data.len() {
        return Err(Error::InvalidBlock);
    }
    let tx_count = u32::from_be_bytes(take(&mut pos, 4).try_into().unwrap()) as usize;
    // Every transaction costs at least its four-byte length prefix.
    if tx_count > (data.len() - pos) / 4 {
        return Err(Error::InvalidBlock);
    }
    let mut txs = Vec::with_capacity(tx_count);
    for _ in 0..tx_count {
        if pos + 4 > data.len() {
            return Err(Error::InvalidBlock);
        }
        let len = u32::from_be_bytes(take(&mut pos, 4).try_into().unwrap()) as usize;
        if pos + len > data.len() {
            return Err(Error::InvalidBlock);
        }
        txs.push(wire::read_tx(take(&mut pos, len))?);
    }

    if pos != data.len() {
        return Err(Error::TrailingBytes);
    }
    Ok((height, epoch, parents, txs))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::block::binding;
    use crate::txs::{Kind, Proof, Shielded, Tx};

    fn bind() -> Id {
        binding(&ids::repeated(40), 1)
    }

    fn transfer(nullifier: u8) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            version: 1,
            nullifiers: vec![vec![nullifier; 32]],
            outputs: vec![Shielded {
                commitment: vec![0x50; 32],
                ..Shielded::default()
            }],
            proof: Some(Proof {
                system: b"stark".to_vec(),
                data: vec![0x60; 192],
                public: vec![vec![0x61; 32]],
            }),
            fee: 1,
            expiry: 1000,
            ..Tx::default()
        }
    }

    fn vertex(height: u64, parent: Id, txs: Vec<Tx>) -> Vertex {
        Vertex::new(&bind(), height, 0, vec![parent], txs)
    }

    #[test]
    fn a_vertex_round_trips_through_its_own_framing() {
        for txs in [vec![], vec![transfer(1)], vec![transfer(1), transfer(2)]] {
            let v = vertex(1, ids::repeated(9), txs);
            let back = Vertex::parse(&bind(), v.bytes()).unwrap();
            assert_eq!(back.id(), v.id());
            assert_eq!(back.height(), v.height());
            assert_eq!(back.epoch(), v.epoch());
            assert_eq!(back.parents(), v.parents());
            assert_eq!(back.txs(), v.txs());
        }
    }

    #[test]
    fn every_field_is_in_the_identity() {
        let base = compute_id(&bind(), 1, 0, &[ids::repeated(9)], &[transfer(1)]);
        assert_ne!(base, compute_id(&bind(), 2, 0, &[ids::repeated(9)], &[transfer(1)]));
        assert_ne!(base, compute_id(&bind(), 1, 1, &[ids::repeated(9)], &[transfer(1)]));
        assert_ne!(base, compute_id(&bind(), 1, 0, &[ids::repeated(8)], &[transfer(1)]));
        assert_ne!(base, compute_id(&bind(), 1, 0, &[ids::repeated(9)], &[transfer(2)]));
        assert_ne!(
            base,
            compute_id(&binding(&ids::repeated(41), 1), 1, 0, &[ids::repeated(9)], &[transfer(1)])
        );
    }

    #[test]
    fn bytes_past_the_end_are_refused_rather_than_carried_along() {
        let v = vertex(1, ids::repeated(9), vec![transfer(1)]);
        let mut raw = v.bytes().to_vec();
        raw.push(0xFF);
        assert_eq!(Vertex::parse(&bind(), &raw), Err(Error::TrailingBytes));
    }

    #[test]
    fn a_truncated_vertex_is_refused_at_every_cut() {
        let v = vertex(1, ids::repeated(9), vec![transfer(1)]);
        let raw = v.bytes();
        for cut in [0, 8, 15, 16, raw.len() / 2, raw.len() - 1] {
            assert!(Vertex::parse(&bind(), &raw[..cut]).is_err(), "cut at {cut}");
        }
    }

    /// The count is bounded before anything is allocated.
    #[test]
    fn an_absurd_parent_count_allocates_nothing() {
        let mut raw = Vec::new();
        raw.extend_from_slice(&1u64.to_be_bytes());
        raw.extend_from_slice(&0u32.to_be_bytes());
        raw.extend_from_slice(&u32::MAX.to_be_bytes());
        assert_eq!(Vertex::parse(&bind(), &raw), Err(Error::InvalidBlock));
    }

    #[test]
    fn an_absurd_transaction_count_allocates_nothing() {
        let mut raw = Vec::new();
        raw.extend_from_slice(&1u64.to_be_bytes());
        raw.extend_from_slice(&0u32.to_be_bytes());
        raw.extend_from_slice(&0u32.to_be_bytes());
        raw.extend_from_slice(&u32::MAX.to_be_bytes());
        assert_eq!(Vertex::parse(&bind(), &raw), Err(Error::InvalidBlock));
    }

    /// The rule the vertex once did not have. A vertex naming no parent, at a
    /// height nothing reached, is refused.
    #[test]
    fn a_vertex_that_names_no_parent_is_refused() {
        let v = Vertex::new(&bind(), 1 << 40, 0, vec![], vec![]);
        assert!(matches!(
            v.shape(100, &ids::repeated(9), 0),
            Err(Error::NotOnTip(_))
        ));
    }

    #[test]
    fn a_vertex_extends_the_tip_and_the_height_above_it() {
        let tip = ids::repeated(9);
        assert_eq!(vertex(1, tip, vec![]).shape(100, &tip, 0), Ok(()));
        assert_eq!(
            vertex(5, tip, vec![]).shape(100, &tip, 0),
            Err(Error::InvalidHeight)
        );
        assert!(matches!(
            vertex(1, ids::repeated(3), vec![]).shape(100, &tip, 0),
            Err(Error::NotOnTip(_))
        ));
    }

    #[test]
    fn a_vertex_over_the_cap_is_refused() {
        let tip = ids::repeated(9);
        let txs: Vec<Tx> = (0..3u8).map(transfer).collect();
        assert_eq!(
            vertex(1, tip, txs).shape(2, &tip, 0),
            Err(Error::TxCap { txs: 3, cap: 2 })
        );
    }

    #[test]
    fn one_note_spent_twice_inside_one_vertex_is_refused() {
        let tip = ids::repeated(9);
        let mut twin = transfer(1);
        twin.fee = 2;
        assert_eq!(
            vertex(1, tip, vec![transfer(1), twin]).shape(100, &tip, 0),
            Err(Error::DuplicateNullifier)
        );
    }

    #[test]
    fn two_vertices_conflict_exactly_when_they_spend_a_note_in_common() {
        let tip = ids::repeated(9);
        let a = vertex(1, tip, vec![transfer(1), transfer(2)]);
        let shares = vertex(1, tip, vec![transfer(2), transfer(3)]);
        let disjoint = vertex(1, tip, vec![transfer(3)]);
        assert!(a.conflicts(&shares));
        assert!(shares.conflicts(&a));
        assert!(!a.conflicts(&disjoint));
        assert!(!vertex(1, tip, vec![]).conflicts(&a));
    }
}
