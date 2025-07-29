// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package timeout

import (
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/utils/timer"
	"github.com/luxfi/node/network/p2p"
	"github.com/prometheus/client_golang/prometheus"
)

// Manager manages request timeouts
type Manager interface {
	// RegisterRequest registers a request and returns the time it was registered
	RegisterRequest(nodeID ids.NodeID, chainID ids.ID, requestID uint32, responseOp message.Op, timeoutMsg message.InboundMessage, engineType p2p.EngineType) time.Time

	// RegisterResponse registers that a response was received
	RegisterResponse(nodeID ids.NodeID, chainID ids.ID, requestID uint32, responseOp message.Op, latency time.Duration) (time.Time, bool)

	// TimeoutDuration returns the current timeout duration
	TimeoutDuration() time.Duration

	// Stop stops the timeout manager
	Stop()
}

// manager implements Manager
type manager struct {
	lock            sync.Mutex
	tm              timer.AdaptiveTimeoutManager
	pendingRequests map[requestKey]*requestInfo
	timeoutDuration time.Duration
}

type requestKey struct {
	nodeID    ids.NodeID
	chainID   ids.ID
	requestID uint32
}

type requestInfo struct {
	responseOp  message.Op
	timeoutMsg  message.InboundMessage
	engineType  p2p.EngineType
	sentTime    time.Time
}

// NewManager returns a new timeout manager
func NewManager(config *timer.AdaptiveTimeoutConfig, reg prometheus.Registerer) (Manager, error) {
	tm, err := timer.NewAdaptiveTimeoutManager(config, reg)
	if err != nil {
		return nil, err
	}

	return &manager{
		tm:              tm,
		pendingRequests: make(map[requestKey]*requestInfo),
		timeoutDuration: config.InitialTimeout,
	}, nil
}

func (m *manager) RegisterRequest(nodeID ids.NodeID, chainID ids.ID, requestID uint32, responseOp message.Op, timeoutMsg message.InboundMessage, engineType p2p.EngineType) time.Time {
	m.lock.Lock()
	defer m.lock.Unlock()

	now := time.Now()
	key := requestKey{
		nodeID:    nodeID,
		chainID:   chainID,
		requestID: requestID,
	}

	m.pendingRequests[key] = &requestInfo{
		responseOp: responseOp,
		timeoutMsg: timeoutMsg,
		engineType: engineType,
		sentTime:   now,
	}

	// Schedule timeout
	reqID := ids.RequestID{
		NodeID:    nodeID,
		ChainID:   chainID,
		RequestID: requestID,
	}
	m.tm.Put(reqID, true, func() {
		// Handle timeout by passing the timeout message
	})

	return now
}

func (m *manager) RegisterResponse(nodeID ids.NodeID, chainID ids.ID, requestID uint32, responseOp message.Op, latency time.Duration) (time.Time, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	key := requestKey{
		nodeID:    nodeID,
		chainID:   chainID,
		requestID: requestID,
	}

	info, exists := m.pendingRequests[key]
	if !exists {
		return time.Time{}, false
	}

	delete(m.pendingRequests, key)
	reqID := ids.RequestID{
		NodeID:    nodeID,
		ChainID:   chainID,
		RequestID: requestID,
	}
	m.tm.Remove(reqID)

	return info.sentTime, true
}

func (m *manager) TimeoutDuration() time.Duration {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.timeoutDuration
}

func (m *manager) Stop() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.tm.Stop()
}