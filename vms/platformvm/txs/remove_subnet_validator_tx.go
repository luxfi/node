// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	consensusctx "github.com/luxfi/consensus/context"

	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/verify"
)

var (
	_ UnsignedTx = (*RemoveNetValidatorTx)(nil)

	ErrRemovePrimaryNetworkValidator = errors.New("can't remove primary network validator with RemoveNetValidatorTx")
)

// Removes a validator from a subnet.
type RemoveNetValidatorTx struct {
	BaseTx `serialize:"true"`
	// The node to remove from the subnet.
	NodeID ids.NodeID `serialize:"true" json:"nodeID"`
	// The net to remove the node from.
	Net ids.ID `serialize:"true" json:"netID"`
	// Proves that the issuer has the right to remove the node from the subnet.
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *RemoveSubnetValidatorTx) SyntacticVerify(ctx *consensusctx.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.Net == constants.PrimaryNetworkID:
		return ErrRemovePrimaryNetworkValidator
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *RemoveNetValidatorTx) Visit(visitor Visitor) error {
	return visitor.RemoveNetValidatorTx(tx)
}

// InitializeWithContext initializes the transaction with consensus context
func (tx *RemoveNetValidatorTx) InitializeWithContext(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
