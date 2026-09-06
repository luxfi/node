// SPDX-License-Identifier: BSD-3-Clause-Eco

//! A message another chain signed, and the proof enough of it did.
//!
//! Ported from Go `vms/platformvm/warp` (`unsigned_message.go`, `message.go`,
//! `wire.go`, `signature.go`, `validator.go`).
//!
//! A warp message is how one chain tells another something it will act on. The
//! proof is one aggregated BLS signature plus a bit vector saying which
//! validators of the source chain contributed, so checking it costs one pairing
//! rather than one per signer — which is the whole reason the scheme exists.
//!
//! **Two things here are load-bearing and both are about weight, not
//! signatures.**
//!
//! The bit vector is a big-endian big integer, and a vector with a leading zero
//! byte is refused. Two byte strings denoting the same set would be two
//! messages with one meaning, and the message id is a hash of the bytes.
//!
//! The total weight includes validators with **no key**, while only keyed ones
//! can sign. That is deliberate: a keyless validator's stake still counts
//! toward what a quorum has to beat, so a chain cannot cheapen its own quorum
//! by registering validators that cannot vote.
//!
//! What is here is the message wire and the BitSet (aggregated BLS) signature.
//! The post-quantum Corona signature and the teleport payloads are separate
//! schemes and are not ported; their wire kinds parse to a refusal by name
//! rather than to a signature that verifies trivially.

use std::collections::BTreeMap;

use crate::gas::Wide;
use crate::ids::{hash256, Id, NodeId};
use crate::signer::{self, PUBLIC_KEY_LEN, SIGNATURE_LEN};
use crate::warp_zap as w;

// The offsets are in `chains/schema/warp.zap`, and `warp_zap` is what came
// out of it.

/// The wire kinds a signature can be. Only the first is a scheme this port
/// implements; the rest are named so a refusal can say which one it met.
const KIND_BITSET: u8 = 0x00;

/// Why a warp message is not one, or is not proved.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// The bytes are not a message of this shape.
    Malformed(&'static str),
    /// A signature scheme this port does not implement.
    UnknownSignature(u8),
    /// A bit vector with a leading zero byte: a second encoding of one set.
    NotCanonical,
    /// The vector names a validator the set does not have.
    UnknownValidator {
        named: usize,
        set: usize,
    },
    /// The signers do not carry enough of the source chain's weight.
    InsufficientWeight {
        signed: u64,
        total: u64,
    },
    /// The message was signed for a different network.
    WrongNetwork {
        want: u32,
        got: u32,
    },
    /// The keys do not aggregate, or the aggregate does not verify.
    BadSignature(signer::Error),
    Overflow,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Malformed(why) => write!(f, "warp: {why}"),
            Error::UnknownSignature(k) => {
                write!(f, "warp: signature kind {k} is a scheme this port does not implement")
            }
            Error::NotCanonical => write!(f, "warp: the bit vector is not canonical"),
            Error::UnknownValidator { named, set } => {
                write!(f, "warp: the signature names validator {named} of {set}")
            }
            Error::InsufficientWeight { signed, total } => {
                write!(f, "warp: {signed} of {total} weight signed")
            }
            Error::WrongNetwork { want, got } => {
                write!(f, "warp: the message was signed for network {got}, not {want}")
            }
            Error::BadSignature(e) => write!(f, "warp: {e}"),
            Error::Overflow => write!(f, "warp: the weight overflows"),
        }
    }
}

impl std::error::Error for Error {}

/// Go: `warp.UnsignedMessage`. A 44-byte object with no kind of its own,
/// because it is only ever read in a context that already knows what it is.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Unsigned {
    pub network_id: u32,
    pub source_chain_id: Id,
    pub payload: Vec<u8>,
    /// The bytes this message *is*. Bound by [`Unsigned::build`] or
    /// [`Unsigned::parse`]; nothing is re-encoded in order to be hashed.
    pub bytes: Vec<u8>,
}

impl Unsigned {
    pub fn build(network_id: u32, source_chain_id: Id, payload: &[u8]) -> Unsigned {
        Unsigned {
            network_id,
            source_chain_id,
            payload: payload.to_vec(),
            bytes: w::new_unsigned(&w::UnsignedInput {
                network_id,
                source: &source_chain_id,
                payload,
            }),
        }
    }

    pub fn parse(raw: &[u8]) -> Result<Unsigned, Error> {
        let v = w::Unsigned::wrap(raw)
            .map_err(|_| Error::Malformed("the unsigned message is not a zap message"))?;
        Ok(Unsigned {
            network_id: v.network_id(),
            source_chain_id: *v.source(),
            payload: v.payload().to_vec(),
            bytes: raw.to_vec(),
        })
    }

    /// The name of this message: the hash of the bytes that travelled.
    pub fn id(&self) -> Id {
        hash256(&self.bytes)
    }
}

/// Go: `warp.BitSetSignature`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BitSet {
    /// A big-endian big integer whose i'th bit says validator i signed.
    pub signers: Vec<u8>,
    pub signature: [u8; SIGNATURE_LEN],
}

impl BitSet {
    /// How many validators contributed. A vector with unnecessary leading zero
    /// bytes is refused rather than counted.
    pub fn num_signers(&self) -> Result<usize, Error> {
        if !canonical(&self.signers) {
            return Err(Error::NotCanonical);
        }
        Ok(self.signers.iter().map(|b| b.count_ones() as usize).sum())
    }
}

/// Go: `warp.Message`. The unsigned buffer and the signature buffer, as two
/// opaque byte fields — the container needs no kind, because the signature it
/// carries is dispatched by its own.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Message {
    pub unsigned: Unsigned,
    pub signature: BitSet,
    pub bytes: Vec<u8>,
}

impl Message {
    pub fn build(unsigned: &Unsigned, signature: &BitSet) -> Message {
        let sig_bytes = w::new_bit_set_signature(&w::BitSetSignatureInput {
            kind: KIND_BITSET,
            signature: &signature.signature,
            signers: &signature.signers,
        });
        Message {
            unsigned: unsigned.clone(),
            signature: signature.clone(),
            bytes: w::new_message(&w::MessageInput {
                unsigned: &unsigned.bytes,
                signature: &sig_bytes,
            }),
        }
    }

    pub fn parse(raw: &[u8]) -> Result<Message, Error> {
        let v = w::Message::wrap(raw)
            .map_err(|_| Error::Malformed("the message is not a zap message"))?;
        let unsigned = Unsigned::parse(v.unsigned())?;

        let sig = w::BitSetSignature::wrap(v.signature())
            .map_err(|_| Error::Malformed("the signature is not a zap message"))?;
        let kind = sig.kind();
        if kind != KIND_BITSET {
            return Err(Error::UnknownSignature(kind));
        }

        Ok(Message {
            unsigned,
            signature: BitSet {
                signers: sig.signers().to_vec(),
                signature: *sig.signature(),
            },
            bytes: raw.to_vec(),
        })
    }
}

/// One validator as a warp proof names it: by its **key**, not by its node. Two
/// nodes sharing a key are one signer with the sum of their weights, because
/// one aggregate cannot distinguish them.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Validator {
    pub public_key: [u8; PUBLIC_KEY_LEN],
    pub weight: u64,
    pub node_ids: Vec<NodeId>,
}

/// The source chain's set, in the canonical order a bit vector indexes.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Canonical {
    /// Ordered by uncompressed public key, ascending.
    pub validators: Vec<Validator>,
    /// Includes validators with no key: their stake still counts toward what a
    /// quorum has to beat.
    pub total_weight: u64,
}

/// Go: `warp.FlattenValidatorSet`. Merges duplicate keys, sums the total, and
/// orders by the uncompressed key.
///
/// Takes the set keyed by node, each entry holding the **uncompressed** key
/// (empty for a validator that has none) and the weight.
pub fn flatten(set: &BTreeMap<NodeId, (Vec<u8>, u64)>) -> Result<Canonical, Error> {
    let mut out = Canonical::default();
    let mut by_key: BTreeMap<Vec<u8>, Validator> = BTreeMap::new();

    for (node_id, (key, weight)) in set {
        out.total_weight = out.total_weight.checked_add(*weight).ok_or(Error::Overflow)?;

        // A validator with no key still counts toward the total: its stake is
        // part of what a quorum has to beat, even though it cannot sign.
        if key.len() != signer::PUBLIC_KEY_UNCOMPRESSED_LEN {
            continue;
        }
        // A key that is not a key is not a signer either; the reference skips
        // it rather than refusing the whole set.
        let Ok(compressed) = signer::compress(key) else {
            continue;
        };

        match by_key.get_mut(key) {
            None => {
                by_key.insert(
                    key.clone(),
                    Validator {
                        public_key: compressed,
                        weight: *weight,
                        node_ids: vec![*node_id],
                    },
                );
            }
            Some(existing) => {
                existing.weight = existing.weight.checked_add(*weight).ok_or(Error::Overflow)?;
                existing.node_ids.push(*node_id);
            }
        }
    }

    out.validators = by_key.into_values().collect();
    Ok(out)
}

/// Go: `warp.FilterValidators`. The validators whose bit is set. An index past
/// the end of the set is a refusal — a signature cannot name a validator that
/// is not there.
pub fn filter<'a>(
    signers: &[u8],
    validators: &'a [Validator],
) -> Result<Vec<&'a Validator>, Error> {
    if !canonical(signers) {
        return Err(Error::NotCanonical);
    }
    let named = bit_len(signers);
    if named > validators.len() {
        return Err(Error::UnknownValidator {
            named: named - 1,
            set: validators.len(),
        });
    }
    Ok(validators
        .iter()
        .enumerate()
        .filter(|(i, _)| bit_set(signers, *i))
        .map(|(_, v)| v)
        .collect())
}

pub fn sum_weight(validators: &[&Validator]) -> Result<u64, Error> {
    let mut weight: u64 = 0;
    for v in validators {
        weight = weight.checked_add(v.weight).ok_or(Error::Overflow)?;
    }
    Ok(weight)
}

/// Go: `warp.VerifyWeight`. Satisfied iff `signed ≥ total · num / den`,
/// computed at full width so nothing rounds in the attacker's favour.
pub fn verify_weight(signed: u64, total: u64, quorum_num: u64, quorum_den: u64) -> Result<(), Error> {
    let mut scaled_total = Wide::from_u64(total);
    scaled_total.mul_u64(quorum_num);
    let mut scaled_signed = Wide::from_u64(signed);
    scaled_signed.mul_u64(quorum_den);
    if scaled_total.cmp(&scaled_signed) == std::cmp::Ordering::Greater {
        return Err(Error::InsufficientWeight { signed, total });
    }
    Ok(())
}

/// Go: `BitSetSignature.Verify`. The signature must be by at least `num/den` of
/// the source chain's weight, at the height the caller resolved the set at.
pub fn verify(
    signature: &BitSet,
    unsigned: &Unsigned,
    network_id: u32,
    validators: &Canonical,
    quorum_num: u64,
    quorum_den: u64,
) -> Result<(), Error> {
    if unsigned.network_id != network_id {
        return Err(Error::WrongNetwork {
            want: network_id,
            got: unsigned.network_id,
        });
    }

    let signers = filter(&signature.signers, &validators.validators)?;
    let weight = sum_weight(&signers)?;
    verify_weight(weight, validators.total_weight, quorum_num, quorum_den)?;

    let keys: Vec<[u8; PUBLIC_KEY_LEN]> = signers.iter().map(|v| v.public_key).collect();
    let aggregate = signer::aggregate(&keys).map_err(Error::BadSignature)?;
    signer::verify_signature(&aggregate, &signature.signature, &unsigned.bytes)
        .map_err(Error::BadSignature)
}

/// The bit vector is a big-endian big integer. Bit `i` is bit `i % 8` of the
/// byte `n - 1 - i / 8`, which is what Go's `big.Int.Bit` does over
/// `big.Int.Bytes`.
fn bit_set(bits: &[u8], i: usize) -> bool {
    let from_end = i / 8;
    if from_end >= bits.len() {
        return false;
    }
    (bits[bits.len() - 1 - from_end] >> (i % 8)) & 1 == 1
}

/// One past the highest set bit — Go's `big.Int.BitLen`.
fn bit_len(bits: &[u8]) -> usize {
    for (i, byte) in bits.iter().enumerate() {
        if *byte == 0 {
            continue;
        }
        let top = 8 - byte.leading_zeros() as usize;
        return (bits.len() - 1 - i) * 8 + top;
    }
    0
}

/// A vector that denotes the same set with a shorter encoding is not this
/// vector. Go asserts `len(Bits.Bytes()) == len(Signers)`, and `Bytes()` drops
/// leading zeros, so this is that check.
fn canonical(bits: &[u8]) -> bool {
    bits.is_empty() || bits[0] != 0
}

#[cfg(test)]
mod tests {
    use super::*;

    fn key(seed: &[u8]) -> blst::min_pk::SecretKey {
        let mut ikm = [0u8; 32];
        ikm[..seed.len().min(32)].copy_from_slice(&seed[..seed.len().min(32)]);
        blst::min_pk::SecretKey::key_gen(&ikm, &[]).unwrap()
    }

    /// A set of `n` keyed validators of equal weight, plus what signs for them.
    fn set(n: usize, weight: u64) -> (Canonical, Vec<blst::min_pk::SecretKey>) {
        let mut entries = BTreeMap::new();
        let mut secrets = Vec::new();
        for i in 0..n {
            let sk = key(format!("validator {i}").as_bytes());
            let long = signer::uncompress(&sk.sk_to_pk().compress()).unwrap();
            entries.insert(NodeId([i as u8; 20]), (long, weight));
            secrets.push(sk);
        }
        let canonical = flatten(&entries).unwrap();
        // The order the bit vector indexes is the key order, not the node
        // order, so the secrets have to be put in the same order.
        let mut ordered = Vec::new();
        for v in &canonical.validators {
            let sk = secrets
                .iter()
                .find(|s| s.sk_to_pk().compress() == v.public_key)
                .unwrap();
            ordered.push(sk.clone());
        }
        (canonical, ordered)
    }

    /// A bit vector naming the validators at `indices`, big-endian, canonical.
    fn bits(indices: &[usize], width: usize) -> Vec<u8> {
        let mut out = vec![0u8; width];
        for i in indices {
            let from_end = i / 8;
            let at = out.len() - 1 - from_end;
            out[at] |= 1 << (i % 8);
        }
        out
    }

    fn sign_by(
        secrets: &[blst::min_pk::SecretKey],
        indices: &[usize],
        message: &[u8],
    ) -> [u8; SIGNATURE_LEN] {
        let sigs: Vec<_> = indices.iter().map(|i| signer::sign(&secrets[*i], message)).collect();
        signer::aggregate_signatures(&sigs).unwrap()
    }

    #[test]
    fn a_message_survives_the_trip_to_the_wire_and_back() {
        let unsigned = Unsigned::build(9, [7; 32], b"a payload");
        assert_eq!(Unsigned::parse(&unsigned.bytes), Ok(unsigned.clone()));

        let signature = BitSet {
            signers: vec![0b0000_0101],
            signature: [3u8; SIGNATURE_LEN],
        };
        let message = Message::build(&unsigned, &signature);
        let read = Message::parse(&message.bytes).unwrap();
        assert_eq!(read.unsigned, unsigned);
        assert_eq!(read.signature, signature);
        assert_eq!(read.bytes, message.bytes);
    }

    #[test]
    fn a_messages_name_is_the_hash_of_the_bytes_that_travelled() {
        let a = Unsigned::build(9, [7; 32], b"a payload");
        let b = Unsigned::build(9, [7; 32], b"a payload!");
        assert_eq!(a.id(), hash256(&a.bytes));
        assert_ne!(a.id(), b.id());
    }

    #[test]
    fn a_signature_scheme_this_port_does_not_implement_is_refused_by_name() {
        // The alternative — parsing an unknown kind to an empty BitSet — is a
        // signature that verifies against nothing.
        let unsigned = Unsigned::build(1, [1; 32], b"x");
        let sig_bytes = w::new_bit_set_signature(&w::BitSetSignatureInput {
            kind: 0x01, // Corona
            signature: &[9u8; SIGNATURE_LEN],
            signers: &[],
        });
        let raw = w::new_message(&w::MessageInput {
            unsigned: &unsigned.bytes,
            signature: &sig_bytes,
        });
        assert_eq!(Message::parse(&raw), Err(Error::UnknownSignature(1)));
    }

    #[test]
    fn a_bit_vector_with_a_leading_zero_byte_is_refused() {
        // Two encodings of one set would be two messages with one meaning, and
        // the id is the hash of the bytes.
        let sig = BitSet {
            signers: vec![0x00, 0x01],
            signature: [0u8; SIGNATURE_LEN],
        };
        assert_eq!(sig.num_signers(), Err(Error::NotCanonical));
        assert_eq!(filter(&[0x00, 0x01], &[]), Err(Error::NotCanonical));
        // The empty vector is canonical: it names nobody.
        assert_eq!(BitSet { signers: vec![], ..sig }.num_signers(), Ok(0));
    }

    #[test]
    fn a_vector_naming_a_validator_the_set_does_not_have_is_refused() {
        let (canonical, _) = set(3, 10);
        assert_eq!(
            filter(&bits(&[7], 1), &canonical.validators),
            Err(Error::UnknownValidator { named: 7, set: 3 })
        );
        assert_eq!(filter(&bits(&[2], 1), &canonical.validators).unwrap().len(), 1);
    }

    #[test]
    fn two_nodes_sharing_a_key_are_one_signer_with_both_weights() {
        // One aggregate cannot tell them apart, so the set must not either.
        let sk = key(b"shared");
        let long = signer::uncompress(&sk.sk_to_pk().compress()).unwrap();
        let mut entries = BTreeMap::new();
        entries.insert(NodeId([1; 20]), (long.clone(), 10));
        entries.insert(NodeId([2; 20]), (long, 25));
        let canonical = flatten(&entries).unwrap();
        assert_eq!(canonical.validators.len(), 1);
        assert_eq!(canonical.validators[0].weight, 35);
        assert_eq!(canonical.validators[0].node_ids.len(), 2);
        assert_eq!(canonical.total_weight, 35);
    }

    #[test]
    fn a_keyless_validators_weight_still_counts_against_the_quorum() {
        // The whole point: a chain must not be able to cheapen its own quorum
        // by registering validators that cannot vote.
        let sk = key(b"the only signer");
        let long = signer::uncompress(&sk.sk_to_pk().compress()).unwrap();
        let mut entries = BTreeMap::new();
        entries.insert(NodeId([1; 20]), (long, 10));
        entries.insert(NodeId([2; 20]), (Vec::new(), 90));
        let canonical = flatten(&entries).unwrap();
        assert_eq!(canonical.validators.len(), 1, "a keyless validator cannot sign");
        assert_eq!(canonical.total_weight, 100, "and still weighs on the quorum");

        // 10 of 100 is not four fifths.
        assert_eq!(
            verify_weight(10, canonical.total_weight, 4, 5),
            Err(Error::InsufficientWeight {
                signed: 10,
                total: 100
            })
        );
    }

    #[test]
    fn the_quorum_is_compared_without_dividing() {
        // A division would round in the attacker's favour at these widths.
        assert_eq!(verify_weight(u64::MAX, u64::MAX, 4, 5), Ok(()));
        assert_eq!(
            verify_weight(u64::MAX / 2, u64::MAX, 4, 5),
            Err(Error::InsufficientWeight {
                signed: u64::MAX / 2,
                total: u64::MAX
            })
        );
        // Exactly the threshold passes: the rule is ≥, not >.
        assert_eq!(verify_weight(80, 100, 4, 5), Ok(()));
        assert_eq!(
            verify_weight(79, 100, 4, 5),
            Err(Error::InsufficientWeight {
                signed: 79,
                total: 100
            })
        );
    }

    #[test]
    fn a_proof_by_enough_of_the_source_chain_verifies() {
        let (canonical, secrets) = set(5, 20);
        let unsigned = Unsigned::build(9, [7; 32], b"register this validator");
        let signature = BitSet {
            signers: bits(&[0, 1, 2, 3], 1),
            signature: sign_by(&secrets, &[0, 1, 2, 3], &unsigned.bytes),
        };
        assert_eq!(verify(&signature, &unsigned, 9, &canonical, 4, 5), Ok(()));
    }

    #[test]
    fn a_proof_by_too_few_is_refused_even_though_every_signature_is_real() {
        let (canonical, secrets) = set(5, 20);
        let unsigned = Unsigned::build(9, [7; 32], b"register this validator");
        let signature = BitSet {
            signers: bits(&[0, 1, 2], 1),
            signature: sign_by(&secrets, &[0, 1, 2], &unsigned.bytes),
        };
        assert_eq!(
            verify(&signature, &unsigned, 9, &canonical, 4, 5),
            Err(Error::InsufficientWeight {
                signed: 60,
                total: 100
            })
        );
    }

    #[test]
    fn a_vector_claiming_signers_who_did_not_sign_is_refused() {
        // The weight test would pass; the pairing is what catches it.
        let (canonical, secrets) = set(5, 20);
        let unsigned = Unsigned::build(9, [7; 32], b"register this validator");
        let signature = BitSet {
            signers: bits(&[0, 1, 2, 3, 4], 1),
            signature: sign_by(&secrets, &[0, 1, 2, 3], &unsigned.bytes),
        };
        assert_eq!(
            verify(&signature, &unsigned, 9, &canonical, 4, 5),
            Err(Error::BadSignature(signer::Error::InvalidSignature))
        );
    }

    #[test]
    fn a_proof_over_another_message_does_not_carry_to_this_one() {
        let (canonical, secrets) = set(5, 20);
        let signed = Unsigned::build(9, [7; 32], b"one message");
        let other = Unsigned::build(9, [7; 32], b"another message");
        let signature = BitSet {
            signers: bits(&[0, 1, 2, 3], 1),
            signature: sign_by(&secrets, &[0, 1, 2, 3], &signed.bytes),
        };
        assert_eq!(
            verify(&signature, &other, 9, &canonical, 4, 5),
            Err(Error::BadSignature(signer::Error::InvalidSignature))
        );
    }

    #[test]
    fn a_message_signed_for_another_network_is_refused_before_any_pairing() {
        let (canonical, secrets) = set(5, 20);
        let unsigned = Unsigned::build(11, [7; 32], b"x");
        let signature = BitSet {
            signers: bits(&[0, 1, 2, 3], 1),
            signature: sign_by(&secrets, &[0, 1, 2, 3], &unsigned.bytes),
        };
        assert_eq!(
            verify(&signature, &unsigned, 9, &canonical, 4, 5),
            Err(Error::WrongNetwork { want: 9, got: 11 })
        );
    }

    #[test]
    fn a_proof_by_nobody_satisfies_nothing() {
        // An empty signer list aggregates to no key at all, which must refuse
        // rather than verify vacuously.
        let (canonical, _) = set(5, 0);
        let unsigned = Unsigned::build(9, [7; 32], b"x");
        let signature = BitSet {
            signers: vec![],
            signature: [0u8; SIGNATURE_LEN],
        };
        // Total weight is zero here, so the weight test alone would pass.
        assert_eq!(
            verify(&signature, &unsigned, 9, &canonical, 4, 5),
            Err(Error::BadSignature(signer::Error::MalformedPublicKey))
        );
    }

    #[test]
    fn the_bit_vector_is_read_big_endian() {
        // Bit 0 is the low bit of the LAST byte, which is what Go's big.Int
        // does. Reading it the other way round names different validators.
        assert!(bit_set(&[0x00, 0x01], 0));
        assert!(!bit_set(&[0x01, 0x00], 0));
        assert!(bit_set(&[0x01, 0x00], 8));
        assert_eq!(bit_len(&[0x00, 0x01]), 1);
        assert_eq!(bit_len(&[0x01, 0x00]), 9);
        assert_eq!(bit_len(&[]), 0);
        assert_eq!(bit_len(&[0x00]), 0);
    }
}
