// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What F persists, and the names it derives.
//!
//! THE CIPHERTEXT-BODY INVARIANT, which is the F-Chain's reason to exist.
//! F is the COORDINATION plane for confidential compute. It stores the PUBLIC
//! coordinates of an encrypted value — a 32-byte handle, the digest of the
//! ciphertext body, who owns it, who may act on it, and which threshold
//! decryptions were asked for and answered. It never stores a ciphertext body,
//! never holds an FHE secret key, and never holds a decryption share: the
//! bodies live in off-chain storage and the key shares live on the committee,
//! whose members are named here only by public node id and public signing key.
//!
//! Three things hold that line, because none of them holds it alone. The four
//! records below are everything F persists and every field of them is a public
//! coordinate — hashes, addresses, bitmasks, sizes, epochs, timestamps. A
//! payload decodes as EXACTLY its schema, so a member the schema does not
//! describe is refused rather than kept ([`crate::json`]). And the bytes a
//! transaction does carry are bounded and priced by the byte ([`crate::gas`]),
//! so bulk is refused before it is stored and paid for when it is.
//!
//! THE DERIVATIONS ARE CONSENSUS AND THE ENCODING IS NOT. Every hash in this
//! file is a name two implementations must agree on byte for byte: a handle, a
//! permit id, a request id, a committee digest, an address. The layout the
//! records are WRITTEN in is not — F commits no state root, so nothing about it
//! reaches consensus, and the Go chain's own reason for owning its persistence
//! is that its timestamps come from the accepting block rather than a
//! validator's clock. That property is kept here; the byte layout is this
//! implementation's own, and deliberately not a second copy of Go's JSON to
//! keep in step for no observer.

use sha2::{Digest, Sha256};

use crate::id::{Account, Id, NodeId};

/// A permit is the only revocable thing on F. A registration is a fact about a
/// ciphertext that exists, and a fact does not stop being true; the authority
/// to act on it can be withdrawn at any time. So access is controlled entirely
/// by granting and revoking permits, and there is no second place to look.
pub const STATUS_ACTIVE: u8 = 0;
pub const STATUS_REVOKED: u8 = 1;

/// A decryption request's life.
pub const REQUEST_PENDING: u8 = 0;
pub const REQUEST_COMPLETED: u8 = 2;

/// An epoch's life.
pub const EPOCH_ACTIVE: u8 = 0;
pub const EPOCH_ENDED: u8 = 1;

/// The capabilities a permit can confer.
pub const PERMIT_OP_DECRYPT: u32 = 1;
pub const PERMIT_OP_REENCRYPT: u32 = 2;
pub const PERMIT_OP_COMPUTE: u32 = 4;
pub const PERMIT_OP_TRANSFER: u32 = 8;

/// Every capability bit the FHE runtime defines. A grant that sets a bit
/// outside it is refused rather than silently conferring nothing.
pub const PERMIT_OP_MASK: u32 =
    PERMIT_OP_DECRYPT | PERMIT_OP_REENCRYPT | PERMIT_OP_COMPUTE | PERMIT_OP_TRANSFER;

/// One seat on the threshold committee. Named by a public node id and a public
/// ML-DSA-65 signing key, and by nothing else — there is no share here.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Member {
    pub node_id: NodeId,
    pub public_key: Vec<u8>,
    pub weight: u64,
    pub index: i64,
}

/// One member's vote for one 32-byte value.
///
/// This backs both of F's threshold decisions — which plaintext handle a
/// decryption produced, and which committee the next epoch has — because both
/// are the same question: did `threshold` distinct members say the same thing?
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Attestation {
    pub member: Account,
    pub value: [u8; 32],
}

/// F's record of one registered encrypted value. The body it describes is
/// off-chain; `digest` binds this record to it, and `handle` — derived from
/// `digest` and `scheme` — is the name every other operation uses for it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Ciphertext {
    pub handle: [u8; 32],
    pub owner: Account,
    pub kind: u8,
    pub level: i64,
    pub epoch: u64,
    pub registered_at: i64,
    pub size: u32,
    pub chain_id: Id,
    pub scheme: Vec<u8>,
    pub digest: [u8; 32],
}

/// A capability: the grantor lets the grantee perform a set of operations on
/// one handle until `expiry`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Permit {
    pub permit_id: [u8; 32],
    pub handle: [u8; 32],
    pub grantee: Account,
    pub grantor: Account,
    pub operations: u32,
    pub expiry: i64,
    pub created_at: i64,
    pub chain_id: Id,
    pub status: u8,
}

/// A threshold-decryption request and the committee's answer to it.
///
/// F does not decrypt: the committee combines its shares off-chain and each
/// member ATTESTS the resulting public handle here. The request completes when
/// `threshold` distinct members attest the SAME result — which is why the
/// attestations are a list of (member, result) pairs and not one field. A lone
/// member posting a wrong result buys nothing but its own burnt fee, and cannot
/// stall the honest majority.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Decrypt {
    pub request_id: [u8; 32],
    pub handle: [u8; 32],
    pub requester: Account,
    pub callback: [u8; 20],
    pub selector: [u8; 4],
    pub source_chain: Id,
    pub epoch: u64,
    pub nonce: u64,
    pub expiry: i64,
    pub status: u8,
    pub created_at: i64,
    pub completed_at: i64,
    pub result: [u8; 32],
    pub permit_id: [u8; 32],
    pub attestations: Vec<Attestation>,
}

/// The committee that holds the key shares for one epoch, and the network
/// public key they jointly generated. Epoch 0 comes from genesis; every later
/// epoch is installed once `threshold` members of the CURRENT committee attest
/// the same successor.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Epoch {
    pub epoch: u64,
    pub start_time: i64,
    pub end_time: i64,
    pub committee: Vec<Member>,
    pub threshold: i64,
    pub public_key: Vec<u8>,
    pub status: u8,
    /// Approvals of the NEXT epoch, tallied here.
    pub attestations: Vec<Attestation>,
}

impl Epoch {
    /// Whether `acct` holds a seat.
    ///
    /// A member's F-Chain account is derived from its PUBLIC signing key by
    /// exactly the derivation that authenticates a payer, so committee
    /// membership and payer identity cannot disagree.
    pub fn member_of(&self, acct: &Account) -> bool {
        self.committee.iter().any(|m| address_of(&m.public_key) == *acct)
    }
}

/// How many DISTINCT members attested `value`, which is the only question a
/// threshold decision asks.
pub fn tally(votes: &[Attestation], value: &[u8; 32]) -> i64 {
    let mut seen: Vec<Account> = Vec::new();
    for a in votes {
        if a.value != *value {
            continue;
        }
        if !seen.contains(&a.member) {
            seen.push(a.member);
        }
    }
    seen.len() as i64
}

/// Record a member's choice, REPLACING whatever it chose before.
///
/// One member, one vote — so a member can neither raise a value's count by
/// repeating itself nor hedge across two values — but a vote is not SPENT by
/// being cast. It used to be. A committee that split its vote could then never
/// converge: no proposal reached the threshold, nobody could change their mind,
/// nothing timed out, and the tally cleared only on the advance that could not
/// happen. At unanimity one member wedged rotation alone; below it, an honest
/// race between two key-generation results did the same with no adversary at
/// all.
pub fn cast(votes: &mut Vec<Attestation>, member: Account, value: [u8; 32]) {
    if let Some(a) = votes.iter_mut().find(|a| a.member == member) {
        a.value = value;
        return;
    }
    votes.push(Attestation { member, value });
}

/// Whether a committee is in canonical order: ascending node id.
///
/// A committee is hashed to decide an epoch advance, so two members proposing
/// the same set must produce the same bytes — the order is part of the value.
pub fn in_order(c: &[Member]) -> bool {
    c.windows(2).all(|w| w[0].node_id <= w[1].node_id)
}

// ---- the derivations ----

/// An account from an ML-DSA public key: SHA-256, truncated to twenty bytes.
///
/// F is internally consistent about this. It is the address a payer is checked
/// against and the address a committee member is recognised by, so a member and
/// a payer cannot be two different opinions about one key.
pub fn address_of(public_key: &[u8]) -> Account {
    let h = Sha256::digest(public_key);
    let mut a = [0u8; 20];
    a.copy_from_slice(&h[..20]);
    a
}

/// Write `len(b)` then `b`, so concatenated fields cannot be re-split at a
/// different boundary to forge a colliding digest.
fn len_prefixed(h: &mut Sha256, b: &[u8]) {
    h.update((b.len() as u64).to_be_bytes());
    h.update(b);
}

/// Name a ciphertext by its CONTENT: the digest of the off-chain body under a
/// given scheme.
///
/// Two registrations of the same body under the same scheme therefore collide
/// by construction and the second is refused, and a handle cannot be squatted
/// by someone who does not have the body to hash.
pub fn derive_handle(digest: &[u8; 32], scheme: &[u8]) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/ct/");
    h.update(digest);
    len_prefixed(&mut h, scheme);
    h.finalize().into()
}

/// Name a grant by everything that distinguishes it, including the grantor's
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

/// Name a decryption request by handle, requester and the requester's nonce —
/// all fields the payer signed — so the id is deterministic across validators
/// and unique per request.
pub fn derive_request_id(handle: &[u8; 32], requester: &Account, nonce: u64) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/decrypt/");
    h.update(handle);
    h.update(requester);
    h.update(nonce.to_be_bytes());
    h.finalize().into()
}

/// The value committee members attest when advancing an epoch.
///
/// It hashes the SEMANTIC proposal — epoch, threshold, network public key and
/// the canonically ordered members — rather than the transaction's payload
/// bytes, so two members whose clients encode the same proposal differently
/// still vote for the same thing.
pub fn committee_digest(
    epoch: u64,
    threshold: i64,
    public_key: &[u8],
    c: &[Member],
) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"fhevm/epoch/");
    h.update(epoch.to_be_bytes());
    h.update((threshold as u64).to_be_bytes());
    len_prefixed(&mut h, public_key);
    h.update((c.len() as u64).to_be_bytes());
    for m in c {
        h.update(m.node_id);
        len_prefixed(&mut h, &m.public_key);
        h.update(m.weight.to_be_bytes());
        h.update((m.index as u64).to_be_bytes());
    }
    h.finalize().into()
}

// ---- persistence ----
//
// A compact, deterministic layout for the four records. It is NOT consensus:
// F commits no state root, so nothing about these bytes is signed or compared
// between nodes. What IS consensus — the derivations above, the transaction
// ids, the block ids — is computed from the transaction's own bytes and never
// from these.

/// Appends primitives; the reader below takes them back in the same order.
#[derive(Default)]
pub struct Writer(Vec<u8>);

impl Writer {
    pub fn u8(&mut self, v: u8) -> &mut Self {
        self.0.push(v);
        self
    }
    pub fn u32(&mut self, v: u32) -> &mut Self {
        self.0.extend_from_slice(&v.to_be_bytes());
        self
    }
    pub fn u64(&mut self, v: u64) -> &mut Self {
        self.0.extend_from_slice(&v.to_be_bytes());
        self
    }
    pub fn i64(&mut self, v: i64) -> &mut Self {
        self.u64(v as u64)
    }
    pub fn fixed(&mut self, v: &[u8]) -> &mut Self {
        self.0.extend_from_slice(v);
        self
    }
    pub fn var(&mut self, v: &[u8]) -> &mut Self {
        self.u32(v.len() as u32);
        self.0.extend_from_slice(v);
        self
    }
    pub fn take(&mut self) -> Vec<u8> {
        std::mem::take(&mut self.0)
    }
}

/// Takes primitives back. Every read is bounded; a short buffer answers `None`
/// and the caller drops the record rather than inventing one.
pub struct Reader<'a> {
    b: &'a [u8],
    at: usize,
}

impl<'a> Reader<'a> {
    pub fn new(b: &'a [u8]) -> Reader<'a> {
        Reader { b, at: 0 }
    }
    fn take(&mut self, n: usize) -> Option<&'a [u8]> {
        if self.at + n > self.b.len() {
            return None;
        }
        let s = &self.b[self.at..self.at + n];
        self.at += n;
        Some(s)
    }
    pub fn u8(&mut self) -> Option<u8> {
        self.take(1).map(|s| s[0])
    }
    pub fn u32(&mut self) -> Option<u32> {
        self.take(4).map(|s| u32::from_be_bytes(s.try_into().unwrap()))
    }
    pub fn u64(&mut self) -> Option<u64> {
        self.take(8).map(|s| u64::from_be_bytes(s.try_into().unwrap()))
    }
    pub fn i64(&mut self) -> Option<i64> {
        self.u64().map(|v| v as i64)
    }
    pub fn fixed<const N: usize>(&mut self) -> Option<[u8; N]> {
        self.take(N).map(|s| s.try_into().unwrap())
    }
    pub fn var(&mut self) -> Option<Vec<u8>> {
        let n = self.u32()? as usize;
        self.take(n).map(|s| s.to_vec())
    }
    pub fn text(&mut self) -> Option<String> {
        String::from_utf8(self.var()?).ok()
    }
}

fn write_votes(w: &mut Writer, votes: &[Attestation]) {
    w.u32(votes.len() as u32);
    for a in votes {
        w.fixed(&a.member).fixed(&a.value);
    }
}

fn read_votes(r: &mut Reader<'_>) -> Option<Vec<Attestation>> {
    let n = r.u32()? as usize;
    let mut out = Vec::with_capacity(n.min(1024));
    for _ in 0..n {
        out.push(Attestation { member: r.fixed()?, value: r.fixed()? });
    }
    Some(out)
}

impl Ciphertext {
    pub fn encode(&self) -> Vec<u8> {
        let mut w = Writer::default();
        w.fixed(&self.handle)
            .fixed(&self.owner)
            .u8(self.kind)
            .i64(self.level)
            .u64(self.epoch)
            .i64(self.registered_at)
            .u32(self.size)
            .fixed(&self.chain_id)
            .var(&self.scheme)
            .fixed(&self.digest);
        w.take()
    }

    pub fn decode(b: &[u8]) -> Option<Ciphertext> {
        let mut r = Reader::new(b);
        Some(Ciphertext {
            handle: r.fixed()?,
            owner: r.fixed()?,
            kind: r.u8()?,
            level: r.i64()?,
            epoch: r.u64()?,
            registered_at: r.i64()?,
            size: r.u32()?,
            chain_id: r.fixed()?,
            scheme: r.var()?,
            digest: r.fixed()?,
        })
    }
}

impl Permit {
    pub fn encode(&self) -> Vec<u8> {
        let mut w = Writer::default();
        w.fixed(&self.permit_id)
            .fixed(&self.handle)
            .fixed(&self.grantee)
            .fixed(&self.grantor)
            .u32(self.operations)
            .i64(self.expiry)
            .i64(self.created_at)
            .fixed(&self.chain_id)
            .u8(self.status);
        w.take()
    }

    pub fn decode(b: &[u8]) -> Option<Permit> {
        let mut r = Reader::new(b);
        Some(Permit {
            permit_id: r.fixed()?,
            handle: r.fixed()?,
            grantee: r.fixed()?,
            grantor: r.fixed()?,
            operations: r.u32()?,
            expiry: r.i64()?,
            created_at: r.i64()?,
            chain_id: r.fixed()?,
            status: r.u8()?,
        })
    }
}

impl Decrypt {
    pub fn encode(&self) -> Vec<u8> {
        let mut w = Writer::default();
        w.fixed(&self.request_id)
            .fixed(&self.handle)
            .fixed(&self.requester)
            .fixed(&self.callback)
            .fixed(&self.selector)
            .fixed(&self.source_chain)
            .u64(self.epoch)
            .u64(self.nonce)
            .i64(self.expiry)
            .u8(self.status)
            .i64(self.created_at)
            .i64(self.completed_at)
            .fixed(&self.result)
            .fixed(&self.permit_id);
        write_votes(&mut w, &self.attestations);
        w.take()
    }

    pub fn decode(b: &[u8]) -> Option<Decrypt> {
        let mut r = Reader::new(b);
        Some(Decrypt {
            request_id: r.fixed()?,
            handle: r.fixed()?,
            requester: r.fixed()?,
            callback: r.fixed()?,
            selector: r.fixed()?,
            source_chain: r.fixed()?,
            epoch: r.u64()?,
            nonce: r.u64()?,
            expiry: r.i64()?,
            status: r.u8()?,
            created_at: r.i64()?,
            completed_at: r.i64()?,
            result: r.fixed()?,
            permit_id: r.fixed()?,
            attestations: read_votes(&mut r)?,
        })
    }
}

impl Epoch {
    pub fn encode(&self) -> Vec<u8> {
        let mut w = Writer::default();
        w.u64(self.epoch).i64(self.start_time).i64(self.end_time).i64(self.threshold);
        w.var(&self.public_key).u8(self.status);
        w.u32(self.committee.len() as u32);
        for m in &self.committee {
            w.fixed(&m.node_id).var(&m.public_key).u64(m.weight).i64(m.index);
        }
        write_votes(&mut w, &self.attestations);
        w.take()
    }

    pub fn decode(b: &[u8]) -> Option<Epoch> {
        let mut r = Reader::new(b);
        let epoch = r.u64()?;
        let start_time = r.i64()?;
        let end_time = r.i64()?;
        let threshold = r.i64()?;
        let public_key = r.var()?;
        let status = r.u8()?;
        let n = r.u32()? as usize;
        let mut committee = Vec::with_capacity(n.min(1024));
        for _ in 0..n {
            committee.push(Member {
                node_id: r.fixed()?,
                public_key: r.var()?,
                weight: r.u64()?,
                index: r.i64()?,
            });
        }
        Some(Epoch {
            epoch,
            start_time,
            end_time,
            committee,
            threshold,
            public_key,
            status,
            attestations: read_votes(&mut r)?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::id::hex;

    fn acct(b: u8) -> Account {
        [b; 20]
    }

    // The corpus's own committee: one seat at node id 0x07, holding the payer's
    // own key. Its digest is pinned below against the subject the Go generator
    // signed, so a change to any of these hashes fails here rather than in a
    // differential run.
    fn member(pk: Vec<u8>) -> Member {
        Member { node_id: [7u8; 20], public_key: pk, weight: 1, index: 0 }
    }

    #[test]
    fn a_handle_names_the_body_under_its_scheme() {
        // The subject F_REGISTER carries, from the Go corpus.
        assert_eq!(
            hex(&derive_handle(&[0x21; 32], b"ckks-n14")),
            "924349cac6ed968b52c89997704438278bcc8247fd39937929cb6d145dc8294d"
        );
        // Same body, different scheme, different name — the scheme is length
        // prefixed into the hash, so no two can be re-split into each other.
        assert_ne!(derive_handle(&[0x21; 32], b"ckks-n14"), derive_handle(&[0x21; 32], b"ckks-n13"));
    }

    #[test]
    fn a_permit_id_carries_the_grantors_nonce() {
        let a = derive_permit_id(&[1; 32], &acct(2), &acct(3), 1, 0, 1);
        let b = derive_permit_id(&[1; 32], &acct(2), &acct(3), 1, 0, 2);
        assert_ne!(a, b, "the same grant twice must not collide");
    }

    #[test]
    fn a_request_id_carries_the_requesters_nonce() {
        assert_ne!(
            derive_request_id(&[1; 32], &acct(2), 1),
            derive_request_id(&[1; 32], &acct(2), 2)
        );
    }

    #[test]
    fn an_address_is_the_first_twenty_bytes_of_the_keys_hash() {
        let a = address_of(b"a public key");
        let h = Sha256::digest(b"a public key");
        assert_eq!(&a[..], &h[..20]);
    }

    #[test]
    fn a_committee_digest_is_the_proposal_and_not_its_encoding() {
        let c = vec![member(vec![9u8; 1952])];
        let d = committee_digest(1, 1, &[0x77; 32], &c);
        // Every field is in it: change any one and the members vote for
        // something else.
        assert_ne!(d, committee_digest(2, 1, &[0x77; 32], &c));
        assert_ne!(d, committee_digest(1, 2, &[0x77; 32], &c));
        assert_ne!(d, committee_digest(1, 1, &[0x78; 32], &c));
        let mut other = c.clone();
        other[0].weight = 2;
        assert_ne!(d, committee_digest(1, 1, &[0x77; 32], &other));
    }

    #[test]
    fn one_member_holds_one_vote_and_may_change_it() {
        let mut v = Vec::new();
        cast(&mut v, acct(1), [0xaa; 32]);
        cast(&mut v, acct(1), [0xaa; 32]);
        assert_eq!(tally(&v, &[0xaa; 32]), 1, "repeating yourself is not a second vote");
        cast(&mut v, acct(1), [0xbb; 32]);
        assert_eq!(tally(&v, &[0xaa; 32]), 0, "and a member cannot hedge across two values");
        assert_eq!(tally(&v, &[0xbb; 32]), 1);
        cast(&mut v, acct(2), [0xbb; 32]);
        assert_eq!(tally(&v, &[0xbb; 32]), 2);
    }

    #[test]
    fn a_committee_out_of_order_is_not_in_order() {
        let a = Member { node_id: [1; 20], ..member(vec![1]) };
        let b = Member { node_id: [2; 20], ..member(vec![2]) };
        assert!(in_order(&[a.clone(), b.clone()]));
        assert!(!in_order(&[b, a]));
        assert!(in_order(&[]));
    }

    #[test]
    fn a_seat_is_recognised_by_the_address_its_key_derives() {
        let e = Epoch { committee: vec![member(vec![5u8; 1952])], ..Epoch::default() };
        assert!(e.member_of(&address_of(&[5u8; 1952])));
        assert!(!e.member_of(&acct(9)));
    }

    #[test]
    fn every_record_reads_back_as_it_was_written() {
        let ct = Ciphertext {
            handle: [1; 32],
            owner: acct(2),
            kind: 3,
            level: -4,
            epoch: 5,
            registered_at: 6,
            size: 7,
            chain_id: [8; 32],
            scheme: b"ckks-n14".to_vec(),
            digest: [9; 32],
        };
        assert_eq!(Ciphertext::decode(&ct.encode()).unwrap(), ct);

        let pm = Permit {
            permit_id: [1; 32],
            handle: [2; 32],
            grantee: acct(3),
            grantor: acct(4),
            operations: 5,
            expiry: 6,
            created_at: 7,
            chain_id: [8; 32],
            status: STATUS_REVOKED,
        };
        assert_eq!(Permit::decode(&pm.encode()).unwrap(), pm);

        let dr = Decrypt {
            request_id: [1; 32],
            handle: [2; 32],
            requester: acct(3),
            callback: [4; 20],
            selector: [5; 4],
            source_chain: [6; 32],
            epoch: 7,
            nonce: 8,
            expiry: 9,
            status: REQUEST_COMPLETED,
            created_at: 10,
            completed_at: 11,
            result: [12; 32],
            permit_id: [13; 32],
            attestations: vec![Attestation { member: acct(14), value: [15; 32] }],
        };
        assert_eq!(Decrypt::decode(&dr.encode()).unwrap(), dr);

        let ep = Epoch {
            epoch: 1,
            start_time: 2,
            end_time: 3,
            committee: vec![member(vec![4u8; 1952])],
            threshold: 1,
            public_key: vec![5u8; 32],
            status: EPOCH_ENDED,
            attestations: vec![Attestation { member: acct(6), value: [7; 32] }],
        };
        assert_eq!(Epoch::decode(&ep.encode()).unwrap(), ep);
    }

    #[test]
    fn a_truncated_record_is_dropped_rather_than_half_read() {
        let ep = Epoch { epoch: 1, committee: vec![member(vec![4u8; 32])], ..Epoch::default() };
        let b = ep.encode();
        for cut in [0, 1, b.len() / 2, b.len() - 1] {
            assert!(Epoch::decode(&b[..cut]).is_none(), "cut at {cut}");
        }
        assert!(Epoch::decode(&b).is_some());
    }
}
