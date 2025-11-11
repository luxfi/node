// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
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
	logger := log.NewZeroLog()

	// Create database with metrics
	db, err := databasefactory.New(
		badgerdb.Name,
		dbPath,
		false, // readOnly
		nil,   // config
		gatherer,
		logger,
		dbNamespace,     // "lux_db"
		meterDBNamespace, // "lux_meterdb"
	)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Write a test key-value pair to trigger metrics
	testKey := []byte("test_key")
	testValue := []byte("test_value")
	err = db.Put(testKey, testValue)
	require.NoError(err)

	// Read it back to trigger read metrics
	value, err := db.Get(testKey)
	require.NoError(err)
	require.Equal(testValue, value)

	// Verify metrics are registered
	// The meterdb metrics should be registered under the meterDBNamespace
	registries := gatherer.Registries()
	require.NotEmpty(registries, "No metrics registries found")

	// Look for meterdb metrics
	found := false
	for name := range registries {
		if name == dbNamespace || name == meterDBNamespace {
			found = true
			t.Logf("Found database metrics registry: %s", name)
		}
	}
	require.True(found, "Database metrics not registered in MultiGatherer")

	// Verify we can collect metrics without error
	metricFamilies, err := metric.NewAPIGatherer(gatherer, t.Logf).Gather()
	require.NoError(err)
	require.NotEmpty(metricFamilies, "No metrics collected")

	t.Logf("Successfully collected %d metric families", len(metricFamilies))
}
