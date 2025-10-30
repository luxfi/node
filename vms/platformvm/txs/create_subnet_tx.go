// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	consensusctx "github.com/luxfi/consensus/context"

	"github.com/luxfi/node/vms/platformvm/fx"
)

var _ UnsignedTx = (*CreateNetTx)(nil)

// CreateNetTx is an unsigned proposal to create a new subnet
type CreateNetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Who is authorized to manage this subnet
	Owner fx.Owner `serialize:"true" json:"owner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [CreateNetTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *CreateSubnetTx) InitCtx(ctx *consensusctx.Context) {
	tx.BaseTx.InitCtx(ctx)
	// Owner doesn't have InitCtx method
}

// SyntacticVerify verifies that this transaction is well-formed
func (tx *CreateSubnetTx) SyntacticVerify(ctx *consensusctx.Context) error {
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

func (tx *CreateNetTx) Visit(visitor Visitor) error {
	return visitor.CreateNetTx(tx)
}

// InitializeWithContext initializes the transaction with consensus context
func (tx *CreateNetTx) InitializeWithContext(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
