//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sync re-exports sync types from the appropriate wire format implementation.
// Without grpc tag: uses ZAP wire format (zero protobuf)
// With grpc tag: uses protobuf wire format
package sync

import "github.com/luxfi/node/proto/zap/sync"

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
