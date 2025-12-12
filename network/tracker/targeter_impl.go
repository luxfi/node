// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"
	"sync/atomic"
)

// NewTargeter creates a new targeter with configurable target usage
// For disk throttling, VdrAlloc should be in bytes/second (e.g., 1000 GiB)
// For CPU throttling, VdrAlloc should be the number of CPU cores
func NewTargeter(config *TargeterConfig) Targeter {
	// Default to a very high value (1 TB/s) to effectively disable throttling
	// unless explicitly configured
	targetUsage := uint64(1024 * 1024 * 1024 * 1024) // 1 TB
	if config != nil {
		if config.VdrAlloc > 0 {
			// If VdrAlloc is specified, use it directly as bytes/second or CPU cores
			// For disk, this should be a large value like 1000 GiB
			// For CPU, this should be the number of CPU cores
			targetUsage = uint64(config.VdrAlloc)
		}
	}

	t := &targeterImpl{
		targetUsage: targetUsage,
	}
	return t
}

// TargeterConfig defines resource allocation configuration
type TargeterConfig struct {
	// VdrAlloc is the percentage of resource usage attributed to validators (0-1)
	VdrAlloc float64
	// MaxNonVdrUsage is the max resource usage for non-validators (0-1)
	MaxNonVdrUsage float64
	// MaxNonVdrNodeUsage is the max resource usage per non-validator node (0-1)
	MaxNonVdrNodeUsage float64
}

type targeterImpl struct {
	targetUsage uint64
	mu          sync.RWMutex
}

func (t *targeterImpl) TargetUsage() uint64 {
	return atomic.LoadUint64(&t.targetUsage)
}

func (t *targeterImpl) SetTargetUsage(usage uint64) {
	if usage > 100 {
		usage = 100
	}
	atomic.StoreUint64(&t.targetUsage, usage)
}
