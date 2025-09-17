// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/math/math"
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
	GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error)
	GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error)
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
func FlattenValidatorSet(vdrSet map[ids.NodeID]*validators.GetValidatorOutput) ([]*Validator, uint64, error) {
	var (
		vdrs        = make(map[string]*Validator)
		totalWeight uint64
		err         error
	)
	for nodeID, vdr := range vdrSet {
		totalWeight, err = math.Add64(totalWeight, vdr.Weight)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrWeightOverflow, err)
		}

		// Skip validators without BLS keys
		if len(vdr.PublicKey) == 0 {
			continue
		}

		// Parse the BLS public key
		pk, err := bls.PublicKeyFromCompressedBytes(vdr.PublicKey)
		if err != nil {
			// If it fails, try uncompressed format (96 bytes)
			if len(vdr.PublicKey) == 96 {
				pk = bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey)
				if pk == nil {
					continue // Skip invalid keys
				}
			} else {
				continue // Skip invalid keys
			}
		}

		pkBytes := bls.PublicKeyToCompressedBytes(pk)
		pkStr := string(pkBytes)

		uniqueVdr, ok := vdrs[pkStr]
		if !ok {
			uniqueVdr = &Validator{
				PublicKey:      pk,
				PublicKeyBytes: pkBytes,
				Weight:         0,
				NodeIDs:        []ids.NodeID{},
			}
			vdrs[pkStr] = uniqueVdr
		}

		uniqueVdr.Weight += vdr.Weight
		uniqueVdr.NodeIDs = append(uniqueVdr.NodeIDs, nodeID)
	}

	// Convert map to slice and sort
	vdrList := make([]*Validator, 0, len(vdrs))
	for _, vdr := range vdrs {
		vdrList = append(vdrList, vdr)
	}
	utils.Sort(vdrList)
	return vdrList, totalWeight, nil
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
