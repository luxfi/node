// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package thresholdvm — backend selection re-export.
//
// The canonical dlopen probe lives in github.com/luxfi/chains/thresholdvm.
// Its init() runs once at package load time and pins a process-wide
// GPUBackend handle (cuda → hip → metal → vulkan → webgpu probe order).
// This file is the node-side entry point for diagnostics and ops tooling.
//
// Note: the luxd VM manager registration for ThresholdVM (M-Chain) is
// NOT flipped here per the project memory note ("M-Chain code exists but
// NOT yet registered in running luxd"). This file provides the bridge
// surface only — flipping the manager hook-up is a separate ops PR.

package thresholdvm

// SelectGPUBackend returns the resolved GPU plugin (or nil) and a single
// human-readable diagnostic string. luxd startup logs use this to surface
// "thresholdvm-gpu backend=<name>" lines alongside the cevm backend
// selection, matching the cevm.go pattern at
// ~/work/lux/chains/evm/backend_cgo.go.
//
// Calling this multiple times is cheap — the underlying probe runs once
// at package init via sync.Once in chains/thresholdvm/backend.go.
func SelectGPUBackend() (*GPUBackend, string) {
	g := GPUBackendInstance()
	if g == nil || !g.IsAvailable() {
		return nil, "thresholdvm-gpu: no plugin resolved (CPU-only)"
	}
	return g, "thresholdvm-gpu: backend=" + g.Kind.String() + " path=" + g.Path
}
