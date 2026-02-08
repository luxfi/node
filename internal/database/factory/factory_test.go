// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/database/zapdb"
	"github.com/luxfi/log"
	"github.com/luxfi/node/service/metrics"
	"github.com/stretchr/testify/require"
)

// TestDatabaseFactoryCreation verifies that the database factory
// creates a database with meterdb wrapping without error.
func TestDatabaseFactoryCreation(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "factory_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	gatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	db, err := New(
		zapdb.Name,
		dbPath,
		false,
		nil,
		gatherer,
		logger,
		"test_db",
		"test_meterdb",
	)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Verify database operations work through meterdb wrapper
	testKey := []byte("test_key")
	testValue := []byte("test_value")

	require.NoError(db.Put(testKey, testValue))

	value, err := db.Get(testKey)
	require.NoError(err)
	require.Equal(testValue, value)

	has, err := db.Has(testKey)
	require.NoError(err)
	require.True(has)
}

// TestDatabaseFactoryReadOnly verifies read-only database creation works.
func TestDatabaseFactoryReadOnly(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "factory_readonly_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	gatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	// Create a database first (read-write)
	db, err := New(
		zapdb.Name,
		dbPath,
		false,
		nil,
		gatherer,
		logger,
		"test_db",
		"test_meterdb",
	)
	require.NoError(err)

	// Write some data
	require.NoError(db.Put([]byte("key"), []byte("value")))
	db.Close()

	// Open as read-only with new gatherer to avoid namespace conflicts
	gathererReadOnly := metrics.NewMultiGatherer()

	dbReadOnly, err := New(
		zapdb.Name,
		dbPath,
		true, // readOnly
		nil,
		gathererReadOnly,
		logger,
		"test_db_ro",
		"test_meterdb_ro",
	)
	require.NoError(err)
	require.NotNil(dbReadOnly)
	defer dbReadOnly.Close()

	// Read should work
	value, err := dbReadOnly.Get([]byte("key"))
	require.NoError(err)
	require.Equal([]byte("value"), value)
}

// TestDatabaseFactoryNodePattern verifies the exact pattern used by node.initDatabase().
func TestDatabaseFactoryNodePattern(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "node_pattern_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	const (
		dbNamespace      = "lux_db"
		meterDBNamespace = "lux_meterdb"
	)

	metricsGatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	// Create database using the exact pattern from node.initDatabase()
	db, err := New(
		zapdb.Name,
		dbPath,
		false,
		nil,
		metricsGatherer,
		logger,
		dbNamespace,
		meterDBNamespace,
	)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Perform database operations to verify meterdb wrapping works
	testData := map[string][]byte{
		"genesisID":          []byte("test_genesis_hash"),
		"validator_1":        []byte("validator_data_1"),
		"ungracefulShutdown": {},
	}

	for key, value := range testData {
		require.NoError(db.Put([]byte(key), value))
	}

	for key, expectedValue := range testData {
		value, err := db.Get([]byte(key))
		require.NoError(err)
		require.Equal(expectedValue, value)
	}
}
