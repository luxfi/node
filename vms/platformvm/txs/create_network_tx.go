// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"

	"github.com/luxfi/runtime"

	"github.com/luxfi/node/vms/platformvm/fx"
)

var _ UnsignedTx = (*CreateNetworkTx)(nil)

// CreateNetworkTx is an unsigned proposal to create a new chain
type CreateNetworkTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Who is authorized to manage this chain
	Owner fx.Owner `serialize:"true" json:"owner"`
}

// InitRuntime sets the FxID fields in the inputs and outputs of this
// [CreateNetworkTx]. Also sets the [rt] to the given [vm.rt] so that
// the addresses can be json marshalled into human readable format
func (tx *CreateNetworkTx) InitRuntime(rt *runtime.Runtime) {
	tx.BaseTx.InitRuntime(rt)
	// Owner doesn't have InitRuntime method
}

// SyntacticVerify verifies that this transaction is well-formed
func (tx *CreateNetworkTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return err
	}
	if err := tx.Owner.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *CreateNetworkTx) Visit(visitor Visitor) error {
	return visitor.CreateNetworkTx(tx)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (tx *CreateNetworkTx) Initialize(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
