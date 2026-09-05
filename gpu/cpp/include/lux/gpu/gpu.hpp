// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gpu.hpp — the one place a Lux chain asks for a primitive.
//
// THE BOUNDARY
//
// Kernels are private and live in their own repository. This directory holds no
// kernel source and never will: what crosses the line is a C ABI — four symbol
// names — opened at run time if the library happens to be installed.
//
// THE RULE
//
// The CPU backend is COMPLETE. A node with nothing installed computes every
// primitive itself and is a full node, not a degraded one. The plugin is a
// strict positive overlay: it may only ever be faster, never different. Two
// backends that disagree about a hash disagree about a block, so every plugin
// answer is either byte-identical to the CPU answer or a bug.
//
// THE KNOB
//
//   LUX_GPU=off      CPU only; nothing is opened.
//   LUX_GPU=on       plugin for batch work when installed (default; degrades
//                    to off on its own when nothing is installed).
//   LUX_GPU=verify   compute BOTH and abort on the first differing byte.
//   LUX_GPU_LIB=...  the library path, when it is not on the loader's path.
//
// WHAT ACTUALLY HAS TWO PATHS
//
//   keccak256_batch  CPU and plugin (lux_gpu_keccak256_batch) — and so
//                    merkle_root, which is a batch of keccaks per level.
//   sha256           CPU only. The plugin ABI has no SHA-256 op; its hash
//   ripemd160        surface is keccak256, SHA3-256, SHAKE256, BLAKE3,
//                    Poseidon2, and SHA3-256 is not SHA-256.
//   recover          CPU only. The plugin's lux_gpu_ecrecover_batch answers a
//                    different question — it returns an Ethereum address,
//                    keccak(Q.x||Q.y)[12:], where a Lux address is
//                    ripemd160(sha256(compressed Q)). The recovered key never
//                    leaves that call, so its answer cannot be turned into
//                    this one's.

#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <span>
#include <string>
#include <vector>

namespace lux::gpu {

using Digest = std::array<std::uint8_t, 32>;
using Address = std::array<std::uint8_t, 20>;
using Signature = std::array<std::uint8_t, 65>;
using CompressedKey = std::array<std::uint8_t, 33>;
using Bytes = std::span<const std::uint8_t>;

// Which backend is answering: "cpu", or "plugin:<name>" naming the device the
// installed library selected for itself. A host with the library installed but
// no device driver honestly reports "plugin:cpu".
std::string backend();

// ---- hashes ----------------------------------------------------------------

// Ethereum Keccak-256 — the 0x01 pad, NOT FIPS-202 SHA3 — over the
// concatenation of the parts. One hash is not a batch: there is nothing to
// parallelise and a dispatch costs more than the answer, so this is the CPU
// backend, always.
Digest keccak256(std::span<const Bytes> parts);
Digest keccak256(Bytes one);

// One Keccak-256 per input — the shape that has somewhere else to go.
std::vector<Digest> keccak256_batch(std::span<const Bytes> inputs);

// SHA-256.
Digest sha256(Bytes data);

// RIPEMD-160.
Address ripemd160(Bytes data);

// The address of a public key: RIPEMD-160 of its SHA-256.
Address pubkey_to_address(Bytes compressed_key);

// ---- signatures ------------------------------------------------------------

// Recover the 33-byte compressed public key that signed `hash` from the 65
// bytes r || s || v, or nothing when those bytes are not a signature over it.
//
// Recovery ids 2 and 3. Go accepts all four (luxfi/crypto/secp256k1
// checkSignature refuses only v >= 4) and Rust's k256 does too. They mean
// R.x = r + n, and the first-party C++ curve refuses r >= n at parse time, so
// it cannot do them and says so.
//
// For any r where r + n is not a valid x coordinate — which is every r a wallet
// will ever produce — Go and Rust ALSO return no key, so all three agree. The
// three only part on a deliberately built r where r + n is on the curve, and
// there this refuses what Go accepts, never the other way round.
//
// That direction is the point. chains/cpp/xvm used to mask v & 1 instead, which
// turned v = 2 into v = 0 and recovered the key of a DIFFERENT id — so taking
// any valid signature and moving its recovery byte from 0 to 2 produced a
// transaction a C++ node accepted and Go and Rust both refused. That is a fork
// anyone could build in one byte, and refusing closes it.
std::optional<CompressedKey> recover(const Digest& hash, const Signature& sig);

// ---- the RFC 6962 tagged binary Merkle fold --------------------------------
//
// leaf(d) = keccak(0x00 || d), node(L,R) = keccak(0x01 || L || R), empty =
// keccak(""). On an odd level the last node is promoted UNCHANGED rather than
// paired with itself, which keeps two different leaf sets from folding to one
// root. The tags separate the domains, so a leaf preimage can never be read as
// a node's.

inline constexpr std::uint8_t kLeafTag = 0x00;
inline constexpr std::uint8_t kNodeTag = 0x01;

Digest leaf_hash(const Digest& d);
Digest node_hash(const Digest& l, const Digest& r);
Digest empty_root();

// Folds a DENSE, already-compacted, ascending leaf-digest list. A level is a
// batch: every node on it is independent of every other, which is the only
// reason a device can help at all.
Digest merkle_root(const std::vector<Digest>& leaves);

// ---- the CPU backend, reachable on purpose ---------------------------------
//
// Named so a differential can ask the same question of both backends in one
// process. Nothing in a chain should call these: a chain calls the seam and
// lets the seam decide.
namespace cpu {
Digest keccak256(std::span<const Bytes> parts);
std::vector<Digest> keccak256_batch(std::span<const Bytes> inputs);
Digest sha256(Bytes data);
Address ripemd160(Bytes data);
std::optional<CompressedKey> recover(const Digest& hash, const Signature& sig);
}  // namespace cpu

namespace internal {
// The comparison LUX_GPU=verify makes before it aborts. Exposed so a test can
// hand it a wrong answer and watch it say so — a comparison nothing has ever
// seen fail is a comparison nobody has checked can fail.
bool same(const std::vector<Digest>& got, const std::vector<Digest>& want);
}  // namespace internal

}  // namespace lux::gpu
