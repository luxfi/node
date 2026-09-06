// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! An F-Chain transaction: the six operations, and what each one has to say
//! about itself before the chain will look at it.
//!
//! Together the six are the whole life of a confidential value on F. It is
//! registered; capabilities over it are granted and revoked; its decryption is
//! requested, and the committee answers. The committee that answers is itself
//! rotated by the sixth.
//!
//! WHAT THE SIGNATURE COVERS. `subject` names the OBJECT the operation acts on,
//! and it is bound into the content the payer signs. Syntactic verification
//! then requires it to equal what the payload derives — so an operation cannot
//! be re-aimed at a different object after it was signed, and swapping the
//! payload for one describing a different ciphertext has to break one of the
//! two: either the handle no longer matches the body, or the signature no
//! longer matches the content.
//!
//! TWO LAYERS, AND THE LINE BETWEEN THEM. [`Transaction::syntactic_verify`] is
//! everything decidable without the chain: the operation is one F runs, the
//! payload decodes as exactly its schema, the fields are in range, and the
//! whole thing is PRICEABLE. [`Transaction::check_auth`] is everything that
//! needs the chain: does the ciphertext exist, is the payer its owner, is the
//! permit still active, is the payer on the committee. Nothing decidable
//! without state is left to the second, because a rule that needs no state and
//! is checked with it is a rule a chain in a different state answers
//! differently.

use sha2::{Digest, Sha256};

use crate::error::{Code, Error, Result};
use crate::gas;
use crate::id::{Account, Id, NodeId};
use crate::json::{self, Fields};
use crate::pq;
use crate::record::{
    self, Ciphertext, Decrypt, Epoch, Member, Permit, EPOCH_ACTIVE, EPOCH_ENDED,
    PERMIT_OP_DECRYPT, PERMIT_OP_MASK, REQUEST_COMPLETED, REQUEST_PENDING, STATUS_ACTIVE,
    STATUS_REVOKED,
};
use crate::vm::Vm;
use crate::wire;

/// Record a ciphertext's PUBLIC handle and metadata.
pub const TX_REGISTER_CIPHERTEXT: u8 = 1;
/// The owner grants a capability over a handle.
pub const TX_GRANT_PERMIT: u8 = 2;
/// The owner withdraws a capability it granted.
pub const TX_REVOKE_PERMIT: u8 = 3;
/// A permitted grantee asks the committee to decrypt.
pub const TX_REQUEST_DECRYPT: u8 = 4;
/// A committee member attests the PUBLIC result.
pub const TX_FULFILL_DECRYPT: u8 = 5;
/// The committee installs its successor.
pub const TX_ADVANCE_EPOCH: u8 = 6;

/// What an operation's encoding may occupy. Set by the largest legitimate
/// payload — a full-committee epoch proposal, whose members each carry an
/// ML-DSA-65 public key — with room to spare.
pub const MAX_PAYLOAD: usize = 128 * 1024;

/// What a scheme name may occupy. Scheme names are short labels (`ckks-n14`);
/// anything longer is a channel, not a name.
pub const MAX_SCHEME: usize = 32;

/// How many seats a committee may hold. This bounds the epoch payload, and with
/// it the public-key parsing an UNAUTHENTICATED transaction can demand before
/// its own signature is checked.
pub const MAX_COMMITTEE: usize = 32;

/// The largest off-chain body a registration may describe. The body is not
/// stored here, but a size nobody could ever serve describes nothing.
pub const MAX_CIPHERTEXT_SIZE: u32 = 1 << 30;

/// How long a decryption request stays answerable when the requester names no
/// expiry. A request no committee can still answer is dead weight in state, and
/// an unbounded one would be answerable by a future committee that never saw
/// the permit that authorized it.
pub const DEFAULT_REQUEST_WINDOW: i64 = 3600;

/// The domain that separates a transaction preimage from every other thing this
/// chain hashes, so no other digest can be mistaken for a payer's signature
/// over a transaction.
pub const TX_DOMAIN: &[u8] = b"fhevm/tx/";

/// One consensus transaction.
///
/// The header is deterministic binary; `payload` is an opaque, PUBLIC,
/// operation-specific JSON blob; `auth` is the payer's ML-DSA-65 public key and
/// `sig` its signature over the signing preimage. Nothing here is or can become
/// a ciphertext, a plaintext or a key share.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Transaction {
    pub tx_type: u8,
    /// FHE scheme and ring dimension, which drives the per-scheme price. Empty
    /// for the operations that touch no ciphertext.
    ///
    /// Bytes, not text. The reference's field is a Go `string`, which is a byte
    /// string and not a validated encoding, and the wire is canonical — so a
    /// scheme holding bytes that are not UTF-8 round-trips there and must round
    /// trip here. Validating it into text would refuse, as malformed, a
    /// transaction the reference parses and then refuses for the rule it
    /// actually broke.
    pub scheme: Vec<u8>,
    /// Fee payer and authorization subject.
    pub payer: Account,
    /// The object this operation acts on or creates.
    pub subject: [u8; 32],
    pub gas_limit: u64,
    /// The payer's replay and ordering nonce.
    pub nonce: u64,
    pub payload: Vec<u8>,
    /// The payer's ML-DSA-65 PUBLIC key.
    pub auth: Vec<u8>,
    /// The payer's signature over [`wire::signing_bytes`].
    pub sig: Vec<u8>,
}

impl Transaction {
    /// The transaction's content hash, over its full wire encoding.
    pub fn id(&self) -> Id {
        Sha256::digest(wire::bytes(self)).into()
    }

    /// The canonical wire encoding.
    pub fn bytes(&self) -> Vec<u8> {
        wire::bytes(self)
    }

    /// The preimage the payer signs on `chain`.
    pub fn signing_bytes(&self, chain: &Id) -> Vec<u8> {
        wire::signing_bytes(self, chain)
    }

    /// The operation's canonical name — the wire's word for it, with no
    /// language's spelling in it.
    pub fn kind(&self) -> &'static str {
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => "RegisterCiphertext",
            TX_GRANT_PERMIT => "GrantPermit",
            TX_REVOKE_PERMIT => "RevokePermit",
            TX_REQUEST_DECRYPT => "RequestDecrypt",
            TX_FULFILL_DECRYPT => "FulfillDecrypt",
            TX_ADVANCE_EPOCH => "AdvanceEpoch",
            _ => "unknown",
        }
    }

    /// Well-formed and priceable, without any state.
    ///
    /// Bounds come FIRST, before anything is decoded or parsed, so the work an
    /// unauthenticated transaction can demand is bounded by its own size.
    pub fn syntactic_verify(&self) -> Result<()> {
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT | TX_GRANT_PERMIT | TX_REVOKE_PERMIT | TX_REQUEST_DECRYPT
            | TX_FULFILL_DECRYPT | TX_ADVANCE_EPOCH => {}
            _ => return Err(Error::new(Code::InvalidTxType)),
        }
        if self.payload.len() > MAX_PAYLOAD {
            return Err(Error::detail(
                Code::InvalidPayload,
                format!("payload {} exceeds {} bytes", self.payload.len(), MAX_PAYLOAD),
            ));
        }
        if self.scheme.len() > MAX_SCHEME {
            return Err(Error::detail(
                Code::InvalidPayload,
                format!("scheme {} exceeds {} bytes", self.scheme.len(), MAX_SCHEME),
            ));
        }
        // Auth and signature are fixed-width by algorithm. Pinning them keeps
        // them from becoming a third byte channel the base cost carries free.
        if !self.auth.is_empty() && self.auth.len() != pq::public_key_size() {
            return Err(Error::detail(
                Code::InvalidPayload,
                format!("auth is {} bytes, not {}", self.auth.len(), pq::public_key_size()),
            ));
        }
        if !self.sig.is_empty() && self.sig.len() != pq::signature_size() {
            return Err(Error::detail(
                Code::InvalidPayload,
                format!("signature is {} bytes, not {}", self.sig.len(), pq::signature_size()),
            ));
        }
        // Pricing also decides scheme membership for the operations that name
        // one, so an unpriceable transaction is refused here rather than
        // reaching a chain that would have to charge it nothing.
        gas::gas_for(self)?;
        if self.nonce == 0 {
            return Err(Error::detail(Code::BadNonce, "nonce starts at 1"));
        }

        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => {
                let p = Register::decode(&self.payload)?;
                if p.digest == [0u8; 32] {
                    return Err(Error::detail(
                        Code::InvalidPayload,
                        "register: empty ciphertext digest",
                    ));
                }
                if p.size == 0 || p.size > MAX_CIPHERTEXT_SIZE {
                    return Err(Error::detail(
                        Code::InvalidPayload,
                        format!("register: ciphertext size {}", p.size),
                    ));
                }
                if p.level < 0 {
                    return Err(Error::detail(Code::InvalidPayload, "register: negative level"));
                }
                if self.subject != record::derive_handle(&p.digest, &self.scheme) {
                    return Err(Error::detail(
                        Code::HandleMismatch,
                        "handle does not match digest+scheme",
                    ));
                }
            }

            TX_GRANT_PERMIT => {
                let p = Grant::decode(&self.payload)?;
                if p.operations == 0 {
                    return Err(Error::detail(
                        Code::InvalidPayload,
                        "grant confers no operation",
                    ));
                }
                if p.operations & !PERMIT_OP_MASK != 0 {
                    return Err(Error::detail(
                        Code::InvalidPayload,
                        "unknown permit operation bits",
                    ));
                }
                if p.expiry < 0 {
                    return Err(Error::detail(Code::InvalidPayload, "negative expiry"));
                }
            }

            TX_REVOKE_PERMIT => {
                Revoke::decode(&self.payload)?;
            }

            TX_REQUEST_DECRYPT => {
                let p = Request::decode(&self.payload)?;
                if p.permit_id == [0u8; 32] {
                    return Err(Error::detail(Code::InvalidPayload, "request names no permit"));
                }
                if p.expiry < 0 {
                    return Err(Error::detail(Code::InvalidPayload, "negative expiry"));
                }
            }

            TX_FULFILL_DECRYPT => {
                let p = Fulfill::decode(&self.payload)?;
                if p.result == [0u8; 32] {
                    return Err(Error::detail(
                        Code::InvalidPayload,
                        "fulfill carries no result handle",
                    ));
                }
            }

            TX_ADVANCE_EPOCH => {
                let p = Advance::decode(&self.payload)?;
                validate_committee(&p.committee, p.threshold, &p.public_key)?;
                if self.subject
                    != record::committee_digest(
                        p.epoch,
                        p.threshold,
                        &p.public_key,
                        &p.committee,
                    )
                {
                    return Err(Error::detail(
                        Code::HandleMismatch,
                        "subject does not match the proposed committee",
                    ));
                }
            }

            _ => unreachable!("the operation was checked at the top"),
        }
        Ok(())
    }

    /// Whether the payer authorized this transaction ON THIS CHAIN.
    ///
    /// PUBLIC only: parse the payer's key, require it to hash to `payer`, and
    /// verify the signature over the preimage this chain id binds. No secret
    /// material is touched.
    ///
    /// `chain` is the VERIFYING node's own chain id, never one the transaction
    /// carries — so a transaction signed for another F-Chain fails here rather
    /// than spending a balance the payer holds at the same address on every
    /// chain.
    pub fn authenticate(&self, chain: &Id) -> Result<()> {
        if self.auth.is_empty() || self.sig.is_empty() {
            return Err(Error::new(Code::UnsignedTx));
        }
        if record::address_of(&self.auth) != self.payer {
            return Err(Error::new(Code::PayerMismatch));
        }
        if !pq::is_public_key(&self.auth) {
            return Err(Error::new(Code::BadPublicKey));
        }
        if !pq::verify(&self.auth, &self.signing_bytes(chain), &self.sig) {
            return Err(Error::new(Code::BadSignature));
        }
        Ok(())
    }

    /// The state entry this transaction writes, in 32 bytes — and, where an
    /// entry legitimately takes a write from each of several actors, which
    /// actor writes it.
    ///
    /// Two transactions sharing an effect cannot both take place: the second
    /// would find the ciphertext already registered, the permit already
    /// withdrawn, or the member already counted, and abort the block that
    /// carried them both.
    ///
    /// Nonces do not catch that on their own. A payer's nonces n and n+1 are
    /// both valid, so one payer can build two transactions that individually
    /// pass every check and together spend a block on work only one of them can
    /// do. Naming the effect lets admission refuse the second before it is
    /// queued and lets consensus refuse a peer's block that contains both — one
    /// function, both layers, no drift.
    pub fn effect(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update([self.tx_type]);
        match self.tx_type {
            TX_ADVANCE_EPOCH => {
                // The DECISION, not the proposal. Exactly one epoch is ever
                // open, so a member's vote is named by the member alone. Naming
                // it by subject instead made two votes for two committees two
                // different effects: both passed admission, both passed verify
                // against committed state, and accept applied the first and
                // refused the second. A block every validator certifies and no
                // validator can apply halts the chain at that height, which is
                // the whole thing this function exists to stop.
                h.update(self.payer);
            }
            TX_REGISTER_CIPHERTEXT | TX_REVOKE_PERMIT => {
                // Named by the subject alone: one registration per handle, one
                // withdrawal per permit, whoever asks for it.
                h.update(self.subject);
            }
            TX_FULFILL_DECRYPT => {
                // One write per member per request. The voted value lives in
                // the payload, so the effect is the member's vote on THIS
                // request rather than the result it names.
                h.update(self.subject);
                h.update(self.payer);
            }
            _ => {
                // A grant and a request CREATE an entry whose id already
                // carries the payer and its nonce, so no two of them collide —
                // the same owner may grant twice over one handle, to different
                // grantees, in one block. Qualifying by the same two fields
                // keeps that true here.
                h.update(self.subject);
                h.update(self.payer);
                h.update(self.nonce.to_be_bytes());
            }
        }
        h.finalize().into()
    }
}

/// Whether a committee is installable: non-empty, canonically ordered by node
/// id, free of duplicates, with a real threshold, and with every member
/// carrying a parseable ML-DSA-65 public key.
///
/// The last check is what stops F being wedged by an epoch whose members can
/// never sign an attestation — the committee that cannot speak can never be
/// replaced either. Genesis and the advance operation both come through here,
/// so the two can never disagree about what an installable committee is.
pub fn validate_committee(c: &[Member], threshold: i64, public_key: &[u8]) -> Result<()> {
    if c.is_empty() {
        return Err(Error::detail(Code::InvalidCommittee, "empty committee"));
    }
    if c.len() > MAX_COMMITTEE {
        return Err(Error::detail(
            Code::InvalidCommittee,
            format!("{} members exceeds {}", c.len(), MAX_COMMITTEE),
        ));
    }
    if threshold <= 0 || threshold > c.len() as i64 {
        return Err(Error::detail(
            Code::InvalidThreshold,
            format!("threshold {} of {}", threshold, c.len()),
        ));
    }
    if public_key.is_empty() {
        return Err(Error::detail(Code::InvalidCommittee, "no network public key"));
    }
    if !record::in_order(c) {
        return Err(Error::detail(
            Code::InvalidCommittee,
            "members not in canonical node-ID order",
        ));
    }
    // A seat is only a vote if a DISTINCT ACCOUNT can cast it. Membership is
    // tested by the address a member's key derives — the same derivation that
    // authenticates a payer — so that is what has to be unique. Deduplicating
    // node ids instead let n seats share one key: the committee passed every
    // check, reported itself fully seated, and could never reach its own
    // threshold, for a decryption or for rotating itself out.
    let mut voters: Vec<Account> = Vec::with_capacity(c.len());
    for (i, m) in c.iter().enumerate() {
        if i > 0 && c[i - 1].node_id == m.node_id {
            return Err(Error::detail(
                Code::InvalidCommittee,
                format!("duplicate member {}", seat(&m.node_id)),
            ));
        }
        if !pq::is_public_key(&m.public_key) {
            return Err(Error::detail(
                Code::InvalidCommittee,
                format!("member {} key: invalid key size", seat(&m.node_id)),
            ));
        }
        let acct = record::address_of(&m.public_key);
        if voters.contains(&acct) {
            return Err(Error::detail(
                Code::InvalidCommittee,
                format!("member {} shares a voting identity with another seat", seat(&m.node_id)),
            ));
        }
        voters.push(acct);
    }
    Ok(())
}

fn seat(n: &NodeId) -> String {
    crate::id::node_id_to_string(n)
}

// ---- the operation payloads. Every field is PUBLIC. ----

/// An encrypted value's PUBLIC coordinates.
///
/// `digest` is the hash of the ciphertext BODY, which lives in off-chain
/// storage — F stores the hash so anyone can check a fetched body against the
/// chain, and never the body itself.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Register {
    pub digest: [u8; 32],
    /// The FHE plaintext type tag.
    pub kind: u8,
    /// Remaining multiplicative level.
    pub level: i64,
    /// The body's size in bytes.
    pub size: u32,
}

impl Register {
    pub fn decode(payload: &[u8]) -> Result<Register> {
        let mut f = Fields::open(json::value(payload, "register")?, "register")?;
        let p = Register {
            digest: f.bytes("digest")?,
            kind: f.u8("type")?,
            level: f.int("level")?,
            size: f.u32("size")?,
        };
        f.done()?;
        Ok(p)
    }
}

/// A capability over one handle, for one grantee, until `expiry`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Grant {
    pub grantee: Account,
    /// A bitmask of the capability bits.
    pub operations: u32,
    /// Unix seconds; zero is no expiry.
    pub expiry: i64,
}

impl Grant {
    pub fn decode(payload: &[u8]) -> Result<Grant> {
        let mut f = Fields::open(json::value(payload, "grant")?, "grant")?;
        let p = Grant {
            grantee: f.account("grantee")?,
            operations: f.u32("operations")?,
            expiry: f.i64("expiry")?,
        };
        f.done()?;
        Ok(p)
    }
}

/// The withdrawal of a permit.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Revoke {
    pub reason: String,
}

impl Revoke {
    pub fn decode(payload: &[u8]) -> Result<Revoke> {
        let mut f = Fields::open(json::value(payload, "revoke")?, "revoke")?;
        let p = Revoke { reason: f.string("reason")? };
        f.done()?;
        Ok(p)
    }
}

/// An ask for a threshold decryption, under the authority of a permit the
/// requester holds. `callback` and `selector` name where the answer should be
/// delivered on the source chain.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Request {
    pub permit_id: [u8; 32],
    pub callback: [u8; 20],
    pub selector: [u8; 4],
    /// Unix seconds; zero is the chain's default window.
    pub expiry: i64,
}

impl Request {
    pub fn decode(payload: &[u8]) -> Result<Request> {
        let mut f = Fields::open(json::value(payload, "request")?, "request")?;
        let p = Request {
            permit_id: f.bytes("permitId")?,
            callback: f.bytes("callback")?,
            selector: f.bytes("selector")?,
            expiry: f.i64("expiry")?,
        };
        f.done()?;
        Ok(p)
    }
}

/// One committee member's attestation of the PUBLIC handle a threshold
/// decryption produced. The plaintext itself is delivered off-chain to the
/// callback; F records only which handle the committee agreed on.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Fulfill {
    pub result: [u8; 32],
}

impl Fulfill {
    pub fn decode(payload: &[u8]) -> Result<Fulfill> {
        let mut f = Fields::open(json::value(payload, "fulfill")?, "fulfill")?;
        let p = Fulfill { result: f.bytes("result")? };
        f.done()?;
        Ok(p)
    }
}

/// The next epoch's committee, and the network public key it jointly generated.
/// Every approving member sends the identical proposal; the epoch installs when
/// `threshold` of the CURRENT committee have.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Advance {
    pub epoch: u64,
    pub committee: Vec<Member>,
    pub threshold: i64,
    pub public_key: Vec<u8>,
}

impl Advance {
    pub fn decode(payload: &[u8]) -> Result<Advance> {
        let mut f = Fields::open(json::value(payload, "advance")?, "advance")?;
        let epoch = f.u64("epoch")?;
        let committee = f.list("committee", "[]fhe.CommitteeMember", |mut m| {
            let member = Member {
                node_id: m.node_id("node_id")?,
                public_key: m.base64("public_key")?,
                weight: m.u64("weight")?,
                index: m.int("index")?,
            };
            m.done()?;
            Ok(member)
        })?;
        let p = Advance {
            epoch,
            committee,
            threshold: f.int("threshold")?,
            public_key: f.base64("publicKey")?,
        };
        f.done()?;
        Ok(p)
    }
}

// ---- what needs the chain ----

impl Transaction {
    /// The single, READ-ONLY authorization predicate: may this transaction take
    /// effect against the CURRENT committed state at time `now`?
    ///
    /// It mutates nothing. It is the one place F's access model is enforced,
    /// called at three layers so an unauthorized transaction is refused at the
    /// earliest gate and never charged: admission, consensus, and — as defence
    /// in depth — application.
    pub fn check_auth(&self, vm: &Vm, now: i64) -> Result<()> {
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => {
                if vm.ciphertext(&self.subject).is_some() {
                    return Err(Error::new(Code::CiphertextExists));
                }
                Ok(())
            }

            TX_GRANT_PERMIT => {
                let ct = vm
                    .ciphertext(&self.subject)
                    .ok_or_else(|| Error::new(Code::CiphertextNotFound))?;
                // Only the owner may confer a capability over its ciphertext —
                // and, because only the owner grants, only the owner revokes.
                if ct.owner != self.payer {
                    return Err(Error::new(Code::Unauthorized));
                }
                Ok(())
            }

            TX_REVOKE_PERMIT => {
                let pm =
                    vm.permit(&self.subject).ok_or_else(|| Error::new(Code::PermitNotFound))?;
                if pm.status != STATUS_ACTIVE {
                    return Err(Error::new(Code::PermitRevoked));
                }
                if pm.grantor != self.payer {
                    return Err(Error::new(Code::Unauthorized));
                }
                Ok(())
            }

            TX_REQUEST_DECRYPT => {
                let p = Request::decode(&self.payload)?;
                if vm.ciphertext(&self.subject).is_none() {
                    return Err(Error::new(Code::CiphertextNotFound));
                }
                let pm =
                    vm.permit(&p.permit_id).ok_or_else(|| Error::new(Code::PermitNotFound))?;
                if pm.status != STATUS_ACTIVE {
                    return Err(Error::new(Code::PermitRevoked));
                }
                if pm.handle != self.subject {
                    return Err(Error::detail(
                        Code::PermitInvalid,
                        "permit is for another handle",
                    ));
                }
                if pm.grantee != self.payer {
                    return Err(Error::new(Code::Unauthorized));
                }
                if pm.expiry != 0 && now > pm.expiry {
                    return Err(Error::new(Code::PermitExpired));
                }
                if pm.operations & PERMIT_OP_DECRYPT == 0 {
                    return Err(Error::detail(
                        Code::PermitInvalid,
                        "permit does not confer decrypt",
                    ));
                }
                // The request's id carries the requester's nonce, and a nonce is
                // used once, so a request cannot collide with an existing one.
                // That is the whole uniqueness argument — there is no second
                // check to keep in step with it.
                Ok(())
            }

            TX_FULFILL_DECRYPT => {
                let req =
                    vm.decrypt(&self.subject).ok_or_else(|| Error::new(Code::RequestNotFound))?;
                if req.status != REQUEST_PENDING {
                    return Err(Error::new(Code::RequestClosed));
                }
                if req.expiry != 0 && now > req.expiry {
                    return Err(Error::new(Code::RequestExpired));
                }
                // The permit that authorized the ask must still authorize it.
                // Revocation is a withdrawal of consent and reaches a request
                // already in flight — otherwise an owner who revoked would watch
                // the committee answer anyway and deliver the plaintext to the
                // callback.
                //
                // Expiry is deliberately NOT re-checked. It bounds when the
                // grantee may ASK; the grantee asked in time, and the committee
                // answering afterwards is not the grantee acting. Re-checking it
                // would make any permit shorter than a decryption round useless.
                let pm =
                    vm.permit(&req.permit_id).ok_or_else(|| Error::new(Code::PermitNotFound))?;
                if pm.status != STATUS_ACTIVE {
                    return Err(Error::new(Code::PermitRevoked));
                }
                // Only the committee of the epoch the request was made in may
                // answer it: a later committee holds different shares and never
                // saw the permit.
                let ep = vm.epoch(req.epoch).ok_or_else(|| Error::new(Code::EpochNotFound))?;
                if !ep.member_of(&self.payer) {
                    return Err(Error::new(Code::NotCommittee));
                }
                Ok(())
            }

            TX_ADVANCE_EPOCH => {
                let p = Advance::decode(&self.payload)?;
                let cur = vm.current_epoch();
                if p.epoch != cur.epoch + 1 {
                    return Err(Error::detail(
                        Code::EpochMismatch,
                        format!("proposed {}, next is {}", p.epoch, cur.epoch + 1),
                    ));
                }
                // Only the sitting committee decides its successor.
                if !cur.member_of(&self.payer) {
                    return Err(Error::new(Code::NotCommittee));
                }
                Ok(())
            }

            _ => Err(Error::new(Code::InvalidTxType)),
        }
    }

    /// Where authorization is DECIDED and where state changes.
    ///
    /// It runs inside block acceptance, writing through the buffer, so an
    /// effect commits atomically with the fee burn. `now` is the accepting
    /// block's time — every timestamp F stores comes from here, never from a
    /// validator's clock.
    ///
    /// It reports whether the transaction took effect. One that fails
    /// authorization REVERTS: the answer is `false`, state is untouched, and
    /// the caller still burns the fee and consumes the nonce. This is the only
    /// place the verdict is reached, so every validator reaches it from the
    /// same committed state in the same order. Deciding it earlier would let a
    /// block pass consensus on state an earlier transaction in the same block
    /// then changes — and a block every validator certifies and no validator
    /// can apply halts the chain.
    ///
    /// An error means the write itself failed, which no validator can proceed
    /// past; the caller rolls the whole block back.
    pub fn apply(&self, vm: &mut Vm, now: i64) -> Result<bool> {
        if self.check_auth(vm, now).is_err() {
            return Ok(false);
        }
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => self.apply_register(vm, now)?,
            TX_GRANT_PERMIT => self.apply_grant(vm, now)?,
            TX_REVOKE_PERMIT => self.apply_revoke(vm)?,
            TX_REQUEST_DECRYPT => self.apply_request(vm, now)?,
            TX_FULFILL_DECRYPT => self.apply_fulfill(vm, now)?,
            TX_ADVANCE_EPOCH => self.apply_advance(vm, now)?,
            // Unreachable: syntactic verification refuses an unknown operation
            // before a transaction can reach a block. Treated as a refusal, not
            // a halt.
            _ => return Ok(false),
        }
        Ok(true)
    }

    fn apply_register(&self, vm: &mut Vm, now: i64) -> Result<()> {
        let p = Register::decode(&self.payload)?;
        let rec = Ciphertext {
            handle: self.subject,
            owner: self.payer,
            kind: p.kind,
            level: p.level,
            epoch: vm.current_epoch().epoch,
            registered_at: now,
            size: p.size,
            chain_id: *vm.chain_id(),
            scheme: self.scheme.clone(),
            digest: p.digest,
        };
        vm.put_ciphertext(&rec);
        Ok(())
    }

    fn apply_grant(&self, vm: &mut Vm, now: i64) -> Result<()> {
        let p = Grant::decode(&self.payload)?;
        let rec = Permit {
            permit_id: record::derive_permit_id(
                &self.subject,
                &self.payer,
                &p.grantee,
                p.operations,
                p.expiry,
                self.nonce,
            ),
            handle: self.subject,
            grantee: p.grantee,
            grantor: self.payer,
            operations: p.operations,
            expiry: p.expiry,
            created_at: now,
            chain_id: *vm.chain_id(),
            status: STATUS_ACTIVE,
        };
        vm.put_permit(&rec);
        Ok(())
    }

    fn apply_revoke(&self, vm: &mut Vm) -> Result<()> {
        let mut pm =
            vm.permit(&self.subject).ok_or_else(|| Error::new(Code::PermitNotFound))?;
        pm.status = STATUS_REVOKED;
        vm.put_permit(&pm);
        Ok(())
    }

    fn apply_request(&self, vm: &mut Vm, now: i64) -> Result<()> {
        let p = Request::decode(&self.payload)?;
        let expiry = if p.expiry == 0 { now + DEFAULT_REQUEST_WINDOW } else { p.expiry };
        let rec = Decrypt {
            request_id: record::derive_request_id(&self.subject, &self.payer, self.nonce),
            handle: self.subject,
            requester: self.payer,
            callback: p.callback,
            selector: p.selector,
            source_chain: *vm.chain_id(),
            epoch: vm.current_epoch().epoch,
            nonce: self.nonce,
            expiry,
            status: REQUEST_PENDING,
            created_at: now,
            completed_at: 0,
            result: [0u8; 32],
            permit_id: p.permit_id,
            attestations: Vec::new(),
        };
        vm.put_decrypt(&rec);
        Ok(())
    }

    fn apply_fulfill(&self, vm: &mut Vm, now: i64) -> Result<()> {
        let p = Fulfill::decode(&self.payload)?;
        let mut req =
            vm.decrypt(&self.subject).ok_or_else(|| Error::new(Code::RequestNotFound))?;
        let ep = vm.epoch(req.epoch).ok_or_else(|| Error::new(Code::EpochNotFound))?;
        record::cast(&mut req.attestations, self.payer, p.result);
        // The request completes the moment a threshold of DISTINCT members have
        // named the same handle. A member that names a different one is counted
        // against that value alone, so it delays nothing and pays for the
        // privilege.
        if record::tally(&req.attestations, &p.result) >= ep.threshold {
            req.status = REQUEST_COMPLETED;
            req.result = p.result;
            req.completed_at = now;
        }
        vm.put_decrypt(&req);
        Ok(())
    }

    fn apply_advance(&self, vm: &mut Vm, now: i64) -> Result<()> {
        let p = Advance::decode(&self.payload)?;
        let mut cur = vm.current_epoch();
        record::cast(&mut cur.attestations, self.payer, self.subject);
        if record::tally(&cur.attestations, &self.subject) < cur.threshold {
            // Not yet decided: record the vote against the sitting epoch and
            // stop.
            vm.put_epoch(&cur);
            return Ok(());
        }
        // Decided. The transaction carrying the deciding vote also carries the
        // proposal itself, so no proposal body is ever stored while it is
        // pending — the digest the members attested is the whole record of what
        // they agreed.
        cur.end_time = now;
        cur.status = EPOCH_ENDED;
        vm.put_epoch(&cur);
        let next = Epoch {
            epoch: p.epoch,
            start_time: now,
            end_time: 0,
            committee: p.committee,
            threshold: p.threshold,
            public_key: p.public_key,
            status: EPOCH_ACTIVE,
            attestations: Vec::new(),
        };
        vm.put_epoch(&next);
        vm.set_current_epoch(p.epoch);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::id::hex;

    fn register_payload(digest: [u8; 32]) -> Vec<u8> {
        let nums: Vec<String> = digest.iter().map(|b| b.to_string()).collect();
        format!(
            r#"{{"digest":[{}],"type":1,"level":3,"size":4096}}"#,
            nums.join(",")
        )
        .into_bytes()
    }

    fn register(digest: [u8; 32], scheme: &str) -> Transaction {
        Transaction {
            tx_type: TX_REGISTER_CIPHERTEXT,
            scheme: scheme.as_bytes().to_vec(),
            subject: record::derive_handle(&digest, scheme.as_bytes()),
            gas_limit: 5_000_000,
            nonce: 1,
            payload: register_payload(digest),
            ..Transaction::default()
        }
    }

    fn key(b: u8) -> Vec<u8> {
        vec![b; 1952]
    }

    fn member(node: u8, k: u8) -> Member {
        Member { node_id: [node; 20], public_key: key(k), weight: 1, index: 0 }
    }

    #[test]
    fn a_correct_registration_is_well_formed() {
        register([0x21; 32], "ckks-n14").syntactic_verify().expect("well formed");
    }

    #[test]
    fn an_operation_f_does_not_run_is_refused_by_name() {
        let mut t = register([0x21; 32], "ckks-n14");
        t.tx_type = 9;
        assert_eq!(t.syntactic_verify().unwrap_err().code, Code::InvalidTxType);
        assert_eq!(t.kind(), "unknown");
    }

    #[test]
    fn a_nonce_starts_at_one() {
        let mut t = register([0x21; 32], "ckks-n14");
        t.nonce = 0;
        assert_eq!(t.syntactic_verify().unwrap_err().code, Code::BadNonce);
    }

    #[test]
    fn a_subject_that_is_not_what_the_payload_derives_is_refused() {
        let mut t = register([0x21; 32], "ckks-n14");
        t.subject = [0x99; 32];
        assert_eq!(t.syntactic_verify().unwrap_err().code, Code::HandleMismatch);
    }

    #[test]
    fn swapping_the_ciphertext_after_signing_breaks_the_handle() {
        // The attack the signing preimage exists to stop. The subject still
        // names the body that was signed for; the payload now describes another
        // one, and the two can no longer agree.
        let mut t = register([0x21; 32], "ckks-n14");
        t.payload = register_payload([0x99; 32]);
        assert_eq!(t.syntactic_verify().unwrap_err().code, Code::HandleMismatch);
    }

    #[test]
    fn a_registration_names_a_body_that_exists_and_has_a_size() {
        let mut t = register([0u8; 32], "ckks-n14");
        assert!(t.syntactic_verify().unwrap_err().detail.contains("empty ciphertext digest"));

        t = register([0x21; 32], "ckks-n14");
        t.payload = br#"{"digest":[33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33],"type":1,"level":3}"#.to_vec();
        assert!(t.syntactic_verify().unwrap_err().detail.contains("ciphertext size 0"));

        t.payload = br#"{"digest":[33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33,33],"type":1,"level":-1,"size":4096}"#.to_vec();
        assert!(t.syntactic_verify().unwrap_err().detail.contains("negative level"));
    }

    #[test]
    fn a_scheme_f_cannot_price_is_refused_by_name() {
        let t = register([0x21; 32], "no-such-scheme");
        let e = t.syntactic_verify().unwrap_err();
        assert_eq!(e.code, Code::UnknownScheme);
        assert_eq!(e.class(), crate::error::UNSUPPORTED);
    }

    #[test]
    fn a_scheme_or_payload_longer_than_its_bound_is_refused_before_it_is_decoded() {
        let mut t = register([0x21; 32], "ckks-n14");
        t.scheme = vec![b'x'; MAX_SCHEME + 1];
        assert!(t.syntactic_verify().unwrap_err().detail.contains("scheme 33 exceeds 32"));

        let mut t = register([0x21; 32], "ckks-n14");
        t.payload = vec![b'x'; MAX_PAYLOAD + 1];
        assert!(t.syntactic_verify().unwrap_err().detail.contains("exceeds 131072"));
    }

    #[test]
    fn an_auth_or_signature_of_the_wrong_width_is_refused() {
        let mut t = register([0x21; 32], "ckks-n14");
        t.auth = vec![0u8; 8];
        assert!(t.syntactic_verify().unwrap_err().detail.contains("auth is 8 bytes, not 1952"));

        let mut t = register([0x21; 32], "ckks-n14");
        t.sig = vec![0u8; 8];
        assert!(t
            .syntactic_verify()
            .unwrap_err()
            .detail
            .contains("signature is 8 bytes, not 3309"));
    }

    #[test]
    fn a_grant_confers_something_and_only_bits_that_exist() {
        let mut t = Transaction {
            tx_type: TX_GRANT_PERMIT,
            subject: [1; 32],
            gas_limit: 5_000_000,
            nonce: 1,
            payload: br#"{"grantee":"NG42sa8bZqRUjcaoq9igh5toYw8mXmw7H","operations":1,"expiry":0}"#
                .to_vec(),
            ..Transaction::default()
        };
        t.syntactic_verify().expect("well formed");

        t.payload = br#"{"grantee":"NG42sa8bZqRUjcaoq9igh5toYw8mXmw7H","operations":0,"expiry":0}"#
            .to_vec();
        assert!(t.syntactic_verify().unwrap_err().detail.contains("confers no operation"));

        t.payload =
            br#"{"grantee":"NG42sa8bZqRUjcaoq9igh5toYw8mXmw7H","operations":1048576,"expiry":0}"#
                .to_vec();
        let e = t.syntactic_verify().unwrap_err();
        assert!(e.detail.contains("unknown permit operation bits"));
        // And it is still a SYNTACTIC refusal: the word "unknown" is in the
        // sentence, not in the sentinel.
        assert_eq!(e.class(), crate::error::SYNTACTIC);
    }

    #[test]
    fn a_request_names_a_permit_and_a_fulfilment_names_a_result() {
        let mut t = Transaction {
            tx_type: TX_REQUEST_DECRYPT,
            scheme: b"ckks-n14".to_vec(),
            subject: [1; 32],
            gas_limit: 5_000_000,
            nonce: 1,
            payload: br#"{"permitId":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"callback":[],"selector":[],"expiry":0}"#.to_vec(),
            ..Transaction::default()
        };
        assert!(t.syntactic_verify().unwrap_err().detail.contains("names no permit"));

        t = Transaction {
            tx_type: TX_FULFILL_DECRYPT,
            subject: [1; 32],
            gas_limit: 5_000_000,
            nonce: 1,
            payload: br#"{"result":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}"#.to_vec(),
            ..Transaction::default()
        };
        assert!(t.syntactic_verify().unwrap_err().detail.contains("no result handle"));
    }

    #[test]
    fn a_committee_has_to_be_installable() {
        assert!(validate_committee(&[], 1, &[1])
            .unwrap_err()
            .detail
            .contains("empty committee"));

        let one = vec![member(1, 1)];
        assert_eq!(
            validate_committee(&one, 0, &[1]).unwrap_err().code,
            Code::InvalidThreshold
        );
        assert_eq!(
            validate_committee(&one, 2, &[1]).unwrap_err().code,
            Code::InvalidThreshold
        );
        assert!(validate_committee(&one, 1, &[])
            .unwrap_err()
            .detail
            .contains("no network public key"));
        validate_committee(&one, 1, &[1]).expect("installable");

        let out_of_order = vec![member(2, 1), member(1, 2)];
        assert!(validate_committee(&out_of_order, 1, &[1])
            .unwrap_err()
            .detail
            .contains("canonical node-ID order"));

        let dup_seat = vec![member(1, 1), member(1, 2)];
        assert!(validate_committee(&dup_seat, 1, &[1])
            .unwrap_err()
            .detail
            .contains("duplicate member"));

        // Two seats, one key. Every other check passes and the committee could
        // never reach its own threshold.
        let one_voice = vec![member(1, 5), member(2, 5)];
        assert!(validate_committee(&one_voice, 2, &[1])
            .unwrap_err()
            .detail
            .contains("shares a voting identity"));

        let too_many: Vec<Member> =
            (0..=MAX_COMMITTEE as u8).map(|i| member(i, i.wrapping_add(1))).collect();
        assert!(validate_committee(&too_many, 1, &[1])
            .unwrap_err()
            .detail
            .contains("exceeds 32"));
    }

    #[test]
    fn a_member_whose_key_cannot_sign_cannot_be_seated() {
        // The committee that cannot speak can never be replaced either.
        let mute = vec![Member { public_key: vec![1u8; 8], ..member(1, 1) }];
        assert!(validate_committee(&mute, 1, &[1])
            .unwrap_err()
            .detail
            .contains("invalid key size"));
    }

    #[test]
    fn an_advance_is_read_out_of_the_json_go_writes() {
        let pk = key(4);
        use base64::Engine;
        let b64 = base64::engine::general_purpose::STANDARD.encode(&pk);
        let payload = format!(
            r#"{{"epoch":1,"committee":[{{"node_id":"{}","public_key":"{}","weight":1,"index":0}}],"threshold":1,"publicKey":"{}"}}"#,
            crate::id::node_id_to_string(&[7u8; 20]),
            b64,
            base64::engine::general_purpose::STANDARD.encode([0x77u8; 32])
        );
        let p = Advance::decode(payload.as_bytes()).expect("advance");
        assert_eq!(p.epoch, 1);
        assert_eq!(p.threshold, 1);
        assert_eq!(p.public_key, vec![0x77u8; 32]);
        assert_eq!(p.committee, vec![Member { node_id: [7; 20], public_key: pk, weight: 1, index: 0 }]);
    }

    #[test]
    fn an_effect_names_the_entry_and_not_the_transaction() {
        // A register is named by its subject alone: two payers cannot both
        // register one handle in one block.
        let a = Transaction { tx_type: TX_REGISTER_CIPHERTEXT, subject: [1; 32], payer: [1; 20], nonce: 1, ..Transaction::default() };
        let b = Transaction { payer: [2; 20], nonce: 7, ..a.clone() };
        assert_eq!(a.effect(), b.effect());

        // A grant is named by subject, payer AND nonce: one owner may grant
        // twice over one handle, to different grantees, in one block.
        let g1 = Transaction { tx_type: TX_GRANT_PERMIT, subject: [1; 32], payer: [1; 20], nonce: 1, ..Transaction::default() };
        let g2 = Transaction { nonce: 2, ..g1.clone() };
        assert_ne!(g1.effect(), g2.effect());

        // A vote on the next epoch is named by the member alone, so one member
        // cannot vote twice for two different committees in one block.
        let v1 = Transaction { tx_type: TX_ADVANCE_EPOCH, subject: [1; 32], payer: [1; 20], nonce: 1, ..Transaction::default() };
        let v2 = Transaction { subject: [2; 32], nonce: 2, ..v1.clone() };
        assert_eq!(v1.effect(), v2.effect());

        // A fulfilment is one write per member per request.
        let f1 = Transaction { tx_type: TX_FULFILL_DECRYPT, subject: [1; 32], payer: [1; 20], nonce: 1, ..Transaction::default() };
        let f2 = Transaction { nonce: 9, ..f1.clone() };
        let f3 = Transaction { payer: [2; 20], ..f1.clone() };
        assert_eq!(f1.effect(), f2.effect());
        assert_ne!(f1.effect(), f3.effect());
    }

    #[test]
    fn an_unsigned_transaction_is_refused_before_any_key_is_parsed() {
        let t = register([0x21; 32], "ckks-n14");
        assert_eq!(t.authenticate(&[50u8; 32]).unwrap_err().code, Code::UnsignedTx);
    }

    #[test]
    fn a_payer_the_key_does_not_derive_is_refused() {
        let mut t = register([0x21; 32], "ckks-n14");
        t.auth = key(3);
        t.sig = vec![0u8; 3309];
        t.payer = [0x66; 20];
        assert_eq!(t.authenticate(&[50u8; 32]).unwrap_err().code, Code::PayerMismatch);
        // With the address the key does derive, it gets as far as the signature.
        t.payer = record::address_of(&t.auth);
        assert_eq!(t.authenticate(&[50u8; 32]).unwrap_err().code, Code::BadSignature);
    }

    #[test]
    fn the_kind_names_are_the_wires_words() {
        for (t, want) in [
            (TX_REGISTER_CIPHERTEXT, "RegisterCiphertext"),
            (TX_GRANT_PERMIT, "GrantPermit"),
            (TX_REVOKE_PERMIT, "RevokePermit"),
            (TX_REQUEST_DECRYPT, "RequestDecrypt"),
            (TX_FULFILL_DECRYPT, "FulfillDecrypt"),
            (TX_ADVANCE_EPOCH, "AdvanceEpoch"),
        ] {
            assert_eq!(Transaction { tx_type: t, ..Transaction::default() }.kind(), want);
        }
    }

    #[test]
    fn an_id_is_the_hash_of_the_bytes_that_travel() {
        let t = register([0x21; 32], "ckks-n14");
        assert_eq!(hex(&t.id()), hex(&Sha256::digest(t.bytes())));
    }
}
