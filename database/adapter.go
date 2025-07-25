// Copyright (C) 2020-2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"context"

	"github.com/luxfi/node/api/health"
	luxdb "github.com/luxfi/db"
)

// Adapter wraps a luxfi/db Database to implement the node's Database interface
type Adapter struct {
	luxdb.Database
}

// NewAdapter creates a new adapter wrapping a luxfi/db Database
func NewAdapter(db luxdb.Database) Database {
	return &Adapter{Database: db}
}

// HealthCheck implements health.Checker interface
func (a *Adapter) HealthCheck(ctx context.Context) (interface{}, error) {
	// Call the underlying HealthCheck method
	err := a.Database.HealthCheck()
	if err != nil {
		return nil, err
	}
	return map[string]string{"status": "healthy"}, nil
}

// Ensure Adapter implements all required interfaces
var (
	_ Database      = (*Adapter)(nil)
	_ health.Checker = (*Adapter)(nil)
)