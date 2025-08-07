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
	SourcePath   string       // Path to source database
	DatabaseType DatabaseType // Type of source database
	Namespace    []byte       // Namespace for namespaced databases (optional)
	TestMode     bool         // If true, only replay first 100 blocks
	TestLimit    uint64       // Number of blocks to replay in test mode
	CopyAllState bool         // If true, copy all state data (can be large)
	MaxStateNodes uint64      // Maximum state nodes to copy (0 = unlimited)
}

// UnifiedReplayer handles replaying blocks from various database formats
type UnifiedReplayer struct {
	config      *UnifiedReplayConfig
	targetDB    ethdb.Database
	blockchain  *core.BlockChain
	sourceDB    *pebble.DB
	namespace   []byte
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

	sourceDB, err := pebble.Open(config.SourcePath, &pebble.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open source database: %v", err)
	}

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
	
	// For test mode, copy ALL state nodes to ensure complete state access
	if r.config.TestMode {
		log.Printf("TEST MODE: Copying ALL state trie nodes from source database...")
		copiedCount := 0
		
		// Iterate through ALL entries with namespace prefix that are state nodes
		stateIter, _ := r.sourceDB.NewIter(&pebble.IterOptions{
			LowerBound: r.namespace,
		})
		defer stateIter.Close()
		
		for stateIter.First(); stateIter.Valid(); stateIter.Next() {
			key := stateIter.Key()
			val, _ := stateIter.ValueAndErr()
			
			// State trie nodes have exactly 64 bytes: namespace(32) + hash(32)
			if len(key) == 64 && bytes.HasPrefix(key, r.namespace) {
				// This is a state trie node
				hash := key[32:] // The hash part after namespace
				
				// Write to target database (just the hash, no namespace)
				if err := r.targetDB.Put(hash, val); err != nil {
					log.Printf("Failed to write state node %x: %v", hash, err)
					continue
				}
				
				copiedCount++
				
				if copiedCount % 50000 == 0 {
					log.Printf("Copied %d state trie nodes...", copiedCount)
				}
				
				// No limit in test mode - we need ALL state
				// Remove the 1M limit to ensure complete state
			}
		}
		
		log.Printf("Copied %d state trie nodes", copiedCount)
		return nil
	}
	
	// For production, use selective recursive copy
	processedNodes := make(map[string]bool)
	copiedCount := 0
	
	var copyTrieNode func(hash []byte, depth int) error
	copyTrieNode = func(hash []byte, depth int) error {
		if len(hash) != 32 {
			return nil
		}
		
		hashStr := hex.EncodeToString(hash)
		if processedNodes[hashStr] {
			return nil
		}
		processedNodes[hashStr] = true
		
		// Build key: namespace + hash
		nodeKey := append([]byte(nil), r.namespace...)
		nodeKey = append(nodeKey, hash...)
		
		// Get node data
		nodeData, closer, err := r.sourceDB.Get(nodeKey)
		if err != nil {
			return nil // Node doesn't exist
		}
		defer closer.Close()
		
		// Write to target database
		if err := r.targetDB.Put(hash, nodeData); err != nil {
			return fmt.Errorf("failed to write node %x: %v", hash, err)
		}
		
		copiedCount++
		if copiedCount % 10000 == 0 {
			log.Printf("Copied %d state trie nodes...", copiedCount)
		}
		
		// Check limit
		if r.config.MaxStateNodes > 0 && uint64(copiedCount) >= r.config.MaxStateNodes {
			log.Printf("Reached state node limit (%d nodes)", r.config.MaxStateNodes)
			return nil
		}
		
		// Parse node to find children
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
	
	// Copy state tries for each root
	for root := range stateRoots {
		if err := copyTrieNode(root.Bytes(), 0); err != nil {
			log.Printf("Error copying state trie for root %x: %v", root, err)
		}
	}
	
	log.Printf("Copied %d state trie nodes", copiedCount)
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
		if blockNum == maxBlockNum || blockNum % 10000 == 0 {
			rawdb.WriteHeadBlockHash(r.targetDB, block.Hash())
			rawdb.WriteHeadHeaderHash(r.targetDB, block.Hash())
			rawdb.WriteHeadFastBlockHash(r.targetDB, block.Hash())
		}
		
		replayedCount++
		if replayedCount % 10000 == 0 {
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

// Use encodeBlockNumber from backend.go