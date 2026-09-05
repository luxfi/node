// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The one place a Lux chain asks for a primitive.
//!
//! # The boundary
//!
//! Kernels are private and live in their own repository. This crate contains no
//! kernel source and never will: what crosses the line is a C ABI — four symbol
//! names — opened at run time if the library happens to be installed.
//!
//! # The rule
//!
//! The CPU backend in [`cpu`] is COMPLETE. A node with nothing installed
//! computes every primitive itself and is a full node, not a degraded one. The
//! plugin is a strict positive overlay: it may only ever be faster, never
//! different. Two backends that disagree about a hash disagree about a block,
//! so every plugin answer is either byte-identical to the CPU answer or a bug.
//!
//! # The knob
//!
//! `LUX_GPU` picks the policy, once, at first use:
//!
//! - `off` — CPU only; nothing is opened.
//! - `on` — use the plugin for batch work when a library is installed. This is
//!   the default, and it degrades to `off` on its own when nothing is
//!   installed.
//! - `verify` — compute BOTH and abort on the first byte that differs. This is
//!   what a differential run sets; it is the mode that turns "we believe they
//!   agree" into "we checked".
//!
//! `LUX_GPU_LIB` names the library path when it is not on the loader's path.
//!
//! # What actually has two paths
//!
//! - `keccak256_batch` — CPU and plugin (`lux_gpu_keccak256_batch`), and so
//!   [`merkle_root`], which is a batch of keccaks per level.
//! - `sha256`, `ripemd160` — CPU only. The plugin ABI has no such op: its hash
//!   surface is keccak256, SHA3-256, SHAKE256, BLAKE3, Poseidon2. SHA3-256 is
//!   NOT SHA-256.
//! - [`recover`] — CPU only. The plugin's `lux_gpu_ecrecover_batch` answers a
//!   different question: it returns an Ethereum address, `keccak(Q.x‖Q.y)[12:]`,
//!   while a Lux address is `ripemd160(sha256(compressed Q))`. The recovered
//!   key itself never leaves that call, so its output cannot be turned into
//!   this one's.

pub mod cpu;
mod plugin;

/// A 256-bit digest.
pub type Hash256 = [u8; 32];
/// A 160-bit digest — the width of an address.
pub type Hash160 = [u8; 20];

/// A secp256k1 signature: 64 bytes of (r,s) plus the recovery byte.
pub const SIGNATURE_LEN: usize = 65;

/// What `LUX_GPU` selected.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Policy {
    /// CPU only. Nothing is opened.
    Off,
    /// Plugin when installed, CPU otherwise.
    On,
    /// Both, compared, every call.
    Verify,
}

fn policy() -> Policy {
    use std::sync::OnceLock;
    static P: OnceLock<Policy> = OnceLock::new();
    *P.get_or_init(|| match std::env::var("LUX_GPU").as_deref() {
        Ok("off") | Ok("OFF") | Ok("0") => Policy::Off,
        Ok("verify") | Ok("VERIFY") => Policy::Verify,
        _ => Policy::On,
    })
}

/// Which backend is answering: `cpu`, or `plugin:<name>` naming the device the
/// installed library selected for itself.
///
/// This is a diagnostic, and it is the honest one: a host with the library
/// installed but no device driver reports `plugin:cpu`, not `plugin:cuda`.
pub fn backend() -> String {
    if policy() == Policy::Off {
        return "cpu".to_string();
    }
    match plugin::backend_name() {
        Some(n) => format!("plugin:{n}"),
        None => "cpu".to_string(),
    }
}

/// Ethereum Keccak-256 over the concatenation of the parts.
///
/// One hash is not a batch: there is nothing to parallelise and a dispatch
/// costs more than the answer. This is the CPU backend, always.
pub fn keccak256(parts: &[&[u8]]) -> Hash256 {
    cpu::keccak256(parts)
}

/// One Keccak-256 per input — the shape that has somewhere else to go.
pub fn keccak256_batch(inputs: &[&[u8]]) -> Vec<Hash256> {
    match policy() {
        Policy::Off => cpu::keccak256_batch(inputs),
        Policy::On => {
            plugin::keccak256_batch(inputs).unwrap_or_else(|| cpu::keccak256_batch(inputs))
        }
        Policy::Verify => {
            let want = cpu::keccak256_batch(inputs);
            if let Some(got) = plugin::keccak256_batch(inputs) {
                let lens: Vec<usize> = inputs.iter().map(|i| i.len()).collect();
                agree(&got, &want, &lens);
            }
            want
        }
    }
}

/// What `LUX_GPU=verify` does when it has both answers.
///
/// Separated out so a test can hand it a wrong answer and watch it refuse —
/// a comparison nothing has ever seen fail is a comparison nobody has checked
/// can fail.
fn agree(got: &[Hash256], want: &[Hash256], lens: &[usize]) {
    assert_eq!(
        got.len(),
        want.len(),
        "LUX_GPU=verify: plugin returned {} digests for a batch of {}",
        got.len(),
        want.len()
    );
    for (i, (g, w)) in got.iter().zip(want.iter()).enumerate() {
        assert!(
            g == w,
            "LUX_GPU=verify: plugin and CPU disagree on keccak256 of input {i} \
             ({} bytes): plugin {} vs cpu {} — this is a consensus bug, not a \
             performance one",
            lens[i],
            hex(g),
            hex(w)
        );
    }
}

fn hex(d: &Hash256) -> String {
    d.iter().map(|b| format!("{b:02x}")).collect()
}

/// SHA-256.
pub fn sha256(buf: &[u8]) -> Hash256 {
    cpu::sha256(buf)
}

/// RIPEMD-160.
pub fn ripemd160(buf: &[u8]) -> Hash160 {
    cpu::ripemd160(buf)
}

/// The address of a public key: RIPEMD-160 of its SHA-256.
pub fn pubkey_bytes_to_address(key: &[u8]) -> Hash160 {
    cpu::ripemd160(&cpu::sha256(key))
}

/// Recover the compressed public key that signed `hash`, or `None`.
pub fn recover(hash: &Hash256, sig: &[u8; SIGNATURE_LEN]) -> Option<Vec<u8>> {
    cpu::recover(hash, sig)
}

// ---- the RFC 6962 tagged binary Merkle fold --------------------------------
//
// `leaf(d) = keccak(0x00 ‖ d)`, `node(L,R) = keccak(0x01 ‖ L ‖ R)`, empty =
// `keccak("")`. On an odd level the last node is promoted UNCHANGED rather than
// paired with itself, which is what keeps two different leaf sets from folding
// to one root. The tags separate the domains, so a leaf preimage can never be
// read as a node's.

/// `keccak256(0x00 ‖ d)` — a tagged leaf.
pub fn leaf_hash(d: &Hash256) -> Hash256 {
    keccak256(&[&[0x00], &d[..]])
}

/// `keccak256(0x01 ‖ L ‖ R)` — a tagged internal node.
pub fn node_hash(left: &Hash256, right: &Hash256) -> Hash256 {
    keccak256(&[&[0x01], &left[..], &right[..]])
}

/// `keccak256("")` — the root of nothing.
pub fn empty_root() -> Hash256 {
    keccak256(&[])
}

/// The tagged binary Merkle root over element digests already compacted to the
/// occupied set, in ascending order.
///
/// A level is a batch: every node on it is independent of every other, which is
/// the only reason a device can help at all. The preimages are built here and
/// hashed together, so the whole level makes one dispatch.
pub fn merkle_root(leaves: &[Hash256]) -> Hash256 {
    if leaves.is_empty() {
        return empty_root();
    }

    let mut preimages: Vec<Vec<u8>> = Vec::with_capacity(leaves.len());
    for d in leaves {
        let mut p = Vec::with_capacity(33);
        p.push(0x00);
        p.extend_from_slice(d);
        preimages.push(p);
    }
    let mut level = hash_all(&preimages);

    while level.len() > 1 {
        let cnt = level.len();
        let parents = cnt.div_ceil(2);
        let pairs = cnt / 2;

        let mut preimages: Vec<Vec<u8>> = Vec::with_capacity(pairs);
        for j in 0..pairs {
            let mut p = Vec::with_capacity(65);
            p.push(0x01);
            p.extend_from_slice(&level[2 * j]);
            p.extend_from_slice(&level[2 * j + 1]);
            preimages.push(p);
        }
        let mut next = hash_all(&preimages);
        next.resize(parents, [0u8; 32]);
        if cnt & 1 == 1 {
            // Lone right node: promoted unchanged, never doubled.
            next[parents - 1] = level[cnt - 1];
        }
        level = next;
    }
    level[0]
}

fn hash_all(preimages: &[Vec<u8>]) -> Vec<Hash256> {
    let views: Vec<&[u8]> = preimages.iter().map(|p| p.as_slice()).collect();
    keccak256_batch(&views)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_empty_root_is_the_keccak_of_nothing() {
        assert_eq!(
            hex::encode(empty_root()),
            "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
        );
    }

    #[test]
    fn keccak_is_ethereums_not_fips_202() {
        // SHA3-256("abc") is 3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532;
        // a backend that answered with that would be answering a different
        // question, which is exactly what the plugin's op_sha3_256_hash is for.
        assert_eq!(
            hex::encode(keccak256(&[b"abc"])),
            "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"
        );
    }

    #[test]
    fn a_batch_answers_exactly_what_the_singles_answer() {
        let a = b"".to_vec();
        let b = b"abc".to_vec();
        let c = vec![0xffu8; 4096];
        let ins: Vec<&[u8]> = vec![&a, &b, &c];
        let got = keccak256_batch(&ins);
        assert_eq!(got.len(), 3);
        for (i, w) in ins.iter().enumerate() {
            assert_eq!(got[i], keccak256(&[w]), "batch element {i}");
        }
    }

    #[test]
    fn a_single_leaf_root_is_the_tagged_leaf_itself() {
        let d = [1u8; 32];
        assert_eq!(merkle_root(&[d]), leaf_hash(&d));
    }

    #[test]
    fn an_odd_level_promotes_the_last_node_unchanged() {
        let d: Vec<Hash256> = (0u8..3).map(|i| [i; 32]).collect();
        let l: Vec<Hash256> = d.iter().map(leaf_hash).collect();
        let want = node_hash(&node_hash(&l[0], &l[1]), &l[2]);
        assert_eq!(merkle_root(&d), want);
    }

    /// The fold is written as one batch per level; a scalar fold of the same
    /// leaves must land on the same root for every size, or the rewrite that
    /// made the batching possible changed the chain.
    #[test]
    fn the_batched_fold_equals_the_scalar_fold_at_every_size() {
        for n in 0..40usize {
            let leaves: Vec<Hash256> = (0..n)
                .map(|i| cpu::sha256(&(i as u64).to_le_bytes()))
                .collect();
            assert_eq!(merkle_root(&leaves), scalar_fold(&leaves), "n={n}");
        }
    }

    fn scalar_fold(leaves: &[Hash256]) -> Hash256 {
        if leaves.is_empty() {
            return empty_root();
        }
        let mut level: Vec<Hash256> = leaves.iter().map(leaf_hash).collect();
        while level.len() > 1 {
            let cnt = level.len();
            let parents = cnt.div_ceil(2);
            let mut next = vec![[0u8; 32]; parents];
            for j in 0..(cnt / 2) {
                next[j] = node_hash(&level[2 * j], &level[2 * j + 1]);
            }
            if cnt & 1 == 1 {
                next[parents - 1] = level[cnt - 1];
            }
            level = next;
        }
        level[0]
    }

    #[test]
    fn sha256_and_the_address_derivation_match_go() {
        assert_eq!(
            hex::encode(sha256(b"abc")),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
        assert_eq!(
            hex::encode(pubkey_bytes_to_address(b"abc")),
            "bb1be98c142444d7a56aa3981c3942a978e4dc33"
        );
    }

    #[test]
    fn verify_accepts_two_identical_answers() {
        let a = cpu::keccak256_batch(&[b"a", b"bb"]);
        agree(&a, &a, &[1, 2]);
    }

    #[test]
    #[should_panic(expected = "consensus bug")]
    fn verify_refuses_two_different_answers() {
        let want = cpu::keccak256_batch(&[b"a", b"bb"]);
        let mut got = want.clone();
        got[1][31] ^= 1;
        agree(&got, &want, &[1, 2]);
    }

    #[test]
    #[should_panic(expected = "digests for a batch of")]
    fn verify_refuses_an_answer_of_the_wrong_length() {
        let want = cpu::keccak256_batch(&[b"a", b"bb"]);
        agree(&want[..1], &want, &[1, 2]);
    }

    #[test]
    fn the_backend_names_itself() {
        let b = backend();
        assert!(b == "cpu" || b.starts_with("plugin:"), "backend()={b}");
    }
}
