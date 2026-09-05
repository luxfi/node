// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The three hashes the chain is defined over, and the Merkle fold built on the
//! third of them.
//!
//! - SHA-256 names things: a transaction id is the hash of its signed bytes, a
//!   block id the hash of its block bytes, a UTXO id the hash of the id of the
//!   transaction that made it prefixed by its output index.
//! - RIPEMD-160 of a SHA-256 makes an address out of a public key.
//! - Keccak-256 — Ethereum's, the 0x01 pad, NOT FIPS-202 SHA3 — is the state
//!   root's hash, at every leaf and every node.
//!
//! The Merkle construction is RFC 6962: `leaf(d) = keccak(0x00 ‖ d)`,
//! `node(L,R) = keccak(0x01 ‖ L ‖ R)`, empty = `keccak("")`. On an odd level the
//! last node is promoted UNCHANGED rather than paired with itself, which is what
//! keeps two different leaf sets from folding to one root. The tags separate the
//! three input domains, so a leaf preimage can never be read as a node's.
//!
//! None of it is written here. All five primitives and the fold live in
//! `lux-gpu`, which is where a chain asks for a primitive and where the choice
//! between computing it and handing it to an installed kernel library is made.
//! A hash with two implementations is a chain with two answers, so this module
//! is the chain's NAMES for them and nothing more.

pub use lux_gpu::{
    empty_root, keccak256, leaf_hash, merkle_root, node_hash, pubkey_bytes_to_address, ripemd160,
    sha256, Hash160, Hash256,
};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_empty_root_is_the_keccak_of_nothing() {
        // The value the Go merkle package and every GPU backend produce.
        assert_eq!(
            hex::encode(empty_root()),
            "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
        );
    }

    #[test]
    fn keccak_is_ethereums_not_fips_202() {
        // keccak256("") differs from SHA3-256(""), which is
        // a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a.
        assert_eq!(
            hex::encode(keccak256(&[b"abc"])),
            "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"
        );
    }

    #[test]
    fn a_single_leaf_root_is_the_tagged_leaf_itself() {
        let d = [1u8; 32];
        assert_eq!(merkle_root(&[d]), leaf_hash(&d));
    }

    #[test]
    fn an_odd_level_promotes_the_last_node_unchanged() {
        // Three leaves: (l0,l1) pair, l2 promoted; then that pair with l2.
        let d: Vec<Hash256> = (0u8..3).map(|i| [i; 32]).collect();
        let l: Vec<Hash256> = d.iter().map(leaf_hash).collect();
        let want = node_hash(&node_hash(&l[0], &l[1]), &l[2]);
        assert_eq!(merkle_root(&d), want);
    }

    #[test]
    fn sha256_and_the_address_derivation_match_go() {
        assert_eq!(
            hex::encode(sha256(b"abc")),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
        // RIPEMD-160(SHA-256("abc")) — the Bitcoin-style hash160 Go computes.
        assert_eq!(
            hex::encode(pubkey_bytes_to_address(b"abc")),
            "bb1be98c142444d7a56aa3981c3942a978e4dc33"
        );
    }
}
