// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// Runtime Replay Service - Replays historic blocks in parallel with live C-Chain

package cchainvm

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/pebble"
	"github.com/luxfi/geth/rlp"
)

// ReplayService replays historic blocks from old subnet EVM in parallel with live C-Chain
// Integrates with QuasarCore for quantum finality of both old and new blocks
type ReplayService struct {
	mu sync.RWMutex

	// Historic database (old subnet EVM)
	historicDB ethdb.Database
	historicPath string

	// Current replay position
	currentBlock uint64
	endBlock     uint64

	// Statistics
	blocksReplayed uint64
	txsReplayed    uint64
	gasReplayed    uint64
	startTime      time.Time

	// Quasar integration callback
	onBlockReplayed func(block *ReplayedBlock)

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	active atomic.Bool

	// Configuration
	namespace []byte // Subnet EVM namespace prefix to strip
	batchSize uint64  // Blocks to replay per batch
	delay     time.Duration // Delay between batches to avoid overwhelming Quasar
}

// ReplayedBlock represents a historic block being replayed
type ReplayedBlock struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	Timestamp    uint64
	GasUsed      uint64
	Transactions uint64
	Data         []byte
	Header       *types.Header
	IsHistoric   bool // Distinguishes replayed blocks from live blocks
}

// ReplayConfig configures the replay service
type ReplayConfig struct {
	HistoricDBPath string        // Path to old subnet EVM PebbleDB
	StartBlock     uint64        // Block to start replay from
	EndBlock       uint64        // Block to end replay at (0 = latest)
	BatchSize      uint64        // Blocks per batch (default: 100)
	Delay          time.Duration // Delay between batches (default: 100ms)
	Namespace      []byte        // Namespace prefix to strip
}

// NewReplayService creates a new runtime replay service
func NewReplayService(config ReplayConfig) (*ReplayService, error) {
	// Open historic database (read-only)
	historicDB, err := pebble.New(config.HistoricDBPath, 0, 0, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed to open historic database: %w", err)
	}

	// Default namespace (subnet-evm)
	namespace := config.Namespace
	if len(namespace) == 0 {
		namespace = common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	}

	// Default batch size
	batchSize := config.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}

	// Default delay
	delay := config.Delay
	if delay == 0 {
		delay = 100 * time.Millisecond
	}

	return &ReplayService{
		historicDB:   historicDB,
		historicPath: config.HistoricDBPath,
		currentBlock: config.StartBlock,
		endBlock:     config.EndBlock,
		namespace:    namespace,
		batchSize:    batchSize,
		delay:        delay,
		startTime:    time.Now(),
	}, nil
}

// Start begins replaying historic blocks in parallel with live chain
func (rs *ReplayService) Start(ctx context.Context, onBlockReplayed func(*ReplayedBlock)) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.active.Load() {
		return fmt.Errorf("replay service already running")
	}

	rs.ctx, rs.cancel = context.WithCancel(ctx)
	rs.onBlockReplayed = onBlockReplayed
	rs.active.Store(true)

	go rs.replayLoop()

	fmt.Printf("[REPLAY] Historic replay started from block %d\n", rs.currentBlock)
	fmt.Printf("[REPLAY] Source: %s\n", rs.historicPath)
	fmt.Printf("[REPLAY] Replaying %d blocks per batch with %s delay\n", rs.batchSize, rs.delay)

	return nil
}

// Stop halts the replay service
func (rs *ReplayService) Stop() error {
	if !rs.active.Load() {
		return nil
	}

	rs.cancel()
	rs.active.Store(false)

	rs.mu.Lock()
	if rs.historicDB != nil {
		rs.historicDB.Close()
	}
	rs.mu.Unlock()

	elapsed := time.Since(rs.startTime)
	fmt.Printf("[REPLAY] Stopped after replaying %d blocks in %s\n",
		rs.blocksReplayed, elapsed.Round(time.Second))

	return nil
}

// replayLoop continuously replays historic blocks
func (rs *ReplayService) replayLoop() {
	ticker := time.NewTicker(rs.delay)
	defer ticker.Stop()

	for {
		select {
		case <-rs.ctx.Done():
			return

		case <-ticker.C:
			if err := rs.replayBatch(); err != nil {
				if err == errReplayComplete {
					fmt.Printf("[REPLAY] Historic replay complete! Replayed %d blocks\n", rs.blocksReplayed)
					rs.active.Store(false)
					return
				}
				fmt.Printf("[REPLAY] Error replaying batch: %v\n", err)
			}
		}
	}
}

var errReplayComplete = fmt.Errorf("replay complete")

// replayBatch replays a batch of historic blocks
func (rs *ReplayService) replayBatch() error {
	rs.mu.Lock()
	startBlock := rs.currentBlock
	endBlock := startBlock + rs.batchSize
	if rs.endBlock > 0 && endBlock > rs.endBlock {
		endBlock = rs.endBlock
	}
	rs.mu.Unlock()

	if startBlock >= endBlock {
		return errReplayComplete
	}

	blocksInBatch := uint64(0)

	for blockNum := startBlock; blockNum < endBlock; blockNum++ {
		block, err := rs.loadHistoricBlock(blockNum)
		if err != nil {
			// Block not found, might have reached end of historic chain
			if blockNum == startBlock {
				return errReplayComplete
			}
			continue
		}

		// Send to Quasar for quantum finality
		if rs.onBlockReplayed != nil {
			rs.onBlockReplayed(block)
		}

		atomic.AddUint64(&rs.blocksReplayed, 1)
		atomic.AddUint64(&rs.txsReplayed, block.Transactions)
		atomic.AddUint64(&rs.gasReplayed, block.GasUsed)
		blocksInBatch++
	}

	rs.mu.Lock()
	rs.currentBlock = endBlock
	rs.mu.Unlock()

	// Print progress
	if blocksInBatch > 0 {
		elapsed := time.Since(rs.startTime)
		blocksPerSec := float64(rs.blocksReplayed) / elapsed.Seconds()
		fmt.Printf("[REPLAY] Block %d: %d blocks replayed, %d txs, %.0f blocks/sec\n",
			endBlock-1, rs.blocksReplayed, rs.txsReplayed, blocksPerSec)
	}

	return nil
}

// loadHistoricBlock loads a block from the historic database
func (rs *ReplayService) loadHistoricBlock(number uint64) (*ReplayedBlock, error) {
	// Get canonical hash for this block number
	canonicalKey := rs.canonicalHashKey(number)
	hashBytes, err := rs.historicDB.Get(canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("canonical hash not found for block %d", number)
	}

	blockHash := common.BytesToHash(hashBytes)

	// Load block header
	headerKey := rs.headerKey(number, blockHash)
	headerData, err := rs.historicDB.Get(headerKey)
	if err != nil {
		return nil, fmt.Errorf("header not found for block %d", number)
	}

	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	// Estimate transaction count from gas (approximate)
	txCount := header.GasUsed / 21000
	if header.GasUsed > 0 && txCount == 0 {
		txCount = 1
	}

	return &ReplayedBlock{
		Number:       number,
		Hash:         blockHash,
		ParentHash:   header.ParentHash,
		Timestamp:    header.Time,
		GasUsed:      header.GasUsed,
		Transactions: txCount,
		Data:         headerData,
		Header:       &header,
		IsHistoric:   true,
	}, nil
}

// Key format helpers

func (rs *ReplayService) headerKey(number uint64, hash common.Hash) []byte {
	key := append([]byte("h"), rs.encodeBlockNumber(number)...)
	key = append(key, hash.Bytes()...)

	// Add namespace prefix if present
	if len(rs.namespace) > 0 {
		return append(rs.namespace, key...)
	}
	return key
}

func (rs *ReplayService) canonicalHashKey(number uint64) []byte {
	key := append(append([]byte("h"), rs.encodeBlockNumber(number)...), 'n')

	// Add namespace prefix if present
	if len(rs.namespace) > 0 {
		return append(rs.namespace, key...)
	}
	return key
}

func (rs *ReplayService) encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// GetStats returns current replay statistics
func (rs *ReplayService) GetStats() (blocks, txs, gas uint64, blocksPerSec float64) {
	blocks = atomic.LoadUint64(&rs.blocksReplayed)
	txs = atomic.LoadUint64(&rs.txsReplayed)
	gas = atomic.LoadUint64(&rs.gasReplayed)

	elapsed := time.Since(rs.startTime)
	if elapsed.Seconds() > 0 {
		blocksPerSec = float64(blocks) / elapsed.Seconds()
	}

	return
}

// IsActive returns whether replay is currently active
func (rs *ReplayService) IsActive() bool {
	return rs.active.Load()
}

// GetCurrentBlock returns the current replay position
func (rs *ReplayService) GetCurrentBlock() uint64 {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.currentBlock
}
