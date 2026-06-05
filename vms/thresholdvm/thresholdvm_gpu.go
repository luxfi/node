// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build cgo

// Package thresholdvm re-exports the GPU bridge from
// github.com/luxfi/chains/thresholdvm so callers that import the legacy
// node path keep working without source changes.
//
// One and only one way to dlopen: the canonical bridge lives in
// chains/thresholdvm. This package is a typed alias layer — both the
// types and the GPUBackend singleton come straight from the upstream.
// There is exactly one dlopen handle, one symbol table, one init()
// probe across the entire luxd process. Consumers of either import path
// share the same plugin instance.

package thresholdvm

import "github.com/luxfi/chains/thresholdvm"

// =============================================================================
// Type re-exports — Go aliases preserve the public API so external callers
// (e.g. node/chains, node/main) can drop the import path without source
// changes. The GPU- prefix avoids collisions with the domain types
// already aliased above (Block, Client, Operation).
// =============================================================================

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

// Backend constants re-exported. Matches the chains/thresholdvm dlopen
// probe order: cuda → hip → metal → vulkan → webgpu.
const (
	GPUBackendNone   = thresholdvm.GPUBackendNone
	GPUBackendCUDA   = thresholdvm.GPUBackendCUDA
	GPUBackendHIP    = thresholdvm.GPUBackendHIP
	GPUBackendMetal  = thresholdvm.GPUBackendMetal
	GPUBackendVulkan = thresholdvm.GPUBackendVulkan
	GPUBackendWebGPU = thresholdvm.GPUBackendWebGPU
)

// ErrGPUNotAvailable is the canonical "no plugin loaded" error. Callers
// `errors.Is(err, thresholdvm.ErrGPUNotAvailable)` to distinguish a
// fallback-to-CPU condition from a hard launcher failure.
var ErrGPUNotAvailable = thresholdvm.ErrGPUNotAvailable

// GPUBackendInstance returns the dlopen'd GPU plugin handle resolved at
// chains/thresholdvm package init. nil means no plugin was loaded
// (CPU-only mode). The handle is shared across the whole process — both
// the node and chains paths see the same instance.
//
// The name is GPUBackendInstance (not GPUBackend / Backend) because Go
// disallows a function named the same as a type alias in the same
// package, and `Backend` is already a domain term in the threshold
// state machine. Callers write:
//
//   if g := thresholdvm.GPUBackendInstance(); g != nil && g.IsAvailable() {
//       _, err := g.CeremonyApply(desc, ops, ceremonies)
//       ...
//   }
//
// This mirrors the cevm pattern `cevm.AvailableBackends()` /
// `cevm.LibraryABIVersion()` — discovery via package-scope function,
// not via a global variable.
func GPUBackendInstance() *GPUBackend {
	return thresholdvm.Backend()
}
