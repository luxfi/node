// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/set"
)

// Peers tracks the peers that have been sent messages
type Peers interface {
	// Connected adds a new peer
	Connected(nodeID ids.NodeID)

	// Disconnected removes a peer
	Disconnected(nodeID ids.NodeID)

	// QueryFailed notifies the tracker that a query to a validator failed
	QueryFailed(nodeID ids.NodeID)

	// GetPeers returns the set of connected peers
	GetPeers() set.Set[ids.NodeID]
}

// Tracker tracks the progress of operations
type Tracker interface {
	// IsBootstrapped returns true if the chain is done bootstrapping
	IsBootstrapped() bool

	// Bootstrapped marks the chain as done bootstrapping
	Bootstrapped()

	// RegisterRequest registers an outstanding request
	RegisterRequest(nodeID ids.NodeID, requestID uint32, msgType string) (time.Time, bool)

	// RegisterResponse registers a response to a request
	RegisterResponse(nodeID ids.NodeID, requestID uint32, msgType string, latency time.Duration)
}

// StartupTracker tracks the startup progress
type StartupTracker interface {
	Tracker
	
	// ShouldStart returns true if the startup process should start
	ShouldStart() bool
}

// tracker implements Tracker
type tracker struct {
	lock          sync.RWMutex
	bootstrapped  utils.Atomic[bool]
	requests      map[ids.NodeID]map[uint32]*requestInfo
}

type requestInfo struct {
	msgType  string
	sentTime time.Time
}

// NewTracker returns a new Tracker
func NewTracker() Tracker {
	return &tracker{
		requests: make(map[ids.NodeID]map[uint32]*requestInfo),
	}
}

func (t *tracker) IsBootstrapped() bool {
	return t.bootstrapped.Get()
}

func (t *tracker) Bootstrapped() {
	t.bootstrapped.Set(true)
}

func (t *tracker) RegisterRequest(nodeID ids.NodeID, requestID uint32, msgType string) (time.Time, bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	now := time.Now()
	
	nodeReqs, exists := t.requests[nodeID]
	if !exists {
		nodeReqs = make(map[uint32]*requestInfo)
		t.requests[nodeID] = nodeReqs
	}
	
	if _, exists := nodeReqs[requestID]; exists {
		return now, false
	}
	
	nodeReqs[requestID] = &requestInfo{
		msgType:  msgType,
		sentTime: now,
	}
	
	return now, true
}

func (t *tracker) RegisterResponse(nodeID ids.NodeID, requestID uint32, msgType string, latency time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	
	nodeReqs, exists := t.requests[nodeID]
	if !exists {
		return
	}
	
	delete(nodeReqs, requestID)
	if len(nodeReqs) == 0 {
		delete(t.requests, nodeID)
	}
}