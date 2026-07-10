// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*DisableL1ValidatorTx)(nil)

// DisableL1ValidatorTx disables an L1 validator, proving authorization with a
// credential. The struct IS the wire: it embeds spendingTx (the envelope) and
// adds the validation id plus an auth sig-index list.
//
// Wire: zap header + object{ envelope@0..76, ValidationID:32B@77,
// DisableAuth:listptr@109 } (kind=kindDisableL1Validator).
type DisableL1ValidatorTx struct {
	spendingTx
}

const (
	offDisableValidationID = spendSize // 32B
	offDisableAuth         = 109       // list ptr (8B)
	sizeDisable            = 117
)

// NewDisableL1ValidatorTx builds the tx into a fresh zap buffer.
func NewDisableL1ValidatorTx(base *lux.BaseTx, validationID ids.ID, disableAuth verify.Verifiable) (*DisableL1ValidatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256 + sizeDisable)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	authOff, authCount, err := writeAuth(b, disableAuth)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(sizeDisable)
	setEnvelope(ob, kindDisableL1Validator, base, p)
	setID(ob, offDisableValidationID, validationID)
	ob.SetList(offDisableAuth, authOff, authCount)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		return nil, err
	}
	return &DisableL1ValidatorTx{spendingTx{msg}}, nil
}

// ValidationID is the id of the validator being disabled (offset read).
func (tx *DisableL1ValidatorTx) ValidationID() ids.ID {
	return readID(tx.root(), offDisableValidationID)
}

// DisableAuth authorizes this validator to be disabled (offset read).
func (tx *DisableL1ValidatorTx) DisableAuth() verify.Verifiable {
	return readAuth(tx.root(), offDisableAuth)
}

func (tx *DisableL1ValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	return tx.DisableAuth().Verify()
}

func (tx *DisableL1ValidatorTx) Visit(visitor Visitor) error {
	return visitor.DisableL1ValidatorTx(tx)
}
