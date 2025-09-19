// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/types"
)

const MaxSubnetAddressLength = 4096

var (
	ErrConvertPermissionlessNet            = errors.New("cannot convert a permissionless subnet")
	ErrAddressTooLong                      = errors.New("address is too long")
	ErrConvertMustIncludeValidators        = errors.New("conversion must include at least one validator")
	ErrConvertValidatorsNotSortedAndUnique = errors.New("conversion validators must be sorted and unique")
	ErrZeroWeight                          = errors.New("validator weight must be non-zero")
)

// ConvertNetToL1Validator represents a validator for subnet-to-L1 conversion
type ConvertNetToL1Validator struct {
	// NodeID of this validator
	NodeID types.JSONByteSlice `serialize:"true" json:"nodeID"`

	// Weight of this validator used when sampling
	Weight uint64 `serialize:"true" json:"weight"`

	// Initial balance for this validator
	Balance uint64 `serialize:"true" json:"balance"`

	// Signer is the BLS key and proof of possession for this validator
	Signer signer.ProofOfPossession `serialize:"true" json:"signer"`

	// Leftover LUX from the Balance will be issued to this owner once it is
	// removed from the validator set
	RemainingBalanceOwner message.PChainOwner `serialize:"true" json:"remainingBalanceOwner"`

	// This owner has the authority to manually deactivate this validator
	DeactivationOwner message.PChainOwner `serialize:"true" json:"deactivationOwner"`
}

// Compare implements utils.Sortable
func (v *ConvertNetToL1Validator) Compare(o *ConvertNetToL1Validator) int {
	return bytes.Compare(v.NodeID, o.NodeID)
}

// Verify performs verification for this validator
func (v *ConvertNetToL1Validator) Verify() error {
	if v.Weight == 0 {
		return ErrZeroWeight
	}
	nodeID, err := ids.ToNodeID(v.NodeID)
	if err != nil {
		return err
	}
	if nodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}
	return verify.All(
		&v.Signer,
		&secp256k1fx.OutputOwners{
			Threshold: v.RemainingBalanceOwner.Threshold,
			Addrs:     v.RemainingBalanceOwner.Addresses,
		},
		&secp256k1fx.OutputOwners{
			Threshold: v.DeactivationOwner.Threshold,
			Addrs:     v.DeactivationOwner.Addresses,
		},
	)
}

// ConvertNetToL1Tx converts a net to an L1
type ConvertNetToL1Tx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the Net to transform
	Net ids.ID `serialize:"true" json:"netID"`

	// Chain where the Net manager lives
	ChainID ids.ID `serialize:"true" json:"chainID"`

	// Address of the Net manager
	Address types.JSONByteSlice `serialize:"true" json:"address"`

	// Initial pay-as-you-go validators for the Subnet
	Validators []*ConvertNetToL1Validator `serialize:"true" json:"validators"`

	// Authorizes this conversion
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// MarshalJSON marshals the ConvertNetToL1Tx to JSON with backward compatibility
func (tx *ConvertNetToL1Tx) MarshalJSON() ([]byte, error) {
	// Format address as hex string
	addressHex := "0x" + hex.EncodeToString(tx.Address)

	// Create a map to represent the JSON structure
	jsonMap := map[string]interface{}{
		"networkID":           tx.BaseTx.NetworkID,
		"blockchainID":        tx.BaseTx.BlockchainID,
		"outputs":             tx.BaseTx.Outs,
		"inputs":              tx.BaseTx.Ins,
		"memo":                tx.BaseTx.Memo,
		"subnetID":            tx.Net, // Map Net to subnetID for backward compatibility
		"chainID":             tx.ChainID,
		"address":             addressHex,
		"validators":          tx.Validators,
		"subnetAuthorization": tx.SubnetAuth,
	}

	return json.Marshal(jsonMap)
}

// SyntacticVerify performs syntactic verification of the transaction
func (tx *ConvertNetToL1Tx) SyntacticVerify(ctx context.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.Net == constants.PrimaryNetworkID:
		return ErrConvertPermissionlessNet
	case len(tx.Address) > MaxSubnetAddressLength:
		return ErrAddressTooLong
	case len(tx.Validators) == 0:
		return ErrConvertMustIncludeValidators
	case !utils.IsSortedAndUnique(tx.Validators):
		return ErrConvertValidatorsNotSortedAndUnique
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	for _, vdr := range tx.Validators {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}
	if tx.SubnetAuth != nil {
		if err := tx.SubnetAuth.Verify(); err != nil {
			return err
		}
	}

	tx.SyntacticallyVerified = true
	return nil
}

// InitCtx sets the FxID fields in the inputs and outputs of this tx.
// Also sets the context for json marshalling.
func (tx *ConvertNetToL1Tx) InitCtx(ctx context.Context) {
	tx.BaseTx.InitCtx(ctx)
	// The SubnetAuth doesn't have FxID since it's just a Verifiable
}

// Visit calls visitor.ConvertNetToL1Tx
func (tx *ConvertNetToL1Tx) Visit(visitor Visitor) error {
	return visitor.ConvertNetToL1Tx(tx)
}

var _ utils.Sortable[*ConvertNetToL1Validator] = (*ConvertNetToL1Validator)(nil)
