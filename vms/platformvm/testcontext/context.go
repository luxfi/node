// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package testcontext provides a test context for platformvm tests
package testcontext

import (
	"context"
	"sync"

	"github.com/luxfi/consensus/runtime"
	consensusvalidators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/vm/chains/atomic"
)

// Context provides a test context that mimics the old context.Context
// for compatibility with existing tests
type Context struct {
	context.Context
	NetworkID      uint32 // 1=mainnet, 2=testnet
	ChainID        ids.ID
	NodeID         ids.NodeID
	PublicKey      interface{} // BLS public key
	XChainID       ids.ID
	CChainID       ids.ID
	DChainID       ids.ID
	XAssetID       ids.ID // Primary asset ID (X-chain native)
	ValidatorState consensusvalidators.State
	WarpSigner     runtime.WarpSigner
	Log            log.Logger
	Lock           *sync.RWMutex
	SharedMemory   atomic.SharedMemory
	BCLookup       ids.AliaserReader
	ChainDataDir   string
	Keystore       interface{}
	Signer         interface{}
}

// New creates a new test context
func New(ctx context.Context) *Context {
	return &Context{
		Context: ctx,
		Lock:    &sync.RWMutex{},
		Log:     log.Noop(),
	}
}

// WithIDs sets the IDs from runtime.IDs
func (c *Context) WithIDs(ids runtime.IDs) *Context {
	c.NetworkID = ids.NetworkID
	c.ChainID = ids.ChainID
	c.NodeID = ids.NodeID
	c.XAssetID = ids.XAssetID
	return c
}
