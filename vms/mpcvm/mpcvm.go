// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package mpcvm re-exports the canonical Threshold (FHE / MPC)
// VM from github.com/luxfi/chains/mpcvm so existing callers
// that imported github.com/luxfi/node/vms/mpcvm pre-extraction
// keep working without source changes.
//
// New code should import the canonical path:
//   "github.com/luxfi/chains/mpcvm"
//
// This file is a thin alias wrapper kept for backward compatibility.
package mpcvm

import "github.com/luxfi/chains/mpcvm"

type (
	Block       = mpcvm.Block
	BlockError  = mpcvm.BlockError
	Client      = mpcvm.Client
	Operation   = mpcvm.Operation
)

var (
	ErrInvalidOperation = mpcvm.ErrInvalidOperation
	NewClient           = mpcvm.NewClient
)
