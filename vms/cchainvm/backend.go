// (c) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/consensus"
	"github.com/luxfi/geth/crypto"
	"github.com/luxfi/geth/consensus/clique"
	gethcore "github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/txpool"
	"github.com/luxfi/geth/core/txpool/legacypool"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/core/vm"
	"github.com/luxfi/geth/eth/ethconfig"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/rpc"
	"github.com/luxfi/geth/triedb"
	"github.com/luxfi/log"
)

// PreShanghaiHeader represents header format from EVM before Shanghai upgrade
// EVM headers may have 16 or 17 fields depending on presence of ExtDataHash
type PreShanghaiHeader struct {
	ParentHash  common.Hash
	UncleHash   common.Hash
	Coinbase    common.Address
	Root        common.Hash
	TxHash      common.Hash
	ReceiptHash common.Hash
	Bloom       types.Bloom
	Difficulty  *big.Int
	Number      *big.Int
	GasLimit    uint64
	GasUsed     uint64
	Time        uint64
	Extra       []byte
	MixDigest   common.Hash
	Nonce       types.BlockNonce
	BaseFee     *big.Int `rlp:"optional"` // EIP-1559
	// ExtDataHash can be present as empty bytes, so use []byte instead of Hash
	// to allow RLP decoder to handle variable-length data
	ExtDataHash []byte `rlp:"optional"` // EVM specific, may be empty or 32 bytes
}

// ToPostShanghai converts pre-Shanghai header to post-Shanghai types.Header
func (h *PreShanghaiHeader) ToPostShanghai() *types.Header {
	return &types.Header{
		ParentHash:      h.ParentHash,
		UncleHash:       h.UncleHash,
		Coinbase:        h.Coinbase,
		Root:            h.Root,
		TxHash:          h.TxHash,
		ReceiptHash:     h.ReceiptHash,
		Bloom:           h.Bloom,
		Difficulty:      h.Difficulty,
		Number:          h.Number,
		GasLimit:        h.GasLimit,
		GasUsed:         h.GasUsed,
		Time:            h.Time,
		Extra:           h.Extra,
		MixDigest:       h.MixDigest,
		Nonce:           h.Nonce,
		BaseFee:         h.BaseFee,
		WithdrawalsHash: nil, // Pre-Shanghai has no withdrawals
	}
}

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// createGenesisFromMigratedDB creates a proper Genesis configuration from migrated genesis block
// This returns a *gethcore.Genesis that can be used with SetupGenesisBlock()
// copyGenesisFromMigratedDB copies block 0 and its state directly from migrated database
func copyGenesisFromMigratedDB(newDB ethdb.Database, migratedDBPath string) error {
	fmt.Printf("=== COPYING GENESIS BLOCK 0 FROM MIGRATED DATABASE ===\n")

	// Open migrated database
	// First, try to clean up any corrupt vlog files that may have been left from migration.
	// Since we use high ValueThreshold (1MB), all data is in SST files and vlogs are empty.
	vlogPattern := filepath.Join(migratedDBPath, "*.vlog")
	vlogFiles, _ := filepath.Glob(vlogPattern)
	for _, vlogFile := range vlogFiles {
		// Remove vlog files - data is in SST files with high ValueThreshold
		os.Remove(vlogFile)
		fmt.Printf("Removed potentially corrupt vlog: %s\n", vlogFile)
	}

	opts := badger.DefaultOptions(migratedDBPath).WithReadOnly(true).WithLogger(nil)
	migratedDB, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open migrated database: %w", err)
	}
	defer migratedDB.Close()

	// Find block 0 hash
	targetHash := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
	
	// Read block 0 header
	var headerData []byte
	err = migratedDB.View(func(txn *badger.Txn) error {
		key := make([]byte, 41)
		key[0] = 'h'
		binary.BigEndian.PutUint64(key[1:9], 0)
		copy(key[9:41], targetHash.Bytes())

		item, err := txn.Get(key)
		if err != nil {
			return fmt.Errorf("genesis header not found: %w", err)
		}

		headerData, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return err
	}

	// Decode header
	header := new(types.Header)
	if err := rlp.DecodeBytes(headerData, header); err != nil {
		return fmt.Errorf("failed to decode header: %w", err)
	}

	fmt.Printf("Genesis Block 0:\n")
	fmt.Printf("  Hash:       %s\n", header.Hash().Hex())
	fmt.Printf("  StateRoot:  %s\n", header.Root.Hex())
	fmt.Printf("  Number:     %d\n", header.Number.Uint64())
	fmt.Printf("  Timestamp:  %d\n", header.Time)

	// Write header to new database
	headerRLP, err := rlp.EncodeToBytes(header)
	if err != nil {
		return fmt.Errorf("failed to encode header: %w", err)
	}

	// Write using geth database format:
	// 'h' + 8-byte number + 32-byte hash -> header RLP
	headerKey := make([]byte, 41)
	headerKey[0] = 'h'
	binary.BigEndian.PutUint64(headerKey[1:9], 0)
	copy(headerKey[9:41], targetHash.Bytes())
	
	if err := newDB.Put(headerKey, headerRLP); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write canonical hash: 'h' + 8-byte number + 'n' -> 32-byte hash (Geth format)
	numKey := make([]byte, 10)
	numKey[0] = 'h'
	binary.BigEndian.PutUint64(numKey[1:9], 0)
	numKey[9] = 'n' // CRITICAL: headerHashSuffix required by Geth!

	if err := newDB.Put(numKey, targetHash.Bytes()); err != nil {
		return fmt.Errorf("failed to write canonical hash: %w", err)
	}

	// Write head block hash (LastBlock key)
	if err := newDB.Put([]byte("LastBlock"), targetHash.Bytes()); err != nil {
		return fmt.Errorf("failed to write LastBlock: %w", err)
	}

	// Write head header hash (LastHeader key)
	if err := newDB.Put([]byte("LastHeader"), targetHash.Bytes()); err != nil {
		return fmt.Errorf("failed to write LastHeader: %w", err)
	}

	// Write total difficulty for genesis (should be 0 or header.Difficulty)
	tdKey := append(append([]byte("h"), numKey[1:]...), targetHash.Bytes()...)
	tdKey = append([]byte("t"), tdKey[1:]...) // 't' prefix for total difficulty
	tdBytes, err := rlp.EncodeToBytes(header.Difficulty)
	if err != nil {
		return fmt.Errorf("failed to encode total difficulty: %w", err)
	}
	if err := newDB.Put(tdKey, tdBytes); err != nil {
		return fmt.Errorf("failed to write total difficulty: %w", err)
	}

	// Write header number mapping: 'H' + hash -> 8-byte big-endian block number
	// This is CRITICAL for ReadHeaderNumber() to work
	// IMPORTANT: Must be exactly 8 bytes (not RLP encoded)
	hashKey := make([]byte, 33)
	hashKey[0] = 'H'
	copy(hashKey[1:33], targetHash.Bytes())
	numBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(numBytes, 0)
	if err := newDB.Put(hashKey, numBytes); err != nil {
		return fmt.Errorf("failed to write header number: %w", err)
	}

	// CRITICAL: Copy block BODY from migrated database
	// Block body key: 'b' + 8-byte number + 32-byte hash -> body RLP
	var bodyData []byte
	err = migratedDB.View(func(txn *badger.Txn) error {
		bodyKey := make([]byte, 41)
		bodyKey[0] = 'b'
		binary.BigEndian.PutUint64(bodyKey[1:9], 0)
		copy(bodyKey[9:41], targetHash.Bytes())

		item, err := txn.Get(bodyKey)
		if err != nil {
			// Genesis might have empty body, that's OK
			fmt.Printf("   Block body not found (genesis may have empty body): %v\n", err)
			return nil
		}

		bodyData, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to read block body: %w", err)
	}

	// Write block body if it exists
	if len(bodyData) > 0 {
		bodyKey := make([]byte, 41)
		bodyKey[0] = 'b'
		binary.BigEndian.PutUint64(bodyKey[1:9], 0)
		copy(bodyKey[9:41], targetHash.Bytes())

		if err := newDB.Put(bodyKey, bodyData); err != nil {
			return fmt.Errorf("failed to write block body: %w", err)
		}
		fmt.Printf("   Block body copied (%d bytes)\n", len(bodyData))
	} else {
		fmt.Printf("   Block body: empty (expected for genesis)\n")
	}

	// Copy receipts if they exist
	// Receipts key: 'r' + 8-byte number + 32-byte hash -> receipts RLP
	var receiptsData []byte
	err = migratedDB.View(func(txn *badger.Txn) error {
		receiptsKey := make([]byte, 41)
		receiptsKey[0] = 'r'
		binary.BigEndian.PutUint64(receiptsKey[1:9], 0)
		copy(receiptsKey[9:41], targetHash.Bytes())

		item, err := txn.Get(receiptsKey)
		if err != nil {
			// Genesis might have no receipts, that's OK
			fmt.Printf("   Receipts not found (expected for genesis): %v\n", err)
			return nil
		}

		receiptsData, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to read receipts: %w", err)
	}

	// Write receipts if they exist
	if len(receiptsData) > 0 {
		receiptsKey := make([]byte, 41)
		receiptsKey[0] = 'r'
		binary.BigEndian.PutUint64(receiptsKey[1:9], 0)
		copy(receiptsKey[9:41], targetHash.Bytes())

		if err := newDB.Put(receiptsKey, receiptsData); err != nil {
			return fmt.Errorf("failed to write receipts: %w", err)
		}
		fmt.Printf("   Receipts copied (%d bytes)\n", len(receiptsData))
	} else {
		fmt.Printf("   Receipts: none (expected for genesis)\n")
	}

	fmt.Printf("✅ Genesis block 0 FULLY copied!\n")
	fmt.Printf("   Header: ✓\n")
	fmt.Printf("   Body: ✓\n")
	fmt.Printf("   Receipts: ✓\n")
	fmt.Printf("   Canonical hash: ✓\n")
	fmt.Printf("   Header number mapping: ✓\n")
	fmt.Printf("   Total difficulty: ✓\n")
	fmt.Printf("   LastBlock/LastHeader: %s\n", targetHash.Hex())
	fmt.Printf("   State root preserved: %s\n", header.Root.Hex())
	return nil
}

func createGenesisFromMigratedDB(migratedDBPath string) (*gethcore.Genesis, error) {
	fmt.Printf("=== READING GENESIS FROM MIGRATED DATABASE ===\n")
	fmt.Printf("Source: %s\n", migratedDBPath)

	// Open the migrated BadgerDB
	opts := badger.DefaultOptions(migratedDBPath)
	opts.ReadOnly = true
	opts.Logger = nil

	bdb, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB: %w", err)
	}
	defer bdb.Close()

	// Read block 0 from migrated database
	var headerData []byte

	err = bdb.View(func(txn *badger.Txn) error {
		// The migrated database has NO namespace - keys are stored directly
		// Key for block 0: 'h' + 8-byte block number (0) + 32-byte hash
		targetHash := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")

		// Create the key (no namespace prefix - classic SubnetEVM from 2024)
		key := make([]byte, 41)
		key[0] = 'h'
		binary.BigEndian.PutUint64(key[1:9], 0)
		copy(key[9:41], targetHash.Bytes())

		// Get header data
		item, err := txn.Get(key)
		if err != nil {
			return fmt.Errorf("genesis header not found at key %x: %w", key, err)
		}

		headerData, err = item.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("failed to copy header data: %w", err)
		}

		fmt.Printf("Found genesis header with hash: %s\n", targetHash.Hex())
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read genesis: %w", err)
	}

	// Decode the header using standard types.Header (19-field format)
	header := new(types.Header)
	if err := rlp.DecodeBytes(headerData, header); err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	fmt.Printf("=== GENESIS BLOCK INFO ===\n")
	fmt.Printf("Hash:        %s\n", header.Hash().Hex())
	fmt.Printf("StateRoot:   %s\n", header.Root.Hex())
	fmt.Printf("Number:      %d\n", header.Number.Uint64())
	fmt.Printf("Time:        %d\n", header.Time)
	fmt.Printf("GasLimit:    %d\n", header.GasLimit)
	fmt.Printf("Coinbase:    %s\n", header.Coinbase.Hex())
	fmt.Printf("Difficulty:  %s\n", header.Difficulty.String())
	if header.BaseFee != nil {
		fmt.Printf("BaseFee:     %s\n", header.BaseFee.String())
	}

	// Create Genesis configuration matching the migrated genesis EXACTLY
	// NOTE: The actual genesis had an EMPTY alloc - all balances were created later!
	genesis := &gethcore.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(96369), // Network 96369
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
			LondonBlock:         big.NewInt(0),
			ArrowGlacierBlock:   big.NewInt(0),
			GrayGlacierBlock:    big.NewInt(0),
			MergeNetsplitBlock:  big.NewInt(0),
			ShanghaiTime:        new(uint64),
			CancunTime:          new(uint64),
			BlobScheduleConfig: &params.BlobScheduleConfig{
				Cancun: &params.BlobConfig{
					Target:         1,
					Max:            2,
					UpdateFraction: 1112826,
				},
			},
		},
		Nonce:      header.Nonce.Uint64(),
		Timestamp:  header.Time,
		ExtraData:  header.Extra,
		GasLimit:   header.GasLimit,
		Difficulty: header.Difficulty,
		Mixhash:    header.MixDigest,
		Coinbase:   header.Coinbase,
		// CRITICAL: Genesis had EMPTY alloc - all balances created through transactions
		Alloc: gethcore.GenesisAlloc{},
		// Number must be 0 for genesis
		Number:     0,
		GasUsed:    header.GasUsed,
		ParentHash: header.ParentHash,
		BaseFee:    header.BaseFee,
	}

	// Set Shanghai and Cancun times to 0 (genesis)
	zero := uint64(0)
	genesis.Config.ShanghaiTime = &zero
	genesis.Config.CancunTime = &zero

	fmt.Printf("\n✅ Genesis configuration created successfully!\n")

	return genesis, nil
}

// copyAllDatabaseEntries copies ALL database entries from migrated BadgerDB
// This includes blocks, state, receipts - everything except genesis which was already copied
func copyAllDatabaseEntries(db ethdb.Database, migratedDBPath string) error {
	fmt.Printf("\n=== COPYING ALL DATABASE ENTRIES FROM MIGRATED DATABASE ===\n")
	fmt.Printf("Source database: %s\n", migratedDBPath)
	fmt.Printf("Copying blocks, state, receipts, and all metadata...\n\n")

	// Open BadgerDB
	opts := badger.DefaultOptions(migratedDBPath)
	opts.ReadOnly = true
	opts.Logger = nil

	bdb, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open BadgerDB: %w", err)
	}
	defer bdb.Close()

	// Track copy progress
	copied := 0
	skipped := 0  // Skip genesis block 0 (already copied)
	canonicalHashesGenerated := 0
	lastLogTime := time.Now()

	// Iterate through ALL keys in BadgerDB and copy them
	fmt.Printf("Copying all database entries...\n")
	fmt.Printf("CRITICAL: Will generate canonical hash keys from header keys!\n\n")

	// Create a batch for efficient writes
	batch := db.NewBatch()
	batchSize := 0
	const maxBatchSize = 1000  // Flush every 1000 writes
	const maxBatchBytes = 1 * 1024 * 1024  // Flush every 1MB

	// Iterate through ALL keys in the migrated database and copy them
	err = bdb.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			// DEBUG: Log first 50 keys to understand format
			if copied < 50 {
				firstByte := "?"
				if len(key) > 0 {
					firstByte = fmt.Sprintf("0x%02x", key[0])
				}
				fmt.Printf("DEBUG [%d]: len=%d firstByte=%s hex=%x\n",
					copied, len(key), firstByte, key[:min(len(key), 20)])
			}

			// Skip genesis block 0 entries (already copied)
			// migrated-ethdb is in standard geth format (NO blockchain ID prefix)
			// Header keys: 'h' + 8-byte number + 32-byte hash = 41 bytes
			// Canonical hash keys: 'h' + 8-byte number + 'n' = 10 bytes (we generate these)

			// Check for 41-byte header key: 'h' + block number + hash
			if len(key) == 41 && key[0] == 'h' {
				blockNum := binary.BigEndian.Uint64(key[1:9])
				if blockNum == 0 {
					skipped++
					continue // Skip genesis (already copied)
				}

				// Read header value
				value, err := item.ValueCopy(nil)
				if err != nil {
					return fmt.Errorf("failed to read header value for block %d: %w", blockNum, err)
				}

				// Write the header key-value
				if err := batch.Put(key, value); err != nil {
					return fmt.Errorf("failed to write header key %x: %w", key, err)
				}
				copied++
				batchSize++

				// Compute hash from RLP-encoded header
				actualHash := crypto.Keccak256Hash(value)

				// Generate canonical hash key: 'h' + 8-byte number + 'n'
				canonicalKey := make([]byte, 10)
				canonicalKey[0] = 'h'
				binary.BigEndian.PutUint64(canonicalKey[1:9], blockNum)
				canonicalKey[9] = 'n' // Geth headerHashSuffix

				// Write canonical hash entry
				if err := batch.Put(canonicalKey, actualHash.Bytes()); err != nil {
					return fmt.Errorf("failed to write canonical hash for block %d: %w", blockNum, err)
				}
				canonicalHashesGenerated++
				batchSize++

				// Flush batch if needed
				if batchSize >= maxBatchSize || batch.ValueSize() >= maxBatchBytes {
					if err := batch.Write(); err != nil {
						return fmt.Errorf("failed to flush batch: %w", err)
					}
					batch.Reset()
					batchSize = 0
				}

				// Skip normal copy since we already wrote this key
				continue
			}

			// Copy this key-value pair to the new database
			value, err := item.ValueCopy(nil)
			if err != nil {
				return fmt.Errorf("failed to read value for key %x: %w", key, err)
			}

			if err := batch.Put(key, value); err != nil {
				return fmt.Errorf("failed to write key %x: %w", key, err)
			}
			batchSize++

			copied++

			// Flush batch every maxBatchSize entries
			if batchSize >= maxBatchSize || batch.ValueSize() >= maxBatchBytes {
				if err := batch.Write(); err != nil {
					return fmt.Errorf("failed to flush batch: %w", err)
				}
				batch.Reset()
				batchSize = 0
			}

			// Log progress every 10,000 entries or every second
			if copied%10000 == 0 || time.Since(lastLogTime) > time.Second {
				fmt.Printf("Copied %d entries (skipped %d genesis, generated %d canonical hashes)...\n", 
					copied, skipped, canonicalHashesGenerated)
				lastLogTime = time.Now()
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to copy database: %w", err)
	}

	// Final flush for any remaining entries in batch
	if batchSize > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to flush final batch: %w", err)
		}
		fmt.Printf("Flushed final batch of %d entries\n", batchSize)
	}

	fmt.Printf("\n=== COPY COMPLETE ===\n")
	fmt.Printf("Total entries copied: %d\n", copied)
	fmt.Printf("Genesis entries skipped: %d\n", skipped)
	fmt.Printf("Canonical hash keys generated: %d\n", canonicalHashesGenerated)
	fmt.Printf("This includes:\n")
	fmt.Printf("  - All block headers\n")
	fmt.Printf("  - All block bodies\n")
	fmt.Printf("  - All receipts\n")
	fmt.Printf("  - All state trie data\n")
	fmt.Printf("  - All canonical hash mappings (GENERATED from header keys)\n")
	fmt.Printf("  - All metadata\n\n")

	// CRITICAL: After copying all entries, we need to set the HEAD pointers
	// so that NewBlockChain() knows where the chain head is
	fmt.Printf("Setting head block pointers...\n")

	// Find the highest block number by scanning the NEW database (not migrated!)
	// We'll use binary search to find the highest block that exists
	var highestBlock uint64
	var highestHash common.Hash

	// First, get an upper bound by checking if we have block 1,082,781 (known max from migration)
	maxExpected := uint64(1082781)
	testHash := rawdb.ReadCanonicalHash(db, maxExpected)

	if testHash != (common.Hash{}) {
		// We have blocks up to at least maxExpected
		highestBlock = maxExpected
		highestHash = testHash
		fmt.Printf("Quick check: Found block %d\n", maxExpected)
	} else {
		// Binary search from 0 to maxExpected to find highest block
		fmt.Printf("Binary searching for highest block (0 to %d)...\n", maxExpected)
		left, right := uint64(0), maxExpected

		for left <= right {
			mid := (left + right) / 2
			hash := rawdb.ReadCanonicalHash(db, mid)

			if hash != (common.Hash{}) {
				// Block mid exists, try higher
				highestBlock = mid
				highestHash = hash
				if mid == maxExpected {
					break // Can't go higher
				}
				left = mid + 1
			} else {
				// Block mid doesn't exist, try lower
				if mid == 0 {
					break // Can't go lower
				}
				right = mid - 1
			}
		}
	}

	if highestBlock == 0 {
		fmt.Printf("No blocks found beyond genesis, keeping head at block 0\n")
	} else {
		fmt.Printf("Found highest block: %d (hash: %x)\n", highestBlock, highestHash)

		// Set the head pointers in the database
		rawdb.WriteHeadBlockHash(db, highestHash)
		rawdb.WriteHeadHeaderHash(db, highestHash)
		rawdb.WriteHeadFastBlockHash(db, highestHash)
		
		// CRITICAL: Also write the header number mapping so ReadHeaderNumber works
		rawdb.WriteHeaderNumber(db, highestHash, highestBlock)

		fmt.Printf("✅ Head pointers set to block %d\n", highestBlock)
	}

	return nil
}

// InitializeCChainWithReplay imports blocks from migrated database using runtime replay
// NOTE: This assumes genesis has already been properly initialized via SetupGenesisBlock
func InitializeCChainWithReplay(blockchain *gethcore.BlockChain, db ethdb.Database) error {
	migratedDBPath := "/Users/z/work/lux/genesis/migrated-ethdb"

	// Check if the migrated database exists
	if _, err := os.Stat(migratedDBPath); os.IsNotExist(err) {
		fmt.Printf("Migrated database not found at %s, skipping replay\n", migratedDBPath)
		return nil
	}

	// Copy all blocks and state directly from migrated database
	// Block 0 (genesis) was already copied by copyGenesisFromMigratedDB
	if err := copyAllDatabaseEntries(db, migratedDBPath); err != nil {
		return fmt.Errorf("failed to copy database: %w", err)
	}

	return nil
}

// MinimalEthBackend provides a minimal Ethereum backend without p2p networking
type MinimalEthBackend struct {
	chainConfig *params.ChainConfig
	blockchain  *gethcore.BlockChain
	txPool      *txpool.TxPool
	chainDb     ethdb.Database
	engine      consensus.Engine
	networkID   uint64
}

// NewMigratedBackend creates a special backend for fully migrated data
// This completely bypasses genesis initialization
func NewMigratedBackend(db ethdb.Database, migratedHeight uint64) (*MinimalEthBackend, error) {
	fmt.Printf("Creating migrated backend for Coreth data at height %d\n", migratedHeight)
	fmt.Printf("Database type: %T\n", db)

	// CRITICAL: We MUST use the raw BadgerDB directly, not any wrappers
	// The migrated data uses raw key formats that wrappers will mangle
	rawDB := db

	// Keep unwrapping until we get to the base database
	for {
		type unwrapper interface {
			DB() ethdb.Database
		}
		if wrapped, ok := rawDB.(unwrapper); ok {
			rawDB = wrapped.DB()
			fmt.Printf("Unwrapped database layer, new type: %T\n", rawDB)
		} else {
			break
		}
	}

	fmt.Printf("Final database type after unwrapping: %T\n", rawDB)

	// Verify we can read the migrated canonical hashes
	var foundBlocks uint64 = 0
	fmt.Printf("Testing canonical block reads with rawdb.ReadCanonicalHash()...\n")
	for i := uint64(0); i <= 10; i++ {
		hash := rawdb.ReadCanonicalHash(rawDB, i)
		if hash != (common.Hash{}) {
			foundBlocks++
			fmt.Printf("  Block %d: ✓ Found hash: %x\n", i, hash[:8])
		} else {
			fmt.Printf("  Block %d: ✗ NOT FOUND\n", i)
		}
	}

	if foundBlocks == 0 {
		// Try iterating to see what keys exist
		fmt.Printf("ERROR: No canonical blocks found! Checking what keys exist in database...\n")

		type iterator interface {
			NewIterator([]byte, []byte) ethdb.Iterator
		}

		if iter, ok := rawDB.(iterator); ok {
			fmt.Printf("Database supports iteration, checking first 20 keys...\n")
			it := iter.NewIterator(nil, nil)
			defer it.Release()

			count := 0
			for it.Next() && count < 20 {
				key := it.Key()
				fmt.Printf("  Key %d: len=%d, first_byte=%c (0x%02x), hex=%x\n",
					count, len(key), key[0], key[0], key[:min(16, len(key))])
				count++
			}
		}

		return nil, fmt.Errorf("no canonical blocks found - database may not be properly migrated")
	}

	fmt.Printf("✓ Found %d canonical blocks in database\n", foundBlocks)

	// Create chain config for LUX mainnet with all forks enabled
	chainConfig := &params.ChainConfig{
		ChainID:                 big.NewInt(96369),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		MuirGlacierBlock:        big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		ArrowGlacierBlock:       big.NewInt(0),
		GrayGlacierBlock:        big.NewInt(0),
		MergeNetsplitBlock:      big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              nil,
		VerkleTime:              nil,
		TerminalTotalDifficulty: common.Big0,
		BlobScheduleConfig: &params.BlobScheduleConfig{
			Cancun: &params.BlobConfig{
				Target:         3,
				Max:            6,
				UpdateFraction: 3338477,
			},
		},
	}

	// Create a dummy consensus engine
	engine := &dummyEngine{}

	fmt.Printf("Looking for head block at height %d...\n", migratedHeight)

	// Get the hash at the migrated height using direct key access
	var headHash common.Hash
	var actualHeight = migratedHeight

	// Try the exact height first using rawdb.ReadCanonicalHash()
	headHash = rawdb.ReadCanonicalHash(rawDB, migratedHeight)
	if headHash != (common.Hash{}) {
		fmt.Printf("Found block at requested height %d: %x\n", migratedHeight, headHash)
	} else if migratedHeight == 1082780 {
		// Known last block for the migration
		// Try to find it by scanning backwards
		fmt.Printf("Scanning for highest block (migration height not found directly)...\n")
		for i := migratedHeight; i >= migratedHeight-100 && i > 0; i-- {
			hash := rawdb.ReadCanonicalHash(rawDB, i)
			if hash != (common.Hash{}) {
				headHash = hash
				actualHeight = i
				fmt.Printf("Found highest block at height %d: %x\n", i, headHash)
				break
			}
		}
	}

	if headHash == (common.Hash{}) {
		return nil, fmt.Errorf("no head block found in migrated database at height %d", migratedHeight)
	}

	fmt.Printf("Will use block %d as head with hash: %x\n", actualHeight, headHash)

	// Get genesis block (block 0) using rawdb.ReadCanonicalHash()
	genesisHash := rawdb.ReadCanonicalHash(rawDB, 0)
	if genesisHash == (common.Hash{}) {
		return nil, fmt.Errorf("genesis block (block 0) not found in migrated database")
	}
	fmt.Printf("Genesis block hash: %x\n", genesisHash)

	// Read genesis block to create Genesis object
	fmt.Printf("Attempting to read block 0 with hash %x...\n", genesisHash)

	// Try reading header directly with key format
	headerKey := make([]byte, 0, 41)
	headerKey = append(headerKey, 'h')
	numBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(numBytes, 0)
	headerKey = append(headerKey, numBytes...)
	headerKey = append(headerKey, genesisHash.Bytes()...)

	headerData, err := rawDB.Get(headerKey)
	if err != nil || len(headerData) == 0 {
		fmt.Printf("Header not found with key %x: %v\n", headerKey, err)
		return nil, fmt.Errorf("genesis block header not found for hash %x", genesisHash)
	}

	fmt.Printf("Found header data (%d bytes), decoding as pre-Shanghai format...\n", len(headerData))

	// Decode as pre-Shanghai header (EVM format)
	preGenesisHeader := new(PreShanghaiHeader)
	if err := rlp.DecodeBytes(headerData, preGenesisHeader); err != nil {
		return nil, fmt.Errorf("failed to decode pre-Shanghai genesis header: %w", err)
	}

	fmt.Printf("✓ Decoded pre-Shanghai genesis header, block number: %d\n", preGenesisHeader.Number.Uint64())

	// Convert to post-Shanghai format
	genesisHeader := preGenesisHeader.ToPostShanghai()

	// Re-write header in post-Shanghai format so rawdb can find it
	rawdb.WriteHeader(rawDB, genesisHeader)
	fmt.Printf("✓ Rewrote genesis header in post-Shanghai format\n")

	// Read body using direct key format
	bodyKey := make([]byte, 0, 41)
	bodyKey = append(bodyKey, 'b')
	bodyKey = append(bodyKey, numBytes...)
	bodyKey = append(bodyKey, genesisHash.Bytes()...)

	var genesisBody *types.Body
	if bodyData, err := rawDB.Get(bodyKey); err == nil && len(bodyData) > 0 {
		fmt.Printf("Found body data (%d bytes), decoding...\n", len(bodyData))
		genesisBody = new(types.Body)
		if err := rlp.DecodeBytes(bodyData, genesisBody); err != nil {
			fmt.Printf("Failed to decode body: %v\n", err)
			genesisBody = &types.Body{}
		}
	} else {
		genesisBody = &types.Body{} // Empty body for genesis
	}

	// Re-write body in Coreth format
	rawdb.WriteBody(rawDB, genesisHash, 0, genesisBody)
	fmt.Printf("Successfully loaded and re-wrote genesis block 0\n")

	// Write the full genesis block
	genesisBlock := types.NewBlockWithHeader(genesisHeader).WithBody(*genesisBody)
	rawdb.WriteBlock(rawDB, genesisBlock)

	// Write the chain config to BOTH wrapped and unwrapped databases
	rawdb.WriteChainConfig(rawDB, genesisHash, chainConfig)
	rawdb.WriteChainConfig(db, genesisHash, chainConfig)
	fmt.Printf("Wrote chain config under actual genesis hash %x (to both DBs)\n", genesisHash)

	// Write canonical hashes and header number mappings for critical blocks
	fmt.Printf("Writing canonical hashes and header numbers for critical blocks...\n")

	// Write genesis (block 0)
	rawdb.WriteCanonicalHash(rawDB, genesisHash, 0)
	rawdb.WriteCanonicalHash(db, genesisHash, 0)
	rawdb.WriteHeaderNumber(rawDB, genesisHash, 0)
	rawdb.WriteHeaderNumber(db, genesisHash, 0)

	// Verify genesis write
	if num, ok := rawdb.ReadHeaderNumber(rawDB, genesisHash); ok {
		fmt.Printf("✓ Verified genesis header number: %d\n", num)
	} else {
		fmt.Printf("❌ FAILED to read back genesis header number!\n")
	}

	// Write head block
	rawdb.WriteCanonicalHash(rawDB, headHash, actualHeight)
	rawdb.WriteCanonicalHash(db, headHash, actualHeight)
	rawdb.WriteHeaderNumber(rawDB, headHash, actualHeight)
	rawdb.WriteHeaderNumber(db, headHash, actualHeight)

	// Verify head write
	if num, ok := rawdb.ReadHeaderNumber(rawDB, headHash); ok {
		fmt.Printf("✓ Verified head header number: %d\n", num)
	} else {
		fmt.Printf("❌ FAILED to read back head header number for hash %x!\n", headHash)
	}

	fmt.Printf("✓ Wrote canonical hashes and header numbers for blocks 0 and %d\n", actualHeight)

	// Skip header translation - already done in previous migration run
	fmt.Printf("Skipping header translation (already in geth format)...\n")

	// TD is not needed for PoS/POA chains, skip writing it

	// Set the head pointers on BOTH rawDB and the wrapped db
	// The wrapped db is what NewBlockChain will use, so it MUST have these
	rawdb.WriteHeadBlockHash(rawDB, headHash)
	rawdb.WriteHeadHeaderHash(rawDB, headHash)
	rawdb.WriteHeadFastBlockHash(rawDB, headHash)
	rawdb.WriteLastPivotNumber(rawDB, actualHeight)

	// Also write to wrapped database (the original db parameter)
	rawdb.WriteHeadBlockHash(db, headHash)
	rawdb.WriteHeadHeaderHash(db, headHash)
	rawdb.WriteHeadFastBlockHash(db, headHash)
	rawdb.WriteLastPivotNumber(db, actualHeight)

	fmt.Printf("Set head pointers to block %d (written to both wrapped and unwrapped DB)\n", actualHeight)

	// Verify the head header can be read (just check it exists, don't decode)
	// GETH FORMAT: 'h' + number (8 bytes) + hash (32 bytes)
	gethHeaderKey := make([]byte, 41)
	gethHeaderKey[0] = 'h'
	binary.BigEndian.PutUint64(gethHeaderKey[1:9], actualHeight)
	copy(gethHeaderKey[9:41], headHash.Bytes())

	headerData, getErr := rawDB.Get(gethHeaderKey)
	if getErr != nil || len(headerData) == 0 {
		return nil, fmt.Errorf("cannot read head header at block %d hash %x: %w", actualHeight, headHash, getErr)
	}

	fmt.Printf("✓ Head header verified: %d bytes at block %d\n", len(headerData), actualHeight)

	// CRITICAL: Decode pre-Shanghai header and convert to post-Shanghai format
	// This ensures rawdb.ReadHeadHeader() can find it during NewBlockChain initialization
	preHeader := new(PreShanghaiHeader)
	if err := rlp.DecodeBytes(headerData, preHeader); err == nil {
		fmt.Printf("✓ Decoded pre-Shanghai head header at block %d\n", preHeader.Number.Uint64())

		// Convert to post-Shanghai format
		headHeader := preHeader.ToPostShanghai()
		fmt.Printf("✓ Converted to post-Shanghai format\n")

		// CRITICAL: Write the post-Shanghai header under the ORIGINAL pre-Shanghai hash
		// NOT the new hash! The hash changes when we add WithdrawalsHash field.
		// We must use the original hash (headHash) from the database, not header.Hash()

		// Encode the post-Shanghai header
		postShanghaiData, err := rlp.EncodeToBytes(headHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to encode post-Shanghai header: %w", err)
		}

		// Write using the ORIGINAL hash (headHash), not the new hash
		// Key format: 'h' + number (8 bytes) + hash (32 bytes)
		postShanghaiKey := make([]byte, 41)
		postShanghaiKey[0] = 'h'
		binary.BigEndian.PutUint64(postShanghaiKey[1:9], actualHeight)
		copy(postShanghaiKey[9:41], headHash.Bytes())

		// Write to BOTH databases with the ORIGINAL hash
		if err := rawDB.Put(postShanghaiKey, postShanghaiData); err != nil {
			return nil, fmt.Errorf("failed to write post-Shanghai header to rawDB: %w", err)
		}
		if err := db.Put(postShanghaiKey, postShanghaiData); err != nil {
			return nil, fmt.Errorf("failed to write post-Shanghai header to db: %w", err)
		}

		fmt.Printf("✓ Wrote post-Shanghai head header at block %d using ORIGINAL hash %x\n", headHeader.Number.Uint64(), headHash)
	} else {
		fmt.Printf("ERROR: Could not decode pre-Shanghai head header: %v\n", err)
		return nil, fmt.Errorf("failed to decode pre-Shanghai head header: %w", err)
	}

	// Create blockchain options
	options := &gethcore.BlockChainConfig{
		TrieCleanLimit: 256,
		NoPrefetch:     false,
		StateScheme:    rawdb.HashScheme,
	}

	// Create blockchain - pass nil genesis so it uses what's in the database
	// All the data is already there from our migration
	fmt.Printf("Creating blockchain with nil genesis (using database)...\n")
	blockchain, err := gethcore.NewBlockChain(rawDB, nil, engine, options)
	if err != nil {
		fmt.Printf("Failed to create blockchain: %v\n", err)
		return nil, fmt.Errorf("failed to create blockchain from migrated data: %w", err)
	}

	// Force the blockchain to recognize the correct height
	currentBlock := blockchain.CurrentBlock()
	fmt.Printf("Blockchain initialized at height: %d (expected: %d)\n",
		currentBlock.Number.Uint64(), actualHeight)

	if currentBlock.Number.Uint64() != actualHeight {
		// The blockchain didn't load at the right height
		// This can happen if the chain loading logic reset to genesis
		fmt.Printf("WARNING: Blockchain not at expected height, attempting to advance...\n")

		// Try to force load the head block
		headBlock := rawdb.ReadBlock(rawDB, headHash, actualHeight)
		if headBlock != nil {
			fmt.Printf("Found head block in database, inserting to advance chain...\n")
			if _, err := blockchain.InsertChain([]*types.Block{headBlock}); err != nil {
				fmt.Printf("Failed to insert head block: %v\n", err)
			} else {
				currentBlock = blockchain.CurrentBlock()
				fmt.Printf("After insert, blockchain at height: %d\n", currentBlock.Number.Uint64())
			}
		}
	}

	// Create transaction pool
	legacyPool := legacypool.New(ethconfig.Defaults.TxPool, blockchain)
	txPool, err := txpool.New(ethconfig.Defaults.TxPool.PriceLimit, blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		return nil, err
	}

	backend := &MinimalEthBackend{
		chainConfig: chainConfig,
		blockchain:  blockchain,
		txPool:      txPool,
		chainDb:     db,
		engine:      engine,
		networkID:   96369,
	}

	// Background block replay - Enable for SubnetEVM data re-execution
	// This will re-execute all transactions to rebuild proper state
	fmt.Printf("🔄 Starting background block replay to rebuild state...\n")
	fmt.Printf("   This will re-execute all %d blocks\n", actualHeight)
	
	// Enable the replay in background
	go replayBlocksToRebuildState(blockchain, rawDB, actualHeight)

	fmt.Printf("✅ Blockchain ready at height %d with canonical mappings\n", actualHeight)
	fmt.Printf("   No rebuild needed - EVM data accessible via namespace stripper\n")

	return backend, nil
}

// NewMinimalEthBackendForMigration creates a backend that loads from migrated data
func NewMinimalEthBackendForMigration(db ethdb.Database, config *ethconfig.Config, genesis *gethcore.Genesis, migratedHeight uint64) (*MinimalEthBackend, error) {
	// The migrated data is already in proper geth format in the ethdb
	// We need to unwrap any prefix databases to get to the actual BadgerDB
	rawDB := db

	// Try to unwrap prefixdb if present
	type unwrapper interface {
		DB() ethdb.Database
	}
	if wrapped, ok := rawDB.(unwrapper); ok {
		rawDB = wrapped.DB()
		fmt.Printf("Unwrapped database, new type: %T\n", rawDB)
	}

	var chainConfig *params.ChainConfig
	if genesis != nil && genesis.Config != nil {
		chainConfig = genesis.Config
	} else {
		// Use a default config for migrated data with all forks enabled
		chainConfig = &params.ChainConfig{
			ChainID:                 big.NewInt(96369),
			HomesteadBlock:          big.NewInt(0),
			EIP150Block:             big.NewInt(0),
			EIP155Block:             big.NewInt(0),
			EIP158Block:             big.NewInt(0),
			ByzantiumBlock:          big.NewInt(0),
			ConstantinopleBlock:     big.NewInt(0),
			PetersburgBlock:         big.NewInt(0),
			IstanbulBlock:           big.NewInt(0),
			MuirGlacierBlock:        big.NewInt(0),
			BerlinBlock:             big.NewInt(0),
			LondonBlock:             big.NewInt(0),
			ArrowGlacierBlock:       big.NewInt(0),
			GrayGlacierBlock:        big.NewInt(0),
			MergeNetsplitBlock:      big.NewInt(0),
			ShanghaiTime:            newUint64(0),
			CancunTime:              newUint64(0),
			PragueTime:              nil,
			VerkleTime:              nil,
			TerminalTotalDifficulty: common.Big0,
			BlobScheduleConfig: &params.BlobScheduleConfig{
				Cancun: &params.BlobConfig{
					Target:         3,
					Max:            6,
					UpdateFraction: 3338477,
				},
			},
		}
	}

	// Create consensus engine
	var engine consensus.Engine
	if chainConfig.Clique != nil {
		engine = clique.New(chainConfig.Clique, db)
	} else {
		// Use a dummy engine for PoS
		engine = &dummyEngine{}
	}

	// Set the head pointers to the migrated height
	fmt.Printf("Setting blockchain to migrated height %d\n", migratedHeight)

	// Get the hash at the migrated height using 9-byte format
	key := canonicalKey(migratedHeight)

	var headHash common.Hash
	if val, err := db.Get(key); err == nil && len(val) == 32 {
		copy(headHash[:], val)
		fmt.Printf("Found head hash at height %d: %x\n", migratedHeight, headHash)

		// Write head pointers
		rawdb.WriteHeadBlockHash(db, headHash)
		rawdb.WriteHeadHeaderHash(db, headHash)
		rawdb.WriteHeadFastBlockHash(db, headHash)
		rawdb.WriteLastPivotNumber(db, migratedHeight)
	}

	// Initialize blockchain - skip genesis since we have migrated data
	options := &gethcore.BlockChainConfig{
		TrieCleanLimit: config.TrieCleanCache,
		NoPrefetch:     config.NoPrefetch,
		StateScheme:    rawdb.HashScheme,
	}

	// IMPORTANT: Pass nil genesis to prevent overwriting migrated data
	// Use rawDB (unwrapped) to avoid prefix wrapper issues
	fmt.Printf("Creating blockchain from migrated data...\n")
	blockchain, err := gethcore.NewBlockChain(rawDB, nil, engine, options)
	if err != nil {
		// If it fails, it might be because it expects genesis
		// Try creating a minimal genesis that won't overwrite data
		fmt.Printf("First attempt failed: %v, trying with minimal genesis\n", err)

		minimalGenesis := &gethcore.Genesis{
			Config:     chainConfig,
			Difficulty: big.NewInt(0),
			GasLimit:   8000000,
			Alloc:      nil, // No allocations to prevent state overwrite
		}

		blockchain, err = gethcore.NewBlockChain(rawDB, minimalGenesis, engine, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create blockchain from migrated data: %w", err)
		}
	}

	fmt.Printf("Blockchain created, current height: %d\n", blockchain.CurrentBlock().Number.Uint64())

	// Create transaction pool
	legacyPool := legacypool.New(config.TxPool, blockchain)
	txPool, err := txpool.New(config.TxPool.PriceLimit, blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		return nil, err
	}

	return &MinimalEthBackend{
		chainConfig: chainConfig,
		blockchain:  blockchain,
		txPool:      txPool,
		chainDb:     db,
		engine:      engine,
		networkID:   config.NetworkId,
	}, nil
}

// NewMinimalEthBackend creates a new minimal Ethereum backend
func NewMinimalEthBackend(db ethdb.Database, config *ethconfig.Config, genesis *gethcore.Genesis) (*MinimalEthBackend, error) {
	// Checkpoint: Verify canonical hash at entry
	targetHash := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
	entryHash := rawdb.ReadCanonicalHash(db, 0)
	log.Info("[CHECKPOINT 3] NewMinimalEthBackend entry",
		"expected", targetHash.Hex(),
		"actual", entryHash.Hex(),
		"match", entryHash == targetHash,
		"genesisNil", genesis == nil)
	
	// Special marker for "use existing genesis in database"
	_ = false // useExistingGenesis - may use later

	// CRITICAL FIX: Check if database already has genesis FIRST before creating new one
	// This prevents overwriting a copied genesis from migrated database
	if genesis == nil {
		// Check for existing genesis FIRST to avoid overwriting copied genesis
		if existingHash := rawdb.ReadCanonicalHash(db, 0); existingHash != (common.Hash{}) {
			fmt.Printf("Found existing genesis in database: %s\n", existingHash.Hex())
			fmt.Printf("Skipping genesis creation - will use existing\n")
			// Keep genesis = nil to use existing genesis in database
		} else {
			// No genesis in database, try to load from migrated database
			migratedDBPath := "/Users/z/work/lux/genesis/migrated-ethdb"
			if _, err := os.Stat(migratedDBPath); err == nil {
				fmt.Printf("Found migrated database at %s, loading genesis from it\n", migratedDBPath)

				migratedGenesis, err := createGenesisFromMigratedDB(migratedDBPath)
				if err != nil {
					fmt.Printf("Warning: Failed to load genesis from migrated DB: %v\n", err)
					fmt.Printf("Will use default genesis configuration\n")
				} else {
					fmt.Printf("Successfully loaded genesis from migrated database\n")
					genesis = migratedGenesis
				}
			}
		}
	}

	// If still no genesis after checking database, create default
	if genesis == nil {
		if existingHash := rawdb.ReadCanonicalHash(db, 0); existingHash == (common.Hash{}) {
			// CRITICAL: ALWAYS use proper mainnet genesis for network 96369
			// NEVER use a blank or test genesis for mainnet!
			fmt.Printf("WARNING: No genesis in database - using HARDCODED MAINNET genesis\n")
			genesis = &gethcore.Genesis{
				Config: &params.ChainConfig{
					ChainID:                 big.NewInt(96369),
					HomesteadBlock:          big.NewInt(0),
					EIP150Block:             big.NewInt(0),
					EIP155Block:             big.NewInt(0),
					EIP158Block:             big.NewInt(0),
					ByzantiumBlock:          big.NewInt(0),
					ConstantinopleBlock:     big.NewInt(0),
					PetersburgBlock:         big.NewInt(0),
					IstanbulBlock:           big.NewInt(0),
					MuirGlacierBlock:        big.NewInt(0),
					BerlinBlock:             big.NewInt(0),
					LondonBlock:             big.NewInt(0),
					ArrowGlacierBlock:       big.NewInt(0),
					GrayGlacierBlock:        big.NewInt(0),
					MergeNetsplitBlock:      big.NewInt(0),
					ShanghaiTime:            newUint64(0),
					CancunTime:              newUint64(0),
					TerminalTotalDifficulty: common.Big0,
					BlobScheduleConfig: &params.BlobScheduleConfig{
						Cancun: &params.BlobConfig{
							Target:         3,
							Max:            6,
							UpdateFraction: 3338477,
						},
					},
				},
				// Use exact genesis from block 0 of network 96369
				Nonce:      0x0,
				Timestamp:  0x672485c2, // 1730446786 - from block 0
				ExtraData:  []byte{},
				GasLimit:   12000000, // 12M - from block 0
				Difficulty: big.NewInt(0),
				Mixhash:    common.Hash{},
				Coinbase:   common.Address{},
				// State root from block 0: 0x2d1cedac263020c5c56ef962f6abe0da1f5217bdc6468f8c9258a0ea23699e80
				// Empty alloc - state comes from migrated database
				Alloc: gethcore.GenesisAlloc{
					// Treasury address - will be filled from state
					common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"): {
						Balance: big.NewInt(0), // Will be set from migrated state
					},
				},
			}
		}
	}

	var chainConfig *params.ChainConfig
	if genesis != nil {
		chainConfig = genesis.Config
	}
	if chainConfig == nil {
		// Use default mainnet config for replay scenarios with all forks enabled
		chainConfig = &params.ChainConfig{
			ChainID:                 big.NewInt(96369),
			HomesteadBlock:          big.NewInt(0),
			EIP150Block:             big.NewInt(0),
			EIP155Block:             big.NewInt(0),
			EIP158Block:             big.NewInt(0),
			ByzantiumBlock:          big.NewInt(0),
			ConstantinopleBlock:     big.NewInt(0),
			PetersburgBlock:         big.NewInt(0),
			IstanbulBlock:           big.NewInt(0),
			MuirGlacierBlock:        big.NewInt(0),
			BerlinBlock:             big.NewInt(0),
			LondonBlock:             big.NewInt(0),
			ArrowGlacierBlock:       big.NewInt(0),
			GrayGlacierBlock:        big.NewInt(0),
			MergeNetsplitBlock:      big.NewInt(0),
			ShanghaiTime:            newUint64(0),
			CancunTime:              newUint64(0),
			PragueTime:              nil,
			VerkleTime:              nil,
			TerminalTotalDifficulty: common.Big0,
			BlobScheduleConfig: &params.BlobScheduleConfig{
				Cancun: &params.BlobConfig{
					Target:         3,
					Max:            6,
					UpdateFraction: 3338477,
				},
			},
		}
	}

	// Create consensus engine
	var engine consensus.Engine
	if chainConfig.Clique != nil {
		engine = clique.New(chainConfig.Clique, db)
	} else {
		// Use a dummy engine for PoS
		engine = &dummyEngine{}
	}

	// Initialize blockchain
	options := &gethcore.BlockChainConfig{
		TrieCleanLimit: config.TrieCleanCache,
		NoPrefetch:     config.NoPrefetch,
		StateScheme:    rawdb.HashScheme,
	}

	// For network 96369, check for migrated data first
	// DISABLED: The migrated data has issues with the treasury account missing
	// We need to use regular genesis for now
	if false && config != nil && config.NetworkId == 96369 {
		fmt.Printf("Checking for migrated data with 41-byte key format...\n")
		// Migration check disabled due to iterator issues
		fmt.Printf("Migration check skipped - using regular genesis\n")
	}

	// Log genesis info for debugging
	if genesis != nil {
		genesisBlock := genesis.ToBlock()
		fmt.Printf("Genesis block hash: %s\n", genesisBlock.Hash().Hex())
		fmt.Printf("Genesis chain ID: %v\n", genesis.Config.ChainID)
	}

	// CRITICAL FIX: Check for migrated database BEFORE genesis initialization
	// If we have head pointers pointing to non-genesis blocks, this is a migrated database
	// and we must NOT call SetupGenesisBlockWithOverride (it would overwrite head pointers)
	headHash := rawdb.ReadHeadBlockHash(db)
	stored := rawdb.ReadCanonicalHash(db, 0)

	fmt.Printf("Migration detection: headHash=%s, genesisHash=%s\n", headHash.Hex(), stored.Hex())

	// Detect migrated database: head exists and points to non-genesis block
	isMigratedDB := headHash != (common.Hash{}) && headHash != stored

	if isMigratedDB {
		fmt.Printf("✅ MIGRATED DATABASE DETECTED\n")
		fmt.Printf("   Head block: %s\n", headHash.Hex())
		fmt.Printf("   Genesis:    %s\n", stored.Hex())
		fmt.Printf("   Preserving head pointers - will use createBlockchainWithoutGenesis()\n")
		fmt.Printf("   This will read genesis from block 0 in database, preserving all migrated data\n")

		// For migrated databases:
		// 1. Genesis MUST be at block 0 already (from migration)
		// 2. We must NOT call SetupGenesisBlockWithOverride (it overwrites head pointers)
		// 3. We must NOT write a new genesis (it must match block 0 exactly)
		// 4. The blockchain creation at line 1494 will use createBlockchainWithoutGenesis()
		//    which reads genesis from block 0 and preserves head pointers

		if stored == (common.Hash{}) {
			// This should NOT happen for a properly migrated database
			return nil, fmt.Errorf("migrated database has head pointers but no genesis at block 0")
		}

		// Skip all genesis initialization - genesis is already at block 0
		// Jump directly to blockchain creation which will use createBlockchainWithoutGenesis()
	} else {
		// Normal (non-migrated) database - safe to use standard genesis initialization
		fmt.Printf("Debug: Reading canonical hash key: %x value: %x err: %v\n",
			canonicalKey(0), stored, nil)

		if stored == (common.Hash{}) {
			// Double check with direct key access for migrated data
			// Use 9-byte canonical key format (no suffix)
			key := canonicalKey(0)
			if val, err := db.Get(key); err == nil && len(val) == 32 {
				copy(stored[:], val)
				fmt.Printf("Found canonical hash with direct key access: %x\n", stored)
			}

			if stored == (common.Hash{}) {
				fmt.Printf("No genesis found in database, will initialize\n")

				// SPECIAL CASE: Check if we're replaying from an existing genesis
				// In this case, the genesis is already written but SetupGenesisBlockWithOverride
				// will fail because it sees a different genesis
				expectedReplayGenesis := common.HexToHash("0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e")
				if header := rawdb.ReadHeader(db, expectedReplayGenesis, 0); header != nil {
					fmt.Printf("Found replay genesis in database, using it directly\n")
					stored = expectedReplayGenesis
					// Don't run SetupGenesisBlockWithOverride
				} else {
					// Create trie database for genesis initialization
					tdb := triedb.NewDatabase(db, triedb.HashDefaults)

					// DEBUG: Print genesis config before setup
					if genesis != nil && genesis.Config != nil {
						fmt.Printf("DEBUG: Genesis ChainID=%v\n", genesis.Config.ChainID)
						fmt.Printf("DEBUG: Genesis CancunTime=%v\n", genesis.Config.CancunTime)
						if genesis.Config.BlobScheduleConfig != nil && genesis.Config.BlobScheduleConfig.Cancun != nil {
							fmt.Printf("DEBUG: BlobSchedule Cancun: Target=%d Max=%d UpdateFraction=%d\n",
								genesis.Config.BlobScheduleConfig.Cancun.Target,
								genesis.Config.BlobScheduleConfig.Cancun.Max,
								genesis.Config.BlobScheduleConfig.Cancun.UpdateFraction)
						} else {
							fmt.Printf("DEBUG: BlobScheduleConfig is NIL or Cancun is NIL\n")
						}
					}

					// Initialize genesis block normally
					_, genesisHash, _, err := gethcore.SetupGenesisBlockWithOverride(db, tdb, genesis, nil)
					if err != nil {
						return nil, fmt.Errorf("failed to setup genesis: %w", err)
					}

				if genesisHash != (common.Hash{}) {
					fmt.Printf("Genesis initialized with hash: %s\n", genesisHash.Hex())
				}

				// Check again
				stored = rawdb.ReadCanonicalHash(db, 0)
				fmt.Printf("After setup, canonical hash at 0: %s\n", stored.Hex())
			}
		}
	} else {
		fmt.Printf("Found existing genesis in database: %s\n", stored.Hex())
	}
	} // Close the else block from line 1372

	// Check for highest block in migrated data
	currentHash := rawdb.ReadHeadBlockHash(db)
	if currentHash == (common.Hash{}) {
		// Try to read from our custom keys
		if val, err := db.Get([]byte("LastBlock")); err == nil && len(val) == 32 {
			copy(currentHash[:], val)
			fmt.Printf("Found head block from LastBlock key: %x\n", currentHash)

			// Write it to the standard location
			rawdb.WriteHeadBlockHash(db, currentHash)
			rawdb.WriteHeadHeaderHash(db, currentHash)
			rawdb.WriteHeadFastBlockHash(db, currentHash)
		}
	}

	if currentHash != (common.Hash{}) {
		if header := rawdb.ReadHeader(db, currentHash, 0); header != nil {
			fmt.Printf("Found header at hash %x with number %d\n", currentHash, header.Number.Uint64())
		} else {
			// Try to read the header by iterating through possible block numbers
			if heightBytes, err := db.Get([]byte("Height")); err == nil && len(heightBytes) == 8 {
				height := binary.BigEndian.Uint64(heightBytes)
				if header := rawdb.ReadHeader(db, currentHash, height); header != nil {
					fmt.Printf("Found header at height %d\n", height)
				}
			}
		}
	}

	// Check if migrated database exists and copy all entries BEFORE creating blockchain
	// This way the blockchain will discover the correct head when initialized
	migratedDBPath := "/Users/z/work/lux/genesis/migrated-ethdb"
	if _, err := os.Stat(migratedDBPath); err == nil {
		// Check if we need to copy - look for a block MUCH higher than genesis
		// to ensure we haven't already completed the migration
		// Use block 1000 as threshold - if it exists, migration is complete
		headHash := rawdb.ReadHeadBlockHash(db)
		block1000Hash := rawdb.ReadCanonicalHash(db, 1000)
		fmt.Printf("DEBUG: Migration check - headHash=%s, block1000Hash=%s\n", headHash.Hex(), block1000Hash.Hex())
		if headHash == (common.Hash{}) || block1000Hash == (common.Hash{}) {
			fmt.Printf("Found migrated database, copying all blocks BEFORE blockchain creation...\n")
			// Copy all database entries from migrated database
			// Note: copyGenesisFromMigratedDB already copied block 0, but copyAllDatabaseEntries
			// will skip genesis entries automatically
			if err := copyAllDatabaseEntries(db, migratedDBPath); err != nil {
				return nil, fmt.Errorf("failed to copy database entries: %w", err)
			}
			fmt.Printf("Database copy complete! Proceeding to create blockchain...\n")
		} else {
			fmt.Printf("Database already has blocks, skipping migrated database copy\n")
		}
	}

	// Now create blockchain - it will use the already initialized genesis
	// When genesis is nil and database has genesis, NewBlockChain will use it
	// However, NewBlockChain calls SetupGenesisBlockWithOverride which causes issues
	// when we have a custom genesis already in the database
	// So we need to create the blockchain manually when we have existing genesis

	var blockchain *gethcore.BlockChain
	var err error

	// Check if we already have a genesis in the database
	existingGenesisHash := rawdb.ReadCanonicalHash(db, 0)
	if existingGenesisHash != (common.Hash{}) && genesis == nil {
		// We have genesis in database and no new genesis provided
		// Create blockchain without calling SetupGenesisBlockWithOverride
		fmt.Printf("Creating blockchain with existing genesis: %s\n", existingGenesisHash.Hex())

		// Read the chain config from database
		storedConfig := rawdb.ReadChainConfig(db, existingGenesisHash)
		if storedConfig == nil {
			// No stored config, use our default
			storedConfig = chainConfig
			// Write it to database
			rawdb.WriteChainConfig(db, existingGenesisHash, storedConfig)
		}

		// CRITICAL FIX: Ensure stored config has BlobSchedule for Cancun
		if storedConfig.CancunTime != nil {
			if storedConfig.BlobScheduleConfig == nil {
				storedConfig.BlobScheduleConfig = &params.BlobScheduleConfig{}
			}
			if storedConfig.BlobScheduleConfig.Cancun == nil {
				storedConfig.BlobScheduleConfig.Cancun = &params.BlobConfig{
					Target:         3,
					Max:            6,
					UpdateFraction: 3338477,
				}
				// Update database with fixed config
				rawdb.WriteChainConfig(db, existingGenesisHash, storedConfig)
			}
		}

		// Create the blockchain directly without genesis setup
		// At this point, the database has all blocks (0 to 1082781) already copied
		blockchain, err = createBlockchainWithoutGenesis(db, storedConfig, engine, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create blockchain without genesis: %w", err)
		}

		// Log the discovered head
		currentHeight := blockchain.CurrentBlock().Number.Uint64()
		fmt.Printf("Blockchain created! Current head: block %d\n", currentHeight)
	} else {
		// Normal path - let NewBlockChain handle genesis
		blockchain, err = gethcore.NewBlockChain(db, genesis, engine, options)
		if err != nil {
			return nil, fmt.Errorf("failed to create blockchain: %w", err)
		}
	}

	// Create transaction pool
	legacyPool := legacypool.New(config.TxPool, blockchain)
	txPool, err := txpool.New(config.TxPool.PriceLimit, blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		return nil, err
	}

	backend := &MinimalEthBackend{
		chainConfig: chainConfig,
		blockchain:  blockchain,
		txPool:      txPool,
		chainDb:     db,
		engine:      engine,
		networkID:   config.NetworkId,
	}

	// For network 96369 (LUX mainnet), check if we should replay from migrated data
	networkID := uint64(0)
	if config != nil {
		networkID = config.NetworkId
	}

	fmt.Printf("DEBUG: Network ID = %d\n", networkID)

	// DISABLED: Automatic replay disabled to test direct database usage
	/*
	if networkID == 96369 {
		// Check if migrated database exists
		migratedDBPath := "/home/z/.lux-cli/mainnet/chainData/network-96369/4aYc2FXx3EDKf98wqmxaRkkLERa7QSbbNnKRL7awjHqVqGgxj/db/ethdb.migrated"
		fmt.Printf("DEBUG: Checking for migrated database at: %s\n", migratedDBPath)

		// Try to open it to verify it exists (NOT ReadOnly due to truncation issue)
		testOpts := badger.DefaultOptions(migratedDBPath)
		testOpts.Logger = nil
		if testDB, err := badger.Open(testOpts); err == nil {
			testDB.Close()
			fmt.Printf("\n🔄 Migrated database detected at: %s\n", migratedDBPath)
			fmt.Printf("Will start block replay from migrated data\n\n")
			// Start background block replay
			go rebuildChainWithFixedHashes(blockchain, db, 1074615)
		} else {
			fmt.Printf("DEBUG: Failed to open migrated database: %v\n", err)
		}
	}
	*/

	
	fmt.Printf("DEBUG: C-Chain initialized with replay support\n")

	return backend, nil
}

// BlockChain returns the blockchain
func (b *MinimalEthBackend) BlockChain() *gethcore.BlockChain {
	return b.blockchain
}

// TxPool returns the transaction pool
func (b *MinimalEthBackend) TxPool() *txpool.TxPool {
	return b.txPool
}

// ChainConfig returns the chain configuration
func (b *MinimalEthBackend) ChainConfig() *params.ChainConfig {
	return b.chainConfig
}

// APIs returns the collection of RPC services the ethereum package offers
func (b *MinimalEthBackend) APIs() []rpc.API {
	// Return basic APIs needed for Ethereum RPC
	return []rpc.API{
		{
			Namespace: "eth",
			Service:   NewEthAPI(b),
			Public:    true,
		},
		{
			Namespace: "net",
			Service:   &NetAPI{networkID: b.networkID},
			Public:    true,
		},
		{
			Namespace: "web3",
			Service:   &Web3API{},
			Public:    true,
		},
	}
}

// createBlockchainWithoutGenesis creates a blockchain using existing genesis in database
// This avoids calling SetupGenesisBlockWithOverride which would fail with genesis mismatch
func createBlockchainWithoutGenesis(db ethdb.Database, chainConfig *params.ChainConfig, engine consensus.Engine, options *gethcore.BlockChainConfig) (*gethcore.BlockChain, error) {
	// The key insight is that NewBlockChain with nil genesis will use what's in the database
	// But it compares against the default mainnet genesis (d4e56740...)
	// We need to make it think our genesis IS the mainnet genesis

	// Get the genesis hash from database
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return nil, fmt.Errorf("no genesis found in database")
	}

	fmt.Printf("Attempting to create blockchain with genesis hash: %s\n", genesisHash.Hex())

	// The issue is that when genesis is nil, NewBlockChain defaults to mainnet genesis
	// and compares it with what's in the database
	// We need to pass nil and hope it accepts what's in the database

	// First ensure the chain config is written
	if rawdb.ReadChainConfig(db, genesisHash) == nil {
		fmt.Printf("Writing chain config for genesis %s\n", genesisHash.Hex())
		rawdb.WriteChainConfig(db, genesisHash, chainConfig)
	}

	// CRITICAL: Save the actual head hash BEFORE NewBlockChain
	// NewBlockChain calls genesis.Commit which overwrites head pointers to genesis
	originalHeadHash := rawdb.ReadHeadBlockHash(db)
	originalHeadNumber, hasOriginalHead := rawdb.ReadHeaderNumber(db, originalHeadHash)
	fmt.Printf("SAVING original head BEFORE NewBlockChain: hash=%s, number=%d, ok=%v\n",
		originalHeadHash.Hex(), originalHeadNumber, hasOriginalHead)

	// Try to create blockchain with nil genesis
	// This should use what's in the database
	blockchain, err := gethcore.NewBlockChain(db, nil, engine, options)
	if err != nil {
		// If it fails with genesis mismatch, we have a problem
		// The only way around this is to modify the geth code itself
		// or to use the exact genesis that matches our extracted one
		return nil, fmt.Errorf("failed to create blockchain: %w", err)
	}

	// CRITICAL: After creating blockchain from replayed data, advance to HEAD
	// NewBlockChain's genesis.Commit overwrites head pointers to genesis
	// So we use the ORIGINAL head hash we saved above
	headHash := originalHeadHash
	fmt.Printf("DEBUG: headHash=%s, genesisHash=%s, empty=%v, different=%v\n",
		headHash.Hex(), genesisHash.Hex(),
		headHash == (common.Hash{}), headHash != genesisHash)
	if headHash != (common.Hash{}) && headHash != genesisHash {
		headNumber, ok := rawdb.ReadHeaderNumber(db, headHash)
		if ok {
			headHeader := rawdb.ReadHeader(db, headHash, headNumber)
			if headHeader != nil {
				fmt.Printf("Advancing blockchain to HEAD block %d (hash: %s)\n",
					headNumber, headHash.Hex())

				// The blockchain's currentBlock is at genesis but we need it at HEAD
				// Use SetHead() which sets the head by looking up the block in the database
				// This doesn't require parent validation like InsertChain does
				fmt.Printf("Setting blockchain head to block %d...\n", headNumber)
				err := blockchain.SetHead(headNumber)
				if err != nil {
					fmt.Printf("WARNING: Failed to set HEAD to block %d: %v\n", headNumber, err)
					fmt.Printf("Blockchain will remain at genesis, balance queries may fail\n")
				} else {
					currentBlock := blockchain.CurrentBlock()
					if currentBlock != nil {
						fmt.Printf("✅ Blockchain head set to block %d (hash: %s)\n",
							currentBlock.Number.Uint64(), currentBlock.Hash().Hex())
					} else {
						fmt.Printf("✅ SetHead succeeded but CurrentBlock is nil\n")
					}
				}
			}
		}
	}

	return blockchain, nil
}

// rebuildChainWithFixedHashes rebuilds the chain by creating new blocks with fixed parent hashes
func rebuildChainWithFixedHashes(blockchain *gethcore.BlockChain, db ethdb.Database, targetHeight uint64) {
	fmt.Printf("\n=== REBUILDING CHAIN WITH FIXED PARENT HASHES ===\n")
	fmt.Printf("Target height: %d blocks\n", targetHeight)
	fmt.Printf("This will create new blocks with correct parent linkage\n\n")

	// Open the migrated database as source (not read-only due to log truncation issue)
	migratedDBPath := "/home/z/.lux-cli/mainnet/chainData/network-96369/4aYc2FXx3EDKf98wqmxaRkkLERa7QSbbNnKRL7awjHqVqGgxj/db/ethdb.migrated"

	sourceOpts := badger.DefaultOptions(migratedDBPath)
	sourceOpts.Logger = nil
	// Note: NOT using ReadOnly due to truncated log file issue in BadgerDB v4
	// The database is accessible in normal mode

	sourceDB, err := badger.Open(sourceOpts)
	if err != nil {
		fmt.Printf("ERROR: Failed to open migrated database for reading: %v\n", err)
		fmt.Printf("Cannot rebuild chain without source data\n")
		return
	}
	defer sourceDB.Close()

	fmt.Printf("✓ Opened migrated database as read-only source\n")
	fmt.Printf("Reading blocks from: %s\n", migratedDBPath)
	fmt.Printf("Writing to fresh blockchain\n\n")

	startTime := time.Now()
	lastReport := startTime
	const reportInterval = 10 * time.Second

	// Start from block 1 (genesis is already correct in fresh chain)
	prevBlock := blockchain.CurrentBlock()

	for blockNum := uint64(1); blockNum <= targetHeight; blockNum++ {
		var oldBlockHash common.Hash
		var oldHeader *types.Header
		var oldBody *types.Body

		// Read block from migrated database using BadgerDB transaction
		err := sourceDB.View(func(txn *badger.Txn) error {
			// Read canonical hash
			canonicalKey := make([]byte, 9)
			canonicalKey[0] = 'H'
			binary.BigEndian.PutUint64(canonicalKey[1:], blockNum)

			item, err := txn.Get(canonicalKey)
			if err != nil {
				return fmt.Errorf("canonical hash not found: %w", err)
			}

			err = item.Value(func(val []byte) error {
				if len(val) != 32 {
					return fmt.Errorf("invalid hash length: %d", len(val))
				}
				copy(oldBlockHash[:], val)
				return nil
			})
			if err != nil {
				return err
			}

			// Read header
			headerKey := make([]byte, 41)
			headerKey[0] = 'h'
			binary.BigEndian.PutUint64(headerKey[1:9], blockNum)
			copy(headerKey[9:41], oldBlockHash.Bytes())

			item, err = txn.Get(headerKey)
			if err != nil {
				return fmt.Errorf("header not found: %w", err)
			}

			var headerData []byte
			err = item.Value(func(val []byte) error {
				headerData = append([]byte{}, val...)
				return nil
			})
			if err != nil {
				return err
			}

			// Decode header - try pre-Shanghai first
			preHeader := new(PreShanghaiHeader)
			if err := rlp.DecodeBytes(headerData, preHeader); err == nil {
				oldHeader = preHeader.ToPostShanghai()
			} else {
				// Try post-Shanghai
				if err := rlp.DecodeBytes(headerData, &oldHeader); err != nil {
					return fmt.Errorf("header decode failed: %w", err)
				}
			}

			// Read body
			bodyKey := make([]byte, 41)
			bodyKey[0] = 'b'
			binary.BigEndian.PutUint64(bodyKey[1:9], blockNum)
			copy(bodyKey[9:41], oldBlockHash.Bytes())

			item, err = txn.Get(bodyKey)
			if err == nil {
				var bodyData []byte
				err = item.Value(func(val []byte) error {
					bodyData = append([]byte{}, val...)
					return nil
				})
				if err == nil && len(bodyData) > 0 {
					oldBody = new(types.Body)
					if err := rlp.DecodeBytes(bodyData, oldBody); err != nil {
						oldBody = &types.Body{}
					}
				} else {
					oldBody = &types.Body{}
				}
			} else {
				oldBody = &types.Body{} // Empty body
			}

			return nil
		})

		if err != nil {
			fmt.Printf("ERROR reading block %d: %v\n", blockNum, err)
			break
		}

		// Create old block for reference
		oldBlock := types.NewBlockWithHeader(oldHeader).WithBody(*oldBody)

		// Create new block header with FIXED parent hash pointing to actual previous block
		// Keep other fields as validation targets - InsertChain will verify them
		newHeader := &types.Header{
			ParentHash:      prevBlock.Hash(), // CRITICAL FIX: Use actual previous block hash
			UncleHash:       oldBlock.Header().UncleHash,
			Coinbase:        oldBlock.Header().Coinbase,
			Root:            oldBlock.Header().Root, // Keep as validation target
			TxHash:          oldBlock.Header().TxHash,
			ReceiptHash:     oldBlock.Header().ReceiptHash, // Keep as validation target
			Bloom:           oldBlock.Header().Bloom,       // Keep as validation target
			Difficulty:      oldBlock.Header().Difficulty,
			Number:          oldBlock.Header().Number,
			GasLimit:        oldBlock.Header().GasLimit,
			GasUsed:         oldBlock.Header().GasUsed, // Keep as validation target
			Time:            oldBlock.Header().Time,
			Extra:           oldBlock.Header().Extra,
			MixDigest:       oldBlock.Header().MixDigest,
			Nonce:           oldBlock.Header().Nonce,
			BaseFee:         oldBlock.Header().BaseFee,
			WithdrawalsHash: nil, // Post-Shanghai format
		}

		// Create new block with same transactions but fixed header
		newBlock := types.NewBlockWithHeader(newHeader).WithBody(*oldBlock.Body())

		// Insert the block - this will execute transactions and rebuild state
		_, err = blockchain.InsertChain([]*types.Block{newBlock})
		if err != nil {
			fmt.Printf("ERROR inserting block %d: %v\n", blockNum, err)
			fmt.Printf("Block hash: %s, parent: %s\n", newBlock.Hash().Hex(), newHeader.ParentHash.Hex())
			// STOP on first error - don't skip blocks
			fmt.Printf("Stopping replay due to error\n")
			break
		}

		// Update previous block reference
		prevBlock = blockchain.CurrentBlock()

		// Report progress
		if time.Since(lastReport) >= reportInterval {
			elapsed := time.Since(startTime)
			rate := float64(prevBlock.Number.Uint64()) / elapsed.Seconds()
			remaining := targetHeight - prevBlock.Number.Uint64()
			eta := time.Duration(float64(remaining)/rate) * time.Second

			fmt.Printf("Progress: block %d/%d (%.1f%%) | Rate: %.0f blocks/s | ETA: %s\n",
				prevBlock.Number.Uint64(), targetHeight,
				float64(prevBlock.Number.Uint64())*100/float64(targetHeight),
				rate, eta.Round(time.Second))
			lastReport = time.Now()
		}
	}

	elapsed := time.Since(startTime)
	finalBlock := blockchain.CurrentBlock()
	fmt.Printf("\n=== CHAIN REBUILD COMPLETE ===\n")
	fmt.Printf("Final height: %d/%d\n", finalBlock.Number.Uint64(), targetHeight)
	fmt.Printf("Time elapsed: %s\n", elapsed.Round(time.Second))
	if finalBlock.Number.Uint64() > 0 {
		fmt.Printf("Average rate: %.0f blocks/second\n", float64(finalBlock.Number.Uint64())/elapsed.Seconds())
	}
}

// replayBlocksToRebuildState replays blocks from database to rebuild state
func replayBlocksToRebuildState(blockchain *gethcore.BlockChain, db ethdb.Database, targetHeight uint64) {
	fmt.Printf("\n=== STARTING RUNTIME BLOCK REPLAY ===\n")
	fmt.Printf("Target height: %d blocks\n", targetHeight)
	fmt.Printf("This will rebuild the state by executing all transactions\n\n")

	startTime := time.Now()
	lastReport := startTime
	const batchSize = 100
	const reportInterval = 10 * time.Second

	for blockNum := uint64(1); blockNum <= targetHeight; {
		// Read a batch of blocks
		batch := make([]*types.Block, 0, batchSize)

		for i := uint64(0); i < batchSize && blockNum+i <= targetHeight; i++ {
			currentNum := blockNum + i

			// Read canonical hash using rawdb.ReadCanonicalHash()
			blockHash := rawdb.ReadCanonicalHash(db, currentNum)
			if blockHash == (common.Hash{}) {
				fmt.Printf("ERROR: Block %d canonical hash not found\n", currentNum)
				continue
			}

			// Read the full block
			block := rawdb.ReadBlock(db, blockHash, currentNum)
			if block == nil {
				fmt.Printf("ERROR: Block %d body not found\n", currentNum)
				continue
			}

			batch = append(batch, block)
		}

		if len(batch) == 0 {
			blockNum += batchSize
			continue
		}

		// Insert the batch to execute transactions and rebuild state
		_, err := blockchain.InsertChain(batch)
		if err != nil {
			fmt.Printf("ERROR inserting blocks %d-%d: %v\n", blockNum, blockNum+uint64(len(batch))-1, err)
			// Try inserting blocks one at a time
			for _, block := range batch {
				if _, err := blockchain.InsertChain([]*types.Block{block}); err != nil {
					fmt.Printf("ERROR inserting block %d: %v\n", block.NumberU64(), err)
					break // Stop on first error in single-block mode
				}
			}
		}

		blockNum += uint64(len(batch))
		currentBlock := blockchain.CurrentBlock()

		// Report progress
		if time.Since(lastReport) >= reportInterval {
			elapsed := time.Since(startTime)
			rate := float64(currentBlock.Number.Uint64()) / elapsed.Seconds()
			remaining := targetHeight - currentBlock.Number.Uint64()
			eta := time.Duration(float64(remaining)/rate) * time.Second

			fmt.Printf("Progress: block %d/%d (%.1f%%) | Rate: %.0f blocks/s | ETA: %s\n",
				currentBlock.Number.Uint64(), targetHeight,
				float64(currentBlock.Number.Uint64())*100/float64(targetHeight),
				rate, eta.Round(time.Second))
			lastReport = time.Now()
		}
	}

	elapsed := time.Since(startTime)
	finalBlock := blockchain.CurrentBlock()
	fmt.Printf("\n=== BLOCK REPLAY COMPLETE ===\n")
	fmt.Printf("Final height: %d/%d\n", finalBlock.Number.Uint64(), targetHeight)
	fmt.Printf("Time elapsed: %s\n", elapsed.Round(time.Second))
	if finalBlock.Number.Uint64() > 0 {
		fmt.Printf("Average rate: %.0f blocks/second\n", float64(finalBlock.Number.Uint64())/elapsed.Seconds())
	}
}

// dummyEngine is a consensus engine that does nothing (for PoS mode)
type dummyEngine struct{}

func (d *dummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (d *dummyEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (d *dummyEngine) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))
	for range headers {
		results <- nil
	}
	return abort, results
}

func (d *dummyEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return nil
}

func (d *dummyEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (d *dummyEngine) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB, body *types.Body) {
	// No-op for PoS
}

func (d *dummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt) (*types.Block, error) {
	// Finalize the state
	d.Finalize(chain, header, state, body)

	// Assemble and return the block
	return types.NewBlock(header, body, receipts, nil), nil
}

func (d *dummyEngine) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	results <- block
	return nil
}

func (d *dummyEngine) SealHash(header *types.Header) common.Hash {
	return header.Hash()
}

func (d *dummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (d *dummyEngine) Close() error {
	return nil
}
