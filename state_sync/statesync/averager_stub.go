//go:build evm_nostats

package statesync

import "time"

// averager is a stub when stats are disabled
type averager struct{}

func newAverager(initial float64, halfLife time.Duration, now time.Time) *averager {
	return &averager{}
}

func (a *averager) Observe(value float64, now time.Time) {}
func (a *averager) Read() float64 { return 0 }

func estimateETA(start time.Time, done, total uint64) time.Duration { return 0 }