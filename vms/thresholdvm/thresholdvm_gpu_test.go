// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package thresholdvm

import (
	"errors"
	"testing"
	"unsafe"
)

// TestNodeGPULayoutSizes pins the re-exported wire structs to the same
// device-side __align__(16) values declared in
// ~/work/lux-private/gpu-kernels/ops/mpcvm/cuda/mpcvm_kernels_common.cuh.
//
// Even though the structs are type aliases to chains/thresholdvm, this
// test sits at the node import boundary — if upstream sizes drift the
// node-side surface MUST also fail loud rather than miscompile.
func TestNodeGPULayoutSizes(t *testing.T) {
	cases := []struct {
		name string
		want uintptr
		got  uintptr
	}{
		{"GPUCeremony", 128, unsafe.Sizeof(GPUCeremony{})},
		{"GPUKeyShare", 368, unsafe.Sizeof(GPUKeyShare{})},
		{"GPUContribution", 432, unsafe.Sizeof(GPUContribution{})},
		{"GPUMPCVMState", 160, unsafe.Sizeof(GPUMPCVMState{})},
		{"GPUMPCVMRoundDescriptor", 96, unsafe.Sizeof(GPUMPCVMRoundDescriptor{})},
		{"GPUCeremonyOp", 96, unsafe.Sizeof(GPUCeremonyOp{})},
		{"GPUContributionOp", 416, unsafe.Sizeof(GPUContributionOp{})},
		{"GPUMPCVMTransitionResult", 176, unsafe.Sizeof(GPUMPCVMTransitionResult{})},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: sizeof=%d want=%d", c.name, c.got, c.want)
		}
	}
}

// TestNodeGPUBackendRoundTrip is the dlopen round-trip required by the
// task spec. Two acceptable outcomes:
//
//  1. Plugin resolved: GPUBackendInstance() != nil && IsAvailable(); a
//     zero-fixture CeremonyApply against the best backend returns rc=0
//     (no ops processed) with no error.
//
//  2. Plugin absent: GPUBackendInstance() == nil; nil-receiver methods
//     return ErrGPUNotAvailable. SelectGPUBackend reports the CPU-only
//     diagnostic string.
//
// Both outcomes count as passing — the bridge correctly surfaces GPU
// state through the public node API.
func TestNodeGPUBackendRoundTrip(t *testing.T) {
	b, diag := SelectGPUBackend()
	t.Logf("SelectGPUBackend: %s", diag)
	if b == nil {
		// Plugin-absent path. The nil receiver contract must hold —
		// every method returns ErrGPUNotAvailable, no panic.
		_, err := (*GPUBackend)(nil).CeremonyApply(nil, nil, nil)
		if !errors.Is(err, ErrGPUNotAvailable) {
			t.Fatalf("nil receiver CeremonyApply: want ErrGPUNotAvailable, got %v", err)
		}
		return
	}

	// Plugin-resolved path. Zero fixture: 1-slot ceremony arena, no ops.
	// The kernel walks zero ops and returns applied=0 with rc=0. Any
	// non-zero rc means a real launcher failure.
	desc := &GPUMPCVMRoundDescriptor{
		ChainID:     0xCAFEBABE,
		Round:       1,
		TimestampNs: 1700000000_000000000,
		Epoch:       1,
	}
	ceremonies := make([]GPUCeremony, 1)
	applied, err := b.CeremonyApply(desc, nil, ceremonies)
	if err != nil {
		t.Fatalf("CeremonyApply(zero): %v", err)
	}
	if applied != 0 {
		t.Errorf("CeremonyApply(zero): applied=%d want=0", applied)
	}
}

// TestNodeGPUStubContract verifies the nocgo-equivalent contract on a
// zero-value GPUBackend: IsAvailable()==false and every state-machine
// method returns ErrGPUNotAvailable. The same contract holds under cgo
// when no plugin is dlopened — making this test build-flavor-independent.
func TestNodeGPUStubContract(t *testing.T) {
	var b GPUBackend
	if b.IsAvailable() {
		t.Fatal("zero GPUBackend.IsAvailable() must be false")
	}
	if _, err := b.CeremonyApply(nil, nil, nil); !errors.Is(err, ErrGPUNotAvailable) {
		t.Errorf("CeremonyApply: want ErrGPUNotAvailable, got %v", err)
	}
	if _, _, _, err := b.KeyShareApply(nil, nil, nil, nil, 0); !errors.Is(err, ErrGPUNotAvailable) {
		t.Errorf("KeyShareApply: want ErrGPUNotAvailable, got %v", err)
	}
	if _, err := b.ContributionApply(nil, nil, nil, nil, 0); !errors.Is(err, ErrGPUNotAvailable) {
		t.Errorf("ContributionApply: want ErrGPUNotAvailable, got %v", err)
	}
	if _, err := b.MPCTransition(nil, nil, nil, nil, nil); !errors.Is(err, ErrGPUNotAvailable) {
		t.Errorf("MPCTransition: want ErrGPUNotAvailable, got %v", err)
	}
}
