// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"time"

	"github.com/luxfi/ids"
)

// StateSyncer defines the interface for state synchronization
type StateSyncer interface {
	// Start starts the state sync process
	Start(context.Context, uint64) error

	// IsEnabled returns true if state sync is enabled
	IsEnabled() bool

	// GetStateSummaryFrontier retrieves the state summary frontier
	GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error

	// GetAcceptedStateSummary retrieves the accepted state summary
	GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights []uint64) error

	// StateSummaryFrontier handles state summary frontier response
	StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error

	// AcceptedStateSummary handles accepted state summary response
	AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error

	// GetBlock retrieves a block
	GetBlock(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockID ids.ID) error

	// Block handles a block response
	Block(ctx context.Context, nodeID ids.NodeID, requestID uint32, block []byte) error
}

// Config configures the state syncer
type Config struct {
	Enabled        bool
	StartHeight    uint64
	TargetHeight   uint64
	RequestTimeout time.Duration
}

// Manager manages state sync operations
type Manager interface {
	// GetOngoingSyncs returns the set of node IDs currently syncing
	GetOngoingSyncs() []ids.NodeID

	// GetSyncStatus returns the sync status for a node
	GetSyncStatus(nodeID ids.NodeID) (SyncStatus, bool)

	// StartSync starts syncing with a node
	StartSync(nodeID ids.NodeID) error

	// EndSync ends syncing with a node
	EndSync(nodeID ids.NodeID) error
}

// SyncStatus represents the status of a sync operation
type SyncStatus struct {
	StartTime     time.Time
	StartHeight   uint64
	CurrentHeight uint64
	TargetHeight  uint64
}