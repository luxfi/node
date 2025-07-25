//go:build evm_nostats

package statesync

import (
	"time"
	"github.com/luxfi/geth/common"
)

// trieSyncStats is a stub implementation when stats are disabled
type trieSyncStats struct{}

func newTrieSyncStats() *trieSyncStats {
	return &trieSyncStats{}
}

// Stub out all the metrics methods
func (t *trieSyncStats) incTriesSegmented() {}
func (t *trieSyncStats) incLeafs(segment *trieSegment, count uint64, remaining uint64) {}
func (t *trieSyncStats) trieDone(root common.Hash) {}
func (t *trieSyncStats) setTriesRemaining(triesRemaining int) {}
func (t *trieSyncStats) estimateSegmentsInProgressTime() time.Duration { return 0 }
func (t *trieSyncStats) updateETA(sinceUpdate time.Duration, now time.Time) time.Duration { return 0 }