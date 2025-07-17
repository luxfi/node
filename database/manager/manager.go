// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"github.com/luxfi/node/database"
)

// Manager manages database instances
type Manager interface {
	// Current returns the current database
	Current() database.Database
	// Close closes the manager
	Close() error
}

type manager struct {
	db database.Database
}

// NewManager creates a new database manager
func NewManager(db database.Database) Manager {
	return &manager{db: db}
}

func (m *manager) Current() database.Database {
	return m.db
}

func (m *manager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}