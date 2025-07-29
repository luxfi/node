// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package consensus

import (
	"context"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/consensus/validators"
	"github.com/prometheus/client_golang/prometheus"
)

// Context contains the state that is used by consensus engines.
type Context struct {
	// NetworkID is the ID of the network this node is connected to.
	NetworkID uint32

	// ChainID is the chain this engine is working on.
	ChainID ids.ID

	// SubnetID is the subnet this engine is working on.
	SubnetID ids.ID

	// NodeID is the ID of this node.
	NodeID ids.NodeID

	// BCLookup maps aliases to chain IDs.
	BCLookup BCLookup

	// Registerer for registering metrics.
	Registerer Registerer

	// Log is used for logging messages.
	Log Logger

	// Lock is used to synchronize access to shared resources.
	Lock sync.Locker

	// ValidatorSet contains the validators for this subnet.
	ValidatorSet validators.Set

	// ValidatorState provides access to validator information.
	ValidatorState ValidatorState

	// Sender is used to send messages to other nodes.
	Sender Sender

	// Bootstrappers are the nodes that are used to bootstrap this chain.
	Bootstrappers []ids.NodeID

	// StartTime is the time this engine started.
	StartTime time.Time

	// RequestID is used to create unique request IDs.
	RequestID *RequestID

	// LUXAssetID is the ID of the LUX asset.
	LUXAssetID ids.ID

	// State represents the current consensus state
	State *EngineState
}

// ContextInitializable defines an interface for objects that need context initialization
type ContextInitializable interface {
	InitCtx(ctx *Context)
}

// ValidatorState provides access to validator information
type ValidatorState interface {
	// GetMinimumHeight returns the minimum height of the P-chain.
	GetMinimumHeight(ctx context.Context) (uint64, error)

	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight(ctx context.Context) (uint64, error)

	// GetSubnetID returns the subnet ID for the given chain ID.
	GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error)

	// GetValidatorSet returns the validators of the given subnet at the
	// given P-chain height.
	GetValidatorSet(
		ctx context.Context,
		height uint64,
		subnetID ids.ID,
	) (map[ids.NodeID]*validators.GetValidatorOutput, error)
}


// BCLookup is the interface for looking up chain IDs by alias
type BCLookup interface {
	Lookup(alias string) (ids.ID, error)
	PrimaryAlias(id ids.ID) (string, error)
}

// For missing imports
type (
	Registerer prometheus.Registerer
	Logger     log.Logger
	Sender     interface{}
	RequestID  struct{}
)