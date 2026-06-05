// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	validators "github.com/luxfi/validators"

	safemath "github.com/luxfi/math"
)

func (s *state) ApplyValidatorWeightDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	netID ids.ID,
) error {
	diffIter := s.validatorWeightDiffsDB.NewIteratorWithStartAndPrefix(
		marshalStartDiffKey(netID, startHeight),
		netID[:],
	)
	defer diffIter.Release()

	prevHeight := startHeight + 1
	for diffIter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, parsedHeight, nodeID, err := unmarshalDiffKey(diffIter.Key())
		if err != nil {
			return err
		}

		if parsedHeight > prevHeight {
			log.Error("unexpected parsed height",
				log.Stringer("netID", netID),
				log.Uint64("parsedHeight", parsedHeight),
				log.Stringer("nodeID", nodeID),
				log.Uint64("prevHeight", prevHeight),
				log.Uint64("startHeight", startHeight),
				log.Uint64("endHeight", endHeight),
			)
		}

		// If the parsedHeight is less than our target endHeight, then we have
		// fully processed the diffs from startHeight through endHeight.
		if parsedHeight < endHeight {
			return diffIter.Error()
		}

		prevHeight = parsedHeight

		weightDiff, err := unmarshalWeightDiff(diffIter.Value())
		if err != nil {
			return err
		}

		if err := applyWeightDiff(validators, nodeID, weightDiff); err != nil {
			return err
		}
	}
	return diffIter.Error()
}

func applyWeightDiff(
	vdrs map[ids.NodeID]*validators.GetValidatorOutput,
	nodeID ids.NodeID,
	weightDiff *ValidatorWeightDiff,
) error {
	vdr, ok := vdrs[nodeID]
	if !ok {
		// This node isn't in the current validator set.
		vdr = &validators.GetValidatorOutput{
			NodeID: nodeID,
		}
		vdrs[nodeID] = vdr
	}

	// Preserve TxID from ValidationID if this is an L1 validator diff.
	// The ValidationID field in the diff always contains the original TxID
	// (set during diff creation in calculateValidatorDiffs).
	// When applying diffs backward, this restores the correct TxID even when
	// ValidationID changes.
	if weightDiff.ValidationID != ids.Empty {
		vdr.TxID = weightDiff.ValidationID
	}

	// The weight of this node changed at this block.
	var err error
	if weightDiff.Decrease {
		// The validator's weight was decreased at this block, so in the
		// prior block it was higher.
		vdr.Weight, err = safemath.Add(vdr.Weight, weightDiff.Amount)
	} else {
		// The validator's weight was increased at this block, so in the
		// prior block it was lower.
		vdr.Weight, err = safemath.Sub(vdr.Weight, weightDiff.Amount)
	}
	if err != nil {
		return err
	}

	// Keep Light in sync with Weight (Light is an alias for Weight)
	vdr.Light = vdr.Weight

	if vdr.Weight == 0 {
		// The validator's weight was 0 before this block so they weren't in the
		// validator set.
		delete(vdrs, nodeID)
	}
	return nil
}

func (s *state) ApplyValidatorPublicKeyDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	chainID ids.ID,
) error {
	diffIter := s.validatorPublicKeyDiffsDB.NewIteratorWithStartAndPrefix(
		marshalStartDiffKey(chainID, startHeight),
		chainID[:],
	)
	defer diffIter.Release()

	for diffIter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, parsedHeight, nodeID, err := unmarshalDiffKey(diffIter.Key())
		if err != nil {
			return err
		}
		// If the parsedHeight is less than our target endHeight, then we have
		// fully processed the diffs from startHeight through endHeight.
		if parsedHeight < endHeight {
			break
		}

		vdr, ok := validators[nodeID]
		if !ok {
			continue
		}

		pkBytes := diffIter.Value()
		if len(pkBytes) == 0 {
			vdr.PublicKey = nil
			continue
		}

		vdr.PublicKey = pkBytes
	}

	// Note: this does not fallback to the linkeddb index because the linkeddb
	// index does not contain entries for when to remove the public key.
	//
	// Nodes may see inconsistent public keys for heights before the new public
	// key index was populated.
	return diffIter.Error()
}

// getInheritedPublicKey returns the primary network validator's public key.
//
// Note: This function may return a nil public key and no error if the primary
// network validator does not have a public key.
func (s *state) getInheritedPublicKey(nodeID ids.NodeID) (*bls.PublicKey, error) {
	if vdr, ok := s.currentStakers.validators[constants.PrimaryNetworkID][nodeID]; ok && vdr.validator != nil {
		// The primary network validator is present.
		return vdr.validator.PublicKey, nil
	}
	if vdr, ok := s.currentStakers.validatorDiffs[constants.PrimaryNetworkID][nodeID]; ok && vdr.validator != nil {
		// The primary network validator is being modified.
		return vdr.validator.PublicKey, nil
	}
	return nil, fmt.Errorf("%w: %s", errMissingPrimaryNetworkValidator, nodeID)
}

// updateValidatorManager updates the validator manager with the pending
// validator set changes. L1s with zero active weight remain cached to
// avoid repeated database lookups during chain reorganization.
//
// This function must be called prior to writeCurrentStakers and
// writeL1Validators.
func (s *state) updateValidatorManager(updateValidators bool) error {
	if !updateValidators {
		return nil
	}

	for chainID, validatorDiffs := range s.currentStakers.validatorDiffs {
		// Record the change in weight and/or public key for each validator.
		for nodeID, diff := range validatorDiffs {
			weightDiff, err := diff.WeightDiff()
			if err != nil {
				return err
			}

			if weightDiff.Amount == 0 {
				continue // No weight change; go to the next validator.
			}

			if weightDiff.Decrease {
				if err := s.validators.RemoveWeight(chainID, nodeID, weightDiff.Amount); err != nil {
					return fmt.Errorf("failed to reduce validator weight: %w", err)
				}
				continue
			}

			if diff.validatorStatus != added {
				if err := s.validators.AddWeight(chainID, nodeID, weightDiff.Amount); err != nil {
					return fmt.Errorf("failed to increase validator weight: %w", err)
				}
				continue
			}

			pk, err := s.getInheritedPublicKey(nodeID)
			if err != nil {
				// This should never happen as there should always be a primary
				// network validator corresponding to a chain validator.
				return err
			}

			err = s.validators.AddStaker(
				chainID,
				nodeID,
				bls.PublicKeyToUncompressedBytes(pk),
				diff.validator.TxID,
				weightDiff.Amount,
			)
			if err != nil {
				return fmt.Errorf("failed to add validator: %w", err)
			}
		}
	}

	// Remove all deleted L1 validators. This must be done before adding new
	// L1 validators to support the case where a validator is removed and then
	// immediately re-added with a different validationID.
	//
	// Sort validators by ValidationID for deterministic processing order.
	// This is important when multiple inactive validators share the same
	// effectiveNodeID (ids.EmptyNodeID), as the first one processed sets
	// the TxID in the validators manager.
	sortedValidationIDs := slices.Collect(maps.Keys(s.l1ValidatorsDiff.modified))
	slices.SortFunc(sortedValidationIDs, func(a, b ids.ID) int {
		return a.Compare(b)
	})
	for _, validationID := range sortedValidationIDs {
		l1Validator := s.l1ValidatorsDiff.modified[validationID]
		if !l1Validator.isDeleted() {
			continue
		}

		priorL1Validator, err := s.getPersistedL1Validator(validationID)
		if err == database.ErrNotFound {
			// Deleting a non-existent validator is a noop. This can happen if
			// the validator was added and then immediately removed.
			continue
		}
		if err != nil {
			return err
		}

		if err := s.validators.RemoveWeight(priorL1Validator.ChainID, priorL1Validator.effectiveNodeID(), priorL1Validator.Weight); err != nil {
			return err
		}
	}

	// Now that the removed L1 validators have been deleted, perform additions
	// and modifications.
	for _, validationID := range sortedValidationIDs {
		l1Validator := s.l1ValidatorsDiff.modified[validationID]
		if l1Validator.isDeleted() {
			continue
		}

		priorL1Validator, err := s.getPersistedL1Validator(validationID)
		switch err {
		case nil:
			// Modifying an existing validator
			if priorL1Validator.IsActive() == l1Validator.IsActive() {
				// This validator's active status isn't changing. This means
				// the effectiveNodeIDs are equal.
				nodeID := l1Validator.effectiveNodeID()
				if priorL1Validator.Weight < l1Validator.Weight {
					err = s.validators.AddWeight(l1Validator.ChainID, nodeID, l1Validator.Weight-priorL1Validator.Weight)
				} else if priorL1Validator.Weight > l1Validator.Weight {
					err = s.validators.RemoveWeight(l1Validator.ChainID, nodeID, priorL1Validator.Weight-l1Validator.Weight)
				}
			} else {
				// This validator's active status is changing.
				err = errors.Join(
					s.validators.RemoveWeight(l1Validator.ChainID, priorL1Validator.effectiveNodeID(), priorL1Validator.Weight),
					addL1ValidatorToValidatorManager(s.validators, l1Validator),
				)
			}
		case database.ErrNotFound:
			// Adding a new validator
			err = addL1ValidatorToValidatorManager(s.validators, l1Validator)
		}
		if err != nil {
			return err
		}
	}

	// Update the stake metrics
	totalWeight, err := s.validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("failed to get total weight of primary network: %w", err)
	}

	s.metrics.SetLocalStake(s.validators.GetWeight(constants.PrimaryNetworkID, s.rt.NodeID))
	s.metrics.SetTotalStake(totalWeight)
	return nil
}

type validatorDiff struct {
	weightDiff    ValidatorWeightDiff
	prevPublicKey []byte
	newPublicKey  []byte
	hadRemoval    bool // True if pass 1 processed a removal for this (netID, nodeID)
}

// calculateValidatorDiffs calculates the validator set diff contained by the
// pending validator set changes.
//
// This function must be called prior to writeCurrentStakers.
func (s *state) calculateValidatorDiffs() (map[chainIDNodeID]*validatorDiff, error) {
	changes := make(map[chainIDNodeID]*validatorDiff)

	// Calculate the changes to the pre-LP-77 validator set
	for chainID, chainDiffs := range s.currentStakers.validatorDiffs {
		for nodeID, diff := range chainDiffs {
			weightDiff, err := diff.WeightDiff()
			if err != nil {
				return nil, err
			}

			// For legacy validators, set ValidationID to the validator's TxID
			// to ensure TxID is preserved during historical reconstruction.
			if diff.validator != nil && weightDiff.Amount != 0 {
				weightDiff.ValidationID = diff.validator.TxID
			}

			pk, err := s.getInheritedPublicKey(nodeID)
			if err != nil {
				// This should never happen as there should always be a primary
				// network validator corresponding to a chain validator.
				return nil, err
			}

			change := &validatorDiff{
				weightDiff: weightDiff,
			}
			if pk != nil {
				pkBytes := bls.PublicKeyToUncompressedBytes(pk)
				if diff.validatorStatus != added {
					change.prevPublicKey = pkBytes
				}
				if diff.validatorStatus != deleted {
					change.newPublicKey = pkBytes
				}
			}

			chainIDNodeID := chainIDNodeID{
				chainID: chainID,
				nodeID:  nodeID,
			}
			changes[chainIDNodeID] = change
		}
	}

	// Calculate the changes to the LP-77 validator set
	//
	// Process in two passes to ensure TxID preservation during ValidationID changes:
	// Pass 1: Process removals (weight decreases) to capture original TxIDs
	// Pass 2: Process additions (weight increases) without overwriting TxIDs from pass 1

	// Collect entries into two slices to ensure deterministic processing order
	type validatorEntry struct {
		validationID ids.ID
		validator    L1Validator
	}
	var removals, additions []validatorEntry

	for validationID, l1Validator := range s.l1ValidatorsDiff.modified {
		_, err := s.getPersistedL1Validator(validationID)
		if err == nil {
			// This validator existed before, so we're removing it
			removals = append(removals, validatorEntry{validationID, l1Validator})
		} else if err != database.ErrNotFound {
			return nil, err
		}

		// If not deleted, we're also adding it (possibly with a different ValidationID)
		if !l1Validator.isDeleted() {
			additions = append(additions, validatorEntry{validationID, l1Validator})
		}
	}

	// Pass 1: Process all removals first
	for _, entry := range removals {
		priorL1Validator, err := s.getPersistedL1Validator(entry.validationID)
		if err != nil {
			return nil, err
		}

		chainIDNodeID := chainIDNodeID{
			chainID: priorL1Validator.ChainID,
			nodeID:  priorL1Validator.effectiveNodeID(),
		}
		diff := getOrSetDefault(changes, chainIDNodeID)
		// For removals, always set ValidationID to the original TxID.
		// This ensures TxID preservation when ValidationID changes.
		diff.weightDiff.ValidationID = entry.validationID
		diff.hadRemoval = true // Mark that this diff includes a removal
		if err := diff.weightDiff.Sub(priorL1Validator.Weight); err != nil {
			return nil, err
		}
		diff.prevPublicKey = priorL1Validator.effectivePublicKeyBytes()
	}

	// Pass 2: Process all additions
	for _, entry := range additions {
		chainIDNodeID := chainIDNodeID{
			chainID: entry.validator.ChainID,
			nodeID:  entry.validator.effectiveNodeID(),
		}
		diff := getOrSetDefault(changes, chainIDNodeID)
		// Only set ValidationID if not already set by pass 1 (removal).
		// This preserves the original TxID when a ValidationID changes.
		if diff.weightDiff.ValidationID == ids.Empty {
			diff.weightDiff.ValidationID = entry.validationID
		}
		if err := diff.weightDiff.Add(entry.validator.Weight); err != nil {
			return nil, err
		}
		diff.newPublicKey = entry.validator.effectivePublicKeyBytes()
	}

	return changes, nil
}

// writeValidatorDiffs writes the validator set diff contained by the pending
// validator set changes to disk.
//
// This function must be called prior to writeCurrentStakers.
func (s *state) writeValidatorDiffs(height uint64) error {
	changes, err := s.calculateValidatorDiffs()
	if err != nil {
		return err
	}

	// Write the changes to the database
	for chainIDNodeID, diff := range changes {
		diffKey := marshalDiffKey(chainIDNodeID.chainID, height, chainIDNodeID.nodeID)
		// Write weight diff if:
		// 1. Amount changed, OR
		// 2. Amount didn't change but ValidationID changed (hadRemoval indicates removal+addition)
		// This ensures TxID tracking across ValidationID changes even when net weight is unchanged.
		shouldWrite := diff.weightDiff.Amount != 0 || (diff.hadRemoval && diff.weightDiff.ValidationID != ids.Empty)
		if shouldWrite {
			err := s.validatorWeightDiffsDB.Put(
				diffKey,
				marshalWeightDiff(&diff.weightDiff),
			)
			if err != nil {
				return err
			}
		}
		if !bytes.Equal(diff.prevPublicKey, diff.newPublicKey) {
			err := s.validatorPublicKeyDiffsDB.Put(
				diffKey,
				diff.prevPublicKey,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// getOrSetDefault returns the value at k in m if it exists. If it doesn't
// exist, it sets m[k] to a new value and returns that value.
func getOrSetDefault[K comparable, V any](m map[K]*V, k K) *V {
	if v, ok := m[k]; ok {
		return v
	}

	v := new(V)
	m[k] = v
	return v
}
