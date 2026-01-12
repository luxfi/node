// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/metrics"
	databasefactory "github.com/luxfi/node/internal/database/factory"
	"github.com/stretchr/testify/require"
)

// TestMeterDBMetricsRegistration verifies that the root database
// properly registers meterdb metrics when created via the factory.
func TestMeterDBMetricsRegistration(t *testing.T) {
	require := require.New(t)

	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "meterdb_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	// Create metrics gatherer
	gatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	// Create database with metrics
	db, err := databasefactory.New(
		badgerdb.Name,
		dbPath,
		false, // readOnly
		nil,   // config
		gatherer,
		logger,
		"lux_db",      // dbNamespace
		"lux_meterdb", // meterDBNamespace
	)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Write a test key-value pair to trigger metrics
	testKey := []byte("test_key")
	testValue := []byte("test_value")
	require.NoError(db.Put(testKey, testValue))

	// Read it back to trigger read metrics
	value, err := db.Get(testKey)
	require.NoError(err)
	require.Equal(testValue, value)

	// Verify metrics are registered by attempting to gather them
	// The meterdb should have been wrapped by the factory
	families, err := gatherer.Gather()
	require.NoError(err)

	// Should have some metrics from meterdb operations
	foundDBMetrics := false
	for _, family := range families {
		if family.Name != "" {
			foundDBMetrics = true
			t.Logf("Found metric: %s", family.Name)
		}
	}

	require.True(foundDBMetrics, "No database metrics found after operations")
	t.Logf("Successfully verified %d metric families registered", len(families))
}
