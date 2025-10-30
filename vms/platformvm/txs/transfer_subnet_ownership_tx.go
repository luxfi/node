// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	consensusctx "github.com/luxfi/consensus/context"

	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	_ UnsignedTx = (*TransferNetOwnershipTx)(nil)

	ErrTransferPermissionlessNet = errors.New("cannot transfer ownership of a permissionless subnet")
)

type TransferNetOwnershipTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the net this tx is modifying
	Net ids.ID `serialize:"true" json:"netID"`
	// Proves that the issuer has the right to remove the node from the subnet.
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
	// Who is now authorized to manage this subnet
	Owner fx.Owner `serialize:"true" json:"newOwner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [TransferNetOwnershipTx]. Also sets the [ctx] to the given [vm.ctx] so
// that the addresses can be json marshalled into human readable format
func (tx *TransferSubnetOwnershipTx) InitCtx(ctx *consensusctx.Context) {
	tx.BaseTx.InitCtx(ctx)
	// Initialize context for Owner if it's *secp256k1fx.OutputOwners
	if owner, ok := tx.Owner.(*secp256k1fx.OutputOwners); ok {
		owner.InitCtx(ctx)
	}
}

func (tx *TransferSubnetOwnershipTx) SyntacticVerify(ctx *consensusctx.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.Net == constants.PrimaryNetworkID:
		return ErrTransferPermissionlessNet
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := verify.All(tx.SubnetAuth, tx.Owner); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *TransferNetOwnershipTx) Visit(visitor Visitor) error {
	return visitor.TransferNetOwnershipTx(tx)
}

// InitializeWithContext initializes the transaction with consensus context
func (tx *TransferNetOwnershipTx) InitializeWithContext(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
