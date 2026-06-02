// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// IncreaseL1ValidatorBalanceTx — ValidationID (ids.ID) + Balance uint64.
//
// Tops up the continuous-fee balance for an L1 validator without changing
// its stake weight or nonce.
const (
	SchemaVersionIncreaseL1ValidatorBalanceTx uint16 = 2

	OffsetIncreaseL1ValidatorBalanceTx_ValidationID = 0  // 32 bytes
	OffsetIncreaseL1ValidatorBalanceTx_Balance      = 32 // uint64
	SizeIncreaseL1ValidatorBalanceTx                = 40
)

type IncreaseL1ValidatorBalanceTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t IncreaseL1ValidatorBalanceTx) ValidationID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetIncreaseL1ValidatorBalanceTx_ValidationID + i)
	}
	return out
}

func (t IncreaseL1ValidatorBalanceTx) Balance() uint64 {
	return t.obj.Uint64(OffsetIncreaseL1ValidatorBalanceTx_Balance)
}

func (t IncreaseL1ValidatorBalanceTx) Bytes() []byte { return t.msg.Bytes() }
func (t IncreaseL1ValidatorBalanceTx) IsZero() bool  { return t.msg == nil }

func WrapIncreaseL1ValidatorBalanceTx(b []byte) (IncreaseL1ValidatorBalanceTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return IncreaseL1ValidatorBalanceTx{}, err
	}
	return IncreaseL1ValidatorBalanceTx{msg: msg, obj: msg.Root()}, nil
}

func NewIncreaseL1ValidatorBalanceTx(validationID ids.ID, balance uint64) IncreaseL1ValidatorBalanceTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeIncreaseL1ValidatorBalanceTx)
	ob := b.StartObject(SizeIncreaseL1ValidatorBalanceTx)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetIncreaseL1ValidatorBalanceTx_ValidationID+i, validationID[i])
	}
	ob.SetUint64(OffsetIncreaseL1ValidatorBalanceTx_Balance, balance)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return IncreaseL1ValidatorBalanceTx{msg: msg, obj: msg.Root()}
}
