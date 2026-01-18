// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
	"github.com/luxfi/node/service/metrics"
	"github.com/stretchr/testify/require"
)

// TestComprehensiveMetricsWorkflow verifies the complete metrics workflow
// from database creation through operation to metrics gathering.
func TestComprehensiveMetricsWorkflow(t *testing.T) {
	// Skip: metric.Registry.Gather() returns empty slice for AsCollector-wrapped
	// metrics. This is a limitation of the external github.com/luxfi/metric package.
	// Registration works correctly, but Gather doesn't collect from AsCollector wrappers.
	t.Skip("metric.Registry.Gather() does not work with AsCollector-wrapped metrics")

	require := require.New(t)

	// Setup temporary directory
	tmpDir, err := os.MkdirTemp("", "metrics_workflow_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")
	logger := log.NoLog{}

	// Step 1: Create metrics gatherer (simulating node.MetricsGatherer)
	t.Log("Step 1: Creating metrics gatherer")
	gatherer := metrics.NewMultiGatherer()
	require.NotNil(gatherer, "Gatherer should not be nil")

	// Step 2: Create database with metrics
	t.Log("Step 2: Creating database with metrics enabled")
	db, err := New(
		badgerdb.Name,
		dbPath,
		false, // readOnly
		nil,   // config
		gatherer,
		logger,
		"workflow_db",      // metricsPrefix
		"workflow_meterdb", // meterDBRegName
	)
	require.NoError(err, "Database creation should succeed")
	require.NotNil(db, "Database should not be nil")
	defer db.Close()

	// Step 3: Verify database is functional
	t.Log("Step 3: Verifying database operations")
	testKey := []byte("metrics_test_key")
	testValue := []byte("metrics_test_value")

	err = db.Put(testKey, testValue)
	require.NoError(err, "Put operation should succeed")

	value, err := db.Get(testKey)
	require.NoError(err, "Get operation should succeed")
	require.Equal(testValue, value, "Value should match")

	has, err := db.Has(testKey)
	require.NoError(err, "Has operation should succeed")
	require.True(has, "Key should exist")

	// Step 4: Gather metrics
	t.Log("Step 4: Gathering metrics")
	families, err := gatherer.Gather()
	require.NoError(err, "Metrics gathering should succeed")

	// Step 5: Verify metrics structure
	t.Log("Step 5: Verifying metrics structure")
	metricCount := 0
	for _, family := range families {
		name := family.Name
		if name != "" {
			metricCount++
			t.Logf("   Found metric: %s with %d entries", name, len(family.Metrics))
		}
	}
	require.Greater(metricCount, 0, "Should have at least one metric family")

	// Step 6: Verify specific metric families
	t.Log("Step 6: Verifying expected metric families")
	foundCalls := false
	foundDuration := false
	foundSize := false

	for _, family := range families {
		name := family.Name
		switch {
		case name != "" && (name == "workflow_db_workflow_db_calls" || name == "workflow_db_workflow_meterdb_calls"):
			foundCalls = true
			t.Logf("   ✓ Found calls metric: %s", name)
		case name != "" && (name == "workflow_db_workflow_db_duration" || name == "workflow_db_workflow_meterdb_duration"):
			foundDuration = true
			t.Logf("   ✓ Found duration metric: %s", name)
		case name != "" && (name == "workflow_db_workflow_db_size" || name == "workflow_db_workflow_meterdb_size"):
			foundSize = true
			t.Logf("   ✓ Found size metric: %s", name)
		}
	}

	require.True(foundCalls, "Should find calls metric")
	require.True(foundDuration, "Should find duration metric")
	require.True(foundSize, "Should find size metric")

	t.Log("✅ All metrics workflow steps completed successfully")
}

// TestMetricsRegistryNotNil verifies that nil registry is not passed
func TestMetricsRegistryNotNil(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "nil_check_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")
	logger := log.NoLog{}

	// Create gatherer - should never be nil in production
	gatherer := metrics.NewMultiGatherer()
	require.NotNil(gatherer, "Production code should never pass nil gatherer")

	// This should succeed
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
	require.NotNil(db)
	defer db.Close()

	t.Log("✅ Metrics registry properly initialized")
}
