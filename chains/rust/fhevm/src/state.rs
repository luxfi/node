// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What F persists, and the names it derives.
//!
//! THE CIPHERTEXT-BODY INVARIANT — the F-Chain's reason to exist.
//!
//! F is the COORDINATION plane for confidential compute. It stores the PUBLIC
//! coordinates of an encrypted value — a 32-byte handle, the digest of the
//! ciphertext body, who owns it, who may act on it, and which threshold
//! decryptions were asked for and answered. It never stores a ciphertext body,
//! never holds the FHE secret key, and never holds a decryption share: the bodies
//! live in off-chain storage and the key shares live on the threshold committee,
//! whose members are named here only by public node id and public signing key.
//!
//! Three things hold that line, because none of them holds it alone. The four
//! record types below are everything F persists, and every field of them is a
//! public coordinate: hashes, addresses, bitmasks, sizes, epochs, timestamps —
//! and `tests/invariant.rs` PINS that field list exactly, so a field added,
//! removed or retyped fails a test until someone writes it down. Second, a
//! transaction's payload decodes as EXACTLY its schema: a member the schema does
//! not describe is refused, not ignored. Third, the bytes a transaction does
//! carry are bounded and priced by the byte, so bulk is refused before it is
//! stored and paid for when it is.
//!
//! The records carry the FHE runtime's own types so the chain and the runtime
//! speak one vocabulary. F owns the PERSISTENCE, because persistence in a chain
//! is consensus: every field F writes is derived from the accepting block — its
//! timestamp, its transactions — and never from a validator's wall clock, so two
//! validators replaying the same block write the same bytes.

use sha2::{Digest, Sha256};

use crate::fee::Account;
use crate::fhe;
use crate::gojson::{self, Error as JsonError};
use crate::ids::{self, Id};

/// A permit is the only revocable thing on F: a registration is a fact about a
/// ciphertext that exists, and a fact does not stop being true, whereas the
/// authority to act on it can be withdrawn at any time.
pub const STATUS_ACTIVE: &str = "active";
pub const STATUS_REVOKED: &str = "revoked";

/// F's record of one registered encrypted value. The body it describes is
/// off-chain; `digest` binds this record to it, and `handle` — derived from the
/// digest and the scheme — is the name every other operation uses.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct CiphertextRecord {
    pub meta: fhe::CiphertextMeta,
    pub scheme: String,
    pub digest: [u8; 32],
}

/// A capability: the grantor lets the grantee perform a set of operations on one
/// handle until it expires. It is the only thing that authorizes a decryption
/// request, so it is checked at admission, in consensus, and again at
/// application.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct PermitRecord {
    pub permit: fhe::Permit,
    pub status: String,
}

/// A threshold-decryption request and the committee's answer to it. F does not
/// decrypt: each member ATTESTS the resulting public handle here, and the request
/// completes when a threshold of DISTINCT members attest the SAME result — which
/// is why the attestations are a list of (member, value) pairs and not one field.
/// A lone member posting a wrong result buys nothing but its own burnt fee.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct DecryptRecord {
    pub request: fhe::DecryptRequest,
    pub permit_id: [u8; 32],
    pub attestations: fhe::List<Attestation>,
}

/// One committee member's vote for one 32-byte value. It backs both of F's
/// threshold decisions — which plaintext handle a decryption produced, and which
/// committee the next epoch has — because both are the same question: did
/// threshold distinct members say the same thing?
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Attestation {
    pub member: Account,
    pub value: [u8; 32],
}

/// The committee that holds the FHE key shares for one epoch, together with the
/// network public key they jointly generated. Epoch 0 comes from genesis; every
/// later epoch is installed once a threshold of the CURRENT committee attests the
/// same successor.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct EpochRecord {
    pub info: fhe::EpochInfo,
    pub attestations: fhe::List<Attestation>,
}

// ------------------------------------------------------------ the decisions --

/// How many DISTINCT members attested `value`, which is the only question a
/// threshold decision asks.
pub fn tally(attestations: &[Attestation], value: &[u8; 32]) -> i64 {
    let mut seen: Vec<Account> = Vec::new();
    for a in attestations {
        if &a.value != value {
            continue;
        }
        if !seen.contains(&a.member) {
            seen.push(a.member);
        }
    }
    seen.len() as i64
}

/// Records a member's choice, REPLACING whatever it chose before. One member, one
/// vote — so a member can neither raise a value's count by repeating itself nor
/// hedge across two values — but a vote is not spent by being cast.
///
/// It used to be spent. A committee that split its vote could then never
/// converge: no proposal reached the threshold, nobody could change their mind,
/// nothing timed out, and the tally cleared only on the advance that could not
/// happen. At unanimity one member wedged rotation alone; below it, an honest
/// race between two DKG results did the same with no adversary at all.
pub fn vote(list: &mut fhe::List<Attestation>, member: Account, value: [u8; 32]) {
    let entries = list.get_or_insert_with(Vec::new);
    for a in entries.iter_mut() {
        if a.member == member {
            a.value = value;
            return;
        }
    }
    entries.push(Attestation { member, value });
}

impl EpochRecord {
    /// Whether `acct` holds a seat this epoch. A member's F-Chain account is
    /// derived from its PUBLIC signing key by exactly the derivation that
    /// authenticates a transaction payer, so committee membership and payer
    /// identity cannot disagree.
    pub fn member_of(&self, acct: &Account) -> bool {
        self.info
            .committee
            .as_deref()
            .unwrap_or(&[])
            .iter()
            .any(|m| &address_of(&m.public_key) == acct)
    }

    pub fn committee(&self) -> &[fhe::CommitteeMember] {
        self.info.committee.as_deref().unwrap_or(&[])
    }

    pub fn attestations(&self) -> &[Attestation] {
        self.attestations.as_deref().unwrap_or(&[])
    }
}

impl DecryptRecord {
    pub fn attestations(&self) -> &[Attestation] {
        self.attestations.as_deref().unwrap_or(&[])
    }
}

/// An account from an ML-DSA public key. F is internally consistent: it derives
/// the same address it checks a payer against and the same address it recognises
/// a committee member by. A PUBLIC, one-way derivation — no secret is involved.
pub fn address_of(public_key: &[u8]) -> Account {
    let h = Sha256::digest(public_key);
    let mut a: Account = [0u8; ids::SHORT_ID_LEN];
    a.copy_from_slice(&h[..ids::SHORT_ID_LEN]);
    a
}

/// The canonical ordering of a committee: ascending node id. A committee is
/// hashed to decide an epoch advance, so two members proposing the same set must
/// produce the same bytes — the order is part of the value.
pub fn committee_order(c: &[fhe::CommitteeMember]) -> bool {
    c.windows(2).all(|w| w[0].node_id <= w[1].node_id)
}

/// Writes `len(b)` then `b`, so concatenated fields cannot be re-split at a
/// different boundary to forge a colliding digest.
fn write_len_prefixed(h: &mut Sha256, b: &[u8]) {
    h.update((b.len() as u64).to_be_bytes());
    h.update(b);
}

/// The value committee members attest when advancing an epoch. It hashes the
/// SEMANTIC proposal — epoch, threshold, network public key, and the canonically
/// ordered members — rather than the transaction's payload bytes, so two members
/// whose clients encode the same proposal differently still vote for the same
/// thing.
pub fn committee_digest(
    epoch: u64,
    threshold: i64,
    public_key: &[u8],
    c: &[fhe::CommitteeMember],
) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/epoch/");
    h.update(epoch.to_be_bytes());
    h.update((threshold as u64).to_be_bytes());
    write_len_prefixed(&mut h, public_key);
    h.update((c.len() as u64).to_be_bytes());
    for m in c {
        h.update(m.node_id);
        write_len_prefixed(&mut h, &m.public_key);
        h.update(m.weight.to_be_bytes());
        h.update((m.index as u64).to_be_bytes());
    }
    h.finalize().into()
}

/// Names a ciphertext by its CONTENT: the digest of the off-chain body under a
/// given scheme. Two registrations of the same body under the same scheme
/// therefore collide by construction and the second is refused, and a handle
/// cannot be squatted by someone who does not have the body to hash.
pub fn derive_handle(digest: &[u8; 32], scheme: &[u8]) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/ct/");
    h.update(digest);
    write_len_prefixed(&mut h, scheme);
    h.finalize().into()
}

/// Names a grant by everything that distinguishes it, including the grantor's
/// nonce, so a grantor may re-grant the same capability later without colliding
/// with the earlier permit.
pub fn derive_permit_id(
    handle: &[u8; 32],
    grantor: &Account,
    grantee: &Account,
    ops: u32,
    expiry: i64,
    nonce: u64,
) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/permit/");
    h.update(handle);
    h.update(grantor);
    h.update(grantee);
    h.update((ops as u64).to_be_bytes());
    h.update((expiry as u64).to_be_bytes());
    h.update(nonce.to_be_bytes());
    h.finalize().into()
}

/// Names a decryption request by handle, requester and the requester's nonce —
/// all fields the payer signed — so the id is deterministic across validators and
/// unique per request.
pub fn derive_request_id(handle: &[u8; 32], requester: &Account, nonce: u64) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/decrypt/");
    h.update(handle);
    h.update(requester);
    h.update(nonce.to_be_bytes());
    h.finalize().into()
}

// ---------------------------------------------------------------- the bytes --

fn write_attestations(out: &mut String, list: &fhe::List<Attestation>) {
    match list {
        None => out.push_str("null"),
        Some(entries) => {
            out.push('[');
            for (i, a) in entries.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                let mut first = true;
                out.push('{');
                gojson::write_key(out, &mut first, "member");
                gojson::write_string(out, &ids::short_id_string(&a.member));
                gojson::write_key(out, &mut first, "value");
                gojson::write_byte_array(out, &a.value);
                out.push('}');
            }
            out.push(']');
        }
    }
}

fn read_attestations(
    obj: &serde_json::Map<String, serde_json::Value>,
    name: &str,
    op: &str,
) -> Result<fhe::List<Attestation>, JsonError> {
    let Some(items) = gojson::array(obj, name, op)? else {
        return Ok(None);
    };
    let mut out = Vec::with_capacity(items.len());
    for item in items {
        let entry = gojson::object(item, op, &["member", "value"])?;
        out.push(Attestation {
            member: gojson::account_field(entry, "member", op)?,
            value: gojson::byte_array::<32>(entry, "value", op)?,
        });
    }
    Ok(Some(out))
}

pub fn write_committee_json(out: &mut String, list: &fhe::List<fhe::CommitteeMember>) {
    match list {
        None => out.push_str("null"),
        Some(members) => {
            out.push('[');
            for (i, m) in members.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                out.push('{');
                let mut first = true;
                gojson::write_key(out, &mut first, "node_id");
                gojson::write_string(out, &ids::node_id_string(&m.node_id));
                gojson::write_key(out, &mut first, "public_key");
                gojson::write_base64_or_null(out, slice_or_null(&m.public_key));
                gojson::write_key(out, &mut first, "weight");
                out.push_str(&m.weight.to_string());
                gojson::write_key(out, &mut first, "index");
                out.push_str(&m.index.to_string());
                out.push('}');
            }
            out.push(']');
        }
    }
}

/// Go writes a nil `[]byte` as `null` and an allocated empty one as `""`. Nothing
/// in F ever holds the second, so an empty one is the nil one.
fn slice_or_null(b: &[u8]) -> Option<&[u8]> {
    if b.is_empty() {
        None
    } else {
        Some(b)
    }
}

/// Reads a committee back. Public keys are the only place F parses bytes it was
/// handed, and this is a store read, not a wire read: a record that does not
/// decode is skipped by the loader, never guessed at.
pub fn read_committee(
    obj: &serde_json::Map<String, serde_json::Value>,
    name: &str,
    op: &str,
) -> Result<fhe::List<fhe::CommitteeMember>, JsonError> {
    let Some(items) = gojson::array(obj, name, op)? else {
        return Ok(None);
    };
    let mut out = Vec::with_capacity(items.len());
    for item in items {
        let m = gojson::object(item, op, &["node_id", "public_key", "weight", "index"])?;
        out.push(fhe::CommitteeMember {
            node_id: gojson::node_id_field(m, "node_id", op)?,
            public_key: gojson::bytes_field(m, "public_key", op)?,
            weight: gojson::u64_field(m, "weight", op)?,
            index: gojson::i64_field(m, "index", op)?,
        });
    }
    Ok(Some(out))
}

impl CiphertextRecord {
    pub fn to_json(&self) -> String {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "handle");
        gojson::write_byte_array(&mut out, &self.meta.handle);
        gojson::write_key(&mut out, &mut first, "owner");
        gojson::write_byte_array(&mut out, &self.meta.owner);
        gojson::write_key(&mut out, &mut first, "type");
        out.push_str(&self.meta.kind.to_string());
        gojson::write_key(&mut out, &mut first, "level");
        out.push_str(&self.meta.level.to_string());
        gojson::write_key(&mut out, &mut first, "epoch");
        out.push_str(&self.meta.epoch.to_string());
        gojson::write_key(&mut out, &mut first, "registered_at");
        out.push_str(&self.meta.registered_at.to_string());
        gojson::write_key(&mut out, &mut first, "size");
        out.push_str(&self.meta.size.to_string());
        gojson::write_key(&mut out, &mut first, "chain_id");
        gojson::write_string(&mut out, &ids::id_string(&self.meta.chain_id));
        gojson::write_key(&mut out, &mut first, "scheme");
        gojson::write_string(&mut out, &self.scheme);
        gojson::write_key(&mut out, &mut first, "digest");
        gojson::write_byte_array(&mut out, &self.digest);
        out.push('}');
        out
    }

    pub fn from_json(raw: &[u8]) -> Result<Self, JsonError> {
        const OP: &str = "ciphertext record";
        let v = gojson::parse(raw, OP)?;
        let o = gojson::object(
            &v,
            OP,
            &[
                "handle",
                "owner",
                "type",
                "level",
                "epoch",
                "registered_at",
                "size",
                "chain_id",
                "scheme",
                "digest",
            ],
        )?;
        Ok(CiphertextRecord {
            meta: fhe::CiphertextMeta {
                handle: gojson::byte_array::<32>(o, "handle", OP)?,
                owner: gojson::byte_array::<20>(o, "owner", OP)?,
                kind: gojson::u8_field(o, "type", OP)?,
                level: gojson::i64_field(o, "level", OP)?,
                epoch: gojson::u64_field(o, "epoch", OP)?,
                registered_at: gojson::i64_field(o, "registered_at", OP)?,
                size: gojson::u32_field(o, "size", OP)?,
                chain_id: gojson::id_field(o, "chain_id", OP)?,
            },
            scheme: gojson::string_field(o, "scheme", OP)?,
            digest: gojson::byte_array::<32>(o, "digest", OP)?,
        })
    }
}

impl PermitRecord {
    pub fn to_json(&self) -> String {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "permit_id");
        gojson::write_byte_array(&mut out, &self.permit.permit_id);
        gojson::write_key(&mut out, &mut first, "handle");
        gojson::write_byte_array(&mut out, &self.permit.handle);
        gojson::write_key(&mut out, &mut first, "grantee");
        gojson::write_byte_array(&mut out, &self.permit.grantee);
        gojson::write_key(&mut out, &mut first, "grantor");
        gojson::write_byte_array(&mut out, &self.permit.grantor);
        gojson::write_key(&mut out, &mut first, "operations");
        out.push_str(&self.permit.operations.to_string());
        gojson::write_key(&mut out, &mut first, "expiry");
        out.push_str(&self.permit.expiry.to_string());
        gojson::write_key(&mut out, &mut first, "created_at");
        out.push_str(&self.permit.created_at.to_string());
        if !self.permit.attestation.is_empty() {
            gojson::write_key(&mut out, &mut first, "attestation");
            gojson::write_base64(&mut out, &self.permit.attestation);
        }
        gojson::write_key(&mut out, &mut first, "chain_id");
        gojson::write_string(&mut out, &ids::id_string(&self.permit.chain_id));
        gojson::write_key(&mut out, &mut first, "status");
        gojson::write_string(&mut out, &self.status);
        out.push('}');
        out
    }

    pub fn from_json(raw: &[u8]) -> Result<Self, JsonError> {
        const OP: &str = "permit record";
        let v = gojson::parse(raw, OP)?;
        let o = gojson::object(
            &v,
            OP,
            &[
                "permit_id",
                "handle",
                "grantee",
                "grantor",
                "operations",
                "expiry",
                "created_at",
                "attestation",
                "chain_id",
                "status",
            ],
        )?;
        Ok(PermitRecord {
            permit: fhe::Permit {
                permit_id: gojson::byte_array::<32>(o, "permit_id", OP)?,
                handle: gojson::byte_array::<32>(o, "handle", OP)?,
                grantee: gojson::byte_array::<20>(o, "grantee", OP)?,
                grantor: gojson::byte_array::<20>(o, "grantor", OP)?,
                operations: gojson::u32_field(o, "operations", OP)?,
                expiry: gojson::i64_field(o, "expiry", OP)?,
                created_at: gojson::i64_field(o, "created_at", OP)?,
                attestation: gojson::bytes_field(o, "attestation", OP)?,
                chain_id: gojson::id_field(o, "chain_id", OP)?,
            },
            status: gojson::string_field(o, "status", OP)?,
        })
    }
}

impl DecryptRecord {
    pub fn to_json(&self) -> String {
        let r = &self.request;
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "request_id");
        gojson::write_byte_array(&mut out, &r.request_id);
        gojson::write_key(&mut out, &mut first, "ciphertext_handle");
        gojson::write_byte_array(&mut out, &r.ciphertext_handle);
        gojson::write_key(&mut out, &mut first, "requester");
        gojson::write_byte_array(&mut out, &r.requester);
        gojson::write_key(&mut out, &mut first, "callback");
        gojson::write_byte_array(&mut out, &r.callback);
        gojson::write_key(&mut out, &mut first, "callback_selector");
        gojson::write_byte_array(&mut out, &r.callback_selector);
        gojson::write_key(&mut out, &mut first, "source_chain");
        gojson::write_string(&mut out, &ids::id_string(&r.source_chain));
        gojson::write_key(&mut out, &mut first, "epoch");
        out.push_str(&r.epoch.to_string());
        gojson::write_key(&mut out, &mut first, "nonce");
        out.push_str(&r.nonce.to_string());
        gojson::write_key(&mut out, &mut first, "expiry");
        out.push_str(&r.expiry.to_string());
        gojson::write_key(&mut out, &mut first, "status");
        out.push_str(&(r.status as u8).to_string());
        gojson::write_key(&mut out, &mut first, "created_at");
        out.push_str(&r.created_at.to_string());
        if r.completed_at != 0 {
            gojson::write_key(&mut out, &mut first, "completed_at");
            out.push_str(&r.completed_at.to_string());
        }
        // A fixed array has no empty form, so `omitempty` never omits it: the
        // result handle is written whether or not anything was decided.
        gojson::write_key(&mut out, &mut first, "result_handle");
        gojson::write_byte_array(&mut out, &r.result_handle);
        if !r.error.is_empty() {
            gojson::write_key(&mut out, &mut first, "error");
            gojson::write_string(&mut out, &r.error);
        }
        gojson::write_key(&mut out, &mut first, "permitId");
        gojson::write_byte_array(&mut out, &self.permit_id);
        gojson::write_key(&mut out, &mut first, "attestations");
        write_attestations(&mut out, &self.attestations);
        out.push('}');
        out
    }

    pub fn from_json(raw: &[u8]) -> Result<Self, JsonError> {
        const OP: &str = "decrypt record";
        let v = gojson::parse(raw, OP)?;
        let o = gojson::object(
            &v,
            OP,
            &[
                "request_id",
                "ciphertext_handle",
                "requester",
                "callback",
                "callback_selector",
                "source_chain",
                "epoch",
                "nonce",
                "expiry",
                "status",
                "created_at",
                "completed_at",
                "result_handle",
                "error",
                "permitId",
                "attestations",
            ],
        )?;
        let status_raw = gojson::u8_field(o, "status", OP)?;
        let status = fhe::RequestStatus::from_u8(status_raw)
            .ok_or_else(|| JsonError(format!("{OP}: status {status_raw} is not a status")))?;
        Ok(DecryptRecord {
            request: fhe::DecryptRequest {
                request_id: gojson::byte_array::<32>(o, "request_id", OP)?,
                ciphertext_handle: gojson::byte_array::<32>(o, "ciphertext_handle", OP)?,
                requester: gojson::byte_array::<20>(o, "requester", OP)?,
                callback: gojson::byte_array::<20>(o, "callback", OP)?,
                callback_selector: gojson::byte_array::<4>(o, "callback_selector", OP)?,
                source_chain: gojson::id_field(o, "source_chain", OP)?,
                epoch: gojson::u64_field(o, "epoch", OP)?,
                nonce: gojson::u64_field(o, "nonce", OP)?,
                expiry: gojson::i64_field(o, "expiry", OP)?,
                status,
                created_at: gojson::i64_field(o, "created_at", OP)?,
                completed_at: gojson::i64_field(o, "completed_at", OP)?,
                result_handle: gojson::byte_array::<32>(o, "result_handle", OP)?,
                error: gojson::string_field(o, "error", OP)?,
            },
            permit_id: gojson::byte_array::<32>(o, "permitId", OP)?,
            attestations: read_attestations(o, "attestations", OP)?,
        })
    }
}

impl EpochRecord {
    pub fn to_json(&self) -> String {
        let e = &self.info;
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "epoch");
        out.push_str(&e.epoch.to_string());
        gojson::write_key(&mut out, &mut first, "start_time");
        out.push_str(&e.start_time.to_string());
        if e.end_time != 0 {
            gojson::write_key(&mut out, &mut first, "end_time");
            out.push_str(&e.end_time.to_string());
        }
        gojson::write_key(&mut out, &mut first, "committee");
        write_committee_json(&mut out, &e.committee);
        gojson::write_key(&mut out, &mut first, "threshold");
        out.push_str(&e.threshold.to_string());
        gojson::write_key(&mut out, &mut first, "public_key");
        gojson::write_base64_or_null(&mut out, slice_or_null(&e.public_key));
        gojson::write_key(&mut out, &mut first, "status");
        out.push_str(&(e.status as u8).to_string());
        gojson::write_key(&mut out, &mut first, "attestations");
        write_attestations(&mut out, &self.attestations);
        out.push('}');
        out
    }

    pub fn from_json(raw: &[u8]) -> Result<Self, JsonError> {
        const OP: &str = "epoch record";
        let v = gojson::parse(raw, OP)?;
        let o = gojson::object(
            &v,
            OP,
            &[
                "epoch",
                "start_time",
                "end_time",
                "committee",
                "threshold",
                "public_key",
                "status",
                "attestations",
            ],
        )?;
        let status_raw = gojson::u8_field(o, "status", OP)?;
        let status = fhe::EpochStatus::from_u8(status_raw)
            .ok_or_else(|| JsonError(format!("{OP}: status {status_raw} is not a status")))?;
        Ok(EpochRecord {
            info: fhe::EpochInfo {
                epoch: gojson::u64_field(o, "epoch", OP)?,
                start_time: gojson::i64_field(o, "start_time", OP)?,
                end_time: gojson::i64_field(o, "end_time", OP)?,
                committee: read_committee(o, "committee", OP)?,
                threshold: gojson::i64_field(o, "threshold", OP)?,
                public_key: gojson::bytes_field(o, "public_key", OP)?,
                status,
            },
            attestations: read_attestations(o, "attestations", OP)?,
        })
    }
}

/// The id this chain is named by in `luxfi/constants`: an ASCII name, left-padded
/// into 32 bytes. A vmID is an immutable one-way door — it is baked into the
/// genesis CreateChainTx and stored by the P-Chain forever — so it is written out
/// once, here, and asserted against its cb58 word in `tests/vmid.rs`.
pub fn vm_id() -> Id {
    let mut id = ids::EMPTY;
    id[..5].copy_from_slice(b"fhevm");
    id
}

#[cfg(test)]
mod tests {
    use super::*;

    fn acct(b: u8) -> Account {
        [b; 20]
    }

    #[test]
    fn a_threshold_decision_counts_members_not_messages() {
        let (a, b) = (acct(1), acct(2));
        let (x, y) = ([1u8; 32], [2u8; 32]);

        let mut list: fhe::List<Attestation> = None;
        vote(&mut list, a, x);
        vote(&mut list, a, x);
        assert_eq!(list.as_deref().unwrap().len(), 1, "a repeated member holds one entry");
        assert_eq!(tally(list.as_deref().unwrap(), &x), 1, "and counts once");

        vote(&mut list, b, x);
        assert_eq!(tally(list.as_deref().unwrap(), &x), 2);
        assert_eq!(tally(list.as_deref().unwrap(), &y), 0);

        // Moving a vote moves it: the old value keeps nothing.
        vote(&mut list, a, y);
        assert_eq!(list.as_deref().unwrap().len(), 2, "still one entry per member");
        assert_eq!(tally(list.as_deref().unwrap(), &x), 1);
        assert_eq!(tally(list.as_deref().unwrap(), &y), 1);

        // And the count is over DISTINCT members whatever the list holds.
        let doubled = [Attestation { member: a, value: x }, Attestation { member: a, value: x }];
        assert_eq!(tally(&doubled, &x), 1);
    }

    #[test]
    fn a_committee_is_ordered_by_node_id() {
        let member = |n: u8| fhe::CommitteeMember {
            node_id: [n; 20],
            public_key: vec![n],
            weight: 1,
            index: 0,
        };
        assert!(committee_order(&[member(1), member(2), member(3)]));
        assert!(!committee_order(&[member(3), member(1)]));
        assert!(committee_order(&[]));
        assert!(committee_order(&[member(1)]));
    }

    #[test]
    fn every_derivation_changes_with_every_input() {
        let d = [9u8; 32];
        let (a, b) = (acct(1), acct(2));

        assert_eq!(derive_handle(&d, b"ckks-n14"), derive_handle(&d, b"ckks-n14"));
        assert_ne!(derive_handle(&d, b"ckks-n14"), derive_handle(&d, b"bfv-n13"));
        assert_ne!(derive_handle(&d, b"ckks-n14"), derive_handle(&[8u8; 32], b"ckks-n14"));

        let h = derive_handle(&d, b"ckks-n14");
        assert_eq!(derive_permit_id(&h, &a, &b, 1, 0, 1), derive_permit_id(&h, &a, &b, 1, 0, 1));
        assert_ne!(derive_permit_id(&h, &a, &b, 1, 0, 1), derive_permit_id(&h, &a, &b, 1, 0, 2));
        assert_ne!(derive_permit_id(&h, &a, &b, 1, 0, 1), derive_permit_id(&h, &b, &a, 1, 0, 1));

        assert_eq!(derive_request_id(&h, &a, 3), derive_request_id(&h, &a, 3));
        assert_ne!(derive_request_id(&h, &a, 3), derive_request_id(&h, &a, 4));
        assert_ne!(derive_request_id(&h, &a, 3), derive_request_id(&h, &b, 3));
    }

    #[test]
    fn a_committee_digest_is_the_proposal_and_cannot_be_re_split() {
        let member = |n: u8, key: Vec<u8>| fhe::CommitteeMember {
            node_id: [n; 20],
            public_key: key,
            weight: 1,
            index: n as i64,
        };
        let c = vec![member(1, vec![0xaa, 0xbb]), member(2, vec![0xcc])];
        let pk = b"network-key";
        let base = committee_digest(1, 2, pk, &c);

        assert_eq!(base, committee_digest(1, 2, pk, &c));
        assert_ne!(base, committee_digest(2, 2, pk, &c), "epoch must bind");
        assert_ne!(base, committee_digest(1, 3, pk, &c), "threshold must bind");
        assert_ne!(base, committee_digest(1, 2, b"other", &c), "network key must bind");
        assert_ne!(base, committee_digest(1, 2, pk, &c[..1]), "membership must bind");

        // Length-prefixing means one member's key cannot swallow the next.
        let mut shifted = c.clone();
        shifted[0].public_key = vec![0xaa, 0xbb, 0xcc];
        assert_ne!(base, committee_digest(1, 2, pk, &shifted));
    }

    #[test]
    fn every_record_round_trips_through_its_stored_form() {
        let ct = CiphertextRecord {
            meta: fhe::CiphertextMeta {
                handle: [1; 32],
                owner: acct(2),
                kind: 4,
                level: 3,
                epoch: 7,
                registered_at: 1_700_000_000,
                size: 4096,
                chain_id: [5; 32],
            },
            scheme: "ckks-n14".into(),
            digest: [6; 32],
        };
        assert_eq!(CiphertextRecord::from_json(ct.to_json().as_bytes()).unwrap(), ct);

        let pm = PermitRecord {
            permit: fhe::Permit {
                permit_id: [1; 32],
                handle: [2; 32],
                grantee: acct(3),
                grantor: acct(4),
                operations: fhe::PERMIT_OP_DECRYPT,
                expiry: 99,
                created_at: 10,
                attestation: Vec::new(),
                chain_id: [5; 32],
            },
            status: STATUS_ACTIVE.into(),
        };
        assert!(!pm.to_json().contains("attestation\""), "an empty slice is omitted");
        assert_eq!(PermitRecord::from_json(pm.to_json().as_bytes()).unwrap(), pm);

        let dr = DecryptRecord {
            request: fhe::DecryptRequest {
                request_id: [1; 32],
                ciphertext_handle: [2; 32],
                requester: acct(3),
                callback: [0xca; 20],
                callback_selector: [1, 2, 3, 4],
                source_chain: [5; 32],
                epoch: 1,
                nonce: 2,
                expiry: 3,
                status: fhe::RequestStatus::Completed,
                created_at: 4,
                completed_at: 5,
                result_handle: [6; 32],
                error: String::new(),
            },
            permit_id: [7; 32],
            attestations: Some(vec![Attestation { member: acct(8), value: [9; 32] }]),
        };
        assert_eq!(DecryptRecord::from_json(dr.to_json().as_bytes()).unwrap(), dr);

        let ep = EpochRecord {
            info: fhe::EpochInfo {
                epoch: 1,
                start_time: 2,
                end_time: 3,
                committee: Some(vec![fhe::CommitteeMember {
                    node_id: [7; 20],
                    public_key: vec![1, 2, 3],
                    weight: 1,
                    index: 0,
                }]),
                threshold: 2,
                public_key: b"pk".to_vec(),
                status: fhe::EpochStatus::Ended,
            },
            attestations: None,
        };
        assert!(ep.to_json().contains("\"attestations\":null"), "a nil list is null");
        assert_eq!(EpochRecord::from_json(ep.to_json().as_bytes()).unwrap(), ep);
    }

    #[test]
    fn a_zero_completed_at_and_a_zero_end_time_are_omitted() {
        let dr = DecryptRecord::default();
        assert!(!dr.to_json().contains("completed_at"));
        assert!(dr.to_json().contains("result_handle"), "a fixed array is never omitted");
        let ep = EpochRecord::default();
        assert!(!ep.to_json().contains("end_time"));
    }

    #[test]
    fn an_address_is_the_first_twenty_bytes_of_the_keys_hash() {
        let key = b"a public key";
        let want: [u8; 32] = Sha256::digest(key).into();
        assert_eq!(address_of(key).to_vec(), want[..20].to_vec());
    }
}
