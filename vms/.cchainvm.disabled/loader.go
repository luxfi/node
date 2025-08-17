package cchainvm

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/rlp"
)

// LoadSubnetEVMDatabase directly copies all blocks from SubnetEVM database
func LoadSubnetEVMDatabase(targetDB ethdb.Database) error {
	fmt.Println("Loading SubnetEVM database...")

	// Hardcoded path to SubnetEVM database
	subnetDBPath := "/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"

	// Open SubnetEVM database
	opts := &pebble.Options{
		ReadOnly: true,
	}

	sourceDB, err := pebble.Open(subnetDBPath, opts)
	if err != nil {
		return fmt.Errorf("failed to open SubnetEVM database: %w", err)
	}
	defer sourceDB.Close()

	// Subnet-EVM namespace
	namespace := []byte{
		0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
		0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
		0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
		0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
	}

	blocksLoaded := 0
	highestBlock := uint64(0)

	fmt.Println("Database has NO canonical H keys - iterating over headers directly...")

	// Create iterator to scan all keys
	iter, err := sourceDB.NewIter(nil)
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Map to store blocks by number
	blocksByNumber := make(map[uint64]*types.Header)

	totalKeys := 0
	headerKeys := 0
	validHeaders := 0

	for iter.First(); iter.Valid(); iter.Next() {
		totalKeys++
		if totalKeys%1000000 == 0 {
			fmt.Printf("Scanned %d keys, found %d headers...\n", totalKeys, validHeaders)
		}
		key := iter.Key()

		// Look for header keys: namespace (32 bytes) + hash (32 bytes)
		if len(key) == 64 && bytes.Equal(key[:32], namespace) {
			headerKeys++
			hash := key[32:]

			// Check if the hash encodes block number in first 3 bytes
			blockNum := uint64(hash[0])<<16 | uint64(hash[1])<<8 | uint64(hash[2])

			// Skip if block number seems invalid
			if blockNum > 2000000 {
				continue
			}

			value := iter.Value()

			// Try to decode ANY value as RLP header (not just >100 bytes)
			if len(value) > 0 {
				var header types.Header
				if err := rlp.DecodeBytes(value, &header); err != nil {
					if validHeaders == 0 && headerKeys < 10 {
						fmt.Printf("RLP decode error for block %d: %v\n", blockNum, err)
					}
					continue
				}

				// Use the actual block number from the header
				actualBlockNum := header.Number.Uint64()

				var h common.Hash
				copy(h[:], hash)

				// Store the header
				blocksByNumber[actualBlockNum] = &header

				if actualBlockNum > highestBlock {
					highestBlock = actualBlockNum
				}

				validHeaders++
				if validHeaders <= 10 || validHeaders%100000 == 0 {
					fmt.Printf("Found block %d at hash %x\n", actualBlockNum, hash[:8])
				}
				blocksLoaded++
			}
		}

	}

	fmt.Printf("Scan complete: %d total keys, %d header keys, %d valid headers\n", totalKeys, headerKeys, validHeaders)
	fmt.Printf("Writing %d blocks to target database...\n", len(blocksByNumber))

	// Write all blocks to target database
	written := 0
	var latestHash common.Hash

	for blockNum, header := range blocksByNumber {
		// Calculate the hash
		hash := header.Hash()

		// Write canonical hash mapping
		rawdb.WriteCanonicalHash(targetDB, hash, blockNum)

		// Write the header
		rawdb.WriteHeader(targetDB, header)

		// Track the latest block
		if blockNum == highestBlock {
			latestHash = hash
		}

		written++
		if written <= 10 || written%100000 == 0 {
			fmt.Printf("Written block %d to target database\n", blockNum)
		}
	}

	// Set head pointers to highest block
	if highestBlock > 0 && latestHash != (common.Hash{}) {
		rawdb.WriteHeadBlockHash(targetDB, latestHash)
		rawdb.WriteHeadHeaderHash(targetDB, latestHash)
		rawdb.WriteHeadFastBlockHash(targetDB, latestHash)
		rawdb.WriteLastPivotNumber(targetDB, highestBlock)

		fmt.Printf("Set head to block %d with hash %x\n", highestBlock, latestHash)
	}

	fmt.Printf("✅ Successfully loaded %d blocks from SubnetEVM database\n", blocksLoaded)
	fmt.Printf("Highest block: %d\n", highestBlock)

	return nil
}
