// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Command backup provides CLI tools for database backup and restore operations.
//
// Usage:
//
//	# Full backup with zstd compression
//	luxd backup --output /path/to/backup.zst --since 0
//
//	# Incremental backup (using version from last backup)
//	luxd backup --output /path/to/backup-incr.zst --since 12345678
//
//	# Restore from backup
//	luxd restore --input /path/to/backup.zst
//
//	# Get last backup metadata
//	luxd backup-info --data-dir ~/.lux/mainnet
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/luxfi/database"
	databasefactory "github.com/luxfi/database/factory"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/service/backup"
)

var (
	// Flags
	outputPath  string
	inputPath   string
	sinceVer    uint64
	dataDir     string
	dbType      string
	noCompress  bool
	forceRestore bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "backup",
		Short: "Database backup and restore utilities",
		Long: `Backup and restore utilities for Lux node database.

Supports:
- Full and incremental backups using BadgerDB native backup
- Zstd compression (enabled by default for .zst extension)
- Works with both BadgerDB and PebbleDB`,
	}

	// backup command
	backupCmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a database backup",
		Long: `Create a backup of the node database.

Examples:
  # Full backup with zstd compression
  luxd backup --output /backups/lux-backup.zst --data-dir ~/.lux/mainnet

  # Incremental backup (pass version from previous backup)
  luxd backup --output /backups/lux-backup-incr.zst --since 12345678 --data-dir ~/.lux/mainnet

  # Backup without compression
  luxd backup --output /backups/lux-backup.db --no-compress --data-dir ~/.lux/mainnet`,
		RunE: runBackup,
	}
	backupCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (required)")
	backupCmd.Flags().Uint64VarP(&sinceVer, "since", "s", 0, "Version for incremental backup (0 for full)")
	backupCmd.Flags().StringVarP(&dataDir, "data-dir", "d", "", "Node data directory (required)")
	backupCmd.Flags().StringVar(&dbType, "db-type", "badgerdb", "Database type (badgerdb or pebbledb)")
	backupCmd.Flags().BoolVar(&noCompress, "no-compress", false, "Disable zstd compression")
	backupCmd.MarkFlagRequired("output")
	backupCmd.MarkFlagRequired("data-dir")

	// restore command
	restoreCmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore database from backup",
		Long: `Restore the node database from a backup file.

WARNING: This will overwrite the existing database!

Examples:
  # Restore from compressed backup
  luxd restore --input /backups/lux-backup.zst --data-dir ~/.lux/mainnet

  # Force restore (skip confirmation)
  luxd restore --input /backups/lux-backup.zst --data-dir ~/.lux/mainnet --force`,
		RunE: runRestore,
	}
	restoreCmd.Flags().StringVarP(&inputPath, "input", "i", "", "Input backup file path (required)")
	restoreCmd.Flags().StringVarP(&dataDir, "data-dir", "d", "", "Node data directory (required)")
	restoreCmd.Flags().StringVar(&dbType, "db-type", "badgerdb", "Database type (badgerdb or pebbledb)")
	restoreCmd.Flags().BoolVarP(&forceRestore, "force", "f", false, "Skip confirmation prompt")
	restoreCmd.MarkFlagRequired("input")
	restoreCmd.MarkFlagRequired("data-dir")

	// info command
	infoCmd := &cobra.Command{
		Use:   "backup-info",
		Short: "Show backup metadata",
		Long: `Display information about the last backup.

Examples:
  luxd backup-info --data-dir ~/.lux/mainnet`,
		RunE: runInfo,
	}
	infoCmd.Flags().StringVarP(&dataDir, "data-dir", "d", "", "Node data directory (required)")
	infoCmd.MarkFlagRequired("data-dir")

	rootCmd.AddCommand(backupCmd, restoreCmd, infoCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runBackup(cmd *cobra.Command, args []string) error {
	logger := log.New("cmd", "backup")

	// Open database
	dbPath := filepath.Join(dataDir, "db")
	db, err := openDatabase(dbPath, dbType, logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create backup service
	backupSvc, err := backup.New(backup.Config{
		DB:          db,
		MetadataDir: filepath.Join(dataDir, "backup"),
		Log:         logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create backup service: %w", err)
	}

	// Determine compression
	compress := !noCompress && (strings.HasSuffix(outputPath, ".zst") || strings.HasSuffix(outputPath, ".zstd"))

	fmt.Printf("Starting backup...\n")
	fmt.Printf("  Output: %s\n", outputPath)
	fmt.Printf("  Since version: %d\n", sinceVer)
	fmt.Printf("  Compression: %v\n", compress)

	version, err := backupSvc.BackupToFile(outputPath, sinceVer, compress)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Get file size
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("failed to stat backup file: %w", err)
	}

	fmt.Printf("\nBackup completed successfully!\n")
	fmt.Printf("  Version: %d\n", version)
	fmt.Printf("  Size: %s\n", formatBytes(info.Size()))
	fmt.Printf("  Incremental: %v\n", sinceVer > 0)
	fmt.Printf("\nFor next incremental backup, use: --since %d\n", version)

	return nil
}

func runRestore(cmd *cobra.Command, args []string) error {
	logger := log.New("cmd", "restore")

	// Check if backup file exists
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", inputPath)
	}

	// Confirm restore
	if !forceRestore {
		fmt.Printf("WARNING: This will overwrite the database at %s\n", dataDir)
		fmt.Printf("Are you sure you want to continue? (yes/no): ")
		var response string
		fmt.Scanln(&response)
		if response != "yes" {
			fmt.Println("Restore cancelled.")
			return nil
		}
	}

	// Open database
	dbPath := filepath.Join(dataDir, "db")
	db, err := openDatabase(dbPath, dbType, logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create backup service
	backupSvc, err := backup.New(backup.Config{
		DB:          db,
		MetadataDir: filepath.Join(dataDir, "backup"),
		Log:         logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create backup service: %w", err)
	}

	// Determine if compressed
	compressed := strings.HasSuffix(inputPath, ".zst") || strings.HasSuffix(inputPath, ".zstd")

	fmt.Printf("Starting restore...\n")
	fmt.Printf("  Input: %s\n", inputPath)
	fmt.Printf("  Compressed: %v\n", compressed)

	if err := backupSvc.RestoreFromFile(inputPath, compressed); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Printf("\nRestore completed successfully!\n")
	return nil
}

func runInfo(cmd *cobra.Command, args []string) error {
	metadataPath := filepath.Join(dataDir, "backup", backup.MetadataFileName)

	data, err := os.ReadFile(metadataPath)
	if os.IsNotExist(err) {
		fmt.Println("No backup metadata found.")
		fmt.Printf("Metadata path: %s\n", metadataPath)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	fmt.Println("Backup Metadata:")
	fmt.Println(string(data))
	return nil
}

func openDatabase(path string, dbType string, logger log.Logger) (database.Database, error) {
	// Ensure directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	gatherer := metric.NewRegistry()
	db, err := databasefactory.New(dbType, path, false, nil, gatherer, logger, "backup", "db")
	if err != nil {
		return nil, err
	}

	return db, nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
