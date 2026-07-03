// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build cgo

// Package mpcvm re-exports the GPU bridge from
// github.com/luxfi/chains/mpcvm so callers that import the legacy
// node path keep working without source changes.
//
// One and only one way to dlopen: the canonical bridge lives in
// chains/mpcvm. This package is a typed alias layer — both the
// types and the GPUBackend singleton come straight from the upstream.
// There is exactly one dlopen handle, one symbol table, one init()
// probe across the entire luxd process. Consumers of either import path
// share the same plugin instance.

package mpcvm

import "github.com/luxfi/chains/mpcvm"

// =============================================================================
// Type re-exports — Go aliases preserve the public API so external callers
// (e.g. node/chains, node/main) can drop the import path without source
// changes. The GPU- prefix avoids collisions with the domain types
// already aliased above (Block, Client, Operation).
// =============================================================================

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

// Backend constants re-exported. Matches the chains/mpcvm dlopen
// probe order: cuda → hip → metal → vulkan → webgpu.
const (
	GPUBackendNone   = mpcvm.GPUBackendNone
	GPUBackendCUDA   = mpcvm.GPUBackendCUDA
	GPUBackendHIP    = mpcvm.GPUBackendHIP
	GPUBackendMetal  = mpcvm.GPUBackendMetal
	GPUBackendVulkan = mpcvm.GPUBackendVulkan
	GPUBackendWebGPU = mpcvm.GPUBackendWebGPU
)

// ErrGPUNotAvailable is the canonical "no plugin loaded" error. Callers
// `errors.Is(err, mpcvm.ErrGPUNotAvailable)` to distinguish a
// fallback-to-CPU condition from a hard launcher failure.
var ErrGPUNotAvailable = mpcvm.ErrGPUNotAvailable

// GPUBackendInstance returns the dlopen'd GPU plugin handle resolved at
// chains/mpcvm package init. nil means no plugin was loaded
// (CPU-only mode). The handle is shared across the whole process — both
// the node and chains paths see the same instance.
//
// The name is GPUBackendInstance (not GPUBackend / Backend) because Go
// disallows a function named the same as a type alias in the same
// package, and `Backend` is already a domain term in the threshold
// state machine. Callers write:
//
//   if g := mpcvm.GPUBackendInstance(); g != nil && g.IsAvailable() {
//       _, err := g.CeremonyApply(desc, ops, ceremonies)
//       ...
//   }
//
// This mirrors the cevm pattern `cevm.AvailableBackends()` /
// `cevm.LibraryABIVersion()` — discovery via package-scope function,
// not via a global variable.
func GPUBackendInstance() *GPUBackend {
	return mpcvm.Backend()
}
