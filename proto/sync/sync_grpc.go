//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sync re-exports sync types from the appropriate wire format implementation.
// Without grpc tag: uses ZAP wire format (zero protobuf)
// With grpc tag: uses protobuf wire format
package sync

import pb "github.com/luxfi/node/proto/pb/sync"

// Re-export all types from protobuf implementation
type (
	Key         = pb.Key
	MaybeBytes  = pb.MaybeBytes
	ProofNode   = pb.ProofNode
	Proof       = pb.Proof
	KeyValue    = pb.KeyValue
	KeyChange   = pb.KeyChange
	RangeProof  = pb.RangeProof
	ChangeProof = pb.ChangeProof
)
