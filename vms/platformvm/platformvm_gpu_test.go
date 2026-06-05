// Round-trip test for the platformvm GPU bridge.
//
// Validates the dlopen + dlsym path end-to-end:
//
//  1. AutoBackend() probes the canonical plugin order.
//  2. If a plugin is found, ValidatorSetApply is invoked with a 1-validator
//     fixture (kVOpAdd) and must return without error.
//  3. The result must reflect the apply: applied_out == 1 AND the
//     validator slot must be marked occupied + Active|PendingAdd.
//
// SKIP-on-miss is the canonical contract: if no plugin is on disk the
// test logs SKIP and exits 0. This is how the existing chains/aivm GPU
// tests behave — the bridge is optional and a missing plugin is not a
// failure mode of luxd itself.
//
// The launcher (lux_<backend>_platformvm_validator_set_apply) must NOT
// return any error code; any nonzero rc is a regression.

package platformvm

import (
	"testing"
)

func TestPlatformVMGPUBridge_AutoBackend(t *testing.T) {
	b := ActiveGPUBackend()
	if b == nil || !b.IsAvailable() {
		t.Skip("platformvm: no GPU plugin loaded — skipping bridge round-trip")
	}

	t.Logf("loaded GPU plugin: kind=%s path=%s", b.Kind(), b.Path())

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

// TestPlatformVMGPUBridge_NilHandle exercises the Zero-value GPUBackend
// behaviour. Every method must return ErrGPUNotAvailable when the handle
// is nil (the "no plugin loaded" case) without panicking. This is the
// fall-through contract the vm.go opt-in helpers rely on.
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

// TestPlatformVMGPU_EnvKnob verifies the false-by-default contract: even
// when a plugin successfully loaded, GPUEnabled() must remain false
// unless LUX_PLATFORMVM_GPU is set to a recognised true value.
func TestPlatformVMGPU_EnvKnob(t *testing.T) {
	t.Setenv("LUX_PLATFORMVM_GPU", "")
	if GPUEnabled() {
		t.Fatal("LUX_PLATFORMVM_GPU unset: GPUEnabled() = true, want false")
	}

	for _, v := range []string{"1", "true", "yes", "TRUE"} {
		t.Setenv("LUX_PLATFORMVM_GPU", v)
		if !GPUEnabled() {
			t.Errorf("LUX_PLATFORMVM_GPU=%q: GPUEnabled() = false, want true", v)
		}
	}

	for _, v := range []string{"0", "false", "no", ""} {
		t.Setenv("LUX_PLATFORMVM_GPU", v)
		if GPUEnabled() {
			t.Errorf("LUX_PLATFORMVM_GPU=%q: GPUEnabled() = true, want false", v)
		}
	}
}
