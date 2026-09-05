// SPDX-License-Identifier: BSD-3-Clause-Eco

//! How a validator proves the key it will sign with is its own.
//!
//! A permissionless validator registers a BLS public key, and consensus later
//! aggregates signatures under it. Aggregation is what makes the proof
//! necessary: without one, anyone could register a key that is the difference
//! of an honest validator's key and a key they hold, and then produce
//! aggregate signatures the honest validator never took part in. The proof is
//! a signature, under a domain separate from every other signature the network
//! makes, over the public key itself.
//!
//! The domain tag is the wire: `BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_`,
//! the same one the Go node uses. A different tag would accept proofs the Go
//! node refuses and refuse ones it accepts, which is a fork.

use crate::zap;

/// Bytes in a compressed BLS public key.
pub const PUBLIC_KEY_LEN: usize = 48;
/// Bytes in a BLS signature.
pub const SIGNATURE_LEN: usize = 96;
/// Bytes the signer occupies inside a transaction: a tag and the two blobs.
pub const SIGNER_SIZE: usize = 1 + PUBLIC_KEY_LEN + SIGNATURE_LEN;

/// Bytes in an uncompressed BLS public key. The set commitment hashes this
/// form; a proof of possession signs the compressed one.
pub const PUBLIC_KEY_UNCOMPRESSED_LEN: usize = 96;

/// The domain a proof of possession is made under.
const DST_POP: &[u8] = b"BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_";

/// The domain every other signature is made under — Go's
/// `bls.CiphersuiteSignature`. A warp message is signed under this one, and a
/// second tag would be a second network.
const DST_SIGNATURE: &[u8] = b"BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_";

/// The 48-byte compressed key as the 96 uncompressed bytes a validator set
/// stores. Go: `bls.PublicKeyToUncompressedBytes`.
///
/// Bytes that are not a point are refused rather than padded: a validator
/// stored with a key nobody can verify under carries weight in every quorum
/// denominator with no way for anyone to vote toward it.
pub fn uncompress(compressed: &[u8; PUBLIC_KEY_LEN]) -> Result<Vec<u8>, Error> {
    let pk =
        blst::min_pk::PublicKey::key_validate(compressed).map_err(|_| Error::MalformedPublicKey)?;
    Ok(pk.serialize().to_vec())
}

/// The inverse. Go: `bls.PublicKeyFromValidUncompressedBytes`.
pub fn compress(uncompressed: &[u8]) -> Result<[u8; PUBLIC_KEY_LEN], Error> {
    if uncompressed.len() != PUBLIC_KEY_UNCOMPRESSED_LEN {
        return Err(Error::MalformedPublicKey);
    }
    let pk = blst::min_pk::PublicKey::deserialize(uncompressed)
        .map_err(|_| Error::MalformedPublicKey)?;
    Ok(pk.compress())
}

/// One key standing for many. Go: `bls.AggregatePublicKeys`.
///
/// An empty set has no aggregate: a signature verified against nothing would
/// be a signature by nobody that satisfied a quorum of nobody.
pub fn aggregate(keys: &[[u8; PUBLIC_KEY_LEN]]) -> Result<[u8; PUBLIC_KEY_LEN], Error> {
    if keys.is_empty() {
        return Err(Error::MalformedPublicKey);
    }
    let parsed: Result<Vec<_>, Error> = keys
        .iter()
        .map(|k| blst::min_pk::PublicKey::key_validate(k).map_err(|_| Error::MalformedPublicKey))
        .collect();
    let parsed = parsed?;
    let refs: Vec<&blst::min_pk::PublicKey> = parsed.iter().collect();
    let agg = blst::min_pk::AggregatePublicKey::aggregate(&refs, false)
        .map_err(|_| Error::MalformedPublicKey)?;
    Ok(agg.to_public_key().compress())
}

/// Check a signature over `message` by `public_key`, under the signature
/// domain. Go: `bls.Verify`.
pub fn verify_signature(
    public_key: &[u8; PUBLIC_KEY_LEN],
    signature: &[u8; SIGNATURE_LEN],
    message: &[u8],
) -> Result<(), Error> {
    let pk =
        blst::min_pk::PublicKey::key_validate(public_key).map_err(|_| Error::MalformedPublicKey)?;
    let sig =
        blst::min_pk::Signature::from_bytes(signature).map_err(|_| Error::MalformedSignature)?;
    match sig.verify(true, message, DST_SIGNATURE, &[], &pk, true) {
        blst::BLST_ERROR::BLST_SUCCESS => Ok(()),
        _ => Err(Error::InvalidSignature),
    }
}

/// Sign `message` under the signature domain — what a validator of a source
/// chain does before its signature is aggregated into a warp proof.
pub fn sign(secret: &blst::min_pk::SecretKey, message: &[u8]) -> [u8; SIGNATURE_LEN] {
    secret.sign(message, DST_SIGNATURE, &[]).compress()
}

/// Many signatures over one message as one. Go: `bls.AggregateSignatures`.
pub fn aggregate_signatures(sigs: &[[u8; SIGNATURE_LEN]]) -> Result<[u8; SIGNATURE_LEN], Error> {
    if sigs.is_empty() {
        return Err(Error::MalformedSignature);
    }
    let parsed: Result<Vec<_>, Error> = sigs
        .iter()
        .map(|s| blst::min_pk::Signature::from_bytes(s).map_err(|_| Error::MalformedSignature))
        .collect();
    let parsed = parsed?;
    let refs: Vec<&blst::min_pk::Signature> = parsed.iter().collect();
    let agg = blst::min_pk::AggregateSignature::aggregate(&refs, false)
        .map_err(|_| Error::MalformedSignature)?;
    Ok(agg.to_signature().compress())
}

/// The key a validator will sign with, or none.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Signer {
    /// A validator that signs nothing. Legal for the kinds that predate BLS
    /// and for chain validators, which do not aggregate.
    Empty,
    ProofOfPossession {
        public_key: [u8; PUBLIC_KEY_LEN],
        /// A signature over `public_key`, under the proof-of-possession domain.
        proof: [u8; SIGNATURE_LEN],
    },
}

/// Why a proof is not one.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    /// The bytes are not a point on the curve, or are the identity.
    MalformedPublicKey,
    MalformedSignature,
    /// Well-formed, and not a signature over this key.
    InvalidProofOfPossession,
    /// Well-formed, and not a signature over this message by this key.
    InvalidSignature,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::MalformedPublicKey => write!(f, "malformed public key"),
            Error::MalformedSignature => write!(f, "malformed signature"),
            Error::InvalidProofOfPossession => write!(f, "invalid proof of possession"),
            Error::InvalidSignature => write!(f, "invalid signature"),
        }
    }
}

impl std::error::Error for Error {}

impl Signer {
    /// Make the proof for a key you hold.
    ///
    /// This is what a node runs once, when it writes the transaction that puts
    /// it in the validator set.
    pub fn prove(secret: &blst::min_pk::SecretKey) -> Signer {
        let public_key = secret.sk_to_pk().compress();
        let proof = secret.sign(&public_key, DST_POP, &[]).compress();
        Signer::ProofOfPossession { public_key, proof }
    }

    /// The key, when there is one.
    pub fn public_key(&self) -> Option<[u8; PUBLIC_KEY_LEN]> {
        match self {
            Signer::Empty => None,
            Signer::ProofOfPossession { public_key, .. } => Some(*public_key),
        }
    }

    /// Check the proof.
    ///
    /// An empty signer has nothing to check and passes; that is not a hole,
    /// because a validator with no key is never aggregated into.
    pub fn verify(&self) -> Result<(), Error> {
        let (public_key, proof) = match self {
            Signer::Empty => return Ok(()),
            Signer::ProofOfPossession { public_key, proof } => (public_key, proof),
        };

        let pk = blst::min_pk::PublicKey::key_validate(public_key)
            .map_err(|_| Error::MalformedPublicKey)?;
        let sig =
            blst::min_pk::Signature::from_bytes(proof).map_err(|_| Error::MalformedSignature)?;

        // The message is the key itself: that is what makes the proof about
        // this key and not about anything else the holder ever signed.
        match sig.verify(true, public_key, DST_POP, &[], &pk, true) {
            blst::BLST_ERROR::BLST_SUCCESS => Ok(()),
            _ => Err(Error::InvalidProofOfPossession),
        }
    }

    pub(crate) fn write(&self, b: &mut zap::Builder, ob: &zap::ObjectBuilder, off: usize) {
        match self {
            Signer::Empty => b.set_u8(ob, off, 0),
            Signer::ProofOfPossession { public_key, proof } => {
                b.set_u8(ob, off, 1);
                b.set_bytes_fixed(ob, off + 1, public_key);
                b.set_bytes_fixed(ob, off + 1 + PUBLIC_KEY_LEN, proof);
            }
        }
    }

    pub(crate) fn read(o: zap::Object<'_>, off: usize) -> Signer {
        if o.u8(off) == 0 {
            return Signer::Empty;
        }
        let mut public_key = [0u8; PUBLIC_KEY_LEN];
        let mut proof = [0u8; SIGNATURE_LEN];
        let pk = o.bytes_fixed(off + 1, PUBLIC_KEY_LEN);
        public_key[..pk.len()].copy_from_slice(pk);
        let p = o.bytes_fixed(off + 1 + PUBLIC_KEY_LEN, SIGNATURE_LEN);
        proof[..p.len()].copy_from_slice(p);
        Signer::ProofOfPossession { public_key, proof }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn key(seed: &[u8]) -> blst::min_pk::SecretKey {
        let mut ikm = [0u8; 32];
        ikm[..seed.len().min(32)].copy_from_slice(&seed[..seed.len().min(32)]);
        blst::min_pk::SecretKey::key_gen(&ikm, &[]).unwrap()
    }

    fn pop(sk: &blst::min_pk::SecretKey) -> Signer {
        Signer::prove(sk)
    }

    #[test]
    fn a_proof_over_its_own_key_verifies() {
        assert_eq!(pop(&key(b"a validator")).verify(), Ok(()));
    }

    #[test]
    fn an_empty_signer_has_nothing_to_prove() {
        assert_eq!(Signer::Empty.verify(), Ok(()));
        assert_eq!(Signer::Empty.public_key(), None);
    }

    #[test]
    fn a_proof_made_by_a_different_key_is_refused() {
        // The rogue-key shape: a real proof, attached to someone else's key.
        let mine = key(b"mine");
        let theirs = key(b"theirs");
        let signer = Signer::ProofOfPossession {
            public_key: theirs.sk_to_pk().compress(),
            proof: mine
                .sign(&mine.sk_to_pk().compress(), DST_POP, &[])
                .compress(),
        };
        assert_eq!(signer.verify(), Err(Error::InvalidProofOfPossession));
    }

    #[test]
    fn a_key_survives_the_trip_through_its_uncompressed_form() {
        // The set commitment hashes 96 bytes and a proof signs 48; a chain that
        // confuses them produces a root that verifies against nothing.
        let sk = key(b"round trip");
        let compressed = sk.sk_to_pk().compress();
        let long = uncompress(&compressed).unwrap();
        assert_eq!(long.len(), PUBLIC_KEY_UNCOMPRESSED_LEN);
        assert_eq!(compress(&long), Ok(compressed));

        assert_eq!(uncompress(&[0u8; PUBLIC_KEY_LEN]), Err(Error::MalformedPublicKey));
        assert_eq!(compress(&[0u8; 95]), Err(Error::MalformedPublicKey));
        assert_eq!(
            compress(&[0xffu8; PUBLIC_KEY_UNCOMPRESSED_LEN]),
            Err(Error::MalformedPublicKey)
        );
    }

    #[test]
    fn many_signatures_over_one_message_verify_under_one_aggregate_key() {
        // This is the whole warp scheme: one pairing rather than one per signer.
        let secrets = [key(b"one"), key(b"two"), key(b"three")];
        let message = b"a message another chain signed";
        let keys: Vec<_> = secrets.iter().map(|s| s.sk_to_pk().compress()).collect();
        let sigs: Vec<_> = secrets.iter().map(|s| sign(s, message)).collect();

        let agg_key = aggregate(&keys).unwrap();
        let agg_sig = aggregate_signatures(&sigs).unwrap();
        assert_eq!(verify_signature(&agg_key, &agg_sig, message), Ok(()));

        // A signer who did not sign cannot be counted: the aggregate of four
        // keys does not verify a signature by three.
        let with_stranger = aggregate(&[keys[0], keys[1], keys[2], key(b"stranger").sk_to_pk().compress()])
            .unwrap();
        assert_eq!(
            verify_signature(&with_stranger, &agg_sig, message),
            Err(Error::InvalidSignature)
        );
        // Nor does it verify over a different message.
        assert_eq!(
            verify_signature(&agg_key, &agg_sig, b"something else"),
            Err(Error::InvalidSignature)
        );
    }

    #[test]
    fn nobody_is_not_a_signer() {
        // An empty aggregate would be a quorum satisfied by no one.
        assert_eq!(aggregate(&[]), Err(Error::MalformedPublicKey));
        assert_eq!(aggregate_signatures(&[]), Err(Error::MalformedSignature));
    }

    #[test]
    fn a_proof_of_possession_is_not_a_message_signature() {
        // The two domains are what keep one from being replayed as the other.
        let sk = key(b"a validator");
        let compressed = sk.sk_to_pk().compress();
        let Signer::ProofOfPossession { proof, .. } = pop(&sk) else {
            unreachable!("prove makes a proof")
        };
        assert_eq!(
            verify_signature(&compressed, &proof, &compressed),
            Err(Error::InvalidSignature)
        );
    }

    #[test]
    fn a_proof_made_under_another_domain_is_refused() {
        // The whole reason the tag exists: a signature the validator made for
        // some other purpose must not double as a proof of possession.
        let sk = key(b"a validator");
        let public_key = sk.sk_to_pk().compress();
        let signer = Signer::ProofOfPossession {
            public_key,
            proof: sk.sign(&public_key, DST_SIGNATURE, &[]).compress(),
        };
        assert_eq!(signer.verify(), Err(Error::InvalidProofOfPossession));
    }

    #[test]
    fn malformed_bytes_are_refused_rather_than_trusted() {
        let signer = Signer::ProofOfPossession {
            public_key: [0u8; PUBLIC_KEY_LEN],
            proof: [0u8; SIGNATURE_LEN],
        };
        assert_eq!(signer.verify(), Err(Error::MalformedPublicKey));

        let sk = key(b"a validator");
        let signer = Signer::ProofOfPossession {
            public_key: sk.sk_to_pk().compress(),
            proof: [0xffu8; SIGNATURE_LEN],
        };
        assert_eq!(signer.verify(), Err(Error::MalformedSignature));
    }

    #[test]
    fn a_signer_occupies_the_bytes_the_wire_reserves() {
        // 1 tag + 48 key + 96 signature. The offsets of everything after the
        // signer in a transaction depend on this number.
        assert_eq!(SIGNER_SIZE, 145);
    }

    #[test]
    fn a_signer_round_trips_through_the_wire() {
        for signer in [Signer::Empty, pop(&key(b"round trip"))] {
            let mut b = zap::Builder::new(zap::HEADER_SIZE + 256);
            let ob = b.start_object(SIGNER_SIZE);
            signer.write(&mut b, &ob, 0);
            b.finish_as_root(&ob);
            let bytes = b.finish();
            let msg = zap::Message::parse(&bytes).unwrap();
            assert_eq!(Signer::read(msg.root(), 0), signer);
        }
    }
}
