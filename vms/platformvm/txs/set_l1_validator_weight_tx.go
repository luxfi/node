// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*SetL1ValidatorWeightTx)(nil)

// SetL1ValidatorWeightTx carries a signed Warp message that sets an L1
// validator's weight. The struct IS the wire: it embeds spendingTx (the
// envelope) and adds the Warp Message as a variable-length bytes tail.
//
// Wire: zap header + object{ envelope@0..76, Message:bytesptr@77 }
// (kind=kindSetL1ValidatorWeight).
type SetL1ValidatorWeightTx struct {
	spendingTx
}

const (
	offSetWeightMessage = spendSize // bytes ptr (8B)
	sizeSetWeight       = 85
)

// NewSetL1ValidatorWeightTx builds the tx into a fresh zap buffer.
func NewSetL1ValidatorWeightTx(base *lux.BaseTx, message []byte) (*SetL1ValidatorWeightTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256 + sizeSetWeight)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(sizeSetWeight)
	setEnvelope(ob, kindSetL1ValidatorWeight, base, p)
	ob.SetBytes(offSetWeightMessage, message)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		return nil, err
	}
	return &SetL1ValidatorWeightTx{spendingTx{msg}}, nil
}

// Message is the signed Warp message (an AddressedCall carrying the
// SetL1ValidatorWeight payload). Returns a copy so callers can't mutate the
// buffer.
func (tx *SetL1ValidatorWeightTx) Message() []byte {
	if m := tx.root().Bytes(offSetWeightMessage); len(m) > 0 {
		return append([]byte(nil), m...)
	}
	return nil
}

func (tx *SetL1ValidatorWeightTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	return verifyBaseTx(tx.baseTx(), rt)
}

func (tx *SetL1ValidatorWeightTx) Visit(visitor Visitor) error {
	return visitor.SetL1ValidatorWeightTx(tx)
}
