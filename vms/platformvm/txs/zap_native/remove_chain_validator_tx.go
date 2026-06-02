// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// RemoveChainValidatorTx v1 schema — NodeID (20B) + Subnet (32B).
//
// Legacy struct also has ChainAuth (verify.Verifiable credential) which lives
// outside the unsigned-tx bytes in the signed wrapper — the unsigned encoding
// here pins only the identity tuple the executor matches against state.
//
// Fixed-section layout (size 52 bytes):
//
//	NodeID 20B @ 0    (ids.NodeID)
//	Subnet 32B @ 20   (ids.ID)
const (
	SchemaVersionRemoveChainValidatorTx uint16 = 2

	OffsetRemoveChainValidatorTx_NodeID = 0
	OffsetRemoveChainValidatorTx_Subnet = ids.ShortIDLen
	SizeRemoveChainValidatorTx          = OffsetRemoveChainValidatorTx_Subnet + 32
)

type RemoveChainValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t RemoveChainValidatorTx) NodeID() ids.NodeID {
	var out ids.NodeID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetRemoveChainValidatorTx_NodeID + i)
	}
	return out
}

func (t RemoveChainValidatorTx) Subnet() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetRemoveChainValidatorTx_Subnet + i)
	}
	return out
}

func (t RemoveChainValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t RemoveChainValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapRemoveChainValidatorTx(b []byte) (RemoveChainValidatorTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return RemoveChainValidatorTx{}, err
	}
	return RemoveChainValidatorTx{msg: msg, obj: msg.Root()}, nil
}

func NewRemoveChainValidatorTx(nodeID ids.NodeID, subnet ids.ID) RemoveChainValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeRemoveChainValidatorTx)
	ob := b.StartObject(SizeRemoveChainValidatorTx)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetRemoveChainValidatorTx_NodeID+i, nodeID[i])
	}
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetRemoveChainValidatorTx_Subnet+i, subnet[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return RemoveChainValidatorTx{msg: msg, obj: msg.Root()}
}
