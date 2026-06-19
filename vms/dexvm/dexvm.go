// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package dexvm re-exports the canonical DEX VM from
// github.com/luxfi/chains/dexvm so existing callers that imported
// github.com/luxfi/node/vms/dexvm pre-extraction keep working
// without source-level changes.
//
// New code should import the canonical path:
//   "github.com/luxfi/chains/dexvm"
//
// This package is a thin, unconditional backward-compatibility alias. The
// underlying chains/dexvm is the pure-Go stateless atomic proxy (zero private
// deps), so — like every other genesis VM — it is linked into every build with
// no build tag.
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
