// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// WarpMessage is an embedded cross-chain message payload. It carries the
// 32-byte source-network identifier plus the addressed-call payload bytes.
// The signature aggregation lives outside the unsigned-tx encoding (in the
// signed-tx wrapper); this primitive captures only the unsigned content
// that hashes into the messageID.
//
// Fixed-section layout (40 bytes; the Payload Bytes lives in the variable
// section after the fixed section, identical to BaseTx.Memo):
//
//	SourceNetwork  32B    @ 0    (ids.ID; the source network identifier)
//	Payload        bytes  @ 32   (offset+length pair, 8 bytes)
//
// Total fixed-section size: 40. Payload Bytes pointer is unsigned/forward
// per the F1 contract.
const (
	OffsetWarpMessage_SourceNetwork = 0
	OffsetWarpMessage_Payload       = 32 // bytes (relOffset + length, 8 bytes)
	SizeWarpMessage                 = 40
)

// WarpMessage is the zero-copy view over a WarpMessage object embedded in a
// parent tx.
type WarpMessage struct {
	obj zap.Object
}

// SourceNetwork returns the originating network identifier.
func (m WarpMessage) SourceNetwork() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = m.obj.Uint8(OffsetWarpMessage_SourceNetwork + i)
	}
	return out
}

// Payload returns the addressed-call payload bytes.
//
// READ-ONLY: the returned slice aliases the underlying ZAP buffer. Mutation
// corrupts the parsed tx and breaks TxID = hash(buffer). To own the bytes,
// copy first: append([]byte(nil), p...).
func (m WarpMessage) Payload() []byte {
	return m.obj.Bytes(OffsetWarpMessage_Payload)
}

// IsNull returns true if no message pointer was set.
func (m WarpMessage) IsNull() bool { return m.obj.IsNull() }

// WarpMessageView reads a WarpMessage from a parent object's field offset.
func WarpMessageView(parent zap.Object, fieldOffset int) WarpMessage {
	return WarpMessage{obj: parent.Object(fieldOffset)}
}

// WriteWarpMessage writes a WarpMessage object as a nested sub-object inside
// the builder's buffer and returns its offset. Callers pass the returned
// offset to ObjectBuilder.SetObject on the parent's WarpMessage field.
//
// Zero-alloc: the per-message scratch buffer is stack-sized; only the
// payload byte slice (caller-owned) is consulted by reference.
func WriteWarpMessage(b *zap.Builder, sourceNetwork ids.ID, payload []byte) int {
	ob := b.StartObject(SizeWarpMessage)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetWarpMessage_SourceNetwork+i, sourceNetwork[i])
	}
	ob.SetBytes(OffsetWarpMessage_Payload, payload)
	return ob.Finish()
}
