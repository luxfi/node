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
/// not a signature at all. A signature whose `s` was negated and whose
/// recovery id was flipped recovers the same key — that is ECDSA, and Go has
/// it too, so the two implementations accept and refuse exactly the same
/// bytes. It is not a way to spend someone's output: it changes the
/// transaction's id, because the id is the hash of the bytes that travel.
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
