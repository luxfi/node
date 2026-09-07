// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! CPU against plugin, byte for byte.
//!
//! Run with `LUX_GPU_LIB=<path to libluxgpu>` to point at an installed kernel
//! library. With none installed the test says so and passes on the CPU alone —
//! it prints "skipped", it does not print a green it did not earn.

use lux_gpu::{backend, cpu, keccak256_batch, merkle_root, Hash256};

/// A cheap, seeded, reproducible byte stream. A differential wants many shapes,
/// not many crates.
struct Rng(u64);

impl Rng {
    fn next(&mut self) -> u64 {
        // splitmix64
        self.0 = self.0.wrapping_add(0x9e37_79b9_7f4a_7c15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
        z ^ (z >> 31)
    }
    fn bytes(&mut self, n: usize) -> Vec<u8> {
        (0..n).map(|_| (self.next() & 0xff) as u8).collect()
    }
}

fn plugin_present() -> bool {
    backend().starts_with("plugin:")
}

#[test]
fn keccak256_batch_is_the_same_answer_on_both_backends() {
    if !plugin_present() {
        eprintln!(
            "skipped: no kernel library installed (backend={}). \
             Set LUX_GPU_LIB to run the differential.",
            backend()
        );
        return;
    }
    eprintln!("differential against {}", backend());

    let mut rng = Rng(0x5eed);
    // Sizes that matter: empty, one byte, under the 136-byte rate, exactly the
    // rate, over it, and a multi-block input.
    let sizes = [0usize, 1, 31, 32, 135, 136, 137, 271, 272, 1024, 4096];

    for batch in [1usize, 2, 3, 7, 64, 257] {
        let mut owned: Vec<Vec<u8>> = Vec::with_capacity(batch);
        for i in 0..batch {
            owned.push(rng.bytes(sizes[i % sizes.len()]));
        }
        let ins: Vec<&[u8]> = owned.iter().map(|v| v.as_slice()).collect();

        let want = cpu::keccak256_batch(&ins);
        let got = keccak256_batch(&ins);
        assert_eq!(got.len(), want.len(), "batch={batch}");
        for i in 0..want.len() {
            assert_eq!(
                got[i],
                want[i],
                "batch={batch} element={i} len={}",
                ins[i].len()
            );
        }
    }
}

#[test]
fn a_batch_of_nothing_but_empty_inputs_still_agrees() {
    if !plugin_present() {
        eprintln!(
            "skipped: no kernel library installed (backend={})",
            backend()
        );
        return;
    }
    let empty: Vec<u8> = Vec::new();
    for batch in [1usize, 8, 100] {
        let ins: Vec<&[u8]> = (0..batch).map(|_| empty.as_slice()).collect();
        assert_eq!(
            keccak256_batch(&ins),
            cpu::keccak256_batch(&ins),
            "batch={batch}"
        );
    }
}

#[test]
fn the_merkle_root_is_the_same_root_on_both_backends() {
    let mut rng = Rng(0xf01d);
    for n in [0usize, 1, 2, 3, 5, 8, 17, 64, 129, 1000] {
        let leaves: Vec<Hash256> = (0..n)
            .map(|_| {
                let mut d = [0u8; 32];
                d.copy_from_slice(&rng.bytes(32));
                d
            })
            .collect();

        // The CPU root, computed without ever consulting the plugin.
        let want = scalar_root(&leaves);
        let got = merkle_root(&leaves);
        assert_eq!(got, want, "n={n} backend={}", backend());
    }
    if !plugin_present() {
        eprintln!(
            "note: the roots above were CPU-against-CPU (backend={}); \
             install a kernel library to make this a cross-backend check",
            backend()
        );
    }
}

/// The fold written the obvious way, with no batching and no dispatch. It is
/// the thing the seam's version has to keep agreeing with.
fn scalar_root(leaves: &[Hash256]) -> Hash256 {
    if leaves.is_empty() {
        return cpu::keccak256(&[]);
    }
    let mut level: Vec<Hash256> = leaves
        .iter()
        .map(|d| cpu::keccak256(&[&[0x00], &d[..]]))
        .collect();
    while level.len() > 1 {
        let cnt = level.len();
        let parents = cnt.div_ceil(2);
        let mut next = vec![[0u8; 32]; parents];
        for j in 0..(cnt / 2) {
            next[j] = cpu::keccak256(&[&[0x01], &level[2 * j][..], &level[2 * j + 1][..]]);
        }
        if cnt & 1 == 1 {
            next[parents - 1] = level[cnt - 1];
        }
        level = next;
    }
    level[0]
}

/// The recovery ids, pinned — and the cross-language edge named in
/// `gpu/cpp/include/lux/gpu/gpu.hpp`.
///
/// The vector is k256's own: hash = 0x07 repeated, key = 0x11 repeated. Go
/// (`luxfi/crypto/secp256k1`) refuses only `v >= 4` and recovers all four ids;
/// so does this. The first-party C++ curve cannot do 2 or 3 at all and refuses
/// them, which for THIS signature is the same answer — both say "no key",
/// because `r + n` is not a valid x coordinate here. The two only part on a
/// deliberately built `r` where it is, and there C++ refuses what Go accepts,
/// never the other way round.
#[test]
fn the_recovery_ids_are_the_ones_go_accepts() {
    let hash: Hash256 = [7u8; 32];
    let mut sig = [0u8; 65];
    let s = "111f20b9521ba1924ecfb91595426246b152cc1187e83f798cbd61f95f2c4cb1\
             07da8d209539506429d1ecd4033b2c207b89267f7dd8674421737193cd84f1dc00";
    let s: String = s.chars().filter(|c| !c.is_whitespace()).collect();
    for (i, b) in sig.iter_mut().enumerate() {
        *b = u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).unwrap();
    }

    sig[64] = 0;
    assert_eq!(
        lux_gpu::recover(&hash, &sig).map(hex),
        Some("034f355bdcb7cc0af728ef3cceb9615d90684bb5b2ca5f859ab0f0b704075871aa".to_string()),
        "id 0 recovers the key that signed"
    );
    sig[64] = 1;
    assert!(
        lux_gpu::recover(&hash, &sig).is_some(),
        "id 1 recovers the other candidate, as Go does"
    );
    // r + n is not a valid x coordinate for this r, so both curves say no key.
    sig[64] = 2;
    assert!(lux_gpu::recover(&hash, &sig).is_none(), "id 2");
    sig[64] = 3;
    assert!(lux_gpu::recover(&hash, &sig).is_none(), "id 3");
    // Four ids exist; a fifth is not a signature at all.
    sig[64] = 4;
    assert!(lux_gpu::recover(&hash, &sig).is_none(), "id 4");
}

/// A malleated signature names the same key, because the reference spends
/// under it.
///
/// Negating `s` and flipping the recovery parity is the same signature over
/// the same message by the same key — negating `s` and negating `R` cancel,
/// which is ECDSA. Go recovers under it (`RecoverPubkey` checks the length and
/// `v < 4`; the low-`s` refusal lives in `VerifySignature`, which no spend path
/// calls) and so does the first-party C++ curve. `k256` is the only one of the
/// three that refuses a high `s` in recovery, so the seam puts the pair back in
/// the form it reads.
///
/// Without that, this backend alone would refuse an output Go spends — a chain
/// split anyone can build by negating one scalar. The address is checked, not
/// just the some-ness: a recovery that returned a DIFFERENT key would be worse
/// than a refusal.
#[test]
fn a_malleated_signature_recovers_the_same_key() {
    use k256::elliptic_curve::scalar::IsHigh;

    let hash: Hash256 = [7u8; 32];
    let signing = k256::ecdsa::SigningKey::from_bytes(&[0x11u8; 32].into()).unwrap();
    let (sig, recid) = signing.sign_prehash_recoverable(&hash).unwrap();

    let mut plain = [0u8; 65];
    plain[..64].copy_from_slice(&sig.to_bytes());
    plain[64] = recid.to_byte();
    let want = lux_gpu::recover(&hash, &plain).map(hex);
    assert!(want.is_some(), "the honest signature recovers");

    // The signer emits a low `s`, so the malleated form is the negated one.
    assert!(bool::from(sig.s().is_high()) == false, "the signer emits low s");
    let flipped = k256::ecdsa::Signature::from_scalars(*sig.r(), -*sig.s()).unwrap();
    let mut malleated = [0u8; 65];
    malleated[..64].copy_from_slice(&flipped.to_bytes());
    malleated[64] = if recid.to_byte() & 1 == 1 { recid.to_byte() & !1 } else { recid.to_byte() | 1 };

    assert_eq!(
        lux_gpu::recover(&hash, &malleated).map(hex),
        want,
        "the malleated pair names the same key, as Go and C++ both say"
    );
}

fn hex(b: Vec<u8>) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}
