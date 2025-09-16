// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"time"

	"github.com/luxfi/node/network/tracker"
)

// Type aliases for router types
type (
	ChainRouter = Router
	Targeter    = tracker.Targeter
)

// HealthConfig for router health monitoring
type HealthConfig struct {
	Enabled                      bool          `json:"enabled"`
	PollingInterval              time.Duration `json:"pollingInterval"`
	MaxOutstandingRequestDuration time.Duration `json:"maxOutstandingRequestDuration"`
	MaxTimeSinceMsgReceived      time.Duration `json:"maxTimeSinceMsgReceived"`
	MaxTimeSinceMsgSent          time.Duration `json:"maxTimeSinceMsgSent"`
	MaxPortionSentQueueBytesFull float64      `json:"maxPortionSentQueueBytesFull"`
	MaxPortionSendQueueFull      float64      `json:"maxPortionSendQueueFull"`
	MaxSendFailRate              float64      `json:"maxSendFailRate"`
	MinConnectedPeers            int          `json:"minConnectedPeers"`
	ReadTimeout                  time.Duration `json:"readTimeout"`
	WriteTimeout                 time.Duration `json:"writeTimeout"`
}