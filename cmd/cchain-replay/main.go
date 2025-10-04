package main

import (
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/ethdb/leveldb"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/node/vms/cchainvm"
)

func main() {
	var (
		sourcePath = flag.String("source", "", "Path to source database (required)")
		targetPath = flag.String("target", "", "Path to target database (required)")
		backend    = flag.String("backend", "auto", "Database backend: badgerdb, pebbledb, or auto")
		dbType     = flag.String("type", "auto", "Database type: standard, namespaced, or auto")
		height     = flag.Uint64("height", 0, "Target block height to replay (0 = all)")
		wallet     = flag.String("wallet", "", "Wallet address to track during replay")
		testMode   = flag.Bool("test", false, "Test mode - only replay first 100 blocks")
		verify     = flag.Bool("verify", false, "Verify state roots after each block")
	)

	flag.Parse()

	if *sourcePath == "" || *targetPath == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -source <path> -target <path> [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nRequired:\n")
		fmt.Fprintf(os.Stderr, "  -source <path>    Path to source database\n")
		fmt.Fprintf(os.Stderr, "  -target <path>    Path to target database\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  -backend <type>   Database backend: badgerdb, pebbledb, or auto (default: auto)\n")
		fmt.Fprintf(os.Stderr, "  -type <type>      Database type: standard, namespaced, or auto (default: auto)\n")
		fmt.Fprintf(os.Stderr, "  -height <n>       Target block height to replay (default: 0 = all)\n")
		fmt.Fprintf(os.Stderr, "  -wallet <addr>    Wallet address to track during replay\n")
		fmt.Fprintf(os.Stderr, "  -test             Test mode - only replay first 100 blocks\n")
		fmt.Fprintf(os.Stderr, "  -verify           Verify state roots after each block\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Replay from BadgerDB (auto-detect)\n")
		fmt.Fprintf(os.Stderr, "  %s -source /home/z/.luxd-node1/chainData/C/db -target /tmp/cchain-db\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Replay from PebbleDB with verification\n")
		fmt.Fprintf(os.Stderr, "  %s -source /home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb -target /tmp/cchain-db -backend pebbledb -verify\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Test mode with wallet tracking\n")
		fmt.Fprintf(os.Stderr, "  %s -source /path/to/source -target /tmp/test-db -test -wallet 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714\n", os.Args[0])
		os.Exit(1)
	}

	// Convert backend string to enum
	var dbBackend cchainvm.DatabaseBackend
	switch *backend {
	case "badgerdb":
		dbBackend = cchainvm.BadgerBackend
	case "pebbledb":
		dbBackend = cchainvm.PebbleBackend
	case "auto":
		dbBackend = cchainvm.AutoDetectBackend
	default:
		log.Fatalf("Invalid backend: %s (must be badgerdb, pebbledb, or auto)", *backend)
	}

	// Convert database type string to enum
	var databaseType cchainvm.DatabaseType
	switch *dbType {
	case "standard":
		databaseType = cchainvm.StandardDB
	case "namespaced":
		databaseType = cchainvm.NamespacedDB
	case "auto":
		databaseType = cchainvm.AutoDetect
	default:
		log.Fatalf("Invalid database type: %s (must be standard, namespaced, or auto)", *dbType)
	}

	// Parse wallet address if provided
	var targetWallet common.Address
	if *wallet != "" {
		if !common.IsHexAddress(*wallet) {
			log.Fatalf("Invalid wallet address: %s", *wallet)
		}
		targetWallet = common.HexToAddress(*wallet)
	}

	// Create configuration
	config := &cchainvm.UnifiedReplayConfig{
		SourcePath:         *sourcePath,
		DatabaseBackend:    dbBackend,
		DatabaseType:       databaseType,
		TestMode:           *testMode,
		TargetHeight:       *height,
		TargetWallet:       targetWallet,
		ReplayTransactions: true,
		VerifyStateRoots:   *verify,
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(96369), // LUX mainnet
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
		},
	}

	// Open target database
	log.Printf("Opening target database at %s", *targetPath)
	levelDB, err := leveldb.New(*targetPath, 256, 0, "", false)
	if err != nil {
		log.Fatalf("Failed to open target database: %v", err)
	}
	defer levelDB.Close()

	// Wrap in rawdb interface
	targetDB := rawdb.NewDatabase(levelDB)

	// Create replayer
	replayer, err := cchainvm.NewUnifiedReplayer(config, targetDB, nil)
	if err != nil {
		log.Fatalf("Failed to create replayer: %v", err)
	}
	defer replayer.Close()

	// Run replay
	log.Println("Starting block replay...")
	if err := replayer.ReplayWithEVM(); err != nil {
		log.Fatalf("Replay failed: %v", err)
	}

	log.Println("Replay completed successfully!")
}