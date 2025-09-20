package leveldb

import "github.com/luxfi/database"

// Database wraps a LevelDB instance
type Database struct {
	database.Database
}

// New creates a new LevelDB database
func New(path string, cacheSize int) (*Database, error) {
	return &Database{}, nil
}
