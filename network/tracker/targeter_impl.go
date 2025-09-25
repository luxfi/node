package tracker

import (
	"sync"
	"sync/atomic"
)

// NewTargeter creates a new targeter with configurable target usage
func NewTargeter(config *TargeterConfig) Targeter {
	targetUsage := uint64(80) // Default to 80%
	if config != nil {
		if config.VdrAlloc > 0 {
			targetUsage = uint64(config.VdrAlloc * 100)
		}
	}

	t := &targeterImpl{
		targetUsage: targetUsage,
	}
	t.targetUsage = targetUsage
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
