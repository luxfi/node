// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package testcontext provides a test context for platformvm tests
package testcontext

import (
	"context"
	"sync"

	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/chains/atomic"
)

// Context provides a test context that mimics the old context.Context
// for compatibility with existing tests
type Context struct {
	context.Context
	NetworkID    uint32
	NetID        ids.ID
	ChainID      ids.ID
	NodeID       ids.NodeID
	XChainID     ids.ID
	CChainID     ids.ID
	XAssetID   ids.ID
	Log          log.Logger
	Lock         *sync.RWMutex
	SharedMemory atomic.SharedMemory
	BCLookup     ids.AliaserReader
}

// New creates a new test context
func New(ctx context.Context) *Context {
	return &Context{
		Context: ctx,
		Lock:    &sync.RWMutex{},
		Log:     log.NoLog{},
	}
}

// WithIDs sets the IDs from consensus.IDs
func (c *Context) WithIDs(ids consensus.IDs) *Context {
	c.NetworkID = ids.NetworkID
	c.ChainID = ids.ChainID
	c.NetID = ids.NetID
	c.NodeID = ids.NodeID
	c.XAssetID = ids.LUXAssetID
	return c
}
