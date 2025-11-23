// (c) 2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/dgraph-io/badger/v4"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/core/vm"
	"github.com/luxfi/geth/ethdb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/consensus"
	"github.com/luxfi/geth/consensus/misc/eip4844"
	"github.com/luxfi/node/database"
	"github.com/luxfi/node/database/manager"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/cchainvm"
)

var (
	sourceDB    = flag.String("source", "/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb", "SubnetEVM database path")
	targetDB    = flag.String("target", "/tmp/cchain-replay", "Target C-Chain database path")
	blockLimit  = flag.Uint64("limit", 0, "Limit number of blocks to replay (0 for all)")
	treasuryAddr = flag.String("treasury", "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", "Treasury address")
	treasuryAmt  = flag.String("amount", "61500000000000000000000000000", "Treasury amount in wei")
)

func main() {
	flag.Parse()

	fmt.Println("=== Lux C-Chain Runtime Replay ===")
	fmt.Printf("Source database: %s\n", *sourceDB)
	fmt.Printf("Target database: %s\n", *targetDB)
	fmt.Printf("Treasury: %s (%s wei)\n", *treasuryAddr, *treasuryAmt)
	fmt.Println()

	ctx := context.Background()

	// Step 1: Open SubnetEVM PebbleDB
	fmt.Println("Step 1: Opening SubnetEVM PebbleDB...")
	opts := &pebble.Options{
		ReadOnly: true,
		Logger:   nil, // Silence pebble logs
	}
	
	pebbleDB, err := pebble.Open(*sourceDB, opts)
	if err != nil {
		log.Fatalf("Failed to open PebbleDB: %v", err)
	}
	defer pebbleDB.Close()

	// Step 2: Create target BadgerDB for C-Chain
	fmt.Println("Step 2: Creating target BadgerDB...")
	os.RemoveAll(*targetDB) // Clean slate
	
	badgerOpts := badger.DefaultOptions(*targetDB)
	badgerOpts.Logger = nil
	badgerOpts.SyncWrites = true
	
	badgerDB, err := badger.Open(badgerOpts)
	if err != nil {
		log.Fatalf("Failed to create BadgerDB: %v", err)
	}
	defer badgerDB.Close()

	// Wrap BadgerDB for Ethereum compatibility
	ethDB := cchainvm.WrapBadgerDB(badgerDB)

	// Step 3: Create chain configuration
	fmt.Println("Step 3: Setting up chain configuration...")
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
		TerminalTotalDifficulty: common.Big0,
	}

	// Step 4: Create genesis with treasury
	fmt.Println("Step 4: Creating genesis with treasury...")
	treasuryBalance, _ := new(big.Int).SetString(*treasuryAmt, 10)
	treasury := common.HexToAddress(*treasuryAddr)
	
	genesis := &core.Genesis{
		Config:     chainConfig,
		Timestamp:  1609459200, // Jan 1, 2021
		ExtraData:  []byte("Lux C-Chain Genesis"),
		GasLimit:   8000000,
		Difficulty: big.NewInt(0),
		Alloc: types.GenesisAlloc{
			treasury: {
				Balance: treasuryBalance,
			},
		},
	}

	// Initialize the genesis block
	genesisBlock := genesis.ToBlock()
	rawdb.WriteBlock(ethDB, genesisBlock)
	rawdb.WriteCanonicalHash(ethDB, genesisBlock.Hash(), 0)
	rawdb.WriteHeadBlockHash(ethDB, genesisBlock.Hash())
	rawdb.WriteHeadHeaderHash(ethDB, genesisBlock.Hash())
	rawdb.WriteChainConfig(ethDB, genesisBlock.Hash(), chainConfig)

	// Step 5: Read blocks from SubnetEVM and re-execute
	fmt.Println("Step 5: Reading and re-executing blocks...")
	
	// SubnetEVM namespace prefix (from your analysis)
	namespacePrefix := []byte{0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c, 
		                       0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e, 
		                       0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a, 
		                       0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1}

	// Create a simple consensus engine for replay
	engine := &dummyEngine{}

	// Create blockchain
	cacheConfig := &core.CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		TrieTimeLimit:  5 * time.Minute,
	}

	blockchain, err := core.NewBlockChain(ethDB, cacheConfig, genesis, nil, engine, vm.Config{}, nil)
	if err != nil {
		log.Fatalf("Failed to create blockchain: %v", err)
	}
	defer blockchain.Stop()

	fmt.Printf("Genesis block created at height 0\n")
	fmt.Printf("Treasury balance: %s wei\n", treasuryBalance.String())
	fmt.Println()

	// Iterate through blocks and replay them
	startTime := time.Now()
	blocksProcessed := uint64(0)
	maxBlocks := uint64(1074616) // Known max height from your data
	if *blockLimit > 0 && *blockLimit < maxBlocks {
		maxBlocks = *blockLimit
	}

	for blockNum := uint64(1); blockNum <= maxBlocks; blockNum++ {
		// Read block from PebbleDB
		block := readBlockFromSubnetEVM(pebbleDB, namespacePrefix, blockNum)
		if block == nil {
			continue
		}

		// Re-execute block transactions to rebuild state
		receipts, _, usedGas, err := blockchain.Processor().Process(block, blockchain.GetBlockByHash(block.ParentHash()).Header(), blockchain.State())
		if err != nil {
			fmt.Printf("Error processing block %d: %v\n", blockNum, err)
			continue
		}

		// Write block and receipts
		rawdb.WriteBlock(ethDB, block)
		rawdb.WriteReceipts(ethDB, block.Hash(), blockNum, receipts)
		rawdb.WriteCanonicalHash(ethDB, block.Hash(), blockNum)
		
		blocksProcessed++
		
		// Progress report every 1000 blocks
		if blockNum%1000 == 0 {
			elapsed := time.Since(startTime)
			rate := float64(blocksProcessed) / elapsed.Seconds()
			fmt.Printf("Progress: Block %d/%d (%.1f%%) | Rate: %.0f blocks/s | Gas: %d\n",
				blockNum, maxBlocks, float64(blockNum)*100/float64(maxBlocks), rate, usedGas)
		}
	}

	// Final report
	elapsed := time.Since(startTime)
	fmt.Println("\n=== Replay Complete ===")
	fmt.Printf("Blocks processed: %d\n", blocksProcessed)
	fmt.Printf("Time elapsed: %s\n", elapsed)
	fmt.Printf("Average rate: %.1f blocks/s\n", float64(blocksProcessed)/elapsed.Seconds())

	// Verify final state
	stateDB, err := blockchain.State()
	if err == nil {
		finalBalance := stateDB.GetBalance(treasury)
		fmt.Printf("\nFinal treasury balance: %s wei\n", finalBalance.String())
	}
}

// readBlockFromSubnetEVM reads a block from the SubnetEVM PebbleDB with namespace prefix
func readBlockFromSubnetEVM(db *pebble.DB, namespace []byte, blockNum uint64) *types.Block {
	// Construct key for canonical hash (similar to how SubnetEVM stores it)
	// Format: namespace + "h" + blockNum (8 bytes big-endian)
	key := make([]byte, len(namespace)+1+8)
	copy(key, namespace)
	key[len(namespace)] = 'h' // canonical hash prefix
	binary.BigEndian.PutUint64(key[len(namespace)+1:], blockNum)

	// Read canonical hash
	value, closer, err := db.Get(key)
	if err != nil {
		return nil
	}
	defer closer.Close()

	if len(value) != 32 {
		return nil
	}

	var blockHash common.Hash
	copy(blockHash[:], value)

	// Now read the block header
	// Format: namespace + "H" + blockHash
	headerKey := make([]byte, len(namespace)+1+32)
	copy(headerKey, namespace)
	headerKey[len(namespace)] = 'H'
	copy(headerKey[len(namespace)+1:], blockHash[:])

	headerData, closer2, err := db.Get(headerKey)
	if err != nil {
		return nil
	}
	defer closer2.Close()

	// Decode header
	var header types.Header
	if err := header.UnmarshalJSON(headerData); err != nil {
		// Try RLP decoding
		// This would need proper RLP decoding implementation
		return nil
	}

	// Read block body
	// Format: namespace + "b" + blockHash
	bodyKey := make([]byte, len(namespace)+1+32)
	copy(bodyKey, namespace)
	bodyKey[len(namespace)] = 'b'
	copy(bodyKey[len(namespace)+1:], blockHash[:])

	bodyData, closer3, err := db.Get(bodyKey)
	if err != nil {
		// Block might not have transactions
		return types.NewBlockWithHeader(&header)
	}
	defer closer3.Close()

	// Decode body (this would need proper implementation)
	// For now, return block with just header
	return types.NewBlockWithHeader(&header)
}

// Helper functions
func newUint64(val uint64) *uint64 {
	return &val
}

// dummyEngine is a consensus engine that accepts all blocks
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
}

func (d *dummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB, body *types.Body, receipts []*types.Receipt) (*types.Block, error) {
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
	return big.NewInt(0)
}

func (d *dummyEngine) Close() error {
	return nil
}