// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sync re-exports sync types from the ZAP wire format
// implementation. ZAP is the only supported transport.
package sync

import "github.com/luxfi/proto/node/zap/sync"

// Re-export all types from ZAP implementation
type (
	Key         = sync.Key
	MaybeBytes  = sync.MaybeBytes
	ProofNode   = sync.ProofNode
	Proof       = sync.Proof
	KeyValue    = sync.KeyValue
	KeyChange   = sync.KeyChange
	RangeProof  = sync.RangeProof
	ChangeProof = sync.ChangeProof
)
