// Package xvm — pure-Go canonical implementation of the four bridge
// transitions. This is the SINGLE source of truth for UTXOTransition,
// AssetTransition, MembershipRebuild, and RootUpdate: the cgo build
// falls back to these functions when the GPU plugin returns an error,
// and the nocgo build calls them directly.
//
// Algorithm fidelity: byte-equal to the C++ device kernels at
// lux-private/gpu-kernels/ops/xvm/cuda/xvm_*.cu (the canonical
// determinism oracle every GPU backend is validated against). The CUDA
// / HIP / Metal / Vulkan / WebGPU kernels at
// lux-private/gpu-kernels/ops/xvm/<backend>/ MUST match the output of
// these Go functions byte-for-byte. If the GPU plugin disagrees with
// the Go impl, the Go impl wins — file a follow-up at the plugin.
//
// One file, no build tag — same code compiles into both build modes.
// The cgo bridge in xvm_gpu.go wraps these helpers with the GPU-first
// dispatch; the nocgo stub in xvm_gpu_nocgo.go calls them directly.
package xvm

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// =============================================================================
// keccak256 — the same keccak that the GPU device code uses. We route
// through golang.org/x/crypto/sha3.NewLegacyKeccak256 which is already a
// transitive dependency of the node (used by luxfi/geth). Wrapping it
// locally keeps the call sites small and the intent visible.
// =============================================================================

func xvmKeccak256(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// =============================================================================
// hashQuad — four u64 lanes from keccak256(utxo_id). Used by the bloom and
// cuckoo helpers. Mirrors hash_quad_local() in xvm_kernels_common.cuh.
// =============================================================================

func xvmHashQuad(utxoID *[32]byte) [4]uint64 {
	h := xvmKeccak256(utxoID[:])
	var out [4]uint64
	for i := 0; i < 4; i++ {
		out[i] = binary.LittleEndian.Uint64(h[i*8 : i*8+8])
	}
	return out
}

// =============================================================================
// Bloom filter — bit-set membership oracle. Bit-equivalent to the GPU
// device code in xvm_kernels_common.cuh.
// =============================================================================

func xvmBloomSet(bits []byte, bitCount uint32, utxoID *[32]byte) {
	if bitCount == 0 {
		return
	}
	hashes := xvmHashQuad(utxoID)
	for i := 0; i < xvmBloomHashes; i++ {
		bit := hashes[i] % uint64(bitCount)
		byteOff := uint32(bit >> 3)
		mask := byte(1 << (uint32(bit) & 7))
		if byteOff < uint32(len(bits)) {
			bits[byteOff] |= mask
		}
	}
}

func xvmBloomTest(bits []byte, bitCount uint32, utxoID *[32]byte) bool {
	if bitCount == 0 {
		return true
	}
	hashes := xvmHashQuad(utxoID)
	for i := 0; i < xvmBloomHashes; i++ {
		bit := hashes[i] % uint64(bitCount)
		byteOff := uint32(bit >> 3)
		mask := byte(1 << (uint32(bit) & 7))
		if byteOff >= uint32(len(bits)) {
			return false
		}
		if bits[byteOff]&mask == 0 {
			return false
		}
	}
	return true
}

// =============================================================================
// Cuckoo arena — slot-index lookup keyed on utxo_id. Mirrors cuckoo_insert
// / cuckoo_query / cuckoo_remove in xvm_kernels_common.cuh.
// =============================================================================

func xvmCuckooBucket(h uint64, bucketCount uint32) uint32 {
	return uint32(h) & (bucketCount - 1)
}

func xvmMemeq32(a, b *[32]byte) bool {
	return *a == *b
}

func xvmCuckooInsert(arena []XVMCuckooEntry, bucketCount uint32, utxoID *[32]byte, slotIndex uint32) bool {
	hashes := xvmHashQuad(utxoID)
	for k := 0; k < 2; k++ {
		b := xvmCuckooBucket(hashes[k], bucketCount)
		for s := uint32(0); s < xvmCuckooSlotsPerBucket; s++ {
			idx := b*xvmCuckooSlotsPerBucket + s
			if idx >= uint32(len(arena)) {
				continue
			}
			e := &arena[idx]
			if e.Occupied != 0 && xvmMemeq32(&e.UtxoID, utxoID) {
				e.SlotIndex = slotIndex
				return true
			}
		}
	}
	for k := 0; k < 2; k++ {
		b := xvmCuckooBucket(hashes[k], bucketCount)
		for s := uint32(0); s < xvmCuckooSlotsPerBucket; s++ {
			idx := b*xvmCuckooSlotsPerBucket + s
			if idx >= uint32(len(arena)) {
				continue
			}
			e := &arena[idx]
			if e.Occupied == 0 {
				e.UtxoID = *utxoID
				e.SlotIndex = slotIndex
				e.Occupied = 1
				return true
			}
		}
	}
	return false
}

func xvmCuckooQuery(arena []XVMCuckooEntry, bucketCount uint32, utxoID *[32]byte) (uint32, bool) {
	hashes := xvmHashQuad(utxoID)
	for k := 0; k < 2; k++ {
		b := xvmCuckooBucket(hashes[k], bucketCount)
		for s := uint32(0); s < xvmCuckooSlotsPerBucket; s++ {
			idx := b*xvmCuckooSlotsPerBucket + s
			if idx >= uint32(len(arena)) {
				continue
			}
			e := &arena[idx]
			if e.Occupied != 0 && xvmMemeq32(&e.UtxoID, utxoID) {
				return e.SlotIndex, true
			}
		}
	}
	return 0, false
}

func xvmCuckooRemove(arena []XVMCuckooEntry, bucketCount uint32, utxoID *[32]byte) {
	hashes := xvmHashQuad(utxoID)
	for k := 0; k < 2; k++ {
		b := xvmCuckooBucket(hashes[k], bucketCount)
		for s := uint32(0); s < xvmCuckooSlotsPerBucket; s++ {
			idx := b*xvmCuckooSlotsPerBucket + s
			if idx >= uint32(len(arena)) {
				continue
			}
			e := &arena[idx]
			if e.Occupied != 0 && xvmMemeq32(&e.UtxoID, utxoID) {
				e.UtxoID = [32]byte{}
				e.SlotIndex = 0
				e.Occupied = 0
				e._pad0 = 0
				return
			}
		}
	}
}

// =============================================================================
// FNV-1a open-addressing locators — assets + export markers. Mirrors
// asset_locate() and export_marker_locate() in xvm_kernels_common.cuh.
// =============================================================================

func xvmFNV1a32(id *[32]byte, mask uint32) uint32 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < 32; i++ {
		h ^= uint64(id[i])
		h *= 0x100000001b3
	}
	return uint32(h) & mask
}

func xvmAssetLocate(tab []XVMAsset, assetID *[32]byte, insertIfMissing bool) uint32 {
	count := uint32(len(tab))
	if count == 0 {
		return 0xFFFFFFFF
	}
	mask := count - 1
	idx := xvmFNV1a32(assetID, mask)
	for probe := uint32(0); probe < count; probe++ {
		a := &tab[idx]
		if a.Occupied == 0 {
			if insertIfMissing {
				a.AssetID = *assetID
				a.TotalSupplyLo = 0
				a.TotalSupplyHi = 0
				a.MintAuthorityRoot = [32]byte{}
				a.FreezeFlag = xvmAssetActive
				a.Denomination = 0
				a.NameOffset = 0
				a.NameLength = 0
				a.Occupied = 1
				a._pad0 = 0
				a._pad1 = 0
				return idx
			}
			return 0xFFFFFFFF
		}
		if xvmMemeq32(&a.AssetID, assetID) {
			return idx
		}
		idx = (idx + 1) & mask
	}
	return 0xFFFFFFFF
}

func xvmExportMarkerLocate(arena []XVMAtomicExportMarker, markerID *[32]byte, insertIfMissing bool) uint32 {
	count := uint32(len(arena))
	if count == 0 {
		return 0xFFFFFFFF
	}
	mask := count - 1
	idx := xvmFNV1a32(markerID, mask)
	for probe := uint32(0); probe < count; probe++ {
		m := &arena[idx]
		if m.Occupied == 0 {
			if insertIfMissing {
				m.MarkerID = *markerID
				m.AssetID = [32]byte{}
				m.AmountLo = 0
				m.AmountHi = 0
				m.SourceChain = 0
				m.TargetChain = 0
				m.Status = xvmExportPending
				m.Occupied = 1
				m.RecipientRoot = [32]byte{}
				m._pad0 = 0
				m._pad1 = 0
				return idx
			}
			return 0xFFFFFFFF
		}
		if xvmMemeq32(&m.MarkerID, markerID) {
			return idx
		}
		idx = (idx + 1) & mask
	}
	return 0xFFFFFFFF
}

// =============================================================================
// 128-bit saturating-ish arithmetic — matches u128_add / u128_sub_ref in
// the GPU common header.
// =============================================================================

func xvmU128Add(lo, hi, addLo, addHi uint64) (uint64, uint64) {
	newLo := lo + addLo
	var carry uint64
	if newLo < lo {
		carry = 1
	}
	return newLo, hi + addHi + carry
}

// xvmU128Sub returns (newLo, newHi, ok). ok=false means underflow (no mutation
// applied at the call site).
func xvmU128Sub(lo, hi, subLo, subHi uint64) (uint64, uint64, bool) {
	if hi < subHi || (hi == subHi && lo < subLo) {
		return lo, hi, false
	}
	var borrow uint64
	if lo < subLo {
		borrow = 1
	}
	return lo - subLo, hi - subHi - borrow, true
}

// =============================================================================
// UTXO arena bump-insert — mirrors utxo_arena_insert_local in the device
// header. Returns 0xFFFFFFFF when full.
// =============================================================================

func xvmUtxoArenaInsert(arena []XVMUTXO, src *XVMUTXO) uint32 {
	for i := range arena {
		s := &arena[i]
		if s.Status&xvmUtxoOccupied == 0 {
			s.UtxoID = src.UtxoID
			s.AssetID = src.AssetID
			s.AmountLo = src.AmountLo
			s.AmountHi = src.AmountHi
			s.OwnerRoot = src.OwnerRoot
			s.Locktime = src.Locktime
			s.Threshold = src.Threshold
			s.AddressesOffset = src.AddressesOffset
			s.AddressesCount = src.AddressesCount
			s.Status = xvmUtxoOccupied
			s._pad0 = 0
			return uint32(i)
		}
	}
	return 0xFFFFFFFF
}

// =============================================================================
// composeMarkerID = keccak256(tx_id || target_chain_le || amount_lo_le || amount_hi_le)
// Mirrors compose_marker_id() in the device header.
// =============================================================================

func xvmComposeMarkerID(txID *[32]byte, targetChain uint32, amountLo, amountHi uint64) [32]byte {
	var buf [32 + 4 + 8 + 8]byte
	o := 0
	copy(buf[o:o+32], txID[:])
	o += 32
	binary.LittleEndian.PutUint32(buf[o:o+4], targetChain)
	o += 4
	binary.LittleEndian.PutUint64(buf[o:o+8], amountLo)
	o += 8
	binary.LittleEndian.PutUint64(buf[o:o+8], amountHi)
	o += 8
	return xvmKeccak256(buf[:o])
}

// =============================================================================
// txInputsHaveDuplicates — sequential pair-wise scan over an input batch.
// Mirrors tx_inputs_have_duplicates_dev in xvm_utxo.cu.
// =============================================================================

func xvmTxInputsHaveDuplicates(inputs []byte, inputOffset, inputCount uint32) bool {
	if inputCount < 2 {
		return false
	}
	for i := uint32(0); i+1 < inputCount; i++ {
		for j := i + 1; j < inputCount; j++ {
			iOff := (inputOffset + i) * 32
			jOff := (inputOffset + j) * 32
			if iOff+32 > uint32(len(inputs)) || jOff+32 > uint32(len(inputs)) {
				continue
			}
			eq := true
			for k := uint32(0); k < 32; k++ {
				if inputs[iOff+k] != inputs[jOff+k] {
					eq = false
					break
				}
			}
			if eq {
				return true
			}
		}
	}
	return false
}

// =============================================================================
// UTXOTransition — pure-Go canonical implementation. Mirrors
// xvm_utxo_transition() in ops/xvm/cuda/xvm_utxo.cu byte-for-byte.
// Sequential walk over `desc.TxCount` txs; mutates txs/utxos/bloom/cuckoo
// in place; returns the inputs/outputs counts via the *_out pointers.
// =============================================================================

func cpuXVMUTXOTransition(
	desc *XVMRoundDescriptor,
	txs []XVMTx,
	inputBatches []XVMInputBatch,
	outputBatches []XVMOutputBatch,
	inputs []byte,
	outputs []XVMUTXO,
	utxos []XVMUTXO,
	bloom []byte,
	cuckoo []XVMCuckooEntry,
	utxoCount, bloomBitCount, cuckooBucketCount uint32,
	inputBatchCount, outputBatchCount, outputsCount uint32,
) (inputsConsumed, outputsCreated uint32) {
	if int(utxoCount) > len(utxos) {
		utxoCount = uint32(len(utxos))
	}
	if int(outputsCount) > len(outputs) {
		outputsCount = uint32(len(outputs))
	}

	height := desc.Height
	count := desc.TxCount
	if int(count) > len(txs) {
		count = uint32(len(txs))
	}

	for ti := uint32(0); ti < count; ti++ {
		tx := &txs[ti]
		var reject bool
		var rejectReason uint32

		var ib *XVMInputBatch
		if tx.InputBatchOffset < inputBatchCount && tx.InputBatchOffset < uint32(len(inputBatches)) {
			ib = &inputBatches[tx.InputBatchOffset]
		}

		if ib != nil {
			if xvmTxInputsHaveDuplicates(inputs, ib.InputOffset, ib.InputCount) {
				reject = true
				rejectReason = xvmRejectDuplicateInput
			}
		}

		// Track consumed slots so we can apply spent-mark + cuckoo-remove
		// after all input checks pass.
		var consumedSlots [256]uint32
		var consumedN uint32

		if !reject && ib != nil {
			for i := uint32(0); i < ib.InputCount; i++ {
				off := (ib.InputOffset + i) * 32
				if off+32 > uint32(len(inputs)) {
					reject = true
					rejectReason = xvmRejectMissingInput
					break
				}
				var uid [32]byte
				copy(uid[:], inputs[off:off+32])
				if !xvmBloomTest(bloom, bloomBitCount, &uid) {
					reject = true
					rejectReason = xvmRejectMissingInput
					break
				}
				slot, ok := xvmCuckooQuery(cuckoo, cuckooBucketCount, &uid)
				if !ok {
					reject = true
					rejectReason = xvmRejectMissingInput
					break
				}
				if slot >= utxoCount {
					reject = true
					rejectReason = xvmRejectMissingInput
					break
				}
				u := &utxos[slot]
				if u.Status&xvmUtxoOccupied == 0 {
					reject = true
					rejectReason = xvmRejectMissingInput
					break
				}
				if u.Status&xvmUtxoSpent != 0 {
					reject = true
					rejectReason = xvmRejectAlreadySpent
					break
				}
				if u.Locktime > height {
					reject = true
					rejectReason = xvmRejectLocktime
					break
				}
				if u.Threshold > 0 && ib.WitnessCount == 0 {
					reject = true
					rejectReason = xvmRejectAuth
					break
				}
				if consumedN < 256 {
					consumedSlots[consumedN] = slot
					consumedN++
				}
			}
		}

		if reject {
			tx.Status = xvmTxStatusRejected
			tx.RejectReason = rejectReason
			continue
		}

		for i := uint32(0); i < consumedN; i++ {
			slot := consumedSlots[i]
			u := &utxos[slot]
			uid := u.UtxoID
			u.Status |= xvmUtxoSpent
			xvmCuckooRemove(cuckoo, cuckooBucketCount, &uid)
			inputsConsumed++
		}

		var arenaFull bool
		if tx.OutputBatchOffset < outputBatchCount && tx.OutputBatchOffset < uint32(len(outputBatches)) {
			ob := &outputBatches[tx.OutputBatchOffset]
			for j := uint32(0); j < ob.OutputCount; j++ {
				off := ob.OutputOffset + j
				if off >= outputsCount {
					break
				}
				src := outputs[off]
				src.Status = xvmUtxoOccupied
				newSlot := xvmUtxoArenaInsert(utxos[:utxoCount], &src)
				if newSlot == 0xFFFFFFFF {
					arenaFull = true
					break
				}
				xvmBloomSet(bloom, bloomBitCount, &src.UtxoID)
				if !xvmCuckooInsert(cuckoo, cuckooBucketCount, &src.UtxoID, newSlot) {
					arenaFull = true
					break
				}
				outputsCreated++
			}
		}
		if arenaFull {
			tx.Status = xvmTxStatusRejected
			tx.RejectReason = xvmRejectArenaFull
			continue
		}

		tx.Status = xvmTxStatusAccepted
		tx.RejectReason = 0
	}

	return inputsConsumed, outputsCreated
}

// =============================================================================
// AssetTransition — pure-Go canonical implementation. Mirrors
// xvm_asset_transition() in ops/xvm/cuda/xvm_asset.cu byte-for-byte.
// =============================================================================

func cpuXVMAssetTransition(
	desc *XVMRoundDescriptor,
	txs []XVMTx,
	assetOps []XVMAssetOp,
	assets []XVMAsset,
	markers []XVMAtomicExportMarker,
	assetOpCount uint32,
) (applied, exports, imports, mintedLo, mintedHi, burnedLo, burnedHi uint32) {
	if int(assetOpCount) > len(assetOps) {
		assetOpCount = uint32(len(assetOps))
	}
	var mintedLo64, mintedHi64 uint64
	var burnedLo64, burnedHi64 uint64

	count := desc.TxCount
	if int(count) > len(txs) {
		count = uint32(len(txs))
	}

	for ti := uint32(0); ti < count; ti++ {
		tx := &txs[ti]
		if tx.Status == xvmTxStatusRejected {
			continue
		}
		if tx.AssetChangesCount == 0 {
			continue
		}
		if tx.AssetChangesOffset >= assetOpCount {
			continue
		}

		txDone := false
		for k := uint32(0); k < tx.AssetChangesCount && !txDone; k++ {
			off := tx.AssetChangesOffset + k
			if off >= assetOpCount {
				break
			}
			op := &assetOps[off]
			assetID := op.AssetID

			aIdx := xvmAssetLocate(assets, &assetID, false)
			if aIdx == 0xFFFFFFFF {
				if op.Kind == xvmAssetOpMint {
					aIdx = xvmAssetLocate(assets, &assetID, true)
					if aIdx == 0xFFFFFFFF {
						tx.Status = xvmTxStatusRejected
						tx.RejectReason = xvmRejectArenaFull
						txDone = true
						break
					}
					assets[aIdx].MintAuthorityRoot = op.AuthorityWitnessRoot
				} else {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectAssetMissing
					txDone = true
					break
				}
			}
			a := &assets[aIdx]

			switch op.Kind {
			case xvmAssetOpMint:
				witness := op.AuthorityWitnessRoot
				if !xvmMemeq32(&a.MintAuthorityRoot, &witness) {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectMintAuthority
					txDone = true
					break
				}
				a.TotalSupplyLo, a.TotalSupplyHi = xvmU128Add(a.TotalSupplyLo, a.TotalSupplyHi, op.AmountLo, op.AmountHi)
				mintedLo64, mintedHi64 = xvmU128Add(mintedLo64, mintedHi64, op.AmountLo, op.AmountHi)
				applied++
			case xvmAssetOpBurn:
				newLo, newHi, ok := xvmU128Sub(a.TotalSupplyLo, a.TotalSupplyHi, op.AmountLo, op.AmountHi)
				if !ok {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectAmountOverflow
					txDone = true
					break
				}
				a.TotalSupplyLo, a.TotalSupplyHi = newLo, newHi
				burnedLo64, burnedHi64 = xvmU128Add(burnedLo64, burnedHi64, op.AmountLo, op.AmountHi)
				applied++
			case xvmAssetOpTransfer:
				applied++
			case xvmAssetOpExport:
				newLo, newHi, ok := xvmU128Sub(a.TotalSupplyLo, a.TotalSupplyHi, op.AmountLo, op.AmountHi)
				if !ok {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectAmountOverflow
					txDone = true
					break
				}
				a.TotalSupplyLo, a.TotalSupplyHi = newLo, newHi
				txID := tx.TxID
				markerID := xvmComposeMarkerID(&txID, op.TargetChain, op.AmountLo, op.AmountHi)
				mIdx := xvmExportMarkerLocate(markers, &markerID, true)
				if mIdx == 0xFFFFFFFF {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectArenaFull
					txDone = true
					break
				}
				m := &markers[mIdx]
				m.AssetID = op.AssetID
				m.AmountLo = op.AmountLo
				m.AmountHi = op.AmountHi
				m.SourceChain = 0
				m.TargetChain = op.TargetChain
				m.RecipientRoot = op.AuthorityWitnessRoot
				exports++
				applied++
			case xvmAssetOpImport:
				proof := tx.ProofDigest
				mIdx := xvmExportMarkerLocate(markers, &proof, false)
				if mIdx == 0xFFFFFFFF {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectImportNoMarker
					txDone = true
					break
				}
				m := &markers[mIdx]
				if m.Status == xvmExportConsumed {
					tx.Status = xvmTxStatusRejected
					tx.RejectReason = xvmRejectImportNoMarker
					txDone = true
					break
				}
				m.Status = xvmExportConsumed
				a.TotalSupplyLo, a.TotalSupplyHi = xvmU128Add(a.TotalSupplyLo, a.TotalSupplyHi, op.AmountLo, op.AmountHi)
				mintedLo64, mintedHi64 = xvmU128Add(mintedLo64, mintedHi64, op.AmountLo, op.AmountHi)
				imports++
				applied++
			}
		}
	}

	// Output packing — same as the device kernel: the 4 *_lo_out / *_hi_out
	// slots receive the low and (low>>32) halves of the running u64
	// counters. The device kernel discards the true u128 high lanes; we
	// preserve that exact quirk for byte parity (the launcher contract is
	// 4 u32 outputs, not 4 u64 outputs).
	mintedLo = uint32(mintedLo64 & 0xFFFFFFFF)
	mintedHi = uint32((mintedLo64 >> 32) & 0xFFFFFFFF)
	burnedLo = uint32(burnedLo64 & 0xFFFFFFFF)
	burnedHi = uint32((burnedLo64 >> 32) & 0xFFFFFFFF)
	_ = mintedHi64
	_ = burnedHi64
	return applied, exports, imports, mintedLo, mintedHi, burnedLo, burnedHi
}

// =============================================================================
// MembershipRebuild — pure-Go canonical implementation. Mirrors
// xvm_membership_rebuild() in ops/xvm/cuda/xvm_membership.cu. Clears the
// cuckoo arena and re-seeds bloom + cuckoo from the live UTXO arena.
// =============================================================================

func cpuXVMMembershipRebuild(
	utxos []XVMUTXO,
	bloom []byte,
	cuckoo []XVMCuckooEntry,
	utxoCount, bloomBitCount, cuckooBucketCount uint32,
) {
	if int(utxoCount) > len(utxos) {
		utxoCount = uint32(len(utxos))
	}
	total := cuckooBucketCount * xvmCuckooSlotsPerBucket
	if total > uint32(len(cuckoo)) {
		total = uint32(len(cuckoo))
	}
	for i := uint32(0); i < total; i++ {
		e := &cuckoo[i]
		e.UtxoID = [32]byte{}
		e.SlotIndex = 0
		e.Occupied = 0
		e._pad0 = 0
	}
	for i := uint32(0); i < utxoCount; i++ {
		u := &utxos[i]
		if u.Status&xvmUtxoOccupied == 0 {
			continue
		}
		if u.Status&xvmUtxoSpent != 0 {
			continue
		}
		uid := u.UtxoID
		xvmBloomSet(bloom, bloomBitCount, &uid)
		_ = xvmCuckooInsert(cuckoo, cuckooBucketCount, &uid, i)
	}
}

// =============================================================================
// RootUpdate — pure-Go canonical implementation. Mirrors xvm_root_update()
// in ops/xvm/cuda/xvm_roots.cu. Three sub-roots (utxo / asset / tx) plus
// the composed execution_root.
// =============================================================================

func cpuXVMRootUpdate(
	desc *XVMRoundDescriptor,
	txs []XVMTx,
	utxos []XVMUTXO,
	assets []XVMAsset,
	result *XVMTransitionResult,
	txCount, utxoCount, assetCount uint32,
) {
	if int(txCount) > len(txs) {
		txCount = uint32(len(txs))
	}
	if int(utxoCount) > len(utxos) {
		utxoCount = uint32(len(utxos))
	}
	if int(assetCount) > len(assets) {
		assetCount = uint32(len(assets))
	}

	// Phase 1a/2a: UTXO root.
	var acc [32]byte
	for i := uint32(0); i < utxoCount; i++ {
		u := &utxos[i]
		if u.Status&xvmUtxoOccupied == 0 {
			continue
		}
		// leaf layout (bytes): utxo_id[32] || asset_id[32] || u64 amount_lo
		//   || u64 amount_hi || owner_root[32] || u64 locktime || u32 threshold
		//   || u32 status || u32 i
		leaf := make([]byte, 0, 32+32+8+8+32+8+4+4+4)
		leaf = append(leaf, u.UtxoID[:]...)
		leaf = append(leaf, u.AssetID[:]...)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], u.AmountLo)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], u.AmountHi)
		leaf = append(leaf, tmp[:]...)
		leaf = append(leaf, u.OwnerRoot[:]...)
		binary.LittleEndian.PutUint64(tmp[:], u.Locktime)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], u.Threshold)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], u.Status)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], i)
		leaf = append(leaf, tmp[:4]...)
		leafHash := xvmKeccak256(leaf)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], leafHash[:])
		acc = xvmKeccak256(buf[:])
	}
	result.UtxoRoot = acc

	// Phase 1b/2b: asset root.
	acc = [32]byte{}
	for i := uint32(0); i < assetCount; i++ {
		a := &assets[i]
		if a.Occupied == 0 {
			continue
		}
		// leaf: asset_id[32] || u64 total_supply_lo || u64 total_supply_hi
		//   || mint_authority_root[32] || u32 freeze_flag || u32 denomination
		//   || u32 i
		leaf := make([]byte, 0, 32+8+8+32+4+4+4)
		leaf = append(leaf, a.AssetID[:]...)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], a.TotalSupplyLo)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], a.TotalSupplyHi)
		leaf = append(leaf, tmp[:]...)
		leaf = append(leaf, a.MintAuthorityRoot[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], a.FreezeFlag)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], a.Denomination)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], i)
		leaf = append(leaf, tmp[:4]...)
		leafHash := xvmKeccak256(leaf)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], leafHash[:])
		acc = xvmKeccak256(buf[:])
	}
	result.AssetRoot = acc

	// Phase 1c/2c: tx root + accepted/rejected counters.
	acc = [32]byte{}
	var accepted, rejected uint32
	for i := uint32(0); i < txCount; i++ {
		tx := &txs[i]
		if tx.Status == xvmTxStatusAccepted {
			accepted++
		} else if tx.Status == xvmTxStatusRejected {
			rejected++
		}
		// leaf: tx_id[32] || u32 kind || u32 status || u32 reject_reason
		//   || proof_digest[32] || u32 i
		leaf := make([]byte, 0, 32+4+4+4+32+4)
		leaf = append(leaf, tx.TxID[:]...)
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], tx.Kind)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:], tx.Status)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:], tx.RejectReason)
		leaf = append(leaf, tmp[:]...)
		leaf = append(leaf, tx.ProofDigest[:]...)
		binary.LittleEndian.PutUint32(tmp[:], i)
		leaf = append(leaf, tmp[:]...)
		leafHash := xvmKeccak256(leaf)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], leafHash[:])
		acc = xvmKeccak256(buf[:])
	}
	result.TxRoot = acc

	// Phase 2d: composed execution_root.
	composed := make([]byte, 0, 32+32+32+32+8)
	composed = append(composed, desc.ParentExecutionRoot[:]...)
	composed = append(composed, result.UtxoRoot[:]...)
	composed = append(composed, result.AssetRoot[:]...)
	composed = append(composed, result.TxRoot[:]...)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], desc.Height)
	composed = append(composed, tmp[:]...)
	result.ExecutionRoot = xvmKeccak256(composed)

	result.Status = 1
	result.TxAccepted = accepted
	result.TxRejected = rejected
	result.Height = desc.Height
}
