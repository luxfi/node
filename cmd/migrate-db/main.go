// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/luxfi/node/internal/database/migration"
)

func main() {
	var (
		sourcePath  = flag.String("source", "", "Path to source Pebble database")
		targetPath  = flag.String("target", "", "Path to target Badger database")
		dataDir     = flag.String("data-dir", "/home/z/.luxd", "Data directory for Lux node")
		genesisOnly = flag.Bool("genesis-only", false, "Migrate only genesis data")
		showStats   = flag.Bool("stats", false, "Show database statistics")
		_           = flag.Bool("verbose", false, "Enable verbose logging")
	)

	flag.Parse()

	// Setup logger
	logger := log.New(os.Stdout, "[MIGRATION] ", log.Ldate|log.Ltime)

	// Determine paths if not provided
	if *sourcePath == "" {
		*sourcePath = filepath.Join(*dataDir, "db")
	}
	if *targetPath == "" {
		*targetPath = filepath.Join(*dataDir, "db-badger")
	}

	// Create migrator
	migrator := migration.NewPebbleToBadgerMigrator(*sourcePath, *targetPath, logger)

	// Show stats if requested
	if *showStats {
		stats, err := migrator.GetStats()
		if err != nil {
			logger.Fatalf("Failed to get stats: %v", err)
		}
		fmt.Printf("\nDatabase Statistics:\n")
		fmt.Printf("%s\n\n", stats)
		return
	}

	// Perform migration
	logger.Printf("=== LUX Database Migration Tool ===\n")
	logger.Printf("Source (Pebble): %s\n", *sourcePath)
	logger.Printf("Target (Badger): %s\n", *targetPath)
	logger.Printf("Genesis Only: %v\n", *genesisOnly)
	logger.Printf("=====================================\n\n")

	// Check if source exists
	if _, err := os.Stat(*sourcePath); os.IsNotExist(err) {
		logger.Fatalf("Source database does not exist: %s", *sourcePath)
	}

	// Create target directory if it doesn't exist
	if err := os.MkdirAll(*targetPath, 0755); err != nil {
		logger.Fatalf("Failed to create target directory: %v", err)
	}

	// Perform migration
	var err error
	if *genesisOnly {
		err = migrator.MigrateGenesis()
	} else {
		err = migrator.Migrate()
	}

	if err != nil {
		logger.Fatalf("Migration failed: %v", err)
	}

	// Show final stats
	stats, err := migrator.GetStats()
	if err != nil {
		logger.Printf("Warning: Could not get final stats: %v", err)
	} else {
		logger.Printf("\nFinal Statistics:\n%s\n", stats)
	}

	logger.Println("\n✅ Migration completed successfully!")
}
