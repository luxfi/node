// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// SlashValidatorTx v3 schema — TxKind + NodeID (20B) + Network (32B) +
// SlashPercentage.
//
// The legacy struct also carries an Evidence object (height, type, two
// message/signature pairs). Evidence is variable-length nested bytes and
// ships with the proper list/object schema in batch 3. SlashPercentage is
// included here as a uint32 because it's fixed-size and the executor needs
// it before evaluating the evidence.
//
// Fixed-section layout (size 57 bytes):
//
//	TxKind          uint8  @ 0
//	NodeID          20B    @ 1     (ids.NodeID, ShortIDLen)
//	Network         32B    @ 21    (ids.ID)
//	SlashPercentage uint32 @ 53
const (
	SchemaVersionSlashValidatorTx uint16 = 3

	OffsetSlashValidatorTx_NodeID          = 1
	OffsetSlashValidatorTx_Network         = OffsetSlashValidatorTx_NodeID + ids.ShortIDLen
	OffsetSlashValidatorTx_SlashPercentage = OffsetSlashValidatorTx_Network + 32
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

// Network returns the L1 network ID (32B) the slashed validator belongs to.
// (Field stays named Network on the wire; ids.ID semantics unchanged.)
func (t SlashValidatorTx) Network() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetSlashValidatorTx_Network + i)
	}
	return out
}

func (t SlashValidatorTx) SlashPercentage() uint32 {
	return t.obj.Uint32(OffsetSlashValidatorTx_SlashPercentage)
}

func (t SlashValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t SlashValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapSlashValidatorTx(b []byte) (SlashValidatorTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindSlashValidator)
	if err != nil {
		return SlashValidatorTx{}, err
	}
	return SlashValidatorTx{msg: msg, obj: obj}, nil
}

func NewSlashValidatorTx(nodeID ids.NodeID, network ids.ID, slashPercentage uint32) SlashValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeSlashValidatorTx)
	ob := b.StartObject(SizeSlashValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindSlashValidator))
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetSlashValidatorTx_NodeID+i, nodeID[i])
	}
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetSlashValidatorTx_Network+i, network[i])
	}
	ob.SetUint32(OffsetSlashValidatorTx_SlashPercentage, slashPercentage)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return SlashValidatorTx{msg: msg, obj: msg.Root()}
}
