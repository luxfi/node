// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/platformvm/fx"
)

var _ UnsignedTx = (*CreateSubnetTx)(nil)

// CreateSubnetTx is an unsigned proposal to create a new subnet
type CreateSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Who is authorized to manage this subnet
	Owner fx.Owner `serialize:"true" json:"owner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [CreateSubnetTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *CreateSubnetTx) InitCtx(ctx *quasar.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.Owner.Initialize(ctx)
}

// Initialize implements quasar.ContextInitializable
func (tx *CreateSubnetTx) Initialize(ctx *quasar.Context) error {
	tx.InitCtx(ctx)
	return nil
}

// SyntacticVerify verifies that this transaction is well-formed
func (tx *CreateSubnetTx) SyntacticVerify(ctx *quasar.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := tx.Owner.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *CreateSubnetTx) Visit(visitor Visitor) error {
	return visitor.CreateSubnetTx(tx)
}
