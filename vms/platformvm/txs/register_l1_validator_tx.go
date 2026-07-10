// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var _ UnsignedTx = (*RegisterL1ValidatorTx)(nil)

// RegisterL1ValidatorTx registers an L1 validator. The struct IS the wire: it
// embeds the spending envelope and reads its delta fields by offset.
//
// Delta layout (fixed section, after the 77-byte spending envelope):
//
//	Balance           u64  @ 77   (Balance <= sum($LUX inputs) - sum($LUX outputs) - TxFee)
//	ProofOfPossession 96B  @ 85   (fixed, BLS PoP of the key in the Message)
//	Message           8B   @ 181  (bytes ptr: signed Warp AddressedCall payload)
type RegisterL1ValidatorTx struct {
	spendingTx
}

const (
	offRegisterBalance      = spendSize // 77
	offRegisterPoP          = 85
	offRegisterMessage      = 181
	sizeRegisterL1Validator = 189
)

// NewRegisterL1ValidatorTx builds the tx into a fresh zap buffer.
func NewRegisterL1ValidatorTx(base *lux.BaseTx, balance uint64, proofOfPossession [bls.SignatureLen]byte, message []byte) (*RegisterL1ValidatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeRegisterL1Validator)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeRegisterL1Validator)
	setEnvelope(ob, kindRegisterL1Validator, base, p)
	ob.SetUint64(offRegisterBalance, balance)
	ob.SetBytesFixed(offRegisterPoP, proofOfPossession[:])
	ob.SetBytes(offRegisterMessage, message)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &RegisterL1ValidatorTx{spendingTx{msg}}, nil
}

// Balance is the initial balance funding this L1 validator (offset read).
func (tx *RegisterL1ValidatorTx) Balance() uint64 { return tx.root().Uint64(offRegisterBalance) }

// ProofOfPossession is the BLS PoP of the key included in the Message.
func (tx *RegisterL1ValidatorTx) ProofOfPossession() [bls.SignatureLen]byte {
	var pop [bls.SignatureLen]byte
	copy(pop[:], tx.root().BytesFixedSlice(offRegisterPoP, bls.SignatureLen))
	return pop
}

// Message is the signed Warp message carrying the RegisterL1Validator payload.
func (tx *RegisterL1ValidatorTx) Message() []byte {
	if m := tx.root().Bytes(offRegisterMessage); len(m) > 0 {
		return append([]byte(nil), m...)
	}
	return nil
}

func (tx *RegisterL1ValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}

	base := BaseTx{BaseTx: tx.baseTx()}
	return base.SyntacticVerify(rt)
}

func (tx *RegisterL1ValidatorTx) Visit(visitor Visitor) error {
	return visitor.RegisterL1ValidatorTx(tx)
}
