// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package thresholdvm re-exports the canonical Threshold (FHE / MPC)
// VM from github.com/luxfi/chains/thresholdvm so existing callers
// that imported github.com/luxfi/node/vms/thresholdvm pre-extraction
// keep working without source changes.
//
// New code should import the canonical path:
//   "github.com/luxfi/chains/thresholdvm"
//
// This file is a thin alias wrapper kept for backward compatibility.
package thresholdvm

import "github.com/luxfi/chains/thresholdvm"

type (
	Block       = thresholdvm.Block
	BlockError  = thresholdvm.BlockError
	Client      = thresholdvm.Client
	Operation   = thresholdvm.Operation
)

var (
	ErrInvalidOperation = thresholdvm.ErrInvalidOperation
	NewClient           = thresholdvm.NewClient
)
