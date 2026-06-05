//go:build !cgo

// Package xvm GPU backend — pure-Go bridge for the !cgo build.
//
// The cgo build (xvm_gpu.go + backend.go) uses dlopen/dlsym to find a
// lux-gpu-kernels plugin at process start. Without cgo there's no way to
// reach a C function pointer, so every GPUBackend method routes directly
// to the canonical pure-Go implementations in xvm_gpu_cpu.go (which the
// cgo build also falls back to on launcher errors).
//
// One Go impl, two dispatch paths. Output is byte-identical to the cgo
// build — locked by TestXVMGPUBridge_CgoNocgoParity.
//
// This file keeps the public API surface identical between build modes:
// the same struct names, the same method signatures, the same package
// constants. Only the dispatch path differs.
package xvm

import (
	"errors"
	"fmt"
	"unsafe"
)

// ErrGPUNotAvailable mirrors the cgo build's sentinel. Callers can
// compare against it without caring which build mode is active. Returned
// only by methods that strictly require a loaded plugin (none of the
// four bridge methods do — they always succeed via the Go path).
var ErrGPUNotAvailable = errors.New("xvm: GPU plugin unavailable (built without CGo)")

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
// unreachable on this build but kept declared so callers compile against
// the same constants either way.
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
// Layout structs — kept fully declared so package-internal helpers
// compile identically in both modes. Field tags and sizes are NOT
// enforced under nocgo (no cgo boundary to validate against).
// =============================================================================

// XVMUTXO mirrors the cgo build's layout.
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

// XVMInputBatch mirrors the cgo build's layout.
type XVMInputBatch struct {
	TxID          [32]byte
	InputOffset   uint32
	InputCount    uint32
	WitnessOffset uint32
	WitnessCount  uint32
	_pad0         uint64
	_pad1         uint64
}

// XVMOutputBatch mirrors the cgo build's layout.
type XVMOutputBatch struct {
	TxID         [32]byte
	OutputOffset uint32
	OutputCount  uint32
	_pad0        uint64
	_pad1        uint64
	_pad2        uint64
}

// XVMAsset mirrors the cgo build's layout.
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

// XVMCuckooEntry mirrors the cgo build's layout.
type XVMCuckooEntry struct {
	UtxoID    [32]byte
	SlotIndex uint32
	Occupied  uint32
	_pad0     uint64
}

// XVMAtomicExportMarker mirrors the cgo build's layout.
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

// XVMTx mirrors the cgo build's layout.
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

// XVMAssetOp mirrors the cgo build's layout.
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

// XVMRoundDescriptor mirrors the cgo build's layout.
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

// XVMTransitionResult mirrors the cgo build's layout.
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

// Constants — identical to the cgo build so callers compile either way.
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

// Layout-drift guard — same check as the cgo build but advisory only
// (there's no C boundary to compare against). Keeps the failure surface
// symmetric: a layout regression panics regardless of build mode.
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
				"xvm: layout drift — Go sizeof(%s)=%d but on-device layout=%d.",
				c.name, c.got, c.want))
		}
	}
}

// =============================================================================
// GPUBackend stub — methods route to the canonical pure-Go impls in
// xvm_gpu_cpu.go. Identical output to the cgo build.
// =============================================================================

// GPUBackend is the nocgo handle. Holding it is unconditionally safe;
// every method calls into xvm_gpu_cpu.go so the bridge produces the same
// result regardless of build mode.
type GPUBackend struct{}

// openGPUBackend is the nocgo entry point used by backend.go. Always
// returns (nil, ErrGPUNotAvailable) since dlopen requires cgo. The
// resulting nil *GPUBackend still services every method via the Go path.
func openGPUBackend(_ GPUBackendKind, _ string) (*GPUBackend, error) {
	return nil, ErrGPUNotAvailable
}

// Kind always returns GPUNone under nocgo.
func (b *GPUBackend) Kind() GPUBackendKind { return GPUNone }

// Path returns an empty string under nocgo.
func (b *GPUBackend) Path() string { return "" }

// IsAvailable always returns false under nocgo. Callers that branch on
// this still get correct output through the bridge methods — Go impl
// runs unconditionally.
func (b *GPUBackend) IsAvailable() bool { return false }

// Close is a no-op under nocgo.
func (b *GPUBackend) Close() error { return nil }

// UTXOTransition routes to cpuXVMUTXOTransition.
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

// AssetTransition routes to cpuXVMAssetTransition.
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

// MembershipRebuild routes to cpuXVMMembershipRebuild.
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
	cpuXVMMembershipRebuild(utxos, bloom, cuckoo, utxoCount, bloomBitCount, cuckooBucketCount)
	return nil
}

// RootUpdate routes to cpuXVMRootUpdate.
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
	cpuXVMRootUpdate(desc, txs, utxos, assets, result, txCount, utxoCount, assetCount)
	return nil
}
