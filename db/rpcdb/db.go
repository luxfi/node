//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcdb re-exports the proto/rpcdb package for backwards compatibility
package rpcdb

import "github.com/luxfi/node/proto/rpcdb"

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
