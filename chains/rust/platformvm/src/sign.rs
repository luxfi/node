// SPDX-License-Identifier: BSD-3-Clause-Eco

//! Who authorised a spend.
//!
//! An unspent output names the addresses that may move it. A transaction that
//! moves it carries signatures, and this is the one place that turns a
//! signature back into the address that made it. Everything else — how many
//! signatures, which of the owner's addresses they stand for — is the flow
//! check's business; this answers only "whose".
//!
//! A signature is 65 bytes: `r ‖ s ‖ v`, where `v` is the recovery id, zero
//! based. That is the shape Go writes (`luxfi/crypto/secp256k1`), and it is
//! what makes a public key recoverable from a signature instead of having to
//! travel beside it — a transaction carries signatures and no keys.
//!
//! An address is `ripemd160(sha256(compressed key))`. Not the EVM's
//! `keccak256(uncompressed key)[12..]`: the two chains derive different
//! addresses from one key on purpose, and a node that confused them would
//! credit the wrong owner.

use crate::ids::{Id, ShortId};

/// The address a compressed public key spends under.
pub fn address(compressed_key: &[u8]) -> ShortId {
    ShortId(lux_gpu::pubkey_bytes_to_address(compressed_key))
}

/// The address that signed `sighash`, or nothing when the bytes are not a
/// signature over it.
///
/// The recovery id must be one of the four the curve defines; anything else is
/// not a signature at all. Beyond that this asks only what the curve answers,
/// because that is all Go asks: `luxfi/crypto/secp256k1.RecoverPubkey` checks
/// the length and `v < 4` and recovers. Its low-`s` refusal lives in
/// `VerifySignature`, which the credential path never calls — `secp256k1fx`
/// recovers an address and compares it to the output's owner, and nothing in
/// between looks at `s`.
///
/// So a signature whose `s` was negated and whose recovery id had its parity
/// flipped is one Go spends under, and it must be one we spend under. It names
/// the same key: negating `s` and negating the point `R` cancel, which is
/// ECDSA. Putting such a signature back in the form the curve will read is the
/// seam's job, not this chain's — it is the same rule for every chain, so it
/// is stated once, in `lux_gpu`. The answer is the same address either way,
/// which is the point: malleating is not a way to take someone's output, it
/// only changes the transaction's id, because the id is the hash of the bytes
/// that travel.
pub fn recover(sighash: &Id, sig: &[u8; 65]) -> Option<ShortId> {
    Some(address(&lux_gpu::recover(sighash, sig)?))
}

/// Sign a hash the way a wallet does, into the 65 bytes the wire carries.
///
/// Only tests hold a key: a chain verifies signatures and never makes them,
/// and a node that could sign for an owner would be a node an owner has to
/// trust.
#[cfg(test)]
pub(crate) fn sign(key: &k256::ecdsa::SigningKey, sighash: &Id) -> [u8; 65] {
    let (signature, recovery) = key
        .sign_prehash_recoverable(&sighash[..])
        .expect("signing a 32-byte hash");
    let mut out = [0u8; 65];
    out[..64].copy_from_slice(&signature.to_bytes());
    out[64] = recovery.to_byte();
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use k256::ecdsa::SigningKey;
    // Deliberately NOT the seam: this module's job is to check that what the
    // seam returns is the hash chain the P-Chain is defined over, and a check
    // written with the thing it is checking checks nothing.
    use ripemd::Ripemd160;
    use sha2::{Digest, Sha256};

    fn key(seed: u8) -> SigningKey {
        SigningKey::from_bytes(&[seed; 32].into()).expect("a key")
    }

    #[test]
    fn a_signature_names_the_address_that_made_it() {
        let k = key(0x11);
        let sighash: Id = [7; 32];
        let sig = sign(&k, &sighash);
        let want = address(k.verifying_key().to_encoded_point(true).as_bytes());
        assert_eq!(recover(&sighash, &sig), Some(want));
    }

    #[test]
    fn a_signature_over_something_else_names_someone_else() {
        // The whole point: a signature is over one message, and moving it to
        // another recovers a key nobody holds.
        let k = key(0x11);
        let sig = sign(&k, &[7; 32]);
        let want = address(k.verifying_key().to_encoded_point(true).as_bytes());
        assert_ne!(recover(&[8; 32], &sig), Some(want));
    }

    #[test]
    fn two_keys_are_two_addresses() {
        let sighash: Id = [7; 32];
        let a = recover(&sighash, &sign(&key(0x11), &sighash));
        let b = recover(&sighash, &sign(&key(0x22), &sighash));
        assert!(a.is_some() && b.is_some());
        assert_ne!(a, b);
    }

    /// Sixteen bytes of hex into the bytes they spell.
    fn unhex(s: &str) -> Vec<u8> {
        (0..s.len() / 2)
            .map(|i| u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).expect("hex"))
            .collect()
    }

    /// The bytes Go answers with, recorded from Go.
    ///
    /// Produced by `luxfi/crypto@v1.20.5` — `secp256k1.ToPrivateKey([0x11; 32])`,
    /// `SignHash([7; 32])`, then `RecoverPublicKeyFromHash` and
    /// `hash.PubkeyBytesToAddress`, which is the credential path
    /// `luxfi/utxo/secp256k1fx.Fx.VerifyCredentials` walks. The same values come
    /// out of both Go builds — cgo over `libsecp256k1`, and `CGO_ENABLED=0` over
    /// decred — so they are the network's answer and not one build's.
    const GO_KEY_SEED: u8 = 0x11;
    const GO_SIGHASH: Id = [7; 32];
    const GO_SIG: &str = "111f20b9521ba1924ecfb91595426246b152cc1187e83f798cbd61f95f2c4cb107da8d209539506429d1ecd4033b2c207b89267f7dd8674421737193cd84f1dc00";
    /// The same signature with `s` negated and the recovery id's parity bit
    /// flipped. Go recovers the same key from it, because that is what the
    /// curve does, and Go looks no further.
    const GO_MALLEATED: &str = "111f20b9521ba1924ecfb91595426246b152cc1187e83f798cbd61f95f2c4cb1f82572df6ac6af9bd62e132bfcc4d3de3f25b667317038f79e5eecf902b14f6501";
    const GO_ADDRESS: &str = "fc7250a211deddc70ee5a2738de5f07817351cef";

    fn go_sig(hex: &str) -> [u8; 65] {
        let mut out = [0u8; 65];
        out.copy_from_slice(&unhex(hex));
        out
    }

    fn go_address() -> ShortId {
        let mut out = [0u8; 20];
        out.copy_from_slice(&unhex(GO_ADDRESS));
        ShortId(out)
    }

    /// Signing here writes the bytes Go signs, and reading them names the
    /// address Go names.
    #[test]
    fn the_canonical_signature_is_byte_for_byte_gos() {
        let ours = sign(&key(GO_KEY_SEED), &GO_SIGHASH);
        assert_eq!(
            ours.to_vec(),
            unhex(GO_SIG),
            "the deterministic signature diverged from Go's"
        );
        assert_eq!(recover(&GO_SIGHASH, &ours), Some(go_address()));
    }

    /// The finding. A malleated signature is one Go spends under, so refusing
    /// it here would be two networks: one node's valid transaction is another
    /// node's invalid one, and the chain forks on a credential.
    ///
    /// `s` is negated and the recovery id's parity bit flipped — the same key
    /// comes back out, which is ECDSA, and Go's recover does no low-`s` check
    /// to stop it (`RecoverPubkey` checks only the length and `v < 4`; the
    /// low-`s` refusal lives in `VerifySignature`, which this path never
    /// calls).
    #[test]
    fn a_malleated_signature_names_the_address_go_names() {
        let malleated = go_sig(GO_MALLEATED);
        let canonical = go_sig(GO_SIG);
        // The premise: these really are two different signatures.
        assert_ne!(malleated, canonical);
        // And really the malleated form — high `s`, flipped parity.
        assert_eq!(malleated[..32], canonical[..32], "r is untouched");
        assert_ne!(malleated[32..64], canonical[32..64], "s is negated");
        assert_eq!(malleated[64], canonical[64] ^ 1, "parity is flipped");

        assert_eq!(
            recover(&GO_SIGHASH, &malleated),
            Some(go_address()),
            "Go spends under this signature and we refused it"
        );
    }

    /// Malleating is not a way to take someone's output: the same address
    /// comes back, so the flow check still asks the same owner.
    #[test]
    fn malleating_does_not_move_the_output_to_someone_else() {
        assert_eq!(
            recover(&GO_SIGHASH, &go_sig(GO_MALLEATED)),
            recover(&GO_SIGHASH, &go_sig(GO_SIG))
        );
    }

    /// Negating `s` without flipping the recovery id is a different point, and
    /// so a different key — one nobody holds. Go answers some address here
    /// too, just not the signer's, and the flow check refuses it there.
    #[test]
    fn negating_s_alone_names_someone_else() {
        let mut half = go_sig(GO_MALLEATED);
        half[64] ^= 1; // put the parity back
        assert_ne!(recover(&GO_SIGHASH, &half), Some(go_address()));
    }

    #[test]
    fn bytes_that_are_not_a_signature_name_nobody() {
        assert_eq!(recover(&[7; 32], &[0u8; 65]), None);
        // A recovery id outside the four the curve defines.
        let mut bad = sign(&key(0x11), &[7; 32]);
        bad[64] = 4;
        assert_eq!(recover(&[7; 32], &bad), None);
    }

    /// The address is the hash chain Go uses, not the EVM's.
    #[test]
    fn an_address_is_ripemd_of_sha_of_the_compressed_key() {
        let k = key(0x11);
        let compressed = k.verifying_key().to_encoded_point(true);
        let by_hand = {
            let sha = Sha256::digest(compressed.as_bytes());
            let ripe = Ripemd160::digest(sha);
            let mut out = [0u8; 20];
            out.copy_from_slice(&ripe);
            ShortId(out)
        };
        assert_eq!(address(compressed.as_bytes()), by_hand);
        // A compressed key is 33 bytes; using the uncompressed one would be a
        // different address for the same key.
        assert_eq!(compressed.as_bytes().len(), 33);
        assert_ne!(
            address(k.verifying_key().to_encoded_point(false).as_bytes()),
            by_hand
        );
    }
}
