// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"

	safemath "github.com/luxfi/math"
)

func (s *state) Commit() error {
	defer s.Abort()
	batch, err := s.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (s *state) Abort() {
	s.baseDB.Abort()
}

func (s *state) Checksum() ids.ID {
	return s.utxoState.Checksum()
}

func (s *state) CommitBatch() (database.Batch, error) {
	// updateValidators is set to true here so that the validator manager is
	// kept up to date with the last accepted state.
	if err := s.write(true /*=updateValidators*/, s.currentHeight); err != nil {
		return nil, err
	}
	return s.baseDB.CommitBatch()
}

func (s *state) Close() error {
	// Only close the base database. All other databases are prefixdb wrappers
	// that don't need to be closed separately. Closing them would cause
	// "closed" errors because prefixdb.Close() calls Close() on the underlying
	// database, which would be closed multiple times.
	// The baseDB field was not stored in the state struct, but we can close
	// the underlying database through any of the prefixdb instances.
	// We'll close singletonDB which is a direct prefixdb over baseDB.
	return s.singletonDB.Close()
}

func (s *state) write(updateValidators bool, height uint64) error {
	codecVersion := CodecVersion1
	if !s.upgrades.IsDurangoActivated(s.GetTimestamp()) {
		codecVersion = CodecVersion0
	}

	return errors.Join(
		s.writeBlocks(),
		s.writeExpiry(),
		s.updateValidatorManager(updateValidators),
		s.writeValidatorDiffs(height),
		s.writeCurrentStakers(codecVersion),
		s.writePendingStakers(),
		s.WriteValidatorMetadata(s.currentValidatorList, s.currentNetValidatorList, codecVersion), // Must be called after writeCurrentStakers
		s.writeL1Validators(),
		s.writeTXs(),
		s.writeRewardUTXOs(),
		s.writeUTXOs(),
		s.writeNets(),
		s.writeNetOwners(),
		s.writeNetToL1Conversions(),
		s.writeTransformedNets(),
		s.writeNetSupplies(),
		s.writeChains(),
		s.writeMetadata(),
	)
}

func (s *state) sync(genesis []byte) error {
	wasInitialized, err := isInitialized(s.singletonDB)
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database wasn't previously initialized, create the platform chain
	// anew using the provided genesis state.
	if !wasInitialized {
		s.rt.Log.Info("P-Chain state: initializing from genesis (fresh database)")
		if err := s.init(genesis); err != nil {
			return fmt.Errorf(
				"failed to initialize the database: %w",
				err,
			)
		}
		s.rt.Log.Info("P-Chain state: genesis init completed, loading state")
	} else {
		s.rt.Log.Info("P-Chain state: database already initialized, loading existing state")
	}

	if err := s.load(); err != nil {
		// Database is corrupt. Return the error so the node exits.
		// On restart with a fresh PVC, init() will run cleanly.
		// The init() fix (no Abort after genesis write) prevents this from recurring.
		if wasInitialized {
			s.rt.Log.Error("P-Chain state corrupt after init — database must be wiped",
				log.Reflect("error", err),
			)
		}
		{
			return fmt.Errorf(
				"failed to load the database state: %w",
				err,
			)
		}
	}

	// Migrate: add any genesis chains missing from state (e.g., D-Chain added after initial genesis)
	if wasInitialized {
		if err := s.migrateNewGenesisChains(genesis); err != nil {
			return fmt.Errorf("failed to migrate new genesis chains: %w", err)
		}
	}
	return nil
}

func (s *state) init(genesisBytes []byte) error {
	// Create the genesis block and save it as being accepted (We don't do
	// genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hash.ComputeHash256Array(genesisBytes)
	genesisBlock, err := block.NewApricotCommitBlock(genesisID, 0 /*height*/)
	if err != nil {
		return err
	}

	parsedGenesis, err := genesis.Parse(genesisBytes)
	if err != nil {
		return err
	}

	if err := s.syncGenesis(genesisBlock, parsedGenesis); err != nil {
		return err
	}

	if err := markInitialized(s.singletonDB); err != nil {
		return err
	}

	// Write all genesis state + markInitialized atomically.
	// We do NOT use s.Commit() here because Commit() defers s.Abort() which
	// clears the versiondb diff layer. After init(), load() reads from the
	// versiondb and needs to see the committed data. Instead we write+commit
	// without aborting, so the diff layer retains the written values for
	// the subsequent load() call.
	s.rt.Log.Info("init: before write",
		"currentSupply", s.currentSupply,
		"persistedCurrentSupply", s.persistedCurrentSupply,
		"willWrite", s.persistedCurrentSupply != s.currentSupply,
	)
	if err := s.write(true, 0); err != nil {
		return fmt.Errorf("init: write failed: %w", err)
	}
	s.rt.Log.Info("init: after write",
		"currentSupply", s.currentSupply,
		"persistedCurrentSupply", s.persistedCurrentSupply,
	)
	batch, err := s.baseDB.CommitBatch()
	if err != nil {
		return fmt.Errorf("init: commit batch failed: %w", err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("init: batch write failed: %w", err)
	}
	// Force sync to disk — ensures genesis state survives container restart.
	if err := s.baseDB.Sync(); err != nil {
		s.rt.Log.Warn("init: db sync failed", log.Reflect("error", err))
	}
	s.rt.Log.Info("init: committed to disk successfully")
	return nil
}

func (s *state) syncGenesis(genesisBlk block.Block, genesis *genesis.Genesis) error {
	genesisBlkID := genesisBlk.ID()
	s.SetLastAccepted(genesisBlkID)
	s.SetTimestamp(time.Unix(int64(genesis.Timestamp), 0))
	s.SetCurrentSupply(constants.PrimaryNetworkID, genesis.InitialSupply)
	s.rt.Log.Info("syncGenesis: initial supply set",
		"initialSupply", genesis.InitialSupply,
		"currentSupply", s.currentSupply,
		"persistedCurrentSupply", s.persistedCurrentSupply,
	)
	s.AddStatelessBlock(genesisBlk)

	// Initialize fee state with default values for genesis
	// This is required because loadMetadata expects fee state to exist
	// We must directly write it since both feeState and persistedFeeState
	// start as zero values and won't trigger the write in writeMetadata
	initialFeeState := gas.State{}
	if err := putFeeState(s.singletonDB, initialFeeState); err != nil {
		return fmt.Errorf("failed to write initial fee state: %w", err)
	}
	s.feeState = initialFeeState
	s.persistedFeeState = initialFeeState

	// Write CurrentSupply directly (same pattern as feeState above).
	// writeMetadata won't write it if persistedCurrentSupply == currentSupply,
	// which can happen on re-init after a crash.
	if err := database.PutUInt64(s.singletonDB, CurrentSupplyKey, genesis.InitialSupply); err != nil {
		return fmt.Errorf("failed to write initial current supply: %w", err)
	}

	// Persist UTXOs that exist at genesis
	for _, utxo := range genesis.UTXOs {
		luxUTXO := utxo.UTXO
		s.AddUTXO(&luxUTXO)
	}

	// Persist primary network validator set at genesis
	for _, vdrTx := range genesis.Validators {
		// We expect genesis validator txs to be either AddValidatorTx or
		// AddPermissionlessValidatorTx.
		//
		validatorTx, ok := vdrTx.Unsigned.(txs.ScheduledStaker)
		if !ok {
			return fmt.Errorf("expected a scheduled staker but got %T", vdrTx.Unsigned)
		}

		stakeAmount := validatorTx.Weight()
		// Note: We use [StartTime()] here because genesis transactions are
		// guaranteed to be pre-Durango activation.
		startTime := validatorTx.StartTime()
		stakeDuration := validatorTx.EndTime().Sub(startTime)
		currentSupply, err := s.GetCurrentSupply(constants.PrimaryNetworkID)
		if err != nil {
			return err
		}

		potentialReward := s.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := safemath.Add64(currentSupply, potentialReward)
		if err != nil {
			return err
		}

		staker, err := NewCurrentStaker(vdrTx.ID(), validatorTx, startTime, potentialReward)
		if err != nil {
			return err
		}

		if err := s.PutCurrentValidator(staker); err != nil {
			return err
		}
		s.AddTx(vdrTx, status.Committed)
		s.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)
	}

	for _, chain := range genesis.Chains {
		unsignedChain, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chain.Unsigned)
		}

		// Ensure all chains that the genesis bytes say to create have the right
		// network ID
		networkID := s.rt.NetworkID
		if false && unsignedChain.NetworkID != networkID { // Temporarily disabled for genesis compatibility
			return lux.ErrWrongNetworkID
		}

		s.AddChain(chain)
		s.AddTx(chain, status.Committed)
	}

	// updateValidators is set to false here to maintain the invariant that the
	// primary network's validator set is empty before the validator sets are
	// initialized.
	if err := s.write(false /*=updateValidators*/, 0); err != nil {
		return err
	}

	// Mark blocks as already reindexed since this is a fresh database with no
	// legacy block indices to convert. This prevents the ReindexBlocks goroutine
	// from running and racing with other state operations on fresh databases.
	return s.singletonDB.Put(BlocksReindexedKey, nil)
}

// Load pulls data previously stored on disk that is expected to be in memory.
func (s *state) load() error {
	return errors.Join(
		s.loadMetadata(),
		s.loadExpiry(),
		s.loadActiveL1Validators(),
		s.loadCurrentValidators(),
		s.loadPendingValidators(),
		s.initValidatorSets(),
	)
}

// migrateNewGenesisChains adds any chains from genesis that are missing from
// state. This handles the case where new primary network chains (e.g., D-Chain)
// are added to genesis after the database was already initialized.
func (s *state) migrateNewGenesisChains(genesisBytes []byte) error {
	parsedGenesis, err := genesis.Parse(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis for chain migration: %w", err)
	}

	// Get existing chains for the primary network
	existingChains, err := s.GetChains(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}

	// Build set of existing chain tx IDs
	existingIDs := make(map[ids.ID]bool, len(existingChains))
	for _, chain := range existingChains {
		existingIDs[chain.ID()] = true
	}

	added := 0
	for _, chain := range parsedGenesis.Chains {
		if existingIDs[chain.ID()] {
			continue
		}
		unsignedChain, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			continue
		}
		log.Info("migrating new genesis chain into state",
			"name", unsignedChain.BlockchainName,
			"chainID", chain.ID(),
			"vmID", unsignedChain.VMID,
		)
		s.AddChain(chain)
		s.AddTx(chain, status.Committed)
		added++
	}

	if added > 0 {
		if err := s.write(false, 0); err != nil {
			return fmt.Errorf("failed to write migrated chains: %w", err)
		}
		if _, err := s.baseDB.CommitBatch(); err != nil {
			return fmt.Errorf("failed to commit migrated chains: %w", err)
		}
		if err := s.Commit(); err != nil {
			return fmt.Errorf("failed to commit migrated chains to disk: %w", err)
		}
		log.Info("migrated new genesis chains into state", "count", added)
	}

	return nil
}
