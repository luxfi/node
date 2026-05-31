// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/timer"
)

func (s *state) AddStatelessBlock(block block.Block) {
	blkID := block.ID()
	s.addedBlockIDs[block.Height()] = blkID
	s.addedBlocks[blkID] = block
}

func (s *state) GetStatelessBlock(blockID ids.ID) (block.Block, error) {
	if blk, exists := s.addedBlocks[blockID]; exists {
		return blk, nil
	}
	if blk, cached := s.blockCache.Get(blockID); cached {
		if blk == nil {
			return nil, database.ErrNotFound
		}
		return blk, nil
	}

	blkBytes, err := s.blockDB.Get(blockID[:])
	if err == database.ErrNotFound {
		s.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	blk, _, err := parseStoredBlock(blkBytes)
	if err != nil {
		return nil, err
	}

	s.blockCache.Put(blockID, blk)
	return blk, nil
}

func (s *state) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, exists := s.addedBlockIDs[height]; exists {
		return blkID, nil
	}
	if blkID, cached := s.blockIDCache.Get(height); cached {
		if blkID == ids.Empty {
			return ids.Empty, database.ErrNotFound
		}
		return blkID, nil
	}

	blkID, err := database.GetID(s.blockIDDB, database.PackUInt64(height))
	if err == database.ErrNotFound {
		s.blockIDCache.Put(height, ids.Empty)
		return ids.Empty, database.ErrNotFound
	}
	if err != nil {
		return ids.Empty, err
	}

	s.blockIDCache.Put(height, blkID)
	return blkID, nil
}

func (s *state) writeBlocks() error {
	for blkID, blk := range s.addedBlocks {
		blkBytes := blk.Bytes()
		blkHeight := blk.Height()
		heightKey := database.PackUInt64(blkHeight)

		delete(s.addedBlockIDs, blkHeight)
		s.blockIDCache.Put(blkHeight, blkID)
		if err := database.PutID(s.blockIDDB, heightKey, blkID); err != nil {
			return fmt.Errorf("failed to add blockID: %w", err)
		}

		delete(s.addedBlocks, blkID)
		// Note: Evict is used rather than Put here because blk may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.blockCache.Evict(blkID)
		if err := s.blockDB.Put(blkID[:], blkBytes); err != nil {
			return fmt.Errorf("failed to write block %s: %w", blkID, err)
		}
	}
	return nil
}

// parseStoredBlock returns the block and whether it is a legacy [stateBlk].
// Invariant: blkBytes is safe to parse with blocks.GenesisCodec.
// Retained for backward compatibility with pre-v1.14.x databases.
func parseStoredBlock(blkBytes []byte) (block.Block, bool, error) {
	// Attempt to parse as blocks.Block
	blk, err := block.Parse(block.GenesisCodec, blkBytes)
	if err == nil {
		return blk, false, nil
	}

	// Fallback to [stateBlk] using our legacy codec
	blkState := stateBlk{}
	if _, err := multiVersionUnmarshal(block.GenesisCodec, blkBytes, &blkState); err != nil {
		// If we can't unmarshal as stateBlk, this might not be a block at all
		// (could be an index entry or other data in the blockDB)
		// Return the original parse error
		return nil, false, err
	}

	blk, err = block.Parse(block.GenesisCodec, blkState.Bytes)
	return blk, true, err
}

func (s *state) ReindexBlocks(lock sync.Locker, log log.Logger) error {
	has, err := s.singletonDB.Has(BlocksReindexedKey)
	if err != nil {
		return err
	}
	if has {
		log.Info("blocks already reindexed")
		return nil
	}

	// It is possible that new blocks are added after grabbing this iterator.
	// New blocks are guaranteed to be persisted in the new format, so we don't
	// need to check them.
	blockIterator := s.blockDB.NewIterator()
	// Releasing is done using a closure to ensure that updating blockIterator
	// will result in having the most recent iterator released when executing
	// the deferred function.
	defer func() {
		blockIterator.Release()
	}()

	log.Info("starting block reindexing")

	var (
		startTime         = time.Now()
		lastCommit        = startTime
		nextUpdate        = startTime.Add(indexLogFrequency)
		numIndicesChecked = 0
		numIndicesUpdated = 0
	)

	for blockIterator.Next() {
		keyBytes := blockIterator.Key()
		valueBytes := blockIterator.Value()

		// Skip entries that are not 32 bytes (not block IDs)
		if len(keyBytes) != ids.IDLen {
			continue
		}

		blk, isStateBlk, err := parseStoredBlock(valueBytes)
		if err != nil {
			// Skip entries that can't be parsed as blocks
			// This could be metadata or other non-block data
			continue
		}

		blkID := blk.ID()

		// This block was previously stored using the legacy format, update the
		// index to remove the usage of stateBlk.
		if isStateBlk {
			blkBytes := blk.Bytes()
			if err := s.blockDB.Put(blkID[:], blkBytes); err != nil {
				return fmt.Errorf("failed to write block: %w", err)
			}

			numIndicesUpdated++
		}

		numIndicesChecked++

		now := time.Now()
		if now.After(nextUpdate) {
			nextUpdate = now.Add(indexLogFrequency)

			progress := timer.ProgressFromHash(blkID[:])
			eta := timer.EstimateETA(
				startTime,
				progress,
				math.MaxUint64,
			)

			log.Info("reindexing blocks",
				"numIndicesUpdated", numIndicesUpdated,
				"numIndicesChecked", numIndicesChecked,
				"eta", eta,
			)
		}

		if numIndicesChecked%indexIterationLimit == 0 {
			// We must hold the lock during committing to make sure we don't
			// attempt to commit to disk while a block is concurrently being
			// accepted.
			lock.Lock()
			err := errors.Join(
				s.Commit(),
				blockIterator.Error(),
			)
			lock.Unlock()
			if err != nil {
				return err
			}

			// We release the iterator here to allow the underlying database to
			// clean up deleted state.
			blockIterator.Release()

			// We take the minimum here because it's possible that the node is
			// currently bootstrapping. This would mean that grabbing the lock
			// could take an extremely long period of time; which we should not
			// delay processing for.
			indexDuration := now.Sub(lastCommit)
			sleepDuration := min(
				indexIterationSleepMultiplier*indexDuration,
				indexIterationSleepCap,
			)
			time.Sleep(sleepDuration)

			// Make sure not to include the sleep duration into the next index
			// duration.
			lastCommit = time.Now()

			blockIterator = s.blockDB.NewIteratorWithStart(blkID[:])
		}
	}

	// Ensure we fully iterated over all blocks before writing that indexing has
	// finished.
	//
	// Note: This is needed because a transient read error could cause the
	// iterator to stop early.
	if err := blockIterator.Error(); err != nil {
		return fmt.Errorf("failed to iterate over historical blocks: %w", err)
	}

	if err := s.singletonDB.Put(BlocksReindexedKey, nil); err != nil {
		return fmt.Errorf("failed to put marked blocks as reindexed: %w", err)
	}

	// We must hold the lock during committing to make sure we don't attempt to
	// commit to disk while a block is concurrently being accepted.
	lock.Lock()
	defer lock.Unlock()

	log.Info("finished block reindexing",
		"numIndicesUpdated", numIndicesUpdated,
		"numIndicesChecked", numIndicesChecked,
		"duration", time.Since(startTime),
	)

	return s.Commit()
}
