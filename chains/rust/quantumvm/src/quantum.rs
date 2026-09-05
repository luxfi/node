// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The per-validator ML-DSA identity signature, and the stamp that binds it to
//! a moment.
//!
//! This is FIPS 204 ML-DSA — the same `libluxcrypto` the Go node signs under,
//! reached through `lux-pq`. A second ML-DSA in the estate would be a second
//! set of keys nobody else can check.
//!
//! ## What the signature covers
//!
//! `message ‖ stamp ‖ big-endian u64 of the nanosecond the stamp was made`.
//!
//! The time is in the preimage because a verifier acts on it. It was once a
//! plain field beside the signature and nothing signed it, so the freshness
//! check ran against a number the holder could rewrite: an expired stamp was
//! revived by setting its timestamp to now, and the same edit forward produced
//! one that never expired. A field a verifier trusts has to be a field the
//! signature covers.
//!
//! ## What a stamp is
//!
//! `sha512(message ‖ key nonce ‖ big-endian u64 nanoseconds) ‖ 32 fresh random
//! bytes`. The hash binds the message to this validator's nonce and to the
//! moment; the random tail makes two stamps over one message at one nanosecond
//! distinguishable, so a captured stamp is evidence of one signing rather than
//! of a signer.
//!
//! ## Parameter sets
//!
//! 1 = ML-DSA-44, 2 = ML-DSA-65, 3 = ML-DSA-87. The number fixes every key and
//! signature width, so there is nothing else to size. A version that does not
//! exist is REFUSED rather than falling through to a default: an operator who
//! asked for something else would otherwise get a chain signing under a
//! parameter set nobody chose, and never hear about it.
//!
//! Only ML-DSA-65 can be signed under here, because ML-DSA-65 is the only
//! parameter set `libluxcrypto` exposes ([`ALGORITHM_MLDSA44`] and
//! [`ALGORITHM_MLDSA87`] have no `mldsa44_*`/`mldsa87_*` entry points). Asking
//! for one of those is an error naming that, not a silent downgrade to 65 —
//! the whole point of refusing an unknown version.

use std::time::{Duration, SystemTime, UNIX_EPOCH};

use lux_pq::{sign as mldsa, GenerateKeyPair, Signer as _, Verifier as _};
use sha2::{Digest, Sha512};

use crate::error::{Error, Result};

/// NIST level 2.
pub const ALGORITHM_MLDSA44: u32 = 1;
/// NIST level 3 — what Q-Chain validators hold.
pub const ALGORITHM_MLDSA65: u32 = 2;
/// NIST level 5.
pub const ALGORITHM_MLDSA87: u32 = 3;

/// Bytes of fresh randomness in a stamp's tail.
const NOISE: usize = 32;
/// Bytes of per-key nonce a stamp folds in.
pub const NONCE: usize = 32;

/// A validator's ML-DSA identity key.
///
/// This is NOT a Corona threshold share. The share is a piece of one combined
/// committee key and lives in the threshold protocol; this is one validator's
/// own key, and what it produces is attributable to that validator alone. The
/// RPC method that mints one is still spelled `generateCoronaKey`, because that
/// name is on the wire.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Key {
    /// The parameter set this key was minted under.
    pub version: u32,
    pub public: Vec<u8>,
    pub secret: Vec<u8>,
    /// What a stamp made with this key folds in, so two validators stamping one
    /// message at one nanosecond produce different stamps.
    pub nonce: [u8; NONCE],
}

/// A signature, the stamp it was made with, and the moment it was made.
///
/// There is one public key here, not two. Go carries a second copy under the
/// name `CoronaKey`, set from the same key at signing time and derived again on
/// the way off the wire; a second copy is a second thing to disagree.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Stamp {
    pub algorithm: u32,
    /// Unix NANOSECONDS. The wire carries this number and the signature covers
    /// it.
    pub stamped: i64,
    pub public_key: Vec<u8>,
    pub signature: Vec<u8>,
    /// The quantum stamp: `sha512(...) ‖ noise`.
    pub quantum_stamp: Vec<u8>,
}

impl Stamp {
    /// The key this signature was made under, under the name the RPC uses.
    /// Derived, never carried twice.
    pub fn corona_key(&self) -> &[u8] {
        &self.public_key
    }
}

/// The chain's signer: one parameter set, one validity window.
#[derive(Clone, Debug)]
pub struct Quantum {
    algorithm: u32,
    window: Duration,
}

impl Quantum {
    /// A signer at `algorithm`, whose stamps are good for `window`.
    pub fn new(algorithm: u32, window: Duration) -> Result<Quantum> {
        match algorithm {
            ALGORITHM_MLDSA65 => {}
            ALGORITHM_MLDSA44 | ALGORITHM_MLDSA87 => {
                return Err(Error::Algorithm(format!(
                    "ML-DSA parameter set {algorithm} is not available in this build: libluxcrypto exposes ML-DSA-65 only"
                )))
            }
            other => {
                return Err(Error::Algorithm(format!(
                    "unsupported quantum algorithm: {other} (1=ML-DSA-44, 2=ML-DSA-65, 3=ML-DSA-87)"
                )))
            }
        }
        Ok(Quantum { algorithm, window })
    }

    /// The parameter set this signer signs and verifies under.
    pub fn algorithm(&self) -> u32 {
        self.algorithm
    }

    /// How long a stamp of this signer's stays good, in either direction.
    pub fn window(&self) -> Duration {
        self.window
    }

    /// Bytes in a public key of this parameter set.
    pub fn public_key_size(&self) -> usize {
        mldsa::public_key_size()
    }

    /// Bytes in a signature of this parameter set.
    pub fn signature_size(&self) -> usize {
        mldsa::signature_size()
    }

    /// Bytes in a secret key of this parameter set.
    pub fn secret_key_size(&self) -> usize {
        mldsa::secret_key_size()
    }

    /// Mint a fresh validator identity key.
    pub fn generate(&self) -> Result<Key> {
        let pair = mldsa::KeyPair::generate().map_err(|e| Error::Signing(e.to_string()))?;
        let mut nonce = [0u8; NONCE];
        getrandom::getrandom(&mut nonce).map_err(|e| Error::Signing(e.to_string()))?;
        Ok(Key {
            version: self.algorithm,
            public: pair.public_key().as_bytes().to_vec(),
            secret: pair.secret_key().as_bytes().to_vec(),
            nonce,
        })
    }

    /// Sign `message` under `key`, stamping it with the moment it was made.
    ///
    /// One reading of the clock: the stamp is derived from it, the signature
    /// covers it, and it is what the signature reports. Two readings would be
    /// two different times for one signature.
    pub fn sign(&self, message: &[u8], key: &Key) -> Result<Stamp> {
        // The width is fixed by the parameter set, so a key of another width is
        // refused here rather than handed to the library to have an opinion
        // about.
        if key.secret.len() != self.secret_key_size() {
            return Err(Error::Signing(format!(
                "secret key is {} bytes, ML-DSA-{} takes {}",
                key.secret.len(),
                65,
                self.secret_key_size()
            )));
        }
        let stamped = now_nanos();
        let stamp = quantum_stamp(message, &key.nonce, stamped)?;
        let secret = mldsa::SecretKey::from_bytes(key.secret.clone());
        let signature = secret
            .sign(&signed_data(message, &stamp, stamped))
            .map_err(|e| Error::Signing(e.to_string()))?;

        Ok(Stamp {
            algorithm: self.algorithm,
            stamped,
            public_key: key.public.clone(),
            signature: signature.as_bytes().to_vec(),
            quantum_stamp: stamp,
        })
    }

    /// Check a signature against the message it claims to cover.
    ///
    /// In the order the rules depend on each other: the parameter set (a
    /// signature made under another one would be verified against a key of the
    /// wrong width), then freshness, then the key, then the signature itself.
    pub fn verify(&self, message: &[u8], sig: Option<&Stamp>) -> Result<()> {
        let sig = match sig {
            Some(s) => s,
            None => return Err(Error::NoStamp),
        };
        if sig.algorithm != self.algorithm {
            return Err(Error::Algorithm(format!(
                "signature is ML-DSA parameter set {}, this signer runs {}",
                sig.algorithm, self.algorithm
            )));
        }

        // Fresh in BOTH directions. Only one side was once checked, and an age
        // goes NEGATIVE for a future date, so any timestamp ahead of now
        // compared as arbitrarily fresh and never expired at all.
        let window = self.window.as_nanos() as i64;
        let age = now_nanos().saturating_sub(sig.stamped);
        if age > window || age < -window {
            return Err(Error::StampExpired {
                age_nanos: age,
                window_nanos: window,
            });
        }

        // The key travels with the signature, so it is whatever the sender
        // chose. Refuse a width that is not this parameter set's.
        if sig.public_key.len() != self.public_key_size() {
            return Err(Error::Algorithm(format!(
                "public key is {} bytes, ML-DSA-65 takes {}",
                sig.public_key.len(),
                self.public_key_size()
            )));
        }
        if sig.signature.len() != self.signature_size() {
            return Err(Error::Algorithm(format!(
                "signature is {} bytes, ML-DSA-65 takes {}",
                sig.signature.len(),
                self.signature_size()
            )));
        }

        let public = mldsa::PublicKey::from_bytes(sig.public_key.clone());
        let ok = public.verify(
            &signed_data(message, &sig.quantum_stamp, sig.stamped),
            &lux_pq::Signature::from_bytes(sig.signature.clone()),
        );
        if ok {
            Ok(())
        } else {
            Err(Error::SignatureRefused)
        }
    }

    /// Verify a batch. The verdict is the AND of its members: a batch that
    /// passed while holding one bad signature would let a forged transaction
    /// into a block on the strength of the honest ones beside it.
    ///
    /// The pairing is by construction — a message and the signature over it
    /// arrive together — so there is no way to hand this two lists of different
    /// lengths and have it verify the wrong pairs.
    ///
    /// The work is spread across threads. There is no hardware batch path here:
    /// the accelerator's ML-DSA batch kernel is reachable from the Go runtime
    /// only, so this runtime always takes the CPU path. `gpu_batch_threshold`
    /// in the config is the size at which THAT runtime hands a batch over; it
    /// is part of the chain's configuration surface, and this runtime reports
    /// it without reading it.
    pub fn verify_all(&self, batch: &[(&[u8], Option<&Stamp>)]) -> Result<()> {
        if batch.is_empty() {
            return Ok(());
        }
        if batch.len() == 1 {
            return self.verify(batch[0].0, batch[0].1);
        }

        let threads = std::thread::available_parallelism()
            .map(|n| n.get())
            .unwrap_or(1)
            .min(batch.len());
        let chunk = batch.len().div_ceil(threads);

        std::thread::scope(|scope| {
            let mut handles = Vec::with_capacity(threads);
            for part in batch.chunks(chunk) {
                handles.push(scope.spawn(move || {
                    for (i, (message, sig)) in part.iter().enumerate() {
                        self.verify(message, *sig).map_err(|e| (i, e))?;
                    }
                    Ok::<(), (usize, Error)>(())
                }));
            }
            for h in handles {
                match h.join() {
                    Ok(Ok(())) => {}
                    Ok(Err((_, e))) => return Err(e),
                    // A panicking verifier is a verifier that did not verify.
                    Err(_) => return Err(Error::SignatureRefused),
                }
            }
            Ok(())
        })
    }
}

/// What the signature covers: the message, the stamp, and the TIME the stamp
/// was made. Big-endian, because that is the byte order the Go chain writes and
/// a signature is only checkable if both sides fold the same bytes.
pub fn signed_data(message: &[u8], stamp: &[u8], stamped: i64) -> Vec<u8> {
    let mut out = Vec::with_capacity(message.len() + stamp.len() + 8);
    out.extend_from_slice(message);
    out.extend_from_slice(stamp);
    out.extend_from_slice(&(stamped as u64).to_be_bytes());
    out
}

/// The stamp for one message, one key and one moment.
pub fn quantum_stamp(message: &[u8], nonce: &[u8], stamped: i64) -> Result<Vec<u8>> {
    let mut h = Sha512::new();
    h.update(message);
    h.update(nonce);
    h.update((stamped as u64).to_be_bytes());
    let digest = h.finalize();

    let mut noise = [0u8; NOISE];
    getrandom::getrandom(&mut noise).map_err(|e| Error::Signing(e.to_string()))?;

    let mut stamp = Vec::with_capacity(digest.len() + NOISE);
    stamp.extend_from_slice(&digest);
    stamp.extend_from_slice(&noise);
    Ok(stamp)
}

/// Nanoseconds since the epoch, as the wire carries them.
pub fn now_nanos() -> i64 {
    match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(d) => d.as_nanos() as i64,
        // A clock before the epoch is a clock, and the sign is the answer.
        Err(e) => -(e.duration().as_nanos() as i64),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn signer() -> Quantum {
        Quantum::new(ALGORITHM_MLDSA65, Duration::from_secs(60)).unwrap()
    }

    // Go: TestSignParsesStoredSecretOnce — signing twice from a key that came
    // back as bytes works both times. (There is nothing to parse here: this
    // backend takes the secret as bytes, so the Go cache the test pins does not
    // exist. What has to hold is that the second signature verifies.)
    #[test]
    fn a_stored_key_signs_more_than_once() {
        let qs = signer();
        let key = qs.generate().unwrap();
        let stored = Key {
            version: key.version,
            public: key.public.clone(),
            secret: key.secret.clone(),
            nonce: key.nonce,
        };
        let msg = b"round digest";
        for _ in 0..3 {
            let sig = qs.sign(msg, &stored).unwrap();
            qs.verify(msg, Some(&sig)).unwrap();
        }
    }

    // Go: TestSignRejectsUnparseableSecret — a key that cannot be a key fails
    // closed, every time, rather than signing with whatever is in the buffer.
    #[test]
    fn an_unusable_secret_fails_closed_every_time() {
        let qs = signer();
        let key = Key {
            version: ALGORITHM_MLDSA65,
            public: vec![0; qs.public_key_size()],
            secret: b"too short to be a key".to_vec(),
            nonce: [0; NONCE],
        };
        for _ in 0..2 {
            assert!(qs.sign(b"round digest", &key).is_err());
        }
    }

    // Go: TestVerifyRefusesAnExpiredStamp. The window is the whole reason a
    // stamp exists — without the check, a signature captured once is replayable
    // for as long as the chain runs.
    #[test]
    fn a_stamp_outside_its_window_is_refused() {
        let qs = Quantum::new(ALGORITHM_MLDSA65, Duration::from_millis(50)).unwrap();
        let key = qs.generate().unwrap();
        let msg = b"round digest";
        let sig = qs.sign(msg, &key).unwrap();
        qs.verify(msg, Some(&sig)).unwrap();

        std::thread::sleep(Duration::from_millis(120));
        assert!(matches!(
            qs.verify(msg, Some(&sig)),
            Err(Error::StampExpired { .. })
        ));
    }

    // The other direction, which a one-sided check misses: a stamp from the
    // future is not "arbitrarily fresh".
    #[test]
    fn a_stamp_from_the_future_is_refused_too() {
        let qs = Quantum::new(ALGORITHM_MLDSA65, Duration::from_secs(1)).unwrap();
        let key = qs.generate().unwrap();
        let mut sig = qs.sign(b"digest", &key).unwrap();
        sig.stamped += 10_000_000_000; // ten seconds ahead
        assert!(matches!(
            qs.verify(b"digest", Some(&sig)),
            Err(Error::StampExpired { .. })
        ));
    }

    // Go: TestVerifyRefusesAnotherAlgorithm. A signature made under one
    // parameter set must not be accepted by a signer running another — the key
    // and signature widths differ, so accepting one means verifying against a
    // truncated key.
    #[test]
    fn a_signature_claiming_another_parameter_set_is_refused() {
        let qs = signer();
        let key = qs.generate().unwrap();
        let mut sig = qs.sign(b"digest", &key).unwrap();
        sig.algorithm = ALGORITHM_MLDSA44;
        assert!(matches!(
            qs.verify(b"digest", Some(&sig)),
            Err(Error::Algorithm(_))
        ));
    }

    // A parameter set this build cannot sign under is an error that says so —
    // not a silent downgrade to the one it can.
    #[test]
    fn an_unavailable_parameter_set_says_so() {
        for v in [ALGORITHM_MLDSA44, ALGORITHM_MLDSA87] {
            let err = Quantum::new(v, Duration::from_secs(1)).unwrap_err();
            match err {
                Error::Algorithm(why) => assert!(why.contains("ML-DSA-65"), "{why}"),
                other => panic!("{other}"),
            }
        }
        assert!(matches!(
            Quantum::new(9, Duration::from_secs(1)),
            Err(Error::Algorithm(_))
        ));
    }

    // Go: TestVerifyRefusesAlteredMessageAndNilSignature.
    #[test]
    fn other_bytes_and_no_signature_are_both_refused() {
        let qs = signer();
        let key = qs.generate().unwrap();
        let sig = qs.sign(b"the real message", &key).unwrap();
        assert!(matches!(
            qs.verify(b"a different message", Some(&sig)),
            Err(Error::SignatureRefused)
        ));
        assert!(matches!(
            qs.verify(b"the real message", None),
            Err(Error::NoStamp)
        ));
    }

    // Go: TestVerifyRefusesAnUnparseablePublicKey. The key travels with the
    // signature, so it is attacker-controlled and must fail closed.
    #[test]
    fn a_public_key_that_is_not_one_is_refused() {
        let qs = signer();
        let key = qs.generate().unwrap();
        let mut sig = qs.sign(b"digest", &key).unwrap();
        sig.public_key = b"not a key".to_vec();
        assert!(qs.verify(b"digest", Some(&sig)).is_err());
    }

    // The stamp is covered by the signature, so rewriting it does not make the
    // signature check out — this is what stops a stamp being lifted onto
    // another signature.
    #[test]
    fn the_stamp_is_covered_by_the_signature() {
        let qs = signer();
        let key = qs.generate().unwrap();
        let mut sig = qs.sign(b"digest", &key).unwrap();
        sig.quantum_stamp[0] ^= 0xFF;
        assert!(matches!(
            qs.verify(b"digest", Some(&sig)),
            Err(Error::SignatureRefused)
        ));
    }

    // And so is the time it was made, which is what makes the freshness check
    // mean anything: an expired stamp cannot be revived by editing its clock.
    #[test]
    fn the_stamp_time_is_covered_by_the_signature() {
        let qs = Quantum::new(ALGORITHM_MLDSA65, Duration::from_millis(50)).unwrap();
        let key = qs.generate().unwrap();
        let mut sig = qs.sign(b"digest", &key).unwrap();
        std::thread::sleep(Duration::from_millis(120));
        // Rewriting the clock gets the signature past the window and straight
        // into a failed signature check, because the window was signed.
        sig.stamped = now_nanos();
        assert!(matches!(
            qs.verify(b"digest", Some(&sig)),
            Err(Error::SignatureRefused)
        ));
    }

    // Go: TestParallelVerifyFailsOnOneBadSignature.
    #[test]
    fn one_forged_signature_fails_the_whole_batch() {
        let qs = signer();
        let key = qs.generate().unwrap();
        let msgs: Vec<Vec<u8>> = (0..12u8).map(|i| vec![i, b'm', b's', b'g']).collect();
        let mut sigs: Vec<Stamp> = msgs.iter().map(|m| qs.sign(m, &key).unwrap()).collect();

        let batch: Vec<(&[u8], Option<&Stamp>)> = msgs
            .iter()
            .zip(sigs.iter())
            .map(|(m, s)| (m.as_slice(), Some(s)))
            .collect();
        qs.verify_all(&batch).unwrap();
        drop(batch);

        sigs[6].signature[0] ^= 0xFF;
        let batch: Vec<(&[u8], Option<&Stamp>)> = msgs
            .iter()
            .zip(sigs.iter())
            .map(|(m, s)| (m.as_slice(), Some(s)))
            .collect();
        assert!(qs.verify_all(&batch).is_err());
    }

    // Go: TestParallelVerifyRefusesMismatchedInputs — an empty batch is a
    // no-op, and a message with no signature beside it is not a thing to
    // verify.
    #[test]
    fn an_empty_batch_is_a_no_op_and_a_missing_signature_is_not() {
        let qs = signer();
        qs.verify_all(&[]).unwrap();
        assert!(matches!(
            qs.verify_all(&[(b"one".as_slice(), None)]),
            Err(Error::NoStamp)
        ));
    }

    // Go: TestSignerReportsItsWidths — the caller sizes buffers from these, so
    // they come from the parameter set rather than a configured guess.
    #[test]
    fn the_signer_reports_the_widths_it_actually_produces() {
        let qs = signer();
        let key = qs.generate().unwrap();
        assert_eq!(key.public.len(), qs.public_key_size());
        assert_eq!(key.secret.len(), qs.secret_key_size());
        let sig = qs.sign(b"digest", &key).unwrap();
        assert_eq!(sig.signature.len(), qs.signature_size());

        // A second signer named the same way verifies what the first produced.
        let peer = signer();
        peer.verify(b"digest", Some(&sig)).unwrap();
    }

    // ML-DSA-65 is FIPS 204 level 3: 1952-byte key, 3309-byte signature. These
    // are the widths the Go chain writes, so a wire built here fits a Go
    // reader's expectations exactly.
    #[test]
    fn the_widths_are_the_fips_204_level_3_widths() {
        let qs = signer();
        assert_eq!(qs.public_key_size(), 1952);
        assert_eq!(qs.signature_size(), 3309);
    }

    #[test]
    fn what_the_signature_covers_is_message_stamp_and_time() {
        let got = signed_data(b"ab", b"cd", 1);
        assert_eq!(&got[..2], b"ab");
        assert_eq!(&got[2..4], b"cd");
        assert_eq!(&got[4..], &1u64.to_be_bytes());
    }

    #[test]
    fn a_stamp_is_a_sha512_and_thirty_two_bytes_of_noise() {
        let a = quantum_stamp(b"m", &[7; NONCE], 5).unwrap();
        let b = quantum_stamp(b"m", &[7; NONCE], 5).unwrap();
        assert_eq!(a.len(), 64 + NOISE);
        assert_eq!(
            a[..64],
            b[..64],
            "the hash half is determined by its inputs"
        );
        assert_ne!(a[64..], b[64..], "the noise half is fresh each time");
    }
}
