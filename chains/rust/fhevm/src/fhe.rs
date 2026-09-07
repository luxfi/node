// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The FHE runtime's vocabulary.
//!
//! F coordinates confidential compute; the runtime performs it. These are the
//! runtime's own types — a ciphertext's public coordinates, a capability over
//! one, a threshold-decryption request, a committee and an epoch — carried here
//! so the chain and the runtime speak one language rather than two that have to
//! be translated. Every field is a PUBLIC coordinate: a hash, an address, a
//! bitmask, a size, an epoch, a timestamp. There is no ciphertext body here, no
//! FHE secret key and no decryption share, and there is no field one could be
//! put in.
//!
//! What F does NOT take from the runtime is its store. The runtime's registry
//! stamps records with the wall clock, which is right for the off-chain daemon it
//! was written for and wrong for consensus: two validators replaying one block
//! would write different bytes. F owns its persistence for that reason, and every
//! timestamp it writes comes from the accepting block.

use crate::fee::Account;
use crate::ids::{Id, NodeId};

/// A Go slice, kept honest: absent is `null` and present-but-empty is `[]`, and
/// a record that wrote one where the other belonged would not be the bytes the
/// Go chain wrote.
pub type List<T> = Option<Vec<T>>;

/// Where a decryption request stands.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum RequestStatus {
    Pending = 0,
    Processing = 1,
    Completed = 2,
    Failed = 3,
    Expired = 4,
}

impl RequestStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            RequestStatus::Pending => "pending",
            RequestStatus::Processing => "processing",
            RequestStatus::Completed => "completed",
            RequestStatus::Failed => "failed",
            RequestStatus::Expired => "expired",
        }
    }

    pub fn from_u8(v: u8) -> Option<Self> {
        match v {
            0 => Some(RequestStatus::Pending),
            1 => Some(RequestStatus::Processing),
            2 => Some(RequestStatus::Completed),
            3 => Some(RequestStatus::Failed),
            4 => Some(RequestStatus::Expired),
            _ => None,
        }
    }
}

/// Where an epoch stands.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum EpochStatus {
    Active = 0,
    Ended = 1,
    Pending = 2,
}

impl EpochStatus {
    pub fn from_u8(v: u8) -> Option<Self> {
        match v {
            0 => Some(EpochStatus::Active),
            1 => Some(EpochStatus::Ended),
            2 => Some(EpochStatus::Pending),
            _ => None,
        }
    }
}

/// What a permit may confer. A grant setting a bit outside these is refused
/// rather than silently conferring nothing.
pub const PERMIT_OP_DECRYPT: u32 = 1;
pub const PERMIT_OP_REENCRYPT: u32 = 1 << 1;
pub const PERMIT_OP_COMPUTE: u32 = 1 << 2;
pub const PERMIT_OP_TRANSFER: u32 = 1 << 3;

/// One seat on the threshold committee.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CommitteeMember {
    pub node_id: NodeId,
    pub public_key: Vec<u8>,
    pub weight: u64,
    pub index: i64,
}

/// A registered encrypted value's public coordinates. The body it describes is
/// off-chain; `handle` names it and the record's digest binds the two.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct CiphertextMeta {
    pub handle: [u8; 32],
    pub owner: Account,
    pub kind: u8,
    pub level: i64,
    pub epoch: u64,
    pub registered_at: i64,
    pub size: u32,
    pub chain_id: Id,
}

/// A capability: the grantor lets the grantee act on one handle until it expires.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Permit {
    pub permit_id: [u8; 32],
    pub handle: [u8; 32],
    pub grantee: Account,
    pub grantor: Account,
    pub operations: u32,
    pub expiry: i64,
    pub created_at: i64,
    pub attestation: Vec<u8>,
    pub chain_id: Id,
}

/// A threshold-decryption request. F never decrypts: the committee combines its
/// shares off-chain and attests the public handle of what came out.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DecryptRequest {
    pub request_id: [u8; 32],
    pub ciphertext_handle: [u8; 32],
    pub requester: Account,
    pub callback: [u8; 20],
    pub callback_selector: [u8; 4],
    pub source_chain: Id,
    pub epoch: u64,
    pub nonce: u64,
    pub expiry: i64,
    pub status: RequestStatus,
    pub created_at: i64,
    pub completed_at: i64,
    pub result_handle: [u8; 32],
    pub error: String,
}

impl Default for DecryptRequest {
    fn default() -> Self {
        DecryptRequest {
            request_id: [0; 32],
            ciphertext_handle: [0; 32],
            requester: [0; 20],
            callback: [0; 20],
            callback_selector: [0; 4],
            source_chain: crate::ids::EMPTY,
            epoch: 0,
            nonce: 0,
            expiry: 0,
            status: RequestStatus::Pending,
            created_at: 0,
            completed_at: 0,
            result_handle: [0; 32],
            error: String::new(),
        }
    }
}

/// The committee that holds the key shares for one epoch, and the network public
/// key they jointly generated.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct EpochInfo {
    pub epoch: u64,
    pub start_time: i64,
    pub end_time: i64,
    pub committee: List<CommitteeMember>,
    pub threshold: i64,
    pub public_key: Vec<u8>,
    pub status: EpochStatus,
}

impl Default for EpochInfo {
    fn default() -> Self {
        EpochInfo {
            epoch: 0,
            start_time: 0,
            end_time: 0,
            committee: None,
            threshold: 0,
            public_key: Vec::new(),
            status: EpochStatus::Active,
        }
    }
}

/// The CKKS parameters this network encrypts under, as the runtime's default
/// threshold configuration declares them. They are reported by the RPC surface so
/// a client encrypting for F and a node evaluating for F agree by construction;
/// F itself does no evaluation, so it holds the numbers and nothing else.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ThresholdParams {
    pub log_n: i64,
    pub log_qp: i64,
    pub log_scale: i64,
}

/// The runtime's default threshold configuration: the CKKS 128-bit parameter set
/// at ring degree 2^14, whose moduli sum to 435 bits (325 of Q and 110 of P,
/// truncated as Go truncates the sum of the two float logs) at a default scale of
/// 2^45. Restated here because F reports the numbers and computes on none of
/// them; `tests/golden.rs` checks them against the values the Go runtime returns.
pub const DEFAULT_THRESHOLD_PARAMS: ThresholdParams =
    ThresholdParams { log_n: 14, log_qp: 435, log_scale: 45 };

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_status_is_the_number_and_the_word_go_uses() {
        assert_eq!(RequestStatus::Pending as u8, 0);
        assert_eq!(RequestStatus::Completed as u8, 2);
        assert_eq!(RequestStatus::Expired as u8, 4);
        assert_eq!(RequestStatus::Pending.as_str(), "pending");
        assert_eq!(RequestStatus::Completed.as_str(), "completed");
        assert_eq!(RequestStatus::Expired.as_str(), "expired");
        assert_eq!(EpochStatus::Active as u8, 0);
        assert_eq!(EpochStatus::Ended as u8, 1);
        assert_eq!(RequestStatus::from_u8(9), None);
        assert_eq!(EpochStatus::from_u8(9), None);
    }

    #[test]
    fn the_capability_bits_are_one_hot_and_in_order() {
        assert_eq!(PERMIT_OP_DECRYPT, 1);
        assert_eq!(PERMIT_OP_REENCRYPT, 2);
        assert_eq!(PERMIT_OP_COMPUTE, 4);
        assert_eq!(PERMIT_OP_TRANSFER, 8);
    }
}
