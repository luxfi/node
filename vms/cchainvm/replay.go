package cchainvm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/luxfi/geth/common"
	gethcore "github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/log"
	"github.com/luxfi/geth/rlp"
)

// ReplayConfig holds configuration for block replay
type ReplayConfig struct {
	GenesisDB  string // Path to database containing blocks to replay
	StartBlock uint64 // Block number to start replay from
	EndBlock   uint64 // Block number to end replay at (0 = replay all)
	BatchSize  uint64 // Number of blocks to replay in each batch
}

// BlockReplayer handles replaying blocks from an external database
type BlockReplayer struct {
	config     *ReplayConfig
	blockchain *gethcore.BlockChain
	db         *pebble.DB
}

// NewBlockReplayer creates a new block replayer
func NewBlockReplayer(config *ReplayConfig, blockchain *gethcore.BlockChain) (*BlockReplayer, error) {
	if config.GenesisDB == "" {
		return nil, fmt.Errorf("genesis database path is required")
	}

	db, err := pebble.Open(config.GenesisDB, &pebble.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open genesis database: %v", err)
	}

	return &BlockReplayer{
		config:     config,
		blockchain: blockchain,
		db:         db,
	}, nil
}

// Close closes the replayer
func (r *BlockReplayer) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// Replay starts the block replay process
func (r *BlockReplayer) Replay() error {
	log.Info("Starting block replay", "start", r.config.StartBlock, "end", r.config.EndBlock)

	// Find the highest block if end is not specified
	if r.config.EndBlock == 0 {
		highest, err := r.findHighestBlock()
		if err != nil {
			return fmt.Errorf("failed to find highest block: %v", err)
		}
		r.config.EndBlock = highest
		log.Info("Found highest block", "block", highest)
	}

	// Get current chain height
	currentHeight := r.blockchain.CurrentBlock().Number.Uint64()
	log.Info("Current chain height", "height", currentHeight)

	// Start replay from the appropriate block
	startBlock := r.config.StartBlock
	if startBlock <= currentHeight {
		startBlock = currentHeight + 1
		log.Info("Adjusting start block", "newStart", startBlock)
	}

	// Replay in batches
	batchSize := r.config.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}

	totalReplayed := uint64(0)
	totalErrors := uint64(0)
	startTime := time.Now()

	for blockNum := startBlock; blockNum <= r.config.EndBlock; blockNum += batchSize {
		endBatch := blockNum + batchSize - 1
		if endBatch > r.config.EndBlock {
			endBatch = r.config.EndBlock
		}

		replayed, errors := r.replayBatch(blockNum, endBatch)
		totalReplayed += replayed
		totalErrors += errors

		// Log progress
		if blockNum%1000 == 0 || endBatch == r.config.EndBlock {
			elapsed := time.Since(startTime)
			blocksPerSec := float64(totalReplayed) / elapsed.Seconds()
			log.Info("Replay progress",
				"current", blockNum,
				"total", r.config.EndBlock,
				"replayed", totalReplayed,
				"errors", totalErrors,
				"speed", fmt.Sprintf("%.2f blocks/sec", blocksPerSec),
			)
		}

		// Rate limiting to avoid overwhelming the system
		if blockNum%100 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	log.Info("Block replay complete",
		"replayed", totalReplayed,
		"errors", totalErrors,
		"duration", time.Since(startTime),
	)

	return nil
}

// replayBatch replays a batch of blocks
func (r *BlockReplayer) replayBatch(start, end uint64) (replayed, errors uint64) {
	for blockNum := start; blockNum <= end; blockNum++ {
		// Check if block already exists
		if r.blockchain.GetBlockByNumber(blockNum) != nil {
			replayed++
			continue
		}

		// Get block from genesis database
		block, err := r.getBlock(blockNum)
		if err != nil {
			if blockNum > start {
				// We've reached the end of available blocks
				break
			}
			log.Error("Failed to get block", "number", blockNum, "error", err)
			errors++
			continue
		}

		// Insert block into blockchain
		if err := r.insertBlock(block); err != nil {
			log.Error("Failed to insert block", "number", blockNum, "error", err)
			errors++
			continue
		}

		replayed++
	}

	return replayed, errors
}

// getBlock retrieves a block from the genesis database
func (r *BlockReplayer) getBlock(blockNum uint64) (*types.Block, error) {
	// Get canonical hash
	canonicalKey := make([]byte, 9)
	canonicalKey[0] = 'H'
	binary.BigEndian.PutUint64(canonicalKey[1:], blockNum)

	hashBytes, closer, err := r.db.Get(canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("canonical hash not found: %v", err)
	}
	closer.Close()

	var blockHash common.Hash
	copy(blockHash[:], hashBytes)

	// Get header
	headerKey := append([]byte("h"), make([]byte, 8)...)
	binary.BigEndian.PutUint64(headerKey[1:], blockNum)
	headerKey = append(headerKey, blockHash[:]...)

	headerData, closer, err := r.db.Get(headerKey)
	if err != nil {
		// Try without number prefix
		headerKey = append([]byte("h"), blockHash[:]...)
		headerData, closer, err = r.db.Get(headerKey)
		if err != nil {
			return nil, fmt.Errorf("header not found: %v", err)
		}
	}
	closer.Close()

	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		return nil, fmt.Errorf("failed to decode header: %v", err)
	}

	// Get body
	bodyKey := append([]byte("b"), make([]byte, 8)...)
	binary.BigEndian.PutUint64(bodyKey[1:], blockNum)
	bodyKey = append(bodyKey, blockHash[:]...)

	bodyData, closer, err := r.db.Get(bodyKey)
	if err != nil {
		// Try without number prefix
		bodyKey = append([]byte("b"), blockHash[:]...)
		bodyData, closer, err = r.db.Get(bodyKey)
		if err != nil {
			// Empty body is valid
			return types.NewBlockWithHeader(&header), nil
		}
	}
	closer.Close()

	var body types.Body
	if err := rlp.DecodeBytes(bodyData, &body); err != nil {
		return nil, fmt.Errorf("failed to decode body: %v", err)
	}

	return types.NewBlockWithHeader(&header).WithBody(types.Body{
		Transactions: body.Transactions,
		Uncles:       body.Uncles,
	}), nil
}

// insertBlock inserts a block into the blockchain
func (r *BlockReplayer) insertBlock(block *types.Block) error {
	// Validate block
	if err := r.validateBlock(block); err != nil {
		return fmt.Errorf("block validation failed: %v", err)
	}

	// Insert through blockchain processor
	_, err := r.blockchain.InsertChain([]*types.Block{block})
	return err
}

// validateBlock performs basic validation on a block
func (r *BlockReplayer) validateBlock(block *types.Block) error {
	// Check block number sequence
	currentBlock := r.blockchain.CurrentBlock()
	if block.NumberU64() != currentBlock.Number.Uint64()+1 {
		return fmt.Errorf("invalid block number: expected %d, got %d",
			currentBlock.Number.Uint64()+1, block.NumberU64())
	}

	// Check parent hash
	if block.ParentHash() != currentBlock.Hash() {
		return fmt.Errorf("invalid parent hash: expected %s, got %s",
			currentBlock.Hash().Hex(), block.ParentHash().Hex())
	}

	return nil
}

// findHighestBlock finds the highest block number in the genesis database
func (r *BlockReplayer) findHighestBlock() (uint64, error) {
	// Binary search for the highest block
	low := uint64(0)
	high := uint64(10000000) // Start with 10M as upper bound
	highest := uint64(0)

	for low <= high {
		mid := (low + high) / 2

		canonicalKey := make([]byte, 9)
		canonicalKey[0] = 'H'
		binary.BigEndian.PutUint64(canonicalKey[1:], mid)

		_, closer, err := r.db.Get(canonicalKey)
		if err == nil {
			closer.Close()
			highest = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	return highest, nil
}