// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"

	"github.com/luxfi/runtime"

	"errors"
	"fmt"

	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/utxo/secp256k1fx"
)

var (
	_ UnsignedTx = (*ExportTx)(nil)

	ErrWrongLocktime   = errors.New("wrong locktime reported")
	errNoExportOutputs = errors.New("no export outputs")
)

// ExportTx is an unsigned exportTx
type ExportTx struct {
	BaseTx `serialize:"true"`

	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`

	// Outputs that are exported to the chain
	ExportedOutputs []*lux.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// InitRuntime sets the FxID fields in the inputs and outputs of this
// [UnsignedExportTx]. Also sets the [rt] to the given [vm.rt] so that
// the addresses can be json marshalled into human readable format
func (tx *ExportTx) InitRuntime(rt *runtime.Runtime) {
	tx.BaseTx.InitRuntime(rt)
	for _, out := range tx.ExportedOutputs {
		out.FxID = secp256k1fx.ID
		out.InitRuntime(rt)
	}
}

// SyntacticVerify this transaction is well-formed
func (tx *ExportTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.ExportedOutputs) == 0:
		return errNoExportOutputs
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return err
	}

	for _, out := range tx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("output failed verification: %w", err)
		}
		if _, ok := out.Output().(*stakeable.LockOut); ok {
			return ErrWrongLocktime
		}
	}
	if !lux.IsSortedTransferableOutputs(tx.ExportedOutputs, Codec) {
		return errOutputsNotSorted
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ExportTx) Visit(visitor Visitor) error {
	return visitor.ExportTx(tx)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (tx *ExportTx) Initialize(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
