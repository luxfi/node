// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The Q-Chain finality bridge.
//!
//! A block is final when a quorum of the committee has SIGNED it and the
//! aggregate of those signatures verifies against the committee's keys. Every
//! word of that is load-bearing: signed, not claimed; verified, not counted;
//! aggregate, not a tally of names a sender chose for itself.
//!
//! Each validator signature has two halves, made over the same bytes: BLS12-381
//! (which is what aggregates) and the validator's ML-DSA-65 identity (which is
//! what survives a quantum adversary). A signature is admitted only if both
//! halves check out, so the post-quantum half rides inside every signature the
//! quorum is counted from.
//!
//! ## Why the certificate is not a field of the block
//!
//! The block id is the SHA-256 of the block's own bytes. A signature inside the
//! block would make the id depend on WHO signed, so two honest nodes would
//! compute two ids for one block — a fork by construction. The certificate is a
//! quorum's statement ABOUT a block, verifiable by anyone holding the validator
//! set, and it lives here.

use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::Mutex;

use blst::min_pk::{
    AggregatePublicKey, AggregateSignature, PublicKey, SecretKey, Signature as BlsSignature,
};

use crate::config;
use crate::error::{Error, Result};
use crate::ids::Id;
use crate::quantum::{Key as MldsaKey, Quantum, ALGORITHM_MLDSA65};

/// The domain the committee signs under.
///
/// The same string the Go committee uses (`CiphersuiteSignature`). A different
/// tag is a different network whose signatures silently never verify here.
pub const DST: &[u8] = b"BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_";

/// One validator's statement about one block.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Sig {
    /// Who claims to have made it. A claim: the name is only worth anything
    /// once the signature verifies against the key registered under it.
    pub validator: String,
    /// BLS12-381, compressed G2. What aggregates.
    pub bls: Vec<u8>,
    /// ML-DSA-65 over the same bytes. What survives a quantum adversary.
    pub mldsa: Vec<u8>,
}

/// A quorum's statement about one block.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Aggregate {
    /// The aggregated BLS signature.
    pub bls: Vec<u8>,
    /// Who is in it. This is the list the threshold is counted over, and it is
    /// counted as a SET — see [`Quasar::verify_aggregate`].
    pub validators: Vec<String>,
}

/// A committee member: what is needed to CHECK their signatures.
#[derive(Clone, Debug)]
struct Member {
    bls: PublicKey,
    mldsa: Vec<u8>,
    weight: u64,
}

/// A block gathering signatures.
///
/// Every signature in it has been verified against the block hash and against
/// the registered key of the validator it names, and no two name the same
/// validator.
#[derive(Clone, Debug)]
pub struct Pending {
    pub block: Id,
    pub hash: Vec<u8>,
    pub height: u64,
    pub signatures: Vec<Sig>,
    pub finalized: bool,
}

impl Pending {
    /// This block's signature from one validator, if it has one.
    fn by(&self, validator: &str) -> Option<&Sig> {
        self.signatures.iter().find(|s| s.validator == validator)
    }
}

/// How the bridge is set up. The committee size is the only quorum input: the
/// threshold follows from it, so the two cannot be set to disagree.
#[derive(Clone, Debug)]
pub struct Setup {
    pub validator: String,
    pub committee: usize,
}

struct Held {
    members: BTreeMap<String, Member>,
    /// The secret halves this node actually holds — its own, and any minted
    /// here by [`Quasar::add_validator`].
    secrets: BTreeMap<String, (SecretKey, MldsaKey)>,
    pending: HashMap<Id, Pending>,
    finalized: HashSet<Id>,
}

/// The bridge.
pub struct Quasar {
    held: Mutex<Held>,
    validator: String,
    threshold: usize,
    committee: usize,
    quantum: Quantum,
}

impl Quasar {
    /// Create the bridge and register this node as its own validator.
    ///
    /// Registration is not optional and not deferred, because a bridge that
    /// cannot sign for its own identity does nothing at all: signing answers
    /// "validator not found", so no signature is ever recorded, so no peer
    /// signature ever finds a block to attach to, so the quorum is never
    /// reached and finality is unreachable — silently, one warning per block.
    ///
    /// The committee must be able to survive a fault. Below
    /// [`config::COMMITTEE_MIN`] the quorum ⌊2n/3⌋+1 is the entire committee,
    /// which makes one absent validator a halt and one dishonest validator the
    /// decision.
    pub fn new(setup: Setup) -> Result<Quasar> {
        if setup.validator.is_empty() {
            return Err(Error::NoValidatorId);
        }
        let committee = if setup.committee == 0 {
            config::COMMITTEE_MIN
        } else {
            setup.committee
        };
        if committee < config::COMMITTEE_MIN {
            return Err(Error::Config(format!(
                "a committee of {committee} tolerates no fault; {} is the smallest that does",
                config::COMMITTEE_MIN
            )));
        }

        // The stamp window does not bound a committee signature — a block's
        // certificate is checked whenever the block is, which may be long
        // after. What the signer is for here is the ML-DSA half of a validator
        // signature, checked directly rather than through the freshness rule.
        let quantum = Quantum::new(ALGORITHM_MLDSA65, std::time::Duration::from_secs(1))?;

        let q = Quasar {
            held: Mutex::new(Held {
                members: BTreeMap::new(),
                secrets: BTreeMap::new(),
                pending: HashMap::new(),
                finalized: HashSet::new(),
            }),
            validator: setup.validator.clone(),
            threshold: config::quorum(committee),
            committee,
            quantum,
        };
        q.add_validator(&setup.validator, 1)?;
        Ok(q)
    }

    /// How many validators must sign a block.
    pub fn threshold(&self) -> usize {
        self.threshold
    }

    /// The committee size the threshold was derived from.
    pub fn committee(&self) -> usize {
        self.committee
    }

    /// This node's identity in the committee.
    pub fn validator(&self) -> &str {
        &self.validator
    }

    /// How many validators are registered.
    pub fn registered(&self) -> usize {
        self.held.lock().expect("quasar").members.len()
    }

    /// Register a validator whose keys are minted here.
    ///
    /// This is the reference's registration path, and it is a LOCAL ceremony:
    /// the keys come into existence in this process. That is right for this
    /// node's own identity and for a single-process committee; a peer's keys
    /// come from the chain that published them, and register through
    /// [`Quasar::admit`], which takes public halves only.
    ///
    /// The committee is a SET, and it is the set the threshold was derived
    /// from. Registering an id twice would hand it a fresh key, silently
    /// invalidating every signature that validator has already contributed;
    /// registering more validators than the committee declares would make the
    /// threshold a quorum of a committee that no longer exists.
    pub fn add_validator(&self, validator: &str, weight: u64) -> Result<()> {
        let mut ikm = [0u8; 32];
        getrandom::getrandom(&mut ikm).map_err(|e| Error::Signing(e.to_string()))?;
        let bls = SecretKey::key_gen(&ikm, &[]).map_err(|e| Error::Signing(format!("{e:?}")))?;
        let mldsa = self.quantum.generate()?;

        let member = Member {
            bls: bls.sk_to_pk(),
            mldsa: mldsa.public.clone(),
            weight,
        };
        let mut held = self.held.lock().expect("quasar");
        Self::register(&mut held, validator, member, self.committee)?;
        held.secrets.insert(validator.to_string(), (bls, mldsa));
        Ok(())
    }

    /// Register a validator from the public halves the chain published.
    ///
    /// This node cannot sign as them, which is the point: what a committee
    /// member needs from a peer is the ability to CHECK what that peer says.
    pub fn admit(&self, validator: &str, weight: u64, bls: &[u8], mldsa: &[u8]) -> Result<()> {
        let bls = PublicKey::key_validate(bls)
            .map_err(|e| Error::UnverifiedSigner(format!("{validator}: BLS key: {e:?}")))?;
        if mldsa.len() != self.quantum.public_key_size() {
            return Err(Error::UnverifiedSigner(format!(
                "{validator}: ML-DSA key is {} bytes, ML-DSA-65 takes {}",
                mldsa.len(),
                self.quantum.public_key_size()
            )));
        }
        let member = Member {
            bls,
            mldsa: mldsa.to_vec(),
            weight,
        };
        let mut held = self.held.lock().expect("quasar");
        Self::register(&mut held, validator, member, self.committee)
    }

    fn register(held: &mut Held, validator: &str, member: Member, committee: usize) -> Result<()> {
        if validator.is_empty() {
            return Err(Error::NoValidatorId);
        }
        if held.members.contains_key(validator) {
            return Err(Error::AlreadyRegistered(validator.to_string()));
        }
        if held.members.len() >= committee {
            return Err(Error::CommitteeFull {
                have: held.members.len(),
                committee,
            });
        }
        held.members.insert(validator.to_string(), member);
        Ok(())
    }

    /// This validator's public halves, for a peer that has to check what it
    /// says.
    pub fn public_keys(&self, validator: &str) -> Result<(Vec<u8>, Vec<u8>)> {
        let held = self.held.lock().expect("quasar");
        let m = held
            .members
            .get(validator)
            .ok_or_else(|| Error::UnverifiedSigner(validator.to_string()))?;
        Ok((m.bls.compress().to_vec(), m.mldsa.clone()))
    }

    /// The weight registered for a validator.
    pub fn weight(&self, validator: &str) -> Option<u64> {
        self.held
            .lock()
            .expect("quasar")
            .members
            .get(validator)
            .map(|m| m.weight)
    }

    /// Sign a block with this node's key and record the signature.
    ///
    /// Signing the same block again returns the signature already recorded: one
    /// validator makes one statement about one block.
    pub fn sign_block(&self, block: Id, hash: &[u8], height: u64) -> Result<Sig> {
        let mut held = self.held.lock().expect("quasar");
        let tracked = held.pending.contains_key(&block);
        held.pending.entry(block).or_insert_with(|| Pending {
            block,
            hash: hash.to_vec(),
            height,
            signatures: Vec::new(),
            finalized: false,
        });
        if let Some(have) = held.pending[&block].by(&self.validator) {
            return Ok(have.clone());
        }
        // Sign over the hash the ENTRY holds, not the argument: a second call
        // naming other bytes for a block already tracked would otherwise
        // produce a signature over something no one else signed.
        let message = held.pending[&block].hash.clone();

        let sig = match self.make(&held, &self.validator, &message) {
            Ok(sig) => sig,
            Err(e) => {
                // The entry was created for a signature that never arrived.
                // Only a block this node signed is ever finalized and only a
                // finalized block is ever cleaned up, so leaving it behind
                // leaks one entry per failure for the life of the process.
                if !tracked {
                    held.pending.remove(&block);
                }
                return Err(e);
            }
        };

        let Held {
            members, pending, ..
        } = &mut *held;
        let entry = pending.get_mut(&block).expect("just inserted");
        Self::admit_sig(members, &self.quantum, entry, sig.clone())?;
        Ok(sig)
    }

    /// Admit a peer's signature for a block this node is tracking.
    pub fn add_signature(&self, block: Id, sig: Sig) -> Result<()> {
        let mut held = self.held.lock().expect("quasar");
        let Held {
            members, pending, ..
        } = &mut *held;
        let entry = pending.get_mut(&block).ok_or(Error::UnknownBlock(block))?;
        Self::admit_sig(members, &self.quantum, entry, sig)
    }

    /// Verify one signature, then file it under the identity that verification
    /// authenticated.
    ///
    /// Counting a caller-supplied name instead made the quorum a count of
    /// strings: three fabricated validator ids finalized a block, and five
    /// spellings of one name — case, a trailing space, a NUL, the fullwidth
    /// forms — counted as five signers of one signature. Verification looks the
    /// claimed id up in the committee and checks the signature against THAT key
    /// over THIS block's hash, so a name nobody holds a key for fails, a
    /// respelling resolves to no validator and fails, and a signature made for
    /// another block fails against this one.
    fn admit_sig(
        members: &BTreeMap<String, Member>,
        quantum: &Quantum,
        entry: &mut Pending,
        sig: Sig,
    ) -> Result<()> {
        Self::check(members, quantum, &entry.hash, &sig)?;
        if entry.by(&sig.validator).is_some() {
            return Err(Error::DuplicateSigner(sig.validator));
        }
        entry.signatures.push(sig);
        Ok(())
    }

    /// Both halves of one validator signature, over one message.
    fn check(
        members: &BTreeMap<String, Member>,
        quantum: &Quantum,
        message: &[u8],
        sig: &Sig,
    ) -> Result<()> {
        let member = members.get(&sig.validator).ok_or_else(|| {
            Error::UnverifiedSigner(format!("{} is not in the committee", sig.validator))
        })?;

        let bls = BlsSignature::uncompress(&sig.bls)
            .map_err(|e| Error::UnverifiedSigner(format!("{}: BLS: {e:?}", sig.validator)))?;
        if bls.verify(true, message, DST, &[], &member.bls, true) != blst::BLST_ERROR::BLST_SUCCESS
        {
            return Err(Error::UnverifiedSigner(format!(
                "{}: BLS does not check out",
                sig.validator
            )));
        }

        // The post-quantum half is not optional here. It rides in every
        // signature this bridge makes, so one arriving without it is one made
        // by something that is not a Q-Chain validator.
        if sig.mldsa.len() != quantum.signature_size() {
            return Err(Error::UnverifiedSigner(format!(
                "{}: ML-DSA half is {} bytes, ML-DSA-65 takes {}",
                sig.validator,
                sig.mldsa.len(),
                quantum.signature_size()
            )));
        }
        let ok = {
            use lux_pq::Verifier as _;
            let pk = lux_pq::sign::PublicKey::from_bytes(member.mldsa.clone());
            pk.verify(message, &lux_pq::Signature::from_bytes(sig.mldsa.clone()))
        };
        if !ok {
            return Err(Error::UnverifiedSigner(format!(
                "{}: ML-DSA does not check out",
                sig.validator
            )));
        }
        Ok(())
    }

    /// Make this node's signature over `message`.
    fn make(&self, held: &Held, validator: &str, message: &[u8]) -> Result<Sig> {
        let (bls, mldsa) = held.secrets.get(validator).ok_or_else(|| {
            Error::UnverifiedSigner(format!("{validator}: this node holds no key for it"))
        })?;
        let bls_sig = bls.sign(message, DST, &[]).compress().to_vec();
        let mldsa_sig = {
            use lux_pq::Signer as _;
            let sk = lux_pq::sign::SecretKey::from_bytes(mldsa.secret.clone());
            sk.sign(message)
                .map_err(|e| Error::Signing(e.to_string()))?
                .as_bytes()
                .to_vec()
        };
        Ok(Sig {
            validator: validator.to_string(),
            bls: bls_sig,
            mldsa: mldsa_sig,
        })
    }

    /// Whether one validator signature checks out over a message.
    pub fn verify(&self, message: &[u8], sig: &Sig) -> bool {
        let held = self.held.lock().expect("quasar");
        Self::check(&held.members, &self.quantum, message, sig).is_ok()
    }

    /// Whether an aggregate checks out: the aggregate itself against the
    /// aggregated keys of the DISTINCT registered validators it names, at or
    /// above the threshold.
    ///
    /// Distinct is the load-bearing word. BLS is linear, so a repeated id adds
    /// the same key again: t copies of one validator yield t·pk, which that
    /// validator's own signature scaled to t·σ satisfies — one validator forges
    /// a t-of-n aggregate. A signer COUNT carried inside the message is not
    /// evidence of anything and is never what the threshold is compared
    /// against.
    pub fn verify_aggregate(&self, message: &[u8], agg: &Aggregate) -> bool {
        let held = self.held.lock().expect("quasar");
        let sig = match BlsSignature::uncompress(&agg.bls) {
            Ok(s) => s,
            Err(_) => return false,
        };

        let mut seen = HashSet::new();
        let mut keys = Vec::with_capacity(agg.validators.len());
        for validator in &agg.validators {
            if !seen.insert(validator.clone()) {
                return false;
            }
            match held.members.get(validator) {
                Some(m) => keys.push(m.bls),
                None => return false,
            }
        }
        if seen.len() < self.threshold {
            return false;
        }

        let refs: Vec<&PublicKey> = keys.iter().collect();
        let agg_pk = match AggregatePublicKey::aggregate(&refs, true) {
            Ok(pk) => pk.to_public_key(),
            Err(_) => return false,
        };
        sig.verify(true, message, DST, &[], &agg_pk, true) == blst::BLST_ERROR::BLST_SUCCESS
    }

    /// Finalize a block once the quorum's signatures aggregate into a signature
    /// that verifies over it.
    ///
    /// Reaching the count is necessary and not sufficient: the aggregate is
    /// built and CHECKED, and only a check that passes finalizes anything.
    pub fn try_finalize(&self, block: Id) -> Result<Option<Aggregate>> {
        let (hash, sigs, height) = {
            let held = self.held.lock().expect("quasar");
            let pending = held.pending.get(&block).ok_or(Error::UnknownBlock(block))?;
            if pending.signatures.len() < self.threshold {
                return Ok(None);
            }
            (
                pending.hash.clone(),
                pending.signatures.clone(),
                pending.height,
            )
        };

        let mut parsed = Vec::with_capacity(sigs.len());
        for s in &sigs {
            parsed
                .push(BlsSignature::uncompress(&s.bls).map_err(|e| {
                    Error::UnverifiedSigner(format!("{}: BLS: {e:?}", s.validator))
                })?);
        }
        let refs: Vec<&BlsSignature> = parsed.iter().collect();
        let agg = AggregateSignature::aggregate(&refs, true)
            .map_err(|e| Error::Signing(format!("aggregation failed: {e:?}")))?
            .to_signature();

        let aggregate = Aggregate {
            bls: agg.compress().to_vec(),
            validators: sigs.iter().map(|s| s.validator.clone()).collect(),
        };
        if !self.verify_aggregate(&hash, &aggregate) {
            return Err(Error::AggregateRefused(block));
        }

        let mut held = self.held.lock().expect("quasar");
        if let Some(p) = held.pending.get_mut(&block) {
            p.finalized = true;
        }
        held.finalized.insert(block);
        let _ = height;
        Ok(Some(aggregate))
    }

    /// Whether a block has been finalized here.
    pub fn is_finalized(&self, block: &Id) -> bool {
        self.held.lock().expect("quasar").finalized.contains(block)
    }

    /// What a block has collected so far.
    pub fn pending(&self, block: &Id) -> Option<Pending> {
        self.held
            .lock()
            .expect("quasar")
            .pending
            .get(block)
            .cloned()
    }

    /// Drop every block tracked below `min_height`.
    ///
    /// The height is the caller's finalized frontier, so a block beneath it
    /// will never gather another signature whether it finalized or not. Keeping
    /// only the finalized ones meant the entries that could not be cleaned up
    /// were exactly the ones that accumulated: every proposal that lost, timed
    /// out or failed to sign stayed in both maps for the life of the process.
    pub fn cleanup(&self, min_height: u64) {
        let mut held = self.held.lock().expect("quasar");
        let stale: Vec<Id> = held
            .pending
            .iter()
            .filter(|(_, p)| p.height < min_height)
            .map(|(id, _)| *id)
            .collect();
        for id in stale {
            held.pending.remove(&id);
            held.finalized.remove(&id);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ids;

    fn bridge(committee: usize) -> Quasar {
        Quasar::new(Setup {
            validator: "node-0".into(),
            committee,
        })
        .unwrap()
    }

    /// A committee of `n` in one process: every member's keys are minted here,
    /// which is what lets one test drive a whole quorum.
    fn committee(n: usize) -> Quasar {
        let q = bridge(n);
        for i in 1..n {
            q.add_validator(&format!("node-{i}"), 1).unwrap();
        }
        q
    }

    fn sign_as(q: &Quasar, validator: &str, message: &[u8]) -> Sig {
        let held = q.held.lock().expect("quasar");
        q.make(&held, validator, message).unwrap()
    }

    #[test]
    fn a_bridge_registers_its_own_identity_or_it_can_never_sign() {
        let q = bridge(4);
        assert_eq!(q.registered(), 1);
        assert_eq!(q.threshold(), 3);
        assert_eq!(q.committee(), 4);
        let sig = q.sign_block(ids::filled(1), b"hash", 1).unwrap();
        assert_eq!(sig.validator, "node-0");
    }

    #[test]
    fn a_committee_that_tolerates_no_fault_is_refused() {
        for n in 1..config::COMMITTEE_MIN {
            assert!(Quasar::new(Setup {
                validator: "node-0".into(),
                committee: n,
            })
            .is_err());
        }
        // Zero means unset, and settles on the smallest committee that works.
        let q = Quasar::new(Setup {
            validator: "node-0".into(),
            committee: 0,
        })
        .unwrap();
        assert_eq!(q.committee(), config::COMMITTEE_MIN);
    }

    #[test]
    fn a_signer_with_no_identity_is_not_a_member_of_a_quorum() {
        assert!(matches!(
            Quasar::new(Setup {
                validator: String::new(),
                committee: 4,
            }),
            Err(Error::NoValidatorId)
        ));
    }

    #[test]
    fn the_committee_is_a_set_of_a_declared_size() {
        let q = bridge(4);
        q.add_validator("node-1", 1).unwrap();
        assert!(matches!(
            q.add_validator("node-1", 1),
            Err(Error::AlreadyRegistered(_))
        ));
        q.add_validator("node-2", 1).unwrap();
        q.add_validator("node-3", 1).unwrap();
        assert!(matches!(
            q.add_validator("node-4", 1),
            Err(Error::CommitteeFull { .. })
        ));
    }

    #[test]
    fn signing_the_same_block_twice_is_one_statement() {
        let q = bridge(4);
        let id = ids::filled(2);
        let first = q.sign_block(id, b"hash", 7).unwrap();
        let second = q.sign_block(id, b"hash", 7).unwrap();
        assert_eq!(first, second);
        assert_eq!(q.pending(&id).unwrap().signatures.len(), 1);
    }

    #[test]
    fn a_signature_that_names_a_validator_nobody_holds_a_key_for_is_refused() {
        let q = committee(4);
        let id = ids::filled(3);
        q.sign_block(id, b"hash", 1).unwrap();

        let mut forged = sign_as(&q, "node-1", b"hash");
        forged.validator = "node-9".into();
        assert!(matches!(
            q.add_signature(id, forged),
            Err(Error::UnverifiedSigner(_))
        ));
        assert_eq!(q.pending(&id).unwrap().signatures.len(), 1);
    }

    #[test]
    fn a_respelling_of_a_name_is_not_a_second_signer() {
        let q = committee(4);
        let id = ids::filled(4);
        q.sign_block(id, b"hash", 1).unwrap();
        for name in ["NODE-0", "node-0 ", "node-0\0", "ｎｏｄｅ-0"] {
            let mut copy = sign_as(&q, "node-0", b"hash");
            copy.validator = name.into();
            assert!(
                q.add_signature(id, copy).is_err(),
                "{name} was admitted as a second signer"
            );
        }
        assert_eq!(q.pending(&id).unwrap().signatures.len(), 1);
    }

    #[test]
    fn one_validator_cannot_reach_the_quorum_by_resending() {
        let q = committee(4);
        let id = ids::filled(5);
        q.sign_block(id, b"hash", 1).unwrap();
        let again = sign_as(&q, "node-0", b"hash");
        assert!(matches!(
            q.add_signature(id, again),
            Err(Error::DuplicateSigner(_))
        ));
    }

    #[test]
    fn a_signature_made_for_another_block_does_not_count_for_this_one() {
        let q = committee(4);
        let id = ids::filled(6);
        q.sign_block(id, b"this block", 1).unwrap();
        let elsewhere = sign_as(&q, "node-1", b"another block");
        assert!(matches!(
            q.add_signature(id, elsewhere),
            Err(Error::UnverifiedSigner(_))
        ));
    }

    #[test]
    fn a_signature_missing_its_post_quantum_half_is_refused() {
        let q = committee(4);
        let id = ids::filled(7);
        q.sign_block(id, b"hash", 1).unwrap();
        let mut half = sign_as(&q, "node-1", b"hash");
        half.mldsa.clear();
        assert!(matches!(
            q.add_signature(id, half),
            Err(Error::UnverifiedSigner(_))
        ));

        // …and one whose post-quantum half is over other bytes, likewise.
        let mut swapped = sign_as(&q, "node-1", b"hash");
        swapped.mldsa = sign_as(&q, "node-1", b"other").mldsa;
        assert!(matches!(
            q.add_signature(id, swapped),
            Err(Error::UnverifiedSigner(_))
        ));
    }

    #[test]
    fn a_block_finalizes_when_the_quorum_aggregates_and_verifies() {
        let q = committee(4);
        let id = ids::filled(8);
        q.sign_block(id, b"the block hash", 9).unwrap();
        assert!(q.try_finalize(id).unwrap().is_none(), "one of three");

        q.add_signature(id, sign_as(&q, "node-1", b"the block hash"))
            .unwrap();
        assert!(q.try_finalize(id).unwrap().is_none(), "two of three");

        q.add_signature(id, sign_as(&q, "node-2", b"the block hash"))
            .unwrap();
        let agg = q.try_finalize(id).unwrap().expect("three of three");
        assert!(q.is_finalized(&id));
        assert!(q.verify_aggregate(b"the block hash", &agg));
        assert!(!q.verify_aggregate(b"a different message", &agg));
    }

    #[test]
    fn an_aggregate_naming_one_validator_three_times_is_not_a_quorum() {
        let q = committee(4);
        let id = ids::filled(9);
        q.sign_block(id, b"hash", 1).unwrap();
        q.add_signature(id, sign_as(&q, "node-1", b"hash")).unwrap();
        q.add_signature(id, sign_as(&q, "node-2", b"hash")).unwrap();
        let agg = q.try_finalize(id).unwrap().unwrap();

        let repeated = Aggregate {
            bls: agg.bls.clone(),
            validators: vec!["node-0".into(), "node-0".into(), "node-0".into()],
        };
        assert!(!q.verify_aggregate(b"hash", &repeated));

        let short = Aggregate {
            bls: agg.bls.clone(),
            validators: vec!["node-0".into(), "node-1".into()],
        };
        assert!(!q.verify_aggregate(b"hash", &short), "under the threshold");

        let stranger = Aggregate {
            bls: agg.bls,
            validators: vec!["node-0".into(), "node-1".into(), "node-404".into()],
        };
        assert!(!q.verify_aggregate(b"hash", &stranger));
    }

    #[test]
    fn a_peer_registers_by_its_public_halves_and_this_node_cannot_sign_as_it() {
        let mine = committee(4);
        let theirs = bridge(4);
        let (bls, mldsa) = theirs.public_keys("node-0").unwrap();

        let peer = bridge(4);
        peer.admit("peer", 5, &bls, &mldsa).unwrap();
        assert_eq!(peer.weight("peer"), Some(5));

        // Their signature checks out here…
        let id = ids::filled(10);
        peer.sign_block(id, b"hash", 1).unwrap();
        let theirs_sig = sign_as(&theirs, "node-0", b"hash");
        let renamed = Sig {
            validator: "peer".into(),
            ..theirs_sig
        };
        peer.add_signature(id, renamed).unwrap();

        // …and this node holds no secret for them, so it cannot speak as them.
        let held = peer.held.lock().expect("quasar");
        assert!(peer.make(&held, "peer", b"hash").is_err());
        drop(held);
        let _ = mine;
    }

    #[test]
    fn a_key_that_is_not_a_key_is_refused_at_registration() {
        let q = bridge(4);
        assert!(q.admit("peer", 1, b"not a bls key", &[0; 1952]).is_err());
        let (bls, _) = q.public_keys("node-0").unwrap();
        assert!(q.admit("peer", 1, &bls, b"short").is_err());
    }

    // Go: TestSignBlockDoesNotRaceIncomingSignatures. This node signing and
    // peers' signatures arriving happen at once for every block; what must hold
    // is that each validator ends up counted exactly once.
    #[test]
    fn signing_does_not_race_the_signatures_coming_in() {
        let q = std::sync::Arc::new(committee(4));
        let id = ids::filled(14);
        let hash = b"the block hash";
        // The peers' signatures are made up front: making one needs the lock
        // this test is about.
        let peers: Vec<Sig> = ["node-1", "node-2", "node-3"]
            .iter()
            .map(|v| sign_as(&q, v, hash))
            .collect();

        std::thread::scope(|scope| {
            let mine = std::sync::Arc::clone(&q);
            scope.spawn(move || {
                for _ in 0..16 {
                    mine.sign_block(id, hash, 3).unwrap();
                }
            });
            for sig in &peers {
                let theirs = std::sync::Arc::clone(&q);
                let sig = sig.clone();
                scope.spawn(move || {
                    // The block may not be tracked yet, and a duplicate is
                    // refused by design; both are answers, not crashes.
                    let _ = theirs.add_signature(id, sig);
                });
            }
        });

        // Whatever the interleaving was, this node signed once and no validator
        // is in the set twice.
        let pending = q.pending(&id).expect("the block is tracked");
        let mut names: Vec<&str> = pending
            .signatures
            .iter()
            .map(|s| s.validator.as_str())
            .collect();
        let before = names.len();
        names.sort_unstable();
        names.dedup();
        assert_eq!(names.len(), before, "a validator was counted twice");
        assert!(names.contains(&"node-0"));
    }

    #[test]
    fn a_block_below_the_finalized_frontier_is_dropped_whether_it_finalized_or_not() {
        let q = committee(4);
        let lost = ids::filled(11);
        let won = ids::filled(12);
        q.sign_block(lost, b"lost", 5).unwrap();
        q.sign_block(won, b"won", 9).unwrap();
        q.add_signature(won, sign_as(&q, "node-1", b"won")).unwrap();
        q.add_signature(won, sign_as(&q, "node-2", b"won")).unwrap();
        q.try_finalize(won).unwrap().unwrap();

        q.cleanup(9);
        assert!(
            q.pending(&lost).is_none(),
            "a proposal that lost still leaked"
        );
        assert!(q.pending(&won).is_some());

        q.cleanup(10);
        assert!(q.pending(&won).is_none());
        assert!(!q.is_finalized(&won));
    }

    #[test]
    fn a_signature_for_a_block_nobody_is_tracking_has_nothing_to_attach_to() {
        let q = committee(4);
        let sig = sign_as(&q, "node-1", b"hash");
        assert!(matches!(
            q.add_signature(ids::filled(13), sig),
            Err(Error::UnknownBlock(_))
        ));
        assert!(matches!(
            q.try_finalize(ids::filled(13)),
            Err(Error::UnknownBlock(_))
        ));
    }
}
