// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// SlashValidatorTx v1 schema — NodeID (20B) + Subnet (32B).
//
// The legacy struct also carries an Evidence object (height, type, two
// message/signature pairs) and SlashPercentage. Evidence is variable-length
// nested bytes and ships with the proper list/object schema in batch 3.
// SlashPercentage is included here as a uint32 because it's fixed-size and
// the executor needs it before evaluating the evidence.
//
// Fixed-section layout (size 56 bytes):
//
//	NodeID          20B    @ 0    (ids.NodeID, ShortIDLen)
//	Subnet          32B    @ 20   (ids.ID)
//	SlashPercentage uint32 @ 52
const (
	SchemaVersionSlashValidatorTx uint16 = 2

	OffsetSlashValidatorTx_NodeID          = 0
	OffsetSlashValidatorTx_Subnet          = ids.ShortIDLen
	OffsetSlashValidatorTx_SlashPercentage = OffsetSlashValidatorTx_Subnet + 32
	SizeSlashValidatorTx                   = OffsetSlashValidatorTx_SlashPercentage + 4
)

type SlashValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t SlashValidatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetSlashValidatorTx_NodeID + i)
	}
	return out
}

func (t SlashValidatorTx) Subnet() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetSlashValidatorTx_Subnet + i)
	}
	return out
}

func (t SlashValidatorTx) SlashPercentage() uint32 {
	return t.obj.Uint32(OffsetSlashValidatorTx_SlashPercentage)
}

func (t SlashValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t SlashValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapSlashValidatorTx(b []byte) (SlashValidatorTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return SlashValidatorTx{}, err
	}
	return SlashValidatorTx{msg: msg, obj: msg.Root()}, nil
}

func NewSlashValidatorTx(nodeID ids.NodeID, subnet ids.ID, slashPercentage uint32) SlashValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeSlashValidatorTx)
	ob := b.StartObject(SizeSlashValidatorTx)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetSlashValidatorTx_NodeID+i, nodeID[i])
	}
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetSlashValidatorTx_Subnet+i, subnet[i])
	}
	ob.SetUint32(OffsetSlashValidatorTx_SlashPercentage, slashPercentage)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return SlashValidatorTx{msg: msg, obj: msg.Root()}
}
