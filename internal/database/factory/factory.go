// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"github.com/luxfi/database"
	dbfactory "github.com/luxfi/database/factory"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/metrics"
)

// New creates a new database instance based on the provided configuration.
//
// This is a thin wrapper around luxfi/database/factory.New that adapts
// the node's MultiGatherer to the database factory's expected interface.
//
// dbName is the name of the database, either leveldb, memdb, pebbledb, or badgerdb.
// dbPath is the path to the database folder.
// readOnly indicates if the database should be read-only.
// dbConfig is the database configuration in JSON format.
// dbMetricsPrefix is used to create a new metrics registerer for the database.
// meterDBRegName is used to create a new metrics registerer for the meter DB.
func New(
	name string,
	path string,
	readOnly bool,
	config []byte,
	gatherer metrics.MultiGatherer,
	logger log.Logger,
	metricsPrefix string,
	meterDBRegName string,
) (database.Database, error) {
	// Use the luxfi/database/factory.New which properly handles all database types
	// The factory handles LevelDB, PebbleDB, BadgerDB, and MemDB with correct signatures
	return dbfactory.New(
		name,
		path,
		readOnly,
		config,
		gatherer, // MultiGatherer implements the interface the factory expects
		logger,
		metricsPrefix,
		meterDBRegName,
	)
}
