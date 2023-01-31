// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/avalanchego/ids"
<<<<<<< HEAD
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
=======
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
)

// Validator is a struct that contains the base values representing a validator
// of the Avalanche Network.
type Validator struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	TxID      ids.ID
	Weight    uint64

	// index is used to efficiently remove validators from the validator set. It
	// represents the index of this validator in the vdrSlice and weights
	// arrays.
	index int
}

<<<<<<< HEAD
// GetValidatorOutput is a struct that contains the publicly relevant values of
// a validator of the Avalanche Network for the output of GetValidator.
type GetValidatorOutput struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	Weight    uint64
=======
func (v *validator) ID() ids.NodeID {
	return v.nodeID
}

func (v *validator) Weight() uint64 {
	return v.weight
}

// NewValidator returns a validator object that implements the Validator
// interface
func NewValidator(
	nodeID ids.NodeID,
	weight uint64,
) Validator {
	return &validator{
		nodeID: nodeID,
		weight: weight,
	}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
}
