// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
	databasefactory "github.com/luxfi/node/internal/database/factory"
	"github.com/luxfi/node/service/metrics"
	"github.com/stretchr/testify/require"
)

// TestNodeDatabaseCreation verifies that the root database
// can be created with meterdb wrapping via the factory.
func TestNodeDatabaseCreation(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "meterdb_test")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "db")

	gatherer := metrics.NewMultiGatherer()
	logger := log.NoLog{}

	db, err := databasefactory.New(
		badgerdb.Name,
		dbPath,
		false,
		nil,
		gatherer,
		logger,
		"lux_db",
		"lux_meterdb",
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
}
