// Copyright (C) 2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/set"
)

// Getter defines the interface for fetching blocks
type Getter interface {
	// Get retrieves blocks by their IDs
	Get(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		blockIDs set.Set[ids.ID],
	) error

	// GetAncestors retrieves ancestors of a block
	GetAncestors(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		blockID ids.ID,
	) error
}

// Manager manages block fetching operations
type Manager struct {
	getter    Getter
	timeout   time.Duration
	pending   map[uint32]*request
	nextReqID uint32
}

type request struct {
	nodeID    ids.NodeID
	blockIDs  set.Set[ids.ID]
	startTime time.Time
}

// NewManager creates a new getter manager
func NewManager(getter Getter, timeout time.Duration) *Manager {
	return &Manager{
		getter:  getter,
		timeout: timeout,
		pending: make(map[uint32]*request),
	}
}

// Get initiates a block fetch request
func (m *Manager) Get(ctx context.Context, nodeID ids.NodeID, blockIDs set.Set[ids.ID]) error {
	reqID := m.nextReqID
	m.nextReqID++

	m.pending[reqID] = &request{
		nodeID:    nodeID,
		blockIDs:  blockIDs,
		startTime: time.Now(),
	}

	return m.getter.Get(ctx, nodeID, reqID, blockIDs)
}

// GetAncestors initiates an ancestor fetch request
func (m *Manager) GetAncestors(ctx context.Context, nodeID ids.NodeID, blockID ids.ID) error {
	reqID := m.nextReqID
	m.nextReqID++

	return m.getter.GetAncestors(ctx, nodeID, reqID, blockID)
}

// OnResponse handles responses to fetch requests
func (m *Manager) OnResponse(reqID uint32) {
	delete(m.pending, reqID)
}

// TimeoutPending removes timed out requests
func (m *Manager) TimeoutPending() {
	now := time.Now()
	for reqID, req := range m.pending {
		if now.Sub(req.startTime) > m.timeout {
			delete(m.pending, reqID)
		}
	}
}

// Outstanding returns the number of outstanding requests
func (m *Manager) Outstanding() int {
	return len(m.pending)
}
