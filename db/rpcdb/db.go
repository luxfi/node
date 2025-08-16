// Package rpcdb re-exports the proto/rpcdb package for backwards compatibility
package rpcdb

import (
	"github.com/luxfi/node/proto/rpcdb"
)

// Type aliases for backwards compatibility
type (
	DatabaseClient = rpcdb.DatabaseClient
	DatabaseServer = rpcdb.DatabaseServer
)

// Function aliases for backwards compatibility
var (
	NewClient = rpcdb.NewClient
	NewServer = rpcdb.NewServer
)
