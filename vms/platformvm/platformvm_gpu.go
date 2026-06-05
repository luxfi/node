//go:build cgo

// Package platformvm GPU backend — runtime-loaded plugin bridge.
//
// Mirrors the chains/aivm/aivm_gpu.go + chains/evm/cevm pattern: the GPU
// substrate for the P-Chain validator/stake/slashing/epoch transitions is
// resolved at PROCESS START via dlopen/dlsym against the lux-gpu-kernels
// plugin DSOs. This keeps the node module compilable without
// the lux GPU plugin present in the build tree — the plugin is fully
// optional and the chain runs the existing pure-Go path otherwise.
//
// Lookup order (handled by backend.go):
//
//	libluxgpu_backend_cuda.so       (Linux x86_64 + NVIDIA)
//	libluxgpu_backend_hip.so        (Linux x86_64 + AMD ROCm)
//	libluxgpu_backend_metal.dylib   (macOS Apple Silicon / Intel)
//	libluxgpu_backend_vulkan.so/.dylib   (any Vulkan ICD)
//	libluxgpu_backend_webgpu.so/.dylib   (Dawn / wgpu-native)
//
// Each plugin exports four extern "C" host launchers per backend:
//
//	lux_<backend>_platformvm_validator_set_apply
//	lux_<backend>_platformvm_stake_transition
//	lux_<backend>_platformvm_slashing_transition
//	lux_<backend>_platformvm_epoch_transition
//
// Struct layout matches ops/platformvm/cuda/platformvm_kernels_common.cuh
// and platformvm_gpu_layout.hpp byte-for-byte (asserted by init()).
// Pointer ABI is HOST pointers — the launcher does H2D-upload / dispatch /
// D2H-download internally.
//
// FALSE-by-default: even when cgo is on and a plugin loads, the chain
// runs the existing pure-Go path until an operator explicitly sets
// `LUX_PLATFORMVM_GPU=1`. Any launcher error → fall back to Go path
// WITHOUT panic and emit a warn log.
package platformvm

/*
#cgo darwin LDFLAGS: -ldl
#cgo linux  LDFLAGS: -ldl

#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

// Four host-launcher trampolines — invoked by Go via cgo. The function
// pointer is opaque (void*); the cgo bridge casts it to the expected
// C signature and forwards arguments.
//
// All four launcher prototypes are identical across backends (cuda / hip /
// metal / vulkan / webgpu). Argument pointers are HOST pointers; the
// last `void*` is a "stream" handle that is always nullptr on the host-
// pointer ABI backends (metal / vulkan / webgpu) and the cuda/hip stream
// slot from the kernel author's POV.

typedef int (*pvm_validator_set_fn)(
    const void* desc,
    const void* ops,
    void*       validators,
    void*       applied_out,
    uint32_t    validator_count,
    void*       stream);

typedef int (*pvm_stake_fn)(
    const void* desc,
    const void* ops,
    void*       validators,
    void*       stake,
    void*       applied_out,
    uint32_t    validator_count,
    uint32_t    stake_count,
    void*       stream);

typedef int (*pvm_slashing_fn)(
    const void* desc,
    const void* evidence,
    void*       validators,
    void*       slashing,
    void*       applied_out,
    void*       total_lo_out,
    void*       total_hi_out,
    uint32_t    validator_count,
    uint32_t    slashing_count,
    void*       stream);

typedef int (*pvm_epoch_fn)(
    const void* desc,
    void*       validators,
    void*       stake,
    void*       slashing,
    void*       epoch,
    void*       result,
    uint32_t    validator_count,
    uint32_t    stake_count,
    uint32_t    slashing_count,
    void*       leaf_scratch,
    void*       stream);

static int call_pvm_validator_set(void* fn,
                                  const void* desc, const void* ops,
                                  void* validators, void* applied_out,
                                  uint32_t validator_count) {
    return ((pvm_validator_set_fn)fn)(desc, ops, validators, applied_out,
                                       validator_count, NULL);
}
static int call_pvm_stake(void* fn,
                          const void* desc, const void* ops,
                          void* validators, void* stake, void* applied_out,
                          uint32_t validator_count, uint32_t stake_count) {
    return ((pvm_stake_fn)fn)(desc, ops, validators, stake, applied_out,
                               validator_count, stake_count, NULL);
}
static int call_pvm_slashing(void* fn,
                             const void* desc, const void* evidence,
                             void* validators, void* slashing, void* applied_out,
                             void* total_lo_out, void* total_hi_out,
                             uint32_t validator_count, uint32_t slashing_count) {
    return ((pvm_slashing_fn)fn)(desc, evidence, validators, slashing,
                                  applied_out, total_lo_out, total_hi_out,
                                  validator_count, slashing_count, NULL);
}
static int call_pvm_epoch(void* fn,
                          const void* desc, void* validators, void* stake,
                          void* slashing, void* epoch, void* result,
                          uint32_t validator_count, uint32_t stake_count,
                          uint32_t slashing_count, void* leaf_scratch) {
    return ((pvm_epoch_fn)fn)(desc, validators, stake, slashing, epoch, result,
                               validator_count, stake_count, slashing_count,
                               leaf_scratch, NULL);
}

// dlopen / dlsym wrappers — kept here so backend.go can stay pure Go.
static void* lux_pvm_dlopen(const char* path) {
    return dlopen(path, RTLD_NOW | RTLD_LOCAL);
}
static void* lux_pvm_dlsym(void* handle, const char* sym) {
    return dlsym(handle, sym);
}
static const char* lux_pvm_dlerror() {
    return dlerror();
}
static void lux_pvm_dlclose(void* handle) {
    dlclose(handle);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// ErrGPUNotAvailable is returned by every GPUBackend method when no plugin
// was loadable at init time. Callers fall through to the existing Go path.
var ErrGPUNotAvailable = errors.New("platformvm: no GPU plugin available")

// GPUBackendKind identifies which lux-gpu-kernels plugin satisfied the
// runtime dlopen probe. AvailableNone is the sentinel "fall through to Go".
type GPUBackendKind uint8

const (
	GPUNone   GPUBackendKind = 0
	GPUCUDA   GPUBackendKind = 1
	GPUHIP    GPUBackendKind = 2
	GPUMetal  GPUBackendKind = 3
	GPUVulkan GPUBackendKind = 4
	GPUWebGPU GPUBackendKind = 5
)

// String returns the human-readable name for the backend kind.
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
		return fmt.Sprintf("backend(%d)", uint8(k))
	}
}

// =============================================================================
// Layout-drift guards — match ops/platformvm/cuda/platformvm_kernels_common.cuh
// + platformvm_gpu_layout.hpp byte-for-byte.
//
// The struct bytes Go hands to C MUST match the on-disk layout file at
// the GPU plugin install tree ops/platformvm/op.yaml — every kernel reads
// them via reinterpret_cast. A silent layout shift produces consensus-divergent
// state roots. init() refuses to load if any size drifts.
// =============================================================================

// PVMValidatorSlot mirrors platformvm::gpu::ValidatorSlot (176 bytes).
type PVMValidatorSlot struct {
	ValidatorID      uint64
	Weight           uint64
	BLSPubkey        [48]byte
	CoronaPubkey     [32]byte // Corona / Ringtail commitment digest
	MLDSAPubkey      [32]byte
	MLDSAGroth16Root [32]byte
	Status           uint32
	JailUntilEpoch   uint32
	Occupied         uint32
	_pad0            uint32
}

// PVMStakeRecord mirrors platformvm::gpu::StakeRecord (64 bytes).
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

// PVMSlashEvidence mirrors platformvm::gpu::SlashEvidence (80 bytes).
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

// PVMEpochState mirrors platformvm::gpu::EpochState (160 bytes).
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

// PVMRoundDescriptor mirrors platformvm::gpu::PVMRoundDescriptor (96 bytes).
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

// PVMValidatorOp mirrors platformvm::gpu::ValidatorOp (176 bytes).
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

// PVMStakeOp mirrors platformvm::gpu::StakeOp (64 bytes).
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

// PVMTransitionResult mirrors platformvm::gpu::PVMTransitionResult (192 bytes).
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

// PlatformVM op-kind constants — must match
// ops/platformvm/cuda/platformvm_kernels_common.cuh.
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

// Layout-drift guard — refuse to load if any struct size disagrees with
// the on-device layout. Any disagreement here means Go would write
// garbage at the C boundary.
func init() {
	type sz struct {
		name string
		got  uintptr
		want uintptr
	}
	checks := []sz{
		{"PVMValidatorSlot", unsafe.Sizeof(PVMValidatorSlot{}), 176},
		{"PVMStakeRecord", unsafe.Sizeof(PVMStakeRecord{}), 64},
		{"PVMSlashEvidence", unsafe.Sizeof(PVMSlashEvidence{}), 80},
		{"PVMEpochState", unsafe.Sizeof(PVMEpochState{}), 160},
		{"PVMRoundDescriptor", unsafe.Sizeof(PVMRoundDescriptor{}), 96},
		{"PVMValidatorOp", unsafe.Sizeof(PVMValidatorOp{}), 176},
		{"PVMStakeOp", unsafe.Sizeof(PVMStakeOp{}), 64},
		{"PVMTransitionResult", unsafe.Sizeof(PVMTransitionResult{}), 192},
	}
	for _, c := range checks {
		if c.got != c.want {
			panic(fmt.Sprintf(
				"platformvm: layout drift — Go sizeof(%s)=%d but on-device layout=%d. "+
					"Re-sync vms/platformvm/platformvm_gpu.go against "+
					"the GPU plugin install tree ops/platformvm/cuda/platformvm_kernels_common.cuh.",
				c.name, c.got, c.want))
		}
	}
}

// =============================================================================
// GPUBackend — handle to an open plugin DSO + its four resolved launchers.
// =============================================================================

// GPUBackend is a handle to an open lux-gpu-kernels plugin. Zero value is
// usable (every method returns ErrGPUNotAvailable). The active backend is
// stored at package level by backend.go's init(); call ActiveGPUBackend()
// to retrieve it.
type GPUBackend struct {
	mu             sync.Mutex
	kind           GPUBackendKind
	handle         unsafe.Pointer // dlopen result
	path           string
	fnValidatorSet unsafe.Pointer
	fnStake        unsafe.Pointer
	fnSlashing     unsafe.Pointer
	fnEpoch        unsafe.Pointer
}

// Kind returns which backend satisfied the dlopen probe.
func (b *GPUBackend) Kind() GPUBackendKind {
	if b == nil {
		return GPUNone
	}
	return b.kind
}

// Path returns the absolute path of the loaded plugin DSO.
func (b *GPUBackend) Path() string {
	if b == nil {
		return ""
	}
	return b.path
}

// IsAvailable reports whether the backend is loaded AND all four host
// launchers were successfully resolved.
func (b *GPUBackend) IsAvailable() bool {
	if b == nil || b.handle == nil {
		return false
	}
	return b.fnValidatorSet != nil && b.fnStake != nil &&
		b.fnSlashing != nil && b.fnEpoch != nil
}

// openGPUBackend attempts to dlopen `path` and dlsym the four host launchers
// for `kind`. Returns a fully-initialised *GPUBackend on success, or
// (nil, error) when either the dlopen or any dlsym fails.
//
// On dlsym failure the dlopened handle IS dlclose'd before returning so
// we never leak a half-bound plugin.
func openGPUBackend(kind GPUBackendKind, path string) (*GPUBackend, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	// Clear any pending error so a stale dlerror() from a previous failed
	// dlsym doesn't get mis-attributed to this dlopen call.
	C.lux_pvm_dlerror()

	handle := C.lux_pvm_dlopen(cpath)
	if handle == nil {
		return nil, fmt.Errorf("platformvm: dlopen(%s): %s",
			path, C.GoString(C.lux_pvm_dlerror()))
	}

	backendName := kind.String() // cuda / hip / metal / vulkan / webgpu

	resolve := func(suffix string) (unsafe.Pointer, error) {
		sym := fmt.Sprintf("lux_%s_platformvm_%s", backendName, suffix)
		csym := C.CString(sym)
		defer C.free(unsafe.Pointer(csym))
		C.lux_pvm_dlerror()
		ptr := C.lux_pvm_dlsym(handle, csym)
		if ptr == nil {
			return nil, fmt.Errorf("platformvm: dlsym(%s, %s): %s",
				path, sym, C.GoString(C.lux_pvm_dlerror()))
		}
		return ptr, nil
	}

	b := &GPUBackend{kind: kind, handle: handle, path: path}
	var err error
	if b.fnValidatorSet, err = resolve("validator_set_apply"); err != nil {
		C.lux_pvm_dlclose(handle)
		return nil, err
	}
	if b.fnStake, err = resolve("stake_transition"); err != nil {
		C.lux_pvm_dlclose(handle)
		return nil, err
	}
	if b.fnSlashing, err = resolve("slashing_transition"); err != nil {
		C.lux_pvm_dlclose(handle)
		return nil, err
	}
	if b.fnEpoch, err = resolve("epoch_transition"); err != nil {
		C.lux_pvm_dlclose(handle)
		return nil, err
	}
	return b, nil
}

// Close releases the dlopen handle. Idempotent — safe to call on a nil
// receiver or an already-closed backend.
func (b *GPUBackend) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handle == nil {
		return nil
	}
	C.lux_pvm_dlclose(b.handle)
	b.handle = nil
	b.fnValidatorSet = nil
	b.fnStake = nil
	b.fnSlashing = nil
	b.fnEpoch = nil
	return nil
}

// =============================================================================
// Four host launcher wrappers. Each is a thin cgo trampoline that pins the
// Go-side slice memory (via runtime.KeepAlive) for the duration of the C
// call. The launchers ALWAYS take HOST pointers — no D2H/H2D contract on
// the Go side beyond a defer'd KeepAlive on every input/output buffer.
//
// Error contract: any nonzero return code from the launcher → wrap in a Go
// error. The vm.go opt-in helpers fall back to the Go path on error
// WITHOUT panic and the caller is expected to emit a warn log.
// =============================================================================

// ValidatorSetApply runs the GPU validator-set-apply kernel. `validators` is
// read+written in place; `appliedOut` receives the count of successfully
// applied ops.
func (b *GPUBackend) ValidatorSetApply(
	desc *PVMRoundDescriptor,
	ops []PVMValidatorOp,
	validators []PVMValidatorSlot,
	appliedOut *uint32,
) error {
	if !b.IsAvailable() {
		return ErrGPUNotAvailable
	}
	if desc == nil || appliedOut == nil {
		return errors.New("platformvm: ValidatorSetApply: nil desc or appliedOut")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: ValidatorSetApply: empty validators table")
	}

	var opsPtr unsafe.Pointer
	if len(ops) > 0 {
		opsPtr = unsafe.Pointer(&ops[0])
	}
	rc := C.call_pvm_validator_set(
		b.fnValidatorSet,
		unsafe.Pointer(desc),
		opsPtr,
		unsafe.Pointer(&validators[0]),
		unsafe.Pointer(appliedOut),
		C.uint32_t(len(validators)),
	)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(ops)
	runtime.KeepAlive(validators)
	runtime.KeepAlive(appliedOut)
	if rc != 0 {
		return fmt.Errorf("platformvm: %s_platformvm_validator_set_apply returned %d",
			b.kind, int(rc))
	}
	return nil
}

// StakeTransition runs the GPU stake-transition kernel. Reads + writes
// `validators` and `stake` in place; `appliedOut` receives the count.
func (b *GPUBackend) StakeTransition(
	desc *PVMRoundDescriptor,
	ops []PVMStakeOp,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	appliedOut *uint32,
) error {
	if !b.IsAvailable() {
		return ErrGPUNotAvailable
	}
	if desc == nil || appliedOut == nil {
		return errors.New("platformvm: StakeTransition: nil desc or appliedOut")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: StakeTransition: empty validators table")
	}
	if len(stake) == 0 {
		return errors.New("platformvm: StakeTransition: empty stake table")
	}

	var opsPtr unsafe.Pointer
	if len(ops) > 0 {
		opsPtr = unsafe.Pointer(&ops[0])
	}
	rc := C.call_pvm_stake(
		b.fnStake,
		unsafe.Pointer(desc),
		opsPtr,
		unsafe.Pointer(&validators[0]),
		unsafe.Pointer(&stake[0]),
		unsafe.Pointer(appliedOut),
		C.uint32_t(len(validators)),
		C.uint32_t(len(stake)),
	)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(ops)
	runtime.KeepAlive(validators)
	runtime.KeepAlive(stake)
	runtime.KeepAlive(appliedOut)
	if rc != 0 {
		return fmt.Errorf("platformvm: %s_platformvm_stake_transition returned %d",
			b.kind, int(rc))
	}
	return nil
}

// SlashingTransition runs the GPU slashing-transition kernel. Reads +
// writes `validators` and `slashing` in place; `appliedOut` receives the
// count, and `totalLoOut`/`totalHiOut` receive the low/high u32 halves of
// the total slashed amount (u64).
func (b *GPUBackend) SlashingTransition(
	desc *PVMRoundDescriptor,
	evidence []PVMSlashEvidence,
	validators []PVMValidatorSlot,
	slashing []PVMSlashEvidence,
	appliedOut, totalLoOut, totalHiOut *uint32,
) error {
	if !b.IsAvailable() {
		return ErrGPUNotAvailable
	}
	if desc == nil || appliedOut == nil || totalLoOut == nil || totalHiOut == nil {
		return errors.New("platformvm: SlashingTransition: nil desc or output pointer")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: SlashingTransition: empty validators table")
	}

	var evidencePtr, slashingPtr unsafe.Pointer
	if len(evidence) > 0 {
		evidencePtr = unsafe.Pointer(&evidence[0])
	}
	if len(slashing) > 0 {
		slashingPtr = unsafe.Pointer(&slashing[0])
	}
	rc := C.call_pvm_slashing(
		b.fnSlashing,
		unsafe.Pointer(desc),
		evidencePtr,
		unsafe.Pointer(&validators[0]),
		slashingPtr,
		unsafe.Pointer(appliedOut),
		unsafe.Pointer(totalLoOut),
		unsafe.Pointer(totalHiOut),
		C.uint32_t(len(validators)),
		C.uint32_t(len(slashing)),
	)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(evidence)
	runtime.KeepAlive(validators)
	runtime.KeepAlive(slashing)
	runtime.KeepAlive(appliedOut)
	runtime.KeepAlive(totalLoOut)
	runtime.KeepAlive(totalHiOut)
	if rc != 0 {
		return fmt.Errorf("platformvm: %s_platformvm_slashing_transition returned %d",
			b.kind, int(rc))
	}
	return nil
}

// EpochTransition runs the GPU epoch-finalisation kernel. Composes the
// per-epoch validator_set_root / stake_root / slashing_root / epoch_root
// (which doubles as pchain_validator_root for Quasar binding).
//
// `leafScratch` is an optional byte arena the kernel uses for keccak leaf
// hashes. If nil or empty the kernel allocates internally. Otherwise it
// must be sized to at least 32 × max(validator_count, stake_count,
// slashing_count) bytes.
func (b *GPUBackend) EpochTransition(
	desc *PVMRoundDescriptor,
	validators []PVMValidatorSlot,
	stake []PVMStakeRecord,
	slashing []PVMSlashEvidence,
	epoch *PVMEpochState,
	result *PVMTransitionResult,
	leafScratch []byte,
) error {
	if !b.IsAvailable() {
		return ErrGPUNotAvailable
	}
	if desc == nil || epoch == nil || result == nil {
		return errors.New("platformvm: EpochTransition: nil desc, epoch, or result")
	}
	if len(validators) == 0 {
		return errors.New("platformvm: EpochTransition: empty validators table")
	}

	var stakePtr, slashingPtr, scratchPtr unsafe.Pointer
	if len(stake) > 0 {
		stakePtr = unsafe.Pointer(&stake[0])
	}
	if len(slashing) > 0 {
		slashingPtr = unsafe.Pointer(&slashing[0])
	}
	if len(leafScratch) > 0 {
		scratchPtr = unsafe.Pointer(&leafScratch[0])
	}
	rc := C.call_pvm_epoch(
		b.fnEpoch,
		unsafe.Pointer(desc),
		unsafe.Pointer(&validators[0]),
		stakePtr,
		slashingPtr,
		unsafe.Pointer(epoch),
		unsafe.Pointer(result),
		C.uint32_t(len(validators)),
		C.uint32_t(len(stake)),
		C.uint32_t(len(slashing)),
		scratchPtr,
	)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(validators)
	runtime.KeepAlive(stake)
	runtime.KeepAlive(slashing)
	runtime.KeepAlive(epoch)
	runtime.KeepAlive(result)
	runtime.KeepAlive(leafScratch)
	if rc != 0 {
		return fmt.Errorf("platformvm: %s_platformvm_epoch_transition returned %d",
			b.kind, int(rc))
	}
	return nil
}
