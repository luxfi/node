//go:build !cgo

// Package platformvm GPU backend — stub used when CGO is disabled.
//
// The cgo build (platformvm_gpu.go + backend.go) uses dlopen/dlsym to find a
// lux-gpu-kernels plugin at process start. Without cgo there's no way to
// reach a C function pointer, so every GPUBackend method returns
// ErrGPUNotAvailable. vm.go callers see GPUAvailable() == false and
// fall through to the existing Go path.
//
// This file keeps the public API surface identical between build modes:
// the same struct names, the same method signatures, the same package
// constants. Only the implementation differs.
package platformvm

import "errors"

// ErrGPUNotAvailable mirrors the cgo build's sentinel. vm.go can compare
// against it without caring which build mode is active.
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
// GPUBackend stub — every method returns ErrGPUNotAvailable.
// =============================================================================

// GPUBackend is the nocgo stub. All methods return ErrGPUNotAvailable so
// vm.go can treat both builds identically.
type GPUBackend struct{}

// openGPUBackend is the nocgo entry point used by backend.go. Always
// returns (nil, ErrGPUNotAvailable) since dlopen requires cgo.
func openGPUBackend(_ GPUBackendKind, _ string) (*GPUBackend, error) {
	return nil, ErrGPUNotAvailable
}

// Kind always returns GPUNone under nocgo.
func (b *GPUBackend) Kind() GPUBackendKind { return GPUNone }

// Path returns an empty string under nocgo.
func (b *GPUBackend) Path() string { return "" }

// IsAvailable always returns false under nocgo.
func (b *GPUBackend) IsAvailable() bool { return false }

// Close is a no-op under nocgo.
func (b *GPUBackend) Close() error { return nil }

// ValidatorSetApply returns ErrGPUNotAvailable under nocgo.
func (b *GPUBackend) ValidatorSetApply(
	desc *PVMRoundDescriptor,
	ops []PVMValidatorOp,
	validators []PVMValidatorSlot,
	appliedOut *uint32,
) error {
	return ErrGPUNotAvailable
}

// StakeTransition returns ErrGPUNotAvailable under nocgo.
func (b *GPUBackend) StakeTransition(
	desc *PVMRoundDescriptor,
	ops []PVMStakeOp,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	appliedOut *uint32,
) error {
	return ErrGPUNotAvailable
}

// SlashingTransition returns ErrGPUNotAvailable under nocgo.
func (b *GPUBackend) SlashingTransition(
	desc *PVMRoundDescriptor,
	evidence []PVMSlashEvidence,
	validators []PVMValidatorSlot,
	slashing []PVMSlashEvidence,
	appliedOut, totalLoOut, totalHiOut *uint32,
) error {
	return ErrGPUNotAvailable
}

// EpochTransition returns ErrGPUNotAvailable under nocgo.
func (b *GPUBackend) EpochTransition(
	desc *PVMRoundDescriptor,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	slashing []PVMSlashEvidence,
	epoch *PVMEpochState,
	result *PVMTransitionResult,
	leafScratch []byte,
) error {
	return ErrGPUNotAvailable
}
