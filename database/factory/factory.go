// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"fmt"

	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/db"
	"github.com/luxfi/db/corruptabledb"
	"github.com/luxfi/db/leveldb"
	"github.com/luxfi/db/memdb"
	"github.com/luxfi/db/meterdb"
	"github.com/luxfi/db/pebbledb"
	"github.com/luxfi/db/versiondb"
	"github.com/luxfi/node/utils/logging"
)

// New creates a new database instance based on the provided configuration.
//
// It also wraps the database with a corruptable DB and a meter DB.
//
// dbName is the name of the database, either leveldb, memdb, or pebbledb.
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
	logger logging.Logger,
	metricsPrefix string,
	meterDBRegName string,
) (database.Database, error) {
	dbRegisterer, err := metrics.MakeAndRegister(
		gatherer,
		metricsPrefix,
	)
	if err != nil {
		return nil, err
	}
	var db database.Database
	// start the db
	switch name {
	case leveldb.Name:
		db, err = leveldb.New(path, config, logger, dbRegisterer)
		if err != nil {
			return nil, fmt.Errorf("couldn't create %s at %s: %w", leveldb.Name, path, err)
		}
	case memdb.Name:
		db = memdb.New()
	case pebbledb.Name:
		db, err = pebbledb.New(path, config, logger, dbRegisterer)
		if err != nil {
			return nil, fmt.Errorf("couldn't create %s at %s: %w", pebbledb.Name, path, err)
		}
	default:
		return nil, fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			name,
			leveldb.Name,
			memdb.Name,
			pebbledb.Name,
		)
	}

	// Wrap with corruptable DB
	db = corruptabledb.New(db, logger)

	if readOnly && name != memdb.Name {
		db = versiondb.New(db)
	}

	meterDBReg, err := metrics.MakeAndRegister(
		gatherer,
		meterDBRegName,
	)
	if err != nil {
		return nil, err
	}

	db, err = meterdb.New(meterDBReg, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create meterdb: %w", err)
	}

	return db, nil
}
