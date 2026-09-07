// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*IncreaseL1ValidatorBalanceTx)(nil)

	ErrZeroBalance = errors.New("balance must be greater than 0")
)

// IncreaseL1ValidatorBalanceTx tops up an L1 validator's continuous-fee
// balance. The struct IS the wire: it embeds spendingTx (the envelope) and
// adds two delta fields read by offset.
//
// Wire: zap header + object{ envelope@0..76, ValidationID:32B@77,
// Balance:u64@109 } (kind=kindIncreaseL1ValidatorBalance).
type IncreaseL1ValidatorBalanceTx struct {
	spendingTx
}

const (
	offIncreaseValidationID = spendSize // 32B
	offIncreaseBalance      = 109       // u64
	sizeIncrease            = 117
)

// NewIncreaseL1ValidatorBalanceTx builds the tx into a fresh zap buffer.
func NewIncreaseL1ValidatorBalanceTx(base *lux.BaseTx, validationID ids.ID, balance uint64) (*IncreaseL1ValidatorBalanceTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256 + sizeIncrease)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(sizeIncrease)
	setEnvelope(ob, kindIncreaseL1ValidatorBalance, base, p)
	setID(ob, offIncreaseValidationID, validationID)
	ob.SetUint64(offIncreaseBalance, balance)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		return nil, err
	}
	return &IncreaseL1ValidatorBalanceTx{spendingTx{msg}}, nil
}

// ValidationID is the id of the validator being topped up (offset read).
func (tx *IncreaseL1ValidatorBalanceTx) ValidationID() ids.ID {
	return readID(tx.root(), offIncreaseValidationID)
}

// Balance is the amount to add; Balance <= sum($LUX inputs) - sum($LUX outputs)
// - TxFee (offset read).
func (tx *IncreaseL1ValidatorBalanceTx) Balance() uint64 {
	return tx.root().Uint64(offIncreaseBalance)
}

func (tx *IncreaseL1ValidatorBalanceTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Balance() == 0:
		return ErrZeroBalance
	}
	return verifyBaseTx(tx.baseTx(), rt)
}

func (tx *IncreaseL1ValidatorBalanceTx) Visit(visitor Visitor) error {
	return visitor.IncreaseL1ValidatorBalanceTx(tx)
}
