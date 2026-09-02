// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/container/iterator"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/database/linkeddb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/txs"
)

func (s *state) GetCurrentValidators(ctx context.Context, chainID ids.ID) ([]*Staker, []L1Validator, uint64, error) {
	// First add the current validators (non-L1)
	legacyBaseStakers := s.currentStakers.validators[chainID]
	legacyStakers := make([]*Staker, 0, len(legacyBaseStakers))
	for _, staker := range legacyBaseStakers {
		validator := staker.validator
		if validator == nil {
			continue
		}

		// A legacy chain validator holds no key of its own — AddChainValidatorTx
		// carries no signer — but it signs with the key of its primary-network
		// validator. That inherited key is what initValidatorSets registers in
		// the validator manager and what the height diffs record, so it is also
		// what this set has to name: quorum and warp read the key from here, and
		// a validator surfaced keyless keeps its weight in the denominator with
		// no way to ever vote toward it.
		if chainID != constants.PrimaryNetworkID && validator.PublicKey == nil {
			publicKey, err := s.getInheritedPublicKey(validator.NodeID)
			if err != nil {
				return nil, nil, 0, err
			}
			// getInheritedPublicKey returns a nil key without error for a
			// primary-network validator registered before BLS keys existed.
			// There is no key to inherit in that case.
			if publicKey != nil {
				// The stakers in currentStakers are live state shared with the
				// write paths, so hand back a copy rather than writing the
				// inherited key into the stored chain staker.
				inherited := *validator
				inherited.PublicKey = publicKey
				validator = &inherited
			}
		}

		legacyStakers = append(legacyStakers, validator)
	}

	// Then iterate over chainIDNodeID DB and add the L1 validators
	var l1Validators []L1Validator
	validationIDIter := s.chainIDNodeIDDB.NewIteratorWithPrefix(
		chainID[:],
	)
	defer validationIDIter.Release()

	for validationIDIter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, nil, 0, err
		}

		validationID, err := ids.ToID(validationIDIter.Value())
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to parse validation ID: %w", err)
		}

		vdr, err := s.GetL1Validator(validationID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to get validator: %w", err)
		}
		l1Validators = append(l1Validators, vdr)
	}

	return legacyStakers, l1Validators, s.currentHeight, nil
}

func (s *state) GetActiveL1ValidatorsIterator() (iterator.Iterator[L1Validator], error) {
	s.l1ValidatorsDiffLock.RLock()
	defer s.l1ValidatorsDiffLock.RUnlock()

	return s.l1ValidatorsDiff.getActiveL1ValidatorsIterator(
		s.activeL1Validators.newIterator(),
	), nil
}

func (s *state) NumActiveL1Validators() int {
	return s.activeL1Validators.len() + s.l1ValidatorsDiff.netAddedActive
}

func (s *state) WeightOfL1Validators(chainID ids.ID) (uint64, error) {
	if weight, modified := s.l1ValidatorsDiff.modifiedTotalWeight[chainID]; modified {
		return weight, nil
	}

	if weight, ok := s.weightsCache.Get(chainID); ok {
		return weight, nil
	}

	weight, err := database.GetUInt64(s.weightsDB, chainID[:])
	if err != nil {
		if err == database.ErrNotFound {
			weight = 0
		} else {
			return 0, err
		}
	}

	s.weightsCache.Put(chainID, weight)
	return weight, nil
}

// GetL1Validator allows for concurrent reads.
func (s *state) GetL1Validator(validationID ids.ID) (L1Validator, error) {
	if l1Validator, modified := s.l1ValidatorsDiff.modified[validationID]; modified {
		if l1Validator.isDeleted() {
			return L1Validator{}, database.ErrNotFound
		}
		return l1Validator, nil
	}

	return s.getPersistedL1Validator(validationID)
}

// getPersistedL1Validator returns the currently persisted
// L1Validator with the given validationID. It is guaranteed that any
// returned validator is either active or inactive (not deleted).
func (s *state) getPersistedL1Validator(validationID ids.ID) (L1Validator, error) {
	if l1Validator, ok := s.activeL1Validators.get(validationID); ok {
		return l1Validator, nil
	}

	return getL1Validator(s.inactiveCache, s.inactiveDB, validationID)
}

func (s *state) HasL1Validator(chainID ids.ID, nodeID ids.NodeID) (bool, error) {
	if has, modified := s.l1ValidatorsDiff.hasL1Validator(chainID, nodeID); modified {
		return has, nil
	}

	chainIDNodeID := chainIDNodeID{
		chainID: chainID,
		nodeID:  nodeID,
	}
	if has, ok := s.chainIDNodeIDCache.Get(chainIDNodeID); ok {
		return has, nil
	}

	key := chainIDNodeID.Marshal()
	has, err := s.chainIDNodeIDDB.Has(key)
	if err != nil {
		return false, err
	}

	s.chainIDNodeIDCache.Put(chainIDNodeID, has)
	return has, nil
}

func (s *state) PutL1Validator(l1Validator L1Validator) error {
	return s.l1ValidatorsDiff.putL1Validator(s, l1Validator)
}

func (s *state) GetCurrentValidator(netID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.currentStakers.GetValidator(netID, nodeID)
}

func (s *state) PutCurrentValidator(staker *Staker) error {
	s.currentStakers.PutValidator(staker)
	return nil
}

func (s *state) DeleteCurrentValidator(staker *Staker) {
	s.currentStakers.DeleteValidator(staker)
}

func (s *state) GetCurrentDelegatorIterator(chainID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	return s.currentStakers.GetDelegatorIterator(chainID, nodeID), nil
}

func (s *state) PutCurrentDelegator(staker *Staker) {
	s.currentStakers.PutDelegator(staker)
}

func (s *state) DeleteCurrentDelegator(staker *Staker) {
	s.currentStakers.DeleteDelegator(staker)
}

func (s *state) GetCurrentStakerIterator() (iterator.Iterator[*Staker], error) {
	return s.currentStakers.GetStakerIterator(), nil
}

func (s *state) GetPendingValidator(netID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.pendingStakers.GetValidator(netID, nodeID)
}

func (s *state) PutPendingValidator(staker *Staker) error {
	s.pendingStakers.PutValidator(staker)
	return nil
}

func (s *state) DeletePendingValidator(staker *Staker) {
	s.pendingStakers.DeleteValidator(staker)
}

func (s *state) GetPendingDelegatorIterator(chainID ids.ID, nodeID ids.NodeID) (iterator.Iterator[*Staker], error) {
	return s.pendingStakers.GetDelegatorIterator(chainID, nodeID), nil
}

func (s *state) PutPendingDelegator(staker *Staker) {
	s.pendingStakers.PutDelegator(staker)
}

func (s *state) DeletePendingDelegator(staker *Staker) {
	s.pendingStakers.DeleteDelegator(staker)
}

func (s *state) GetPendingStakerIterator() (iterator.Iterator[*Staker], error) {
	return s.pendingStakers.GetStakerIterator(), nil
}

func (s *state) GetStartTime(nodeID ids.NodeID, netID ids.ID) (time.Time, error) {
	staker, err := s.currentStakers.GetValidator(netID, nodeID)
	if err != nil {
		return time.Time{}, err
	}
	return staker.StartTime, nil
}

func (s *state) GetUptime(vdrID ids.NodeID, netID ids.ID) (time.Duration, time.Duration, error) {
	upDuration, lastUpdated, err := s.validatorState.GetUptime(vdrID, netID)
	if err != nil {
		return 0, 0, err
	}
	// Convert lastUpdated time.Time to Duration since Unix epoch
	lastUpdatedDuration := time.Duration(lastUpdated.Unix()) * time.Second
	return upDuration, lastUpdatedDuration, nil
}

func (s *state) SetUptime(vdrID ids.NodeID, netID ids.ID, upDuration time.Duration, lastUpdated time.Time) error {
	return s.validatorState.SetUptime(vdrID, netID, upDuration, lastUpdated)
}

func (s *state) loadActiveL1Validators() error {
	it := s.activeDB.NewIterator()
	defer it.Release()
	for it.Next() {
		key := it.Key()
		validationID, err := ids.ToID(key)
		if err != nil {
			return fmt.Errorf("failed to unmarshal ValidationID during load: %w", err)
		}

		var (
			value       = it.Value()
			l1Validator = L1Validator{
				ValidationID: validationID,
			}
		)
		if err := parseL1Validator(value, &l1Validator); err != nil {
			return fmt.Errorf("failed to unmarshal L1 validator: %w", err)
		}

		s.activeL1Validators.put(l1Validator)
	}

	return nil
}

func (s *state) loadCurrentValidators() error {
	s.currentStakers = newBaseStakers()

	fmt.Println("[VALIDATOR DEBUG] loadCurrentValidators: STARTING")
	log.Warn("loadCurrentValidators: starting", "dbPrefix", "currentValidatorList")
	validatorCount := 0
	validatorIt := s.currentValidatorList.NewIterator()
	defer validatorIt.Release()
	for validatorIt.Next() {
		validatorCount++
		txIDBytes := validatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return fmt.Errorf("failed loading validator transaction txID %s, %w", txID, err)
		}

		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
		}

		metadataBytes := validatorIt.Value()
		metadata := &validatorMetadata{
			txID: txID,
		}
		if scheduledStakerTx, ok := tx.Unsigned.(txs.ScheduledStaker); ok {
			// Populate [StakerStartTime] using the tx as a default in the event
			// it was not stored in the database.
			//
			// Note: We do not populate [LastUpdated] since it is expected to
			// always be present on disk.
			metadata.StakerStartTime = uint64(scheduledStakerTx.StartTime().Unix())
		}
		if err := parseValidatorMetadata(metadataBytes, metadata); err != nil {
			return err
		}

		staker, err := NewCurrentStaker(
			txID,
			stakerTx,
			time.Unix(int64(metadata.StakerStartTime), 0),
			metadata.PotentialReward)
		if err != nil {
			return err
		}

		s.currentStakers.LoadValidator(staker)
		log.Warn("loadCurrentValidators: loaded validator",
			"txID", staker.TxID,
			"nodeID", staker.NodeID,
			"chainID", staker.ChainID,
			"weight", staker.Weight,
		)

		s.validatorState.LoadValidatorMetadata(staker.NodeID, staker.ChainID, metadata)
	}
	log.Info("loadCurrentValidators: primary validators loaded",
		"count", validatorCount,
		"currentStakersValidatorsLen", len(s.currentStakers.validators),
	)

	chainValidatorIt := s.currentNetValidatorList.NewIterator()
	defer chainValidatorIt.Release()
	for chainValidatorIt.Next() {
		txIDBytes := chainValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
		}

		metadataBytes := chainValidatorIt.Value()
		metadata := &validatorMetadata{
			txID: txID,
		}
		if scheduledStakerTx, ok := tx.Unsigned.(txs.ScheduledStaker); ok {
			// Populate [StakerStartTime] and [LastUpdated] using the tx as a
			// default in the event they are not stored in the database.
			startTime := uint64(scheduledStakerTx.StartTime().Unix())
			metadata.StakerStartTime = startTime
			metadata.LastUpdated = startTime
		}
		if err := parseValidatorMetadata(metadataBytes, metadata); err != nil {
			return err
		}

		staker, err := NewCurrentStaker(
			txID,
			stakerTx,
			time.Unix(int64(metadata.StakerStartTime), 0),
			metadata.PotentialReward,
		)
		if err != nil {
			return err
		}
		s.currentStakers.LoadValidator(staker)

		s.validatorState.LoadValidatorMetadata(staker.NodeID, staker.ChainID, metadata)
	}

	delegatorIt := s.currentDelegatorList.NewIterator()
	defer delegatorIt.Release()

	chainDelegatorIt := s.currentNetDelegatorList.NewIterator()
	defer chainDelegatorIt.Release()

	for _, delegatorIt := range []database.Iterator{delegatorIt, chainDelegatorIt} {
		for delegatorIt.Next() {
			txIDBytes := delegatorIt.Key()
			txID, err := ids.ToID(txIDBytes)
			if err != nil {
				return err
			}
			tx, _, err := s.GetTx(txID)
			if err != nil {
				return err
			}

			stakerTx, ok := tx.Unsigned.(txs.Staker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			metadataBytes := delegatorIt.Value()
			metadata := &delegatorMetadata{
				txID: txID,
			}
			if scheduledStakerTx, ok := tx.Unsigned.(txs.ScheduledStaker); ok {
				// Populate [StakerStartTime] using the tx as a default in the
				// event it was not stored in the
				// database.
				metadata.StakerStartTime = uint64(scheduledStakerTx.StartTime().Unix())
			}
			err = parseDelegatorMetadata(metadataBytes, metadata)
			if err != nil {
				return err
			}

			staker, err := NewCurrentStaker(
				txID,
				stakerTx,
				time.Unix(int64(metadata.StakerStartTime), 0),
				metadata.PotentialReward,
			)
			if err != nil {
				return err
			}

			s.currentStakers.LoadDelegator(staker)
		}
	}

	return errors.Join(
		validatorIt.Error(),
		chainValidatorIt.Error(),
		delegatorIt.Error(),
		chainDelegatorIt.Error(),
	)
}

func (s *state) loadPendingValidators() error {
	s.pendingStakers = newBaseStakers()

	validatorIt := s.pendingValidatorList.NewIterator()
	defer validatorIt.Release()

	chainValidatorIt := s.pendingNetValidatorList.NewIterator()
	defer chainValidatorIt.Release()

	for _, validatorIt := range []database.Iterator{validatorIt, chainValidatorIt} {
		for validatorIt.Next() {
			txIDBytes := validatorIt.Key()
			txID, err := ids.ToID(txIDBytes)
			if err != nil {
				return err
			}
			tx, _, err := s.GetTx(txID)
			if err != nil {
				return err
			}

			stakerTx, ok := tx.Unsigned.(txs.ScheduledStaker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			staker, err := NewPendingStaker(txID, stakerTx)
			if err != nil {
				return err
			}

			s.pendingStakers.LoadValidator(staker)
		}
	}

	delegatorIt := s.pendingDelegatorList.NewIterator()
	defer delegatorIt.Release()

	chainDelegatorIt := s.pendingNetDelegatorList.NewIterator()
	defer chainDelegatorIt.Release()

	for _, delegatorIt := range []database.Iterator{delegatorIt, chainDelegatorIt} {
		for delegatorIt.Next() {
			txIDBytes := delegatorIt.Key()
			txID, err := ids.ToID(txIDBytes)
			if err != nil {
				return err
			}
			tx, _, err := s.GetTx(txID)
			if err != nil {
				return err
			}

			stakerTx, ok := tx.Unsigned.(txs.ScheduledStaker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			staker, err := NewPendingStaker(txID, stakerTx)
			if err != nil {
				return err
			}

			s.pendingStakers.LoadDelegator(staker)
		}
	}

	return errors.Join(
		validatorIt.Error(),
		chainValidatorIt.Error(),
		delegatorIt.Error(),
		chainDelegatorIt.Error(),
	)
}

// Invariant: initValidatorSets requires loadActiveL1Validators and
// loadCurrentValidators to have already been called.
func (s *state) initValidatorSets() error {
	log.Info("initValidatorSets: starting",
		"numNets", s.validators.NumNets(),
		"currentStakersChains", len(s.currentStakers.validators),
	)

	// Always populate validators - don't skip even if already populated.
	// The validators manager may have entries without BLS keys if pre-populated
	// by the network layer. We need to ensure validators have proper BLS keys
	// for Warp messaging and consensus.
	if s.validators.NumNets() != 0 {
		log.Info("initValidatorSets: validator manager not empty, will update with BLS keys",
			"numNets", s.validators.NumNets(),
		)
	}

	// Load active LP-77 validators
	if err := s.activeL1Validators.addStakersToValidatorManager(s.validators); err != nil {
		return err
	}
	log.Info("initValidatorSets: after L1 validators", "numNets", s.validators.NumNets())

	// Load inactive LP-77 validators
	//
	// Inactive validators must be loaded individually with their ValidationID
	// as TxID, not aggregated with ids.Empty.
	inactiveIt := s.inactiveDB.NewIterator()
	defer inactiveIt.Release()

	for inactiveIt.Next() {
		validationIDBytes := inactiveIt.Key()
		validationID, err := ids.ToID(validationIDBytes)
		if err != nil {
			return fmt.Errorf("failed to parse validation ID: %w", err)
		}

		var l1Validator L1Validator
		if err := parseL1Validator(inactiveIt.Value(), &l1Validator); err != nil {
			return fmt.Errorf("failed to unmarshal inactive L1 validator: %w", err)
		}
		l1Validator.ValidationID = validationID

		// Add inactive validator to validator manager using addL1ValidatorToValidatorManager
		// which properly handles effectiveNodeID, effectivePublicKey, and effectiveValidationID
		if err := addL1ValidatorToValidatorManager(s.validators, l1Validator); err != nil {
			return fmt.Errorf("failed to add inactive L1 validator: %w", err)
		}
	}

	// Load primary network and non-LP77 validators
	primaryNetworkValidators := s.currentStakers.validators[constants.PrimaryNetworkID]
	log.Info("initValidatorSets: loading primary network validators",
		"primaryNetworkID", constants.PrimaryNetworkID,
		"primaryNetworkValidatorCount", len(primaryNetworkValidators),
		"totalChains", len(s.currentStakers.validators),
	)
	for chainID, chainValidators := range s.currentStakers.validators {
		log.Info("initValidatorSets: processing chain",
			"chainID", chainID,
			"validatorCount", len(chainValidators),
		)
		for nodeID, chainValidator := range chainValidators {
			// The chain validator's Public Key is inherited from the
			// corresponding primary network validator.
			primaryValidator, ok := primaryNetworkValidators[nodeID]
			if !ok {
				return fmt.Errorf("%w: %s", errMissingPrimaryNetworkValidator, nodeID)
			}

			var (
				primaryStaker = primaryValidator.validator
				chainStaker   = chainValidator.validator
			)
			// An entry here is not proof of a validator: DeleteValidator nils
			// the validator and pruneValidatorLocked keeps the entry while
			// delegators remain. Both stakers are dereferenced immediately
			// below, and this runs at startup, so a placeholder is a boot panic.
			if chainStaker == nil {
				// The delegators outlived the validator they backed; there is
				// no staker to register their weight against.
				continue
			}
			if primaryStaker == nil {
				// No primary validator left to inherit a key from. GetValidator
				// already reads a nil validator as absent, so this takes the
				// same answer as the missing case above — registering the
				// staker keyless would strand its weight in the denominator.
				return fmt.Errorf("%w: %s", errMissingPrimaryNetworkValidator, nodeID)
			}
			if err := s.validators.AddStaker(chainID, nodeID, bls.PublicKeyToUncompressedBytes(primaryStaker.PublicKey), chainStaker.TxID, chainStaker.Weight); err != nil {
				return err
			}
			log.Info("initValidatorSets: added validator",
				"chainID", chainID,
				"nodeID", nodeID,
				"weight", chainStaker.Weight,
			)

			delegatorIterator := iterator.FromTree(chainValidator.delegators)
			for delegatorIterator.Next() {
				delegatorStaker := delegatorIterator.Value()
				if err := s.validators.AddWeight(chainID, nodeID, delegatorStaker.Weight); err != nil {
					delegatorIterator.Release()
					return err
				}
			}
			delegatorIterator.Release()
		}
	}

	s.metrics.SetLocalStake(s.validators.GetWeight(constants.PrimaryNetworkID, s.rt.NodeID))
	totalWeight, err := s.validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("failed to get total weight of primary network validators: %w", err)
	}
	s.metrics.SetTotalStake(totalWeight)

	// Log final state
	numValidators := s.validators.NumValidators(constants.PrimaryNetworkID)
	log.Info("initValidatorSets: complete",
		"primaryNetworkID", constants.PrimaryNetworkID,
		"numValidators", numValidators,
		"totalWeight", totalWeight,
		"numNets", s.validators.NumNets(),
	)

	return nil
}

func (s *state) writeCurrentStakers() error {
	for chainID, validatorDiffs := range s.currentStakers.validatorDiffs {
		// Select db to write to
		validatorDB := s.currentNetValidatorList
		delegatorDB := s.currentNetDelegatorList
		if chainID == constants.PrimaryNetworkID {
			validatorDB = s.currentValidatorList
			delegatorDB = s.currentDelegatorList
		}

		// Record the change in weight and/or public key for each validator.
		for nodeID, validatorDiff := range validatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				staker := validatorDiff.validator

				// The validator is being added.
				//
				// Invariant: It's impossible for a delegator to have been rewarded
				// in the same block that the validator was added.
				startTime := uint64(staker.StartTime.Unix())
				metadata := &validatorMetadata{
					txID:        staker.TxID,
					lastUpdated: staker.StartTime,

					UpDuration:               0,
					LastUpdated:              startTime,
					StakerStartTime:          startTime,
					PotentialReward:          staker.PotentialReward,
					PotentialDelegateeReward: 0,
				}

				metadataBytes, err := marshalValidatorMetadata(metadata)
				if err != nil {
					return fmt.Errorf("failed to serialize current validator: %w", err)
				}

				if err = validatorDB.Put(staker.TxID[:], metadataBytes); err != nil {
					return fmt.Errorf("failed to write current validator to list: %w", err)
				}

				s.validatorState.LoadValidatorMetadata(nodeID, chainID, metadata)
			case deleted:
				if err := validatorDB.Delete(validatorDiff.validator.TxID[:]); err != nil {
					return fmt.Errorf("failed to delete current staker: %w", err)
				}

				s.validatorState.DeleteValidatorMetadata(nodeID, chainID)
			}

			err := writeCurrentDelegatorDiff(
				delegatorDB,
				validatorDiff,
			)
			if err != nil {
				return err
			}
		}
	}
	clear(s.currentStakers.validatorDiffs)
	return nil
}

func writeCurrentDelegatorDiff(
	currentDelegatorList linkeddb.LinkedDB,
	validatorDiff *diffValidator,
) error {
	addedDelegatorIterator := iterator.FromTree(validatorDiff.addedDelegators)
	defer addedDelegatorIterator.Release()

	for addedDelegatorIterator.Next() {
		staker := addedDelegatorIterator.Value()

		metadata := &delegatorMetadata{
			txID:            staker.TxID,
			PotentialReward: staker.PotentialReward,
			StakerStartTime: uint64(staker.StartTime.Unix()),
		}
		if err := writeDelegatorMetadata(currentDelegatorList, metadata); err != nil {
			return fmt.Errorf("failed to write current delegator to list: %w", err)
		}
	}

	for _, staker := range validatorDiff.deletedDelegators {
		if err := currentDelegatorList.Delete(staker.TxID[:]); err != nil {
			return fmt.Errorf("failed to delete current staker: %w", err)
		}
	}
	return nil
}

func (s *state) writePendingStakers() error {
	for netID, chainValidatorDiffs := range s.pendingStakers.validatorDiffs {
		delete(s.pendingStakers.validatorDiffs, netID)

		validatorDB := s.pendingNetValidatorList
		delegatorDB := s.pendingNetDelegatorList
		if netID == constants.PrimaryNetworkID {
			validatorDB = s.pendingValidatorList
			delegatorDB = s.pendingDelegatorList
		}

		for _, validatorDiff := range chainValidatorDiffs {
			err := writePendingDiff(
				validatorDB,
				delegatorDB,
				validatorDiff,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func writePendingDiff(
	pendingValidatorList linkeddb.LinkedDB,
	pendingDelegatorList linkeddb.LinkedDB,
	validatorDiff *diffValidator,
) error {
	switch validatorDiff.validatorStatus {
	case added:
		err := pendingValidatorList.Put(validatorDiff.validator.TxID[:], nil)
		if err != nil {
			return fmt.Errorf("failed to add pending validator: %w", err)
		}
	case deleted:
		err := pendingValidatorList.Delete(validatorDiff.validator.TxID[:])
		if err != nil {
			return fmt.Errorf("failed to delete pending validator: %w", err)
		}
	}

	addedDelegatorIterator := iterator.FromTree(validatorDiff.addedDelegators)
	defer addedDelegatorIterator.Release()
	for addedDelegatorIterator.Next() {
		staker := addedDelegatorIterator.Value()

		if err := pendingDelegatorList.Put(staker.TxID[:], nil); err != nil {
			return fmt.Errorf("failed to write pending delegator to list: %w", err)
		}
	}

	for _, staker := range validatorDiff.deletedDelegators {
		if err := pendingDelegatorList.Delete(staker.TxID[:]); err != nil {
			return fmt.Errorf("failed to delete pending delegator: %w", err)
		}
	}
	return nil
}

func (s *state) writeL1Validators() error {
	// Write modified weights
	for chainID, weight := range s.l1ValidatorsDiff.modifiedTotalWeight {
		var err error
		if weight == 0 {
			err = s.weightsDB.Delete(chainID[:])
		} else {
			err = database.PutUInt64(s.weightsDB, chainID[:], weight)
		}
		if err != nil {
			return err
		}

		s.weightsCache.Put(chainID, weight)
	}

	// The L1 validator diff application is split into two loops to ensure that all
	// deletions to the chainIDNodeIDDB happen prior to any additions.
	// Otherwise replacing an L1 validator by deleting it and then re-adding it with a
	// different validationID could result in an inconsistent state.
	for validationID, l1Validator := range s.l1ValidatorsDiff.modified {
		// Delete the prior validator if it exists
		var err error
		if s.activeL1Validators.delete(validationID) {
			err = deleteL1Validator(s.activeDB, emptyL1ValidatorCache, validationID)
		} else {
			err = deleteL1Validator(s.inactiveDB, s.inactiveCache, validationID)
		}
		if err != nil {
			return err
		}

		if !l1Validator.isDeleted() {
			continue
		}

		var (
			chainIDNodeID = chainIDNodeID{
				chainID: l1Validator.ChainID,
				nodeID:  l1Validator.NodeID,
			}
			chainIDNodeIDKey = chainIDNodeID.Marshal()
		)
		if err := s.chainIDNodeIDDB.Delete(chainIDNodeIDKey); err != nil {
			return err
		}

		s.chainIDNodeIDCache.Put(chainIDNodeID, false)
	}

	for validationID, l1Validator := range s.l1ValidatorsDiff.modified {
		if l1Validator.isDeleted() {
			continue
		}

		// Update the chainIDNodeID mapping
		var (
			chainIDNodeID = chainIDNodeID{
				chainID: l1Validator.ChainID,
				nodeID:  l1Validator.NodeID,
			}
			chainIDNodeIDKey = chainIDNodeID.Marshal()
		)
		if err := s.chainIDNodeIDDB.Put(chainIDNodeIDKey, validationID[:]); err != nil {
			return err
		}

		s.chainIDNodeIDCache.Put(chainIDNodeID, true)

		// Add the new validator
		var err error
		if l1Validator.IsActive() {
			s.activeL1Validators.put(l1Validator)
			err = putL1Validator(s.activeDB, emptyL1ValidatorCache, l1Validator)
		} else {
			err = putL1Validator(s.inactiveDB, s.inactiveCache, l1Validator)
		}
		if err != nil {
			return err
		}
	}

	s.l1ValidatorsDiffLock.Lock()
	s.l1ValidatorsDiff = newL1ValidatorsDiff()
	s.l1ValidatorsDiffLock.Unlock()
	return nil
}
