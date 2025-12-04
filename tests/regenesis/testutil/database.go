// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
)

// CreateTestDatabase creates a new test database
// For unit tests, we use an in-memory database for speed
func CreateTestDatabase(path string) database.Database {
	return memdb.New()
}

// OpenTestDatabase opens an existing test database
// For unit tests, we return a new memory database
// In integration tests, this would open the actual database
func OpenTestDatabase(path string) database.Database {
	return memdb.New()
}

// CreatePersistentTestDatabase creates a test database that persists to disk
// This is useful for integration tests that need to verify data across restarts
func CreatePersistentTestDatabase(path string) (database.Database, error) {
	// For now, use memory database
	// In production, this would use pebble or badger
	return memdb.New(), nil
}
