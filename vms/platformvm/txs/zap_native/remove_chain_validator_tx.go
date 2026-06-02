// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// RemoveChainValidatorTx v3 schema — TxKind + NodeID (20B) + Network (32B).
//
// Legacy struct also has ChainAuth (verify.Verifiable credential) which lives
// outside the unsigned-tx bytes in the signed wrapper — the unsigned encoding
// here pins only the identity tuple the executor matches against state.
//
// Fixed-section layout (size 53 bytes):
//
//	TxKind  uint8 @ 0
//	NodeID  20B   @ 1     (ids.NodeID)
//	Network 32B   @ 21    (ids.ID)
const (
	SchemaVersionRemoveChainValidatorTx uint16 = 3

	OffsetRemoveChainValidatorTx_NodeID  = 1
	OffsetRemoveChainValidatorTx_Network = OffsetRemoveChainValidatorTx_NodeID + ids.ShortIDLen
	SizeRemoveChainValidatorTx           = OffsetRemoveChainValidatorTx_Network + 32
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

// Network returns the L1 network ID (32B) the validator belongs to.
func (t RemoveChainValidatorTx) Network() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetRemoveChainValidatorTx_Network + i)
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
	obj := msg.Root()
	if TxKind(obj.Uint8(OffsetTxKind)) != TxKindRemoveChainValidator {
		return RemoveChainValidatorTx{}, ErrWrongTxKind
	}
	return RemoveChainValidatorTx{msg: msg, obj: obj}, nil
}

func NewRemoveChainValidatorTx(nodeID ids.NodeID, network ids.ID) RemoveChainValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeRemoveChainValidatorTx)
	ob := b.StartObject(SizeRemoveChainValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindRemoveChainValidator))
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetRemoveChainValidatorTx_NodeID+i, nodeID[i])
	}
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetRemoveChainValidatorTx_Network+i, network[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return RemoveChainValidatorTx{msg: msg, obj: msg.Root()}
}
