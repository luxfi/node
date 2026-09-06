// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Whether a shielded transaction's proof is good for it.
//!
//! A zero-knowledge proof IS the credential on a shielded chain: it is the only
//! thing standing between the pool and minting from nothing, so everything here
//! fails closed. A proof under a system this chain does not run is refused for
//! what it IS rather than judged; a proof under the system it does run is
//! judged, and refused when nothing can judge it.
//!
//! ## The profile gate is ONE function in ONE place
//!
//! [`Verifier::refuse_classical_under_strict_pq`] is the whole of the strict-PQ
//! rule for shielded value, and it runs BEFORE any other check. One bit —
//! [`crate::config::Config::strict_pq`] — drives both switches: on a strict-PQ
//! chain the classical verifiers are absent and the shielded verifier refuses
//! classical systems. There is no second place to keep in step, and no path
//! that reaches a pairing with the bit set.
//!
//! ## The cache is keyed on CONTENT
//!
//! [`crate::txs::Tx::id`] covers the nullifiers, the outputs and the proof, so
//! a hit means this exact transaction verified before. A cache keyed on
//! anything the peer supplies is a cache an attacker addresses: copy an
//! accepted transaction's id, proof type, proof and public inputs onto a
//! transaction spending entirely different notes, and the key matches — the hit
//! returns before anything ties the public inputs to what is being spent, and
//! the shielded pool gains value backed by a proof that attests to nothing
//! about it.
//!
//! ## What this port does not carry
//!
//! The classical path decodes, checks its public inputs and checks its lengths,
//! and then refuses: there is no bn254 pairing arithmetic here. That is
//! reachable only on a chain that turns strict-PQ OFF *and* supplies real
//! verifying keys, since a chain with no keys refuses the classical path
//! outright and a strict-PQ chain refuses the keys themselves. Where the
//! reference would compute a pairing, this answers
//! [`Error::Groth16Unbound`] — the same posture the STARK path takes with no
//! FRI binding, and for the same reason: a proof nothing verified is never
//! accepted.

use std::collections::{HashMap, VecDeque};

use crate::config::Config;
use crate::error::{Error, Result};
use crate::ids::Id;
use crate::starkfri;
use crate::txs::Tx;

/// A Groth16 proof is two G1 points and one G2 point.
const GROTH16_PROOF_LEN: usize = 2 * 64 + 128;

/// A PLONK proof is nine G1 commitments and the five evaluations that go with
/// them. Every evaluation is part of the proof, so a frame short of one is not
/// a proof rather than a proof with a zero in it.
const PLONK_PROOF_LEN: usize = 9 * 64 + 5 * 32;

/// The proof verifier, and the verdicts it has already reached.
#[derive(Debug)]
pub struct Verifier {
    strict_pq: bool,

    /// `sha256(ChainID ‖ NetworkID)`. The first public input every proof is
    /// checked against, so a proof made for another chain does not verify here
    /// even when the notes it names are unspent on both.
    bind: Id,

    /// A real verifying key per circuit. A circuit the operator did not key has
    /// no key here, so the lookup in front of each verify refuses it by name —
    /// per circuit, so an absent key can never be mistaken for a present one.
    keys: HashMap<u8, Vec<u8>>,

    /// Whether there are no real keys at all. A verifier holding none judges
    /// nothing, which is what the classical path checks before it starts.
    dummy: bool,

    cache: Cache,

    verified: u64,
    hits: u64,
    misses: u64,
}

impl Verifier {
    /// Build a verifier for `cfg` on the chain `bind` names.
    ///
    /// Loading a real bn254 verifying key on a strict-PQ chain is refused HERE,
    /// at construction, rather than left to the dummy-key detector: such keys
    /// would re-enable the forgeable classical path for shielded value, and the
    /// shielded path on a strict-PQ chain is STARK/FRI only.
    pub fn new(cfg: &Config, bind: Id) -> Result<Verifier> {
        let mut keys = HashMap::new();
        for circuit in Config::keyed_circuits() {
            if let Some(k) = cfg.verifying_keys.get(&circuit.0) {
                if !k.is_empty() {
                    keys.insert(circuit.0, k.clone());
                }
            }
        }
        let dummy = keys.is_empty();
        if cfg.strict_pq && !dummy {
            return Err(Error::StrictPqRealVkForbidden);
        }
        Ok(Verifier {
            strict_pq: cfg.strict_pq,
            bind,
            keys,
            dummy,
            cache: Cache::new(cfg.proof_cache_size as usize),
            verified: 0,
            hits: 0,
            misses: 0,
        })
    }

    /// Whether real verifying keys are loaded.
    pub fn keys_loaded(&self) -> bool {
        !self.dummy
    }

    /// How many verdicts the cache holds.
    pub fn cache_len(&self) -> usize {
        self.cache.len()
    }

    /// Verifications run, cache hits, cache misses.
    pub fn stats(&self) -> (u64, u64, u64) {
        (self.verified, self.hits, self.misses)
    }

    /// Judge a transaction's proof.
    pub fn verify_tx(&mut self, tx: &Tx) -> Result<()> {
        let Some(proof) = &tx.proof else {
            return Err(Error::MissingProof);
        };

        // The profile gate, before anything else. A machine that broke bn254
        // must not be able to forge a shield or unshield proof.
        self.refuse_classical_under_strict_pq(&proof.system)?;

        // The only accepted system on a strict-PQ chain, and the quantum-safe
        // shielded path everywhere.
        if proof.system == b"stark" {
            return self.verify_stark(tx);
        }

        // Below here the chain is not strict-PQ — the gate above already
        // refused these otherwise — and the classical path needs real keys.
        if self.dummy {
            return Err(Error::ProofVerificationDisabled);
        }

        let key = tx.id();
        self.verified += 1;
        if let Some(verdict) = self.cache.get(&key) {
            self.hits += 1;
            return if verdict {
                Ok(())
            } else {
                Err(Error::ProofVerificationDisabled)
            };
        }
        self.misses += 1;

        let verdict = match proof.system.as_slice() {
            b"groth16" => self.verify_groth16(tx),
            b"plonk" => self.verify_plonk(tx),
            b"bulletproofs" => Err(Error::BulletproofsUnimplemented),
            _ => Err(Error::UnsupportedProofType),
        };
        self.cache.add(key, verdict.is_ok());
        verdict
    }

    /// Judge every transaction in a block that carries an aggregated proof.
    ///
    /// There is ONE verification path. A batch path taken when an accelerator
    /// is present would be a second, inline copy of the checks that consulted
    /// neither the cache nor the profile gate — so whether a node accepted a
    /// block would turn on whether that node had an accelerator, and validators
    /// with and without one would reject each other.
    pub fn verify_block_proof(&mut self, txs: &[Tx]) -> Result<()> {
        for tx in txs {
            self.verify_tx(tx)?;
        }
        Ok(())
    }

    /// The single strict-PQ enforcement point for shielded transactions.
    ///
    /// On a non-strict chain it is a no-op. On a strict-PQ one it leaves
    /// exactly one system standing, by name — so a system nobody has heard of
    /// is refused the same way groth16 is, rather than falling through to a
    /// default.
    fn refuse_classical_under_strict_pq(&self, system: &[u8]) -> Result<()> {
        if !self.strict_pq || system == b"stark" {
            return Ok(());
        }
        Err(Error::StrictPqClassicalForbidden)
    }

    /// Delegate to the STARK/FRI verifier, binding the proof to this
    /// transaction's shielded value flow.
    ///
    /// The public inputs are the chain binding, then the nullifiers it spends,
    /// then the commitments it creates. The binding comes first so a proof made
    /// for another chain does not verify here even when the notes it names are
    /// unspent on both.
    fn verify_stark(&self, tx: &Tx) -> Result<()> {
        let mut public = Vec::with_capacity(96);
        public.extend_from_slice(&self.bind);
        for n in &tx.nullifiers {
            public.extend_from_slice(n);
        }
        for c in tx.commitments() {
            public.extend_from_slice(c);
        }

        let data = tx.proof.as_ref().map(|p| p.data.as_slice()).unwrap_or(&[]);
        match starkfri::verify(data, &public) {
            Ok(true) => Ok(()),
            Ok(false) => Err(Error::StarkRejected),
            // The binding is pending: fail closed, and say which of the two it
            // was so an operator is not left guessing whether the proof was
            // wrong or the node could not judge it.
            Err(e @ Error::StarkVerifierNotRegistered) => Err(Error::StarkUnbound(Box::new(e))),
            Err(e) => Err(Error::StarkFailed(Box::new(e))),
        }
    }

    fn verify_groth16(&self, tx: &Tx) -> Result<()> {
        let proof = tx.proof.as_ref().ok_or(Error::MissingProof)?;
        if !self.keys.contains_key(&tx.kind.0) {
            return Err(Error::NoVerifyingKey(tx.kind.0));
        }
        self.check_public_inputs(tx)?;
        if proof.data.len() < GROTH16_PROOF_LEN {
            return Err(Error::Groth16ProofLength);
        }
        Err(Error::Groth16Unbound)
    }

    /// PLONK refuses every proof, and the decoding above it still runs so a
    /// caller can tell a malformed proof from one this will not judge.
    ///
    /// What used to stand in this place, in the reference, checked
    /// `e(Wz + u·Wzw, [α]₂) = e(z·Wz + u·zω·Wzw, [1]₂)` and nothing else: it
    /// computed the public-input polynomial at the challenge point and threw
    /// the value away, and it never read the selector, permutation, quotient or
    /// evaluation commitments. It related the two opening proofs to each other
    /// and said nothing about the statement being proved, and a proof bound to
    /// no statement is a proof of anything. Refusing costs nothing that works
    /// today, because an honest prover's proof does not satisfy that equation
    /// either.
    fn verify_plonk(&self, tx: &Tx) -> Result<()> {
        let proof = tx.proof.as_ref().ok_or(Error::MissingProof)?;
        if !self.keys.contains_key(&tx.kind.0) {
            return Err(Error::NoVerifyingKey(tx.kind.0));
        }
        self.check_public_inputs(tx)?;
        if proof.data.len() < PLONK_PROOF_LEN {
            return Err(Error::PlonkProofLength);
        }
        Err(Error::PlonkIncomplete)
    }

    /// Whether the public inputs say what the transaction spends and creates.
    ///
    /// The first is the chain binding; then one per nullifier, in order; then
    /// one per output commitment. Exact byte comparison throughout — a public
    /// input that merely resembles a nullifier is a proof about a different
    /// note.
    fn check_public_inputs(&self, tx: &Tx) -> Result<()> {
        let proof = tx.proof.as_ref().ok_or(Error::MissingProof)?;
        let Some(first) = proof.public.first() else {
            return Err(Error::PublicInputs("no public inputs provided"));
        };
        if first.as_slice() != self.bind {
            return Err(Error::PublicInputs("public input mismatch for chain binding"));
        }

        for (i, nullifier) in tx.nullifiers.iter().enumerate() {
            let Some(given) = proof.public.get(i + 1) else {
                return Err(Error::PublicInputs("missing public input for nullifier"));
            };
            if given != nullifier {
                return Err(Error::PublicInputs("public input mismatch for nullifier"));
            }
        }

        let offset = tx.nullifiers.len() + 1;
        for (i, commitment) in tx.commitments().iter().enumerate() {
            let Some(given) = proof.public.get(offset + i) else {
                return Err(Error::PublicInputs(
                    "missing public input for output commitment",
                ));
            };
            if given.as_slice() != *commitment {
                return Err(Error::PublicInputs(
                    "public input mismatch for output commitment",
                ));
            }
        }
        Ok(())
    }
}

/// A bounded set of verdicts, oldest USE evicted first.
///
/// Recency rather than insertion order, because the transactions being asked
/// about repeat: a pool re-offering the same transaction every round would
/// otherwise push out the verdict it is about to ask for again.
#[derive(Debug)]
struct Cache {
    cap: usize,
    order: VecDeque<Id>,
    map: HashMap<Id, bool>,
}

impl Cache {
    fn new(cap: usize) -> Cache {
        Cache {
            cap: cap.max(1),
            order: VecDeque::new(),
            map: HashMap::new(),
        }
    }

    fn len(&self) -> usize {
        self.map.len()
    }

    fn get(&mut self, k: &Id) -> Option<bool> {
        let v = *self.map.get(k)?;
        self.touch(k);
        Some(v)
    }

    fn add(&mut self, k: Id, v: bool) {
        if self.map.insert(k, v).is_some() {
            self.touch(&k);
            return;
        }
        self.order.push_back(k);
        while self.map.len() > self.cap {
            if let Some(oldest) = self.order.pop_front() {
                self.map.remove(&oldest);
            }
        }
    }

    fn touch(&mut self, k: &Id) {
        if let Some(at) = self.order.iter().position(|x| x == k) {
            self.order.remove(at);
            self.order.push_back(*k);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::hash::Fold;
    use crate::ids;
    use crate::txs::{Kind, Proof, Shielded, Tx};

    fn bind() -> Id {
        let mut h = Fold::new();
        h.raw(&ids::repeated(40)).num32(1);
        h.id()
    }

    fn transfer(system: &str) -> Tx {
        Tx {
            kind: Kind::TRANSFER,
            version: 1,
            nullifiers: vec![vec![0x10; 32]],
            outputs: vec![Shielded {
                commitment: vec![0x50; 32],
                ..Shielded::default()
            }],
            proof: Some(Proof {
                system: system.as_bytes().to_vec(),
                data: vec![0x60; 192],
                public: vec![vec![0x61; 32], vec![0x62; 32]],
            }),
            fee: 1,
            expiry: 1000,
            ..Tx::default()
        }
    }

    fn strict() -> Verifier {
        Verifier::new(&Config::chain_default(), bind()).unwrap()
    }

    fn permissive_with_keys() -> Verifier {
        let mut cfg = Config::chain_default();
        cfg.strict_pq = false;
        cfg.verifying_keys.insert(Kind::TRANSFER.0, b"a key".to_vec());
        Verifier::new(&cfg, bind()).unwrap()
    }

    #[test]
    fn a_strict_chain_refuses_every_classical_system_by_name() {
        let mut v = strict();
        for system in ["groth16", "plonk", "bulletproofs", "something new"] {
            assert_eq!(
                v.verify_tx(&transfer(system)),
                Err(Error::StrictPqClassicalForbidden),
                "{system} on a strict-PQ chain"
            );
        }
    }

    /// The gate runs before anything else, so a classical proof is refused for
    /// what it IS even when everything else about it is in order.
    #[test]
    fn the_profile_gate_runs_before_the_key_lookup_and_the_length_check() {
        let mut v = strict();
        let mut tx = transfer("groth16");
        tx.proof.as_mut().unwrap().data = vec![7; GROTH16_PROOF_LEN];
        assert_eq!(v.verify_tx(&tx), Err(Error::StrictPqClassicalForbidden));
    }

    #[test]
    fn a_strict_chain_judges_a_stark_proof_and_refuses_a_bad_one() {
        let mut v = strict();
        // The corpus's proof: 192 bytes of one value, which is not a proof of
        // this system at all.
        assert_eq!(
            v.verify_tx(&transfer("stark")),
            Err(Error::StarkFailed(Box::new(Error::StarkInvalidProof)))
        );
    }

    #[test]
    fn a_transaction_with_no_proof_is_refused_here_too() {
        let mut v = strict();
        let mut tx = transfer("stark");
        tx.proof = None;
        assert_eq!(v.verify_tx(&tx), Err(Error::MissingProof));
    }

    /// A real bn254 key on a strict-PQ chain is refused where the key is
    /// loaded, not where it is used.
    #[test]
    fn a_strict_chain_refuses_to_hold_a_real_classical_key() {
        let mut cfg = Config::chain_default();
        cfg.verifying_keys.insert(Kind::TRANSFER.0, b"a key".to_vec());
        assert_eq!(
            Verifier::new(&cfg, bind()).err(),
            Some(Error::StrictPqRealVkForbidden)
        );
    }

    #[test]
    fn an_empty_key_is_not_a_key() {
        let mut cfg = Config::chain_default();
        cfg.verifying_keys.insert(Kind::TRANSFER.0, Vec::new());
        let v = Verifier::new(&cfg, bind()).expect("an empty key is no key at all");
        assert!(!v.keys_loaded());
    }

    #[test]
    fn a_chain_with_no_keys_refuses_the_classical_path_outright() {
        let mut cfg = Config::chain_default();
        cfg.strict_pq = false;
        let mut v = Verifier::new(&cfg, bind()).unwrap();
        assert_eq!(
            v.verify_tx(&transfer("groth16")),
            Err(Error::ProofVerificationDisabled)
        );
    }

    #[test]
    fn a_circuit_the_operator_did_not_key_is_refused_by_name() {
        let mut v = permissive_with_keys();
        let mut tx = transfer("groth16");
        tx.kind = Kind::SHIELD;
        tx.transparent_in.push(crate::txs::TransparentIn::default());
        assert_eq!(v.verify_tx(&tx), Err(Error::NoVerifyingKey(Kind::SHIELD.0)));
    }

    #[test]
    fn public_inputs_have_to_say_what_the_transaction_spends() {
        let mut v = permissive_with_keys();

        let mut none = transfer("groth16");
        none.proof.as_mut().unwrap().public.clear();
        assert_eq!(
            v.verify_tx(&none),
            Err(Error::PublicInputs("no public inputs provided"))
        );

        let mut wrong_chain = transfer("groth16");
        wrong_chain.proof.as_mut().unwrap().public[0] = vec![0xFF; 32];
        assert_eq!(
            v.verify_tx(&wrong_chain),
            Err(Error::PublicInputs("public input mismatch for chain binding"))
        );

        let mut bound = transfer("groth16");
        bound.proof.as_mut().unwrap().public = vec![bind().to_vec(), vec![0xFF; 32]];
        assert_eq!(
            v.verify_tx(&bound),
            Err(Error::PublicInputs("public input mismatch for nullifier"))
        );

        let mut short = transfer("groth16");
        short.proof.as_mut().unwrap().public = vec![bind().to_vec()];
        assert_eq!(
            v.verify_tx(&short),
            Err(Error::PublicInputs("missing public input for nullifier"))
        );

        let mut no_commitment = transfer("groth16");
        no_commitment.proof.as_mut().unwrap().public =
            vec![bind().to_vec(), vec![0x10; 32]];
        assert_eq!(
            v.verify_tx(&no_commitment),
            Err(Error::PublicInputs("missing public input for output commitment"))
        );
    }

    /// Everything the classical path can check, checked — and then a refusal
    /// where the reference computes a pairing. Named, because it is the one
    /// thing this port does not carry.
    #[test]
    fn the_classical_path_checks_everything_it_can_and_then_fails_closed() {
        let mut v = permissive_with_keys();
        let mut tx = transfer("groth16");
        tx.proof.as_mut().unwrap().public =
            vec![bind().to_vec(), vec![0x10; 32], vec![0x50; 32]];

        // Short of a proof's worth of bytes, the length says so.
        assert_eq!(v.verify_tx(&tx), Err(Error::Groth16ProofLength));

        tx.proof.as_mut().unwrap().data = vec![7; GROTH16_PROOF_LEN];
        assert_eq!(v.verify_tx(&tx), Err(Error::Groth16Unbound));
    }

    #[test]
    fn plonk_is_refused_after_it_decodes() {
        let mut v = permissive_with_keys();
        let mut tx = transfer("plonk");
        tx.proof.as_mut().unwrap().public =
            vec![bind().to_vec(), vec![0x10; 32], vec![0x50; 32]];
        assert_eq!(v.verify_tx(&tx), Err(Error::PlonkProofLength));
        tx.proof.as_mut().unwrap().data = vec![7; PLONK_PROOF_LEN];
        assert_eq!(v.verify_tx(&tx), Err(Error::PlonkIncomplete));
    }

    #[test]
    fn a_system_nobody_has_heard_of_is_refused_on_a_permissive_chain_too() {
        let mut v = permissive_with_keys();
        let mut tx = transfer("moon math");
        tx.proof.as_mut().unwrap().public =
            vec![bind().to_vec(), vec![0x10; 32], vec![0x50; 32]];
        assert_eq!(v.verify_tx(&tx), Err(Error::UnsupportedProofType));
    }

    /// The claim the cache key is there to make: the same PROOF and public
    /// inputs on a transaction spending different notes is a different key.
    #[test]
    fn the_cache_key_moves_when_what_is_spent_moves() {
        let a = transfer("groth16");
        let mut b = transfer("groth16");
        b.nullifiers[0] = vec![0x11; 32];
        assert_ne!(a.id(), b.id());
        assert_eq!(a.proof, b.proof);
    }

    #[test]
    fn a_second_ask_about_one_transaction_is_answered_from_the_cache() {
        let mut v = permissive_with_keys();
        let mut tx = transfer("groth16");
        tx.proof.as_mut().unwrap().data = vec![7; GROTH16_PROOF_LEN];
        tx.proof.as_mut().unwrap().public =
            vec![bind().to_vec(), vec![0x10; 32], vec![0x50; 32]];

        assert!(v.verify_tx(&tx).is_err());
        assert!(v.verify_tx(&tx).is_err());
        assert_eq!(v.stats(), (2, 1, 1));
        assert_eq!(v.cache_len(), 1);
    }

    /// The STARK path does not consult the cache: on the chain's own profile
    /// every verdict is reached fresh.
    #[test]
    fn the_strict_path_caches_nothing() {
        let mut v = strict();
        assert!(v.verify_tx(&transfer("stark")).is_err());
        assert_eq!(v.cache_len(), 0);
        assert_eq!(v.stats(), (0, 0, 0));
    }

    #[test]
    fn the_cache_is_bounded_and_drops_the_least_recently_used() {
        let mut c = Cache::new(2);
        c.add(ids::repeated(1), true);
        c.add(ids::repeated(2), true);
        assert_eq!(c.get(&ids::repeated(1)), Some(true)); // 1 is now the newest
        c.add(ids::repeated(3), true);
        assert_eq!(c.len(), 2);
        assert_eq!(c.get(&ids::repeated(2)), None);
        assert_eq!(c.get(&ids::repeated(1)), Some(true));
        assert_eq!(c.get(&ids::repeated(3)), Some(true));
    }

    #[test]
    fn a_block_proof_judges_every_transaction_under_it() {
        let mut v = strict();
        let txs = vec![transfer("stark"), transfer("stark")];
        assert!(v.verify_block_proof(&txs).is_err());
        assert_eq!(v.verify_block_proof(&[]), Ok(()));
    }
}
