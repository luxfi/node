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

// TestDatabaseWithMetricsCreation verifies that the database factory
// creates a database with metrics enabled without error.
func TestDatabaseWithMetricsCreation(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "metrics_creation_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")
	logger := log.NoLog{}

	gatherer := metrics.NewMultiGatherer()
	require.NotNil(gatherer, "Gatherer should not be nil")

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
	require.NoError(err, "Database creation should succeed")
	require.NotNil(db, "Database should not be nil")
	defer db.Close()

	// Verify database operations work
	testKey := []byte("test_key")
	testValue := []byte("test_value")

	require.NoError(db.Put(testKey, testValue))

	value, err := db.Get(testKey)
	require.NoError(err)
	require.Equal(testValue, value)
}
