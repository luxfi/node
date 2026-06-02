// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// BaseTx v1 schema — NetworkID + BlockchainID + Memo. The Outs/Ins fields of
// the legacy lux.BaseTx are variable-length nested-object lists and need a
// proper ZAP list schema; they ship in batch 3. This v1 form is the metadata
// envelope every higher-level platformvm tx embeds.
//
// Fixed-section layout (size 44 bytes):
//
//	NetworkID    uint32 @ 0
//	BlockchainID 32B    @ 4
//	Memo         bytes  @ 36   (offset+length pair, 8 bytes)
const (
	SchemaVersionBaseTx uint16 = 2

	OffsetBaseTx_NetworkID    = 0  // uint32
	OffsetBaseTx_BlockchainID = 4  // ids.ID (32 bytes)
	OffsetBaseTx_Memo         = 36 // bytes (offset+length, 8 bytes)
	SizeBaseTx                = 44
)

// BaseTx is a zero-copy typed accessor over a ZAP buffer carrying a v1 BaseTx.
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

// Memo returns the memo bytes. Zero-copy: the returned slice aliases the
// underlying ZAP buffer. Callers MUST NOT mutate it.
func (t BaseTx) Memo() []byte {
	return t.obj.Bytes(OffsetBaseTx_Memo)
}

func (t BaseTx) Bytes() []byte { return t.msg.Bytes() }
func (t BaseTx) IsZero() bool  { return t.msg == nil }

// WrapBaseTx parses a ZAP buffer into a typed BaseTx accessor.
func WrapBaseTx(b []byte) (BaseTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return BaseTx{}, err
	}
	return BaseTx{msg: msg, obj: msg.Root()}, nil
}

// NewBaseTx builds a v1 BaseTx into a fresh ZAP buffer. Memo may be nil.
func NewBaseTx(networkID uint32, blockchainID ids.ID, memo []byte) BaseTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeBaseTx + len(memo))
	ob := b.StartObject(SizeBaseTx)
	ob.SetUint32(OffsetBaseTx_NetworkID, networkID)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetBaseTx_BlockchainID+i, blockchainID[i])
	}
	ob.SetBytes(OffsetBaseTx_Memo, memo)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return BaseTx{msg: msg, obj: msg.Root()}
}
