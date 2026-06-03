// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/runtime"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
)

var (
	_ UnsignedTx = (*BaseTx)(nil)

	ErrNilTx = errors.New("tx is nil")

	errOutputsNotSorted      = errors.New("outputs not sorted")
	errInputsNotSortedUnique = errors.New("inputs not sorted and unique")
)

// BaseTx contains fields common to many transaction types. It should be
// embedded in transaction implementations.
type BaseTx struct {
	lux.BaseTx `serialize:"true"`

	// true iff this transaction has already passed syntactic verification
	SyntacticallyVerified bool `json:"-"`

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (tx *BaseTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

func (tx *BaseTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (tx *BaseTx) InputIDs() set.Set[ids.ID] {
	inputIDs := set.NewSet[ids.ID](len(tx.Ins))
	for _, in := range tx.Ins {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

func (tx *BaseTx) Outputs() []*lux.TransferableOutput {
	return tx.Outs
}

// InitRuntime sets the FxID fields in the inputs and outputs of this [BaseTx]. Also
// sets the [rt] to the given [vm.rt] so that the addresses can be json
// marshalled into human readable format
func (tx *BaseTx) InitRuntime(rt *runtime.Runtime) {
	for _, in := range tx.BaseTx.Ins {
		in.FxID = secp256k1fx.ID
	}
	for _, out := range tx.BaseTx.Outs {
		out.FxID = secp256k1fx.ID
		out.InitRuntime(rt)
	}
}

// InitializeRuntime is a no-op. Runtime is passed explicitly.
func (tx *BaseTx) Initialize(ctx context.Context) error {
	return nil
}

// SyntacticVerify returns nil iff this tx is well formed
func (tx *BaseTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}
	if err := tx.BaseTx.Verify(rt); err != nil {
		return fmt.Errorf("metadata failed verification: %w", err)
	}
	for _, out := range tx.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
	}
	for _, in := range tx.Ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("input failed verification: %w", err)
		}
	}
	switch {
	case !lux.IsSortedTransferableOutputs(tx.Outs):
		return errOutputsNotSorted
	case !utils.IsSortedAndUnique(tx.Ins):
		return errInputsNotSortedUnique
	default:
		return nil
	}
}

func (tx *BaseTx) Visit(visitor Visitor) error {
	return visitor.BaseTx(tx)
}
