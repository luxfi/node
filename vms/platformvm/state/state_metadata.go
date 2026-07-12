// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/gas"
)

func (s *state) GetTimestamp() time.Time {
	return s.timestamp
}

func (s *state) SetTimestamp(tm time.Time) {
	s.timestamp = tm
}

func (s *state) GetFeeState() gas.State {
	return s.feeState
}

func (s *state) SetFeeState(feeState gas.State) {
	s.feeState = feeState
}

func (s *state) GetL1ValidatorExcess() gas.Gas {
	return s.l1ValidatorExcess
}

func (s *state) SetL1ValidatorExcess(e gas.Gas) {
	s.l1ValidatorExcess = e
}

func (s *state) GetAccruedFees() uint64 {
	return s.accruedFees
}

func (s *state) SetAccruedFees(accruedFees uint64) {
	s.accruedFees = accruedFees
}

func (s *state) GetLastAccepted() ids.ID {
	s.lastAcceptedLock.RLock()
	defer s.lastAcceptedLock.RUnlock()
	return s.lastAccepted
}

func (s *state) SetLastAccepted(lastAccepted ids.ID) {
	s.lastAcceptedLock.Lock()
	defer s.lastAcceptedLock.Unlock()
	s.lastAccepted = lastAccepted
}

func (s *state) GetCurrentSupply(netID ids.ID) (uint64, error) {
	if netID == constants.PrimaryNetworkID {
		return s.currentSupply, nil
	}

	supply, ok := s.modifiedSupplies[netID]
	if ok {
		return supply, nil
	}

	cachedSupply, ok := s.supplyCache.Get(netID)
	if ok {
		if cachedSupply == nil {
			return 0, database.ErrNotFound
		}
		return *cachedSupply, nil
	}

	supply, err := database.GetUInt64(s.supplyDB, netID[:])
	if err == database.ErrNotFound {
		s.supplyCache.Put(netID, nil)
		return 0, database.ErrNotFound
	}
	if err != nil {
		return 0, err
	}

	s.supplyCache.Put(netID, &supply)
	return supply, nil
}

func (s *state) SetCurrentSupply(netID ids.ID, cs uint64) {
	if netID == constants.PrimaryNetworkID {
		s.currentSupply = cs
	} else {
		s.modifiedSupplies[netID] = cs
	}
}

func (s *state) SetHeight(height uint64) {
	if s.indexedHeights == nil {
		// If indexedHeights hasn't been created yet, then we are newly tracking
		// the range. This means we should initialize the LowerBound to the
		// current height.
		s.indexedHeights = &heightRange{
			LowerBound: height,
		}
	}

	s.indexedHeights.UpperBound = height
	s.currentHeight = height
}

func (s *state) loadMetadata() error {
	timestamp, err := database.GetTimestamp(s.singletonDB, TimestampKey)
	if err != nil {
		return fmt.Errorf("loadMetadata: TimestampKey: %w", err)
	}
	s.persistedTimestamp = timestamp
	s.SetTimestamp(timestamp)

	feeState, err := getFeeState(s.singletonDB)
	if err != nil {
		return fmt.Errorf("loadMetadata: feeState: %w", err)
	}
	s.persistedFeeState = feeState
	s.SetFeeState(feeState)

	l1ValidatorExcess, err := database.GetUInt64(s.singletonDB, L1ValidatorExcessKey)
	if err != nil {
		if err != database.ErrNotFound {
			return fmt.Errorf("loadMetadata: L1ValidatorExcess: %w", err)
		}
		l1ValidatorExcess = 0
	}
	s.persistedL1ValidatorExcess = gas.Gas(l1ValidatorExcess)
	s.SetL1ValidatorExcess(gas.Gas(l1ValidatorExcess))

	accruedFees, err := database.GetUInt64(s.singletonDB, AccruedFeesKey)
	if err != nil {
		if err != database.ErrNotFound {
			return fmt.Errorf("loadMetadata: AccruedFees: %w", err)
		}
		accruedFees = 0
	}
	s.persistedAccruedFees = accruedFees
	s.SetAccruedFees(accruedFees)

	currentSupply, err := database.GetUInt64(s.singletonDB, CurrentSupplyKey)
	if err != nil {
		return fmt.Errorf("loadMetadata: CurrentSupply: %w", err)
	}
	s.persistedCurrentSupply = currentSupply
	s.SetCurrentSupply(constants.PrimaryNetworkID, currentSupply)

	lastAccepted, err := database.GetID(s.singletonDB, LastAcceptedKey)
	if err != nil {
		return err
	}
	s.persistedLastAccepted = lastAccepted
	s.lastAcceptedLock.Lock()
	s.lastAccepted = lastAccepted
	s.lastAcceptedLock.Unlock()

	// Lookup the most recently indexed range on disk. If we haven't started
	// indexing the weights, then we keep the indexed heights as nil.
	indexedHeightsBytes, err := s.singletonDB.Get(HeightsIndexedKey)
	if err == database.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	indexedHeights := &heightRange{}
	if err := parseHeightRange(indexedHeightsBytes, indexedHeights); err != nil {
		return err
	}

	// If the indexed range is not up to date, then we will act as if the range
	// doesn't exist.
	lastAcceptedBlock, err := s.GetStatelessBlock(lastAccepted)
	if err != nil {
		return err
	}
	if indexedHeights.UpperBound != lastAcceptedBlock.Height() {
		return nil
	}
	s.indexedHeights = indexedHeights
	return nil
}

func (s *state) writeMetadata() error {
	if !s.persistedTimestamp.Equal(s.timestamp) {
		if err := database.PutTimestamp(s.singletonDB, TimestampKey, s.timestamp); err != nil {
			return fmt.Errorf("failed to write timestamp: %w", err)
		}
		s.persistedTimestamp = s.timestamp
	}
	if s.feeState != s.persistedFeeState {
		if err := putFeeState(s.singletonDB, s.feeState); err != nil {
			return fmt.Errorf("failed to write fee state: %w", err)
		}
		s.persistedFeeState = s.feeState
	}
	if s.l1ValidatorExcess != s.persistedL1ValidatorExcess {
		if err := database.PutUInt64(s.singletonDB, L1ValidatorExcessKey, uint64(s.l1ValidatorExcess)); err != nil {
			return fmt.Errorf("failed to write l1Validator excess: %w", err)
		}
		s.persistedL1ValidatorExcess = s.l1ValidatorExcess
	}
	if s.accruedFees != s.persistedAccruedFees {
		if err := database.PutUInt64(s.singletonDB, AccruedFeesKey, s.accruedFees); err != nil {
			return fmt.Errorf("failed to write accrued fees: %w", err)
		}
		s.persistedAccruedFees = s.accruedFees
	}
	if s.persistedCurrentSupply != s.currentSupply {
		if err := database.PutUInt64(s.singletonDB, CurrentSupplyKey, s.currentSupply); err != nil {
			return fmt.Errorf("failed to write current supply: %w", err)
		}
		s.persistedCurrentSupply = s.currentSupply
	}
	s.lastAcceptedLock.RLock()
	lastAcceptedChanged := s.persistedLastAccepted != s.lastAccepted
	currentLastAccepted := s.lastAccepted
	s.lastAcceptedLock.RUnlock()
	if lastAcceptedChanged {
		if err := database.PutID(s.singletonDB, LastAcceptedKey, currentLastAccepted); err != nil {
			return fmt.Errorf("failed to write last accepted: %w", err)
		}
		s.persistedLastAccepted = currentLastAccepted
	}
	if s.indexedHeights != nil {
		indexedHeightsBytes, err := marshalHeightRange(s.indexedHeights)
		if err != nil {
			return err
		}
		if err := s.singletonDB.Put(HeightsIndexedKey, indexedHeightsBytes); err != nil {
			return fmt.Errorf("failed to write indexed range: %w", err)
		}
	}
	return nil
}

func markInitialized(db database.KeyValueWriter) error {
	return db.Put(InitializedKey, nil)
}

func isInitialized(db database.KeyValueReader) (bool, error) {
	return db.Has(InitializedKey)
}

func putFeeState(db database.KeyValueWriter, feeState gas.State) error {
	feeStateBytes, err := marshalFeeState(feeState)
	if err != nil {
		return err
	}
	return db.Put(FeeStateKey, feeStateBytes)
}

func getFeeState(db database.KeyValueReader) (gas.State, error) {
	feeStateBytes, err := db.Get(FeeStateKey)
	if err == database.ErrNotFound {
		return gas.State{}, nil
	}
	if err != nil {
		return gas.State{}, err
	}

	feeState, err := parseFeeState(feeStateBytes)
	if err != nil {
		return gas.State{}, err
	}
	return feeState, nil
}
