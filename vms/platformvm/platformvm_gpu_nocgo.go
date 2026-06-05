//go:build !cgo

// Package platformvm GPU backend — pure-Go path when CGO is disabled.
//
// The cgo build (platformvm_gpu.go + backend.go) uses dlopen/dlsym to find
// a lux-gpu-kernels plugin at process start and routes the four
// validator/stake/slashing/epoch transitions through the GPU launcher
// (falling back to the Go path on any launcher error). Without cgo
// there's no way to reach a C function pointer, so every GPUBackend
// method goes DIRECTLY through the canonical Go path defined in
// platformvm_gpu_cpu.go.
//
// This file keeps the public API surface identical between build modes:
// the same struct names, the same method signatures, the same package
// constants, and bit-equivalent output. Both build tags drive the same
// pure-Go state-transition impl when no GPU plugin is bound.
package platformvm

import "errors"

// ErrGPUNotAvailable is kept declared so callers can compare against it in
// either build mode. The four bridge methods do NOT return it in the
// nocgo build — they always run the canonical pure-Go path.
var ErrGPUNotAvailable = errors.New("platformvm: GPU plugin unavailable (built without CGo)")

// GPUBackendKind identifies which lux-gpu-kernels plugin satisfied the
// runtime probe. GPUNone is the only value reachable without cgo.
type GPUBackendKind uint8

const (
	GPUNone   GPUBackendKind = 0
	GPUCUDA   GPUBackendKind = 1
	GPUHIP    GPUBackendKind = 2
	GPUMetal  GPUBackendKind = 3
	GPUVulkan GPUBackendKind = 4
	GPUWebGPU GPUBackendKind = 5
)

// String returns "none" under the nocgo stub. The other kinds are
// unreachable on this build but kept declared so callers can compare
// against the same constants either way.
func (k GPUBackendKind) String() string {
	switch k {
	case GPUNone:
		return "none"
	case GPUCUDA:
		return "cuda"
	case GPUHIP:
		return "hip"
	case GPUMetal:
		return "metal"
	case GPUVulkan:
		return "vulkan"
	case GPUWebGPU:
		return "webgpu"
	default:
		return "unknown"
	}
}

// =============================================================================
// Layout structs — kept fully declared so package-internal helpers compile
// identically in both modes. Field tags and sizes are NOT enforced under
// nocgo (no cgo boundary to validate against).
// =============================================================================

// PVMValidatorSlot mirrors the cgo build's layout.
type PVMValidatorSlot struct {
	ValidatorID      uint64
	Weight           uint64
	BLSPubkey        [48]byte
	CoronaPubkey     [32]byte
	MLDSAPubkey      [32]byte
	MLDSAGroth16Root [32]byte
	Status           uint32
	JailUntilEpoch   uint32
	Occupied         uint32
	_pad0            uint32
}

// PVMStakeRecord mirrors the cgo build's layout.
type PVMStakeRecord struct {
	DelegatorID       uint64
	ValidatorID       uint64
	Amount            uint64
	LockUntilEpoch    uint64
	RewardAccumulator uint64
	CommissionBPS     uint32
	Status            uint32
	EpochBonded       uint32
	EpochUnbonded     uint32
	_pad0             uint64
}

// PVMSlashEvidence mirrors the cgo build's layout.
type PVMSlashEvidence struct {
	ValidatorID    uint64
	Height         uint64
	SlashAmount    uint64
	Kind           uint32
	Epoch          uint32
	JailForEpochs  uint32
	_pad0          uint32
	EvidenceDigest [32]byte
	_pad1          uint64
}

// PVMEpochState mirrors the cgo build's layout.
type PVMEpochState struct {
	CurrentEpoch         uint64
	NextEpochHeight      uint64
	TotalActiveStake     uint64
	ActiveValidatorCount uint32
	PendingDropCount     uint32
	ValidatorSetRoot     [32]byte
	StakeRoot            [32]byte
	SlashingRoot         [32]byte
	EpochRoot            [32]byte
}

// PVMRoundDescriptor mirrors the cgo build's layout.
type PVMRoundDescriptor struct {
	ChainID            uint64
	Round              uint64
	TimestampNS        uint64
	Epoch              uint64
	Mode               uint32
	ValidatorOpCount   uint32
	StakeOpCount       uint32
	SlashEvidenceCount uint32
	ClosingFlag        uint32
	_pad0              uint32
	_pad1              uint64
	ParentEpochRoot    [32]byte
}

// PVMValidatorOp mirrors the cgo build's layout.
type PVMValidatorOp struct {
	ValidatorID      uint64
	Weight           uint64
	BLSPubkey        [48]byte
	CoronaPubkey     [32]byte
	MLDSAPubkey      [32]byte
	MLDSAGroth16Root [32]byte
	Kind             uint32
	JailUntilEpoch   uint32
	Epoch            uint32
	_pad0            uint32
}

// PVMStakeOp mirrors the cgo build's layout.
type PVMStakeOp struct {
	DelegatorID       uint64
	ValidatorID       uint64
	Amount            uint64
	LockUntilEpoch    uint64
	SourceValidatorID uint64
	Kind              uint32
	CommissionBPS     uint32
	Epoch             uint32
	_pad0             uint32
	_pad1             uint64
}

// PVMTransitionResult mirrors the cgo build's layout.
type PVMTransitionResult struct {
	Status               uint32
	ValidatorApplyCount  uint32
	StakeApplyCount      uint32
	SlashApplyCount      uint32
	ActiveValidatorCount uint32
	PendingDropCount     uint32
	JailedCount          uint32
	TombstonedCount      uint32
	TotalActiveStake     uint64
	TotalSlashed         uint64
	TotalRewards         uint64
	Epoch                uint64
	ValidatorSetRoot     [32]byte
	StakeRoot            [32]byte
	SlashingRoot         [32]byte
	EpochRoot            [32]byte
}

// Constants — identical to the cgo build so callers compile either way.
const (
	PVMVOpAdd          uint32 = 0
	PVMVOpRemove       uint32 = 1
	PVMVOpUpdateWeight uint32 = 2
	PVMVOpJail         uint32 = 3
	PVMVOpUnjail       uint32 = 4
	PVMVOpRotateKeys   uint32 = 5

	PVMSOpBond       uint32 = 0
	PVMSOpUnbond     uint32 = 1
	PVMSOpDelegate   uint32 = 2
	PVMSOpRedelegate uint32 = 3
	PVMSOpReward     uint32 = 4
	PVMSOpCommission uint32 = 5

	PVMEvEquivocation uint32 = 0
	PVMEvDowntime     uint32 = 1
	PVMEvInvalidVote  uint32 = 2

	PVMModeValidator uint32 = 0
	PVMModeStake     uint32 = 1
	PVMModeSlashing  uint32 = 2
	PVMModeEpoch     uint32 = 3
	PVMModeFullRound uint32 = 4

	PVMStatusActive      uint32 = 0x1
	PVMStatusJailed      uint32 = 0x2
	PVMStatusTombstoned  uint32 = 0x4
	PVMStatusPendingAdd  uint32 = 0x8
	PVMStatusPendingDrop uint32 = 0x10

	PVMStakeStatusActive    uint32 = 1
	PVMStakeStatusUnbonding uint32 = 2
	PVMStakeStatusRetired   uint32 = 3
)

// =============================================================================
// GPUBackend — nocgo handle. Carries no plugin state; every transition
// method dispatches directly to the canonical pure-Go path defined in
// platformvm_gpu_cpu.go. The struct is intentionally empty so a zero
// value is safe to use (matching the cgo build's nil-receiver contract).
// =============================================================================

// GPUBackend is the nocgo handle. Calling its transition methods runs the
// canonical Go state-transition impl directly.
type GPUBackend struct{}

// openGPUBackend is the nocgo entry point used by backend.go. There is no
// dlopen without cgo so this always returns (nil, ErrGPUNotAvailable);
// callers fall through to ActiveGPUBackend()=nil and invoke the four
// transition methods on the nil receiver — which still runs the Go path.
func openGPUBackend(_ GPUBackendKind, _ string) (*GPUBackend, error) {
	return nil, ErrGPUNotAvailable
}

// Kind always returns GPUNone under nocgo.
func (b *GPUBackend) Kind() GPUBackendKind { return GPUNone }

// Path returns an empty string under nocgo.
func (b *GPUBackend) Path() string { return "" }

// IsAvailable always returns false under nocgo — no plugin can be bound
// without cgo. The transition methods do NOT consult IsAvailable; they
// dispatch unconditionally to the pure-Go path.
func (b *GPUBackend) IsAvailable() bool { return false }

// Close is a no-op under nocgo.
func (b *GPUBackend) Close() error { return nil }

// ValidatorSetApply runs the canonical pure-Go validator-set-apply
// transition (cpuValidatorSetApply in platformvm_gpu_cpu.go). Same
// validation contract as the cgo build, byte-identical output.
func (b *GPUBackend) ValidatorSetApply(
	desc *PVMRoundDescriptor,
	ops []PVMValidatorOp,
	validators []PVMValidatorSlot,
	appliedOut *uint32,
) error {
	if desc == nil || appliedOut == nil {
		return errors.New("platformvm: ValidatorSetApply: nil desc or appliedOut")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: ValidatorSetApply: empty validators table")
	}
	*appliedOut = cpuValidatorSetApply(desc, ops, validators)
	return nil
}

// StakeTransition runs the canonical pure-Go stake transition.
func (b *GPUBackend) StakeTransition(
	desc *PVMRoundDescriptor,
	ops []PVMStakeOp,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	appliedOut *uint32,
) error {
	if desc == nil || appliedOut == nil {
		return errors.New("platformvm: StakeTransition: nil desc or appliedOut")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: StakeTransition: empty validators table")
	}
	if len(stake) == 0 {
		return errors.New("platformvm: StakeTransition: empty stake table")
	}
	*appliedOut = cpuStakeTransition(desc, ops, validators, stake)
	return nil
}

// SlashingTransition runs the canonical pure-Go slashing transition.
func (b *GPUBackend) SlashingTransition(
	desc *PVMRoundDescriptor,
	evidence []PVMSlashEvidence,
	validators []PVMValidatorSlot,
	slashing []PVMSlashEvidence,
	appliedOut, totalLoOut, totalHiOut *uint32,
) error {
	if desc == nil || appliedOut == nil || totalLoOut == nil || totalHiOut == nil {
		return errors.New("platformvm: SlashingTransition: nil desc or output pointer")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: SlashingTransition: empty validators table")
	}
	*appliedOut, *totalLoOut, *totalHiOut = cpuSlashingTransition(desc, evidence, validators, slashing)
	return nil
}

// EpochTransition runs the canonical pure-Go epoch transition. The
// `leafScratch` argument is accepted for ABI parity with the cgo build
// and ignored — the Go impl allocates per-leaf.
func (b *GPUBackend) EpochTransition(
	desc *PVMRoundDescriptor,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	slashing []PVMSlashEvidence,
	epoch *PVMEpochState,
	result *PVMTransitionResult,
	leafScratch []byte,
) error {
	_ = leafScratch
	if desc == nil || epoch == nil || result == nil {
		return errors.New("platformvm: EpochTransition: nil desc, epoch, or result")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: EpochTransition: empty validators table")
	}
	cpuEpochTransition(desc, validators, stake, slashing, epoch, result)
	return nil
}
