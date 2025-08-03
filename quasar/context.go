// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"sync"

	"github.com/luxfi/node/api/keystore"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// Consensus represents a general consensus instance
type Consensus interface{}

// Context is the interface for VM contexts
type Context struct {
	NetworkID uint32
	SubnetID  ids.ID
	ChainID   ids.ID
	NodeID    ids.NodeID

	XChainID    ids.ID
	CChainID    ids.ID
	AVAXAssetID ids.ID

	Log          log.Logger
	Lock         sync.RWMutex
	Keystore     keystore.BlockchainKeystore
	SharedMemory atomic.SharedMemory
	BCLookup     ids.AliaserReader
	Metrics      map[string]interface{}

	// snowman/block.ChainVM uses ValidatorState as a special case for the
	// Platform Chain VM.
	ValidatorState interface{}

	// ChainDataDir is the root directory of this blockchain's
	// database.
	ChainDataDir string
}

// SubnetOnlyValidator validates a subnet only
type SubnetOnlyValidator struct {
	ValidationID     ids.ID
	SubnetID         ids.ID
	NodeID           ids.NodeID
	PublicKey        []byte
	RemainingBalance uint64
	Weight           uint64
	MinNonce         uint64
	EndAccumulatedFee uint64
}

// ContextInitializable represents something that can be initialized
// given a *Context
type ContextInitializable interface {
	// InitCtx initializes an object provided a *Context object
	InitCtx(ctx *Context)
}