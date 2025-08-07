// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcdb provides a wrapper to bridge proto and db/rpcdb packages
package rpcdb

import (
	"github.com/luxfi/database"
	"github.com/luxfi/node/db/rpcdb"
	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
)

// NewServer creates a new database server wrapper
func NewServer(db database.Database) *rpcdb.DatabaseServer {
	return rpcdb.NewServer(db)
}

// NewClient creates a new database client wrapper
func NewClient(client rpcdbpb.DatabaseClient) *rpcdb.DatabaseClient {
	return rpcdb.NewClient(client)
}