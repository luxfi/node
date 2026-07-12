// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package bridgevmroot computes the bridgevm_state_root in native Go,
// byte-identical to the lux-private GPU bridgevm_transition kernel
// (ops/bridgevm/cuda/bridgevm_transition.cu + bridgevm_kernels_common.cuh) and
// every backend's CPU oracle. It is the native-Go authority the licensed GPU
// accelerator mirrors: the same five family leaf preimages, the same per-family
// skip predicates, the same ascending-i compacted-occupied ordering, the same
// RFC-6962 tagged binary Merkle fold for the five variable-length sub-roots, and
// the same fixed-shape keccak256 composition for the final bridgevm_state_root.
//
// It is the symmetric peer of vms/xvm/state/xvmroot (xvm execution_root): same
// crypto, same combiner, same value-struct decoupling. Where xvmroot has three
// families (utxo / asset / tx), bridgevm has five (signer_set / liquidity /
// inbox / outbox / daily_limit).
//
// The root is a pure function of the canonical leaf FIELD VALUES, not of any
// in-memory struct. So this package consumes the accelerator's state-snapshot
// layout directly — SignerLeaf, LiquidityLeaf, MessageLeaf, DailyLeaf mirror the
// GPU structs' hashed fields exactly, in the exact preimage order. The leaves
// are the POST-transition values (the GPU's K1 mutates signer status / resets
// daily limits before hashing); callers supply the already-mutated field values,
// exactly as the GPU hashes them.
//
// The leaf/node/empty Merkle combiner lives in github.com/luxfi/crypto/merkle
// (RFC-6962: leaf=keccak256(0x00‖d), node=keccak256(0x01‖L‖R), lone-right
// promotion, empty=keccak256("")). The un-tagged keccak256 (Ethereum Keccak-256,
// 0x01 pad — NOT FIPS-202 SHA3, i.e. sha3.NewLegacyKeccak256) used for both the
// leaf-digest preimage and the bridgevm_state_root compose lives in
// github.com/luxfi/crypto/hash. Integers are little-endian, matching the
// kernel's absorb_u32 / absorb_u64.
//
// CRITICAL: the active-bond aggregate is a FULL 128-bit value (BondLo + BondHi
// with carry) — the GPU was fixed (MED-1 / "FIX 4") to accumulate both limbs and
// commit BondHi into both the signer leaf and the compose preimage. This package
// matches that: it accumulates the full 128-bit sum and binds both BondLo and
// BondHi into the composed root. Dropping BondHi would diverge from the GPU.
//
// Normative spec: lux-private/gpu-kernels/backends/common/lux_merkle_fold.hpp,
// ops/bridgevm/cuda/bridgevm_transition.cu, and the CPU oracle in
// backends/vulkan/tests/test_bridgevm_transition_kat.cpp. KAT parity is proven
// in bridgevmroot_test.go against the values all GPU backends + the CPU oracle
// produce.
package bridgevmroot

import (
	"encoding/binary"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/merkle"
)

// Size is the byte length of every root and element digest (a keccak256 output).
const Size = hash.KeccakSize

// Signer status bits — match bridgevm_kernels_common.cuh kSignerStatus*. A
// signer is folded into signer_set_root iff Occupied != 0; the status bits below
// only affect the active-bond aggregate (active = Active set, Jailed clear,
// Tombstoned clear), never inclusion.
const (
	SignerStatusActive     uint32 = 0x1
	SignerStatusJailed     uint32 = 0x2
	SignerStatusTombstoned uint32 = 0x4
	SignerStatusPendingAdd uint32 = 0x8
	SignerStatusPendingDrop uint32 = 0x10
	SignerStatusExiting    uint32 = 0x20
)

// SignerLeaf is one bridgevm signer in the accelerator's state-snapshot layout,
// holding the POST-transition field values. Its fields are hashed, in this exact
// order, into the leaf preimage:
//
//	SignerID ‖ UTXOAddr[20] ‖ 0u32 ‖ BondLo ‖ BondHi ‖ OptInHeight ‖
//	ExitEpoch ‖ SignCount ‖ BLSPubkey[48] ‖ CoronaPubkey[32] ‖ MLDSAPubkey[32] ‖
//	Status ‖ JailUntilEpoch ‖ SlashCount ‖ index   (integers little-endian)
//
// A signer with Occupied == 0 is skipped (not folded), exactly as the kernel
// skips unoccupied slots. The 0u32 after UTXOAddr is the GPU struct's
// _pad_addr, committed as four zero bytes.
type SignerLeaf struct {
	SignerID       uint64
	UTXOAddr     [20]byte
	BondLo         uint64
	BondHi         uint64
	OptInHeight    uint64
	ExitEpoch      uint64
	SignCount      uint64
	BLSPubkey      [48]byte
	CoronaPubkey   [32]byte
	MLDSAPubkey    [32]byte
	Status         uint32
	JailUntilEpoch uint32
	SlashCount     uint32
	Occupied       uint32
}

// LiquidityLeaf is one bridgevm liquidity entry in the accelerator's
// state-snapshot layout. Its fields are hashed, in this exact order, into the
// leaf preimage:
//
//	ProviderAddr[20] ‖ 0u32 ‖ AssetID ‖ Status ‖ AmountLo ‖ AmountHi ‖
//	FeeAccrualLo ‖ FeeAccrualHi ‖ index   (integers little-endian)
//
// An entry with Status == 0 is skipped, matching the kernel's status-occupied
// check. The 0u32 after ProviderAddr is the GPU struct's _pad_addr.
type LiquidityLeaf struct {
	ProviderAddr [20]byte
	AssetID      uint32
	Status       uint32
	AmountLo     uint64
	AmountHi     uint64
	FeeAccrualLo uint64
	FeeAccrualHi uint64
}

// MessageLeaf is one bridgevm message (inbox or outbox) in the accelerator's
// state-snapshot layout. Its fields are hashed, in this exact order, into the
// leaf preimage:
//
//	MsgID[32] ‖ PayloadRoot[32] ‖ Nonce ‖ SrcChain ‖ DstChain ‖ Kind ‖
//	Status ‖ AmountLo ‖ AmountHi ‖ index   (integers little-endian)
//
// A message that is FREE — Status == 0 AND MsgID is all-zero — is skipped,
// matching the kernel's free-slot predicate. (A nonzero MsgID OR a nonzero
// Status makes the slot live and folded.) The agg_signature, signers_bitmap,
// asset_id, signer_count and arrival_height fields of the GPU Message struct are
// NOT part of the leaf preimage and so are absent here.
type MessageLeaf struct {
	MsgID       [32]byte
	PayloadRoot [32]byte
	Nonce       uint64
	SrcChain    uint32
	DstChain    uint32
	Kind        uint32
	Status      uint32
	AmountLo    uint64
	AmountHi    uint64
}

// DailyLeaf is one bridgevm daily-limit entry in the accelerator's
// state-snapshot layout, holding the POST-reset field values. Its fields are
// hashed, in this exact order, into the leaf preimage:
//
//	AssetID ‖ Status ‖ DailyCapLo ‖ DailyCapHi ‖ UsedTodayLo ‖ UsedTodayHi ‖
//	index   (integers little-endian)
//
// An entry with Status == 0 is skipped, matching the kernel's status-occupied
// check.
type DailyLeaf struct {
	AssetID     uint32
	Status      uint32
	DailyCapLo  uint64
	DailyCapHi  uint64
	UsedTodayLo uint64
	UsedTodayHi uint64
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

// signerLeafDigest = keccak256(preimage) over the signer leaf preimage at slot i.
// This is the per-element digest d_i the GPU's K1 signer-leaf hash produces.
func signerLeafDigest(s SignerLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 8+20+4+8+8+8+8+8+48+32+32+4+4+4+4)
	b = le64(b, s.SignerID)
	b = append(b, s.UTXOAddr[:]...)
	b = le32(b, 0) // _pad_addr
	b = le64(b, s.BondLo)
	b = le64(b, s.BondHi)
	b = le64(b, s.OptInHeight)
	b = le64(b, s.ExitEpoch)
	b = le64(b, s.SignCount)
	b = append(b, s.BLSPubkey[:]...)
	b = append(b, s.CoronaPubkey[:]...)
	b = append(b, s.MLDSAPubkey[:]...)
	b = le32(b, s.Status)
	b = le32(b, s.JailUntilEpoch)
	b = le32(b, s.SlashCount)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// liquidityLeafDigest = keccak256(preimage) over the liquidity leaf preimage at
// slot i.
func liquidityLeafDigest(s LiquidityLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 20+4+4+4+8+8+8+8+4)
	b = append(b, s.ProviderAddr[:]...)
	b = le32(b, 0) // _pad_addr
	b = le32(b, s.AssetID)
	b = le32(b, s.Status)
	b = le64(b, s.AmountLo)
	b = le64(b, s.AmountHi)
	b = le64(b, s.FeeAccrualLo)
	b = le64(b, s.FeeAccrualHi)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// messageLeafDigest = keccak256(preimage) over the message leaf preimage at slot
// i. Used identically for inbox and outbox (one shared preimage layout).
func messageLeafDigest(m MessageLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 32+32+8+4+4+4+4+8+8+4)
	b = append(b, m.MsgID[:]...)
	b = append(b, m.PayloadRoot[:]...)
	b = le64(b, m.Nonce)
	b = le32(b, m.SrcChain)
	b = le32(b, m.DstChain)
	b = le32(b, m.Kind)
	b = le32(b, m.Status)
	b = le64(b, m.AmountLo)
	b = le64(b, m.AmountHi)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// dailyLeafDigest = keccak256(preimage) over the daily-limit leaf preimage at
// slot i.
func dailyLeafDigest(s DailyLeaf, i uint32) [Size]byte {
	b := make([]byte, 0, 4+4+8+8+8+8+4)
	b = le32(b, s.AssetID)
	b = le32(b, s.Status)
	b = le64(b, s.DailyCapLo)
	b = le64(b, s.DailyCapHi)
	b = le64(b, s.UsedTodayLo)
	b = le64(b, s.UsedTodayHi)
	b = le32(b, i)
	return hash.ComputeKeccak256Array(b)
}

// messageLive reports whether a message slot is folded: live iff NOT (Status == 0
// AND MsgID all-zero). Matches the kernel's free-slot skip predicate.
func messageLive(m MessageLeaf) bool {
	if m.Status != 0 {
		return true
	}
	for _, c := range m.MsgID {
		if c != 0 {
			return true
		}
	}
	return false
}

// SignerSetRoot is the tagged binary Merkle root over the occupied signers' leaf
// digests, in ascending slot index. A signer with Occupied == 0 is skipped. An
// empty occupied set yields keccak256("") (merkle.EmptyRoot).
func SignerSetRoot(signers []SignerLeaf) [Size]byte {
	leaves := make([][Size]byte, 0, len(signers))
	for i := range signers {
		if signers[i].Occupied == 0 {
			continue
		}
		leaves = append(leaves, signerLeafDigest(signers[i], uint32(i)))
	}
	return merkle.Root(leaves)
}

// LiquidityRoot is the tagged binary Merkle root over the active liquidity
// entries' leaf digests, in ascending slot index. An entry with Status == 0 is
// skipped.
func LiquidityRoot(entries []LiquidityLeaf) [Size]byte {
	leaves := make([][Size]byte, 0, len(entries))
	for i := range entries {
		if entries[i].Status == 0 {
			continue
		}
		leaves = append(leaves, liquidityLeafDigest(entries[i], uint32(i)))
	}
	return merkle.Root(leaves)
}

// InboxRoot is the tagged binary Merkle root over the live inbox messages' leaf
// digests, in ascending slot index. A free slot (Status == 0 && MsgID all-zero)
// is skipped.
func InboxRoot(inbox []MessageLeaf) [Size]byte {
	return messageRoot(inbox)
}

// OutboxRoot is the tagged binary Merkle root over the live outbox messages' leaf
// digests, in ascending slot index. A free slot (Status == 0 && MsgID all-zero)
// is skipped.
func OutboxRoot(outbox []MessageLeaf) [Size]byte {
	return messageRoot(outbox)
}

// messageRoot is the shared inbox/outbox fold (one preimage layout, one skip
// predicate).
func messageRoot(msgs []MessageLeaf) [Size]byte {
	leaves := make([][Size]byte, 0, len(msgs))
	for i := range msgs {
		if !messageLive(msgs[i]) {
			continue
		}
		leaves = append(leaves, messageLeafDigest(msgs[i], uint32(i)))
	}
	return merkle.Root(leaves)
}

// DailyLimitRoot is the tagged binary Merkle root over the active daily-limit
// entries' leaf digests, in ascending slot index. An entry with Status == 0 is
// skipped.
func DailyLimitRoot(entries []DailyLeaf) [Size]byte {
	leaves := make([][Size]byte, 0, len(entries))
	for i := range entries {
		if entries[i].Status == 0 {
			continue
		}
		leaves = append(leaves, dailyLeafDigest(entries[i], uint32(i)))
	}
	return merkle.Root(leaves)
}

// ActiveBond returns the FULL 128-bit aggregate Σ(BondLo : BondHi) over active
// signers (Active set, Jailed clear, Tombstoned clear), as a (lo, hi) pair with
// carry, alongside the active-signer count. This is the GPU's post-FIX-4
// accumulation: BondHi is carried, never dropped. Used by Compose and surfaced
// for callers that need the epoch counters. Occupied == 0 slots do not
// contribute.
func ActiveBond(signers []SignerLeaf) (bondLo, bondHi uint64, active uint32) {
	for i := range signers {
		s := &signers[i]
		if s.Occupied == 0 {
			continue
		}
		if s.Status&SignerStatusActive != 0 &&
			s.Status&SignerStatusJailed == 0 &&
			s.Status&SignerStatusTombstoned == 0 {
			active++
			old := bondLo
			bondLo += s.BondLo
			var carry uint64
			if bondLo < old { // carry out of the 64-bit lo sum
				carry = 1
			}
			bondHi += s.BondHi + carry // 128-bit total high word
		}
	}
	return bondLo, bondHi, active
}

// Compose returns the bridgevm_state_root from the parent root, the five
// sub-roots, and the epoch counters, using the fixed-shape un-tagged keccak256
// composition (this is NOT a Merkle node):
//
//	bridgevm_state_root = keccak256(
//	    parentStateRoot ‖ signerSetRoot ‖ liquidityRoot ‖ inboxRoot ‖
//	    outboxRoot ‖ dailyLimitRoot ‖ epoch_u64_le ‖ bondLo_u64_le ‖
//	    bondHi_u64_le ‖ active_u32_le )
//
// epoch is the finalized current epoch (target epoch on a closing round, else the
// unchanged current epoch). bondLo/bondHi are the full 128-bit active-bond
// aggregate; active is the active-signer count.
func Compose(
	parentStateRoot, signerSetRoot, liquidityRoot, inboxRoot, outboxRoot, dailyLimitRoot [Size]byte,
	epoch, bondLo, bondHi uint64,
	active uint32,
) [Size]byte {
	var e, bl, bh [8]byte
	var a [4]byte
	binary.LittleEndian.PutUint64(e[:], epoch)
	binary.LittleEndian.PutUint64(bl[:], bondLo)
	binary.LittleEndian.PutUint64(bh[:], bondHi)
	binary.LittleEndian.PutUint32(a[:], active)
	return hash.ComputeKeccak256Array(
		parentStateRoot[:],
		signerSetRoot[:],
		liquidityRoot[:],
		inboxRoot[:],
		outboxRoot[:],
		dailyLimitRoot[:],
		e[:],
		bl[:],
		bh[:],
		a[:],
	)
}

// StateRoot computes the full bridgevm_state_root over the round's POST-transition
// state: the five sub-roots (signer_set, liquidity, inbox, outbox, daily_limit)
// folded as RFC-6962 tagged binary Merkle trees, the full 128-bit active-bond
// aggregate computed over the signer set, then composed with the parent state
// root and finalized epoch. It is byte-identical to lux_<backend>_bridgevm_transition.
//
// epoch is the finalized current epoch the GPU writes into the compose preimage
// (target_epoch = desc.epoch + closing_flag, used as current_epoch on a closing
// round; otherwise the carried current_epoch). The caller resolves it from the
// round descriptor exactly as the kernel does.
func StateRoot(
	parentStateRoot [Size]byte,
	signers []SignerLeaf,
	liquidity []LiquidityLeaf,
	inbox []MessageLeaf,
	outbox []MessageLeaf,
	daily []DailyLeaf,
	epoch uint64,
) (stateRoot, signerSetRoot, liquidityRoot, inboxRoot, outboxRoot, dailyLimitRoot [Size]byte, bondLo, bondHi uint64, active uint32) {
	signerSetRoot = SignerSetRoot(signers)
	liquidityRoot = LiquidityRoot(liquidity)
	inboxRoot = InboxRoot(inbox)
	outboxRoot = OutboxRoot(outbox)
	dailyLimitRoot = DailyLimitRoot(daily)
	bondLo, bondHi, active = ActiveBond(signers)
	stateRoot = Compose(
		parentStateRoot,
		signerSetRoot, liquidityRoot, inboxRoot, outboxRoot, dailyLimitRoot,
		epoch, bondLo, bondHi, active,
	)
	return stateRoot, signerSetRoot, liquidityRoot, inboxRoot, outboxRoot, dailyLimitRoot, bondLo, bondHi, active
}
