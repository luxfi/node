// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// RewardValidatorTx — single 32-byte TxID field.
// Issued by the chain at the end of every validator's stake period to
// distribute the reward.
const (
	SchemaVersionRewardValidatorTx uint16 = 2

	OffsetRewardValidatorTx_TxID = 0 // ids.ID (32 bytes)
	SizeRewardValidatorTx        = 32
)

type RewardValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t RewardValidatorTx) TxID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetRewardValidatorTx_TxID + i)
	}
	return out
}

func (t RewardValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t RewardValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapRewardValidatorTx(b []byte) (RewardValidatorTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return RewardValidatorTx{}, err
	}
	return RewardValidatorTx{msg: msg, obj: msg.Root()}, nil
}

func NewRewardValidatorTx(txID ids.ID) RewardValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeRewardValidatorTx)
	ob := b.StartObject(SizeRewardValidatorTx)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetRewardValidatorTx_TxID+i, txID[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return RewardValidatorTx{msg: msg, obj: msg.Root()}
}
