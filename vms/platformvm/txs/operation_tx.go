// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/runtime"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

var (
	_ UnsignedTx             = (*OperationTx)(nil)
	_ secp256k1fx.UnsignedTx = (*OperationTx)(nil)

	ErrNilOperationTx           = errors.New("operation tx is nil")
	ErrNoOperations             = errors.New("operation tx must have at least one operation")
	ErrOperationsNotSortedUnique = errors.New("operations are not sorted and unique")
	ErrOperationDoubleSpend     = errors.New("operations attempt to double spend an input")
	// ErrWrongNumberOfCredentials catches malformed wire payloads where the
	// peer sent fewer (or more) credentials than [NumCredentials] requires.
	// Without this check the semantic executor would slice past the end of
	// [tx.Creds] and panic on op credentials.
	ErrWrongNumberOfCredentials = errors.New("wrong number of credentials")
)

// OperationTx applies one or more [Operation]s against previously minted
// assets. Like BaseTx it consumes inputs and produces outputs for fees; the
// authoritative side-effect is the operations.
type OperationTx struct {
	BaseTx `serialize:"true"`

	Ops []*Operation `serialize:"true" json:"operations"`
}

func (tx *OperationTx) InitRuntime(rt *runtime.Runtime) {
	tx.BaseTx.InitRuntime(rt)
}

// Operations returns the slice of operations carried by this tx. The
// returned slice MUST NOT be mutated.
func (tx *OperationTx) Operations() []*Operation {
	return tx.Ops
}

// InputIDs returns the set of UTXOIDs spent across BaseTx and every Operation.
func (tx *OperationTx) InputIDs() set.Set[ids.ID] {
	inputs := tx.BaseTx.InputIDs()
	for _, op := range tx.Ops {
		for _, utxo := range op.UTXOIDs {
			inputs.Add(utxo.InputID())
		}
	}
	return inputs
}

// OperationInputs returns the UTXOIDs consumed by the operations only
// (BaseTx inputs are returned by [BaseTx.InputIDs]).
func (tx *OperationTx) OperationInputs() []*lux.UTXOID {
	utxos := make([]*lux.UTXOID, 0)
	for _, op := range tx.Ops {
		utxos = append(utxos, op.UTXOIDs...)
	}
	return utxos
}

// NumCredentials returns the number of credentials expected when this tx is
// signed (one per BaseTx input plus one per Operation).
func (tx *OperationTx) NumCredentials() int {
	return len(tx.Ins) + len(tx.Ops)
}

// SyntacticVerify returns nil iff this tx is well-formed.
func (tx *OperationTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilOperationTx
	case tx.SyntacticallyVerified:
		return nil
	case len(tx.Ops) == 0:
		return ErrNoOperations
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return err
	}

	// Operation UTXOIDs must not collide with BaseTx input UTXOIDs and must
	// not collide with each other across operations.
	inputs := set.NewSet[ids.ID](len(tx.Ins))
	for _, in := range tx.Ins {
		inputs.Add(in.InputID())
	}
	for _, op := range tx.Ops {
		if err := op.Verify(); err != nil {
			return err
		}
		for _, utxoID := range op.UTXOIDs {
			inputID := utxoID.InputID()
			if inputs.Contains(inputID) {
				return ErrOperationDoubleSpend
			}
			inputs.Add(inputID)
		}
	}
	if !IsSortedAndUniqueOperations(tx.Ops, Codec) {
		return ErrOperationsNotSortedUnique
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *OperationTx) Visit(v Visitor) error {
	return v.OperationTx(tx)
}
