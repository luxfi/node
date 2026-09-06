// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/runtime"
	"github.com/luxfi/util"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx = (*BaseTx)(nil)

	ErrNilTx = errors.New("tx is nil")

	errOutputsNotSorted      = errors.New("outputs not sorted")
	errInputsNotSortedUnique = errors.New("inputs not sorted and unique")
)

// BaseTx is the bare spending envelope: kind + NetworkID/BlockchainID/Outs/
// Ins/Memo, no delta fields. The struct IS the wire — it embeds spendingTx,
// which holds the zap buffer and serves the whole envelope surface (Bytes/
// NetworkID/Outputs/InputIDs/Memo/...). Delta-carrying tx types embed
// spendingTx the same way and add their own field accessors.
//
// Wire: zap header + object{ envelope@0..76 } (kind=kindBase).
type BaseTx struct {
	spendingTx
}

// NewBaseTx builds a bare spending tx into a fresh zap buffer — the one place
// the envelope fields become bytes (construction, not serialization).
func NewBaseTx(base *lux.BaseTx) (*BaseTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 256 + spendSize)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(spendSize)
	setEnvelope(ob, kindBase, base, p)
	ob.FinishAsRoot()
	msg, err := zap.Parse(b.Finish())
	if err != nil {
		return nil, err
	}
	return &BaseTx{spendingTx{msg}}, nil
}

// SyntacticVerify returns nil iff this tx is well formed.
func (tx *BaseTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	return verifyBaseTx(tx.baseTx(), rt)
}

func (tx *BaseTx) Visit(visitor Visitor) error { return visitor.BaseTx(tx) }

func (tx *BaseTx) Initialize(context.Context) error { return nil }

// verifyBaseTx runs the shared spending-envelope checks (metadata, per-output
// and per-input verification, canonical ordering). Every spending tx type
// composes this over its own delta-field checks — one envelope validator,
// reused.
func verifyBaseTx(base lux.BaseTx, rt *runtime.Runtime) error {
	if err := base.Verify(rt); err != nil {
		return fmt.Errorf("metadata failed verification: %w", err)
	}
	for _, out := range base.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
	}
	for _, in := range base.Ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("input failed verification: %w", err)
		}
	}
	switch {
	case !lux.IsSortedTransferableOutputs(base.Outs):
		return errOutputsNotSorted
	case !utils.IsSortedAndUnique(base.Ins):
		return errInputsNotSortedUnique
	default:
		return nil
	}
}
