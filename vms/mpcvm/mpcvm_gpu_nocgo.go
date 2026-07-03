// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !cgo

// nocgo re-export of the chains/mpcvm GPU bridge. Under !cgo the
// upstream GPUBackend methods all return ErrGPUNotAvailable and
// Backend() returns nil; this layer transparently passes that through
// so callers see identical behaviour regardless of build mode.
//
// One and only one nocgo stub for the entire process: it lives in
// chains/mpcvm. This package just re-exports the types and the
// sentinel error.

package mpcvm

import "github.com/luxfi/chains/mpcvm"

type (
	GPUBackend     = mpcvm.GPUBackend
	GPUBackendKind = mpcvm.GPUBackendKind

	GPUCeremony              = mpcvm.GPUCeremony
	GPUKeyShare              = mpcvm.GPUKeyShare
	GPUContribution          = mpcvm.GPUContribution
	GPUMPCVMState            = mpcvm.GPUMPCVMState
	GPUMPCVMRoundDescriptor  = mpcvm.GPUMPCVMRoundDescriptor
	GPUCeremonyOp            = mpcvm.GPUCeremonyOp
	GPUContributionOp        = mpcvm.GPUContributionOp
	GPUMPCVMTransitionResult = mpcvm.GPUMPCVMTransitionResult
)

const (
	GPUBackendNone   = mpcvm.GPUBackendNone
	GPUBackendCUDA   = mpcvm.GPUBackendCUDA
	GPUBackendHIP    = mpcvm.GPUBackendHIP
	GPUBackendMetal  = mpcvm.GPUBackendMetal
	GPUBackendVulkan = mpcvm.GPUBackendVulkan
	GPUBackendWebGPU = mpcvm.GPUBackendWebGPU
)

var ErrGPUNotAvailable = mpcvm.ErrGPUNotAvailable

// GPUBackendInstance returns nil under !cgo — the upstream Backend()
// returns nil because no dlopen ever happens. Callers branch on the
// IsAvailable() check (or `g == nil`) and route to the CPU reference.
func GPUBackendInstance() *GPUBackend {
	return mpcvm.Backend()
}
