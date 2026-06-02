// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// SetL1ValidatorWeightTx — ValidationID (ids.ID, 32 bytes) + Nonce uint64 + Weight uint64.
//
// Used to adjust the stake weight of an L1 validator without a full
// re-register cycle. The Nonce is a strict monotonic counter scoped to the
// ValidationID, preventing replay.
const (
	SchemaVersionSetL1ValidatorWeightTx uint16 = 2

	OffsetSetL1ValidatorWeightTx_ValidationID = 0  // 32 bytes
	OffsetSetL1ValidatorWeightTx_Nonce        = 32 // uint64
	OffsetSetL1ValidatorWeightTx_Weight       = 40 // uint64
	SizeSetL1ValidatorWeightTx                = 48
)

type SetL1ValidatorWeightTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t SetL1ValidatorWeightTx) ValidationID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetSetL1ValidatorWeightTx_ValidationID + i)
	}
	return out
}

func (t SetL1ValidatorWeightTx) Nonce() uint64 {
	return t.obj.Uint64(OffsetSetL1ValidatorWeightTx_Nonce)
}

func (t SetL1ValidatorWeightTx) Weight() uint64 {
	return t.obj.Uint64(OffsetSetL1ValidatorWeightTx_Weight)
}

func (t SetL1ValidatorWeightTx) Bytes() []byte { return t.msg.Bytes() }
func (t SetL1ValidatorWeightTx) IsZero() bool  { return t.msg == nil }

func WrapSetL1ValidatorWeightTx(b []byte) (SetL1ValidatorWeightTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return SetL1ValidatorWeightTx{}, err
	}
	return SetL1ValidatorWeightTx{msg: msg, obj: msg.Root()}, nil
}

func NewSetL1ValidatorWeightTx(validationID ids.ID, nonce, weight uint64) SetL1ValidatorWeightTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeSetL1ValidatorWeightTx)
	ob := b.StartObject(SizeSetL1ValidatorWeightTx)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetSetL1ValidatorWeightTx_ValidationID+i, validationID[i])
	}
	ob.SetUint64(OffsetSetL1ValidatorWeightTx_Nonce, nonce)
	ob.SetUint64(OffsetSetL1ValidatorWeightTx_Weight, weight)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return SetL1ValidatorWeightTx{msg: msg, obj: msg.Root()}
}
