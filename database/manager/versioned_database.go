// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"github.com/luxdefi/node/database"
	"github.com/luxdefi/node/utils"
	"github.com/luxdefi/node/version"
)

var _ utils.Sortable[*VersionedDatabase] = (*VersionedDatabase)(nil)

type VersionedDatabase struct {
	Database database.Database
	Version  *version.Semantic
}

// Close the underlying database
func (db *VersionedDatabase) Close() error {
	return db.Database.Close()
}

// Note this sorts in descending order (newest version --> oldest version)
func (db *VersionedDatabase) Less(other *VersionedDatabase) bool {
	return db.Version.Compare(other.Version) > 0
}
