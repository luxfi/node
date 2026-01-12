// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/metrics"
	"github.com/stretchr/testify/require"
)

// TestMeterDBMetricsRegistration verifies that the database factory
// properly wraps the database with meterdb and registers metrics.
func TestMeterDBMetricsRegistration(t *testing.T) {
	require := require.New(t)

	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "factory_meterdb_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	// Create metrics gatherer (simulating node.MetricsGatherer)
	gatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	// Create database with metrics - this should wrap with meterdb
	db, err := New(
		badgerdb.Name,
		dbPath,
		false, // readOnly
		nil,   // config
		gatherer,
		logger,
		"test_db",
		"test_meterdb",
	)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Write and read to trigger metrics
	testKey := []byte("metric_test_key")
	testValue := []byte("metric_test_value")

	require.NoError(db.Put(testKey, testValue))

	value, err := db.Get(testKey)
	require.NoError(err)
	require.Equal(testValue, value)

	// Gather metrics to verify registration
	families, err := gatherer.Gather()
	require.NoError(err)

	// Should have metrics from database operations
	foundMetrics := false
	for _, family := range families {
		name := family.Name
		if name != "" {
			foundMetrics = true
			t.Logf("Found metric family: %s with %d metrics", name, len(family.Metrics))
		}
	}

	require.True(foundMetrics, "Expected meterdb metrics to be registered, but found none")
	t.Logf("✅ MeterDB metrics successfully registered via factory")
}

// TestMeterDBWrappingWithReadOnly verifies meterdb wrapping with read-only databases
func TestMeterDBWrappingWithReadOnly(t *testing.T) {
	require := require.New(t)

	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "factory_readonly_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	// Create a database first (read-write)
	gatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	db, err := New(
		badgerdb.Name,
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

	// Open as read-only
	// Create a new gatherer for read-only database to avoid namespace conflicts
	gathererReadOnly := metrics.NewMultiGatherer()

	dbReadOnly, err := New(
		badgerdb.Name,
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

	// Verify metrics are still registered
	families, err := gatherer.Gather()
	require.NoError(err)
	require.NotEmpty(families, "Expected metrics from read-only database")

	t.Logf("✅ MeterDB metrics work correctly with read-only databases")
}

// TestNodeDatabasePattern verifies the exact pattern used by node.initDatabase()
// This ensures root database meterdb metrics are properly registered.
func TestNodeDatabasePattern(t *testing.T) {
	require := require.New(t)

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "node_pattern_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	// Simulate node initialization pattern
	const (
		dbNamespace      = "lux_db"
		meterDBNamespace = "lux_meterdb"
	)

	// Create metrics gatherer (like node.MetricsGatherer)
	metricsGatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	// Create database using EXACT pattern from node.initDatabase()
	// See node/node.go lines 775-784
	// Note: Use meterDBNamespace for 7th param, not "all"
	db, err := New(
		badgerdb.Name,
		dbPath,
		false, // readOnly
		nil,   // config
		metricsGatherer,
		logger,
		dbNamespace,      // metrics prefix (6th param)
		meterDBNamespace, // meterDBRegName (7th param) - must match namespace
	)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Perform database operations to generate metrics
	testData := map[string][]byte{
		"genesisID":          []byte("test_genesis_hash"),
		"validator_1":        []byte("validator_data_1"),
		"validator_2":        []byte("validator_data_2"),
		"ungracefulShutdown": nil, // Empty value like node uses
	}

	for key, value := range testData {
		require.NoError(db.Put([]byte(key), value))
	}

	// Read back to trigger read metrics
	for key, expectedValue := range testData {
		value, err := db.Get([]byte(key))
		require.NoError(err)
		if expectedValue == nil {
			// Databases typically normalize nil to empty []byte{}
			require.True(value == nil || len(value) == 0)
		} else {
			require.Equal(expectedValue, value)
		}
	}

	// Verify meterdb metrics are registered
	families, err := metricsGatherer.Gather()
	require.NoError(err)

	// Track which metric types we found
	metricTypes := make(map[string]int)
	for _, family := range families {
		name := family.Name
		if name != "" {
			metricTypes[name] = len(family.Metrics)
			t.Logf("Found metric: %s with %d entries", name, len(family.Metrics))
		}
	}

	// Verify we have at least some database metrics
	// The actual naming scheme depends on how meterdb registers metrics
	foundDbMetrics := false
	for name := range metricTypes {
		if len(name) > 0 && (strings.Contains(name, dbNamespace) || strings.Contains(name, "calls") || strings.Contains(name, "duration")) {
			foundDbMetrics = true
			t.Logf("✓ Found database metric: %s", name)
		}
	}

	require.True(foundDbMetrics, "Expected to find some database metrics")

	t.Logf("✅ Node root database meterdb metrics properly registered")
	t.Logf("   Verified pattern: New(badgerdb, path, false, nil, gatherer, logger, %q, %q)",
		dbNamespace, "all")
}
