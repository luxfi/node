// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// DisableL1ValidatorTx v3 schema — TxKind + ValidationID.
//
// Disables an L1 validator. The validator's stake is unbonded and removed
// from the active set. Once disabled the validator can be re-registered
// via a fresh RegisterL1ValidatorTx.
//
// Fixed-section layout (size 33 bytes):
//
//	TxKind         uint8 @ 0
//	ValidationID   32B   @ 1
const (
	SchemaVersionDisableL1ValidatorTx uint16 = 3

	OffsetDisableL1ValidatorTx_ValidationID = 1 // 32 bytes
	SizeDisableL1ValidatorTx                = 33
)

type DisableL1ValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t DisableL1ValidatorTx) ValidationID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetDisableL1ValidatorTx_ValidationID + i)
	}
	return out
}

func (t DisableL1ValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t DisableL1ValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapDisableL1ValidatorTx(b []byte) (DisableL1ValidatorTx, error) {
	msg, obj, err := parseAndCheckKind(b, TxKindDisableL1Validator)
	if err != nil {
		return DisableL1ValidatorTx{}, err
	}
	return DisableL1ValidatorTx{msg: msg, obj: obj}, nil
}

func NewDisableL1ValidatorTx(validationID ids.ID) DisableL1ValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeDisableL1ValidatorTx)
	ob := b.StartObject(SizeDisableL1ValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindDisableL1Validator))
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetDisableL1ValidatorTx_ValidationID+i, validationID[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return DisableL1ValidatorTx{msg: msg, obj: msg.Root()}
}
