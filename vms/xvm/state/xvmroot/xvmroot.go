// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package xvmroot computes the xvm execution_root in native Go, byte-identical
// to the lux-private GPU xvm_root_update kernel (ops/xvm/cuda/xvm_roots.cu) and
// every backend's CPU oracle. It is the native-Go authority the licensed GPU
// accelerator mirrors: the same leaf preimages, the same per-family skip
// predicates, the same ascending-i ordering, the same RFC-6962 tagged binary
// Merkle fold for the three variable-length sub-roots, and the same fixed-shape
// keccak256 composition for the final execution_root.
//
// The root is a pure function of the canonical leaf FIELD VALUES, not of any
// in-memory struct. The xvm executor's UTXO/Asset/Tx types are Avalanche-style
// (output interfaces, codec-serialized) and deliberately do NOT share the GPU's
// flat packed layout; the accelerator hashes a state-snapshot layout. So this
// package consumes that snapshot layout directly — UTXOLeaf, AssetLeaf, TxLeaf
// mirror the GPU structs' hashed fields exactly, in the exact preimage order.
// The leaf/node/empty Merkle combiner lives in github.com/luxfi/crypto/merkle;
// the un-tagged keccak256 (Ethereum Keccak-256, 0x01 pad — NOT FIPS-202 SHA3)
// used for both the leaf-digest preimage and the execution_root compose lives
// in github.com/luxfi/crypto/hash. Integers are little-endian, matching the
// kernel's absorb_u32 / absorb_u64.
//
// Normative spec: lux-private/gpu-kernels/backends/common/lux_merkle_fold.hpp
// and ops/xvm/cuda/xvm_roots.cu. KAT parity is proven in xvmroot_test.go
// against the value all seven GPU backends + the CPU oracle produce.
package xvmroot

import (
	"encoding/binary"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/merkle"
)

// Size is the byte length of every root and element digest (a keccak256 output).
const Size = hash.KeccakSize

// UTXOOccupied is the bit in UTXOLeaf.Status marking a slot occupied. A UTXO is
// included in utxo_root iff (Status & UTXOOccupied) != 0 — matching the kernel's
// kUtxoOccupied skip predicate.
const UTXOOccupied uint32 = 0x1

// UTXOLeaf is one xvm UTXO in the accelerator's state-snapshot layout. Its
// fields are hashed, in this exact order, into the leaf preimage:
//
//	UTXOID ‖ AssetID ‖ AmountLo ‖ AmountHi ‖ OwnerRoot ‖ Locktime ‖
//	Threshold ‖ Status ‖ index   (integers little-endian)
//
// A UTXO with (Status & UTXOOccupied) == 0 is skipped (not folded), exactly as
// the kernel skips unoccupied slots.
type UTXOLeaf struct {
	UTXOID    [32]byte
	AssetID   [32]byte
	AmountLo  uint64
	AmountHi  uint64
	OwnerRoot [32]byte
	Locktime  uint64
	Threshold uint32
	Status    uint32
}

// AssetLeaf is one xvm asset in the accelerator's state-snapshot layout. Its
// fields are hashed, in this exact order, into the leaf preimage:
//
//	AssetID ‖ TotalSupplyLo ‖ TotalSupplyHi ‖ MintAuthorityRoot ‖
//	FreezeFlag ‖ Denomination ‖ index   (integers little-endian)
//
// An asset with Occupied == 0 is skipped, matching the kernel's occupied check.
type AssetLeaf struct {
	AssetID           [32]byte
	TotalSupplyLo     uint64
	TotalSupplyHi     uint64
	MintAuthorityRoot [32]byte
	FreezeFlag        uint32
	Denomination      uint32
	Occupied          uint32
}

// TxLeaf is one xvm transaction in the accelerator's state-snapshot layout. Its
// fields are hashed, in this exact order, into the leaf preimage:
//
//	TxID ‖ Kind ‖ Status ‖ RejectReason ‖ ProofDigest ‖ index
//	(integers little-endian)
//
// Every tx is folded — tx_root has no skip predicate.
type TxLeaf struct {
	TxID         [32]byte
	Kind         uint32
	Status       uint32
	RejectReason uint32
	ProofDigest  [32]byte
}

// le32 / le64 append a little-endian integer, matching the kernel's absorb_u32 /
// absorb_u64 (dst[off+k] = (v >> (k*8)) & 0xFF).
func le32(b []byte, v uint32) []byte {
	var x [4]byte
	binary.LittleEndian.PutUint32(x[:], v)
	return append(b, x[:]...)
}

func le64(b []byte, v uint64) []byte {
	var x [8]byte
	binary.LittleEndian.PutUint64(x[:], v)
	return append(b, x[:]...)
}

// utxoLeafDigest = keccak256(preimage) over the UTXO leaf preimage at slot i.
// This is the per-element digest d_i the GPU's K1 leaf kernel produces.
func utxoLeafDigest(u UTXOLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 32+32+8+8+32+8+4+4+4)
	b = append(b, u.UTXOID[:]...)
	b = append(b, u.AssetID[:]...)
	b = le64(b, u.AmountLo)
	b = le64(b, u.AmountHi)
	b = append(b, u.OwnerRoot[:]...)
	b = le64(b, u.Locktime)
	b = le32(b, u.Threshold)
	b = le32(b, u.Status)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// assetLeafDigest = keccak256(preimage) over the asset leaf preimage at slot i.
func assetLeafDigest(a AssetLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 32+8+8+32+4+4+4)
	b = append(b, a.AssetID[:]...)
	b = le64(b, a.TotalSupplyLo)
	b = le64(b, a.TotalSupplyHi)
	b = append(b, a.MintAuthorityRoot[:]...)
	b = le32(b, a.FreezeFlag)
	b = le32(b, a.Denomination)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// txLeafDigest = keccak256(preimage) over the tx leaf preimage at slot i.
func txLeafDigest(t TxLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 32+4+4+4+32+4)
	b = append(b, t.TxID[:]...)
	b = le32(b, t.Kind)
	b = le32(b, t.Status)
	b = le32(b, t.RejectReason)
	b = append(b, t.ProofDigest[:]...)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// UTXOLeafDigest returns the canonical per-element keccak256 leaf digest d_i for
// the UTXO at slot index i — the same bytes UTXORoot folds. It is exported so a
// parallel builder can compute the family's leaf digests across worker
// goroutines from the one canonical preimage definition (no second copy of the
// layout), then merkle.Root-combine to a byte-identical root.
func UTXOLeafDigest(u UTXOLeaf, i uint32) [Size]byte { return utxoLeafDigest(u, i) }

// AssetLeafDigest returns the canonical per-element keccak256 leaf digest d_i for
// the asset at slot index i — the same bytes AssetRoot folds.
func AssetLeafDigest(a AssetLeaf, i uint32) [Size]byte { return assetLeafDigest(a, i) }

// TxLeafDigest returns the canonical per-element keccak256 leaf digest d_i for
// the tx at slot index i — the same bytes TxRoot folds.
func TxLeafDigest(t TxLeaf, i uint32) [Size]byte { return txLeafDigest(t, i) }

// UTXORoot is the tagged binary Merkle root over the occupied UTXOs' leaf
// digests, in ascending slot index. Slot index i is the position in utxos
// (bound into each leaf preimage); a UTXO with (Status & UTXOOccupied) == 0 is
// skipped. An empty occupied set yields keccak256("") (merkle.EmptyRoot).
func UTXORoot(utxos []UTXOLeaf) [Size]byte {
	leaves := make([][Size]byte, 0, len(utxos))
	for i := range utxos {
		if utxos[i].Status&UTXOOccupied == 0 {
			continue
		}
		leaves = append(leaves, utxoLeafDigest(utxos[i], uint32(i)))
	}
	return merkle.Root(leaves)
}

// AssetRoot is the tagged binary Merkle root over the occupied assets' leaf
// digests, in ascending slot index. An asset with Occupied == 0 is skipped.
func AssetRoot(assets []AssetLeaf) [Size]byte {
	leaves := make([][Size]byte, 0, len(assets))
	for i := range assets {
		if assets[i].Occupied == 0 {
			continue
		}
		leaves = append(leaves, assetLeafDigest(assets[i], uint32(i)))
	}
	return merkle.Root(leaves)
}

// TxRoot is the tagged binary Merkle root over ALL txs' leaf digests, in
// ascending slot index (no skip predicate).
func TxRoot(txs []TxLeaf) [Size]byte {
	leaves := make([][Size]byte, len(txs))
	for i := range txs {
		leaves[i] = txLeafDigest(txs[i], uint32(i))
	}
	return merkle.Root(leaves)
}

// Compose returns the execution_root from the three sub-roots and the parent
// root + height, using the fixed-shape un-tagged keccak256 composition (this is
// NOT a Merkle node):
//
//	execution_root = keccak256(
//	    parentExecutionRoot ‖ utxoRoot ‖ assetRoot ‖ txRoot ‖ height_u64_le )
func Compose(parentExecutionRoot, utxoRoot, assetRoot, txRoot [Size]byte, height uint64) [Size]byte {
	var h [8]byte
	binary.LittleEndian.PutUint64(h[:], height)
	return hash.ComputeKeccak256Array(
		parentExecutionRoot[:],
		utxoRoot[:],
		assetRoot[:],
		txRoot[:],
		h[:],
	)
}

// ExecutionRoot computes the full xvm execution_root over the round's state:
// the three sub-roots (utxo, asset, tx) folded as RFC-6962 tagged binary Merkle
// trees, then composed with the parent execution root and height. It is
// byte-identical to lux_<backend>_xvm_root_update.
func ExecutionRoot(
	parentExecutionRoot [Size]byte,
	utxos []UTXOLeaf,
	assets []AssetLeaf,
	txs []TxLeaf,
	height uint64,
) (executionRoot, utxoRoot, assetRoot, txRoot [Size]byte) {
	utxoRoot = UTXORoot(utxos)
	assetRoot = AssetRoot(assets)
	txRoot = TxRoot(txs)
	executionRoot = Compose(parentExecutionRoot, utxoRoot, assetRoot, txRoot, height)
	return executionRoot, utxoRoot, assetRoot, txRoot
}
