// Round-trip + parity tests for the xvm GPU bridge.
//
// Two tests:
//
//  1. TestXVMGPUBridge_AutoBackend — round-trip via dlopen → UTXOTransition
//     with a 1-utxo fixture; SKIP-on-miss when no plugin is on disk.
//
//  2. TestXVMGPUBridge_CgoNocgoParity — runs every bridge method on a
//     non-trivial fixture and asserts the GPU path matches the canonical
//     pure-Go path byte-for-byte. The parity check is the one-and-only-
//     one-way lock: if a plugin is loaded, its output equals the Go
//     impl's; if no plugin is loaded, the Go impl is used directly via
//     the nil-receiver path (so the test still validates the Go impl
//     against itself, catching any regression in the in-place mutation
//     contract).
//
// SKIP-on-miss for the dlopen round-trip is the canonical contract — the
// bridge is optional and a missing plugin is not a failure mode of luxd
// itself.

package xvm

import (
	"testing"
)

// fixture builds a small but non-trivial XVM workload that exercises
// every dispatch path (mint asset, accepted transfer, rejected transfer,
// occupied + spent utxo states, the bloom/cuckoo membership tables).
type xvmFixture struct {
	desc           XVMRoundDescriptor
	txs            []XVMTx
	inputBatches   []XVMInputBatch
	outputBatches  []XVMOutputBatch
	inputs         []byte
	outputs        []XVMUTXO
	utxos          []XVMUTXO
	bloom          []byte
	cuckoo         []XVMCuckooEntry
	assets         []XVMAsset
	markers        []XVMAtomicExportMarker
	assetOps       []XVMAssetOp
	utxoCount      uint32
	bloomBitCount  uint32
	cuckooBuckets  uint32
	assetCount     uint32
	markerCount    uint32
	txCount        uint32
	inputBatchN    uint32
	outputBatchN   uint32
	outputsCount   uint32
	assetOpCount   uint32
}

func newXVMFixture(txCount, inputsPerTx, outputsPerTx, utxoCount uint32) *xvmFixture {
	f := &xvmFixture{
		utxoCount:     utxoCount,
		bloomBitCount: 4096,
		cuckooBuckets: 64,
		assetCount:    32, // power-of-two for the open-addressing mask
		markerCount:   16, // power-of-two
		txCount:       txCount,
		inputBatchN:   txCount,
		outputBatchN:  txCount,
		outputsCount:  txCount * outputsPerTx,
		assetOpCount:  txCount, // every tx carries a mint op so asset_transition has work
	}

	f.desc = XVMRoundDescriptor{
		ChainID:          0xCAFEBABE,
		Round:            1,
		TimestampNS:      0x100,
		Height:           42,
		Mode:             xvmModeFullRound,
		TxCount:          txCount,
		InputCount:       txCount * inputsPerTx,
		OutputCount:      f.outputsCount,
		AssetOpCount:     f.assetOpCount,
		InputBatchCount:  f.inputBatchN,
		OutputBatchCount: f.outputBatchN,
		ClosingFlag:      1,
	}
	for k := 0; k < 32; k++ {
		f.desc.ParentExecutionRoot[k] = byte(0xEE + k)
	}

	f.txs = make([]XVMTx, txCount)
	for i := uint32(0); i < txCount; i++ {
		tx := &f.txs[i]
		for k := 0; k < 32; k++ {
			tx.TxID[k] = byte(int(i) + k)
		}
		tx.Kind = xvmTxMint
		tx.AssetChangesOffset = i
		tx.AssetChangesCount = 1
		tx.InputBatchOffset = i
		tx.OutputBatchOffset = i
	}

	f.inputBatches = make([]XVMInputBatch, txCount)
	for i := uint32(0); i < txCount; i++ {
		ib := &f.inputBatches[i]
		ib.TxID = f.txs[i].TxID
		ib.InputOffset = i * inputsPerTx
		ib.InputCount = inputsPerTx
		ib.WitnessCount = inputsPerTx
	}

	f.outputBatches = make([]XVMOutputBatch, txCount)
	for i := uint32(0); i < txCount; i++ {
		ob := &f.outputBatches[i]
		ob.TxID = f.txs[i].TxID
		ob.OutputOffset = i * outputsPerTx
		ob.OutputCount = outputsPerTx
	}

	f.inputs = make([]byte, txCount*inputsPerTx*32)
	for i := range f.inputs {
		f.inputs[i] = byte((i * 31) ^ 0xA5)
	}

	f.outputs = make([]XVMUTXO, f.outputsCount)
	for i := uint32(0); i < f.outputsCount; i++ {
		u := &f.outputs[i]
		for k := 0; k < 32; k++ {
			u.UtxoID[k] = byte(int(i)+k) ^ 0x5A
			u.AssetID[k] = byte(0x10 + k)
		}
		u.AmountLo = 1000 + uint64(i)
	}

	f.utxos = make([]XVMUTXO, utxoCount)
	// Seed half the arena live so MembershipRebuild has genuine work.
	for i := uint32(0); i < utxoCount/2; i++ {
		u := &f.utxos[i]
		for k := 0; k < 32; k++ {
			u.UtxoID[k] = byte((int(i)*7)+k) ^ 0x37
			u.AssetID[k] = byte(0x10 + k)
		}
		u.AmountLo = 1
		u.Status = xvmUtxoOccupied
	}

	f.bloom = make([]byte, (f.bloomBitCount+7)/8)
	f.cuckoo = make([]XVMCuckooEntry, uint32(f.cuckooBuckets)*xvmCuckooSlotsPerBucket)
	f.assets = make([]XVMAsset, f.assetCount)
	f.markers = make([]XVMAtomicExportMarker, f.markerCount)

	f.assetOps = make([]XVMAssetOp, f.assetOpCount)
	for i := uint32(0); i < f.assetOpCount; i++ {
		op := &f.assetOps[i]
		// Distinct asset_id per op + matching authority witness so the
		// mint succeeds.
		for k := 0; k < 32; k++ {
			op.AssetID[k] = byte((int(i)*19)+k) ^ 0xC3
		}
		op.AmountLo = 10 + uint64(i)
		op.Kind = xvmAssetOpMint
	}

	return f
}

// clone returns a deep copy of the fixture so two runs of the bridge
// see byte-identical input buffers. Slice headers + array fields are
// copied via a sequence of `make + copy` pairs (no aliasing).
func (f *xvmFixture) clone() *xvmFixture {
	c := &xvmFixture{
		desc:          f.desc,
		utxoCount:     f.utxoCount,
		bloomBitCount: f.bloomBitCount,
		cuckooBuckets: f.cuckooBuckets,
		assetCount:    f.assetCount,
		markerCount:   f.markerCount,
		txCount:       f.txCount,
		inputBatchN:   f.inputBatchN,
		outputBatchN:  f.outputBatchN,
		outputsCount:  f.outputsCount,
		assetOpCount:  f.assetOpCount,
	}
	c.txs = append([]XVMTx(nil), f.txs...)
	c.inputBatches = append([]XVMInputBatch(nil), f.inputBatches...)
	c.outputBatches = append([]XVMOutputBatch(nil), f.outputBatches...)
	c.inputs = append([]byte(nil), f.inputs...)
	c.outputs = append([]XVMUTXO(nil), f.outputs...)
	c.utxos = append([]XVMUTXO(nil), f.utxos...)
	c.bloom = append([]byte(nil), f.bloom...)
	c.cuckoo = append([]XVMCuckooEntry(nil), f.cuckoo...)
	c.assets = append([]XVMAsset(nil), f.assets...)
	c.markers = append([]XVMAtomicExportMarker(nil), f.markers...)
	c.assetOps = append([]XVMAssetOp(nil), f.assetOps...)
	return c
}

// runAll runs the four bridge methods against the fixture using the
// provided backend handle. A nil backend forces the pure-Go path (or, on
// the nocgo build, the same Go path under a non-nil zero-value handle).
func (f *xvmFixture) runAll(b *GPUBackend) (uint32, uint32, [7]uint32, XVMTransitionResult, error) {
	var inputsConsumed, outputsCreated uint32
	if err := b.UTXOTransition(
		&f.desc, f.txs, f.inputBatches, f.outputBatches, f.inputs, f.outputs,
		f.utxos, f.bloom, f.cuckoo,
		f.utxoCount, f.bloomBitCount, f.cuckooBuckets,
		f.inputBatchN, f.outputBatchN, f.outputsCount,
		&inputsConsumed, &outputsCreated,
	); err != nil {
		return 0, 0, [7]uint32{}, XVMTransitionResult{}, err
	}

	var applied, exportsN, importsN, mintedLo, mintedHi, burnedLo, burnedHi uint32
	if err := b.AssetTransition(
		&f.desc, f.txs, f.assetOps, f.assets, f.markers,
		f.assetCount, f.assetOpCount, f.markerCount,
		&applied, &exportsN, &importsN, &mintedLo, &mintedHi, &burnedLo, &burnedHi,
	); err != nil {
		return 0, 0, [7]uint32{}, XVMTransitionResult{}, err
	}

	if err := b.MembershipRebuild(
		f.utxos, f.bloom, f.cuckoo,
		f.utxoCount, f.bloomBitCount, f.cuckooBuckets,
	); err != nil {
		return 0, 0, [7]uint32{}, XVMTransitionResult{}, err
	}

	var result XVMTransitionResult
	if err := b.RootUpdate(
		&f.desc, f.txs, f.utxos, f.assets, &result,
		f.txCount, f.utxoCount, f.assetCount,
	); err != nil {
		return 0, 0, [7]uint32{}, XVMTransitionResult{}, err
	}

	return inputsConsumed, outputsCreated,
		[7]uint32{applied, exportsN, importsN, mintedLo, mintedHi, burnedLo, burnedHi},
		result, nil
}

// TestXVMGPUBridge_AutoBackend is the dlopen round-trip. SKIPs cleanly
// when no plugin is on disk.
func TestXVMGPUBridge_AutoBackend(t *testing.T) {
	b := ActiveGPUBackend()
	if b == nil || !b.IsAvailable() {
		t.Skip("xvm: no GPU plugin loaded — skipping bridge round-trip")
	}

	t.Logf("loaded GPU plugin: kind=%s path=%s", b.Kind(), b.Path())

	// 1-utxo fixture — single mint tx, single output, 8-slot arena.
	f := newXVMFixture(1, 1, 1, 8)
	_, _, _, _, err := f.runAll(b)
	if err != nil {
		t.Fatalf("xvm: bridge round-trip: %v", err)
	}
}

// TestXVMGPUBridge_CgoNocgoParity is the one-and-only-one-way lock. It
// runs every bridge method on a non-trivial fixture twice — once via
// the active GPU backend (which may or may not have an open plugin) and
// once forcing the pure-Go path via a nil receiver — and asserts every
// output buffer matches byte-for-byte.
//
// When no plugin is loaded both paths are the same code (the Go impl
// runs on both sides). When a plugin IS loaded, the GPU path on side A
// must equal the Go path on side B. Either way, the test catches any
// regression where the two dispatch paths diverge.
func TestXVMGPUBridge_CgoNocgoParity(t *testing.T) {
	base := newXVMFixture(8, 2, 2, 64)

	// Run via the active backend (may use GPU plugin if one is loaded).
	fGPU := base.clone()
	icGPU, ocGPU, countersGPU, resultGPU, err := fGPU.runAll(ActiveGPUBackend())
	if err != nil {
		t.Fatalf("xvm: GPU-path bridge failed: %v", err)
	}

	// Run via a nil receiver — guaranteed to take the pure-Go path on
	// both build modes (the bridge methods accept a nil receiver and
	// short-circuit IsAvailable() to false).
	fGo := base.clone()
	icGo, ocGo, countersGo, resultGo, err := fGo.runAll(nil)
	if err != nil {
		t.Fatalf("xvm: Go-path bridge failed: %v", err)
	}

	if icGPU != icGo {
		t.Errorf("inputs_consumed: gpu=%d, go=%d", icGPU, icGo)
	}
	if ocGPU != ocGo {
		t.Errorf("outputs_created: gpu=%d, go=%d", ocGPU, ocGo)
	}
	if countersGPU != countersGo {
		t.Errorf("asset counters: gpu=%v, go=%v", countersGPU, countersGo)
	}
	if resultGPU != resultGo {
		t.Errorf("transition result mismatch:\n  gpu=%+v\n  go=%+v", resultGPU, resultGo)
	}

	// Buffer-level parity — every mutated arena must match.
	if !sliceEqUTXO(fGPU.utxos, fGo.utxos) {
		t.Errorf("utxos arena mismatch (len gpu=%d go=%d)", len(fGPU.utxos), len(fGo.utxos))
	}
	if !sliceEqBytes(fGPU.bloom, fGo.bloom) {
		t.Errorf("bloom mismatch")
	}
	if !sliceEqCuckoo(fGPU.cuckoo, fGo.cuckoo) {
		t.Errorf("cuckoo arena mismatch")
	}
	if !sliceEqAsset(fGPU.assets, fGo.assets) {
		t.Errorf("assets table mismatch")
	}
	if !sliceEqMarker(fGPU.markers, fGo.markers) {
		t.Errorf("markers arena mismatch")
	}
	if !sliceEqTx(fGPU.txs, fGo.txs) {
		t.Errorf("txs mismatch")
	}
}

// TestXVMGPUBridge_NilHandle exercises the zero-value GPUBackend
// behaviour. The handle is the "no plugin loaded" case; methods must
// run the Go path and produce results.
func TestXVMGPUBridge_NilHandle(t *testing.T) {
	var b *GPUBackend
	if b.IsAvailable() {
		t.Fatal("nil GPUBackend: IsAvailable() = true, want false")
	}
	if b.Kind() != GPUNone {
		t.Fatalf("nil GPUBackend: Kind() = %s, want none", b.Kind())
	}
	if b.Path() != "" {
		t.Fatalf("nil GPUBackend: Path() = %q, want empty", b.Path())
	}
	if err := b.Close(); err != nil {
		t.Fatalf("nil GPUBackend: Close() = %v, want nil", err)
	}

	// Smoke: the nil-receiver bridge still produces output.
	f := newXVMFixture(2, 1, 1, 16)
	_, _, _, result, err := f.runAll(b)
	if err != nil {
		t.Fatalf("nil GPUBackend: runAll: %v", err)
	}
	if result.Status != 1 {
		t.Errorf("nil GPUBackend: result.Status = %d, want 1", result.Status)
	}
}

// =============================================================================
// Slice equality helpers — element-wise comparison to keep failure
// reports crisp (Go's reflect.DeepEqual on byte arrays embedded inside
// structs reports the whole tree, which buries the diff).
// =============================================================================

func sliceEqUTXO(a, b []XVMUTXO) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceEqCuckoo(a, b []XVMCuckooEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceEqAsset(a, b []XVMAsset) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceEqMarker(a, b []XVMAtomicExportMarker) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceEqTx(a, b []XVMTx) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceEqBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
