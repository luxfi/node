// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"strings"

	"github.com/luxfi/database"
	"github.com/luxfi/database/linkeddb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/txs"
)

func (s *state) GetChainIDs() ([]ids.ID, error) {
	if s.cachedChainIDs != nil {
		return s.cachedChainIDs, nil
	}

	chainDBIt := s.chainDB.NewIterator()
	defer chainDBIt.Release()

	chainIDs := []ids.ID{}
	for chainDBIt.Next() {
		chainIDBytes := chainDBIt.Key()
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			return nil, err
		}
		chainIDs = append(chainIDs, chainID)
	}
	if err := chainDBIt.Error(); err != nil {
		return nil, err
	}
	chainIDs = append(chainIDs, s.addedChainIDs...)
	s.cachedChainIDs = chainIDs
	return chainIDs, nil
}

func (s *state) AddNet(chainID ids.ID) {
	s.addedChainIDs = append(s.addedChainIDs, chainID)
	if s.cachedChainIDs != nil {
		s.cachedChainIDs = append(s.cachedChainIDs, chainID)
	}
}

func (s *state) GetNetOwner(netID ids.ID) (fx.Owner, error) {
	if owner, exists := s.chainOwners[netID]; exists {
		return owner, nil
	}

	if ownerAndSize, cached := s.chainOwnerCache.Get(netID); cached {
		if ownerAndSize.owner == nil {
			return nil, database.ErrNotFound
		}
		return ownerAndSize.owner, nil
	}

	ownerBytes, err := s.chainOwnerDB.Get(netID[:])
	if err == nil {
		owner, err := parseOwner(ownerBytes)
		if err != nil {
			return nil, err
		}
		s.chainOwnerCache.Put(netID, fxOwnerAndSize{
			owner: owner,
			size:  len(ownerBytes),
		})
		return owner, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	chainIntf, _, err := s.GetTx(netID)
	if err != nil {
		if err == database.ErrNotFound {
			s.chainOwnerCache.Put(netID, fxOwnerAndSize{})
		}
		return nil, err
	}

	network, ok := chainIntf.Unsigned.(*txs.CreateNetworkTx)
	if !ok {
		return nil, fmt.Errorf("%q %w", netID, errIsNotNet)
	}

	owner := network.Owner()
	s.SetNetOwner(netID, owner)
	return owner, nil
}

func (s *state) SetNetOwner(netID ids.ID, owner fx.Owner) {
	s.chainOwners[netID] = owner
}

// GetNetToL1Conversion allows for concurrent reads.
func (s *state) GetNetToL1Conversion(chainID ids.ID) (NetToL1Conversion, error) {
	if c, ok := s.chainToL1Conversions[chainID]; ok {
		return c, nil
	}

	if c, ok := s.chainToL1ConversionCache.Get(chainID); ok {
		return c, nil
	}

	bytes, err := s.chainToL1ConversionDB.Get(chainID[:])
	if err != nil {
		return NetToL1Conversion{}, err
	}

	c, err := parseConversion(bytes)
	if err != nil {
		return NetToL1Conversion{}, err
	}
	s.chainToL1ConversionCache.Put(chainID, c)
	return c, nil
}

func (s *state) SetNetToL1Conversion(chainID ids.ID, c NetToL1Conversion) {
	s.chainToL1Conversions[chainID] = c
}

func (s *state) GetNetTransformation(chainID ids.ID) (*txs.Tx, error) {
	if tx, exists := s.transformedNets[chainID]; exists {
		return tx, nil
	}

	if tx, cached := s.transformedNetCache.Get(chainID); cached {
		if tx == nil {
			return nil, database.ErrNotFound
		}
		return tx, nil
	}

	transformNetTxID, err := database.GetID(s.transformedNetDB, chainID[:])
	if err == database.ErrNotFound {
		s.transformedNetCache.Put(chainID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	transformNetTx, _, err := s.GetTx(transformNetTxID)
	if err != nil {
		return nil, err
	}
	s.transformedNetCache.Put(chainID, transformNetTx)
	return transformNetTx, nil
}

func (s *state) AddNetTransformation(transformNetTxIntf *txs.Tx) {
	transformNetTx := transformNetTxIntf.Unsigned.(*txs.TransformChainTx)
	s.transformedNets[transformNetTx.Chain()] = transformNetTxIntf
}

func (s *state) GetChains(netID ids.ID) ([]*txs.Tx, error) {
	if chains, cached := s.chainCache.Get(netID); cached {
		return chains, nil
	}
	chainDB := s.getChainDB(netID)
	chainDBIt := chainDB.NewIterator()
	defer chainDBIt.Release()

	txs := []*txs.Tx(nil)
	for chainDBIt.Next() {
		chainIDBytes := chainDBIt.Key()
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			return nil, err
		}
		chainTx, _, err := s.GetTx(chainID)
		if err != nil {
			return nil, err
		}
		txs = append(txs, chainTx)
	}
	if err := chainDBIt.Error(); err != nil {
		return nil, err
	}
	txs = append(txs, s.addedChains[netID]...)
	s.chainCache.Put(netID, txs)
	return txs, nil
}

func (s *state) AddChain(createChainTxIntf *txs.Tx) {
	createChainTx := createChainTxIntf.Unsigned.(*txs.CreateChainTx)
	netID := createChainTx.ChainID()
	s.addedChains[netID] = append(s.addedChains[netID], createChainTxIntf)
	if chains, cached := s.chainCache.Get(netID); cached {
		chains = append(chains, createChainTxIntf)
		s.chainCache.Put(netID, chains)
	}

	// Register chain name for uniqueness tracking (case-insensitive)
	if createChainTx.BlockchainName() != "" {
		nameLower := strings.ToLower(createChainTx.BlockchainName())
		chainID := createChainTxIntf.ID()
		s.addedChainNames[nameLower] = chainID
		s.chainNameCache.Put(nameLower, chainID)
	}
}

// GetChainIDByName returns the chain ID for the given chain name (case-insensitive).
// Returns database.ErrNotFound if no chain with the given name exists.
func (s *state) GetChainIDByName(name string) (ids.ID, error) {
	nameLower := strings.ToLower(name)

	// Check in-memory additions first
	if chainID, exists := s.addedChainNames[nameLower]; exists {
		return chainID, nil
	}

	// Check cache
	if chainID, cached := s.chainNameCache.Get(nameLower); cached {
		return chainID, nil
	}

	// Check database
	chainIDBytes, err := s.chainNameDB.Get([]byte(nameLower))
	if err != nil {
		return ids.Empty, err
	}

	chainID, err := ids.ToID(chainIDBytes)
	if err != nil {
		return ids.Empty, err
	}

	s.chainNameCache.Put(nameLower, chainID)
	return chainID, nil
}

// IsChainNameTaken returns true if a chain with the given name already exists (case-insensitive).
func (s *state) IsChainNameTaken(name string) bool {
	_, err := s.GetChainIDByName(name)
	return err == nil
}

func (s *state) getChainDB(netID ids.ID) linkeddb.LinkedDB {
	if chainDB, cached := s.chainDBCache.Get(netID); cached {
		return chainDB
	}
	rawChainDB := prefixdb.New(netID[:], s.chainsDB)
	chainDB := linkeddb.NewDefault(rawChainDB)
	s.chainDBCache.Put(netID, chainDB)
	return chainDB
}

func (s *state) writeNets() error {
	for _, chainID := range s.addedChainIDs {
		if err := s.chainDB.Put(chainID[:], nil); err != nil {
			return fmt.Errorf("failed to write chain: %w", err)
		}
	}
	s.addedChainIDs = nil
	return nil
}

func (s *state) writeNetOwners() error {
	for chainID, owner := range s.chainOwners {
		delete(s.chainOwners, chainID)

		ownerBytes, err := marshalOwner(owner)
		if err != nil {
			return fmt.Errorf("failed to marshal net owner: %w", err)
		}

		s.chainOwnerCache.Put(chainID, fxOwnerAndSize{
			owner: owner,
			size:  len(ownerBytes),
		})

		if err := s.chainOwnerDB.Put(chainID[:], ownerBytes); err != nil {
			return fmt.Errorf("failed to write net owner: %w", err)
		}
	}
	return nil
}

func (s *state) writeNetToL1Conversions() error {
	for chainID, c := range s.chainToL1Conversions {
		delete(s.chainToL1Conversions, chainID)

		bytes, err := marshalConversion(c)
		if err != nil {
			return fmt.Errorf("failed to marshal chain conversion: %w", err)
		}

		s.chainToL1ConversionCache.Put(chainID, c)

		if err := s.chainToL1ConversionDB.Put(chainID[:], bytes); err != nil {
			return fmt.Errorf("failed to write chain conversion: %w", err)
		}
	}
	return nil
}

func (s *state) writeTransformedNets() error {
	for netID, tx := range s.transformedNets {
		txID := tx.ID()

		delete(s.transformedNets, netID)
		// Note: Evict is used rather than Put here because tx may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.transformedNetCache.Evict(netID)
		if err := database.PutID(s.transformedNetDB, netID[:], txID); err != nil {
			return fmt.Errorf("failed to write transformed chain: %w", err)
		}
	}
	return nil
}

func (s *state) writeNetSupplies() error {
	for chainID, supply := range s.modifiedSupplies {
		delete(s.modifiedSupplies, chainID)
		s.supplyCache.Put(chainID, &supply)
		if err := database.PutUInt64(s.supplyDB, chainID[:], supply); err != nil {
			return fmt.Errorf("failed to write chain supply: %w", err)
		}
	}
	return nil
}

func (s *state) writeChains() error {
	for netID, chains := range s.addedChains {
		for _, chain := range chains {
			chainDB := s.getChainDB(netID)

			chainID := chain.ID()
			if err := chainDB.Put(chainID[:], nil); err != nil {
				return fmt.Errorf("failed to write chain: %w", err)
			}
		}
		delete(s.addedChains, netID)
	}

	// Persist chain names for network-wide uniqueness
	for nameLower, chainID := range s.addedChainNames {
		if err := s.chainNameDB.Put([]byte(nameLower), chainID[:]); err != nil {
			return fmt.Errorf("failed to write chain name %s: %w", nameLower, err)
		}
		delete(s.addedChainNames, nameLower)
	}
	return nil
}
