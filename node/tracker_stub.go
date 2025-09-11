// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"time"

	"github.com/luxfi/ids"
)

// tracker package stubs for compilation
type Targeter interface {
	TargetUsage() uint64
}

type TargeterConfig struct {
	// VdrAlloc is the percentage of total stake to allocate to validators
	VdrAlloc float64
	// MaxNonVdrUsage is the max percentage of non-validator usage
	MaxNonVdrUsage float64
	// MaxNonVdrNodeUsage is the max percentage of non-validator node usage
	MaxNonVdrNodeUsage float64
}

type noOpTargeter struct{}

func (n *noOpTargeter) TargetUsage() uint64 {
	return 50 // Default 50% usage as uint64
}

func NewTargeter(config *TargeterConfig) Targeter {
	return &noOpTargeter{}
}

// HealthConfig for health checks
type HealthConfig struct {
	MaxTimeSinceMsgReceived time.Duration
	MaxTimeSinceMsgSent     time.Duration
	MaxPortionSendQueueFull float64
	MinConnectedPeers       uint
	ReadTimeout             time.Duration
	WriteTimeout            time.Duration
	MaxSendFailRate         float64
}

// Router extension methods
type ExtendedRouter interface {
	Connected(ctx context.Context, nodeID ids.NodeID) error
	Disconnected(ctx context.Context, nodeID ids.NodeID) error
}