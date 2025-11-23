package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/luxfi/node/chainmigrate"
)

func main() {
	var (
		// Source chain flags
		sourceType   = flag.String("source-type", "subnet-evm", "Source chain type (subnet-evm, c-chain, zoo-l2)")
		sourceDB     = flag.String("source-db", "", "Source database path (for subnet-evm/c-chain)")
		sourceRPC    = flag.String("source-rpc", "", "Source RPC URL (for zoo-l2)")
		sourceDBType = flag.String("source-db-type", "pebble", "Source database type (pebble, badger, leveldb)")

		// Destination chain flags
		destType   = flag.String("dest-type", "c-chain", "Destination chain type (c-chain, subnet-evm)")
		destDB     = flag.String("dest-db", "", "Destination database path")
		destDBType = flag.String("dest-db-type", "leveldb", "Destination database type (pebble, leveldb)")

		// Migration options
		startBlock = flag.Uint64("start-block", 0, "Start block number")
		endBlock   = flag.Uint64("end-block", 0, "End block number (0 = latest)")
		batchSize  = flag.Int("batch-size", 100, "Block batch size")
		workers    = flag.Int("workers", 4, "Number of parallel workers")

		// Features
		verifyBlocks = flag.Bool("verify-blocks", false, "Verify migrated blocks")
		verifyState  = flag.Bool("verify-state", false, "Verify migrated state")
		continueOnError = flag.Bool("continue-on-error", false, "Continue on errors")
		corethCompat = flag.Bool("coreth-compat", false, "Enable CorethVM compatibility for C-Chain")

		// L2 specific flags
		l1Contract = flag.String("l1-contract", "", "L1 contract address for L2 chains")
		namespace  = flag.String("namespace", "", "Database namespace for subnet-evm")

		// Progress
		quiet = flag.Bool("quiet", false, "Suppress progress output")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Chain Migration Tool

This tool migrates blockchain data between different chain implementations.

Supported source types:
  - subnet-evm: SubnetEVM database (PebbleDB/BadgerDB/LevelDB)
  - c-chain: C-Chain database
  - zoo-l2: Zoo L2 via RPC

Supported destination types:
  - c-chain: C-Chain with optional CorethVM compatibility
  - subnet-evm: SubnetEVM database

Examples:

  # Migrate SubnetEVM to C-Chain
  chainmigrate \
    --source-type=subnet-evm \
    --source-db=/path/to/subnet/db \
    --dest-type=c-chain \
    --dest-db=/path/to/cchain/db \
    --coreth-compat

  # Migrate Zoo L2 to C-Chain
  chainmigrate \
    --source-type=zoo-l2 \
    --source-rpc=http://zoo-l2-rpc:8545 \
    --dest-type=c-chain \
    --dest-db=/path/to/cchain/db \
    --start-block=1000000 \
    --end-block=2000000

  # Migrate with verification
  chainmigrate \
    --source-type=subnet-evm \
    --source-db=/path/to/subnet/db \
    --dest-type=c-chain \
    --dest-db=/path/to/cchain/db \
    --verify-blocks \
    --verify-state

Usage:
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	// Validate flags
	if err := validateFlags(*sourceType, *sourceDB, *sourceRPC, *destType, *destDB); err != nil {
		log.Fatalf("Invalid flags: %v", err)
	}

	// Create exporter
	exporter, err := createExporter(*sourceType, *sourceDB, *sourceRPC, *sourceDBType, *namespace, *l1Contract)
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}
	defer exporter.Close()

	// Create importer
	importer, err := createImporter(*destType, *destDB, *destDBType, *corethCompat)
	if err != nil {
		log.Fatalf("Failed to create importer: %v", err)
	}
	defer importer.Close()

	// Create migrator config
	logFunc := func(format string, args ...interface{}) {
		if !*quiet {
			fmt.Printf(format+"\n", args...)
		}
	}

	config := chainmigrate.MigratorConfig{
		StartBlock:       *startBlock,
		EndBlock:         *endBlock,
		BlockBatchSize:   *batchSize,
		StateBatchSize:   1000,
		ParallelWorkers:  *workers,
		ContinueOnError:  *continueOnError,
		VerifyBlocks:     *verifyBlocks,
		VerifyState:      *verifyState,
		ProgressInterval: 10 * time.Second,
		LogFunc:          logFunc,
	}

	// Create migrator
	migrator := chainmigrate.NewChainMigrator(exporter, importer, config)
	defer migrator.Close()

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logFunc("\nReceived interrupt signal, shutting down gracefully...")
		cancel()
	}()

	// Run migration
	logFunc("Starting chain migration...")
	logFunc("Source: %s", *sourceType)
	logFunc("Destination: %s", *destType)
	logFunc("")

	startTime := time.Now()

	if err := migrator.Migrate(ctx); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	elapsed := time.Since(startTime)
	logFunc("\nMigration completed successfully in %s", elapsed.Round(time.Second))
}

func validateFlags(sourceType, sourceDB, sourceRPC, destType, destDB string) error {
	// Validate source
	switch sourceType {
	case "subnet-evm", "c-chain":
		if sourceDB == "" {
			return fmt.Errorf("--source-db required for %s source", sourceType)
		}
	case "zoo-l2":
		if sourceRPC == "" {
			return fmt.Errorf("--source-rpc required for zoo-l2 source")
		}
	default:
		return fmt.Errorf("unsupported source type: %s", sourceType)
	}

	// Validate destination
	switch destType {
	case "c-chain", "subnet-evm":
		if destDB == "" {
			return fmt.Errorf("--dest-db required for %s destination", destType)
		}
	default:
		return fmt.Errorf("unsupported destination type: %s", destType)
	}

	return nil
}

func createExporter(sourceType, sourceDB, sourceRPC, dbType, namespace, l1Contract string) (chainmigrate.ChainExporter, error) {
	switch sourceType {
	case "subnet-evm":
		config := chainmigrate.SubnetEVMExporterConfig{
			DatabasePath: sourceDB,
			DatabaseType: dbType,
		}

		// Parse namespace if provided
		if namespace != "" {
			config.Namespace = common.FromHex(strings.TrimPrefix(namespace, "0x"))
		}

		return chainmigrate.NewSubnetEVMExporter(config), nil

	case "c-chain":
		// For now, C-Chain export uses same as SubnetEVM
		// In production, would have specific C-Chain exporter
		config := chainmigrate.SubnetEVMExporterConfig{
			DatabasePath: sourceDB,
			DatabaseType: dbType,
		}
		return chainmigrate.NewSubnetEVMExporter(config), nil

	case "zoo-l2":
		var l1Addr common.Address
		if l1Contract != "" {
			l1Addr = common.HexToAddress(l1Contract)
		}
		return chainmigrate.NewZooL2Exporter(sourceRPC, l1Addr), nil

	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

func createImporter(destType, destDB, dbType string, corethCompat bool) (chainmigrate.ChainImporter, error) {
	switch destType {
	case "c-chain":
		config := chainmigrate.ImporterConfig{
			DatabasePath:       destDB,
			DatabaseType:       dbType,
			ContinueOnError:    false,
			EnableCorethCompat: corethCompat,
		}
		return chainmigrate.NewCChainImporter(config), nil

	case "subnet-evm":
		// For subnet-evm import, we would create a SubnetEVM importer
		// For now, reuse C-Chain importer without Coreth compat
		config := chainmigrate.ImporterConfig{
			DatabasePath:       destDB,
			DatabaseType:       dbType,
			ContinueOnError:    false,
			EnableCorethCompat: false,
		}
		return chainmigrate.NewCChainImporter(config), nil

	default:
		return nil, fmt.Errorf("unsupported destination type: %s", destType)
	}
}