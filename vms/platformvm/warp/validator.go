// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"golang.org/x/exp/maps"

	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/utils/math"
	"github.com/luxfi/math/set"
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

type CanonicalValidatorSet struct {
	// Validators slice in canonical ordering of the validators that has public key
	Validators []*Validator
	// The total weight of all the validators, including the ones that doesn't have a public key
	TotalWeight uint64
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

// GetCanonicalValidatorSetFromSubnetID returns the CanonicalValidatorSet of [subnetID] at
// [pChcainHeight]. The returned CanonicalValidatorSet includes the validator set in a canonical ordering
// and the total weight.
func GetCanonicalValidatorSetFromSubnetID(
	ctx context.Context,
	pChainState ValidatorState,
	pChainHeight uint64,
	subnetID ids.ID,
) (CanonicalValidatorSet, error) {
	// Get the validator set at the given height.
	vdrSet, err := pChainState.GetValidatorSet(ctx, pChainHeight, netID)
	if err != nil {
		return CanonicalValidatorSet{}, err
	}

	// Convert the validator set into the canonical ordering.
	return FlattenValidatorSet(vdrSet)
}

// FlattenValidatorSet converts the provided [vdrSet] into a canonical ordering.
// Also returns the total weight of the validator set.
func FlattenValidatorSet(vdrSet map[ids.NodeID]*validators.GetValidatorOutput) (CanonicalValidatorSet, error) {
	var (
		// Map public keys to validators to handle duplicates
		pkToValidator = make(map[string]*Validator)
		totalWeight   uint64
		err           error
	)
	for _, vdr := range vdrSet {
		totalWeight, err = math.Add(totalWeight, vdr.Weight)
		if err != nil {
			return CanonicalValidatorSet{}, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}

		// Skip validators without public keys
		if len(vdrData.PublicKey) == 0 {
			continue
		}

		// Convert []byte to *bls.PublicKey
		blsPK, err := bls.PublicKeyFromCompressedBytes(vdr.PublicKey)
		if err != nil {
			continue // Skip invalid public keys
		}

		pkBytes := bls.PublicKeyToUncompressedBytes(blsPK)
		uniqueVdr, ok := vdrs[string(pkBytes)]
		if !ok {
			uniqueVdr = &Validator{
				PublicKey:      blsPK,
				PublicKeyBytes: pkBytes,
			}
			vdrs[string(pkBytes)] = uniqueVdr
		}

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

	// Sort validators by public key
	vdrList := maps.Values(vdrs)
	utils.Sort(vdrList)
	return CanonicalValidatorSet{Validators: vdrList, TotalWeight: totalWeight}, nil
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
		weight, err = math.Add(weight, vdr.Weight)
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

// GetCanonicalValidatorSetFromChainID returns the canonical validator set given a validators.State, pChain height and a sourceChainID.
func GetCanonicalValidatorSetFromChainID(ctx context.Context,
	pChainState validators.State,
	pChainHeight uint64,
	sourceChainID ids.ID,
) (CanonicalValidatorSet, error) {
	// In the new architecture, use sourceChainID as the subnet ID
	// This assumes a 1:1 mapping between chains and subnets
	return GetCanonicalValidatorSetFromSubnetID(ctx, pChainState, pChainHeight, sourceChainID)
}
