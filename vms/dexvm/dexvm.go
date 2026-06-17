// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build dchain

// Package dexvm re-exports the canonical DEX VM from
// github.com/luxfi/chains/dexvm so existing callers that imported
// github.com/luxfi/node/vms/dexvm pre-extraction keep working
// without source-level changes.
//
// New code should import the canonical path:
//   "github.com/luxfi/chains/dexvm"
//
// This package is a thin alias wrapper kept for backward compatibility. It is
// gated behind the `dchain` build tag: it imports github.com/luxfi/chains/dexvm
// (the D-Chain proxy tree), which MUST be absent from the public OSS luxd build
// (public-build purity). Nothing in-tree imports this alias; it exists only for
// out-of-tree backward compatibility, and only a venue (dchain) build links it.
package dexvm

import (
	"github.com/luxfi/chains/dexvm"
)

// Re-export the public surface.
type (
	Block       = dexvm.Block
	ChainVM     = dexvm.ChainVM
	Factory     = dexvm.Factory
	OrderKey    = dexvm.OrderKey
	DexVertex   = dexvm.DexVertex
	Status      = dexvm.Status
)

var (
	// VMID identifies the canonical primary-network D-Chain VM.
	VMID = dexvm.VMID

	// NewChainVM constructs a fresh DEX chain VM.
	NewChainVM = dexvm.NewChainVM
)
