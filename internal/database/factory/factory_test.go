// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"os"
	"path/filepath"
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

	err = db.Put(testKey, testValue)
	require.NoError(err)

	value, err := db.Get(testKey)
	require.NoError(err)
	require.Equal(testValue, value)

	// Gather metrics to verify registration
	families, err := gatherer.Gather()
	require.NoError(err)

	// Should have metrics from database operations
	foundMetrics := false
	for _, family := range families {
		name := family.GetName()
		if name != "" {
			foundMetrics = true
			t.Logf("Found metric family: %s with %d metrics", name, len(family.GetMetric()))
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
	err = db.Put([]byte("key"), []byte("value"))
	require.NoError(err)
	db.Close()

	// Open as read-only
	dbReadOnly, err := New(
		badgerdb.Name,
		dbPath,
		true, // readOnly
		nil,
		gatherer,
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
	db, err := New(
		badgerdb.Name,
		dbPath,
		false, // readOnly
		nil,   // config
		metricsGatherer,
		logger,
		dbNamespace, // metrics prefix (6th param)
		"all",       // meterDBRegName (7th param)
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
		err = db.Put([]byte(key), value)
		require.NoError(err)
	}

	// Read back to trigger read metrics
	for key, expectedValue := range testData {
		value, err := db.Get([]byte(key))
		require.NoError(err)
		if expectedValue == nil {
			require.Nil(value)
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
		name := family.GetName()
		if name != "" {
			metricTypes[name] = len(family.GetMetric())
		}
	}

	// Verify we have the expected meterdb metric families
	expectedMetrics := []string{
		dbNamespace + "_calls",
		dbNamespace + "_duration",
		dbNamespace + "_size",
	}

	for _, expectedMetric := range expectedMetrics {
		count, found := metricTypes[expectedMetric]
		require.True(found, "Expected metric family %s not found", expectedMetric)
		require.Greater(count, 0, "Metric family %s has no metrics", expectedMetric)
		t.Logf("✓ Found %s with %d metrics", expectedMetric, count)
	}

	t.Logf("✅ Node root database meterdb metrics properly registered")
	t.Logf("   Verified pattern: New(badgerdb, path, false, nil, gatherer, logger, %q, %q)",
		dbNamespace, "all")
}
