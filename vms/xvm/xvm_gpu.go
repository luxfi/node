//go:build cgo

// Package xvm GPU backend — runtime-loaded plugin bridge.
//
// Mirrors the platformvm GPU bridge pattern: the GPU substrate for the
// X-Chain UTXO + asset + membership + root-update transitions is resolved
// at PROCESS START via dlopen/dlsym against the lux-gpu-kernels plugin
// DSOs. This keeps the node module compilable without the lux GPU plugin
// present in the build tree — the plugin is fully optional and the chain
// runs the canonical pure-Go path in xvm_gpu_cpu.go otherwise.
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
//	lux_<backend>_xvm_utxo_transition
//	lux_<backend>_xvm_asset_transition
//	lux_<backend>_xvm_membership_rebuild
//	lux_<backend>_xvm_root_update
//
// Struct layout matches ops/xvm/cuda/xvm_kernels_common.cuh byte-for-byte
// (asserted by init()). Pointer ABI is HOST pointers — the launcher does
// H2D-upload / dispatch / D2H-download internally.
//
// Dispatch contract: if a plugin opens cleanly, the cgo trampoline is
// tried first. On a nonzero launcher return code (or unbound handle),
// the canonical pure-Go path (cpuXVM*) runs against the SAME input +
// output buffers — GPU is a strict positive overlay. Either dispatch
// path produces byte-identical results — validated by
// TestXVMGPUBridge_CgoNocgoParity.
package xvm

/*
#cgo darwin LDFLAGS: -ldl
#cgo linux  LDFLAGS: -ldl

#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

// Four host-launcher trampolines — invoked by Go via cgo. Each function
// pointer is opaque (void*); the cgo bridge casts it to the expected
// C signature and forwards arguments.
//
// All four launcher prototypes are identical across backends (cuda / hip /
// metal / vulkan / webgpu). Argument pointers are HOST pointers; the
// trailing `void*` is a "stream" handle that is always nullptr on the
// host-pointer ABI backends (metal / vulkan / webgpu) and the cuda/hip
// stream slot from the kernel author's POV. We always pass NULL.

typedef int (*xvm_utxo_fn)(
    const void* desc,
    void*       txs,
    const void* input_batches,
    const void* output_batches,
    const void* inputs,
    const void* outputs,
    void*       utxos,
    void*       bloom_bits,
    void*       cuckoo,
    void*       inputs_consumed_out,
    void*       outputs_created_out,
    uint32_t    utxo_count,
    uint32_t    bloom_bit_count,
    uint32_t    cuckoo_bucket_count,
    uint32_t    input_batch_count,
    uint32_t    output_batch_count,
    uint32_t    outputs_count,
    void*       stream);

typedef int (*xvm_asset_fn)(
    const void* desc,
    void*       txs,
    const void* asset_ops,
    void*       assets,
    void*       markers,
    void*       applied_out,
    void*       exports_out,
    void*       imports_out,
    void*       minted_lo_out,
    void*       minted_hi_out,
    void*       burned_lo_out,
    void*       burned_hi_out,
    uint32_t    asset_count,
    uint32_t    asset_op_count,
    uint32_t    marker_count,
    void*       stream);

typedef int (*xvm_membership_fn)(
    void*    utxos,
    void*    bloom_bits,
    void*    cuckoo,
    uint32_t utxo_count,
    uint32_t bloom_bit_count,
    uint32_t cuckoo_bucket_count,
    void*    stream);

typedef int (*xvm_roots_fn)(
    const void* desc,
    const void* txs,
    const void* utxos,
    const void* assets,
    void*       result,
    uint32_t    tx_count,
    uint32_t    utxo_count,
    uint32_t    asset_count,
    void*       stream);

static int call_xvm_utxo(void* fn,
                         const void* desc, void* txs,
                         const void* input_batches, const void* output_batches,
                         const void* inputs, const void* outputs,
                         void* utxos, void* bloom_bits, void* cuckoo,
                         void* inputs_consumed_out, void* outputs_created_out,
                         uint32_t utxo_count, uint32_t bloom_bit_count,
                         uint32_t cuckoo_bucket_count,
                         uint32_t input_batch_count, uint32_t output_batch_count,
                         uint32_t outputs_count) {
    return ((xvm_utxo_fn)fn)(desc, txs, input_batches, output_batches,
                              inputs, outputs, utxos, bloom_bits, cuckoo,
                              inputs_consumed_out, outputs_created_out,
                              utxo_count, bloom_bit_count, cuckoo_bucket_count,
                              input_batch_count, output_batch_count, outputs_count,
                              NULL);
}

static int call_xvm_asset(void* fn,
                          const void* desc, void* txs,
                          const void* asset_ops, void* assets, void* markers,
                          void* applied_out, void* exports_out, void* imports_out,
                          void* minted_lo_out, void* minted_hi_out,
                          void* burned_lo_out, void* burned_hi_out,
                          uint32_t asset_count, uint32_t asset_op_count,
                          uint32_t marker_count) {
    return ((xvm_asset_fn)fn)(desc, txs, asset_ops, assets, markers,
                               applied_out, exports_out, imports_out,
                               minted_lo_out, minted_hi_out,
                               burned_lo_out, burned_hi_out,
                               asset_count, asset_op_count, marker_count,
                               NULL);
}

static int call_xvm_membership(void* fn,
                               void* utxos, void* bloom_bits, void* cuckoo,
                               uint32_t utxo_count, uint32_t bloom_bit_count,
                               uint32_t cuckoo_bucket_count) {
    return ((xvm_membership_fn)fn)(utxos, bloom_bits, cuckoo,
                                    utxo_count, bloom_bit_count, cuckoo_bucket_count,
                                    NULL);
}

static int call_xvm_roots(void* fn,
                          const void* desc, const void* txs,
                          const void* utxos, const void* assets,
                          void* result, uint32_t tx_count,
                          uint32_t utxo_count, uint32_t asset_count) {
    return ((xvm_roots_fn)fn)(desc, txs, utxos, assets, result,
                               tx_count, utxo_count, asset_count, NULL);
}

// dlopen / dlsym wrappers — kept here so backend.go can stay pure Go.
static void* lux_xvm_dlopen(const char* path) {
    return dlopen(path, RTLD_NOW | RTLD_LOCAL);
}
static void* lux_xvm_dlsym(void* handle, const char* sym) {
    return dlsym(handle, sym);
}
static const char* lux_xvm_dlerror() {
    return dlerror();
}
static void lux_xvm_dlclose(void* handle) {
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

// ErrGPUNotAvailable is the sentinel returned when no plugin was loadable
// at init time. The bridge methods do NOT bubble this up — they fall
// through to the pure-Go path. It is exported so callers that want to
// branch on availability can compare against it.
var ErrGPUNotAvailable = errors.New("xvm: no GPU plugin available")

// GPUBackendKind identifies which lux-gpu-kernels plugin satisfied the
// runtime dlopen probe. GPUNone is the sentinel "fall through to Go".
type GPUBackendKind uint8

const (
	GPUNone   GPUBackendKind = 0
	GPUCUDA   GPUBackendKind = 1
	GPUHIP    GPUBackendKind = 2
	GPUMetal  GPUBackendKind = 3
	GPUVulkan GPUBackendKind = 4
	GPUWebGPU GPUBackendKind = 5
)

// String returns the human-readable backend kind name.
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
// Layout structs — match ops/xvm/cuda/xvm_kernels_common.cuh byte-for-byte.
//
// The struct bytes Go hands to C MUST match the on-device layout. A
// silent layout shift produces consensus-divergent state roots. init()
// refuses to load if any size drifts.
// =============================================================================

// XVMUTXO mirrors xvm::cuda::UTXO (144 bytes).
type XVMUTXO struct {
	UtxoID          [32]byte
	AssetID         [32]byte
	AmountLo        uint64
	AmountHi        uint64
	OwnerRoot       [32]byte
	Locktime        uint64
	Threshold       uint32
	Status          uint32
	AddressesOffset uint32
	AddressesCount  uint32
	_pad0           uint64
}

// XVMInputBatch mirrors xvm::cuda::InputBatch (64 bytes).
type XVMInputBatch struct {
	TxID          [32]byte
	InputOffset   uint32
	InputCount    uint32
	WitnessOffset uint32
	WitnessCount  uint32
	_pad0         uint64
	_pad1         uint64 // align(16) tail pad
}

// XVMOutputBatch mirrors xvm::cuda::OutputBatch (64 bytes).
type XVMOutputBatch struct {
	TxID         [32]byte
	OutputOffset uint32
	OutputCount  uint32
	_pad0        uint64
	_pad1        uint64
	_pad2        uint64
}

// XVMAsset mirrors xvm::cuda::Asset (112 bytes).
type XVMAsset struct {
	AssetID           [32]byte
	TotalSupplyLo     uint64
	TotalSupplyHi     uint64
	MintAuthorityRoot [32]byte
	FreezeFlag        uint32
	Denomination      uint32
	NameOffset        uint32
	NameLength        uint32
	Occupied          uint32
	_pad0             uint32
	_pad1             uint64
}

// XVMCuckooEntry mirrors xvm::cuda::CuckooEntry (48 bytes).
type XVMCuckooEntry struct {
	UtxoID    [32]byte
	SlotIndex uint32
	Occupied  uint32
	_pad0     uint64
}

// XVMAtomicExportMarker mirrors xvm::cuda::AtomicExportMarker (144 bytes).
type XVMAtomicExportMarker struct {
	MarkerID      [32]byte
	AssetID       [32]byte
	AmountLo      uint64
	AmountHi      uint64
	SourceChain   uint32
	TargetChain   uint32
	Status        uint32
	Occupied      uint32
	RecipientRoot [32]byte
	_pad0         uint64
	_pad1         uint64
}

// XVMTx mirrors xvm::cuda::XvmTx (112 bytes).
type XVMTx struct {
	TxID               [32]byte
	Kind               uint32
	InputBatchOffset   uint32
	OutputBatchOffset  uint32
	AssetChangesOffset uint32
	AssetChangesCount  uint32
	TargetChain        uint32
	Status             uint32
	RejectReason       uint32
	ProofDigest        [32]byte
	_pad0              uint64
	_pad1              uint64
}

// XVMAssetOp mirrors xvm::cuda::AssetOp (112 bytes).
type XVMAssetOp struct {
	AssetID              [32]byte
	AmountLo             uint64
	AmountHi             uint64
	AuthorityWitnessRoot [32]byte
	Kind                 uint32
	TargetChain          uint32
	_pad0                uint64
	_pad1                uint64
	_pad2                uint64
}

// XVMRoundDescriptor mirrors xvm::cuda::XVMRoundDescriptor (112 bytes).
type XVMRoundDescriptor struct {
	ChainID             uint64
	Round               uint64
	TimestampNS         uint64
	Height              uint64
	Mode                uint32
	TxCount             uint32
	InputCount          uint32
	OutputCount         uint32
	AssetOpCount        uint32
	InputBatchCount     uint32
	OutputBatchCount    uint32
	ClosingFlag         uint32
	ParentExecutionRoot [32]byte
	_pad0               uint64
	_pad1               uint64
}

// XVMTransitionResult mirrors xvm::cuda::XVMTransitionResult (208 bytes).
type XVMTransitionResult struct {
	Status          uint32
	TxAccepted      uint32
	TxRejected      uint32
	InputsConsumed  uint32
	OutputsCreated  uint32
	AssetOpsApplied uint32
	ExportMarkers   uint32
	ImportVerified  uint32
	TotalBurnedLo   uint64
	TotalBurnedHi   uint64
	TotalMintedLo   uint64
	TotalMintedHi   uint64
	Height          uint64
	_pad0           uint64
	UtxoRoot        [32]byte
	AssetRoot       [32]byte
	TxRoot          [32]byte
	ExecutionRoot   [32]byte
}

// XVM constants — must match ops/xvm/cuda/xvm_kernels_common.cuh.
const (
	xvmUtxoOccupied uint32 = 0x1
	xvmUtxoSpent    uint32 = 0x2

	xvmAssetActive uint32 = 0x1
	xvmAssetFrozen uint32 = 0x2

	xvmExportPending  uint32 = 0
	xvmExportConsumed uint32 = 1

	xvmTxStatusPending  uint32 = 0
	xvmTxStatusAccepted uint32 = 1
	xvmTxStatusRejected uint32 = 2

	xvmRejectMissingInput   uint32 = 1
	xvmRejectDuplicateInput uint32 = 2
	xvmRejectAlreadySpent   uint32 = 3
	xvmRejectLocktime       uint32 = 4
	xvmRejectAuth           uint32 = 5
	xvmRejectMintAuthority  uint32 = 6
	xvmRejectAssetMissing   uint32 = 7
	xvmRejectImportNoMarker uint32 = 8
	xvmRejectArenaFull      uint32 = 9
	xvmRejectAmountOverflow uint32 = 10

	xvmTxTransfer uint32 = 0
	xvmTxMint     uint32 = 1
	xvmTxBurn     uint32 = 2
	xvmTxExport   uint32 = 3
	xvmTxImport   uint32 = 4

	xvmAssetOpMint     uint32 = 0
	xvmAssetOpBurn     uint32 = 1
	xvmAssetOpTransfer uint32 = 2
	xvmAssetOpExport   uint32 = 3
	xvmAssetOpImport   uint32 = 4

	xvmBloomHashes          = 4
	xvmCuckooSlotsPerBucket = 4

	xvmModeInputCheck      uint32 = 0
	xvmModeTransitionApply uint32 = 1
	xvmModeAssetTransition uint32 = 2
	xvmModeRootUpdate      uint32 = 3
	xvmModeFullRound       uint32 = 4
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
		{"XVMUTXO", unsafe.Sizeof(XVMUTXO{}), 144},
		{"XVMInputBatch", unsafe.Sizeof(XVMInputBatch{}), 64},
		{"XVMOutputBatch", unsafe.Sizeof(XVMOutputBatch{}), 64},
		{"XVMAsset", unsafe.Sizeof(XVMAsset{}), 112},
		{"XVMCuckooEntry", unsafe.Sizeof(XVMCuckooEntry{}), 48},
		{"XVMAtomicExportMarker", unsafe.Sizeof(XVMAtomicExportMarker{}), 144},
		{"XVMTx", unsafe.Sizeof(XVMTx{}), 112},
		{"XVMAssetOp", unsafe.Sizeof(XVMAssetOp{}), 112},
		{"XVMRoundDescriptor", unsafe.Sizeof(XVMRoundDescriptor{}), 112},
		{"XVMTransitionResult", unsafe.Sizeof(XVMTransitionResult{}), 208},
	}
	for _, c := range checks {
		if c.got != c.want {
			panic(fmt.Sprintf(
				"xvm: layout drift — Go sizeof(%s)=%d but on-device layout=%d. "+
					"Re-sync vms/xvm/xvm_gpu.go against "+
					"the GPU plugin ops/xvm/cuda/xvm_kernels_common.cuh.",
				c.name, c.got, c.want))
		}
	}
}

// =============================================================================
// GPUBackend — handle to an open plugin DSO + its four resolved launchers.
// =============================================================================

// GPUBackend is a handle to an open lux-gpu-kernels plugin. Zero value is
// usable — every method falls back to the pure-Go path automatically.
// The active backend is stored at package level by backend.go's init();
// call ActiveGPUBackend() to retrieve it.
type GPUBackend struct {
	mu             sync.Mutex
	kind           GPUBackendKind
	handle         unsafe.Pointer // dlopen result
	path           string
	fnUTXO         unsafe.Pointer
	fnAsset        unsafe.Pointer
	fnMembership   unsafe.Pointer
	fnRoots        unsafe.Pointer
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
	return b.fnUTXO != nil && b.fnAsset != nil &&
		b.fnMembership != nil && b.fnRoots != nil
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
	C.lux_xvm_dlerror()

	handle := C.lux_xvm_dlopen(cpath)
	if handle == nil {
		return nil, fmt.Errorf("xvm: dlopen(%s): %s",
			path, C.GoString(C.lux_xvm_dlerror()))
	}

	backendName := kind.String() // cuda / hip / metal / vulkan / webgpu

	resolve := func(suffix string) (unsafe.Pointer, error) {
		sym := fmt.Sprintf("lux_%s_xvm_%s", backendName, suffix)
		csym := C.CString(sym)
		defer C.free(unsafe.Pointer(csym))
		C.lux_xvm_dlerror()
		ptr := C.lux_xvm_dlsym(handle, csym)
		if ptr == nil {
			return nil, fmt.Errorf("xvm: dlsym(%s, %s): %s",
				path, sym, C.GoString(C.lux_xvm_dlerror()))
		}
		return ptr, nil
	}

	b := &GPUBackend{kind: kind, handle: handle, path: path}
	var err error
	if b.fnUTXO, err = resolve("utxo_transition"); err != nil {
		C.lux_xvm_dlclose(handle)
		return nil, err
	}
	if b.fnAsset, err = resolve("asset_transition"); err != nil {
		C.lux_xvm_dlclose(handle)
		return nil, err
	}
	if b.fnMembership, err = resolve("membership_rebuild"); err != nil {
		C.lux_xvm_dlclose(handle)
		return nil, err
	}
	if b.fnRoots, err = resolve("root_update"); err != nil {
		C.lux_xvm_dlclose(handle)
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
	C.lux_xvm_dlclose(b.handle)
	b.handle = nil
	b.fnUTXO = nil
	b.fnAsset = nil
	b.fnMembership = nil
	b.fnRoots = nil
	return nil
}

// =============================================================================
// Four host launcher wrappers. Each is a thin cgo trampoline that pins the
// Go-side slice memory (via runtime.KeepAlive) for the duration of the C
// call. The launchers ALWAYS take HOST pointers — no D2H/H2D contract on
// the Go side beyond a defer'd KeepAlive on every input/output buffer.
//
// Dispatch contract: GPU plugin first, fall through to the pure-Go path
// in xvm_gpu_cpu.go on ANY launcher error (or unbound backend). Both
// paths produce byte-identical results.
// =============================================================================

// UTXOTransition runs the UTXO transition. Mutates `txs`, `utxos`, `bloom`,
// `cuckoo` in place; writes `*inputsConsumed` and `*outputsCreated`. The
// `desc` carries the canonical tx/input/output counts.
func (b *GPUBackend) UTXOTransition(
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
	inputsConsumed, outputsCreated *uint32,
) error {
	if desc == nil || inputsConsumed == nil || outputsCreated == nil {
		return errors.New("xvm: UTXOTransition: nil desc or counter pointer")
	}
	if len(utxos) == 0 {
		return errors.New("xvm: UTXOTransition: empty utxos arena")
	}
	if len(bloom) == 0 {
		return errors.New("xvm: UTXOTransition: empty bloom bits")
	}
	if len(cuckoo) == 0 {
		return errors.New("xvm: UTXOTransition: empty cuckoo arena")
	}

	if b.IsAvailable() {
		var ibPtr, obPtr, inPtr, outPtr unsafe.Pointer
		if len(inputBatches) > 0 {
			ibPtr = unsafe.Pointer(&inputBatches[0])
		}
		if len(outputBatches) > 0 {
			obPtr = unsafe.Pointer(&outputBatches[0])
		}
		if len(inputs) > 0 {
			inPtr = unsafe.Pointer(&inputs[0])
		}
		if len(outputs) > 0 {
			outPtr = unsafe.Pointer(&outputs[0])
		}
		rc := C.call_xvm_utxo(
			b.fnUTXO,
			unsafe.Pointer(desc),
			cgoTxsPtr(txs),
			ibPtr, obPtr, inPtr, outPtr,
			unsafe.Pointer(&utxos[0]),
			unsafe.Pointer(&bloom[0]),
			unsafe.Pointer(&cuckoo[0]),
			unsafe.Pointer(inputsConsumed),
			unsafe.Pointer(outputsCreated),
			C.uint32_t(utxoCount),
			C.uint32_t(bloomBitCount),
			C.uint32_t(cuckooBucketCount),
			C.uint32_t(inputBatchCount),
			C.uint32_t(outputBatchCount),
			C.uint32_t(outputsCount),
		)
		runtime.KeepAlive(desc)
		runtime.KeepAlive(txs)
		runtime.KeepAlive(inputBatches)
		runtime.KeepAlive(outputBatches)
		runtime.KeepAlive(inputs)
		runtime.KeepAlive(outputs)
		runtime.KeepAlive(utxos)
		runtime.KeepAlive(bloom)
		runtime.KeepAlive(cuckoo)
		runtime.KeepAlive(inputsConsumed)
		runtime.KeepAlive(outputsCreated)
		if rc == 0 {
			return nil
		}
		// Plugin returned an error — fall through to the Go path.
	}

	ic, oc := cpuXVMUTXOTransition(
		desc, txs, inputBatches, outputBatches, inputs, outputs,
		utxos, bloom, cuckoo,
		utxoCount, bloomBitCount, cuckooBucketCount,
		inputBatchCount, outputBatchCount, outputsCount,
	)
	*inputsConsumed = ic
	*outputsCreated = oc
	return nil
}

// AssetTransition runs the asset transition. Mutates `txs`, `assets`,
// `markers` in place; writes the seven u32 output counters via the
// supplied pointers.
func (b *GPUBackend) AssetTransition(
	desc *XVMRoundDescriptor,
	txs []XVMTx,
	assetOps []XVMAssetOp,
	assets []XVMAsset,
	markers []XVMAtomicExportMarker,
	assetCount, assetOpCount, markerCount uint32,
	applied, exports, imports, mintedLo, mintedHi, burnedLo, burnedHi *uint32,
) error {
	if desc == nil {
		return errors.New("xvm: AssetTransition: nil desc")
	}
	if applied == nil || exports == nil || imports == nil ||
		mintedLo == nil || mintedHi == nil ||
		burnedLo == nil || burnedHi == nil {
		return errors.New("xvm: AssetTransition: nil counter pointer")
	}
	if len(assets) == 0 {
		return errors.New("xvm: AssetTransition: empty assets table")
	}
	if len(markers) == 0 {
		return errors.New("xvm: AssetTransition: empty markers arena")
	}

	if b.IsAvailable() {
		var opsPtr unsafe.Pointer
		if len(assetOps) > 0 {
			opsPtr = unsafe.Pointer(&assetOps[0])
		}
		rc := C.call_xvm_asset(
			b.fnAsset,
			unsafe.Pointer(desc),
			cgoTxsPtr(txs),
			opsPtr,
			unsafe.Pointer(&assets[0]),
			unsafe.Pointer(&markers[0]),
			unsafe.Pointer(applied),
			unsafe.Pointer(exports),
			unsafe.Pointer(imports),
			unsafe.Pointer(mintedLo),
			unsafe.Pointer(mintedHi),
			unsafe.Pointer(burnedLo),
			unsafe.Pointer(burnedHi),
			C.uint32_t(assetCount),
			C.uint32_t(assetOpCount),
			C.uint32_t(markerCount),
		)
		runtime.KeepAlive(desc)
		runtime.KeepAlive(txs)
		runtime.KeepAlive(assetOps)
		runtime.KeepAlive(assets)
		runtime.KeepAlive(markers)
		runtime.KeepAlive(applied)
		runtime.KeepAlive(exports)
		runtime.KeepAlive(imports)
		runtime.KeepAlive(mintedLo)
		runtime.KeepAlive(mintedHi)
		runtime.KeepAlive(burnedLo)
		runtime.KeepAlive(burnedHi)
		if rc == 0 {
			return nil
		}
	}

	a, e, i, mlo, mhi, blo, bhi := cpuXVMAssetTransition(desc, txs, assetOps, assets, markers, assetOpCount)
	*applied = a
	*exports = e
	*imports = i
	*mintedLo = mlo
	*mintedHi = mhi
	*burnedLo = blo
	*burnedHi = bhi
	return nil
}

// MembershipRebuild clears the cuckoo arena and re-seeds bloom + cuckoo
// from the live UTXO arena. The bloom byte array must be pre-zeroed by
// the caller per the contract documented at ops/xvm/cuda/xvm_membership.cu.
func (b *GPUBackend) MembershipRebuild(
	utxos []XVMUTXO,
	bloom []byte,
	cuckoo []XVMCuckooEntry,
	utxoCount, bloomBitCount, cuckooBucketCount uint32,
) error {
	if len(utxos) == 0 {
		return errors.New("xvm: MembershipRebuild: empty utxos arena")
	}
	if len(bloom) == 0 {
		return errors.New("xvm: MembershipRebuild: empty bloom bits")
	}
	if len(cuckoo) == 0 {
		return errors.New("xvm: MembershipRebuild: empty cuckoo arena")
	}

	if b.IsAvailable() {
		rc := C.call_xvm_membership(
			b.fnMembership,
			unsafe.Pointer(&utxos[0]),
			unsafe.Pointer(&bloom[0]),
			unsafe.Pointer(&cuckoo[0]),
			C.uint32_t(utxoCount),
			C.uint32_t(bloomBitCount),
			C.uint32_t(cuckooBucketCount),
		)
		runtime.KeepAlive(utxos)
		runtime.KeepAlive(bloom)
		runtime.KeepAlive(cuckoo)
		if rc == 0 {
			return nil
		}
	}

	cpuXVMMembershipRebuild(utxos, bloom, cuckoo, utxoCount, bloomBitCount, cuckooBucketCount)
	return nil
}

// RootUpdate composes the per-round utxo_root / asset_root / tx_root and
// the composed execution_root into `result`. Pure read of `txs`, `utxos`,
// `assets`; mutation only on `result`.
func (b *GPUBackend) RootUpdate(
	desc *XVMRoundDescriptor,
	txs []XVMTx,
	utxos []XVMUTXO,
	assets []XVMAsset,
	result *XVMTransitionResult,
	txCount, utxoCount, assetCount uint32,
) error {
	if desc == nil || result == nil {
		return errors.New("xvm: RootUpdate: nil desc or result")
	}
	if len(utxos) == 0 {
		return errors.New("xvm: RootUpdate: empty utxos arena")
	}
	if len(assets) == 0 {
		return errors.New("xvm: RootUpdate: empty assets table")
	}

	if b.IsAvailable() {
		rc := C.call_xvm_roots(
			b.fnRoots,
			unsafe.Pointer(desc),
			cgoTxsConstPtr(txs),
			unsafe.Pointer(&utxos[0]),
			unsafe.Pointer(&assets[0]),
			unsafe.Pointer(result),
			C.uint32_t(txCount),
			C.uint32_t(utxoCount),
			C.uint32_t(assetCount),
		)
		runtime.KeepAlive(desc)
		runtime.KeepAlive(txs)
		runtime.KeepAlive(utxos)
		runtime.KeepAlive(assets)
		runtime.KeepAlive(result)
		if rc == 0 {
			return nil
		}
	}

	cpuXVMRootUpdate(desc, txs, utxos, assets, result, txCount, utxoCount, assetCount)
	return nil
}

// cgoTxsPtr returns a host pointer to the first element of `txs`. The
// launcher signature takes `void* txs` (non-const) — Go passes the slice
// header's data pointer. Empty slice → NULL.
func cgoTxsPtr(txs []XVMTx) unsafe.Pointer {
	if len(txs) == 0 {
		return nil
	}
	return unsafe.Pointer(&txs[0])
}

// cgoTxsConstPtr is the const variant — same address, semantically read-only.
func cgoTxsConstPtr(txs []XVMTx) unsafe.Pointer {
	if len(txs) == 0 {
		return nil
	}
	return unsafe.Pointer(&txs[0])
}
