// (c) 2024 Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/ethdb/leveldb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/node/vms/cchainvm"
)

func main() {
	// Command-line flags
	var (
		sourcePath   = flag.String("source", "", "Path to source EVM database (required)")
		targetPath   = flag.String("target", "", "Path to target C-Chain database (required)")
		targetHeight = flag.Uint64("height", 1082780, "Target block height to replay to")
		walletAddr   = flag.String("wallet", "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", "Wallet address to track")
		testMode     = flag.Bool("test", false, "Run in test mode (only process first 100 blocks)")
		testLimit    = flag.Uint64("test-limit", 100, "Number of blocks to process in test mode")
		verifyRoots  = flag.Bool("verify", false, "Verify state roots after each block")
		logInterval  = flag.Uint64("log-interval", 1000, "Log progress every N blocks")
		chainID      = flag.Int64("chain-id", 96369, "Chain ID for the network")
	)

	flag.Parse()

	// Validate required flags
	if *sourcePath == "" || *targetPath == "" {
		fmt.Fprintf(os.Stderr, "Error: Both -source and -target paths are required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Convert wallet address
	var targetWallet common.Address
	if *walletAddr != "" {
		if !common.IsHexAddress(*walletAddr) {
			log.Fatalf("Invalid wallet address: %s", *walletAddr)
		}
		targetWallet = common.HexToAddress(*walletAddr)
	}

	// Set default source path if using standard location
	if *sourcePath == "" {
		*sourcePath = "/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	}

	// Create target directory if it doesn't exist
	targetDir := filepath.Dir(*targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		log.Fatalf("Failed to create target directory: %v", err)
	}

	log.Printf("===========================================")
	log.Printf("C-Chain Replay Engine")
	log.Printf("===========================================")
	log.Printf("Source DB: %s", *sourcePath)
	log.Printf("Target DB: %s", *targetPath)
	log.Printf("Target Height: %d", *targetHeight)
	log.Printf("Tracking Wallet: %s", targetWallet.Hex())
	log.Printf("Chain ID: %d", *chainID)
	log.Printf("Test Mode: %v", *testMode)
	if *testMode {
		log.Printf("Test Limit: %d blocks", *testLimit)
	}
	log.Printf("===========================================")

	// Open target database
	log.Printf("Opening target database...")
	ldb, err := leveldb.New(*targetPath, 256, 16, "cchain", false)
	if err != nil {
		log.Fatalf("Failed to open leveldb: %v", err)
	}

	// Wrap with high-level database
	targetDB, err := rawdb.Open(ldb, rawdb.OpenOptions{
		Ancient:  filepath.Join(*targetPath, "ancient"),
		ReadOnly: false,
	})
	if err != nil {
		log.Fatalf("Failed to open target database: %v", err)
	}
	defer targetDB.Close()

	// Configure chain parameters
	chainConfig := &params.ChainConfig{
		ChainID:             big.NewInt(*chainID),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		// Additional hard forks can be added as needed
	}

	// Create replay configuration
	config := &cchainvm.UnifiedReplayConfig{
		SourcePath:         *sourcePath,
		DatabaseType:       cchainvm.AutoDetect,
		TestMode:           *testMode,
		TestLimit:          *testLimit,
		TargetWallet:       targetWallet,
		TargetHeight:       *targetHeight,
		ChainConfig:        chainConfig,
		ReplayTransactions: true, // Always replay transactions for proper state building
		VerifyStateRoots:   *verifyRoots,
		LogInterval:        *logInterval,
	}

	// Create replayer
	log.Printf("Initializing replayer...")
	replayer, err := cchainvm.NewUnifiedReplayer(config, targetDB, nil)
	if err != nil {
		log.Fatalf("Failed to create replayer: %v", err)
	}
	defer replayer.Close()

	// Start replay with EVM execution
	log.Printf("Starting EVM-based replay...")
	if err := replayer.ReplayWithEVM(); err != nil {
		log.Fatalf("Replay failed: %v", err)
	}

	log.Printf("===========================================")
	log.Printf("Replay completed successfully!")
	log.Printf("Database written to: %s", *targetPath)
	log.Printf("===========================================")
}