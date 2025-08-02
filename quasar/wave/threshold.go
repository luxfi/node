// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package wave

import (
	"github.com/luxfi/ids"
)

// Threshold represents wave interference patterns for vote counting
// (Previously known as Quorum in Avalanche consensus)
//
// When photon reflections (votes) return, they create interference
// patterns. Constructive interference above the threshold indicates
// consensus forming.
type Threshold interface {
	// AddVote adds a vote and returns if threshold is reached
	AddVote(nodeID ids.NodeID, choice ids.ID) bool

	// GetThreshold returns the current alpha threshold
	GetThreshold() int

	// GetVoteCount returns votes for a specific choice
	GetVoteCount(choice ids.ID) int

	// GetTotalVotes returns total votes received
	GetTotalVotes() int

	// GetLeader returns the current leading choice and its vote count
	GetLeader() (ids.ID, int)

	// Reset clears all votes
	Reset()
}

// WavePattern represents the voting state at a given moment
// (Previously QuorumState)
type WavePattern struct {
	Choices    map[ids.ID]int // Vote counts per choice
	TotalVotes int
	Threshold  int // Alpha threshold
}

// IsConstructive returns true if the pattern shows constructive interference
// (i.e., one choice has exceeded the threshold)
func (w *WavePattern) IsConstructive() bool {
	for _, votes := range w.Choices {
		if votes >= w.Threshold {
			return true
		}
	}
	return false
}

// GetAmplitude returns the "amplitude" (vote count) for a choice
func (w *WavePattern) GetAmplitude(choice ids.ID) int {
	return w.Choices[choice]
}

// ThresholdType identifies the type of threshold mechanism
type ThresholdType int

const (
	Static ThresholdType = iota
	Dynamic
	Adaptive
)