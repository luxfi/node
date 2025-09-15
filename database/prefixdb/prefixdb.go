package prefixdb

import "github.com/luxfi/node/database"

// New creates a new prefixed database
func New(prefix []byte, db database.Database) database.Database {
	return db
}
