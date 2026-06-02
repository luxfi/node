// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// BaseTx v3 schema — TxKind + NetworkID + BlockchainID + Memo. The Outs/Ins
// fields of the legacy lux.BaseTx are variable-length nested-object lists
// and need a proper ZAP list schema; they ship in batch 3 as BaseTxFull.
// This minimal form is the metadata envelope every higher-level platformvm
// tx embeds.
//
// Fixed-section layout (size 45 bytes):
//
//	TxKind       uint8  @ 0
//	NetworkID    uint32 @ 1
//	BlockchainID 32B    @ 5
//	Memo         bytes  @ 37  (offset+length pair, 8 bytes)
const (
	SchemaVersionBaseTx uint16 = 3

	OffsetBaseTx_NetworkID    = 1  // uint32
	OffsetBaseTx_BlockchainID = 5  // ids.ID (32 bytes)
	OffsetBaseTx_Memo         = 37 // bytes (offset+length, 8 bytes)
	SizeBaseTx                = 45
)

// BaseTx is a zero-copy typed accessor over a ZAP buffer carrying a v3 BaseTx.
type BaseTx struct {
	msg *zap.Message
	obj zap.Object
}

// NetworkID returns the network ID the tx targets.
func (t BaseTx) NetworkID() uint32 {
	return t.obj.Uint32(OffsetBaseTx_NetworkID)
}

// BlockchainID returns the chain ID the tx executes against.
func (t BaseTx) BlockchainID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetBaseTx_BlockchainID + i)
	}
	return out
}

// Memo returns the memo bytes.
//
// READ-ONLY: the returned slice aliases the underlying ZAP buffer. Mutation
// corrupts the parsed tx and any TxID = hash(buffer) computed downstream. To
// own the bytes, copy first: append([]byte(nil), m...).
//
// Zero-copy / zero-allocation contract: a defensive copy would allocate on
// every call, defeating the native-ZAP design. The read-only discipline is
// type-system-erased; enforce via review and the lint rule deferred to a
// follow-up.
func (t BaseTx) Memo() []byte {
	return t.obj.Bytes(OffsetBaseTx_Memo)
}

func (t BaseTx) Bytes() []byte { return t.msg.Bytes() }
func (t BaseTx) IsZero() bool  { return t.msg == nil }

// WrapBaseTx parses a ZAP buffer into a typed BaseTx accessor.
//
// Returns ErrWrongTxKind if the discriminator byte does not match
// TxKindBase; returns ErrInvalidMagic/ErrInvalidVersion from the ZAP layer
// on a malformed buffer.
func WrapBaseTx(b []byte) (BaseTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindBase)
	if err != nil {
		return BaseTx{}, err
	}
	return BaseTx{msg: msg, obj: obj}, nil
}

// NewBaseTx builds a v3 BaseTx into a fresh ZAP buffer. Memo may be nil.
func NewBaseTx(networkID uint32, blockchainID ids.ID, memo []byte) BaseTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeBaseTx + len(memo))
	ob := b.StartObject(SizeBaseTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindBase))
	ob.SetUint32(OffsetBaseTx_NetworkID, networkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetBaseTx_BlockchainID+i, blockchainID[i])
	}
	ob.SetBytes(OffsetBaseTx_Memo, memo)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return BaseTx{msg: msg, obj: msg.Root()}
}
