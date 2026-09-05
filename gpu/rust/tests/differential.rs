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
                got[i], want[i],
                "batch={batch} element={i} len={}",
                ins[i].len()
            );
        }
    }
}

#[test]
fn a_batch_of_nothing_but_empty_inputs_still_agrees() {
    if !plugin_present() {
        eprintln!("skipped: no kernel library installed (backend={})", backend());
        return;
    }
    let empty: Vec<u8> = Vec::new();
    for batch in [1usize, 8, 100] {
        let ins: Vec<&[u8]> = (0..batch).map(|_| empty.as_slice()).collect();
        assert_eq!(keccak256_batch(&ins), cpu::keccak256_batch(&ins), "batch={batch}");
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
