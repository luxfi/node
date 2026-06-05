// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !cgo

// nocgo re-export of the chains/thresholdvm GPU bridge. Under !cgo the
// upstream GPUBackend methods all return ErrGPUNotAvailable and
// Backend() returns nil; this layer transparently passes that through
// so callers see identical behaviour regardless of build mode.
//
// One and only one nocgo stub for the entire process: it lives in
// chains/thresholdvm. This package just re-exports the types and the
// sentinel error.

package thresholdvm

import "github.com/luxfi/chains/thresholdvm"

type (
	GPUBackend     = thresholdvm.GPUBackend
	GPUBackendKind = thresholdvm.GPUBackendKind

	GPUCeremony              = thresholdvm.GPUCeremony
	GPUKeyShare              = thresholdvm.GPUKeyShare
	GPUContribution          = thresholdvm.GPUContribution
	GPUMPCVMState            = thresholdvm.GPUMPCVMState
	GPUMPCVMRoundDescriptor  = thresholdvm.GPUMPCVMRoundDescriptor
	GPUCeremonyOp            = thresholdvm.GPUCeremonyOp
	GPUContributionOp        = thresholdvm.GPUContributionOp
	GPUMPCVMTransitionResult = thresholdvm.GPUMPCVMTransitionResult
)

const (
	GPUBackendNone   = thresholdvm.GPUBackendNone
	GPUBackendCUDA   = thresholdvm.GPUBackendCUDA
	GPUBackendHIP    = thresholdvm.GPUBackendHIP
	GPUBackendMetal  = thresholdvm.GPUBackendMetal
	GPUBackendVulkan = thresholdvm.GPUBackendVulkan
	GPUBackendWebGPU = thresholdvm.GPUBackendWebGPU
)

var ErrGPUNotAvailable = thresholdvm.ErrGPUNotAvailable

// GPUBackendInstance returns nil under !cgo — the upstream Backend()
// returns nil because no dlopen ever happens. Callers branch on the
// IsAvailable() check (or `g == nil`) and route to the CPU reference.
func GPUBackendInstance() *GPUBackend {
	return thresholdvm.Backend()
}
