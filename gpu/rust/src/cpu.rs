// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The CPU backend. Always compiled, always complete.
//!
//! Every function here is the DEFINITION of its primitive for this node. The
//! plugin backend does not get to have its own opinion: a plugin answer that
//! differs from one of these is a fork, and `LUX_GPU=verify` exists to find
//! that out on purpose rather than in production.

use ripemd::Ripemd160;
use sha2::{Digest, Sha256};
use sha3::Keccak256;

use crate::{Hash160, Hash256, SIGNATURE_LEN};

/// Ethereum Keccak-256 — the 0x01 pad, NOT FIPS-202 SHA3 — over the
/// concatenation of the parts.
pub fn keccak256(parts: &[&[u8]]) -> Hash256 {
    let mut h = Keccak256::new();
    for p in parts {
        h.update(p);
    }
    h.finalize().into()
}

/// One Keccak-256 per input.
pub fn keccak256_batch(inputs: &[&[u8]]) -> Vec<Hash256> {
    inputs.iter().map(|i| keccak256(&[i])).collect()
}

/// SHA-256.
pub fn sha256(buf: &[u8]) -> Hash256 {
    let mut h = Sha256::new();
    h.update(buf);
    h.finalize().into()
}

/// RIPEMD-160.
pub fn ripemd160(buf: &[u8]) -> Hash160 {
    let mut h = Ripemd160::new();
    h.update(buf);
    h.finalize().into()
}

/// Recover the compressed public key that signed `hash`.
///
/// The 65 bytes are `r ‖ s ‖ recovery_id`, and the answer is the 33-byte
/// compressed SEC1 encoding — the shape a Lux address is derived from, which
/// is not the shape the EVM `ecrecover` precompile returns. See the note on
/// [`crate::recover`].
pub fn recover(hash: &Hash256, sig: &[u8; SIGNATURE_LEN]) -> Option<Vec<u8>> {
    use k256::ecdsa::{RecoveryId, Signature, VerifyingKey};

    let recid = RecoveryId::from_byte(sig[64])?;
    let signature = Signature::from_slice(&sig[..64]).ok()?;
    let vk = VerifyingKey::recover_from_prehash(hash, &signature, recid).ok()?;
    Some(vk.to_encoded_point(true).as_bytes().to_vec())
}
