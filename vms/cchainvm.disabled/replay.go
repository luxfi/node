// (c) 2024 Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/rlp"
)

// SubnetEVMHeader is the SubnetEVM header format with additional optional fields
type SubnetEVMHeader struct {
	ParentHash      common.Hash      `json:"parentHash"       gencodec:"required"`
	UncleHash       common.Hash      `json:"sha3Uncles"       gencodec:"required"`
	Coinbase        common.Address   `json:"miner"`
	Root            common.Hash      `json:"stateRoot"        gencodec:"required"`
	TxHash          common.Hash      `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash     common.Hash      `json:"receiptsRoot"     gencodec:"required"`
	Bloom           types.Bloom      `json:"logsBloom"        gencodec:"required"`
	Difficulty      *big.Int         `json:"difficulty"       gencodec:"required"`
	Number          *big.Int         `json:"number"           gencodec:"required"`
	GasLimit        uint64           `json:"gasLimit"         gencodec:"required"`
	GasUsed         uint64           `json:"gasUsed"          gencodec:"required"`
	Time            uint64           `json:"timestamp"        gencodec:"required"`
	Extra           []byte           `json:"extraData"        gencodec:"required"`
	MixDigest       common.Hash      `json:"mixHash"`
	Nonce           types.BlockNonce `json:"nonce"`
	BaseFee         *big.Int         `json:"baseFeePerGas"    rlp:"optional"`
	WithdrawalsHash common.Hash      `json:"withdrawalsRoot"  rlp:"optional"`
	BlockGasCost    *big.Int         `rlp:"optional"`
	ExtDataHash     common.Hash      `rlp:"optional"`
}

// DatabaseType represents the type of database to replay from
type DatabaseType string

const (
	// StandardDB is a regular geth/coreth database without namespacing
	StandardDB DatabaseType = "standard"
	// NamespacedDB is a SubnetEVM database with 32-byte namespace prefix
	NamespacedDB DatabaseType = "namespaced"
	// AutoDetect will attempt to detect the database type
	AutoDetect DatabaseType = "auto"
)

// UnifiedReplayConfig holds configuration for database replay
type UnifiedReplayConfig struct {
	SourcePath               string       // Path to source database
	DatabaseType             DatabaseType // Type of source database
	Namespace                []byte       // Namespace for namespaced databases (optional)
	TestMode                 bool         // If true, only replay first 100 blocks
	TestLimit                uint64       // Number of blocks to replay in test mode
	CopyAllState             bool         // If true, copy all state data (can be large)
	MaxStateNodes            uint64       // Maximum state nodes to copy (0 = unlimited)
	ExtractGenesisFromSource bool         // If true, extract genesis from block 0
	ParallelWorkers          int          // Number of parallel workers for processing (default: 8)
	BatchSize                int          // Batch size for database writes (default: 100000)
}

// UnifiedReplayer handles replaying blocks from various database formats
type UnifiedReplayer struct {
	config       *UnifiedReplayConfig
	targetDB     ethdb.Database
	blockchain   *core.BlockChain
	sourceDB     *pebble.DB
	namespace    []byte
	isNamespaced bool
}

// NewUnifiedReplayer creates a new unified database replayer
func NewUnifiedReplayer(config *UnifiedReplayConfig, targetDB ethdb.Database, blockchain *core.BlockChain) (*UnifiedReplayer, error) {
	if config.SourcePath == "" {
		return nil, fmt.Errorf("source database path is required")
	}

	// Set defaults
	if config.TestMode && config.TestLimit == 0 {
		config.TestLimit = 100
	}
	if config.MaxStateNodes == 0 && !config.CopyAllState {
		config.MaxStateNodes = 1000000 // Default to 1M nodes if not copying all
	}
	if config.ParallelWorkers == 0 {
		config.ParallelWorkers = 8 // Default to 8 parallel workers
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100000 // Default to 100k batch size
	}

	sourceDB, err := pebble.Open(config.SourcePath, &pebble.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("CRITICAL: Cannot open source database at %s: %v", config.SourcePath, err)
	}
	log.Printf("Successfully opened source database at %s", config.SourcePath)

	replayer := &UnifiedReplayer{
		config:     config,
		targetDB:   targetDB,
		blockchain: blockchain,
		sourceDB:   sourceDB,
	}

	// Auto-detect database type if needed
	if config.DatabaseType == AutoDetect {
		if err := replayer.detectDatabaseType(); err != nil {
			sourceDB.Close()
			return nil, err
		}
	} else {
		replayer.isNamespaced = (config.DatabaseType == NamespacedDB)
		if replayer.isNamespaced && len(config.Namespace) == 0 {
			// Use default LUX subnet namespace
			replayer.namespace = []byte{
				0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
				0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
				0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
				0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
			}
		} else {
			replayer.namespace = config.Namespace
		}
	}

	return replayer, nil
}

// detectDatabaseType attempts to detect if the database is namespaced or standard
func (r *UnifiedReplayer) detectDatabaseType() error {
	log.Printf("Auto-detecting database type...")

	// Check for standard geth database markers
	if _, closer, err := r.sourceDB.Get([]byte("LastBlock")); err == nil {
		if closer != nil {
			closer.Close()
		}
		r.isNamespaced = false
		log.Printf("Detected standard geth database")
		return nil
	}

	// Check for namespaced database with common namespace
	testNamespace := []byte{
		0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
		0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
		0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
		0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
	}

	// Look for headers with namespace prefix
	headerPrefix := append(testNamespace, 'h')
	iter, err := r.sourceDB.NewIter(&pebble.IterOptions{
		LowerBound: headerPrefix,
		UpperBound: append(headerPrefix, 0xFF),
	})
	if err != nil {
		return fmt.Errorf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	if iter.First() && iter.Valid() {
		r.isNamespaced = true
		r.namespace = testNamespace
		log.Printf("Detected namespaced SubnetEVM database")
		return nil
	}

	return fmt.Errorf("unable to detect database type")
}

// ExtractGenesis extracts the genesis block from the source database
func (r *UnifiedReplayer) ExtractGenesis() (*types.Block, error) {
	if !r.isNamespaced {
		return nil, fmt.Errorf("genesis extraction only supported for namespaced databases")
	}

	// Get block 0 (genesis)
	// Canonical hash key for block 0: namespace + 'h' + blocknum(8) + 'n'
	canonicalKey := append([]byte(nil), r.namespace...)
	canonicalKey = append(canonicalKey, 'h')
	canonicalKey = append(canonicalKey, encodeBlockNumber(0)...)
	canonicalKey = append(canonicalKey, 'n')

	hashData, closer, err := r.sourceDB.Get(canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("genesis block hash not found: %v", err)
	}
	defer closer.Close()

	if len(hashData) != 32 {
		return nil, fmt.Errorf("invalid genesis hash length: %d", len(hashData))
	}

	genesisHash := common.BytesToHash(hashData)
	log.Printf("Found genesis hash in source: %s", genesisHash.Hex())

	// Get the header: namespace + 'h' + blocknum(8) + hash(32)
	headerKey := append([]byte(nil), r.namespace...)
	headerKey = append(headerKey, 'h')
	headerKey = append(headerKey, encodeBlockNumber(0)...)
	headerKey = append(headerKey, genesisHash.Bytes()...)

	headerData, closer2, err := r.sourceDB.Get(headerKey)
	if err != nil {
		return nil, fmt.Errorf("genesis header not found: %v", err)
	}
	defer closer2.Close()

	// Try to decode header
	var header types.Header
	if err := rlp.DecodeBytes(headerData, &header); err != nil {
		// Try SubnetEVM format
		var subnetHeader SubnetEVMHeader
		if err2 := rlp.DecodeBytes(headerData, &subnetHeader); err2 != nil {
			return nil, fmt.Errorf("failed to decode genesis header: %v", err)
		}
		// Convert to standard header
		header = types.Header{
			ParentHash:  subnetHeader.ParentHash,
			UncleHash:   subnetHeader.UncleHash,
			Coinbase:    subnetHeader.Coinbase,
			Root:        subnetHeader.Root,
			TxHash:      subnetHeader.TxHash,
			ReceiptHash: subnetHeader.ReceiptHash,
			Bloom:       subnetHeader.Bloom,
			Difficulty:  subnetHeader.Difficulty,
			Number:      subnetHeader.Number,
			GasLimit:    subnetHeader.GasLimit,
			GasUsed:     subnetHeader.GasUsed,
			Time:        subnetHeader.Time,
			Extra:       subnetHeader.Extra,
			MixDigest:   subnetHeader.MixDigest,
			Nonce:       subnetHeader.Nonce,
			BaseFee:     subnetHeader.BaseFee,
		}
	}

	// Get the body: namespace + 'b' + blocknum(8) + hash(32)
	bodyKey := append([]byte(nil), r.namespace...)
	bodyKey = append(bodyKey, 'b')
	bodyKey = append(bodyKey, encodeBlockNumber(0)...)
	bodyKey = append(bodyKey, genesisHash.Bytes()...)

	bodyData, closer3, err := r.sourceDB.Get(bodyKey)
	if err != nil {
		// Genesis might not have a body, that's ok
		return types.NewBlockWithHeader(&header), nil
	}
	defer closer3.Close()

	var body types.Body
	if err := rlp.DecodeBytes(bodyData, &body); err != nil {
		// If body decode fails, just use empty body
		return types.NewBlockWithHeader(&header), nil
	}

	genesis := types.NewBlockWithHeader(&header).WithBody(body)
	log.Printf("Extracted genesis block: number=%d, hash=%s, stateRoot=%s",
		genesis.NumberU64(), genesis.Hash().Hex(), genesis.Root().Hex())

	return genesis, nil
}

// Run executes the database replay
func (r *UnifiedReplayer) Run() error {
	if r.isNamespaced {
		log.Printf("Starting namespaced database replay from %s", r.config.SourcePath)
		return r.replayNamespacedDatabase()
	} else {
		log.Printf("Starting standard database replay from %s", r.config.SourcePath)
		return r.replayStandardDatabase()
	}
}

// replayStandardDatabase replays a standard geth/coreth database
func (r *UnifiedReplayer) replayStandardDatabase() error {
	// Implementation for standard database replay
	// This would be similar to the existing replay.go logic
	log.Printf("Standard database replay not yet implemented")
	return fmt.Errorf("standard database replay not yet implemented")
}

// replayNamespacedDatabase replays a namespaced SubnetEVM database
func (r *UnifiedReplayer) replayNamespacedDatabase() error {
	startTime := time.Now()

	log.Printf("Namespace: %x", r.namespace)

	if r.config.TestMode {
		log.Printf("🧪 TEST MODE: Limiting to %d blocks", r.config.TestLimit)
	}

	// Step 1: Collect canonical block mappings
	canonicalHashes, maxBlockNum, err := r.collectCanonicalMappings()
	if err != nil {
		return err
	}

	if len(canonicalHashes) == 0 {
		return fmt.Errorf("no canonical blocks found in database")
	}

	log.Printf("Found %d canonical blocks, max height: %d", len(canonicalHashes), maxBlockNum)

	// Step 2: Fetch headers and bodies
	headers, bodies, err := r.fetchBlockData(canonicalHashes, maxBlockNum)
	if err != nil {
		return err
	}

	log.Printf("Fetched %d headers and %d bodies", len(headers), len(bodies))

	// Step 3: Copy state data if requested
	if r.config.CopyAllState || r.config.TestMode {
		if err := r.copyStateData(headers); err != nil {
			return err
		}
	}

	// Step 4: Replay blocks to target database
	replayedCount, err := r.replayBlocks(headers, bodies, maxBlockNum)
	if err != nil {
		return err
	}

	log.Printf("✅ Database replay complete!")
	log.Printf("   Blocks replayed: %d", replayedCount)
	log.Printf("   Time taken: %v", time.Since(startTime))
	if replayedCount > 0 {
		rate := float64(replayedCount) / time.Since(startTime).Seconds()
		log.Printf("   Rate: %.1f blocks/sec", rate)
	}

	return nil
}

// collectCanonicalMappings finds all canonical block number to hash mappings
func (r *UnifiedReplayer) collectCanonicalMappings() (map[uint64]common.Hash, uint64, error) {
	canonicalHashes := make(map[uint64]common.Hash)
	var maxBlockNum uint64

	// Look for canonical mappings: namespace + 'h' + blocknum(8) + 'n' -> hash(32)
	headerPrefix := append(r.namespace, 'h')

	iter, err := r.sourceDB.NewIter(&pebble.IterOptions{
		LowerBound: headerPrefix,
		UpperBound: append(headerPrefix, 0xFF),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		val, _ := iter.ValueAndErr()

		// Check for canonical mapping format
		if len(key) == 42 && bytes.Equal(key[:32], r.namespace) && key[32] == 'h' && key[41] == 'n' {
			blockNum := binary.BigEndian.Uint64(key[33:41])

			// Apply test limit if in test mode
			if r.config.TestMode && blockNum > r.config.TestLimit {
				continue
			}

			if len(val) == 32 {
				hash := common.BytesToHash(val)
				canonicalHashes[blockNum] = hash
				if blockNum > maxBlockNum {
					maxBlockNum = blockNum
				}
			}
		}
	}

	return canonicalHashes, maxBlockNum, nil
}

// fetchBlockData retrieves headers and bodies for the canonical blocks
func (r *UnifiedReplayer) fetchBlockData(canonicalHashes map[uint64]common.Hash, maxBlockNum uint64) (map[uint64]*types.Header, map[common.Hash]*types.Body, error) {
	headers := make(map[uint64]*types.Header)
	bodies := make(map[common.Hash]*types.Body)

	// Fetch headers
	for blockNum, hash := range canonicalHashes {
		// Build header key: namespace + 'h' + blocknum(8) + hash(32)
		headerKey := append(r.namespace, 'h')
		headerKey = append(headerKey, encodeBlockNumber(blockNum)...)
		headerKey = append(headerKey, hash.Bytes()...)

		headerData, closer, err := r.sourceDB.Get(headerKey)
		if err == nil && closer != nil {
			// Try to decode as standard header
			var header types.Header
			if err := rlp.DecodeBytes(headerData, &header); err != nil {
				// Try SubnetEVM header format
				var subnetHeader SubnetEVMHeader
				if err2 := rlp.DecodeBytes(headerData, &subnetHeader); err2 == nil {
					// Convert to standard header
					header = types.Header{
						ParentHash:  subnetHeader.ParentHash,
						UncleHash:   subnetHeader.UncleHash,
						Coinbase:    subnetHeader.Coinbase,
						Root:        subnetHeader.Root,
						TxHash:      subnetHeader.TxHash,
						ReceiptHash: subnetHeader.ReceiptHash,
						Bloom:       subnetHeader.Bloom,
						Difficulty:  subnetHeader.Difficulty,
						Number:      subnetHeader.Number,
						GasLimit:    subnetHeader.GasLimit,
						GasUsed:     subnetHeader.GasUsed,
						Time:        subnetHeader.Time,
						Extra:       subnetHeader.Extra,
						MixDigest:   subnetHeader.MixDigest,
						Nonce:       subnetHeader.Nonce,
						BaseFee:     subnetHeader.BaseFee,
					}
				}
			}
			headers[blockNum] = &header
			closer.Close()
		}
	}

	// Fetch bodies
	bodyPrefix := append(r.namespace, 'b')
	iter, _ := r.sourceDB.NewIter(&pebble.IterOptions{
		LowerBound: bodyPrefix,
		UpperBound: append(bodyPrefix, 0xFF),
	})
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		val, _ := iter.ValueAndErr()

		if len(key) == 73 && key[32] == 'b' {
			blockHash := common.BytesToHash(key[41:73])
			var body types.Body
			if err := rlp.DecodeBytes(val, &body); err == nil {
				bodies[blockHash] = &body
			}
		}
	}

	return headers, bodies, nil
}

// copyStateData copies state trie nodes for the blocks being replayed
func (r *UnifiedReplayer) copyStateData(headers map[uint64]*types.Header) error {
	log.Printf("Copying state data for blocks...")

	// Collect unique state roots
	stateRoots := make(map[common.Hash]bool)
	for _, header := range headers {
		if header != nil {
			stateRoots[header.Root] = true
		}
	}

	log.Printf("Found %d unique state roots to copy", len(stateRoots))

	// Copy state nodes - optimized for parallel processing
	log.Printf("Copying state trie nodes for %d blocks...", len(stateRoots))
	copiedCount := 0
	processedNodes := make(map[string]bool)

	// Create a batch for faster writes
	batch := r.targetDB.NewBatch()
	batchSize := 0
	const maxBatchSize = 100000 // Increased batch size for better performance

	// Recursive function to copy a trie node and its children
	var copyTrieNode func(hash []byte, depth int) error
	copyTrieNode = func(hash []byte, depth int) error {
		if len(hash) != 32 {
			return nil
		}

		hashStr := hex.EncodeToString(hash)
		if processedNodes[hashStr] {
			return nil // Already processed
		}
		processedNodes[hashStr] = true

		// Build key: namespace + hash
		nodeKey := append([]byte(nil), r.namespace...)
		nodeKey = append(nodeKey, hash...)

		// Get node data from source
		nodeData, closer, err := r.sourceDB.Get(nodeKey)
		if err != nil {
			// This is CRITICAL - if we can't find a node, the state will be incomplete
			if depth == 0 {
				// Root nodes MUST exist
				return fmt.Errorf("CRITICAL: Root node %x not found in source database: %v", hash, err)
			}
			// For child nodes, log but continue (might be pruned)
			log.Printf("WARNING: Child node %x at depth %d not found: %v", hash, depth, err)
			return nil
		}
		defer closer.Close()

		// Add to batch (just the hash, no namespace)
		if err := batch.Put(hash, nodeData); err != nil {
			return fmt.Errorf("failed to batch node %x: %v", hash, err)
		}

		copiedCount++
		batchSize++

		// Write batch when it gets large enough
		if batchSize >= maxBatchSize {
			if err := batch.Write(); err != nil {
				log.Printf("Failed to write batch: %v", err)
			}
			batch.Reset()
			batchSize = 0
			log.Printf("Copied %d state trie nodes...", copiedCount)
		}

		// Parse node to find children and recurse
		var nodeList []interface{}
		if err := rlp.DecodeBytes(nodeData, &nodeList); err == nil {
			// Process branch and extension nodes recursively
			for _, item := range nodeList {
				if child, ok := item.([]byte); ok && len(child) == 32 {
					if err := copyTrieNode(child, depth+1); err != nil {
						return err
					}
				}
			}
		}

		return nil
	}

	// Copy state tries for each unique root
	for root := range stateRoots {
		log.Printf("Copying state trie from root %x", root)
		if err := copyTrieNode(root.Bytes(), 0); err != nil {
			// FAIL LOUDLY - we CANNOT continue with incomplete state
			return fmt.Errorf("FAILED TO COPY STATE: Root %x copy failed: %v", root, err)
		}
		log.Printf("Successfully copied tree for root %x", root)
	}

	// Write any remaining batch
	if batchSize > 0 {
		if err := batch.Write(); err != nil {
			log.Printf("Failed to write final batch: %v", err)
		}
	}

	log.Printf("Copied %d state trie nodes for %d blocks", copiedCount, len(stateRoots))
	return nil
}

// replayBlocks writes the blocks to the target database
func (r *UnifiedReplayer) replayBlocks(headers map[uint64]*types.Header, bodies map[common.Hash]*types.Body, maxBlockNum uint64) (int, error) {
	log.Printf("Replaying %d blocks to target database...", len(headers))
	replayedCount := 0

	// Process blocks in order
	for blockNum := uint64(0); blockNum <= maxBlockNum; blockNum++ {
		header, hasHeader := headers[blockNum]
		if !hasHeader {
			continue
		}

		// Get or create body
		body, hasBody := bodies[header.Hash()]
		if !hasBody {
			body = &types.Body{}
		}

		// Create block
		block := types.NewBlockWithHeader(header).WithBody(*body)

		// Write to target database
		rawdb.WriteBlock(r.targetDB, block)
		rawdb.WriteCanonicalHash(r.targetDB, block.Hash(), block.NumberU64())
		rawdb.WriteHeader(r.targetDB, header)
		rawdb.WriteBody(r.targetDB, block.Hash(), block.NumberU64(), body)
		rawdb.WriteReceipts(r.targetDB, block.Hash(), block.NumberU64(), nil)

		// Update head periodically
		if blockNum == maxBlockNum || blockNum%10000 == 0 {
			rawdb.WriteHeadBlockHash(r.targetDB, block.Hash())
			rawdb.WriteHeadHeaderHash(r.targetDB, block.Hash())
			rawdb.WriteHeadFastBlockHash(r.targetDB, block.Hash())
		}

		replayedCount++
		if replayedCount%10000 == 0 {
			log.Printf("Replayed %d blocks...", replayedCount)
		}
	}

	// Set final head
	if replayedCount > 0 {
		for blockNum := maxBlockNum; blockNum >= 0; blockNum-- {
			if header, exists := headers[blockNum]; exists {
				log.Printf("Setting final head to block %d (hash: %s)", blockNum, header.Hash().Hex())
				rawdb.WriteHeadBlockHash(r.targetDB, header.Hash())
				rawdb.WriteHeadHeaderHash(r.targetDB, header.Hash())
				rawdb.WriteHeadFastBlockHash(r.targetDB, header.Hash())
				rawdb.WriteLastPivotNumber(r.targetDB, blockNum)
				break
			}
		}
	}

	return replayedCount, nil
}

// Close closes the replayer
func (r *UnifiedReplayer) Close() error {
	if r.sourceDB != nil {
		return r.sourceDB.Close()
	}
	return nil
}

// ParallelReplay performs optimized parallel replay of the entire database
func (r *UnifiedReplayer) ParallelReplay() error {
	startTime := time.Now()
	log.Printf("Starting OPTIMIZED parallel database replay with %d workers", r.config.ParallelWorkers)

	// Use all CPU cores if not in test mode
	if !r.config.TestMode {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	// First, get all canonical blocks in parallel
	log.Printf("Phase 1: Discovering all canonical blocks...")
	canonicalBlocks, maxHeight := r.discoverCanonicalBlocksParallel()
	log.Printf("Found %d canonical blocks, max height: %d", len(canonicalBlocks), maxHeight)

	if r.config.TestMode && maxHeight > r.config.TestLimit {
		maxHeight = r.config.TestLimit
		log.Printf("TEST MODE: Limiting to %d blocks", maxHeight)
	}

	// Phase 2: Fetch headers and bodies in parallel batches
	log.Printf("Phase 2: Fetching headers and bodies in parallel...")
	headers, bodies := r.fetchBlockDataParallel(canonicalBlocks, maxHeight)

	// Phase 3: Copy state data with parallel workers
	log.Printf("Phase 3: Copying state data with parallel workers...")
	if err := r.copyStateDataParallel(headers); err != nil {
		return fmt.Errorf("state copy failed: %v", err)
	}

	// Phase 4: Write blocks to database in large batches
	log.Printf("Phase 4: Writing blocks in optimized batches...")
	replayedCount, err := r.replayBlocksBatched(headers, bodies, maxHeight)
	if err != nil {
		return fmt.Errorf("block replay failed: %v", err)
	}

	elapsed := time.Since(startTime)
	rate := float64(replayedCount) / elapsed.Seconds()
	log.Printf("✅ OPTIMIZED replay complete! Replayed %d blocks in %v (%.1f blocks/sec)",
		replayedCount, elapsed, rate)

	return nil
}

// discoverCanonicalBlocksParallel discovers all canonical blocks in parallel
func (r *UnifiedReplayer) discoverCanonicalBlocksParallel() (map[uint64]common.Hash, uint64) {
	canonicalBlocks := make(map[uint64]common.Hash)
	var maxHeight uint64
	var mu sync.Mutex

	// Create workers to scan ranges in parallel
	numWorkers := r.config.ParallelWorkers
	blockRangeSize := uint64(100000) // Each worker scans 100k blocks

	var wg sync.WaitGroup
	for worker := 0; worker < numWorkers; worker++ {
		wg.Add(1)
		startBlock := uint64(worker) * blockRangeSize
		endBlock := startBlock + blockRangeSize

		go func(start, end uint64) {
			defer wg.Done()

			for blockNum := start; blockNum < end; blockNum++ {
				// Build canonical key
				canonicalKey := append([]byte(nil), r.namespace...)
				canonicalKey = append(canonicalKey, 'h')
				canonicalKey = append(canonicalKey, encodeBlockNumber(blockNum)...)
				canonicalKey = append(canonicalKey, 'n')

				if hashData, closer, err := r.sourceDB.Get(canonicalKey); err == nil {
					if len(hashData) == 32 {
						hash := common.BytesToHash(hashData)
						mu.Lock()
						canonicalBlocks[blockNum] = hash
						if blockNum > maxHeight {
							maxHeight = blockNum
						}
						mu.Unlock()
					}
					closer.Close()
				}
			}
		}(startBlock, endBlock)
	}

	wg.Wait()
	return canonicalBlocks, maxHeight
}

// fetchBlockDataParallel fetches headers and bodies in parallel
func (r *UnifiedReplayer) fetchBlockDataParallel(canonicalBlocks map[uint64]common.Hash, maxHeight uint64) (map[uint64]*types.Header, map[common.Hash]*types.Body) {
	headers := make(map[uint64]*types.Header)
	bodies := make(map[common.Hash]*types.Body)
	var headerMu, bodyMu sync.Mutex

	// Process blocks in chunks
	chunkSize := uint64(10000)
	numChunks := (maxHeight / chunkSize) + 1

	var wg sync.WaitGroup
	processedBlocks := int32(0)

	for chunk := uint64(0); chunk < numChunks; chunk++ {
		wg.Add(1)
		startBlock := chunk * chunkSize
		endBlock := startBlock + chunkSize
		if endBlock > maxHeight {
			endBlock = maxHeight + 1
		}

		go func(start, end uint64) {
			defer wg.Done()

			// Create local batch for this chunk
			localHeaders := make(map[uint64]*types.Header)
			localBodies := make(map[common.Hash]*types.Body)

			for blockNum := start; blockNum < end; blockNum++ {
				hash, exists := canonicalBlocks[blockNum]
				if !exists {
					continue
				}

				// Fetch header
				headerKey := append([]byte(nil), r.namespace...)
				headerKey = append(headerKey, 'h')
				headerKey = append(headerKey, encodeBlockNumber(blockNum)...)
				headerKey = append(headerKey, hash.Bytes()...)

				if headerData, closer, err := r.sourceDB.Get(headerKey); err == nil {
					var header types.Header
					if err := rlp.DecodeBytes(headerData, &header); err == nil {
						localHeaders[blockNum] = &header

						// Fetch body
						bodyKey := append([]byte(nil), r.namespace...)
						bodyKey = append(bodyKey, 'b')
						bodyKey = append(bodyKey, encodeBlockNumber(blockNum)...)
						bodyKey = append(bodyKey, hash.Bytes()...)

						if bodyData, bodyCloser, err := r.sourceDB.Get(bodyKey); err == nil {
							var body types.Body
							if err := rlp.DecodeBytes(bodyData, &body); err == nil {
								localBodies[hash] = &body
							}
							bodyCloser.Close()
						}
					}
					closer.Close()
				}

				// Update progress
				processed := atomic.AddInt32(&processedBlocks, 1)
				if processed%10000 == 0 {
					log.Printf("Fetched %d blocks...", processed)
				}
			}

			// Merge local results
			headerMu.Lock()
			for k, v := range localHeaders {
				headers[k] = v
			}
			headerMu.Unlock()

			bodyMu.Lock()
			for k, v := range localBodies {
				bodies[k] = v
			}
			bodyMu.Unlock()
		}(startBlock, endBlock)
	}

	wg.Wait()
	log.Printf("Fetched %d headers and %d bodies", len(headers), len(bodies))
	return headers, bodies
}

// copyStateDataParallel copies state data using parallel workers
func (r *UnifiedReplayer) copyStateDataParallel(headers map[uint64]*types.Header) error {
	// Collect unique state roots
	stateRoots := make(map[common.Hash]bool)
	for _, header := range headers {
		if header != nil {
			stateRoots[header.Root] = true
		}
	}

	log.Printf("Copying state for %d unique roots using %d workers", len(stateRoots), r.config.ParallelWorkers)

	// Create work queue
	workQueue := make(chan common.Hash, len(stateRoots))
	for root := range stateRoots {
		workQueue <- root
	}
	close(workQueue)

	// Track progress
	var copiedNodes int64
	var errorCount int32

	// Create parallel workers
	var wg sync.WaitGroup
	for i := 0; i < r.config.ParallelWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Each worker gets its own batch
			batch := r.targetDB.NewBatch()
			batchSize := 0
			processedNodes := make(map[string]bool)

			var copyNode func(hash []byte) error
			copyNode = func(hash []byte) error {
				if len(hash) != 32 {
					return nil
				}

				hashStr := hex.EncodeToString(hash)
				if processedNodes[hashStr] {
					return nil
				}
				processedNodes[hashStr] = true

				// Build key with namespace
				nodeKey := append([]byte(nil), r.namespace...)
				nodeKey = append(nodeKey, hash...)

				// Get node data
				nodeData, closer, err := r.sourceDB.Get(nodeKey)
				if err != nil {
					return nil // Node might not exist
				}
				defer closer.Close()

				// Add to batch
				batch.Put(hash, nodeData)
				batchSize++

				// Write batch when full
				if batchSize >= r.config.BatchSize {
					if err := batch.Write(); err != nil {
						atomic.AddInt32(&errorCount, 1)
						return err
					}
					batch.Reset()
					copied := atomic.AddInt64(&copiedNodes, int64(batchSize))
					if copied%100000 == 0 {
						log.Printf("Worker %d: Copied %d state nodes total", workerID, copied)
					}
					batchSize = 0
				}

				// Parse for children
				var nodeList []interface{}
				if err := rlp.DecodeBytes(nodeData, &nodeList); err == nil {
					for _, item := range nodeList {
						if child, ok := item.([]byte); ok && len(child) == 32 {
							if err := copyNode(child); err != nil {
								return err
							}
						}
					}
				}

				return nil
			}

			// Process work queue
			for root := range workQueue {
				if err := copyNode(root.Bytes()); err != nil {
					log.Printf("Worker %d: Error copying root %x: %v", workerID, root, err)
				}
			}

			// Flush remaining batch
			if batchSize > 0 {
				batch.Write()
				atomic.AddInt64(&copiedNodes, int64(batchSize))
			}
		}(i)
	}

	wg.Wait()

	if errorCount > 0 {
		return fmt.Errorf("encountered %d errors during state copy", errorCount)
	}

	log.Printf("Successfully copied %d state nodes", copiedNodes)
	return nil
}

// replayBlocksBatched writes blocks in large batches for maximum performance
func (r *UnifiedReplayer) replayBlocksBatched(headers map[uint64]*types.Header, bodies map[common.Hash]*types.Body, maxHeight uint64) (int, error) {
	log.Printf("Writing %d blocks to database in batches...", len(headers))

	batch := r.targetDB.NewBatch()
	batchSize := 0
	replayedCount := 0

	for blockNum := uint64(0); blockNum <= maxHeight; blockNum++ {
		header, hasHeader := headers[blockNum]
		if !hasHeader {
			continue
		}

		body, hasBody := bodies[header.Hash()]
		if !hasBody {
			body = &types.Body{}
		}

		// Create block
		block := types.NewBlockWithHeader(header).WithBody(*body)

		// Add all writes to batch
		rawdb.WriteBlock(batch, block)
		rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
		rawdb.WriteHeader(batch, header)
		rawdb.WriteBody(batch, block.Hash(), block.NumberU64(), body)
		rawdb.WriteReceipts(batch, block.Hash(), block.NumberU64(), nil)

		batchSize++
		replayedCount++

		// Write batch when full
		if batchSize >= r.config.BatchSize {
			if err := batch.Write(); err != nil {
				return replayedCount, fmt.Errorf("batch write failed: %v", err)
			}
			batch.Reset()
			log.Printf("Wrote batch of %d blocks (total: %d)", batchSize, replayedCount)
			batchSize = 0
		}
	}

	// Write final batch
	if batchSize > 0 {
		if err := batch.Write(); err != nil {
			return replayedCount, fmt.Errorf("final batch write failed: %v", err)
		}
	}

	// Set final head
	if lastHeader, exists := headers[maxHeight]; exists {
		rawdb.WriteHeadBlockHash(r.targetDB, lastHeader.Hash())
		rawdb.WriteHeadHeaderHash(r.targetDB, lastHeader.Hash())
		rawdb.WriteHeadFastBlockHash(r.targetDB, lastHeader.Hash())
		rawdb.WriteLastPivotNumber(r.targetDB, maxHeight)
		log.Printf("Set final head to block %d (hash: %s)", maxHeight, lastHeader.Hash().Hex())
	}

	return replayedCount, nil
}

// encodeBlockNumber is defined in backend.go
