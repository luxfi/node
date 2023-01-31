// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/avalanchego/ids"
<<<<<<< HEAD
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
=======
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var _ Validator = (*validator)(nil)

// Validator is the minimal description of someone that can be sampled.
type Validator interface {
	// ID returns the node ID of this validator
	ID() ids.NodeID

	// PublicKey returns the BLS public key this validator registered when being
	// added, if one exists.
	PublicKey() *bls.PublicKey

	// Weight that can be used for weighted sampling. If this validator is
	// validating the primary network, returns the amount of AVAX staked.
	Weight() uint64
}

// validator is a struct that contains the base values required by the validator
// interface.
type validator struct {
	nodeID ids.NodeID
	pk     *bls.PublicKey
	weight uint64
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))

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

func (v *validator) PublicKey() *bls.PublicKey {
	return v.pk
}

func (v *validator) Weight() uint64 {
	return v.weight
}

// NewValidator returns a validator object that implements the Validator
// interface
func NewValidator(
	nodeID ids.NodeID,
	pk *bls.PublicKey,
	weight uint64,
) Validator {
	return &validator{
		nodeID: nodeID,
		pk:     pk,
		weight: weight,
	}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
}
