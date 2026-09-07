// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The six operations, and the rules each is held to.
//!
//! Together they are the whole life of a confidential value on F: it is
//! registered, capabilities over it are granted and revoked, its decryption is
//! requested, and the committee answers; the committee that answers is itself
//! rotated by the sixth. Each is a MUTATING operation that may only take effect
//! through a fee-settled consensus block, never through a synchronous call.
//!
//! Three gates, in this order, and each is here for a different reason.
//!
//! [`Transaction::syntactic_verify`] is structure alone — no state, no signature.
//! It runs on an UNAUTHENTICATED transaction, so everything it does is bounded by
//! that transaction's own declared size: the payload and the scheme are bounded
//! BEFORE either is decoded, the auth and signature widths are pinned so they
//! cannot become a third byte channel, and the one expensive decode — parsing a
//! committee's public keys — is bounded by [`MAX_COMMITTEE`] before any key is
//! looked at.
//!
//! [`Transaction::authenticate`] is the payer. It is PUBLIC work: parse the
//! attached public key, require it to hash to the payer's address, and verify the
//! signature over the preimage for THIS chain. No secret is touched.
//!
//! [`Transaction::check_auth`] is authorization, and it is read-only. It decides
//! whether an operation may take effect against the CURRENT committed state. It
//! is called at three layers so an unauthorized transaction is refused at the
//! earliest gate and never charged: admission, consensus, and — as defence in
//! depth — application. The VERDICT is only ever reached once, in
//! [`Transaction::apply`], because the state it reads is state an earlier
//! transaction in the same block may have changed, and a block every validator
//! certifies and no validator can apply halts the chain.

use sha2::{Digest, Sha256};

use crate::error::{Error, Result};
use crate::fee::Account;
use crate::fhe;
use crate::gas;
use crate::gojson;
use crate::ids::{self, Id};
use crate::mldsa;
use crate::state::{
    self, address_of, committee_digest, committee_order, derive_handle, derive_permit_id,
    derive_request_id, CiphertextRecord, DecryptRecord, EpochRecord, PermitRecord, STATUS_ACTIVE,
    STATUS_REVOKED,
};
use crate::vm::State;

/// Record a ciphertext's PUBLIC handle and metadata.
pub const TX_REGISTER_CIPHERTEXT: u8 = 1;
/// An owner grants a capability over a handle.
pub const TX_GRANT_PERMIT: u8 = 2;
/// An owner withdraws a capability it granted.
pub const TX_REVOKE_PERMIT: u8 = 3;
/// A permitted grantee asks the committee to decrypt.
pub const TX_REQUEST_DECRYPT: u8 = 4;
/// A committee member attests the PUBLIC result.
pub const TX_FULFILL_DECRYPT: u8 = 5;
/// The committee installs its successor.
pub const TX_ADVANCE_EPOCH: u8 = 6;

/// An operation's encoding. Set by the largest legitimate payload — a
/// [`MAX_COMMITTEE`]-member epoch proposal, whose members each carry an ML-DSA-65
/// public key — with room to spare.
pub const MAX_PAYLOAD: usize = 128 * 1024;

/// A scheme name. Scheme names are short labels ("ckks-n14"); anything longer is
/// a channel, not a name.
pub const MAX_SCHEME: usize = 32;

/// A threshold committee. It bounds the epoch payload, and with it the public-key
/// parsing an UNAUTHENTICATED transaction can demand before its own signature is
/// checked.
pub const MAX_COMMITTEE: usize = 32;

/// The off-chain body a registration may describe. The body is not stored here,
/// but a size nobody could ever serve describes nothing.
pub const MAX_CIPHERTEXT_SIZE: u32 = 1 << 30;

/// How long a decryption request stays answerable when the requester names no
/// expiry. A request no committee can still answer is dead weight in state, and
/// an unbounded one would be answerable by a future committee that never saw the
/// permit that authorized it.
pub const DEFAULT_REQUEST_WINDOW: i64 = 3600;

/// Every capability bit the FHE runtime defines. A grant that sets a bit outside
/// it is refused rather than silently conferring nothing.
pub const PERMIT_OP_MASK: u32 =
    fhe::PERMIT_OP_DECRYPT | fhe::PERMIT_OP_REENCRYPT | fhe::PERMIT_OP_COMPUTE | fhe::PERMIT_OP_TRANSFER;

/// An F-Chain consensus transaction.
///
/// The header is deterministic binary; the payload is an opaque, PUBLIC,
/// op-specific JSON blob; `auth` is the payer's ML-DSA-65 public key and `sig`
/// the payer's signature over the signing bytes. Nothing here is or can become a
/// ciphertext, a plaintext, or a key share.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Transaction {
    pub tx_type: u8,
    /// FHE scheme and ring dimension, which drives the per-scheme gas. Empty for
    /// the scheme-independent operations.
    ///
    /// BYTES, not a Rust string. Go's `string` is a byte string: a scheme that is
    /// not valid UTF-8 is bounded, priced and re-serialized there unchanged, and a
    /// port that insisted on UTF-8 would refuse a block Go accepts — which is a
    /// fork, not a stricter parser.
    pub scheme: Vec<u8>,
    /// The fee payer and the authorization subject — a public address.
    pub payer: Account,
    /// The object this operation acts on or creates. It is ALWAYS checked against
    /// what the payload derives, so the signature covers the object and not
    /// merely the arguments that happen to produce it:
    ///
    /// - register: the handle the payload's digest and scheme derive
    /// - grant: the handle being granted over
    /// - revoke: the permit being withdrawn
    /// - request: the handle whose decryption is asked for
    /// - fulfil: the request being answered
    /// - advance: the digest of the successor committee being approved
    pub subject: [u8; 32],
    /// The payer's declared gas ceiling for this transaction.
    pub gas_limit: u64,
    /// The payer's replay and ordering nonce.
    pub nonce: u64,
    /// The op-specific PUBLIC encoding.
    pub payload: Vec<u8>,
    /// The payer's ML-DSA-65 PUBLIC key.
    pub auth: Vec<u8>,
    /// The payer's signature over [`Transaction::signing_bytes`].
    pub sig: Vec<u8>,
}

// ---------------------------------------------------------------- payloads --

/// An encrypted value's PUBLIC coordinates. `digest` is the hash of the
/// ciphertext BODY, which lives in off-chain storage — F stores the hash so
/// anyone can check a fetched body against the chain, and never the body itself.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct RegisterPayload {
    pub digest: [u8; 32],
    /// The FHE plaintext type tag (bool, uint8, …).
    pub kind: u8,
    /// The remaining multiplicative level.
    pub level: i64,
    /// The ciphertext body's size in bytes.
    pub size: u32,
}

/// Grants a capability over one handle to one grantee until it expires.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct GrantPayload {
    pub grantee: Account,
    /// A bitmask of the runtime's permit operations.
    pub operations: u32,
    /// Unix seconds; 0 is no expiry.
    pub expiry: i64,
}

/// Withdraws a permit.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct RevokePayload {
    pub reason: String,
}

/// Asks the committee to threshold-decrypt a handle under the authority of a
/// permit the requester holds. The callback and selector name where the answer
/// should be delivered on the source chain.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct RequestPayload {
    pub permit_id: [u8; 32],
    pub callback: [u8; 20],
    pub selector: [u8; 4],
    /// Unix seconds; 0 is the chain's default window.
    pub expiry: i64,
}

/// One committee member's attestation of the PUBLIC handle a threshold decryption
/// produced. The plaintext itself is delivered off-chain to the callback; F
/// records only which handle the committee agreed on.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct FulfillPayload {
    pub result: [u8; 32],
}

/// Proposes the next epoch's committee and the network public key it jointly
/// generated. Every approving member sends the identical proposal; the epoch
/// installs when a threshold of the CURRENT committee have.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct AdvancePayload {
    pub epoch: u64,
    pub committee: fhe::List<fhe::CommitteeMember>,
    pub threshold: i64,
    pub public_key: Vec<u8>,
}

impl RegisterPayload {
    pub fn to_json(&self) -> Vec<u8> {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "digest");
        gojson::write_byte_array(&mut out, &self.digest);
        gojson::write_key(&mut out, &mut first, "type");
        out.push_str(&self.kind.to_string());
        gojson::write_key(&mut out, &mut first, "level");
        out.push_str(&self.level.to_string());
        gojson::write_key(&mut out, &mut first, "size");
        out.push_str(&self.size.to_string());
        out.push('}');
        out.into_bytes()
    }

    pub fn decode(payload: &[u8]) -> Result<Self> {
        const OP: &str = "register";
        let v = gojson::parse(payload, OP)?;
        let o = gojson::object(&v, OP, &["digest", "type", "level", "size"])?;
        Ok(RegisterPayload {
            digest: gojson::byte_array::<32>(o, "digest", OP)?,
            kind: gojson::u8_field(o, "type", OP)?,
            level: gojson::i64_field(o, "level", OP)?,
            size: gojson::u32_field(o, "size", OP)?,
        })
    }
}

impl GrantPayload {
    pub fn to_json(&self) -> Vec<u8> {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "grantee");
        gojson::write_string(&mut out, &ids::short_id_string(&self.grantee));
        gojson::write_key(&mut out, &mut first, "operations");
        out.push_str(&self.operations.to_string());
        gojson::write_key(&mut out, &mut first, "expiry");
        out.push_str(&self.expiry.to_string());
        out.push('}');
        out.into_bytes()
    }

    pub fn decode(payload: &[u8]) -> Result<Self> {
        const OP: &str = "grant";
        let v = gojson::parse(payload, OP)?;
        let o = gojson::object(&v, OP, &["grantee", "operations", "expiry"])?;
        Ok(GrantPayload {
            grantee: gojson::account_field(o, "grantee", OP)?,
            operations: gojson::u32_field(o, "operations", OP)?,
            expiry: gojson::i64_field(o, "expiry", OP)?,
        })
    }
}

impl RevokePayload {
    pub fn to_json(&self) -> Vec<u8> {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "reason");
        gojson::write_string(&mut out, &self.reason);
        out.push('}');
        out.into_bytes()
    }

    pub fn decode(payload: &[u8]) -> Result<Self> {
        const OP: &str = "revoke";
        let v = gojson::parse(payload, OP)?;
        let o = gojson::object(&v, OP, &["reason"])?;
        Ok(RevokePayload { reason: gojson::string_field(o, "reason", OP)? })
    }
}

impl RequestPayload {
    pub fn to_json(&self) -> Vec<u8> {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "permitId");
        gojson::write_byte_array(&mut out, &self.permit_id);
        gojson::write_key(&mut out, &mut first, "callback");
        gojson::write_byte_array(&mut out, &self.callback);
        gojson::write_key(&mut out, &mut first, "selector");
        gojson::write_byte_array(&mut out, &self.selector);
        gojson::write_key(&mut out, &mut first, "expiry");
        out.push_str(&self.expiry.to_string());
        out.push('}');
        out.into_bytes()
    }

    pub fn decode(payload: &[u8]) -> Result<Self> {
        const OP: &str = "request";
        let v = gojson::parse(payload, OP)?;
        let o = gojson::object(&v, OP, &["permitId", "callback", "selector", "expiry"])?;
        Ok(RequestPayload {
            permit_id: gojson::byte_array::<32>(o, "permitId", OP)?,
            callback: gojson::byte_array::<20>(o, "callback", OP)?,
            selector: gojson::byte_array::<4>(o, "selector", OP)?,
            expiry: gojson::i64_field(o, "expiry", OP)?,
        })
    }
}

impl FulfillPayload {
    pub fn to_json(&self) -> Vec<u8> {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "result");
        gojson::write_byte_array(&mut out, &self.result);
        out.push('}');
        out.into_bytes()
    }

    pub fn decode(payload: &[u8]) -> Result<Self> {
        const OP: &str = "fulfill";
        let v = gojson::parse(payload, OP)?;
        let o = gojson::object(&v, OP, &["result"])?;
        Ok(FulfillPayload { result: gojson::byte_array::<32>(o, "result", OP)? })
    }
}

impl AdvancePayload {
    pub fn to_json(&self) -> Vec<u8> {
        let mut out = String::new();
        let mut first = true;
        out.push('{');
        gojson::write_key(&mut out, &mut first, "epoch");
        out.push_str(&self.epoch.to_string());
        gojson::write_key(&mut out, &mut first, "committee");
        state::write_committee_json(&mut out, &self.committee);
        gojson::write_key(&mut out, &mut first, "threshold");
        out.push_str(&self.threshold.to_string());
        gojson::write_key(&mut out, &mut first, "publicKey");
        gojson::write_base64_or_null(
            &mut out,
            if self.public_key.is_empty() { None } else { Some(&self.public_key) },
        );
        out.push('}');
        out.into_bytes()
    }

    pub fn decode(payload: &[u8]) -> Result<Self> {
        const OP: &str = "advance";
        let v = gojson::parse(payload, OP)?;
        let o = gojson::object(&v, OP, &["epoch", "committee", "threshold", "publicKey"])?;
        Ok(AdvancePayload {
            epoch: gojson::u64_field(o, "epoch", OP)?,
            committee: state::read_committee(o, "committee", OP)?,
            threshold: gojson::i64_field(o, "threshold", OP)?,
            public_key: gojson::bytes_field(o, "publicKey", OP)?,
        })
    }

    pub fn members(&self) -> &[fhe::CommitteeMember] {
        self.committee.as_deref().unwrap_or(&[])
    }
}

// ------------------------------------------------------------------- rules --

/// Checks a committee is installable: non-empty, bounded, canonically ordered by
/// node id, free of duplicate seats and of duplicate VOTERS, with a real
/// threshold and with every member carrying a public key of the right width.
///
/// The width check is what stops F being wedged by an epoch whose members can
/// never sign an attestation — the committee that cannot speak can never be
/// replaced either. Genesis and an epoch advance both go through here, so the two
/// can never disagree.
///
/// A seat is only a vote if a DISTINCT account can cast it. Membership is tested
/// by the address of a member's public key — the same derivation that
/// authenticates a payer — so that is what must be unique. Deduplicating node ids
/// instead let n seats share one key: the committee passed every check, reported
/// itself fully seated, and could never reach its own threshold, for a decryption
/// or for rotating itself out.
pub fn validate_committee(
    c: &[fhe::CommitteeMember],
    threshold: i64,
    public_key: &[u8],
) -> Result<()> {
    if c.is_empty() {
        return Err(Error::InvalidCommittee("empty committee".into()));
    }
    if c.len() > MAX_COMMITTEE {
        return Err(Error::InvalidCommittee(format!(
            "{} members exceeds {MAX_COMMITTEE}",
            c.len()
        )));
    }
    if threshold <= 0 || threshold > c.len() as i64 {
        return Err(Error::InvalidThreshold(format!("threshold {threshold} of {}", c.len())));
    }
    if public_key.is_empty() {
        return Err(Error::InvalidCommittee("no network public key".into()));
    }
    if !committee_order(c) {
        return Err(Error::InvalidCommittee("members not in canonical node-ID order".into()));
    }
    let mut voters: Vec<Account> = Vec::with_capacity(c.len());
    for (i, m) in c.iter().enumerate() {
        if i > 0 && c[i - 1].node_id == m.node_id {
            return Err(Error::InvalidCommittee(format!(
                "duplicate member {}",
                ids::node_id_string(&m.node_id)
            )));
        }
        if let Err(e) = mldsa::parse_public_key(&m.public_key) {
            return Err(Error::InvalidCommittee(format!(
                "member {} key: {e}",
                ids::node_id_string(&m.node_id)
            )));
        }
        let acct = address_of(&m.public_key);
        if voters.contains(&acct) {
            return Err(Error::InvalidCommittee(format!(
                "member {} shares a voting identity with another seat",
                ids::node_id_string(&m.node_id)
            )));
        }
        voters.push(acct);
    }
    Ok(())
}

impl Transaction {
    /// The transaction's content hash, over its full canonical wire bytes.
    pub fn id(&self) -> Id {
        Sha256::digest(self.bytes()).into()
    }

    /// Checks the transaction is well formed and priceable, without any state.
    ///
    /// It refuses unknown types, unpriceable schemes, undecodable or out-of-range
    /// payloads, and any subject that disagrees with what the payload derives —
    /// all fail closed.
    pub fn syntactic_verify(&self) -> Result<()> {
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT | TX_GRANT_PERMIT | TX_REVOKE_PERMIT | TX_REQUEST_DECRYPT
            | TX_FULFILL_DECRYPT | TX_ADVANCE_EPOCH => {}
            _ => return Err(Error::InvalidTxType),
        }
        // Bounds FIRST, before anything is decoded or parsed, so the work an
        // unauthenticated transaction can demand is bounded by its own size.
        if self.payload.len() > MAX_PAYLOAD {
            return Err(Error::InvalidPayload(format!(
                "payload {} exceeds {MAX_PAYLOAD} bytes",
                self.payload.len()
            )));
        }
        if self.scheme.len() > MAX_SCHEME {
            return Err(Error::InvalidPayload(format!(
                "scheme {} exceeds {MAX_SCHEME} bytes",
                self.scheme.len()
            )));
        }
        // Auth and sig are fixed-width by algorithm. Pinning them here keeps them
        // from becoming a third byte channel the base cost would carry for free.
        if !self.auth.is_empty() && self.auth.len() != mldsa::PUBLIC_KEY_SIZE {
            return Err(Error::InvalidPayload(format!(
                "auth is {} bytes, not {}",
                self.auth.len(),
                mldsa::PUBLIC_KEY_SIZE
            )));
        }
        if !self.sig.is_empty() && self.sig.len() != mldsa::SIGNATURE_SIZE {
            return Err(Error::InvalidPayload(format!(
                "signature is {} bytes, not {}",
                self.sig.len(),
                mldsa::SIGNATURE_SIZE
            )));
        }
        // Pricing also validates scheme membership for scheme-bearing operations.
        gas::gas_for(self)?;
        if self.nonce == 0 {
            return Err(Error::BadNonce);
        }

        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => {
                let p = RegisterPayload::decode(&self.payload)?;
                if p.digest == [0u8; 32] {
                    return Err(Error::InvalidPayload("register: empty ciphertext digest".into()));
                }
                if p.size == 0 || p.size > MAX_CIPHERTEXT_SIZE {
                    return Err(Error::InvalidPayload(format!(
                        "register: ciphertext size {}",
                        p.size
                    )));
                }
                if p.level < 0 {
                    return Err(Error::InvalidPayload("register: negative level".into()));
                }
                if self.subject != derive_handle(&p.digest, &self.scheme) {
                    return Err(Error::HandleMismatch(
                        "handle does not match digest+scheme".into(),
                    ));
                }
            }

            TX_GRANT_PERMIT => {
                let p = GrantPayload::decode(&self.payload)?;
                if p.operations == 0 {
                    return Err(Error::InvalidPayload("grant confers no operation".into()));
                }
                if p.operations & !PERMIT_OP_MASK != 0 {
                    return Err(Error::InvalidPayload("unknown permit operation bits".into()));
                }
                if p.expiry < 0 {
                    return Err(Error::InvalidPayload("negative expiry".into()));
                }
            }

            TX_REVOKE_PERMIT => {
                RevokePayload::decode(&self.payload)?;
            }

            TX_REQUEST_DECRYPT => {
                let p = RequestPayload::decode(&self.payload)?;
                if p.permit_id == [0u8; 32] {
                    return Err(Error::InvalidPayload("request names no permit".into()));
                }
                if p.expiry < 0 {
                    return Err(Error::InvalidPayload("negative expiry".into()));
                }
            }

            TX_FULFILL_DECRYPT => {
                let p = FulfillPayload::decode(&self.payload)?;
                if p.result == [0u8; 32] {
                    return Err(Error::InvalidPayload("fulfill carries no result handle".into()));
                }
            }

            TX_ADVANCE_EPOCH => {
                let p = AdvancePayload::decode(&self.payload)?;
                validate_committee(p.members(), p.threshold, &p.public_key)?;
                if self.subject
                    != committee_digest(p.epoch, p.threshold, &p.public_key, p.members())
                {
                    return Err(Error::HandleMismatch(
                        "subject does not match the proposed committee".into(),
                    ));
                }
            }

            _ => unreachable!("the type was checked above"),
        }
        Ok(())
    }

    /// Verifies the payer authorized this transaction ON THIS CHAIN.
    ///
    /// PUBLIC only: parse the payer's public key, require it to hash to the
    /// payer's address, and verify the signature over the preimage for `chain`.
    /// The chain is the verifying node's own, never one the transaction carries,
    /// so a transaction signed for another F-Chain fails here rather than spending
    /// a balance the payer holds on every chain at the same address.
    pub fn authenticate(&self, chain: &Id) -> Result<()> {
        if self.auth.is_empty() || self.sig.is_empty() {
            return Err(Error::UnsignedTx);
        }
        if address_of(&self.auth) != self.payer {
            return Err(Error::PayerMismatch);
        }
        mldsa::parse_public_key(&self.auth)
            .map_err(|e| Error::InvalidPayload(format!("payer public key: {e}")))?;
        if !mldsa::verify(&self.auth, &self.signing_bytes(chain), &self.sig) {
            return Err(Error::BadSignature);
        }
        Ok(())
    }

    /// Names, in 32 bytes, the state entry this transaction writes — and, where an
    /// entry legitimately takes a write from each of several actors, which actor
    /// writes it. Two transactions sharing an effect cannot both take place: the
    /// second would find the ciphertext already registered, the permit already
    /// withdrawn, or the member already counted, and abort the block that carried
    /// them both.
    ///
    /// Nonces do not catch that on their own: a payer's nonces n and n+1 are both
    /// valid, so one payer can build two transactions that individually pass every
    /// check and together spend a block on work only one of them can do. Naming
    /// the effect lets admission refuse the second before it is ever queued and
    /// lets consensus refuse a peer's block that contains both. One function, both
    /// layers, no drift.
    pub fn effect(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update([self.tx_type]);
        match self.tx_type {
            TX_ADVANCE_EPOCH => {
                // The DECISION, not the proposal. Exactly one epoch is ever open —
                // check_auth refuses any target but the sitting epoch's successor —
                // so a member's vote is named by the member alone. Naming it by the
                // subject instead made two votes for two different committees two
                // different effects: both passed admission, both passed Verify
                // against committed state, and Accept applied the first and refused
                // the second. A block every validator certifies and no validator can
                // apply halts the chain at that height, which is the whole thing
                // this function exists to stop.
                h.update(self.payer);
            }
            TX_REGISTER_CIPHERTEXT | TX_REVOKE_PERMIT => {
                // The entry is named by the subject alone: one registration per
                // handle, one withdrawal per permit, whoever asks for it.
                h.update(self.subject);
            }
            TX_FULFILL_DECRYPT => {
                // One write per member per request. The voted value lives in the
                // payload, so the effect is the member's vote on THIS request
                // rather than the result it names.
                h.update(self.subject);
                h.update(self.payer);
            }
            _ => {
                // A grant and a request CREATE an entry whose id already carries the
                // payer and its nonce, so no two of them collide — the same owner
                // may grant twice over one handle, to different grantees, in one
                // block. Qualifying by the same two fields keeps that true here.
                h.update(self.subject);
                h.update(self.payer);
                h.update(self.nonce.to_be_bytes());
            }
        }
        h.finalize().into()
    }

    /// The single, read-only authorization predicate: whether this transaction may
    /// take effect against the CURRENT committed state at time `now`. It mutates
    /// nothing.
    pub fn check_auth(&self, st: &State, now: i64) -> Result<()> {
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => {
                if st.ciphertexts.contains_key(&self.subject) {
                    return Err(Error::CiphertextExists);
                }
                Ok(())
            }

            TX_GRANT_PERMIT => {
                let ct = st.ciphertexts.get(&self.subject).ok_or(Error::CiphertextNotFound)?;
                // Only the owner may confer a capability over its ciphertext — and,
                // because only the owner grants, only the owner revokes.
                if ct.meta.owner != self.payer {
                    return Err(Error::Unauthorized);
                }
                Ok(())
            }

            TX_REVOKE_PERMIT => {
                let pm = st.permits.get(&self.subject).ok_or(Error::PermitNotFound)?;
                if pm.status != STATUS_ACTIVE {
                    return Err(Error::PermitRevoked);
                }
                if pm.permit.grantor != self.payer {
                    return Err(Error::Unauthorized);
                }
                Ok(())
            }

            TX_REQUEST_DECRYPT => {
                let p = RequestPayload::decode(&self.payload)?;
                if !st.ciphertexts.contains_key(&self.subject) {
                    return Err(Error::CiphertextNotFound);
                }
                let pm = st.permits.get(&p.permit_id).ok_or(Error::PermitNotFound)?;
                if pm.status != STATUS_ACTIVE {
                    return Err(Error::PermitRevoked);
                }
                if pm.permit.handle != self.subject {
                    return Err(Error::PermitInvalid("permit is for another handle".into()));
                }
                if pm.permit.grantee != self.payer {
                    return Err(Error::Unauthorized);
                }
                if pm.permit.expiry != 0 && now > pm.permit.expiry {
                    return Err(Error::PermitExpired);
                }
                if pm.permit.operations & fhe::PERMIT_OP_DECRYPT == 0 {
                    return Err(Error::PermitInvalid("permit does not confer decrypt".into()));
                }
                // The request's id carries the requester's nonce, and a nonce is
                // used once, so a request cannot collide with an existing one. That
                // is the whole uniqueness argument — there is no second check to
                // keep in step with it.
                Ok(())
            }

            TX_FULFILL_DECRYPT => {
                let req = st.decrypts.get(&self.subject).ok_or(Error::RequestNotFound)?;
                if req.request.status != fhe::RequestStatus::Pending {
                    return Err(Error::RequestClosed);
                }
                if req.request.expiry != 0 && now > req.request.expiry {
                    return Err(Error::RequestExpired);
                }
                // The permit that authorized the ask must still authorize it.
                // Revocation is a withdrawal of consent and reaches a request
                // already in flight — otherwise an owner who revoked would watch the
                // committee answer anyway and deliver the plaintext to the callback.
                //
                // Expiry is deliberately NOT re-checked. It bounds when the grantee
                // may ASK; the grantee asked in time, and the committee answering
                // afterwards is not the grantee acting. Re-checking it would make
                // any permit shorter than a decryption round useless.
                let pm = st.permits.get(&req.permit_id).ok_or(Error::PermitNotFound)?;
                if pm.status != STATUS_ACTIVE {
                    return Err(Error::PermitRevoked);
                }
                // Only the committee of the epoch the request was made in may answer
                // it: a later committee holds different shares and never saw the
                // permit.
                let ep = st.epochs.get(&req.request.epoch).ok_or(Error::EpochNotFound)?;
                if !ep.member_of(&self.payer) {
                    return Err(Error::NotCommittee);
                }
                Ok(())
            }

            TX_ADVANCE_EPOCH => {
                let p = AdvancePayload::decode(&self.payload)?;
                let cur = st.current_epoch();
                if p.epoch != cur.info.epoch + 1 {
                    return Err(Error::EpochMismatch(format!(
                        "proposed {}, next is {}",
                        p.epoch,
                        cur.info.epoch + 1
                    )));
                }
                // Only the sitting committee decides its successor.
                if !cur.member_of(&self.payer) {
                    return Err(Error::NotCommittee);
                }
                Ok(())
            }

            _ => Err(Error::InvalidTxType),
        }
    }

    /// Where authorization is DECIDED and where state changes. It runs inside a
    /// block's acceptance, writing through the version layer so an effect commits
    /// atomically with the fee burn. `now` is the accepting block's unix time —
    /// every timestamp F stores comes from here, never from a validator's clock.
    ///
    /// It reports whether the transaction took effect. One that fails
    /// authorization REVERTS: applied is false, no error, state untouched, and the
    /// caller still burns the fee and consumes the nonce. This is the only place
    /// the verdict is reached, so every validator reaches it from the same
    /// committed state in the same order.
    ///
    /// An error means the WRITE failed, which no validator can proceed past; the
    /// block rolls back whole.
    pub fn apply(&self, st: &mut State, now: i64) -> Result<bool> {
        if self.check_auth(st, now).is_err() {
            return Ok(false);
        }
        match self.tx_type {
            TX_REGISTER_CIPHERTEXT => self.apply_register(st, now)?,
            TX_GRANT_PERMIT => self.apply_grant(st, now)?,
            TX_REVOKE_PERMIT => self.apply_revoke(st)?,
            TX_REQUEST_DECRYPT => self.apply_request(st, now)?,
            TX_FULFILL_DECRYPT => self.apply_fulfill(st, now)?,
            TX_ADVANCE_EPOCH => self.apply_advance(st, now)?,
            // Unreachable: check_auth refuses every type the arms above do not
            // name, so nothing gets here. It stays because a missing arm would
            // otherwise report the transaction APPLIED, which is the one answer
            // that must never be given by accident.
            _ => return Ok(false),
        }
        Ok(true)
    }

    pub fn apply_register(&self, st: &mut State, now: i64) -> Result<()> {
        let p = RegisterPayload::decode(&self.payload)?;
        let rec = CiphertextRecord {
            meta: fhe::CiphertextMeta {
                handle: self.subject,
                owner: self.payer,
                kind: p.kind,
                level: p.level,
                epoch: st.current_epoch().info.epoch,
                registered_at: now,
                size: p.size,
                chain_id: st.chain_id,
            },
            // Only a PRICED scheme reaches here — the structural gate refuses a
            // registration naming any other — and every priced scheme is ASCII, so
            // the record's scheme is the same characters the transaction carried.
            scheme: String::from_utf8_lossy(&self.scheme).into_owned(),
            digest: p.digest,
        };
        st.put_ciphertext(rec)
    }

    pub fn apply_grant(&self, st: &mut State, now: i64) -> Result<()> {
        let p = GrantPayload::decode(&self.payload)?;
        let rec = PermitRecord {
            permit: fhe::Permit {
                permit_id: derive_permit_id(
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
                attestation: Vec::new(),
                chain_id: st.chain_id,
            },
            status: STATUS_ACTIVE.into(),
        };
        st.put_permit(rec)
    }

    pub fn apply_revoke(&self, st: &mut State) -> Result<()> {
        let mut pm = st.permits.get(&self.subject).cloned().ok_or(Error::PermitNotFound)?;
        pm.status = STATUS_REVOKED.into();
        st.put_permit(pm)
    }

    pub fn apply_request(&self, st: &mut State, now: i64) -> Result<()> {
        let p = RequestPayload::decode(&self.payload)?;
        let expiry = if p.expiry == 0 { now + DEFAULT_REQUEST_WINDOW } else { p.expiry };
        let rec = DecryptRecord {
            request: fhe::DecryptRequest {
                request_id: derive_request_id(&self.subject, &self.payer, self.nonce),
                ciphertext_handle: self.subject,
                requester: self.payer,
                callback: p.callback,
                callback_selector: p.selector,
                source_chain: st.chain_id,
                epoch: st.current_epoch().info.epoch,
                nonce: self.nonce,
                expiry,
                status: fhe::RequestStatus::Pending,
                created_at: now,
                completed_at: 0,
                result_handle: [0u8; 32],
                error: String::new(),
            },
            permit_id: p.permit_id,
            attestations: None,
        };
        st.put_decrypt(rec)
    }

    pub fn apply_fulfill(&self, st: &mut State, now: i64) -> Result<()> {
        let p = FulfillPayload::decode(&self.payload)?;
        let mut req = st.decrypts.get(&self.subject).cloned().ok_or(Error::RequestNotFound)?;
        let threshold = st
            .epochs
            .get(&req.request.epoch)
            .ok_or(Error::EpochNotFound)?
            .info
            .threshold;
        state::vote(&mut req.attestations, self.payer, p.result);
        // The request completes the moment a threshold of DISTINCT members have
        // named the same handle. A member that names a different one is counted
        // against that value alone, so it delays nothing and pays for the
        // privilege.
        if state::tally(req.attestations(), &p.result) >= threshold {
            req.request.status = fhe::RequestStatus::Completed;
            req.request.result_handle = p.result;
            req.request.completed_at = now;
        }
        st.put_decrypt(req)
    }

    pub fn apply_advance(&self, st: &mut State, now: i64) -> Result<()> {
        let p = AdvancePayload::decode(&self.payload)?;
        let mut cur = st.current_epoch();
        state::vote(&mut cur.attestations, self.payer, self.subject);
        if state::tally(cur.attestations(), &self.subject) < cur.info.threshold {
            // Not yet decided: record the vote against the sitting epoch and stop.
            return st.put_epoch(cur);
        }
        // Decided. The transaction carrying the deciding vote also carries the
        // proposal itself, so no proposal body is ever stored while it is pending —
        // the digest the members attested is the whole record of what they agreed.
        cur.info.end_time = now;
        cur.info.status = fhe::EpochStatus::Ended;
        st.put_epoch(cur)?;
        let next = EpochRecord {
            info: fhe::EpochInfo {
                epoch: p.epoch,
                start_time: now,
                end_time: 0,
                committee: p.committee.clone(),
                threshold: p.threshold,
                public_key: p.public_key.clone(),
                status: fhe::EpochStatus::Active,
            },
            attestations: None,
        };
        st.put_epoch(next)?;
        st.set_current_epoch(p.epoch)
    }
}

