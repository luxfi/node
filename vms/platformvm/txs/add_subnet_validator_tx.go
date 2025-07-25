// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/vms/components/verify"
)

var (
	_ StakerTx        = (*AddSubnetValidatorTx)(nil)
	_ ScheduledStaker = (*AddSubnetValidatorTx)(nil)

	errAddPrimaryNetworkValidator = errors.New("can't add primary network validator with AddSubnetValidatorTx")
)

// AddSubnetValidatorTx is an unsigned addSubnetValidatorTx
type AddSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *AddSubnetValidatorTx) NodeID() ids.NodeID {
	return tx.SubnetValidator.NodeID
}

func (*AddSubnetValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	return nil, false, nil
}

func (*AddSubnetValidatorTx) PendingPriority() Priority {
	return SubnetPermissionedValidatorPendingPriority
}

func (*AddSubnetValidatorTx) CurrentPriority() Priority {
	return SubnetPermissionedValidatorCurrentPriority
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *AddSubnetValidatorTx) SyntacticVerify(ctx *consensus.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Subnet == constants.PrimaryNetworkID:
		return errAddPrimaryNetworkValidator
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.SubnetAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *AddSubnetValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddSubnetValidatorTx(tx)
}
