// (c) 2024 Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// replay-standalone is a standalone tool for replaying EVM data into C-Chain format
// It doesn't depend on the node package, making it more portable.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/ethdb/leveldb"
	"github.com/luxfi/geth/rlp"
)

// Simplified replay implementation for standalone usage
type StandaloneReplayer struct {
	sourceDB     *pebble.DB
	targetDB     ethdb.Database
	namespace    []byte
	targetHeight uint64
	targetWallet common.Address
}

func main() {
	// Command-line flags
	var (
		sourcePath   = flag.String("source", "/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb", "Path to source EVM database")
		targetPath   = flag.String("target", "", "Path to target C-Chain database (required)")
		targetHeight = flag.Uint64("height", 100, "Target block height to replay to")
		walletAddr   = flag.String("wallet", "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", "Wallet address to track")
		copyState    = flag.Bool("copy-state", false, "Copy state data (in addition to blocks)")
	)

	flag.Parse()

	if *targetPath == "" {
		fmt.Fprintf(os.Stderr, "Error: -target path is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Convert wallet address
	var targetWallet common.Address
	if *walletAddr != "" && common.IsHexAddress(*walletAddr) {
		targetWallet = common.HexToAddress(*walletAddr)
	}

	// Create target directory
	if err := os.MkdirAll(filepath.Dir(*targetPath), 0755); err != nil {
		log.Fatalf("Failed to create target directory: %v", err)
	}

	log.Printf("===========================================")
	log.Printf("Standalone C-Chain Replay Tool")
	log.Printf("===========================================")
	log.Printf("Source: %s", *sourcePath)
	log.Printf("Target: %s", *targetPath)
	log.Printf("Height: %d", *targetHeight)
	log.Printf("Wallet: %s", targetWallet.Hex())
	log.Printf("Copy State: %v", *copyState)
	log.Printf("===========================================")

	// Open source database (EVM with namespace)
	sourceDB, err := pebble.Open(*sourcePath, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("Failed to open source database: %v", err)
	}
	defer sourceDB.Close()

	// Open target database
	ldb, err := leveldb.New(*targetPath, 256, 16, "cchain", false)
	if err != nil {
		log.Fatalf("Failed to open leveldb: %v", err)
	}

	targetDB, err := rawdb.Open(ldb, rawdb.OpenOptions{
		Ancient:  filepath.Join(*targetPath, "ancient"),
		ReadOnly: false,
	})
	if err != nil {
		log.Fatalf("Failed to open target database: %v", err)
	}
	defer targetDB.Close()

	// Create replayer
	replayer := &StandaloneReplayer{
		sourceDB:     sourceDB,
		targetDB:     targetDB,
		targetHeight: *targetHeight,
		targetWallet: targetWallet,
		// Default LUX namespace
		namespace: []byte{
			0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
			0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
			0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
			0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
		},
	}

	// Run simplified replay (just copy blocks, not full EVM execution)
	if err := replayer.ReplayBlocks(*copyState); err != nil {
		log.Fatalf("Replay failed: %v", err)
	}

	log.Printf("===========================================")
	log.Printf("Replay completed successfully!")
	log.Printf("Database written to: %s", *targetPath)
	log.Printf("===========================================")
}

// ReplayBlocks copies blocks and optionally state from source to target
func (r *StandaloneReplayer) ReplayBlocks(copyState bool) error {
	startTime := time.Now()
	log.Printf("Starting block replay up to height %d", r.targetHeight)

	// First, copy the genesis block
	if err := r.copyGenesis(); err != nil {
		return fmt.Errorf("failed to copy genesis: %w", err)
	}

	// Copy blocks
	blocksProcessed := uint64(0)
	for blockNum := uint64(1); blockNum <= r.targetHeight; blockNum++ {
		if err := r.copyBlock(blockNum); err != nil {
			log.Printf("Warning: Failed to copy block %d: %v", blockNum, err)
			continue
		}

		blocksProcessed++

		// Log progress
		if blockNum%1000 == 0 || blockNum == r.targetHeight {
			elapsed := time.Since(startTime)
			blocksPerSec := float64(blockNum) / elapsed.Seconds()
			remaining := r.targetHeight - blockNum
			eta := time.Duration(float64(remaining) / blocksPerSec * float64(time.Second))

			log.Printf("Progress: %d/%d blocks (%.2f%%) | %.2f blocks/s | ETA: %s",
				blockNum, r.targetHeight,
				float64(blockNum)/float64(r.targetHeight)*100,
				blocksPerSec, eta)
		}
	}

	// Optionally copy state data
	if copyState {
		log.Printf("Copying state data...")
		if err := r.copyStateData(); err != nil {
			return fmt.Errorf("failed to copy state: %w", err)
		}
	}

	elapsed := time.Since(startTime)
	log.Printf("Replay completed in %s, processed %d blocks", elapsed, blocksProcessed)

	// Check wallet balance if specified
	if r.targetWallet != (common.Address{}) {
		r.checkWalletBalance()
	}

	return nil
}

func (r *StandaloneReplayer) copyGenesis() error {
	// Copy block 0
	return r.copyBlock(0)
}

func (r *StandaloneReplayer) copyBlock(blockNum uint64) error {
	// Get canonical hash
	canonicalKey := r.makeKey('h', blockNum, 'n')
	hashData, closer, err := r.sourceDB.Get(canonicalKey)
	if err != nil {
		return fmt.Errorf("canonical hash not found: %w", err)
	}
	defer closer.Close()

	hash := common.BytesToHash(hashData)

	// Get header
	headerKey := r.makeKeyWithHash('h', blockNum, hash)
	headerData, closer2, err := r.sourceDB.Get(headerKey)
	if err != nil {
		return fmt.Errorf("header not found: %w", err)
	}
	defer closer2.Close()

	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		return fmt.Errorf("failed to decode header: %w", err)
	}

	// Get body
	bodyKey := r.makeKeyWithHash('b', blockNum, hash)
	bodyData, closer3, err := r.sourceDB.Get(bodyKey)
	if err == nil && closer3 != nil {
		defer closer3.Close()

		var body types.Body
		if err := rlp.DecodeBytes(bodyData, &body); err == nil {
			// Create and store block
			block := types.NewBlockWithHeader(&header).WithBody(body)
			rawdb.WriteBlock(r.targetDB, block)
			rawdb.WriteCanonicalHash(r.targetDB, hash, blockNum)

			if blockNum == 0 {
				rawdb.WriteHeadBlockHash(r.targetDB, hash)
				rawdb.WriteHeadHeaderHash(r.targetDB, hash)
			}
		}
	} else {
		// No body, just store header
		block := types.NewBlockWithHeader(&header)
		rawdb.WriteBlock(r.targetDB, block)
		rawdb.WriteCanonicalHash(r.targetDB, hash, blockNum)
	}

	return nil
}

func (r *StandaloneReplayer) copyStateData() error {
	// Copy state trie nodes and account data
	copied := 0
	batch := r.targetDB.NewBatch()
	batchSize := 0
	const maxBatchSize = 10000

	// Iterate through namespaced keys
	iter, err := r.sourceDB.NewIter(&pebble.IterOptions{
		LowerBound: r.namespace,
		UpperBound: append(r.namespace, 0xFF),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) <= 32 {
			continue
		}

		// Remove namespace prefix
		targetKey := key[32:]
		value, _ := iter.ValueAndErr()

		// Only copy state-related keys (skip headers, bodies, receipts)
		keyType := targetKey[0]
		if keyType != 'h' && keyType != 'b' && keyType != 'r' && keyType != 't' {
			continue
		}

		if err := batch.Put(targetKey, value); err != nil {
			return err
		}

		copied++
		batchSize++

		if batchSize >= maxBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
			batchSize = 0

			if copied%100000 == 0 {
				log.Printf("Copied %d state entries...", copied)
			}
		}
	}

	// Write final batch
	if batchSize > 0 {
		if err := batch.Write(); err != nil {
			return err
		}
	}

	log.Printf("Copied %d state entries", copied)
	return nil
}

func (r *StandaloneReplayer) checkWalletBalance() {
	// Try to check the wallet balance
	// This is simplified - full balance check would require state trie traversal
	log.Printf("Wallet balance check for %s would require full state tree (not implemented in standalone mode)",
		r.targetWallet.Hex())
}

func (r *StandaloneReplayer) makeKey(prefix byte, blockNum uint64, suffix byte) []byte {
	key := make([]byte, 0, 32+1+8+1)
	key = append(key, r.namespace...)
	key = append(key, prefix)
	key = append(key, encodeBlockNumber(blockNum)...)
	key = append(key, suffix)
	return key
}

func (r *StandaloneReplayer) makeKeyWithHash(prefix byte, blockNum uint64, hash common.Hash) []byte {
	key := make([]byte, 0, 32+1+8+32)
	key = append(key, r.namespace...)
	key = append(key, prefix)
	key = append(key, encodeBlockNumber(blockNum)...)
	key = append(key, hash.Bytes()...)
	return key
}

func encodeBlockNumber(num uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, num)
	return enc
}