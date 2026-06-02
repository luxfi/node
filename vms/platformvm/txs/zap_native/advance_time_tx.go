// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package zap_native implements native ZAP encoding for platformvm txs.
//
// Architectural anchor: there is NO marshal step. The Go struct wraps the
// ZAP buffer directly. Constructors write to fixed offsets. Accessors read
// via offset arithmetic on the buffer with zero copy and zero allocation.
//
// The buffer IS the wire format. tx.Bytes() returns it literally.
// Parse(b) wraps b in a typed accessor — no decode step.
// TxID is hash of buffer — no re-encoding.
//
// This file implements the simplest platformvm tx (AdvanceTimeTx, one uint64
// field) as the canary for the pattern. The other ~32 tx types follow the
// same shape: schema offsets + Wrap function + Builder + Bytes/ID accessors.
package zap_native

import (
	"github.com/luxfi/zap"
)

// AdvanceTimeTx ZAP schema v3: TxKind discriminator + uint64 timestamp.
//
// Wire = ZAP_HEADER (16 bytes) + Object{ TxKind: u8@0, Time: u64@1 } (9 bytes).
const (
	SchemaVersionAdvanceTimeTx uint16 = 3

	OffsetAdvanceTimeTx_Time = 1 // uint64 (after TxKind@0)
	SizeAdvanceTimeTx        = 9
)

// AdvanceTimeTx is a zero-copy typed accessor over a ZAP buffer.
type AdvanceTimeTx struct {
	msg *zap.Message
	obj zap.Object
}

// Time returns the timestamp the proposer is suggesting the network advance to.
// Zero copy, zero allocation.
func (t AdvanceTimeTx) Time() uint64 {
	return t.obj.Uint64(OffsetAdvanceTimeTx_Time)
}

// Bytes returns the underlying ZAP buffer literally. NO encoding step.
func (t AdvanceTimeTx) Bytes() []byte {
	return t.msg.Bytes()
}

// IsZero reports whether the accessor is uninitialized.
func (t AdvanceTimeTx) IsZero() bool {
	return t.msg == nil
}

// WrapAdvanceTimeTx parses a ZAP buffer into a typed accessor.
//
// Zero copy: the returned accessor references the input buffer directly.
// The caller MUST keep the buffer alive while the accessor is in use.
// Returns ErrWrongTxKind if the discriminator byte does not match
// TxKindAdvanceTime; returns ErrInvalidMagic/ErrInvalidVersion from the
// ZAP layer on a malformed buffer.
func WrapAdvanceTimeTx(b []byte) (AdvanceTimeTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindAdvanceTime)
	if err != nil {
		return AdvanceTimeTx{}, err
	}
	return AdvanceTimeTx{msg: msg, obj: obj}, nil
}

// NewAdvanceTimeTx builds an AdvanceTimeTx into a fresh ZAP buffer.
//
// No reflection. No serialize tag walk. The builder writes TxKind at offset 0
// and Time at offset 1 in the object payload, finalizes the ZAP message, and
// returns a typed accessor over the resulting buffer.
func NewAdvanceTimeTx(time uint64) AdvanceTimeTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeAdvanceTimeTx)
	ob := b.StartObject(SizeAdvanceTimeTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindAdvanceTime))
	ob.SetUint64(OffsetAdvanceTimeTx_Time, time)
	ob.FinishAsRoot()
	buf := b.Finish()
	// Re-parse the buffer to materialize the typed accessor without an
	// allocation in the hot path on subsequent reads. The Parse is itself
	// zero-copy.
	msg, _ := zap.Parse(buf)
	return AdvanceTimeTx{msg: msg, obj: msg.Root()}
}
