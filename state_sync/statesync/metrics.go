//go:build !evm_nostats

package statesync

import (
	"time"
	"github.com/luxfi/geth/common"
)

// trieSyncStats tracks statistics for trie sync operations
type trieSyncStats struct{}

func newTrieSyncStats() *trieSyncStats {
	return &trieSyncStats{}
}

// Stub implementations for now
func (t *trieSyncStats) incTriesSegmented() {}
func (t *trieSyncStats) incLeafs(segment *trieSegment, count uint64, remaining uint64) {}
func (t *trieSyncStats) trieDone(root common.Hash) {}
func (t *trieSyncStats) setTriesRemaining(triesRemaining int) {}
func (t *trieSyncStats) estimateSegmentsInProgressTime() time.Duration { return 0 }
func (t *trieSyncStats) updateETA(sinceUpdate time.Duration, now time.Time) time.Duration { return 0 }