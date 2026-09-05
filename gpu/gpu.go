// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

// Package gpu is the one place a Lux chain asks for a primitive.
//
// # The boundary
//
// Kernels are private and live in their own repository. This package contains
// no kernel source and never will: what crosses the line is a C ABI — four
// symbol names — opened at run time if the library happens to be installed.
//
// # The rule
//
// The CPU backend is COMPLETE. A node with nothing installed computes every
// primitive itself and is a full node, not a degraded one. The plugin is a
// strict positive overlay: it may only ever be faster, never different. Two
// backends that disagree about a hash disagree about a block, so every plugin
// answer is either byte-identical to the CPU answer or a bug.
//
// # The knob
//
// LUX_GPU picks the policy, once, at first use:
//
//	off      CPU only; nothing is opened.
//	on       plugin for batch work when a library is installed. The default,
//	         and it degrades to off on its own when nothing is installed.
//	verify   compute BOTH and stop on the first byte that differs.
//
// LUX_GPU_LIB names the library path when it is not on the loader's path.
//
// # What actually has two paths
//
//   - Keccak256Batch — CPU and plugin (lux_gpu_keccak256_batch), and so
//     MerkleRoot, which is a batch of keccaks per level.
//   - Sha256, Ripemd160 — CPU only. The plugin ABI has no such op: its hash
//     surface is keccak256, SHA3-256, SHAKE256, BLAKE3, Poseidon2, and
//     SHA3-256 is not SHA-256.
//   - Recover — CPU only. The plugin's lux_gpu_ecrecover_batch answers a
//     different question: it returns an Ethereum address, keccak(Q.x‖Q.y)[12:],
//     where a Lux address is ripemd160(sha256(compressed Q)). The recovered
//     key never leaves that call, so its answer cannot be turned into this
//     one's.
//
// # A note on CGO
//
// A CGO_ENABLED=0 build has no way to open a shared library, so it never has a
// plugin. That is the point: it is still a whole node.
package gpu

import (
	"fmt"
	"os"
	"sync"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/keccak256"
	"github.com/luxfi/crypto/ripemd160"
	"github.com/luxfi/crypto/secp256k1"
)

// Hash256 is a 256-bit digest.
type Hash256 = [32]byte

// Hash160 is a 160-bit digest — the width of an address.
type Hash160 = [20]byte

// SignatureLen is a secp256k1 signature: 64 bytes of (r,s) plus the recovery
// byte.
const SignatureLen = 65

// The RFC 6962 domain tags. leaf(d) = keccak(0x00 ‖ d),
// node(L,R) = keccak(0x01 ‖ L ‖ R). They separate the domains, so a leaf
// preimage can never be read as a node's.
const (
	LeafTag byte = 0x00
	NodeTag byte = 0x01
)

// Policy is what LUX_GPU selected.
type Policy int

const (
	// Off means CPU only. Nothing is opened.
	Off Policy = iota
	// On means plugin when installed, CPU otherwise.
	On
	// Verify means both, compared, every call.
	Verify
)

var policy = sync.OnceValue(func() Policy {
	switch os.Getenv("LUX_GPU") {
	case "off", "OFF", "0":
		return Off
	case "verify", "VERIFY":
		return Verify
	default:
		return On
	}
})

// Backend names which backend is answering: "cpu", or "plugin:<name>" naming
// the device the installed library selected for itself.
//
// This is a diagnostic, and it is the honest one: a host with the library
// installed but no device driver reports "plugin:cpu", not "plugin:cuda".
func Backend() string {
	if policy() == Off {
		return "cpu"
	}
	if n, ok := pluginName(); ok {
		return "plugin:" + n
	}
	return "cpu"
}

// ---- hashes ----------------------------------------------------------------

// Keccak256 is Ethereum Keccak-256 — the 0x01 pad, NOT FIPS-202 SHA3 — over
// the concatenation of the parts.
//
// One hash is not a batch: there is nothing to parallelise and a dispatch costs
// more than the answer. This is the CPU backend, always.
func Keccak256(parts ...[]byte) Hash256 { return cpuKeccak256(parts...) }

// Keccak256Batch is one Keccak-256 per input — the shape that has somewhere
// else to go.
func Keccak256Batch(inputs [][]byte) []Hash256 {
	switch policy() {
	case Off:
		return cpuKeccak256Batch(inputs)
	case Verify:
		want := cpuKeccak256Batch(inputs)
		if got, ok := pluginKeccak256Batch(inputs); ok {
			agree(got, want, inputs)
		}
		return want
	default:
		if got, ok := pluginKeccak256Batch(inputs); ok {
			return got
		}
		return cpuKeccak256Batch(inputs)
	}
}

// Sha256 names things: a transaction id is the hash of its signed bytes and a
// block id the hash of its block bytes.
func Sha256(b []byte) Hash256 { return hash.ComputeHash256Array(b) }

// Ripemd160 is the second half of an address.
func Ripemd160(b []byte) Hash160 { return ripemd160.Sum160(b) }

// PubkeyToAddress is the address of a public key: RIPEMD-160 of its SHA-256.
func PubkeyToAddress(key []byte) Hash160 {
	h := Sha256(key)
	return hash.ComputeHash160Array(h[:])
}

// ---- signatures ------------------------------------------------------------

// Recover returns the compressed public key that signed h, or false.
//
// The 65 bytes are r ‖ s ‖ recovery_id, and the answer is the 33-byte
// compressed SEC1 encoding — the shape a Lux address is derived from, which is
// not the shape the EVM ecrecover precompile returns.
func Recover(h Hash256, sig [SignatureLen]byte) ([]byte, bool) {
	key, err := secp256k1.RecoverPublicKeyFromHash(h[:], sig[:])
	if err != nil {
		return nil, false
	}
	return key.Bytes(), true
}

// ---- the RFC 6962 tagged binary Merkle fold --------------------------------

// LeafHash is keccak256(0x00 ‖ d) — a tagged leaf.
func LeafHash(d Hash256) Hash256 { return Keccak256([]byte{LeafTag}, d[:]) }

// NodeHash is keccak256(0x01 ‖ L ‖ R) — a tagged internal node.
func NodeHash(l, r Hash256) Hash256 { return Keccak256([]byte{NodeTag}, l[:], r[:]) }

// EmptyRoot is keccak256("") — the root of nothing.
func EmptyRoot() Hash256 { return Keccak256() }

// MerkleRoot folds a dense, already-compacted, ascending leaf-digest list. A
// lone right node is promoted unchanged rather than paired with itself, which
// is what keeps two different leaf sets from folding to one root.
//
// A level is a batch: every node on it is independent of every other, which is
// the only reason a device can help at all. The preimages are built here and
// hashed together, so the whole level makes one dispatch.
func MerkleRoot(leaves []Hash256) Hash256 {
	if len(leaves) == 0 {
		return EmptyRoot()
	}

	pre := make([][]byte, len(leaves))
	for i, d := range leaves {
		p := make([]byte, 0, 33)
		p = append(p, LeafTag)
		p = append(p, d[:]...)
		pre[i] = p
	}
	level := Keccak256Batch(pre)

	for len(level) > 1 {
		cnt := len(level)
		parents := (cnt + 1) / 2
		pairs := cnt / 2

		pre := make([][]byte, pairs)
		for j := 0; j < pairs; j++ {
			p := make([]byte, 0, 65)
			p = append(p, NodeTag)
			p = append(p, level[2*j][:]...)
			p = append(p, level[2*j+1][:]...)
			pre[j] = p
		}
		next := Keccak256Batch(pre)
		for len(next) < parents {
			next = append(next, Hash256{})
		}
		if cnt%2 == 1 {
			// Lone right node: promoted unchanged, never doubled.
			next[parents-1] = level[cnt-1]
		}
		level = next
	}
	return level[0]
}

// ---- the CPU backend, reachable on purpose ---------------------------------
//
// Named so a differential can ask the same question of both backends in one
// process. Nothing in a chain should call these: a chain calls the seam and
// lets the seam decide.

// CPUKeccak256 is Keccak256 with the plugin never consulted.
func CPUKeccak256(parts ...[]byte) Hash256 { return cpuKeccak256(parts...) }

// CPUKeccak256Batch is Keccak256Batch with the plugin never consulted.
func CPUKeccak256Batch(inputs [][]byte) []Hash256 { return cpuKeccak256Batch(inputs) }

func cpuKeccak256(parts ...[]byte) Hash256 { return keccak256.Concat(parts...) }

// Same is the comparison LUX_GPU=verify makes before it stops. Exported so a
// test can hand it a wrong answer and watch it say so — a comparison nothing
// has ever seen fail is a comparison nobody has checked can fail.
func Same(got, want []Hash256) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// agree stops the process when the two backends do not answer the same thing.
// A node that keeps going after that is a node building on a block half the
// network will not have.
func agree(got, want []Hash256, inputs [][]byte) {
	if Same(got, want) {
		return
	}
	if len(got) != len(want) {
		panic(fmt.Sprintf("LUX_GPU=verify: plugin returned %d digests for a batch of %d",
			len(got), len(want)))
	}
	for i := range want {
		if got[i] != want[i] {
			panic(fmt.Sprintf(
				"LUX_GPU=verify: plugin and CPU disagree on keccak256 of input %d (%d bytes): "+
					"plugin %x vs cpu %x — this is a consensus bug, not a performance one",
				i, len(inputs[i]), got[i], want[i]))
		}
	}
}

func cpuKeccak256Batch(inputs [][]byte) []Hash256 {
	out := make([]Hash256, len(inputs))
	for i, in := range inputs {
		out[i] = keccak256.Concat(in)
	}
	return out
}
