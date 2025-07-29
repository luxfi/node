// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/chain"
)

// Linearizer represents beam-based linear chain consensus
// (Previously known as Snowman in Avalanche)
//
// The beam linearizer ensures blocks form a single coherent
// beam of light, with each block building on exactly one parent,
// creating a linear history.
type Linearizer interface {
	// Add a new block to the beam
	Add(ctx context.Context, block Block) error

	// Vote for a block preference
	Vote(ctx context.Context, blockID ids.ID) error

	// RecordPoll records the result of photon sampling
	RecordPoll(ctx context.Context, votes map[ids.ID]int) error

	// Preferred returns the current preferred block
	Preferred() ids.ID

	// Finalized returns finalized blocks since last call
	Finalized() []ids.ID

	// IsFinalized checks if a block has been finalized
	IsFinalized(blockID ids.ID) bool

	// HealthCheck returns consensus health metrics
	HealthCheck(ctx context.Context) (Health, error)
}

// Block represents a block in the linear beam
// (Previously snowman.Block)
type Block interface {
	linear.Block

	// BeamHeight returns the block's position in the beam
	BeamHeight() uint64

	// BeamScore returns the block's consensus score
	BeamScore() uint64

	// Photons returns the number of photon queries for this block
	Photons() int
}

// BeamState represents the current state of linear consensus
type BeamState struct {
	PreferredID      ids.ID
	PreferredHeight  uint64
	FinalizedID      ids.ID
	FinalizedHeight  uint64
	OutstandingBlocks int
	BeamIntensity    int // Consensus strength (0-100)
}

// Health represents consensus health metrics
type Health struct {
	Healthy           bool
	BeamCoherence     float64 // 0-1, how coherent the beam is
	FocusStrength     int     // Current focus strength
	LastPollTime      time.Time
	ConsecutivePolls  int
	OutstandingBlocks int
}

// BeamMetrics tracks beam consensus performance
type BeamMetrics struct {
	BlocksAdded      uint64
	BlocksFinalized  uint64
	PollsProcessed   uint64
	BeamSplits       uint64 // Times the beam split (reorgs)
	AveragePollTime  time.Duration
	AverageFocusTime time.Duration
}