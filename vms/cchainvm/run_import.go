// Copyright (C) 2019-2025 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// +build ignore

// This is a standalone tool to run the EVM import
// Build with: go build -o import_tool run_import.go import.go import_integration.go
// Run with: ./import_tool

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/luxfi/node/vms/cchainvm"
)

func main() {
	var (
		sourcePath   = flag.String("source", "/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb", "Source EVM database path")
		targetPath   = flag.String("target", "/home/z/work/lux/node/chaindata/cchain", "Target C-Chain database path")
		startBlock   = flag.Uint64("start", 0, "Start block number")
		endBlock     = flag.Uint64("end", 1082780, "End block number")
		batchSize    = flag.Int("batch", 5000, "Batch size for processing")
		workers      = flag.Int("workers", 8, "Number of worker threads")
		wallet       = flag.String("wallet", "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", "Wallet address to track")
		configFile   = flag.String("config", "", "Configuration file path (JSON)")
		verifyState  = flag.Bool("verify", false, "Verify state roots during import")
		rebuildState = flag.Bool("rebuild", true, "Rebuild state during import")
		stripNS      = flag.Bool("strip-namespace", true, "Strip EVM namespace prefix")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "EVM to C-Chain Import Tool\n")
		fmt.Fprintf(os.Stderr, "=================================\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "This tool imports EVM blockchain data into C-Chain format.\n")
		fmt.Fprintf(os.Stderr, "It handles namespace prefix stripping and key format translation.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Import with default settings\n")
		fmt.Fprintf(os.Stderr, "  %s\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Import specific block range\n")
		fmt.Fprintf(os.Stderr, "  %s -start 1000000 -end 1082780\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Import with config file\n")
		fmt.Fprintf(os.Stderr, "  %s -config import_config.json\n\n", os.Args[0])
	}

	flag.Parse()

	log.SetPrefix("[IMPORT] ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	fmt.Println("╔════════════════════════════════════════════╗")
	fmt.Println("║   EVM to C-Chain Import Tool         ║")
	fmt.Println("╚════════════════════════════════════════════╝")
	fmt.Println()

	startTime := time.Now()

	// If config file is specified, use it
	if *configFile != "" {
		fmt.Printf("Loading configuration from: %s\n", *configFile)
		if err := cchainvm.RunImportFromFile(*configFile); err != nil {
			log.Fatalf("Import failed: %v", err)
		}
	} else {
		// Use command-line parameters
		fmt.Println("Import Configuration:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("  Source Database:  %s\n", *sourcePath)
		fmt.Printf("  Target Database:  %s\n", *targetPath)
		fmt.Printf("  Block Range:      %d - %d\n", *startBlock, *endBlock)
		fmt.Printf("  Total Blocks:     %d\n", *endBlock-*startBlock+1)
		fmt.Printf("  Batch Size:       %d\n", *batchSize)
		fmt.Printf("  Workers:          %d\n", *workers)
		fmt.Printf("  Target Wallet:    %s\n", *wallet)
		fmt.Printf("  Verify State:     %v\n", *verifyState)
		fmt.Printf("  Rebuild State:    %v\n", *rebuildState)
		fmt.Printf("  Strip Namespace:  %v\n", *stripNS)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println()

		// Confirm before starting
		fmt.Print("Do you want to proceed with the import? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Import cancelled.")
			os.Exit(0)
		}

		fmt.Println("\nStarting import process...")
		fmt.Println()

		// Create import command
		cmd := &cchainvm.ImportCommand{
			Type:         "subnet_evm_import",
			SourcePath:   *sourcePath,
			TargetPath:   *targetPath,
			StartBlock:   *startBlock,
			EndBlock:     *endBlock,
			BatchSize:    *batchSize,
			Workers:      *workers,
			TargetWallet: *wallet,
			Options: struct {
				VerifyState    bool `json:"verify_state"`
				RebuildState   bool `json:"rebuild_state"`
				StripNamespace bool `json:"strip_namespace"`
			}{
				VerifyState:    *verifyState,
				RebuildState:   *rebuildState,
				StripNamespace: *stripNS,
			},
		}

		// Execute import
		hooks := &cchainvm.VMImportHooks{}
		if err := hooks.ExecuteImport(cmd); err != nil {
			log.Fatalf("Import failed: %v", err)
		}
	}

	elapsed := time.Since(startTime)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("✅ Import completed successfully in %v\n", elapsed)
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("1. Start the C-Chain VM with the imported database")
	fmt.Println("2. Verify the wallet balance and state")
	fmt.Println("3. Test transaction replay and state queries")
	fmt.Println()
}