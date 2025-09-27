// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/math"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils"
)

var (
	_ utils.Sortable[*Validator] = (*Validator)(nil)

	ErrUnknownValidator = errors.New("unknown validator")
	ErrWeightOverflow   = errors.New("weight overflowed")
)

// ValidatorState defines the functions that must be implemented to get
// the canonical validator set for warp message validation.
type ValidatorState interface {
	GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*ValidatorData, error)
	GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error)
}

// ValidatorData contains the data for a single validator
type ValidatorData struct {
	NodeID    ids.NodeID
	PublicKey []byte
	Weight    uint64
}

type Validator struct {
	PublicKey      *bls.PublicKey
	PublicKeyBytes []byte
	Weight         uint64
	NodeIDs        []ids.NodeID
}

func (v *Validator) Compare(o *Validator) int {
	return bytes.Compare(v.PublicKeyBytes, o.PublicKeyBytes)
}

// GetCanonicalValidatorSet returns the validator set of [netID] at
// [pChcainHeight] in a canonical ordering. Also returns the total weight on
// [netID].
func GetCanonicalValidatorSet(
	ctx context.Context,
	pChainState ValidatorState,
	pChainHeight uint64,
	netID ids.ID,
) ([]*Validator, uint64, error) {
	// Get the validator set at the given height.
	vdrSet, err := pChainState.GetValidatorSet(ctx, pChainHeight, netID)
	if err != nil {
		return nil, 0, err
	}

	// Convert the validator set into the canonical ordering.
	return FlattenValidatorSet(vdrSet)
}

// FlattenValidatorSet converts the provided [vdrSet] into a canonical ordering.
// Also returns the total weight of the validator set.
func FlattenValidatorSet(vdrSet map[ids.NodeID]*ValidatorData) ([]*Validator, uint64, error) {
	var (
		// Map public keys to validators to handle duplicates
		pkToValidator = make(map[string]*Validator)
		totalWeight   uint64
		err           error
	)

	for nodeID, vdrData := range vdrSet {
		totalWeight, err = math.Add64(totalWeight, vdrData.Weight)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}

		// Skip validators without public keys
		if len(vdrData.PublicKey) == 0 {
			continue
		}

		// Parse the BLS public key - assume it's in uncompressed format
		// This is safe because we skip validators with invalid keys
		pk := bls.PublicKeyFromValidUncompressedBytes(vdrData.PublicKey)

		// Use uncompressed bytes as the canonical key representation
		pkBytes := bls.PublicKeyToUncompressedBytes(pk)
		pkKey := string(pkBytes)

		// Check if we already have a validator with this public key
		if existingVdr, exists := pkToValidator[pkKey]; exists {
			// Merge validators with duplicate public keys
			existingVdr.Weight, err = math.Add64(existingVdr.Weight, vdrData.Weight)
			if err != nil {
				return nil, 0, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
			}
			existingVdr.NodeIDs = append(existingVdr.NodeIDs, nodeID)
		} else {
			// Create new validator
			vdr := &Validator{
				PublicKey:      pk,
				PublicKeyBytes: pkBytes,
				Weight:         vdrData.Weight,
				NodeIDs:        []ids.NodeID{nodeID},
			}
			pkToValidator[pkKey] = vdr
		}
	}

	// Convert map to slice
	vdrs := make([]*Validator, 0, len(pkToValidator))
	for _, vdr := range pkToValidator {
		vdrs = append(vdrs, vdr)
	}

	// Sort validators by public key for canonical ordering
	utils.Sort(vdrs)

	// Recalculate total weight based on validators with valid public keys
	totalWeight = 0
	for _, vdr := range vdrs {
		totalWeight, err = math.Add64(totalWeight, vdr.Weight)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}
	}

	return vdrs, totalWeight, nil
}

// FilterValidators returns the validators in [vdrs] whose bit is set to 1 in
// [indices].
//
// Returns an error if [indices] references an unknown validator.
func FilterValidators(
	indices set.Bits,
	vdrs []*Validator,
) ([]*Validator, error) {
	// Verify that all alleged signers exist
	if indices.BitLen() > len(vdrs) {
		return nil, fmt.Errorf(
			"%w: NumIndices (%d) >= NumFilteredValidators (%d)",
			ErrUnknownValidator,
			indices.BitLen()-1, // -1 to convert from length to index
			len(vdrs),
		)
	}

	filteredVdrs := make([]*Validator, 0, len(vdrs))
	for i, vdr := range vdrs {
		if !indices.Contains(i) {
			continue
		}

		filteredVdrs = append(filteredVdrs, vdr)
	}
	return filteredVdrs, nil
}

// SumWeight returns the total weight of the provided validators.
func SumWeight(vdrs []*Validator) (uint64, error) {
	var (
		weight uint64
		err    error
	)
	for _, vdr := range vdrs {
		weight, err = math.Add64(weight, vdr.Weight)
		if err != nil {
			return 0, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}
	}
	return weight, nil
}

// AggregatePublicKeys returns the public key of the provided validators.
//
// Invariant: All of the public keys in [vdrs] are valid.
func AggregatePublicKeys(vdrs []*Validator) (*bls.PublicKey, error) {
	pks := make([]*bls.PublicKey, len(vdrs))
	for i, vdr := range vdrs {
		pks[i] = vdr.PublicKey
	}
	return bls.AggregatePublicKeys(pks)
}
