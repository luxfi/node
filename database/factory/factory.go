// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"fmt"

	"github.com/luxfi/database"
	"github.com/luxfi/database/leveldb"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/pebbledb"
	"github.com/luxfi/log"
)

// Factory creates new database instances
type Factory interface {
	// New returns a new database instance
	New(config *Config) (database.Database, error)
}

// Config contains database configuration
type Config struct {
	// The data directory for the database
	Dir string

	// The name of the database
	Name string

	// The engine to use for the database
	Engine Engine

	// Logger to use for the database
	Log log.Logger

	// LevelDB specific configuration
	CacheSize        int
	WriteBufferSize  int
	HandleCap        int
	BitsPerKey       int
	CompactionTableSize      int
	CompactionTableSizeMultiplier float64
	CompactionTotalSize           int
	CompactionTotalSizeMultiplier float64

	// Pebble specific configuration
	BytesPerSync                int
	WALBytesPerSync             int
	MemTableStopWritesThreshold int
	MemTableSize                uint64
	MaxOpenFiles                int
	MaxConcurrentCompactions    func() int
}

// Engine is the type of database engine
type Engine int

const (
	// LevelDB is a LevelDB database
	LevelDB Engine = iota
	// Memory is an in-memory database
	Memory
	// Pebble is a PebbleDB database
	Pebble
)

// DefaultFactory is the default implementation of Factory
type DefaultFactory struct{}

// New creates a new database instance based on the configuration
func (f *DefaultFactory) New(config *Config) (database.Database, error) {
	switch config.Engine {
	case LevelDB:
		// leveldb.New expects: path string, blockCacheSize int, writeCacheSize int, handleCap int
		return leveldb.New(
			config.Dir+"/"+config.Name,
			config.CacheSize,
			config.WriteBufferSize,
			config.HandleCap,
		)
	case Pebble:
		// pebbledb.New expects: path string, cacheSize int, handles int, namespace string, readonly bool
		return pebbledb.New(
			config.Dir+"/"+config.Name,
			config.CacheSize,
			config.HandleCap,
			"", // empty namespace
			false, // not readonly
		)
	case Memory:
		return memdb.New(), nil
	default:
		return nil, fmt.Errorf("unknown database engine: %d", config.Engine)
	}
}

// New returns a new default factory
func New() Factory {
	return &DefaultFactory{}
}