// Round-trip + parity tests for the platformvm bridge.
//
// The bridge has TWO execution paths:
//
//  1. GPU plugin (cgo build only) — dlopen + dlsym onto the lux-gpu-kernels
//     plugin DSOs; tried first when a backend is bound.
//  2. Canonical pure-Go (both builds) — cpuValidatorSetApply / Stake /
//     Slashing / Epoch in platformvm_gpu_cpu.go.
//
// On the cgo build the GPU path falls back to the Go impl on any launcher
// error; on the nocgo build only the Go path exists. Both paths produce
// byte-identical results — this is the consensus contract every backend
// must satisfy.
//
// TestPlatformVMGPUBridge_AutoBackend: smoke test for the ValidatorSetApply
// entry point on the currently-active backend (GPU plugin if bound, Go
// path otherwise). Always runs — never skips.
//
// TestPlatformVMGPUBridge_NilHandle: nil GPUBackend is still safe to query
// for Kind/Path/IsAvailable/Close. The transition methods are exercised
// against a nil receiver inside the parity test.
//
// TestPlatformVMGPUBridge_CgoNocgoParity: the BIG one. Runs every bridge
// method on a non-trivial validator-set + stake + slashing fixture and
// asserts the canonical pure-Go result matches the active backend's
// result byte-for-byte. The fixture is the same scenario used by the C++
// shader KAT (test_platformvm_kat.cpp) so the Go output is also a
// regression check against the GPU device code.

package platformvm

import (
	"bytes"
	"testing"
)

func TestPlatformVMGPUBridge_AutoBackend(t *testing.T) {
	b := ActiveGPUBackend()
	if b == nil {
		t.Logf("no GPU plugin loaded — exercising pure-Go path")
	} else {
		t.Logf("loaded GPU plugin: kind=%s path=%s available=%v",
			b.Kind(), b.Path(), b.IsAvailable())
	}

	// Single-validator fixture: ADD validator_id=42 with weight=1000.
	desc := PVMRoundDescriptor{
		ChainID:          0xCAFEBABE,
		Round:            1,
		Epoch:            10,
		Mode:             PVMModeValidator,
		ValidatorOpCount: 1,
	}
	op := PVMValidatorOp{
		ValidatorID: 42,
		Weight:      1000,
		Kind:        PVMVOpAdd,
		Epoch:       10,
	}
	// Power-of-two slot count — required by the open-addressing locator
	// in platformvm_kernels_common.cuh (mask = count - 1).
	validators := make([]PVMValidatorSlot, 8)
	var applied uint32

	err := b.ValidatorSetApply(&desc, []PVMValidatorOp{op}, validators, &applied)
	if err != nil {
		t.Fatalf("ValidatorSetApply: %v", err)
	}
	if applied != 1 {
		t.Fatalf("ValidatorSetApply: applied=%d, want=1", applied)
	}

	// Locate the slot we wrote — open-addressing hash on validator_id.
	found := false
	for i := range validators {
		if validators[i].Occupied != 0 && validators[i].ValidatorID == 42 {
			found = true
			if validators[i].Weight != 1000 {
				t.Errorf("validator slot: Weight=%d, want=1000", validators[i].Weight)
			}
			wantStatus := PVMStatusActive | PVMStatusPendingAdd
			if validators[i].Status != wantStatus {
				t.Errorf("validator slot: Status=0x%x, want=0x%x",
					validators[i].Status, wantStatus)
			}
			break
		}
	}
	if !found {
		t.Fatal("ValidatorSetApply: no occupied slot with validator_id=42")
	}
}

// TestPlatformVMGPUBridge_NilHandle exercises the zero-value GPUBackend
// query surface. Kind / Path / IsAvailable / Close MUST never panic on a
// nil receiver — both build modes promise this.
func TestPlatformVMGPUBridge_NilHandle(t *testing.T) {
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
}

// =============================================================================
// parityFixture — the canonical KAT scenario embedded in
// lux-private/gpu-kernels/backends/vulkan/tests/test_platformvm_kat.cpp.
// Eight validator slots, eight stake slots, four slashing slots; three
// validator ops, three stake ops, one slashing op; one closing-flag
// epoch transition.
// =============================================================================

const (
	parityValidatorCount = 8 // power of two
	parityStakeCount     = 8 // power of two
	paritySlashingCount  = 4
)

func parityFixture() (PVMRoundDescriptor, []PVMValidatorOp, []PVMStakeOp, []PVMSlashEvidence) {
	var desc PVMRoundDescriptor
	desc.ChainID = 0xCAFEBABEDEADBEEF
	desc.Round = 42
	desc.TimestampNS = 1700000000 * 1000000000
	desc.Epoch = 10
	desc.Mode = PVMModeFullRound
	desc.ClosingFlag = 1
	for k := 0; k < 32; k++ {
		desc.ParentEpochRoot[k] = 0xBB
	}

	vOps := make([]PVMValidatorOp, 0, 3)
	{
		var v PVMValidatorOp
		v.ValidatorID = 1
		v.Weight = 1000000
		for k := 0; k < 48; k++ {
			v.BLSPubkey[k] = byte(0xA0 + (k & 0xF))
		}
		for k := 0; k < 32; k++ {
			v.CoronaPubkey[k] = byte(0xB0 + (k & 0xF))
			v.MLDSAPubkey[k] = byte(0xC0 + (k & 0xF))
			v.MLDSAGroth16Root[k] = byte(0xD0 + (k & 0xF))
		}
		v.Kind = PVMVOpAdd
		vOps = append(vOps, v)
	}
	{
		var v PVMValidatorOp
		v.ValidatorID = 2
		v.Weight = 2000000
		for k := 0; k < 48; k++ {
			v.BLSPubkey[k] = byte(0xE0 + (k & 0xF))
		}
		for k := 0; k < 32; k++ {
			v.CoronaPubkey[k] = byte(0xF0 + (k & 0xF))
			v.MLDSAPubkey[k] = byte(0x10 + (k & 0xF))
			v.MLDSAGroth16Root[k] = byte(0x20 + (k & 0xF))
		}
		v.Kind = PVMVOpAdd
		vOps = append(vOps, v)
	}
	{
		var v PVMValidatorOp
		v.ValidatorID = 1
		v.Weight = 900000
		v.Kind = PVMVOpUpdateWeight
		vOps = append(vOps, v)
	}
	desc.ValidatorOpCount = uint32(len(vOps))

	sOps := make([]PVMStakeOp, 0, 3)
	sOps = append(sOps, PVMStakeOp{
		DelegatorID: 11, ValidatorID: 1, Amount: 500000,
		Kind: PVMSOpBond, Epoch: 10,
	})
	sOps = append(sOps, PVMStakeOp{
		DelegatorID: 12, ValidatorID: 1, Amount: 200000,
		Kind: PVMSOpBond, Epoch: 10,
	})
	sOps = append(sOps, PVMStakeOp{
		DelegatorID: 13, ValidatorID: 2, Amount: 750000,
		Kind: PVMSOpDelegate, Epoch: 10,
	})
	desc.StakeOpCount = uint32(len(sOps))

	evidence := make([]PVMSlashEvidence, 0, 1)
	{
		var ev PVMSlashEvidence
		ev.ValidatorID = 1
		ev.Height = 12345
		ev.SlashAmount = 0 // 1% default for downtime
		ev.Kind = PVMEvDowntime
		ev.Epoch = 10
		ev.JailForEpochs = 50
		for k := 0; k < 32; k++ {
			ev.EvidenceDigest[k] = byte(0xE0 + (k & 0xF))
		}
		evidence = append(evidence, ev)
	}
	desc.SlashEvidenceCount = uint32(len(evidence))

	return desc, vOps, sOps, evidence
}

// runFixtureWith runs every bridge method on `b` against a fresh copy of
// the parity fixture and returns the final transition result + post-state
// arrays for comparison.
func runFixtureWith(t *testing.T, b *GPUBackend) (
	PVMTransitionResult,
	PVMEpochState,
	[]PVMValidatorSlot,
	[]PVMStakeRecord,
	[]PVMSlashEvidence,
	uint32, // validator-set apply count
	uint32, // stake apply count
	uint32, // slashing apply count
	uint32, // total slashed (lo)
	uint32, // total slashed (hi)
) {
	t.Helper()
	desc, vOps, sOps, evidence := parityFixture()

	validators := make([]PVMValidatorSlot, parityValidatorCount)
	stake := make([]PVMStakeRecord, parityStakeCount)
	slashing := make([]PVMSlashEvidence, paritySlashingCount)

	var vApplied, sApplied, slashApplied, totLo, totHi uint32
	if err := b.ValidatorSetApply(&desc, vOps, validators, &vApplied); err != nil {
		t.Fatalf("ValidatorSetApply: %v", err)
	}
	if err := b.StakeTransition(&desc, sOps, validators, stake, &sApplied); err != nil {
		t.Fatalf("StakeTransition: %v", err)
	}
	if err := b.SlashingTransition(&desc, evidence, validators, slashing, &slashApplied, &totLo, &totHi); err != nil {
		t.Fatalf("SlashingTransition: %v", err)
	}

	var epoch PVMEpochState
	var result PVMTransitionResult
	if err := b.EpochTransition(&desc, validators, stake, slashing, &epoch, &result, nil); err != nil {
		t.Fatalf("EpochTransition: %v", err)
	}

	return result, epoch, validators, stake, slashing,
		vApplied, sApplied, slashApplied, totLo, totHi
}

// TestPlatformVMGPUBridge_CgoNocgoParity drives the four bridge methods
// twice — once on the currently-active backend (GPU plugin if bound,
// otherwise the same Go path), and once on a forcibly-nil GPUBackend
// (always the Go path) — and asserts byte-equality of every observable.
//
// In the nocgo build both runs go through the Go path, so the test is
// essentially a self-consistency check that the bridge dispatches
// deterministically. In the cgo build with a plugin loaded the first
// run takes the GPU path and the second takes the Go fallback; the
// assertion guarantees the device kernel and Go impl agree.
//
// Either way: the same fixture in MUST produce the same fixture out.
func TestPlatformVMGPUBridge_CgoNocgoParity(t *testing.T) {
	active := ActiveGPUBackend()
	if active != nil {
		t.Logf("active backend: kind=%s path=%s available=%v",
			active.Kind(), active.Path(), active.IsAvailable())
	} else {
		t.Logf("no GPU plugin loaded — both runs take the Go path")
	}

	r1, ep1, val1, stk1, sl1, va1, sa1, slaa1, lo1, hi1 := runFixtureWith(t, active)
	t.Logf("active validator_set_root: %x", r1.ValidatorSetRoot)
	t.Logf("active stake_root:         %x", r1.StakeRoot)
	t.Logf("active slashing_root:      %x", r1.SlashingRoot)
	t.Logf("active epoch_root:         %x", r1.EpochRoot)

	// Force the Go path: nil receiver IsAvailable() is false, so the
	// cgo bridge skips the plugin branch and the nocgo bridge always
	// runs the Go path.
	r2, ep2, val2, stk2, sl2, va2, sa2, slaa2, lo2, hi2 := runFixtureWith(t, nil)
	t.Logf("nil-rcv validator_set_root: %x", r2.ValidatorSetRoot)
	t.Logf("nil-rcv stake_root:         %x", r2.StakeRoot)
	t.Logf("nil-rcv slashing_root:      %x", r2.SlashingRoot)
	t.Logf("nil-rcv epoch_root:         %x", r2.EpochRoot)

	// Apply counts.
	if va1 != va2 {
		t.Errorf("validator-set apply count: active=%d gopath=%d", va1, va2)
	}
	if sa1 != sa2 {
		t.Errorf("stake apply count: active=%d gopath=%d", sa1, sa2)
	}
	if slaa1 != slaa2 {
		t.Errorf("slashing apply count: active=%d gopath=%d", slaa1, slaa2)
	}
	if lo1 != lo2 || hi1 != hi2 {
		t.Errorf("total slashed: active=(%d,%d) gopath=(%d,%d)", lo1, hi1, lo2, hi2)
	}

	// Result roots.
	if r1.ValidatorSetRoot != r2.ValidatorSetRoot {
		t.Errorf("validator_set_root: active=%x gopath=%x",
			r1.ValidatorSetRoot, r2.ValidatorSetRoot)
	}
	if r1.StakeRoot != r2.StakeRoot {
		t.Errorf("stake_root: active=%x gopath=%x", r1.StakeRoot, r2.StakeRoot)
	}
	if r1.SlashingRoot != r2.SlashingRoot {
		t.Errorf("slashing_root: active=%x gopath=%x", r1.SlashingRoot, r2.SlashingRoot)
	}
	if r1.EpochRoot != r2.EpochRoot {
		t.Errorf("epoch_root: active=%x gopath=%x", r1.EpochRoot, r2.EpochRoot)
	}

	// Result counters.
	if r1.ActiveValidatorCount != r2.ActiveValidatorCount {
		t.Errorf("active_validator_count: active=%d gopath=%d",
			r1.ActiveValidatorCount, r2.ActiveValidatorCount)
	}
	if r1.JailedCount != r2.JailedCount {
		t.Errorf("jailed_count: active=%d gopath=%d", r1.JailedCount, r2.JailedCount)
	}
	if r1.TombstonedCount != r2.TombstonedCount {
		t.Errorf("tombstoned_count: active=%d gopath=%d",
			r1.TombstonedCount, r2.TombstonedCount)
	}
	if r1.PendingDropCount != r2.PendingDropCount {
		t.Errorf("pending_drop_count: active=%d gopath=%d",
			r1.PendingDropCount, r2.PendingDropCount)
	}
	if r1.TotalActiveStake != r2.TotalActiveStake {
		t.Errorf("total_active_stake: active=%d gopath=%d",
			r1.TotalActiveStake, r2.TotalActiveStake)
	}
	if r1.TotalRewards != r2.TotalRewards {
		t.Errorf("total_rewards: active=%d gopath=%d",
			r1.TotalRewards, r2.TotalRewards)
	}
	if r1.TotalSlashed != r2.TotalSlashed {
		t.Errorf("total_slashed: active=%d gopath=%d", r1.TotalSlashed, r2.TotalSlashed)
	}
	if r1.Epoch != r2.Epoch {
		t.Errorf("epoch: active=%d gopath=%d", r1.Epoch, r2.Epoch)
	}
	if r1.Status != r2.Status {
		t.Errorf("status: active=%d gopath=%d", r1.Status, r2.Status)
	}

	// Epoch-state.
	if ep1.CurrentEpoch != ep2.CurrentEpoch {
		t.Errorf("epoch.CurrentEpoch: active=%d gopath=%d",
			ep1.CurrentEpoch, ep2.CurrentEpoch)
	}
	if ep1.TotalActiveStake != ep2.TotalActiveStake {
		t.Errorf("epoch.TotalActiveStake: active=%d gopath=%d",
			ep1.TotalActiveStake, ep2.TotalActiveStake)
	}
	if ep1.ActiveValidatorCount != ep2.ActiveValidatorCount {
		t.Errorf("epoch.ActiveValidatorCount: active=%d gopath=%d",
			ep1.ActiveValidatorCount, ep2.ActiveValidatorCount)
	}
	if ep1.PendingDropCount != ep2.PendingDropCount {
		t.Errorf("epoch.PendingDropCount: active=%d gopath=%d",
			ep1.PendingDropCount, ep2.PendingDropCount)
	}
	if ep1.ValidatorSetRoot != ep2.ValidatorSetRoot {
		t.Errorf("epoch.ValidatorSetRoot")
	}
	if ep1.StakeRoot != ep2.StakeRoot {
		t.Errorf("epoch.StakeRoot")
	}
	if ep1.SlashingRoot != ep2.SlashingRoot {
		t.Errorf("epoch.SlashingRoot")
	}
	if ep1.EpochRoot != ep2.EpochRoot {
		t.Errorf("epoch.EpochRoot")
	}

	// Validator slots — every byte.
	for i := range val1 {
		if val1[i] != val2[i] {
			t.Errorf("validator[%d]: active=%+v gopath=%+v", i, val1[i], val2[i])
		}
	}
	// Stake records.
	for i := range stk1 {
		if stk1[i] != stk2[i] {
			t.Errorf("stake[%d]: active=%+v gopath=%+v", i, stk1[i], stk2[i])
		}
	}
	// Slashing records.
	for i := range sl1 {
		if sl1[i] != sl2[i] {
			t.Errorf("slashing[%d]: active=%+v gopath=%+v", i, sl1[i], sl2[i])
		}
	}

	// Sanity: the fixture must actually exercise the state machine.
	// active=2 (both validators kept Active after ValidatorSetApply +
	// minus 1 jailed by slashing == 1 active, with v=2 still active)
	if r1.ActiveValidatorCount == 0 {
		t.Errorf("fixture sanity: active validator count is 0; check fixture")
	}
	// Validator 1 (which we Bond+Bond+Slash on) must end up jailed but
	// not tombstoned, since the slash kind is Downtime not Equivocation.
	if r1.JailedCount == 0 {
		t.Errorf("fixture sanity: no jailed validators; expected v=1 jailed by downtime slash")
	}
	if r1.TombstonedCount != 0 {
		t.Errorf("fixture sanity: tombstoned=%d; downtime should jail not tombstone",
			r1.TombstonedCount)
	}

	// epoch_root MUST NOT be all-zero — the composed-root step always
	// runs, and parent_epoch_root is non-zero.
	var zero32 [32]byte
	if bytes.Equal(r1.EpochRoot[:], zero32[:]) {
		t.Errorf("epoch_root is all zero — composed-root step did not run")
	}
}
