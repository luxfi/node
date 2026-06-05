// Package platformvm — pure-Go canonical implementation of the four bridge
// transitions. This is the SINGLE source of truth for ValidatorSetApply,
// StakeTransition, SlashingTransition, and EpochTransition: the cgo build
// falls back to these functions when the GPU plugin returns an error, and
// the nocgo build calls them directly.
//
// Algorithm fidelity: byte-equal to the C++ CPU reference embedded in
// the GPU plugin's vulkan KAT (the canonical determinism oracle every
// GPU backend is validated against). The CUDA / HIP / Metal / Vulkan /
// WebGPU kernels at ops/platformvm/<backend>/ MUST match the output of
// these Go functions byte-for-byte. If the GPU plugin disagrees with the
// Go impl, the Go impl wins — file a follow-up at the plugin.
//
// One file, no build tag — same code compiles into both build modes. The
// cgo bridge in platformvm_gpu.go wraps these helpers with the GPU-first
// dispatch; the nocgo stub in platformvm_gpu_nocgo.go calls them directly.
package platformvm

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// =============================================================================
// keccak256 — the same keccak that the GPU device code uses. We route
// through golang.org/x/crypto/sha3.NewLegacyKeccak256 which is already a
// transitive dependency of the node (used by go-ethereum and luxfi/geth).
// Wrapping it locally keeps the call sites small and the intent visible.
// =============================================================================

func keccak256(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// =============================================================================
// Open-addressing locators — identical hash + probe shape to
// ops/platformvm/cuda/platformvm_kernels_common.cuh.
// =============================================================================

// validatorIndexHash is the FNV-style index the GPU kernels use. Must be
// byte-equivalent on every backend.
func validatorIndexHash(validatorID uint64, mask uint32) uint32 {
	h := uint64(0xcbf29ce484222325)
	h = (h ^ validatorID) * 0x100000001b3
	return uint32(h) & mask
}

// validatorLocate finds the slot for `validatorID` in the open-addressed
// table `tab`. If `insertIfMissing` is true, an empty probe slot is
// claimed and re-initialized. Returns 0xFFFFFFFF on failure.
//
// Mirrors validator_locate() in platformvm_kernels_common.cuh — same hash,
// same linear-probe step, same insert reset pattern.
func validatorLocate(tab []PVMValidatorSlot, validatorID uint64, insertIfMissing bool) uint32 {
	count := uint32(len(tab))
	if count == 0 {
		return 0xFFFFFFFF
	}
	mask := count - 1
	idx := validatorIndexHash(validatorID, mask)
	for probe := uint32(0); probe < count; probe++ {
		s := &tab[idx]
		if s.Occupied == 0 {
			if insertIfMissing {
				s.ValidatorID = validatorID
				s.Weight = 0
				s.Status = 0
				s.JailUntilEpoch = 0
				s.Occupied = 1
				s.BLSPubkey = [48]byte{}
				s.CoronaPubkey = [32]byte{}
				s.MLDSAPubkey = [32]byte{}
				s.MLDSAGroth16Root = [32]byte{}
				return idx
			}
			return 0xFFFFFFFF
		}
		if s.ValidatorID == validatorID {
			return idx
		}
		idx = (idx + 1) & mask
	}
	return 0xFFFFFFFF
}

// stakeRecordIndexHash mirrors stake_record_index_hash() in the GPU
// common header. Byte-equivalent across all backends.
func stakeRecordIndexHash(delegator, validator uint64, mask uint32) uint32 {
	composite := delegator ^ (validator + 0x9E3779B97F4A7C15 +
		(delegator << 6) + (delegator >> 2))
	return uint32(composite) & mask
}

// stakeRecordLocate mirrors stake_record_locate(). Insert path re-uses
// the same field-clear sequence as the GPU.
func stakeRecordLocate(tab []PVMStakeRecord, delegator, validator uint64, insertIfMissing bool) uint32 {
	count := uint32(len(tab))
	if count == 0 {
		return 0xFFFFFFFF
	}
	mask := count - 1
	idx := stakeRecordIndexHash(delegator, validator, mask)
	for probe := uint32(0); probe < count; probe++ {
		s := &tab[idx]
		if s.Status == 0 {
			if insertIfMissing {
				s.DelegatorID = delegator
				s.ValidatorID = validator
				s.Amount = 0
				s.LockUntilEpoch = 0
				s.RewardAccumulator = 0
				s.CommissionBPS = 0
				s.Status = PVMStakeStatusActive
				s.EpochBonded = 0
				s.EpochUnbonded = 0
				return idx
			}
			return 0xFFFFFFFF
		}
		if s.DelegatorID == delegator && s.ValidatorID == validator {
			return idx
		}
		idx = (idx + 1) & mask
	}
	return 0xFFFFFFFF
}

// =============================================================================
// Saturating arithmetic — matches sat_add_u64 / sat_sub_u64 in the GPU
// common header. The transition kernels rely on these wrapping the same
// way the device code does.
// =============================================================================

func satAddU64(a, b uint64) uint64 {
	r := a + b
	if r < a {
		return 0xFFFFFFFFFFFFFFFF
	}
	return r
}

func satSubU64(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// =============================================================================
// ValidatorSetApply — pure-Go canonical implementation. Sequential walk
// over `desc.ValidatorOpCount` ops; mutates `validators` in place; writes
// the apply count via the return value.
//
// Reference: cpu_validator_set_apply() + the full op-set body in
// platformvm_validator_set.cu (cuda kernel). The CPU oracle in the KAT
// covers Add + UpdateWeight only; the full six-kind op set lives in the
// GPU kernel and is mirrored here verbatim.
// =============================================================================

func cpuValidatorSetApply(
	desc *PVMRoundDescriptor,
	ops []PVMValidatorOp,
	validators []PVMValidatorSlot,
) uint32 {
	count := desc.ValidatorOpCount
	if count > uint32(len(ops)) {
		count = uint32(len(ops))
	}
	var applied uint32
	for i := uint32(0); i < count; i++ {
		op := &ops[i]
		switch op.Kind {
		case PVMVOpAdd:
			idx := validatorLocate(validators, op.ValidatorID, true)
			if idx == 0xFFFFFFFF {
				continue
			}
			s := &validators[idx]
			s.Weight = op.Weight
			s.BLSPubkey = op.BLSPubkey
			s.CoronaPubkey = op.CoronaPubkey
			s.MLDSAPubkey = op.MLDSAPubkey
			s.MLDSAGroth16Root = op.MLDSAGroth16Root
			s.Status = PVMStatusActive | PVMStatusPendingAdd
			s.JailUntilEpoch = 0
			applied++
		case PVMVOpRemove:
			idx := validatorLocate(validators, op.ValidatorID, false)
			if idx == 0xFFFFFFFF {
				continue
			}
			s := &validators[idx]
			if s.Status&PVMStatusTombstoned != 0 {
				continue
			}
			s.Status |= PVMStatusPendingDrop
			s.Status &^= PVMStatusActive
			applied++
		case PVMVOpUpdateWeight:
			idx := validatorLocate(validators, op.ValidatorID, false)
			if idx == 0xFFFFFFFF {
				continue
			}
			s := &validators[idx]
			if s.Status&PVMStatusTombstoned != 0 {
				continue
			}
			s.Weight = op.Weight
			applied++
		case PVMVOpJail:
			idx := validatorLocate(validators, op.ValidatorID, false)
			if idx == 0xFFFFFFFF {
				continue
			}
			s := &validators[idx]
			if s.Status&PVMStatusTombstoned != 0 {
				continue
			}
			s.Status |= PVMStatusJailed
			s.Status &^= PVMStatusActive
			if op.JailUntilEpoch > s.JailUntilEpoch {
				s.JailUntilEpoch = op.JailUntilEpoch
			}
			applied++
		case PVMVOpUnjail:
			idx := validatorLocate(validators, op.ValidatorID, false)
			if idx == 0xFFFFFFFF {
				continue
			}
			s := &validators[idx]
			if s.Status&PVMStatusTombstoned != 0 {
				continue
			}
			if op.Epoch < s.JailUntilEpoch {
				continue
			}
			s.Status &^= PVMStatusJailed
			s.Status |= PVMStatusActive
			s.JailUntilEpoch = 0
			applied++
		case PVMVOpRotateKeys:
			idx := validatorLocate(validators, op.ValidatorID, false)
			if idx == 0xFFFFFFFF {
				continue
			}
			s := &validators[idx]
			if s.Status&PVMStatusTombstoned != 0 {
				continue
			}
			s.BLSPubkey = op.BLSPubkey
			s.CoronaPubkey = op.CoronaPubkey
			s.MLDSAPubkey = op.MLDSAPubkey
			s.MLDSAGroth16Root = op.MLDSAGroth16Root
			applied++
		}
	}
	return applied
}

// =============================================================================
// StakeTransition — pure-Go canonical implementation. Mirrors
// platformvm_staking.cu (the full six-kind op set: Bond / Unbond /
// Delegate / Redelegate / Reward / Commission).
// =============================================================================

const cpuRewardScale uint64 = 1000000000000000000 // 1e18

func cpuStakeTransition(
	desc *PVMRoundDescriptor,
	ops []PVMStakeOp,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
) uint32 {
	count := desc.StakeOpCount
	if count > uint32(len(ops)) {
		count = uint32(len(ops))
	}
	stakeCount := uint32(len(stake))
	var applied uint32
	for i := uint32(0); i < count; i++ {
		op := &ops[i]
		switch op.Kind {
		case PVMSOpBond:
			vIdx := validatorLocate(validators, op.ValidatorID, false)
			if vIdx == 0xFFFFFFFF {
				continue
			}
			v := &validators[vIdx]
			if v.Status&PVMStatusTombstoned != 0 {
				continue
			}
			sIdx := stakeRecordLocate(stake, op.DelegatorID, op.ValidatorID, true)
			if sIdx == 0xFFFFFFFF {
				continue
			}
			s := &stake[sIdx]
			s.Amount = satAddU64(s.Amount, op.Amount)
			if op.LockUntilEpoch > s.LockUntilEpoch {
				s.LockUntilEpoch = op.LockUntilEpoch
			}
			if s.EpochBonded == 0 {
				s.EpochBonded = op.Epoch
			}
			s.Status = PVMStakeStatusActive
			v.Weight = satAddU64(v.Weight, op.Amount)
			applied++
		case PVMSOpUnbond:
			sIdx := stakeRecordLocate(stake, op.DelegatorID, op.ValidatorID, false)
			if sIdx == 0xFFFFFFFF {
				continue
			}
			s := &stake[sIdx]
			if s.Status != PVMStakeStatusActive {
				continue
			}
			if uint64(op.Epoch) < s.LockUntilEpoch {
				continue
			}
			amt := op.Amount
			if amt > s.Amount {
				amt = s.Amount
			}
			s.Amount = satSubU64(s.Amount, amt)
			s.EpochUnbonded = op.Epoch
			if s.Amount == 0 {
				s.Status = PVMStakeStatusRetired
			} else {
				s.Status = PVMStakeStatusUnbonding
			}
			vIdx := validatorLocate(validators, op.ValidatorID, false)
			if vIdx != 0xFFFFFFFF {
				v := &validators[vIdx]
				v.Weight = satSubU64(v.Weight, amt)
			}
			applied++
		case PVMSOpDelegate:
			vIdx := validatorLocate(validators, op.ValidatorID, false)
			if vIdx == 0xFFFFFFFF {
				continue
			}
			v := &validators[vIdx]
			if v.Status&(PVMStatusTombstoned|PVMStatusJailed) != 0 {
				continue
			}
			sIdx := stakeRecordLocate(stake, op.DelegatorID, op.ValidatorID, true)
			if sIdx == 0xFFFFFFFF {
				continue
			}
			s := &stake[sIdx]
			s.Amount = satAddU64(s.Amount, op.Amount)
			s.Status = PVMStakeStatusActive
			if s.EpochBonded == 0 {
				s.EpochBonded = op.Epoch
			}
			v.Weight = satAddU64(v.Weight, op.Amount)
			applied++
		case PVMSOpRedelegate:
			if op.SourceValidatorID == op.ValidatorID {
				continue
			}
			srcIdx := stakeRecordLocate(stake, op.DelegatorID, op.SourceValidatorID, false)
			if srcIdx == 0xFFFFFFFF {
				continue
			}
			src := &stake[srcIdx]
			if src.Status != PVMStakeStatusActive {
				continue
			}
			if uint64(op.Epoch) < src.LockUntilEpoch {
				continue
			}
			vDstIdx := validatorLocate(validators, op.ValidatorID, false)
			if vDstIdx == 0xFFFFFFFF {
				continue
			}
			vDst := &validators[vDstIdx]
			if vDst.Status&PVMStatusTombstoned != 0 {
				continue
			}
			amt := op.Amount
			if amt > src.Amount {
				amt = src.Amount
			}
			src.Amount = satSubU64(src.Amount, amt)
			if src.Amount == 0 {
				src.Status = PVMStakeStatusRetired
			}
			vSrcIdx := validatorLocate(validators, op.SourceValidatorID, false)
			if vSrcIdx != 0xFFFFFFFF {
				vSrc := &validators[vSrcIdx]
				vSrc.Weight = satSubU64(vSrc.Weight, amt)
			}
			dstIdx := stakeRecordLocate(stake, op.DelegatorID, op.ValidatorID, true)
			if dstIdx == 0xFFFFFFFF {
				continue
			}
			dst := &stake[dstIdx]
			dst.Amount = satAddU64(dst.Amount, amt)
			dst.Status = PVMStakeStatusActive
			if dst.EpochBonded == 0 {
				dst.EpochBonded = op.Epoch
			}
			vDst.Weight = satAddU64(vDst.Weight, amt)
			applied++
		case PVMSOpReward:
			vIdx := validatorLocate(validators, op.ValidatorID, false)
			if vIdx == 0xFFFFFFFF {
				continue
			}
			v := &validators[vIdx]
			if v.Weight == 0 {
				continue
			}
			var scaled uint64
			if op.Amount > 0xFFFFFFFFFFFFFFFF/cpuRewardScale {
				scaled = 0xFFFFFFFFFFFFFFFF
			} else {
				scaled = op.Amount * cpuRewardScale
			}
			perUnit := scaled / v.Weight
			if perUnit == 0 {
				continue
			}
			for si := uint32(0); si < stakeCount; si++ {
				s := &stake[si]
				if s.Status != PVMStakeStatusActive {
					continue
				}
				if s.ValidatorID != op.ValidatorID {
					continue
				}
				var delta uint64
				if s.Amount > 0xFFFFFFFFFFFFFFFF/perUnit {
					delta = 0xFFFFFFFFFFFFFFFF
				} else {
					delta = s.Amount * perUnit
				}
				s.RewardAccumulator = satAddU64(s.RewardAccumulator, delta)
			}
			applied++
		case PVMSOpCommission:
			vIdx := validatorLocate(validators, op.ValidatorID, false)
			if vIdx == 0xFFFFFFFF {
				continue
			}
			if op.CommissionBPS > 10000 {
				continue
			}
			sIdx := stakeRecordLocate(stake, op.ValidatorID, op.ValidatorID, false)
			if sIdx == 0xFFFFFFFF {
				continue
			}
			stake[sIdx].CommissionBPS = op.CommissionBPS
			applied++
		}
	}
	return applied
}

// =============================================================================
// SlashingTransition — pure-Go canonical implementation. Mirrors
// platformvm_slashing.cu including the per-field copy that resets _pad0
// and _pad1 on the persisted-slashing slot. Returns the 64-bit
// total-slashed split into (lo, hi) u32 halves to match the GPU launcher
// ABI.
// =============================================================================

func cpuSlashingTransition(
	desc *PVMRoundDescriptor,
	evidence []PVMSlashEvidence,
	validators []PVMValidatorSlot,
	slashing []PVMSlashEvidence,
) (applied, totalLo, totalHi uint32) {
	count := desc.SlashEvidenceCount
	if count > uint32(len(evidence)) {
		count = uint32(len(evidence))
	}
	slashingCount := uint32(len(slashing))
	var totalSlashed uint64
	var cursor uint32

	for i := uint32(0); i < count; i++ {
		ev := &evidence[i]
		vIdx := validatorLocate(validators, ev.ValidatorID, false)
		if vIdx == 0xFFFFFFFF {
			continue
		}
		v := &validators[vIdx]
		if v.Status&PVMStatusTombstoned != 0 {
			continue
		}

		amount := ev.SlashAmount
		if amount == 0 {
			switch ev.Kind {
			case PVMEvEquivocation:
				amount = v.Weight / 20
			case PVMEvDowntime:
				amount = v.Weight / 100
			case PVMEvInvalidVote:
				amount = v.Weight / 50
			}
		}
		if amount > v.Weight {
			amount = v.Weight
		}
		v.Weight = satSubU64(v.Weight, amount)
		totalSlashed = satAddU64(totalSlashed, amount)

		if ev.Kind == PVMEvEquivocation {
			v.Status |= PVMStatusTombstoned
			v.Status &^= PVMStatusActive
		} else {
			v.Status |= PVMStatusJailed
			v.Status &^= PVMStatusActive
			jailFor := ev.JailForEpochs
			if jailFor == 0 {
				jailFor = 100
			}
			until := ev.Epoch + jailFor
			if until > v.JailUntilEpoch {
				v.JailUntilEpoch = until
			}
		}

		if cursor < slashingCount {
			dst := &slashing[cursor]
			dst.ValidatorID = ev.ValidatorID
			dst.Height = ev.Height
			dst.SlashAmount = ev.SlashAmount
			dst.Kind = ev.Kind
			dst.Epoch = ev.Epoch
			dst.JailForEpochs = ev.JailForEpochs
			dst._pad0 = 0
			dst.EvidenceDigest = ev.EvidenceDigest
			dst._pad1 = 0
			cursor++
		}
		applied++
	}

	totalLo = uint32(totalSlashed & 0xFFFFFFFF)
	totalHi = uint32((totalSlashed >> 32) & 0xFFFFFFFF)
	return applied, totalLo, totalHi
}

// =============================================================================
// EpochTransition — pure-Go canonical implementation. Mirrors
// cpu_epoch_transition() in the KAT byte-for-byte: phase 1a promotion +
// auto-unjail, phase 1b validator leaf merkle accumulator, phase 1c stake
// leaf accumulator, phase 1d slashing leaf accumulator, phase 2 composed
// epoch_root + result write-out.
//
// Leaf encoding is little-endian for the integer fields and matches the
// GPU device code in platformvm_transition.cu exactly (absorb_u32 /
// absorb_u64 write little-endian).
// =============================================================================

func cpuEpochTransition(
	desc *PVMRoundDescriptor,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	slashing []PVMSlashEvidence,
	epoch *PVMEpochState,
	result *PVMTransitionResult,
) {
	validatorCount := uint32(len(validators))
	stakeCount := uint32(len(stake))
	slashingCount := uint32(len(slashing))

	targetLo := uint32(desc.Epoch & 0xFFFFFFFF)
	if desc.ClosingFlag != 0 {
		targetLo += 1
	}

	// Phase 1a: promotion / auto-unjail.
	var pendingDrop uint32
	for i := uint32(0); i < validatorCount; i++ {
		s := &validators[i]
		if s.Occupied == 0 {
			continue
		}
		if s.Status&PVMStatusPendingAdd != 0 {
			s.Status &^= PVMStatusPendingAdd
		}
		if s.Status&PVMStatusPendingDrop != 0 {
			s.Status &^= PVMStatusPendingDrop
			s.Status |= PVMStatusTombstoned
			pendingDrop++
		}
		if s.Status&PVMStatusJailed != 0 &&
			s.JailUntilEpoch != 0 &&
			targetLo >= s.JailUntilEpoch &&
			s.Status&PVMStatusTombstoned == 0 {
			s.Status &^= PVMStatusJailed
			s.Status |= PVMStatusActive
			s.JailUntilEpoch = 0
		}
	}
	epoch.PendingDropCount = pendingDrop

	// Phase 1b: validator merkle accumulator + counters.
	var acc [32]byte
	var active, jailed, tombstoned uint32
	var totalStake uint64
	for i := uint32(0); i < validatorCount; i++ {
		s := &validators[i]
		if s.Occupied == 0 {
			continue
		}
		if s.Status&PVMStatusTombstoned != 0 {
			tombstoned++
		}
		if s.Status&PVMStatusJailed != 0 {
			jailed++
		}
		if s.Status&PVMStatusActive != 0 {
			active++
			totalStake = satAddU64(totalStake, s.Weight)
		}
		// leaf layout: u64 validator_id || u64 weight || u32 status ||
		//              u32 jail_until_epoch || 48B bls || 32B corona ||
		//              32B mldsa || 32B groth16_root || u32 i
		leaf := make([]byte, 0, 172)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], s.ValidatorID)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], s.Weight)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], s.Status)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], s.JailUntilEpoch)
		leaf = append(leaf, tmp[:4]...)
		leaf = append(leaf, s.BLSPubkey[:]...)
		leaf = append(leaf, s.CoronaPubkey[:]...)
		leaf = append(leaf, s.MLDSAPubkey[:]...)
		leaf = append(leaf, s.MLDSAGroth16Root[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], i)
		leaf = append(leaf, tmp[:4]...)

		leafHash := keccak256(leaf)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], leafHash[:])
		acc = keccak256(buf[:])
	}
	epoch.ValidatorSetRoot = acc

	// Phase 1c: stake merkle accumulator + total_rewards.
	acc = [32]byte{}
	var totalRewards uint64
	for i := uint32(0); i < stakeCount; i++ {
		s := &stake[i]
		if s.Status == 0 {
			continue
		}
		totalRewards = satAddU64(totalRewards, s.RewardAccumulator)
		// leaf layout: u64 delegator_id || u64 validator_id || u64 amount ||
		//              u64 lock_until_epoch || u64 reward_accumulator ||
		//              u32 commission_bps || u32 status || u32 epoch_bonded ||
		//              u32 epoch_unbonded || u32 i
		leaf := make([]byte, 0, 60)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], s.DelegatorID)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], s.ValidatorID)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], s.Amount)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], s.LockUntilEpoch)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], s.RewardAccumulator)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], s.CommissionBPS)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], s.Status)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], s.EpochBonded)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], s.EpochUnbonded)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], i)
		leaf = append(leaf, tmp[:4]...)

		leafHash := keccak256(leaf)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], leafHash[:])
		acc = keccak256(buf[:])
	}
	epoch.StakeRoot = acc

	// Phase 1d: slashing merkle accumulator. Skip rows that are entirely
	// zero (validator_id == 0 AND height == 0 AND digest all zero) — the
	// GPU kernel uses the same skip predicate.
	acc = [32]byte{}
	for i := uint32(0); i < slashingCount; i++ {
		ev := &slashing[i]
		zeroDigest := true
		for k := 0; k < 32; k++ {
			if ev.EvidenceDigest[k] != 0 {
				zeroDigest = false
				break
			}
		}
		if ev.ValidatorID == 0 && zeroDigest && ev.Height == 0 {
			continue
		}
		// leaf layout: u64 validator_id || u64 height || u64 slash_amount ||
		//              u32 kind || u32 epoch || u32 jail_for_epochs ||
		//              32B evidence_digest || u32 i
		leaf := make([]byte, 0, 72)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], ev.ValidatorID)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], ev.Height)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint64(tmp[:], ev.SlashAmount)
		leaf = append(leaf, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], ev.Kind)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], ev.Epoch)
		leaf = append(leaf, tmp[:4]...)
		binary.LittleEndian.PutUint32(tmp[:4], ev.JailForEpochs)
		leaf = append(leaf, tmp[:4]...)
		leaf = append(leaf, ev.EvidenceDigest[:]...)
		binary.LittleEndian.PutUint32(tmp[:4], i)
		leaf = append(leaf, tmp[:4]...)

		leafHash := keccak256(leaf)
		var buf [64]byte
		copy(buf[:32], acc[:])
		copy(buf[32:], leafHash[:])
		acc = keccak256(buf[:])
	}
	epoch.SlashingRoot = acc

	epoch.ActiveValidatorCount = active
	epoch.TotalActiveStake = totalStake
	if desc.ClosingFlag != 0 {
		epoch.CurrentEpoch = desc.Epoch + 1
	}

	// Phase 2: composed epoch_root = keccak(parent || vset || stake ||
	// slashing || u64 epoch || u64 total_stake || u32 active_count).
	composed := make([]byte, 0, 148)
	composed = append(composed, desc.ParentEpochRoot[:]...)
	composed = append(composed, epoch.ValidatorSetRoot[:]...)
	composed = append(composed, epoch.StakeRoot[:]...)
	composed = append(composed, epoch.SlashingRoot[:]...)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], epoch.CurrentEpoch)
	composed = append(composed, tmp[:]...)
	binary.LittleEndian.PutUint64(tmp[:], epoch.TotalActiveStake)
	composed = append(composed, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:4], epoch.ActiveValidatorCount)
	composed = append(composed, tmp[:4]...)
	epoch.EpochRoot = keccak256(composed)

	// Write out result.
	result.ValidatorSetRoot = epoch.ValidatorSetRoot
	result.StakeRoot = epoch.StakeRoot
	result.SlashingRoot = epoch.SlashingRoot
	result.EpochRoot = epoch.EpochRoot
	result.ActiveValidatorCount = active
	result.JailedCount = jailed
	result.TombstonedCount = tombstoned
	result.TotalActiveStake = epoch.TotalActiveStake
	result.TotalRewards = totalRewards
	result.PendingDropCount = pendingDrop
	result.Epoch = epoch.CurrentEpoch
	result.Status = 1
}
